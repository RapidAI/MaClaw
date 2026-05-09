package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ===== Gap 7: SplitSkillDocSections =====

func TestSplitSkillDocSections_NoMarkers(t *testing.T) {
	content := "# My Skill\n\nDo step A.\nDo step B."
	core, ref := SplitSkillDocSections(content)
	if core != content {
		t.Errorf("expected entire content as core, got %q", core)
	}
	if ref != "" {
		t.Errorf("expected empty reference, got %q", ref)
	}
}

func TestSplitSkillDocSections_BothMarkers(t *testing.T) {
	content := "# Preamble\n\n<!-- CORE: essential -->\n## Step 1\nDo A.\n\n<!-- REFERENCE: details -->\n## Detailed A\nLong text..."
	core, ref := SplitSkillDocSections(content)
	if core == "" {
		t.Fatal("core empty")
	}
	if ref == "" {
		t.Fatal("ref empty")
	}
	if !strHas(core, "Preamble") {
		t.Error("core missing preamble")
	}
	if !strHas(core, "Step 1") {
		t.Error("core missing Step 1")
	}
	if !strHas(ref, "Detailed A") {
		t.Error("ref missing Detailed A")
	}
	if strHas(core, "Detailed A") {
		t.Error("core has ref content")
	}
}

func TestSplitSkillDocSections_OnlyReference(t *testing.T) {
	content := "# Core stuff\n\n<!-- REFERENCE: extra -->\n## Extra details"
	core, ref := SplitSkillDocSections(content)
	if !strHas(core, "Core stuff") {
		t.Error("core missing")
	}
	if !strHas(ref, "Extra details") {
		t.Error("ref missing")
	}
}

func TestSplitSkillDocSections_WrongOrder(t *testing.T) {
	content := "<!-- REFERENCE: docs -->\nref\n<!-- CORE: inst -->\ncore"
	core, ref := SplitSkillDocSections(content)
	if core != content {
		t.Error("wrong order: core != content")
	}
	if ref != "" {
		t.Error("wrong order: ref not empty")
	}
}

// ===== Gap 1: Description Quality =====

func TestEvaluateDescription_HighQuality(t *testing.T) {
	q := EvaluateDescription("Convert Markdown files to PDF format, use before generating reports", []string{"pdf", "markdown", "convert"})
	if q.Score < 0.8 {
		t.Errorf("score %.1f < 0.8, missing: %v", q.Score, q.Missing)
	}
}

func TestEvaluateDescription_LowQuality(t *testing.T) {
	q := EvaluateDescription("helps", nil)
	if q.Score > 0.4 {
		t.Errorf("score %.1f > 0.4", q.Score)
	}
	if len(q.Suggestions) == 0 {
		t.Error("no suggestions")
	}
}

// ===== Gap 6: Execution Preamble =====

func TestBuildExecutionPreamble_SimpleSkill(t *testing.T) {
	s := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hello"}}}}
	if BuildExecutionPreamble(s) != "" {
		t.Error("simple skill should have empty preamble")
	}
}

func TestBuildExecutionPreamble_DeploySkill(t *testing.T) {
	s := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "kubectl apply -f deploy.yaml"}}}}
	p := BuildExecutionPreamble(s)
	if p == "" {
		t.Fatal("empty")
	}
	if !strHas(p, "production") {
		t.Error("missing production warning")
	}
}

func TestBuildExecutionPreamble_CraftToolSkill(t *testing.T) {
	s := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"task": "gen"}}}}
	p := BuildExecutionPreamble(s)
	if p == "" {
		t.Fatal("empty")
	}
	if !strHas(p, "try/catch") {
		t.Error("missing error handling rule")
	}
}

func TestBuildExecutionPreamble_DatabaseSkill(t *testing.T) {
	s := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "psql -c 'SELECT 1'"}}}}
	p := BuildExecutionPreamble(s)
	if p == "" {
		t.Fatal("empty")
	}
	if !strHas(p, "DROP") {
		t.Error("missing database safety rule")
	}
}

// ===== Gap 4: Skill State =====

func TestSkillState_LoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := &SkillState{CurrentPhase: "analysis", NextPrompt: "Continue"}
	st.SetVar("done", "3")
	st.AppendHistory(StateHistoryEntry{Summary: "Analyzed 3", Success: true})
	if err := SaveState(dir, st); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentPhase != "analysis" {
		t.Errorf("phase %q", loaded.CurrentPhase)
	}
	if loaded.GetVar("done") != "3" {
		t.Errorf("var %q", loaded.GetVar("done"))
	}
	if loaded.NextPrompt == "" {
		t.Error("NextPrompt empty")
	}
	if len(loaded.History) != 1 {
		t.Errorf("history %d", len(loaded.History))
	}
}

func TestSkillState_LoadNonExistent(t *testing.T) {
	st, err := LoadState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsEmpty() {
		t.Error("not empty")
	}
}

func TestSkillState_HistoryMaxLen(t *testing.T) {
	st := &SkillState{}
	for i := 0; i < 15; i++ {
		st.AppendHistory(StateHistoryEntry{Summary: fmt.Sprintf("run %d", i), Success: true})
	}
	if len(st.History) != 10 {
		t.Errorf("len %d", len(st.History))
	}
	if st.History[0].Summary != "run 5" {
		t.Errorf("oldest %q", st.History[0].Summary)
	}
}

func TestSkillState_ClearState(t *testing.T) {
	dir := t.TempDir()
	SaveState(dir, &SkillState{CurrentPhase: "test"})
	ClearState(dir)
	loaded, _ := LoadState(dir)
	if !loaded.IsEmpty() {
		t.Error("not empty after clear")
	}
}

// ===== Gap 5: Pipeline =====

type mockExecutor struct{ results map[string]mockResult }
type mockResult struct {
	vars   map[string]string
	output string
	err    error
}

func (m *mockExecutor) RunSubSkill(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
	r, ok := m.results[name]
	if !ok {
		return nil, "", fmt.Errorf("not found: %s", name)
	}
	return r.vars, r.output, r.err
}

type skillExecutorFunc func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error)

func (fn skillExecutorFunc) RunSubSkill(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
	return fn(ctx, name, params)
}

func TestPipelineRunner_BasicSequence(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"lint": {vars: map[string]string{"status": "clean"}}, "test": {output: "pass"}, "build": {output: "ok"}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint"}, {Skill: "test"}, {Skill: "build"}}, nil)
	if res.Status != "completed" {
		t.Errorf("got %q", res.Status)
	}
	if res.Vars["lint.status"] != "clean" {
		t.Errorf("lint.status %q", res.Vars["lint.status"])
	}
}

func TestPipelineRunner_FailureStops(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"lint": {err: fmt.Errorf("fail")}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint"}, {Skill: "test"}}, nil)
	if res.Status != "failed" {
		t.Errorf("got %q", res.Status)
	}
}

func TestPipelineRunner_NilExecutorFailsWithoutPanic(t *testing.T) {
	r := &PipelineRunner{}
	res, err := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint"}}, nil)
	if err == nil {
		t.Fatal("expected nil executor error")
	}
	if res == nil || res.Status != "failed" {
		t.Fatalf("result = %#v, want failed", res)
	}
}

func TestPipelineRunner_EmptySkillNameFailsBeforeExecutor(t *testing.T) {
	called := false
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		called = true
		return nil, "", nil
	})}
	res, err := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: " "}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("executor was called for empty pipeline skill")
	}
	if res == nil || res.Status != "failed" || !strings.Contains(res.Error, "missing skill") {
		t.Fatalf("result = %#v, want missing-skill failure", res)
	}
}

func TestPipelineRunner_UnresolvedParamFailsBeforeExecutor(t *testing.T) {
	called := false
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		called = true
		return nil, "", nil
	})}
	res, err := r.Run(context.Background(), []corelib.SkillPipelineStep{{
		Skill:  "test",
		Params: map[string]string{"input": "{{missing.output}}"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("executor was called with unresolved pipeline params")
	}
	if res == nil || res.Status != "failed" || !strings.Contains(res.Error, "unresolved variable") {
		t.Fatalf("result = %#v, want unresolved-variable failure", res)
	}
}

func TestPipelineRunner_UnresolvedParamMessageIsDeterministic(t *testing.T) {
	got := unresolvedPipelineParamMessage(map[string]string{
		"z": "{{later.output}}",
		"a": "{{first.output}}",
	})

	if !strings.Contains(got, "a={{first.output}}, z={{later.output}}") {
		t.Fatalf("message = %q, want sorted missing vars", got)
	}
}

func TestPipelineRunner_FailedStepKeepsOutputInSummary(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"lint": {output: "diagnostic output", err: fmt.Errorf("fail")}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint"}}, nil)
	if res.Status != "failed" {
		t.Errorf("got %q", res.Status)
	}
	summary := FormatPipelineResult(res)
	if !strings.Contains(summary, "diagnostic output") {
		t.Fatalf("summary = %q, want failed output", summary)
	}
}

func TestPipelineRunner_FailedStepWithCapturedVarsKeepsOutputInSummary(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"lint": {vars: map[string]string{"file": "report.md"}, output: "diagnostic output", err: fmt.Errorf("fail")}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint"}}, nil)
	if res.Status != "failed" {
		t.Errorf("got %q", res.Status)
	}
	if len(res.StepResults) != 1 || res.StepResults[0].CapturedVars["output"] != "diagnostic output" {
		t.Fatalf("step result = %#v, want output preserved beside captured vars", res.StepResults)
	}
	summary := FormatPipelineResult(res)
	if !strings.Contains(summary, "diagnostic output") {
		t.Fatalf("summary = %q, want failed output even when captured vars exist", summary)
	}
}

func TestPipelineRunner_TruncatesOutputOnRuneBoundary(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"report": {output: strings.Repeat("界", 1200)}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "report"}}, nil)

	summary := FormatPipelineResult(res)

	if !utf8.ValidString(summary) || !strings.Contains(summary, "... (truncated)") {
		t.Fatalf("summary valid=%v contains_truncated=%v", utf8.ValidString(summary), strings.Contains(summary, "... (truncated)"))
	}
	if got := res.Vars["report.output"]; !utf8.ValidString(got) || !strings.HasSuffix(got, "...") {
		t.Fatalf("captured output var = %q, want valid truncated UTF-8", got)
	}
}

func TestPipelineRunner_ContinueOnFail(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"lint": {err: fmt.Errorf("warn")}, "test": {output: "pass"}}}
	r := &PipelineRunner{Executor: ex}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "lint", ContinueOnFail: true}, {Skill: "test"}}, nil)
	if res.Status != "completed" {
		t.Errorf("got %q", res.Status)
	}
}

func TestPipelineRunner_ContinueOnFailPropagatesFailedOutput(t *testing.T) {
	var testParams map[string]string
	ex := &mockExecutor{results: map[string]mockResult{
		"lint": {output: "lint diagnostics", err: fmt.Errorf("warn")},
		"test": {vars: map[string]string{"status": "used"}},
	}}
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		if name == "test" {
			testParams = params
		}
		return ex.RunSubSkill(ctx, name, params)
	})}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{
		{Skill: "lint", ContinueOnFail: true},
		{Skill: "test", Params: map[string]string{"diagnostic": "{{lint.output}}"}},
	}, nil)
	if res.Status != "completed" {
		t.Errorf("got %q", res.Status)
	}
	if testParams["diagnostic"] != "lint diagnostics" {
		t.Fatalf("test params = %#v, want failed output propagated", testParams)
	}
}

func TestPipelineRunner_ContinueOnFailPropagatesFailedCapturedVars(t *testing.T) {
	var testParams map[string]string
	ex := &mockExecutor{results: map[string]mockResult{
		"lint": {vars: map[string]string{"file": "report.md", "input": "report.md"}, output: "file=report.md", err: fmt.Errorf("warn")},
		"test": {vars: map[string]string{"status": "used"}},
	}}
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		if name == "test" {
			testParams = params
		}
		return ex.RunSubSkill(ctx, name, params)
	})}

	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{
		{Skill: "lint", ContinueOnFail: true},
		{Skill: "test", Params: map[string]string{"input": "{{lint.input}}"}},
	}, nil)

	if res.Status != "completed" {
		t.Errorf("got %q", res.Status)
	}
	if testParams["input"] != "report.md" {
		t.Fatalf("test params = %#v, want failed captured alias propagated", testParams)
	}
}

func TestPipelineRunner_ContextErrorIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		cancel()
		return nil, "partial output", ctx.Err()
	})}
	res, err := r.Run(ctx, []corelib.SkillPipelineStep{{Skill: "slow"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" || !strings.Contains(FormatPipelineResult(res), "partial output") {
		t.Fatalf("result = %#v, summary = %q; want cancelled with output", res, FormatPipelineResult(res))
	}
}

func TestPipelineRunner_PreCancelledContextRecordsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &PipelineRunner{Executor: skillExecutorFunc(func(ctx context.Context, name string, params map[string]string) (map[string]string, string, error) {
		t.Fatal("executor should not run after pre-cancel")
		return nil, "", nil
	})}

	res, err := r.Run(ctx, []corelib.SkillPipelineStep{{Skill: "slow"}}, nil)

	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" || !strings.Contains(res.Error, "canceled") {
		t.Fatalf("result = %#v, want cancelled result with context error", res)
	}
}

func TestPipelineRunner_CheckpointStop(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"build": {output: "ok"}, "deploy": {output: "ok"}}}
	r := &PipelineRunner{Executor: ex, AskUser: func(q string, opts []string) (int, error) { return 1, nil }}
	res, _ := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "build", Checkpoint: true}, {Skill: "deploy"}}, nil)
	if res.Status != "stopped_at_checkpoint" {
		t.Errorf("got %q", res.Status)
	}
}

func TestPipelineRunner_CheckpointAskErrorFails(t *testing.T) {
	ex := &mockExecutor{results: map[string]mockResult{"build": {output: "ok"}}}
	r := &PipelineRunner{Executor: ex, AskUser: func(q string, opts []string) (int, error) {
		return 0, fmt.Errorf("prompt unavailable")
	}}

	res, err := r.Run(context.Background(), []corelib.SkillPipelineStep{{Skill: "build", Checkpoint: true}}, nil)

	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "failed" || res.FailedSkill != "build" || !strings.Contains(res.Error, "prompt unavailable") {
		t.Fatalf("result = %#v, want checkpoint prompt failure", res)
	}
}

func TestPipelineRunner_CheckpointCancellationIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ex := &mockExecutor{results: map[string]mockResult{"build": {output: "ok"}}}
	r := &PipelineRunner{Executor: ex, AskUser: func(q string, opts []string) (int, error) {
		cancel()
		return 0, ctx.Err()
	}}

	res, err := r.Run(ctx, []corelib.SkillPipelineStep{{Skill: "build", Checkpoint: true}}, nil)

	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" || !strings.Contains(res.Error, "canceled") {
		t.Fatalf("result = %#v, want checkpoint cancellation", res)
	}
}

// ===== Gap 2: References =====

func TestScanReferences(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "references"), 0755)
	os.WriteFile(filepath.Join(dir, "references", "workers.md"), []byte("# Workers\n\nDeploy..."), 0644)
	os.WriteFile(filepath.Join(dir, "references", "d1.md"), []byte("# D1\n\nConfig..."), 0644)
	refs := scanReferences(dir)
	if len(refs) != 2 {
		t.Fatalf("got %d", len(refs))
	}
}

func TestScanReferences_NoDir(t *testing.T) {
	if refs := scanReferences(t.TempDir()); refs != nil {
		t.Error("should be nil")
	}
}

func strHas(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
