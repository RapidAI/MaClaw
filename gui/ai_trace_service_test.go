package main

import (
	"strings"
	"testing"
)

func TestAITraceServiceStartAndGetTrace(t *testing.T) {
	svc := NewAITraceService()
	job, run := svc.StartJobRun(TraceJobKindAIAssistant, "Fix auth bug", "desktop", "u1", "/project")
	if job == nil || run == nil {
		t.Fatal("expected job and run")
	}
	if job.JobID == "" || run.RunID == "" {
		t.Fatal("expected IDs to be assigned")
	}
	if run.JobID != job.JobID {
		t.Fatalf("run.JobID = %q, want %q", run.JobID, job.JobID)
	}
	if run.ProjectPath != "/project" {
		t.Fatalf("run.ProjectPath = %q, want /project", run.ProjectPath)
	}

	svc.AppendEvent(run.RunID, TraceEvent{Kind: "request.accepted", Title: "Accepted", Summary: "Accepted user request"})
	svc.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "ai_event", Category: "result", Summary: "Drafted plan", ContentSnippet: "Plan created"})
	svc.UpdateRun(run.RunID, TraceRunStatusCompleted, "Completed successfully", "")

	view, ok := svc.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if view.Status != TraceRunStatusCompleted {
		t.Fatalf("view.Status = %q, want %q", view.Status, TraceRunStatusCompleted)
	}
	if view.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", view.EventCount)
	}
	if view.EvidenceCount != 1 {
		t.Fatalf("EvidenceCount = %d, want 1", view.EvidenceCount)
	}
	if len(view.Events) != 1 || len(view.Evidence) != 1 {
		t.Fatalf("unexpected trace lengths: events=%d evidence=%d", len(view.Events), len(view.Evidence))
	}
}

func TestAITraceServiceRecallEvidencePrefersProjectAndMatches(t *testing.T) {
	svc := NewAITraceService()
	_, run1 := svc.StartJobRun(TraceJobKindRemoteSession, "Backend task", "remote", "u1", "/project-a")
	_, run2 := svc.StartJobRun(TraceJobKindRemoteSession, "Other task", "remote", "u1", "/project-b")

	svc.AppendEvidence(run1.RunID, EvidenceRecord{
		SourceKind:     "remote_event",
		Category:       "error",
		Summary:        "database migration failed",
		ContentSnippet: "migration failed because column user_id already exists",
		ProjectPath:    "/project-a",
	})
	svc.AppendEvidence(run2.RunID, EvidenceRecord{
		SourceKind:     "remote_event",
		Category:       "result",
		Summary:        "frontend lint passed",
		ContentSnippet: "all checks passed",
		ProjectPath:    "/project-b",
	})

	results := svc.RecallEvidence("/project-a", "migration failed", 3)
	if len(results) == 0 {
		t.Fatal("expected evidence results")
	}
	if results[0].ProjectPath != "/project-a" {
		t.Fatalf("top result ProjectPath = %q, want /project-a", results[0].ProjectPath)
	}
	if results[0].Category != "error" {
		t.Fatalf("top result Category = %q, want error", results[0].Category)
	}
}

func TestAITraceServiceLinkRunsDedupes(t *testing.T) {
	svc := NewAITraceService()
	_, parent := svc.StartJobRun(TraceJobKindAIAssistant, "Ask AI", "desktop", "u1", "/project")
	_, child := svc.StartJobRun(TraceJobKindRemoteSession, "Remote session", "remote", "u1", "/project")

	svc.LinkRuns(parent.RunID, child.RunID)
	svc.LinkRuns(parent.RunID, child.RunID)
	view, ok := svc.GetTrace(parent.RunID)
	if !ok {
		t.Fatal("expected trace")
	}
	if len(view.LinkedRunIDs) != 1 {
		t.Fatalf("LinkedRunIDs len = %d, want 1", len(view.LinkedRunIDs))
	}
	if view.LinkedRunIDs[0] != child.RunID {
		t.Fatalf("linked run = %q, want %q", view.LinkedRunIDs[0], child.RunID)
	}
}

func TestAITraceServiceBuildsTrialReflectSummary(t *testing.T) {
	svc := NewAITraceService()
	_, run := svc.StartJobRun(TraceJobKindAIAssistant, "Trial reflect", "desktop", "u1", "/project")

	svc.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=failed command=npm test"})
	svc.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=succeeded command=npm test"})
	svc.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "adaptive_retry", Category: "args", Summary: "retry decision", ContentSnippet: "invalid parameter"})
	svc.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "trial_reflect", Category: "repeat_guard", Summary: "avoid repeating failed actions", ContentSnippet: "bash"})
	svc.UpdateRun(run.RunID, TraceRunStatusCompleted, "success after retry", "")

	view, ok := svc.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if view.TrialReflectSummary == nil {
		t.Fatal("expected trial reflect summary")
	}
	if view.TrialReflectSummary.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2", view.TrialReflectSummary.AttemptCount)
	}
	if view.TrialReflectSummary.FailureCount != 1 {
		t.Fatalf("FailureCount = %d, want 1", view.TrialReflectSummary.FailureCount)
	}
	if !view.TrialReflectSummary.Recovered {
		t.Fatal("expected Recovered = true")
	}
	if view.TrialReflectSummary.FinalOutcome != "recovered_success" {
		t.Fatalf("FinalOutcome = %q, want recovered_success", view.TrialReflectSummary.FinalOutcome)
	}
	if len(view.TrialReflectSummary.AttemptedTools) == 0 || view.TrialReflectSummary.AttemptedTools[0] != "bash" {
		t.Fatalf("AttemptedTools = %#v, want bash", view.TrialReflectSummary.AttemptedTools)
	}
	if len(view.TrialReflectSummary.FailureCategories) == 0 || view.TrialReflectSummary.FailureCategories[0] != "args" {
		t.Fatalf("FailureCategories = %#v, want args", view.TrialReflectSummary.FailureCategories)
	}
	if !strings.Contains(view.TrialReflectSummary.StrategyNote, "repeat guard") {
		t.Fatalf("StrategyNote = %q, want repeat guard mention", view.TrialReflectSummary.StrategyNote)
	}
}

func TestAITraceServiceRecallEvidencePrefersTrialReflectSummaryForRecoveryQueries(t *testing.T) {
	svc := NewAITraceService()
	_, run := svc.StartJobRun(TraceJobKindAIAssistant, "Recovery trace", "desktop", "u1", "/project")

	svc.AppendEvidence(run.RunID, EvidenceRecord{
		SourceKind:     "adaptive_retry",
		Category:       "args",
		Summary:        "retry decision",
		ContentSnippet: "invalid parameter",
		ProjectPath:    "/project",
		CreatedAt:      1,
	})
	svc.AppendEvidence(run.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect",
		Category:       "repeat_guard",
		Summary:        "avoid repeating failed actions",
		ContentSnippet: "bash",
		ProjectPath:    "/project",
		CreatedAt:      2,
	})
	svc.AppendEvidence(run.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "tools=bash; failures=1; categories=args; repeat guard avoided duplicate failed actions; recovered after failure",
		ProjectPath:    "/project",
		CreatedAt:      3,
	})

	results := svc.RecallEvidence("/project", "how did it recover after failure", 3)
	if len(results) == 0 {
		t.Fatal("expected evidence results")
	}
	if results[0].SourceKind != "trial_reflect_summary" {
		t.Fatalf("top result SourceKind = %q, want trial_reflect_summary", results[0].SourceKind)
	}
	if !strings.Contains(results[0].ContentSnippet, "recovered after failure") {
		t.Fatalf("top result ContentSnippet = %q, want recovery detail", results[0].ContentSnippet)
	}
}

func TestAITraceServiceRecallEvidenceCombinesProjectSummaryAndRecency(t *testing.T) {
	svc := NewAITraceService()
	_, projectRun := svc.StartJobRun(TraceJobKindAIAssistant, "Project trace", "desktop", "u1", "/project-a")
	_, otherRun := svc.StartJobRun(TraceJobKindAIAssistant, "Other trace", "desktop", "u1", "/project-b")

	svc.AppendEvidence(projectRun.RunID, EvidenceRecord{
		SourceKind:     "adaptive_retry",
		Category:       "args",
		Summary:        "retry decision",
		ContentSnippet: "recovered after failure by fixing args",
		ProjectPath:    "/project-a",
		CreatedAt:      10,
	})
	svc.AppendEvidence(projectRun.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "older recovery summary with repeat guard and recovered after failure",
		ProjectPath:    "/project-a",
		CreatedAt:      20,
	})
	svc.AppendEvidence(projectRun.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "newest recovery summary with repeat guard and recovered after failure",
		ProjectPath:    "/project-a",
		CreatedAt:      30,
	})
	svc.AppendEvidence(otherRun.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "other project recovery summary with recovered after failure",
		ProjectPath:    "/project-b",
		CreatedAt:      100,
	})

	results := svc.RecallEvidence("/project-a", "recovered after failure repeat guard summary", 4)
	if len(results) < 3 {
		t.Fatalf("len(results) = %d, want at least 3", len(results))
	}
	if results[0].ProjectPath != "/project-a" {
		t.Fatalf("top result ProjectPath = %q, want /project-a", results[0].ProjectPath)
	}
	if results[0].SourceKind != "trial_reflect_summary" {
		t.Fatalf("top result SourceKind = %q, want trial_reflect_summary", results[0].SourceKind)
	}
	if results[0].CreatedAt != 30 {
		t.Fatalf("top result CreatedAt = %d, want 30", results[0].CreatedAt)
	}
	if results[1].ProjectPath != "/project-a" {
		t.Fatalf("second result ProjectPath = %q, want /project-a", results[1].ProjectPath)
	}
	if results[1].SourceKind != "trial_reflect_summary" {
		t.Fatalf("second result SourceKind = %q, want trial_reflect_summary", results[1].SourceKind)
	}
	if results[1].CreatedAt != 20 {
		t.Fatalf("second result CreatedAt = %d, want 20", results[1].CreatedAt)
	}
	for i := 0; i < 3 && i < len(results); i++ {
		if results[i].ProjectPath != "/project-a" {
			t.Fatalf("result[%d] ProjectPath = %q, want /project-a in top three", i, results[i].ProjectPath)
		}
	}
}
