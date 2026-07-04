package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSummarizeSkillRun_PopulatesCurrentAndLastStep(t *testing.T) {
	status := &SkillRunStatus{
		RunID:  "run-1",
		Skill:  "demo",
		Status: skillRunStatusRunning,
		Steps: []StepResult{
			{Index: 0, Action: "create_session", Status: skillStepStatusSuccess, Output: "会话已创建: ID=sess-1"},
			{Index: 1, Action: "send_and_observe", Status: skillStepStatusRunning},
		},
		Session: &SkillRunSessionMeta{SessionID: "sess-1"},
	}
	summarizeSkillRun(status)
	if !status.Summary.HasSessionBinding {
		t.Fatal("expected session binding summary")
	}
	if status.Summary.CurrentStep != "send_and_observe" || status.Summary.CurrentStepStatus != skillStepStatusRunning {
		t.Fatalf("unexpected current step summary: %#v", status.Summary)
	}
	if status.Summary.LastCompletedStep != "create_session" {
		t.Fatalf("unexpected last completed step: %#v", status.Summary)
	}
	if !strings.Contains(status.Summary.LastOutputSnippet, "会话已创建") {
		t.Fatalf("expected output snippet, got %#v", status.Summary)
	}
}

func TestSummarizeSkillRun_MarksInstructionOnlyCraftTool(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "demo.pdf")
	status := &SkillRunStatus{
		RunID:  "run-craft",
		Skill:  "pptx-generator",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{{
			Index:  0,
			Action: "craft_tool",
			Status: skillStepStatusSuccess,
			Output: "📝 脚本语言: python\n📁 脚本路径: C:/tmp/tool.py\n" + outputPath + "\n\n✅ 脚本执行成功",
		}},
	}
	summarizeSkillRun(status)
	if !status.Summary.NeedsArtifactVerification {
		t.Fatalf("expected summary to require artifact verification, got %#v", status.Summary)
	}
	if status.Summary.ArtifactPath != outputPath {
		t.Fatalf("ArtifactPath = %q, want %q", status.Summary.ArtifactPath, outputPath)
	}
	if status.Summary.ArtifactStatus != skillArtifactStatusMissing {
		t.Fatalf("ArtifactStatus = %q, want missing", status.Summary.ArtifactStatus)
	}
}

func TestSummarizeSkillRun_VerificationPassedSkipsArtifactVerification(t *testing.T) {
	status := &SkillRunStatus{
		RunID:  "run-craft-ok",
		Skill:  "pptx-generator",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{{
			Index:  0,
			Action: "craft_tool",
			Status: skillStepStatusSuccess,
			Output: "📝 脚本语言: python\n📁 脚本路径: C:/tmp/tool.py\nattempts: 1\nverification: passed\nartifact: C:/tmp/done.pdf\n\n✅ 脚本执行成功",
		}},
	}
	summarizeSkillRun(status)
	if status.Summary.ArtifactStatus != skillArtifactStatusVerified {
		t.Fatalf("ArtifactStatus = %q, want verified", status.Summary.ArtifactStatus)
	}
	if status.Summary.NeedsArtifactVerification {
		t.Fatalf("expected verification passed to clear follow-up verification, got %#v", status.Summary)
	}
}

func TestSummarizeSkillRun_VerifiesExpectedOutputArtifact(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "done.pdf")
	if err := os.WriteFile(outputPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	status := &SkillRunStatus{
		RunID:          "run-artifact",
		Skill:          "xh-md-to-pdf",
		Status:         skillRunStatusSuccess,
		ExpectedOutput: outputPath,
		Steps:          []StepResult{{Index: 0, Action: "craft_tool", Status: skillStepStatusSuccess, Output: "✅ 脚本执行成功"}},
	}
	summarizeSkillRun(status)
	if status.Summary.ArtifactPath != outputPath {
		t.Fatalf("ArtifactPath = %q, want %q", status.Summary.ArtifactPath, outputPath)
	}
	if status.Summary.ArtifactStatus != skillArtifactStatusVerified {
		t.Fatalf("ArtifactStatus = %q, want verified", status.Summary.ArtifactStatus)
	}
	if status.Summary.NeedsArtifactVerification {
		t.Fatalf("expected no pending verification, got %#v", status.Summary)
	}
}

func TestMaterializeStdoutToExpectedOutputCopiesJSONArtifactFile(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	expectedPath := filepath.Join(dir, "paper_output.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-json-artifact",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "shell: cmd.exe\nelapsed: 1s\n───────────────\n" +
				"{\"ok\":true,\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	got, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile(expected) error = %v", err)
	}
	if string(got) != "%PDF translated" {
		t.Fatalf("expected copied artifact bytes, got %q", string(got))
	}
}

func TestMaterializeStdoutToExpectedOutputFindsTrailingJSONArtifactFile(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	expectedPath := filepath.Join(dir, "paper_output.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated after logs"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-json-artifact-with-logs",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "shell: cmd.exe\nelapsed: 1s\n───────────────\n" +
				"translating page 1/2\ntranslating page 2/2\n" +
				"{\"ok\":true,\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	got, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile(expected) error = %v", err)
	}
	if string(got) != "%PDF translated after logs" {
		t.Fatalf("expected copied artifact bytes, got %q", string(got))
	}
}

func TestMaterializeStdoutToExpectedOutputDoesNotWritePlainTextAsPDF(t *testing.T) {
	dir := t.TempDir()
	expectedPath := filepath.Join(dir, "paper_output.pdf")
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-plain-pdf",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "translation completed, output saved elsewhere",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("plain stdout should not be materialized as PDF, stat err=%v", err)
	}
}

func TestSamePathCaseSensitivityMatchesPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !samePath(`C:\Tmp\Out.pdf`, `c:\tmp\out.pdf`) {
			t.Fatal("expected Windows paths to compare case-insensitively")
		}
		return
	}
	if samePath("/tmp/Out.pdf", "/tmp/out.pdf") {
		t.Fatal("expected non-Windows paths to compare case-sensitively")
	}
}

func TestMergeSkillRuntimeExtraEnvPathOverridesCaseEquivalentKey(t *testing.T) {
	basePathKey := "PATH"
	if runtime.GOOS == "windows" {
		basePathKey = "Path"
	}
	got := mergeSkillRuntimeExtraEnv(
		map[string]string{basePathKey: "base", "API_KEY": "user-value"},
		map[string]string{"PATH": "runtime", "API_KEY": "runtime-value"},
	)

	if got["PATH"] != "runtime" {
		t.Fatalf("PATH = %q, want runtime", got["PATH"])
	}
	if got["API_KEY"] != "user-value" {
		t.Fatalf("API_KEY = %q, want user-value", got["API_KEY"])
	}
	pathKeys := 0
	for key := range got {
		if strings.EqualFold(key, "PATH") {
			pathKeys++
		}
	}
	if pathKeys != 1 {
		t.Fatalf("merged env has %d PATH-like keys: %#v", pathKeys, got)
	}
}

func TestSummarizeSkillRun_PopulatesErrorSnippet(t *testing.T) {
	status := &SkillRunStatus{
		RunID:  "run-2",
		Skill:  "demo",
		Status: skillRunStatusFailed,
		Steps:  []StepResult{{Index: 0, Action: "send_and_observe", Status: skillStepStatusFailed, Error: "network timeout while waiting for output"}},
		Error:  "step failed",
	}
	summarizeSkillRun(status)
	if status.Summary.CurrentStep != "send_and_observe" || status.Summary.CurrentStepStatus != skillStepStatusFailed {
		t.Fatalf("unexpected failed current step summary: %#v", status.Summary)
	}
	if !strings.Contains(status.Summary.LastErrorSnippet, "network timeout") {
		t.Fatalf("expected error snippet, got %#v", status.Summary)
	}
}

func TestSkillRunnerGetRunStatus_IncludesSummary(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	runner.runs["run-3"] = &skillRun{status: SkillRunStatus{
		RunID:   "run-3",
		Skill:   "demo",
		Status:  skillRunStatusRunning,
		Steps:   []StepResult{{Index: 0, Action: "create_session", Status: skillStepStatusSuccess, Output: "会话已创建: ID=sess-3"}},
		Session: &SkillRunSessionMeta{SessionID: "sess-3"},
	}}
	status, err := runner.GetRunStatus("run-3")
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if status.Summary.LastCompletedStep != "create_session" {
		t.Fatalf("expected summary last completed step, got %#v", status.Summary)
	}
	if !status.Summary.HasSessionBinding {
		t.Fatalf("expected session binding summary, got %#v", status.Summary)
	}
}
