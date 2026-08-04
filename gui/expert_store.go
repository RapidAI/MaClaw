package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Expert store: local-first JSON storage for AI expert definitions.
//
// Layout: <MaclawDataDir>/experts/experts.json
//
//	{"experts":[...], "deleted_ids":{"<id>":"RFC3339"}}
//
// deleted_ids holds tombstones so a Hub pull cannot resurrect locally deleted
// experts. Local file is the source of truth; Hub is a sync replica.
// ---------------------------------------------------------------------------

// ExpertDefinition is the expert card contract shared with the frontend and Hub.
type ExpertDefinition struct {
	ID           string   `json:"id"`            // builtin: "builtin-*"; user: "expert-<ts>-<rand>"
	Name         string   `json:"name"`          // display name
	Description  string   `json:"description"`   // card blurb
	Icon         string   `json:"icon"`          // single emoji
	SystemPrompt string   `json:"system_prompt"` // persona injected as role description
	Tools        []string `json:"tools"`         // tool allow-list; empty = all tools
	Skills       []string `json:"skills"`        // skill allow-list; empty = all active skills
	Builtin      bool     `json:"builtin"`       // true only for in-binary definitions
	CreatedAt    string   `json:"created_at"`    // RFC3339
	UpdatedAt    string   `json:"updated_at"`    // RFC3339, last-writer-wins key
}

// expertStoreFile is the on-disk JSON shape.
type expertStoreFile struct {
	Experts           []ExpertDefinition `json:"experts"`
	DeletedIDs        map[string]string  `json:"deleted_ids,omitempty"`
	PendingHubUploads map[string]bool    `json:"pending_hub_uploads,omitempty"`
	PendingHubDeletes map[string]string  `json:"pending_hub_deletes,omitempty"`
	// LocalOnlyIDs keeps device-local installations (currently Expert Market
	// downloads) out of Hub reconciliation.  Unlike a deletion tombstone, the
	// marker intentionally survives an uninstall so a stale cloud copy cannot
	// make an uninstalled market expert reappear on this device.
	LocalOnlyIDs map[string]bool `json:"local_only_ids,omitempty"`
	// MarketInstallIDs identifies definitions acquired through the Expert
	// Market. Package imports use the same stable pkgexp-* IDs, so the prefix
	// alone is not sufficient to decide whether the marketplace may offer an
	// uninstall action.
	MarketInstallIDs map[string]bool `json:"market_install_ids,omitempty"`
}

// expertStore provides concurrency-safe, atomic access to experts.json.
type expertStore struct {
	mu     sync.Mutex
	pathFn func() string
}

// newExpertStore returns a store rooted at a fixed path (tests).
func newExpertStore(path string) *expertStore {
	return &expertStore{pathFn: func() string { return path }}
}

// defaultExpertStorePath resolves the live store path. It is evaluated lazily
// (not at package init) so a configured data_dir is honored after config load.
func defaultExpertStorePath() string {
	return filepath.Join(corelib.MaclawDataDir(), "experts", "experts.json")
}

// defaultExpertStore is the process-wide store used by Wails bindings and the
// session policy hooks.
var defaultExpertStore = &expertStore{pathFn: defaultExpertStorePath}

func (s *expertStore) path() string { return s.pathFn() }

// loadLocked reads the file; a missing file yields an empty store (not an error).
func (s *expertStore) loadLocked() (expertStoreFile, error) {
	var f expertStoreFile
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return expertStoreFile{}, nil
		}
		return f, fmt.Errorf("read experts store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return expertStoreFile{}, nil
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return expertStoreFile{}, fmt.Errorf("parse experts store: %w", err)
	}
	return f, nil
}

// writeLocked persists atomically: write tmp file in the same dir, then rename.
func (s *expertStore) writeLocked(f expertStoreFile) error {
	path := s.path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create experts dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write experts store tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename experts store: %w", err)
	}
	return nil
}

// List returns the stored (user) experts and the tombstone map.
func (s *expertStore) List() ([]ExpertDefinition, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, nil, err
	}
	return f.Experts, f.DeletedIDs, nil
}

// ListForHubSync returns the local expert snapshot and synchronization state
// under one lock. Pending operations survive restarts so offline changes can
// be retried when Hub becomes reachable again.
func (s *expertStore) ListForHubSync() ([]ExpertDefinition, map[string]string, map[string]bool, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	local := make([]ExpertDefinition, 0, len(f.Experts))
	for _, expert := range f.Experts {
		if !f.LocalOnlyIDs[expert.ID] {
			local = append(local, expert)
		}
	}
	return local, f.DeletedIDs, f.PendingHubUploads, f.PendingHubDeletes, nil
}

// MarkPendingHubUpload records a custom expert whose Hub upload must be
// retried. It is intentionally persisted so an offline import survives a
// restart and syncs once Hub becomes reachable.
func (s *expertStore) MarkPendingHubUpload(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	if f.PendingHubUploads != nil && f.PendingHubUploads[id] {
		return nil
	}
	if f.PendingHubUploads == nil {
		f.PendingHubUploads = make(map[string]bool)
	}
	f.PendingHubUploads[id] = true
	return s.writeLocked(f)
}

// ClearPendingHubUploadIfCurrent removes the retry marker only when the
// acknowledged version is still the current local version. A stale network
// response must never acknowledge away a newer edit made while it was in
// flight.
func (s *expertStore) ClearPendingHubUploadIfCurrent(id, updatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	if f.PendingHubUploads == nil || !f.PendingHubUploads[id] {
		return nil
	}
	for _, expert := range f.Experts {
		if expert.ID == id && expert.UpdatedAt == updatedAt {
			delete(f.PendingHubUploads, id)
			return s.writeLocked(f)
		}
	}
	// The expert was replaced or deleted after this request began; leave the
	// marker in place so the newer local operation is reconciled instead.
	return nil
}

// PendingHubUploadIsCurrent reports whether a specific local version still
// needs uploading. It lets a queued sync skip an obsolete request after a
// newer edit has already replaced it locally.
func (s *expertStore) PendingHubUploadIsCurrent(id, updatedAt string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil || f.PendingHubUploads == nil || !f.PendingHubUploads[id] {
		return false
	}
	for _, expert := range f.Experts {
		if expert.ID == id {
			return expert.UpdatedAt == updatedAt
		}
	}
	return false
}

// MarkPendingHubDelete records a custom-expert deletion that Hub has not yet
// confirmed. This is paired with the local tombstone, which prevents a later
// Hub pull from restoring the deleted expert locally.
func (s *expertStore) MarkPendingHubDelete(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return "", err
	}
	deletedAt := f.DeletedIDs[id]
	if deletedAt == "" {
		return "", fmt.Errorf("pending Hub delete requires a local tombstone")
	}
	if f.PendingHubDeletes != nil && f.PendingHubDeletes[id] == deletedAt {
		return deletedAt, nil
	}
	if f.PendingHubDeletes == nil {
		f.PendingHubDeletes = make(map[string]string)
	}
	f.PendingHubDeletes[id] = deletedAt
	return deletedAt, s.writeLocked(f)
}

// ClearPendingHubDelete removes the retry marker after Hub confirms deletion
// (including a 404, which means the target is already absent remotely).
func (s *expertStore) ClearPendingHubDeleteIfCurrent(id, deletedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	if f.PendingHubDeletes == nil || f.PendingHubDeletes[id] != deletedAt {
		return nil
	}
	delete(f.PendingHubDeletes, id)
	return s.writeLocked(f)
}

// PendingHubDeleteIsCurrent reports whether an id is still locally deleted
// and waiting for Hub confirmation. A queued remote delete must be skipped if
// the user recreated the expert before the request left this device.
func (s *expertStore) PendingHubDeleteIsCurrent(id, deletedAt string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil || f.PendingHubDeletes == nil || f.PendingHubDeletes[id] != deletedAt {
		return false
	}
	if f.DeletedIDs[id] != deletedAt {
		return false
	}
	for _, expert := range f.Experts {
		if expert.ID == id {
			return false
		}
	}
	return true
}

// Get returns one stored expert by id.
func (s *expertStore) Get(id string) (ExpertDefinition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return ExpertDefinition{}, false, err
	}
	for _, e := range f.Experts {
		if e.ID == id {
			return e, true, nil
		}
	}
	return ExpertDefinition{}, false, nil
}

// Save upserts by id and clears any tombstone for that id (a re-saved expert
// may sync back from Hub legitimately).
func (s *expertStore) Save(def ExpertDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return fmt.Errorf("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.Experts {
		if f.Experts[i].ID == def.ID {
			f.Experts[i] = def
			replaced = true
			break
		}
	}
	if !replaced {
		f.Experts = append(f.Experts, def)
	}
	if f.DeletedIDs != nil {
		delete(f.DeletedIDs, def.ID)
	}
	if f.PendingHubDeletes != nil {
		delete(f.PendingHubDeletes, def.ID)
	}
	return s.writeLocked(f)
}

// SaveLocalOnly upserts a device-local expert. The local-only marker is
// persisted alongside the definition, so its Hub exclusion is atomic with the
// install/update. Existing markers are deliberately retained by Save as well:
// editing a market installation must not silently turn it into a Hub-synced
// expert.
func (s *expertStore) SaveLocalOnly(def ExpertDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return fmt.Errorf("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.Experts {
		if f.Experts[i].ID == def.ID {
			f.Experts[i] = def
			replaced = true
			break
		}
	}
	if !replaced {
		f.Experts = append(f.Experts, def)
	}
	if f.DeletedIDs != nil {
		delete(f.DeletedIDs, def.ID)
	}
	if f.PendingHubDeletes != nil {
		delete(f.PendingHubDeletes, def.ID)
	}
	if f.PendingHubUploads != nil {
		delete(f.PendingHubUploads, def.ID)
	}
	if f.LocalOnlyIDs == nil {
		f.LocalOnlyIDs = make(map[string]bool)
	}
	f.LocalOnlyIDs[def.ID] = true
	return s.writeLocked(f)
}

// IsLocalOnly reports whether an id belongs to a device-local installation.
func (s *expertStore) IsLocalOnly(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	return f.LocalOnlyIDs[id], nil
}

// MarkLocalOnly preserves a device-local install marker even after its expert
// definition has been removed. This prevents an older market installation
// which was once synced by a previous app version from being restored by a
// later Hub pull.
func (s *expertStore) MarkLocalOnly(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	if f.LocalOnlyIDs == nil {
		f.LocalOnlyIDs = make(map[string]bool)
	}
	if f.LocalOnlyIDs[id] {
		return nil
	}
	f.LocalOnlyIDs[id] = true
	if f.PendingHubUploads != nil {
		delete(f.PendingHubUploads, id)
	}
	return s.writeLocked(f)
}

// SaveMarketInstall atomically records a device-local Expert Market install.
// This keeps the marketplace origin separate from ordinary ZIP package imports
// which may share the same pkgexp-* package identity.
func (s *expertStore) SaveMarketInstall(def ExpertDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return fmt.Errorf("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.Experts {
		if f.Experts[i].ID == def.ID {
			f.Experts[i] = def
			replaced = true
			break
		}
	}
	if !replaced {
		f.Experts = append(f.Experts, def)
	}
	if f.DeletedIDs != nil {
		delete(f.DeletedIDs, def.ID)
	}
	if f.PendingHubDeletes != nil {
		delete(f.PendingHubDeletes, def.ID)
	}
	if f.PendingHubUploads != nil {
		delete(f.PendingHubUploads, def.ID)
	}
	if f.LocalOnlyIDs == nil {
		f.LocalOnlyIDs = make(map[string]bool)
	}
	if f.MarketInstallIDs == nil {
		f.MarketInstallIDs = make(map[string]bool)
	}
	f.LocalOnlyIDs[def.ID] = true
	f.MarketInstallIDs[def.ID] = true
	return s.writeLocked(f)
}

// IsMarketInstall reports whether the definition was installed through the
// Expert Market, rather than merely imported from a portable ZIP package.
func (s *expertStore) IsMarketInstall(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	return f.MarketInstallIDs[id], nil
}

// ListMarketInstallIDs returns the currently installed Expert Market records
// from one consistent store snapshot. MarketInstallIDs deliberately survives
// an uninstall to preserve the local-only anti-resurrection marker, so stale
// marker-only IDs are excluded here.
func (s *expertStore) ListMarketInstallIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	installed := make(map[string]bool)
	for _, expert := range f.Experts {
		id := strings.TrimSpace(expert.ID)
		if id != "" && f.MarketInstallIDs[id] {
			installed[id] = true
		}
	}
	return installed, nil
}

// MarkMarketInstall promotes an existing package definition to an Expert
// Market install without changing its content. It is used for an idempotent
// market download when the exact package was previously imported locally.
func (s *expertStore) MarkMarketInstall(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	found := false
	for _, expert := range f.Experts {
		if expert.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("expert was not found")
	}
	if f.LocalOnlyIDs == nil {
		f.LocalOnlyIDs = make(map[string]bool)
	}
	if f.MarketInstallIDs == nil {
		f.MarketInstallIDs = make(map[string]bool)
	}
	if f.LocalOnlyIDs[id] && f.MarketInstallIDs[id] {
		return nil
	}
	f.LocalOnlyIDs[id] = true
	f.MarketInstallIDs[id] = true
	if f.PendingHubUploads != nil {
		delete(f.PendingHubUploads, id)
	}
	return s.writeLocked(f)
}

// Delete removes an expert. With tombstone=true the deletion is recorded so a
// later Hub pull cannot resurrect the item.
func (s *expertStore) Delete(id string, tombstone bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	kept := f.Experts[:0]
	removed := false
	for _, e := range f.Experts {
		if e.ID == id {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	f.Experts = kept
	if tombstone {
		if f.DeletedIDs == nil {
			f.DeletedIDs = map[string]string{}
		}
		// Nanosecond precision distinguishes rapid delete → recreate → delete
		// sequences so a late acknowledgement cannot clear a newer tombstone.
		f.DeletedIDs[id] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	pendingCleared := false
	if f.PendingHubUploads != nil && f.PendingHubUploads[id] {
		delete(f.PendingHubUploads, id)
		pendingCleared = true
	}
	if f.PendingHubDeletes != nil {
		delete(f.PendingHubDeletes, id)
	}
	if !removed && !tombstone && !pendingCleared {
		return nil // nothing changed; skip disk write
	}
	return s.writeLocked(f)
}

// MergeAndSaveFromHub merges Hub items into the store under a single lock:
// re-reads the current file, applies union-by-id + updated_at LWW (tombstones
// drop entries, builtin ids are rejected), and persists atomically in one
// write. This closes the TOCTOU window of a separate List→merge→SaveAll
// sequence where a concurrent local Save/Delete could be clobbered.
// Returns the ids whose local entry changed (for cache invalidation).
func (s *expertStore) MergeAndSaveFromHub(hubItems []ExpertDefinition) (changedIDs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	merged, changedIDs, needsSave := mergeExpertsForSyncWithLocalOnly(f.Experts, hubItems, f.DeletedIDs, f.LocalOnlyIDs)
	if !needsSave {
		return nil, nil
	}
	f.Experts = merged
	if err := s.writeLocked(f); err != nil {
		return nil, err
	}
	return changedIDs, nil
}

// newExpertID generates a user expert id: expert-<unixnano>-<rand8>.
func newExpertID() string {
	return fmt.Sprintf("expert-%d-%s", time.Now().UnixNano(), uuid.NewString()[:8])
}

// expertUpdatedAtAfter reports whether timestamp a is strictly later than b.
// Unparseable timestamps count as "not newer": ties and invalid values keep
// the local copy (fail-safe toward local-first storage).
func expertUpdatedAtAfter(a, b string) bool {
	ta, ea := time.Parse(time.RFC3339, strings.TrimSpace(a))
	tb, eb := time.Parse(time.RFC3339, strings.TrimSpace(b))
	switch {
	case ea != nil && eb != nil:
		return false // both unparseable: tie → keep local
	case ea != nil:
		return false // a invalid, b valid → b wins
	case eb != nil:
		return true
	default:
		return ta.After(tb)
	}
}

// mergeExpertsForSync merges local and Hub expert lists into one:
//   - union by id;
//   - last-writer-wins on updated_at (ties/unparseable keep local);
//   - any id present in tombstones is dropped (prevents Hub resurrection);
//   - Hub items whose id collides with a builtin expert are dropped (builtins
//     ship in-binary and never sync).
//
// changedIDs lists ids whose local entry changed (hub added/updated, or local
// dropped by tombstone) — callers use it for cache invalidation.
// localNeedsSave is true when the merged result differs from the local list
// and should be written back.
func mergeExpertsForSync(local, hub []ExpertDefinition, tombstones map[string]string) (merged []ExpertDefinition, changedIDs []string, localNeedsSave bool) {
	return mergeExpertsForSyncWithLocalOnly(local, hub, tombstones, nil)
}

// mergeExpertsForSyncWithLocalOnly applies the normal LWW merge while refusing
// Hub records that this device has explicitly marked as local-only. This is
// distinct from tombstones: the Hub record remains valid for other devices,
// but must not reinstall itself after a local market uninstall.
func mergeExpertsForSyncWithLocalOnly(local, hub []ExpertDefinition, tombstones map[string]string, localOnly map[string]bool) (merged []ExpertDefinition, changedIDs []string, localNeedsSave bool) {
	byID := make(map[string]ExpertDefinition, len(local)+len(hub))
	order := make([]string, 0, len(local)+len(hub))
	for _, e := range local {
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		if _, dup := byID[e.ID]; !dup {
			order = append(order, e.ID)
		}
		byID[e.ID] = e
	}
	changed := false
	changedSeen := map[string]bool{}
	markChanged := func(id string) {
		changed = true
		if !changedSeen[id] {
			changedSeen[id] = true
			changedIDs = append(changedIDs, id)
		}
	}
	for _, h := range hub {
		if strings.TrimSpace(h.ID) == "" {
			continue
		}
		if builtinExpertByID(h.ID) != nil {
			continue // builtin ids are never accepted from Hub
		}
		if localOnly[h.ID] {
			continue // device-local market install; never restore it from Hub
		}
		if _, dead := tombstones[h.ID]; dead {
			continue
		}
		l, ok := byID[h.ID]
		if !ok {
			byID[h.ID] = h
			order = append(order, h.ID)
			markChanged(h.ID)
			continue
		}
		if expertUpdatedAtAfter(h.UpdatedAt, l.UpdatedAt) {
			byID[h.ID] = h
			markChanged(h.ID)
		}
	}
	merged = make([]ExpertDefinition, 0, len(byID))
	for _, id := range order {
		e := byID[id]
		if _, dead := tombstones[id]; dead {
			markChanged(id) // local item dropped by tombstone
			continue
		}
		merged = append(merged, e)
	}
	// Stable presentation: oldest first, id as tiebreak.
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].CreatedAt != merged[j].CreatedAt {
			return merged[i].CreatedAt < merged[j].CreatedAt
		}
		return merged[i].ID < merged[j].ID
	})
	return merged, changedIDs, changed
}
