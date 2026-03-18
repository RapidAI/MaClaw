package main

import (
	"strings"
	"testing"
)

func TestDynamicToolBuilder_BuildAll(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Description: "tool a", Category: ToolCategoryBuiltin, Status: RegToolAvailable})
	r.Register(RegisteredTool{Name: "b", Description: "tool b", Category: ToolCategoryMCP, Status: RegToolAvailable})

	b := NewDynamicToolBuilder(r)
	defs := b.BuildAll()
	if len(defs) != 2 {
		t.Fatalf("BuildAll len = %d, want 2", len(defs))
	}
}

func TestDynamicToolBuilder_Build_UnderThreshold(t *testing.T) {
	r := NewToolRegistry()
	for i := 0; i < 15; i++ {
		r.Register(RegisteredTool{
			Name: "tool_" + string(rune('a'+i)), Category: ToolCategoryBuiltin, Status: RegToolAvailable,
		})
	}
	b := NewDynamicToolBuilder(r)
	defs := b.Build("hello")
	if len(defs) != 15 {
		t.Errorf("Build under threshold: len = %d, want 15", len(defs))
	}
}

func TestDynamicToolBuilder_Build_OverThreshold_FiltersNonBuiltin(t *testing.T) {
	r := NewToolRegistry()
	// 10 builtin tools
	for i := 0; i < 10; i++ {
		r.Register(RegisteredTool{
			Name: "builtin_" + string(rune('a'+i)), Category: ToolCategoryBuiltin, Status: RegToolAvailable,
		})
	}
	// 15 MCP tools
	for i := 0; i < 15; i++ {
		r.Register(RegisteredTool{
			Name: "mcp_" + string(rune('a'+i)), Category: ToolCategoryMCP, Status: RegToolAvailable,
			Tags: []string{"mcp"}, Description: "mcp tool",
		})
	}
	b := NewDynamicToolBuilder(r)
	defs := b.Build("some message")
	// Should have 10 builtins + up to 15 dynamic
	if len(defs) < 10 {
		t.Errorf("Build over threshold: len = %d, want >= 10 builtins", len(defs))
	}
	if len(defs) > 25 {
		t.Errorf("Build over threshold: len = %d, want <= 25", len(defs))
	}
}

func TestDynamicToolBuilder_Build_GroupActivation(t *testing.T) {
	r := NewToolRegistry()
	// 10 builtin
	for i := 0; i < 10; i++ {
		r.Register(RegisteredTool{
			Name: "builtin_" + string(rune('a'+i)), Category: ToolCategoryBuiltin, Status: RegToolAvailable,
		})
	}
	// 15 non-builtin, 3 with "git" tag
	for i := 0; i < 12; i++ {
		r.Register(RegisteredTool{
			Name: "other_" + string(rune('a'+i)), Category: ToolCategoryMCP, Status: RegToolAvailable,
			Tags: []string{"other"},
		})
	}
	r.Register(RegisteredTool{Name: "git_status", Category: ToolCategoryNonCode, Status: RegToolAvailable, Tags: []string{"git", "vcs"}})
	r.Register(RegisteredTool{Name: "git_diff", Category: ToolCategoryNonCode, Status: RegToolAvailable, Tags: []string{"git", "vcs"}})
	r.Register(RegisteredTool{Name: "git_commit", Category: ToolCategoryNonCode, Status: RegToolAvailable, Tags: []string{"git", "vcs"}})

	b := NewDynamicToolBuilder(r)
	defs := b.Build("使用 git 工具查看状态")

	// All git tools should be included via group activation
	gitCount := 0
	for _, d := range defs {
		fn := d["function"].(map[string]interface{})
		name := fn["name"].(string)
		if strings.HasPrefix(name, "git_") {
			gitCount++
		}
	}
	if gitCount != 3 {
		t.Errorf("group activation: git tools = %d, want 3", gitCount)
	}
}

func TestDetectGroupTags_Chinese(t *testing.T) {
	tags := detectGroupTags("使用数据库工具查询")
	if !tags["database"] || !tags["sql"] {
		t.Errorf("expected database/sql tags for 数据库, got %v", tags)
	}
}

func TestDetectGroupTags_English(t *testing.T) {
	tags := detectGroupTags("use git tools")
	if !tags["git"] || !tags["vcs"] {
		t.Errorf("expected git/vcs tags, got %v", tags)
	}
}

func TestDetectGroupTags_NoMatch(t *testing.T) {
	tags := detectGroupTags("hello world")
	if len(tags) != 0 {
		t.Errorf("expected no tags, got %v", tags)
	}
}

func TestRegisteredToolToDef(t *testing.T) {
	tool := RegisteredTool{
		Name:        "test_tool",
		Description: "a test",
		InputSchema: map[string]interface{}{
			"path": map[string]string{"type": "string"},
		},
		Required: []string{"path"},
	}
	def := registeredToolToDef(tool)
	fn := def["function"].(map[string]interface{})
	if fn["name"] != "test_tool" {
		t.Errorf("name = %v, want test_tool", fn["name"])
	}
	params := fn["parameters"].(map[string]interface{})
	req := params["required"].([]string)
	if len(req) != 1 || req[0] != "path" {
		t.Errorf("required = %v, want [path]", req)
	}
}

func TestTokenizeSimple(t *testing.T) {
	tokens := tokenizeSimple("hello_world foo-bar")
	if len(tokens) != 4 {
		t.Errorf("tokenize len = %d, want 4, tokens = %v", len(tokens), tokens)
	}
}
