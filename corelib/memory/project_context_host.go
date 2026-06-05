package memory

import "sort"

// ProjectContextForHostData groups the memory entries and scene metadata a host
// needs when opening or summarizing a project-scoped tab. It centralizes the
// strict project recall queries so GUI/TUI/server hosts do not duplicate them.
type ProjectContextForHostData struct {
	TaskArtifacts    []Entry     `json:"task_artifacts"`
	ProjectKnowledge []Entry     `json:"project_knowledge"`
	Entries          []Entry     `json:"entries"`
	Scene            SceneRecord `json:"scene"`
	HasScene         bool        `json:"has_scene"`
}

// ProjectContextForHost returns project-scoped artifacts, knowledge, and scene
// metadata using the same strict project isolation as project-tab recall.
func (s *Store) ProjectContextForHost(projectPath string, sceneLimit int) ProjectContextForHostData {
	data := ProjectContextForHostData{}
	if s == nil || projectPath == "" {
		return data
	}
	projectLower := semanticNormalizeProjectPath(projectPath)
	if projectLower == "" {
		return data
	}

	s.mu.RLock()
	entries := append([]Entry(nil), s.entries...)
	s.mu.RUnlock()

	projectSceneEntries := make([]Entry, 0, 16)
	for _, entry := range entries {
		if recallDynamicEntryAllowedStrict(entry, CategoryTaskArtifact, projectLower, "") {
			data.TaskArtifacts = append(data.TaskArtifacts, entry)
		}
		if recallDynamicEntryAllowedStrict(entry, CategoryProjectKnowledge, projectLower, "") {
			data.ProjectKnowledge = append(data.ProjectKnowledge, entry)
		}
		if projectContextSceneEntryAllowed(entry, projectLower) {
			projectSceneEntries = append(projectSceneEntries, entry)
		}
	}
	sortProjectContextEntries(data.TaskArtifacts)
	sortProjectContextEntries(data.ProjectKnowledge)
	data.TaskArtifacts = limitProjectContextEntries(data.TaskArtifacts, 8)
	data.ProjectKnowledge = limitProjectContextEntries(data.ProjectKnowledge, 8)

	seen := make(map[string]bool, len(data.TaskArtifacts)+len(data.ProjectKnowledge))
	for _, entry := range data.TaskArtifacts {
		if !seen[entry.ID] {
			seen[entry.ID] = true
			data.Entries = append(data.Entries, entry)
		}
	}
	for _, entry := range data.ProjectKnowledge {
		if !seen[entry.ID] {
			seen[entry.ID] = true
			data.Entries = append(data.Entries, entry)
		}
	}
	if sceneLimit <= 0 {
		sceneLimit = 20
	}
	for _, scene := range BuildSceneIndex(projectSceneEntries, sceneLimit) {
		if semanticProjectPathMatches(scene.ProjectPath, projectLower) {
			data.Scene = scene
			data.HasScene = true
			break
		}
	}
	return data
}

func sortProjectContextEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
}

func limitProjectContextEntries(entries []Entry, limit int) []Entry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}
	return append([]Entry(nil), entries[:limit]...)
}

func projectContextSceneEntryAllowed(entry Entry, projectLower string) bool {
	if projectLower == "" || !entry.IsActive() {
		return false
	}
	if !projectCategories[entry.Category] && !projectCategories[MapToCanonical(entry.Category)] {
		return false
	}
	if !recallBoundaryAllowed(entry, projectLower, "") {
		return false
	}
	projectPath := semanticNormalizeProjectPath(inferProjectPath(&entry))
	return semanticProjectPathMatches(projectPath, projectLower)
}

// SceneForProjectForHost returns the scene navigation record for one project.
func (s *Store) SceneForProjectForHost(projectPath string, limit int) (SceneRecord, bool) {
	if s == nil || projectPath == "" {
		return SceneRecord{}, false
	}
	if limit <= 0 {
		limit = 100
	}
	for _, scene := range s.SceneIndex(limit) {
		if scene.ProjectPath == projectPath {
			return scene, true
		}
	}
	return SceneRecord{}, false
}

// ScenesByProjectForHost returns scene records keyed by project path for host
// project list projections.
func (s *Store) ScenesByProjectForHost(limit int) map[string]SceneRecord {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	scenes := s.SceneIndex(limit)
	if len(scenes) == 0 {
		return nil
	}
	byPath := make(map[string]SceneRecord, len(scenes))
	for _, scene := range scenes {
		if scene.ProjectPath == "" {
			continue
		}
		byPath[scene.ProjectPath] = scene
	}
	return byPath
}

// ProjectSearchRecordForHost is the corelib-owned project search/list
// projection. Hosts can map it to their transport DTOs without reaching into
// ProjectIndex preference and scene internals.
type ProjectSearchRecordForHost struct {
	Record      ProjectRecord `json:"record"`
	DisplayName string        `json:"display_name,omitempty"`
	Pinned      bool          `json:"pinned"`
	Archived    bool          `json:"archived"`
	Scene       SceneRecord   `json:"scene"`
	HasScene    bool          `json:"has_scene"`
}

// SearchProjectsForHost returns output-backed project records with preference
// and scene metadata already joined by corelib/memory.
func (s *Store) SearchProjectsForHost(query string, limit int) []ProjectSearchRecordForHost {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	pi := s.ProjectIndex()
	if pi == nil {
		return nil
	}
	var records []ProjectRecord
	if query == "" {
		records = pi.ListRecent(limit)
	} else {
		records = pi.Search(query, limit)
	}
	scenesByPath := s.ScenesByProjectForHost(limit * 2)
	out := make([]ProjectSearchRecordForHost, 0, len(records))
	for _, rec := range records {
		item := projectSearchRecordForHost(pi, rec)
		if scene, ok := scenesByPath[rec.ProjectPath]; ok {
			item.Scene = scene
			item.HasScene = true
		}
		out = append(out, item)
	}
	return out
}

// ProjectRecordForHost returns a single project record joined with user
// preferences and scene metadata.
func (s *Store) ProjectRecordForHost(projectPath string) (ProjectSearchRecordForHost, bool) {
	if s == nil || projectPath == "" {
		return ProjectSearchRecordForHost{}, false
	}
	pi := s.ProjectIndex()
	if pi == nil {
		return ProjectSearchRecordForHost{}, false
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return ProjectSearchRecordForHost{}, false
	}
	item := projectSearchRecordForHost(pi, *rec)
	if scene, ok := s.SceneForProjectForHost(projectPath, 100); ok {
		item.Scene = scene
		item.HasScene = true
	}
	return item, true
}

func projectSearchRecordForHost(pi *ProjectIndex, rec ProjectRecord) ProjectSearchRecordForHost {
	item := ProjectSearchRecordForHost{
		Record:   rec,
		Pinned:   pi.IsPinned(rec.ProjectPath),
		Archived: pi.IsArchived(rec.ProjectPath),
	}
	if custom := pi.CustomName(rec.ProjectPath); custom != "" {
		item.DisplayName = custom
	} else {
		item.DisplayName = rec.Name
	}
	return item
}

// ProjectDisplayNameForHost returns the user-facing project name from the
// corelib project index preferences, falling back to the indexed name.
func (s *Store) ProjectDisplayNameForHost(projectPath string) string {
	if s == nil || projectPath == "" {
		return ""
	}
	pi := s.ProjectIndex()
	if pi == nil {
		return ""
	}
	return pi.GetDisplayName(projectPath)
}

// RenameProjectForHost updates the user-defined display name and returns the
// resulting display name.
func (s *Store) RenameProjectForHost(projectPath, name string) string {
	if s == nil || projectPath == "" {
		return ""
	}
	pi := s.ProjectIndex()
	if pi == nil {
		return ""
	}
	pi.SetCustomName(projectPath, name)
	return pi.GetDisplayName(projectPath)
}

// PinProjectForHost pins or unpins a project in host task lists.
func (s *Store) PinProjectForHost(projectPath string, pinned bool) {
	if s == nil || projectPath == "" {
		return
	}
	if pi := s.ProjectIndex(); pi != nil {
		pi.SetPinned(projectPath, pinned)
	}
}

// HideProjectForHost hides a project from host task lists without deleting its
// underlying long-term memories.
func (s *Store) HideProjectForHost(projectPath string) {
	if s == nil || projectPath == "" {
		return
	}
	if pi := s.ProjectIndex(); pi != nil {
		pi.SetHidden(projectPath, true)
	}
}

// ArchiveProjectPreferenceForHost marks a project archived in the corelib
// project index. Archival extraction remains a separate generated-memory write.
func (s *Store) ArchiveProjectPreferenceForHost(projectPath string, archived bool) {
	if s == nil || projectPath == "" {
		return
	}
	if pi := s.ProjectIndex(); pi != nil {
		pi.SetArchived(projectPath, archived)
	}
}

// ProjectArchivedForHost returns whether the project is archived in the
// corelib project index preferences.
func (s *Store) ProjectArchivedForHost(projectPath string) bool {
	if s == nil || projectPath == "" {
		return false
	}
	if pi := s.ProjectIndex(); pi != nil {
		return pi.IsArchived(projectPath)
	}
	return false
}

// SetProjectChangeHandlerForHost registers a host callback for project-index
// changes without exposing the ProjectIndex implementation to GUI/TUI/server
// layers.
func (s *Store) SetProjectChangeHandlerForHost(handler func(projectPath string)) {
	if s == nil {
		return
	}
	if pi := s.ProjectIndex(); pi != nil {
		pi.OnChanged = handler
	}
}
