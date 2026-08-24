package memory

import "time"

// TaskArtifactUpsertOptions describes a generated task artifact write.
// It centralizes the CategoryTaskArtifact + ScopeProject convention shared by
// GUI, TUI, and server-side integrations.
type TaskArtifactUpsertOptions struct {
	Title             string
	Content           string
	Tags              []string
	IdentityTagCount  int
	OwnerID           string
	SourceType        string
	SourceURL         string
	EvidenceIDs       []string
	RelatedIDs        []string
	DerivedKind       string
	Boundary          *MemoryBoundary
	CreatedAt         time.Time
	UpdatedAt         time.Time
	MergeExistingTags func(existing, desired []string) []string
}

// UpsertTaskArtifact creates or updates a generated project-scoped task
// artifact. Use this for workflow outputs, context checkpoints, trim refs,
// manual task artifacts, and similar records with a stable generated identity.
func (s *Store) UpsertTaskArtifact(opts TaskArtifactUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	sourceType := opts.SourceType
	if sourceType == "" {
		sourceType = "task_artifact"
	}
	derivedKind := opts.DerivedKind
	if derivedKind == "" {
		derivedKind = sourceType
	}
	boundary := opts.Boundary
	if boundary == nil {
		boundary = generatedRecordBoundary(opts.Tags, opts.OwnerID, sourceType)
	}
	return s.UpsertEntryByTags(UpsertByTagsOptions{
		Title:              opts.Title,
		Content:            opts.Content,
		Category:           CategoryTaskArtifact,
		Tags:               opts.Tags,
		IdentityTagCount:   opts.IdentityTagCount,
		Scope:              ScopeProject,
		OwnerID:            opts.OwnerID,
		SourceType:         sourceType,
		SourceURL:          opts.SourceURL,
		EvidenceIDs:        opts.EvidenceIDs,
		RelatedIDs:         opts.RelatedIDs,
		DerivedKind:        opts.DerivedKind,
		Boundary:           opts.Boundary,
		DefaultDerivedKind: derivedKind,
		DefaultBoundary:    boundary,
		CreatedAt:          opts.CreatedAt,
		UpdatedAt:          opts.UpdatedAt,
		MergeExistingTags:  opts.MergeExistingTags,
	})
}
