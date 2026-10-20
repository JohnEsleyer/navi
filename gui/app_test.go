package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		"/key", "/baseurl", "/clear", "/dump", "/nosuchcmd", "/",
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

// TestKeyAndBaseURLCommandsAreFree guards the same billing bug for the new
// key/endpoint commands: inspecting or setting a key must never reach the API.
func TestKeyAndBaseURLCommandsAreFree(t *testing.T) {
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

	// Inspect (no args) and set (with args) must both be free.
	for _, cmd := range []string{"/key", "/key my-new-token", "/baseurl", "/baseurl http://example.invalid/v1"} {
		res, err := app.Ask(cmd)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", cmd, err)
		}
		if res.Meta.TurnCost != 0 || res.Meta.PromptTokens != 0 {
			t.Errorf("%q: reported billed tokens: %+v", cmd, res.Meta)
		}
	}

	if requests != 0 {
		t.Errorf("key/baseurl commands reached the LLM API %d time(s); they must be free", requests)
	}

	// The key set above must actually have been applied.
	if app.GetAPIKey() != "my-new-token" {
		t.Errorf("key was not set, got: %q", app.GetAPIKey())
	}
	if app.GetBaseURL() != "http://example.invalid/v1" {
		t.Errorf("baseurl was not set, got: %q", app.GetBaseURL())
	}
	// Display must never expose the raw secret.
	if app.MaskedAPIKey() == "my-new-token" {
		t.Error("MaskedAPIKey returned the raw key")
	}
}

// TestSettingsSurviveRestart proves the GUI remembers what the user set.
// The complaint was that every launch started over: a blank API key and the
// default model, no matter what was chosen the day before.
func TestSettingsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NAVI_DB_PATH", filepath.Join(dir, "navi.db"))
	// Keep the ambient environment out of the picture: an explicit env var
	// overrides the persisted value by design.
	t.Setenv("NAVI_MODEL", "")
	t.Setenv("NAVI_API_KEY", "")
	t.Setenv("NAVI_BASE_URL", "")

	first := NewApp()
	first.SetModel("gpt-4o-mini")
	first.SetAPIKey("sk-gui-key")

	// A second launch of the app must pick both values back up.
	second := NewApp()
	if got := second.GetStats().Model; got != "gpt-4o-mini" {
		t.Errorf("model was not persisted across restarts: got %q, want %q", got, "gpt-4o-mini")
	}
	if got := second.GetAPIKey(); got != "sk-gui-key" {
		t.Errorf("api key was not persisted across restarts: got %q, want %q", got, "sk-gui-key")
	}
}

// TestGetWorkingDir backs the path badge in the chat header: it must report
// the directory the action commands actually run in.
func TestGetWorkingDir(t *testing.T) {
	app := NewApp()
	got := app.GetWorkingDir()
	if got == "" {
		t.Fatal("GetWorkingDir returned nothing")
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got != want {
		t.Errorf("GetWorkingDir = %q, want %q", got, want)
	}
}
