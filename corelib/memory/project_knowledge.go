package memory

import "strings"

// ProjectKnowledgeUpsertOptions describes a generated project-knowledge write.
// It centralizes the CategoryProjectKnowledge convention and defaults scope to
// project unless callers explicitly choose a different scope.
type ProjectKnowledgeUpsertOptions struct {
	ID                string
	Title             string
	Content           string
	Tags              []string
	IdentityTagCount  int
	Scope             Scope
	OwnerID           string
	SourceType        string
	SourceURL         string
	EvidenceIDs       []string
	RelatedIDs        []string
	DerivedKind       string
	Boundary          *MemoryBoundary
	MergeExistingTags func(existing, desired []string) []string
}

// UpsertProjectKnowledge creates or updates a generated project-knowledge
// entry. Use this for learned usage patterns, auto-fetch items, A2A discussion
// summaries, archive experience, and similar generated knowledge records.
func (s *Store) UpsertProjectKnowledge(opts ProjectKnowledgeUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	sourceType := opts.SourceType
	if sourceType == "" {
		sourceType = "project_knowledge"
	}
	scope := opts.Scope
	if scope == "" {
		scope = InferScope(CategoryProjectKnowledge)
	}
	derivedKind := opts.DerivedKind
	if derivedKind == "" {
		derivedKind = sourceType
	}
	id := strings.TrimSpace(opts.ID)
	boundary := opts.Boundary
	if boundary == nil && !upsertMatchedIDHasBoundary(s, id) {
		boundary = generatedRecordBoundary(opts.Tags, opts.OwnerID, sourceType)
	}
	if id != "" {
		return s.upsertEntryByID(Entry{
			ID:          id,
			Title:       opts.Title,
			Content:     opts.Content,
			Category:    CategoryProjectKnowledge,
			Tags:        opts.Tags,
			Scope:       scope,
			OwnerID:     opts.OwnerID,
			SourceType:  sourceType,
			SourceURL:   opts.SourceURL,
			EvidenceIDs: append([]string(nil), opts.EvidenceIDs...),
			RelatedIDs:  append([]string(nil), opts.RelatedIDs...),
			DerivedKind: derivedKind,
			Boundary:    cloneMemoryBoundary(boundary),
		}, opts.MergeExistingTags, true)
	}
	return s.UpsertEntryByTags(UpsertByTagsOptions{
		Title:              opts.Title,
		Content:            opts.Content,
		Category:           CategoryProjectKnowledge,
		Tags:               opts.Tags,
		IdentityTagCount:   opts.IdentityTagCount,
		Scope:              scope,
		OwnerID:            opts.OwnerID,
		SourceType:         sourceType,
		SourceURL:          opts.SourceURL,
		EvidenceIDs:        opts.EvidenceIDs,
		RelatedIDs:         opts.RelatedIDs,
		DerivedKind:        opts.DerivedKind,
		Boundary:           opts.Boundary,
		DefaultDerivedKind: derivedKind,
		DefaultBoundary:    boundary,
		MergeExistingTags:  opts.MergeExistingTags,
	})
}
