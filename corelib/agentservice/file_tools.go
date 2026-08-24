package agentservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	srvReadFileMaxLines = 200
	srvWriteFileMaxSize = 512 * 1024 // 512 KB
)

// executeReadFile handles the read_file tool, scoped to the workspace.
func (c *coreAgentCallbacks) executeReadFile(args map[string]interface{}) string {
	text, _ := c.readFileDetailed(args)
	return text
}

// readFileDetailed is executeReadFile with its verdict left intact.
//
// The legacy tool surface can answer only with a string, so every failure here
// is spelled "Error: ..." and is indistinguishable from a file whose contents
// happen to start that way. A caller that is able to report a failure must be
// able to ask about one instead of parsing for it.
func (c *coreAgentCallbacks) readFileDetailed(args map[string]interface{}) (string, error) {
	p := stringArg(args, "path")
	if p == "" {
		return "Error: missing path parameter", fmt.Errorf("host_file_read_arguments_rejected")
	}
	absPath, err := c.resolveWorkspacePath(p)
	if err != nil {
		return "Error: " + err.Error(), fmt.Errorf("host_file_read_path_rejected")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Error: file not found or inaccessible: %v", err), fmt.Errorf("host_file_read_not_found")
	}
	if info.IsDir() {
		return fmt.Sprintf("Error: %s is a directory, use list_directory", absPath), fmt.Errorf("host_file_read_path_is_directory")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("Error: read failed: %v", err), fmt.Errorf("host_file_read_failed")
	}
	lines := strings.SplitAfter(string(data), "\n")
	totalLines := len(lines)

	// offset parameter: read last N lines (like tail -n)
	if tailN := intArg(args, "offset", 0); tailN > 0 {
		if tailN >= totalLines {
			return string(data), nil
		}
		startIdx := totalLines - tailN
		tailContent := strings.Join(lines[startIdx:], "")
		return fmt.Sprintf("... (skipped first %d lines, showing last %d of %d total)\n%s", startIdx, tailN, totalLines, tailContent), nil
	}

	maxLines := srvReadFileMaxLines
	if n := intArg(args, "lines", 0); n > 0 {
		maxLines = n
	}
	startLine := 1
	if n := intArg(args, "start_line", 1); n > 1 {
		startLine = n
	}

	startIdx := startLine - 1
	if startIdx >= totalLines {
		return fmt.Sprintf("Error: start_line=%d exceeds total lines %d", startLine, totalLines), fmt.Errorf("host_file_read_range_rejected")
	}
	if startIdx < 0 {
		startIdx = 0
	}
	remaining := lines[startIdx:]
	if len(remaining) > maxLines {
		chunk := remaining[:maxLines]
		endLine := startLine + maxLines - 1
		return strings.Join(chunk, "") + fmt.Sprintf(
			"\n... (total %d lines, showing %d-%d. Next: start_line=%d)",
			totalLines, startLine, endLine, endLine+1), nil
	}
	if startIdx > 0 {
		return fmt.Sprintf("(lines %d-%d of %d)\n%s", startLine, totalLines, totalLines, strings.Join(remaining, "")), nil
	}
	return string(data), nil
}

// executeWriteFile handles the write_file tool, scoped to the workspace.
func (c *coreAgentCallbacks) executeWriteFile(args map[string]interface{}) string {
	p := stringArg(args, "path")
	if p == "" {
		return "Error: missing path parameter"
	}
	content := stringArg(args, "content")
	if _, ok := args["content"]; !ok {
		return "Error: missing content parameter"
	}
	if len(content) > srvWriteFileMaxSize {
		return fmt.Sprintf("Error: content too large (%d bytes), max %d bytes", len(content), srvWriteFileMaxSize)
	}
	mode := stringArg(args, "mode")
	absPath, err := c.resolveWorkspacePath(p)
	if err != nil {
		return "Error: " + err.Error()
	}
	size, err := coretool.WriteTextFile(absPath, content, mode)
	if err != nil {
		return fmt.Sprintf("Error: write failed: %v", err)
	}
	resolvedMode, _ := coretool.NormalizeWriteModeKind(mode)
	if resolvedMode == coretool.WriteModeAppend {
		return fmt.Sprintf("Appended to %s (%d bytes total)", absPath, size)
	}
	return fmt.Sprintf("Written to %s (%d bytes)", absPath, size)
}

// executeEditFile handles the edit_file tool, scoped to the workspace.
func (c *coreAgentCallbacks) executeEditFile(args map[string]interface{}) string {
	p := stringArg(args, "path")
	oldString := stringArg(args, "old_string")
	newString := stringArg(args, "new_string")
	if p == "" || oldString == "" {
		return "Error: missing path or old_string parameter"
	}
	if _, ok := args["new_string"]; !ok {
		return "Error: missing new_string parameter"
	}
	absPath, err := c.resolveWorkspacePath(p)
	if err != nil {
		return "Error: " + err.Error()
	}
	replaceAll := boolArg(args, "replace_all", false)
	res, err := coretool.EditTextFile(absPath, oldString, newString, replaceAll)
	if err != nil {
		return fmt.Sprintf("Error: edit failed: %v", err)
	}
	return fmt.Sprintf("Edited %s (%d replacements, %d bytes)", res.Path, res.Count, res.Size)
}

// executeListDirectory handles the list_directory tool, scoped to the workspace.
func (c *coreAgentCallbacks) executeListDirectory(args map[string]interface{}) string {
	text, _ := c.listDirectoryDetailed(args)
	return text
}

// listDirectoryDetailed is executeListDirectory with its verdict left intact,
// for the same reason readFileDetailed exists: an unreadable directory and an
// empty one are different answers, and prose cannot be trusted to separate
// them.
func (c *coreAgentCallbacks) listDirectoryDetailed(args map[string]interface{}) (string, error) {
	p := stringArg(args, "path")
	if p == "" {
		p = "."
	}
	absPath, err := c.resolveWorkspacePath(p)
	if err != nil {
		return "Error: " + err.Error(), fmt.Errorf("host_file_read_path_rejected")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Error: path not found: %v", err), fmt.Errorf("host_file_read_not_found")
	}
	if !info.IsDir() {
		return fmt.Sprintf("Error: %s is not a directory", absPath), fmt.Errorf("host_file_read_path_is_not_directory")
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Sprintf("Error: read directory failed: %v", err), fmt.Errorf("host_file_read_failed")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Directory: %s (%d items)\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... %d more items not shown\n", len(entries)-shown))
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
	return b.String(), nil
}

// resolveWorkspacePath resolves a relative path within the workspace.
// Returns error if the path escapes the workspace boundary.
func (c *coreAgentCallbacks) resolveWorkspacePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	base := strings.TrimSpace(c.workspace)
	if base == "" {
		// No workspace restriction — resolve as absolute.
		return filepath.Abs(p)
	}
	// If path is already absolute, validate it's within workspace.
	if filepath.IsAbs(p) {
		if err := ensurePathWithinBase(p, base); err != nil {
			return "", err
		}
		return filepath.Clean(p), nil
	}
	// Relative path — resolve against workspace.
	resolved := filepath.Join(base, p)
	if err := ensurePathWithinBase(resolved, base); err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
