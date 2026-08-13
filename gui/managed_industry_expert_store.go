package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

// managedIndustryExpertStore records that a locally installed market package
// is controlled by an industry catalogue. The expert definition remains in
// the existing hardened market-install store (so package validation and
// dependency handling are unchanged); this sidecar supplies the immutable
// origin and prevents it entering normal edit/delete/share flows.
type managedIndustryExpertInstall struct {
	AssetID        string `json:"asset_id"`
	ListingID      string `json:"listing_id"`
	LocalExpertID  string `json:"local_expert_id"`
	Version        string `json:"version,omitempty"`
	Active         bool   `json:"active"`
	CatalogueScope string `json:"catalogue_scope,omitempty"`
}

func (item managedIndustryExpertInstall) sameOrigin(other managedIndustryExpertInstall) bool {
	return item.AssetID == other.AssetID && item.ListingID == other.ListingID && item.CatalogueScope == other.CatalogueScope
}

type managedIndustryExpertStoreFile struct {
	Installs []managedIndustryExpertInstall `json:"installs"`
	// ActiveAssets is the asset snapshot from the most recently successful
	// catalogue pull.  It deliberately remains present even when empty: nil
	// means a legacy sidecar that has never been reconciled, while an empty map
	// means the platform has explicitly withdrawn every asset.
	ActiveAssets map[string]bool `json:"active_assets"`
	// CatalogueRevision is monotonic for one tenant catalogue. It prevents an
	// older Hub response that finishes late from reactivating assets withdrawn
	// by a newer response.
	CatalogueRevision int64  `json:"catalogue_revision"`
	CatalogueHash     string `json:"catalogue_hash"`
	CatalogueScope    string `json:"catalogue_scope"`
}
type managedIndustryExpertStore struct {
	mu     sync.Mutex
	pathFn func() string
}

func defaultManagedIndustryExpertStorePath() string {
	return filepath.Join(corelib.MaclawDataDir(), "experts", "managed-industry-experts.json")
}

var defaultManagedIndustryExpertStore = &managedIndustryExpertStore{pathFn: defaultManagedIndustryExpertStorePath}

func (s *managedIndustryExpertStore) loadLocked() (managedIndustryExpertStoreFile, error) {
	var f managedIndustryExpertStoreFile
	data, err := os.ReadFile(s.pathFn())
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return f, nil
	}
	return f, json.Unmarshal(data, &f)
}
func (s *managedIndustryExpertStore) writeLocked(f managedIndustryExpertStoreFile) error {
	if err := os.MkdirAll(filepath.Dir(s.pathFn()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.pathFn() + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err = os.Rename(tmp, s.pathFn()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *managedIndustryExpertStore) Save(item managedIndustryExpertInstall) error {
	return s.save(item, nil)
}

// SaveInactive records an installation whose authority could not be confirmed
// after the package was downloaded. Keeping its managed origin prevents the
// just-imported definition from briefly appearing as an editable personal
// expert while a withdrawn catalogue is being reconciled.
func (s *managedIndustryExpertStore) SaveInactive(item managedIndustryExpertInstall) error {
	inactive := false
	return s.save(item, &inactive)
}

func (s *managedIndustryExpertStore) save(item managedIndustryExpertInstall, activeOverride *bool) error {
	item.AssetID = strings.TrimSpace(item.AssetID)
	item.ListingID = strings.TrimSpace(item.ListingID)
	item.LocalExpertID = strings.TrimSpace(item.LocalExpertID)
	if item.AssetID == "" || item.ListingID == "" || item.LocalExpertID == "" {
		return fmt.Errorf("managed industry expert origin is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	// Do not let an installation started from an older catalogue reactivate an
	// asset that a later successful catalogue pull has withdrawn.  For legacy
	// sidecars (which have no snapshot yet), retain the old active-by-default
	// behavior until the first reconciliation.
	item.CatalogueScope = strings.TrimSpace(item.CatalogueScope)
	if item.CatalogueScope == "" {
		item.CatalogueScope = f.CatalogueScope
	}
	item.Active = f.ActiveAssets == nil || (item.CatalogueScope == f.CatalogueScope && f.ActiveAssets[item.AssetID])
	if activeOverride != nil {
		item.Active = *activeOverride
	}
	for i := range f.Installs {
		// Deduplicate only the same tenant-scoped asset or the same local
		// definition. Matching an asset across scopes would overwrite the prior
		// tenant's immutable origin and could make it disappear from policy checks.
		if f.Installs[i].sameOrigin(item) {
			f.Installs[i] = item
			return s.writeLocked(f)
		}
		if f.Installs[i].LocalExpertID == item.LocalExpertID {
			// One package ID can be selected by different tenant catalogues. Do
			// not overwrite the prior scope's origin; retain a second immutable
			// record so both policy histories remain protected.
			continue
		}
	}
	f.Installs = append(f.Installs, item)
	return s.writeLocked(f)
}

// ReconcileActiveAssets applies a successfully fetched catalogue. An entry
// removed by platform policy remains identified as managed, but cannot start a
// new expert session and cannot turn into an editable personal expert.
func (s *managedIndustryExpertStore) ReconcileActiveAssets(scope string, revision int64, contentHash string, assetIDs map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	scope = strings.TrimSpace(scope)
	contentHash = strings.TrimSpace(contentHash)
	scopeChanged := scope != f.CatalogueScope
	if !scopeChanged && revision < f.CatalogueRevision {
		return nil
	}
	if !scopeChanged && revision == f.CatalogueRevision && f.CatalogueHash != "" && contentHash != "" && f.CatalogueHash != contentHash {
		return fmt.Errorf("managed industry catalogue revision %d has conflicting content", revision)
	}
	activeAssets := make(map[string]bool, len(assetIDs))
	for assetID, active := range assetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID != "" && active {
			activeAssets[assetID] = true
		}
	}
	changed := scopeChanged || !sameManagedIndustryAssetSet(f.ActiveAssets, activeAssets) || revision != f.CatalogueRevision || contentHash != f.CatalogueHash
	f.ActiveAssets = activeAssets
	f.CatalogueRevision = revision
	f.CatalogueHash = contentHash
	f.CatalogueScope = scope
	for i := range f.Installs {
		// A pre-scope sidecar originated on this device before tenant scoping was
		// introduced. Associate it with the first verified catalogue, then never
		// let a later Hub/tenant change inherit it.
		if strings.TrimSpace(f.Installs[i].CatalogueScope) == "" {
			f.Installs[i].CatalogueScope = scope
			changed = true
		}
		active := f.Installs[i].CatalogueScope == scope && activeAssets[strings.TrimSpace(f.Installs[i].AssetID)]
		if f.Installs[i].Active != active {
			f.Installs[i].Active = active
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.writeLocked(f)
}

func sameManagedIndustryAssetSet(left, right map[string]bool) bool {
	if left == nil {
		return false
	}
	if len(left) != len(right) {
		return false
	}
	for assetID := range left {
		if !right[assetID] {
			return false
		}
	}
	return true
}
func (s *managedIndustryExpertStore) ByLocalID(id string) (managedIndustryExpertInstall, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return managedIndustryExpertInstall{}, false, err
	}
	for _, item := range f.Installs {
		if item.LocalExpertID == strings.TrimSpace(id) {
			return item, true, nil
		}
	}
	return managedIndustryExpertInstall{}, false, nil
}
func (s *managedIndustryExpertStore) ByAssetID(id string) (managedIndustryExpertInstall, bool, error) {
	return s.ByAssetIDInScope(id, "")
}

func (s *managedIndustryExpertStore) ByAssetIDInScope(id, scope string) (managedIndustryExpertInstall, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return managedIndustryExpertInstall{}, false, err
	}
	scope = strings.TrimSpace(scope)
	for _, item := range f.Installs {
		if item.AssetID == strings.TrimSpace(id) && (scope == "" || item.CatalogueScope == scope) {
			return item, true, nil
		}
	}
	return managedIndustryExpertInstall{}, false, nil
}

// IsActiveLocalIDInScope confirms that a rendered managed card still belongs
// to the current tenant scope. It prevents a completed background install from
// being treated as usable after a concurrent reassignment.
func (s *managedIndustryExpertStore) IsActiveLocalIDInScope(id, scope string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	id, scope = strings.TrimSpace(id), strings.TrimSpace(scope)
	for _, item := range f.Installs {
		if item.LocalExpertID == id && item.Active && item.CatalogueScope == scope {
			return true, nil
		}
	}
	return false, nil
}
func isManagedIndustryExpert(id string) bool {
	_, ok, err := defaultManagedIndustryExpertStore.ByLocalID(id)
	return err == nil && ok
}

func isActiveManagedIndustryExpert(id string) bool {
	// A stable package definition may legitimately be selected by more than one
	// historical tenant scope. It is usable when at least one retained managed
	// origin is active; a stale inactive origin must not shadow the current one.
	return defaultManagedIndustryExpertStore.HasActiveLocalID(id)
}

func (s *managedIndustryExpertStore) HasActiveLocalID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return false
	}
	id = strings.TrimSpace(id)
	for _, item := range f.Installs {
		if item.LocalExpertID == id && item.Active {
			return true
		}
	}
	return false
}
