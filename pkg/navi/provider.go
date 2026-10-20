package navi

import (
	"net/url"
	"os"
	"strings"
)

// Provider describes an OpenAI-compatible endpoint. Every provider Navi
// supports speaks the same wire protocol (POST /chat/completions), so a
// provider is data, not code: adding one means appending a Provider here,
// never writing a new client. Vendor SDKs would invert that - each new
// provider would mean a new dependency and a new code path.
type Provider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	BaseURL string   `json:"base_url"`
	EnvKeys []string `json:"env_keys"`
	Models  []string `json:"models"`
	// IsLocal marks providers whose servers run on the user's own machine.
	// Local inference charges nothing, so pricing must never fall back to a
	// cloud rate for these, and no key is required.
	IsLocal bool `json:"is_local"`
}

var KnownProviders = []Provider{
	{
		ID:      "groq",
		Name:    "Groq",
		BaseURL: "https://api.groq.com/openai/v1",
		EnvKeys: []string{"GROQ_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"llama-3.1-8b-instant", "llama-3.3-70b-versatile", "mixtral-8x7b-32768", "gemma2-9b-it"},
	},
	{
		ID:      "ollama",
		Name:    "Ollama (Local)",
		BaseURL: resolveOllamaURL(),
		EnvKeys: []string{"OLLAMA_API_KEY", "NAVI_API_KEY"},
		Models: []string{
			"llama3.2:1b", "llama3.2", "llama3.1", "llama3", "mistral",
			"phi3", "phi4", "qwen2.5", "qwen2.5-coder", "deepseek-r1",
			"deepseek-r1:8b", "deepseek-r1:14b", "codellama", "gemma2",
		},
		IsLocal: true,
	},
	{
		ID:      "lmstudio",
		Name:    "LM Studio (Local)",
		BaseURL: resolveLMStudioURL(),
		EnvKeys: []string{"LMSTUDIO_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"local-model"},
		IsLocal: true,
	},
	{
		ID:      "llamacpp",
		Name:    "llama.cpp / LocalAI (Local)",
		BaseURL: "http://localhost:8080/v1",
		EnvKeys: []string{"LOCAL_API_KEY", "NAVI_API_KEY"},
		Models:  []string{},
		IsLocal: true,
	},
	{
		ID:      "openai",
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		EnvKeys: []string{"OPENAI_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"gpt-4o-mini", "gpt-4o", "gpt-3.5-turbo", "o1-mini", "o3-mini"},
	},
	{
		ID:      "deepseek",
		Name:    "DeepSeek",
		BaseURL: "https://api.deepseek.com",
		EnvKeys: []string{"DEEPSEEK_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"deepseek-chat", "deepseek-reasoner"},
	},
	{
		ID:      "openrouter",
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		EnvKeys: []string{"OPENROUTER_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"openai/gpt-oss-20b"},
	},
}

// resolveOllamaURL honors the standard Ollama environment variables. OLLAMA_HOST
// may be bare (host:port) or already a URL; OLLAMA_BASE_URL wins when set.
func resolveOllamaURL() string {
	if host := os.Getenv("OLLAMA_BASE_URL"); host != "" {
		return strings.TrimRight(host, "/")
	}
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		host = strings.TrimSpace(host)
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = "http://" + host
		}
		if !strings.HasSuffix(host, "/v1") {
			host = strings.TrimRight(host, "/") + "/v1"
		}
		return host
	}
	return "http://localhost:11434/v1"
}

func resolveLMStudioURL() string {
	if u := os.Getenv("LMSTUDIO_BASE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:1234/v1"
}

// IsLocalEndpoint checks whether a base URL points to localhost or the
// loopback family. It is the pricing backstop: a custom endpoint pointed at
// the loopback is charged nothing even when its provider ID is unknown.
func IsLocalEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1")
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0"
}

// providerByID looks a provider up by ID. The registry is the single source
// of truth for which providers exist and in what order.
func providerByID(id string) (Provider, bool) {
	for _, p := range KnownProviders {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// DetectProvider maps a model name to the provider that serves it. Explicit
// local prefixes win first, then exact model matches, then name prefixes and
// substrings as heuristics. An unrecognised model yields a custom provider
// whose BaseURL is empty, so the caller must supply an endpoint.
func DetectProvider(model string) Provider {
	modelLower := strings.ToLower(strings.TrimSpace(model))

	// Explicit local prefixes: "ollama/llama3.2", "lmstudio/x", "local/x".
	// The prefix is stripped conceptually - the BaseURL alone tells the HTTP
	// client where to ask.
	if strings.HasPrefix(modelLower, "ollama/") {
		if p, ok := providerByID("ollama"); ok {
			return p
		}
	}
	if strings.HasPrefix(modelLower, "lmstudio/") || strings.HasPrefix(modelLower, "lms/") {
		if p, ok := providerByID("lmstudio"); ok {
			return p
		}
	}
	if strings.HasPrefix(modelLower, "local/") {
		if p, ok := providerByID("llamacpp"); ok {
			return p
		}
	}

	// Namespaced models ("openai/gpt-oss-120b") belong to the provider that
	// owns the namespace. Aggregators publish vendor-prefixed names, so the
	// registry carries one representative per namespace ("openai/gpt-oss-20b"
	// for OpenRouter) and the part in front of the slash is the routing
	// signal. Without this, such a model would fall through to the custom
	// provider and be sent to the wrong endpoint.
	if idx := strings.Index(modelLower, "/"); idx > 0 {
		namespace := modelLower[:idx+1]
		for _, p := range KnownProviders {
			for _, m := range p.Models {
				if strings.HasPrefix(strings.ToLower(m), namespace) {
					return p
				}
			}
		}
	}

	for _, p := range KnownProviders {
		for _, m := range p.Models {
			if strings.EqualFold(m, modelLower) {
				return p
			}
		}
	}

	// Heuristics for models not listed explicitly. These resolve by ID rather
	// than by position in KnownProviders, so reordering or extending that
	// slice cannot silently re-point them at the wrong provider. Order
	// matters: local-family matches (colon tags, r1/qwen/phi/…) are checked
	// before the broad "contains deepseek" cloud rule, otherwise
	// deepseek-r1:* would route to the DeepSeek cloud API.
	switch {
	case strings.HasPrefix(modelLower, "gpt-"),
		strings.HasPrefix(modelLower, "o1-"),
		strings.HasPrefix(modelLower, "o3-"):
		if p, ok := providerByID("openai"); ok {
			return p
		}
	case strings.Contains(modelLower, ":"),
		strings.Contains(modelLower, "deepseek-r1"),
		strings.Contains(modelLower, "qwen"),
		strings.Contains(modelLower, "phi4"),
		strings.Contains(modelLower, "phi-4"),
		strings.Contains(modelLower, "mistral"),
		strings.Contains(modelLower, "local"):
		// A colon tag (llama3.2:3b) and the popular local families are
		// Ollama's territory until another explicit endpoint says otherwise.
		if p, ok := providerByID("ollama"); ok {
			return p
		}
	case strings.Contains(modelLower, "deepseek"):
		if p, ok := providerByID("deepseek"); ok {
			return p
		}
	case strings.Contains(modelLower, "instant"), strings.Contains(modelLower, "versatile"):
		if p, ok := providerByID("groq"); ok {
			return p
		}
	}

	return Provider{
		ID:      "custom",
		Name:    "Custom / OpenAI-Compatible",
		BaseURL: "",
		EnvKeys: []string{"NAVI_API_KEY"},
	}
}

// ResolveKeyForProvider checks provider-specific environment variables in
// priority order, then falls back to the generic NAVI_API_KEY.
func ResolveKeyForProvider(p Provider) string {
	for _, env := range p.EnvKeys {
		if val := strings.TrimSpace(os.Getenv(env)); val != "" {
			return val
		}
	}
	return strings.TrimSpace(os.Getenv("NAVI_API_KEY"))
}

// MaskKey returns a masked representation of an API key for safe display.
// Keys are never printed in full; only the first and last four characters
// survive, which is enough to tell two keys apart without exposing either.
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "••••••••"
	}
	return key[:4] + "••••" + key[len(key)-4:]
}

// ProviderInfo is the UI-facing projection of a Provider: the fields the
// switcher dropdown needs, plus a sensible default model to select when the
// user hops to a provider they have not used before. Decoupled from Provider
// so the wire format can evolve without touching the registry.
type ProviderInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	IsLocal      bool   `json:"is_local"`
	DefaultModel string `json:"default_model"`
}

// GetKnownProviders snapshots the registry as ProviderInfo values, so the
// GUI can list local runtimes and cloud services from the same source of
// truth DetectProvider uses.
func GetKnownProviders() []ProviderInfo {
	res := make([]ProviderInfo, len(KnownProviders))
	for i, p := range KnownProviders {
		defModel := ""
		if len(p.Models) > 0 {
			defModel = p.Models[0]
		}
		res[i] = ProviderInfo{
			ID:           p.ID,
			Name:         p.Name,
			BaseURL:      p.BaseURL,
			IsLocal:      p.IsLocal,
			DefaultModel: defModel,
		}
	}
	return res
}
