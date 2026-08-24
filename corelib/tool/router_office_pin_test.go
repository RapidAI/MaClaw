package tool

import (
	"strings"
	"testing"
)

func TestRoute_DocumentPathDoesNotGrantOffice(t *testing.T) {
	r := NewRouter(nil)
	// Fill allTools with many core names so MaxToolBudget pressure is real.
	all := make([]map[string]interface{}, 0, 40)
	for _, name := range []string{
		"bash", "read_file", "office", "craft_tool", "search_files", "manage_skill",
		"memory", "web_fetch", "list_directory", "write_file", "edit_file", "task",
		"async_wait", "compress_context", "discover_tool", "call_mcp_tool", "tts",
		"asr", "record_audio", "download_file", "set_nickname", "goal",
		"list_sessions", "get_session_output", "get_session_events", "read_tool_result",
		"ripgrep", "Glob", "FileRead",
	} {
		all = append(all, toolDefForRoute(name))
	}
	// Force many tools into core via session pins to pressure budget.
	for _, name := range []string{"bash", "read_file", "memory", "task", "web_fetch", "manage_skill", "tts", "asr"} {
		r.ActivateSessionTool(name)
	}
	msg := "[用户选择的本地文件路径]\nC:\\Users\\ma\\a.docx"
	selected := r.RouteWithOptions(msg, all, RouteOptions{SkipUnifiedClassifier: true})
	found := false
	for _, tdef := range selected {
		if ExtractToolName(tdef) == "office" {
			found = true
			break
		}
	}
	if found {
		t.Fatal("document path text must not grant office under budget pressure")
	}
}

func TestRoute_GUIDocxPathDoesNotGrantOffice(t *testing.T) {
	r := NewRouter(nil)
	all := []map[string]interface{}{
		toolDefForRoute("bash"),
		toolDefForRoute("read_file"),
		toolDefForRoute("office"),
		toolDefForRoute("craft_tool"),
		toolDefForRoute("search_files"),
		toolDefForRoute("manage_skill"),
		toolDefForRoute("memory"),
		toolDefForRoute("web_fetch"),
		toolDefForRoute("list_directory"),
		toolDefForRoute("write_file"),
		toolDefForRoute("edit_file"),
		toolDefForRoute("task"),
		toolDefForRoute("async_wait"),
		toolDefForRoute("compress_context"),
		toolDefForRoute("discover_tool"),
		toolDefForRoute("call_mcp_tool"),
		toolDefForRoute("tts"),
		toolDefForRoute("asr"),
		toolDefForRoute("record_audio"),
		toolDefForRoute("download_file"),
		toolDefForRoute("set_nickname"),
		toolDefForRoute("goal"),
		toolDefForRoute("list_sessions"),
		toolDefForRoute("get_session_output"),
		toolDefForRoute("get_session_events"),
		toolDefForRoute("read_tool_result"),
	}
	msg := "对比评分表 技术部分能得多少分\n\n[用户选择的本地文件路径]\nC:\\Users\\ma139\\Desktop\\对比评分表 技术部分能得多少分.docx\n\n" +
		"[系统已自动解析文档正文 — 优先基于下列内容回答]\n--- auto_extract: begin path=\"C:\\\\x.docx\" format=\"docx\" truncated=true ---"
	// Skip UIC: host presentation text alone is not capability evidence.
	selected := r.RouteWithOptions(msg, all, RouteOptions{SkipUnifiedClassifier: true})
	names := map[string]bool{}
	for _, tdef := range selected {
		names[ExtractToolName(tdef)] = true
	}
	if names["office"] {
		var list []string
		for n := range names {
			list = append(list, n)
		}
		t.Fatalf("GUI path marker must not pin office, got %v", list)
	}
}

func TestRoute_DoesNotPinOfficeForPlainChat(t *testing.T) {
	r := NewRouter(nil)
	all := []map[string]interface{}{
		toolDefForRoute("bash"),
		toolDefForRoute("read_file"),
		toolDefForRoute("office"),
		toolDefForRoute("manage_skill"),
		toolDefForRoute("memory"),
		toolDefForRoute("task"),
		toolDefForRoute("async_wait"),
		toolDefForRoute("compress_context"),
		toolDefForRoute("list_directory"),
		toolDefForRoute("write_file"),
		toolDefForRoute("edit_file"),
		toolDefForRoute("web_fetch"),
		toolDefForRoute("discover_tool"),
		toolDefForRoute("call_mcp_tool"),
		toolDefForRoute("tts"),
		toolDefForRoute("asr"),
		toolDefForRoute("record_audio"),
		toolDefForRoute("download_file"),
		toolDefForRoute("set_nickname"),
		toolDefForRoute("goal"),
		toolDefForRoute("list_sessions"),
		toolDefForRoute("get_session_output"),
		toolDefForRoute("get_session_events"),
		toolDefForRoute("read_tool_result"),
	}
	selected := r.RouteWithOptions("今天天气怎么样", all, RouteOptions{SkipUnifiedClassifier: true})
	for _, tdef := range selected {
		if ExtractToolName(tdef) == "office" {
			t.Fatal("office must not be pinned for plain chat without document signals")
		}
	}
}

func toolDefForRoute(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": name + " tool for routing tests " + strings.Repeat("x", 8),
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}
