package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestAgentsInstructionsCacheHit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules\nuse gofmt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := "desktop-user:agents-cache"
	// Prime sticky + cache.
	c1 := h.ensureStickyProjectInstructions(userID, dir)
	if !strings.Contains(c1, "gofmt") {
		t.Fatalf("c1=%q", c1)
	}
	// Second call should hit mtime cache (still correct content).
	c2 := h.ensureStickyProjectInstructions(userID, dir)
	if c2 != c1 {
		t.Fatalf("cache mismatch %q vs %q", c1, c2)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestLoadCodingWorkbenchProjectInstructions(t *testing.T) {
	dir := t.TempDir()
	if content, src := loadCodingWorkbenchProjectInstructions(dir); content != "" || len(src) != 0 {
		t.Fatalf("empty project should have no instructions")
	}
	agents := "# Project rules\n\n- Use tabs\n- Run tests\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	content, sources := loadCodingWorkbenchProjectInstructions(dir)
	if !strings.Contains(content, "Use tabs") {
		t.Fatalf("content=%q", content)
	}
	if len(sources) == 0 || sources[0] != "AGENTS.md" {
		t.Fatalf("sources=%v", sources)
	}
	// CLAUDE.md should also be picked when no AGENTS (priority group).
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "CLAUDE.md"), []byte("Claude rules here"), 0o644); err != nil {
		t.Fatal(err)
	}
	content2, sources2 := loadCodingWorkbenchProjectInstructions(dir2)
	if !strings.Contains(content2, "Claude rules") {
		t.Fatalf("content2=%q", content2)
	}
	if len(sources2) == 0 {
		t.Fatal("expected CLAUDE.md source")
	}
}

func TestNormalizeCodingPlanMode(t *testing.T) {
	if normalizeCodingPlanMode("") != codingPlanModeAuto {
		t.Fatal("default auto")
	}
	if normalizeCodingPlanMode("approve") != codingPlanModeApprove {
		t.Fatal("approve")
	}
	if normalizeCodingPlanMode("off") != codingPlanModeOff {
		t.Fatal("off")
	}
	if normalizeCodingPlanMode("confirm") != codingPlanModeApprove {
		t.Fatal("confirm alias")
	}
}

func TestStickyPendingPlanRoundTrip(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:test-plan-approve"
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map code"},
		{Index: 2, Title: "implement", Description: "write code", DependsOn: []int{1}},
	}
	h.storeStickyPendingCodingPlan(userID, "build auth", "### T1: explore\n### T2: implement", tasks)
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) != 2 {
		t.Fatalf("pending=%+v ok=%v", pending, ok)
	}
	if pending.UserText != "build auth" {
		t.Fatalf("user text=%q", pending.UserText)
	}
	promoted, ok := h.promotePendingToApprovedCodingPlan(userID)
	if !ok || len(promoted.Tasks) != 2 {
		t.Fatalf("promoted=%+v", promoted)
	}
	if _, still := h.loadStickyPendingCodingPlan(userID); still {
		t.Fatal("pending should be cleared after promote")
	}
	taken, ok := h.takeStickyApprovedCodingPlan(userID)
	if !ok || len(taken.Tasks) != 2 {
		t.Fatalf("taken=%+v", taken)
	}
	if _, again := h.takeStickyApprovedCodingPlan(userID); again {
		t.Fatal("approved plan is one-shot")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestStepNeedsVerifyGate(t *testing.T) {
	if !stepNeedsVerifyGate("verify", "run tests", 3, 3) {
		t.Fatal("last step should gate")
	}
	if !stepNeedsVerifyGate("implement JWT", "write code", 2, 3) {
		t.Fatal("implement should gate")
	}
	if stepNeedsVerifyGate("explore codebase", "read files", 1, 3) {
		t.Fatal("explore should not gate")
	}
}

func TestIsCodingWorkbenchSlash(t *testing.T) {
	for _, cmd := range []string{"/plan", "/plan approve", "/review", "/test", "/commit msg", "/pr", "/map foo", "/checkpoint", "/agents", "/coding-help"} {
		if !isCodingWorkbenchSlash(cmd) {
			t.Fatalf("expected slash: %s", cmd)
		}
	}
	if isCodingWorkbenchSlash("/goal x") {
		t.Fatal("/goal is not coding workbench slash")
	}
	if classifyImmediateIMCommand("/plan approve") != imCommandCodingWorkbench {
		t.Fatal("classify plan")
	}
}

func TestCodingWorkbenchStepsFromTasks(t *testing.T) {
	steps := codingWorkbenchStepsFromTasks([]*v2.TaskItem{
		{Index: 1, Title: "a"},
		{Index: 2, Title: "b"},
	}, codingStepPending)
	if len(steps) != 2 || steps[0].Status != codingStepPending || steps[1].Title != "b" {
		t.Fatalf("%+v", steps)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:test-checkpoint"
	h.setStickyCodingSessionPlan(userID, "Ship feature X")
	h.setStickyCodingExecutionPlan(userID, "### T1: do it")
	cp := h.saveStickyCodingCheckpoint(userID, "mid")
	if cp.Label != "mid" {
		t.Fatalf("label=%q", cp.Label)
	}
	h.setStickyCodingSessionPlan(userID, "changed")
	restored, ok := h.restoreStickyCodingCheckpoint(userID)
	if !ok || restored.Label != "mid" {
		t.Fatalf("restore=%+v", restored)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "Ship feature X" {
		t.Fatalf("session plan=%q", mem.SessionPlan)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestResolvePlanModeOff(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-off"
	text := "1. explore auth\n2. implement JWT\n3. add tests"
	tasks, _, planned := h.resolveCodingWorkbenchTasks(userID, text, t.TempDir(), stickyCodingWorkbenchMemory{
		PlanMode: codingPlanModeOff,
	}, nil, nil)
	if planned || len(tasks) != 1 {
		t.Fatalf("plan mode off should single-task: planned=%v len=%d", planned, len(tasks))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestResolvePlanModeApproveStoresPending(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-approve-mode"
	text := "1. explore auth module structure carefully\n2. implement JWT login endpoint\n3. add unit tests for auth"
	tasks, md, planned := h.resolveCodingWorkbenchTasks(userID, text, t.TempDir(), stickyCodingWorkbenchMemory{
		PlanMode: codingPlanModeApprove,
	}, nil, nil)
	if !planned || len(tasks) < 2 {
		t.Fatalf("planned=%v tasks=%d md=%q", planned, len(tasks), md)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) < 2 {
		t.Fatalf("expected pending plan, got ok=%v pending=%+v", ok, pending)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestPrevOutputsIncludesProjectInstructions(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{
		ProjectInstructions:       "Always use gofmt",
		ProjectInstructionSources: []string{"AGENTS.md"},
		SessionPlan:               "goal",
	}
	out := mem.prevOutputs()
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "Always use gofmt") {
		t.Fatalf("prevOutputs missing instructions: %q", joined)
	}
}

func TestMarkRemainingCodingStepsSkippedRewritesPrematurePassed(t *testing.T) {
	userID := "desktop-user:skip-premature"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "T1", Status: codingStepFailed},
		{Index: 2, Title: "T2", Status: codingStepPassed},   // premature agent mark
		{Index: 3, Title: "T3", Status: codingStepRunning},  // should also skip
		{Index: 4, Title: "T4", Status: codingStepPending},
		{Index: 5, Title: "T5", Status: codingStepSkipped}, // already skipped
	})
	h.markRemainingCodingStepsSkipped(userID, 1, "skipped: prior step failed")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	byIdx := map[int]codingWorkbenchStepStatus{}
	for _, st := range mem.StepStatuses {
		byIdx[st.Index] = st
	}
	if byIdx[1].Status != codingStepFailed {
		t.Fatalf("T1 must stay failed: %+v", byIdx[1])
	}
	for _, idx := range []int{2, 3, 4} {
		if byIdx[idx].Status != codingStepSkipped {
			t.Fatalf("T%d want skipped, got %+v", idx, byIdx[idx])
		}
		if !strings.Contains(byIdx[idx].Summary, "prior step failed") {
			t.Fatalf("T%d summary=%q", idx, byIdx[idx].Summary)
		}
	}
	if byIdx[5].Status != codingStepSkipped {
		t.Fatalf("already-skipped T5 should remain skipped: %+v", byIdx[5])
	}
}
