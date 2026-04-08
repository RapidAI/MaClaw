package main

import (
	"strings"
	"testing"
)

func TestSummarizeSkillRun_PopulatesCurrentAndLastStep(t *testing.T) {
	status := &SkillRunStatus{
		RunID:  "run-1",
		Skill:  "demo",
		Status: "running",
		Steps: []StepResult{
			{Index: 0, Action: "create_session", Status: "success", Output: "会话已创建: ID=sess-1"},
			{Index: 1, Action: "send_and_observe", Status: "running"},
		},
		Session: &SkillRunSessionMeta{SessionID: "sess-1"},
	}
	summarizeSkillRun(status)
	if !status.Summary.HasSessionBinding {
		t.Fatal("expected session binding summary")
	}
	if status.Summary.CurrentStep != "send_and_observe" || status.Summary.CurrentStepStatus != "running" {
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
	status := &SkillRunStatus{
		RunID:  "run-craft",
		Skill:  "pptx-generator",
		Status: "success",
		Steps: []StepResult{{
			Index:  0,
			Action: "craft_tool",
			Status: "success",
			Output: "📝 脚本语言: python\n📁 脚本路径: C:/tmp/tool.py\n\n✅ 脚本执行成功",
		}},
	}
	summarizeSkillRun(status)
	if !status.Summary.NeedsArtifactVerification {
		t.Fatalf("expected summary to require artifact verification, got %#v", status.Summary)
	}
}

func TestSummarizeSkillRun_PopulatesErrorSnippet(t *testing.T) {
	status := &SkillRunStatus{
		RunID:  "run-2",
		Skill:  "demo",
		Status: "failed",
		Steps:  []StepResult{{Index: 0, Action: "send_and_observe", Status: "failed", Error: "network timeout while waiting for output"}},
		Error:  "step failed",
	}
	summarizeSkillRun(status)
	if status.Summary.CurrentStep != "send_and_observe" || status.Summary.CurrentStepStatus != "failed" {
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
		Status:  "running",
		Steps:   []StepResult{{Index: 0, Action: "create_session", Status: "success", Output: "会话已创建: ID=sess-3"}},
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
