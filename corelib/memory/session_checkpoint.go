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

// LatestSessionCheckpointForHost returns the content of the checkpoint with the
// latest UpdatedAt whose tags contain projectPath. It touches the selected entry.
func (s *Store) LatestSessionCheckpointForHost(projectPath string) string {
	return s.LatestSessionCheckpointForOwner(projectPath, "")
}

// LatestSessionCheckpointForOwner is LatestSessionCheckpointForHost restricted
// to ownerID. An empty ownerID does not filter. Named owners do not see each
// other's checkpoints.
func (s *Store) LatestSessionCheckpointForOwner(projectPath, ownerID string) string {
	if s == nil || projectPath == "" {
		return ""
	}
	ownerID = strings.TrimSpace(ownerID)

	var id, content string

	s.mu.RLock()
	bestIdx := -1
	for i := range s.entries {
		e := &s.entries[i]
		if e.Category != CategorySessionCheckpoint || !e.IsActive() {
			continue
		}
		if ownerID != "" && !memoryOwnersEqual(e.OwnerID, ownerID) {
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
		if bestIdx == -1 || checkpointNewer(e, &s.entries[bestIdx]) {
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

func checkpointNewer(candidate, current *Entry) bool {
	if candidate.UpdatedAt.After(current.UpdatedAt) {
		return true
	}
	if current.UpdatedAt.After(candidate.UpdatedAt) {
		return false
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}
