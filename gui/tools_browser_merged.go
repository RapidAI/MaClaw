package main

import (
	"fmt"
	"strings"
)

// MergedBrowserToolName is the single tool name that replaces the many browser_*
// tool definitions in the LLM context.
const MergedBrowserToolName = "browser"

const mergedBrowserToolDescription = `Browser automation tool (merged entrypoint). Select behavior with action.

Session:
- session_start: start or reuse a stable browser session; returns session_id
- session_stop: stop a browser session handle without killing the browser process
- connect: alias for session_start; defaults to persistent login/cookies
- close: disconnect CDP without killing the browser
- info: current page title, URL, load state
- list_pages: list tabs
- switch_page: switch tab

Page actions:
- navigate: open URL
- observe: snapshot page and refs
- click: click by ref, selector, or visible text
- type: type text by ref/selector, or into current focused editable element; set content_format="markdown" for rich editors/article publishing
- select, scroll, back, refresh, set_files
- extract, wait

Task helpers:
- task_run, task_status, task_verify, list_flows; task_run type steps support focused editable input after click, plus params.content_format="markdown" or top-level content_format="markdown"

All page actions require session_id. First call browser(action="session_start") or browser(action="connect"). Use persistent for normal work; isolated is clean debug only. Browser process lifetime is managed by the app, not by tool calls. Arbitrary JavaScript is not part of the stable browser path; use observe -> click -> type -> click -> observe/verify.`

var mergedBrowserInputSchema = map[string]interface{}{
	"action":         map[string]string{"type": "string", "description": "Action name"},
	"session_id":     map[string]string{"type": "string", "description": "Browser session id"},
	"url":            map[string]string{"type": "string", "description": "URL for navigate"},
	"ref":            map[string]string{"type": "string", "description": "Element ref from observe"},
	"selector":       map[string]string{"type": "string", "description": "CSS selector for click/type/extract/wait"},
	"text":           map[string]string{"type": "string", "description": "Text for type, or visible text for click"},
	"content_format": map[string]string{"type": "string", "description": "For type: plain (default) or markdown. Use markdown for rich editors so headings/lists/bold render instead of raw Markdown."},
	"value":          map[string]string{"type": "string", "description": "Value for select"},
	"delta_x":        map[string]string{"type": "number", "description": "Horizontal scroll delta"},
	"delta_y":        map[string]string{"type": "number", "description": "Scroll delta"},
	"target_id":      map[string]string{"type": "string", "description": "Tab target id"},
	"steps":          map[string]string{"type": "array", "description": "Steps for task_run. Type steps may omit ref/selector to type into the currently focused editable element after a click. Type steps may set params.content_format=markdown for rich editors; top-level content_format=markdown applies to type steps that omit it."},
	"task_id":        map[string]string{"type": "string", "description": "Task id for task_status"},
	"flow_name":      map[string]string{"type": "string", "description": "Flow name for list_flows"},
	"criteria":       map[string]string{"type": "array", "description": "Criteria for task_verify"},

	"start_url":        map[string]string{"type": "string", "description": "Initial URL for session_start"},
	"reuse_existing":   map[string]string{"type": "boolean", "description": "Reuse existing session; default true"},
	"mode":             map[string]string{"type": "string", "description": "persistent (default, preserves login/cookies) / isolated / connect_user / auto (maps to persistent)"},
	"allowed_domains":  map[string]string{"type": "array", "description": "Allowed domains"},
	"blocked_domains":  map[string]string{"type": "array", "description": "Blocked domains"},
	"file_paths":       map[string]string{"type": "array", "description": "Files for set_files"},
	"duration_ms":      map[string]string{"type": "number", "description": "Wait duration in ms"},
	"success_criteria": map[string]string{"type": "string", "description": "Success criteria for task_run/task_verify"},
}

func dispatchMergedBrowser(registry *ToolRegistry, args map[string]interface{}) string {
	if ownerID, explicitRuntimeOwner := runtimePolicyOwnerIDFromToolArgsWithPresence(args); explicitRuntimeOwner && ownerID == "" {
		return "browser failed: runtime owner is missing; isolated runtime will not fall back to desktop owner"
	}
	actionText, _ := args["action"].(string)
	actionText = strings.TrimSpace(actionText)
	if actionText == "" {
		return "missing browser action; call browser(action=\"session_start\") first"
	}

	action := normalizeBrowserToolAction(actionText)
	if action == browserToolActionSessionStart {
		args = cloneBrowserToolArgs(args)
		args["mode"] = stableBrowserSessionMode(args)
	}
	if action == browserToolActionSessionStop {
		args = cloneBrowserToolArgs(args)
		args["close_browser"] = false
	}
	if action == browserToolActionConnect {
		action = browserToolActionSessionStart
		args = cloneBrowserToolArgs(args)
		args["action"] = string(browserToolActionSessionStart)
		if _, ok := args["reuse_existing"]; !ok {
			args["reuse_existing"] = true
		}
		args["mode"] = stableBrowserSessionMode(args)
	}
	if action == browserToolActionTaskVerify {
		if _, hasCriteria := args["criteria"]; !hasCriteria {
			if successCriteria, ok := args["success_criteria"]; ok {
				args = cloneBrowserToolArgs(args)
				args["criteria"] = successCriteria
			}
		}
	}
	if action == browserToolActionObserve {
		args = cloneBrowserToolArgs(args)
		args["include_screenshot"] = false
	}
	if browserActionUnsupportedInMerged(action) {
		return fmt.Sprintf("browser action %s is not wired to the stable session path; use session_start plus normal page actions", action)
	}
	if !browserActionSupportedInMerged(action) {
		return fmt.Sprintf("unknown browser action: %s (supported: %s)", action, strings.Join(browserSupportedActionNames(), ", "))
	}
	if msg := rejectLikelyMojibakeBrowserArgs(action, args); msg != "" {
		return msg
	}
	if browserActionRequiresSession(action) && strings.TrimSpace(stringArgForMergedBrowser(args, "session_id")) == "" {
		return fmt.Sprintf("browser action %s requires session_id. First call browser(action=\"session_start\") and use the returned browser-session-*.", action)
	}

	toolName := action.ToolName()
	tool, ok := registry.Get(toolName)
	if !ok || tool.Handler == nil {
		return fmt.Sprintf("browser tool %s is not registered or has no handler", toolName)
	}
	return tool.Handler(args)
}

func browserActionSupportedInMerged(action browserToolAction) bool {
	for _, supported := range mergedBrowserSupportedActions {
		if action == supported {
			return true
		}
	}
	return false
}

func browserActionRequiresSession(action browserToolAction) bool {
	switch action {
	case browserToolActionSessionStart, browserToolActionConnect, browserToolActionListFlows:
		return false
	case browserToolActionUnknown:
		return false
	default:
		return true
	}
}

func browserActionUnsupportedInMerged(action browserToolAction) bool {
	switch action {
	case browserToolActionEval, browserToolActionClickAt, browserToolActionGetText, browserToolActionGetHTML, browserToolActionScreenshot, browserToolActionOCR, browserToolActionTaskReplay, browserToolActionRecordStart, browserToolActionRecordStop:
		return true
	default:
		return false
	}
}

func stringArgForMergedBrowser(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func stableBrowserSessionMode(args map[string]interface{}) string {
	mode := strings.ToLower(strings.TrimSpace(stringArgForMergedBrowser(args, "mode")))
	switch mode {
	case "", "auto":
		return "persistent"
	case "persistent", "isolated", "connect_user":
		return mode
	default:
		return "persistent"
	}
}

func cloneBrowserToolArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	return out
}

func rejectLikelyMojibakeBrowserArgs(action browserToolAction, args map[string]interface{}) string {
	for _, key := range []string{"selector", "text", "value", "query"} {
		value := stringArgForMergedBrowser(args, key)
		if !looksLikeBrowserMojibake(value) {
			continue
		}
		return fmt.Sprintf("browser action %s arg %s looks like mojibake/wrong encoding; run observe again and use returned ref instead of Chinese text or corrupted selector", action, key)
	}
	return ""
}

func looksLikeBrowserMojibake(value string) bool {
	if strings.ContainsRune(value, '\ufffd') {
		return true
	}
	for _, marker := range []string{
		"\u9352\u638d\u68d4\u95ca",
		"\u9352\u55d5\u95ca", "\u6d63\u612d\u6c49", "\u9359\u621e\u7d11", "\u942d\u7ac0\u7bb0",
		"\u93c3\u5b2b\u7d85", "\u93ba\u509b\u7d8d", "\u9359\u621d\u7afd", "\u6d93\u5d85\u57aa",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
