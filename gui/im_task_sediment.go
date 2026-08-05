package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// directOutputTools lists tools that directly create user-visible artifacts.
// Shell-like tools are checked separately because commands such as `date` or
// `git status` are reads, while tests/builds/patches produce task evidence.
var directOutputTools = map[string]bool{
	"write_file":         true,
	"edit_file":          true,
	"edit_lines":         true,
	"generate_pdf":       true,
	"send_file":          true,
	"run_skill":          true,
	"save_artifact":      true,
	"save_artifact_full": true,
}

var productiveTools = map[string]bool{
	"bash":               true,
	"exec_cmd":           true,
	"exec_command":       true,
	"powershell":         true,
	"cmd":                true,
	"ssh":                true,
	"browser":            true,
	"manage_skill":       true,
	"manage_schedule":    true,
	"im_message":         true,
	"manage_template":    true,
	"write_file":         true,
	"edit_file":          true,
	"edit_lines":         true,
	"generate_pdf":       true,
	"send_file":          true,
	"run_skill":          true,
	"save_artifact":      true,
	"save_artifact_full": true,
}

var outputToolActions = map[string]map[string]bool{
	"manage_skill": {
		"install":   true,
		"patch":     true,
		"run":       true,
		"uninstall": true,
		"upload":    true,
	},
	"manage_schedule": {
		"create": true,
		"delete": true,
		"update": true,
	},
	"im_message": {
		"send": true,
	},
	"manage_template": {
		"create": true,
		"launch": true,
	},
	"office": {
		"generate_pdf": true,
		"write_excel":  true,
	},
}

// sedimentTaskEntry saves a lightweight project_knowledge entry when the
// agent loop produced tangible output, so the task appears in the recent
// tasks list.
//
// Gate: at least one side-effect tool call must be present in the
// conversation history. This objectively separates "tasks with output"
// from pure chat / lookups / reads.
//
// Project path: every task gets a unique standalone path derived from its
// title hash, so each task appears as a separate item in the sidebar.
// The current project path is added as a secondary tag for search affinity
// but does NOT determine the index key (inferProjectPath picks the first
// path-like tag, which is the standalone path).
func (h *IMMessageHandler) sedimentTaskEntry(userID string, history []agent.ConversationEntry) {
	if isIsolatedAssistantSessionUserID(userID) {
		return
	}
	if h.memoryStore == nil || len(history) == 0 {
		return
	}

	userRequest, turnHistory := latestSedimentTurn(history)
	if userRequest == "" {
		return
	}
	outputTools, resultSummary := conversationHasTangibleOutput(turnHistory)
	if len(outputTools) == 0 {
		return
	}

	// Skip system-generated switch messages.
	if r := []rune(userRequest); len(r) > 0 && (r[0] == 0x1F516 || r[0] == 0x1F4C2 || r[0] == 0x1F4C1) {
		return
	}

	title := buildSedimentTitleFromConversation(userRequest, resultSummary)
	if title == "" {
		return
	}

	// Brief content: request + last assistant snippet.
	var lastReply string
	for i := len(turnHistory) - 1; i >= 0; i-- {
		if turnHistory[i].Role == "assistant" {
			if s, ok := turnHistory[i].Content.(string); ok && strings.TrimSpace(s) != "" {
				lastReply = strings.TrimSpace(s)
				break
			}
		}
	}
	var buf strings.Builder
	buf.WriteString("Task: ")
	buf.WriteString(truncSediment(userRequest, 200))
	if lastReply != "" {
		buf.WriteString("\nResult: ")
		buf.WriteString(truncSediment(lastReply, 300))
	}

	// Determine project path.
	// Every task gets a standalone path (appears as its own list item).
	// The current project path is added as a secondary tag for affinity.
	standalonePath, projectTag := h.resolveTaskProjectPath(title)
	tags := buildSedimentTags(standalonePath, projectTag, outputTools)

	identityTagCount := 2
	if standalonePath != "" || projectTag != "" {
		identityTagCount = 3
	}
	_, err := h.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
		Title:            title,
		Content:          buf.String(),
		Tags:             tags,
		IdentityTagCount: identityTagCount,
		Scope:            memory.ScopeProject,
		SourceType:       "task_sediment",
		OwnerID:          userID,
	})
	if err != nil {
		log.Printf("[task_sediment] upsert failed: %v", err)
	} else if h.app != nil {
		h.app.triggerMemoryPipelineSoon(45 * time.Second)
	}
}

func latestSedimentTurn(history []agent.ConversationEntry) (string, []agent.ConversationEntry) {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		if s, ok := history[i].Content.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), history[i:]
		}
	}
	return "", nil
}

type sedimentToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type sedimentToolResult struct {
	Content string
	Outcome string
}

func conversationHasTangibleOutput(history []agent.ConversationEntry) (map[string]bool, string) {
	toolResults := make(map[string]sedimentToolResult)
	for _, e := range history {
		if (e.Role == "tool" || e.Role == "tool_result") && e.ToolCallID != "" {
			toolResults[e.ToolCallID] = sedimentToolResult{Content: contentString(e.Content), Outcome: e.ToolOutcome}
		}
	}

	outputTools := make(map[string]bool)
	toolSummary := ""
	for _, e := range history {
		if e.Role != "assistant" || e.ToolCalls == nil {
			continue
		}
		for _, call := range extractSedimentToolCalls(e.ToolCalls) {
			name := strings.ToLower(strings.TrimSpace(call.Name))
			if !isProductiveOutputTool(name, call.Arguments) {
				continue
			}
			result, ok := toolResults[call.ID]
			if call.ID != "" && (!ok || toolResultFailed(result)) {
				continue
			}
			outputTools[name] = true
			if toolSummary == "" {
				toolSummary = firstSedimentResultLine(result.Content)
			}
		}
	}
	assistantSummary := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			assistantSummary = firstSedimentResultLine(contentString(history[i].Content))
			if assistantSummary != "" {
				break
			}
		}
	}
	return outputTools, chooseSedimentSummary(assistantSummary, toolSummary)
}

func chooseSedimentSummary(assistantSummary, toolSummary string) string {
	if assistantSummary != "" && !looksLikeRawToolEcho(assistantSummary) {
		return assistantSummary
	}
	if toolSummary != "" {
		return toolSummary
	}
	return assistantSummary
}

func looksLikeRawToolEcho(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "ok ") || strings.HasPrefix(lower, "success.") || strings.HasPrefix(lower, "success:")
}

func extractSedimentToolCalls(toolCalls interface{}) []sedimentToolCall {
	data, err := json.Marshal(toolCalls)
	if err != nil {
		return nil
	}
	var calls []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Args     any    `json:"arguments"`
		Function struct {
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil
	}
	out := make([]sedimentToolCall, 0, len(calls))
	for _, call := range calls {
		name := call.Function.Name
		if name == "" {
			name = call.Name
		}
		arguments := argumentText(call.Function.Arguments)
		if arguments == "" {
			arguments = argumentText(call.Args)
		}
		if name != "" {
			out = append(out, sedimentToolCall{ID: call.ID, Name: name, Arguments: arguments})
		}
	}
	return out
}

func isProductiveOutputTool(name, arguments string) bool {
	if directOutputTools[name] {
		return true
	}
	if actionLooksProductive(name, arguments) {
		return true
	}
	if isShellLikeToolName(name) {
		return shellCommandLooksProductive(arguments)
	}
	if _, hasActionPolicy := outputToolActions[name]; hasActionPolicy {
		return false
	}
	return productiveTools[name] && !isReadOnlyToolName(name)
}

func actionLooksProductive(name, arguments string) bool {
	allowed, ok := outputToolActions[name]
	if !ok {
		return false
	}
	action := strings.ToLower(strings.TrimSpace(argumentString(arguments, "action")))
	if action == "" {
		return false
	}
	if name == "manage_skill" && action == "validate" {
		return argumentBool(arguments, "auto_fix")
	}
	return allowed[action]
}

func argumentString(arguments, key string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(arguments)), &payload) != nil {
		return ""
	}
	if s, ok := payload[key].(string); ok {
		return s
	}
	return ""
}

func argumentBool(arguments, key string) bool {
	var payload map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(arguments)), &payload) != nil {
		return false
	}
	if b, ok := payload[key].(bool); ok {
		return b
	}
	return false
}

func argumentText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func isShellLikeToolName(name string) bool {
	switch name {
	case "bash", "exec_cmd", "exec_command", "powershell", "cmd", "ssh":
		return true
	default:
		return false
	}
}

func shellCommandLooksProductive(arguments string) bool {
	cmd := extractShellCommand(arguments)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	haystack := " " + lower + " "
	productiveMarkers := []string{
		"apply_patch", " >", ">>", " set-content ", " add-content ", " out-file ",
		"new-item", "remove-item", "move-item", "copy-item",
		" mkdir", " touch ", " cp ", " mv ", " rm ", " sed -i", " tee ",
		"gofmt", "go test", "go build", "go mod tidy", "go clean",
		"npm test", "npm run build", "npx vitest", "npx vite build", "npx tsc", "tsc ",
		"cargo test", "cargo build", "pytest", "make test", "make build",
		"git add", "git commit", "git push",
	}
	for _, marker := range productiveMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func extractShellCommand(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(trimmed), &payload) == nil {
		for _, key := range []string{"cmd", "command", "script"} {
			if s, ok := payload[key].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return trimmed
}

func isReadOnlyToolName(name string) bool {
	switch name {
	case "read_file", "list_directory", "glob", "grep", "rg", "web_search", "web_fetch", "screenshot", "memory", "async_wait":
		return true
	default:
		return false
	}
}

func toolResultFailed(result sedimentToolResult) bool {
	if normalizeToolOutcome(result.Outcome) == toolOutcomeFailed {
		return true
	}
	trimmed := strings.TrimSpace(result.Content)
	if trimmed == "" {
		return false
	}
	var payload map[string]interface{}
	if json.Unmarshal([]byte(trimmed), &payload) == nil {
		for _, key := range []string{"ok", "success"} {
			if v, ok := payload[key].(bool); ok && !v {
				return true
			}
		}
		if s, ok := payload["status"].(string); ok {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "error" || s == "failed" || s == "failure" {
				return true
			}
		}
		if payload["error"] != nil || payload["failed"] != nil {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "failed:") || strings.HasPrefix(lower, "[error]") || strings.HasPrefix(lower, "[stderr]") || strings.Contains(lower, "失败") || strings.Contains(lower, "错误")
}

func contentString(content interface{}) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(data)
}

func firstSedimentResultLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>*- \t"))
		line = normalizeSedimentResultLine(line)
		if line != "" && !isGenericSedimentLine(line) {
			return line
		}
	}
	return ""
}

func normalizeSedimentResultLine(line string) string {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "success. updated the following files") || strings.HasPrefix(lower, "success: updated the following files") {
		return ""
	}
	if len(trimmed) > 2 && trimmed[1] == ' ' {
		status := trimmed[:1]
		path := strings.TrimSpace(trimmed[2:])
		switch status {
		case "M":
			return "Updated " + path
		case "A":
			return "Created " + path
		case "D":
			return "Deleted " + path
		}
	}
	return trimmed
}

func isGenericSedimentLine(line string) bool {
	lower := strings.ToLower(strings.Trim(strings.TrimSpace(line), " .,!?:;"))
	switch lower {
	case "ok", "done", "completed", "finished", "success", "updated", "fixed", "created", "null", "operation completed", "successfully completed":
		return true
	default:
		return false
	}
}

func buildSedimentTags(standalonePath, projectTag string, outputTools map[string]bool) []string {
	tags := []string{"task_sediment", "auto"}
	if standalonePath != "" {
		tags = append(tags, standalonePath)
	} else if projectTag != "" {
		tags = append(tags, projectTag)
	}
	tags = append(tags, "tangible_output")
	if standalonePath != "" && projectTag != "" && projectTag != standalonePath {
		tags = append(tags, projectTag)
	}
	toolNames := make([]string, 0, len(outputTools))
	for name := range outputTools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	for _, name := range toolNames {
		tags = append(tags, "output_tool:"+name)
	}
	return tags
}

// resolveTaskProjectPath returns the standalone path and project tag for a
// task sediment entry.
//
// Every task gets its own standalone path so it appears as a separate item
// in task management. The standalone path is derived from the task
// title (deterministic hash), ensuring:
//   - Different tasks → different paths → separate list items
//   - Same task re-run → same path → updates existing item (idempotent)
//
// The current project path is returned as a secondary tag for search
// affinity. The caller adds both to Tags; inferProjectPath picks the
// standalone path (first path-like tag) as the index key.
func (h *IMMessageHandler) resolveTaskProjectPath(title string) (standalone string, projectTag string) {
	maclawDataDir := ""
	if h.app != nil {
		maclawDataDir = h.app.GetDataDir()
	}
	standalone = buildStandaloneTaskPath(maclawDataDir, title)
	// Tag with top-bar / tool working directory for search affinity.
	projectTag = h.effectiveWorkingDirForUser(desktopUserID)
	return
}

// buildStandaloneTaskPath creates a synthetic path for standalone tasks
// (tasks not tied to a specific project directory). The path is:
//
//	{maclawDataDir}/tasks/{title-hash-prefix}
//
// This is a real directory path that passes looksLikeProjectPath validation
// in ProjectIndex.inferProjectPath (Windows drive letter + 2+ segments + no
// short file extension).
//
// Properties:
//   - Deterministic: same title → same path (idempotent, no duplicate entries)
//   - Unique: different titles → different paths (each task gets its own entry)
//   - Valid: passes all ProjectIndex path validation checks
func buildStandaloneTaskPath(maclawDataDir, title string) string {
	if maclawDataDir == "" {
		return ""
	}
	// Use a short hash of the title for uniqueness + determinism.
	h := sha256.Sum256([]byte(title))
	slug := fmt.Sprintf("%x", h[:6]) // 12 hex chars
	return filepath.Join(maclawDataDir, "tasks", slug)
}

func buildSedimentTitle(req string) string {
	t := strings.TrimLeftFunc(req, func(r rune) bool {
		return r == '#' || r == '*' || r == '-' || r == ' '
	})
	runes := []rune(t)
	if len(runes) > 50 {
		cut := 50
		for i := cut; i > 30; i-- {
			if runes[i] == ' ' || runes[i] == ',' || runes[i] == '.' ||
				runes[i] == '\u3002' || runes[i] == '\uff0c' {
				cut = i
				break
			}
		}
		t = string(runes[:cut])
	}
	return strings.TrimSpace(t)
}

func buildSedimentTitleFromConversation(req, resultSummary string) string {
	if isGenericSedimentRequest(req) {
		return buildSedimentTitle(resultSummary)
	}
	return buildSedimentTitle(req)
}

func isGenericSedimentRequest(req string) bool {
	lower := strings.ToLower(strings.Trim(strings.TrimSpace(req), " .,!?:;"))
	switch lower {
	case "", "continue", "继续", "ok", "okay", "thanks", "thank you", "review/fix/optimize", "review fix optimize", "fix", "review", "optimize", "继续处理", "继续优化":
		return true
	default:
		return false
	}
}

func truncSediment(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
