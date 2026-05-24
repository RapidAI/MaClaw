package memory

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type proactiveRecallCandidate struct {
	entry     Entry
	candidate lifecycle.Candidate
	rank      int
}

func selectBalancedProactiveEntries(entries []Entry, decision lifecycle.RetrievalDecision) []Entry {
	if len(entries) == 0 {
		return nil
	}
	candidates := make([]proactiveRecallCandidate, 0, len(entries))
	for i, entry := range entries {
		candidate, ok := buildProactiveRecallCandidate(entry, i, len(entries), decision)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	entryByID := make(map[string]Entry, len(candidates))
	pool := make([]lifecycle.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		pool = append(pool, candidate.candidate)
		if candidate.entry.ID != "" {
			entryByID[candidate.entry.ID] = candidate.entry
		}
	}
	selected := lifecycle.SelectBalancedCandidates(pool, decision)

	out := make([]Entry, 0, len(selected))
	for _, candidate := range selected {
		if entry, ok := entryByID[candidate.Entry.ID]; ok {
			out = append(out, entry)
		}
	}
	return out
}

func buildProactiveRecallCandidate(entry Entry, rank int, total int, decision lifecycle.RetrievalDecision) (proactiveRecallCandidate, bool) {
	if !proactiveRecallBoundaryAllowed(entry, decision.Boundary) {
		return proactiveRecallCandidate{}, false
	}
	entryType := classifyExperienceEntryType(entry)
	tokenCost := EstimateTextTokens(firstNonEmptyString(entry.CompactForm, entry.Content))
	if tokenCost <= 0 {
		tokenCost = 1
	}
	relevance := proactiveRecallRankRelevance(rank, total)
	priority := proactiveRecallPriorityScore(entry)
	boundary := proactiveRecallBoundaryScore(entry, decision.Boundary)
	return proactiveRecallCandidate{
		entry: entry,
		candidate: lifecycle.Candidate{
			Entry: lifecycle.Entry{
				ID:          entry.ID,
				EntryType:   entryType,
				WhenToUse:   firstNonEmptyString(entry.Title, entry.DerivedKind),
				Content:     firstNonEmptyString(entry.CompactForm, entry.Content),
				SourceType:  entry.SourceType,
				SourceURL:   entry.SourceURL,
				EvidenceIDs: append([]string(nil), entry.EvidenceIDs...),
				Boundary:    memoryBoundaryToLifecycle(entry.Boundary, entry.OwnerID, entry.Scope, entry.Tags),
				Priority:    priority,
			},
			Relevance:     relevance,
			PriorityScore: priority,
			BoundaryScore: boundary,
			TokenCost:     tokenCost,
			Reason:        "proactive_balanced",
		},
		rank: rank,
	}, true
}

func proactiveRecallRankRelevance(rank int, total int) float64 {
	if total <= 1 {
		return 1
	}
	return 1 - (float64(rank) / float64(total))
}

func proactiveRecallPriorityScore(entry Entry) float64 {
	score := 0.0
	if entry.Pinned {
		score += 0.4
	}
	if entry.Strength > 0 {
		strength := entry.Strength
		if strength > 5 {
			strength = 5
		}
		score += strength / 5
	}
	if entry.AccessCount > 0 {
		access := entry.AccessCount
		if access > 10 {
			access = 10
		}
		score += float64(access) / 50
	}
	return score
}

func proactiveRecallBoundaryScore(entry Entry, boundary lifecycle.Boundary) float64 {
	score := 0.0
	if boundary.OwnerID != "" && entry.OwnerID != "" && entry.OwnerID == boundary.OwnerID {
		score += 0.25
	}
	project := semanticNormalizeProjectPath(boundary.ProjectPath)
	if project != "" {
		if entry.Boundary != nil && semanticProjectPathMatches(semanticNormalizeProjectPath(entry.Boundary.ProjectPath), project) {
			score += 0.35
		} else if entry.Scope == ScopeProject && semanticProjectAllowed(entry.Scope, entry.Tags, project) {
			score += 0.25
		}
	}
	return score
}

func proactiveRecallBoundaryAllowed(entry Entry, boundary lifecycle.Boundary) bool {
	if boundary.OwnerID != "" && entry.OwnerID != "" && entry.OwnerID != boundary.OwnerID {
		return false
	}
	project := semanticNormalizeProjectPath(boundary.ProjectPath)
	if project != "" && !recallProjectEntryAllowed(entry, project) {
		return false
	}
	return recallBoundaryAllowed(entry, project, boundary.OwnerID)
}

func memoryBoundaryToLifecycle(boundary *MemoryBoundary, ownerID string, scope Scope, tags []string) lifecycle.Boundary {
	out := lifecycle.Boundary{OwnerID: ownerID}
	if boundary != nil {
		out.ProjectPath = boundary.ProjectPath
		out.TaskType = boundary.TaskType
		out.Workflow = boundary.Workflow
		out.SourceScope = boundary.SourceScope
		if boundary.OwnerID != "" {
			out.OwnerID = boundary.OwnerID
		}
		if boundary.Toolchain != "" {
			out.Toolchain = []string{boundary.Toolchain}
		}
	}
	if out.ProjectPath == "" && scope == ScopeProject {
		for _, tag := range tags {
			if normalized := semanticNormalizeProjectPath(tag); semanticLooksLikePath(normalized) {
				out.ProjectPath = tag
				break
			}
		}
	}
	return out
}

func classifyExperienceEntryType(entry Entry) lifecycle.EntryType {
	text := strings.ToLower(strings.Join(append(append([]string{}, entry.Tags...), entry.Content, entry.Title, entry.SourceType, entry.DerivedKind), " "))
	if hasAnyExperienceMarker(text, "comparative", "positive_path", "negative_path", "instead of", "rather than", "优于", "对比") {
		return lifecycle.EntryTypeComparativeSkill
	}
	if hasAnyExperienceMarker(text, "failure", "failed", "error", "timeout", "retry", "recover", "avoid", "blocked", "失败", "错误", "重试", "规避", "不要") {
		return lifecycle.EntryTypeFailureSkill
	}
	canonical := MapToCanonical(entry.Category)
	if canonical == CategoryInstruction || hasAnyExperienceMarker(text, "success", "workflow", "skill", "pattern", "should", "must", "成功", "步骤", "流程") {
		return lifecycle.EntryTypeSuccessSkill
	}
	if canonical == CategoryConversationSummary || canonical == CategorySessionCheckpoint || canonical == CategoryTaskArtifact || hasAnyExperienceMarker(text, "tool_usage", "group_discussion", "trace", "session") {
		return lifecycle.EntryTypeEpisodic
	}
	return lifecycle.EntryTypeFactual
}

func hasAnyExperienceMarker(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
