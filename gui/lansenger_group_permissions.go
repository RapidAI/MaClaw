package main

import (
	"fmt"
	"path/filepath"
	"strings"

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
}

func lansengerGroupPermissionsFromConfig(cfg *corelib.AppConfig) lansengerGroupPermissionPolicy {
	if cfg == nil {
		return lansengerGroupPermissionPolicy{}
	}
	return lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs:  append([]string(nil), cfg.LansengerGroupKnowledgeSourceIDs...),
		AllowAllDirectories: cfg.LansengerGroupAllowAllDirectories,
		AllowedDirectories:  append([]string(nil), cfg.LansengerGroupAllowedDirectories...),
	}
}

func (p lansengerGroupPermissionPolicy) allowsKnowledge() bool {
	return len(p.KnowledgeSourceIDs) > 0
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
	if !p.allowsKnowledge() {
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
		args["source_ids"] = append([]string(nil), p.KnowledgeSourceIDs...)
		return nil
	}
	allowed := make(map[string]struct{}, len(p.KnowledgeSourceIDs))
	for _, id := range p.KnowledgeSourceIDs {
		allowed[strings.TrimSpace(id)] = struct{}{}
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
