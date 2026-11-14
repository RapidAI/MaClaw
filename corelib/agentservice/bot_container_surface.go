package agentservice

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// DesktopWeb runs web_search and web_fetch through the desktop container.
// MaClawSrv installs it. A nil hook keeps the host web stack for a digital
// employee. A bot turn does not fall through to that host stack.
var DesktopWeb func(ctx context.Context, tenantID, userID, kind, query, rawURL string, maxChars int) (string, error)

// CloudProgress rewrites the container progress file after a cloud-desktop
// tool return. MaClawSrv installs it. A nil hook leaves the run unchanged.
var CloudProgress func(ctx context.Context, tenantID, userID, taskID, toolName, result string)

const botContainerToolReason = "this bot works inside its desktop container. Use the desktop tool for files, commands, and windows. web_search and web_fetch use this desktop's network"

// botUsesContainer reports a cloud-desktop bot turn. A digital employee has
// neither hub_bot nor bot_phase and keeps the host system.
// desktopPersonAccount is the Hub user whose cloud desktop noVNC opens.
// A bot turn does not use the MaClaw service principal for that desktop.
func (c *coreAgentCallbacks) desktopPersonAccount() (string, string, bool) {
	if c == nil {
		return "", "", false
	}
	meta := c.runtimeRequest.Instance.Metadata
	if meta == nil {
		meta = c.instance.Metadata
	}
	user := strings.TrimSpace(meta["hub_user_id"])
	if !desktop.ValidUserID(user) {
		return "", "", false
	}
	tenant := strings.TrimSpace(meta["hub_tenant_id"])
	if tenant == "" || len(tenant) > 200 || strings.ContainsAny(tenant, "\r\n\x00") {
		tenant = strings.TrimSpace(c.principal.TenantID)
	}
	return tenant, user, true
}

// hubBotInstance reports a Hub bot instance. runtimeRequest is filled only
// when the turn has a durable runtime attempt; a plain chat turn leaves it
// empty, and reading only that copy made such a turn look like a digital
// employee: the container web_fetch fell through to the semantic surface and
// failed with "unknown semantic tool web_fetch". c.instance is the same
// instance on every path, so either copy marks the bot.
func (c *coreAgentCallbacks) hubBotInstance() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.runtimeRequest.Instance.Metadata["hub_bot"]) == "1" {
		return true
	}
	return strings.TrimSpace(c.instance.Metadata["hub_bot"]) == "1"
}

func (c *coreAgentCallbacks) botUsesContainer() bool {
	if c == nil {
		return false
	}
	if c.hubBotInstance() {
		return true
	}
	switch strings.TrimSpace(c.messageMetadata["bot_phase"]) {
	case "plan", "execute":
		return true
	default:
		return false
	}
}

func (c *coreAgentCallbacks) botMissesContainer(name string) bool {
	if !c.botUsesContainer() {
		return false
	}
	name = strings.TrimSpace(name)
	// ssh is the same remote-host tool a digital employee already runs on
	// this service. It is not the container shell and not the desktop.
	// The generic handler is what says this account cannot open a machine.
	if name == "web_search" || name == "web_fetch" || name == "ssh" {
		return false
	}
	if botRejectsToolName(name) {
		return true
	}
	surface := c.dynamicSemanticSurface
	if surface == nil {
		return false
	}
	grant, ok := surface.grants[name]
	if !ok {
		return false
	}
	adapter := strings.TrimSpace(grant.AdapterName)
	if adapter == "web_search" || adapter == "web_fetch" {
		return true
	}
	return botRejectsToolName(adapter)
}

func botRejectsToolName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "desktop" || name == "web_search" || name == "web_fetch" {
		return false
	}
	if desktopLeavesLoggedInBrowser(name) || botHostSystemTool(name) {
		return true
	}
	rendered := coretool.SemanticModelFunctionName(name)
	if rendered == "" || rendered == name {
		return false
	}
	if rendered == "web_search" || rendered == "web_fetch" {
		return true
	}
	return desktopLeavesLoggedInBrowser(rendered) || botHostSystemTool(rendered)
}

func botHostSystemTool(name string) bool {
	switch name {
	case "read_file", "write_file", "edit_file", "edit_lines", "list_directory",
		"read_document", "FileRead", "ripgrep", "Glob", "search_files", "delete_file",
		"bash", "craft_tool", "screenshot", "generate_pdf", "archive", "office",
		"build_verify", "read_excel", "write_excel", "read_pptx", "write_pptx":
		return true
	}
	if strings.HasPrefix(name, "git_") || strings.HasPrefix(name, "computer_") || strings.HasPrefix(name, "gui_") {
		return true
	}
	if strings.HasPrefix(name, "host_fs_") || strings.HasPrefix(name, "host_document_") ||
		strings.HasPrefix(name, "host_system_launch_") || strings.HasPrefix(name, "host_repo_") {
		return true
	}
	switch name {
	case "host_shell_execute_local", "host_shell_execute_remote_host",
		"host_computer_control_desktop", "host_browser_control_web",
		"host_visual_capture_desktop", "host_artifact_acquire_remote",
		"host_build_verify_local", "host_information_search_web",
		"host_information_fetch_web":
		return true
	}
	return false
}

func (c *coreAgentCallbacks) finishToolSurface(tools []map[string]interface{}) []map[string]interface{} {
	tools = c.withBotScheduleTool(tools)
	if !c.botUsesContainer() {
		return tools
	}
	kept := make([]map[string]interface{}, 0, len(tools)+2)
	seenWeb := map[string]bool{}
	for _, tool := range tools {
		name := tooldef.Name(tool)
		if name == "ssh" && !c.canUseSSH() {
			continue
		}
		if c.botMissesContainer(name) {
			continue
		}
		if name == "delegate_task" && c.hubBotInstance() {
			// A Hub bot hands work to a teammate with desktop
			// action=delegate. The in-process sub-agent is not a teammate:
			// asked to "let collab-b write it", the bot ran delegate_task
			// and reported the sub-agent's code as collab-b's, with no
			// note in Hub (papers 2026-10-30 run_928f4029).
			continue
		}
		if name == "web_search" || name == "web_fetch" {
			if seenWeb[name] {
				continue
			}
			seenWeb[name] = true
			kept = append(kept, containerWebTool(name))
			continue
		}
		kept = append(kept, tool)
	}
	for _, name := range []string{"web_search", "web_fetch"} {
		if !seenWeb[name] {
			kept = append(kept, containerWebTool(name))
		}
	}
	if def := c.botSSHToolDefinition(); def != nil {
		replaced := false
		for i, tool := range kept {
			if tooldef.Name(tool) == "ssh" {
				kept[i] = def
				replaced = true
				break
			}
		}
		if !replaced {
			kept = append(kept, def)
		}
	}
	return kept
}

// withBotScheduleTool keeps bot_schedule on a desktop-bot turn, including a
// semantic plan and a light turn. The plan chooses the work tools. This tool
// is how that bot registers its own later runs.
func (c *coreAgentCallbacks) withBotScheduleTool(tools []map[string]interface{}) []map[string]interface{} {
	if c == nil || c.botScheduleHandler == nil || strings.TrimSpace(c.botID) == "" {
		return tools
	}
	for _, tool := range tools {
		if tooldef.Name(tool) == "bot_schedule" {
			return tools
		}
	}
	out := make([]map[string]interface{}, len(tools)+1)
	copy(out, tools)
	out[len(tools)] = botScheduleToolDefinition()
	return out
}

func botScheduleToolDefinition() map[string]interface{} {
	return functionToolDefinition("bot_schedule",
		"Register, update, list, or cancel this bot's own timer. Call it when the work in front of you should happen again later, including when the person did not name a clock. name is the timer. The same name updates that timer. A different repeating job uses another name. instruction is the whole order for one later run. interval_minutes repeats. hour and minute together are a clock time; set both or neither. Omit a clock field to keep the armed value. A new timer needs interval_minutes, or both hour and minute. action is create, list, or cancel.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":           map[string]interface{}{"type": "string", "description": "create, list, or cancel. Default create."},
				"name":             map[string]interface{}{"type": "string", "description": "Stable timer name. Required for create and cancel."},
				"instruction":      map[string]interface{}{"type": "string", "description": "The whole order for one later run. Empty on update keeps the stored order."},
				"interval_minutes": map[string]interface{}{"type": "integer", "description": "Repeat every N minutes. Omit to keep the armed interval."},
				"hour":             map[string]interface{}{"type": "integer", "description": "Clock hour 0-23. Set together with minute, or omit both."},
				"minute":           map[string]interface{}{"type": "integer", "description": "Clock minute 0-59. Set together with hour, or omit both."},
				"day_of_week":      map[string]interface{}{"type": "integer", "description": "0 Sunday through 6 Saturday. Omit to keep the armed weekday."},
			},
		},
	)
}

// botSSHPlanAction is the read-only slice of the ssh tool. A plan turn can
// see which sessions already exist. Opening one or running a command waits
// until the user confirms.
func botSSHPlanAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "list", "list_tasks", "check_task":
		return true
	default:
		return false
	}
}

func (c *coreAgentCallbacks) botSSHToolDefinition() map[string]interface{} {
	if c == nil || !c.botUsesContainer() || !c.canUseSSH() {
		return nil
	}
	spec := specFromCoreTool("ssh", sshToolDescription(c.allowDirectSSH, c.allowSSHFileTransfer, len(c.configuredSSHHosts()) > 0), true, "")
	specs := []coreToolSpec{spec}
	applySSHActionEnum(c, specs)
	return functionToolDefinition(specs[0].Name, specs[0].Description, specs[0].Parameters)
}

func containerWebTool(name string) map[string]interface{} {
	switch name {
	case "web_search":
		return functionToolDefinition("web_search",
			"Search the public web through this desktop's network. Returns titles and URLs. A site the user may have signed into stays in the desktop browser.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":       map[string]interface{}{"type": "string", "description": "Search query"},
					"max_results": map[string]interface{}{"type": "integer", "description": "Max results, default 8, max 20"},
				},
				"required": []string{"query"},
			})
	default:
		return functionToolDefinition("web_fetch",
			"Fetch a public http or https URL through this desktop's network and return its text. A site the user may have signed into stays in the desktop browser.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":       map[string]interface{}{"type": "string", "description": "http or https URL"},
					"max_chars": map[string]interface{}{"type": "integer", "description": "Max characters to return"},
				},
				"required": []string{"url"},
			})
	}
}
