package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"navi/pkg/navi"
)

type App struct {
	ctx    context.Context
	engine *navi.Engine
}

func NewApp() *App {
	apiKey := os.Getenv("NAVI_API_KEY")
	baseURL := os.Getenv("NAVI_BASE_URL")
	model := os.Getenv("NAVI_MODEL")

	// BaseURL is deliberately not defaulted. navi.New picks the endpoint from
	// the model via the provider registry, so forcing one here would pin it
	// and break switching models across providers.
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	engine := navi.New(navi.Config{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		MaxHistory: 8,
	})

	return &App{engine: engine}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// localResult builds a Result that never touched the API, so the frontend
// renders it with a $0.000000 cost line instead of a real token spend.
func (a *App) localResult(text string) *navi.Result {
	return &navi.Result{
		Text: text,
		Meta: navi.ResponseMeta{SessionCost: a.engine.GetStats().TotalCost},
	}
}

// handleCommand intercepts slash commands before they reach the LLM.
// The CLI has this layer in cmd/navi/main.go; the GUI previously had none,
// so "/exit" and friends were billed as normal prompts.
// Returns nil when the input is a plain prompt that should go to the model.
func (a *App) handleCommand(input string) *navi.Result {
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(input, "/")), " ", 2)
	name := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	if name == "" {
		return a.localResult("Type a message, or one of: /help /key /model /baseurl /stats /history /clear /dump /exit")
	}

	switch name {
	case "help":
		return a.localResult(
			"/help     show this list\n" +
				"/key      inspect or set API key (/key <secret>)\n" +
				"/model    inspect or switch model (e.g. /model gpt-4o-mini)\n" +
				"/baseurl  inspect or switch endpoint (e.g. /baseurl http://localhost:11434/v1)\n" +
				"/stats    session tokens, cost, cache size\n" +
				"/history  show the current context window\n" +
				"/clear    reset the context window\n" +
				"/dump     write the context window to a .txt file\n" +
				"/exit     quit the GUI")

	case "key":
		if args == "" {
			return a.localResult(fmt.Sprintf("Active API Key: %s\nEndpoint: %s\nUse /key <api_key> to update. Keys are per-session and never written to disk.",
				a.engine.MaskedAPIKey(), a.engine.GetBaseURL()))
		}
		a.engine.SetAPIKey(args)
		return a.localResult(fmt.Sprintf("✔ API key updated (%s)", a.engine.MaskedAPIKey()))

	case "baseurl":
		if args == "" {
			return a.localResult("Current Base URL: " + a.engine.GetBaseURL())
		}
		a.engine.SetBaseURL(args)
		return a.localResult("✔ Switched base URL to: " + a.engine.GetBaseURL())

	case "stats":
		s := a.engine.GetStats()
		return a.localResult(fmt.Sprintf(
			"Model:        %s\nEndpoint:     %s\nInput tokens:  %d\nOutput tokens: %d\nTotal cost:    $%.6f\nCache entries: %d\nBuffer depth:  %d messages",
			s.Model, a.engine.GetBaseURL(), s.InputTokens, s.OutputTokens, s.TotalCost, s.CacheEntries, s.HistoryDepth))

	case "history":
		return a.localResult(a.engine.GetContextText())

	case "model":
		if args == "" {
			return a.localResult(fmt.Sprintf("Current model: %s (Endpoint: %s)", a.engine.GetStats().Model, a.engine.GetBaseURL()))
		}
		a.engine.SetModel(args)
		return a.localResult(fmt.Sprintf("Switched model to %s (Endpoint: %s, Key: %s)",
			args, a.engine.GetBaseURL(), a.engine.MaskedAPIKey()))

	case "clear":
		a.engine.ResetHistory()
		return a.localResult("Context window cleared.")

	case "dump":
		path, err := a.engine.DumpContext()
		if err != nil {
			return a.localResult("Dump failed: " + err.Error())
		}
		return a.localResult("Context dumped to:\n" + path)

	case "exit":
		os.Exit(0)
		return nil

	default:
		return a.localResult(fmt.Sprintf("Unknown command: /%s — try /help", name))
	}
}

func (a *App) Ask(prompt string) (*navi.Result, error) {
	if res := a.handleCommand(prompt); res != nil {
		return res, nil
	}
	// Wails sets a.ctx in OnStartup; fall back so a call made before startup
	// (or in tests) doesn't fail with "net/http: nil Context".
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.engine.Ask(ctx, prompt)
}

// Puppet Mode: Human acts as AI agent
func (a *App) PretendAgent(content string) bool {
	a.engine.InjectAssistantMessage(content)
	return true
}

func (a *App) GetContextText() string {
	return a.engine.GetContextText()
}

func (a *App) DumpContext() (string, error) {
	return a.engine.DumpContext()
}

func (a *App) ResetHistory() bool {
	a.engine.ResetHistory()
	return true
}

func (a *App) SetModel(model string) bool {
	a.engine.SetModel(model)
	return true
}

func (a *App) GetStats() navi.Stats {
	return a.engine.GetStats()
}

// --- API key & endpoint management ---

func (a *App) SetAPIKey(key string) bool {
	a.engine.SetAPIKey(key)
	return true
}

// GetAPIKey returns the raw key. It exists so the settings dialog can offer
// the current value for editing; prefer MaskedAPIKey for anything displayed.
func (a *App) GetAPIKey() string {
	return a.engine.GetAPIKey()
}

func (a *App) MaskedAPIKey() string {
	return a.engine.MaskedAPIKey()
}

func (a *App) SetBaseURL(url string) bool {
	a.engine.SetBaseURL(url)
	return true
}

func (a *App) GetBaseURL() string {
	return a.engine.GetBaseURL()
}
