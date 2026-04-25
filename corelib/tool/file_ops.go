package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultWriteMode = "overwrite"

type PathResolver func() string

func ResolveFileToolPath(path string, projectDirResolver PathResolver) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	p := strings.TrimSpace(path)
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if projectDirResolver != nil {
		if base := strings.TrimSpace(projectDirResolver()); base != "" {
			return filepath.Clean(filepath.Join(base, p)), nil
		}
	}
	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		return filepath.Clean(filepath.Join(cwd, p)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(p), nil
	}
	return filepath.Clean(filepath.Join(home, p)), nil
}

func NormalizeWriteMode(mode string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return DefaultWriteMode, nil
	}
	switch m {
	case "overwrite", "append":
		return m, nil
	default:
		return "", fmt.Errorf("不支持的 mode: %s", mode)
	}
}

func WriteTextFile(path, content, mode string) (int64, error) {
	m, err := NormalizeWriteMode(mode)
	if err != nil {
		return 0, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("创建目录失败 %s: %w", dir, err)
	}

	if m == "append" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, fmt.Errorf("打开文件失败 %s: %w", path, err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return 0, fmt.Errorf("写入内容失败: %w", err)
		}
	} else {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return 0, fmt.Errorf("写入文件失败 %s: %w", path, err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}
	return info.Size(), nil
}

type EditTextFileResult struct {
	Count int
	Size  int64
	Path  string
}

func EditTextFile(path, oldString, newString string, replaceAll bool) (*EditTextFileResult, error) {
	if oldString == "" {
		return nil, fmt.Errorf("缺少 old_string 参数")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	text := string(data)
	count := strings.Count(text, oldString)
	if count == 0 {
		return nil, fmt.Errorf("未找到要替换的内容")
	}
	updated := text
	applied := 1
	if replaceAll {
		updated = strings.ReplaceAll(text, oldString, newString)
		applied = count
	} else {
		updated = strings.Replace(text, oldString, newString, 1)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败 %s: %w", path, err)
	}

	// Verify write succeeded.
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}
	return &EditTextFileResult{Count: applied, Size: info.Size(), Path: path}, nil
}


// EditLineResult is the outcome of a line-level edit operation.
type EditLineResult struct {
	Path         string
	LinesChanged int   // number of lines affected
	TotalLines   int   // total lines after edit
	Size         int64 // file size after edit
}

// EditLineOperation specifies what kind of line edit to perform.
type EditLineOperation string

const (
	// EditLineReplace replaces lines [startLine, endLine] with new content.
	EditLineReplace EditLineOperation = "replace"
	// EditLineInsert inserts new content after the specified line (startLine).
	// endLine is ignored.
	EditLineInsert EditLineOperation = "insert"
	// EditLineDelete deletes lines [startLine, endLine].
	EditLineDelete EditLineOperation = "delete"
)

// EditFileByLine performs a line-level edit on a file.
//
// Line numbers are 1-indexed. For replace and delete, both startLine and
// endLine are inclusive: [startLine, endLine].
//
// Operations:
//   - replace: replaces lines startLine..endLine with newContent
//   - insert:  inserts newContent after startLine (0 = insert at beginning)
//   - delete:  removes lines startLine..endLine
//
// This is more precise than EditTextFile (substring match) because it uses
// line numbers for addressing — no ambiguity from duplicate content.
func EditFileByLine(path string, op EditLineOperation, startLine, endLine int, newContent string) (*EditLineResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}

	// Detect and preserve the file's line ending style.
	// This prevents mixing \r\n and \n after editing.
	text := string(data)
	lineEnding := "\n"
	if strings.Contains(text, "\r\n") {
		lineEnding = "\r\n"
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}

	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	// Validate line numbers.
	switch op {
	case EditLineInsert:
		if startLine < 0 || startLine > totalLines {
			return nil, fmt.Errorf("行号越界: start_line=%d（文件共 %d 行，insert 允许 0..%d）", startLine, totalLines, totalLines)
		}
	case EditLineReplace, EditLineDelete:
		if startLine < 1 || startLine > totalLines {
			return nil, fmt.Errorf("行号越界: start_line=%d（文件共 %d 行）", startLine, totalLines)
		}
		if endLine < startLine {
			return nil, fmt.Errorf("end_line=%d 不能小于 start_line=%d", endLine, startLine)
		}
		if endLine > totalLines {
			return nil, fmt.Errorf("行号越界: end_line=%d（文件共 %d 行）", endLine, totalLines)
		}
	default:
		return nil, fmt.Errorf("未知操作: %s（支持 replace/insert/delete）", op)
	}

	var result []string
	var linesChanged int

	switch op {
	case EditLineReplace:
		// Replace lines [startLine, endLine] with newContent.
		// Normalize newContent line endings to match internal \n representation.
		normalizedContent := strings.ReplaceAll(newContent, "\r\n", "\n")
		newLines := strings.Split(normalizedContent, "\n")
		result = append(result, lines[:startLine-1]...)
		result = append(result, newLines...)
		result = append(result, lines[endLine:]...)
		linesChanged = endLine - startLine + 1

	case EditLineInsert:
		// Insert newContent after startLine. startLine=0 means insert at top.
		normalizedContent := strings.ReplaceAll(newContent, "\r\n", "\n")
		newLines := strings.Split(normalizedContent, "\n")
		result = append(result, lines[:startLine]...)
		result = append(result, newLines...)
		result = append(result, lines[startLine:]...)
		linesChanged = len(newLines)

	case EditLineDelete:
		// Delete lines [startLine, endLine].
		result = append(result, lines[:startLine-1]...)
		result = append(result, lines[endLine:]...)
		linesChanged = endLine - startLine + 1
	}

	output := strings.Join(result, lineEnding)
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败 %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}

	return &EditLineResult{
		Path:         path,
		LinesChanged: linesChanged,
		TotalLines:   len(result),
		Size:         info.Size(),
	}, nil
}
