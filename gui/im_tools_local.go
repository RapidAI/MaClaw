package main

// Local tools: bash, read_file, write_file, list_directory, send_file, open.
// These operate directly on the host machine without a coding session.

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
)

func (h *IMMessageHandler) toolBash(args map[string]interface{}, onProgress ProgressCallback) string {
	command, _ := args["command"].(string)
	if command == "" {
		return "缺少 command 参数"
	}

	timeout := bashDefaultTimeout
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > bashMaxTimeout {
			timeout = bashMaxTimeout
		}
	}

	workDir := resolvePath(stringVal(args, "working_dir"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

	// Start the command and send periodic heartbeats for long-running ops.
	err := cmd.Start()
	if err != nil {
		return fmt.Sprintf("[错误] 命令启动失败: %v", err)
	}

	// Heartbeat goroutine: send progress every 30s while the command runs.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		elapsed := 0
		for {
			select {
			case <-ticker.C:
				elapsed += 30
				// Truncate command for display.
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

func (h *IMMessageHandler) toolReadFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，请使用 list_directory 工具", absPath)
	}

	maxLines := readFileMaxLines
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

func (h *IMMessageHandler) toolWriteFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if p == "" || content == "" {
		return "缺少 path 或 content 参数"
	}
	if len(content) > writeFileMaxSize {
		return fmt.Sprintf("内容过大（%d 字节），最大允许 %d 字节", len(content), writeFileMaxSize)
	}

	absPath := resolvePath(p)

	// 自动创建父目录
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("创建目录失败: %s", err.Error())
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Sprintf("写入失败: %s", err.Error())
	}

	// 验证写入
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("写入后验证失败: %s", err.Error())
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", absPath, info.Size())
}

func (h *IMMessageHandler) toolListDirectory(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	absPath := resolvePath(p)

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

const sendFileMaxSize = 200 << 20 // 200 MB — large files are handled by plugin-level fallback (temp URL)

func (h *IMMessageHandler) toolSendFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，不能作为文件发送", absPath)
	}
	if info.Size() > sendFileMaxSize {
		return fmt.Sprintf("文件过大（%d 字节），最大允许 %d 字节", info.Size(), sendFileMaxSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %s", err.Error())
	}

	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		fileName = filepath.Base(absPath)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	// Check if the caller wants to forward the file to IM channels.
	forwardIM, _ := args["forward_to_im"].(bool)
	if forwardIM {
		// Use | as delimiter; append |im flag so the interceptor knows to forward.
		return fmt.Sprintf("[file_base64|%s|%s|im]%s", fileName, mimeType, b64)
	}
	// Use | as delimiter to avoid conflicts with : in filenames or MIME types.
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

func (h *IMMessageHandler) toolOpen(args map[string]interface{}) string {
	target, _ := args["target"].(string)
	if target == "" {
		return "缺少 target 参数"
	}

	// Detect URLs (http, https, file, mailto, etc.)
	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = resolvePath(target)
		// Verify the path exists before attempting to open.
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use rundll32 url.dll,FileProtocolHandler — opens files/URLs with
		// the default handler without spawning a visible console window.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("打开失败: %s", err.Error())
	}

	// Don't wait for the process — it's a GUI application.
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("已用默认浏览器打开: %s", target)
	}
	return fmt.Sprintf("已用默认程序打开: %s", target)
}

// ---------------------------------------------------------------------------
// Memory Tools
// ---------------------------------------------------------------------------
