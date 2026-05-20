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
	if normalizedProjectPath == "" {
		return false
	}
	for _, tag := range entry.Tags {
		if normalizeMemoryProjectTag(tag) == normalizedProjectPath {
			return true
		}
	}
	return false
}

func normalizeMemoryProjectTag(path string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
}
