package memory

import (
	"fmt"
	"strings"
	"time"
)

// UpsertResult reports whether a memory write created, updated, or only
// touched an existing entry.
type UpsertResult struct {
	Created bool
	Updated bool
	Touched bool
	EntryID string
}

// UpsertEntryByID creates an entry or updates the existing entry with the same
// ID. Unchanged entries are touched so recall recency still reflects use.
func (s *Store) UpsertEntryByID(entry Entry) (UpsertResult, error) {
	return s.upsertEntryByID(entry, nil, false)
}

func (s *Store) upsertEntryByID(entry Entry, mergeExistingTags func(existing, desired []string) []string, mergeMatchedIDTags bool) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		entry.ID = generateID()
		if duplicate := s.findUpsertDuplicateByContent(entry); duplicate != nil {
			entry = upsertDesiredForDuplicate(*duplicate, entry, mergeExistingTags)
			if upsertEntryEquivalent(*duplicate, entry) {
				s.TouchAccess([]string{duplicate.ID})
				return UpsertResult{Touched: true, EntryID: duplicate.ID}, nil
			}
			if err := s.updateEntryFromUpsert(duplicate.ID, entry); err != nil {
				return UpsertResult{}, err
			}
			return UpsertResult{Updated: true, EntryID: duplicate.ID}, nil
		}
		if err := s.insertEntryFromUpsert(entry); err != nil {
			return UpsertResult{}, err
		}
		return UpsertResult{Created: true, EntryID: entry.ID}, nil
	}
	existing := s.SearchDirectByID(entry.ID)
	if len(existing) == 0 {
		if duplicate := s.findUpsertDuplicateByContent(entry); duplicate != nil {
			entry = upsertDesiredForDuplicate(*duplicate, entry, mergeExistingTags)
			if upsertEntryEquivalent(*duplicate, entry) {
				s.TouchAccess([]string{duplicate.ID})
				return UpsertResult{Touched: true, EntryID: duplicate.ID}, nil
			}
			if err := s.updateEntryFromUpsert(duplicate.ID, entry); err != nil {
				return UpsertResult{}, err
			}
			return UpsertResult{Updated: true, EntryID: duplicate.ID}, nil
		}
		if err := s.insertEntryFromUpsert(entry); err != nil {
			return UpsertResult{}, err
		}
		return UpsertResult{Created: true, EntryID: entry.ID}, nil
	}
	current := existing[0]
	if mergeMatchedIDTags {
		entry = upsertDesiredForDuplicate(current, entry, mergeExistingTags)
	}
	if upsertEntryEquivalent(current, entry) {
		s.TouchAccess([]string{entry.ID})
		return UpsertResult{Touched: true, EntryID: entry.ID}, nil
	}
	if err := s.updateEntryFromUpsert(entry.ID, entry); err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{Updated: true, EntryID: entry.ID}, nil
}

// UpsertByTagsOptions controls upsert matching for generated memory records
// that are identified by a stable prefix of tags rather than by a fixed ID.
type UpsertByTagsOptions struct {
	Content            string
	Category           Category
	Tags               []string
	IdentityTagCount   int
	Title              string
	SourceType         string
	SourceURL          string
	Scope              Scope
	OwnerID            string
	Level              TemporalLevel
	Interval           *TimeInterval
	EvidenceIDs        []string
	RelatedIDs         []string
	RelatedEdges       []RelatedEdge
	DerivedKind        string
	Boundary           *MemoryBoundary
	DefaultDerivedKind string
	DefaultBoundary    *MemoryBoundary
	CreatedAt          time.Time
	UpdatedAt          time.Time
	MergeExistingTags  func(existing, desired []string) []string
}

// UpsertEntryByTags creates or updates a generated memory entry matched by the
// first IdentityTagCount tags. It centralizes the common generated-memory write
// path used by host integrations.
func (s *Store) UpsertEntryByTags(opts UpsertByTagsOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	content := strings.TrimSpace(opts.Content)
	tags := normalizeUpsertTags(opts.Tags)
	if content == "" || len(tags) == 0 {
		return UpsertResult{}, nil
	}
	category := opts.Category
	if category == "" {
		category = CategoryProjectKnowledge
	}
	identityCount := opts.IdentityTagCount
	if identityCount <= 0 || identityCount > len(tags) {
		identityCount = len(tags)
	}
	identityTags := tags[:identityCount]

	for _, entry := range s.List(category, "") {
		if !upsertOwnerMatches(entry.OwnerID, opts.OwnerID) || !entryHasAllTags(entry.Tags, identityTags...) {
			continue
		}
		mergedTags := upsertMergedDuplicateTags(entry.Tags, tags, opts.MergeExistingTags)
		derivedKind := strings.TrimSpace(opts.DerivedKind)
		if derivedKind == "" && strings.TrimSpace(entry.DerivedKind) == "" {
			derivedKind = strings.TrimSpace(opts.DefaultDerivedKind)
		}
		boundary := opts.Boundary
		if boundary == nil && entry.Boundary == nil {
			boundary = opts.DefaultBoundary
		}
		desired := Entry{
			Title:        strings.TrimSpace(opts.Title),
			Content:      content,
			Category:     category,
			Tags:         mergedTags,
			SourceType:   opts.SourceType,
			SourceURL:    strings.TrimSpace(opts.SourceURL),
			Scope:        opts.Scope,
			OwnerID:      strings.TrimSpace(opts.OwnerID),
			Level:        opts.Level,
			Interval:     cloneTimeInterval(opts.Interval),
			EvidenceIDs:  append([]string(nil), opts.EvidenceIDs...),
			RelatedIDs:   append([]string(nil), opts.RelatedIDs...),
			RelatedEdges: append([]RelatedEdge(nil), opts.RelatedEdges...),
			DerivedKind:  derivedKind,
			Boundary:     cloneMemoryBoundary(boundary),
		}
		if upsertEntryEquivalent(entry, desired) {
			s.TouchAccess([]string{entry.ID})
			return UpsertResult{Touched: true, EntryID: entry.ID}, nil
		}
		if err := s.updateEntryFromUpsert(entry.ID, desired); err != nil {
			return UpsertResult{}, err
		}
		return UpsertResult{Updated: true, EntryID: entry.ID}, nil
	}

	derivedKind := strings.TrimSpace(opts.DerivedKind)
	if derivedKind == "" {
		derivedKind = strings.TrimSpace(opts.DefaultDerivedKind)
	}
	boundary := opts.Boundary
	if boundary == nil {
		boundary = opts.DefaultBoundary
	}
	entry := Entry{
		ID:           generateID(),
		Title:        strings.TrimSpace(opts.Title),
		Content:      content,
		Category:     category,
		Tags:         tags,
		SourceType:   opts.SourceType,
		SourceURL:    strings.TrimSpace(opts.SourceURL),
		Scope:        opts.Scope,
		OwnerID:      strings.TrimSpace(opts.OwnerID),
		Level:        opts.Level,
		Interval:     cloneTimeInterval(opts.Interval),
		EvidenceIDs:  append([]string(nil), opts.EvidenceIDs...),
		RelatedIDs:   append([]string(nil), opts.RelatedIDs...),
		RelatedEdges: append([]RelatedEdge(nil), opts.RelatedEdges...),
		DerivedKind:  derivedKind,
		Boundary:     cloneMemoryBoundary(boundary),
		CreatedAt:    opts.CreatedAt,
		UpdatedAt:    opts.UpdatedAt,
	}
	if duplicate := s.findUpsertDuplicateByContent(entry); duplicate != nil {
		entry = upsertDesiredForDuplicate(*duplicate, entry, opts.MergeExistingTags)
		if opts.DerivedKind == "" && strings.TrimSpace(duplicate.DerivedKind) != "" {
			entry.DerivedKind = ""
		}
		if opts.Boundary == nil && duplicate.Boundary != nil {
			entry.Boundary = nil
		}
		if upsertEntryEquivalent(*duplicate, entry) {
			s.TouchAccess([]string{duplicate.ID})
			return UpsertResult{Touched: true, EntryID: duplicate.ID}, nil
		}
		if err := s.updateEntryFromUpsert(duplicate.ID, entry); err != nil {
			return UpsertResult{}, err
		}
		return UpsertResult{Updated: true, EntryID: duplicate.ID}, nil
	}
	if err := s.insertEntryFromUpsert(entry); err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{Created: true, EntryID: entry.ID}, nil
}

func (s *Store) insertEntryFromUpsert(entry Entry) error {
	content := strings.TrimSpace(entry.Content)
	if content == "" {
		return fmt.Errorf("memory_store: content must not be empty")
	}
	if err := ScanForInjection(content); err != nil {
		return fmt.Errorf("memory_store: rejected: %w", err)
	}
	entry.Content = redactSecretsInMemory(content)
	if entry.ID == "" {
		entry.ID = generateID()
	}
	if len(entry.Embedding) == 0 {
		s.mu.RLock()
		emb := s.embedder
		s.mu.RUnlock()
		if emb != nil {
			vec, err := emb.Embed(entry.Content)
			if err == nil && len(vec) > 0 {
				entry.Embedding = vec
			}
		}
	}

	hash := computeContentHash(entry.Content)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.entries {
		if existing.ContentHash != hash && strings.TrimSpace(existing.Content) != entry.Content {
			continue
		}
		if entry.Category != "" && MapToCanonical(existing.Category) != MapToCanonical(entry.Category) {
			continue
		}
		if !upsertContentDuplicateOwnerMatches(existing.OwnerID, entry.OwnerID) {
			continue
		}
		return fmt.Errorf("memory_store: duplicate content (matches entry %q)", existing.ID)
	}

	return s.insertPreparedEntryLocked(entry, hash, now, false)
}

func (s *Store) updateEntryFromUpsert(id string, desired Entry) error {
	content := strings.TrimSpace(desired.Content)
	if content == "" {
		return fmt.Errorf("memory_store: content must not be empty")
	}
	if err := ScanForInjection(content); err != nil {
		return fmt.Errorf("memory_store: rejected: %w", err)
	}
	desired.Content = redactSecretsInMemory(content)

	desiredOwner := strings.TrimSpace(desired.OwnerID)
	desiredCategory := MapToCanonical(desired.Category)
	s.mu.RLock()
	var updated Entry
	found := false
	duplicateID := ""
	for _, e := range s.entries {
		if e.ID == id || strings.TrimSpace(e.Content) != desired.Content {
			continue
		}
		if desired.Category != "" && MapToCanonical(e.Category) != desiredCategory {
			continue
		}
		if !upsertContentDuplicateOwnerMatches(e.OwnerID, desiredOwner) {
			continue
		}
		duplicateID = e.ID
		break
	}

	if duplicateID == "" {
		for _, e := range s.entries {
			if e.ID != id {
				continue
			}
			updated = e
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if duplicateID != "" {
		return fmt.Errorf("memory_store: duplicate content (matches entry %q)", duplicateID)
	}
	if !found {
		return fmt.Errorf("memory_store: entry %q not found", id)
	}

	updated.Content = desired.Content
	updated.Category = desired.Category
	updated.Tags = append([]string(nil), desired.Tags...)
	if title := strings.TrimSpace(desired.Title); title != "" {
		updated.Title = title
	}
	if sourceType := strings.TrimSpace(desired.SourceType); sourceType != "" {
		updated.SourceType = sourceType
	}
	if sourceURL := strings.TrimSpace(desired.SourceURL); sourceURL != "" {
		updated.SourceURL = sourceURL
	}
	if desired.Scope != "" {
		updated.Scope = desired.Scope
	}
	if ownerID := strings.TrimSpace(desired.OwnerID); ownerID != "" {
		updated.OwnerID = ownerID
	}
	if desired.Level != LevelNone {
		updated.Level = desired.Level
	}
	if desired.Interval != nil {
		updated.Interval = cloneTimeInterval(desired.Interval)
	}
	if len(desired.EvidenceIDs) > 0 {
		updated.EvidenceIDs = append([]string(nil), desired.EvidenceIDs...)
	}
	if len(desired.RelatedIDs) > 0 {
		updated.RelatedIDs = append([]string(nil), desired.RelatedIDs...)
	}
	if len(desired.RelatedEdges) > 0 {
		updated.RelatedEdges = append([]RelatedEdge(nil), desired.RelatedEdges...)
	}
	if derivedKind := strings.TrimSpace(desired.DerivedKind); derivedKind != "" {
		updated.DerivedKind = derivedKind
	}
	if desired.Boundary != nil {
		updated.Boundary = cloneMemoryBoundary(desired.Boundary)
	}
	updated.CompactForm = ""
	updated.ContentHash = computeContentHash(desired.Content)
	updated.Stale = false
	if updated.Status == StatusDormant && IsDurableTaskManagementEntry(&updated) {
		updated.Status = StatusActive
	}
	if err := s.UpdateEntriesByID([]Entry{updated}); err != nil {
		return fmt.Errorf("memory_store: persist updated entry: %w", err)
	}
	return nil
}

func (s *Store) findUpsertDuplicateByContent(desired Entry) *Entry {
	if s == nil {
		return nil
	}
	content := strings.TrimSpace(desired.Content)
	if content == "" {
		return nil
	}
	if err := ScanForInjection(content); err != nil {
		return nil
	}
	content = redactSecretsInMemory(content)
	desiredCategory := MapToCanonical(desired.Category)
	desiredOwner := strings.TrimSpace(desired.OwnerID)
	desiredHash := computeContentHash(content)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.entries {
		entry := s.entries[i]
		if strings.TrimSpace(entry.ID) == strings.TrimSpace(desired.ID) {
			continue
		}
		if desired.Category != "" && MapToCanonical(entry.Category) != desiredCategory {
			continue
		}
		if !upsertContentDuplicateOwnerMatches(entry.OwnerID, desiredOwner) {
			continue
		}
		if entry.ContentHash == desiredHash || strings.TrimSpace(entry.Content) == content || isDuplicateContentCandidate(strings.ToLower(content), desired.Entities, entry) {
			cp := entry
			return &cp
		}
	}
	return nil
}
func upsertEntryEquivalent(current, desired Entry) bool {
	if strings.TrimSpace(current.Content) != strings.TrimSpace(desired.Content) || current.Category != desired.Category || !sameStringSet(current.Tags, desired.Tags) {
		return false
	}
	if strings.TrimSpace(desired.Title) != "" && strings.TrimSpace(current.Title) != strings.TrimSpace(desired.Title) {
		return false
	}
	if strings.TrimSpace(desired.SourceType) != "" && strings.TrimSpace(current.SourceType) != strings.TrimSpace(desired.SourceType) {
		return false
	}
	if strings.TrimSpace(desired.SourceURL) != "" && strings.TrimSpace(current.SourceURL) != strings.TrimSpace(desired.SourceURL) {
		return false
	}
	if desired.Scope != "" && current.Scope != desired.Scope {
		return false
	}
	if strings.TrimSpace(desired.OwnerID) != "" && strings.TrimSpace(current.OwnerID) != strings.TrimSpace(desired.OwnerID) {
		return false
	}
	if desired.Level != LevelNone && current.Level != desired.Level {
		return false
	}
	if desired.Interval != nil && !sameTimeInterval(current.Interval, desired.Interval) {
		return false
	}
	if len(desired.EvidenceIDs) > 0 && !sameStringSet(current.EvidenceIDs, desired.EvidenceIDs) {
		return false
	}
	if len(desired.RelatedIDs) > 0 && !sameStringSet(current.RelatedIDs, desired.RelatedIDs) {
		return false
	}
	if len(desired.RelatedEdges) > 0 && !sameRelatedEdges(current.RelatedEdges, desired.RelatedEdges) {
		return false
	}
	if strings.TrimSpace(desired.DerivedKind) != "" && strings.TrimSpace(current.DerivedKind) != strings.TrimSpace(desired.DerivedKind) {
		return false
	}
	if desired.Boundary != nil && !sameMemoryBoundary(current.Boundary, desired.Boundary) {
		return false
	}
	return true
}

func cloneTimeInterval(in *TimeInterval) *TimeInterval {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func cloneMemoryBoundary(in *MemoryBoundary) *MemoryBoundary {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func cloneStabilityMeta(in *StabilityMeta) *StabilityMeta {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func applyEntryMutationFields(current *Entry, desired Entry, now time.Time) {
	contentChanged := current.Content != desired.Content
	current.Content = desired.Content
	current.Category = desired.Category
	current.Tags = append([]string(nil), desired.Tags...)
	current.Entities = append([]string(nil), desired.Entities...)
	if contentChanged && desired.CompactForm == current.CompactForm {
		current.CompactForm = ""
	} else {
		current.CompactForm = desired.CompactForm
	}
	current.ContentHash = computeContentHash(desired.Content)
	current.Embedding = append([]float32(nil), desired.Embedding...)
	current.UpdatedAt = now
	current.AccessCount = desired.AccessCount
	current.Strength = desired.Strength
	current.Status = desired.Status
	current.Pinned = desired.Pinned
	current.Stale = desired.Stale
	current.Title = desired.Title
	current.Scope = desired.Scope
	current.OwnerID = desired.OwnerID
	current.SourceType = desired.SourceType
	current.SourceURL = desired.SourceURL
	current.EvidenceIDs = append([]string(nil), desired.EvidenceIDs...)
	current.RelatedIDs = append([]string(nil), desired.RelatedIDs...)
	current.RelatedEdges = append([]RelatedEdge(nil), desired.RelatedEdges...)
	current.DerivedKind = desired.DerivedKind
	current.Boundary = cloneMemoryBoundary(desired.Boundary)
	current.Level = desired.Level
	current.Interval = cloneTimeInterval(desired.Interval)
	current.ParentID = desired.ParentID
	current.ChildIDs = append([]string(nil), desired.ChildIDs...)
	current.ValidAt = cloneTimePtr(desired.ValidAt)
	current.InvalidAt = cloneTimePtr(desired.InvalidAt)
	current.Stability = cloneStabilityMeta(desired.Stability)
}

func sameTimeInterval(a, b *TimeInterval) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Start.Equal(b.Start) && a.End.Equal(b.End)
}

func sameMemoryBoundary(a, b *MemoryBoundary) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.ProjectPath != b.ProjectPath || a.OwnerID != b.OwnerID || a.TaskType != b.TaskType || a.Workflow != b.Workflow || a.Toolchain != b.Toolchain || a.SourceScope != b.SourceScope {
		return false
	}
	if (a.Since == nil) != (b.Since == nil) || (a.Until == nil) != (b.Until == nil) {
		return false
	}
	if a.Since != nil && !a.Since.Equal(*b.Since) {
		return false
	}
	if a.Until != nil && !a.Until.Equal(*b.Until) {
		return false
	}
	return true
}

func upsertMatchedIDHasBoundary(s *Store, id string) bool {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return false
	}
	for _, entry := range s.SearchDirectByID(id) {
		return entry.Boundary != nil
	}
	return false
}
func upsertDesiredForDuplicate(duplicate Entry, desired Entry, merge func(existing, desired []string) []string) Entry {
	desired.Tags = upsertMergedDuplicateTags(duplicate.Tags, desired.Tags, merge)
	if strings.TrimSpace(duplicate.OwnerID) == "" && strings.TrimSpace(desired.OwnerID) != "" {
		desired.OwnerID = ""
	}
	return desired
}

func upsertMergedDuplicateTags(existing, desired []string, merge func(existing, desired []string) []string) []string {
	if merge != nil {
		return normalizeUpsertTags(merge(existing, desired))
	}
	return normalizeUpsertTags(mergeTags(existing, desired))
}

func upsertContentDuplicateOwnerMatches(entryOwnerID, desiredOwnerID string) bool {
	// Owner matching is asymmetric: owner-scoped writes may repair shared
	// canonical records, but shared writes must not absorb private records.
	desiredOwnerID = strings.TrimSpace(desiredOwnerID)
	entryOwnerID = strings.TrimSpace(entryOwnerID)
	if desiredOwnerID == "" {
		return entryOwnerID == ""
	}
	return entryOwnerID == "" || entryOwnerID == desiredOwnerID
}

func upsertOwnerMatches(entryOwnerID, desiredOwnerID string) bool {
	desiredOwnerID = strings.TrimSpace(desiredOwnerID)
	entryOwnerID = strings.TrimSpace(entryOwnerID)
	if desiredOwnerID == "" {
		return entryOwnerID == ""
	}
	return entryOwnerID == desiredOwnerID
}

func normalizeUpsertTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func entryHasAllTags(tags []string, targets ...string) bool {
	if len(targets) == 0 {
		return true
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		seen[tag] = true
	}
	for _, target := range targets {
		if !seen[target] {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	a = normalizeUpsertTags(a)
	b = normalizeUpsertTags(b)
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}
