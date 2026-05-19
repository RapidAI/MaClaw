package memory

// GeneratedInsightUpsertOptions describes a generated semantic memory that is
// not tied to a narrower project/task/summary category helper. Use it for
// self-review insights and similar system-authored standing guidance.
type GeneratedInsightUpsertOptions struct {
	Title            string
	Content          string
	Category         Category
	Tags             []string
	IdentityTagCount int
	Scope            Scope
	OwnerID          string
	SourceType       string
	EvidenceIDs      []string
	RelatedIDs       []string
	DerivedKind      string
	Boundary         *MemoryBoundary
}

// UpsertGeneratedInsight creates or updates a generated semantic insight while
// applying the evidence-first derived metadata defaults used by generated paths.
func (s *Store) UpsertGeneratedInsight(opts GeneratedInsightUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	category := opts.Category
	if category == "" {
		category = CategoryInstruction
	}
	scope := opts.Scope
	if scope == "" {
		scope = InferScope(category)
	}
	sourceType := opts.SourceType
	if sourceType == "" {
		sourceType = "generated_insight"
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
		Category:           category,
		Tags:               opts.Tags,
		IdentityTagCount:   opts.IdentityTagCount,
		Scope:              scope,
		OwnerID:            opts.OwnerID,
		SourceType:         sourceType,
		EvidenceIDs:        opts.EvidenceIDs,
		RelatedIDs:         opts.RelatedIDs,
		DerivedKind:        opts.DerivedKind,
		Boundary:           opts.Boundary,
		DefaultDerivedKind: derivedKind,
		DefaultBoundary:    boundary,
	})
}
