package memory

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// ThemeMaintenanceResult reports the outcome of applying safe maintenance.
type ThemeMaintenanceResult struct {
	Plan                 ThemeMaintenancePlan `json:"plan"`
	Before               ThemeHealth          `json:"before"`
	After                ThemeHealth          `json:"after"`
	RequestedActions     int                  `json:"requested_actions"`
	AppliedActions       []string             `json:"applied_actions,omitempty"`
	SkippedActions       []string             `json:"skipped_actions,omitempty"`
	BackfilledEmbeddings int                  `json:"backfilled_embeddings,omitempty"`
	RebuiltThemes        bool                 `json:"rebuilt_themes"`
	Errors               []string             `json:"errors,omitempty"`
}

// ApplyThemeMaintenancePlan applies only conservative, non-destructive theme
// maintenance: synchronous embedding backfill when an embedder is available,
// then a theme-layer rebuild. It never edits memory content or manually splits
// themes.
func (s *Store) ApplyThemeMaintenancePlan(issueLimit int, actionLimit int) ThemeMaintenanceResult {
	report := s.ThemeDiagnostics(issueLimit)
	plan := PlanThemeMaintenance(report, actionLimit)
	result := ThemeMaintenanceResult{
		Plan:             plan,
		Before:           report.Health,
		RequestedActions: len(plan.Actions),
	}
	if s == nil || s.themeManager == nil {
		result.After = result.Before
		result.Errors = append(result.Errors, "memory store or theme manager is not initialized")
		return result
	}

	needsBackfill := false
	for _, action := range plan.Actions {
		switch action.Action {
		case "backfill_theme_inputs":
			needsBackfill = true
		case "review_split_theme", "review_isolated_theme", "deduplicate_theme_membership":
			result.SkippedActions = append(result.SkippedActions, action.Action+":manual_review_required")
		default:
			result.SkippedActions = append(result.SkippedActions, action.Action+":unsupported")
		}
	}

	if needsBackfill {
		updated, err := s.backfillThemeMaintenanceEmbeddings()
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.SkippedActions = append(result.SkippedActions, "backfill_theme_inputs:"+err.Error())
		} else {
			result.BackfilledEmbeddings = updated
			if updated > 0 {
				result.AppliedActions = append(result.AppliedActions, "backfill_theme_inputs")
			} else {
				result.SkippedActions = append(result.SkippedActions, "backfill_theme_inputs:no_missing_embeddings_or_embedder")
			}
		}
	}

	entries := s.List("", "")
	s.themeManager.MarkDirty()
	s.themeManager.EnsureUpToDate(entries, nil)
	result.RebuiltThemes = true
	result.AppliedActions = append(result.AppliedActions, "rebuild_theme_layer")
	result.After = s.ThemeHealth()
	return result
}

func (s *Store) backfillThemeMaintenanceEmbeddings() (int, error) {
	s.mu.RLock()
	emb := s.embedder
	gen := s.embedderGen
	type pending struct {
		id      string
		content string
	}
	var todo []pending
	for _, e := range s.entries {
		if !e.IsActive() || !themeEntryAllowed(e) || len(e.Embedding) > 0 || strings.TrimSpace(e.Content) == "" {
			continue
		}
		todo = append(todo, pending{id: e.ID, content: e.Content})
	}
	s.mu.RUnlock()

	if emb == nil || embedding.IsNoop(emb) {
		return 0, fmt.Errorf("no active embedder")
	}
	if len(todo) == 0 {
		return 0, nil
	}

	type computed struct {
		id  string
		vec []float32
	}
	var computedEmbeddings []computed
	for _, item := range todo {
		vec, err := emb.Embed(item.content)
		if err != nil || len(vec) == 0 {
			continue
		}
		computedEmbeddings = append(computedEmbeddings, computed{id: item.id, vec: vec})
	}
	if len(computedEmbeddings) == 0 {
		return 0, nil
	}

	updates := make([]Entry, 0, len(computedEmbeddings))
	s.mu.RLock()
	if s.embedderGen != gen {
		s.mu.RUnlock()
		return 0, fmt.Errorf("embedder changed during maintenance")
	}
	for _, item := range computedEmbeddings {
		for i := range s.entries {
			if s.entries[i].ID != item.id || len(s.entries[i].Embedding) > 0 {
				continue
			}
			updated := s.entries[i]
			updated.Embedding = append([]float32(nil), item.vec...)
			updates = append(updates, updated)
			break
		}
	}
	s.mu.RUnlock()
	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			return 0, err
		}
	}
	return len(updates), nil
}
