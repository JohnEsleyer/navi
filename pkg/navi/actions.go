package navi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Action describes one XML tag the harness intercepts live out of the model's
// output stream. Two flavors exist:
//
//   - Built-in (IsBuiltin): the tag body IS the command (<terminal>ls -l</terminal>).
//     Built-ins cannot be deleted, because the harness semantic itself hangs
//     off them, but they can be toggled off.
//   - User-defined: the tag body is a parameter substituted into
//     CommandTemplate (<create-folder>books</create-folder> runs "mkdir {x}"
//     with x=books).
type Action struct {
	ID              string `json:"id"`
	Tag             string `json:"tag"`              // e.g. "terminal", "create-folder"
	Description     string `json:"description"`      // shown to the user and documented to the model
	CommandTemplate string `json:"command_template"` // e.g. "mkdir {x}"; empty for raw execution
	ParamName       string `json:"param_name"`       // placeholder name in the template, e.g. "x"
	IsBuiltin       bool   `json:"is_builtin"`       // built-in actions cannot be deleted
	Enabled         bool   `json:"enabled"`          // toggled off actions are neither executed nor advertised
}

// BuiltinTerminalAction gives the model plain shell access. It ships enabled
// out of the box; everything about it except Enabled is fixed.
var BuiltinTerminalAction = Action{
	ID:              "builtin-terminal",
	Tag:             "terminal",
	Description:     "Executes the command in the local shell and returns stdout/stderr.",
	CommandTemplate: "", // empty means: the tag body is executed verbatim
	ParamName:       "command",
	IsBuiltin:       true,
	Enabled:         true,
}

// ActionManager owns the action registry and the master on/off switch. It is
// safe for concurrent use: the streaming reader goroutine queries it on every
// token while the GUI mutates it from the management view.
type ActionManager struct {
	mu             sync.RWMutex
	actions        map[string]Action
	actionsEnabled bool

	// path is the actions.json file snapshotting user configuration. Empty
	// disables persistence (":memory:" test engines stay hermetic, and a
	// broken disk must not stop actions from working in-memory).
	path string

	// onChange is notified after every mutation so the owner can refresh
	// whatever mirrors the registry - in practice the engine's context
	// window, which has to carry the action catalogue. It is called with
	// the lock released, so the callback may safely read the registry.
	onChange func()
}

func NewActionManager(path string) *ActionManager {
	am := &ActionManager{
		actions:        make(map[string]Action),
		actionsEnabled: true,
		path:           path,
	}
	// The built-in terminal action is always present, even if the persisted
	// file was sabotaged or lost.
	am.actions[BuiltinTerminalAction.ID] = BuiltinTerminalAction
	am.load()
	return am
}

// load restores user-defined actions and the master switch from actions.json.
// Built-ins are excluded from the file, so a stale or hand-edited file cannot
// demote them to user actions.
func (am *ActionManager) load() {
	if am.path == "" {
		return
	}
	data, err := os.ReadFile(am.path)
	if err != nil {
		return // first run: nothing saved yet
	}
	var saved struct {
		Enabled bool     `json:"enabled"`
		Actions []Action `json:"actions"`
	}
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	am.actionsEnabled = saved.Enabled
	// Re-add built-ins with persisted Enabled state only. Other fields always
	// come from the compiled-in definition.
	builtins := map[string]Action{
		BuiltinTerminalAction.ID: BuiltinTerminalAction,
	}
	for _, a := range saved.Actions {
		if b, ok := builtins[a.ID]; ok {
			b.Enabled = a.Enabled
			am.actions[b.ID] = b
			continue
		}
		if a.ID != "" && a.Tag != "" {
			am.actions[a.ID] = a
		}
	}
}

// persist writes the non-builtin actions and switch state to disk. Errors are
// swallowed deliberately: the in-memory registry remains fully functional and
// the next mutation retries the write.
func (am *ActionManager) persist() {
	if am.path == "" {
		return
	}
	saved := struct {
		Enabled bool     `json:"enabled"`
		Actions []Action `json:"actions"`
	}{Enabled: am.actionsEnabled}
	for _, a := range am.actions {
		if !a.IsBuiltin {
			saved.Actions = append(saved.Actions, a)
		}
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(am.path, data, 0600)
}

// SetOnChange registers a callback fired after every registry mutation. The
// engine uses it to keep the context window's action catalogue in sync, so
// the model learns about a new or toggled action without a restart.
func (am *ActionManager) SetOnChange(fn func()) {
	am.mu.Lock()
	am.onChange = fn
	am.mu.Unlock()
}

// notifyChanged fires the change callback. It must be called with the lock
// released: the callback reads the registry, and a recursive read lock
// would deadlock against a waiting writer.
func (am *ActionManager) notifyChanged() {
	am.mu.RLock()
	fn := am.onChange
	am.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (am *ActionManager) SetActionsEnabled(enabled bool) {
	am.mu.Lock()
	am.actionsEnabled = enabled
	am.persist()
	am.mu.Unlock()
	am.notifyChanged()
}

func (am *ActionManager) IsActionsEnabled() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.actionsEnabled
}

// ListActions snapshots the registry, built-in first so the management UI has
// a stable principal row.
func (am *ActionManager) ListActions() []Action {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.orderedActions()
}

// SaveAction upserts an action. A new action gets a random ID; the tag is
// stripped of any angle brackets so tags arrive clean whether typed as
// "create-folder" or "<create-folder>".
func (am *ActionManager) SaveAction(a Action) Action {
	am.mu.Lock()
	if a.ID == "" {
		a.ID = "act-" + randomID()
	}
	a.Tag = strings.Trim(strings.TrimSpace(a.Tag), "<>/ ")
	am.actions[a.ID] = a
	am.persist()
	am.mu.Unlock()
	am.notifyChanged()
	return a
}

func (am *ActionManager) DeleteAction(id string) error {
	am.mu.Lock()
	if a, ok := am.actions[id]; ok && a.IsBuiltin {
		am.mu.Unlock()
		return fmt.Errorf("cannot delete built-in action")
	}
	delete(am.actions, id)
	am.persist()
	am.mu.Unlock()
	am.notifyChanged()
	return nil
}

func (am *ActionManager) ToggleAction(id string, enabled bool) error {
	am.mu.Lock()
	a, ok := am.actions[id]
	if !ok {
		am.mu.Unlock()
		return fmt.Errorf("action not found")
	}
	a.Enabled = enabled
	am.actions[id] = a
	am.persist()
	am.mu.Unlock()
	am.notifyChanged()
	return nil
}

// FindEnabledByTag resolves an enabled action for a closing tag. The lookup
// runs on every streamed token, so it is a plain map hit behind an RLock.
func (am *ActionManager) FindEnabledByTag(tag string) (Action, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	clean := strings.ToLower(strings.Trim(strings.TrimSpace(tag), "<>/ "))
	for _, a := range am.actions {
		if a.Enabled && strings.ToLower(a.Tag) == clean {
			return a, true
		}
	}
	return Action{}, false
}

// GenerateSystemPromptInstructions documents the live execution contract to
// the model - which actions it may use, what each one does and where the
// commands run - or, when actions are off, injects the notice the AI needs
// so it stops emitting action tags that would never be executed.
func (am *ActionManager) GenerateSystemPromptInstructions() string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if !am.actionsEnabled {
		return "\n[SYSTEM NOTICE: Actions and terminal execution are currently DISABLED by the user. Do not output action tags like <terminal> as they will not be executed.]"
	}

	var sb strings.Builder
	sb.WriteString("\n\n### ACTION EXECUTION HARNESS:\n")
	// The working directory matters: every command the harness runs starts
	// here, so the model must know it instead of guessing a path.
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		sb.WriteString(fmt.Sprintf("Operating Working Directory: `%s`\n", cwd))
	}
	sb.WriteString("You have live action execution capabilities. When you need to inspect files, execute commands, or run scripts, output the action XML tags. The harness will immediately intercept the closing tag, run the command in the operating directory, and feed the output back to you.\n")
	sb.WriteString("Available Enabled Actions:\n")

	// Built-ins first, then the user-defined ones in a stable order, so the
	// documented list does not shuffle between turns. The registry is walked
	// directly instead of through ListActions, which would take the lock a
	// second time and can deadlock against a waiting writer.
	ordered := am.orderedActions()
	for _, a := range ordered {
		if !a.Enabled {
			continue
		}
		if a.IsBuiltin {
			sb.WriteString(fmt.Sprintf("- `<%s>command</%s>`: %s\n", a.Tag, a.Tag, a.Description))
		} else {
			param := a.ParamName
			if param == "" {
				param = "arg"
			}
			sb.WriteString(fmt.Sprintf("- `<%s>%s</%s>`: %s (Runs: `%s`)\n", a.Tag, param, a.Tag, a.Description, a.CommandTemplate))
		}
	}
	sb.WriteString("Always wait for the harness `<..._output>` response turn before concluding your answer.\n")
	return sb.String()
}

// orderedActions returns the registry with the built-in action first and the
// user-defined ones sorted by tag. The caller must hold the lock.
func (am *ActionManager) orderedActions() []Action {
	list := make([]Action, 0, len(am.actions))
	if b, ok := am.actions[BuiltinTerminalAction.ID]; ok {
		list = append(list, b)
	}
	rest := make([]Action, 0, len(am.actions))
	for id, a := range am.actions {
		if id != BuiltinTerminalAction.ID {
			rest = append(rest, a)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Tag == rest[j].Tag {
			return rest[i].ID < rest[j].ID
		}
		return rest[i].Tag < rest[j].Tag
	})
	return append(list, rest...)
}

// ExecuteAction substitutes the tag body into the command template (for
// user-defined actions) and runs it, returning the text that is fed back to
// the model as the harness turn. The output is wrapped in the matching
// <tag_output> element so the model can tell its own turn apart from the
// harness's answer.
func (am *ActionManager) ExecuteAction(ctx context.Context, act Action, tagBody string) string {
	cmdStr := strings.TrimSpace(tagBody)

	// Template substitution is intentionally generous across the popular
	// placeholder styles ({{x}}, {x}, $x, $1) so users can copy shell habits.
	// Longest pattern first: "{{x}}" contains "{x}", so replacing the short
	// form first would corrupt the double-brace form.
	if act.CommandTemplate != "" {
		tmpl := act.CommandTemplate
		param := act.ParamName
		if param != "" && param != "arg" {
			tmpl = strings.ReplaceAll(tmpl, "{{"+param+"}}", cmdStr)
			tmpl = strings.ReplaceAll(tmpl, "{"+param+"}", cmdStr)
			tmpl = strings.ReplaceAll(tmpl, "$"+param, cmdStr)
		}
		tmpl = strings.ReplaceAll(tmpl, "{{arg}}", cmdStr)
		tmpl = strings.ReplaceAll(tmpl, "{arg}", cmdStr)
		tmpl = strings.ReplaceAll(tmpl, "$1", cmdStr)
		cmdStr = tmpl
	}

	out, err := executeShellCommand(ctx, cmdStr)
	outTag := act.Tag + "_output"
	if err != nil {
		return fmt.Sprintf("<%s>\n[Command: %s]\nError: %v\nOutput: %s\n</%s>", outTag, cmdStr, err, out, outTag)
	}
	if strings.TrimSpace(out) == "" {
		out = "Process exited with code 0 (no output)"
	}
	return fmt.Sprintf("<%s>\n%s\n</%s>", outTag, strings.TrimRight(out, "\n"), outTag)
}

// executeShellCommand runs one command line through the system shell with a
// hard 30s ceiling. Output is capped at 64KB so a chatty command cannot blow
// up the context window.
func executeShellCommand(ctx context.Context, cmdStr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
	}

	var stdout, stderr bytes.Buffer
	const maxOut = 64 << 10
	cmd.Stdout = &limitedWriter{b: &stdout, max: maxOut}
	cmd.Stderr = &limitedWriter{b: &stderr, max: maxOut}

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	return output, err
}

// limitedWriter truncates a stream after max bytes instead of buffering it all.
type limitedWriter struct {
	b   *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.b.Len()+len(p) > w.max {
		room := w.max - w.b.Len()
		if room > 0 {
			w.b.Write(p[:room])
			w.b.WriteString("\n... (output truncated)")
		}
		return len(p), nil // lie about the write; the process keeps running
	}
	return w.b.Write(p)
}
