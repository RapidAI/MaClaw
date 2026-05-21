package memory

import "strings"

// SessionCheckpointUpsertOptions describes a generated session checkpoint.
type SessionCheckpointUpsertOptions struct {
	Title            string
	Content          string
	Tags             []string
	IdentityTagCount int
	OwnerID          string
	EvidenceIDs      []string
}

// UpsertSessionCheckpoint creates or updates a project-scoped session progress
// checkpoint that can be recalled when work resumes on the same project.
func (s *Store) UpsertSessionCheckpoint(opts SessionCheckpointUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	sourceType := "session_checkpoint"
	boundary := generatedRecordBoundary(opts.Tags, opts.OwnerID, sourceType)
	return s.UpsertEntryByTags(UpsertByTagsOptions{
		Title:            opts.Title,
		Content:          opts.Content,
		Category:         CategorySessionCheckpoint,
		Tags:             opts.Tags,
		IdentityTagCount: opts.IdentityTagCount,
		Scope:            ScopeProject,
		OwnerID:          opts.OwnerID,
		SourceType:       sourceType,
		EvidenceIDs:      opts.EvidenceIDs,
		DerivedKind:      "session_checkpoint",
		Boundary:         boundary,
	})
}

// LatestSessionCheckpointForHost returns the content of the most recent
// session checkpoint whose tags contain the given project path. It touches
// (increments AccessCount on) the selected entry so that the forgetting curve
// keeps it alive. Returns "" if no matching checkpoint exists.
func (s *Store) LatestSessionCheckpointForHost(projectPath string) string {
	if s == nil || projectPath == "" {
		return ""
	}

	var id, content string

	s.mu.RLock()
	var bestIdx int = -1
	for i := range s.entries {
		e := &s.entries[i]
		if e.Category != CategorySessionCheckpoint {
			continue
		}
		found := false
		for _, tag := range e.Tags {
			if strings.EqualFold(tag, projectPath) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if bestIdx == -1 || e.CreatedAt.After(s.entries[bestIdx].CreatedAt) {
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		id = s.entries[bestIdx].ID
		content = s.entries[bestIdx].Content
	}
	s.mu.RUnlock()

	if id != "" {
		s.TouchAccess([]string{id})
	}
	return content
}
