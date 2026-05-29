package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func executeCodingReadFile(args map[string]interface{}) codingToolExecutionResult {
	p, _ := args["path"].(string)
	if p == "" {
		return codingToolExecutionResult{Text: "missing path parameter", Outcome: codingToolOutcomeFailed}
	}
	absPath, err := resolveFileToolPath(p)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return codingToolExecutionResult{Text: fmt.Sprintf("file does not exist or cannot be accessed: %s", err.Error()), Outcome: codingToolOutcomeFailed}
	}
	if info.IsDir() {
		return codingToolExecutionResult{Text: fmt.Sprintf("%s is a directory; use list_directory", absPath), Outcome: codingToolOutcomeFailed}
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
		return codingToolExecutionResult{Text: fmt.Sprintf("read failed: %s", err.Error()), Outcome: codingToolOutcomeFailed}
	}

	lines := strings.SplitAfter(string(data), "\n")
	totalLines := len(lines)
	if offset, ok := args["offset"].(float64); ok && offset > 0 {
		tailN := int(offset)
		if tailN >= totalLines {
			return codingToolExecutionResult{Text: string(data), Outcome: codingToolOutcomeSuccess}
		}
		startIdx := totalLines - tailN
		tailContent := strings.Join(lines[startIdx:], "")
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("... (skipped first %d lines, showing last %d of %d lines)\n%s", startIdx, tailN, totalLines, tailContent),
			Outcome: codingToolOutcomeSuccess,
		}
	}

	if !explicitLines && !explicitStart && totalLines > readFileMaxLines {
		return codingToolExecutionResult{Text: buildAdaptiveReadResult(lines, totalLines, absPath), Outcome: codingToolOutcomeSuccess}
	}

	startIdx := startLine - 1
	if startIdx >= totalLines {
		return codingToolExecutionResult{Text: fmt.Sprintf("start_line=%d exceeds total lines %d", startLine, totalLines), Outcome: codingToolOutcomeFailed}
	}
	if startIdx < 0 {
		startIdx = 0
	}
	remaining := lines[startIdx:]
	if len(remaining) > maxLines {
		chunk := remaining[:maxLines]
		endLine := startLine + maxLines - 1
		nextStart := endLine + 1
		if explicitStart {
			var numbered strings.Builder
			for i, line := range chunk {
				numbered.WriteString(fmt.Sprintf("%4d | %s", startLine+i, line))
			}
			return codingToolExecutionResult{
				Text:    numbered.String() + fmt.Sprintf("\n... (%d total lines, showing %d-%d. Next start_line=%d)", totalLines, startLine, endLine, nextStart),
				Outcome: codingToolOutcomeSuccess,
			}
		}
		return codingToolExecutionResult{
			Text:    strings.Join(chunk, "") + fmt.Sprintf("\n... (%d total lines, showing %d-%d. Next start_line=%d)", totalLines, startLine, endLine, nextStart),
			Outcome: codingToolOutcomeSuccess,
		}
	}

	if startIdx > 0 {
		if explicitStart {
			var numbered strings.Builder
			for i, line := range remaining {
				numbered.WriteString(fmt.Sprintf("%4d | %s", startLine+i, line))
			}
			return codingToolExecutionResult{Text: fmt.Sprintf("(lines %d-%d of %d)\n%s", startLine, totalLines, totalLines, numbered.String()), Outcome: codingToolOutcomeSuccess}
		}
		return codingToolExecutionResult{Text: fmt.Sprintf("(lines %d-%d of %d)\n%s", startLine, totalLines, totalLines, strings.Join(remaining, "")), Outcome: codingToolOutcomeSuccess}
	}
	return codingToolExecutionResult{Text: string(data), Outcome: codingToolOutcomeSuccess}
}

func executeCodingListDirectory(args map[string]interface{}) codingToolExecutionResult {
	p, _ := args["path"].(string)
	if p == "" {
		return codingToolExecutionResult{Text: "missing path parameter", Outcome: codingToolOutcomeFailed}
	}
	absPath, err := resolveFileToolPath(p)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return codingToolExecutionResult{Text: fmt.Sprintf("path does not exist or cannot be accessed: %s", err.Error()), Outcome: codingToolOutcomeFailed}
	}
	if !info.IsDir() {
		return codingToolExecutionResult{Text: fmt.Sprintf("%s is not a directory", absPath), Outcome: codingToolOutcomeFailed}
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return codingToolExecutionResult{Text: fmt.Sprintf("read directory failed: %s", err.Error()), Outcome: codingToolOutcomeFailed}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("directory: %s (%d entries)\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... %d more entries not shown\n", len(entries)-shown))
			break
		}
		entryInfo, _ := entry.Info()
		name := filepath.ToSlash(entry.Name())
		if entry.IsDir() {
			b.WriteString(fmt.Sprintf("  [dir] %s/\n", name))
		} else if entryInfo != nil {
			b.WriteString(fmt.Sprintf("  [file] %s (%d bytes)\n", name, entryInfo.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  [file] %s\n", name))
		}
		shown++
	}
	return codingToolExecutionResult{Text: b.String(), Outcome: codingToolOutcomeSuccess}
}

func executeCodingWriteFile(args map[string]interface{}) codingToolExecutionResult {
	p, _ := args["path"].(string)
	content, ok := args["content"].(string)
	if p == "" {
		return codingToolExecutionResult{Text: "missing path parameter", Outcome: codingToolOutcomeFailed}
	}
	if !ok {
		return codingToolExecutionResult{Text: "missing content parameter", Outcome: codingToolOutcomeFailed}
	}
	if len(content) > writeFileMaxSize {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("content is too large (%d bytes), maximum allowed is %d bytes", len(content), writeFileMaxSize),
			Outcome: codingToolOutcomeFailed,
		}
	}
	absPath, err := resolveFileToolPath(p)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	size, err := coretool.WriteTextFile(absPath, content, stringVal(args, "mode"))
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	resolvedMode, _ := coretool.NormalizeWriteModeKind(stringVal(args, "mode"))
	if resolvedMode == coretool.WriteModeAppend {
		return codingToolExecutionResult{Text: fmt.Sprintf("appended to %s (current %d bytes)", absPath, size), Outcome: codingToolOutcomeSuccess}
	}
	if content == "" {
		return codingToolExecutionResult{Text: fmt.Sprintf("cleared %s (%d bytes)", absPath, size), Outcome: codingToolOutcomeSuccess}
	}
	return codingToolExecutionResult{Text: fmt.Sprintf("wrote %s (%d bytes)", absPath, size), Outcome: codingToolOutcomeSuccess}
}

func executeCodingEditFile(args map[string]interface{}) codingToolExecutionResult {
	p, _ := args["path"].(string)
	oldString, okOld := args["old_string"].(string)
	newString, okNew := args["new_string"].(string)
	if p == "" || !okOld || !okNew {
		return codingToolExecutionResult{Text: "missing path, old_string, or new_string parameter", Outcome: codingToolOutcomeFailed}
	}
	absPath, err := resolveFileToolPath(p)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	replaceAll, _ := args["replace_all"].(bool)
	res, err := coretool.EditTextFile(absPath, oldString, newString, replaceAll)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	return codingToolExecutionResult{
		Text:    fmt.Sprintf("edited %s (replaced %d occurrence(s), current %d bytes)", res.Path, res.Count, res.Size),
		Outcome: codingToolOutcomeSuccess,
	}
}

func executeCodingEditLines(args map[string]interface{}) codingToolExecutionResult {
	p, _ := args["path"].(string)
	if p == "" {
		return codingToolExecutionResult{Text: "missing path parameter", Outcome: codingToolOutcomeFailed}
	}
	absPath, err := resolveFileToolPath(p)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	opStr, _ := args["operation"].(string)
	if opStr == "" {
		return codingToolExecutionResult{Text: "missing operation parameter (replace/insert/delete)", Outcome: codingToolOutcomeFailed}
	}
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
	res, err := coretool.EditFileByLine(absPath, coretool.EditLineOperation(opStr), startLine, endLine, content)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	contextPreview := buildEditLineContext(absPath, startLine, res.TotalLines)
	return codingToolExecutionResult{
		Text:    fmt.Sprintf("edited %s (%s %d line(s), now %d lines, %d bytes)\n%s", res.Path, opStr, res.LinesChanged, res.TotalLines, res.Size, contextPreview),
		Outcome: codingToolOutcomeSuccess,
	}
}
