package navi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- ActionManager ---

func TestActionManagerBasics(t *testing.T) {
	am := NewActionManager("")

	acts := am.ListActions()
	if len(acts) != 1 || acts[0].ID != BuiltinTerminalAction.ID {
		t.Fatalf("fresh manager should contain exactly the built-in terminal action, got %+v", acts)
	}

	// User-defined action upsert + template lookup.
	saved := am.SaveAction(Action{
		Tag:             "create-folder",
		Description:     "Creates a folder",
		CommandTemplate: "mkdir {x}",
		ParamName:       "x",
		Enabled:         true,
	})
	if saved.ID == "" {
		t.Fatal("SaveAction should assign an ID to a new action")
	}
	if loaded, ok := am.FindEnabledByTag("<create-folder>"); !ok || loaded.ID != saved.ID {
		t.Errorf("FindEnabledByTag should accept raw and bracketed tags")
	}

	// Toggle off -> no longer found.
	if err := am.ToggleAction(saved.ID, false); err != nil {
		t.Fatalf("ToggleAction: %v", err)
	}
	if _, ok := am.FindEnabledByTag("create-folder"); ok {
		t.Error("disabled action must not resolve via FindEnabledByTag")
	}

	// Built-in protection.
	if err := am.DeleteAction(BuiltinTerminalAction.ID); err == nil {
		t.Error("built-in action must refuse deletion")
	}
	if err := am.DeleteAction(saved.ID); err != nil {
		t.Errorf("user action deletion: %v", err)
	}

	// Master switch.
	am.SetActionsEnabled(false)
	if am.IsActionsEnabled() {
		t.Error("master switch should read back false")
	}
	notice := am.GenerateSystemPromptInstructions()
	if !strings.Contains(notice, "DISABLED") {
		t.Errorf("disabled notice missing from instructions: %q", notice)
	}
}

func TestActionPersistence(t *testing.T) {
	path := t.TempDir() + "/actions.json"

	am1 := NewActionManager(path)
	am1.SaveAction(Action{Tag: "git-status", CommandTemplate: "git status", ParamName: "x", Enabled: true})
	am1.SetActionsEnabled(false)

	// A fresh manager over the same file restores user actions and switch.
	am2 := NewActionManager(path)
	if am2.IsActionsEnabled() != false {
		t.Error("actions master switch should persist")
	}
	if _, ok := am2.FindEnabledByTag("git-status"); !ok {
		t.Error("user-defined action should persist across managers")
	}
	// The built-in survives even though it is not written to the file.
	if _, ok := am2.FindEnabledByTag("terminal"); !ok {
		t.Error("built-in action must always exist after load")
	}
}

func TestActionExecutionTemplates(t *testing.T) {
	am := NewActionManager("")

	// Template substitution, several placeholder styles.
	out := am.ExecuteAction(context.Background(), Action{
		Tag:             "echo-args",
		CommandTemplate: "echo {x} {{x}} $x",
		ParamName:       "x",
	}, "books")
	if !strings.Contains(out, "<echo-args_output>") {
		t.Fatalf("wrong output wrapper: %q", out)
	}
	if !strings.Contains(out, "books books books") {
		t.Errorf("template placeholders not all substituted: %q", out)
	}

	// Built-in raw execution, including stderr capture and exit-code handling.
	out = am.ExecuteAction(context.Background(), BuiltinTerminalAction, "echo terminal-ok")
	if strings.Contains(out, "Error") {
		t.Errorf("echo should not fail: %q", out)
	}
	if !strings.Contains(out, "terminal-ok") {
		t.Errorf("builtin terminal output missing body: %q", out)
	}

	// Empty templates with no output get the code-0 message instead of an
	// empty tag body.
	out = am.ExecuteAction(context.Background(), BuiltinTerminalAction, "true")
	if !strings.Contains(out, "Process exited with code 0") {
		t.Errorf("silent success should be annotated: %q", out)
	}

	// Failing commands surface the error inside the harness turn.
	out = am.ExecuteAction(context.Background(), BuiltinTerminalAction, "exit 3")
	if !strings.Contains(out, "Error") {
		t.Errorf("failing command should carry Error: %q", out)
	}
}

func TestActionSystemPromptDocumentsEnabledActions(t *testing.T) {
	am := NewActionManager("")
	am.SaveAction(Action{
		Tag:             "git-status",
		Description:     "prints repo status",
		CommandTemplate: "git status",
		ParamName:       "x",
		Enabled:         true,
	})
	doc := am.GenerateSystemPromptInstructions()
	if !strings.Contains(doc, "<terminal>") || !strings.Contains(doc, "<git-status>") {
		t.Errorf("enabled actions missing from doc: %q", doc)
	}
	if !strings.Contains(doc, "git status") {
		t.Errorf("template not documented: %q", doc)
	}
}

// --- Streaming interception end-to-end ---

// sseRecorder collects what AskStream delivered live.
type sseRecorder struct {
	mu        sync.Mutex
	tokens    []string
	actions   []string // tags intercepted
	results   []string // harness outputs
	finalText string
	err       error
}

func (r *sseRecorder) callbacks() StreamCallbacks {
	return StreamCallbacks{
		OnToken: func(tok string) {
			r.mu.Lock()
			r.tokens = append(r.tokens, tok)
			r.mu.Unlock()
		},
		OnActionStart: func(tag, command string) {
			r.mu.Lock()
			r.actions = append(r.actions, tag+"|"+command)
			r.mu.Unlock()
		},
		OnActionResult: func(tag, output string) {
			r.mu.Lock()
			r.results = append(r.results, output)
			r.mu.Unlock()
		},
		OnComplete: func(res *Result) {
			r.mu.Lock()
			r.finalText = res.Text
			r.mu.Unlock()
		},
		OnError: func(err error) {
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
		},
	}
}

// sseMessage renders one assistant delta as a data line. Pieces are written
// separately so closing tags are split across SSE events, which is exactly
// the boundary-stress case live interception has to survive.
func sseMessage(w http.ResponseWriter, flusher http.Flusher, parts ...string) {
	for _, p := range parts {
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": p}}},
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// TestAskStreamInterceptsTerminalLive: the model emits <terminal>echo
// harness-ok</terminal> split across chunk boundaries. The harness must cut
// the turn on the closing tag, execute, and generate a second turn whose
// text comes from the server's second response.
func TestAskStreamInterceptsTerminalLive(t *testing.T) {
	hits := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if n == 1 {
			sseMessage(w, flusher,
				"I'll check. <terminal>e",
				"cho har",
				"ness-ok</term",
				"inal>",
			)
			return
		}

		// Second request body must contain the intercepted turn plus output.
		var req chatRequest
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if json.Unmarshal(body, &req) == nil {
			joined := ""
			for _, m := range req.Messages {
				joined += m.Content + "\n"
			}
			if !strings.Contains(joined, "<terminal>echo harness-ok</terminal>") {
				t.Errorf("history lost intercepted tag: %q", joined)
			}
			if !strings.Contains(joined, "<terminal_output>") || !strings.Contains(joined, "harness-ok") {
				t.Errorf("history lost action output: %q", joined)
			}
		}
		sseMessage(w, flusher, "I just ran the command.")
	}))
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, Model: "mystery-model", DBPath: ":memory:"})
	rec := &sseRecorder{}
	res, err := e.AskStream(context.Background(), "run echo", rec.callbacks())
	if err != nil {
		t.Fatalf("AskStream: %v (rec.err=%v)", err, rec.err)
	}

	if hits := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hits
	}(); hits != 2 {
		t.Errorf("expected exactly 2 model turns, got %d", hits)
	}

	if !strings.Contains(res.Text, "I just ran the command.") {
		t.Errorf("final text should come from second turn: %q", res.Text)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.actions) != 1 || !strings.Contains(rec.actions[0], "echo harness-ok") {
		t.Errorf("action start callbacks wrong: %v", rec.actions)
	}
	if len(rec.results) != 1 || !strings.Contains(rec.results[0], "harness-ok") {
		t.Errorf("action result callbacks wrong: %v", rec.results)
	}
	if len(rec.tokens) == 0 {
		t.Error("no tokens streamed to callbacks")
	}

	// The persistently stored exchange must record the full multi-turn text.
	msgs, _ := e.store.GetMessages(e.ActiveSession().ID)
	var found bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "I just ran the command.") {
			found = true
		}
	}
	if !found {
		t.Error("streamed exchange was not persisted")
	}
}

// TestAskStreamDisabledSkipsInterception: with the master switch off, tags
// stay literal text and no extra model turn happens.
func TestAskStreamDisabledSkipsInterception(t *testing.T) {
	hits := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		sseMessage(w, w.(http.Flusher), "plain <terminal>echo no</terminal> text")
	}))
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, Model: "mystery-model", DBPath: ":memory:"})
	e.ActionManager().SetActionsEnabled(false)

	rec := &sseRecorder{}
	if _, err := e.AskStream(context.Background(), "hello", rec.callbacks()); err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	if got := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hits
	}(); got != 1 {
		t.Errorf("expected a single turn when actions are off, got %d", got)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.actions) != 0 || len(rec.results) != 0 {
		t.Errorf("no action should fire while disabled: %v / %v", rec.actions, rec.results)
	}
}

// TestAskStreamToggleOffNotifiesModel: the disabled notice must ride along
// with the system prompt so the model stops emitting action tags.
func TestAskStreamToggleOffNotifiesModel(t *testing.T) {
	var gotSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		_ = json.Unmarshal(body, &req)
		if len(req.Messages) > 0 {
			gotSystem = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sseMessage(w, w.(http.Flusher), "ok")
	}))
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, Model: "mystery-model", DBPath: ":memory:"})
	e.ActionManager().SetActionsEnabled(false)

	if _, err := e.AskStream(context.Background(), "hi", StreamCallbacks{}); err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if !strings.Contains(gotSystem, "DISABLED") {
		t.Errorf("system prompt should carry the disabled notice: %q", gotSystem)
	}

	// And when enabled, the action documentation takes its place.
	e.ActionManager().SetActionsEnabled(true)
	if _, err := e.AskStream(context.Background(), "hi again", StreamCallbacks{}); err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if !strings.Contains(gotSystem, "ACTION EXECUTION HARNESS") {
		t.Errorf("system prompt should document actions: %q", gotSystem)
	}
}
