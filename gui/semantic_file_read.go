package main

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
			"description": "Inspect a workspace path. Empty path lists the workspace root; query searches contents; file_pattern locates files by name and narrows a query to the files it matches. File versus directory is decided by the filesystem.",
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
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedFileReadArgsAllowed(args map[string]interface{}) (path, query, filePattern string, err error) {
	if len(args) > 3 {
		return "", "", "", fmt.Errorf("trusted_file_read_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", "", fmt.Errorf("trusted_file_read_arguments_rejected")
		}
		switch key {
		case "path":
			path = strings.TrimSpace(value)
		case "query":
			query = strings.TrimSpace(value)
		case "file_pattern":
			filePattern = strings.TrimSpace(value)
		default:
			return "", "", "", fmt.Errorf("trusted_file_read_arguments_rejected")
		}
	}
	return path, query, filePattern, nil
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

func (h *IMMessageHandler) readTrustedFile(principalID, path, query, filePattern string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_file_read_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_file_read_principal_required")
	}
	path, query, filePattern = strings.TrimSpace(path), strings.TrimSpace(query), strings.TrimSpace(filePattern)
	if h.semanticTrustedFileRead != nil {
		return h.semanticTrustedFileRead(principalID, path, query, filePattern)
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
	workspace = strings.TrimSpace(workspace)
	path = strings.TrimSpace(path)
	if workspace == "" {
		return "", fmt.Errorf("trusted_file_read_path_unavailable")
	}
	if path == "" {
		path = "."
	}
	base, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("trusted_file_read_path_unavailable")
	}
	candidate := path
	if !filepath.IsAbs(path) {
		candidate = filepath.Join(base, path)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("trusted_file_read_path_rejected")
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("trusted_file_read_path_rejected")
	}
	return abs, nil
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

func trustedFileReadPage(content string, tail bool) string {
	lines := strings.SplitAfter(content, "\n")
	total := len(lines)
	if total <= readFileMaxLines {
		return content
	}
	if tail {
		start := total - readFileMaxLines
		return fmt.Sprintf("... (skipped first %d lines, showing last %d of %d total)\n%s", start, readFileMaxLines, total, strings.Join(lines[start:], ""))
	}
	chunk := strings.Join(lines[:readFileMaxLines], "")
	return chunk + fmt.Sprintf("\n... (total %d lines, showing first %d)", total, readFileMaxLines)
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
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_file_read_empty")
	}
	return text, nil
}
