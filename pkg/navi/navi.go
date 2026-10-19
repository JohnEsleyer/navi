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
	Cached       bool
	PromptTokens int
	OutputTokens int
	TurnCost     float64
	SessionCost  float64
	Duration     time.Duration
}

type Result struct {
	Text string
	Meta ResponseMeta
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
	Model        string
	InputTokens  int
	OutputTokens int
	TotalCost    float64
	CacheEntries int
	HistoryDepth int
}

type Engine struct {
	cfg         Config
	client      *http.Client
	cacheMu     sync.RWMutex
	cache       map[string]string
	historyMu   sync.RWMutex
	history     []Message
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
	return &Engine{
		cfg:     cfg,
		client:  &http.Client{Timeout: 60 * time.Second},
		cache:   make(map[string]string),
		history: []Message{{Role: "system", Content: cfg.SystemPrompt}},
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
	e.history = append(e.history, Message{Role: "user", Content: input})

	// Sliding window
	if len(e.history) > e.cfg.MaxHistory+1 {
		pruned := []Message{e.history[0]}
		pruned = append(pruned, e.history[len(e.history)-e.cfg.MaxHistory:]...)
		e.history = pruned
	}

	currentHistory := make([]Message, len(e.history))
	copy(currentHistory, e.history)
	e.historyMu.Unlock()

	key := e.hash(currentHistory)

	// Check cache
	e.cacheMu.RLock()
	cachedReply, found := e.cache[key]
	e.cacheMu.RUnlock()

	if found {
		e.historyMu.Lock()
		e.history = append(e.history, Message{Role: "assistant", Content: cachedReply})
		e.historyMu.Unlock()

		return &Result{
			Text: cachedReply,
			Meta: ResponseMeta{
				Cached:      true,
				SessionCost: e.totalCost,
				Duration:    time.Since(start),
			},
		}, nil
	}

	bodyBytes, err := json.Marshal(chatRequest{
		Model:       e.cfg.Model,
		Messages:    currentHistory,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.cfg.BaseURL+"/chat/completions", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
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

	e.historyMu.Lock()
	e.history = append(e.history, Message{Role: "assistant", Content: reply})
	e.historyMu.Unlock()

	e.cacheMu.Lock()
	e.cache[key] = reply
	cacheCount := len(e.cache)
	e.cacheMu.Unlock()

	inT := parsed.Usage.PromptTokens
	outT := parsed.Usage.CompletionTokens
	e.totalInput += inT
	e.totalOutput += outT

	pricing, exists := DefaultPricing[e.cfg.Model]
	if !exists {
		pricing = DefaultPricing["gpt-4o-mini"]
	}

	turnCost := (float64(inT) / 1_000_000 * pricing.InputPer1MTokens) +
		(float64(outT) / 1_000_000 * pricing.OutputPer1MTokens)
	e.totalCost += turnCost

	_ = cacheCount

	return &Result{
		Text: reply,
		Meta: ResponseMeta{
			Cached:       false,
			PromptTokens: inT,
			OutputTokens: outT,
			TurnCost:     turnCost,
			SessionCost:  e.totalCost,
			Duration:     time.Since(start),
		},
	}, nil
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

func (e *Engine) SetModel(model string) {
	e.cfg.Model = strings.TrimSpace(model)
}

func (e *Engine) SetSystemPrompt(prompt string) {
	e.cfg.SystemPrompt = prompt
	e.ResetHistory()
}

func (e *Engine) TotalCost() float64 {
	return e.totalCost
}

func (e *Engine) GetStats() Stats {
	e.cacheMu.RLock()
	cCount := len(e.cache)
	e.cacheMu.RUnlock()

	e.historyMu.RLock()
	hDepth := len(e.history)
	e.historyMu.RUnlock()

	return Stats{
		Model:        e.cfg.Model,
		InputTokens:  e.totalInput,
		OutputTokens: e.totalOutput,
		TotalCost:    e.totalCost,
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
