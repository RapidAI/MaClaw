package main

import (
	"strings"
	"testing"
)

func TestRenderSpecPrompt_Requirements(t *testing.T) {
	ctx := PromptContext{
		Requirements: "实现用户登录功能",
		TechStack:    "Go + React",
	}
	got, err := RenderSpecPrompt("requirements", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "实现用户登录功能") {
		t.Error("requirements prompt should contain the original requirements")
	}
	if !strings.Contains(got, "Go + React") {
		t.Error("requirements prompt should contain tech stack")
	}
	if !strings.Contains(got, "验收标准") {
		t.Error("requirements prompt should mention acceptance criteria")
	}
}

func TestRenderSpecPrompt_Design(t *testing.T) {
	ctx := PromptContext{
		ProjectName:  "my-project",
		Requirements: "用户登录需求文档...",
		TechStack:    "Go",
	}
	got, err := RenderSpecPrompt("design", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "my-project") {
		t.Error("design prompt should contain project name")
	}
	if !strings.Contains(got, "接口定义") {
		t.Error("design prompt should mention interface definitions")
	}
	if !strings.Contains(got, "数据模型") {
		t.Error("design prompt should mention data models")
	}
}

func TestRenderSpecPrompt_UnknownPhase(t *testing.T) {
	_, err := RenderSpecPrompt("nonexistent", PromptContext{})
	if err == nil {
		t.Error("should return error for unknown phase")
	}
}

func TestFormatCriteria(t *testing.T) {
	tests := []struct {
		name     string
		criteria []string
		want     string
	}{
		{"empty", nil, ""},
		{"empty slice", []string{}, ""},
		{"single", []string{"代码可编译"}, "1. 代码可编译\n"},
		{"multiple", []string{"可编译", "有测试", "有文档"}, "1. 可编译\n2. 有测试\n3. 有文档\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCriteria(tt.criteria)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubTask_AcceptanceCriteria(t *testing.T) {
	task := SubTask{
		Index:              0,
		Description:        "实现登录 API",
		AcceptanceCriteria: []string{"POST /api/login 返回 JWT", "密码错误返回 401"},
	}
	if len(task.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria, got %d", len(task.AcceptanceCriteria))
	}
}

func TestSwarmRun_SpecFields(t *testing.T) {
	run := &SwarmRun{
		ID:           "test-run",
		Requirements: "结构化需求文档",
		DesignDoc:    "结构化设计文档",
	}
	if run.Requirements == "" {
		t.Error("Requirements field should be set")
	}
	if run.DesignDoc == "" {
		t.Error("DesignDoc field should be set")
	}
}

func TestPhaseRequirements_Constant(t *testing.T) {
	if PhaseRequirements != "requirements" {
		t.Errorf("PhaseRequirements = %q, want 'requirements'", PhaseRequirements)
	}
	if PhaseDesign != "design" {
		t.Errorf("PhaseDesign = %q, want 'design'", PhaseDesign)
	}
}

func TestTaskVerifier_WithAcceptanceCriteria(t *testing.T) {
	v := NewTaskVerifier(MaclawLLMConfig{})

	// Empty output should still fail regardless of criteria
	verdict, err := v.Verify("implement login", "", "returns JWT", "handles errors")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Pass {
		t.Error("empty output should not pass even with criteria")
	}
}

func TestDeveloperPrompt_IncludesCriteria(t *testing.T) {
	ctx := PromptContext{
		ProjectName:        "test-project",
		TechStack:          "Go",
		TaskDesc:           "实现登录功能",
		AcceptanceCriteria: "1. POST /login 返回 JWT\n2. 密码错误返回 401\n",
	}
	got, err := RenderPrompt(RoleDeveloper, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "验收标准") {
		t.Error("developer prompt should contain acceptance criteria section")
	}
	if !strings.Contains(got, "POST /login") {
		t.Error("developer prompt should contain the actual criteria")
	}
}

func TestDeveloperPrompt_NoCriteria(t *testing.T) {
	ctx := PromptContext{
		ProjectName: "test-project",
		TechStack:   "Go",
		TaskDesc:    "实现登录功能",
	}
	got, err := RenderPrompt(RoleDeveloper, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// When no criteria, the section should not appear
	if strings.Contains(got, "验收标准（你的代码必须满足以下所有条件）") {
		t.Error("developer prompt should not contain criteria section when empty")
	}
}

func TestSplitViaAgent_WithAcceptanceCriteria(t *testing.T) {
	splitter := NewTaskSplitter(MaclawLLMConfig{})
	input := `[
		{
			"description": "实现用户注册",
			"expected_files": ["user.go"],
			"dependencies": [],
			"acceptance_criteria": ["注册成功返回200", "邮箱重复返回409"]
		}
	]`
	tasks, err := splitter.SplitViaAgent(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if len(tasks[0].AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria, got %d", len(tasks[0].AcceptanceCriteria))
	}
	if tasks[0].AcceptanceCriteria[0] != "注册成功返回200" {
		t.Errorf("criteria[0] = %q", tasks[0].AcceptanceCriteria[0])
	}
}
