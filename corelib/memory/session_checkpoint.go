package memory

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
