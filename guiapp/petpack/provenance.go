package petpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const packSourceMarkerName = ".maclaw-pet-source"

func packSourceForDir(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, packSourceMarkerName))
	if err != nil {
		// A folder created directly in the user's pet-pack directory predates
		// provenance markers and is treated as its owner's authored pack.
		return SourceCreated
	}
	switch strings.TrimSpace(string(data)) {
	case SourceImported:
		return SourceImported
	case SourceMarket:
		return SourceMarket
	default:
		return SourceCreated
	}
}

// SetPackSource records where an installed user pack came from. The marker is
// local-only metadata: exporters skip it, so a creator's portable package does
// not claim marketplace ownership when another user installs it.
func (r *Registry) SetPackSource(id, source string) error {
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	id = strings.TrimSpace(id)
	if !IsValidPackID(id) {
		return fmt.Errorf("invalid pack id")
	}
	if source != SourceCreated && source != SourceImported && source != SourceMarket {
		return fmt.Errorf("invalid pet pack source")
	}
	dir := filepath.Join(r.userRoot, id)
	if err := assertUnderUserRoot(r.userRoot, dir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, packSourceMarkerName), []byte(source+"\n"), 0o600); err != nil {
		return err
	}
	return r.Scan()
}

// IsPackSourceMarker reports whether a relative path is local provenance
// metadata and must not be included in a portable pet-pack archive.
func IsPackSourceMarker(rel string) bool {
	return filepath.Base(filepath.Clean(rel)) == packSourceMarkerName
}

// IsPackCreatorOwned reports whether a user-scoped installed pack originated
// as a local authored pack rather than an import or market download.
func (r *Registry) IsPackCreatorOwned(id string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	m := r.packs[strings.TrimSpace(id)]
	r.mu.RUnlock()
	return m != nil && m.Scope == ScopeUser && strings.TrimSpace(m.Dir) != "" && packSourceForDir(m.Dir) == SourceCreated
}

// IsPackMarketInstalled reports whether the local copy was installed through
// the Pet Store. These packs retain a local marker so they cannot be relisted
// by the buyer even when the archive carries the same manifest ID.
func (r *Registry) IsPackMarketInstalled(id string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	m := r.packs[strings.TrimSpace(id)]
	r.mu.RUnlock()
	return m != nil && m.Scope == ScopeUser && strings.TrimSpace(m.Dir) != "" && packSourceForDir(m.Dir) == SourceMarket
}
