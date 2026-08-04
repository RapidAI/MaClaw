package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// lansengerGroupPermissionPolicy is applied only to local blue-letter group
// turns. Private chats, desktop turns, and other IM integrations retain their
// existing permissions.
type lansengerGroupPermissionPolicy struct {
	KnowledgeSourceIDs  []string
	AllowWebSearch      bool
	AllowAllDirectories bool
	AllowedDirectories  []string

	// knowledgePriority is intentionally a pointer so copies made while
	// filtering tool definitions share the same per-turn state and never copy a
	// mutex. It is initialized lazily to preserve zero-value test policies.
	knowledgePriority *lansengerGroupKnowledgePriorityState
}

type lansengerGroupKnowledgePriorityState struct {
	mu                       sync.Mutex
	knowledgeSearchAttempted bool
	knowledgeSearchNoResult  bool
	knowledgeEvidenceFound   bool
}

var lansengerGroupKnowledgePriorityInitMu sync.Mutex

// This internal argument marks the narrowly-scoped public-network contract
// after group permission checks have succeeded. It is never advertised in a
// tool schema and lets the shared handlers disable implicit browser state.
const lansengerGroupPublicWebToolFlag = "_lansenger_group_public_web"

func lansengerGroupPermissionsFromConfig(cfg *corelib.AppConfig) lansengerGroupPermissionPolicy {
	if cfg == nil {
		return lansengerGroupPermissionPolicy{}
	}
	return lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs:  normalizedLansengerKnowledgeSourceIDs(cfg.LansengerGroupKnowledgeSourceIDs),
		AllowWebSearch:      cfg.LansengerGroupAllowWebSearch,
		AllowAllDirectories: cfg.LansengerGroupAllowAllDirectories,
		AllowedDirectories:  append([]string(nil), cfg.LansengerGroupAllowedDirectories...),
		knowledgePriority:   &lansengerGroupKnowledgePriorityState{},
	}
}

func (p lansengerGroupPermissionPolicy) allowsKnowledge() bool {
	return len(normalizedLansengerKnowledgeSourceIDs(p.KnowledgeSourceIDs)) > 0
}

// normalizedLansengerKnowledgeSourceIDs is deliberately shared by the config
// boundary and execution boundary. An empty source_ids filter means "all
// sources" to the knowledge store, so blank configured IDs must never survive
// long enough to become an unscoped group lookup.
func normalizedLansengerKnowledgeSourceIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (p *lansengerGroupPermissionPolicy) knowledgePriorityState() *lansengerGroupKnowledgePriorityState {
	if p == nil {
		return nil
	}
	lansengerGroupKnowledgePriorityInitMu.Lock()
	defer lansengerGroupKnowledgePriorityInitMu.Unlock()
	if p.knowledgePriority == nil {
		p.knowledgePriority = &lansengerGroupKnowledgePriorityState{}
	}
	return p.knowledgePriority
}

// markKnowledgeAutoRecallEvidence records that the prompt already contains a
// relevant result from the authorised knowledge sources. In that case web
// research is not a fallback for this turn.
func (p *lansengerGroupPermissionPolicy) markKnowledgeAutoRecallEvidence() {
	if p == nil {
		return
	}
	state := p.knowledgePriorityState()
	state.mu.Lock()
	state.knowledgeEvidenceFound = true
	state.mu.Unlock()
}

// webFallbackBlockReason enforces the group-turn order: memory (prompt
// context), authorised knowledge, then the network only when that knowledge
// lookup had no result. It deliberately has no effect for groups that were not
// granted any knowledge sources.
func (p *lansengerGroupPermissionPolicy) webFallbackBlockReason() string {
	if p == nil || !p.allowsKnowledge() {
		return ""
	}
	state := p.knowledgePriorityState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.knowledgeEvidenceFound {
		return "已在已授权知识库中找到相关内容，请基于该内容回复；只有知识库无足够信息时才可使用网络搜索"
	}
	if !state.knowledgeSearchAttempted {
		return "群聊必须先检索已授权知识库。请先调用 knowledge_search；仅在知识库没有相关结果时才可使用网络搜索"
	}
	if !state.knowledgeSearchNoResult {
		return "已授权知识库检索未返回可用的无结果结论，请修正或重试 knowledge_search；不能直接改用网络搜索"
	}
	return ""
}

func (p *lansengerGroupPermissionPolicy) requiresKnowledgeLookup() bool {
	if p == nil || !p.allowsKnowledge() {
		return false
	}
	state := p.knowledgePriorityState()
	state.mu.Lock()
	defer state.mu.Unlock()
	return !state.knowledgeEvidenceFound && !state.knowledgeSearchNoResult
}

// recordKnowledgeSearchResult advances the per-turn fallback state after the
// scoped knowledge_search handler returns. The built-in handler returns an
// explicit successful JSON object with count and results, so a malformed or
// incomplete tool response cannot be misread as permission to use the network.
func (p *lansengerGroupPermissionPolicy) recordKnowledgeSearchResult(result toolExecutionResult) {
	if p == nil || !p.allowsKnowledge() {
		return
	}
	state := p.knowledgePriorityState()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.knowledgeSearchAttempted = true
	if result.Outcome != toolOutcomeSucceeded {
		return
	}
	var payload struct {
		OK      *bool            `json:"ok"`
		Count   *int             `json:"count"`
		Results *json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Text)), &payload); err == nil &&
		payload.OK != nil && *payload.OK && payload.Count != nil && payload.Results != nil {
		results, valid := lansengerKnowledgeSearchResults(*payload.Results, *payload.Count)
		if !valid {
			return
		}
		if *payload.Count > 0 && len(results) > 0 {
			state.knowledgeEvidenceFound = true
		} else if *payload.Count == 0 && len(results) == 0 {
			state.knowledgeSearchNoResult = true
		}
	}
}

// lansengerKnowledgeSearchResults validates the built-in search response's
// count/results invariant before it can alter network permission. This keeps a
// truncated or custom-tool response from fabricating a no-result fallback or
// suppressing a required retry.
func lansengerKnowledgeSearchResults(raw json.RawMessage, count int) ([]json.RawMessage, bool) {
	if count < 0 {
		return nil, false
	}
	var results []json.RawMessage
	if err := json.Unmarshal(raw, &results); err != nil || len(results) != count {
		return nil, false
	}
	return results, true
}

func lansengerGroupKnowledgePriorityPrompt() string {
	return `
## 群聊信息来源优先级
- 本轮必须按以下顺序作答：已有记忆与会话上下文 → 已授权的本地知识库 → 网络。
- 当已授权知识库可用时，先使用当前提示中已召回的知识；若仍不足，必须先调用 knowledge_search 检索已授权范围。
- 知识库检索有相关结果时，基于其内容回复，不得改用 web_search 或 web_fetch 补充、替代或臆测。
- 仅当已授权知识库检索没有相关结果时，网络才是兜底来源。`
}

func (p lansengerGroupPermissionPolicy) allowsTool(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "memory" {
		// The action is checked again immediately before execution. A group needs
		// its owner-scoped recall path as the first information source, but other
		// read-only memory actions include store-wide inspection views and are not
		// safe to expose in a group.
		return true
	}
	if isLansengerKnowledgeTool(name) {
		switch name {
		case "knowledge_search", "knowledge_explain", "knowledge_context_pack", "knowledge_search_facets":
			return p.allowsKnowledge()
		default:
			// Group permission grants retrieval only. Source maintenance, import,
			// export, metadata and other knowledge-management tools can disclose or
			// alter resources beyond the configured source scope.
			return false
		}
	}
	switch name {
	case "read_file", "list_directory", "search_files", "send_file", "send_to_im":
		return p.AllowAllDirectories || len(p.AllowedDirectories) > 0
	case "current_datetime":
		return true
	case "web_search", "web_fetch":
		// Network access is opt-in for group bots. Downloads are constrained to
		// the agent working directory by the shared web_fetch implementation.
		return p.AllowWebSearch
	default:
		// Fail closed. Registered tools evolve frequently and many apparently
		// innocuous tools (for example git_status, project search, browser/MCP
		// bridges, skills, and task helpers) can reach local state through an
		// argument or an implicit working directory. A group permission must
		// never become a grant for a newly-added tool just because it was not
		// included in a deny list above.
		return false
	}
}

func markLansengerGroupPublicWebToolArgs(args map[string]interface{}) {
	if args != nil {
		args[lansengerGroupPublicWebToolFlag] = true
	}
}

func isLansengerGroupPublicWebToolCall(args map[string]interface{}) bool {
	value, _ := args[lansengerGroupPublicWebToolFlag].(bool)
	return value
}

// lansengerGroupPublicWebSearchStrategy removes browser-based engines and the
// browser fallback. A group permission authorizes public research, not access
// through the user's managed browser profile.
func lansengerGroupPublicWebSearchStrategy(strategy corelib.WebSearchStrategy) corelib.WebSearchStrategy {
	strategy.BrowserFallbackEnabled = false
	strategy.BrowserHumanAssistEnabled = false
	strategy.Engines = append([]corelib.WebSearchEngineConfig(nil), strategy.Engines...)
	for i := range strategy.Engines {
		// API engines use configured user credentials. Baidu's HTML adapter
		// acquires a verification cookie. The group contract allows only
		// anonymous public HTTP search.
		if strategy.Engines[i].Transport != corelib.WebSearchTransportHTTPHTML || strategy.Engines[i].ID == "baidu" {
			strategy.Engines[i].Enabled = false
		}
	}
	return strategy
}

func (p lansengerGroupPermissionPolicy) allowsMemoryAction(action string) bool {
	return normalizeMemoryToolAction(action) == memoryToolActionRecall
}

// restrictMemoryRecallArgs preserves the narrow group-memory contract at both
// the schema and dispatch boundaries. Group owners are conversations, not
// project workspaces, so all caller-controlled recall scope selectors are
// discarded before the shared memory tool executes.
func (p lansengerGroupPermissionPolicy) restrictMemoryRecallArgs(args map[string]interface{}) {
	if args == nil {
		return
	}
	delete(args, "project_path")
	delete(args, "project")
	delete(args, "category")
}

// memoryRecallTransportBlockReason rejects recall modes that carry state from
// another request.  Group memory is intentionally a small, owner-isolated
// lookup; cursors, scroll sessions, and exhaustive scans are not part of that
// contract.  Keep this at the execution boundary as well as in the schema so
// manually constructed tool calls cannot bypass the advertised interface.
func (p lansengerGroupPermissionPolicy) memoryRecallTransportBlockReason(args map[string]interface{}) string {
	if strings.TrimSpace(nonEmptyStringFromAny(args["cursor"])) != "" ||
		lansengerGroupBoolToolArg(args, "session") ||
		strings.EqualFold(strings.TrimSpace(nonEmptyStringFromAny(args["mode"])), "exhaustive") {
		return "群聊中的 memory 工具不支持分页、滚动会话或 exhaustive 检索"
	}
	return ""
}

func lansengerGroupBoolToolArg(args map[string]interface{}, key string) bool {
	value, ok := args[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	default:
		return false
	}
}

func isLansengerKnowledgeTool(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "knowledge_")
}

func localizedLansengerGroupCommandRestrictedMessage(lang string) string {
	if normalizeAppLanguageKind(lang).IsChinese() {
		return "群聊权限不允许执行快捷命令；请在私聊中操作。"
	}
	return "Group permissions do not allow shortcut commands. Please use a private chat."
}

func filterToolsForLansengerGroupPermissions(tools []map[string]interface{}, policy lansengerGroupPermissionPolicy) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		name := tool.ExtractToolName(def)
		if policy.allowsTool(name) {
			switch name {
			case "memory":
				out = append(out, lansengerGroupMemoryRecallToolDefinition(def))
			case "web_fetch":
				out = append(out, lansengerGroupWebFetchToolDefinition(def))
			default:
				out = append(out, def)
			}
		}
	}
	return out
}

// lansengerGroupMemoryRecallToolDefinition gives a group turn a narrow view of
// the shared memory tool. Execution still enforces this restriction, but a
// recall-only schema prevents the model from attempting write or store-wide
// inspection actions in the first place. The input definition is copied so a
// group-specific description never changes the desktop or private-chat schema.
func lansengerGroupMemoryRecallToolDefinition(def map[string]interface{}) map[string]interface{} {
	result := copyLansengerGroupToolMap(def)
	function, ok := def["function"].(map[string]interface{})
	if !ok {
		return result
	}
	functionCopy := copyLansengerGroupToolMap(function)
	functionCopy["description"] = "只读群聊记忆检索。action 必须为 recall；仅检索当前蓝信群会话自己的记忆。"
	parameters, ok := function["parameters"].(map[string]interface{})
	if !ok {
		result["function"] = functionCopy
		return result
	}
	parametersCopy := copyLansengerGroupToolMap(parameters)
	properties := map[string]interface{}{
		"action": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"recall"},
			"description": "固定为 recall。",
		},
		"query": copyLansengerGroupMemoryProperty(parameters, "query"),
		"tags":  copyLansengerGroupMemoryProperty(parameters, "tags"),
		"limit": copyLansengerGroupMemoryProperty(parameters, "limit"),
	}
	for name, property := range properties {
		if property == nil {
			delete(properties, name)
		}
	}
	parametersCopy["properties"] = properties
	parametersCopy["required"] = []string{"action"}
	functionCopy["parameters"] = parametersCopy
	result["function"] = functionCopy
	return result
}

// lansengerGroupWebFetchToolDefinition advertises the narrower public-web
// contract used in group conversations. The execution boundary below enforces
// the same restriction for manually constructed tool calls.
func lansengerGroupWebFetchToolDefinition(def map[string]interface{}) map[string]interface{} {
	result := copyLansengerGroupToolMap(def)
	function, ok := def["function"].(map[string]interface{})
	if !ok {
		return result
	}
	functionCopy := copyLansengerGroupToolMap(function)
	functionCopy["description"] = "抓取公开网页内容或下载公开文件。不可使用浏览器登录态、Cookie 或自定义请求头。"
	parameters, ok := function["parameters"].(map[string]interface{})
	if !ok {
		result["function"] = functionCopy
		return result
	}
	parametersCopy := copyLansengerGroupToolMap(parameters)
	if properties, ok := parameters["properties"].(map[string]interface{}); ok {
		propertiesCopy := copyLansengerGroupToolMap(properties)
		for _, name := range []string{"render_js", "headers", "cookie", "use_browser_cookies", "via_browser"} {
			delete(propertiesCopy, name)
		}
		parametersCopy["properties"] = propertiesCopy
	}
	functionCopy["parameters"] = parametersCopy
	result["function"] = functionCopy
	return result
}

func copyLansengerGroupToolMap(source map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func copyLansengerGroupMemoryProperty(parameters map[string]interface{}, name string) interface{} {
	properties, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		return nil
	}
	property, ok := properties[name]
	if !ok {
		return nil
	}
	if propertyMap, ok := property.(map[string]interface{}); ok {
		return copyLansengerGroupToolMap(propertyMap)
	}
	return property
}

// restrictWebFetchArgs prevents a group message from turning the workstation's
// authenticated browser state into a network credential. Public web fetches
// and explicit file downloads remain allowed when AllowWebSearch is enabled.
func (p lansengerGroupPermissionPolicy) restrictWebFetchArgs(args map[string]interface{}) error {
	if args == nil {
		return nil
	}
	for _, key := range []string{"render_js", "headers", "cookie", "use_browser_cookies", "via_browser"} {
		if value, exists := args[key]; exists && lansengerGroupWebFetchSensitiveArgSet(value) {
			return fmt.Errorf("群聊 web_fetch 不允许 %s；仅支持访问公开网页", key)
		}
	}
	return nil
}

func lansengerGroupWebFetchSensitiveArgSet(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]interface{}:
		return len(typed) > 0
	case map[string]string:
		return len(typed) > 0
	default:
		return true
	}
}

func (p lansengerGroupPermissionPolicy) restrictKnowledgeArgs(args map[string]interface{}) error {
	allowedIDs := normalizedLansengerKnowledgeSourceIDs(p.KnowledgeSourceIDs)
	if len(allowedIDs) == 0 {
		return fmt.Errorf("群聊权限未授权访问知识库")
	}
	// A disabled source is deliberately unavailable until its owner restores it.
	// A group permission selects eligible sources; it must not let a caller
	// override that lifecycle state with an include_disabled flag.
	delete(args, "include_disabled")
	// The source allowlist is the complete group boundary. Project scope filters
	// are global workspace selectors and can make an otherwise authorised source
	// look empty (or disclose whether an unrelated project exists). Keep group
	// retrieval source-scoped and independent of the desktop's current project.
	delete(args, "project_path")
	delete(args, "search_scope")
	requested, specified, err := knowledgeSourceIDsFromArgs(args)
	if err != nil {
		return err
	}
	if len(requested) == 0 {
		if specified {
			return fmt.Errorf("知识库来源必须是非空字符串数组")
		}
		args["source_ids"] = append([]string(nil), allowedIDs...)
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]string, 0, len(requested))
	for _, id := range requested {
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("知识库来源 %q 不在当前群聊权限范围内", id)
		}
		filtered = append(filtered, id)
	}
	args["source_ids"] = filtered
	delete(args, "ids")
	return nil
}

func knowledgeSourceIDsFromArgs(args map[string]interface{}) ([]string, bool, error) {
	if args == nil {
		return nil, false, nil
	}
	for _, key := range []string{"source_ids", "ids"} {
		value, ok := args[key]
		if !ok {
			continue
		}
		var raw []interface{}
		switch typed := value.(type) {
		case []interface{}:
			raw = typed
		case []string:
			raw = make([]interface{}, len(typed))
			for i := range typed {
				raw[i] = typed[i]
			}
		default:
			return nil, true, fmt.Errorf("知识库来源必须是字符串数组")
		}
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
				out = append(out, strings.TrimSpace(id))
			}
		}
		return out, true, nil
	}
	return nil, false, nil
}

func (p lansengerGroupPermissionPolicy) validateFileToolArgs(name string, args map[string]interface{}) error {
	if p.AllowAllDirectories {
		return nil
	}
	if len(p.AllowedDirectories) == 0 {
		return fmt.Errorf("群聊权限未授权访问本地目录")
	}
	path := stringFromLansengerGroupFileArgs(args, name)
	switch strings.TrimSpace(name) {
	case "read_file", "send_file", "send_to_im":
		_, err := ValidateVEFilePath(path, p.AllowedDirectories)
		return err
	case "list_directory", "search_files":
		_, err := IsWithinAllowedDirs(path, p.AllowedDirectories)
		return err
	default:
		return nil
	}
}

// resolveAndValidateFileToolArgs makes the permission check use the exact path
// the local tool will receive. File tools accept relative paths, but their
// execution base can be a project workspace rather than the process working
// directory. Validating the un-resolved argument would therefore permit a
// relative path based on one directory and read it from another.
func (p lansengerGroupPermissionPolicy) resolveAndValidateFileToolArgs(name string, args map[string]interface{}, resolvePath func(string) (string, error)) error {
	if p.AllowAllDirectories {
		return nil
	}
	if resolvePath == nil {
		return fmt.Errorf("群聊权限无法解析本地路径")
	}
	key := "path"
	if strings.TrimSpace(name) == "search_files" {
		key = "project_path"
	}
	raw, _ := args[key].(string)
	resolved, err := resolvePath(raw)
	if err != nil {
		return err
	}
	// Preserve the resolved path for the registered tool as well as the
	// validation step, eliminating a check/use base-directory mismatch.
	args[key] = resolved
	return p.validateFileToolArgs(name, args)
}

func stringFromLansengerGroupFileArgs(args map[string]interface{}, name string) string {
	if args == nil {
		return ""
	}
	if name == "search_files" {
		if path, ok := args["project_path"].(string); ok && strings.TrimSpace(path) != "" {
			return path
		}
	}
	path, _ := args["path"].(string)
	return path
}

// restrictAbsolutePath protects shell-like tools that do not have a dedicated
// path argument contract. They are not added to the group tool menu, but this
// helper keeps the allowlist semantics available for future file tools.
func (p lansengerGroupPermissionPolicy) restrictAbsolutePath(path string) error {
	if p.AllowAllDirectories || !filepath.IsAbs(path) {
		return nil
	}
	_, err := IsWithinAllowedDirs(path, p.AllowedDirectories)
	return err
}
