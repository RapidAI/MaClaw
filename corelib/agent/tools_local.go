package agent

// tools_local.go implements local tool handlers (bash, read_file, write_file,
// edit_file, list_directory, send_file, open) as standalone functions.
//
// These are the most fundamental tools shared by all platforms (GUI, TUI, IM).
// The gui/ package wraps them as IMMessageHandler methods; the shared RunLoop
// can call them directly via LoopCallbacks.ExecuteTool.
//
// Migrated from gui/im_tools_local.go as part of the agent-unification plan.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// --- Constants ---

const (
	ReadFileMaxLines = 200
	WriteFileMaxSize = 512 * 1024 // 512 KB per write
	SendFileMaxSize  = 200 << 20  // 200 MB
)

// --- Bash ---

// ToolBash executes a shell command on the host machine.
// onProgress is called every 30s with a heartbeat message for long-running commands.
func ToolBash(args map[string]interface{}, onProgress func(string)) string {
	command := StringArg(args, "command")
	if command == "" {
		return "缺少 command 参数"
	}

	timeout := ResolveBashTimeout(args, command)
	workDir := ResolvePath(StringArg(args, "working_dir"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	cmd.Dir = workDir
	cmd.Env = tool.AppendUTF8Env(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	HideCommandWindow(cmd)

	err := cmd.Start()
	if err != nil {
		return fmt.Sprintf("[错误] 命令启动失败: %v", err)
	}

	// Heartbeat goroutine.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		elapsed := 0
		for {
			select {
			case <-ticker.C:
				elapsed += 30
				displayCmd := command
				if len(displayCmd) > 60 {
					displayCmd = displayCmd[:60] + "…"
				}
				if onProgress != nil {
					onProgress(fmt.Sprintf("⏳ 命令仍在执行中（已 %ds）: %s", elapsed, displayCmd))
				}
			case <-done:
				return
			}
		}
	}()

	err = cmd.Wait()
	close(done)

	var b strings.Builder
	if stdout.Len() > 0 {
		out := stdout.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n... (输出已截断)"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (错误输出已截断)"
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[错误] 命令超时（%d 秒）", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[错误] 退出码: %v", err))
		}
	}

	if b.Len() == 0 {
		return "(命令执行完成，无输出)"
	}
	return b.String()
}

// --- Read File ---

// ToolReadFile reads a file and returns its content.
func ToolReadFile(args map[string]interface{}) string {
	p := StringArg(args, "path")
	if p == "" {
		return "缺少 path 参数"
	}
	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，请使用 list_directory 工具", absPath)
	}

	maxLines := ReadFileMaxLines
	explicitLines := false
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
		explicitLines = true
	}

	startLine := 1
	explicitStart := false
	if n, ok := args["start_line"].(float64); ok && n > 1 {
		startLine = int(n)
		explicitStart = true
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取失败: %s", err.Error())
	}

	lines := strings.SplitAfter(string(data), "\n")
	totalLines := len(lines)

	// 自适应策略：LLM 未指定 start_line/lines 时，根据文件大小自动决定返回内容
	if !explicitLines && !explicitStart && totalLines > ReadFileMaxLines {
		return BuildAdaptiveReadResult(lines, totalLines, absPath)
	}

	// 精准读取模式
	startIdx := startLine - 1
	if startIdx >= totalLines {
		return fmt.Sprintf("start_line=%d 超出文件总行数 %d", startLine, totalLines)
	}
	if startIdx < 0 {
		startIdx = 0
	}
	remaining := lines[startIdx:]

	if len(remaining) > maxLines {
		chunk := remaining[:maxLines]
		endLine := startLine + maxLines - 1
		nextStart := endLine + 1
		return strings.Join(chunk, "") + fmt.Sprintf(
			"\n... (共 %d 行，显示第 %d-%d 行。下一段: start_line=%d)",
			totalLines, startLine, endLine, nextStart)
	}

	if startIdx > 0 {
		return fmt.Sprintf("(第 %d-%d 行，共 %d 行)\n%s", startLine, totalLines, totalLines, strings.Join(remaining, ""))
	}
	return string(data)
}

// BuildAdaptiveReadResult 根据文件类型和大小自动决定返回内容。
// 返回结构摘要（标题/函数签名/关键行）+ 预览，LLM 再用 start_line 精准读取。
func BuildAdaptiveReadResult(lines []string, totalLines int, absPath string) string {
	ext := strings.ToLower(filepath.Ext(absPath))
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("[文件概览] %s（共 %d 行）\n\n", filepath.Base(absPath), totalLines))

	outline := ExtractStructureOutline(lines, totalLines, ext)
	if outline != "" {
		buf.WriteString("=== 结构摘要 ===\n")
		buf.WriteString(outline)
		buf.WriteString("\n")
	}

	previewLines := 80
	if totalLines > 1000 {
		previewLines = 50
	}
	buf.WriteString(fmt.Sprintf("=== 前 %d 行预览 ===\n", previewLines))
	if previewLines > totalLines {
		previewLines = totalLines
	}
	preview := lines[:previewLines]
	buf.WriteString(strings.Join(preview, ""))

	buf.WriteString(fmt.Sprintf("\n\n... (预览结束。如需查看特定段落，请使用 start_line 参数精准读取，如 start_line=%d)", previewLines+1))
	return buf.String()
}

// ExtractStructureOutline 从文件内容中提取结构性行（标题、函数签名等）。
func ExtractStructureOutline(lines []string, totalLines int, ext string) string {
	var result []string

	isCode := IsCodeFileExt(ext)
	isMarkdown := ext == ".md" || ext == ".markdown" || ext == ".mdx"

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lineNum := i + 1
		keep := false

		if isMarkdown {
			if strings.HasPrefix(trimmed, "#") {
				keep = true
			}
		} else if isCode {
			keep = IsCodeStructureLine(trimmed, ext)
		} else {
			keep = IsTextStructureLine(trimmed, i, lines)
		}

		if keep {
			display := trimmed
			if len([]rune(display)) > 120 {
				display = string([]rune(display)[:117]) + "..."
			}
			result = append(result, fmt.Sprintf("  L%-4d %s", lineNum, display))
		}
	}

	maxOutlineLines := 60
	if len(result) > maxOutlineLines {
		result = append(result[:maxOutlineLines], fmt.Sprintf("  ... (共 %d 个结构行，已截断)", len(result)))
	}

	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, "\n") + "\n"
}

// IsCodeFileExt returns true for known source code file extensions.
func IsCodeFileExt(ext string) bool {
	switch ext {
	case ".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".c", ".cpp", ".h", ".hpp",
		".cs", ".rb", ".rs", ".swift", ".kt", ".scala", ".php", ".lua", ".sh", ".bash",
		".pl", ".r", ".m", ".mm", ".zig", ".v", ".dart", ".ex", ".exs", ".clj", ".hs",
		".vue", ".svelte":
		return true
	}
	return false
}

// IsCodeStructureLine detects function/class/struct definition lines.
func IsCodeStructureLine(trimmed string, ext string) bool {
	switch ext {
	case ".go":
		return strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") ||
			strings.HasPrefix(trimmed, "package ") || strings.HasPrefix(trimmed, "import ")
	case ".py":
		return strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ")
	case ".js", ".ts", ".jsx", ".tsx", ".vue", ".svelte":
		return strings.HasPrefix(trimmed, "function ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "const ") || strings.HasPrefix(trimmed, "interface ")
	case ".java", ".cs", ".kt", ".scala":
		return strings.HasPrefix(trimmed, "public ") || strings.HasPrefix(trimmed, "private ") ||
			strings.HasPrefix(trimmed, "protected ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "interface ") || strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "package ") || strings.HasPrefix(trimmed, "fun ")
	case ".c", ".cpp", ".h", ".hpp", ".rs":
		return strings.HasPrefix(trimmed, "#include") || strings.HasPrefix(trimmed, "#define") ||
			strings.HasPrefix(trimmed, "struct ") || strings.HasPrefix(trimmed, "enum ") ||
			strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "impl ") ||
			strings.HasPrefix(trimmed, "pub ") || strings.HasPrefix(trimmed, "mod ") ||
			strings.HasPrefix(trimmed, "use ") || strings.HasPrefix(trimmed, "typedef ")
	case ".rb":
		return strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "module ") || strings.HasPrefix(trimmed, "require ")
	case ".php":
		return strings.HasPrefix(trimmed, "function ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "namespace ") || strings.HasPrefix(trimmed, "use ") ||
			strings.HasPrefix(trimmed, "public ") || strings.HasPrefix(trimmed, "private ")
	case ".sh", ".bash":
		return strings.Contains(trimmed, "()") && strings.HasSuffix(trimmed, "{")
	}
	return false
}

// IsTextStructureLine detects heading-like lines in plain text files.
func IsTextStructureLine(trimmed string, idx int, lines []string) bool {
	if len(trimmed) > 3 && len(trimmed) < 80 && strings.ToUpper(trimmed) == trimmed && !strings.ContainsAny(trimmed, "{}()[]<>|") {
		return true
	}
	if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		prefix := trimmed[:min(5, len(trimmed))]
		if strings.Contains(prefix, ".") || strings.Contains(prefix, "、") {
			return true
		}
	}
	if idx+1 < len(lines) {
		nextTrimmed := strings.TrimSpace(lines[idx+1])
		if len(nextTrimmed) >= 3 && (allSameCharCorelib(nextTrimmed, '=') || allSameCharCorelib(nextTrimmed, '-')) {
			return true
		}
	}
	return false
}

func allSameCharCorelib(s string, ch byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ch {
			return false
		}
	}
	return true
}

// --- Write File ---

// ToolWriteFile writes content to a file.
func ToolWriteFile(args map[string]interface{}) string {
	p := StringArg(args, "path")
	content, ok := args["content"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	if !ok {
		return "缺少 content 参数"
	}
	if len(content) > WriteFileMaxSize {
		return fmt.Sprintf("内容过大（%d 字节），最大允许 %d 字节", len(content), WriteFileMaxSize)
	}
	mode := StringArg(args, "mode")

	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}
	size, err := tool.WriteTextFile(absPath, content, mode)
	if err != nil {
		if strings.Contains(err.Error(), "不支持的 mode") {
			return err.Error()
		}
		return fmt.Sprintf("写入失败: %s", err.Error())
	}
	resolvedMode, _ := tool.NormalizeWriteMode(mode)
	if resolvedMode == "append" {
		return fmt.Sprintf("已追加到 %s（当前 %d 字节）", absPath, size)
	}
	if content == "" {
		return fmt.Sprintf("已清空 %s（%d 字节）", absPath, size)
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", absPath, size)
}

// --- Edit File ---

// ToolEditFile edits a file by replacing text.
func ToolEditFile(args map[string]interface{}) string {
	p := StringArg(args, "path")
	oldString, okOld := args["old_string"].(string)
	newString, okNew := args["new_string"].(string)
	if p == "" || !okOld || !okNew {
		return "缺少 path、old_string 或 new_string 参数"
	}
	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}
	replaceAll, _ := args["replace_all"].(bool)
	res, err := tool.EditTextFile(absPath, oldString, newString, replaceAll)
	if err != nil {
		if strings.Contains(err.Error(), "未找到要替换的内容") || strings.Contains(err.Error(), "缺少 old_string 参数") {
			return err.Error()
		}
		return fmt.Sprintf("编辑失败: %s", err.Error())
	}
	return fmt.Sprintf("已编辑 %s（替换 %d 处，当前 %d 字节）", res.Path, res.Count, res.Size)
}

// --- List Directory ---

// ToolListDirectory lists the contents of a directory.
func ToolListDirectory(args map[string]interface{}) string {
	p := StringArg(args, "path")
	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s 不是目录", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Sprintf("读取目录失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("目录: %s（共 %d 项）\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... 还有 %d 项未显示\n", len(entries)-shown))
			break
		}
		info, _ := entry.Info()
		if entry.IsDir() {
			b.WriteString(fmt.Sprintf("  📁 %s/\n", entry.Name()))
		} else if info != nil {
			b.WriteString(fmt.Sprintf("  📄 %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  📄 %s\n", entry.Name()))
		}
		shown++
	}
	return b.String()
}

// --- Send File ---

// ToolSendFile reads a file and returns it as a base64-encoded payload.
func ToolSendFile(args map[string]interface{}) string {
	p := StringArg(args, "path")
	if p == "" {
		return "缺少 path 参数"
	}
	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，不能作为文件发送", absPath)
	}
	if info.Size() > SendFileMaxSize {
		return fmt.Sprintf("文件过大（%d 字节），最大允许 %d 字节", info.Size(), SendFileMaxSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %s", err.Error())
	}

	fileName := StringArg(args, "file_name")
	if fileName == "" {
		fileName = filepath.Base(absPath)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	forwardIM, _ := args["forward_to_im"].(bool)
	if forwardIM {
		return fmt.Sprintf("[file_base64|%s|%s|im]%s", fileName, mimeType, b64)
	}
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

// --- Open ---

// ToolOpen opens a file or URL with the system default handler.
func ToolOpen(args map[string]interface{}) string {
	target := StringArg(args, "target")
	if target == "" {
		return "缺少 target 参数"
	}

	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = ResolvePath(target)
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("打开失败: %s", err.Error())
	}
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("已用默认浏览器打开: %s", target)
	}
	return fmt.Sprintf("已用默认程序打开: %s", target)
}

// --- Helpers ---

// StringArg extracts a string value from an args map.
func StringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

// ResolvePath resolves a path relative to the default workspace.
func ResolvePath(p string) string {
	if p == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".maclaw", "workspace")
	}
	if filepath.IsAbs(p) {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maclaw", "workspace", p)
}

// ResolveFileToolPath resolves a file path for tool operations.
func ResolveFileToolPath(path string) (string, error) {
	return tool.ResolveFileToolPath(path, func() string {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return ""
	})
}

// ResolveBashTimeout determines the timeout for a bash command.
func ResolveBashTimeout(args map[string]interface{}, command string) int {
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout := int(t)
		if timeout > 120 {
			timeout = 120
		}
		return timeout
	}
	return 30
}

// HideCommandWindow is a platform-specific function to hide the console
// window on Windows. No-op on other platforms.
func HideCommandWindow(cmd *exec.Cmd) {
	hideCommandWindowImpl(cmd)
}
