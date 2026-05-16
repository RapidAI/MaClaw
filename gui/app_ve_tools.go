package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// veRemoteToolDefinitions returns the tool definitions available to VE sessions.
// These are safe, read-only tools that don't modify the filesystem or execute commands.
func veRemoteToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
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
			sb.WriteString(fmt.Sprintf("📁 %s/\n", entry.Name()))
		} else if info != nil {
			sb.WriteString(fmt.Sprintf("📄 %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			sb.WriteString(fmt.Sprintf("📄 %s\n", entry.Name()))
		}
	}

	return sb.String()
}

// vePathIsSensitive checks if a file path points to a sensitive file
// that should not be exposed to remote VE users.
func vePathIsSensitive(path string) bool {
	lower := strings.ToLower(filepath.Base(path))

	// Sensitive file patterns (exact base name match or prefix match)
	sensitiveExactNames := []string{
		".env", ".env.local", ".env.production", ".env.development",
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
