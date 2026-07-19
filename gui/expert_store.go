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
	Experts    []ExpertDefinition `json:"experts"`
	DeletedIDs map[string]string  `json:"deleted_ids,omitempty"`
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
		f.DeletedIDs[id] = time.Now().UTC().Format(time.RFC3339)
	}
	if !removed && !tombstone {
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
	merged, changedIDs, needsSave := mergeExpertsForSync(f.Experts, hubItems, f.DeletedIDs)
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
