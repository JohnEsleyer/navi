package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"navi/pkg/navi"
)

const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
	ColorCyan   = "\033[36m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorRed    = "\033[31m"
	ColorGray   = "\033[90m"
	ColorPurple = "\033[35m"
)

type SlashCommand struct {
	Name        string
	Args        string
	Description string
	Execute     func(engine *navi.Engine, args string) bool
}

var commandRegistry = map[string]SlashCommand{}

func registerCommands() {
	commandRegistry["help"] = SlashCommand{
		Name:        "/help",
		Args:        "",
		Description: "Display available slash commands and usage",
		Execute: func(engine *navi.Engine, args string) bool {
			printCommandPreview("")
			return false
		},
	}

	commandRegistry["session"] = SlashCommand{
		Name:        "/session",
		Args:        "<sub>",
		Description: "Saved conversations: list|new|switch|rename|delete",
		Execute: func(engine *navi.Engine, args string) bool {
			parts := strings.Fields(args)
			action := "list"
			if len(parts) > 0 {
				action = strings.ToLower(parts[0])
			}

			switch action {
			case "list":
				sessions, err := engine.ListSessions()
				if err != nil {
					fmt.Printf("%sError listing sessions: %v%s\n", ColorRed, err, ColorReset)
					return false
				}
				active := engine.ActiveSession()
				fmt.Printf("\n%sSaved sessions (%d):%s\n", ColorBold, len(sessions), ColorReset)
				for _, s := range sessions {
					marker := "  "
					title := s.Title
					if active != nil && active.ID == s.ID {
						marker = "▶ "
						title = ColorBold + title + ColorReset
					}
					fmt.Printf("%s%s %s%s%s  %s %s[%s]%s\n",
						ColorGray, marker, ColorCyan, shortID(s.ID), ColorReset,
						title, ColorDim, s.Model, ColorReset)
				}
				fmt.Printf("%sUse /session switch <id> to change session.%s\n", ColorDim, ColorReset)
				return false

			case "new":
				title := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), "new"))
				sess, err := engine.CreateSession(title)
				if err != nil {
					fmt.Printf("%sError creating session: %v%s\n", ColorRed, err, ColorReset)
					return false
				}
				fmt.Printf("%s✔ New session %s — %s [%s]%s\n",
					ColorGreen, shortID(sess.ID), sess.Title, sess.Model, ColorReset)
				return false

			case "switch":
				if len(parts) < 2 {
					fmt.Printf("%sUsage: /session switch <id-prefix>%s\n", ColorYellow, ColorReset)
					return false
				}
				target, found := findSessionByPrefix(engine, parts[1])
				if !found {
					fmt.Printf("%sNo session matches %q. Run /session list.%s\n", ColorRed, parts[1], ColorReset)
					return false
				}
				sess, err := engine.SwitchSession(target)
				if err != nil {
					fmt.Printf("%sError switching session: %v%s\n", ColorRed, err, ColorReset)
					return false
				}
				msgs := engine.GetSessionMessages(sess.ID)
				fmt.Printf("%s✔ Switched to %s — %s (%d messages restored)%s\n",
					ColorGreen, shortID(sess.ID), sess.Title, len(msgs), ColorReset)
				return false

			case "rename":
				active := engine.ActiveSession()
				if active == nil {
					return false
				}
				newTitle := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), "rename"))
				if newTitle == "" {
					fmt.Printf("%sUsage: /session rename <new title>%s\n", ColorYellow, ColorReset)
					return false
				}
				if err := engine.RenameSession(active.ID, newTitle); err != nil {
					fmt.Printf("%sError renaming session: %v%s\n", ColorRed, err, ColorReset)
					return false
				}
				fmt.Printf("%s✔ Renamed session %s to %q%s\n", ColorGreen, shortID(active.ID), newTitle, ColorReset)
				return false

			case "delete":
				if len(parts) < 2 {
					fmt.Printf("%sUsage: /session delete <id-prefix>%s\n", ColorYellow, ColorReset)
					return false
				}
				target, found := findSessionByPrefix(engine, parts[1])
				if !found {
					fmt.Printf("%sNo session matches %q. Run /session list.%s\n", ColorRed, parts[1], ColorReset)
					return false
				}
				if err := engine.DeleteSession(target); err != nil {
					fmt.Printf("%sError deleting session: %v%s\n", ColorRed, err, ColorReset)
					return false
				}
				fmt.Printf("%s✔ Deleted session %s%s\n", ColorGreen, shortID(target), ColorReset)
				return false

			default:
				fmt.Printf("%sUnknown subcommand %q. Use: /session <list|new|switch|rename|delete>%s\n",
					ColorYellow, action, ColorReset)
			}
			return false
		},
	}

	commandRegistry["dump"] = SlashCommand{
		Name:        "/dump",
		Args:        "",
		Description: "Export active session context into a local .txt file",
		Execute: func(engine *navi.Engine, args string) bool {
			path, err := engine.DumpContext()
			if err != nil {
				fmt.Printf("%sError dumping context: %v%s\n", ColorRed, err, ColorReset)
				return false
			}
			fmt.Printf("%s✔ Context dumped to current working directory:%s\n  %s%s%s\n",
				ColorGreen, ColorReset, ColorCyan, path, ColorReset)
			return false
		},
	}

	commandRegistry["gui"] = SlashCommand{
		Name:        "/gui",
		Args:        "",
		Description: "Launch the standalone dark-mode Wails desktop interface",
		Execute: func(engine *navi.Engine, args string) bool {
			launchGUI(engine)
			return false
		},
	}

	commandRegistry["clear"] = SlashCommand{
		Name:        "/clear",
		Args:        "",
		Description: "Reset conversation history to start a fresh token window",
		Execute: func(engine *navi.Engine, args string) bool {
			engine.ResetHistory()
			fmt.Printf("%s✔ Memory cleared. Active context tokens reset to zero.%s\n", ColorGreen, ColorReset)
			return false
		},
	}

	commandRegistry["stats"] = SlashCommand{
		Name:        "/stats",
		Args:        "",
		Description: "View cumulative session cost, token usage, and cache stats",
		Execute: func(engine *navi.Engine, args string) bool {
			s := engine.GetStats()
			fmt.Printf("\n%s📊 Session Metrics:%s\n", ColorBold, ColorReset)
			fmt.Printf("  • Model:        %s%s%s\n", ColorCyan, s.Model, ColorReset)
			fmt.Printf("  • Input Tokens:  %d\n", s.InputTokens)
			fmt.Printf("  • Output Tokens: %d\n", s.OutputTokens)
			fmt.Printf("  • Total Cost:    %s$%.6f%s\n", ColorYellow, s.TotalCost, ColorReset)
			fmt.Printf("  • Cached Items:  %d in-memory\n", s.CacheEntries)
			fmt.Printf("  • Buffer Depth:  %d messages\n", s.HistoryDepth)
			return false
		},
	}

	commandRegistry["model"] = SlashCommand{
		Name:        "/model",
		Args:        "<name>",
		Description: "Inspect or switch active model (e.g. /model gpt-4o-mini)",
		Execute: func(engine *navi.Engine, args string) bool {
			if strings.TrimSpace(args) == "" {
				s := engine.GetStats()
				fmt.Printf("Current model: %s%s%s\n", ColorCyan, s.Model, ColorReset)
				return false
			}
			engine.SetModel(args)
			fmt.Printf("%s✔ Switched active model to %s%s\n", ColorGreen, args, ColorReset)
			return false
		},
	}

	commandRegistry["history"] = SlashCommand{
		Name:        "/history",
		Args:        "",
		Description: "Inspect messages in current sliding context window",
		Execute: func(engine *navi.Engine, args string) bool {
			history := engine.GetHistory()
			fmt.Printf("\n%s📜 Current Context Window (%d messages):%s\n", ColorBold, len(history), ColorReset)
			for i, m := range history {
				roleColor := ColorCyan
				if m.Role == "user" {
					roleColor = ColorGreen
				} else if m.Role == "system" {
					roleColor = ColorPurple
				}
				content := strings.ReplaceAll(m.Content, "\n", " ")
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				fmt.Printf("  [%d] %s%-9s%s: %s\n", i, roleColor, m.Role, ColorReset, content)
			}
			return false
		},
	}

	commandRegistry["key"] = SlashCommand{
		Name:        "/key",
		Args:        "[api_key]",
		Description: "Inspect or update API key for current session",
		Execute: func(engine *navi.Engine, args string) bool {
			if strings.TrimSpace(args) == "" {
				fmt.Printf("Active API Key: %s%s%s (Endpoint: %s%s%s)\n",
					ColorYellow, engine.MaskedAPIKey(), ColorReset,
					ColorCyan, engine.GetBaseURL(), ColorReset)
				fmt.Printf("Run %s/key <your-api-key>%s to update it.\n", ColorBold, ColorReset)
				fmt.Printf("%sKeys are per-session and never written to disk.%s\n", ColorDim, ColorReset)
				return false
			}
			engine.SetAPIKey(args)
			fmt.Printf("%s✔ API key updated (%s)%s\n", ColorGreen, engine.MaskedAPIKey(), ColorReset)
			return false
		},
	}

	commandRegistry["baseurl"] = SlashCommand{
		Name:        "/baseurl",
		Args:        "[url]",
		Description: "Inspect or set API base URL (e.g. /baseurl http://localhost:11434/v1)",
		Execute: func(engine *navi.Engine, args string) bool {
			if strings.TrimSpace(args) == "" {
				fmt.Printf("Current Base URL: %s%s%s\n", ColorCyan, engine.GetBaseURL(), ColorReset)
				fmt.Printf("Run %s/baseurl <url>%s to change it; it then stays fixed across /model switches.\n",
					ColorBold, ColorReset)
				return false
			}
			engine.SetBaseURL(args)
			fmt.Printf("%s✔ Switched base URL to %s%s\n", ColorGreen, engine.GetBaseURL(), ColorReset)
			return false
		},
	}

	commandRegistry["exit"] = SlashCommand{
		Name:        "/exit",
		Args:        "",
		Description: "Close the Navi session",
		Execute: func(engine *navi.Engine, args string) bool {
			return true
		},
	}
}

// printCommandPreview renders the command table. The border width is computed
// from the row layout rather than hard-coded, so the box cannot go ragged when
// a column width changes: previously the rows were 65 cells wide inside a
// 62-cell border.
func printCommandPreview(filter string) {
	const sigW = 18
	const descW = 41
	// │  <sig> <desc> │  -> 1 + 2 + sigW + 1 + descW + 1 + 1
	inner := 2 + sigW + 1 + descW + 1

	// The ─ glyph is 3 bytes in UTF-8, so len() would over-count it and emit
	// 4 too few dashes; count runes instead.
	const title = "── Slash Commands "
	fmt.Printf("\n%s┌%s%s┐%s\n",
		ColorPurple, title, strings.Repeat("─", inner-utf8.RuneCountInString(title)), ColorReset)

	var keys []string
	for k := range commandRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		cmd := commandRegistry[k]
		if filter != "" && !strings.HasPrefix(cmd.Name, filter) {
			continue
		}
		cmdSignature := fit(fmt.Sprintf("%s %s", cmd.Name, cmd.Args), sigW)
		// Truncate to the column widths so a long entry cannot push past the
		// right-hand border and leave a ragged box.
		fmt.Printf("%s│%s  %s%-*s%s %-*s %s│%s\n",
			ColorPurple, ColorReset,
			ColorBold, sigW, cmdSignature, ColorReset,
			descW, fit(cmd.Description, descW),
			ColorPurple, ColorReset)
	}
	fmt.Printf("%s└%s┘%s\n", ColorPurple, strings.Repeat("─", inner), ColorReset)
}

// fit truncates s to at most n display cells, marking elision with an
// ellipsis. It keeps fixed-width tables (like the command box) from acquiring
// a ragged edge when a label is longer than its column.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// shortID returns a display-safe prefix. It guards the length instead of
// slicing blindly so a malformed or externally-created ID cannot panic the UI.
func shortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// findSessionByPrefix resolves a user-typed ID prefix to a full session ID.
// An ambiguous prefix matches nothing rather than guessing, so a /session
// switch or delete can never act on the wrong conversation.
func findSessionByPrefix(engine *navi.Engine, prefix string) (string, bool) {
	sessions, err := engine.ListSessions()
	if err != nil {
		return "", false
	}
	var found string
	matches := 0
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, prefix) {
			found = s.ID
			matches++
		}
	}
	if matches != 1 {
		return "", false
	}
	return found, true
}

// isTerminal reports whether f is an interactive terminal. Output niceties
// (spinner, typewriter) are suppressed otherwise, so piping navi output into a
// file or another program yields clean text with no ANSI escapes or CR noise.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// runSpinner starts a Braille spinner showing elapsed seconds and returns a
// stop function. stop is idempotent, so calling it twice cannot panic, and it
// always clears the line so the spinner never collides with the reply.
func runSpinner(label string) func() {
	if !isTerminal(os.Stdout) {
		return func() {}
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done := make(chan struct{})
	start := time.Now()

	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				fmt.Printf("\r%s%s%s %s%s %.1fs%s\033[K",
					ColorCyan, frames[i%len(frames)], ColorReset,
					ColorDim, label, elapsed, ColorReset)
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			fmt.Print("\r\033[K")
		})
	}
}

// renderTypewriter streams a response word by word for a "live" feel. The
// per-word delay shrinks as the answer grows so a long reply is never delayed
// by more than ~1.5s total, and non-terminal output is written whole.
func renderTypewriter(text string) {
	if !isTerminal(os.Stdout) {
		fmt.Println(text)
		return
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		fmt.Println()
		return
	}

	const maxTotal = 1500 * time.Millisecond
	delay := 12 * time.Millisecond
	if total := time.Duration(len(words)) * delay; total > maxTotal {
		delay = maxTotal / time.Duration(len(words))
	}

	for i, w := range words {
		fmt.Print(w)
		if i < len(words)-1 {
			fmt.Print(" ")
		}
		time.Sleep(delay)
	}
	fmt.Println()
}

// launchGUI starts the desktop interface, preferring a prebuilt binary over
// `go run`. It resolves paths independently of the caller's working directory,
// reaps the child, and reports launch failures instead of swallowing them.
func launchGUI(engine *navi.Engine) {
	cmd, how := guiCommand(engine)

	// Hand the GUI the model, key and endpoint currently active in this
	// session, so a /model, /key or /baseurl switch here isn't silently lost.
	// Existing env vars are preserved; these only fill in what's missing.
	cmd.Env = append(os.Environ(),
		"NAVI_MODEL="+engine.GetStats().Model,
		"NAVI_API_KEY="+engine.GetAPIKey(),
		"NAVI_BASE_URL="+engine.GetBaseURL(),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Printf("%s✖ Failed to launch GUI (%s): %v%s\n", ColorRed, how, err, ColorReset)
		return
	}

	fmt.Printf("%s✔ GUI launching via %s (pid %d)%s\n", ColorGreen, how, cmd.Process.Pid, ColorReset)

	// Reap in the background so the child never becomes a zombie, and surface
	// a late failure (e.g. `go run` failing to find go.mod) to the user.
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Printf("%s✖ GUI exited with error: %v%s\n", ColorRed, err, ColorReset)
		}
	}()
}

// guiCommand picks how to start the GUI: a prebuilt binary if one exists,
// otherwise `go run` against a located module root.
func guiCommand(engine *navi.Engine) (*exec.Cmd, string) {
	if path, ok := findGUIBinary(); ok {
		return exec.Command(path), path
	}

	root, err := findModuleRoot()
	if err != nil {
		// Still return a go run command; its failure is reported by the caller.
		fmt.Printf("%s⚠ No navi-gui binary found and module root unresolved (%v); trying current directory.%s\n",
			ColorYellow, err, ColorReset)
	} else {
		cmd := exec.Command("go", "run", "-tags", "production", "./gui")
		cmd.Dir = root
		return cmd, "go run @ " + root
	}

	return exec.Command("go", "run", "-tags", "production", "./gui"), "go run @ ."
}

// findGUIBinary looks for a prebuilt navi-gui next to the running executable
// and then on PATH, so an installed user doesn't need the Go toolchain.
func findGUIBinary() (string, bool) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "navi-gui")
		if isExecutable(candidate) {
			return candidate, true
		}
	}
	if path, err := exec.LookPath("navi-gui"); err == nil {
		return path, true
	}
	return "", false
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

// findModuleRoot walks up from the executable's directory (then the working
// directory) looking for go.mod, so `go run` works regardless of where the
// user invoked the CLI from.
func findModuleRoot() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if root, ok := walkForGoMod(filepath.Dir(exe)); ok {
			return root, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if root, ok := walkForGoMod(cwd); ok {
			return root, nil
		}
	}
	return "", fmt.Errorf("no go.mod found above the executable or working directory")
}

func walkForGoMod(start string) (string, bool) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func main() {
	registerCommands()

	apiKey := os.Getenv("NAVI_API_KEY")
	baseURL := os.Getenv("NAVI_BASE_URL")
	model := os.Getenv("NAVI_MODEL")

	// BaseURL is deliberately NOT defaulted here. navi.New resolves the
	// endpoint from the model via the provider registry, so forcing a value
	// would pin the endpoint and break /model switching between providers.
	// An explicit NAVI_BASE_URL still wins and stays fixed.
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	engine := navi.New(navi.Config{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		MaxHistory: 6,
	})
	// Flush the chat store on exit so the last turn is never lost.
	defer func() { _ = engine.Close() }()

	sessionTitle := "New Chat"
	if active := engine.ActiveSession(); active != nil && strings.TrimSpace(active.Title) != "" {
		sessionTitle = active.Title
	}

	printHeader(engine.GetStats().Model, engine.GetBaseURL(), sessionTitle)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s%syou ›%s ", ColorBold, ColorGreen, ColorReset)
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			cleanInput := strings.TrimPrefix(input, "/")
			parts := strings.SplitN(cleanInput, " ", 2)
			cmdKey := strings.ToLower(parts[0])
			args := ""
			if len(parts) > 1 {
				args = strings.TrimSpace(parts[1])
			}

			if cmdKey == "" {
				printCommandPreview("")
				continue
			}

			cmd, exists := commandRegistry[cmdKey]
			if exists {
				if cmd.Execute(engine, args) {
					fmt.Printf("\n%sSession closed. Final spend: $%.6f%s\n", ColorYellow, engine.TotalCost(), ColorReset)
					break
				}
			} else {
				fmt.Printf("%sUnknown command: /%s%s\n", ColorRed, cmdKey, ColorReset)
				printCommandPreview("/" + cmdKey)
			}
			continue
		}

		// Braille spinner + elapsed time while the request is in flight. It
		// self-suppresses when stdout is piped, so redirected output stays clean.
		stop := runSpinner("thinking")
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := engine.Ask(ctx, input)
		cancel()
		stop()

		if err != nil {
			fmt.Printf("%sError: %v%s\n", ColorRed, err, ColorReset)
			continue
		}

		fmt.Printf("%s%snavi ›%s ", ColorBold, ColorCyan, ColorReset)
		renderTypewriter(res.Text)
		if res.Meta.Cached {
			fmt.Printf("%s⚡ [CACHE HIT] $0.000000 | Session: $%.6f | %v%s\n",
				ColorYellow, res.Meta.SessionCost, res.Meta.Duration.Round(time.Millisecond), ColorReset)
		} else {
			fmt.Printf("%s[%d in / %d out] • Turn: $%.6f • Session: $%.6f • %v%s\n",
				ColorGray, res.Meta.PromptTokens, res.Meta.OutputTokens, res.Meta.TurnCost, res.Meta.SessionCost, res.Meta.Duration.Round(time.Millisecond), ColorReset)
		}
	}
}

// printHeader renders the startup banner. The session name is shown because a
// resumed conversation should be visible before the first prompt.
//
// Every interior row is padded to a fixed 56 cells so the right-hand border
// lines up regardless of how long a value is; the same was not true when the
// rows were formatted ad hoc, which left the "Type /" line one cell wider.
func printHeader(model, endpoint, sessionTitle string) {
	const inner = 56
	row := func(text string) {
		fmt.Printf("%s│%s%-*s%s│%s\n", ColorCyan, ColorDim, inner, text, ColorCyan, ColorReset)
	}

	fmt.Printf("%s┌%s┐%s\n", ColorCyan, strings.Repeat("─", inner), ColorReset)
	row("              NAVI AI HARNESS              ")
	row("   Session:  " + fit(sessionTitle, 42))
	row("   Model:    " + fit(model, 42))
	row("   Endpoint: " + fit(endpoint, 42))
	// No inline color here: the row helper pads with %-*s, and escape bytes
	// count toward that width, which would silently shrink the visible row.
	row("   Type / for commands (/session, /gui, /model, etc.)")
	fmt.Printf("%s└%s┘%s\n", ColorCyan, strings.Repeat("─", inner), ColorReset)
}
