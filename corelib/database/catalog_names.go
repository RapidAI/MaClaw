package database

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// SetCatalogNamePath loads remembered schema/database names from path and
// writes later RememberCatalogNames calls there. Empty path keeps memory only.
func (m *Manager) SetCatalogNamePath(path string) {
	path = strings.TrimSpace(path)
	var stored map[string][]string
	if path != "" {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &stored)
		}
	}
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.catalogPath = path
	if m.catalogNames == nil {
		m.catalogNames = make(map[string][]string)
	}
	for id, names := range stored {
		m.mergeCatalogNamesLocked(id, names)
	}
}

// RememberCatalogNames records non-secret schema/database names observed
// during inspect/connect so later short queries can retrieve the database tool.
func (m *Manager) RememberCatalogNames(profileID string, names ...string) {
	if m == nil || strings.TrimSpace(profileID) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if m.mergeCatalogNamesLocked(profileID, names) {
		m.persistCatalogNamesLocked()
	}
}

// SearchTokens is the host-facing retrieval overlay: configured profiles plus
// remembered catalog names, still without secrets.
func (m *Manager) SearchTokens() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	profiles := make([]Profile, 0, len(m.profiles))
	live := make(map[string]struct{}, len(m.profiles))
	for id, p := range m.profiles {
		if p.Disabled {
			continue
		}
		live[id] = struct{}{}
		p.SecretRef = ""
		p.DSN = ""
		p.Password = ""
		profiles = append(profiles, p)
	}
	tokens := ProfileSearchTokens(profiles)
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		seen[strings.ToLower(token)] = struct{}{}
	}
	for id, names := range m.catalogNames {
		if _, ok := live[id]; !ok {
			continue
		}
		for _, name := range names {
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			tokens = append(tokens, name)
		}
	}
	return tokens
}

func (m *Manager) mergeCatalogNamesLocked(profileID string, names []string) bool {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return false
	}
	if m.catalogNames == nil {
		m.catalogNames = make(map[string][]string)
	}
	current := append([]string{}, m.catalogNames[profileID]...)
	seen := make(map[string]struct{}, len(current))
	for _, name := range current {
		seen[strings.ToLower(name)] = struct{}{}
	}
	changed := false
	for _, raw := range names {
		token, ok := searchToken(raw)
		if !ok {
			continue
		}
		key := strings.ToLower(token)
		if _, exists := seen[key]; exists {
			continue
		}
		if len(current) >= maxCatalogNamesPerProfile {
			break
		}
		seen[key] = struct{}{}
		current = append(current, token)
		changed = true
	}
	if changed {
		m.catalogNames[profileID] = current
	}
	return changed
}

func (m *Manager) persistCatalogNamesLocked() {
	if strings.TrimSpace(m.catalogPath) == "" {
		return
	}
	data, err := json.Marshal(m.catalogNames)
	if err != nil {
		return
	}
	dir := filepath.Dir(m.catalogPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = fileutil.AtomicWriteFile(m.catalogPath, data, 0o644)
}
