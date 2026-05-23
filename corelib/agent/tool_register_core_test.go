package agent

import (
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
		"create_session":     true, // requires RemoteSessionManager (Wails)
		"send_and_observe":   true, // requires RemoteSessionManager (Wails)
		"get_session_output": true, // requires RemoteSessionManager (Wails)
		"get_session_events": true, // requires RemoteSessionManager (Wails)
		"control_session":    true, // requires RemoteSessionManager (Wails)
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
}
