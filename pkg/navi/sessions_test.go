package navi

import (
	"path/filepath"
	"testing"
)

// TestSyncFromStoreAdoptsExternalWrites simulates the /gui handoff: a second
// store handle (as if opened by another process) writes a newer session, and
// the engine's SyncFromStore must adopt it instead of staying on the session
// it already had.
func TestSyncFromStoreAdoptsExternalWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync-test")

	// First handle: create one session plus a turn.
	first, err := OpenChatStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := first.CreateSession("solo", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// New() resumes the most recent session, so this engine starts on "solo".
	engine := New(Config{DBPath: dbPath})
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	// Second handle = the GUI's own view of the same db.
	second, err := OpenChatStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	guiSession, err := second.CreateSession("from gui", "llama-3.1-8b-instant")
	if err != nil {
		t.Fatalf("gui create session: %v", err)
	}
	if _, err := second.AddMessage(guiSession.ID, MessageRecord{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("gui add message: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}

	engine.SyncFromStore()

	active := engine.ActiveSession()
	if active == nil {
		t.Fatal("no active session after sync")
	}
	if active.ID != guiSession.ID {
		t.Fatalf("SyncFromStore did not adopt the newest session: got %q (%s), want %q",
			active.Title, active.ID, guiSession.ID)
	}
	// The engine prepends its system prompt, so the adopted turn is second.
	got := engine.GetHistory()
	if len(got) != 2 || got[1].Role != "user" || got[1].Content != "hello" {
		t.Fatalf("SyncFromStore did not load the adopted session's history: %v", got)
	}

	_ = engine.Close()
}
