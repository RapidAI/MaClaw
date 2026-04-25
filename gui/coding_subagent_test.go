package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCodingSubAgentSystemPrompt_Minimal(t *testing.T) {
	task := &TaskItem{
		Index:       1,
		Title:       "实现 Player 类",
		Description: "实现玩家角色的移动和跳跃逻辑",
		Files:       []string{"src/player.h", "src/player.cpp"},
		AcceptanceCriteria: []string{
			"玩家可以左右移动",
			"玩家可以跳跃",
		},
	}

	prompt := buildCodingSubAgentSystemPrompt(task, "D:\\workprj\\morio", "", "", nil)

	// Should contain coding rules.
	if !strings.Contains(prompt, "编码执行器") {
		t.Error("prompt should contain coding executor role")
	}
	// Should contain project path.
	if !strings.Contains(prompt, "D:\\workprj\\morio") {
		t.Error("prompt should contain project path")
	}
	// Should contain platform info.
	if !strings.Contains(prompt, "平台:") {
		t.Error("prompt should contain platform info")
	}
	// Should contain edit_file/edit_lines strategy.
	if !strings.Contains(prompt, "edit_file") || !strings.Contains(prompt, "edit_lines") {
		t.Error("prompt should contain edit_file/edit_lines strategy")
	}
	if !strings.Contains(prompt, "禁止用 write_file 重写已有文件") {
		t.Error("prompt should contain write_file prohibition for existing files")
	}
	// Should NOT contain IM rules, memory, browser, SSH, etc.
	for _, noise := range []string{"飞书", "微信", "QQ", "Browser:", "SSH", "memory", "screenshot", "IM 通道"} {
		if strings.Contains(prompt, noise) {
			t.Errorf("prompt should NOT contain non-coding noise: %q", noise)
		}
	}
}

func TestBuildCodingSubAgentSystemPrompt_WithContext(t *testing.T) {
	task := &TaskItem{Index: 2, Title: "实现关卡加载"}

	longReq := strings.Repeat("需求内容。", 200) // ~1000 chars
	longDesign := strings.Repeat("设计内容。", 200)
	prevOutputs := []string{"src/player.h (已完成)", "src/player.cpp (已完成)"}

	prompt := buildCodingSubAgentSystemPrompt(task, "/project", longReq, longDesign, prevOutputs)

	// Context should be truncated.
	if len([]rune(prompt)) > 5000 {
		t.Errorf("prompt too long: %d runes, expected <5000", len([]rune(prompt)))
	}
	// Should contain previous outputs.
	if !strings.Contains(prompt, "src/player.h") {
		t.Error("prompt should contain previous task outputs")
	}
}

func TestBuildCodingToolDefinitions_OnlyCodingTools(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()

	if len(tools) != 6 {
		t.Fatalf("expected 6 coding tools, got %d", len(tools))
	}

	expectedNames := map[string]bool{
		"read_file":      true,
		"write_file":     true,
		"edit_file":      true,
		"edit_lines":     true,
		"bash":           true,
		"list_directory":  true,
	}

	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !expectedNames[name] {
			t.Errorf("unexpected tool: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		t.Errorf("missing tools: %v", expectedNames)
	}
}

func TestBuildCodingToolDefinitions_TokenEstimate(t *testing.T) {
	tools := buildCodingToolDefinitionsFallback()

	// Estimate total token cost of tool definitions.
	// Each tool definition is roughly 100-200 tokens in JSON.
	// 5 tools should be well under 2000 tokens.
	totalChars := 0
	for _, tool := range tools {
		// Rough estimate: JSON marshal and count chars.
		data, _ := json.Marshal(tool)
		totalChars += len(data)
	}

	// At ~2.5 bytes/token, total should be < 2000 tokens.
	estimatedTokens := totalChars * 10 / 25
	if estimatedTokens > 2000 {
		t.Errorf("tool definitions too large: ~%d tokens (chars=%d), expected <2000", estimatedTokens, totalChars)
	}
	t.Logf("tool definitions: %d chars, ~%d tokens", totalChars, estimatedTokens)
}

func TestTruncateRunesForSubAgent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		wantTrunc bool
	}{
		{"short", "hello", 10, false},
		{"exact", "hello", 5, false},
		{"truncate", "hello world this is a long string", 10, true},
		{"chinese", "这是一段中文测试文本，用于验证截断功能", 10, true},
		{"paragraph_boundary", "第一段\n\n第二段\n\n第三段很长很长", 15, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateRunesForSubAgent(tt.input, tt.max)
			if tt.wantTrunc {
				if !strings.Contains(result, "截断") {
					t.Errorf("expected truncation marker, got: %s", result)
				}
				if len([]rune(result)) > tt.max+20 { // allow some slack for the marker
					t.Errorf("result too long: %d runes", len([]rune(result)))
				}
			} else {
				if result != tt.input {
					t.Errorf("expected no truncation, got: %s", result)
				}
			}
		})
	}
}


func TestBuildCodingToolDefinitionsFromRegistry_NilHandler(t *testing.T) {
	// When handler is nil, should fall back to inline definitions.
	tools := buildCodingToolDefinitionsFromRegistry(nil)
	if len(tools) != 6 {
		t.Fatalf("expected 6 fallback tools, got %d", len(tools))
	}
}

func TestCodingSubAgentToolNames_SingleSource(t *testing.T) {
	// Verify that codingSubAgentToolNames is the single source of truth:
	// every tool in the fallback definitions must be in the map.
	fallback := buildCodingToolDefinitionsFallback()
	for _, tool := range fallback {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !codingSubAgentToolNames[name] {
			t.Errorf("fallback tool %q not in codingSubAgentToolNames", name)
		}
	}
	// And the map should have exactly as many entries as the fallback.
	if len(codingSubAgentToolNames) != len(fallback) {
		t.Errorf("codingSubAgentToolNames has %d entries, fallback has %d tools — they should match",
			len(codingSubAgentToolNames), len(fallback))
	}
}
