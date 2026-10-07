package guiapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedFileReadAdapter        = "semantic_read_trusted_file"
	semanticTrustedFileReadImplementation = "trusted-fs-read-v1"
	semanticTrustedFileReadTimeout        = 10 * time.Second
	semanticTrustedFileReadSearchTimeout  = 30 * time.Second
	semanticTrustedFileReadListLimit      = 100
)

func semanticUnpublishedLegacyFileReadProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityFSReadLocal {
			return true
		}
	}
	return false
}

func semanticTrustedFileReadDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedFileReadAdapter,
			"description": "Inspect a workspace path. Empty path lists the workspace root; query searches contents; file_pattern locates files by name and narrows a query to the files it matches. start_line reads one host-sized page of a text file from that 1-based line; the page length is fixed. Do not combine start_line with query or file_pattern. File versus directory is decided by the filesystem.",
			"parameters":  semanticTrustedFileReadInvocationSchema(),
		},
	}
}

func semanticTrustedFileReadInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":  map[string]interface{}{"type": "string"},
			"query": map[string]interface{}{"type": "string"},
			// file_pattern names an outcome the other two cannot reach: which
			// files exist under this name shape. It is not the legacy
			// search_files knob of the same name, and it carries none of that
			// tool's other arguments.
			"file_pattern": map[string]interface{}{"type": "string"},
			// start_line names an outcome path and query cannot reach: the
			// text of a known file past the first page, or the page a compiler
			// log already addressed by line. The page length stays host-fixed,
			// so this is not the legacy lines/offset/end_line knob set.
			// The catalog renderer replaces the function description with the
			// capability summary, so this property description is what the model
			// reads. Do not put the page length in it: a number there invites
			// the model to pass the forbidden lines knob.
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "1-based start of one host-sized text page. The page length is fixed. Do not combine with query or file_pattern.",
			},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedFileReadArgsAllowed(args map[string]interface{}) (path, query, filePattern string, startLine int, err error) {
	if len(args) > 4 {
		return "", "", "", 0, fmt.Errorf("trusted_file_read_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "start_line":
			line, ok := tool.PositiveSchemaInteger(raw)
			if !ok {
				return "", "", "", 0, fmt.Errorf("trusted_file_read_arguments_rejected")
			}
			startLine = line
			continue
		}
		value, ok := raw.(string)
		if !ok {
			return "", "", "", 0, fmt.Errorf("trusted_file_read_arguments_rejected")
		}
		switch key {
		case "path":
			path = strings.TrimSpace(value)
		case "query":
			query = strings.TrimSpace(value)
		case "file_pattern":
			filePattern = strings.TrimSpace(value)
		default:
			return "", "", "", 0, fmt.Errorf("trusted_file_read_arguments_rejected")
		}
	}
	// A line page and a search name two different reads. Picking one would
	// hide the other.
	if startLine > 0 && (query != "" || filePattern != "") {
		return "", "", "", 0, fmt.Errorf("trusted_file_read_conflicting_fields")
	}
	return path, query, filePattern, startLine, nil
}

// trustedFileReadLocated turns a name walk into either its matches or a
// failure.
//
// The walk states trouble by putting the reason in Text and marking Outcome,
// so a caller that reads only Text hands that reason back as though it were
// the list of matching files -- and "no such pattern" then reads to the model
// exactly like "no such file". Finding nothing is a different fact: it is a
// complete answer, and must stay a success.
func trustedFileReadLocated(found agent.SearchToolResult) (string, error) {
	if found.Outcome == agent.SearchToolOutcomeError {
		return "", fmt.Errorf("trusted_file_read_locate_failed")
	}
	return found.Text, nil
}

func (h *IMMessageHandler) readTrustedFile(principalID, path, query, filePattern string, startLine int) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_file_read_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_file_read_principal_required")
	}
	path, query, filePattern = strings.TrimSpace(path), strings.TrimSpace(query), strings.TrimSpace(filePattern)
	// A line page and a search name two different reads. Rejecting here, before
	// the hook, keeps a direct call from hiding one of them inside the other.
	if startLine < 0 || (startLine > 0 && (query != "" || filePattern != "")) {
		return "", fmt.Errorf("trusted_file_read_conflicting_fields")
	}
	if h.semanticTrustedFileRead != nil {
		return h.semanticTrustedFileRead(principalID, path, query, filePattern, startLine)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedFileReadResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	ctx, cancel := trustedFileReadContext(query, filePattern)
	defer cancel()
	if query != "" {
		// Given both, the name shape narrows the content search rather than
		// describing a second, separate search.
		raw := tool.SearchFilesInProjectCtx(ctx, absPath, query, filePattern)
		// The search reports a cut-short walk only in its prose, and a
		// truncated result reads exactly like an exhaustive one that found
		// less. Re-checking the deadline asks the same question the search
		// asked, rather than trusting it to say so in words.
		if ctx.Err() != nil {
			return "", fmt.Errorf("trusted_file_read_search_incomplete")
		}
		return trustedFileReadRewriteWorkspace(workspace, raw), nil
	}
	if filePattern != "" {
		// Locating files by name was the one thing this surface could not do:
		// path lists a single directory and query reads contents, so a plan
		// holding only fs.read.local had no way to discover what to read. The
		// walk is the reviewed one the legacy tool uses, so managed and legacy
		// turns agree on matching, exclusions, and result bounds, and the model
		// gets no knob to widen any of them.
		located, err := trustedFileReadLocated(agent.ToolGlobDetailedCtx(ctx, map[string]interface{}{"pattern": filePattern, "path": absPath}))
		if err != nil {
			return "", err
		}
		return trustedFileReadRewriteWorkspace(workspace, located), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("trusted_file_read_not_found")
	}
	if startLine > 0 {
		return trustedFileReadFromStartLine(absPath, info, startLine)
	}
	display := trustedFileWriteDisplayPath(workspace, absPath, path)
	if info.IsDir() {
		return trustedFileReadList(absPath, display)
	}
	if trustedFileReadUsesDocumentReader(absPath) {
		out := agent.ToolReadDocumentWithOfficeReadConfigAndContext(map[string]interface{}{"file_path": absPath}, agent.OfficeReadConfig{}, 0)
		if class, failed := agent.DocumentReadFailure(out); failed {
			return "", fmt.Errorf("trusted_document_read_failed_%s", class)
		}
		return trustedFileReadRewriteWorkspace(workspace, semanticDocumentReadResultProjection(out)), nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	if trustedFileReadUsesTailDefault(absPath) {
		return trustedFileReadPage(string(data), true), nil
	}
	return trustedFileReadPage(string(data), false), nil
}

// trustedFileReadContext gives the tree-walking modes the wider bound. Reading
// one known path is bounded by that file; searching contents or locating files
// by name is bounded by the size of the workspace.
func trustedFileReadContext(query, filePattern string) (context.Context, context.CancelFunc) {
	timeout := semanticTrustedFileReadTimeout
	if query != "" || filePattern != "" {
		timeout = semanticTrustedFileReadSearchTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func trustedFileReadResolvePath(workspace, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	abs, _, kind := resolvePathInsideWorkspace(workspace, path)
	switch kind {
	case trustedPathOK:
		return abs, nil
	case trustedPathNoWorkspace:
		return "", fmt.Errorf("trusted_file_read_path_unavailable")
	default:
		return "", fmt.Errorf("trusted_file_read_path_rejected")
	}
}

func trustedFileReadList(absPath, display string) (string, error) {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", err
	}
	if display == "" {
		display = "."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Directory: %s (%d items)\n", display, len(entries))
	shown := 0
	for _, entry := range entries {
		if shown >= semanticTrustedFileReadListLimit {
			fmt.Fprintf(&b, "... %d more items not shown\n", len(entries)-shown)
			break
		}
		if entry.IsDir() {
			fmt.Fprintf(&b, "  %s/\n", entry.Name())
		} else if info, err := entry.Info(); err == nil {
			fmt.Fprintf(&b, "  %s (%d bytes)\n", entry.Name(), info.Size())
		} else {
			fmt.Fprintf(&b, "  %s\n", entry.Name())
		}
		shown++
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func trustedFileReadFromStartLine(absPath string, info os.FileInfo, startLine int) (string, error) {
	if info.IsDir() || trustedFileReadUsesDocumentReader(absPath) {
		return "", fmt.Errorf("trusted_file_read_start_line_unsupported")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	// A requested page is a text-file read. It overrides the .log tail default,
	// including start_line=1, which is the head of the file.
	return trustedFileReadWindow(string(data), startLine, false)
}

func trustedFileReadPage(content string, tail bool) string {
	text, err := trustedFileReadWindow(content, 0, tail)
	if err != nil {
		return content
	}
	return text
}

// trustedFileReadWindow pages one text file. startLine 0 keeps the historical
// head, or the tail for a log. A positive startLine is the 1-based page start;
// the page length stays readFileMaxLines. The body is the file text itself, so
// a later old_string copy is not prefixed with line numbers.
func trustedFileReadWindow(content string, startLine int, tail bool) (string, error) {
	lines := tool.SplitTextLines(content)
	total := len(lines)
	if startLine > 0 {
		if startLine > total {
			return "", fmt.Errorf("trusted_file_read_start_past_end")
		}
		window := lines[startLine-1:]
		if len(window) <= readFileMaxLines {
			if startLine == 1 {
				return content, nil
			}
			return fmt.Sprintf("(lines %d-%d of %d)\n%s", startLine, total, total, strings.Join(window, "")), nil
		}
		end := startLine + readFileMaxLines - 1
		chunk := strings.Join(window[:readFileMaxLines], "")
		return chunk + fmt.Sprintf("\n... (total %d lines, showing %d-%d. Next: start_line=%d)", total, startLine, end, end+1), nil
	}
	if total <= readFileMaxLines {
		return content, nil
	}
	if tail {
		start := total - readFileMaxLines
		return fmt.Sprintf("... (skipped first %d lines, showing last %d of %d total)\n%s", start, readFileMaxLines, total, strings.Join(lines[start:], "")), nil
	}
	chunk := strings.Join(lines[:readFileMaxLines], "")
	return chunk + fmt.Sprintf("\n... (total %d lines, showing %d-%d. Next: start_line=%d)", total, 1, readFileMaxLines, readFileMaxLines+1), nil
}

func trustedFileReadUsesDocumentReader(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx":
		return true
	default:
		return false
	}
}

func trustedFileReadUsesTailDefault(path string) bool {
	return strings.ToLower(filepath.Ext(strings.TrimSpace(path))) == ".log"
}

func trustedFileReadRewriteWorkspace(workspace, text string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || text == "" {
		return text
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return text
	}
	abs = normalizeProjectSessionPath(abs)
	replacements := []string{abs + string(filepath.Separator), filepath.ToSlash(abs) + "/", abs, filepath.ToSlash(abs)}
	for _, prefix := range replacements {
		text = strings.ReplaceAll(text, prefix, "")
	}
	return text
}

func semanticTrustedFileReadResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_file_read_delivery_token")
	}
	// A page footer names absolute lines, and the body is what a later
	// old_string copies. Trimming would drop a blank line and the final
	// newline, so the text the model sees would no longer be those lines.
	// Only a result that is empty is a failed read. A file of blank lines
	// is still that file.
	if text == "" {
		return "", fmt.Errorf("trusted_file_read_empty")
	}
	return text, nil
}
