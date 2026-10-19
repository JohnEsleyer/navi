package navi

import "time"

// Session represents a conversation thread stored in the chat database.
type Session struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Model     string          `json:"model"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Messages  []MessageRecord `json:"messages,omitempty"`
}

// MessageRecord represents an individual user or assistant turn persisted in
// the chat database.
type MessageRecord struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	PromptTokens int       `json:"prompt_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Cost         float64   `json:"cost"`
}

// SessionFilter allows querying and paginating conversation sessions.
type SessionFilter struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// ChatAPI is the programmatic interface for software wrappers to manage chat
// sessions, messages and model history offline. It deliberately hides the
// storage backend: callers never see tables or queries, so the implementation
// can move between a driver, the sqlite3 CLI and the JSON fallback without
// touching any caller.
type ChatAPI interface {
	// Session CRUD
	CreateSession(title, model string) (*Session, error)
	GetSession(id string) (*Session, error)
	ListSessions() ([]*Session, error)
	ListSessionsFiltered(filter SessionFilter) ([]*Session, error)
	UpdateSessionTitle(id, title string) error
	UpdateSessionModel(id, model string) error
	DeleteSession(id string) error

	// Message CRUD
	AddMessage(sessionID string, msg MessageRecord) (*MessageRecord, error)
	GetMessages(sessionID string) ([]MessageRecord, error)
	GetMessage(messageID string) (*MessageRecord, error)
	UpdateMessageContent(messageID, newContent string) error
	DeleteMessage(messageID string) error
	ClearSessionMessages(sessionID string) error

	// Model History
	GetModelHistory() ([]string, error)
	SaveModelHistory(model string) error
	DeleteModelHistory(model string) error

	// Lifecycle
	Close() error
}

// NewChatAPI creates or opens an offline chat database at dbPath.
// If dbPath is empty, it uses the default local storage path (~/.config/navi/navi.db).
// This is the direct entry point for third-party programs and software wrappers.
func NewChatAPI(dbPath string) (ChatAPI, error) {
	return OpenChatStore(dbPath)
}
