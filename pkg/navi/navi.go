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
}

func New(cfg Config) *Engine {
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 6
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are Navi, an ultra-efficient, cost-conscious assistant. Keep answers concise."
	}
	if cfg.Model == "" {
		cfg.Model = "llama-3.1-8b-instant"
	}

	provider := DetectProvider(cfg.Model)

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
	if provider.ID != "" && cfg.APIKey != "" {
		pKeys[provider.ID] = cfg.APIKey
	}

	return &Engine{
		cfg:           cfg,
		customBaseURL: callerSetBaseURL,
		providerKeys:  pKeys,
		client:        &http.Client{Timeout: 60 * time.Second},
		cache:         make(map[string]string),
		history:       []Message{{Role: "system", Content: cfg.SystemPrompt}},
	}
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

	pricing, exists := DefaultPricing[model]
	if !exists {
		pricing = DefaultPricing["gpt-4o-mini"]
	}

	turnCost := (float64(inT) / 1_000_000 * pricing.InputPer1MTokens) +
		(float64(outT) / 1_000_000 * pricing.OutputPer1MTokens)
	sessionCost := e.addUsage(inT, outT, turnCost)

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
// provider, without restarting the session.
func (e *Engine) SetAPIKey(key string) {
	key = strings.TrimSpace(key)
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()
	e.cfg.APIKey = key
	provider := DetectProvider(e.cfg.Model)
	if provider.ID != "" {
		if e.providerKeys == nil {
			e.providerKeys = make(map[string]string)
		}
		e.providerKeys[provider.ID] = key
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
// marks it explicit, so later SetModel calls leave it alone.
func (e *Engine) SetBaseURL(url string) {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()
	e.cfg.BaseURL = url
	e.customBaseURL = url != ""
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
	buf.WriteString(fmt.Sprintf("🧚 NAVI CONTEXT DUMP - %s\n", time.Now().Format(time.RFC1123)))
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
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	e.history = []Message{{Role: "system", Content: e.cfg.SystemPrompt}}
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
// It also clears the response cache, because the cache key is a hash of the
// message list only and carries no model identity - without this, switching
// models keeps serving the previous model's cached replies.
func (e *Engine) SetModel(model string) {
	model = strings.TrimSpace(model)
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
