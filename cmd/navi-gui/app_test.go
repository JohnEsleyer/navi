package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"navi/pkg/navi"
)

// newTestEngine builds an Engine pointed at the fake API.
func newTestEngine(baseURL string) *navi.Engine {
	return navi.New(navi.Config{BaseURL: baseURL, APIKey: "test", Model: "test-model"})
}

// TestSlashCommandsAreFree guards the bug where every slash command typed in
// the GUI was billed as a normal prompt, because the GUI had no command layer
// (unlike cmd/navi/main.go). A request reaching the fake API == real money.
func TestSlashCommandsAreFree(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "fake"}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer srv.Close()

	app := NewApp()
	app.engine = newTestEngine(srv.URL)
	app.startup(nil)

	commands := []string{
		"/help", "/stats", "/history", "/model", "/model gpt-4o-mini",
		"/clear", "/dump", "/nosuchcmd", "/",
	}
	for _, cmd := range commands {
		res, err := app.Ask(cmd)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", cmd, err)
		}
		if res.Meta.TurnCost != 0 || res.Meta.PromptTokens != 0 || res.Meta.OutputTokens != 0 {
			t.Errorf("%q: reported billed tokens: %+v", cmd, res.Meta)
		}
	}

	if requests != 0 {
		t.Errorf("slash commands reached the LLM API %d time(s); commands must be free", requests)
	}

	// A plain prompt must still reach the model.
	if _, err := app.Ask("hello"); err != nil {
		t.Fatalf("plain prompt: %v", err)
	}
	if requests != 1 {
		t.Errorf("plain prompt: expected exactly 1 API request, got %d", requests)
	}
}

// TestAskBeforeStartup ensures a prompt sent before Wails OnStartup doesn't
// fail with "net/http: nil Context".
func TestAskBeforeStartup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer srv.Close()

	app := NewApp()
	app.engine = newTestEngine(srv.URL)
	app.ctx = nil // simulate OnStartup not having fired

	if _, err := app.Ask("hello"); err != nil {
		t.Fatalf("Ask with nil ctx failed: %v", err)
	}
}
