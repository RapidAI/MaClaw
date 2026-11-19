package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileContractBindsApprovedPayload(t *testing.T) {
	project := t.TempDir()
	pending := codingWorkbenchPendingPlan{UserText: "refactor the parser", Markdown: "## plan\n1. step"}
	c1 := compileCodingExecutionContract(pending, project)
	c2 := compileCodingExecutionContract(pending, project)
	if c1.PlanDigest == "" || c1.PlanDigest != c2.PlanDigest {
		t.Fatalf("digest must be deterministic, got %q vs %q", c1.PlanDigest, c2.PlanDigest)
	}
	pending.UserText = "refactor the parser differently"
	if compileCodingExecutionContract(pending, project).PlanDigest == c1.PlanDigest {
		t.Fatal("digest must change when the approved payload changes")
	}
	if c1.ProjectPath != project {
		t.Fatalf("frozen scope = %q, want %q", c1.ProjectPath, project)
	}
}

func TestCompileContractRequiredFilesFromExplicitRefs(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "config", "app.yaml"), []byte("a: 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := codingWorkbenchPendingPlan{
		UserText: "update @config/app.yaml using <<File name=\"docs/spec.md\">> and fix alice@example.com mentions",
	}
	c := compileCodingExecutionContract(pending, project)
	joined := strings.Join(c.RequiredFiles, ",")
	if !strings.Contains(joined, "config/app.yaml") || !strings.Contains(joined, "docs/spec.md") {
		t.Fatalf("RequiredFiles = %v, want the explicit @ref and <<File>> marker", c.RequiredFiles)
	}
	if strings.Contains(joined, "example.com") {
		t.Fatalf("email address must not become a required file: %v", c.RequiredFiles)
	}
	// config/app.yaml exists; docs/spec.md does not → fail-closed input check.
	if len(c.MissingRequiredFiles) != 1 || c.MissingRequiredFiles[0] != "docs/spec.md" {
		t.Fatalf("MissingRequiredFiles = %v, want [docs/spec.md]", c.MissingRequiredFiles)
	}
}

func TestCompileContractRejectsMalformedRefs(t *testing.T) {
	pending := codingWorkbenchPendingPlan{
		UserText: "read <<File name=\"../../etc/passwd\">> and <<File name=\"C:/win/system32/x.dll\">>",
	}
	c := compileCodingExecutionContract(pending, t.TempDir())
	if len(c.RequiredFiles) != 0 {
		t.Fatalf("traversal/absolute refs must be rejected, got %v", c.RequiredFiles)
	}
}

func TestInferApprovedPlanValidators(t *testing.T) {
	pending := codingWorkbenchPendingPlan{
		Markdown: "## 计划\n1. 改代码\n验证命令: go test ./pkg/...\nChecks：npm test\n普通文本里的 go build ./... 不算",
	}
	got := inferApprovedPlanValidators(pending)
	if len(got) != 2 || got[0] != "go test ./pkg/..." || got[1] != "npm test" {
		t.Fatalf("validators = %v, want the two labeled commands only", got)
	}
	// Unlabeled plan text never becomes a host obligation.
	plain := compileCodingExecutionContract(codingWorkbenchPendingPlan{Markdown: "1. run go test ./..."}, t.TempDir())
	if len(plain.Validators) != 0 {
		t.Fatalf("unlabeled commands must not become validators: %v", plain.Validators)
	}
	// A labeled line inside a fenced code block is an example, not a command
	// the host may execute — quoted samples must not become validators.
	fenced := compileCodingExecutionContract(codingWorkbenchPendingPlan{
		Markdown: "按计划修改。\n```\n验证命令: rm -rf /\n```\n验证命令: go test ./pkg/...",
	}, t.TempDir())
	if len(fenced.Validators) != 1 || fenced.Validators[0] != "go test ./pkg/..." {
		t.Fatalf("fenced-block validator must be ignored, got %v", fenced.Validators)
	}
}

func TestFinishApprovedPlanValidatorsLifecycle(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:validator-lifecycle"
	h.clearStickyCodingWorkbenchMemory(userID)
	defer h.clearStickyCodingWorkbenchMemory(userID)
	project := t.TempDir()
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ProjectPath = project
	})
	h.storeApprovedPlanValidators(userID, []string{"echo passed"}, project)

	// A continuing round keeps the validators pending for the terminal round.
	resp := &IMAgentResponse{Text: "partial", sessionRoundContinue: true}
	if got := h.finishApprovedPlanValidators(userID, resp, true, true); got != resp {
		t.Fatal("continuation response must pass through unmodified")
	}
	if len(h.getStickyCodingWorkbenchMemory(userID).ApprovedPlanValidators) != 1 {
		t.Fatal("validators must survive a continuing round")
	}

	// An errored terminal round keeps them pending as well.
	respErr := &IMAgentResponse{Text: "failed", Error: "step failed"}
	if got := h.finishApprovedPlanValidators(userID, respErr, true, false); got != respErr {
		t.Fatal("errored response must pass through unmodified")
	}
	if len(h.getStickyCodingWorkbenchMemory(userID).ApprovedPlanValidators) != 1 {
		t.Fatal("validators must survive an errored round")
	}

	// An unrelated terminal turn drops the stale validators without firing.
	stale := &IMAgentResponse{Text: "other task done"}
	if got := h.finishApprovedPlanValidators(userID, stale, false, false); got.Text != "other task done" {
		t.Fatalf("unrelated turn must not carry validator output: %q", got.Text)
	}
	if len(h.getStickyCodingWorkbenchMemory(userID).ApprovedPlanValidators) != 0 {
		t.Fatal("stale validators must be dropped on an unrelated terminal turn")
	}

	// The plan's own terminal round runs them and appends the verdict.
	h.storeApprovedPlanValidators(userID, []string{"echo passed"}, project)
	done := &IMAgentResponse{Text: "plan delivered"}
	got := h.finishApprovedPlanValidators(userID, done, true, false)
	if !strings.Contains(got.Text, "批准后验证") || !strings.Contains(got.Text, "全部验证通过") {
		t.Fatalf("terminal plan turn must carry the verified verdict, got %q", got.Text)
	}
	if len(h.getStickyCodingWorkbenchMemory(userID).ApprovedPlanValidators) != 0 {
		t.Fatal("validators must be consumed once they fire")
	}
}
