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
