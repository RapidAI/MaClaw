package memory

import "strings"

// ProjectEntriesForArchive returns project-scoped knowledge/artifact entries
// that belong to projectPath. It centralizes the archive collection rule so
// host adapters do not inspect Store internals directly.
func (s *Store) ProjectEntriesForArchive(projectPath string) []Entry {
	if s == nil || strings.TrimSpace(projectPath) == "" {
		return nil
	}
	normalizedPath := normalizeMemoryProjectTag(projectPath)
	var result []Entry
	for _, entry := range s.AllEntries() {
		cat := MapToCanonical(entry.Category)
		if cat != CategoryTaskArtifact && cat != CategoryProjectKnowledge {
			continue
		}
		if !entryBelongsToProjectPath(entry, normalizedPath) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// ArchivedExperienceForProject returns the archived experience summary stored
// for projectPath, if any.
func (s *Store) ArchivedExperienceForProject(projectPath string) string {
	if s == nil || strings.TrimSpace(projectPath) == "" {
		return ""
	}
	normalizedPath := normalizeMemoryProjectTag(projectPath)
	for _, entry := range s.AllEntries() {
		if MapToCanonical(entry.Category) != CategoryProjectKnowledge {
			continue
		}
		if !entryHasAllTags(entry.Tags, "archived_experience") {
			continue
		}
		if entryHasExactProjectTag(entry, normalizedPath) {
			return entry.Content
		}
	}
	return ""
}

// DeleteProjectEntries permanently removes memory entries owned by one task.
// Project ownership is deliberately exact: deleting a task must not remove
// entries belonging to a nested project that merely shares a path prefix.
// The call waits until the project index rebuild finishes so a replacement
// task created immediately cannot observe the deleted row.
func (s *Store) DeleteProjectEntries(projectPath string) (int, error) {
	return s.deleteProjectEntries(projectPath, true)
}

// DeleteProjectEntriesUnwaited is DeleteProjectEntries without waiting for
// the index rebuild. The sidebar delete path drops the row itself
// (ProjectIndex.ForgetProject) and must not sit on the full rescan.
func (s *Store) DeleteProjectEntriesUnwaited(projectPath string) (int, error) {
	return s.deleteProjectEntries(projectPath, false)
}

func (s *Store) deleteProjectEntries(projectPath string, wait bool) (int, error) {
	ids := s.projectEntryIDs(projectPath)
	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.UpdateEntriesAndDeleteIDs(nil, ids); err != nil {
		return 0, err
	}
	if wait {
		// Callers that immediately create a replacement task must not observe the
		// deleted task through the asynchronously rebuilt project index.
		s.WaitRebuild()
	}
	return len(ids), nil
}

// projectEntryIDs collects matching ids under the store lock without copying
// entry bodies. AllEntries duplicates every conversation and artifact on the
// delete path, and that copy held the read lock long enough to stall writers.
func (s *Store) projectEntryIDs(projectPath string) []string {
	if s == nil {
		return nil
	}
	target := normalizeMemoryProjectTag(projectPath)
	if target == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0)
	for i := range s.entries {
		entry := &s.entries[i]
		if entry.ID == "" {
			continue
		}
		boundary := ""
		if entry.Boundary != nil {
			boundary = entry.Boundary.ProjectPath
		}
		if entryTagsHaveExactProject(entry.Tags, target) || normalizeMemoryProjectTag(boundary) == target {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

func entryTagsHaveExactProject(tags []string, target string) bool {
	if target == "" {
		return false
	}
	for _, tag := range tags {
		if normalizeMemoryProjectTag(tag) == target {
			return true
		}
	}
	return false
}

func entryBelongsToProjectPath(entry Entry, normalizedProjectPath string) bool {
	if normalizedProjectPath == "" {
		return false
	}
	for _, tag := range entry.Tags {
		normalizedTag := normalizeMemoryProjectTag(tag)
		if normalizedTag == normalizedProjectPath || strings.HasPrefix(normalizedTag, normalizedProjectPath+"/") {
			return true
		}
	}
	return false
}

func entryHasExactProjectTag(entry Entry, normalizedProjectPath string) bool {
	return entryTagsHaveExactProject(entry.Tags, normalizedProjectPath)
}

func normalizeMemoryProjectTag(path string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
}
