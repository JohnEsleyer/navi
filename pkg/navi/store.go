package navi

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpenChatStore creates or opens a chat store at dbPath. If dbPath is empty, a
// default path under the user config directory is used.
//
// Three backends are tried in order of fidelity, because "offline" must never
// mean "fails to start":
//
//  1. A registered database/sql driver (modernc.org/sqlite, mattn/go-sqlite3),
//     if the host binary happens to import one.
//  2. The sqlite3 CLI, which needs no cgo and gives real SQLite.
//  3. A pure-stdlib JSON store, so the feature still works with neither.
//
// Callers only ever see ChatAPI, so which one was chosen is invisible to them.
func OpenChatStore(dbPath string) (ChatAPI, error) {
	if dbPath == "" {
		dbPath = defaultDBPath()
	}

	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	// 1. A registered SQL driver, if any.
	for _, driver := range sql.Drivers() {
		if driver == "sqlite3" || driver == "sqlite" {
			db, err := sql.Open(driver, dbPath)
			if err == nil && db.Ping() == nil {
				s := &sqlDriverStore{db: db}
				if err := s.initSchema(); err == nil {
					return s, nil
				}
			}
		}
	}

	// 2. The sqlite3 CLI (no cgo). Requires -json, so it is probed rather than
	// assumed: an older CLI that cannot produce JSON must not be selected,
	// or every read would fail at query time instead of here.
	if _, err := exec.LookPath("sqlite3"); err == nil && dbPath != ":memory:" {
		s := &sqliteCLIStore{dbPath: dbPath}
		if err := s.probe(); err == nil {
			if err := s.initSchema(); err == nil {
				return s, nil
			}
		}
	}

	// 3. Pure-stdlib fallback: always succeeds.
	s, err := newFallbackStore(dbPath)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func defaultDBPath() string {
	if env := os.Getenv("NAVI_DB_PATH"); env != "" {
		return env
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "navi.db"
	}
	return filepath.Join(dir, "navi", "navi.db")
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const schemaSQL = `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		model TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		prompt_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cost REAL DEFAULT 0.0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS model_history (
		model TEXT PRIMARY KEY,
		last_used INTEGER NOT NULL
	);`

// --- SQL Driver Implementation ---

type sqlDriverStore struct {
	db *sql.DB
}

func (s *sqlDriverStore) initSchema() error {
	_, err := s.db.Exec(schemaSQL)
	return err
}

func (s *sqlDriverStore) CreateSession(title, model string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        randomID(),
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec("INSERT INTO sessions (id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		sess.ID, sess.Title, sess.Model, sess.CreatedAt.UnixMilli(), sess.UpdatedAt.UnixMilli())
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *sqlDriverStore) GetSession(id string) (*Session, error) {
	var sess Session
	var cAt, uAt int64
	err := s.db.QueryRow("SELECT id, title, model, created_at, updated_at FROM sessions WHERE id = ?", id).
		Scan(&sess.ID, &sess.Title, &sess.Model, &cAt, &uAt)
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = time.UnixMilli(cAt)
	sess.UpdatedAt = time.UnixMilli(uAt)

	if msgs, err := s.GetMessages(id); err == nil {
		sess.Messages = msgs
	}
	return &sess, nil
}

func (s *sqlDriverStore) ListSessions() ([]*Session, error) {
	return s.ListSessionsFiltered(SessionFilter{})
}

func (s *sqlDriverStore) ListSessionsFiltered(filter SessionFilter) ([]*Session, error) {
	query := "SELECT id, title, model, created_at, updated_at FROM sessions"
	var args []any
	if filter.Query != "" {
		query += " WHERE title LIKE ?"
		args = append(args, "%"+filter.Query+"%")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var sess Session
		var cAt, uAt int64
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Model, &cAt, &uAt); err != nil {
			return nil, err
		}
		sess.CreatedAt = time.UnixMilli(cAt)
		sess.UpdatedAt = time.UnixMilli(uAt)
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

func (s *sqlDriverStore) UpdateSessionTitle(id, title string) error {
	_, err := s.db.Exec("UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?", title, time.Now().UnixMilli(), id)
	return err
}

func (s *sqlDriverStore) UpdateSessionModel(id, model string) error {
	_, err := s.db.Exec("UPDATE sessions SET model = ?, updated_at = ? WHERE id = ?", model, time.Now().UnixMilli(), id)
	return err
}

func (s *sqlDriverStore) DeleteSession(id string) error {
	if _, err := s.db.Exec("DELETE FROM messages WHERE session_id = ?", id); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

func (s *sqlDriverStore) AddMessage(sessionID string, msg MessageRecord) (*MessageRecord, error) {
	if msg.ID == "" {
		msg.ID = randomID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.SessionID = sessionID

	_, err := s.db.Exec(`INSERT INTO messages (id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, msg.Content, msg.PromptTokens, msg.OutputTokens, msg.Cost, msg.CreatedAt.UnixMilli())
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec("UPDATE sessions SET updated_at = ? WHERE id = ?", msg.CreatedAt.UnixMilli(), sessionID)
	return &msg, nil
}

func (s *sqlDriverStore) GetMessages(sessionID string) ([]MessageRecord, error) {
	rows, err := s.db.Query(`SELECT id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at
		FROM messages WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		var cAt int64
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.PromptTokens, &m.OutputTokens, &m.Cost, &cAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(cAt)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *sqlDriverStore) GetMessage(messageID string) (*MessageRecord, error) {
	var m MessageRecord
	var cAt int64
	err := s.db.QueryRow(`SELECT id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at
		FROM messages WHERE id = ?`, messageID).
		Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.PromptTokens, &m.OutputTokens, &m.Cost, &cAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.UnixMilli(cAt)
	return &m, nil
}

func (s *sqlDriverStore) UpdateMessageContent(messageID, newContent string) error {
	_, err := s.db.Exec("UPDATE messages SET content = ? WHERE id = ?", newContent, messageID)
	return err
}

func (s *sqlDriverStore) DeleteMessage(messageID string) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE id = ?", messageID)
	return err
}

func (s *sqlDriverStore) ClearSessionMessages(sessionID string) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	return err
}

func (s *sqlDriverStore) GetModelHistory() ([]string, error) {
	rows, err := s.db.Query("SELECT model FROM model_history ORDER BY last_used DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil && m != "" {
			models = append(models, m)
		}
	}
	return models, rows.Err()
}

func (s *sqlDriverStore) SaveModelHistory(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO model_history (model, last_used) VALUES (?, ?)
		ON CONFLICT(model) DO UPDATE SET last_used = excluded.last_used`, model, time.Now().UnixMilli())
	return err
}

func (s *sqlDriverStore) DeleteModelHistory(model string) error {
	_, err := s.db.Exec("DELETE FROM model_history WHERE model = ?", strings.TrimSpace(model))
	return err
}

func (s *sqlDriverStore) Close() error {
	return s.db.Close()
}

// --- SQLite CLI Implementation (no cgo required) ---
//
// The CLI takes no placeholders, so values are escaped rather than bound.
// esc() doubles single quotes, which is SQLite's own rule; anything a caller
// passes through here therefore arrives as data, never as SQL.

type sqliteCLIStore struct {
	dbPath string
	mu     sync.Mutex
}

// probe confirms the installed CLI is new enough to emit JSON. Without this a
// missing -json flag would surface later as a parse error on every read.
func (s *sqliteCLIStore) probe() error {
	out, err := exec.Command("sqlite3", "-json", s.dbPath, "SELECT 1;").Output()
	if err != nil {
		return fmt.Errorf("sqlite3 -json unsupported: %w", err)
	}
	if !strings.Contains(string(out), "1") {
		return fmt.Errorf("sqlite3 -json returned no rows")
	}
	return nil
}

func (s *sqliteCLIStore) initSchema() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return exec.Command("sqlite3", s.dbPath, schemaSQL).Run()
}

func (s *sqliteCLIStore) execSQL(query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.Command("sqlite3", s.dbPath, query)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *sqliteCLIStore) queryJSON(query string, dest any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.Command("sqlite3", "-json", s.dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return json.Unmarshal([]byte(trimmed), dest)
}

func esc(str string) string {
	return strings.ReplaceAll(str, "'", "''")
}

func (s *sqliteCLIStore) CreateSession(title, model string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        randomID(),
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	query := fmt.Sprintf("INSERT INTO sessions (id, title, model, created_at, updated_at) VALUES ('%s', '%s', '%s', %d, %d);",
		sess.ID, esc(sess.Title), esc(sess.Model), sess.CreatedAt.UnixMilli(), sess.UpdatedAt.UnixMilli())
	if err := s.execSQL(query); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *sqliteCLIStore) GetSession(id string) (*Session, error) {
	var rows []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Model     string `json:"model"`
		CreatedAt int64  `json:"created_at"`
		UpdatedAt int64  `json:"updated_at"`
	}
	q := fmt.Sprintf("SELECT id, title, model, created_at, updated_at FROM sessions WHERE id = '%s';", esc(id))
	if err := s.queryJSON(q, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("session not found")
	}
	sess := &Session{
		ID:        rows[0].ID,
		Title:     rows[0].Title,
		Model:     rows[0].Model,
		CreatedAt: time.UnixMilli(rows[0].CreatedAt),
		UpdatedAt: time.UnixMilli(rows[0].UpdatedAt),
	}
	if msgs, err := s.GetMessages(id); err == nil {
		sess.Messages = msgs
	}
	return sess, nil
}

func (s *sqliteCLIStore) ListSessions() ([]*Session, error) {
	return s.ListSessionsFiltered(SessionFilter{})
}

func (s *sqliteCLIStore) ListSessionsFiltered(filter SessionFilter) ([]*Session, error) {
	q := "SELECT id, title, model, created_at, updated_at FROM sessions"
	if filter.Query != "" {
		q += fmt.Sprintf(" WHERE title LIKE '%%%s%%'", esc(filter.Query))
	}
	q += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}
	q += ";"

	var rows []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Model     string `json:"model"`
		CreatedAt int64  `json:"created_at"`
		UpdatedAt int64  `json:"updated_at"`
	}
	if err := s.queryJSON(q, &rows); err != nil {
		return nil, err
	}
	var out []*Session
	for _, r := range rows {
		out = append(out, &Session{
			ID:        r.ID,
			Title:     r.Title,
			Model:     r.Model,
			CreatedAt: time.UnixMilli(r.CreatedAt),
			UpdatedAt: time.UnixMilli(r.UpdatedAt),
		})
	}
	return out, nil
}

func (s *sqliteCLIStore) UpdateSessionTitle(id, title string) error {
	return s.execSQL(fmt.Sprintf("UPDATE sessions SET title = '%s', updated_at = %d WHERE id = '%s';",
		esc(title), time.Now().UnixMilli(), esc(id)))
}

func (s *sqliteCLIStore) UpdateSessionModel(id, model string) error {
	return s.execSQL(fmt.Sprintf("UPDATE sessions SET model = '%s', updated_at = %d WHERE id = '%s';",
		esc(model), time.Now().UnixMilli(), esc(id)))
}

func (s *sqliteCLIStore) DeleteSession(id string) error {
	return s.execSQL(fmt.Sprintf(
		"DELETE FROM messages WHERE session_id = '%s'; DELETE FROM sessions WHERE id = '%s';",
		esc(id), esc(id)))
}

func (s *sqliteCLIStore) AddMessage(sessionID string, msg MessageRecord) (*MessageRecord, error) {
	if msg.ID == "" {
		msg.ID = randomID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.SessionID = sessionID
	query := fmt.Sprintf(`INSERT INTO messages (id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at)
		VALUES ('%s', '%s', '%s', '%s', %d, %d, %f, %d);
		UPDATE sessions SET updated_at = %d WHERE id = '%s';`,
		msg.ID, esc(msg.SessionID), esc(msg.Role), esc(msg.Content),
		msg.PromptTokens, msg.OutputTokens, msg.Cost, msg.CreatedAt.UnixMilli(),
		msg.CreatedAt.UnixMilli(), esc(sessionID))
	if err := s.execSQL(query); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *sqliteCLIStore) GetMessages(sessionID string) ([]MessageRecord, error) {
	var rows []struct {
		ID           string  `json:"id"`
		SessionID    string  `json:"session_id"`
		Role         string  `json:"role"`
		Content      string  `json:"content"`
		PromptTokens int     `json:"prompt_tokens"`
		OutputTokens int     `json:"output_tokens"`
		Cost         float64 `json:"cost"`
		CreatedAt    int64   `json:"created_at"`
	}
	q := fmt.Sprintf(`SELECT id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at
		FROM messages WHERE session_id = '%s' ORDER BY created_at ASC;`, esc(sessionID))
	if err := s.queryJSON(q, &rows); err != nil {
		return nil, err
	}
	var out []MessageRecord
	for _, r := range rows {
		out = append(out, MessageRecord{
			ID:           r.ID,
			SessionID:    r.SessionID,
			Role:         r.Role,
			Content:      r.Content,
			PromptTokens: r.PromptTokens,
			OutputTokens: r.OutputTokens,
			Cost:         r.Cost,
			CreatedAt:    time.UnixMilli(r.CreatedAt),
		})
	}
	return out, nil
}

func (s *sqliteCLIStore) GetMessage(messageID string) (*MessageRecord, error) {
	var rows []struct {
		ID           string  `json:"id"`
		SessionID    string  `json:"session_id"`
		Role         string  `json:"role"`
		Content      string  `json:"content"`
		PromptTokens int     `json:"prompt_tokens"`
		OutputTokens int     `json:"output_tokens"`
		Cost         float64 `json:"cost"`
		CreatedAt    int64   `json:"created_at"`
	}
	q := fmt.Sprintf(`SELECT id, session_id, role, content, prompt_tokens, output_tokens, cost, created_at
		FROM messages WHERE id = '%s';`, esc(messageID))
	if err := s.queryJSON(q, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	return &MessageRecord{
		ID:           rows[0].ID,
		SessionID:    rows[0].SessionID,
		Role:         rows[0].Role,
		Content:      rows[0].Content,
		PromptTokens: rows[0].PromptTokens,
		OutputTokens: rows[0].OutputTokens,
		Cost:         rows[0].Cost,
		CreatedAt:    time.UnixMilli(rows[0].CreatedAt),
	}, nil
}

func (s *sqliteCLIStore) UpdateMessageContent(messageID, newContent string) error {
	return s.execSQL(fmt.Sprintf("UPDATE messages SET content = '%s' WHERE id = '%s';",
		esc(newContent), esc(messageID)))
}

func (s *sqliteCLIStore) DeleteMessage(messageID string) error {
	return s.execSQL(fmt.Sprintf("DELETE FROM messages WHERE id = '%s';", esc(messageID)))
}

func (s *sqliteCLIStore) ClearSessionMessages(sessionID string) error {
	return s.execSQL(fmt.Sprintf("DELETE FROM messages WHERE session_id = '%s';", esc(sessionID)))
}

func (s *sqliteCLIStore) GetModelHistory() ([]string, error) {
	var rows []struct {
		Model string `json:"model"`
	}
	if err := s.queryJSON("SELECT model FROM model_history ORDER BY last_used DESC;", &rows); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.Model != "" {
			out = append(out, r.Model)
		}
	}
	return out, nil
}

func (s *sqliteCLIStore) SaveModelHistory(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	return s.execSQL(fmt.Sprintf(`INSERT INTO model_history (model, last_used) VALUES ('%s', %d)
		ON CONFLICT(model) DO UPDATE SET last_used = excluded.last_used;`, esc(model), time.Now().UnixMilli()))
}

func (s *sqliteCLIStore) DeleteModelHistory(model string) error {
	return s.execSQL(fmt.Sprintf("DELETE FROM model_history WHERE model = '%s';", esc(strings.TrimSpace(model))))
}

func (s *sqliteCLIStore) Close() error {
	return nil
}

// --- JSON fallback (no driver, no sqlite3 binary) ---

type fallbackStore struct {
	path     string
	mu       sync.RWMutex
	sessions map[string]*Session
	messages map[string][]MessageRecord
	history  map[string]int64
}

func newFallbackStore(path string) (*fallbackStore, error) {
	store := &fallbackStore{
		path:     path,
		sessions: make(map[string]*Session),
		messages: make(map[string][]MessageRecord),
		history:  make(map[string]int64),
	}
	_ = store.load()
	return store, nil
}

type fallbackData struct {
	Sessions map[string]*Session        `json:"sessions"`
	Messages map[string][]MessageRecord `json:"messages"`
	History  map[string]int64           `json:"history"`
}

func (f *fallbackStore) filePath() string {
	return f.path + ".json"
}

func (f *fallbackStore) save() error {
	if f.path == ":memory:" {
		return nil
	}
	data, err := json.MarshalIndent(fallbackData{
		Sessions: f.sessions,
		Messages: f.messages,
		History:  f.history,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.filePath(), data, 0600)
}

func (f *fallbackStore) load() error {
	if f.path == ":memory:" {
		return nil
	}
	data, err := os.ReadFile(f.filePath())
	if err != nil {
		return err
	}
	var fd fallbackData
	if err := json.Unmarshal(data, &fd); err != nil {
		return err
	}
	if fd.Sessions != nil {
		f.sessions = fd.Sessions
	}
	if fd.Messages != nil {
		f.messages = fd.Messages
	}
	if fd.History != nil {
		f.history = fd.History
	}
	return nil
}

func (f *fallbackStore) CreateSession(title, model string) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	s := &Session{
		ID:        randomID(),
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.sessions[s.ID] = s
	_ = f.save()
	return s, nil
}

func (f *fallbackStore) GetSession(id string) (*Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	copied := *s
	copied.Messages = append([]MessageRecord(nil), f.messages[id]...)
	return &copied, nil
}

func (f *fallbackStore) ListSessions() ([]*Session, error) {
	return f.ListSessionsFiltered(SessionFilter{})
}

func (f *fallbackStore) ListSessionsFiltered(filter SessionFilter) ([]*Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*Session
	for _, s := range f.sessions {
		if filter.Query == "" || strings.Contains(strings.ToLower(s.Title), strings.ToLower(filter.Query)) {
			copied := *s
			list = append(list, &copied)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	if filter.Offset > 0 {
		if filter.Offset >= len(list) {
			return []*Session{}, nil
		}
		list = list[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(list) {
		list = list[:filter.Limit]
	}
	return list, nil
}

func (f *fallbackStore) UpdateSessionTitle(id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	s.Title = title
	s.UpdatedAt = time.Now()
	return f.save()
}

func (f *fallbackStore) UpdateSessionModel(id, model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	s.Model = model
	s.UpdatedAt = time.Now()
	return f.save()
}

func (f *fallbackStore) DeleteSession(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	delete(f.messages, id)
	return f.save()
}

func (f *fallbackStore) AddMessage(sessionID string, msg MessageRecord) (*MessageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if msg.ID == "" {
		msg.ID = randomID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.SessionID = sessionID
	f.messages[sessionID] = append(f.messages[sessionID], msg)
	if s, ok := f.sessions[sessionID]; ok {
		s.UpdatedAt = msg.CreatedAt
	}
	_ = f.save()
	return &msg, nil
}

func (f *fallbackStore) GetMessages(sessionID string) ([]MessageRecord, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return append([]MessageRecord(nil), f.messages[sessionID]...), nil
}

func (f *fallbackStore) GetMessage(messageID string) (*MessageRecord, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, msgs := range f.messages {
		for _, m := range msgs {
			if m.ID == messageID {
				copied := m
				return &copied, nil
			}
		}
	}
	return nil, fmt.Errorf("message not found")
}

func (f *fallbackStore) UpdateMessageContent(messageID, newContent string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sid, msgs := range f.messages {
		for i := range msgs {
			if msgs[i].ID == messageID {
				msgs[i].Content = newContent
				f.messages[sid] = msgs
				return f.save()
			}
		}
	}
	return fmt.Errorf("message not found")
}

func (f *fallbackStore) DeleteMessage(messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sid, msgs := range f.messages {
		for i := range msgs {
			if msgs[i].ID == messageID {
				f.messages[sid] = append(msgs[:i], msgs[i+1:]...)
				return f.save()
			}
		}
	}
	return fmt.Errorf("message not found")
}

func (f *fallbackStore) ClearSessionMessages(sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.messages, sessionID)
	return f.save()
}

func (f *fallbackStore) GetModelHistory() ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	type pair struct {
		model string
		time  int64
	}
	var pairs []pair
	for m, t := range f.history {
		pairs = append(pairs, pair{model: m, time: t})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].time > pairs[j].time
	})
	var out []string
	for _, p := range pairs {
		out = append(out, p.model)
	}
	return out, nil
}

func (f *fallbackStore) SaveModelHistory(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history[model] = time.Now().UnixMilli()
	return f.save()
}

func (f *fallbackStore) DeleteModelHistory(model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.history, strings.TrimSpace(model))
	return f.save()
}

func (f *fallbackStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.save()
}
