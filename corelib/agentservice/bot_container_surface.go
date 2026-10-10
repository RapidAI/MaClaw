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

func (c *coreAgentCallbacks) botUsesContainer() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.runtimeRequest.Instance.Metadata["hub_bot"]) == "1" {
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
