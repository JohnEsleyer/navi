package navi

import (
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
		ID:      "ollama",
		Name:    "Ollama (Local)",
		BaseURL: "http://localhost:11434/v1",
		EnvKeys: []string{"OLLAMA_API_KEY"},
		Models:  []string{"llama3.2:1b", "llama3.2", "llama3.1", "mistral", "phi3"},
	},
	{
		ID:      "openrouter",
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		EnvKeys: []string{"OPENROUTER_API_KEY", "NAVI_API_KEY"},
		Models:  []string{"openai/gpt-oss-20b"},
	},
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

// DetectProvider maps a model name to the provider that serves it. Exact
// model matches win; failing that, name prefixes and substrings are used as
// heuristics. An unrecognised model yields a custom provider whose BaseURL is
// empty, so the caller must supply an endpoint.
func DetectProvider(model string) Provider {
	modelLower := strings.ToLower(strings.TrimSpace(model))

	for _, p := range KnownProviders {
		for _, m := range p.Models {
			if strings.EqualFold(m, modelLower) {
				return p
			}
		}
	}

	// Heuristics for models not listed explicitly. These resolve by ID rather
	// than by position in KnownProviders, so reordering or extending that
	// slice cannot silently re-point them at the wrong provider.
	switch {
	case strings.HasPrefix(modelLower, "gpt-"),
		strings.HasPrefix(modelLower, "o1-"),
		strings.HasPrefix(modelLower, "o3-"):
		if p, ok := providerByID("openai"); ok {
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
	case strings.Contains(modelLower, ":"), strings.Contains(modelLower, "local"):
		if p, ok := providerByID("ollama"); ok {
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
