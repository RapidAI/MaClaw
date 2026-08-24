package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func (h *IMMessageHandler) writeHorizonExperience(ctx context.Context, sess *horizonSession, audit *longhorizon.AuditReport) {
	if h == nil || sess == nil || audit == nil {
		return
	}
	sess.mu.Lock()
	if sess.cancelled || sess.state == nil {
		sess.mu.Unlock()
		return
	}
	if sess.experienceWrites < sess.state.ExperienceWrites {
		sess.experienceWrites = sess.state.ExperienceWrites
	}
	if sess.experienceWrites >= longhorizon.MaxExperiencePerTask {
		sess.mu.Unlock()
		horizonLog(sess, "experience_skip", "reason=cap")
		return
	}
	taskID := sess.state.TaskID
	userGoal := sess.state.UserGoal
	projectRoot := sess.state.Policy.ProjectRoot
	untrusted := sess.state.Policy.Untrusted
	cancelled := sess.cancelled || sess.state.Status == longhorizon.StatusCancelled
	sess.mu.Unlock()

	if !longhorizon.ExperienceEligible(longhorizon.EligibilityInput{
		HorizonTaskID: taskID,
		RoundIndex:    audit.RoundIndex,
		AuditDigest:   audit.Digest,
		Audit:         audit,
		Untrusted:     untrusted,
		Cancelled:     cancelled,
	}) {
		if audit.Mechanical || audit.Status == "complete" || audit.Status == "blocked" || untrusted || cancelled {
			horizonLog(sess, "experience_skip", horizonLogKV(
				"reason=ineligible",
				"status="+audit.Status,
				"integrity="+audit.Integrity,
				"alignment="+audit.Alignment,
				fmt.Sprintf("mechanical=%v", audit.Mechanical),
			))
		}
		return
	}
	if h.app == nil {
		horizonLog(sess, "experience_skip", "reason=no_store")
		return
	}
	store := h.app.ensureCodingKnowledgeStore()
	if store == nil {
		horizonLog(sess, "experience_skip", "reason=no_store")
		return
	}
	summary := longhorizon.SanitizeExperienceText(audit.Summary)
	if strings.TrimSpace(summary) == "" {
		horizonLog(sess, "experience_skip", "reason=empty_summary")
		return
	}
	if sess.isCancelled() {
		horizonLog(sess, "experience_skip", "reason=cancelled")
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		horizonLog(sess, "experience_skip", "reason=ctx_done")
		return
	}
	category := knowledge.CodingCategoryPitfall
	if audit.Status == "complete" && audit.Integrity == "clean" && audit.Alignment == "aligned" {
		category = knowledge.CodingCategoryPattern
	}
	exp := knowledge.CodingExperience{
		Title:            longhorizon.Clip("Horizon round "+fmt.Sprintf("%d", audit.RoundIndex)+": "+userGoal, 120),
		Category:         category,
		Scope:            knowledge.CodingScopeProject,
		ProjectPath:      projectRoot,
		TriggerCondition: longhorizon.Clip(userGoal, 200),
		Content:          summary,
		Labels: []string{
			"horizon",
			"horizon_task:" + taskID,
			fmt.Sprintf("horizon_round:%d", audit.RoundIndex),
		},
		SourceTaskTitle:        userGoal,
		SourceRuntimeTaskID:    "horizon:" + taskID,
		SourceRuntimeAttemptID: fmt.Sprintf("round-%d", audit.RoundIndex),
		EvidenceDigest:         audit.Digest,
		Status:                 knowledge.CodingStatusCandidate,
		CreatedBy:              "runtime",
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := store.SaveRuntimeExperience(ctx, exp); err != nil {
		horizonLog(sess, "experience_skip", horizonLogKV("reason=save_error", horizonLogField("err", err.Error())))
		return
	}
	sess.mu.Lock()
	if !sess.cancelled {
		sess.experienceWrites++
		if sess.state != nil {
			sess.state.ExperienceWrites = sess.experienceWrites
			sess.persistLocked()
		}
	}
	sess.mu.Unlock()
	horizonLog(sess, "experience_write", horizonLogKV(fmt.Sprintf("round=%d", audit.RoundIndex), "category="+category))
}

func horizonManagerSearchOptions(query, projectRoot string) knowledge.CodingSearchOptions {
	return knowledge.CodingSearchOptions{
		Query:       query,
		ProjectPath: projectRoot,
		Labels:      []string{"horizon"},
		Limit:       longhorizon.MaxExperiencePerTask,
		Status: []string{
			knowledge.CodingStatusCandidate,
			knowledge.CodingStatusActive,
			knowledge.CodingStatusVerified,
		},
	}
}

func formatHorizonManagerEvidence(exps []knowledge.CodingExperience) string {
	parts := make([]string, 0, 2)
	for _, exp := range exps {
		text := longhorizon.SanitizeExperienceText(strings.TrimSpace(exp.Title) + "\n" + strings.TrimSpace(exp.Content))
		if text == "" {
			continue
		}
		parts = append(parts, text)
		if len(parts) >= longhorizon.MaxExperiencePerTask {
			break
		}
	}
	return strings.Join(parts, "\n---\n")
}

func (h *IMMessageHandler) retrieveHorizonManagerEvidence(ctx context.Context, sess *horizonSession, state *longhorizon.TaskState) string {
	if h == nil || h.app == nil || state == nil {
		return ""
	}
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	if sess != nil && sess.isCancelled() {
		return ""
	}
	query := strings.TrimSpace(state.UserGoal)
	if query == "" {
		return ""
	}
	store := h.app.ensureCodingKnowledgeStore()
	if store == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	results, err := store.SearchExperiences(ctx, horizonManagerSearchOptions(query, state.Policy.ProjectRoot))
	if err != nil || len(results) == 0 {
		return ""
	}
	return formatHorizonManagerEvidence(results)
}
