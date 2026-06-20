package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// KnowledgeStore is the interface required by the agent executor for knowledge operations.
// It stores cited documents/cards/facts for retrieval, not Maclaw long-term memory;
// durable user/agent memories, recall, audit, and surgery are owned by
// corelib/memory.Store. Satisfied by *knowledge.SQLiteStore.
type KnowledgeStore interface {
	Search(ctx context.Context, opts knowledge.SearchOptions) ([]knowledge.SearchResult, error)
	ContextPack(ctx context.Context, opts knowledge.ContextPackOptions) (knowledge.ContextPackResult, error)
	SaveURL(ctx context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error)
	SaveText(ctx context.Context, req knowledge.TextSaveRequest) (knowledge.Source, error)
	ScanDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error)
	ScanFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error)
	ImportDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error)
	ImportFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error)
	Stats(ctx context.Context) (knowledge.Stats, error)
}

// SetKnowledgeStore wires the knowledge store into the executor.
// Must be called before Execute() to enable knowledge tools and auto-recall.
func (e *CoreAgentExecutor) SetKnowledgeStore(store KnowledgeStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.knowledgeStore = store
}



// appendKnowledgeAutoRecall searches the knowledge base and injects relevant
// results into the system prompt. Uses shared constants from corelib/agent/prompt_blocks.go
// to stay in sync with GUI and TUI implementations.
func (c *coreAgentCallbacks) parentContext() context.Context {
	if c != nil && c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}
func (c *coreAgentCallbacks) appendKnowledgeAutoRecall(b *strings.Builder, userMsg string) {
	if c.knowledgeStore == nil || userMsg == "" {
		return
	}

	query := userMsg
	if utf8.RuneCountInString(query) > agent.KnowledgeAutoRecallMaxQueryRunes {
		runes := []rune(query)
		query = string(runes[:agent.KnowledgeAutoRecallMaxQueryRunes])
	}

	ctx, cancel := context.WithTimeout(c.parentContext(), 3*time.Second)
	defer cancel()

	results, err := c.knowledgeStore.Search(ctx, knowledge.SearchOptions{
		Query:    query,
		OwnerID:  c.principal.UserID,
		TenantID: c.principal.TenantID,
		Limit:    agent.KnowledgeAutoRecallSearchLimit,
	})
	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v", err)
		return
	}
	if len(results) == 0 {
		// FTS returned nothing. This could mean the knowledge base is empty OR
		// the query terms don't match any indexed content. Without a cheap
		// existence check we cannot distinguish the two cases, so stay silent
		// to avoid confusing users with an empty knowledge base.
		return
	}

	topScore := results[0].Score
	maxInject := agent.KnowledgeAutoRecallMaxInject(topScore)
	if maxInject == 0 {
		// Results exist but scores are below injection threshold. The knowledge
		// base definitely has content — hint the LLM to try deeper search.
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	b.WriteString(agent.KnowledgeAutoRecallHeader)

	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < agent.KnowledgeAutoRecallScoreThreshold {
			break
		}
		text := knowledgeSnippet(r)
		if text == "" {
			continue
		}
		if len([]rune(text)) > agent.KnowledgeAutoRecallSnippetMaxRunes {
			text = string([]rune(text)[:agent.KnowledgeAutoRecallSnippetMaxRunes]) + "..."
		}
		b.WriteString(fmt.Sprintf("- Source: %s; Citation: %s; Evidence: %s\n", knowledgeSourceLabel(r), knowledgeCitationLabel(r), text))
		injected++
	}
}

func knowledgeSourceLabel(r knowledge.SearchResult) string {
	return knowledge.FormatSourceLabel(r)
}

func knowledgeCitationLabel(r knowledge.SearchResult) string {
	return knowledge.FormatCitationLabel(r)
}

// knowledgeSnippet extracts the best display text from a search result.
// Delegates to the shared knowledge.BestContentText for consistent priority across all platforms.
func knowledgeSnippet(r knowledge.SearchResult) string {
	return knowledge.BestContentText(r)
}

// --- Knowledge tool execution ---

func (c *coreAgentCallbacks) executeKnowledgeSearch(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	opts := buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)
	ctx, cancel := context.WithTimeout(c.parentContext(), 10*time.Second)
	defer cancel()

	results, err := c.knowledgeStore.Search(ctx, opts)
	if err != nil {
		return fmt.Sprintf("Error: knowledge search failed: %v", err)
	}
	if len(results) == 0 {
		return knowledge.EmptySearchResultMessage
	}
	return formatSearchResults(results)
}

func (c *coreAgentCallbacks) executeKnowledgeContextPack(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	searchOpts := buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)
	maxItems := intArg(args, "max_items", 10)
	maxChars := intArg(args, "max_chars", 4000)

	ctx, cancel := context.WithTimeout(c.parentContext(), 10*time.Second)
	defer cancel()

	result, err := c.knowledgeStore.ContextPack(ctx, knowledge.ContextPackOptions{
		SearchOptions: searchOpts,
		MaxItems:      maxItems,
		MaxChars:      maxChars,
	})
	if err != nil {
		return fmt.Sprintf("Error: knowledge context pack failed: %v", err)
	}
	if len(result.Items) == 0 {
		return knowledge.EmptyContextPackMessage
	}
	return knowledge.FormatContextPackForLLM(result)
}

func (c *coreAgentCallbacks) executeKnowledgeSaveURL(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	url := firstStringArg(args, "url", "link", "href", "uri", "target")
	if url == "" {
		return "Error: url parameter is required (aliases: link, href, uri, target)"
	}
	title := stringArg(args, "title")
	topicHint := stringArg(args, "topic_hint")

	ctx, cancel := context.WithTimeout(c.parentContext(), 30*time.Second)
	defer cancel()

	source, err := c.knowledgeStore.SaveURL(ctx, knowledge.URLSaveRequest{
		URL:       url,
		OwnerID:   c.principal.UserID,
		TenantID:  c.principal.TenantID,
		TopicHint: topicHint,
	})
	if err != nil {
		return fmt.Sprintf("Error: failed to save URL: %v", err)
	}
	result := fmt.Sprintf("URL saved to knowledge base. Source ID: %s", source.ID)
	if title != "" || source.Title != "" {
		t := source.Title
		if t == "" {
			t = title
		}
		result += fmt.Sprintf(", Title: %s", t)
	}
	return result
}

func (c *coreAgentCallbacks) executeKnowledgeSaveText(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	text := stringArg(args, "text")
	if text == "" {
		text = stringArg(args, "content")
	}
	if text == "" {
		return "Error: text parameter is required"
	}
	title := stringArg(args, "title")
	topicHint := stringArg(args, "topic_hint")

	ctx, cancel := context.WithTimeout(c.parentContext(), 10*time.Second)
	defer cancel()

	source, err := c.knowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
		Text:      text,
		Title:     title,
		OwnerID:   c.principal.UserID,
		TenantID:  c.principal.TenantID,
		TopicHint: topicHint,
	})
	if err != nil {
		return fmt.Sprintf("Error: failed to save text: %v", err)
	}
	result := fmt.Sprintf("Text saved to knowledge base. Source ID: %s", source.ID)
	if source.Title != "" {
		result += fmt.Sprintf(", Title: %s", source.Title)
	}
	return result
}

func (c *coreAgentCallbacks) executeKnowledgeImportDirectory(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	req := buildDirectoryImportRequest(args, c.principal.TenantID, c.principal.UserID, "root_path", "path", "dir", "directory", "folder", "root")
	if req.RootPath == "" {
		return "Error: root_path parameter is required (aliases: path, dir, directory, folder, root)"
	}
	action, err := knowledgeImportAction(args)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	rootPath, err := c.resolveKnowledgeImportPath(req.RootPath)
	if err != nil {
		return fmt.Sprintf("Error: knowledge import path rejected: %v", err)
	}
	req.RootPath = rootPath

	ctx, cancel := context.WithTimeout(c.parentContext(), 5*time.Minute)
	defer cancel()

	var result knowledge.DirectoryImportResult
	if action == "scan" {
		result, err = c.knowledgeStore.ScanDirectory(ctx, req)
	} else {
		result, err = c.knowledgeStore.ImportDirectory(ctx, req)
	}
	if err != nil {
		return fmt.Sprintf("Error: knowledge directory import failed: %v", err)
	}
	return formatDirectoryImportResult("Directory", result)
}

func (c *coreAgentCallbacks) executeKnowledgeImportFiles(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	filePaths := toFilePathSlice(args["file_paths"])
	if len(filePaths) == 0 {
		filePaths = toFilePathSlice(args["paths"])
	}
	if len(filePaths) == 0 {
		filePaths = toFilePathSlice(args["files"])
	}
	if len(filePaths) == 0 {
		filePaths = toFilePathSlice(args["file_path"])
	}
	if len(filePaths) == 0 {
		filePaths = toFilePathSlice(args["path"])
	}
	if len(filePaths) == 0 {
		return "Error: file_paths parameter is required (aliases: paths, files, file_path, path)"
	}
	req := buildDirectoryImportRequest(args, c.principal.TenantID, c.principal.UserID, "root_path", "dir", "directory", "folder", "root")
	action, err := knowledgeImportAction(args)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	resolvedPaths, err := c.resolveKnowledgeImportPaths(filePaths)
	if err != nil {
		return fmt.Sprintf("Error: knowledge import path rejected: %v", err)
	}
	filePaths = resolvedPaths
	if req.RootPath != "" {
		rootPath, err := c.resolveKnowledgeImportPath(req.RootPath)
		if err != nil {
			return fmt.Sprintf("Error: knowledge import root_path rejected: %v", err)
		}
		req.RootPath = rootPath
	} else {
		rootPath, err := c.resolveKnowledgeWorkspaceRoot()
		if err != nil {
			return fmt.Sprintf("Error: knowledge import root_path rejected: %v", err)
		}
		req.RootPath = rootPath
	}
	if err := ensureKnowledgeImportFilesWithinRoot(filePaths, req.RootPath); err != nil {
		return fmt.Sprintf("Error: knowledge import file rejected: %v", err)
	}

	ctx, cancel := context.WithTimeout(c.parentContext(), 5*time.Minute)
	defer cancel()

	var result knowledge.DirectoryImportResult
	if action == "scan" {
		result, err = c.knowledgeStore.ScanFiles(ctx, req, filePaths)
	} else {
		result, err = c.knowledgeStore.ImportFiles(ctx, req, filePaths)
	}
	if err != nil {
		return fmt.Sprintf("Error: knowledge files import failed: %v", err)
	}
	return formatDirectoryImportResult("Files", result)
}

func knowledgeImportAction(args map[string]interface{}) (string, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	if action == "" {
		return "import", nil
	}
	if action != "scan" && action != "import" {
		return "", fmt.Errorf("unsupported knowledge import action %q; expected scan or import", action)
	}
	return action, nil
}

func (c *coreAgentCallbacks) resolveKnowledgeWorkspaceRoot() (string, error) {
	workspace := strings.TrimSpace(c.workspace)
	if workspace == "" {
		return "", fmt.Errorf("knowledge import requires a workspace-scoped path")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

func (c *coreAgentCallbacks) resolveKnowledgeImportPath(p string) (string, error) {
	if _, err := c.resolveKnowledgeWorkspaceRoot(); err != nil {
		return "", err
	}
	resolved, err := c.resolveWorkspacePath(p)
	if err != nil {
		return "", fmt.Errorf("outside workspace: %w", err)
	}
	return resolved, nil
}

func (c *coreAgentCallbacks) resolveKnowledgeImportPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		absPath, err := c.resolveKnowledgeImportPath(p)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, absPath)
	}
	return resolved, nil
}

func ensureKnowledgeImportFilesWithinRoot(filePaths []string, rootPath string) error {
	for _, filePath := range filePaths {
		if err := ensurePathWithinBase(filePath, rootPath); err != nil {
			return fmt.Errorf("file %q is outside root_path %q: %w", filePath, rootPath, err)
		}
	}
	return nil
}

func toFilePathSlice(v interface{}) []string {
	appendPath := func(result []string, raw string) []string {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == '\n' || r == '\r'
		}) {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
		return result
	}

	switch arr := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = appendPath(result, s)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			result = appendPath(result, item)
		}
		return result
	case string:
		return appendPath(nil, arr)
	default:
		return nil
	}
}

func buildDirectoryImportRequest(args map[string]interface{}, tenantID, userID string, rootKeys ...string) knowledge.DirectoryImportRequest {
	maxFileBytes := int64(intArg(args, "max_file_bytes", 0))
	if maxFileBytes == 0 {
		if maxMB := intArg(args, "max_file_mb", 0); maxMB > 0 {
			maxFileBytes = int64(maxMB) * 1024 * 1024
		}
	}
	req := knowledge.DirectoryImportRequest{
		RootPath:     firstStringArg(args, rootKeys...),
		OwnerID:      userID,
		TenantID:     tenantID,
		ProjectPath:  stringArg(args, "project_path"),
		TopicHint:    stringArg(args, "topic_hint"),
		SaveScope:    stringArg(args, "save_scope"),
		DistillMode:  stringArg(args, "distill_mode"),
		Labels:       toStringSlice(args["labels"]),
		Recursive:    boolArg(args, "recursive", true),
		IncludeExts:  toStringSlice(args["include_exts"]),
		ExcludeGlobs: toStringSlice(args["exclude_globs"]),
		MaxFileBytes: maxFileBytes,
	}
	if _, ok := args["auto_labels"]; ok {
		req.AutoLabels = boolArg(args, "auto_labels", false)
	}
	return req
}

func formatDirectoryImportResult(prefix string, result knowledge.DirectoryImportResult) string {
	return fmt.Sprintf("%s import %s. Batch ID: %s, total=%d, imported=%d, skipped=%d, duplicates=%d, failed=%d", prefix, result.Status, result.BatchID, result.TotalFiles, result.ImportedFiles, result.SkippedFiles, result.DuplicateFiles, result.FailedFiles)
}

// --- Helpers ---

func buildSearchOptions(args map[string]interface{}, tenantID, userID string) knowledge.SearchOptions {
	opts := knowledge.SearchOptions{
		Query:       stringArg(args, "query"),
		OwnerID:     userID,
		TenantID:    tenantID,
		SearchScope: stringArg(args, "search_scope"),
		TopicHint:   stringArg(args, "topic_hint"),
		Domain:      stringArg(args, "domain"),
		ProjectPath: stringArg(args, "project_path"),
		Limit:       intArg(args, "limit", 8),
	}
	if opts.Limit > 50 {
		opts.Limit = 50
	}
	if v, ok := args["context_terms"]; ok {
		opts.ContextTerms = toStringSlice(v)
	}
	if v, ok := args["result_types"]; ok {
		opts.ResultTypes = toStringSlice(v)
	}
	if v, ok := args["source_kinds"]; ok {
		opts.SourceKinds = toStringSlice(v)
	}
	if v, ok := args["source_ids"]; ok {
		opts.SourceIDs = toStringSlice(v)
	}
	if len(opts.SourceIDs) == 0 {
		for _, key := range []string{"source_id", "id"} {
			if v, ok := args[key]; ok {
				opts.SourceIDs = toStringSlice(v)
				break
			}
		}
	}
	if v, ok := args["labels"]; ok {
		opts.Labels = toStringSlice(v)
	}
	if v, ok := args["include_disabled"]; ok {
		if b, ok2 := v.(bool); ok2 {
			opts.IncludeDisabled = b
		}
	}
	return opts
}

func formatSearchResults(results []knowledge.SearchResult) string {
	return knowledge.FormatSearchResultsForLLM(results)
}

func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstStringArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if s := stringArg(args, key); s != "" {
			return s
		}
	}
	return ""
}

func boolArg(args map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		if b, ok2 := v.(bool); ok2 {
			return b
		}
	}
	return defaultVal
}

func intArg(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

func toStringSlice(v interface{}) []string {
	appendValue := func(result []string, raw string) []string {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == ';'
		}) {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
		return result
	}

	switch arr := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = appendValue(result, s)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			result = appendValue(result, item)
		}
		return result
	case string:
		return appendValue(nil, arr)
	default:
		return nil
	}
}
