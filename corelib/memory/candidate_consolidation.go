package memory

import (
	"context"
	"sort"
	"strings"
	"time"
)

const memoryCandidateTag = "memory_candidate"

const memoryCandidateRejectAfter = 7 * 24 * time.Hour

// CandidateConsolidationResult summarizes one offline pass over quarantined
// memory candidates.
type CandidateConsolidationResult struct {
	Scanned  int      `json:"scanned"`
	Promoted int      `json:"promoted"`
	Merged   int      `json:"merged"`
	Rejected int      `json:"rejected"`
	Kept     int      `json:"kept"`
	Errors   []string `json:"errors,omitempty"`
}

// MemoryCandidateSnapshot is a read-only view of a quarantined candidate and
// its current governance assessment.
type MemoryCandidateSnapshot struct {
	Entry    Entry                    `json:"entry"`
	Decision MemoryGovernanceDecision `json:"decision"`
}

// MemoryCandidateHealth summarizes the current quarantine queue.
type MemoryCandidateHealth struct {
	Total      int `json:"total"`
	Accept     int `json:"accept"`
	Quarantine int `json:"quarantine"`
	Reject     int `json:"reject"`
	Stale      int `json:"stale"`
}

// MemoryCandidateHealth returns aggregate governance status for quarantined candidates.
func (s *Store) MemoryCandidateHealth() MemoryCandidateHealth {
	var health MemoryCandidateHealth
	for _, candidate := range s.ListMemoryCandidates("", 0) {
		health.Total++
		switch candidate.Decision.Action {
		case MemoryGovernanceAccept:
			health.Accept++
		case MemoryGovernanceQuarantine:
			health.Quarantine++
		case MemoryGovernanceReject:
			health.Reject++
		}
		if candidate.Entry.Stale {
			health.Stale++
		}
	}
	return health
}

// ListMemoryCandidates returns dormant write-governed candidates for inspection.
func (s *Store) ListMemoryCandidates(keyword string, limit int) []MemoryCandidateSnapshot {
	if s == nil {
		return nil
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]MemoryCandidateSnapshot, 0)
	for _, entry := range s.entries {
		if !isDormantMemoryCandidate(entry) {
			continue
		}
		if kw != "" && !containsKeyword(entry, kw) {
			continue
		}
		result = append(result, MemoryCandidateSnapshot{
			Entry:    entry,
			Decision: AssessMemoryCandidate(entry, ""),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		a := result[i].Entry.UpdatedAt
		b := result[j].Entry.UpdatedAt
		if !a.Equal(b) {
			return a.After(b)
		}
		return result[i].Entry.ID < result[j].Entry.ID
	})
	if limit > 0 && len(result) > limit {
		result = append([]MemoryCandidateSnapshot(nil), result[:limit]...)
	}
	return result
}

// ConsolidateMemoryCandidates revisits dormant write-governed candidates during
// background maintenance. It is deterministic and deliberately conservative:
// strong candidates are promoted, obvious duplicates merge into active memory,
// and low-value rejected candidates are marked stale instead of being deleted.
func (s *Store) ConsolidateMemoryCandidates(ctx context.Context) CandidateConsolidationResult {
	var result CandidateConsolidationResult
	if s == nil {
		return result
	}

	now := time.Now()
	for _, snapshot := range s.ListMemoryCandidates("", 0) {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		candidate := snapshot.Entry
		if !isDormantMemoryCandidate(candidate) {
			continue
		}
		result.Scanned++

		if s.consolidateCandidateDuplicate(candidate.ID, now, &result) {
			continue
		}

		decision := AssessMemoryCandidate(candidate, "")
		switch decision.Action {
		case MemoryGovernanceAccept:
			updated := candidate
			updated.Status = StatusActive
			updated.Stale = false
			updated.Tags = removeTag(updated.Tags, memoryCandidateTag)
			if updated.SourceType == memoryCandidateTag {
				updated.SourceType = "memory_governed"
			}
			if updated.Strength < 1.0 {
				updated.Strength = 1.0
			}
			updated.UpdatedAt = now
			if err := s.UpdateEntriesByID([]Entry{updated}); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Promoted++
		case MemoryGovernanceReject:
			if shouldRejectCandidate(candidate, decision, now) {
				updated := candidate
				updated.Stale = true
				updated.UpdatedAt = now
				if err := s.UpdateEntriesByID([]Entry{updated}); err != nil {
					result.Errors = append(result.Errors, err.Error())
					continue
				}
				result.Rejected++
			} else {
				result.Kept++
			}
		default:
			result.Kept++
		}
	}
	return result
}

func (s *Store) consolidateCandidateDuplicate(candidateID string, now time.Time, result *CandidateConsolidationResult) bool {
	s.mu.RLock()
	candidateIdx := -1
	for i := range s.entries {
		if s.entries[i].ID == candidateID {
			candidateIdx = i
			break
		}
	}
	if candidateIdx < 0 || !isDormantMemoryCandidate(s.entries[candidateIdx]) {
		s.mu.RUnlock()
		return false
	}
	targetIdx := s.findCandidateMergeTargetLocked(candidateIdx)
	if targetIdx < 0 {
		s.mu.RUnlock()
		return false
	}
	candidate := s.entries[candidateIdx]
	target := s.entries[targetIdx]
	s.mu.RUnlock()

	target.Tags = mergeTags(target.Tags, removeTag(candidate.Tags, memoryCandidateTag))
	target.Entities = mergeTags(target.Entities, candidate.Entities)
	target.UpdatedAt = now
	target.AccessCount++
	if target.Strength < 1.0 {
		target.Strength = 1.0
	}
	if err := s.UpdateEntriesAndDeleteIDs([]Entry{target}, []string{candidate.ID}); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return true
	}
	result.Merged++
	return true
}

func isDormantMemoryCandidate(entry Entry) bool {
	return entry.Status == StatusDormant && hasTag(entry.Tags, memoryCandidateTag)
}

func shouldRejectCandidate(entry Entry, decision MemoryGovernanceDecision, now time.Time) bool {
	if decision.Score <= 0 {
		return true
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = entry.UpdatedAt
	}
	return !createdAt.IsZero() && now.Sub(createdAt) >= memoryCandidateRejectAfter
}

func (s *Store) findCandidateMergeTargetLocked(candidateIdx int) int {
	candidate := s.entries[candidateIdx]
	for i := range s.entries {
		if i == candidateIdx {
			continue
		}
		active := s.entries[i]
		if !active.IsActive() || active.OwnerID != candidate.OwnerID {
			continue
		}
		if MapToCanonical(active.Category) != MapToCanonical(candidate.Category) {
			continue
		}
		if memoryCandidateDuplicate(active, candidate) {
			return i
		}
	}
	return -1
}

func memoryCandidateDuplicate(active, candidate Entry) bool {
	a := normalizeCandidateText(active.Content)
	b := normalizeCandidateText(candidate.Content)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	shorter, longer := a, b
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	return len([]rune(shorter)) >= 24 && strings.Contains(longer, shorter)
}

func normalizeCandidateText(content string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(content))), " ")
}

func hasTag(tags []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(tag)) == target {
			return true
		}
	}
	return false
}

func removeTag(tags []string, target string) []string {
	target = strings.ToLower(strings.TrimSpace(target))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(tag)) == target {
			continue
		}
		out = append(out, tag)
	}
	return out
}
