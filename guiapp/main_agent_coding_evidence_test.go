package guiapp

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestMainAgentCodingToolEventClassification(t *testing.T) {
	cases := []struct {
		tool   string
		result toolExecutionResult
		want   string
		ok     bool
	}{
		{"write_file", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "file_activity", true},
		{"edit_file", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "file_activity", true},
		{"edit_lines", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "file_activity", true},
		{"semantic_write_trusted_file", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "file_activity", true},
		{"host_fs_write_local", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "file_activity", true},
		{"bash", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "verification", true},
		{"semantic_execute_trusted_shell", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "verification", true},
		{"build_verify", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "verification", true},
		{"bash", toolExecutionResult{Outcome: toolOutcomeFailed}, "", false},
		{"read_file", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "", false},
		{"coding_knowledge_search", toolExecutionResult{Outcome: toolOutcomeSucceeded}, "", false},
	}
	for _, tc := range cases {
		got, ok := mainAgentCodingToolEvent(tc.tool, tc.result)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%s outcome %v: got %q/%v, want %q/%v", tc.tool, tc.result.Outcome, got, ok, tc.want, tc.ok)
		}
	}
	if !mainAgentCodingToolFailureEvent("bash", toolExecutionResult{Outcome: toolOutcomeFailed, FailureKind: toolFailureHandlerReported}) {
		t.Fatal("a command that ran and failed must count as verification failure")
	}
	if mainAgentCodingToolFailureEvent("bash", toolExecutionResult{Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}) {
		t.Fatal("a policy-rejected command must not count as verification failure")
	}
	if mainAgentCodingToolFailureEvent("write_file", toolExecutionResult{Outcome: toolOutcomeFailed, FailureKind: toolFailureHandlerReported}) {
		t.Fatal("a failed file write must not count as verification failure")
	}
}

func TestVerifyRuntimeCodingExperienceAcceptsTurnEvidence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	evidence, err := store.RecordTurnEvidence(ctx, knowledge.CodingTurnEvidence{
		OwnerID: "owner", ProjectPath: "/repo", Status: knowledge.CodingTurnStatusCompleted,
		MaterialCount: 1, EvidenceDigest: "sha256:turn-candidate",
	})
	if err != nil {
		t.Fatalf("RecordTurnEvidence: %v", err)
	}
	exp := knowledge.CodingExperience{
		SourceRuntimeTaskID:    knowledge.CodingTurnEvidenceTaskPrefix + evidence.ID,
		SourceRuntimeAttemptID: knowledge.CodingTurnEvidenceAttemptPrefix + evidence.ID,
		EvidenceDigest:         "sha256:turn-candidate",
	}
	if err := app.verifyRuntimeCodingExperience(ctx, exp); err != nil {
		t.Fatalf("verifyRuntimeCodingExperience turn evidence: %v", err)
	}
	exp.EvidenceDigest = "sha256:wrong"
	if err := app.verifyRuntimeCodingExperience(ctx, exp); err == nil {
		t.Fatal("tampered turn candidate evidence must not verify")
	}
}

func newMainAgentEvidenceTestApp(t *testing.T) (*App, *knowledge.CodingKnowledgeStore, knowledge.CodingExperience) {
	t.Helper()
	previousExtraction := mainAgentTurnExtraction
	mainAgentTurnExtraction = func(*IMMessageHandler, knowledge.CodingTurnEvidence, *mainAgentCodingEvidenceState, string, string, string, string) {
	}
	t.Cleanup(func() { mainAgentTurnExtraction = previousExtraction })
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exp, err := store.SaveExperience(ctx, knowledge.CodingExperience{
		Title: "Main-agent evidence rule", Category: knowledge.CodingCategoryPattern, Scope: knowledge.CodingScopeUniversal,
		TriggerCondition: "main-agent evidence", Content: "Direct-turn guidance.", Status: knowledge.CodingStatusActive,
	})
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	return app, store, exp
}

func TestFinalizeMainAgentCodingTurnRecordsRecallOutcome(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-test", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "write_file", `{"path":"main.go"}`, toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "written"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "success", loop.SemanticTurnGeneration(), "", "test task", "done")

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.GetExperience(context.Background(), exp.ID)
		if err == nil && got.RecallCount >= 1 && got.SuccessCount >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("main-agent turn recall outcome was not recorded: exp=%+v err=%v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFinalizeMainAgentCodingTurnRequiresFileActivity(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-command-only", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "bash", `{"command":"ls"}`, toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "files"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "success", loop.SemanticTurnGeneration(), "", "test task", "done")
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetExperience(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.RecallCount != 0 {
		t.Fatalf("command-only turn changed recall stats: %+v", got)
	}
}

func TestFinalizeMainAgentCodingTurnCancelledDoesNotRecord(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-cancelled", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "bash", `{"command":"go test ./..."}`, toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "ok"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "cancelled", loop.SemanticTurnGeneration(), "", "test task", "done")
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetExperience(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.RecallCount != 0 {
		t.Fatalf("cancelled turn changed recall stats: %+v", got)
	}
}

func TestFinalizeMainAgentCodingTurnFailureDoesNotRecord(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-failed-turn", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "write_file", `{"path":"main.go"}`, toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "written"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "error", loop.SemanticTurnGeneration(), "", "test task", "done")
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetExperience(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.RecallCount != 0 {
		t.Fatalf("failed main-agent turn changed recall stats: %+v", got)
	}
}

func TestFinalizeMainAgentCodingTurnInvokesExtraction(t *testing.T) {
	app, _, _ := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-extraction", 1, nil)
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "write_file", `{"path":"main.go"}`,
		toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "written"}, "/repo")

	var gotEvidence knowledge.CodingTurnEvidence
	var gotStatus, gotUser, gotFinal, gotError string
	mainAgentTurnExtraction = func(_ *IMMessageHandler, evidence knowledge.CodingTurnEvidence, _ *mainAgentCodingEvidenceState, status, userText, finalText, outcomeErr string) {
		gotEvidence = evidence
		gotStatus, gotUser, gotFinal, gotError = status, userText, finalText, outcomeErr
	}
	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "success", loop.SemanticTurnGeneration(), "", "fix nil client", "done")

	if gotEvidence.ID == "" || gotEvidence.EvidenceDigest == "" {
		t.Fatalf("extraction evidence = %+v, want a durable turn evidence row", gotEvidence)
	}
	if gotStatus != knowledge.CodingTurnStatusCompleted || gotUser != "fix nil client" || gotFinal != "done" || gotError != "" {
		t.Fatalf("extraction args = status %q user %q final %q error %q", gotStatus, gotUser, gotFinal, gotError)
	}
}

func TestFinalizeMainAgentCodingTurnVerificationFailureRecordsNegativeOutcome(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-negative", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "bash", `{"command":"go test ./..."}`,
		toolExecutionResult{Outcome: toolOutcomeFailed, FailureKind: toolFailureHandlerReported, Text: "FAIL"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "error", loop.SemanticTurnGeneration(), "verification failed", "test task", "done")

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.GetExperience(context.Background(), exp.ID)
		if err == nil && got.RecallCount >= 1 && got.FailureCount >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("negative turn recall outcome was not recorded: exp=%+v err=%v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFinalizeMainAgentCodingTurnTransientProviderErrorDoesNotRecord(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-provider-error", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "bash", `{"command":"go test ./..."}`,
		toolExecutionResult{Outcome: toolOutcomeFailed, FailureKind: toolFailureHandlerReported, Text: "FAIL"}, "/repo")

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "error", loop.SemanticTurnGeneration(), "LLM call failed: HTTP 502 Bad Gateway", "test task", "done")
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetExperience(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.RecallCount != 0 {
		t.Fatalf("transient provider error changed recall stats: %+v", got)
	}
}

func TestFinalizeMainAgentCodingTurnDropsStaleGeneration(t *testing.T) {
	app, store, exp := newMainAgentEvidenceTestApp(t)
	loop := NewLoopContext("main-agent-evidence-stale", 1, nil)
	staleGeneration := loop.SemanticTurnGeneration()
	loop.noteCodingKnowledgeRecallIDs(staleGeneration, []string{exp.ID})
	noteMainAgentCodingToolEvidence(loop, loop.SemanticTurnGeneration(), "owner", "write_file", `{"path":"main.go"}`, toolExecutionResult{Outcome: toolOutcomeSucceeded, Text: "written"}, "/repo")
	loop.ReplaceSemanticTurn()

	handler := &IMMessageHandler{app: app}
	handler.finalizeMainAgentCodingTurn(loop, "success", staleGeneration, "", "test task", "done")
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetExperience(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.RecallCount != 0 {
		t.Fatalf("stale turn changed recall stats: %+v", got)
	}
}
