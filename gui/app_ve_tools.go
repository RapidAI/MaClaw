package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// veRemoteToolDefinitions returns the tool definitions available to VE sessions.
// These are safe, read-only tools that don't modify the filesystem or execute commands.
// Knowledge base tools (knowledge_search, knowledge_context_pack) are included because
// they only query the local SQLite FTS index — purely read-only, no LLM calls, no network.
func veRemoteToolDefinitions(hasKnowledge bool) []map[string]interface{} {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "read_file",
				"description": "读取本地文件内容。可以读取文本文件、代码文件、配置文件等。注意：敏感文件（.env、私钥等）不可读取。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "文件的绝对路径",
						},
						"lines": map[string]interface{}{
							"type":        "integer",
							"description": "只读取前 N 行（可选）",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "list_directory",
				"description": "列出目录中的文件和子目录。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "目录的绝对路径",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	}

	if hasKnowledge {
		tools = append(tools,
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "knowledge_search",
					"description": "搜索本地知识库。当用户提问时，优先使用此工具检索知识库中的相关信息。知识库包含所有者保存的网页、文档、笔记等结构化知识。查询基于本地 SQLite 全文搜索，不调用 LLM，不访问网络。",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "搜索关键词",
							},
							"limit": map[string]interface{}{
								"type":        "integer",
								"description": "最大结果数，默认 8，最大 50",
							},
						},
						"required": []string{"query"},
					},
				},
			},
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "knowledge_context_pack",
					"description": "构建知识库上下文包。当需要综合多个知识来源回答复杂问题时使用。返回带引用的排名结果，适合需要多源综合的问题。基于本地 SQLite 全文搜索，不调用 LLM。",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "搜索关键词",
							},
							"max_items": map[string]interface{}{
								"type":        "integer",
								"description": "最大条目数，默认 8，最大 30",
							},
							"max_chars": map[string]interface{}{
								"type":        "integer",
								"description": "最大总字符数，默认 6000，最大 20000",
							},
						},
						"required": []string{"query"},
					},
				},
			},
		)
	}

	return tools
}

// executeVERemoteTool executes a tool call in VE remote mode.
// Only safe, read-only tools are supported.
func executeVERemoteTool(app *App, name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("[error] 参数解析失败: %v", err)
	}

	switch name {
	case "read_file":
		return veToolReadFile(args)
	case "list_directory":
		return veToolListDirectory(args)
	case "knowledge_search":
		return veToolKnowledgeSearch(app, args)
	case "knowledge_context_pack":
		return veToolKnowledgeContextPack(app, args)
	default:
		return fmt.Sprintf("[error] 工具 %s 在数字员工模式下不可用", name)
	}
}

func veToolReadFile(args map[string]interface{}) string {
	path, _ := args["path"].(string)
	if path == "" {
		return "[error] path 参数不能为空"
	}

	// Security: prevent path traversal and sensitive file access
	cleanPath := filepath.Clean(path)
	if vePathIsSensitive(cleanPath) {
		return "[error] 无法读取敏感文件（如 .env、私钥等）"
	}

	// Check file size before reading to avoid OOM on large files
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Sprintf("[error] 无法访问文件: %v", err)
	}
	if info.IsDir() {
		return "[error] 路径是目录，请使用 list_directory"
	}
	const maxFileSize = 2 * 1024 * 1024 // 2MB
	if info.Size() > maxFileSize {
		return fmt.Sprintf("[error] 文件过大（%d bytes），最大支持 2MB", info.Size())
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Sprintf("[error] 无法读取文件: %v", err)
	}

	content := string(data)

	// Apply line limit if specified
	if lines, ok := args["lines"].(float64); ok && lines > 0 {
		lineSlice := strings.Split(content, "\n")
		limit := int(lines)
		if limit < len(lineSlice) {
			content = strings.Join(lineSlice[:limit], "\n")
			content += fmt.Sprintf("\n\n... [只显示前 %d 行，共 %d 行]", limit, len(lineSlice))
		}
	}

	// Truncate output for LLM context (50KB)
	if len(content) > 50*1024 {
		content = content[:50*1024] + "\n\n... [输出已截断到 50KB]"
	}

	return content
}

func veToolListDirectory(args map[string]interface{}) string {
	path, _ := args["path"].(string)
	if path == "" {
		return "[error] path 参数不能为空"
	}

	cleanPath := filepath.Clean(path)

	// Block listing of sensitive directories
	lowerPath := strings.ToLower(cleanPath)
	if strings.Contains(lowerPath, ".ssh") || strings.Contains(lowerPath, "secrets") {
		return "[error] 无法列出敏感目录"
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return fmt.Sprintf("[error] 无法读取目录: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("目录: %s\n\n", cleanPath))

	// Limit to 200 entries
	limit := 200
	if len(entries) > limit {
		sb.WriteString(fmt.Sprintf("(共 %d 项，只显示前 %d 项)\n\n", len(entries), limit))
		entries = entries[:limit]
	}

	for _, entry := range entries {
		info, _ := entry.Info()
		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("%s/\n", entry.Name()))
		} else if info != nil {
			sb.WriteString(fmt.Sprintf("%s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			sb.WriteString(fmt.Sprintf("%s\n", entry.Name()))
		}
	}

	return sb.String()
}

// vePathIsSensitive checks if a file path points to a sensitive file
// that should not be exposed to remote VE users.
// All checks are case-insensitive (.ENV, .Pem, .KEY are all blocked).
func vePathIsSensitive(path string) bool {
	lower := strings.ToLower(filepath.Base(path))

	// Sensitive file patterns (exact base name match)
	sensitiveExactNames := []string{
		".env",
		"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
		"credentials", "credentials.json",
		".npmrc", ".pypirc", ".netrc",
		"keystore.jks", "keystore.p12",
	}
	for _, s := range sensitiveExactNames {
		if lower == s {
			return true
		}
	}

	// .env prefix pattern: blocks .env.local, .env.production, .env.staging, etc.
	// Case-insensitive because lower is already lowercased.
	if strings.HasPrefix(lower, ".env.") {
		return true
	}

	// Sensitive directory names
	dir := strings.ToLower(filepath.Dir(path))
	if strings.Contains(dir, ".ssh") || strings.Contains(dir, "secrets") {
		return true
	}

	// Sensitive extensions
	sensitiveExts := []string{".pem", ".key", ".p12", ".pfx", ".jks"}
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range sensitiveExts {
		if ext == e {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// Knowledge base tools for VE sessions (read-only, local SQLite FTS only)
// ---------------------------------------------------------------------------

// veToolKnowledgeSearch executes a knowledge_search query via the App's knowledge store.
// This is a read-only operation: local SQLite FTS search, no LLM calls, no network access.
func veToolKnowledgeSearch(app *App, args map[string]interface{}) string {
	if app == nil {
		return "[error] 知识库不可用"
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "[error] query 参数不能为空"
	}
	limit := 8
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	if limit > 50 {
		limit = 50
	}
	return app.toolKnowledgeSearch(map[string]interface{}{
		"query": query,
		"limit": float64(limit),
	})
}

// veToolKnowledgeContextPack builds a knowledge context pack via the App's knowledge store.
// This is a read-only operation: local SQLite FTS search, no LLM calls, no network access.
func veToolKnowledgeContextPack(app *App, args map[string]interface{}) string {
	if app == nil {
		return "[error] 知识库不可用"
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "[error] query 参数不能为空"
	}
	maxItems := 8
	if m, ok := args["max_items"].(float64); ok && m > 0 {
		maxItems = int(m)
	}
	if maxItems > 30 {
		maxItems = 30
	}
	maxChars := 6000
	if m, ok := args["max_chars"].(float64); ok && m > 0 {
		maxChars = int(m)
	}
	if maxChars > 20000 {
		maxChars = 20000
	}
	return app.toolKnowledgeContextPack(map[string]interface{}{
		"query":     query,
		"max_items": float64(maxItems),
		"max_chars": float64(maxChars),
	})
}

// veHasKnowledgeSources checks if the knowledge base has any content.
// Used to decide whether to expose knowledge tools to VE sessions.
func veHasKnowledgeSources(app *App) bool {
	if app == nil {
		return false
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		return false
	}
	defer store.Close()
	stats, err := store.Stats(context.Background())
	if err != nil {
		return false
	}
	return stats.Sources > 0
}
