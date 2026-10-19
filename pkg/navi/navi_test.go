package navi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// countingServer records how many requests reached it.
func countingServer(t *testing.T, hits *int, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
}

func failingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
}

// TestConcurrentAccessIsRaceFree hammers the engine from multiple goroutines.
// Run with -race; any unsynchronised field trips the detector.
func TestConcurrentAccessIsRaceFree(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := countingServer(t, &hits, &mu)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", MaxHistory: 8})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if _, err := e.Ask(context.Background(), "ping"); err != nil {
					t.Errorf("Ask: %v", err)
				}
				e.GetStats()
				e.TotalCost()
				e.GetHistory()
			}
		}(i)
	}

	// Concurrent model switching, the pair the detector previously flagged.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			e.SetModel("m2")
			e.GetStats()
		}
	}()

	wg.Wait()
}

// TestFailedRequestLeavesNoDanglingUserMessage: a non-200 must not leave the
// user's prompt in history, or the next call resends an orphaned message.
func TestFailedRequestLeavesNoDanglingUserMessage(t *testing.T) {
	srv := failingServer()
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1"})

	if _, err := e.Ask(context.Background(), "will fail"); err == nil {
		t.Fatal("expected an error from the failing server")
	}

	hist := e.GetHistory()
	if len(hist) != 1 {
		t.Fatalf("expected only the system prompt after a failed call, got %d messages: %+v", len(hist), hist)
	}
	if hist[0].Role != "system" {
		t.Fatalf("expected history[0] to be system, got %q", hist[0].Role)
	}
}

// TestSuccessfulCallCommitsBothTurns: the happy path still records the pair.
func TestSuccessfulCallCommitsBothTurns(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := countingServer(t, &hits, &mu)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1"})

	res, err := e.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Meta.Cached {
		t.Fatal("first call should not be a cache hit")
	}
	if res.Meta.PromptTokens != 10 || res.Meta.OutputTokens != 5 {
		t.Fatalf("unexpected token meta: %+v", res.Meta)
	}

	hist := e.GetHistory()
	if len(hist) != 3 {
		t.Fatalf("expected system+user+assistant, got %d: %+v", len(hist), hist)
	}
	if hist[1].Role != "user" || hist[2].Role != "assistant" {
		t.Fatalf("unexpected ordering: %+v", hist)
	}
}

// TestSetModelClearsCache: the cache key is a hash of messages only, with no
// model in it, so switching models must invalidate or stale replies leak.
func TestSetModelClearsCache(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := countingServer(t, &hits, &mu)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1"})

	if _, err := e.Ask(context.Background(), "hi"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := e.GetStats().CacheEntries; got != 1 {
		t.Fatalf("expected 1 cache entry, got %d", got)
	}

	e.SetModel("m2")

	if got := e.GetStats().CacheEntries; got != 0 {
		t.Fatalf("cache not cleared on model switch: %d entries remain", got)
	}
	if got := e.GetStats().Model; got != "m2" {
		t.Fatalf("model not switched: %q", got)
	}
}

// TestDynamicAPIKeyAndProviderSwitch: a key set at runtime must be used on the
// very next request, with no restart. Run with -race to also cover the new
// cfg-guarded fields being written while other calls read them.
func TestDynamicAPIKeyAndProviderSwitch(t *testing.T) {
	var mu sync.Mutex
	authHeaders := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "initial-key", Model: "m1"})
	e.SetBaseURL(srv.URL) // lock to test server

	if _, err := e.Ask(context.Background(), "msg 1"); err != nil {
		t.Fatalf("first Ask: %v", err)
	}

	e.SetAPIKey("updated-secret-key")
	if e.GetAPIKey() != "updated-secret-key" {
		t.Fatalf("key not updated in engine: %q", e.GetAPIKey())
	}
	if e.MaskedAPIKey() != "upda••••-key" {
		t.Errorf("unexpected masked key: %s", e.MaskedAPIKey())
	}

	if _, err := e.Ask(context.Background(), "msg 2"); err != nil {
		t.Fatalf("second Ask: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer initial-key" {
		t.Errorf("expected 'Bearer initial-key', got %q", authHeaders[0])
	}
	if authHeaders[1] != "Bearer updated-secret-key" {
		t.Errorf("expected 'Bearer updated-secret-key', got %q", authHeaders[1])
	}
}

// TestProviderDetection: model names map to providers via the registry, so a
// new provider is data, not a new code path.
func TestProviderDetection(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"llama-3.1-8b-instant", "groq"},
		{"gpt-4o-mini", "openai"},
		{"llama3.2:1b", "ollama"},
		{"deepseek-reasoner", "deepseek"},
		// Heuristics for unlisted models.
		{"gpt-5-turbo", "openai"},
		{"deepseek-v3", "deepseek"},
		{"llama-3.3-70b-preview", "custom"}, // no heuristic matches this name
		{"qwen3:8b", "ollama"},              // colon -> local
		{"totally-unknown-model", "custom"}, // falls through
	}
	for _, tc := range cases {
		if got := DetectProvider(tc.model).ID; got != tc.want {
			t.Errorf("DetectProvider(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

// TestProviderRegistryHeuristicsUseIDs: a reordering of KnownProviders must not
// change which provider a heuristic resolves to. This guards against the
// fragile index-based lookups (KnownProviders[1]) the registry started with.
func TestProviderRegistryHeuristicsUseIDs(t *testing.T) {
	orig := KnownProviders
	defer func() { KnownProviders = orig }()

	// Reverse the registry; heuristic results must be unchanged.
	rev := make([]Provider, 0, len(orig))
	for i := len(orig) - 1; i >= 0; i-- {
		rev = append(rev, orig[i])
	}
	KnownProviders = rev

	if got := DetectProvider("gpt-4o").ID; got != "openai" {
		t.Errorf("after reorder, gpt-4o -> %q, want openai", got)
	}
	if got := DetectProvider("deepseek-chat").ID; got != "deepseek" {
		t.Errorf("after reorder, deepseek-chat -> %q, want deepseek", got)
	}
}

// TestMaskKeyNeverLeaksFullKey: display must never reveal the whole secret.
func TestMaskKeyNeverLeaksFullKey(t *testing.T) {
	key := "sk-abcdef1234567890SECRET"
	masked := MaskKey(key)
	if masked == key {
		t.Fatal("MaskKey returned the full key")
	}
	if len(masked) > len(key) {
		t.Fatalf("mask longer than key: %q", masked)
	}
	if MaskKey("") != "(not set)" {
		t.Errorf("empty key should render as (not set), got %q", MaskKey(""))
	}
	if MaskKey("short") != "••••••••" {
		t.Errorf("short key should be fully masked, got %q", MaskKey("short"))
	}
}

// TestExplicitBaseURLSurvivesModelSwitch: an endpoint the caller chose must not
// be silently repointed by a later /model. New() takes BaseURL from the CLI and
// GUI from NAVI_BASE_URL, so treating it as "custom" is what keeps it stable.
func TestExplicitBaseURLSurvivesModelSwitch(t *testing.T) {
	e := New(Config{BaseURL: "http://my-own-proxy:8080/v1", APIKey: "k", Model: "m1"})
	e.SetModel("gpt-4o-mini")
	if got := e.GetBaseURL(); got != "http://my-own-proxy:8080/v1" {
		t.Errorf("explicit BaseURL was overwritten by model switch: %q", got)
	}

	// Clearing it returns the engine to auto-detection.
	e.SetBaseURL("")
	e.SetModel("gpt-4o-mini")
	if got := e.GetBaseURL(); got != "https://api.openai.com/v1" {
		t.Errorf("after clearing, expected auto-detected OpenAI endpoint, got %q", got)
	}
}

// TestSetModelFollowsProviderEndpoint: without an explicit endpoint, switching
// models must move the endpoint with the provider.
func TestSetModelFollowsProviderEndpoint(t *testing.T) {
	e := New(Config{Model: "llama-3.1-8b-instant"})
	if got := e.GetBaseURL(); got != "https://api.groq.com/openai/v1" {
		t.Fatalf("default model should use Groq, got %q", got)
	}
	e.SetModel("gpt-4o-mini")
	if got := e.GetBaseURL(); got != "https://api.openai.com/v1" {
		t.Errorf("switching to gpt-4o-mini should move endpoint to OpenAI, got %q", got)
	}
}

// TestSwitchingModelsRestoresProviderKeys: keys remembered per provider must
// come back when returning to that provider.
func TestSwitchingModelsRestoresProviderKeys(t *testing.T) {
	e := New(Config{APIKey: "groq-key", Model: "llama-3.1-8b-instant"})
	e.SetModel("gpt-4o-mini")
	e.SetAPIKey("openai-key")

	e.SetModel("llama-3.1-8b-instant")
	if got := e.GetAPIKey(); got != "groq-key" {
		t.Errorf("expected groq key restored, got %q", got)
	}
	e.SetModel("gpt-4o-mini")
	if got := e.GetAPIKey(); got != "openai-key" {
		t.Errorf("expected openai key restored, got %q", got)
	}
}

// TestSlidingWindowKeepsSystemPrompt: pruning must never drop the system role.
func TestSlidingWindowKeepsSystemPrompt(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := countingServer(t, &hits, &mu)
	defer srv.Close()

	e := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m1", MaxHistory: 4})

	for i := 0; i < 10; i++ {
		if _, err := e.Ask(context.Background(), "msg"); err != nil {
			t.Fatalf("Ask %d: %v", i, err)
		}
	}

	hist := e.GetHistory()
	if hist[0].Role != "system" {
		t.Fatalf("system prompt lost after pruning: %+v", hist)
	}
	if len(hist) > 5 { // system + MaxHistory
		t.Fatalf("history exceeded window: %d messages", len(hist))
	}
}

// TestIsLocalEndpoint: the loopback family (hostnames, IPv4/IPv6 literals and
// the wildcard bind) is local; anything else is a cloud endpoint.
func TestIsLocalEndpoint(t *testing.T) {
	local := []string{
		"http://localhost:11434/v1",
		"http://127.0.0.1:8080/v1",
		"http://[::1]:11434/v1",
		"http://0.0.0.0:8080/v1",
		"http://localhost:1234/v1",
	}
	for _, u := range local {
		if !IsLocalEndpoint(u) {
			t.Errorf("IsLocalEndpoint(%q) = false, want true", u)
		}
	}

	remote := []string{
		"https://api.groq.com/openai/v1",
		"https://api.openai.com/v1",
		"http://my-own-proxy:8080/v1",
		"https://openrouter.ai/api/v1",
	}
	for _, u := range remote {
		if IsLocalEndpoint(u) {
			t.Errorf("IsLocalEndpoint(%q) = true, want false", u)
		}
	}
}

// TestLocalModelIsFree: an Ollama-style model and a loopback endpoint must
// both price a turn at exactly zero, even though the fake server reports
// enough tokens to bill at cloud rates.
func TestLocalModelIsFree(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := countingServer(t, &hits, &mu)
	defer srv.Close()

	// Case 1: local provider by model name, endpoint still auto-resolves to
	// Ollama. Point it at the test server so the HTTP call stays local.
	e := New(Config{BaseURL: srv.URL, Model: "llama3.2:1b", APIKey: ""})
	res, err := e.Ask(context.Background(), "hello local")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Meta.TurnCost != 0 || res.Meta.SessionCost != 0 {
		t.Errorf("local provider priced a turn: turn=%f session=%f", res.Meta.TurnCost, res.Meta.SessionCost)
	}

	// Case 2: unknown model name but a loopback endpoint (127.0.0.1, which
	// httptest.NewServer binds to) -> still free.
	e2 := New(Config{BaseURL: srv.URL, Model: "mystery-model", APIKey: "k"})
	res2, err := e2.Ask(context.Background(), "hello again")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res2.Meta.TurnCost != 0 {
		t.Errorf("loopback endpoint priced a turn: %f", res2.Meta.TurnCost)
	}
	// The in-memory stats echo the same zero.
	if got := e2.TotalCost(); got != 0 {
		t.Errorf("TotalCost nonzero on loopback: %f", got)
	}
}

// TestRemoteModelStillBilled: the free-local rule must not leak onto cloud
// endpoints, or every session would under-report its real cost. The helper is
// called directly with a remote provider/endpoint so no live network is
// needed to verify the positive (nonzero) branch.
func TestRemoteModelStillBilled(t *testing.T) {
	provider := DetectProvider("gpt-4o-mini")
	if provider.IsLocal {
		t.Fatal("gpt-4o-mini should not be marked local")
	}
	cost := computeTurnCost(provider, "https://api.openai.com/v1", "gpt-4o-mini", 1000000, 500000)
	want := 0.15 + 0.60*0.5 // 1M in at $0.15/MT + 0.5M out at $0.60/MT
	if cost < want-1e-9 || cost > want+1e-9 {
		t.Errorf("remote turn billed %f, want %f", cost, want)
	}

	// An unknown cloud model falls back to the gpt-4o-mini table rather than
	// silently being free.
	fallback := computeTurnCost(DetectProvider("totally-unknown-model"), "https://api.groq.com/openai/v1", "totally-unknown-model", 1000000, 0)
	if fallback <= 0 {
		t.Errorf("unknown-model cloud fallback priced %f, want > 0", fallback)
	}

	// ...but the same model names or endpoint being local remain free.
	if free := computeTurnCost(DetectProvider("llama3.2:1b"), "http://localhost:11434/v1", "llama3.2:1b", 1000, 1000); free != 0 {
		t.Errorf("local provider priced %f, want 0", free)
	}
	if free := computeTurnCost(DetectProvider("totally-unknown-model"), "http://127.0.0.1:8080/v1", "totally-unknown-model", 1000, 1000); free != 0 {
		t.Errorf("loopback endpoint priced %f, want 0", free)
	}
}

// TestOllamaHostResolution: OLLAMA_HOST accepts both bare host:port and a
// full URL; OLLAMA_BASE_URL wins when both are set. Without either, the
// default loopback is used.
func TestOllamaHostResolution(t *testing.T) {
	cases := []struct {
		base, host, want string
	}{
		{"", "", "http://localhost:11434/v1"},
		{"", "192.168.1.10:11434", "http://192.168.1.10:11434/v1"},
		{"", "http://inference.lab:11434", "http://inference.lab:11434/v1"},
		{"http://my-box:11434", "ignored", "http://my-box:11434"},
	}
	for _, tc := range cases {
		t.Setenv("OLLAMA_BASE_URL", tc.base)
		t.Setenv("OLLAMA_HOST", tc.host)
		if got := resolveOllamaURL(); got != tc.want {
			t.Errorf("resolveOllamaURL(base=%q, host=%q) = %q, want %q", tc.base, tc.host, got, tc.want)
		}
	}
}
