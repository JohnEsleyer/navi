package navi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings holds the configuration that must survive a restart: the last
// model the user picked, the API key they entered and any custom endpoint.
//
// The API key is stored in plain text on purpose - it is the user's own key
// for their own machine - but the file is written with 0600 permissions in
// the user's config directory so other accounts on the box cannot read it.
type Settings struct {
	Model        string            `json:"model"`
	APIKey       string            `json:"api_key"`
	BaseURL      string            `json:"base_url"`
	ProviderKeys map[string]string `json:"provider_keys"`
}

// SettingsStore is the on-disk home of the Settings struct. It is safe for
// concurrent use: the engine mutates it from the GUI while reads may happen
// from other goroutines.
type SettingsStore struct {
	mu   sync.RWMutex
	path string
	data Settings
}

// NewSettingsStore opens (or creates) the settings file at path. An empty
// path or ":memory:" keeps everything in memory, which is what the test
// engines use so no state leaks between runs or onto the disk.
func NewSettingsStore(path string) *SettingsStore {
	ss := &SettingsStore{
		path: path,
		data: Settings{
			ProviderKeys: make(map[string]string),
		},
	}
	ss.load()
	return ss
}

// load restores the persisted settings. A missing or corrupt file is not an
// error: the engine simply starts with its defaults.
func (s *SettingsStore) load() {
	if s.path == "" || s.path == ":memory:" {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, &s.data)
	if s.data.ProviderKeys == nil {
		s.data.ProviderKeys = make(map[string]string)
	}
}

// save writes the current settings back to disk. Failures are swallowed:
// losing the persistence is annoying, but it must never break the live
// session that just set the value.
func (s *SettingsStore) save() {
	if s.path == "" || s.path == ":memory:" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0755)
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0600)
}

// Get returns a snapshot of the persisted settings.
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

// Update mutates the settings under the lock and persists the result.
func (s *SettingsStore) Update(fn func(st *Settings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	if s.data.ProviderKeys == nil {
		s.data.ProviderKeys = make(map[string]string)
	}
	s.save()
}
