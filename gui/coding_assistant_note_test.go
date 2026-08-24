package main

import "testing"

func TestCompactCodingAssistantNoteFiltersInternal(t *testing.T) {
	if got := compactCodingAssistantNote("Compiling the new hello world."); got == "" {
		t.Fatal("expected engineer note to survive")
	}
	if got := compactCodingAssistantNote("I will add a compile check next."); got == "" {
		t.Fatal("expected engineer note to survive")
	}
	cases := []string{
		"## 执行报告 总计：1",
		"## 验证结果",
		"## 涉及文件",
		"### 计划执行结果",
		"执行步骤： ☐ T1 write",
		"质量审计 PASSED",
		`{"name":"write_file","arguments":{}}`,
		"<think>planning</think>",
		"tool_call write_file",
	}
	for _, input := range cases {
		if got := compactCodingAssistantNote(input); got != "" {
			t.Fatalf("internal note %q should be dropped, got %q", input, got)
		}
	}
}

func TestCodingAssistantNoteReadyStillRequiresBoundary(t *testing.T) {
	if codingAssistantNoteReady("short", "x") {
		t.Fatal("short fragment should not be ready")
	}
	if !codingAssistantNoteReady("Compiling the new hello world.", ".") {
		t.Fatal("sentence should be ready")
	}
}
