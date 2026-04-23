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
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取失败: %s", err.Error())
	}

	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		return strings.Join(lines, "") + fmt.Sprintf("\n... (已截断，共 %d 行，显示前 %d 行)", len(strings.SplitAfter(string(data), "\n")), maxLines)
	}
	return string(data)
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
