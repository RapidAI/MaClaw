package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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

func lansengerGroupPermissionsFromConfig(cfg *corelib.AppConfig) lansengerGroupPermissionPolicy {
	if cfg == nil {
		return lansengerGroupPermissionPolicy{}
	}
	return lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs:  normalizedLansengerKnowledgeSourceIDs(cfg.LansengerGroupKnowledgeSourceIDs),
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
	case "current_datetime", "web_search", "web_fetch":
		// These tools do not read from or mutate local resources. web_fetch is
		// separately prevented from using save_path at execution time.
		return true
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
		if policy.allowsTool(tool.ExtractToolName(def)) {
			out = append(out, def)
		}
	}
	return out
}

func (p lansengerGroupPermissionPolicy) restrictKnowledgeArgs(args map[string]interface{}) error {
	allowedIDs := normalizedLansengerKnowledgeSourceIDs(p.KnowledgeSourceIDs)
	if len(allowedIDs) == 0 {
		return fmt.Errorf("群聊权限未授权访问知识库")
	}
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
