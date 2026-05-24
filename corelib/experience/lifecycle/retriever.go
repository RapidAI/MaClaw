package lifecycle

import (
	"sort"
	"strconv"
	"strings"
)

// SelectBalancedCandidates applies the shared balanced retrieval selection over
// already-scored candidates. Providers are responsible for producing typed
// candidates; this function owns quota, rerank, redundancy, and backfill.
func SelectBalancedCandidates(candidates []Candidate, decision RetrievalDecision) []Candidate {
	if len(candidates) == 0 {
		return nil
	}
	limit := decision.Budget.MaxEntries
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	quotas := decision.Budget.Quotas
	if len(quotas) == 0 {
		quotas = DefaultRetrievalQuotas()
	}
	types := decision.Types
	if len(types) == 0 {
		types = DefaultRetrievalTypes()
	}

	ranked := append([]Candidate(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return CandidateScore(ranked[i]) > CandidateScore(ranked[j])
	})

	selected := make([]Candidate, 0, limit)
	selectedKeys := map[string]struct{}{}
	for _, entryType := range types {
		quota := quotas[entryType]
		if quota <= 0 {
			continue
		}
		for idx, candidate := range ranked {
			if len(selected) >= limit || quota <= 0 {
				break
			}
			if candidate.Entry.EntryType != entryType || candidateSelected(candidate, idx, selectedKeys) {
				continue
			}
			if CandidateRedundant(candidate, selected) {
				continue
			}
			selected = append(selected, candidate)
			markCandidateSelected(candidate, idx, selectedKeys)
			quota--
		}
	}
	for idx, candidate := range ranked {
		if len(selected) >= limit {
			break
		}
		if candidateSelected(candidate, idx, selectedKeys) || CandidateRedundant(candidate, selected) {
			continue
		}
		selected = append(selected, candidate)
		markCandidateSelected(candidate, idx, selectedKeys)
	}
	for idx, candidate := range ranked {
		if len(selected) >= limit {
			break
		}
		if candidateSelected(candidate, idx, selectedKeys) {
			continue
		}
		selected = append(selected, candidate)
		markCandidateSelected(candidate, idx, selectedKeys)
	}
	return selected
}

func CandidateScore(candidate Candidate) float64 {
	return candidate.Relevance + candidate.PriorityScore + candidate.BoundaryScore - candidateTokenPenalty(candidate.TokenCost)
}

func CandidateRedundant(candidate Candidate, selected []Candidate) bool {
	for _, existing := range selected {
		if candidate.Entry.EntryType != existing.Entry.EntryType {
			continue
		}
		if candidateContentSimilarity(candidate, existing) >= 0.72 {
			return true
		}
	}
	return false
}

func candidateContentSimilarity(a Candidate, b Candidate) float64 {
	aTokens := candidateTextTokens(a.Entry.Content)
	bTokens := candidateTextTokens(b.Entry.Content)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			intersection++
		}
	}
	union := len(aTokens) + len(bTokens) - intersection
	if union <= 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func candidateTextTokens(text string) map[string]struct{} {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '/' || r == '.' || r == ':' || r == '\\' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || r > 127)
	})
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "-_./:\\")
		if len([]rune(field)) < 2 {
			continue
		}
		out[field] = struct{}{}
	}
	return out
}

func candidateTokenPenalty(tokenCost int) float64 {
	if tokenCost <= 0 {
		return 0
	}
	if tokenCost > 800 {
		return 0.4
	}
	return float64(tokenCost) / 2000
}

func candidateSelected(candidate Candidate, rank int, selected map[string]struct{}) bool {
	_, ok := selected[candidateKey(candidate, rank)]
	return ok
}

func markCandidateSelected(candidate Candidate, rank int, selected map[string]struct{}) {
	selected[candidateKey(candidate, rank)] = struct{}{}
}

func candidateKey(candidate Candidate, rank int) string {
	if candidate.Entry.ID != "" {
		return "id:" + candidate.Entry.ID
	}
	return "rank:" + strconv.Itoa(rank)
}
