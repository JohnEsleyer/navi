package navi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- Persistent settings ---

// TestSettingsSurviveRestart is the core of the "nothing is remembered"
// complaint: the model and the API key the user picked must come back when
// the app is started again, instead of the default model and an empty key.
func TestSettingsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "navi.db")

	first := New(Config{DBPath: dbPath, Model: "gpt-4o-mini"})
	first.SetModel("gpt-4o-mini")
	first.SetAPIKey("sk-persisted-key")

	settingsPath := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings file was not written: %v", err)
	}

	// A fresh engine with nothing supplied must pick both values back up.
	second := New(Config{DBPath: dbPath})
	if got := second.GetStats().Model; got != "gpt-4o-mini" {
		t.Errorf("model was not restored: got %q, want %q", got, "gpt-4o-mini")
	}
	if got := second.GetAPIKey(); got != "sk-persisted-key" {
		t.Errorf("api key was not restored: got %q, want %q", got, "sk-persisted-key")
	}
}

// TestExplicitConfigBeatsPersistedSettings: an explicitly supplied value is a
// deliberate override and must not be overwritten by the saved one.
func TestExplicitConfigBeatsPersistedSettings(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "navi.db")

	first := New(Config{DBPath: dbPath})
	first.SetModel("gpt-4o-mini")
	first.SetAPIKey("sk-persisted-key")

	second := New(Config{DBPath: dbPath, Model: "gpt-4o", APIKey: "sk-explicit"})
	if got := second.GetStats().Model; got != "gpt-4o" {
		t.Errorf("explicit model was overridden: got %q", got)
	}
	if got := second.GetAPIKey(); got != "sk-explicit" {
		t.Errorf("explicit api key was overridden: got %q", got)
	}
}

// TestSettingsFileIsNotWorldReadable: the file holds a secret, so it must not
// be readable by anyone but the owner.
func TestSettingsFileIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	e := New(Config{DBPath: filepath.Join(dir, "navi.db")})
	e.SetAPIKey("sk-secret")

	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("settings file permissions = %o, want 600", perm)
	}
}

// TestMemoryEngineWritesNoSettingsFile: the ephemeral engines used by the
// test suite (and by ":memory:" callers) must not leave anything on disk.
func TestMemoryEngineWritesNoSettingsFile(t *testing.T) {
	before, _ := os.ReadDir(".")

	e := New(Config{DBPath: ":memory:"})
	e.SetModel("gpt-4o-mini")
	e.SetAPIKey("sk-secret")

	after, _ := os.ReadDir(".")
	if len(after) != len(before) {
		t.Errorf("in-memory engine wrote files into the working directory")
	}
	for _, f := range after {
		if f.Name() == "settings.json" {
			t.Errorf("in-memory engine wrote %q to disk", f.Name())
		}
	}
}

// --- Action instructions in the context window ---

// capturingServer records the request body so the test can inspect what the
// model was actually told.
func capturingServer(t *testing.T, body *string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		*body = string(raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
}

// systemPromptOf decodes the captured request and returns the content of the
// system message. The raw body is JSON, where angle brackets arrive escaped,
// so the assertions have to look at the decoded text.
func systemPromptOf(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("could not decode request: %v", err)
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			return m.Content
		}
	}
	t.Fatalf("no system message in the request: %s", body)
	return ""
}

// TestAskAdvertisesEnabledActions: the one-shot path must tell the model
// which actions exist and what they do, otherwise the agent never uses them.
func TestAskAdvertisesEnabledActions(t *testing.T) {
	var body string
	srv := capturingServer(t, &body)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})
	e.ActionManager().SaveAction(Action{
		Tag:             "git-status",
		Description:     "prints repo status",
		CommandTemplate: "git status",
		ParamName:       "x",
		Enabled:         true,
	})

	if _, err := e.Ask(context.Background(), "what can you do?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	prompt := systemPromptOf(t, body)
	if !strings.Contains(prompt, "<terminal>") {
		t.Errorf("built-in action not advertised to the model: %q", prompt)
	}
	if !strings.Contains(prompt, "<git-status>") || !strings.Contains(prompt, "prints repo status") {
		t.Errorf("user-defined action not advertised with its description: %q", prompt)
	}
}

// TestAskAdvertisesWorkingDirectory: the agent must know which directory the
// commands run in, otherwise it guesses paths.
func TestAskAdvertisesWorkingDirectory(t *testing.T) {
	var body string
	srv := capturingServer(t, &body)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})
	if _, err := e.Ask(context.Background(), "where am I?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	prompt := systemPromptOf(t, body)
	cwd, _ := os.Getwd()
	if !strings.Contains(prompt, "Operating Working Directory") {
		t.Errorf("working directory not advertised: %q", prompt)
	}
	if !strings.Contains(prompt, cwd) {
		t.Errorf("working directory %q missing from the prompt: %q", cwd, prompt)
	}
}

// TestDisabledActionsAreNotAdvertised: with the master switch off, the model
// gets the "do not emit tags" notice instead of the action catalogue.
func TestDisabledActionsAreNotAdvertised(t *testing.T) {
	var body string
	srv := capturingServer(t, &body)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", DBPath: ":memory:"})
	e.ActionManager().SetActionsEnabled(false)

	if _, err := e.Ask(context.Background(), "hello"); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	prompt := systemPromptOf(t, body)
	if !strings.Contains(prompt, "DISABLED") {
		t.Errorf("disabled notice missing from the prompt: %q", prompt)
	}
	if strings.Contains(prompt, "<terminal>command</terminal>") {
		t.Errorf("actions advertised while disabled: %q", prompt)
	}
}

// TestPersistedModelSurvivesRestart is the reported scenario: a model the
// user picked must still be there after the app is closed and reopened,
// instead of falling back to the default.
func TestPersistedModelSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "navi.db")

	first := New(Config{DBPath: dbPath})
	first.SetModel("openai/gpt-oss-120b")

	second := New(Config{DBPath: dbPath})
	if got := second.GetStats().Model; got != "openai/gpt-oss-120b" {
		t.Errorf("model was not restored: got %q, want %q", got, "openai/gpt-oss-120b")
	}
}

// TestBrowsingSessionsKeepsTheChosenModel: switching to a saved conversation
// adopts that conversation's model for the live session, but it must not
// overwrite the model the user picked for the app.
func TestBrowsingSessionsKeepsTheChosenModel(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "navi.db")

	e := New(Config{DBPath: dbPath})
	e.SetModel("llama-3.1-8b-instant")
	chatA, err := e.CreateSession("Chat A")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := e.CreateSession("Chat B"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// The user then picks another model - that is their explicit choice.
	e.SetModel("openai/gpt-oss-120b")

	// Browsing back to Chat A adopts Chat A's model for the live session...
	if _, err := e.SwitchSession(chatA.ID); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if got := e.GetStats().Model; got != "llama-3.1-8b-instant" {
		t.Errorf("switching sessions did not adopt the conversation model: got %q", got)
	}

	// ...but the last explicit choice is what the next launch starts with.
	restarted := New(Config{DBPath: dbPath})
	if got := restarted.GetStats().Model; got != "openai/gpt-oss-120b" {
		t.Errorf("browsing a session overwrote the persisted model: got %q", got)
	}
}

// TestContextWindowCarriesEnabledActions is the core of "the agent must know
// what it can do": the action catalogue lives in the context window itself,
// so it is part of what the user copies or dumps - not a hidden patch on the
// outgoing request.
func TestContextWindowCarriesEnabledActions(t *testing.T) {
	e := New(Config{Model: "m1", DBPath: ":memory:"})
	e.ActionManager().SaveAction(Action{
		Tag:             "git-status",
		Description:     "prints repo status",
		CommandTemplate: "git status",
		ParamName:       "x",
		Enabled:         true,
	})

	ctx := e.GetContextText()
	if !strings.Contains(ctx, "<terminal>") {
		t.Errorf("built-in action missing from the context window: %q", ctx)
	}
	if !strings.Contains(ctx, "<git-status>") || !strings.Contains(ctx, "prints repo status") {
		t.Errorf("user-defined action missing from the context window: %q", ctx)
	}
	cwd, _ := os.Getwd()
	if !strings.Contains(ctx, cwd) {
		t.Errorf("operating directory missing from the context window: %q", ctx)
	}
}

// TestContextWindowFollowsActionChanges: toggling an action or flipping the
// master switch has to update the window immediately, not on the next
// launch.
func TestContextWindowFollowsActionChanges(t *testing.T) {
	e := New(Config{Model: "m1", DBPath: ":memory:"})
	act := e.ActionManager().SaveAction(Action{
		Tag:             "git-status",
		Description:     "prints repo status",
		CommandTemplate: "git status",
		ParamName:       "x",
		Enabled:         true,
	})
	if !strings.Contains(e.GetContextText(), "<git-status>") {
		t.Fatal("new action not in the context window")
	}

	if err := e.ActionManager().ToggleAction(act.ID, false); err != nil {
		t.Fatalf("ToggleAction: %v", err)
	}
	if strings.Contains(e.GetContextText(), "<git-status>") {
		t.Errorf("disabled action still in the context window: %q", e.GetContextText())
	}

	if err := e.ActionManager().ToggleAction(act.ID, true); err != nil {
		t.Fatalf("ToggleAction: %v", err)
	}
	if !strings.Contains(e.GetContextText(), "<git-status>") {
		t.Errorf("re-enabled action missing from the context window: %q", e.GetContextText())
	}

	// The master switch off replaces the catalogue with the notice.
	e.ActionManager().SetActionsEnabled(false)
	ctx := e.GetContextText()
	if !strings.Contains(ctx, "DISABLED") {
		t.Errorf("disabled notice missing from the context window: %q", ctx)
	}
	if strings.Contains(ctx, "<terminal>command</terminal>") {
		t.Errorf("actions still listed while disabled: %q", ctx)
	}
}

// TestContextWindowSurvivesReset: clearing the conversation must not drop
// the action catalogue - the new window still needs it.
func TestContextWindowSurvivesReset(t *testing.T) {
	e := New(Config{Model: "m1", DBPath: ":memory:"})
	e.ResetHistory()
	ctx := e.GetContextText()
	if !strings.Contains(ctx, "<terminal>") {
		t.Errorf("action catalogue missing after a reset: %q", ctx)
	}
}

// TestAskRequestKeepsTheSystemPrompt: the injected notice must not replace
// the system prompt, only extend it.
func TestAskRequestKeepsTheSystemPrompt(t *testing.T) {
	var body string
	srv := capturingServer(t, &body)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", SystemPrompt: "YOU-ARE-NAVI", DBPath: ":memory:"})
	if _, err := e.Ask(context.Background(), "hello"); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	var req struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("could not decode request: %v", err)
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("no system message in the request: %s", body)
	}
	if !strings.Contains(req.Messages[0].Content, "YOU-ARE-NAVI") {
		t.Errorf("system prompt was dropped: %q", req.Messages[0].Content)
	}
}
