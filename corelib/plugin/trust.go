package plugin

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// TrustConfirmFunc is a callback that asks the user whether to trust a
// project-level plugin.  It receives the plugin name and its directory.
type TrustConfirmFunc func(pluginName, pluginDir string) bool

// TrustStore manages the set of trusted project-level plugins.
// Trusted names are persisted to ~/.maclaw/data/trusted_plugins.json.
type TrustStore struct {
	mu       sync.Mutex
	trusted  map[string]bool // plugin name → trusted
	filePath string          // ~/.maclaw/data/trusted_plugins.json
	confirm  TrustConfirmFunc
}

// trustFilePayload is the JSON structure stored on disk.
type trustFilePayload struct {
	Trusted []string `json:"trusted"`
}

// NewTrustStore creates a TrustStore with the given confirmation callback and
// loads any previously trusted plugins from ~/.maclaw/data/trusted_plugins.json.
func NewTrustStore(confirm TrustConfirmFunc) *TrustStore {
	home, _ := os.UserHomeDir()
	fp := filepath.Join(home, ".maclaw", "data", "trusted_plugins.json")

	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
		confirm:  confirm,
	}
	ts.load()
	return ts
}

// IsTrusted reports whether the named plugin has already been trusted.
func (ts *TrustStore) IsTrusted(name string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.trusted[name]
}

// CheckTrust returns true if the plugin is already trusted.  Otherwise it
// invokes the confirm callback; if the user confirms, the plugin is added to
// the trusted set and persisted to disk.
func (ts *TrustStore) CheckTrust(name, dir string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.trusted[name] {
		return true
	}

	if ts.confirm != nil && ts.confirm(name, dir) {
		ts.trusted[name] = true
		ts.save()
		return true
	}
	return false
}

// load reads the JSON trust file into the trusted map.
// If the file does not exist or cannot be parsed, the map stays empty.
func (ts *TrustStore) load() {
	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		return // file missing or unreadable — not an error
	}
	var payload trustFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	for _, name := range payload.Trusted {
		ts.trusted[name] = true
	}
}

// save writes the trusted map to the JSON file, creating parent directories
// if needed.
func (ts *TrustStore) save() {
	names := make([]string, 0, len(ts.trusted))
	for name := range ts.trusted {
		names = append(names, name)
	}
	payload := trustFilePayload{Trusted: names}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Printf("WARN: trust store: marshal failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(ts.filePath), 0o755); err != nil {
		log.Printf("WARN: trust store: create dir failed: %v", err)
		return
	}
	if err := os.WriteFile(ts.filePath, data, 0o644); err != nil {
		log.Printf("WARN: trust store: write failed: %v", err)
	}
}
