package skill

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// ComparativeTrajectoryPair pairs the path that worked with the path that
// failed for the same kind of task. Evidence IDs keep every claim traceable
// back to lifecycle events (trace ids, repair evidence ids, recovery events).
type ComparativeTrajectoryPair struct {
	TaskType            string   `json:"task_type,omitempty"`
	WhenToUse           string   `json:"when_to_use,omitempty"`
	PositivePath        string   `json:"positive_path"`
	NegativePath        string   `json:"negative_path"`
	PositiveEvidenceIDs []string `json:"positive_evidence_ids,omitempty"`
	NegativeEvidenceIDs []string `json:"negative_evidence_ids,omitempty"`
	RepairEvidenceIDs   []string `json:"repair_evidence_ids,omitempty"`
	Toolchain           []string `json:"toolchain,omitempty"`
	ProjectPath         string   `json:"project_path,omitempty"`
}

// ComparativeDistillerOptions controls how conservative the distiller is.
type ComparativeDistillerOptions struct {
	// MinPairs is the minimum number of corroborating pairs per task type
	// before a draft is emitted. Default: 1 (each pair is already curated
	// counterfactual/repair evidence; governance review is the real gate).
	MinPairs int
}

// DistillComparativeSkillDrafts aggregates paired success/failure trajectories
// and repair evidence into comparative_skill drafts. Drafts are governance
// drafts only — they enter the review queue and never become strong execution
// rules without an explicit merge through skill maintenance.
func DistillComparativeSkillDrafts(pairs []ComparativeTrajectoryPair, opts ComparativeDistillerOptions) []lifecycle.Entry {
	if opts.MinPairs <= 0 {
		opts.MinPairs = 1
	}
	type accum struct {
		pair      ComparativeTrajectoryPair
		count     int
		positive  map[string]struct{}
		negative  map[string]struct{}
		evidence  map[string]struct{}
		toolchain map[string]struct{}
	}
	groups := map[string]*accum{}
	for _, pair := range pairs {
		pair.PositivePath = strings.TrimSpace(pair.PositivePath)
		pair.NegativePath = strings.TrimSpace(pair.NegativePath)
		if pair.PositivePath == "" || pair.NegativePath == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(pair.TaskType))
		if key == "" {
			key = "general"
		}
		acc, ok := groups[key]
		if !ok {
			acc = &accum{
				pair:      pair,
				positive:  map[string]struct{}{},
				negative:  map[string]struct{}{},
				evidence:  map[string]struct{}{},
				toolchain: map[string]struct{}{},
			}
			groups[key] = acc
		}
		acc.count++
		for _, id := range pair.PositiveEvidenceIDs {
			if id = strings.TrimSpace(id); id != "" {
				acc.positive[id] = struct{}{}
				acc.evidence[id] = struct{}{}
			}
		}
		for _, id := range pair.NegativeEvidenceIDs {
			if id = strings.TrimSpace(id); id != "" {
				acc.negative[id] = struct{}{}
				acc.evidence[id] = struct{}{}
			}
		}
		for _, id := range pair.RepairEvidenceIDs {
			if id = strings.TrimSpace(id); id != "" {
				acc.evidence[id] = struct{}{}
			}
		}
		for _, tool := range pair.Toolchain {
			if tool = strings.TrimSpace(tool); tool != "" {
				acc.toolchain[tool] = struct{}{}
			}
		}
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	drafts := make([]lifecycle.Entry, 0, len(keys))
	for _, key := range keys {
		acc := groups[key]
		if acc.count < opts.MinPairs {
			continue
		}
		taskType := strings.TrimSpace(acc.pair.TaskType)
		if taskType == "" {
			taskType = "general"
		}
		whenToUse := strings.TrimSpace(acc.pair.WhenToUse)
		if whenToUse == "" {
			whenToUse = fmt.Sprintf("当 %s 类任务出现失败/重试分歧时，参考正反路径选择更稳的做法。", taskType)
		}
		content := strings.Join([]string{
			"Comparative skill draft",
			"Positive path: " + acc.pair.PositivePath,
			"Negative path: " + acc.pair.NegativePath,
			fmt.Sprintf("Evidence pairs: %d", acc.count),
			"Governance: draft only; review required before promotion into the skill library.",
		}, "\n")
		drafts = append(drafts, lifecycle.Entry{
			ID:                  comparativeDraftID(taskType),
			EntryType:           lifecycle.EntryTypeComparativeSkill,
			WhenToUse:           whenToUse,
			Content:             content,
			SourceType:          "comparative_distiller",
			EvidenceIDs:         sortedSet(acc.evidence),
			PositivePath:        acc.pair.PositivePath,
			NegativePath:        acc.pair.NegativePath,
			PositiveEvidenceIDs: sortedSet(acc.positive),
			NegativeEvidenceIDs: sortedSet(acc.negative),
			Boundary: lifecycle.Boundary{
				TaskType:    taskType,
				ProjectPath: strings.TrimSpace(acc.pair.ProjectPath),
				Toolchain:   sortedSet(acc.toolchain),
				SourceScope: "comparative_distiller",
			},
			Priority:   0.5,
			Governance: lifecycle.GovernanceDraft,
		})
	}
	return drafts
}

func comparativeDraftID(taskType string) string {
	slug := strings.ToLower(strings.TrimSpace(taskType))
	var b strings.Builder
	lastDash := false
	for _, r := range slug {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug = strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "general"
	}
	return "comparative_draft:" + slug
}

func sortedSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ComparativeDraftProvider exposes distilled comparative skill drafts through
// the experience governance draft queue, mirroring GovernanceDraftProvider: it
// is read-only evidence for the Balanced Retriever and review tooling, and its
// UpdateUtility is a no-op because drafts never mutate skill state directly.
type ComparativeDraftProvider struct {
	Drafts []lifecycle.Entry
}

func NewComparativeDraftProvider(pairs []ComparativeTrajectoryPair, opts ComparativeDistillerOptions) ComparativeDraftProvider {
	return ComparativeDraftProvider{Drafts: DistillComparativeSkillDrafts(pairs, opts)}
}

func (p ComparativeDraftProvider) ListExperience(_ context.Context, scope lifecycle.Scope) ([]lifecycle.Entry, error) {
	out := make([]lifecycle.Entry, 0, len(p.Drafts))
	for _, draft := range p.Drafts {
		if !skillExperienceTypeAllowed(draft.EntryType, scope.Types) {
			continue
		}
		out = append(out, draft)
		if scope.Limit > 0 && len(out) >= scope.Limit {
			break
		}
	}
	return out, nil
}

func (p ComparativeDraftProvider) SearchExperience(_ context.Context, query lifecycle.Query) ([]lifecycle.Candidate, error) {
	queryText := strings.TrimSpace(query.Text)
	if queryText == "" {
		return nil, nil
	}
	queryTokens := skillExperienceTokens(queryText)
	candidates := make([]lifecycle.Candidate, 0, len(p.Drafts))
	for _, draft := range p.Drafts {
		if !skillExperienceTypeAllowed(draft.EntryType, query.Types) {
			continue
		}
		relevance := skillExperienceRelevance(queryTokens, comparativeDraftSearchText(draft))
		if relevance <= 0 {
			continue
		}
		candidates = append(candidates, lifecycle.Candidate{
			Entry:         draft,
			Relevance:     relevance,
			PriorityScore: draft.Priority,
			BoundaryScore: governanceDraftBoundaryScore(draft.Boundary, query.Boundary),
			TokenCost:     skillExperienceTokenCost(draft.Content),
			Reason:        "comparative_draft_provider",
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return skillExperienceCandidateScore(candidates[i]) > skillExperienceCandidateScore(candidates[j])
	})
	return limitSkillExperienceCandidates(candidates, query.Limit), nil
}

func (p ComparativeDraftProvider) UpdateUtility(_ context.Context, _ lifecycle.UtilityUpdate) error {
	return nil
}

func comparativeDraftSearchText(draft lifecycle.Entry) string {
	return strings.Join([]string{draft.ID, draft.WhenToUse, draft.Content, draft.PositivePath, draft.NegativePath, draft.Boundary.TaskType}, " ")
}
