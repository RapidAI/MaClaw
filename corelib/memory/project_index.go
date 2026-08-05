package memory

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// ProjectRecord represents a single project in the index.
// It aggregates information from multiple memory entries that share
// the same project path.
type ProjectRecord struct {
	// ProjectPath is the canonical absolute path of the project directory.
	// This is the primary key of the index.
	ProjectPath string `json:"project_path"`

	// Name is a human-readable project name, extracted from the first
	// task_artifact or project_knowledge entry's content (first line).
	Name string `json:"name"`

	// WorkflowType is the workflow template type if a workflow was used
	// (e.g. "coding", "product_design"). Empty if no workflow.
	WorkflowType string `json:"workflow_type,omitempty"`

	// Tags is the union of all tags from entries belonging to this project.
	Tags []string `json:"tags,omitempty"`

	// EntryCount is the number of memory entries associated with this project.
	EntryCount int `json:"entry_count"`

	// LastActivity is the most recent UpdatedAt across all entries.
	LastActivity time.Time `json:"last_activity"`

	// CreatedAt is the earliest creation time across all entries belonging to
	// this project. Unlike LastActivity, it is stable when a task is reopened,
	// indexed, or receives derived memory updates.
	CreatedAt time.Time `json:"created_at"`

	// Preview is a short content preview (~150 rune) from the most recently
	// updated entry.
	Preview string `json:"preview,omitempty"`

	// Categories lists the distinct memory categories present for this project.
	Categories []Category `json:"categories,omitempty"`

	// HasOutput marks records backed by a tangible user-visible output.
	HasOutput bool `json:"has_output,omitempty"`

	// Archived indicates whether this project has been archived.
	// Archived projects are hidden by default but can be found via search.
	Archived bool `json:"archived,omitempty"`

	// seenIDs tracks entry IDs already counted to prevent double-counting
	// when the same entry is re-indexed (e.g. after tag merge in dedup paths).
	seenIDs            map[string]bool `json:"-"`
	nameExplicit       bool            `json:"-"` // true if Name was set from Entry.Title (not content heuristic)
	nameFromOutput     bool            `json:"-"`
	outputLastActivity time.Time       `json:"-"`
	outputPreview      string          `json:"-"`
}

// ProjectIndex maintains a lightweight in-memory index that maps
// project paths to aggregated project metadata. It is rebuilt from
// memory entries on startup and incrementally updated on each Save.
//
// Thread-safe: all methods acquire the internal mutex.
type ProjectIndex struct {
	mu        sync.RWMutex
	records   map[string]*ProjectRecord // projectPath → record
	prefs     map[string]*TaskPref      // projectPath → user preferences (persisted)
	prefsPath string                    // path to task_prefs.json

	// OnChanged is called (outside pi.mu but possibly inside Store.mu)
	// when IndexEntry produces a meaningful change to the index — a new
	// project record is created, or an existing record's LastActivity is
	// updated. Callers can use this to notify the UI.
	//
	// The callback receives the affected project path. It must be
	// non-blocking (e.g. async event emission) since it may be called
	// while Store.mu is held.
	//
	// Not called during Rebuild (bulk initialization) to avoid a storm of
	// notifications on startup.
	OnChanged func(projectPath string)
}

// TaskPref stores user-defined preferences for a task in the recent tasks list.
type TaskPref struct {
	Name     string `json:"name,omitempty"`     // custom display name (empty = auto-generated)
	Pinned   bool   `json:"pinned,omitempty"`   // pinned to top of list
	Hidden   bool   `json:"hidden,omitempty"`   // hidden from list (soft delete)
	Archived bool   `json:"archived,omitempty"` // archived (hidden + read-only, experience preserved)
}

// NewProjectIndex creates an empty ProjectIndex.
// dataDir is the directory where task_prefs.json is persisted (e.g. ~/.maclaw/data/).
// Pass empty string to disable preference persistence.
func NewProjectIndex(dataDir ...string) *ProjectIndex {
	pi := &ProjectIndex{
		records: make(map[string]*ProjectRecord),
		prefs:   make(map[string]*TaskPref),
	}
	if len(dataDir) > 0 && dataDir[0] != "" {
		pi.prefsPath = filepath.Join(dataDir[0], "task_prefs.json")
		pi.loadPrefs()
	}
	return pi
}

// projectCategories are the memory categories that contribute to the project index.
var projectCategories = map[Category]bool{
	CategoryProjectKnowledge:    true,
	CategoryTaskArtifact:        true,
	CategoryConversationSummary: true,
	CategoryProject:             true,
	CategoryReference:           true,
}

// Rebuild reconstructs the entire index from a snapshot of memory entries.
// Called once on Store initialization.
func (pi *ProjectIndex) Rebuild(entries []Entry) {
	pi.mu.Lock()
	defer pi.mu.Unlock()

	pi.records = make(map[string]*ProjectRecord)
	for i := range entries {
		_, _ = pi.indexEntryLocked(&entries[i])
	}
}

// IndexEntry incrementally updates the index for a single entry.
// Called after each Save/SaveWithContext.
func (pi *ProjectIndex) IndexEntry(e *Entry) {
	pi.mu.Lock()
	changed, projPath := pi.indexEntryLocked(e)
	cb := pi.OnChanged
	pi.mu.Unlock()

	// Fire callback outside the lock to avoid deadlocks with consumers
	// that call back into ProjectIndex (e.g. SearchProjects).
	if changed && cb != nil {
		cb(projPath)
	}
}

// indexEntryLocked updates the index for a single entry (caller holds mu.Lock).
// Safe to call multiple times for the same entry (idempotent for EntryCount
// via entry ID tracking).
// Returns (changed, projectPath): changed is true when a new record was
// created or an existing record's LastActivity was updated.
func (pi *ProjectIndex) indexEntryLocked(e *Entry) (bool, string) {
	if !projectCategories[e.Category] && !projectCategories[MapToCanonical(e.Category)] {
		return false, ""
	}
	if e.Status == StatusDormant || e.Status == StatusSuperseded {
		return false, ""
	}

	// Determine project path from entry metadata.
	isOutput := isRecentTaskOutputEntry(e)
	projPath := inferProjectPath(e)
	if projPath == "" {
		return false, ""
	}

	changed := false
	rec, ok := pi.records[projPath]
	if !ok {
		rec = &ProjectRecord{
			ProjectPath: projPath,
			seenIDs:     make(map[string]bool),
		}
		pi.records[projPath] = rec
		changed = true // new project record
	}

	// Deduplicate entry count: only increment for entries we haven't seen.
	if rec.seenIDs == nil {
		rec.seenIDs = make(map[string]bool)
	}
	if e.ID != "" && !rec.seenIDs[e.ID] {
		rec.seenIDs[e.ID] = true
		rec.EntryCount++
	}

	// Update last activity.
	if e.UpdatedAt.After(rec.LastActivity) {
		rec.LastActivity = e.UpdatedAt
		// Update preview from the most recent entry.
		rec.Preview = truncateRunes(firstMeaningfulLine(e.Content), 150)
		changed = true // activity timestamp advanced
	}

	// Keep a stable project creation time. Older persisted entries may not have
	// CreatedAt populated, so use their update time as the best available
	// historical fallback rather than leaving the task without a display time.
	entryCreatedAt := e.CreatedAt
	if entryCreatedAt.IsZero() {
		entryCreatedAt = e.UpdatedAt
	}
	if !entryCreatedAt.IsZero() && (rec.CreatedAt.IsZero() || entryCreatedAt.Before(rec.CreatedAt)) {
		rec.CreatedAt = entryCreatedAt
	}

	// Merge tags.
	rec.Tags = mergeTagsDedup(rec.Tags, e.Tags)

	// Track categories.
	rec.Categories = addCategoryDedup(rec.Categories, e.Category)
	outputBecameLatest := false
	if isOutput {
		if !rec.HasOutput {
			changed = true
		}
		rec.HasOutput = true
		if e.UpdatedAt.After(rec.outputLastActivity) {
			rec.outputLastActivity = e.UpdatedAt
			rec.outputPreview = truncateRunes(outputPreviewLine(e.Content), 150)
			outputBecameLatest = true
			changed = true
		}
	}

	// Extract name.
	// Priority: Entry.Title (explicit, set by writer) > content-extracted title (heuristic).
	// Entry.Title is the mechanism-level fix: writers know what the entry is about
	// and set a clean title at creation time. Content extraction is the fallback
	// for legacy entries that don't have Title set.
	candidateName := e.Title
	candidateIsExplicit := candidateName != ""
	if candidateName == "" {
		candidateName = extractTitle(e.Content)
	}
	if candidateName != "" {
		better := false
		switch {
		case rec.Name == "":
			better = true
		case isOutput && !rec.nameFromOutput:
			better = true
		case isOutput && outputBecameLatest && candidateIsExplicit:
			better = true
		case candidateIsExplicit && !rec.nameExplicit:
			// Explicit Title wins over any content-extracted name.
			better = true
		case !candidateIsExplicit && !rec.nameExplicit && isHeadingTitle(candidateName) && !isHeadingTitle(rec.Name):
			// Among content-extracted names: heading beats first-line fallback.
			better = true
			// When both are explicit (or both are heuristic and same quality): keep first.
		}
		if better {
			rec.Name = candidateName
			rec.nameExplicit = candidateIsExplicit
			rec.nameFromOutput = isOutput
		}
	}

	// Extract workflow type from tags.
	if rec.WorkflowType == "" {
		for _, tag := range e.Tags {
			if strings.HasPrefix(tag, "workflow:") {
				rec.WorkflowType = strings.TrimPrefix(tag, "workflow:")
				break
			}
			if strings.HasPrefix(tag, "phase:") {
				// phase tags imply a workflow exists; type may be in another tag
				continue
			}
		}
	}

	return changed, projPath
}

// Search returns project records matching the query, sorted by relevance.
// Uses simple substring + tag matching for speed (<1ms).
func (pi *ProjectIndex) Search(query string, limit int) []ProjectRecord {
	return pi.SearchMatching(query, limit, nil)
}

// SearchMatching returns visible output-backed records that match query and
// predicate, ranked using the same relevance rules as Search. The predicate is
// applied before the result limit so filtered consumers do not lose matching
// records to unrelated results.
func (pi *ProjectIndex) SearchMatching(query string, limit int, predicate func(ProjectRecord) bool) []ProjectRecord {
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)

	pi.mu.RLock()
	defer pi.mu.RUnlock()

	if query == "" {
		// No query: return all projects sorted by last activity (most recent first).
		if predicate == nil {
			return pi.allSortedByActivityLocked(limit)
		}
		return pi.listRecentMatchingLocked(limit, predicate)
	}

	queryLower := strings.ToLower(query)
	queryTokens := strings.Fields(queryLower)

	type scored struct {
		rec   ProjectRecord
		score float64
	}
	var results []scored
	includeArchived := queryWantsArchived(queryLower)

	for _, rec := range pi.records {
		if !rec.HasOutput {
			continue
		}
		if p, ok := pi.prefs[rec.ProjectPath]; ok {
			if p.Hidden {
				continue
			}
			if p.Archived && !includeArchived {
				continue
			}
		}
		clone := pi.outputRecordCloneLocked(rec)
		if predicate != nil && !predicate(clone) {
			continue
		}
		score := pi.scoreRecord(&clone, queryLower, queryTokens)
		if score > 0 {
			results = append(results, scored{rec: clone, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].rec.LastActivity.After(results[j].rec.LastActivity)
	})

	out := make([]ProjectRecord, 0, limit)
	for i := 0; i < len(results) && i < limit; i++ {
		out = append(out, results[i].rec)
	}
	return out
}

func queryWantsArchived(queryLower string) bool {
	return strings.Contains(queryLower, "archive") || strings.Contains(queryLower, "归档") || strings.Contains(queryLower, "歸檔")
}

// ListRecent returns the N most recently active projects.
func (pi *ProjectIndex) ListRecent(limit int) []ProjectRecord {
	if limit <= 0 {
		limit = 10
	}
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.allSortedByActivityLocked(limit)
}

// ListRecentMatching returns the newest visible records matching predicate.
// It applies the predicate before the limit, so callers can efficiently build
// filtered views without repeatedly broadening a recent-record query.
func (pi *ProjectIndex) ListRecentMatching(limit int, predicate func(ProjectRecord) bool) []ProjectRecord {
	if limit <= 0 {
		limit = 10
	}
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.listRecentMatchingLocked(limit, predicate)
}

// ListAllMatching returns every output-backed record that matches predicate,
// including hidden and archived records. It is intended for internal identity
// lookups where a soft-hidden record must still reserve its stable key.
func (pi *ProjectIndex) ListAllMatching(predicate func(ProjectRecord) bool) []ProjectRecord {
	pi.mu.RLock()
	defer pi.mu.RUnlock()

	matched := make([]ProjectRecord, 0, len(pi.records))
	for _, rec := range pi.records {
		if !rec.HasOutput {
			continue
		}
		clone := pi.outputRecordCloneLocked(rec)
		if predicate != nil && !predicate(clone) {
			continue
		}
		matched = append(matched, clone)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].LastActivity.After(matched[j].LastActivity)
	})
	return matched
}

func (pi *ProjectIndex) listRecentMatchingLocked(limit int, predicate func(ProjectRecord) bool) []ProjectRecord {
	matched := make([]ProjectRecord, 0, min(limit, len(pi.records)))
	for _, rec := range pi.records {
		if !rec.HasOutput {
			continue
		}
		if pref, ok := pi.prefs[rec.ProjectPath]; ok && (pref.Hidden || pref.Archived) {
			continue
		}
		clone := pi.outputRecordCloneLocked(rec)
		if predicate != nil && !predicate(clone) {
			continue
		}
		matched = append(matched, clone)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		iPinned := pi.prefs[matched[i].ProjectPath] != nil && pi.prefs[matched[i].ProjectPath].Pinned
		jPinned := pi.prefs[matched[j].ProjectPath] != nil && pi.prefs[matched[j].ProjectPath].Pinned
		if iPinned != jPinned {
			return iPinned
		}
		return matched[i].LastActivity.After(matched[j].LastActivity)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

// Count returns the number of indexed projects.
func (pi *ProjectIndex) Count() int {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return len(pi.records)
}

// Get returns the record for a specific project path, or nil.
// The path is normalized so both slash styles match the same record.
func (pi *ProjectIndex) Get(projectPath string) *ProjectRecord {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	key := normalizeProjectPath(toForwardSlash(projectPath))
	if rec, ok := pi.records[key]; ok {
		clone := *rec
		// Populate Archived from prefs.
		if p, ok := pi.prefs[projectPrefKey(rec.ProjectPath)]; ok && p.Archived {
			clone.Archived = true
		}
		return &clone
	}
	return nil
}

// ReplacePrefixedTags drops tags matching any of dropPrefixes on the project
// record, then appends addTags. Used when meta tags (e.g. remote_host:) must
// be replaced rather than unioned — IndexEntry only merge-appends tags.
func (pi *ProjectIndex) ReplacePrefixedTags(projectPath string, dropPrefixes []string, addTags []string) {
	if pi == nil || strings.TrimSpace(projectPath) == "" {
		return
	}
	pi.mu.Lock()
	defer pi.mu.Unlock()
	key := normalizeProjectPath(toForwardSlash(projectPath))
	rec, ok := pi.records[key]
	if !ok || rec == nil {
		return
	}
	shouldDrop := func(tag string) bool {
		for _, p := range dropPrefixes {
			if p != "" && strings.HasPrefix(tag, p) {
				return true
			}
		}
		return false
	}
	kept := make([]string, 0, len(rec.Tags)+len(addTags))
	seen := make(map[string]bool, len(rec.Tags)+len(addTags))
	for _, tag := range rec.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || shouldDrop(tag) || seen[tag] {
			continue
		}
		seen[tag] = true
		kept = append(kept, tag)
	}
	for _, tag := range addTags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		kept = append(kept, tag)
	}
	rec.Tags = kept
}

// --- Task preferences (rename / pin / hide) ---

// SetCustomName sets a user-defined display name for a task.
// Pass empty name to clear (revert to auto-generated).
func (pi *ProjectIndex) SetCustomName(projectPath, name string) {
	pi.mu.Lock()
	p := pi.getOrCreatePrefLocked(projectPath)
	p.Name = strings.TrimSpace(name)
	pi.cleanupPrefLocked(projectPath)
	pi.mu.Unlock()
	pi.savePrefs()
}

// CustomName returns the user-set custom name, or empty if none.
func (pi *ProjectIndex) CustomName(projectPath string) string {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if p, ok := pi.prefs[projectPrefKey(projectPath)]; ok {
		return p.Name
	}
	return ""
}

// GetDisplayName returns the custom name if set, otherwise the auto-generated name.
func (pi *ProjectIndex) GetDisplayName(projectPath string) string {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	key := projectPrefKey(projectPath)
	if p, ok := pi.prefs[key]; ok && p.Name != "" {
		return p.Name
	}
	if rec, ok := pi.records[key]; ok {
		return rec.Name
	}
	return ""
}

// SetPinned pins or unpins a task. Pinned tasks appear at the top of the list.
func (pi *ProjectIndex) SetPinned(projectPath string, pinned bool) {
	pi.mu.Lock()
	p := pi.getOrCreatePrefLocked(projectPath)
	p.Pinned = pinned
	pi.cleanupPrefLocked(projectPath)
	pi.mu.Unlock()
	pi.savePrefs()
}

// IsPinned returns whether a task is pinned.
func (pi *ProjectIndex) IsPinned(projectPath string) bool {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if p, ok := pi.prefs[projectPrefKey(projectPath)]; ok {
		return p.Pinned
	}
	return false
}

// SetHidden hides or unhides a task from the recent tasks list.
func (pi *ProjectIndex) SetHidden(projectPath string, hidden bool) {
	pi.mu.Lock()
	p := pi.getOrCreatePrefLocked(projectPath)
	p.Hidden = hidden
	pi.cleanupPrefLocked(projectPath)
	pi.mu.Unlock()
	pi.savePrefs()
}

// IsHidden returns whether a task is hidden.
func (pi *ProjectIndex) IsHidden(projectPath string) bool {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if p, ok := pi.prefs[projectPrefKey(projectPath)]; ok {
		return p.Hidden
	}
	return false
}

// SetArchived marks a task as archived or unarchived.
// Archived tasks are hidden by default (same as Hidden) but retain their
// experience in long-term memory.
func (pi *ProjectIndex) SetArchived(projectPath string, archived bool) {
	pi.mu.Lock()
	p := pi.getOrCreatePrefLocked(projectPath)
	p.Archived = archived
	pi.cleanupPrefLocked(projectPath)
	pi.mu.Unlock()
	pi.savePrefs()
}

// ClearTaskPrefs removes every user preference stored for a task. It is used
// when a task is permanently deleted, so a later task with the same path does
// not inherit a previous name, pin, hidden, or archive state.
func (pi *ProjectIndex) ClearTaskPrefs(projectPath string) {
	if pi == nil || strings.TrimSpace(projectPath) == "" {
		return
	}
	pi.mu.Lock()
	delete(pi.prefs, projectPrefKey(projectPath))
	pi.mu.Unlock()
	pi.savePrefs()
}

// IsArchived returns whether a task is archived.
func (pi *ProjectIndex) IsArchived(projectPath string) bool {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if p, ok := pi.prefs[projectPrefKey(projectPath)]; ok {
		return p.Archived
	}
	return false
}

// getOrCreatePrefLocked returns the pref for a path, creating if needed.
// Caller must hold pi.mu.Lock.
func (pi *ProjectIndex) getOrCreatePrefLocked(projectPath string) *TaskPref {
	projectPath = projectPrefKey(projectPath)
	p, ok := pi.prefs[projectPath]
	if !ok {
		p = &TaskPref{}
		pi.prefs[projectPath] = p
	}
	return p
}

// cleanupPrefLocked removes the pref entry if all fields are zero-value,
// keeping the JSON file clean.
func (pi *ProjectIndex) cleanupPrefLocked(projectPath string) {
	projectPath = projectPrefKey(projectPath)
	if p, ok := pi.prefs[projectPath]; ok {
		if p.Name == "" && !p.Pinned && !p.Hidden && !p.Archived {
			delete(pi.prefs, projectPath)
		}
	}
}

func projectPrefKey(projectPath string) string {
	return normalizeProjectPath(toForwardSlash(strings.TrimSpace(projectPath)))
}

func normalizeTaskPrefs(input map[string]*TaskPref) map[string]*TaskPref {
	out := make(map[string]*TaskPref, len(input))
	for path, pref := range input {
		if pref == nil {
			continue
		}
		key := projectPrefKey(path)
		if key == "" {
			continue
		}
		merged := out[key]
		if merged == nil {
			copyPref := *pref
			out[key] = &copyPref
			continue
		}
		if merged.Name == "" {
			merged.Name = pref.Name
		}
		merged.Pinned = merged.Pinned || pref.Pinned
		merged.Hidden = merged.Hidden || pref.Hidden
		merged.Archived = merged.Archived || pref.Archived
	}
	return out
}

type persistedPrefs struct {
	Prefs map[string]*TaskPref `json:"prefs"`
}

func (pi *ProjectIndex) loadPrefs() {
	if pi.prefsPath == "" {
		return
	}
	data, err := os.ReadFile(pi.prefsPath)
	if err != nil {
		// Try migrating from old task_names.json format.
		pi.migrateFromTaskNames()
		return
	}
	var pp persistedPrefs
	if json.Unmarshal(data, &pp) == nil && pp.Prefs != nil {
		pi.prefs = normalizeTaskPrefs(pp.Prefs)
	}
}

// migrateFromTaskNames migrates the old task_names.json (map[string]string)
// to the new task_prefs.json format. One-time migration on first load.
func (pi *ProjectIndex) migrateFromTaskNames() {
	if pi.prefsPath == "" {
		return
	}
	oldPath := filepath.Join(filepath.Dir(pi.prefsPath), "task_names.json")
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return
	}
	var names map[string]string
	if json.Unmarshal(data, &names) != nil || len(names) == 0 {
		return
	}
	for path, name := range names {
		pi.prefs[projectPrefKey(path)] = &TaskPref{Name: name}
	}
	pi.savePrefs()
	// Remove old file after successful migration.
	_ = os.Remove(oldPath)
	log.Printf("[ProjectIndex] migrated %d custom names from task_names.json to task_prefs.json", len(names))
}

func (pi *ProjectIndex) savePrefs() {
	if pi.prefsPath == "" {
		return
	}
	pi.mu.RLock()
	pp := persistedPrefs{Prefs: pi.prefs}
	data, err := json.MarshalIndent(pp, "", "  ")
	pi.mu.RUnlock()
	if err != nil {
		log.Printf("[ProjectIndex] savePrefs marshal failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(pi.prefsPath), 0o755); err != nil {
		log.Printf("[ProjectIndex] savePrefs mkdir failed: %v", err)
		return
	}
	if err := os.WriteFile(pi.prefsPath, data, 0o644); err != nil {
		log.Printf("[ProjectIndex] savePrefs write failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (pi *ProjectIndex) allSortedByActivityLocked(limit int) []ProjectRecord {
	all := make([]ProjectRecord, 0, len(pi.records))
	for _, rec := range pi.records {
		// Skip hidden and archived tasks.
		if p, ok := pi.prefs[rec.ProjectPath]; ok && (p.Hidden || p.Archived) {
			continue
		}
		if !rec.HasOutput {
			continue
		}
		all = append(all, pi.outputRecordCloneLocked(rec))
	}
	// Sort: pinned first (by activity within pinned group), then unpinned by activity.
	sort.SliceStable(all, func(i, j int) bool {
		iPinned := pi.prefs[all[i].ProjectPath] != nil && pi.prefs[all[i].ProjectPath].Pinned
		jPinned := pi.prefs[all[j].ProjectPath] != nil && pi.prefs[all[j].ProjectPath].Pinned
		if iPinned != jPinned {
			return iPinned // pinned items first
		}
		return all[i].LastActivity.After(all[j].LastActivity)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

func (pi *ProjectIndex) outputRecordCloneLocked(rec *ProjectRecord) ProjectRecord {
	clone := *rec
	if !clone.outputLastActivity.IsZero() {
		clone.LastActivity = clone.outputLastActivity
		clone.Preview = clone.outputPreview
	}
	if p, ok := pi.prefs[rec.ProjectPath]; ok && p.Archived {
		clone.Archived = true
	}
	return clone
}

func (pi *ProjectIndex) scoreRecord(rec *ProjectRecord, queryLower string, queryTokens []string) float64 {
	var score float64

	nameLower := strings.ToLower(rec.Name)
	pathLower := strings.ToLower(rec.ProjectPath)
	previewLower := strings.ToLower(rec.Preview)

	// Archive keyword matching: English and Chinese terms find archived tasks.
	if p, ok := pi.prefs[rec.ProjectPath]; ok && p.Archived {
		if queryWantsArchived(queryLower) {
			score += 10.0
		}
	}

	// Exact substring match in name (highest weight).
	if strings.Contains(nameLower, queryLower) {
		score += 10.0
	}

	// Exact substring match in project path.
	if strings.Contains(pathLower, queryLower) {
		score += 5.0
	}

	// Token-level matching in tags.
	for _, token := range queryTokens {
		for _, tag := range rec.Tags {
			if strings.Contains(strings.ToLower(tag), token) {
				score += 3.0
				break
			}
		}
	}

	// Token-level matching in name.
	for _, token := range queryTokens {
		if strings.Contains(nameLower, token) {
			score += 2.0
		}
	}

	// Substring match in preview.
	if strings.Contains(previewLower, queryLower) {
		score += 1.0
	}

	// Recency boost should rank matched projects, not make every recent project a match.
	if score > 0 && time.Since(rec.LastActivity) < 24*time.Hour {
		score += 0.5
	}

	return score
}

func hasRecentTaskOutputTag(tags []string) bool {
	for _, tag := range tags {
		if tag == "tangible_output" || strings.HasPrefix(tag, "output_tool:") {
			return true
		}
	}
	return false
}

func isRecentTaskOutputEntry(e *Entry) bool {
	if e == nil {
		return false
	}
	cat := MapToCanonical(e.Category)
	sourceType := strings.ToLower(strings.TrimSpace(e.SourceType))
	if cat == CategoryProjectKnowledge {
		return sourceType == "task_sediment" && hasRecentTaskOutputTag(e.Tags)
	}
	if cat != CategoryTaskArtifact {
		return false
	}
	switch sourceType {
	case "", "task_artifact", "workflow_output", "workflow_output_ref":
		return true
	case "manual":
		return hasRecentTaskOutputTag(e.Tags) || looksLikeProjectPath(inferProjectPath(e))
	case "conversation_trim_ref", "context_checkpoint_ref", "session_start_extraction":
		return false
	default:
		return strings.Contains(sourceType, "workflow_output")
	}
}

func outputPreviewLine(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "#>*- \t"))
		lower := strings.ToLower(trimmed)
		for _, prefix := range []string{"result:", "output:", "artifact:", "artifacts:"} {
			if strings.HasPrefix(lower, prefix) {
				candidate := strings.TrimSpace(trimmed[len(prefix):])
				if candidate != "" && !isGenericOutputPreviewLine(candidate) {
					return candidate
				}
				for _, next := range lines[i+1:] {
					candidate = strings.TrimSpace(strings.TrimLeft(next, "#>*- \t"))
					if candidate != "" && !isGenericOutputPreviewLine(candidate) {
						return candidate
					}
				}
			}
		}
	}
	return firstMeaningfulLine(content)
}

func isGenericOutputPreviewLine(line string) bool {
	lower := strings.ToLower(strings.Trim(strings.TrimSpace(line), " .,!?:;"))
	switch lower {
	case "", "done", "completed", "finished", "updated", "fixed", "success", "ok":
		return true
	default:
		return false
	}
}

// inferProjectPath extracts a project path from an entry's metadata.
// Priority: SourceURL directory > Tags containing path-like values > empty.
func inferProjectPath(e *Entry) string {
	// 1. SourceURL: if it looks like a file path, use its directory. For
	// generated memory_refs files, prefer explicit project path tags below so
	// the navigation scene points at the real project instead of the cache dir.
	deferredSourceDir := ""
	if e.SourceURL != "" {
		if LooksLikeFilePath(e.SourceURL) {
			// Normalize to forward slashes for consistent Dir() behavior
			// across platforms (Linux filepath.Dir doesn't split on '\').
			fwd := toForwardSlash(e.SourceURL)
			dir := pathDir(fwd)
			if dir != "." && dir != "" && looksLikeProjectPath(dir) {
				if strings.Contains(fwd, "/memory_refs/") {
					deferredSourceDir = normalizeProjectPath(dir)
				} else {
					return normalizeProjectPath(dir)
				}
			}
		}
	}

	// 2. Tags: look for path-like tags.
	for _, tag := range e.Tags {
		fwd := toForwardSlash(tag)
		if looksLikeProjectPath(fwd) {
			return normalizeProjectPath(fwd)
		}
	}

	return deferredSourceDir
}

// toForwardSlash replaces all backslashes with forward slashes.
func toForwardSlash(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// pathDir returns the directory portion of a forward-slash path.
// Unlike filepath.Dir, this always uses '/' as separator, making it
// cross-platform safe for Windows paths processed on Linux.
//
// Matches filepath.Dir semantics: trailing slash means "this is a directory",
// so pathDir("/a/b/") returns "/a/b" (not "/a").
func pathDir(fwdPath string) string {
	// Trailing slash → the path itself is a directory; return it cleaned.
	if len(fwdPath) > 1 && fwdPath[len(fwdPath)-1] == '/' {
		return strings.TrimRight(fwdPath, "/")
	}
	idx := strings.LastIndex(fwdPath, "/")
	if idx < 0 {
		return "."
	}
	dir := fwdPath[:idx]
	if dir == "" {
		return "/"
	}
	return dir
}

// normalizeProjectPath converts a forward-slash path back to the canonical
// form for its platform: Windows paths (drive letter like D:/) get backslashes,
// Unix paths keep forward slashes. This ensures deterministic output regardless
// of the host OS (Linux CI processing Windows paths from user data).
func normalizeProjectPath(fwdPath string) string {
	// Clean up double slashes first (applies to both platforms).
	for strings.Contains(fwdPath, "//") {
		fwdPath = strings.ReplaceAll(fwdPath, "//", "/")
	}
	// Detect Windows path: second char is ':' (e.g. "D:/workprj/snake").
	if len(fwdPath) >= 2 && fwdPath[1] == ':' {
		return strings.ReplaceAll(fwdPath, "/", "\\")
	}
	return fwdPath
}

// LooksLikeFilePath returns true if s looks like an absolute file path
// (Windows drive letter, Unix absolute, or home-relative).
func LooksLikeFilePath(s string) bool {
	if len(s) < 3 {
		return false
	}
	// Windows: C:\... or D:\...
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	// Home directory (~/… or ~\…).
	if maclawpath.IsHomePath(s) {
		return true
	}
	// Unix absolute path — require at least 2 segments to avoid matching
	// content fragments like "/path" or "/tmp". Real project paths are
	// "/home/user/project" (3+ segments).
	if s[0] == '/' && len(s) > 1 {
		segments := 0
		for _, c := range s {
			if c == '/' {
				segments++
			}
		}
		// "/home/user" has 2 slashes → accepted.
		// "/path.dirname" has 1 slash → rejected.
		if segments >= 2 {
			return true
		}
	}
	return false
}

func looksLikeProjectPath(s string) bool {
	// Normalize to forward slashes for cross-platform consistency.
	// On Linux, filepath.Ext/Split don't recognize '\' as separator.
	fwd := toForwardSlash(s)
	if !LooksLikeFilePath(fwd) {
		return false
	}
	// Must not look like a regular file (has short extension like .go, .json).
	// Use last '/' to find the basename, then check for extension.
	lastSlash := strings.LastIndex(fwd, "/")
	base := fwd
	if lastSlash >= 0 {
		base = fwd[lastSlash+1:]
	}
	if dotIdx := strings.LastIndex(base, "."); dotIdx >= 0 {
		ext := base[dotIdx:]
		if len(ext) > 0 && len(ext) <= 5 {
			return false
		}
	}
	// Must have at least 2 path segments — bare roots like "C:/" or "/" are
	// not meaningful project paths.
	// Trim trailing slash, then check for at least one '/' after any drive prefix.
	trimmed := strings.TrimRight(fwd, "/")
	if trimmed == "" {
		return false // root "/"
	}
	// For Windows paths like "D:", strip the drive prefix.
	checkPart := trimmed
	if len(checkPart) >= 2 && checkPart[1] == ':' {
		checkPart = checkPart[2:] // "D:/workprj/snake" → "/workprj/snake"
	}
	// Need at least one '/' in the remaining part for 2+ segments.
	if !strings.Contains(checkPart, "/") {
		return false
	}
	return true
}

// extractTitle extracts a human-readable title from content.
// Tries: first markdown heading, then first non-empty meaningful line.
// Strips common noise prefixes that appear in memory entries.
func extractTitle(content string) string {
	lines := strings.SplitN(content, "\n", 15)
	// Pass 1: look for markdown headings (highest quality).
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return cleanTitle(strings.TrimPrefix(line, "# "))
		}
		if strings.HasPrefix(line, "## ") {
			return cleanTitle(strings.TrimPrefix(line, "## "))
		}
	}
	// Pass 2: first non-empty, non-noise line.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip lines that are just metadata/labels, not real titles.
		if isTitleNoiseLine(line) {
			continue
		}
		return truncateRunes(cleanTitle(line), 60)
	}
	return ""
}

// isHeadingTitle returns true if the title was extracted from a markdown heading
// (contains no leading noise and is reasonably short). Used for quality comparison.
func isHeadingTitle(title string) bool {
	if title == "" {
		return false
	}
	// Heading-extracted titles are clean and short; first-line fallbacks
	// tend to be longer or contain path-like content.
	r := []rune(title)
	return len(r) <= 80 && !strings.ContainsAny(title, "\\/")
}

// cleanTitle removes common noise from extracted titles.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	// Strip leading markdown list markers: "- ", "* ", "1. "
	for _, prefix := range []string{"- ", "* ", "• "} {
		s = strings.TrimPrefix(s, prefix)
	}
	if len(s) > 2 && s[0] >= '0' && s[0] <= '9' && (s[1] == '.' || (s[1] >= '0' && s[1] <= '9' && len(s) > 3 && s[2] == '.')) {
		if idx := strings.Index(s, ". "); idx >= 0 && idx < 4 {
			s = s[idx+2:]
		}
	}
	// Strip leading/trailing bold markers: **text** → text
	s = strings.TrimPrefix(s, "**")
	s = strings.TrimSuffix(s, "**")
	s = strings.TrimSpace(s)
	// Strip leading decorative pictographs (common in workflow documents).
	s = strings.TrimSpace(textutil.StripLeadingEmojiCluster(s))
	// Strip phase prefixes that make all tasks look the same.
	for _, prefix := range []string{
		"需求文档：", "需求文档:", "需求文档 —", "需求文档—",
		"技术设计文档：", "技术设计文档:", "技术设计：", "技术设计:",
		"任务列表：", "任务列表:", "任务拆分：", "任务拆分:",
		"Requirements:", "Technical Design:", "Task List:",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			break
		}
	}
	return s
}

// isTitleNoiseLine returns true if the line is metadata/noise, not a real title.
func isTitleNoiseLine(line string) bool {
	lower := strings.ToLower(line)
	// Phase labels, timestamps, metadata prefixes.
	for _, prefix := range []string{
		"---", "```", "<!--", "category:", "tags:", "scope:",
		"created:", "updated:", "source:", "phase:",
		"[系统", "[system", "[recover", "[执行",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	// Markdown metadata lines: "- **key**: value" or "- key: value"
	trimmed := strings.TrimLeft(line, "- *")
	if idx := strings.Index(trimmed, ":"); idx > 0 && idx < 20 {
		key := strings.ToLower(strings.TrimRight(trimmed[:idx], "* "))
		for _, mk := range []string{
			"项目名称", "地址", "端口", "用户", "密码", "路径",
			"name", "address", "host", "port", "user", "path", "url",
			"status", "version", "type", "模型", "状态", "版本", "类型",
		} {
			if key == mk {
				return true
			}
		}
	}
	// Pure path lines.
	if LooksLikeFilePath(line) {
		return true
	}
	// Lines that are mostly path-like (contain backslash sequences).
	if strings.Count(line, "\\") >= 2 {
		return true
	}
	return false
}

// firstMeaningfulLine returns the first non-empty, non-heading line.
func firstMeaningfulLine(content string) string {
	lines := strings.SplitN(content, "\n", 20)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

func mergeTagsDedup(existing, newTags []string) []string {
	if len(newTags) == 0 {
		return existing
	}
	set := make(map[string]bool, len(existing))
	for _, t := range existing {
		set[t] = true
	}
	for _, t := range newTags {
		if !set[t] {
			existing = append(existing, t)
			set[t] = true
		}
	}
	return existing
}

func addCategoryDedup(cats []Category, c Category) []Category {
	for _, existing := range cats {
		if existing == c {
			return cats
		}
	}
	return append(cats, c)
}
