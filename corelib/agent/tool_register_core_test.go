package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestCoreToolNames_AllRegistered verifies that every tool declared in
// CoreToolNames (the router's "always include" set) is either:
//   - registered by RegisterCoreTools, or
//   - a GUI-only tool that TUI intentionally does not support.
//
// This test is the mechanism-level guard against the "tool declared in
// CoreToolNames but missing from CoreToolRegistry" class of bugs.
// When a new tool is added to CoreToolNames, this test forces the developer
// to either register it in RegisterCoreTools or add it to the exclusion list.
func TestCoreToolNames_AllRegistered(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	// GUI-only tools that require desktop infrastructure (Wails, browser
	// engine, remote session manager, etc.) and are intentionally not
	// available in TUI. Each entry must have a comment explaining why.
	guiOnly := map[string]bool{
		"list_sessions":      true, // requires RemoteSessionManager (Wails)
		"get_session_output": true, // requires RemoteSessionManager (Wails)
		"get_session_events": true, // requires RemoteSessionManager (Wails)
		"call_mcp_tool":      true, // requires MCPRegistry (Wails)
		"set_nickname":       true, // requires GUI user model
		"discover_tool":      true, // requires GUI ToolRegistry + deferred tools
		"async_wait":         true, // requires IMMessageHandler + LoopContext cancel channel (GUI)
		"compress_context":   true, // requires GUI run-loop context compression state
	}

	missing := reg.MissingTools(tool.CoreToolNames)
	for _, name := range missing {
		if guiOnly[name] {
			continue
		}
		t.Errorf("CoreToolNames declares %q but RegisterCoreTools does not register it.\n"+
			"Fix: either register it in RegisterCoreTools (with ExtraHandlers if host-specific),\n"+
			"or add it to guiOnly in this test with a comment explaining why.", name)
	}
}

func TestRegisterCoreTools_DescribesStructuredOfficeFormatBoundaries(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})
	definitions := reg.BuildDefinitions()
	byName := make(map[string]map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		function, _ := definition["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		byName[name] = function
	}
	for name, want := range map[string]string{
		"read_document": "PowerPoint (.ppt/.pptx)",
		"read_excel":    ".xlsx/.csv modern, .xls legacy",
		"read_pptx":     "PPTX-only",
	} {
		function := byName[name]
		if !strings.Contains(fmt.Sprint(function["description"]), want) {
			t.Fatalf("%s description missing %q: %#v", name, want, function)
		}
	}
}

func TestCoreSearchToolsExposeLargeRepoSearchOptions(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	assertProps := func(toolName string, props ...string) {
		t.Helper()
		reg.mu.RLock()
		entry := reg.tools[toolName]
		reg.mu.RUnlock()
		if entry == nil {
			t.Fatalf("tool %q is not registered", toolName)
		}
		for _, prop := range props {
			if _, ok := entry.Properties[prop]; !ok {
				t.Fatalf("tool %q does not expose property %q", toolName, prop)
			}
		}
	}

	assertProps("ripgrep",
		"glob",
		"exclude",
		"exclude_glob",
		"no_ignore",
		"include_hidden",
		"type",
		"fixed_string",
		"whole_word",
		"line_regexp",
		"output_mode",
		"context",
		"before_context",
		"after_context",
		"offset",
		"stats",
	)
	assertProps("Glob",
		"exclude",
		"exclude_glob",
		"no_ignore",
		"include_hidden",
		"type",
	)
}

func TestCoreSearchToolsUseContextHandlers(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		name string
		args map[string]interface{}
	}{
		{name: "ripgrep", args: map[string]interface{}{"pattern": "needle", "path": t.TempDir()}},
		{name: "Glob", args: map[string]interface{}{"pattern": "**/*.go", "path": t.TempDir()}},
	} {
		got := reg.ExecuteCtx(ctx, tc.name, tc.args)
		if !strings.Contains(strings.ToLower(got), "cancelled") {
			t.Fatalf("%s cancelled result = %q, want cancelled", tc.name, got)
		}
	}
}

func TestCoreWriteFileToolGuidesChunkedLargeContent(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	reg.mu.RLock()
	entry := reg.tools["write_file"]
	reg.mu.RUnlock()
	if entry == nil {
		t.Fatal("write_file is not registered")
	}
	if !containsAllSubstrings(entry.Description, []string{"No content length limit", "split", "append"}) {
		t.Fatalf("write_file description should state no length limit and guide large writes: %q", entry.Description)
	}
	contentProp, _ := entry.Properties["content"].(map[string]interface{})
	if !containsAllSubstrings(asString(contentProp["description"]), []string{"No length limit"}) {
		t.Fatalf("write_file content description should state no length limit: %#v", entry.Properties["content"])
	}
	// write_file should NOT have maxLength — removed to prevent LLM from avoiding the tool.
	if got := contentProp["maxLength"]; got != nil {
		t.Fatalf("write_file content should not have maxLength, got %#v", got)
	}
}

func TestCoreEditFileToolGuidesSmallExactReplacements(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	reg.mu.RLock()
	entry := reg.tools["edit_file"]
	reg.mu.RUnlock()
	if entry == nil {
		t.Fatal("edit_file is not registered")
	}
	if !containsAllSubstrings(entry.Description, []string{"1800", "split large edits", "truncated tool-call JSON"}) {
		t.Fatalf("edit_file description should guide small edits: %q", entry.Description)
	}
	for _, propName := range []string{"old_string", "new_string"} {
		prop, _ := entry.Properties[propName].(map[string]interface{})
		if !containsAllSubstrings(asString(prop["description"]), []string{"under 1800", "split large edits"}) {
			t.Fatalf("edit_file %s description should guide small edits: %#v", propName, entry.Properties[propName])
		}
		if got := prop["maxLength"]; got != coreInlineToolPayloadMaxLength {
			t.Fatalf("edit_file %s maxLength = %#v, want %d", propName, got, coreInlineToolPayloadMaxLength)
		}
	}
}

func TestRegisterCoreToolsSecurityGuardBlocksBeforeHandler(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{
		SecurityGuard: func(name string, args map[string]interface{}) (bool, string) {
			if name == "web_fetch" {
				return false, "network disabled"
			}
			return true, ""
		},
		WebFetchHandler: func(args map[string]interface{}) string {
			return "handler ran"
		},
	})

	got := reg.Execute("web_fetch", map[string]interface{}{"url": "https://example.com"})
	if got != "[system rejected] network disabled" {
		t.Fatalf("guarded web_fetch = %q", got)
	}
}

func TestRegisterCoreToolsSecurityGuardBlocksBeforeCtxHandler(t *testing.T) {
	reg := NewCoreToolRegistry()
	ran := false
	RegisterCoreTools(reg, CoreToolDeps{
		SecurityGuard: func(name string, args map[string]interface{}) (bool, string) {
			if name == "web_search" {
				return false, "network disabled"
			}
			return true, ""
		},
		WebSearchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			ran = true
			return "handler ran"
		},
	})

	got := reg.ExecuteCtx(context.Background(), "web_search", map[string]interface{}{"query": "golang"})
	if got != "[system rejected] network disabled" {
		t.Fatalf("guarded web_search ctx = %q", got)
	}
	if ran {
		t.Fatal("guarded web_search ctx handler ran despite security rejection")
	}
}

func TestRegisterCoreToolsWebHandlersReceiveContext(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{
		WebFetchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			if err := ctx.Err(); err != nil {
				return "ctx cancelled"
			}
			return "ctx active"
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := reg.ExecuteCtx(ctx, "web_fetch", map[string]interface{}{"url": "https://example.com"})
	if got != "ctx cancelled" {
		t.Fatalf("web_fetch ctx handler = %q, want ctx cancelled", got)
	}
}
func TestRegisterCoreToolsSecurityGuardWrapsExtraHandlers(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{
		SecurityGuard: func(name string, args map[string]interface{}) (bool, string) {
			if name == "manage_skill" {
				return false, "network disabled"
			}
			return true, ""
		},
		ExtraHandlers: map[string]ToolHandler{
			"manage_skill": func(args map[string]interface{}) string { return "handler ran" },
		},
	})

	got := reg.Execute("manage_skill", map[string]interface{}{"action": "search", "query": "deploy"})
	if got != "[system rejected] network disabled" {
		t.Fatalf("guarded manage_skill = %q", got)
	}
}

func TestCoreKnowledgeImportToolsAreRegistered(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	for _, name := range []string{"knowledge_import_directory", "knowledge_import_files"} {
		if !reg.Has(name) {
			t.Fatalf("expected %s to be registered", name)
		}
	}

	reg.mu.RLock()
	dirEntry := reg.tools["knowledge_import_directory"]
	filesEntry := reg.tools["knowledge_import_files"]
	urlEntry := reg.tools["knowledge_save_url"]
	reg.mu.RUnlock()
	for name, entry := range map[string]*ToolEntry{"directory": dirEntry, "files": filesEntry} {
		for _, format := range []string{"DOC/DOCX", "PPT/PPTX", "XLS/XLSX"} {
			if !strings.Contains(entry.Description, format) {
				t.Fatalf("knowledge_import_%s description missing %s: %q", name, format, entry.Description)
			}
		}
	}
	if _, ok := dirEntry.Properties["root_path"]; !ok {
		t.Fatalf("knowledge_import_directory missing root_path property")
	}
	for _, prop := range []string{"path", "dir", "directory", "folder", "root"} {
		if _, ok := dirEntry.Properties[prop]; !ok {
			t.Fatalf("knowledge_import_directory missing %s property", prop)
		}
	}
	for _, required := range dirEntry.Required {
		if required == "root_path" {
			t.Fatalf("knowledge_import_directory should not require root_path when aliases are accepted")
		}
	}
	if _, ok := filesEntry.Properties["file_paths"]; !ok {
		t.Fatalf("knowledge_import_files missing file_paths property")
	}
	for _, prop := range []string{"paths", "files", "file_path", "path", "start_async"} {
		if _, ok := filesEntry.Properties[prop]; !ok {
			t.Fatalf("knowledge_import_files missing %s property", prop)
		}
	}
	for _, required := range filesEntry.Required {
		if required == "file_paths" {
			t.Fatalf("knowledge_import_files should not require file_paths when aliases are accepted")
		}
	}
	for _, prop := range []string{"url", "link", "href", "uri", "target"} {
		if _, ok := urlEntry.Properties[prop]; !ok {
			t.Fatalf("knowledge_save_url missing %s property", prop)
		}
	}
	for _, required := range urlEntry.Required {
		if required == "url" {
			t.Fatalf("knowledge_save_url should not require url when aliases are accepted")
		}
	}
}

func TestCoreManageSkillToolExposesMaintenancePlanSchema(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	reg.mu.RLock()
	entry := reg.tools["manage_skill"]
	reg.mu.RUnlock()
	if entry == nil {
		t.Fatal("manage_skill is not registered")
	}
	if !containsAllSubstrings(entry.Description, []string{"maintenance_plan", "execute_maintenance_plan", "read-only"}) {
		t.Fatalf("manage_skill description should mention maintenance actions: %q", entry.Description)
	}
	for _, prop := range []string{"max_actions", "stale_after_days", "min_failure_runs", "duplicate_similarity", "dry_run", "confirm", "approved_actions", "allow_duplicate_retire"} {
		if _, ok := entry.Properties[prop]; !ok {
			t.Fatalf("manage_skill missing maintenance property %q", prop)
		}
	}
}

func containsAllSubstrings(value string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}

func asString(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func TestCoreWorkflowDocumentToolsExposeMetadata(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	assertProps := func(toolName string, props ...string) {
		t.Helper()
		reg.mu.RLock()
		entry := reg.tools[toolName]
		reg.mu.RUnlock()
		if entry == nil {
			t.Fatalf("tool %q is not registered", toolName)
		}
		for _, prop := range props {
			if _, ok := entry.Properties[prop]; !ok {
				t.Fatalf("tool %q does not expose property %q", toolName, prop)
			}
		}
	}

	assertProps("write_file", "phase_id", "doc_type")
	assertProps("send_file", "phase_id", "doc_type")
	assertProps("send_to_im", "phase_id", "doc_type")
}

func TestCoreIMFileToolsExplainExactTargetVoiceRouting(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	for _, name := range []string{"send_file", "send_to_im"} {
		reg.mu.RLock()
		entry := reg.tools[name]
		reg.mu.RUnlock()
		if entry == nil {
			t.Fatalf("tool %q is not registered", name)
		}
		for _, want := range []string{"im_message", "list_targets", "channel", "group_id", "user_id", "broadcast"} {
			if !strings.Contains(entry.Description, want) {
				t.Fatalf("tool %q description missing %q: %s", name, want, entry.Description)
			}
		}
		for _, prop := range []string{"channel", "group_id", "group_name", "user_id", "message", "caption"} {
			if _, ok := entry.Properties[prop]; !ok {
				t.Fatalf("tool %q missing exact-target property %q", name, prop)
			}
		}
	}
}
