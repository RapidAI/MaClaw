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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// --- Constants ---

const (
	ReadFileMaxLines  = 200
	WriteFileMaxSize  = 512 * 1024 // 512 KB per write
	SendFileMaxSize   = 200 << 20  // 200 MB
	MaxSearchFileSize = 2 << 20    // 2 MB per file for search tools

	maxSearchGlobPatternCacheEntries = 256
	maxSearchGlobBraceDepth          = 16
)

// maxSearchWalkTime caps the directory walk for search tools.
// Prevents blocking when projectPath accidentally points to a huge tree
// (e.g. user home directory). A var so a test can force the stop.
var maxSearchWalkTime = 30 * time.Second

// errSearchWalkDeadline is not filepath.SkipAll. SkipAll is also how a search
// stops at max_results, and that stop is a finished page. A deadline means
// the tree was not finished; reporting it as SkipAll made the partial page
// look complete.
var errSearchWalkDeadline = errors.New("search walk deadline")

const searchWalkStoppedFooter = "... search stopped before the tree was finished"

// --- Bash ---

// ToolBash executes a shell command on the host machine.
// onProgress is called every 30s with a heartbeat message for long-running commands.
func ToolBash(args map[string]interface{}, onProgress func(string)) string {
	return ToolBashWithContext(context.Background(), args, onProgress)
}

// ToolBashWithContext executes a shell command and stops the process tree when
// the parent context is cancelled or the tool timeout is reached.
func ToolBashWithContext(parent context.Context, args map[string]interface{}, onProgress func(string)) string {
	if parent == nil {
		parent = context.Background()
	}
	command := StringArg(args, "command")
	if command == "" {
		return "缺少 command 参数"
	}

	if rejection, rejected := tool.RejectRawSSHCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectShellDatabaseCLI(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectBroadBrowserKillCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectShellBrowserAutomationCommand(command); rejected {
		return rejection
	}
	if rejection, rejected := tool.RejectBrowserSideEffectHTTPCommand(command); rejected {
		return rejection
	}

	timeout := ResolveBashTimeout(args, command)
	workDir := ResolvePath(StringArg(args, "working_dir"))

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		psPath, err := tool.ResolveWindowsPowerShell()
		if err != nil {
			return "[错误] 命令启动失败: " + err.Error()
		}
		shellName = psPath
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
					onProgress(fmt.Sprintf("命令仍在执行中（已 %ds）: %s", elapsed, displayCmd))
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
		if ctx.Err() == context.Canceled {
			b.WriteString("\n[error] command cancelled")
		} else if ctx.Err() == context.DeadlineExceeded {
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

	lines := tool.SplitTextLines(string(data))
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
	lines := tool.SplitTextLines(text)
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
	return ToolRipgrepDetailedCtx(context.Background(), args)
}

func ToolRipgrepDetailedCtx(ctx context.Context, args map[string]interface{}) SearchToolResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SearchToolResult{Text: "search cancelled", Outcome: SearchToolOutcomeError}
	}
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
	// One line past the page. Stopping at exactly max_results cannot tell a
	// finished tree from a truncated one, and the page hint then sends the
	// model to offset a search that already ended.
	scanLimit := outputLimit + 1
	capped := false
	beforeContext, afterContext := resolveSearchContextArgs(args)
	includeStats := boolArg(args, "stats", false)
	fileType := StringArg(args, "type")
	includeHidden := boolArg(args, "include_hidden", false)
	if !boolArg(args, "no_ignore", false) {
		ctx = withTreeSearchIgnore(ctx, base)
	}

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
	// files_with_matches and count sort paths before offset. The page has to
	// be cut in that same order. Cutting the walk order and sorting the
	// prefix drops a later path that sorts onto an earlier page.
	orderByPath := outputMode == "files_with_matches" || outputMode == "count"
	var listed []string
	scanFile := func(path string, d fs.DirEntry) error {
		if searchOutputLimitReached(outputMode, scanLimit, matches, matchedFiles, fileCounts) {
			capped = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSearchFileSize {
			return nil
		}
		scanned, lineErr := searchFileLinesWithModeCtx(ctx, path, re, scanLimit, outputMode, beforeContext, afterContext, &matches, matchedFiles, fileCounts)
		if scanned {
			filesSearched++
		}
		if dropSearchLookahead(outputMode, outputLimit, &matches, matchedFiles, fileCounts, path) {
			capped = true
			if lineErr != nil && lineErr != filepath.SkipAll {
				return lineErr
			}
			return filepath.SkipAll
		}
		return lineErr
	}
	candidateStart := time.Now()
	candidates, ok, stats := indexedSearchCandidatesCtx(ctx, base, pattern, globPattern, excludePattern, fileType, fixedString, includeHidden, boolArg(args, "no_ignore", false))
	if !ok && strings.HasPrefix(stats.fallbackReason, "ignore_pattern:") {
		return SearchToolResult{Text: "search failed: " + strings.TrimPrefix(stats.fallbackReason, "ignore_pattern:"), Outcome: SearchToolOutcomeError}
	}
	stats.candidateTime = time.Since(candidateStart)
	searchStats = stats
	scanStart := time.Now()
	searchRoot := base
	if absRoot, absErr := filepath.Abs(base); absErr == nil {
		searchRoot = filepath.Clean(absRoot)
	}
	realDirs := make(map[string]bool)
	if ok {
		searchStats = stats
		if orderByPath {
			listed = append(listed, candidates...)
		} else {
			for i, path := range candidates {
				if i&511 == 0 && ctx.Err() != nil {
					err = ctx.Err()
					break
				}
				// Lstat, and do not assign err with :=. Stat follows a symlink, so a
				// file that was indexed and later replaced by a link returns the
				// target's matches under the link path. The walk skips links,
				// including a parent directory that is a link. := on err would hide
				// a cap stop from the result.
				info, statErr := os.Lstat(path)
				if statErr != nil || info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || searchDirReparse(path, info) || searchPathCrossesSymlink(searchRoot, path, realDirs) {
					continue
				}
				visitErr := scanFile(path, fileInfoDirEntry{info: info})
				if visitErr != nil {
					err = visitErr
					break
				}
			}
		}
	} else {
		searchStats = stats
		err = walkSearchFilesFilteredCtx(ctx, base, globPattern, excludePattern, fileType, false, includeHidden, func(path string, d fs.DirEntry) error {
			if !orderByPath {
				return scanFile(path, d)
			}
			info, infoErr := d.Info()
			if infoErr != nil || info.IsDir() || info.Size() > MaxSearchFileSize {
				return nil
			}
			listed = append(listed, path)
			return nil
		})
	}
	if orderByPath && (err == nil || errors.Is(err, errSearchWalkDeadline)) {
		listingStopped := errors.Is(err, errSearchWalkDeadline)
		sort.Strings(listed)
		var scanErr error
		for i, path := range listed {
			if i&511 == 0 && ctx.Err() != nil {
				scanErr = ctx.Err()
				break
			}
			info, statErr := os.Lstat(path)
			if statErr != nil || info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || searchDirReparse(path, info) || searchPathCrossesSymlink(searchRoot, path, realDirs) {
				continue
			}
			scanErr = scanFile(path, fileInfoDirEntry{info: info})
			if scanErr != nil {
				break
			}
		}
		switch {
		case scanErr != nil && scanErr != filepath.SkipAll:
			err = scanErr
		case listingStopped:
			err = errSearchWalkDeadline
		default:
			err = scanErr
		}
	}
	searchStats.scanTime = time.Since(scanStart)
	// The unlisted supplement is a search walk. A deadline there keeps the
	// files already read; the footer has to say the tree was not finished.
	if ok && stats.walkStopped && (err == nil || err == filepath.SkipAll) {
		err = errSearchWalkDeadline
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil && (err == context.Canceled || err == context.DeadlineExceeded) {
		return SearchToolResult{Text: "search cancelled", Outcome: SearchToolOutcomeError}
	}
	walkStopped := errors.Is(err, errSearchWalkDeadline)
	if err != nil && err != filepath.SkipAll && !walkStopped {
		return SearchToolResult{Text: fmt.Sprintf("search failed: %s", err.Error()), Outcome: SearchToolOutcomeError}
	}
	finishEmpty := func() SearchToolResult {
		if walkStopped {
			return SearchToolResult{Text: appendStats(searchWalkStoppedFooter), Outcome: SearchToolOutcomeNoMatch}
		}
		return noMatchResult()
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
			return finishEmpty()
		}
		result := strings.Join(files, "\n")
		result = appendSearchLimitFooter(result, capped, walkStopped, maxResults)
		return matchedResult(result)
	case "count":
		files := make([]string, 0, len(fileCounts))
		for file := range fileCounts {
			files = append(files, file)
		}
		sort.Strings(files)
		files = applyStringOffset(files, offset)
		if len(files) == 0 {
			return finishEmpty()
		}
		if len(files) > maxResults {
			files = files[:maxResults]
		}
		for _, file := range files {
			matches = append(matches, fmt.Sprintf("%s:%d", file, fileCounts[file]))
		}
		result := strings.Join(matches, "\n")
		result = appendSearchLimitFooter(result, capped, walkStopped, maxResults)
		return matchedResult(result)
	}

	if len(matches) == 0 {
		return finishEmpty()
	}
	matches = applyStringOffset(matches, offset)
	if len(matches) == 0 {
		return finishEmpty()
	}
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}
	result := strings.Join(matches, "\n")
	result = appendSearchLimitFooter(result, capped, walkStopped, maxResults)
	return matchedResult(result)
}

func appendSearchLimitFooter(result string, capped, walkStopped bool, maxResults int) string {
	if capped {
		result += fmt.Sprintf("\n... reached max_results=%d", maxResults)
	}
	if walkStopped {
		result += "\n" + searchWalkStoppedFooter
	}
	return result
}

// dropSearchLookahead removes the one result past the page. That result is
// only proof that another page exists; leaving it in the output makes a
// finished page and a truncated page look the same.
func dropSearchLookahead(outputMode string, outputLimit int, matches *[]string, matchedFiles map[string]bool, fileCounts map[string]int, path string) bool {
	switch outputMode {
	case "files_with_matches":
		if len(matchedFiles) > outputLimit {
			delete(matchedFiles, path)
			return true
		}
	case "count":
		if len(fileCounts) > outputLimit {
			delete(fileCounts, path)
			return true
		}
	default:
		if matches != nil && len(*matches) > outputLimit {
			*matches = (*matches)[:outputLimit]
			return true
		}
	}
	return false
}

// ToolGlob finds files by glob pattern, including recursive ** patterns.
func ToolGlob(args map[string]interface{}) string {
	return ToolGlobDetailed(args).Text
}

func ToolGlobDetailed(args map[string]interface{}) SearchToolResult {
	return ToolGlobDetailedCtx(context.Background(), args)
}

func ToolGlobDetailedCtx(ctx context.Context, args map[string]interface{}) SearchToolResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SearchToolResult{Text: "Glob cancelled", Outcome: SearchToolOutcomeError}
	}
	pattern := StringArg(args, "pattern")
	if pattern == "" {
		pattern = StringArg(args, "glob_pattern")
	}
	if pattern == "" {
		pattern = StringArg(args, "glob")
	}
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
	if !boolArg(args, "no_ignore", false) {
		ctx = withTreeSearchIgnore(ctx, base)
	}

	var results []string
	capped := false
	err = walkSearchFilesFilteredCtx(ctx, base, pattern, excludePattern, fileType, includeDirs, includeHidden, func(path string, d fs.DirEntry) error {
		if d.IsDir() && !includeDirs {
			return nil
		}
		results = append(results, path)
		if len(results) > maxResults {
			capped = true
			results = results[:maxResults]
			return filepath.SkipAll
		}
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil && (err == context.Canceled || err == context.DeadlineExceeded) {
		return SearchToolResult{Text: "Glob cancelled", Outcome: SearchToolOutcomeError}
	}
	walkStopped := errors.Is(err, errSearchWalkDeadline)
	if err != nil && err != filepath.SkipAll && !walkStopped {
		return SearchToolResult{Text: fmt.Sprintf("Glob failed: %s", err.Error()), Outcome: SearchToolOutcomeError}
	}
	sort.Strings(results)
	if len(results) == 0 {
		if walkStopped {
			return SearchToolResult{Text: searchWalkStoppedFooter, Outcome: SearchToolOutcomeNoMatch}
		}
		return SearchToolResult{Text: "no matches found", Outcome: SearchToolOutcomeNoMatch}
	}
	result := strings.Join(results, "\n")
	if capped {
		result += fmt.Sprintf("\n... reached max_results=%d", maxResults)
	} else if walkStopped {
		result += "\n" + searchWalkStoppedFooter
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

	// Structured only: destination enum and/or forward_to_im flag (no free-text NLP).
	forwardIM := resolveToolSendFileForward(args)
	if forwardIM {
		targetFlag := EncodeIMFileDeliveryTargetFlag(args)
		if msgFlag := workflowDocDeliveryMessagePayloadFlag(args); msgFlag != "" {
			if targetFlag != "" {
				return fmt.Sprintf("[file_base64|%s|%s|im|%s|%s]%s", fileName, mimeType, targetFlag, msgFlag, b64)
			}
			return fmt.Sprintf("[file_base64|%s|%s|im|%s]%s", fileName, mimeType, msgFlag, b64)
		}
		if targetFlag != "" {
			return fmt.Sprintf("[file_base64|%s|%s|im|%s]%s", fileName, mimeType, targetFlag, b64)
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
	v, exists := args[key]
	if !exists || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "1" || s == "true" || s == "yes" || s == "on" {
			return true
		}
		if s == "0" || s == "false" || s == "no" || s == "off" {
			return false
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return fallback
}

// resolveToolSendFileForward uses structured tool args only (destination /
// forward_to_im). No free-text keyword matching on the user message.
func resolveToolSendFileForward(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	if raw, ok := args["destination"]; ok && raw != nil {
		if s, isStr := raw.(string); isStr {
			switch destKind := agentSendFileDestinationKind(s); destKind {
			case agentSendFileDestIM:
				return true
			case agentSendFileDestDesktop:
				return false
			}
		}
	}
	return boolArg(args, "forward_to_im", false)
}

type agentSendFileDestKind int

const (
	agentSendFileDestUnknown agentSendFileDestKind = iota
	agentSendFileDestIM
	agentSendFileDestDesktop
)

func agentSendFileDestinationKind(dest string) agentSendFileDestKind {
	d := strings.ToLower(strings.TrimSpace(dest))
	raw := strings.TrimSpace(dest)
	switch d {
	case "im", "wechat", "weixin", "wx", "feishu", "lark", "qq",
		"dingtalk", "ding", "telegram", "tg":
		return agentSendFileDestIM
	case "chat", "desktop", "local", "ui", "assistant":
		return agentSendFileDestDesktop
	}
	switch raw {
	case "微信", "飞书", "钉钉", "企微", "企业微信":
		return agentSendFileDestIM
	case "桌面", "对话", "当前对话":
		return agentSendFileDestDesktop
	default:
		return agentSendFileDestUnknown
	}
}

// ToolSendToIM is send_file with IM delivery forced by tool name.
func ToolSendToIM(args map[string]interface{}) string {
	if args == nil {
		args = map[string]interface{}{}
	}
	args["forward_to_im"] = true
	// Tool name wins over missing/desktop/unknown destination.
	if agentSendFileDestinationKind(StringArg(args, "destination")) != agentSendFileDestIM {
		args["destination"] = "im"
	}
	return ToolSendFile(args)
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
	user := strings.Join(nonEmptySearchPatterns(patterns), " ")
	var ignore string
	if !boolArg(args, "no_ignore", false) {
		ignore = rootIgnoreExcludePattern(base)
	}
	switch {
	case user == "" && ignore == "":
		return ""
	case user == "":
		// A leading NUL marks every following pattern as one gitignore glob.
		return "\x00" + ignore
	case ignore == "":
		return user
	default:
		return user + "\x00" + ignore
	}
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

type rootIgnoreCacheEntry struct {
	pattern string
	stamps  []ignoreStamp
}

var rootIgnoreCache = struct {
	sync.Mutex
	byDir map[string]rootIgnoreCacheEntry
}{
	byDir: map[string]rootIgnoreCacheEntry{},
}

// rootIgnoreExcludePattern reads the search directory's own ignore files.
// A file base uses that file's directory. The joined pattern is reused while
// .git, the ignore files, info/exclude, and a worktree commondir keep the
// same size and mtime. The caller adds the leading NUL.
func rootIgnoreExcludePattern(base string) string {
	root := base
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	root = filepath.Clean(root)
	key := searchPathKey(root)
	if pattern, ok := loadFreshRootIgnore(key); ok {
		return pattern
	}
	pattern, stamps := collectRootIgnoreExcludePattern(root)
	storeRootIgnore(key, pattern, stamps)
	return pattern
}

func loadFreshRootIgnore(key string) (string, bool) {
	rootIgnoreCache.Lock()
	entry, ok := rootIgnoreCache.byDir[key]
	rootIgnoreCache.Unlock()
	if !ok || !ignoreStampsMatch(entry.stamps) {
		return "", false
	}
	return entry.pattern, true
}

func storeRootIgnore(key, pattern string, stamps []ignoreStamp) {
	if key == "" || len(stamps) == 0 {
		return
	}
	rootIgnoreCache.Lock()
	defer rootIgnoreCache.Unlock()
	rootIgnoreCache.byDir[key] = rootIgnoreCacheEntry{
		pattern: pattern,
		stamps:  append([]ignoreStamp(nil), stamps...),
	}
	for len(rootIgnoreCache.byDir) > 1024 {
		if !dropOneIgnoreCacheEntry(rootIgnoreCache.byDir, key) {
			return
		}
	}
}

func collectRootIgnoreExcludePattern(root string) (string, []ignoreStamp) {
	var stamps []ignoreStamp
	add := func(path string) {
		stamps = append(stamps, stampIgnorePath(path))
	}
	add(filepath.Join(root, ".git"))
	names := []string{
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".ignore"),
	}
	for _, name := range names {
		add(name)
	}
	for _, extra := range gitPointerStampPaths(root) {
		add(extra)
	}
	if extra := searchGitInfoExcludeFile(root); extra != "" {
		add(extra)
		names = append(names, extra)
	}
	var patterns []string
	for _, ignoreFile := range names {
		data, err := readStampCachedFile(ignoreFile)
		if err != nil {
			continue
		}
		filePatterns, ok := gitignoreExcludePatterns(string(data))
		if !ok {
			continue
		}
		patterns = append(patterns, filePatterns...)
	}
	return strings.Join(patterns, "\x00"), stamps
}

// gitignoreRule is one ignore line after escape handling. dirOnly patterns
// match directories. The contents expansion used at the search root is not
// the same rule as a direct match against a parent path.
type gitignoreRule struct {
	pattern  string
	anchored bool
	dirOnly  bool
}

func parseGitignoreRules(content string) ([]gitignoreRule, bool) {
	var rules []gitignoreRule
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		// Git uses '\' as an escape, including for a trailing space. TrimSpace
		// and Windows ToSlash both destroy that escape and can turn it into a
		// directory rule.
		line := trimGitignoreTrailingSpaces(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '!' {
			return nil, false
		}
		anchored := strings.HasPrefix(line, "/")
		if anchored {
			line = line[1:]
		}
		dirOnly := gitignoreUnescapedTrailingSlash(line)
		if dirOnly {
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		unescaped, ok := unescapeGitignorePattern(line)
		if !ok || unescaped == "" {
			continue
		}
		// "./foo" is not a root anchor. Git keeps the dot segment, and the
		// relative paths we match are cleaned, so the rule matches nothing.
		// compileSingleSearchGlob still strips a leading "./": that is how a
		// leading "/" anchor is encoded, and how a user glob writes ./src.
		// Stripping it here would turn "./*.log" into "*.log".
		if strings.HasPrefix(unescaped, "./") {
			continue
		}
		rules = append(rules, gitignoreRule{pattern: unescaped, anchored: anchored, dirOnly: dirOnly})
	}
	if scanner.Err() != nil {
		return nil, false
	}
	return rules, true
}

func gitignoreExcludePatterns(content string) ([]string, bool) {
	rules, ok := parseGitignoreRules(content)
	if !ok {
		return nil, false
	}
	var patterns []string
	for _, rule := range rules {
		line := rule.pattern
		if rule.dirOnly {
			// foo/**/ ignores directories under foo, not files sitting
			// directly in foo. A trailing globstar already matches those
			// files, so the contents suffix /** would hide them too.
			line = narrowDirOnlyGlobstar(line)
		}
		if rule.anchored {
			if rule.dirOnly {
				patterns = append(patterns, "./"+line+"/**")
			} else {
				patterns = append(patterns, "./"+line, "./"+line+"/**")
			}
			continue
		}
		if rule.dirOnly {
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
	return patterns, true
}

// narrowDirOnlyGlobstar replaces a trailing globstar component with one star.
// The contents suffix /** then requires a child directory. **/ becomes */**,
// and foo/**/ and foo/***/ become foo/*/**. A pattern that does not end in a
// globstar component, such as foo/ or foo/*/, is unchanged.
func narrowDirOnlyGlobstar(line string) string {
	slash := strings.LastIndex(line, "/")
	comp := line
	if slash >= 0 {
		comp = line[slash+1:]
	}
	if len(comp) < 2 || strings.Trim(comp, "*") != "" {
		return line
	}
	if slash < 0 {
		return "*"
	}
	return line[:slash+1] + "*"
}

// gitignoreDirectPatterns matches a path itself. Directory rules stay
// directory rules: "src/" matches a directory named src, not every file
// under the directory the user asked to search.
func gitignoreDirectPatterns(content string) (files, dirs []string, ok bool) {
	rules, ok := parseGitignoreRules(content)
	if !ok {
		return nil, nil, false
	}
	for _, rule := range rules {
		pat := rule.pattern
		if rule.anchored {
			pat = "./" + rule.pattern
		}
		if rule.dirOnly {
			dirs = append(dirs, pat)
			continue
		}
		files = append(files, pat)
	}
	return files, dirs, true
}

// trimGitignoreTrailingSpaces drops trailing spaces that are not escaped.
// A space kept by an odd run of backslashes stays. "foo\\ " drops the space
// because that backslash is the escaped-backslash itself.
func trimGitignoreTrailingSpaces(line string) string {
	i := len(line)
	for i > 0 && line[i-1] == ' ' {
		slashes := 0
		for j := i - 1; j > 0 && line[j-1] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 1 {
			break
		}
		i--
	}
	return line[:i]
}

// gitignoreUnescapedTrailingSlash reports a directory-only trailing '/'.
// An even number of backslashes leaves the slash in place, so foo/ and
// foo\\/ are directories. One backslash escapes the slash, so foo\/ is not.
func gitignoreUnescapedTrailingSlash(line string) bool {
	if len(line) < 2 || line[len(line)-1] != '/' {
		return false
	}
	slashes := 0
	for j := len(line) - 1; j > 0 && line[j-1] == '\\'; j-- {
		slashes++
	}
	return slashes%2 == 0
}

// unescapeGitignorePattern applies gitignore escapes. A final backslash is an
// invalid pattern and matches nothing, so the other lines still apply. Outside
// a character class, glob metacharacters become one-character classes so brace
// expansion leaves "\{a,b}" literal. Inside a class, the next character is a
// member: "\*" is a star, "\-" is a dash rather than a range, and "\]" does
// not close the class.
func unescapeGitignorePattern(line string) (string, bool) {
	if strings.IndexByte(line, '\\') < 0 {
		return line, true
	}
	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(line); i++ {
		if line[i] == '[' {
			if rewritten, next, ok := rewriteGitignoreEscapedClass(line, i); ok {
				b.WriteString(rewritten)
				i = next - 1
				continue
			}
			b.WriteByte('[')
			continue
		}
		if line[i] != '\\' {
			b.WriteByte(line[i])
			continue
		}
		if i+1 >= len(line) {
			return "", false
		}
		i++
		switch line[i] {
		case '*', '?', '[', '{', '}', '\\':
			b.WriteByte('[')
			b.WriteByte(line[i])
			b.WriteByte(']')
		default:
			b.WriteByte(line[i])
		}
	}
	return b.String(), true
}

type gitignoreClassSpan struct {
	lo, hi rune
	rng    bool
	// posix is a [:name:] member. Its ']' is not the class closer, and it
	// clears the previous character so the next dash is not a range.
	posix string
}

// rewriteGitignoreEscapedClass normalizes one character class that contains a
// backslash. Git's wildmatch keeps an escaped character as a member and only
// then looks for a range, so [a\*b], [a\-c], and [a\]b] are each one class.
// A class with no escape is left to the caller, which copies it unchanged.
// An unclosed class is not a class; the caller keeps the '[' literal.

func rewriteGitignoreEscapedClass(line string, open int) (string, int, bool) {
	i := open + 1
	if i >= len(line) {
		return "", 0, false
	}
	neg := ""
	if line[i] == '!' || line[i] == '^' {
		neg = line[i : i+1]
		i++
	}
	var spans []gitignoreClassSpan
	var prev rune
	var havePrev bool
	escaped := false
	seen := false
	for i < len(line) {
		// The first ']' is a member. A later unescaped ']' closes the class.
		if seen && line[i] == ']' {
			i++
			if !escaped {
				return "", 0, false
			}
			return emitGitignoreClass(neg, spans), i, true
		}
		seen = true
		if line[i] == '\\' {
			if i+1 >= len(line) {
				return "", 0, false
			}
			escaped = true
			ch := rune(line[i+1])
			i += 2
			spans = append(spans, gitignoreClassSpan{lo: ch, hi: ch})
			prev = ch
			havePrev = true
			continue
		}
		// [:digit:] is one member. The ']' that ends the name must not close
		// the class, or [ [:digit:] \ ] ] never sees the escaped bracket.
		if line[i] == '[' {
			if name, next, ok := searchGlobPosixClass(line, i); ok {
				spans = append(spans, gitignoreClassSpan{posix: name})
				havePrev = false
				i = next
				continue
			}
		}
		// A dash is a range only when a member sits before it and the end is
		// not the closer. An escaped dash never reaches this branch. After a
		// range the previous member is cleared, so [a-c-e] is a-c, "-", "e".
		if line[i] == '-' && havePrev && i+1 < len(line) && line[i+1] != ']' {
			i++
			var hi rune
			if line[i] == '\\' {
				if i+1 >= len(line) {
					return "", 0, false
				}
				escaped = true
				hi = rune(line[i+1])
				i += 2
			} else {
				hi = rune(line[i])
				i++
			}
			if prev <= hi {
				spans = append(spans, gitignoreClassSpan{lo: prev, hi: hi, rng: true})
			}
			havePrev = false
			continue
		}
		ch := rune(line[i])
		i++
		spans = append(spans, gitignoreClassSpan{lo: ch, hi: ch})
		prev = ch
		havePrev = true
	}
	return "", 0, false
}

// emitGitignoreClass writes a class the existing matcher already understands.
// A literal ']' is the first member. A literal '-' is last, so it is not a
// range. filepath.Match rejects those forms and the path regex compiles them.
// A range that starts or ends at '-' or ']' cannot survive that placement:
// [a\--c] would be reread as a backwards range, and [A-\]] would close at the
// first ']'. A literal '!' or '^' in the same position is reread as class
// negation, so [\!a] would hide every name except a. Those spans are written
// as [[#g:...]] and compiled from the hex list. An ordinary range such as
// [a\-b-c] stays on the text path.
func emitGitignoreClass(neg string, spans []gitignoreClassSpan) string {
	for _, span := range spans {
		if span.lo == '!' || span.lo == '^' || span.hi == '!' || span.hi == '^' ||
			(span.rng && (span.lo == '-' || span.hi == '-' || span.lo == ']' || span.hi == ']')) {
			return emitGitignorePrebuiltClass(neg, spans)
		}
	}
	var middle strings.Builder
	bracket := false
	dash := false
	for _, span := range spans {
		if span.posix != "" {
			middle.WriteString("[:" + span.posix + ":]")
			continue
		}
		if span.rng {
			middle.WriteRune(span.lo)
			middle.WriteByte('-')
			middle.WriteRune(span.hi)
			continue
		}
		switch span.lo {
		case ']':
			bracket = true
		case '-':
			dash = true
		default:
			middle.WriteRune(span.lo)
		}
	}
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(neg)
	if bracket {
		b.WriteByte(']')
	}
	b.WriteString(middle.String())
	if dash {
		b.WriteByte('-')
	}
	b.WriteByte(']')
	return b.String()
}

// emitGitignorePrebuiltClass encodes spans that a second textual parse would
// change. [[#g:2D-63,61]] is the range from '-' to 'c' plus 'a'. A leading
// '!' negates the set. The hex is the member list, not a glob range. @digit
// is the POSIX class [:digit:]. A name that contains a comma cannot be
// stored in this list; that token is unknown and matches nothing.
func emitGitignorePrebuiltClass(neg string, spans []gitignoreClassSpan) string {
	for _, span := range spans {
		if strings.Contains(span.posix, ",") {
			return "[[#g:]]"
		}
	}
	var b strings.Builder
	b.WriteString("[[#g:")
	if neg != "" {
		b.WriteByte('!')
	}
	for i, span := range spans {
		if i > 0 {
			b.WriteByte(',')
		}
		if span.posix != "" {
			b.WriteByte('@')
			b.WriteString(span.posix)
			continue
		}
		fmt.Fprintf(&b, "%X", span.lo)
		if span.hi != span.lo {
			fmt.Fprintf(&b, "-%X", span.hi)
		}
	}
	b.WriteString("]]")
	return b.String()
}

func searchGlobHasPrebuiltClass(pattern string) bool {
	return strings.Contains(pattern, "[[#g:")
}

// searchGlobWindowsClassFold reports a character class whose bytes change
// under strings.ToLower. The basename matcher lowercases the whole pattern
// before filepath.Match, so [0-A] becomes [0-a] and [A-\] becomes the
// backwards range [a-\]. Git folds the text and leaves the class bounds
// alone. The path regex keeps those bounds and folds with (?i).
func searchGlobWindowsClassFold(pattern string) bool {
	if runtime.GOOS != "windows" || !strings.Contains(pattern, "[") {
		return false
	}
	for i := 0; i < len(pattern); {
		if pattern[i] != '[' {
			i++
			continue
		}
		end, ok := searchGlobClassEnd(pattern, i)
		if !ok {
			i++
			continue
		}
		if strings.ToLower(pattern[i:end+1]) != pattern[i:end+1] {
			return true
		}
		i = end + 1
	}
	return false
}

// searchGlobPrebuiltClass reads a [[#g:...]] class emitted for an escaped
// gitignore range. ok is false when the marker is absent or the hex is bad.
func searchGlobPrebuiltClass(pattern string, open int) (string, int, bool) {
	const marker = "[[#g:"
	if open < 0 || open >= len(pattern) || !strings.HasPrefix(pattern[open:], marker) {
		return "", 0, false
	}
	rest := pattern[open+len(marker):]
	rel := strings.Index(rest, "]]")
	if rel < 0 {
		return "", 0, false
	}
	body := rest[:rel]
	closeIdx := open + len(marker) + rel + 1
	neg := false
	if strings.HasPrefix(body, "!") || strings.HasPrefix(body, "^") {
		neg = true
		body = body[1:]
	}
	var b strings.Builder
	if body != "" {
		for _, part := range strings.Split(body, ",") {
			if strings.HasPrefix(part, "@") {
				// An unknown [:name:] aborts the class. Negation does not
				// turn that abort into a match.
				name := part[1:]
				posix, known := searchGlobPosixSpans(name)
				if name == "" || !known {
					return searchGlobNeverMatch, closeIdx, true
				}
				for _, sp := range posix {
					appendSearchGlobClassSpan(&b, sp[0], sp[1])
				}
				continue
			}
			lo, hi, ok := parseGitignoreHexSpan(part)
			if !ok {
				return "", 0, false
			}
			if lo > hi {
				hi = lo
			}
			appendSearchGlobClassSpan(&b, lo, hi)
		}
	}
	expr := b.String()
	if expr == "" {
		if neg {
			return "[^/]", closeIdx, true
		}
		return searchGlobNeverMatch, closeIdx, true
	}
	if neg {
		return "[^/" + expr + "]", closeIdx, true
	}
	return "[" + expr + "]", closeIdx, true
}

func parseGitignoreHexSpan(part string) (lo, hi rune, ok bool) {
	loPart, hiPart, isRange := strings.Cut(part, "-")
	loU, err := strconv.ParseUint(loPart, 16, 32)
	if err != nil || loPart == "" {
		return 0, 0, false
	}
	if !isRange {
		return rune(loU), rune(loU), true
	}
	hiU, err := strconv.ParseUint(hiPart, 16, 32)
	if err != nil || hiPart == "" {
		return 0, 0, false
	}
	return rune(loU), rune(hiU), true
}

// treeSearchIgnore carries parent and nested gitignore rules for one search.
// Patterns stay relative to the ignore file's directory. They are not copied
// into the corpus key, so a nested edit is visible on the next query.
type treeSearchIgnore struct {
	root    string
	parents []parentIgnoreScope
	cache   map[string]string
}

type parentIgnoreScope struct {
	prepend    string
	pattern    string
	dirPattern string
}

type treeSearchIgnoreKey struct{}

func withTreeSearchIgnore(ctx context.Context, base string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := filepath.Abs(base)
	if err != nil {
		root = base
	}
	root = filepath.Clean(root)
	if info, statErr := os.Stat(root); statErr == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	state := &treeSearchIgnore{
		root:    root,
		parents: parentIgnoreScopes(root),
		cache:   map[string]string{},
	}
	return context.WithValue(ctx, treeSearchIgnoreKey{}, state)
}

func treeSearchIgnoreFrom(ctx context.Context) *treeSearchIgnore {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(treeSearchIgnoreKey{}).(*treeSearchIgnore)
	return state
}

// searchDirHasGit reports a repository boundary. A .git directory is a normal
// checkout. A .git file is a worktree or submodule gitlink. Parent ignore
// rules stop there: walking through the file either attached the outer
// repository or, when no parent .git directory existed, dropped this
// repository's own .gitignore.
func searchDirHasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// searchGitInfoExcludeFile is the info/exclude file for this work tree.
// Patterns in it are relative to the work tree, but the file does not always
// live at .git/info/exclude. A checkout's .git is that directory. A symlink
// or junction to a git directory follows to the same place. A worktree or
// submodule .git file names the git directory. A linked worktree then keeps
// info/exclude in the common git directory named by commondir, not in the
// per-worktree git directory.
func searchGitInfoExcludeFile(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "info", "exclude")
	}
	if !info.Mode().IsRegular() {
		return ""
	}
	return gitfileInfoExclude(dir, gitPath)
}

// gitfileInfoExclude reads the gitdir line from a .git file. The path is
// relative to the work tree that contains the file. info/exclude is opened
// under the common git directory when commondir says the per-worktree git
// directory is not the common one.
func gitfileInfoExclude(dir, gitFile string) string {
	line, err := readGitPointerLine(gitFile)
	if err != nil || line == "" {
		return ""
	}
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	gitDir := resolveGitDir(dir, strings.TrimSpace(line[len(prefix):]))
	if gitDir == "" {
		return ""
	}
	common, ok := commonGitDir(gitDir)
	if !ok {
		return ""
	}
	return filepath.Join(common, "info", "exclude")
}

// commonGitDir returns the git directory that holds info/exclude. A linked
// worktree's git directory contains commondir, and that path is relative to
// the git directory. A missing commondir means this git directory is already
// the common one, as with a submodule. A commondir that does not name a
// directory is not replaced with the per-worktree info/exclude: git does not
// read that file.
func commonGitDir(gitDir string) (string, bool) {
	line, err := readGitPointerLine(filepath.Join(gitDir, "commondir"))
	if err != nil {
		if os.IsNotExist(err) {
			return gitDir, true
		}
		return "", false
	}
	resolved := resolveGitDir(gitDir, line)
	if resolved == "" {
		return "", false
	}
	return resolved, true
}

// resolveGitDir cleans a gitdir or commondir path and requires it to be a
// directory. A relative path is joined to base: the work tree for a gitfile,
// the git directory for commondir.
func resolveGitDir(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(base, raw)
	}
	raw = filepath.Clean(raw)
	info, err := os.Stat(raw)
	if err != nil || !info.IsDir() {
		return ""
	}
	return raw
}

// readGitPointerLine reads the first line of a gitfile or commondir. Those
// files are one short path. A larger file named .git is not a pointer.
func readGitPointerLine(path string) (string, error) {
	if path != "" {
		if data, err, ok := loadFreshIgnoreFile(searchPathKey(path)); ok {
			if err != nil {
				return "", err
			}
			return gitPointerLineFrom(data)
		}
	}
	// A multi-megabyte file named .git is not a pointer. Read at most 4096
	// bytes and do not keep them in the ignore cache.
	stamp := stampIgnorePath(path)
	if stamp.ok && stamp.size > 4096 {
		data, err := readFilePrefix(path, 4096)
		if err != nil {
			return "", err
		}
		return gitPointerLineFrom(data)
	}
	data, err := readStampCachedFile(path)
	if err != nil {
		return "", err
	}
	return gitPointerLineFrom(data)
}

func gitPointerLineFrom(data []byte) (string, error) {
	if len(data) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	if len(data) > 4096 {
		data = data[:4096]
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		data = data[:i]
	}
	return strings.TrimSpace(string(data)), nil
}

func readFilePrefix(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if n == 0 {
		if err == nil || err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf[:n], nil
}

type ignoreFileEntry struct {
	data  []byte
	err   error
	stamp ignoreStamp
}

// ignoreFileReads counts ignore-file reads. A stamp hit does not increment it.
var ignoreFileReads atomic.Uint64

var ignoreFileCache = struct {
	sync.Mutex
	byPath map[string]ignoreFileEntry
}{
	byPath: map[string]ignoreFileEntry{},
}

// readStampCachedFile returns an ignore file's bytes. A missing file stays
// missing until its stamp changes, and only when the stamp already said it
// was missing. A delete between the stamp and the read is not cached under
// the old mtime. Other read errors are not cached. A file larger than
// MaxSearchFileSize is read and not kept.
func readStampCachedFile(path string) ([]byte, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	key := searchPathKey(path)
	if data, err, ok := loadFreshIgnoreFile(key); ok {
		return data, err
	}
	ignoreFileReads.Add(1)
	// Stamp before the read. A rewrite in between fails the next stamp check
	// instead of being cached under the new mtime.
	stamp := stampIgnorePath(path)
	if stamp.ok && stamp.size > MaxSearchFileSize {
		return os.ReadFile(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !stamp.ok {
			storeIgnoreFile(key, nil, err, stamp)
		}
		return nil, err
	}
	stored := append([]byte(nil), data...)
	storeIgnoreFile(key, stored, nil, stamp)
	return stored, nil
}

func loadFreshIgnoreFile(key string) ([]byte, error, bool) {
	ignoreFileCache.Lock()
	entry, ok := ignoreFileCache.byPath[key]
	ignoreFileCache.Unlock()
	if !ok || !ignoreStampsMatch([]ignoreStamp{entry.stamp}) {
		return nil, nil, false
	}
	return entry.data, entry.err, true
}

func storeIgnoreFile(key string, data []byte, err error, stamp ignoreStamp) {
	if key == "" || stamp.path == "" {
		return
	}
	ignoreFileCache.Lock()
	defer ignoreFileCache.Unlock()
	ignoreFileCache.byPath[key] = ignoreFileEntry{data: data, err: err, stamp: stamp}
	for len(ignoreFileCache.byPath) > 1024 {
		if !dropOneIgnoreCacheEntry(ignoreFileCache.byPath, key) {
			return
		}
	}
}

func dropOneIgnoreCacheEntry[K comparable, V any](entries map[K]V, keep K) bool {
	for item := range entries {
		if item == keep {
			continue
		}
		delete(entries, item)
		return true
	}
	return false
}

type ignoreStamp struct {
	path string
	mod  time.Time
	size int64
	ok   bool
}

type parentIgnoreCacheEntry struct {
	scopes []parentIgnoreScope
	stamps []ignoreStamp
}

// parentIgnoreCollects counts walks that read ignore files. A cache hit does
// not increment it.
var parentIgnoreCollects atomic.Uint64

var parentIgnoreCache = struct {
	sync.Mutex
	byRoot map[string]parentIgnoreCacheEntry
}{
	byRoot: map[string]parentIgnoreCacheEntry{},
}

// parentIgnoreScopes reads ignore files above the search root, stopping at the
// git root. A search that is not inside a repository does not walk to the
// volume root's rules. The search root's own files are already in the exclude
// pattern. The parsed scopes are reused while every .git and ignore file that
// produced them still has the same size and mtime.
func parentIgnoreScopes(root string) []parentIgnoreScope {
	root = filepath.Clean(root)
	key := searchPathKey(root)
	if scopes, ok := loadFreshParentIgnore(key); ok {
		return scopes
	}
	scopes, stamps := collectParentIgnoreScopes(root)
	storeParentIgnore(key, scopes, stamps)
	return cloneParentIgnoreScopes(scopes)
}

func loadFreshParentIgnore(key string) ([]parentIgnoreScope, bool) {
	parentIgnoreCache.Lock()
	entry, ok := parentIgnoreCache.byRoot[key]
	parentIgnoreCache.Unlock()
	if !ok || !ignoreStampsMatch(entry.stamps) {
		return nil, false
	}
	return cloneParentIgnoreScopes(entry.scopes), true
}

func storeParentIgnore(key string, scopes []parentIgnoreScope, stamps []ignoreStamp) {
	if key == "" || len(stamps) == 0 {
		return
	}
	parentIgnoreCache.Lock()
	defer parentIgnoreCache.Unlock()
	parentIgnoreCache.byRoot[key] = parentIgnoreCacheEntry{
		scopes: cloneParentIgnoreScopes(scopes),
		stamps: append([]ignoreStamp(nil), stamps...),
	}
	for len(parentIgnoreCache.byRoot) > 1024 {
		if !dropOneIgnoreCacheEntry(parentIgnoreCache.byRoot, key) {
			return
		}
	}
}

func cloneParentIgnoreScopes(scopes []parentIgnoreScope) []parentIgnoreScope {
	if len(scopes) == 0 {
		return nil
	}
	out := make([]parentIgnoreScope, len(scopes))
	copy(out, scopes)
	return out
}

func stampIgnorePath(path string) ignoreStamp {
	info, err := os.Lstat(path)
	if err != nil {
		return ignoreStamp{path: path}
	}
	return ignoreStamp{path: path, ok: true, size: info.Size(), mod: info.ModTime()}
}

func ignoreStampsMatch(stamps []ignoreStamp) bool {
	if len(stamps) == 0 {
		return false
	}
	for _, stamp := range stamps {
		info, err := os.Lstat(stamp.path)
		if err != nil {
			if stamp.ok {
				return false
			}
			continue
		}
		if !stamp.ok || info.Size() != stamp.size || !info.ModTime().Equal(stamp.mod) {
			return false
		}
	}
	return true
}

func collectParentIgnoreScopes(root string) ([]parentIgnoreScope, []ignoreStamp) {
	parentIgnoreCollects.Add(1)
	var stamps []ignoreStamp
	add := func(path string) {
		stamps = append(stamps, stampIgnorePath(path))
	}
	add(filepath.Join(root, ".git"))
	if searchDirHasGit(root) {
		return nil, stamps
	}
	var dirs []string
	dir := root
	for i := 0; i < 32; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dirs = append(dirs, parent)
		add(filepath.Join(parent, ".git"))
		if searchDirHasGit(parent) {
			break
		}
		dir = parent
	}
	if len(dirs) == 0 || !searchDirHasGit(dirs[len(dirs)-1]) {
		return nil, stamps
	}
	var scopes []parentIgnoreScope
	for _, dir := range dirs {
		rel, err := filepath.Rel(dir, root)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		add(filepath.Join(dir, ".gitignore"))
		add(filepath.Join(dir, ".ignore"))
		for _, extra := range gitPointerStampPaths(dir) {
			add(extra)
		}
		infoExclude := searchGitInfoExcludeFile(dir)
		if infoExclude != "" {
			add(infoExclude)
		}
		files, dirPatterns := readDirDirectIgnorePatterns(dir, infoExclude)
		if files == "" && dirPatterns == "" {
			continue
		}
		scopes = append(scopes, parentIgnoreScope{
			prepend:    filepath.ToSlash(rel),
			pattern:    files,
			dirPattern: dirPatterns,
		})
	}
	return scopes, stamps
}

// gitPointerStampPaths is the commondir file a gitfile worktree reads while
// resolving info/exclude. The .git file itself is stamped by the caller.
func gitPointerStampPaths(dir string) []string {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return nil
	}
	line, err := readGitPointerLine(gitPath)
	if err != nil {
		return nil
	}
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return nil
	}
	gitDir := resolveGitDir(dir, strings.TrimSpace(line[len(prefix):]))
	if gitDir == "" {
		return nil
	}
	return []string{filepath.Join(gitDir, "commondir")}
}

func readDirIgnorePattern(dir, infoExcludeFile string) string {
	names := []string{
		filepath.Join(dir, ".gitignore"),
		filepath.Join(dir, ".ignore"),
	}
	if infoExcludeFile != "" {
		names = append(names, infoExcludeFile)
	}
	var patterns []string
	for _, name := range names {
		data, err := readStampCachedFile(name)
		if err != nil {
			continue
		}
		filePatterns, ok := gitignoreExcludePatterns(string(data))
		if !ok {
			continue
		}
		patterns = append(patterns, filePatterns...)
	}
	return joinSearchIgnorePatterns(patterns)
}

// readDirDirectIgnorePatterns reads parent ignore files as direct matches.
// "src/" stays a directory rule. The search-root contents expansion would
// hide every file when the user asked to search inside that directory.
func readDirDirectIgnorePatterns(dir, infoExcludeFile string) (files, dirs string) {
	names := []string{
		filepath.Join(dir, ".gitignore"),
		filepath.Join(dir, ".ignore"),
	}
	if infoExcludeFile != "" {
		names = append(names, infoExcludeFile)
	}
	var filePatterns, dirPatterns []string
	for _, name := range names {
		data, err := readStampCachedFile(name)
		if err != nil {
			continue
		}
		fileRules, dirRules, ok := gitignoreDirectPatterns(string(data))
		if !ok {
			continue
		}
		filePatterns = append(filePatterns, fileRules...)
		dirPatterns = append(dirPatterns, dirRules...)
	}
	return joinSearchIgnorePatterns(filePatterns), joinSearchIgnorePatterns(dirPatterns)
}

func joinSearchIgnorePatterns(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	// A leading NUL keeps spaces inside each gitignore line.
	return "\x00" + strings.Join(patterns, "\x00")
}

func (state *treeSearchIgnore) cachedDirPattern(relDir string) string {
	key := relDir
	if runtime.GOOS == "windows" {
		key = strings.ToLower(relDir)
	}
	if pattern, ok := state.cache[key]; ok {
		return pattern
	}
	// A directory below the search root can be its own repository. Its
	// info/exclude is relative to that directory, and the same gitdir and
	// commondir resolution used at the search root applies here.
	dir := filepath.Join(state.root, filepath.FromSlash(relDir))
	pattern := readDirIgnorePattern(dir, searchGitInfoExcludeFile(dir))
	state.cache[key] = pattern
	return pattern
}

// treeIgnoreExcludes reports paths ignored by a nested or parent ignore file.
// rel is relative to the search root. No state means no_ignore or a direct
// walk that did not opt in.
func treeIgnoreExcludes(ctx context.Context, rel string, isDir bool) (bool, error) {
	state := treeSearchIgnoreFrom(ctx)
	if state == nil {
		return false, nil
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return false, nil
	}
	for _, scope := range state.parents {
		excluded, err := parentScopeExcludes(scope, rel, isDir)
		if err != nil || excluded {
			return excluded, err
		}
	}
	dir := path.Dir(rel)
	for dir != "." && dir != "/" && dir != "" {
		prefix := dir + "/"
		if !strings.HasPrefix(rel, prefix) || len(rel) <= len(prefix) {
			break
		}
		pattern := state.cachedDirPattern(dir)
		if pattern != "" {
			matched, err := matchGlob(pattern, rel[len(prefix):], isDir)
			if err != nil || matched {
				return matched, err
			}
		}
		next := path.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return false, nil
}

// parentScopeExcludes applies one parent ignore file. Directory rules match
// the directory, not every file under a directory the user named. File rules
// still match, and a directory below the search root still hides its contents.
func parentScopeExcludes(scope parentIgnoreScope, rel string, isDir bool) (bool, error) {
	full := scope.prepend + "/" + rel
	if excluded, err := matchIgnoreList(scope.pattern, full); err != nil || excluded {
		return excluded, err
	}
	if isDir {
		if excluded, err := matchIgnoreList(scope.dirPattern, full); err != nil || excluded {
			return excluded, err
		}
	}
	ancestor := path.Dir(rel)
	for ancestor != "." && ancestor != "/" && ancestor != "" {
		if !strings.HasPrefix(rel, ancestor+"/") {
			break
		}
		full = scope.prepend + "/" + ancestor
		if excluded, err := matchIgnoreList(scope.pattern, full); err != nil || excluded {
			return excluded, err
		}
		if excluded, err := matchIgnoreList(scope.dirPattern, full); err != nil || excluded {
			return excluded, err
		}
		next := path.Dir(ancestor)
		if next == ancestor {
			break
		}
		ancestor = next
	}
	return false, nil
}

func matchIgnoreList(pattern, rel string) (bool, error) {
	if pattern == "" {
		return false, nil
	}
	// isDir is false so a directory path is the path itself. Directory-only
	// rules are stored separately and are not applied to files.
	return matchGlob(pattern, rel, false)
}

func dropTreeIgnoredPaths(ctx context.Context, root string, paths []string) ([]string, error) {
	if treeSearchIgnoreFrom(ctx) == nil || len(paths) == 0 {
		return paths, nil
	}
	out := make([]string, 0, len(paths))
	for _, filePath := range paths {
		rel, ok := searchIndexRelativePath(root, filePath)
		if !ok {
			continue
		}
		excluded, err := treeIgnoreExcludes(ctx, rel, false)
		if err != nil {
			return nil, err
		}
		if !excluded {
			out = append(out, filePath)
		}
	}
	return out, nil
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
	return walkSearchFilesFilteredCtx(context.Background(), base, pattern, excludePattern, fileType, includeDirs, includeHidden, visit)
}

func walkSearchFilesFilteredCtx(ctx context.Context, base, pattern, excludePattern, fileType string, includeDirs, includeHidden bool, visit func(string, fs.DirEntry) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(base)
	if err != nil {
		return fmt.Errorf("路径不存在或无法访问: %w", err)
	}
	if !info.IsDir() {
		if !matchesSearchFilters(filepath.Base(base), false, pattern, excludePattern, fileType) {
			return nil
		}
		if excluded, ignoreErr := treeIgnoreExcludes(ctx, filepath.Base(base), false); ignoreErr != nil {
			return ignoreErr
		} else if excluded {
			return nil
		}
		err := visit(base, fileInfoDirEntry{info: info})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	deadline := time.Now().Add(maxSearchWalkTime)
	filesVisited := 0
	return walkDirCtx(ctx, base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Check deadline every 512 entries to avoid per-file syscall overhead.
		filesVisited++
		if filesVisited&511 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if time.Now().After(deadline) {
				return errSearchWalkDeadline
			}
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
		if excluded, ignoreErr := treeIgnoreExcludes(ctx, rel, d.IsDir()); ignoreErr != nil {
			return ignoreErr
		} else if excluded {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && !includeDirs {
			return nil
		}
		if !matchesSearchFilters(rel, d.IsDir(), pattern, excludePattern, fileType) {
			return nil
		}
		err = visit(path, d)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	})
}

// readSearchDirBatch is the directory listing call used by walkDirCtx.
// A batch can contain entries and a non-EOF error together; callers must
// visit the entries before treating the directory as unread.
var readSearchDirBatch = func(dir *os.File, n int) ([]os.DirEntry, error) {
	return dir.ReadDir(n)
}

// walkDirCtx reads directories in small batches so cancellation is not blocked by filepath.WalkDir's full-directory ReadDir.
func walkDirCtx(ctx context.Context, root string, walkFn fs.WalkDirFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return walkFn(root, nil, err)
	}
	// A Windows junction is ModeIrregular, so Lstat does not report it as a
	// directory and a search of that path used to return nothing. Open follows
	// the junction when this root is the directory the caller named. A junction
	// found later is left as ReadDir reported it, so the walk does not descend.
	if info.Mode()&os.ModeIrregular != 0 {
		if followed, serr := os.Stat(root); serr == nil && followed.IsDir() {
			info = followed
		}
	}
	return walkDirCtxEntry(ctx, root, fileInfoDirEntry{info: info}, walkFn)
}

func walkDirCtxEntry(ctx context.Context, path string, d fs.DirEntry, walkFn fs.WalkDirFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := walkFn(path, d, nil)
	if err != nil || !d.IsDir() {
		switch err {
		case nil, filepath.SkipDir:
			return nil
		case filepath.SkipAll:
			return filepath.SkipAll
		default:
			return err
		}
	}
	dir, err := os.Open(path)
	if err != nil {
		return walkFn(path, d, err)
	}
	defer dir.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := readSearchDirBatch(dir, 128)
		if len(entries) > 0 {
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			for _, child := range entries {
				childPath := filepath.Join(path, child.Name())
				childErr := walkDirCtxEntry(ctx, childPath, child, walkFn)
				switch childErr {
				case nil:
				case filepath.SkipDir:
					continue
				case filepath.SkipAll:
					return filepath.SkipAll
				default:
					return childErr
				}
			}
		}
		if err == io.EOF || (err == nil && len(entries) == 0) {
			return nil
		}
		if err == nil {
			continue
		}
		if walkErr := walkFn(path, d, err); walkErr != nil && walkErr != filepath.SkipDir {
			return walkErr
		}
		return nil
	}
}
func matchesSearchFilters(rel string, isDir bool, globPattern, excludePattern, fileType string) bool {
	if excludePattern != "" {
		matched, err := matchGlob(excludePattern, rel, isDir)
		if err != nil || matched {
			return false
		}
		// The walk SkipDir's an excluded directory and never sees the files
		// inside. A corpus built without this exclude still lists them, so
		// the file filter has to apply that directory match too.
		if !isDir {
			covered, coverErr := excludeCoversAncestor(excludePattern, rel)
			if coverErr != nil || covered {
				return false
			}
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

// excludeCoversAncestor reports that a parent of rel matches exclude as a
// directory. matchGlob adds the trailing slash only when isDir is set, so a
// pattern such as drafts/ skips the directory on the walk and does not match
// drafts/sub/kept.go as a file.
func excludeCoversAncestor(exclude, rel string) (bool, error) {
	rel = filepath.ToSlash(rel)
	for {
		slash := strings.LastIndex(rel, "/")
		if slash <= 0 {
			return false, nil
		}
		rel = rel[:slash]
		matched, err := matchGlob(exclude, rel, true)
		if err != nil || matched {
			return matched, err
		}
	}
}

func matchGlob(pattern, rel string, isDir bool) (bool, error) {
	patterns, err := cachedSearchGlobPatterns(pattern)
	if err != nil {
		return false, err
	}
	if len(patterns) == 0 {
		return true, nil
	}
	rel = filepath.ToSlash(rel)
	if isDir {
		rel = strings.TrimSuffix(rel, "/") + "/"
	}
	for _, pattern := range patterns {
		if pattern.match(rel) {
			return true, nil
		}
	}
	return false, nil
}

type compiledSearchGlob struct {
	pattern    string
	matchAll   bool
	simpleBase bool
	re         *regexp.Regexp
}

type compiledSearchGlobEntry struct {
	patterns []compiledSearchGlob
	err      error
}

var searchGlobPatternCache = struct {
	sync.RWMutex
	byPattern map[string]compiledSearchGlobEntry
	order     []string
}{
	byPattern: make(map[string]compiledSearchGlobEntry),
}

func cachedSearchGlobPatterns(pattern string) ([]compiledSearchGlob, error) {
	key := searchGlobCacheKey(pattern)
	searchGlobPatternCache.RLock()
	if entry, ok := searchGlobPatternCache.byPattern[key]; ok {
		searchGlobPatternCache.RUnlock()
		return entry.patterns, entry.err
	}
	searchGlobPatternCache.RUnlock()

	patterns, err := compileSearchGlobPatterns(key)
	entry := compiledSearchGlobEntry{patterns: patterns, err: err}
	searchGlobPatternCache.Lock()
	if existing, ok := searchGlobPatternCache.byPattern[key]; ok {
		searchGlobPatternCache.Unlock()
		return existing.patterns, existing.err
	}
	if len(searchGlobPatternCache.byPattern) >= maxSearchGlobPatternCacheEntries && len(searchGlobPatternCache.order) > 0 {
		delete(searchGlobPatternCache.byPattern, searchGlobPatternCache.order[0])
		searchGlobPatternCache.order = searchGlobPatternCache.order[1:]
	}
	searchGlobPatternCache.byPattern[key] = entry
	searchGlobPatternCache.order = append(searchGlobPatternCache.order, key)
	searchGlobPatternCache.Unlock()
	return entry.patterns, entry.err
}

// searchGlobCacheKey trims a user glob. A NUL starts gitignore lines, and an
// escaped trailing space on the last of those lines is part of the name.
// TrimSpace on the whole string deletes that space.
func searchGlobCacheKey(pattern string) string {
	if i := strings.IndexByte(pattern, 0); i >= 0 {
		return strings.TrimSpace(pattern[:i]) + pattern[i:]
	}
	return strings.TrimSpace(pattern)
}

func compileSearchGlobPatterns(pattern string) ([]compiledSearchGlob, error) {
	raw := expandSearchGlobPatterns(pattern)
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]compiledSearchGlob, 0, len(raw))
	for _, pattern := range raw {
		slashed := searchGlobToSlash(pattern)
		if strings.HasPrefix(slashed, "**/") {
			compiled, err := compileSingleSearchGlob(strings.TrimPrefix(slashed, "**/"))
			if err != nil {
				return nil, err
			}
			out = append(out, compiled)
		}
		compiled, err := compileSingleSearchGlob(pattern)
		if err != nil {
			return nil, err
		}
		out = append(out, compiled)
	}
	return out, nil
}

func compileSingleSearchGlob(pattern string) (compiledSearchGlob, error) {
	pattern = searchGlobToSlash(pattern)
	rootAnchored := strings.HasPrefix(pattern, "./")
	if rootAnchored {
		pattern = strings.TrimPrefix(pattern, "./")
	} else if trimmed := strings.TrimLeft(pattern, "/"); trimmed != "" && trimmed != pattern {
		// A leading slash anchors at the search root. Relative paths have
		// no leading slash, so /*.go is the root file, not a literal slash.
		rootAnchored = true
		pattern = trimmed
	}
	compiled := compiledSearchGlob{pattern: pattern}
	if pattern == "" || pattern == "**" || pattern == "**/*" {
		compiled.matchAll = true
		return compiled, nil
	}
	if !rootAnchored && !strings.Contains(pattern, "/") {
		if _, err := filepath.Match(pattern, ""); err != nil {
			// An unclosed '[' is a literal, same as a path glob. Failing the
			// whole pattern list would drop every file for one bad exclude.
			compiledRegexp, rerr := regexp.Compile(searchGlobRegexp(pattern))
			if rerr != nil {
				return compiledSearchGlob{}, err
			}
			compiled.simpleBase = true
			compiled.re = compiledRegexp
			return compiled, nil
		}
		compiled.simpleBase = true
		// filepath.Match treats '!' and '^' in [] as members, closes a class at
		// the first ']', and matches nothing for a backwards range such as
		// [z-a] without returning an error. A **/ prefix ORs this bare pattern
		// with the path regex, so those classes have to use that regex here too.
		if searchGlobHasNegatedClass(pattern) || searchGlobHasPosixClass(pattern) || searchGlobHasBackwardsRange(pattern) || searchGlobHasPrebuiltClass(pattern) || searchGlobWindowsClassFold(pattern) {
			compiledRegexp, err := regexp.Compile(searchGlobRegexp(pattern))
			if err != nil {
				return compiledSearchGlob{}, err
			}
			compiled.re = compiledRegexp
		}
		return compiled, nil
	}
	re := searchGlobRegexp(pattern)
	compiledRegexp, err := regexp.Compile(re)
	if err != nil {
		return compiledSearchGlob{}, err
	}
	compiled.re = compiledRegexp
	return compiled, nil
}

func (pattern compiledSearchGlob) match(rel string) bool {
	if pattern.matchAll {
		return true
	}
	if pattern.simpleBase {
		base := filepath.Base(rel)
		if pattern.re != nil {
			return pattern.re.MatchString(base)
		}
		pat := pattern.pattern
		// filepath.Match compares bytes. Windows search is case-insensitive,
		// matching the (?i) path-glob regex.
		if runtime.GOOS == "windows" {
			base = strings.ToLower(base)
			pat = strings.ToLower(pat)
		}
		matched, err := filepath.Match(pat, base)
		return err == nil && matched
	}
	return pattern.re != nil && pattern.re.MatchString(rel)
}

// searchGlobRegexp compiles a path glob. ** crosses directories only when it
// is a whole path segment (leading **/, a/**/b, or trailing /**). Any other
// ** is two single-segment stars. [abc] is a character class. [!abc] is
// negation and does not match '/'. On Windows the match is case-insensitive.
func searchGlobRegexp(pattern string) string {
	var b strings.Builder
	if runtime.GOOS == "windows" {
		b.WriteString("(?i)")
	}
	b.WriteByte('^')
	for i := 0; i < len(pattern); {
		if n, expr, ok := searchGlobStarSegment(pattern, i); ok {
			b.WriteString(expr)
			i += n
			continue
		}
		switch pattern[i] {
		case '*':
			b.WriteString(`[^/]*`)
			i++
		case '?':
			b.WriteString(`[^/]`)
			i++
		case '[':
			if class, next, ok := searchGlobCharacterClass(pattern, i); ok {
				b.WriteString(class)
				i = next
				continue
			}
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			i++
		default:
			j := i + 1
			for j < len(pattern) && pattern[j] != '*' && pattern[j] != '?' && pattern[j] != '[' {
				j++
			}
			b.WriteString(regexp.QuoteMeta(pattern[i:j]))
			i = j
		}
	}
	b.WriteByte('$')
	return b.String()
}

// searchGlobStarSegment matches a path component that is only asterisks,
// at least two of them. ** and *** are the same globstar: zero or more
// directories. The returned n includes the slash after that component.
// A run that is not its own component, such as src/**.go or foo**bar, is
// not a globstar.
func searchGlobStarSegment(pattern string, i int) (n int, expr string, ok bool) {
	if i >= len(pattern) || pattern[i] != '*' {
		return 0, "", false
	}
	if i > 0 && pattern[i-1] != '/' {
		return 0, "", false
	}
	j := i
	for j < len(pattern) && pattern[j] == '*' {
		j++
	}
	if j-i < 2 {
		return 0, "", false
	}
	if j >= len(pattern) {
		return j - i, ".*", true
	}
	if pattern[j] == '/' {
		return j - i + 1, "(?:.*/)?", true
	}
	return 0, "", false
}

// searchGlobHasNegatedClass reports a class that searchGlobCharacterClass
// negates. [!] and [^] are literal marks. A ']' immediately after the mark
// is a member, so [!]a] and [^]a] still negate. The old scan treated that
// ']' as "not a bang class" and, for '^', never looked at all, so a bare
// glob and the **/ OR kept filepath.Match's literal member.
func searchGlobHasNegatedClass(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '[' {
			continue
		}
		class, next, ok := searchGlobCharacterClass(pattern, i)
		if !ok {
			continue
		}
		// Negated classes are "[^/" + body + "]". A normal class is "[" +
		// hex escapes, so it does not start with "[^".
		if strings.HasPrefix(class, "[^") {
			return true
		}
		if next > i {
			i = next - 1
		}
	}
	return false
}

// searchGlobHasPosixClass reports a [: that might be a POSIX class.
// filepath.Match ends a class at the first ']', so [[:digit:]] would match a
// member of [:digit: plus a literal ']'. An unclosed foo[:bar still compiles:
// the '[' is quoted and the name stays literal.
func searchGlobHasPosixClass(pattern string) bool {
	return strings.Contains(pattern, "[:")
}

// searchGlobHasBackwardsRange reports a class range whose start sorts after
// its end. filepath.Match accepts [z-a] and then matches nothing. The start
// character is still a member.
func searchGlobHasBackwardsRange(pattern string) bool {
	for i := 0; i < len(pattern); {
		if pattern[i] != '[' {
			i++
			continue
		}
		end, ok := searchGlobClassEnd(pattern, i)
		if !ok {
			i++
			continue
		}
		body := i + 1
		if body < end && (pattern[body] == '!' || pattern[body] == '^') {
			body++
		}
		if searchGlobBodyHasBackwardsRange(pattern[body:end]) {
			return true
		}
		i = end + 1
	}
	return false
}

func searchGlobBodyHasBackwardsRange(body string) bool {
	runes := []rune(body)
	var prev rune
	havePrev := false
	for i := 0; i < len(runes); {
		if runes[i] == '[' && i+1 < len(runes) && runes[i+1] == ':' {
			tail := string(runes[i:])
			if _, next, ok := searchGlobPosixClass(tail, 0); ok {
				i += len([]rune(tail[:next]))
				// A POSIX class clears the previous member. [[:digit:]-9]
				// is the digits, '-', and '9', not a range.
				havePrev = false
				continue
			}
		}
		// A slash is not a matchable member, but it is the previous character
		// of a range. [a/-.] runs from '/' to '.' and is backwards.
		if runes[i] == '/' {
			prev = '/'
			havePrev = true
			i++
			continue
		}
		// A '-' with a previous member and another character after it is a
		// range, including when that character is '-'. [a--b] runs from a to '-'.
		if runes[i] == '-' && havePrev && i+1 < len(runes) {
			if prev > runes[i+1] {
				return true
			}
			havePrev = false
			i += 2
			continue
		}
		prev = runes[i]
		havePrev = true
		i++
	}
	return false
}

// searchGlobNeverMatch matches no text. An unknown [:name:] aborts the whole
// pattern, including a file whose name is the pattern itself.
const searchGlobNeverMatch = `[^\x{0}-\x{10FFFF}]`

// searchGlobCharacterClass reads a [abc], [!abc], or [[:digit:]] class.
// A [:name:] token is one member, so its ']' does not close the class.
// An unknown name matches nothing. A '/' outside that token is not a member:
// [a/b] matches a or b, and the slash still anchors the pattern. The slash
// stays the previous character of a range, so [a/-b] is a plus '/' through b.
// A class with no member left matches nothing. A malformed class returns
// ok=false so the '[' stays literal.
func searchGlobCharacterClass(pattern string, start int) (string, int, bool) {
	if expr, end, ok := searchGlobPrebuiltClass(pattern, start); ok {
		return expr, end + 1, true
	}
	end, ok := searchGlobClassEnd(pattern, start)
	if !ok {
		return "", 0, false
	}
	i := start + 1
	if i >= len(pattern) {
		return "", 0, false
	}
	neg := false
	if pattern[i] == '^' || pattern[i] == '!' {
		neg = true
		i++
	}
	if i >= end {
		return "", 0, false
	}
	expr, kind := searchGlobClassBody(pattern[i:end])
	switch kind {
	case searchGlobClassNever:
		return searchGlobNeverMatch, end + 1, true
	case searchGlobClassSlashOnly:
		// [/] has no matchable member. [^/] is one character other than '/'.
		if neg {
			return "[^/]", end + 1, true
		}
		return searchGlobNeverMatch, end + 1, true
	case searchGlobClassOK:
		if neg {
			return "[^/" + expr + "]", end + 1, true
		}
		return "[" + expr + "]", end + 1, true
	default:
		return "", 0, false
	}
}

type searchGlobClassKind int

const (
	searchGlobClassInvalid searchGlobClassKind = iota
	searchGlobClassOK
	searchGlobClassNever
	searchGlobClassSlashOnly
)

func searchGlobClassBody(body string) (string, searchGlobClassKind) {
	var b strings.Builder
	runes := []rune(body)
	var prev rune
	havePrev := false
	for i := 0; i < len(runes); {
		if runes[i] == '[' && i+1 < len(runes) && runes[i+1] == ':' {
			tail := string(runes[i:])
			name, next, ok := searchGlobPosixClass(tail, 0)
			if ok {
				spans, known := searchGlobPosixSpans(name)
				if !known {
					return "", searchGlobClassNever
				}
				for _, sp := range spans {
					if !appendSearchGlobClassSpan(&b, sp[0], sp[1]) {
						return "", searchGlobClassInvalid
					}
				}
				i += len([]rune(tail[:next]))
				// A POSIX class clears the previous member. [[:digit:]-9]
				// is the digits, '-', and '9', not a range.
				havePrev = false
				continue
			}
		}
		r := runes[i]
		// '[', ']', and '\' are members. They are hex-escaped, so [[] is one
		// character. A slash is not a matchable member under pathname rules,
		// but it remains the previous character of a range: [a/-b] is a plus
		// the span from '/' to b. A range that crosses '/' is split below.
		if r == '/' {
			prev = '/'
			havePrev = true
			i++
			continue
		}
		// A '-' with a previous member and another character after it is a
		// range, including when that character is '-'. [a--b] is the empty
		// range a to '-' plus b. [a-] has no endpoint in the body, so a and
		// '-' both stay members. A backwards range adds nothing; its start
		// was already emitted, unless that start was '/'.
		if r == '-' && havePrev && i+1 < len(runes) {
			lo, hi := prev, runes[i+1]
			havePrev = false
			i += 2
			if lo > hi {
				continue
			}
			// A span that is only '/' adds no member. [/-/] matches nothing.
			appendSearchGlobClassSpan(&b, lo, hi)
			continue
		}
		if !appendSearchGlobClassSpan(&b, r, r) {
			return "", searchGlobClassInvalid
		}
		prev = r
		havePrev = true
		i++
	}
	if b.Len() == 0 {
		return "", searchGlobClassSlashOnly
	}
	return b.String(), searchGlobClassOK
}

// searchGlobPosixClass reads a [:name:] token starting at open.
// ok is set for every well-formed token, including an unknown name.
// next is the index after the token's ']'. Without ":]" the '[' is a member.
func searchGlobPosixClass(pattern string, open int) (name string, next int, ok bool) {
	if open < 0 || open >= len(pattern) || pattern[open] != '[' {
		return "", 0, false
	}
	if open+1 >= len(pattern) || pattern[open+1] != ':' {
		return "", 0, false
	}
	i := open + 2
	for i < len(pattern) && pattern[i] != ']' {
		i++
	}
	if i >= len(pattern) || pattern[i-1] != ':' {
		return "", 0, false
	}
	name = pattern[open+2 : i-1]
	if name == "" {
		return "", 0, false
	}
	return name, i + 1, true
}

// searchGlobPosixSpans returns the C-locale ASCII set for a POSIX class name.
// appendSearchGlobClassSpan drops '/' from graph and print.
func searchGlobPosixSpans(name string) ([][2]rune, bool) {
	switch name {
	case "alnum":
		return [][2]rune{{'0', '9'}, {'A', 'Z'}, {'a', 'z'}}, true
	case "alpha":
		return [][2]rune{{'A', 'Z'}, {'a', 'z'}}, true
	case "blank":
		return [][2]rune{{' ', ' '}, {'\t', '\t'}}, true
	case "cntrl":
		return [][2]rune{{0x00, 0x1F}, {0x7F, 0x7F}}, true
	case "digit":
		return [][2]rune{{'0', '9'}}, true
	case "graph":
		return [][2]rune{{0x21, 0x7E}}, true
	case "lower":
		return [][2]rune{{'a', 'z'}}, true
	case "print":
		return [][2]rune{{0x20, 0x7E}}, true
	case "punct":
		return [][2]rune{{0x21, 0x2E}, {0x3A, 0x40}, {0x5B, 0x60}, {0x7B, 0x7E}}, true
	case "space":
		return [][2]rune{{'\t', '\r'}, {' ', ' '}}, true
	case "upper":
		return [][2]rune{{'A', 'Z'}}, true
	case "xdigit":
		return [][2]rune{{'0', '9'}, {'A', 'F'}, {'a', 'f'}}, true
	default:
		return nil, false
	}
}

// appendSearchGlobClassSpan copies lo-hi into a regexp class and skips '/'.
// Endpoints are hex escapes so a dash or caret stays a member, not syntax.
// A span that contains only '/' returns false.
func appendSearchGlobClassSpan(b *strings.Builder, lo, hi rune) bool {
	if lo > hi {
		return false
	}
	wrote := false
	write := func(start, end rune) {
		if start > end {
			return
		}
		fmt.Fprintf(b, `\x{%X}`, start)
		if start != end {
			b.WriteByte('-')
			fmt.Fprintf(b, `\x{%X}`, end)
		}
		wrote = true
	}
	if hi < '/' || lo > '/' {
		write(lo, hi)
		return wrote
	}
	write(lo, '/'-1)
	write('/'+1, hi)
	return wrote
}
func expandSearchGlobPatterns(pattern string) []string {
	user, atomic := splitSearchGlobPatternList(pattern)
	out := expandSearchGlobTokens(user, true)
	return append(out, expandSearchGlobTokens(atomic, false)...)
}

// splitSearchGlobPatternList separates a user glob list from gitignore lines.
// User text splits on spaces. Patterns after a NUL are whole lines, so a
// name like "my notes.txt" or "foo,bar.txt" stays one glob.
func splitSearchGlobPatternList(pattern string) (user, atomic []string) {
	if pattern == "" {
		return nil, nil
	}
	if strings.HasPrefix(pattern, "\x00") {
		return nil, splitSearchGlobAtomic(pattern[1:])
	}
	if i := strings.IndexByte(pattern, 0); i >= 0 {
		return strings.Fields(strings.TrimSpace(pattern[:i])), splitSearchGlobAtomic(pattern[i+1:])
	}
	return strings.Fields(strings.TrimSpace(pattern)), nil
}

func splitSearchGlobAtomic(pattern string) []string {
	if pattern == "" {
		return nil
	}
	parts := strings.Split(pattern, "\x00")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		// Gitignore already trimmed unescaped trailing spaces. A remaining
		// space, as in "foo ", is part of the name.
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func expandSearchGlobTokens(raw []string, splitCommas bool) []string {
	var out []string
	for _, item := range raw {
		parts := []string{item}
		if splitCommas {
			parts = splitSearchGlobAlternatives(item)
		}
		for _, part := range parts {
			if splitCommas {
				part = strings.TrimSpace(part)
			}
			if part == "" {
				continue
			}
			out = append(out, expandSearchGlobBraces(part, 0)...)
		}
	}
	return out
}

// splitSearchGlobAlternatives splits on commas outside braces and character
// classes. A comma inside {a,b} stays with that group. The class end is the
// same rule as brace splitting: a ']' right after '[' or '[!' is a member.
// An unclosed '[' is a literal bracket, so a later comma still splits.
func splitSearchGlobAlternatives(pattern string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '[' {
			if end, ok := searchGlobClassEnd(pattern, i); ok {
				i = end
				continue
			}
		}
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, pattern[start:i])
				start = i + 1
			}
		}
	}
	return append(out, pattern[start:])
}

// expandSearchGlobBraces expands every {a,b} group. A group in the suffix of
// another group is expanded too, and a comma inside a nested group does not
// split the outer group.
func expandSearchGlobBraces(pattern string, depth int) []string {
	if depth > maxSearchGlobBraceDepth {
		return []string{pattern}
	}
	start, end, ok := findSearchGlobBrace(pattern)
	if !ok {
		return []string{pattern}
	}
	prefix, suffix := pattern[:start], pattern[end+1:]
	options := splitSearchGlobBraceOptions(pattern[start+1 : end])
	if len(options) == 0 {
		return []string{pattern}
	}
	out := make([]string, 0, len(options))
	for _, option := range options {
		// An empty alternative is the empty string: file{,.go} is file and
		// file.go. The concatenation itself must not be empty. "" is the
		// same match-all sentinel as ** in compileSingleSearchGlob, and a
		// group such as {,} is not a request to match every file.
		expanded := prefix + option + suffix
		if expanded == "" {
			continue
		}
		out = append(out, expandSearchGlobBraces(expanded, depth+1)...)
	}
	if len(out) == 0 {
		return []string{pattern}
	}
	return out
}

// searchGlobToSlash turns a Windows '\' separator into '/'. A backslash
// inside a character class is a member, so [\] stays. A user glob src\*.go
// still becomes src/*.go.
func searchGlobToSlash(pattern string) string {
	if !strings.Contains(pattern, `\`) {
		return pattern
	}
	var b strings.Builder
	b.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '[' {
			if end, ok := searchGlobClassEnd(pattern, i); ok {
				b.WriteString(pattern[i : end+1])
				i = end
				continue
			}
		}
		if pattern[i] == '\\' && runtime.GOOS == "windows" {
			b.WriteByte('/')
			continue
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}

// searchGlobClassEnd returns the closing ']' of the class that starts at open.
// A ']' right after '[' or '[!' is a member, not the closer. A [:name:]
// inside the class is one member, so its ']' does not close the class.
func searchGlobClassEnd(pattern string, open int) (int, bool) {
	if open >= 0 && open < len(pattern) && strings.HasPrefix(pattern[open:], "[[#g:") {
		rel := strings.Index(pattern[open:], "]]")
		if rel < 0 {
			return 0, false
		}
		return open + rel + 1, true
	}
	i := open + 1
	if i >= len(pattern) {
		return 0, false
	}
	if pattern[i] == '!' || pattern[i] == '^' {
		i++
		if i >= len(pattern) {
			return 0, false
		}
	}
	if i < len(pattern) && pattern[i] == ']' {
		i++
	}
	for i < len(pattern) {
		if pattern[i] == '[' {
			if _, next, ok := searchGlobPosixClass(pattern, i); ok {
				i = next
				continue
			}
		}
		if pattern[i] == ']' {
			return i, true
		}
		i++
	}
	return 0, false
}

func findSearchGlobBrace(pattern string) (int, int, bool) {
	start := -1
	depth := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '[' {
			if end, ok := searchGlobClassEnd(pattern, i); ok {
				i = end
				continue
			}
		}
		switch pattern[i] {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 && i > start+1 {
				return start, i, true
			}
			if depth == 0 {
				start = -1
			}
		}
	}
	return 0, 0, false
}

// splitSearchGlobBraceOptions splits one brace group. Spaces around an
// alternative are trimmed. A trimmed-empty alternative stays: file{,.go}
// is file and file.go, and {src/,}main.go is src/main.go and main.go.
// Dropping that alternative used to keep only the non-empty side.
func splitSearchGlobBraceOptions(body string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		if body[i] == '[' {
			if end, ok := searchGlobClassEnd(body, i); ok {
				i = end
				continue
			}
		}
		switch body[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(body[start:]))
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

func searchFileLinesWithMode(path string, re *regexp.Regexp, maxResults int, outputMode string, beforeContext, afterContext int, matches *[]string, matchedFiles map[string]bool, fileCounts map[string]int) error {
	_, err := searchFileLinesWithModeCtx(context.Background(), path, re, maxResults, outputMode, beforeContext, afterContext, matches, matchedFiles, fileCounts)
	return err
}

func searchFileLinesWithModeCtx(ctx context.Context, path string, re *regexp.Regexp, maxResults int, outputMode string, beforeContext, afterContext int, matches *[]string, matchedFiles map[string]bool, fileCounts map[string]int) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return false, nil
	}
	defer f.Close()
	// Open follows a directory junction. Reading that handle is not a file
	// hit, and the error must not fail the rest of the search.
	if st, statErr := f.Stat(); statErr == nil && st.IsDir() {
		return false, nil
	}
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	if bytes.Contains(buf[:n], []byte{0}) {
		return false, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return false, nil
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSearchFileSize+1)
	lineNo := 0
	var previous []numberedLine
	var pendingAfter int
	lastOutputLine := 0
	for scanner.Scan() {
		lineNo++
		if lineNo&255 == 0 {
			if err := ctx.Err(); err != nil {
				return true, err
			}
		}
		line := scanner.Text()
		if !re.MatchString(line) {
			if pendingAfter > 0 && outputMode == "content" {
				appendSearchOutputLine(path, lineNo, line, false, &lastOutputLine, matches)
				pendingAfter--
				if len(*matches) >= maxResults {
					return true, filepath.SkipAll
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
			return true, nil
		case "count":
			fileCounts[path]++
		default:
			for _, ctx := range previous {
				appendSearchOutputLine(path, ctx.lineNo, ctx.text, false, &lastOutputLine, matches)
				if len(*matches) >= maxResults {
					return true, filepath.SkipAll
				}
			}
			appendSearchOutputLine(path, lineNo, line, true, &lastOutputLine, matches)
			if len(*matches) >= maxResults {
				return true, filepath.SkipAll
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
	return true, scanner.Err()
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
		return corelib.WorkspaceDir()
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(corelib.WorkspaceDir(), p)
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
