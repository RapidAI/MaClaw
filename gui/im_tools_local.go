package main

// Local tools: bash, read_file, write_file, list_directory, send_file, send_to_im, open.
// These operate directly on the host machine without a coding session.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/imgconv"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) toolBash(execCtx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "bash failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	command, _ := args["command"].(string)
	if command == "" {
		return "缺少 command 参数"
	}
	background, _ := args["background"].(bool)

	if rejection, rejected := coretool.RejectRawSSHCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := coretool.RejectBroadBrowserKillCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := coretool.RejectShellBrowserAutomationCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := coretool.RejectBrowserSideEffectHTTPCommand(command); rejected {
		return rejection
	}

	// --- Auto-spill: if command is oversized, write it to a temp script and
	// execute that file instead. This is transparent to the LLM.
	var autoSpillTempFile string
	if len([]rune(command)) > maxAgentLoopInlineBashCommandRunes {
		workDir := h.resolveToolWorkDirForOwner(stringVal(args, "working_dir"), ownerID)
		var result *autoSpillResult
		var err error
		if bashCommandIsAutoSpillable(command) {
			result, err = autoSpillPythonScript(command, workDir)
		} else {
			result, err = autoSpillShellScript(command, workDir)
		}
		if err == nil {
			log.Printf("[bash-auto-spill] rewrote oversized bash command (%d runes) to temp script: %s", len([]rune(command)), result.Command)
			autoSpillTempFile = result.TempFile
			command = result.Command
			if result.TempFile != "" {
				if background {
					// Background tasks run asynchronously — append shell cleanup so the temp
					// file is removed after python finishes, regardless of exit code.
					cleanupCmd := shellRemoveCommand(result.TempFile)
					command = command + "; " + cleanupCmd
				} else {
					// Synchronous execution — defer cleanup after toolBash returns.
					defer os.Remove(result.TempFile)
				}
			}
		} else {
			log.Printf("[bash-auto-spill] failed to spill oversized command: %v", err)
			return fmt.Sprintf("[error] bash command is too large and could not be written to a temporary script: %v", err)
		}
	}

	// --- Background mode: submit to LocalBackgroundTaskManager ---
	if background {
		return h.toolBashBackgroundForOwner(command, stringVal(args, "working_dir"), stringVal(args, "task_role"), ownerID)
	}

	timeout := resolveBashTimeout(args, command)

	workDir := h.resolveToolWorkDirForOwner(stringVal(args, "working_dir"), ownerID)

	ctx, cancel := context.WithTimeout(execCtx, time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		psPath, err := coretool.ResolveWindowsPowerShell()
		if err != nil {
			return fmt.Sprintf("[错误] 无法找到 PowerShell: %v。请确认系统 PATH 包含 PowerShell 路径，或安装 PowerShell 7 (https://aka.ms/powershell)", err)
		}
		shellName = psPath
		// Prepend UTF-8 OutputEncoding to prevent GBK mojibake on Chinese Windows.
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.Command(shellName, shellArgs...)
	cmd.Dir = workDir
	// Force UTF-8 encoding for subprocess I/O on Windows to prevent
	// GBK/CP936 mojibake when commands output non-ASCII text.
	cmd.Env = coretool.AppendNoWindowEnv(coretool.AppendUTF8Env(os.Environ()))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)
	coretool.PrepareCommandForTreeKill(cmd)

	// Early exit if already cancelled — avoid spawning a process just to kill it.
	if execCtx.Err() != nil {
		return "[取消] 命令已被用户取消"
	}

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
					onProgress(fmt.Sprintf("命令仍在执行中（已 %ds）: %s", elapsed, displayCmd))
				}
			case <-done:
				return
			}
		}
	}()

	err = coretool.WaitCommandWithContext(ctx, cmd)
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
		} else if execCtx.Err() == context.Canceled {
			b.WriteString("\n[取消] 命令已被用户取消并终止")
		} else {
			b.WriteString(fmt.Sprintf("\n[错误] 退出码: %v", err))
		}
	}

	if b.Len() == 0 {
		return "(命令执行完成，无输出)"
	}
	output := b.String()
	// Scrub auto-spill temp file path from output (e.g. Python tracebacks)
	// to prevent LLM from trying to read/edit the already-deleted temp file.
	if autoSpillTempFile != "" {
		output = strings.ReplaceAll(output, autoSpillTempFile, "<inline-script>")
		output = strings.ReplaceAll(output, filepath.ToSlash(autoSpillTempFile), "<inline-script>")
	}
	return output
}

func resolveFileToolPath(path string) (string, error) {
	return resolveFileToolPathWithBase(path, func() string {
		// Use EffectiveWorkspaceDir as fallback, NOT os.Getwd() which is the
		// maclaw installation directory on Windows. CodingSubAgent tools always
		// pass absolute paths (via withProjectRelativePath wrapper), but this
		// fallback protects against regressions if the wrapper is omitted.
		return corelib.EffectiveWorkspaceDir()
	})
}

func resolveFileToolPathWithBase(path string, baseResolver coretool.PathResolver) (string, error) {
	return coretool.ResolveFileToolPath(path, baseResolver)
}

func (h *IMMessageHandler) resolveFileToolPathForOwner(path, ownerID string) (string, error) {
	return resolveFileToolPathWithBase(path, func() string {
		if dir := h.projectTabWorkDirForOwner(ownerID); dir != "" {
			return dir
		}
		if h == nil || h.app == nil {
			if wd, err := os.Getwd(); err == nil {
				return wd
			}
		}
		// Fall back to EffectiveWorkspaceDir, NOT os.Getwd() which is the
		// maclaw installation directory on Windows.
		return corelib.EffectiveWorkspaceDir()
	})
}

// projectPathFromUserID extracts the projectPath from a synthesized Project Tab
// userID (format: "desktop-user:{projectPath}"). Returns empty string if the
// userID is not a Project Tab userID.
func projectPathFromUserID(userID string) string {
	return projectPathFromSessionOwnerID(userID)
}

func (h *IMMessageHandler) executionProjectPathForOwner(ownerID string) string {
	// Only project-tab owners have an execution project path. Local sessions
	// return "" here so callers do not confuse desktop workspace with a project.
	if projectPathFromUserID(ownerID) == "" {
		return ""
	}
	if h != nil && h.app != nil {
		return h.app.EffectiveWorkingDirForOwner(ownerID)
	}
	return projectPathFromUserID(ownerID)
}

// projectTabWorkDir returns the execution working directory for the current
// Project Tab session owner (task workspace/ or bound project path).
// Returns empty string if not in a Project Tab context (local desktop tab).
func (h *IMMessageHandler) projectTabWorkDir() string {
	return h.projectTabWorkDirForOwner(h.currentRuntimeOrLegacyPolicyOwnerID())
}

func (h *IMMessageHandler) projectTabWorkDirForOwner(ownerID string) string {
	rawProjectPath := projectPathFromUserID(ownerID)
	if rawProjectPath == "" {
		return ""
	}
	if h == nil {
		return rawProjectPath
	}

	// Managed tasks (…/data/tasks/<slug>-<id>): tools must run in workspace/ (or
	// a custom working_dir tag), never the metadata root that only holds task.md.
	if h.app != nil && h.app.isManagedRecentTaskWorkspacePath(rawProjectPath) {
		if info, err := os.Stat(rawProjectPath); err != nil || !info.IsDir() {
			_ = h.ensureRecentTaskWorkspaceForProjectPath(rawProjectPath)
		}
		exec := h.executionProjectPathForOwner(ownerID)
		if exec == "" {
			exec = filepath.Join(rawProjectPath, "workspace")
		}
		if info, err := os.Stat(exec); err == nil && info.IsDir() {
			return exec
		}
		// Create workspace/ (and task.md) then re-check.
		_ = h.ensureRecentTaskWorkspaceForProjectPath(rawProjectPath)
		if info, err := os.Stat(exec); err == nil && info.IsDir() {
			return exec
		}
		log.Printf("[projectTabWorkDir] managed task exec path missing owner=%q task=%q exec=%q", ownerID, rawProjectPath, exec)
		return exec
	}

	// Non-managed project tab: prefer execution path, else raw project path.
	projectPath := h.executionProjectPathForOwner(ownerID)
	if projectPath == "" {
		projectPath = rawProjectPath
	}
	if info, err := os.Stat(projectPath); err == nil && info.IsDir() {
		return projectPath
	}
	// Project-tab owners are isolation boundaries. Never fall back to home or
	// the default workspace when the bound project path is invalid.
	log.Printf("[projectTabWorkDir] invalid project path %q for owner=%q; using missing path so tool fails closed", projectPath, ownerID)
	return projectPath
}

func (h *IMMessageHandler) ensureRecentTaskWorkspaceForProjectPath(projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || !filepath.IsAbs(projectPath) {
		return false
	}
	if h == nil || h.app == nil || !h.app.isManagedRecentTaskWorkspacePath(projectPath) {
		return false
	}
	// If caller passed workspace/ subdir, repair the task root (parent).
	taskRoot := projectPath
	if base := filepath.Base(projectPath); strings.EqualFold(base, "workspace") {
		if parent := filepath.Dir(projectPath); h.app.isManagedRecentTaskWorkspacePath(parent) {
			taskRoot = parent
		}
	}
	if err := os.MkdirAll(taskRoot, 0o755); err != nil {
		log.Printf("[projectTabWorkDir] repair managed task workspace failed path=%q err=%v", taskRoot, err)
		return false
	}
	if err := ensureManagedTaskWorkspaceDir(taskRoot, lastPathComponent(taskRoot), ""); err != nil {
		log.Printf("[projectTabWorkDir] prepare workspace/ failed path=%q err=%v", taskRoot, err)
	}
	taskFile := filepath.Join(taskRoot, "task.md")
	if _, err := os.Stat(taskFile); os.IsNotExist(err) {
		content := fmt.Sprintf("# %s\n\nRecovered task workspace.\nProject path: %s\n", lastPathComponent(taskRoot), taskRoot)
		if writeErr := os.WriteFile(taskFile, []byte(content), 0o644); writeErr != nil {
			log.Printf("[projectTabWorkDir] repair managed task task.md failed path=%q err=%v", taskFile, writeErr)
		}
	}
	log.Printf("[projectTabWorkDir] repaired managed task workspace path=%q", taskRoot)
	return true
}

// resolveToolWorkDir resolves the working directory for tool execution.
// When an explicit working_dir is provided, it is resolved normally.
// When empty, it checks if the current session is a Project Tab and uses
// the bound projectPath as the working directory. Falls back to the default
// workspace directory (~/.maclaw/workspace) if not in a Project Tab context.
func (h *IMMessageHandler) resolveToolWorkDir(workingDir string) string {
	return h.resolveToolWorkDirForOwner(workingDir, h.currentRuntimeOrLegacyPolicyOwnerID())
}

func (h *IMMessageHandler) resolveToolWorkDirForOwner(workingDir, ownerID string) string {
	if workingDir != "" {
		return resolvePath(workingDir)
	}
	// Project Tab: use projectPath as default working directory.
	if dir := h.projectTabWorkDirForOwner(ownerID); dir != "" {
		return dir
	}
	// Default: ~/.maclaw/workspace (same as resolvePath(""))
	return resolvePath("")
}

func (h *IMMessageHandler) toolReadToolResult(args map[string]interface{}) string {
	return agent.ToolReadToolResult(args)
}

func (h *IMMessageHandler) toolReadFile(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "read_file failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
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

	maxLines := readFileMaxLines
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

	// offset 参数：从文件末尾倒数 N 行开始读取（类似 tail -n）
	// 与 lines/start_line 互斥，优先使用 offset
	if offset, ok := args["offset"].(float64); ok && offset > 0 {
		tailN := int(offset)
		if tailN >= totalLines {
			return string(data)
		}
		startIdx := totalLines - tailN
		tailContent := strings.Join(lines[startIdx:], "")
		return fmt.Sprintf("... (跳过前 %d 行，显示最后 %d 行，共 %d 行)\n%s", startIdx, tailN, totalLines, tailContent)
	}

	// 自适应策略：LLM 未指定 start_line/lines 时，根据文件大小自动决定返回内容
	// 小文件全量返回；大文件返回结构摘要+预览，LLM 再按需精准读取
	if !explicitLines && !explicitStart && totalLines > readFileMaxLines {
		return buildAdaptiveReadResult(lines, totalLines, absPath)
	}

	// 精准读取模式：LLM 显式指定了 start_line 或 lines
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
		// Add line numbers when using precise read mode (start_line specified).
		// This helps the LLM use edit_lines with correct line numbers.
		if explicitStart {
			var numbered strings.Builder
			for i, line := range chunk {
				numbered.WriteString(fmt.Sprintf("%4d | %s", startLine+i, line))
			}
			return numbered.String() + fmt.Sprintf(
				"\n... (共 %d 行，显示第 %d-%d 行。下一段: start_line=%d)",
				totalLines, startLine, endLine, nextStart)
		}
		return strings.Join(chunk, "") + fmt.Sprintf(
			"\n... (共 %d 行，显示第 %d-%d 行。下一段: start_line=%d)",
			totalLines, startLine, endLine, nextStart)
	}

	if startIdx > 0 {
		// Add line numbers for precise reads.
		if explicitStart {
			var numbered strings.Builder
			for i, line := range remaining {
				numbered.WriteString(fmt.Sprintf("%4d | %s", startLine+i, line))
			}
			return fmt.Sprintf("(第 %d-%d 行，共 %d 行)\n%s", startLine, totalLines, totalLines, numbered.String())
		}
		return fmt.Sprintf("(第 %d-%d 行，共 %d 行)\n%s", startLine, totalLines, totalLines, strings.Join(remaining, ""))
	}
	return string(data)
}

func (h *IMMessageHandler) toolWriteFile(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "write_file failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	content, ok := args["content"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	if !ok {
		return "缺少 content 参数"
	}
	if len(content) > writeFileMaxSize {
		return fmt.Sprintf("内容过大（%d 字节），最大允许 %d 字节", len(content), writeFileMaxSize)
	}
	mode := stringVal(args, "mode")
	p = workflowDocWritePath(p, args)

	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
	if err != nil {
		return err.Error()
	}
	created := !localToolFileExists(absPath)
	original, hasOriginal := localToolCodePreviewOriginal(absPath)
	size, err := coretool.WriteTextFile(absPath, content, mode)
	if err != nil {
		if classifyLocalFileToolError(err).ReturnRawError() {
			return err.Error()
		}
		return fmt.Sprintf("写入失败: %s", err.Error())
	}
	h.emitLocalToolCodeFilePreview(ownerID, absPath, created, original, hasOriginal)
	// Diagnostic: detect role-prefix hallucination in write_file content.
	// This is the path where Browser: hallucinations in tool call arguments
	// bypass all streaming filters and reach disk (and potentially the
	// workflow doc preview panel). Log for traceability.
	if browserDiagHasBrowserRolePrefix(content) {
		log.Printf("[browser-diag] CP_WriteFile: Browser: prefix detected in write_file content | path=%s content_len=%d", absPath, len(content))
	}
	resolvedMode, _ := coretool.NormalizeWriteModeKind(mode)
	if resolvedMode == coretool.WriteModeAppend {
		return fmt.Sprintf("已追加到 %s（当前 %d 字节）", absPath, size)
	}
	if content == "" {
		return fmt.Sprintf("已清空 %s（%d 字节）", absPath, size)
	}

	// Auto-convert SVG to PNG when an SVG file is written (overwrite mode only;
	// append to SVG mid-content would produce invalid SVG).
	result := fmt.Sprintf("已写入 %s（%d 字节）", absPath, size)
	if strings.HasSuffix(strings.ToLower(absPath), ".svg") && resolvedMode != coretool.WriteModeAppend {
		pngPath := strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".png"
		if convErr := imgconv.ConvertSVGToPNG(absPath, pngPath, 2000); convErr != nil {
			result += fmt.Sprintf("\nSVG→PNG 自动转换失败: %s", convErr.Error())
		} else {
			pngInfo, _ := os.Stat(pngPath)
			pngSize := int64(0)
			if pngInfo != nil {
				pngSize = pngInfo.Size()
			}
			result += fmt.Sprintf("\n已自动转换为 PNG: %s（%d 字节）", pngPath, pngSize)
		}
	}
	return result
}

func (h *IMMessageHandler) toolEditFile(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "edit_file failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	oldString, okOld := args["old_string"].(string)
	newString, okNew := args["new_string"].(string)
	if p == "" || !okOld || !okNew {
		return "缺少 path、old_string 或 new_string 参数"
	}
	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
	if err != nil {
		return err.Error()
	}
	original, hasOriginal := localToolCodePreviewOriginal(absPath)
	replaceAll, _ := args["replace_all"].(bool)
	res, err := coretool.EditTextFile(absPath, oldString, newString, replaceAll)
	if err != nil {
		if classifyLocalFileToolError(err).ReturnRawError() {
			return err.Error()
		}
		return fmt.Sprintf("编辑失败: %s", err.Error())
	}
	h.emitLocalToolCodeFilePreview(ownerID, absPath, false, original, hasOriginal)
	return fmt.Sprintf("已编辑 %s（替换 %d 处，当前 %d 字节）", res.Path, res.Count, res.Size)
}

func (h *IMMessageHandler) toolEditLines(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "edit_lines failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
	if err != nil {
		return err.Error()
	}
	original, hasOriginal := localToolCodePreviewOriginal(absPath)
	opStr, _ := args["operation"].(string)
	if opStr == "" {
		return "缺少 operation 参数（replace/insert/delete）"
	}
	op := coretool.EditLineOperation(opStr)

	startLine := 0
	if v, ok := args["start_line"].(float64); ok {
		startLine = int(v)
	} else if v, ok := args["start_line"].(int); ok {
		startLine = v
	}

	endLine := startLine
	if v, ok := args["end_line"].(float64); ok {
		endLine = int(v)
	} else if v, ok := args["end_line"].(int); ok {
		endLine = v
	}

	content, _ := args["content"].(string)

	res, err := coretool.EditFileByLine(absPath, op, startLine, endLine, content)
	if err != nil {
		return fmt.Sprintf("行编辑失败: %s", err.Error())
	}
	h.emitLocalToolCodeFilePreview(ownerID, absPath, false, original, hasOriginal)

	// Return a few lines around the edit point so the LLM can verify.
	contextPreview := buildEditLineContext(absPath, startLine, res.TotalLines)

	return fmt.Sprintf("已编辑 %s（%s %d 行，当前共 %d 行，%d 字节）\n%s",
		res.Path, opStr, res.LinesChanged, res.TotalLines, res.Size, contextPreview)
}

func (h *IMMessageHandler) emitLocalToolCodeFilePreview(ownerID, absPath string, created bool, original string, hasOriginal bool) {
	if h == nil || h.app == nil || h.app.codeEventEmitter == nil || strings.TrimSpace(absPath) == "" {
		return
	}
	projectPath := h.executionProjectPathForOwner(ownerID)
	sessionID := localToolCodePreviewSessionID(ownerID)
	// Route with the tab/session project path when owner is a project session.
	routePath := codePreviewRouteProjectPath(ownerID, projectPath)
	if hasOriginal {
		emitCodeFilePreviewForPath(h.app, sessionID, projectPath, routePath, absPath, created, true, original)
		return
	}
	emitCodeFilePreviewForPath(h.app, sessionID, projectPath, routePath, absPath, created, true)
}

func localToolCodePreviewSessionID(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return "local-tools"
	}
	return "local-tools:" + ownerID
}

func localToolFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func localToolCodePreviewOriginal(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxCodeFileSize {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || !isCodePreviewTextContent(data) {
		return "", false
	}
	return string(data), true
}

// buildEditLineContext reads a few lines around the edit point from the file
// and formats them with line numbers, so the LLM can verify the edit.
func buildEditLineContext(path string, editLine, totalLines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	// Show 3 lines before and after the edit point.
	start := editLine - 3
	if start < 1 {
		start = 1
	}
	end := editLine + 3
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("编辑区域预览（第 %d-%d 行）:\n", start, end))
	for i := start; i <= end && i <= len(lines); i++ {
		prefix := "  "
		if i == editLine {
			prefix = "→ "
		}
		b.WriteString(fmt.Sprintf("%s%4d | %s\n", prefix, i, lines[i-1]))
	}
	return b.String()
}

// buildAdaptiveReadResult 根据文件类型和大小自动决定返回内容。
// 小文件（≤200行）由调用方全量返回，此函数只处理大文件（>200行）。
// 策略：返回结构摘要（标题/函数签名/关键行）+ 预览，LLM 再用 start_line 精准读取。
func buildAdaptiveReadResult(lines []string, totalLines int, absPath string) string {
	return agent.BuildAdaptiveReadResult(lines, totalLines, absPath)
}

func (h *IMMessageHandler) toolListDirectory(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "list_directory failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
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
			b.WriteString(fmt.Sprintf("  %s/\n", entry.Name()))
		} else if info != nil {
			b.WriteString(fmt.Sprintf("  %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", entry.Name()))
		}
		shown++
	}
	return b.String()
}

const sendFileMaxSize = 200 << 20 // 200 MB — large files are handled by plugin-level fallback (temp URL)

func (h *IMMessageHandler) toolSendFile(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "file delivery failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath, err := h.resolveFileToolPathForOwner(p, ownerID)
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
	fileName = workflowDocDeliveryFileNameWithFallbackExt(fileName, args, filepath.Ext(absPath))

	// Prefer display-name extension, then source path, then content sniff.
	// Screenshots often arrive as application/octet-stream without a reliable ext.
	mimeType := mime.TypeByExtension(filepath.Ext(fileName))
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(absPath))
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		if sniffed := http.DetectContentType(data); sniffed != "" && sniffed != "application/octet-stream" {
			// DetectContentType may append "; charset=utf-8" — keep the media type only.
			if semi := strings.IndexByte(sniffed, ';'); semi > 0 {
				sniffed = strings.TrimSpace(sniffed[:semi])
			}
			mimeType = sniffed
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	// Structured control only: forward_to_im and/or destination enum.
	// No free-text keyword guessing on the user message.
	forwardIM := applySendFileForwardArgs(args)
	if forwardIM {
		if msgFlag := workflowDocDeliveryMessagePayloadFlag(args); msgFlag != "" {
			return fmt.Sprintf("[file_base64|%s|%s|im|%s]%s", fileName, mimeType, msgFlag, b64)
		}
		// Use | as delimiter; append |im flag so the interceptor knows to forward.
		return fmt.Sprintf("[file_base64|%s|%s|im]%s", fileName, mimeType, b64)
	}
	// Use | as delimiter to avoid conflicts with : in filenames or MIME types.
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

// toolSendToIM always delivers via IM (WeChat/Feishu/etc). Prefer this over
// send_file when the user asked to push a file off the desktop.
func (h *IMMessageHandler) toolSendToIM(args map[string]interface{}) string {
	if args == nil {
		args = map[string]interface{}{}
	}
	forceSendFileToIMArgs(args)
	return h.toolSendFile(args)
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
