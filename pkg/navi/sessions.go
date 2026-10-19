package navi

import "strings"

// Session management builds on ChatAPI, so callers (and the CLI/GUI) never
// touch storage directly. Every method is nil-guarded on e.store: New()
// degrades to a store-less engine rather than refusing to start, and a missing
// store must surface as an error or no-op, never a panic.

// ChatAPI exposes the offline storage interface for software wrappers that
// want direct programmatic access without going through the Engine.
func (e *Engine) ChatAPI() ChatAPI {
	return e.store
}

// ActiveSession returns a copy of the active session. Returning a copy rather
// than the internal pointer keeps callers from reading fields that a
// concurrent RenameSession/SwitchSession could be mutating.
func (e *Engine) ActiveSession() *Session {
	e.sessionMu.RLock()
	defer e.sessionMu.RUnlock()
	if e.activeSession == nil {
		return nil
	}
	copied := *e.activeSession
	return &copied
}

func (e *Engine) ListSessions() ([]*Session, error) {
	if e.store == nil {
		return nil, nil
	}
	return e.store.ListSessions()
}

// CreateSession starts a new conversation, clears the in-memory context window
// and makes it active. An empty title is normalized so sessions are always
// identifiable in listings.
func (e *Engine) CreateSession(title string) (*Session, error) {
	if e.store == nil {
		return nil, nil
	}
	if strings.TrimSpace(title) == "" {
		title = "New Chat"
	}
	sess, err := e.store.CreateSession(title, e.model())
	if err != nil {
		return nil, err
	}

	e.sessionMu.Lock()
	e.activeSession = sess
	e.sessionMu.Unlock()

	e.ResetHistory()
	e.ClearCache()

	copied := *sess
	return &copied, nil
}

// SwitchSession makes an existing conversation active and rebuilds the context
// window from its stored messages. The session's model is adopted, because a
// resumed conversation should answer with the model that wrote it.
func (e *Engine) SwitchSession(id string) (*Session, error) {
	if e.store == nil {
		return nil, nil
	}
	sess, err := e.store.GetSession(id)
	if err != nil {
		return nil, err
	}

	e.sessionMu.Lock()
	e.activeSession = sess
	e.sessionMu.Unlock()

	e.loadSessionHistory(id)
	e.ClearCache()

	if sess.Model != "" && sess.Model != e.model() {
		e.SetModel(sess.Model)
	}

	copied := *sess
	return &copied, nil
}

// DeleteSession removes a conversation. If it was the active one, the engine
// falls back to the most recently updated remaining session, or starts a fresh
// one, so there is never a dangling nil active session.
func (e *Engine) DeleteSession(id string) error {
	if e.store == nil {
		return nil
	}
	if err := e.store.DeleteSession(id); err != nil {
		return err
	}

	e.sessionMu.RLock()
	wasActive := e.activeSession != nil && e.activeSession.ID == id
	e.sessionMu.RUnlock()
	if !wasActive {
		return nil
	}

	// ListSessions orders by updated_at DESC, so index 0 is the newest.
	if sessions, err := e.store.ListSessions(); err == nil && len(sessions) > 0 {
		e.sessionMu.Lock()
		e.activeSession = sessions[0]
		e.sessionMu.Unlock()
		e.loadSessionHistory(sessions[0].ID)
		return nil
	}

	// Nothing left: start a fresh conversation.
	if sess, err := e.store.CreateSession("New Chat", e.model()); err == nil {
		e.sessionMu.Lock()
		e.activeSession = sess
		e.sessionMu.Unlock()
	}
	e.ResetHistory()
	e.ClearCache()
	return nil
}

// RenameSession updates a session's title and mirrors the change onto the
// in-memory copy when the renamed session is the active one.
func (e *Engine) RenameSession(id, title string) error {
	if e.store == nil {
		return nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	if err := e.store.UpdateSessionTitle(id, title); err != nil {
		return err
	}
	e.sessionMu.Lock()
	if e.activeSession != nil && e.activeSession.ID == id {
		e.activeSession.Title = title
	}
	e.sessionMu.Unlock()
	return nil
}

// GetSessionMessages returns the persisted turns of a session, for replaying
// into a UI. Unlike GetHistory it is the full log, not the sliding window.
func (e *Engine) GetSessionMessages(id string) []MessageRecord {
	if e.store == nil {
		return nil
	}
	msgs, err := e.store.GetMessages(id)
	if err != nil {
		return nil
	}
	return msgs
}

func (e *Engine) GetModelHistory() ([]string, error) {
	if e.store == nil {
		return nil, nil
	}
	return e.store.GetModelHistory()
}

func (e *Engine) SaveModelHistory(model string) error {
	if e.store == nil {
		return nil
	}
	return e.store.SaveModelHistory(model)
}

func (e *Engine) DeleteModelHistory(model string) error {
	if e.store == nil {
		return nil
	}
	return e.store.DeleteModelHistory(model)
}

// Close releases the persistence handle. The engine must not be used after.
func (e *Engine) Close() error {
	if e.store == nil {
		return nil
	}
	return e.store.Close()
}
