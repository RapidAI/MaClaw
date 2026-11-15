// Catalog display names (Qwen3.8-Max, DeepSeek-V4-Pro) are what provider
// settings store. The agent_chat node only starts a model for its short key
// (qmodel_38max, dmodel). This map is filled from the live catalog and kept
// across restarts so a saved display name still signs as the right key.
package qoder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type storedAliases struct {
	// Labels maps a wire key to the display name settings store.
	Labels map[string]string `json:"labels"`
	// ByName is the previous file shape (lowercased name → wire key). It is
	// read so a file written before Labels existed still resolves. Entries
	// where the name is the key itself are ignored.
	ByName map[string]string `json:"by_name,omitempty"`
}

var (
	aliasMu         sync.Mutex
	aliasPersistMu  sync.Mutex
	aliasLoaded     bool
	aliasLabelToKey = map[string]string{} // lowercased display name → wire key
	aliasLabelByKey = map[string]string{} // wire key → display name
	builtinLabels   = []Model{
		{Key: "qmodel_38max", DisplayName: DefaultModelLabel, Enabled: true},
		{Key: "qfmodel", DisplayName: "Qwen3.8-Flash", Enabled: true},
	}
)

// RememberModels records display name → catalog key for every entry. Later
// chats can turn a saved normal name back into the key the node expects.
func RememberModels(models []Model) {
	if !rememberModelsLocked(models) {
		return
	}
	persistAliases()
}

func rememberModelsLocked(models []Model) bool {
	aliasMu.Lock()
	defer aliasMu.Unlock()
	loadAliasesLocked()
	changed := false
	for _, model := range models {
		if rememberLocked(model) {
			changed = true
		}
	}
	return changed
}

// persistAliases writes the latest map. Callers serialize on aliasPersistMu
// and copy under aliasMu, so a slower save cannot publish an older snapshot
// over one that already includes a newer catalog.
func persistAliases() {
	aliasPersistMu.Lock()
	defer aliasPersistMu.Unlock()
	aliasMu.Lock()
	path := aliasFilePath()
	labels := make(map[string]string, len(aliasLabelByKey))
	for key, label := range aliasLabelByKey {
		labels[key] = label
	}
	aliasMu.Unlock()
	if path == "" {
		return
	}
	data, err := json.Marshal(storedAliases{Labels: labels})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.WriteFile(path, data, 0o600)
		_ = os.Remove(tmp)
	}
}

// WireModelKey is the catalog key a saved settings name must sign as.
// Display names and the legacy Qwen 3.8 aliases map to that key; a short
// key, or any unknown name, is returned unchanged.
func WireModelKey(model string) string {
	return canonicalQoderModelKey(model)
}

// CanonicalDisplayName returns the catalog display name for a saved model
// string. A short key and its display name both resolve to the display name.
// An unknown name returns "" so callers leave it unchanged.
func CanonicalDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	key := canonicalQoderModelKey(value)
	aliasMu.Lock()
	defer aliasMu.Unlock()
	loadAliasesLocked()
	label := strings.TrimSpace(aliasLabelByKey[key])
	if label == "" || strings.EqualFold(label, key) {
		return ""
	}
	return label
}

func lookupAliasKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	aliasMu.Lock()
	defer aliasMu.Unlock()
	loadAliasesLocked()
	return aliasLabelToKey[strings.ToLower(model)]
}

func rememberLocked(model Model) bool {
	key := strings.TrimSpace(model.Key)
	if key == "" {
		return false
	}
	label := strings.TrimSpace(model.DisplayName)
	// "Auto" for key "auto" is the wire key. Remembering it would make the
	// settings name differ from the only string the node accepts.
	if label == "" || strings.EqualFold(label, key) {
		prev := aliasLabelByKey[key]
		if prev == "" {
			return false
		}
		delete(aliasLabelToKey, strings.ToLower(prev))
		delete(aliasLabelByKey, key)
		return true
	}
	fold := strings.ToLower(label)
	if existing, ok := aliasLabelToKey[fold]; ok && existing != key {
		// Another model already owns this spelling. Recording it here would
		// make CanonicalDisplayName rewrite this key into that model's name,
		// and the chat would then sign the other key.
		return false
	}
	changed := false
	if prev := aliasLabelByKey[key]; prev != "" && prev != label {
		// Drop a spelling the catalog no longer uses. A case-only change
		// keeps the same fold, so the lookup entry is rewritten below.
		if !strings.EqualFold(prev, label) {
			delete(aliasLabelToKey, strings.ToLower(prev))
		}
		changed = true
	}
	if aliasLabelByKey[key] != label {
		aliasLabelByKey[key] = label
		changed = true
	}
	if aliasLabelToKey[fold] != key {
		aliasLabelToKey[fold] = key
		changed = true
	}
	return changed
}

func loadAliasesLocked() {
	if aliasLoaded {
		return
	}
	aliasLoaded = true
	for _, model := range builtinLabels {
		_ = rememberLocked(model)
	}
	path := aliasFilePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var stored storedAliases
	if json.Unmarshal(data, &stored) != nil {
		return
	}
	// Legacy files stored lowercased names. Apply them first so a Labels
	// entry with the original spelling wins.
	for name, key := range stored.ByName {
		name = strings.TrimSpace(name)
		key = strings.TrimSpace(key)
		// A wire key stored under its own name is not a display name. Applying
		// it would drop the real label remembered for that key.
		if name == "" || key == "" || strings.EqualFold(name, key) {
			continue
		}
		_ = rememberLocked(Model{Key: key, DisplayName: name})
	}
	for key, label := range stored.Labels {
		_ = rememberLocked(Model{Key: key, DisplayName: label})
	}
}

func aliasFilePath() string {
	if path := strings.TrimSpace(os.Getenv("QODER_MODEL_ALIAS_FILE")); path != "" {
		if path == "off" {
			return ""
		}
		return path
	}
	// Tests share one process. Writing the developer home here would leak
	// catalog aliases from one test into the next, and into a running app.
	if testing.Testing() {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".maclaw", "qoder-model-aliases.json")
	}
	return filepath.Join(home, ".maclaw", "qoder-model-aliases.json")
}
