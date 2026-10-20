package navi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ModelCost struct {
	InputPer1MTokens  float64
	OutputPer1MTokens float64
}

var DefaultPricing = map[string]ModelCost{
	"gpt-4o-mini":          {InputPer1MTokens: 0.15, OutputPer1MTokens: 0.60},
	"deepseek-chat":        {InputPer1MTokens: 0.14, OutputPer1MTokens: 0.28},
	"llama-3.1-8b-instant": {InputPer1MTokens: 0.05, OutputPer1MTokens: 0.08},
	"llama3.2:1b":          {InputPer1MTokens: 0.00, OutputPer1MTokens: 0.00},
	"local":                {InputPer1MTokens: 0.00, OutputPer1MTokens: 0.00},
}

type Config struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	MaxHistory   int

	// DBPath selects where conversations are persisted. Empty means the
	// default under the user config dir; ":memory:" disables persistence,
	// which is what tests use so no shared on-disk state leaks between them.
	DBPath string
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseMeta struct {
	Cached       bool          `json:"cached"`
	PromptTokens int           `json:"prompt_tokens"`
	OutputTokens int           `json:"output_tokens"`
	TurnCost     float64       `json:"turn_cost"`
	SessionCost  float64       `json:"session_cost"`
	Duration     time.Duration `json:"duration"`
}

type Result struct {
	Text string       `json:"text"`
	Meta ResponseMeta `json:"meta"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type Stats struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
	CacheEntries int     `json:"cache_entries"`
	HistoryDepth int     `json:"history_depth"`
}

type Engine struct {
	cfg Config
	// cfgMu guards cfg.Model, cfg.BaseURL and cfg.APIKey - all of which are
	// mutable at runtime - plus providerKeys and customBaseURL, which are
	// read and written alongside them.
	cfgMu sync.RWMutex

	// customBaseURL records that the endpoint was chosen explicitly (via
	// Config.BaseURL or SetBaseURL). When set, SetModel must not repoint the
	// engine at another provider's endpoint, or an explicitly configured
	// server would be silently overridden by the next model switch.
	customBaseURL bool

	// providerKeys remembers a key per provider ID, so switching models
	// restores that provider's key instead of leaving a stale one in place.
	providerKeys map[string]string

	client *http.Client

	cacheMu sync.RWMutex
	cache   map[string]string

	historyMu sync.RWMutex
	history   []Message

	// statsMu guards the session counters. They used to be read and written
	// with no lock at all, which raced with GetStats/TotalCost/DumpContext.
	statsMu     sync.RWMutex
	totalInput  int
	totalOutput int
	totalCost   float64

	// store persists conversations and model history. It is set once in New
	// and never replaced, so reads of the field itself need no lock; the
	// backends carry their own synchronization.
	store ChatAPI

	// sessionMu guards activeSession, the only field above that can be
	// swapped at runtime by CreateSession/SwitchSession/DeleteSession.
	// Lock order: sessionMu may be held while taking historyMu, but never
	// the reverse, so the two cannot deadlock.
	sessionMu     sync.RWMutex
	activeSession *Session

	// actions owns the live action registry (the <terminal> style tags the
	// streaming harness intercepts). Set once in New, so the field itself is
	// immutable; the manager carries its own lock.
	actions *ActionManager

	// settings persists the model, API key and endpoint between runs so the
	// GUI comes back exactly as the user left it. Set once in New; the store
	// carries its own lock.
	settings *SettingsStore
}

func New(cfg Config) *Engine {
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 6
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are Navi, an ultra-efficient, cost-conscious assistant. Keep answers concise."
	}

	// The chat database and the settings file live side by side, so the
	// persistence lives in the same place whether the caller asked for the
	// default location or pointed at a custom one.
	storePath := cfg.DBPath
	if storePath == "" {
		storePath = defaultDBPath()
	}

	var settings *SettingsStore
	if storePath == ":memory:" {
		// Ephemeral engines (the test suite) must not touch the disk.
		settings = NewSettingsStore(":memory:")
	} else {
		settings = NewSettingsStore(filepath.Join(filepath.Dir(storePath), "settings.json"))
	}

	// Anything the caller did not supply explicitly falls back to what was
	// persisted last time. An explicit environment variable or argument
	// always wins, so a one-off override still works.
	saved := settings.Get()
	if cfg.Model == "" {
		cfg.Model = saved.Model
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = saved.BaseURL
	}

	if cfg.Model == "" {
		cfg.Model = "llama-3.1-8b-instant"
	}

	provider := DetectProvider(cfg.Model)

	// The key follows the same rule: an explicitly supplied one is never
	// touched. Otherwise the key saved for this exact provider is the most
	// precise match, with the globally saved key as the fallback.
	if cfg.APIKey == "" {
		if provider.ID != "" {
			if pKey := strings.TrimSpace(saved.ProviderKeys[provider.ID]); pKey != "" {
				cfg.APIKey = pKey
			}
		}
		if cfg.APIKey == "" {
			cfg.APIKey = saved.APIKey
		}
	}

	// Record whether the CALLER supplied an endpoint, before the resolution
	// below fills one in. Doing this after the fill would see a non-empty
	// BaseURL for every engine and mark it as custom, which would pin the
	// endpoint forever and silently disable provider auto-switching.
	callerSetBaseURL := cfg.BaseURL != ""

	if cfg.BaseURL == "" {
		if provider.BaseURL != "" {
			cfg.BaseURL = provider.BaseURL
		} else {
			cfg.BaseURL = "https://api.groq.com/openai/v1"
		}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if cfg.APIKey == "" {
		cfg.APIKey = ResolveKeyForProvider(provider)
	}

	pKeys := make(map[string]string)
	for k, v := range saved.ProviderKeys {
		pKeys[k] = v
	}
	if provider.ID != "" && cfg.APIKey != "" {
		pKeys[provider.ID] = cfg.APIKey
	}

	eng := &Engine{
		cfg:           cfg,
		customBaseURL: callerSetBaseURL,
		providerKeys:  pKeys,
		settings:      settings,
		client:        &http.Client{Timeout: 60 * time.Second},
		cache:         make(map[string]string),
		history:       []Message{{Role: "system", Content: cfg.SystemPrompt}},
	}

	// Persistence is best-effort: a missing store must degrade to a working
	// engine rather than prevent startup, so errors are recorded as "no
	// store" and every consumer nil-checks.
	store, err := OpenChatStore(cfg.DBPath)
	if err != nil || store == nil {
		if fb, fbErr := newFallbackStore(":memory:"); fbErr == nil {
			store = fb
		}
	}
	eng.store = store

	// Action definitions persist next to the chat database, so user-defined
	// actions survive restarts exactly like conversations do. Ephemeral
	// engines (tests, and anything pointed at ":memory:") skip the file
	// entirely to stay hermetic.
	if storePath == ":memory:" {
		eng.actions = NewActionManager("")
	} else {
		eng.actions = NewActionManager(filepath.Join(filepath.Dir(storePath), "actions.json"))
	}
	// Any change to the registry refreshes the action catalogue in the
	// context window, so the model - and the user copying that window - see
	// the current set of actions right away.
	eng.actions.SetOnChange(eng.syncSystemPrompt)

	// Resume the most recently updated conversation, or start one. Ordering
	// is by updated_at so a returning user lands where they left off.
	if store != nil {
		if sessions, listErr := store.ListSessions(); listErr == nil && len(sessions) > 0 {
			eng.activeSession = sessions[0]
			eng.loadSessionHistory(sessions[0].ID)
		} else if sess, createErr := store.CreateSession("New Chat", cfg.Model); createErr == nil {
			eng.activeSession = sess
		}
		_ = store.SaveModelHistory(cfg.Model)
	}

	// The context window opens with the action catalogue already in the
	// system message, so the very first question knows what it may run.
	eng.syncSystemPrompt()

	return eng
}

// effectiveSystemPrompt is the system prompt the model actually receives:
// the configured prompt plus the live action catalogue and the operating
// directory.
//
// The actions live in the context window itself rather than being patched
// onto the outgoing request, so what the user copies or dumps is exactly
// what the model sees - and toggling an action updates the window at once.
func (e *Engine) effectiveSystemPrompt() string {
	e.cfgMu.RLock()
	base := e.cfg.SystemPrompt
	e.cfgMu.RUnlock()
	if e.actions != nil {
		base += e.actions.GenerateSystemPromptInstructions()
	}
	return base
}

// syncSystemPrompt refreshes the system message in the context window so it
// carries the current action catalogue.
func (e *Engine) syncSystemPrompt() {
	eff := e.effectiveSystemPrompt()
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	if len(e.history) > 0 && e.history[0].Role == "system" {
		e.history[0].Content = eff
		return
	}
	e.history = append([]Message{{Role: "system", Content: eff}}, e.history...)
}

// SyncSystemPrompt refreshes the system prompt in the context window with
// the current actions. The GUI calls it whenever an action is added,
// removed, toggled, or the master switch flips, so the change is visible
// immediately - in the next request and in the copied context window.
func (e *Engine) SyncSystemPrompt() {
	e.syncSystemPrompt()
}

// ActionManager exposes the live action registry for bindings. The manager is
// safe for concurrent use, so callers may hold the pointer and mutate it.
func (e *Engine) ActionManager() *ActionManager {
	return e.actions
}

func (e *Engine) hash(messages []Message) string {
	hasher := sha256.New()
	for _, m := range messages {
		hasher.Write([]byte(m.Role + ":" + m.Content + ";"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func (e *Engine) Ask(ctx context.Context, input string) (*Result, error) {
	start := time.Now()

	e.historyMu.Lock()
	// Build the request payload without committing the user message to shared
	// history yet. If the call fails we must not leave a dangling user turn
	// behind, otherwise the next call resends an orphaned prompt.
	prospective := make([]Message, 0, len(e.history)+1)
	prospective = append(prospective, e.history...)
	prospective = append(prospective, Message{Role: "user", Content: input})
	if len(prospective) > e.cfg.MaxHistory+1 {
		pruned := []Message{prospective[0]}
		pruned = append(pruned, prospective[len(prospective)-e.cfg.MaxHistory:]...)
		prospective = pruned
	}
	e.historyMu.Unlock()

	currentHistory := make([]Message, len(prospective))
	copy(currentHistory, prospective)

	key := e.hash(currentHistory)

	// Check cache
	e.cacheMu.RLock()
	cachedReply, found := e.cache[key]
	e.cacheMu.RUnlock()

	if found {
		e.commitExchange(input, cachedReply)

		return &Result{
			Text: cachedReply,
			Meta: ResponseMeta{
				Cached:      true,
				SessionCost: e.sessionCost(),
				Duration:    time.Since(start),
			},
		}, nil
	}

	// Read model, endpoint and key through locked accessors: all three can be
	// changed by SetModel/SetBaseURL/SetAPIKey while a call is in flight.
	model := e.model()
	baseURL := e.baseURL()
	apiKey := e.apiKey()

	bodyBytes, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    currentHistory,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	if parsed.Error != nil {
		return nil, fmt.Errorf("upstream error: %s", parsed.Error.Message)
	}

	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("empty choice array returned")
	}

	reply := parsed.Choices[0].Message.Content

	e.commitExchange(input, reply)

	e.cacheMu.Lock()
	e.cache[key] = reply
	e.cacheMu.Unlock()

	inT := parsed.Usage.PromptTokens
	outT := parsed.Usage.CompletionTokens

	turnCost := computeTurnCost(DetectProvider(model), baseURL, model, inT, outT)
	sessionCost := e.addUsage(inT, outT, turnCost)

	// Persist the completed turn. Placed here rather than at commitExchange
	// because inT/outT/turnCost are only known once usage and pricing have
	// been resolved. Persistence is deliberately best-effort: a write failure
	// must never turn a successful, already-paid-for reply into an error.
	e.persistExchange(input, reply, model, inT, outT, turnCost)

	return &Result{
		Text: reply,
		Meta: ResponseMeta{
			Cached:       false,
			PromptTokens: inT,
			OutputTokens: outT,
			TurnCost:     turnCost,
			SessionCost:  sessionCost,
			Duration:     time.Since(start),
		},
	}, nil
}

// computeTurnCost prices one assistant turn. Local inference charges
// nothing, so pricing must never fall back to a cloud rate: the check is
// two-dimensional - a known local provider OR a custom endpoint pointed at
// the loopback are both treated as local. Unlisted cloud models fall back to
// the gpt-4o-mini table rather than under-reporting their cost.
func computeTurnCost(provider Provider, baseURL, model string, inT, outT int) float64 {
	if provider.IsLocal || IsLocalEndpoint(baseURL) {
		return 0.0
	}
	pricing, exists := DefaultPricing[model]
	if !exists {
		pricing = DefaultPricing["gpt-4o-mini"]
	}
	return (float64(inT) / 1_000_000 * pricing.InputPer1MTokens) +
		(float64(outT) / 1_000_000 * pricing.OutputPer1MTokens)
}

// commitExchange appends a completed user/assistant pair to history in one
// critical section, then re-applies the sliding window.
func (e *Engine) commitExchange(user, assistant string) {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	e.history = append(e.history,
		Message{Role: "user", Content: user},
		Message{Role: "assistant", Content: assistant},
	)
	if len(e.history) > e.cfg.MaxHistory+1 {
		pruned := []Message{e.history[0]}
		pruned = append(pruned, e.history[len(e.history)-e.cfg.MaxHistory:]...)
		e.history = pruned
	}
}

// persistExchange writes a completed exchange to the active session. It is
// deliberately best-effort and nil-guarded: persistence must never surface an
// error for a reply that already succeeded and was already billed for.
func (e *Engine) persistExchange(input, reply, model string, inT, outT int, turnCost float64) {
	if e.store == nil {
		return
	}

	e.sessionMu.RLock()
	activeID := ""
	if e.activeSession != nil {
		activeID = e.activeSession.ID
	}
	e.sessionMu.RUnlock()

	if activeID == "" {
		return
	}

	// Two separate writes, so a failure on one still records the other rather
	// than losing the whole turn.
	_, _ = e.store.AddMessage(activeID, MessageRecord{
		Role:         "user",
		Content:      input,
		PromptTokens: inT,
	})
	_, _ = e.store.AddMessage(activeID, MessageRecord{
		Role:         "assistant",
		Content:      reply,
		OutputTokens: outT,
		Cost:         turnCost,
	})
	_ = e.store.SaveModelHistory(model)
}

// loadSessionHistory rebuilds the in-memory context window from a stored
// session, keeping the system prompt at index 0 and re-applying the sliding
// window so the oldest turns are dropped rather than the newest.
func (e *Engine) loadSessionHistory(sessionID string) {
	if e.store == nil {
		return
	}
	msgs, err := e.store.GetMessages(sessionID)
	if err != nil {
		return
	}

	// Read the config before taking historyMu rather than inside it, so the
	// two locks are never nested in either order.
	e.cfgMu.RLock()
	maxHist := e.cfg.MaxHistory
	e.cfgMu.RUnlock()

	history := []Message{{Role: "system", Content: e.effectiveSystemPrompt()}}
	for _, m := range msgs {
		history = append(history, Message{Role: m.Role, Content: m.Content})
	}
	if len(history) > maxHist+1 {
		pruned := []Message{history[0]}
		pruned = append(pruned, history[len(history)-maxHist:]...)
		history = pruned
	}

	e.historyMu.Lock()
	e.history = history
	e.historyMu.Unlock()
}

func (e *Engine) model() string {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg.Model
}

func (e *Engine) apiKey() string {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg.APIKey
}

func (e *Engine) baseURL() string {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg.BaseURL
}

// SetAPIKey swaps the key used for subsequent requests, for the current
// provider, without restarting the session. The key is also persisted, so it
// survives a restart instead of having to be retyped every launch.
func (e *Engine) SetAPIKey(key string) {
	key = strings.TrimSpace(key)
	e.cfgMu.Lock()
	e.cfg.APIKey = key
	provider := DetectProvider(e.cfg.Model)
	if provider.ID != "" {
		if e.providerKeys == nil {
			e.providerKeys = make(map[string]string)
		}
		e.providerKeys[provider.ID] = key
	}
	e.cfgMu.Unlock()

	if e.settings != nil {
		e.settings.Update(func(st *Settings) {
			st.APIKey = key
			if provider.ID != "" {
				if st.ProviderKeys == nil {
					st.ProviderKeys = make(map[string]string)
				}
				st.ProviderKeys[provider.ID] = key
			}
		})
	}
}

func (e *Engine) GetAPIKey() string {
	return e.apiKey()
}

// MaskedAPIKey returns a display-safe form of the key. The raw key is never
// returned here, so it cannot reach logs, screens or screenshots.
func (e *Engine) MaskedAPIKey() string {
	return MaskKey(e.apiKey())
}

// SetBaseURL repoints the engine at another OpenAI-compatible endpoint and
// marks it explicit, so later SetModel calls leave it alone. The endpoint is
// persisted as well, so a custom server survives a restart.
func (e *Engine) SetBaseURL(url string) {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	e.cfgMu.Lock()
	e.cfg.BaseURL = url
	e.customBaseURL = url != ""
	e.cfgMu.Unlock()

	if e.settings != nil {
		e.settings.Update(func(st *Settings) {
			st.BaseURL = url
		})
	}
}

func (e *Engine) GetBaseURL() string {
	return e.baseURL()
}

func (e *Engine) sessionCost() float64 {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	return e.totalCost
}

// addUsage folds a turn's token counts and cost into the session totals and
// returns the resulting session cost.
func (e *Engine) addUsage(in, out int, cost float64) float64 {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.totalInput += in
	e.totalOutput += out
	e.totalCost += cost
	return e.totalCost
}

// InjectAssistantMessage manually adds an assistant message without calling LLM
func (e *Engine) InjectAssistantMessage(content string) {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	e.history = append(e.history, Message{Role: "assistant", Content: content})
}

// DumpContext writes the entire current sliding window to a local .txt file
func (e *Engine) DumpContext() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("navi_dump_%s.txt", time.Now().Format("20060102_150405"))
	fullPath := filepath.Join(cwd, filename)

	in, out, cost := e.usageSnapshot()
	model := e.model()
	baseURL := e.baseURL()

	e.historyMu.RLock()
	defer e.historyMu.RUnlock()

	var buf bytes.Buffer
	buf.WriteString("=====================================================\n")
	buf.WriteString(fmt.Sprintf("NAVI CONTEXT DUMP - %s\n", time.Now().Format(time.RFC1123)))
	buf.WriteString(fmt.Sprintf("Model:        %s\n", model))
	buf.WriteString(fmt.Sprintf("Endpoint:     %s\n", baseURL))
	buf.WriteString(fmt.Sprintf("API Key:      %s\n", e.MaskedAPIKey()))
	buf.WriteString(fmt.Sprintf("Session Cost: $%.6f\n", cost))
	buf.WriteString(fmt.Sprintf("Tokens In/Out: %d / %d\n", in, out))
	buf.WriteString("=====================================================\n\n")

	for i, msg := range e.history {
		buf.WriteString(fmt.Sprintf("[%d] %s:\n", i, strings.ToUpper(msg.Role)))
		buf.WriteString(msg.Content)
		buf.WriteString("\n-----------------------------------------------------\n")
	}

	if err := os.WriteFile(fullPath, buf.Bytes(), 0644); err != nil {
		return "", err
	}

	return fullPath, nil
}

// usageSnapshot returns a consistent read of the session token counters.
func (e *Engine) usageSnapshot() (in, out int, cost float64) {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	return e.totalInput, e.totalOutput, e.totalCost
}

func (e *Engine) GetContextText() string {
	e.historyMu.RLock()
	defer e.historyMu.RUnlock()
	var b strings.Builder
	for _, m := range e.history {
		b.WriteString(fmt.Sprintf("[%s]\n%s\n\n", strings.ToUpper(m.Role), m.Content))
	}
	return b.String()
}

func (e *Engine) ResetHistory() {
	// The prompt is built before the lock is taken, so no lock is ever held
	// while another one is acquired.
	eff := e.effectiveSystemPrompt()
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	e.history = []Message{{Role: "system", Content: eff}}
}

func (e *Engine) ClearCache() {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.cache = make(map[string]string)
}

// SetModel switches the active model. It follows the provider registry: the
// endpoint moves with the model unless the endpoint was explicitly set, and
// the remembered key for the new provider is restored if there is one.
//
// The choice is persisted, so the next launch starts on the same model
// instead of falling back to the default.
//
// It also clears the response cache, because the cache key is a hash of the
// message list only and carries no model identity - without this, switching
// models keeps serving the previous model's cached replies.
func (e *Engine) SetModel(model string) {
	e.setModel(model, true)
}

// setModel switches the active model. persist decides whether the choice is
// written to settings.json: switching to a saved conversation adopts that
// conversation's model, which is a browsing decision and must not overwrite
// the model the user picked for the app.
func (e *Engine) setModel(model string, persist bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	provider := DetectProvider(model)

	e.cfgMu.Lock()
	e.cfg.Model = model

	// If BaseURL was not manually locked, auto-adjust to provider endpoint
	if !e.customBaseURL && provider.BaseURL != "" {
		e.cfg.BaseURL = provider.BaseURL
	}

	// Restore remembered provider key or resolve from environment
	if provider.ID != "" {
		if savedKey, exists := e.providerKeys[provider.ID]; exists && savedKey != "" {
			e.cfg.APIKey = savedKey
		} else if envKey := ResolveKeyForProvider(provider); envKey != "" {
			e.cfg.APIKey = envKey
			if e.providerKeys == nil {
				e.providerKeys = make(map[string]string)
			}
			e.providerKeys[provider.ID] = envKey
		}
	}
	e.cfgMu.Unlock()

	// 1. Remember it for the next launch.
	if persist && e.settings != nil {
		e.settings.Update(func(st *Settings) {
			st.Model = model
		})
	}

	// 2. Record it on the active conversation, so the session list stays
	// honest and switching back to it restores the model.
	e.sessionMu.RLock()
	activeID := ""
	if e.activeSession != nil {
		activeID = e.activeSession.ID
	}
	e.sessionMu.RUnlock()
	if activeID != "" {
		_ = e.UpdateSessionModel(activeID, model)
	}

	// 3. Keep the model history current.
	if e.store != nil {
		_ = e.store.SaveModelHistory(model)
	}

	e.ClearCache()
}

func (e *Engine) GetStats() Stats {
	e.cacheMu.RLock()
	cCount := len(e.cache)
	e.cacheMu.RUnlock()

	e.historyMu.RLock()
	hDepth := len(e.history)
	e.historyMu.RUnlock()

	in, out, cost := e.usageSnapshot()

	return Stats{
		Model:        e.model(),
		InputTokens:  in,
		OutputTokens: out,
		TotalCost:    cost,
		CacheEntries: cCount,
		HistoryDepth: hDepth,
	}
}

func (e *Engine) GetHistory() []Message {
	e.historyMu.RLock()
	defer e.historyMu.RUnlock()
	out := make([]Message, len(e.history))
	copy(out, e.history)
	return out
}

func (e *Engine) TotalCost() float64 {
	return e.sessionCost()
}
