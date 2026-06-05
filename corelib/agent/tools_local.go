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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// --- Constants ---

const (
	ReadFileMaxLines  = 200
	WriteFileMaxSize  = 512 * 1024 // 512 KB per write
	SendFileMaxSize   = 200 << 20  // 200 MB
	MaxSearchFileSize = 2 << 20    // 2 MB per file for search tools
)

// --- Bash ---

// ToolBash executes a shell command on the host machine.
// onProgress is called every 30s with a heartbeat message for long-running commands.
func ToolBash(args map[string]interface{}, onProgress func(string)) string {
	command := StringArg(args, "command")
	if command == "" {
		return "缺少 command 参数"
	}

	if rejection, rejected := tool.RejectRawSSHCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectBroadBrowserKillCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectBrowserSideEffectHTTPCommand(command); rejected {
		return rejection
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

	cmd := exec.Command(shellName, shellArgs...)
	cmd.Dir = workDir
	cmd.Env = tool.AppendUTF8Env(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	HideCommandWindow(cmd)
	tool.PrepareCommandForTreeKill(cmd)

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

	err = tool.WaitCommandWithContext(ctx, cmd)
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

// ToolFileRead reads a precise line range from a text file.
func ToolFileRead(args map[string]interface{}) string {
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
		return fmt.Sprintf("%s 是目录，请使用 Glob 或 list_directory 工具", absPath)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取失败: %s", err.Error())
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if total == 0 {
		return fmt.Sprintf("%s 为空文件", absPath)
	}

	startLine := intArg(args, "start_line", 1)
	if startLine < 1 {
		startLine = 1
	}
	endLine := intArg(args, "end_line", 0)
	if endLine <= 0 {
		count := intArg(args, "lines", ReadFileMaxLines)
		if count <= 0 {
			count = ReadFileMaxLines
		}
		if count > 1000 {
			count = 1000
		}
		endLine = startLine + count - 1
	}
	if startLine > total {
		return fmt.Sprintf("start_line=%d 超出文件总行数 %d", startLine, total)
	}
	if endLine < startLine {
		return fmt.Sprintf("end_line=%d 不能小于 start_line=%d", endLine, startLine)
	}
	if endLine > total {
		endLine = total
	}

	showLineNumbers := boolArg(args, "show_line_numbers", true)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (lines %d-%d of %d)\n", absPath, startLine, endLine, total))
	for i := startLine; i <= endLine; i++ {
		line := lines[i-1]
		if showLineNumbers {
			b.WriteString(fmt.Sprintf("%6d\t%s", i, line))
		} else {
			b.WriteString(line)
		}
		if !strings.HasSuffix(line, "\n") {
			b.WriteString("\n")
		}
	}
	if endLine < total {
		b.WriteString(fmt.Sprintf("... next start_line=%d\n", endLine+1))
	}
	return b.String()
}

type SearchToolOutcome string

const (
	SearchToolOutcomeMatched SearchToolOutcome = "matched"
	SearchToolOutcomeNoMatch SearchToolOutcome = "no_match"
	SearchToolOutcomeError   SearchToolOutcome = "error"
)

type SearchToolResult struct {
	Text    string
	Outcome SearchToolOutcome
}

// ToolRipgrep searches text files recursively using Go regexp.
func ToolRipgrep(args map[string]interface{}) string {
	return ToolRipgrepDetailed(args).Text
}

func ToolRipgrepDetailed(args map[string]interface{}) SearchToolResult {
	totalStart := time.Now()
	pattern := StringArg(args, "pattern")
	if pattern == "" {
		return SearchToolResult{Text: "missing pattern parameter", Outcome: SearchToolOutcomeError}
	}
	caseSensitive := boolArg(args, "case_sensitive", false)
	fixedString := boolArg(args, "fixed_string", false)
	wholeWord := boolArg(args, "whole_word", false)
	lineRegexp := boolArg(args, "line_regexp", false)
	compilePattern := buildSearchRegexpPattern(pattern, fixedString, wholeWord, lineRegexp)
	if !caseSensitive {
		compilePattern = "(?i:" + compilePattern + ")"
	}
	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return SearchToolResult{Text: fmt.Sprintf("invalid regex: %s", err.Error()), Outcome: SearchToolOutcomeError}
	}

	base, err := resolveSearchBase(StringArg(args, "path"))
	if err != nil {
		return SearchToolResult{Text: err.Error(), Outcome: SearchToolOutcomeError}
	}
	globPattern := StringArg(args, "glob")
	excludePattern := resolveSearchExcludePattern(args, base)
	maxResults := intArg(args, "max_results", 100)
	if maxResults <= 0 {
		maxResults = 100
	}
	if maxResults > 1000 {
		maxResults = 1000
	}
	outputMode := StringArg(args, "output_mode")
	if outputMode == "" {
		outputMode = "content"
	}
	if !isValidSearchOutputMode(outputMode) {
		return SearchToolResult{Text: fmt.Sprintf("invalid output_mode %q: expected content, files_with_matches, or count", outputMode), Outcome: SearchToolOutcomeError}
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	outputLimit := maxResults + offset
	beforeContext, afterContext := resolveSearchContextArgs(args)
	includeStats := boolArg(args, "stats", false)
	fileType := StringArg(args, "type")
	includeHidden := boolArg(args, "include_hidden", false)

	matches := make([]string, 0, min(maxResults, 100))
	filesSearched := 0
	fileCounts := make(map[string]int)
	matchedFiles := make(map[string]bool)
	searchStats := searchCandidateStats{}
	appendStats := func(result string) string {
		searchStats.totalTime = time.Since(totalStart)
		return appendSearchStats(result, includeStats, searchStats, filesSearched)
	}
	matchedResult := func(result string) SearchToolResult {
		return SearchToolResult{Text: appendStats(result), Outcome: SearchToolOutcomeMatched}
	}
	noMatchResult := func() SearchToolResult {
		return SearchToolResult{Text: appendStats(fmt.Sprintf("no matches found (searched %d files)", filesSearched)), Outcome: SearchToolOutcomeNoMatch}
	}
	visit := func(path string, d fs.DirEntry) error {
		if searchOutputLimitReached(outputMode, outputLimit, matches, matchedFiles, fileCounts) {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSearchFileSize {
			return nil
		}
		if isLikelyBinary(path) {
			return nil
		}
		filesSearched++
		if err := searchFileLinesWithMode(path, re, outputLimit, outputMode, beforeContext, afterContext, &matches, matchedFiles, fileCounts); err != nil {
			return err
		}
		if searchOutputLimitReached(outputMode, outputLimit, matches, matchedFiles, fileCounts) {
			return filepath.SkipAll
		}
		return nil
	}
	candidateStart := time.Now()
	candidates, ok, stats := indexedSearchCandidates(base, pattern, globPattern, excludePattern, fileType, fixedString, includeHidden)
	stats.candidateTime = time.Since(candidateStart)
	searchStats = stats
	scanStart := time.Now()
	if ok {
		searchStats = stats
		for _, path := range candidates {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			visitErr := visit(path, fileInfoDirEntry{info: info})
			if visitErr == filepath.SkipAll {
				break
			}
			if visitErr != nil {
				err = visitErr
				break
			}
		}
	} else {
		searchStats = stats
		err = walkSearchFilesFiltered(base, globPattern, excludePattern, fileType, false, includeHidden, visit)
	}
	searchStats.scanTime = time.Since(scanStart)
	if err != nil && err != filepath.SkipAll {
		return SearchToolResult{Text: fmt.Sprintf("search failed: %s", err.Error()), Outcome: SearchToolOutcomeError}
	}

	switch outputMode {
	case "files_with_matches":
		files := make([]string, 0, len(matchedFiles))
		for file := range matchedFiles {
			files = append(files, file)
		}
		sort.Strings(files)
		files = applyStringOffset(files, offset)
		if len(files) > maxResults {
			files = files[:maxResults]
		}
		if len(files) == 0 {
			return noMatchResult()
		}
		result := strings.Join(files, "\n")
		if len(matchedFiles) >= maxResults+offset {
			result += fmt.Sprintf("\n... reached max_results=%d", maxResults)
		}
		return matchedResult(result)
	case "count":
		files := make([]string, 0, len(fileCounts))
		for file := range fileCounts {
			files = append(files, file)
		}
		sort.Strings(files)
		files = applyStringOffset(files, offset)
		if len(files) == 0 {
			return noMatchResult()
		}
		if len(files) > maxResults {
			files = files[:maxResults]
		}
		for _, file := range files {
			matches = append(matches, fmt.Sprintf("%s:%d", file, fileCounts[file]))
		}
	}

	if len(matches) == 0 {
		return noMatchResult()
	}
	matches = applyStringOffset(matches, offset)
	if len(matches) == 0 {
		return noMatchResult()
	}
	result := strings.Join(matches, "\n")
	if len(matches) >= maxResults {
		result += fmt.Sprintf("\n... reached max_results=%d", maxResults)
	}
	return matchedResult(result)
}

// ToolGlob finds files by glob pattern, including recursive ** patterns.
func ToolGlob(args map[string]interface{}) string {
	return ToolGlobDetailed(args).Text
}

func ToolGlobDetailed(args map[string]interface{}) SearchToolResult {
	pattern := StringArg(args, "pattern")
	if pattern == "" {
		return SearchToolResult{Text: "missing pattern parameter", Outcome: SearchToolOutcomeError}
	}
	base, err := resolveSearchBase(StringArg(args, "path"))
	if err != nil {
		return SearchToolResult{Text: err.Error(), Outcome: SearchToolOutcomeError}
	}
	maxResults := intArg(args, "max_results", 200)
	if maxResults <= 0 {
		maxResults = 200
	}
	if maxResults > 2000 {
		maxResults = 2000
	}
	includeDirs := boolArg(args, "include_dirs", false)
	includeHidden := boolArg(args, "include_hidden", false)
	fileType := StringArg(args, "type")
	excludePattern := resolveSearchExcludePattern(args, base)

	var results []string
	err = walkSearchFilesFiltered(base, pattern, excludePattern, fileType, includeDirs, includeHidden, func(path string, d fs.DirEntry) error {
		if d.IsDir() && !includeDirs {
			return nil
		}
		results = append(results, path)
		if len(results) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return SearchToolResult{Text: fmt.Sprintf("Glob failed: %s", err.Error()), Outcome: SearchToolOutcomeError}
	}
	sort.Strings(results)
	if len(results) == 0 {
		return SearchToolResult{Text: "no matches found", Outcome: SearchToolOutcomeNoMatch}
	}
	result := strings.Join(results, "\n")
	if len(results) >= maxResults {
		result += fmt.Sprintf("\n... reached max_results=%d", maxResults)
	}
	return SearchToolResult{Text: result, Outcome: SearchToolOutcomeMatched}
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
	p = workflowDocWritePath(p, args)

	absPath, err := ResolveFileToolPath(p)
	if err != nil {
		return err.Error()
	}
	size, err := tool.WriteTextFile(absPath, content, mode)
	if err != nil {
		if classifyLocalFileToolError(err).ReturnRawError() {
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
		if classifyLocalFileToolError(err).ReturnRawError() {
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
	fileName = workflowDocDeliveryFileNameWithFallbackExt(fileName, args, filepath.Ext(absPath))

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	forwardIM, _ := args["forward_to_im"].(bool)
	if forwardIM {
		if msgFlag := workflowDocDeliveryMessagePayloadFlag(args); msgFlag != "" {
			return fmt.Sprintf("[file_base64|%s|%s|im|%s]%s", fileName, mimeType, msgFlag, b64)
		}
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

func intArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	}
	return fallback
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	if v, ok := args[key].(bool); ok {
		return v
	}
	return fallback
}

func resolveSearchBase(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		if wd, err := os.Getwd(); err == nil {
			return wd, nil
		}
	}
	return ResolveFileToolPath(path)
}

func resolveSearchExcludePattern(args map[string]interface{}, base string) string {
	var patterns []string
	if exclude := strings.TrimSpace(StringArg(args, "exclude")); exclude != "" {
		patterns = append(patterns, exclude)
	}
	if exclude := strings.TrimSpace(StringArg(args, "exclude_glob")); exclude != "" {
		patterns = append(patterns, exclude)
	}
	if !boolArg(args, "no_ignore", false) {
		patterns = append(patterns, rootIgnoreExcludePattern(base))
	}
	return strings.Join(nonEmptySearchPatterns(patterns), " ")
}

func nonEmptySearchPatterns(patterns []string) []string {
	out := patterns[:0]
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}

func rootIgnoreExcludePattern(base string) string {
	root := base
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	var patterns []string
	for _, ignoreFile := range []string{
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".ignore"),
		filepath.Join(root, ".git", "info", "exclude"),
	} {
		data, err := os.ReadFile(ignoreFile)
		if err != nil {
			continue
		}
		filePatterns, ok := gitignoreExcludePatterns(string(data))
		if !ok {
			continue
		}
		patterns = append(patterns, filePatterns...)
	}
	return strings.Join(patterns, " ")
}

func gitignoreExcludePatterns(content string) ([]string, bool) {
	var patterns []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		} else if strings.HasPrefix(line, "#") {
			continue
		} else if strings.HasPrefix(line, "!") {
			return nil, false
		}
		line = strings.TrimRight(line, " ")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		line = filepath.ToSlash(line)
		if line == "" {
			continue
		}
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		if anchored {
			if dirOnly {
				patterns = append(patterns, "./"+line+"/**")
			} else {
				patterns = append(patterns, "./"+line, "./"+line+"/**")
			}
			continue
		}
		if dirOnly {
			if strings.Contains(line, "/") {
				patterns = append(patterns, line+"/**")
			} else {
				patterns = append(patterns, line+"/**", "**/"+line+"/**")
			}
			continue
		}
		patterns = append(patterns, line, line+"/**")
		if !strings.Contains(line, "/") {
			patterns = append(patterns, "**/"+line+"/**")
		}
	}
	return patterns, scanner.Err() == nil
}

func buildSearchRegexpPattern(pattern string, fixedString, wholeWord, lineRegexp bool) string {
	if fixedString {
		pattern = regexp.QuoteMeta(pattern)
	}
	if wholeWord {
		pattern = `\b(?:` + pattern + `)\b`
	}
	if lineRegexp {
		pattern = `^(?:` + pattern + `)$`
	}
	return pattern
}

func walkSearchFiles(base, pattern string, includeDirs bool, visit func(string, fs.DirEntry) error) error {
	return walkSearchFilesFiltered(base, pattern, "", "", includeDirs, false, visit)
}

func walkSearchFilesFiltered(base, pattern, excludePattern, fileType string, includeDirs, includeHidden bool, visit func(string, fs.DirEntry) error) error {
	info, err := os.Stat(base)
	if err != nil {
		return fmt.Errorf("闂佽崵濮崇拃锕傚垂閹殿喗顐介柣鎰嚟閳绘梻鈧箍鍎遍幊鎰板箺閻樼粯鐓曢柨鏂挎惈婵′粙鏌涢妸锝呭鐎殿噮鍣ｉ幃鈺佺暦閸ャ儮鍋撴繝鍥ㄥ仯濞达絽鎲￠崑銉╂煥? %w", err)
	}
	if !info.IsDir() {
		if !matchesSearchFilters(filepath.Base(base), false, pattern, excludePattern, fileType) {
			return nil
		}
		return visit(base, fileInfoDirEntry{info: info})
	}
	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != base && isSearchSymlink(d) {
			return nil
		}
		if path != base && d.IsDir() && shouldSkipSearchDir(d.Name(), includeHidden) {
			return filepath.SkipDir
		}
		if path == base {
			return nil
		}
		if !includeHidden && isHiddenSearchPath(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return nil
		}
		if excludePattern != "" {
			excluded, err := matchGlob(excludePattern, rel, d.IsDir())
			if err != nil {
				return err
			}
			if excluded && d.IsDir() {
				return filepath.SkipDir
			}
		}
		if d.IsDir() && !includeDirs {
			return nil
		}
		if !matchesSearchFilters(rel, d.IsDir(), pattern, excludePattern, fileType) {
			return nil
		}
		return visit(path, d)
	})
}

func matchesSearchFilters(rel string, isDir bool, globPattern, excludePattern, fileType string) bool {
	if excludePattern != "" {
		matched, err := matchGlob(excludePattern, rel, isDir)
		if err != nil || matched {
			return false
		}
	}
	if globPattern != "" {
		matched, err := matchGlob(globPattern, rel, isDir)
		if err != nil || !matched {
			return false
		}
	}
	if isDir || strings.TrimSpace(fileType) == "" {
		return true
	}
	return matchSearchFileType(rel, fileType)
}

func matchGlob(pattern, rel string, isDir bool) (bool, error) {
	patterns := expandSearchGlobPatterns(pattern)
	if len(patterns) == 0 {
		return true, nil
	}
	for _, pattern := range patterns {
		matched, err := matchSingleGlob(pattern, rel, isDir)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func matchSingleGlob(pattern, rel string, isDir bool) (bool, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	rel = filepath.ToSlash(rel)
	rootAnchored := strings.HasPrefix(pattern, "./")
	if rootAnchored {
		pattern = strings.TrimPrefix(pattern, "./")
	}
	if isDir {
		rel = strings.TrimSuffix(rel, "/") + "/"
	}
	if pattern == "" || pattern == "**" || pattern == "**/*" {
		return true, nil
	}
	if strings.HasPrefix(pattern, "**/") {
		matched, err := matchSingleGlob(strings.TrimPrefix(pattern, "**/"), rel, isDir)
		if err != nil || matched {
			return matched, err
		}
	}
	if !rootAnchored && !strings.Contains(pattern, "/") {
		return filepath.Match(pattern, filepath.Base(rel))
	}
	re := regexp.QuoteMeta(pattern)
	re = strings.ReplaceAll(re, `\*\*`, `.*`)
	re = strings.ReplaceAll(re, `\*`, `[^/]*`)
	re = strings.ReplaceAll(re, `\?`, `[^/]`)
	return regexp.MatchString("^"+re+"$", rel)
}

func expandSearchGlobPatterns(pattern string) []string {
	raw := strings.Fields(strings.TrimSpace(pattern))
	if len(raw) == 0 {
		return nil
	}
	var out []string
	for _, item := range raw {
		parts := []string{item}
		if !(strings.Contains(item, "{") && strings.Contains(item, "}")) {
			parts = strings.Split(item, ",")
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, expandSingleGlobBraces(part)...)
		}
	}
	return out
}

func expandSingleGlobBraces(pattern string) []string {
	start := strings.Index(pattern, "{")
	end := strings.Index(pattern, "}")
	if start < 0 || end <= start {
		return []string{pattern}
	}
	var out []string
	prefix, suffix := pattern[:start], pattern[end+1:]
	for _, option := range strings.Split(pattern[start+1:end], ",") {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		out = append(out, prefix+option+suffix)
	}
	if len(out) == 0 {
		return []string{pattern}
	}
	return out
}

func matchSearchFileType(path, fileType string) bool {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	for _, candidate := range searchFileTypeExtensions(fileType) {
		if strings.HasPrefix(candidate, "name:") && base == strings.TrimPrefix(candidate, "name:") {
			return true
		}
		if strings.HasPrefix(candidate, "suffix:") && strings.HasSuffix(base, strings.TrimPrefix(candidate, "suffix:")) {
			return true
		}
		if ext == candidate {
			return true
		}
	}
	return false
}

func searchFileTypeExtensions(fileType string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(fileType)), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	seen := make(map[string]bool)
	var out []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		for _, ext := range searchFileTypeExtensionSet(part) {
			if !seen[ext] {
				seen[ext] = true
				out = append(out, ext)
			}
		}
	}
	return out
}

func searchFileTypeExtensionSet(fileType string) []string {
	if strings.HasPrefix(fileType, ".") {
		return []string{fileType}
	}
	switch fileType {
	case "go", "golang":
		return []string{".go"}
	case "js", "javascript":
		return []string{".js", ".jsx", ".mjs", ".cjs"}
	case "ts", "typescript":
		return []string{".ts", ".tsx", ".mts", ".cts"}
	case "py", "python":
		return []string{".py", ".pyw"}
	case "rs", "rust":
		return []string{".rs"}
	case "java":
		return []string{".java"}
	case "c":
		return []string{".c", ".h"}
	case "cpp", "c++":
		return []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}
	case "cs", "csharp":
		return []string{".cs"}
	case "md", "markdown":
		return []string{".md", ".markdown", ".mdx"}
	case "json":
		return []string{".json", ".jsonc"}
	case "yaml", "yml":
		return []string{".yaml", ".yml"}
	case "toml":
		return []string{".toml"}
	case "html":
		return []string{".html", ".htm"}
	case "css":
		return []string{".css", ".scss", ".sass", ".less"}
	case "sh", "shell", "bash":
		return []string{".sh", ".bash", ".zsh", ".fish"}
	case "sql":
		return []string{".sql"}
	case "proto", "protobuf":
		return []string{".proto"}
	case "kt", "kotlin":
		return []string{".kt", ".kts"}
	case "swift":
		return []string{".swift"}
	case "php":
		return []string{".php", ".phtml"}
	case "rb", "ruby":
		return []string{".rb"}
	case "xml":
		return []string{".xml"}
	case "docker", "dockerfile":
		return []string{".dockerfile", "name:dockerfile"}
	case "make", "makefile":
		return []string{".mk", "name:makefile"}
	case "cmake":
		return []string{".cmake", "name:cmakelists.txt"}
	case "gradle":
		return []string{".gradle", "suffix:.gradle.kts"}
	default:
		return []string{"." + fileType}
	}
}

func shouldSkipSearchDir(name string, includeHidden bool) bool {
	if name == ".git" {
		return true
	}
	if !includeHidden && isHiddenSearchPath(name) {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build", "out", "target", "coverage", ".next", ".cache", ".gomodcache", ".gocache", ".venv", "venv", "env", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".gradle", ".terraform":
		return true
	default:
		return false
	}
}

func isHiddenSearchPath(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func isSearchSymlink(d fs.DirEntry) bool {
	return d.Type()&fs.ModeSymlink != 0
}

func isLikelyBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	return bytes.Contains(buf[:n], []byte{0})
}

func searchFileLinesWithMode(path string, re *regexp.Regexp, maxResults int, outputMode string, beforeContext, afterContext int, matches *[]string, matchedFiles map[string]bool, fileCounts map[string]int) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSearchFileSize+1)
	lineNo := 0
	var previous []numberedLine
	var pendingAfter int
	lastOutputLine := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !re.MatchString(line) {
			if pendingAfter > 0 && outputMode == "content" {
				appendSearchOutputLine(path, lineNo, line, false, &lastOutputLine, matches)
				pendingAfter--
				if len(*matches) >= maxResults {
					return filepath.SkipAll
				}
			}
			if beforeContext > 0 {
				previous = append(previous, numberedLine{lineNo: lineNo, text: line})
				if len(previous) > beforeContext {
					previous = previous[len(previous)-beforeContext:]
				}
			}
			continue
		}
		switch outputMode {
		case "files_with_matches":
			matchedFiles[path] = true
			return nil
		case "count":
			fileCounts[path]++
		default:
			for _, ctx := range previous {
				appendSearchOutputLine(path, ctx.lineNo, ctx.text, false, &lastOutputLine, matches)
				if len(*matches) >= maxResults {
					return filepath.SkipAll
				}
			}
			appendSearchOutputLine(path, lineNo, line, true, &lastOutputLine, matches)
			if len(*matches) >= maxResults {
				return filepath.SkipAll
			}
			pendingAfter = afterContext
		}
		if beforeContext > 0 {
			previous = append(previous, numberedLine{lineNo: lineNo, text: line})
			if len(previous) > beforeContext {
				previous = previous[len(previous)-beforeContext:]
			}
		}
	}
	return scanner.Err()
}

type numberedLine struct {
	lineNo int
	text   string
}

func appendSearchOutputLine(path string, lineNo int, line string, isMatch bool, lastOutputLine *int, matches *[]string) {
	if lineNo <= *lastOutputLine {
		return
	}
	if *lastOutputLine > 0 && lineNo > *lastOutputLine+1 {
		*matches = append(*matches, "--")
	}
	*lastOutputLine = lineNo
	trimmed := strings.TrimSpace(line)
	if len([]rune(trimmed)) > 240 {
		trimmed = string([]rune(trimmed)[:237]) + "..."
	}
	separator := ":"
	if !isMatch {
		separator = "-"
	}
	*matches = append(*matches, fmt.Sprintf("%s%s%d:%s", path, separator, lineNo, trimmed))
}

func resolveSearchContextArgs(args map[string]interface{}) (int, int) {
	if c := intArg(args, "context", 0); c > 0 {
		return c, c
	}
	before := intArg(args, "before_context", 0)
	after := intArg(args, "after_context", 0)
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	return before, after
}

func applyStringOffset(items []string, offset int) []string {
	if offset <= 0 {
		return items
	}
	if offset >= len(items) {
		return nil
	}
	return items[offset:]
}

func appendSearchStats(result string, include bool, stats searchCandidateStats, filesSearched int) string {
	if !include {
		return result
	}
	mode := "full_scan"
	if stats.indexed {
		mode = "indexed"
	}
	line := fmt.Sprintf("search_stats: mode=%s indexed_files=%d candidates=%d dirty=%d searched=%d rebuilt=%t candidate_ms=%d scan_ms=%d total_ms=%d",
		mode,
		stats.indexedFiles,
		stats.candidateFiles,
		stats.dirtyFiles,
		filesSearched,
		stats.rebuilt,
		durationMillis(stats.candidateTime),
		durationMillis(stats.scanTime),
		durationMillis(stats.totalTime),
	)
	if !stats.indexed && stats.fallbackReason != "" {
		line += " fallback=" + stats.fallbackReason
	}
	return fmt.Sprintf("%s\n%s",
		result, line)
}

func durationMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms == 0 {
		return 1
	}
	return ms
}

func searchOutputLimitReached(outputMode string, limit int, matches []string, matchedFiles map[string]bool, fileCounts map[string]int) bool {
	if limit <= 0 {
		return true
	}
	switch outputMode {
	case "files_with_matches":
		return len(matchedFiles) >= limit
	case "count":
		return len(fileCounts) >= limit
	default:
		return len(matches) >= limit
	}
}

func isValidSearchOutputMode(outputMode string) bool {
	switch outputMode {
	case "content", "files_with_matches", "count":
		return true
	default:
		return false
	}
}

func searchFileLines(path string, re *regexp.Regexp, maxResults int, matches *[]string) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSearchFileSize+1)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if re.MatchString(line) {
			trimmed := strings.TrimSpace(line)
			if len([]rune(trimmed)) > 240 {
				trimmed = string([]rune(trimmed)[:237]) + "..."
			}
			*matches = append(*matches, fmt.Sprintf("%s:%d:%s", path, lineNo, trimmed))
			if len(*matches) >= maxResults {
				return filepath.SkipAll
			}
		}
	}
	return scanner.Err()
}

type fileInfoDirEntry struct {
	info os.FileInfo
}

func (d fileInfoDirEntry) Name() string               { return d.info.Name() }
func (d fileInfoDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d fileInfoDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d fileInfoDirEntry) Info() (os.FileInfo, error) { return d.info, nil }

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
		if timeout < 240 {
			timeout = 240
		}
		if timeout > 600 {
			timeout = 600
		}
		return timeout
	}
	return 600
}

// HideCommandWindow is a platform-specific function to hide the console
// window on Windows. No-op on other platforms.
func HideCommandWindow(cmd *exec.Cmd) {
	hideCommandWindowImpl(cmd)
}
