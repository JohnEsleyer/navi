package navi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFakeAPI returns a server that replies with the given text, for tests that
// care about persistence rather than about request counting.
func newFakeAPI(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
}

// TestChatAPIOfflineOperations exercises the ChatAPI contract against whichever
// backend OpenChatStore selects. It asserts only on observable behaviour, so it
// passes identically on the driver, CLI-JSON and pure-stdlib fallback stores.
func TestChatAPIOfflineOperations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_chat.db")

	store, err := OpenChatStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChatStore failed: %v", err)
	}
	defer store.Close()

	// 1. Session creation & retrieval.
	sess, err := store.CreateSession("Initial Session", "gpt-4o-mini")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("CreateSession returned an empty session ID")
	}
	if sess.Title != "Initial Session" {
		t.Fatalf("title: got %q want %q", sess.Title, "Initial Session")
	}

	got, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.Title != "Initial Session" {
		t.Fatalf("GetSession title: got %q want %q", got.Title, "Initial Session")
	}

	// 2. Message CRUD, including content that would break naive SQL quoting.
	tricky := "it's got 'quotes' and a\nnewline; DROP TABLE sessions;--"
	if _, err := store.AddMessage(sess.ID, MessageRecord{
		Role:         "user",
		Content:      tricky,
		PromptTokens: 5,
	}); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	msgs, err := store.GetMessages(sess.ID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetMessages count: got %d want 1", len(msgs))
	}
	if msgs[0].Content != tricky {
		t.Fatalf("content round-trip: got %q want %q", msgs[0].Content, tricky)
	}
	if msgs[0].PromptTokens != 5 {
		t.Fatalf("PromptTokens: got %d want 5", msgs[0].PromptTokens)
	}

	// A single-quote string must not have destroyed the table.
	if _, err := store.ListSessions(); err != nil {
		t.Fatalf("sessions unreadable after quoting test: %v", err)
	}

	// Update & read back a single message.
	if err := store.UpdateMessageContent(msgs[0].ID, "rewritten"); err != nil {
		t.Fatalf("UpdateMessageContent failed: %v", err)
	}
	one, err := store.GetMessage(msgs[0].ID)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if one.Content != "rewritten" {
		t.Fatalf("updated content: got %q want %q", one.Content, "rewritten")
	}

	// 3. Model history CRUD.
	if err := store.SaveModelHistory("gpt-4o-mini"); err != nil {
		t.Fatalf("SaveModelHistory failed: %v", err)
	}
	if err := store.SaveModelHistory("llama-3.1-8b-instant"); err != nil {
		t.Fatalf("SaveModelHistory failed: %v", err)
	}
	// Saving the same model twice must not duplicate it.
	if err := store.SaveModelHistory("gpt-4o-mini"); err != nil {
		t.Fatalf("SaveModelHistory (dup) failed: %v", err)
	}

	hist, err := store.GetModelHistory()
	if err != nil {
		t.Fatalf("GetModelHistory failed: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("model history count: got %d want 2 (%v)", len(hist), hist)
	}

	if err := store.DeleteModelHistory("gpt-4o-mini"); err != nil {
		t.Fatalf("DeleteModelHistory failed: %v", err)
	}
	histAfter, _ := store.GetModelHistory()
	for _, m := range histAfter {
		if m == "gpt-4o-mini" {
			t.Fatal("gpt-4o-mini still present after DeleteModelHistory")
		}
	}

	// 4. Rename & delete session.
	if err := store.UpdateSessionTitle(sess.ID, "Renamed Session"); err != nil {
		t.Fatalf("UpdateSessionTitle failed: %v", err)
	}
	updated, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession after rename failed: %v", err)
	}
	if updated.Title != "Renamed Session" {
		t.Fatalf("renamed title: got %q want %q", updated.Title, "Renamed Session")
	}

	if err := store.DeleteSession(sess.ID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if _, err := store.GetSession(sess.ID); err == nil {
		t.Fatal("GetSession succeeded for a deleted session")
	}
}

// TestOpenChatStoreFallsBackToWorkingBackend guards the "always usable offline"
// property: whichever backend is chosen, the result must satisfy ChatAPI and
// perform a full round-trip. A backend that opens but cannot read or write
// would be worse than none at all, because New() cannot detect it.
func TestOpenChatStoreFallsBackToWorkingBackend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fallback.db")

	store, err := OpenChatStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChatStore failed: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("OpenChatStore returned a nil store")
	}

	sess, err := store.CreateSession("round trip", "m1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := store.AddMessage(sess.ID, MessageRecord{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	msgs, err := store.GetMessages(sess.ID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("round-trip: got %d messages, want 1 containing %q", len(msgs), "hi")
	}
}

// TestMemoryStorePersistsNothing checks that ":memory:" really is ephemeral:
// tests rely on it for isolation, and a memory path that accidentally wrote to
// a file named ":memory:" would leak state between runs.
func TestMemoryStorePersistsNothing(t *testing.T) {
	store, err := OpenChatStore(":memory:")
	if err != nil {
		t.Fatalf("OpenChatStore(:memory:) failed: %v", err)
	}
	defer store.Close()

	if _, err := store.CreateSession("temp", "m1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if _, err := os.Stat(":memory:"); err == nil {
		t.Fatal("a file named \":memory:\" was created on disk")
	}
}

// TestEnginePersistsTurnsToActiveSession verifies the Ask() integration writes
// both halves of an exchange, which is what makes a conversation survivable
// across restarts.
func TestEnginePersistsTurnsToActiveSession(t *testing.T) {
	srv := newFakeAPI(t, "reply text")
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})

	active := e.ActiveSession()
	if active == nil {
		t.Fatal("engine has no active session after New")
	}

	if _, err := e.Ask(t.Context(), "hello"); err != nil {
		t.Fatalf("Ask failed: %v", err)
	}

	msgs := e.GetSessionMessages(active.ID)
	if len(msgs) != 2 {
		t.Fatalf("persisted message count: got %d want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("first persisted message: got %+v, want user %q", msgs[0], "hello")
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "reply text" {
		t.Fatalf("second persisted message: got %+v, want assistant %q", msgs[1], "reply text")
	}
}

// TestNewResumesMostRecentSession is the restart path: a returning engine must
// rebuild its context window from the newest stored session rather than start
// blank, which is the whole point of persisting.
func TestNewResumesMostRecentSession(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "resume.db")
	srv := newFakeAPI(t, "first reply")
	defer srv.Close()

	e1 := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: dbPath})
	if _, err := e1.Ask(t.Context(), "remember me"); err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if err := e1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// A fresh engine over the same DB is a process restart.
	e2 := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: dbPath})
	defer e2.Close()

	active := e2.ActiveSession()
	if active == nil {
		t.Fatal("resumed engine has no active session")
	}

	history := e2.GetHistory()
	if len(history) < 3 {
		t.Fatalf("resumed history too short: got %d messages, want the prior exchange", len(history))
	}
	if history[0].Role != "system" {
		t.Fatalf("resumed history[0] role: got %q want system", history[0].Role)
	}
	if history[1].Content != "remember me" {
		t.Fatalf("resumed history[1]: got %q want %q", history[1].Content, "remember me")
	}
}

// TestSwitchSessionIsolatesConversations checks that sessions do not bleed into
// each other: swapping the active session must swap the context window too.
func TestSwitchSessionIsolatesConversations(t *testing.T) {
	srv := newFakeAPI(t, "r")
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})

	if _, err := e.Ask(t.Context(), "alpha"); err != nil {
		t.Fatalf("Ask failed: %v", err)
	}

	other, err := e.CreateSession("beta session")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// The new session starts with an empty window (system prompt only).
	if h := e.GetHistory(); len(h) != 1 {
		t.Fatalf("new session history: got %d messages, want 1 (system only)", len(h))
	}

	if _, err := e.SwitchSession(other.ID); err != nil {
		t.Fatalf("SwitchSession failed: %v", err)
	}
	if h := e.GetHistory(); len(h) != 1 {
		t.Fatalf("history in fresh session: got %d messages, want 1", len(h))
	}
}

// TestDeleteActiveSessionLeavesUsableEngine guards the dangling-active-session
// case: deleting the active session must leave the engine with a valid session
// and a clean window, not a nil pointer that panics on the next Ask.
func TestDeleteActiveSessionLeavesUsableEngine(t *testing.T) {
	srv := newFakeAPI(t, "r")
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})

	active := e.ActiveSession()
	if active == nil {
		t.Fatal("no active session after New")
	}

	if err := e.DeleteSession(active.ID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	replacement := e.ActiveSession()
	if replacement == nil {
		t.Fatal("active session is nil after deleting the previous one")
	}
	if replacement.ID == active.ID {
		t.Fatal("deleted session is still active")
	}
	if h := e.GetHistory(); len(h) != 1 {
		t.Fatalf("history after delete: got %d messages, want 1 (system only)", len(h))
	}

	// The engine must still be usable.
	if _, err := e.Ask(t.Context(), "still working?"); err != nil {
		t.Fatalf("Ask after DeleteSession failed: %v", err)
	}
}

// TestResumeOrderBreaksTimestampTies guards a defect the CLI test surfaced: two
// sessions created in the same second share updated_at, so ordering by that
// column alone left the tie unresolved and the engine resumed the wrong (older,
// just-created) session. Millisecond storage makes the order deterministic, so
// the most recently *touched* session is the one that resumes.
func TestResumeOrderBreaksTimestampTies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ties.db")
	store, err := OpenChatStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChatStore failed: %v", err)
	}
	defer store.Close()

	first, err := store.CreateSession("older created", "m1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	second, err := store.CreateSession("newer created", "m1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Rename the OLDER one so it is the most recently touched, even though it
	// was inserted first. If timestamps were second-resolution this would tie.
	if err := store.UpdateSessionTitle(first.ID, "touched last"); err != nil {
		t.Fatalf("UpdateSessionTitle failed: %v", err)
	}

	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("session count: got %d want 2", len(sessions))
	}
	if sessions[0].ID != first.ID {
		t.Fatalf("most-recent session: got %q (%q) want %q (%q)",
			sessions[0].ID, sessions[0].Title, first.ID, first.Title)
	}
	if sessions[1].ID != second.ID {
		t.Fatalf("older session: got %q want %q", sessions[1].ID, second.ID)
	}

	// Timestamps must be fine-grained enough to actually distinguish the two.
	if !sessions[0].UpdatedAt.After(sessions[1].UpdatedAt) {
		t.Fatalf("updated_at not ordered: %v vs %v (equal => sub-second precision missing)",
			sessions[0].UpdatedAt, sessions[1].UpdatedAt)
	}
}

// TestModelHistoryViaEngine confirms the GUI combobox path (save/list/delete)
// works end to end through the engine.
func TestModelHistoryViaEngine(t *testing.T) {
	e := New(Config{Model: "m1", DBPath: ":memory:"})
	defer e.Close()

	if err := e.SaveModelHistory("alpha-model"); err != nil {
		t.Fatalf("SaveModelHistory failed: %v", err)
	}
	if err := e.SaveModelHistory("beta-model"); err != nil {
		t.Fatalf("SaveModelHistory failed: %v", err)
	}

	hist, err := e.GetModelHistory()
	if err != nil {
		t.Fatalf("GetModelHistory failed: %v", err)
	}
	joined := strings.Join(hist, ",")
	if !strings.Contains(joined, "alpha-model") || !strings.Contains(joined, "beta-model") {
		t.Fatalf("model history missing entries: got %v", hist)
	}

	if err := e.DeleteModelHistory("alpha-model"); err != nil {
		t.Fatalf("DeleteModelHistory failed: %v", err)
	}
	histAfter, _ := e.GetModelHistory()
	for _, m := range histAfter {
		if m == "alpha-model" {
			t.Fatal("alpha-model still present after DeleteModelHistory")
		}
	}
}
