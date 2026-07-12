package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
			Output: "脚本语言: python\n脚本路径: C:/tmp/tool.py\n" + outputPath + "\n\n脚本执行成功",
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
			Output: "脚本语言: python\n脚本路径: C:/tmp/tool.py\nattempts: 1\nverification: passed\nartifact: C:/tmp/done.pdf\n\n脚本执行成功",
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
		Steps:          []StepResult{{Index: 0, Action: "craft_tool", Status: skillStepStatusSuccess, Output: "脚本执行成功"}},
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

func TestFinalizeRunOutcomeTreatsVerifiedArtifactAsSuccess(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "translated.pdf")
	if err := os.WriteFile(outputPath, []byte("%PDF translated"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-artifact-timeout",
		Skill:          "paper_pdf_translator",
		Status:         skillRunStatusRunning,
		ExpectedOutput: outputPath,
		StartedAt:      time.Now().Add(-time.Minute).Format(time.RFC3339),
		Error:          "step 1 (bash) failed: timeout",
		Steps: []StepResult{{
			Index:   0,
			Action:  "bash",
			Status:  skillStepStatusFailed,
			Error:   "timeout",
			Timeout: true,
			Output:  "Output: " + outputPath,
		}},
	}}

	runner.finalizeRunOutcome(run, skillRunStatusFailed, time.Now())

	if run.status.Status != skillRunStatusSuccess {
		t.Fatalf("Status = %q, want success", run.status.Status)
	}
	if run.status.Error != "" {
		t.Fatalf("Error = %q, want cleared", run.status.Error)
	}
	if len(run.status.Warnings) == 0 || !strings.Contains(run.status.Warnings[0], "expected artifact") {
		t.Fatalf("Warnings = %#v, want artifact warning", run.status.Warnings)
	}
}

func TestEffectiveSkillGlobalTimeoutUsesMaxForMaclawAppRuns(t *testing.T) {
	runner := NewSkillRunner(nil)
	run := &skillRun{
		status: SkillRunStatus{Skill: "paper_pdf_translator"},
		runArgs: map[string]interface{}{
			"_maclaw_app": "true",
			"app_name":    "PDF\u7ffb\u8bd1\u5de5\u5177",
			"input":       filepath.Join(t.TempDir(), "paper.pdf"),
			"output":      filepath.Join(t.TempDir(), "paper.zh.pdf"),
			"output_mode": "pdf",
		},
	}

	if got := runner.effectiveSkillGlobalTimeoutSec(run, &corelib.NLSkillEntry{Name: "paper_pdf_translator", GlobalTimeout: corelib.DefaultSkillRunnerTimeoutSec}); got != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("effectiveSkillGlobalTimeoutSec() = %d, want %d", got, corelib.MaxSkillRunnerTimeoutSec)
	}
	run.runArgs["global_timeout"] = "14400"
	if got := runner.effectiveSkillGlobalTimeoutSec(run, &corelib.NLSkillEntry{Name: "paper_pdf_translator"}); got != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("effectiveSkillGlobalTimeoutSec(global_timeout) = %d, want %d", got, corelib.MaxSkillRunnerTimeoutSec)
	}
	if got := runner.applyEffectiveSkillGlobalTimeoutSec(run, &corelib.NLSkillEntry{Name: "paper_pdf_translator"}); got != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("applyEffectiveSkillGlobalTimeoutSec() = %d, want %d", got, corelib.MaxSkillRunnerTimeoutSec)
	}
	if got := runner.runDefaultTimeoutSec(run); got != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("runDefaultTimeoutSec() after apply = %d, want %d", got, corelib.MaxSkillRunnerTimeoutSec)
	}
}

func TestEffectiveSkillGlobalTimeoutDoesNotTreatOutputModeAsAppRun(t *testing.T) {
	runner := NewSkillRunner(nil)
	run := &skillRun{
		status: SkillRunStatus{Skill: "generic_converter"},
		runArgs: map[string]interface{}{
			"input":       filepath.Join(t.TempDir(), "paper.pdf"),
			"output":      filepath.Join(t.TempDir(), "paper.txt"),
			"output_mode": "txt",
		},
	}

	if got := runner.effectiveSkillGlobalTimeoutSec(run, &corelib.NLSkillEntry{Name: "generic_converter"}); got != corelib.DefaultSkillRunnerTimeoutSec {
		t.Fatalf("effectiveSkillGlobalTimeoutSec() = %d, want default %d", got, corelib.DefaultSkillRunnerTimeoutSec)
	}
}

func TestSummarizeSkillRun_UsesDetectedArtifactWhenExpectedOutputMissing(t *testing.T) {
	dir := t.TempDir()
	expectedPath := filepath.Join(dir, "missing-output.pdf")
	actualPath := filepath.Join(dir, "translated.pdf")
	if err := os.WriteFile(actualPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	status := &SkillRunStatus{
		RunID:          "run-artifact-detected",
		Skill:          "paper-pdf-translator",
		Status:         skillRunStatusSuccess,
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "Output: " + actualPath,
		}},
	}

	summarizeSkillRun(status)

	if status.Summary.ArtifactPath != actualPath {
		t.Fatalf("ArtifactPath = %q, want detected path %q", status.Summary.ArtifactPath, actualPath)
	}
	if status.Summary.ArtifactStatus != skillArtifactStatusVerified {
		t.Fatalf("ArtifactStatus = %q, want verified", status.Summary.ArtifactStatus)
	}
}

func TestSummarizeSkillRun_UsesDetectedArtifactPathWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir with spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	expectedPath := filepath.Join(dir, "missing output.pdf")
	actualPath := filepath.Join(dir, "translated output.pdf")
	if err := os.WriteFile(actualPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	status := &SkillRunStatus{
		RunID:          "run-artifact-spaces",
		Skill:          "paper-pdf-translator",
		Status:         skillRunStatusSuccess,
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "Output: " + actualPath,
		}},
	}

	summarizeSkillRun(status)

	if status.Summary.ArtifactPath != actualPath {
		t.Fatalf("ArtifactPath = %q, want detected path %q", status.Summary.ArtifactPath, actualPath)
	}
	if status.Summary.ArtifactStatus != skillArtifactStatusVerified {
		t.Fatalf("ArtifactStatus = %q, want verified", status.Summary.ArtifactStatus)
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

func TestMaterializeStdoutToExpectedOutputAllowsRunnerTrailerAfterJSON(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	expectedPath := filepath.Join(dir, "paper_output.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated with trailer"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-json-artifact-with-trailer",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "shell: cmd.exe\nelapsed: 1s\n───────────────\n" +
				"{\"ok\":true,\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}\n" +
				"[stderr] [PaperTranslator] translated 2 pages",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	got, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile(expected) error = %v", err)
	}
	if string(got) != "%PDF translated with trailer" {
		t.Fatalf("expected copied artifact bytes, got %q", string(got))
	}
}

func TestSelectArtifactFileFromJSONOutputAllowsTextAfterJSON(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated with parser trailer"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}\n" +
		"elapsed: 2m1s\ncommand: post-processing summary"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputHandlesBracesInJSONString(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated with brace text"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "progress before json\n" +
		"{\"ok\":true,\"message\":\"kept literal {brace} text\",\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}\n" +
		"runner trailer"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsArtifactObjectPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from artifact object"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "logs before result\n" +
		"{\"ok\":true,\"artifact\":{\"path\":\"" + filepath.ToSlash(sourcePath) + "\"}}\n" +
		"runner trailer"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsArtifactStringPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from artifact string"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"artifact\":\"" + filepath.ToSlash(sourcePath) + "\"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputPrefersExplicitArtifactOverFiles(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "paper.pdf")
	outputPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF original"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("%PDF translated"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}

	content := "{\"ok\":true," +
		"\"files\":[\"" + filepath.ToSlash(inputPath) + "\"]," +
		"\"artifact\":{\"path\":\"" + filepath.ToSlash(outputPath) + "\"}" +
		"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, outputPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want explicit artifact %q", got, outputPath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsOutputPath(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "paper.pdf")
	outputPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF original"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("%PDF translated from output_path"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}

	content := "{\"ok\":true," +
		"\"files\":[\"" + filepath.ToSlash(inputPath) + "\"]," +
		"\"output_path\":\"" + filepath.ToSlash(outputPath) + "\"" +
		"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, outputPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want output_path %q", got, outputPath)
	}
}

func TestSelectArtifactFileFromJSONOutputDoesNotFallbackToInputWhenExplicitOutputMismatches(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "paper.pdf")
	textOutputPath := filepath.Join(dir, "paper.txt")
	if err := os.WriteFile(inputPath, []byte("%PDF original"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}
	if err := os.WriteFile(textOutputPath, []byte("translated text sidecar"), 0o644); err != nil {
		t.Fatalf("WriteFile(text output) error = %v", err)
	}

	content := "{\"ok\":true," +
		"\"files\":[\"" + filepath.ToSlash(inputPath) + "\"]," +
		"\"output_path\":\"" + filepath.ToSlash(textOutputPath) + "\"" +
		"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); got != "" {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want empty instead of falling back to input PDF", got)
	}
}

func TestSelectArtifactFileFromJSONOutputDoesNotFallbackToInputWhenExplicitOutputMissing(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "paper.pdf")
	missingOutputPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF original"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}

	content := "{\"ok\":true," +
		"\"files\":[\"" + filepath.ToSlash(inputPath) + "\"]," +
		"\"output_path\":\"" + filepath.ToSlash(missingOutputPath) + "\"" +
		"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); got != "" {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want empty instead of falling back to input PDF", got)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsNestedResultOutputPath(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "paper.pdf")
	outputPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF original"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("%PDF translated from nested result output_path"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}

	content := "{\"ok\":true," +
		"\"files\":[\"" + filepath.ToSlash(inputPath) + "\"]," +
		"\"result\":{\"output_path\":\"" + filepath.ToSlash(outputPath) + "\"}" +
		"}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, outputPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want nested result output_path %q", got, outputPath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsFileObjectPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from file object"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"file\":{\"path\":\"" + filepath.ToSlash(sourcePath) + "\"}}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputKeepsObjectCandidateWithNonStringPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated despite metadata path"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"artifact\":{\"path\":{\"kind\":\"metadata\"},\"local_file_path\":\"" + filepath.ToSlash(sourcePath) + "\"}}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputIgnoresUnrelatedPathObject(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated with unrelated metadata path"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"path\":{\"kind\":\"metadata\"},\"artifact\":{\"path\":\"" + filepath.ToSlash(sourcePath) + "\"}}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsArtifactsArrayPath(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "paper.txt")
	pdfPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(textPath, []byte("sidecar text"), 0o644); err != nil {
		t.Fatalf("WriteFile(text) error = %v", err)
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF translated from artifacts array"), 0o644); err != nil {
		t.Fatalf("WriteFile(pdf) error = %v", err)
	}

	content := "{\"ok\":true,\"artifacts\":[" +
		"{\"path\":\"" + filepath.ToSlash(textPath) + "\"}," +
		"{\"path\":\"" + filepath.ToSlash(pdfPath) + "\"}" +
		"]}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, pdfPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, pdfPath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsOutputsArrayPath(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "paper.txt")
	pdfPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(textPath, []byte("sidecar text"), 0o644); err != nil {
		t.Fatalf("WriteFile(text) error = %v", err)
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF translated from outputs array"), 0o644); err != nil {
		t.Fatalf("WriteFile(pdf) error = %v", err)
	}

	content := "{\"ok\":true,\"outputs\":[" +
		"{\"path\":\"" + filepath.ToSlash(textPath) + "\"}," +
		"{\"path\":\"" + filepath.ToSlash(pdfPath) + "\"}" +
		"]}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, pdfPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, pdfPath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsFilesObjectArrayPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from files object array"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "{\"ok\":true,\"files\":[{\"local_file_path\":\"" + filepath.ToSlash(sourcePath) + "\"}]}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsMixedFilesArrayPath(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "paper.txt")
	pdfPath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(textPath, []byte("sidecar text"), 0o644); err != nil {
		t.Fatalf("WriteFile(text) error = %v", err)
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF translated from mixed files array"), 0o644); err != nil {
		t.Fatalf("WriteFile(pdf) error = %v", err)
	}

	content := "{\"ok\":true,\"files\":[" +
		"\"" + filepath.ToSlash(textPath) + "\"," +
		"{\"path\":\"" + filepath.ToSlash(pdfPath) + "\"}" +
		"]}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, pdfPath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, pdfPath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsTopLevelArrayPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from top-level array"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "[\"" + filepath.ToSlash(sourcePath) + "\"]"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsTrailingTopLevelArrayStringPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from trailing top-level array string"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "progress before json\n" +
		"[\"" + filepath.ToSlash(sourcePath) + "\"]\n" +
		"runner trailer"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputReadsTrailingTopLevelArrayObjectPath(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.zh.dual.pdf")
	if err := os.WriteFile(sourcePath, []byte("%PDF translated from top-level array object"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	content := "progress before json\n" +
		"[{\"path\":\"" + filepath.ToSlash(sourcePath) + "\"}]\n" +
		"runner trailer"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); !samePath(got, sourcePath) {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want %q", got, sourcePath)
	}
}

func TestSelectArtifactFileFromJSONOutputRejectsMismatchedExtension(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "paper.txt")
	if err := os.WriteFile(textPath, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile(text) error = %v", err)
	}

	content := "{\"ok\":true,\"artifact\":{\"path\":\"" + filepath.ToSlash(textPath) + "\"}}"

	if got := selectArtifactFileFromJSONOutput(content, ".pdf"); got != "" {
		t.Fatalf("selectArtifactFileFromJSONOutput() = %q, want empty for mismatched extension", got)
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

func TestMaterializeStdoutToExpectedOutputDoesNotCopyTextArtifactAsPDF(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "paper.txt")
	expectedPath := filepath.Join(dir, "paper_output.pdf")
	if err := os.WriteFile(sourcePath, []byte("not a pdf artifact"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-json-text-artifact-for-pdf",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "{\"ok\":true,\"files\":[\"" + filepath.ToSlash(sourcePath) + "\"]}",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("text artifact should not be copied as PDF, stat err=%v", err)
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


func TestMaterializeStdoutToExpectedOutputSkipsEmptyTextJSON(t *testing.T) {
	// When a skill outputs JSON with "text":"" (e.g. OCR found no text in image),
	// the runner should NOT write the raw JSON to the .txt file.
	dir := t.TempDir()
	expectedPath := filepath.Join(dir, "output.txt")
	runner := NewSkillRunner(nil)
	run := &skillRun{status: SkillRunStatus{
		RunID:          "run-empty-text-json",
		ExpectedOutput: expectedPath,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "shell: cmd.exe\nelapsed: 2s\n\U0001F4C2 /tmp/test\ncommand: node ocr.js\n\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n{\"ok\":true,\"text\":\"\",\"lines\":[],\"boxes\":[],\"scores\":[],\"source\":\"C:\\\\test.png\"}",
		}},
	}}

	runner.materializeStdoutToExpectedOutput(run)

	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		data, _ := os.ReadFile(expectedPath)
		t.Fatalf("empty-text JSON should not be materialized to .txt, got %d bytes: %s", len(data), string(data))
	}
}

func TestExtractTextFieldFromJSONFallsBackToLines(t *testing.T) {
	// When JSON has "lines" array but no "text" field, extract from lines.
	content := `{"ok":true,"lines":["Hello World","Line 2"],"scores":[0.98,0.99]}`
	extracted := extractTextFieldFromJSON(content)
	if extracted != "Hello World\nLine 2" {
		t.Fatalf("expected lines joined, got %q", extracted)
	}
}

func TestExtractTextFieldFromJSONPrefersTextOverLines(t *testing.T) {
	// "text" field takes priority over "lines" array
	content := `{"ok":true,"text":"combined text","lines":["Hello","World"]}`
	extracted := extractTextFieldFromJSON(content)
	if extracted != "combined text" {
		t.Fatalf("expected 'combined text', got %q", extracted)
	}
}

func TestStripSkillRunnerMetadataPreservesStderrJSON(t *testing.T) {
	// When stdout is empty and stderr contains structured JSON (e.g. rapidocr v1.4+
	// outputting via Python logging), the JSON should be preserved and its
	// [stderr] prefix stripped.
	output := "shell: cmd.exe\nelapsed: 7s\n\U0001F4C2 /tmp/skill\ncommand: node ocr.js\n\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n[stderr] {\"ok\":true,\"text\":\"Hello OCR\",\"lines\":[\"Hello OCR\"]}"
	result := stripSkillRunnerMetadataFromOutput(output)
	if !strings.Contains(result, "Hello OCR") {
		t.Fatalf("expected stderr JSON preserved, got %q", result)
	}
	if strings.Contains(result, "[stderr]") {
		t.Fatalf("expected [stderr] prefix stripped, got %q", result)
	}
	// Verify it's valid JSON
	extracted := extractTextFieldFromJSON(result)
	if extracted != "Hello OCR" {
		t.Fatalf("expected extractTextFieldFromJSON to work on preserved stderr JSON, got %q", extracted)
	}
}
