package main

import (
	"testing"
)

func TestExtractFailedSkillInfo_RunSkillFailed(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "run_skill",
				Arguments: `{"name": "pdf-converter", "input": "test.md"}`,
			},
		},
	}
	toolResults := []string{"❌ Skill 执行失败: status: failed\n依赖 wkhtmltopdf 未安装"}

	name, errMsg := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "pdf-converter" {
		t.Fatalf("expected skill name 'pdf-converter', got %q", name)
	}
	if errMsg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestExtractFailedSkillInfo_ManageSkillRunFailed(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "manage_skill",
				Arguments: `{"action": "run", "name": "my-skill"}`,
			},
		},
	}
	toolResults := []string{"❌ status: failed — timeout after 60s"}

	name, errMsg := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "my-skill" {
		t.Fatalf("expected skill name 'my-skill', got %q", name)
	}
	if errMsg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestExtractFailedSkillInfo_ManageSkillNonRunAction(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "manage_skill",
				Arguments: `{"action": "list"}`,
			},
		},
	}
	toolResults := []string{"❌ 列表获取失败"}

	name, _ := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "" {
		t.Fatalf("expected empty skill name for non-run action, got %q", name)
	}
}

func TestExtractFailedSkillInfo_SuccessfulSkill(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "run_skill",
				Arguments: `{"name": "pdf-converter"}`,
			},
		},
	}
	toolResults := []string{"✅ Skill 已启动\n## 运行信息\n- run_id: run-1\n- status: success"}

	name, _ := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "" {
		t.Fatalf("expected empty skill name for successful skill, got %q", name)
	}
}

func TestExtractFailedSkillInfo_NoSkillTools(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "bash",
				Arguments: `{"command": "ls"}`,
			},
		},
	}
	toolResults := []string{"file1.txt\nfile2.txt"}

	name, _ := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "" {
		t.Fatalf("expected empty skill name for non-skill tool, got %q", name)
	}
}

func TestExtractFailedSkillInfo_EmptyInputs(t *testing.T) {
	name, _ := extractFailedSkillInfo(nil, nil)
	if name != "" {
		t.Fatalf("expected empty skill name for nil inputs, got %q", name)
	}

	name, _ = extractFailedSkillInfo([]llmToolCall{}, []string{})
	if name != "" {
		t.Fatalf("expected empty skill name for empty inputs, got %q", name)
	}
}

func TestExtractFailedSkillInfo_MismatchedLengths(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "run_skill",
				Arguments: `{"name": "test-skill"}`,
			},
		},
	}
	// Mismatched: 1 tool call but 0 results
	name, _ := extractFailedSkillInfo(toolCalls, []string{})
	if name != "" {
		t.Fatalf("expected empty skill name for mismatched lengths, got %q", name)
	}
}

func TestExtractFailedSkillInfo_SkillNameParam(t *testing.T) {
	// Test with skill_name parameter instead of name
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "run_skill",
				Arguments: `{"skill_name": "alt-skill"}`,
			},
		},
	}
	toolResults := []string{"❌ status: failed — dependency missing"}

	name, _ := extractFailedSkillInfo(toolCalls, toolResults)
	if name != "alt-skill" {
		t.Fatalf("expected skill name 'alt-skill', got %q", name)
	}
}

func TestExtractFailedSkillInfo_TruncatesLongError(t *testing.T) {
	toolCalls := []llmToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "run_skill",
				Arguments: `{"name": "verbose-skill"}`,
			},
		},
	}
	// Create a very long error message (>300 chars)
	longError := "❌ status: failed — "
	for i := 0; i < 400; i++ {
		longError += "x"
	}
	toolResults := []string{longError}

	_, errMsg := extractFailedSkillInfo(toolCalls, toolResults)
	if len(errMsg) > 300 {
		t.Fatalf("expected error message truncated to 300 chars, got %d chars", len(errMsg))
	}
}
