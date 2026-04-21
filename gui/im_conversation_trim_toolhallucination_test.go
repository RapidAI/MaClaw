package main

import (
	"testing"
)

func testToolDefs(names ...string) []map[string]interface{} {
	var defs []map[string]interface{}
	for _, name := range names {
		defs = append(defs, map[string]interface{}{
			"function": map[string]interface{}{
				"name":        name,
				"description": "test tool " + name,
			},
		})
	}
	return defs
}

func TestDetectToolAvailabilityHallucination(t *testing.T) {
	tools := testToolDefs("bash", "write_file", "read_file", "manage_skill", "web_search", "screenshot")

	tests := []struct {
		name    string
		input   string
		tools   []map[string]interface{}
		wantHit bool
	}{
		{
			name:    "chinese: 没有 bash 工具",
			input:   "我目前没有 bash 工具可用",
			tools:   tools,
			wantHit: true,
		},
		{
			name:    "chinese: bash 工具不可用",
			input:   "bash 工具不可用",
			tools:   tools,
			wantHit: true,
		},
		{
			name:    "chinese: 没有 manage_skill 工具",
			input:   "没有找到 manage_skill 工具",
			tools:   tools,
			wantHit: true,
		},
		{
			name:    "chinese: 没有 run_skill 和 bash 工具 — run_skill NOT in list",
			input:   "没有 run_skill 和 bash 工具",
			tools:   tools,
			wantHit: true, // bash IS in list
		},
		{
			name:    "english: don't have bash tool",
			input:   "I don't have the bash tool available",
			tools:   tools,
			wantHit: true,
		},
		{
			name:    "english: do not have write_file",
			input:   "I do not have write_file tool",
			tools:   tools,
			wantHit: true,
		},
		{
			name:    "no false positive: 没有找到 bash 脚本 (no 工具 suffix)",
			input:   "没有找到 bash 脚本文件",
			tools:   tools,
			wantHit: false,
		},
		{
			name:    "no false positive: normal usage",
			input:   "我用 bash 工具执行了命令",
			tools:   tools,
			wantHit: false,
		},
		{
			name:    "no false positive: no tool mention",
			input:   "北大的DataFlex是一个数据中心动态训练框架",
			tools:   tools,
			wantHit: false,
		},
		{
			name:    "code block ignored",
			input:   "说明：\n```\n# 没有 bash 工具\n```\n以上是示例",
			tools:   tools,
			wantHit: false,
		},
		{
			name:    "empty input",
			input:   "",
			tools:   tools,
			wantHit: false,
		},
		{
			name:    "empty tool list",
			input:   "没有 bash 工具",
			tools:   nil,
			wantHit: false,
		},
		{
			name:    "tool not in list is not flagged",
			input:   "没有 ssh 工具",
			tools:   testToolDefs("bash", "write_file"),
			wantHit: false,
		},
		{
			name:    "tool in list is flagged",
			input:   "没有 ssh 工具",
			tools:   testToolDefs("bash", "ssh", "write_file"),
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectToolAvailabilityHallucination(tt.input, tt.tools)
			gotHit := result != ""
			if gotHit != tt.wantHit {
				t.Errorf("detectToolAvailabilityHallucination(%q) = %q, wantHit=%v gotHit=%v",
					tt.input, result, tt.wantHit, gotHit)
			}
		})
	}
}

func TestStripCodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no code blocks", "hello world", "hello world"},
		{"single code block", "before\n```\ncode\n```\nafter", "before\n\nafter"},
		{"unclosed code block", "before\n```\ncode", "before\n"},
		{"multiple code blocks", "a\n```\nb\n```\nc\n```\nd\n```\ne", "a\n\nc\n\ne"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeBlocks(tt.input)
			if got != tt.want {
				t.Errorf("stripCodeBlocks(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
