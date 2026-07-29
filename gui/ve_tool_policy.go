package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ---------------------------------------------------------------------------
// VE Tool Policy: deny-list based tool filtering for digital employee sessions.
//
// Design: block tools that modify the local system or execute arbitrary code.
// All other tools (read-only, information retrieval) are automatically available.
// This ensures VE sessions inherit new read-only tools without code changes.
// ---------------------------------------------------------------------------

// veBlockedTools lists VE-specific tools that are NOT available in digital
// employee mode. External coding-session tools are blocked from
// corelib/tool.CodingSessionToolNames so the VE policy cannot drift from the
// main agent tool-list policy.
//
// Adding a new tool to the main agent: if it modifies local state, add it here.
// If it's read-only (search, query, fetch), it's automatically available to VE.
var veBlockedTools = map[string]bool{
	// --- File modification ---
	"write_file": true,
	"edit_file":  true,
	"edit_lines": true,

	// --- Command execution ---
	"bash": true,

	// --- Programming sessions ---
	"send_input":         true,
	"list_sessions":      true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"parallel_execute":   true,

	// --- Remote server operations ---
	"ssh": true,

	// --- Browser / GUI automation ---
	"browser":          true,
	"browser_task_run": true,
	"gui_observe":      true,
	"gui_verify":       true,
	"gui_record_start": true,
	"gui_record_stop":  true,

	// --- System configuration ---
	"manage_config":       true,
	"manage_schedule":     true,
	"im_message":          true,
	"manage_template":     true,
	"switch_llm_provider": true, // backward-compat alias for manage_config

	// --- Task management / delegation ---
	"task":          true,
	"delegate_task": true,

	// --- File generation / local operations ---
	"generate_pdf": true,
	"send_file":    true,
	"open":         true,

	// --- Skill execution ---
	// run_skill/manage_skill read-only actions are allowed; installation is a
	// registry mutation and must not be inherited by VE sessions as a read-like
	// tool.
	"install_skill_hub":        true,
	"search_and_install_skill": true,
	"craft_tool":               true, // generates and executes arbitrary scripts

	// --- Passthrough tasks (arbitrary script execution) ---
	"passthrough_task": true,

	// --- Knowledge base write operations ---
	"knowledge_save_url":         true,
	"knowledge_save_urls":        true,
	"knowledge_save_text":        true,
	"knowledge_import_directory": true,
	"knowledge_import_files":     true,

	// --- Project management ---
	"project_manage":  true,
	"list_providers":  true,
	"switch_provider": true,

	// --- Coding tool gate / workflow ---
	"ask_user":     true,
	"record_audio": true,

	// --- Background task management ---
	"async_wait": true,
}

func init() {
	for name := range disabledExternalCodingSessionTools {
		veBlockedTools[name] = true
	}
}

// veBlockedToolActions defines per-tool action restrictions.
// For tools that support multiple actions (e.g., memory), only specific
// actions are blocked while others remain available.
var veBlockedToolActions = map[string]map[string]bool{
	"memory": {
		"save":   true,
		"delete": true,
		"update": true,
		// "recall" is allowed
	},
	"manage_skill": {
		// list/search/status are read-only. run is guarded by
		// veSkillMCPOnlyGuard. Installation and higher-risk lifecycle mutation
		// actions stay blocked here.
		"install":                  true,
		"uninstall":                true,
		"upload":                   true,
		"validate":                 true,
		"patch":                    true,
		"history":                  true,
		"execute_maintenance_plan": true,
	},
}

// veSkillMCPOnlyGuard is the execution-time guard for run_skill in VE mode.
// It returns true (allowed) only when ALL executable steps of the skill are
// call_mcp_tool. This ensures VE can invoke MCP-based skills without allowing
// arbitrary bash/craft_tool/session execution.
//
// Returns (allowed bool, reason string).
func veSkillMCPOnlyGuard(skillName string, app *App) (bool, string) {
	if app == nil {
		return false, "app not initialized"
	}

	if app.skillExecutor == nil {
		return false, "skill executor not initialized"
	}

	// Load all skills through the standard path
	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == skillName || skills[i].DirName == skillName {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		return false, "skill not found: " + skillName
	}

	if found.Status != "" && found.Status != "active" {
		return false, "skill is not active (status: " + found.Status + ")"
	}

	if len(found.Steps) == 0 {
		return false, "skill has no executable steps"
	}

	// Check every step: all must be call_mcp_tool
	for i, step := range found.Steps {
		normalized := cskill.NormalizeStepActionName(step.Action)
		if normalized != "call_mcp_tool" {
			return false, fmt.Sprintf(
				"step %d has action %q (only call_mcp_tool is allowed for digital employees)",
				i+1, step.Action,
			)
		}
	}

	return true, ""
}

// skillEntryForVE is kept for potential future use but the guard now
// operates directly on corelib.NLSkillEntry via SkillExecutor.loadSkills().

// isVEToolBlocked checks if a tool is blocked in VE mode.
func isVEToolBlocked(toolName string) bool {
	name := strings.TrimSpace(toolName)
	if strings.HasPrefix(name, "browser_") {
		return true
	}
	return coretool.IsCodingSessionTool(name) || veBlockedTools[name]
}

// isVEToolActionBlocked checks if a specific action of a tool is blocked in VE mode.
// Returns true if the tool+action combination should be denied.
func isVEToolActionBlocked(toolName, action string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	act := strings.ToLower(strings.TrimSpace(action))
	actions, ok := veBlockedToolActions[name]
	if !ok {
		return false
	}
	if actions["*"] {
		return true
	}
	return actions[act]
}

// isVERunSkillAllowed checks if a run_skill invocation is allowed in VE mode.
// It uses the MCP-only guard: only skills whose steps are all call_mcp_tool
// can be executed by digital employees.
func isVERunSkillAllowed(skillName string, app *App) (bool, string) {
	return veSkillMCPOnlyGuard(skillName, app)
}

// filterToolsForVE filters a list of tool definitions, removing those blocked by VE policy.
// Input: tool definitions in OpenAI function-calling format ([]map[string]interface{}).
// Output: filtered list with blocked tools removed.
func filterToolsForVE(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		if isVEToolBlocked(name) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// veConfigUnblockedTools lists tools that are conditionally unblocked when
// VEAllowedDirectories is non-empty. These tools remain blocked when the
// directory list is empty (zero-config = zero-risk).
//
// - send_file: allows VE to send files from allowed directories to users
// - list_directory: allows VE to browse allowed directories for file discovery
// - read_file: allows VE to inspect file contents within allowed directories
//
// Note: list_directory and read_file are already not in veBlockedTools (they are
// read-only tools), so they are naturally included. They are listed here for
// documentation clarity; the execution layer (ExecuteTool) enforces directory
// scoping at runtime via ValidateVEFilePath / IsWithinAllowedDirs.
var veConfigUnblockedTools = map[string]bool{
	"send_file":      true,
	"send_to_im":     true,
	"list_directory": true,
	"read_file":      true,
}

// filterToolsForVEWithConfig extends filterToolsForVE to conditionally
// unblock send_file when allowed directories are configured.
//
// When len(allowedDirs) > 0, send_file is removed from the blocked set,
// allowing the VE to send files from the configured directories.
// list_directory and read_file are already unblocked (not in veBlockedTools),
// but their directory scoping is enforced at the execution layer.
//
// When len(allowedDirs) == 0, send_file remains blocked (zero-config = zero-risk).
func filterToolsForVEWithConfig(tools []map[string]interface{}, allowedDirs []string) []map[string]interface{} {
	hasDirs := len(allowedDirs) > 0

	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		if isVEToolBlocked(name) {
			// When directories are configured, conditionally unblock
			// tools in veConfigUnblockedTools.
			if hasDirs && veConfigUnblockedTools[name] {
				// Allow this tool through; execution layer enforces scoping.
				out = append(out, t)
				continue
			}
			continue
		}
		out = append(out, t)
	}
	return out
}
