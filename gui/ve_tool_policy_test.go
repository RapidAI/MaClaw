package main

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"pgregory.net/rapid"
)

func TestIsVEToolBlocked_BlockedTools(t *testing.T) {
	blocked := []string{
		"write_file", "edit_file", "edit_lines", "bash", "ssh", "browser",
		"create_session", "send_and_observe", "send_input",
		"control_session", "interrupt_session", "kill_session",
		"parallel_execute", "craft_tool",
		"passthrough_task", "switch_llm_provider",
		"knowledge_save_url", "knowledge_save_urls", "knowledge_save_text",
		"knowledge_import_directory", "knowledge_import_files",
	}
	for _, name := range blocked {
		if !isVEToolBlocked(name) {
			t.Errorf("expected %q to be blocked in VE mode", name)
		}
	}
}

func TestIsVEToolBlocked_AllowedTools(t *testing.T) {
	allowed := []string{
		"read_file", "list_directory", "web_search", "web_fetch",
		"memory", "call_mcp_tool", "list_mcp_tools",
		"manage_skill", "run_skill", "install_skill_hub", // allowed with guards/policy
	}
	for _, name := range allowed {
		if isVEToolBlocked(name) {
			t.Errorf("expected %q to NOT be blocked in VE mode", name)
		}
	}
}

func TestIsVEToolActionBlocked_ManageSkillReadOnly(t *testing.T) {
	// Read-only actions should be allowed
	readOnly := []string{"list", "search", "status", "maintenance_plan"}
	for _, action := range readOnly {
		if isVEToolActionBlocked("manage_skill", action) {
			t.Errorf("manage_skill action %q should be allowed in VE mode", action)
		}
	}
}

func TestIsVEToolActionBlocked_ManageSkillWriteBlocked(t *testing.T) {
	// Higher-risk lifecycle actions should be blocked
	writeActions := []string{"uninstall", "upload", "validate", "patch", "history", "execute_maintenance_plan"}
	for _, action := range writeActions {
		if !isVEToolActionBlocked("manage_skill", action) {
			t.Errorf("manage_skill action %q should be blocked in VE mode", action)
		}
	}
}

func TestIsVEToolActionBlocked_NormalizesToolAndAction(t *testing.T) {
	if !isVEToolActionBlocked(" manage_skill ", " Execute_Maintenance_Plan ") {
		t.Error("manage_skill execute_maintenance_plan should be blocked after normalization")
	}
}

func TestIsVEToolActionBlocked_ManageSkillInstallRunAllowed(t *testing.T) {
	for _, action := range []string{"install", "run"} {
		if isVEToolActionBlocked("manage_skill", action) {
			t.Errorf("manage_skill action %q should be allowed in VE mode", action)
		}
	}
}

func TestIsVEToolActionBlocked_MemoryRecallAllowed(t *testing.T) {
	if isVEToolActionBlocked("memory", "recall") {
		t.Error("memory recall should be allowed in VE mode")
	}
	if isVEToolActionBlocked("memory", "candidates") {
		t.Error("memory candidates should be allowed in VE mode")
	}
}

func TestIsVEToolActionBlocked_MemorySaveBlocked(t *testing.T) {
	if !isVEToolActionBlocked("memory", "save") {
		t.Error("memory save should be blocked in VE mode")
	}
}

func TestVeSkillMCPOnlyGuard_AllMCPSteps_Allowed(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	// Inject a test skill with all call_mcp_tool steps
	cfg, _ := app.LoadConfig()
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "mcp-search-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{
			{Action: "call_mcp_tool", Params: map[string]interface{}{"server_id": "brave-search", "tool_name": "search"}},
			{Action: "call_mcp_tool", Params: map[string]interface{}{"server_id": "brave-search", "tool_name": "summarize"}},
		},
	}}
	app.SaveConfig(cfg)

	allowed, reason := veSkillMCPOnlyGuard("mcp-search-skill", app)
	if !allowed {
		t.Fatalf("expected MCP-only skill to be allowed, got reason: %s", reason)
	}
}

func TestVeSkillMCPOnlyGuard_MixedSteps_Blocked(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	cfg, _ := app.LoadConfig()
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "mixed-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{
			{Action: "call_mcp_tool", Params: map[string]interface{}{"server_id": "search", "tool_name": "query"}},
			{Action: "bash", Params: map[string]interface{}{"command": "echo dangerous"}},
		},
	}}
	app.SaveConfig(cfg)

	allowed, reason := veSkillMCPOnlyGuard("mixed-skill", app)
	if allowed {
		t.Fatal("expected mixed skill (MCP + bash) to be blocked")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestVeSkillMCPOnlyGuard_BashOnly_Blocked(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	cfg, _ := app.LoadConfig()
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "bash-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hello"}},
		},
	}}
	app.SaveConfig(cfg)

	allowed, _ := veSkillMCPOnlyGuard("bash-skill", app)
	if allowed {
		t.Fatal("expected bash-only skill to be blocked in VE mode")
	}
}

func TestVeSkillMCPOnlyGuard_NotFound(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	cfg, _ := app.LoadConfig()
	cfg.NLSkills = nil
	app.SaveConfig(cfg)

	allowed, reason := veSkillMCPOnlyGuard("nonexistent", app)
	if allowed {
		t.Fatal("expected nonexistent skill to be blocked")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for nonexistent skill")
	}
}

func TestVeSkillMCPOnlyGuard_DisabledSkill_Blocked(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	cfg, _ := app.LoadConfig()
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "disabled-mcp-skill",
		Status: "disabled",
		Steps: []corelib.NLSkillStep{
			{Action: "call_mcp_tool", Params: map[string]interface{}{"server_id": "s", "tool_name": "t"}},
		},
	}}
	app.SaveConfig(cfg)

	allowed, _ := veSkillMCPOnlyGuard("disabled-mcp-skill", app)
	if allowed {
		t.Fatal("expected disabled skill to be blocked")
	}
}

func TestVeSkillMCPOnlyGuard_NoSteps_Blocked(t *testing.T) {
	app := &App{}
	app.skillExecutor = &SkillExecutor{app: app}

	cfg, _ := app.LoadConfig()
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "empty-skill",
		Status: "active",
		Steps:  nil,
	}}
	app.SaveConfig(cfg)

	allowed, _ := veSkillMCPOnlyGuard("empty-skill", app)
	if allowed {
		t.Fatal("expected skill with no steps to be blocked")
	}
}

func TestFilterToolsForVE_KeepsRunSkillAndManageSkill(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "run_skill"}},
		{"function": map[string]interface{}{"name": "manage_skill"}},
		{"function": map[string]interface{}{"name": "call_mcp_tool"}},
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "write_file"}},
	}

	filtered := filterToolsForVE(tools)

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	// run_skill, manage_skill, call_mcp_tool should be kept
	if !names["run_skill"] {
		t.Error("run_skill should be in VE tool list")
	}
	if !names["manage_skill"] {
		t.Error("manage_skill should be in VE tool list")
	}
	if !names["call_mcp_tool"] {
		t.Error("call_mcp_tool should be in VE tool list")
	}

	// bash, write_file should be removed
	if names["bash"] {
		t.Error("bash should NOT be in VE tool list")
	}
	if names["write_file"] {
		t.Error("write_file should NOT be in VE tool list")
	}
}

func TestFilterToolsForVE_BlocksAllCodingSessionTools(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "run_skill"}},
	}
	for name := range coretool.CodingSessionToolNames {
		tools = append(tools, map[string]interface{}{"function": map[string]interface{}{"name": name}})
	}

	filtered := filterToolsForVE(tools)
	for _, tool := range filtered {
		name := extractToolName(tool)
		if coretool.IsCodingSessionTool(name) {
			t.Fatalf("VE tool list should not expose external coding-session tool %s", name)
		}
	}
	if !hasToolNamed(filtered, "run_skill") {
		t.Fatal("VE tool list should keep safe non-coding tools")
	}
}

// --- filterToolsForVEWithConfig tests ---

func TestFilterToolsForVEWithConfig_EmptyDirs_SendFileBlocked(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "send_file"}},
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "list_directory"}},
		{"function": map[string]interface{}{"name": "web_search"}},
		{"function": map[string]interface{}{"name": "bash"}},
	}

	// Empty allowedDirs: send_file stays blocked (zero-config = zero-risk)
	filtered := filterToolsForVEWithConfig(tools, nil)

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	if names["send_file"] {
		t.Error("send_file should be blocked when allowedDirs is empty")
	}
	if !names["read_file"] {
		t.Error("read_file should be allowed (not in veBlockedTools)")
	}
	if !names["list_directory"] {
		t.Error("list_directory should be allowed (not in veBlockedTools)")
	}
	if !names["web_search"] {
		t.Error("web_search should be allowed")
	}
	if names["bash"] {
		t.Error("bash should be blocked")
	}
}

func TestFilterToolsForVEWithConfig_EmptySlice_SendFileBlocked(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "send_file"}},
		{"function": map[string]interface{}{"name": "read_file"}},
	}

	// Empty slice (not nil): send_file stays blocked
	filtered := filterToolsForVEWithConfig(tools, []string{})

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	if names["send_file"] {
		t.Error("send_file should be blocked when allowedDirs is empty slice")
	}
	if !names["read_file"] {
		t.Error("read_file should be allowed")
	}
}

func TestFilterToolsForVEWithConfig_WithDirs_SendFileUnblocked(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "send_file"}},
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "list_directory"}},
		{"function": map[string]interface{}{"name": "web_search"}},
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "write_file"}},
	}

	// Non-empty allowedDirs: send_file is unblocked
	filtered := filterToolsForVEWithConfig(tools, []string{"D:\\Documents\\Templates"})

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	if !names["send_file"] {
		t.Error("send_file should be unblocked when allowedDirs is non-empty")
	}
	if !names["read_file"] {
		t.Error("read_file should be allowed")
	}
	if !names["list_directory"] {
		t.Error("list_directory should be allowed")
	}
	if !names["web_search"] {
		t.Error("web_search should be allowed")
	}
	// bash and write_file should still be blocked (not in veConfigUnblockedTools)
	if names["bash"] {
		t.Error("bash should still be blocked even with allowedDirs")
	}
	if names["write_file"] {
		t.Error("write_file should still be blocked even with allowedDirs")
	}
}

func TestFilterToolsForVEWithConfig_MultipleDirs_SendFileUnblocked(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "send_file"}},
		{"function": map[string]interface{}{"name": "generate_pdf"}},
	}

	// Multiple directories configured
	dirs := []string{
		"D:\\Documents\\Templates",
		"D:\\SharedFiles\\Public",
		"C:\\Users\\Owner\\Downloads\\ForVE",
	}
	filtered := filterToolsForVEWithConfig(tools, dirs)

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	if !names["send_file"] {
		t.Error("send_file should be unblocked with multiple allowedDirs")
	}
	// generate_pdf is blocked and NOT in veConfigUnblockedTools
	if names["generate_pdf"] {
		t.Error("generate_pdf should still be blocked (not conditionally unblocked)")
	}
}

func TestFilterToolsForVEWithConfig_OtherBlockedToolsStayBlocked(t *testing.T) {
	// Verify that configuring allowedDirs only unblocks the specific tools
	// in veConfigUnblockedTools, not all blocked tools.
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "send_file"}},
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "ssh"}},
		{"function": map[string]interface{}{"name": "write_file"}},
		{"function": map[string]interface{}{"name": "edit_file"}},
		{"function": map[string]interface{}{"name": "create_session"}},
		{"function": map[string]interface{}{"name": "browser"}},
		{"function": map[string]interface{}{"name": "open"}},
	}

	filtered := filterToolsForVEWithConfig(tools, []string{"D:\\Shared"})

	names := make(map[string]bool)
	for _, t := range filtered {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}

	// Only send_file should be unblocked
	if !names["send_file"] {
		t.Error("send_file should be unblocked")
	}

	// All other blocked tools should remain blocked
	stillBlocked := []string{"bash", "ssh", "write_file", "edit_file", "create_session", "browser", "open"}
	for _, name := range stillBlocked {
		if names[name] {
			t.Errorf("%q should still be blocked even with allowedDirs configured", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Property-Based Test: Conditional tool unblocking
// Feature: ve-file-sharing-directories, Property 1: Conditional tool unblocking
//
// **Validates: Requirements 3.1, 3.2, 6.1, 6.2**
//
// For any non-empty list of allowed directories, filterToolsForVEWithConfig
// SHALL include send_file, list_directory, and read_file in the output tool list.
// For any empty list, filterToolsForVEWithConfig SHALL exclude send_file from
// the output tool list.
// ---------------------------------------------------------------------------

// genDirectoryPath generates a random absolute directory path string.
func genDirectoryPath(t *rapid.T, label string) string {
	// Generate random drive letter (Windows) or root path
	driveLetters := []string{"C", "D", "E", "F", "G"}
	drive := driveLetters[rapid.IntRange(0, len(driveLetters)-1).Draw(t, label+"_drive")]

	// Generate 1-4 path segments
	numSegments := rapid.IntRange(1, 4).Draw(t, label+"_segments")
	path := drive + ":\\"
	for i := 0; i < numSegments; i++ {
		seg := rapid.StringMatching(`[A-Za-z0-9_-]{1,20}`).Draw(t, fmt.Sprintf("%s_seg%d", label, i))
		path += seg + "\\"
	}
	return path[:len(path)-1] // trim trailing backslash
}

// genNonEmptyDirList generates a non-empty list of directory paths (1-10 entries).
func genNonEmptyDirList(t *rapid.T, label string) []string {
	n := rapid.IntRange(1, 10).Draw(t, label+"_count")
	dirs := make([]string, n)
	for i := 0; i < n; i++ {
		dirs[i] = genDirectoryPath(t, fmt.Sprintf("%s_%d", label, i))
	}
	return dirs
}

// buildFullToolList creates a tool list containing all conditionally unblocked
// tools plus some always-blocked and always-allowed tools for realistic testing.
func buildFullToolList() []map[string]interface{} {
	toolNames := []string{
		// Conditionally unblocked tools (in veConfigUnblockedTools)
		"send_file",
		"list_directory",
		"read_file",
		// Always blocked tools (in veBlockedTools, NOT in veConfigUnblockedTools)
		"bash",
		"write_file",
		"ssh",
		"browser",
		"create_session",
		// Always allowed tools (not in veBlockedTools)
		"web_search",
		"web_fetch",
		"memory",
		"call_mcp_tool",
	}

	tools := make([]map[string]interface{}, len(toolNames))
	for i, name := range toolNames {
		tools[i] = map[string]interface{}{
			"function": map[string]interface{}{"name": name},
		}
	}
	return tools
}

// extractToolNames extracts tool names from a filtered tool list.
func extractToolNames(tools []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// TestProperty1_ConditionalToolUnblocking_NonEmptyDirs verifies that when
// allowedDirs is non-empty, send_file, list_directory, and read_file are
// present in the output tool list.
func TestProperty1_ConditionalToolUnblocking_NonEmptyDirs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-empty directory list
		dirs := genNonEmptyDirList(t, "dirs")

		// Build a realistic tool list
		tools := buildFullToolList()

		// Apply the filter with non-empty dirs
		filtered := filterToolsForVEWithConfig(tools, dirs)
		names := extractToolNames(filtered)

		// Property: send_file MUST be present when dirs is non-empty
		if !names["send_file"] {
			t.Fatalf("send_file should be present when allowedDirs is non-empty (dirs=%v)", dirs)
		}

		// Property: list_directory MUST be present when dirs is non-empty
		if !names["list_directory"] {
			t.Fatalf("list_directory should be present when allowedDirs is non-empty (dirs=%v)", dirs)
		}

		// Property: read_file MUST be present when dirs is non-empty
		if !names["read_file"] {
			t.Fatalf("read_file should be present when allowedDirs is non-empty (dirs=%v)", dirs)
		}

		// Property: always-blocked tools MUST remain blocked
		alwaysBlocked := []string{"bash", "write_file", "ssh", "browser", "create_session"}
		for _, blocked := range alwaysBlocked {
			if names[blocked] {
				t.Fatalf("%q should remain blocked even with non-empty allowedDirs", blocked)
			}
		}

		// Property: always-allowed tools MUST remain allowed
		alwaysAllowed := []string{"web_search", "web_fetch", "memory", "call_mcp_tool"}
		for _, allowed := range alwaysAllowed {
			if !names[allowed] {
				t.Fatalf("%q should remain allowed", allowed)
			}
		}
	})
}

// TestProperty1_ConditionalToolUnblocking_EmptyDirs verifies that when
// allowedDirs is empty (nil or empty slice), send_file is absent from
// the output tool list.
func TestProperty1_ConditionalToolUnblocking_EmptyDirs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate empty directory list (randomly choose nil or empty slice)
		var dirs []string
		useNil := rapid.Bool().Draw(t, "useNil")
		if !useNil {
			dirs = []string{}
		}

		// Build a realistic tool list
		tools := buildFullToolList()

		// Apply the filter with empty dirs
		filtered := filterToolsForVEWithConfig(tools, dirs)
		names := extractToolNames(filtered)

		// Property: send_file MUST be absent when dirs is empty
		if names["send_file"] {
			t.Fatal("send_file should be absent when allowedDirs is empty")
		}

		// Property: list_directory MUST still be present (it's not in veBlockedTools)
		if !names["list_directory"] {
			t.Fatal("list_directory should be present (not in veBlockedTools)")
		}

		// Property: read_file MUST still be present (it's not in veBlockedTools)
		if !names["read_file"] {
			t.Fatal("read_file should be present (not in veBlockedTools)")
		}

		// Property: always-blocked tools MUST remain blocked
		alwaysBlocked := []string{"bash", "write_file", "ssh", "browser", "create_session"}
		for _, blocked := range alwaysBlocked {
			if names[blocked] {
				t.Fatalf("%q should remain blocked when allowedDirs is empty", blocked)
			}
		}

		// Property: always-allowed tools MUST remain allowed
		alwaysAllowed := []string{"web_search", "web_fetch", "memory", "call_mcp_tool"}
		for _, allowed := range alwaysAllowed {
			if !names[allowed] {
				t.Fatalf("%q should remain allowed", allowed)
			}
		}
	})
}
