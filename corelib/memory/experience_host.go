package memory

import "sort"

// ExperienceProtectionSnapshot is the read-only host projection for experience
// retention/maintenance surfaces. It lets GUI/TUI inspect protected anchors
// without scanning Store entries or duplicating the protection classifier.
type ExperienceProtectionSnapshot struct {
	DistillResult ExperienceDistillResult
	Candidates    []ProtectedExperienceCandidate
}

// ExperienceProtectionSnapshotForHost returns a non-mutating projection of
// protected experience candidates. Hosts can filter or format the returned
// candidates, but entry scanning and protection classification stay in
// corelib/memory.
func (s *Store) ExperienceProtectionSnapshotForHost() ExperienceProtectionSnapshot {
	if s == nil {
		return ExperienceProtectionSnapshot{}
	}
	entries := s.List("", "")
	distill := NewExperienceDistiller().AnalyzeWithSampleLimit(entries, 0)
	candidates := make([]ProtectedExperienceCandidate, 0, distill.ProtectedCandidates)
	for _, entry := range entries {
		if !entry.IsActive() {
			continue
		}
		candidate, ok := ProtectedExperienceCandidateForEntry(entry)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return ExperienceProtectionSnapshot{DistillResult: distill, Candidates: candidates}
}

// ExperienceDistillForHost returns the read-only distillation summary used by
// host learning/maintenance surfaces.
func (s *Store) ExperienceDistillForHost(sampleLimit int) ExperienceDistillResult {
	if s == nil {
		return ExperienceDistillResult{}
	}
	entries := s.List("", "")
	distiller := NewExperienceDistiller()
	if sampleLimit <= 0 {
		return distiller.Analyze(entries)
	}
	return distiller.AnalyzeWithSampleLimit(entries, sampleLimit)
}

// ExperienceTraceEntriesForHost returns entries sorted for host trace snapshot
// building. The extraction/formatting layer may still choose which entries are
// trace-like, but store scanning and recency sorting stay in corelib/memory.
func (s *Store) ExperienceTraceEntriesForHost() []Entry {
	if s == nil {
		return nil
	}
	entries := s.List("", "")
	sort.SliceStable(entries, func(i, j int) bool {
		return experienceHostEntryMoreRecent(entries[i], entries[j])
	})
	return entries
}

// EntriesWithTagForHost returns active host-visible entries carrying tag. It is
// used by host learning surfaces that need a narrow generated-memory subset.
func (s *Store) EntriesWithTagForHost(tag string) []Entry {
	if s == nil || tag == "" {
		return nil
	}
	entries := s.List("", "")
	out := make([]Entry, 0)
	for _, entry := range entries {
		if entryHasAllTags(entry.Tags, tag) {
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return experienceHostEntryMoreRecent(out[i], out[j])
	})
	return out
}
func experienceHostEntryMoreRecent(a, b Entry) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}
