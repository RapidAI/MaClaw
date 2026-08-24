package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// KnowledgeStore is the interface required by the agent executor for knowledge operations.
// It stores cited documents/cards/facts for retrieval, not Maclaw long-term memory;
// durable user/agent memories, recall, audit, and surgery are owned by
// corelib/memory.Store. Satisfied by *knowledge.SQLiteStore.
type KnowledgeStore interface {
	Search(ctx context.Context, opts knowledge.SearchOptions) ([]knowledge.SearchResult, error)
	SearchImages(ctx context.Context, opts knowledge.ImageSearchOptions) ([]knowledge.SearchResult, error)
	ContextPack(ctx context.Context, opts knowledge.ContextPackOptions) (knowledge.ContextPackResult, error)
	SaveURL(ctx context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error)
	SaveText(ctx context.Context, req knowledge.TextSaveRequest) (knowledge.Source, error)
	ScanDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error)
	ScanFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error)
	ImportDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error)
	ImportFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error)
	Stats(ctx context.Context) (knowledge.Stats, error)

	// Management capabilities (aligned with the MaClaw GUI knowledge tool
	// surface). *knowledge.SQLiteStore already implements all of these.
	ListSources(ctx context.Context, opts knowledge.ListSourcesOptions) ([]knowledge.Source, error)
	ListSourceLabels(ctx context.Context, opts knowledge.ListSourcesOptions) ([]knowledge.SourceLabelSummary, error)
	GetSource(ctx context.Context, id string) (knowledge.Source, error)
	UpdateSourceMetadata(ctx context.Context, req knowledge.SourceUpdateRequest) (knowledge.Source, error)
	UpdateSourceLabels(ctx context.Context, req knowledge.SourceLabelUpdateRequest) (knowledge.SourceLabelUpdateResult, error)
	EnableSource(ctx context.Context, id string) (knowledge.Source, error)
	DisableSource(ctx context.Context, id string) (knowledge.Source, error)
	DeleteSource(ctx context.Context, id string) error
	RefreshSource(ctx context.Context, id string) (knowledge.Source, error)
	PreviewSourceRefresh(ctx context.Context, id string) (knowledge.SourceChangePreview, error)
	ListImportBatches(ctx context.Context, limit int) ([]knowledge.ImportBatch, error)
	GetImportBatch(ctx context.Context, batchID string) (knowledge.ImportBatch, error)
	ListImportItems(ctx context.Context, batchID string, limit int) ([]knowledge.ImportItem, error)
	RetryImportBatch(ctx context.Context, req knowledge.ImportRetryRequest) (knowledge.DirectoryImportResult, error)
	DeleteImportBatch(ctx context.Context, req knowledge.ImportBatchDeleteRequest) (knowledge.ImportBatchDeleteResult, error)
}

// officeReadScopedKnowledgeRefresher is optional so existing knowledge-store
// implementations retain the public management contract. Multi-tenant hosts
// implement it to bind refresh and preview parsing to a request policy.
type officeReadScopedKnowledgeRefresher interface {
	RefreshSourceWithOfficeReadConfig(ctx context.Context, id string, config *agent.OfficeReadConfig) (knowledge.Source, error)
	PreviewSourceRefreshWithOfficeReadConfig(ctx context.Context, id string, config *agent.OfficeReadConfig) (knowledge.SourceChangePreview, error)
}

// knowledgeImageAssetBaseDirProvider is intentionally optional so existing
// knowledge-store implementations retain compatibility. A process-level
// server store may keep its assets outside the per-instance agent DataDir.
type knowledgeImageAssetBaseDirProvider interface {
	ImageAssetBaseDir() string
}

// SetKnowledgeStore wires the knowledge store into the executor.
// Must be called before Execute() to enable knowledge tools.
func (e *CoreAgentExecutor) SetKnowledgeStore(store KnowledgeStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.knowledgeStore = store
}

// SetReviewedHostAuditReader installs the principal-scoped audit reader used
// by security.audit.read. The reader is host-owned; model args never choose
// tenant or user.
func (e *CoreAgentExecutor) SetReviewedHostAuditReader(reader reviewedHostAuditReader) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.auditReader = reader
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostAuditReader() reviewedHostAuditReader {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.auditReader
}

func (c *coreAgentCallbacks) parentContext() context.Context {
	if c != nil && c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

// --- Knowledge tool execution ---

func (c *coreAgentCallbacks) executeKnowledgeSearch(args map[string]interface{}) string {
	opts := buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)
	ctx, cancel := context.WithTimeout(c.parentContext(), 10*time.Second)
	defer cancel()

	var results []knowledge.SearchResult
	var personalErr error
	if c.knowledgeStore != nil {
		results, personalErr = c.knowledgeStore.Search(ctx, opts)
		if personalErr != nil {
			log.Printf("[knowledge_search] personal search error: %v", personalErr)
		}
	}

	// Merge enterprise digital-asset hits (active libraries only) from user dataDir.
	entHits := c.searchEnterpriseKnowledge(ctx, opts.Query)
	if len(entHits) > 0 {
		results = enterpriseknowledge.MergeSearchResults(results, entHits, opts.Limit, true)
	}

	if c.knowledgeStore == nil && strings.TrimSpace(c.dataDir) == "" {
		return "Error: knowledge base is not configured"
	}
	if personalErr != nil && len(results) == 0 {
		return fmt.Sprintf("Error: knowledge search failed: %v", personalErr)
	}
	if len(results) == 0 {
		return knowledge.EmptySearchResultMessage
	}
	return c.formatSearchResults(results, false)
}

// executeKnowledgeImageSearch is the explicit image-evidence route. Unlike
// knowledge_search with source_kinds=image, it guarantees image-node results,
// so an agent can reliably use the accompanying display marker when a user asks
// to view, select, or compare stored images.
func (c *coreAgentCallbacks) executeKnowledgeImageSearch(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	opts := knowledge.ImageSearchOptions{SearchOptions: buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)}
	ctx, cancel := context.WithTimeout(c.parentContext(), 10*time.Second)
	defer cancel()
	results, err := c.knowledgeStore.SearchImages(ctx, opts)
	if err != nil {
		return fmt.Sprintf("Error: knowledge image search failed: %v", err)
	}
	if len(results) == 0 {
		return knowledge.EmptySearchResultMessage
	}
	return c.formatSearchResults(results, true)
}

// searchEnterpriseKnowledge returns active-library enterprise hits for the tool surface.
func (c *coreAgentCallbacks) searchEnterpriseKnowledge(ctx context.Context, query string) []knowledge.SearchResult {
	if c == nil || strings.TrimSpace(c.dataDir) == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	hits, err := enterpriseknowledge.SearchActiveFromDataDir(ctx, c.dataDir, query, "")
	if err != nil {
		log.Printf("[knowledge_search] enterprise search error: %v", err)
		return nil
	}
	return hits
}

// mergeKnowledgeSearchResults kept for tests; delegates to shared package helper.
func mergeKnowledgeSearchResults(personal, enterprise []knowledge.SearchResult, limit int) []knowledge.SearchResult {
	return enterpriseknowledge.MergeSearchResults(personal, enterprise, limit, true)
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
	req.OfficeReadConfig = officeReadConfigPtrFromAppConfig(c.appCfg)
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
	req.OfficeReadConfig = officeReadConfigPtrFromAppConfig(c.appCfg)
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

// formatSearchResults appends a small number of display-safe image markers to
// the evidence text. The agent can copy an exact marker into its final answer
// when the user asks to see an image; desktop chat recognizes the marker and
// renders its thumbnail. No original file path crosses into model context.
func (c *coreAgentCallbacks) formatSearchResults(results []knowledge.SearchResult, includeImageMarkers bool) string {
	formatted := formatSearchResults(results)
	// A general text search may return image nodes incidentally. Do not copy
	// thumbnail bytes into the model context in that case: image display is an
	// explicit user-facing capability, so only knowledge_image_search receives
	// renderable markers. Text evidence remains available through both routes.
	if c == nil || !includeImageMarkers {
		return formatted
	}
	assetBaseDir := c.knowledgeImageAssetBaseDir()
	if assetBaseDir == "" {
		return formatted
	}
	var markers []string
	const maxImageMarkers = 3
	for _, result := range results {
		if len(markers) >= maxImageMarkers {
			break
		}
		embed := knowledge.EmbedImageThumbForSearchResult(result, assetBaseDir)
		if embed == nil {
			continue
		}
		if marker := knowledge.FormatKBImageMarker(embed); marker != "" {
			markers = append(markers, marker)
		}
	}
	if len(markers) == 0 {
		return formatted
	}
	return formatted + "\nImage display markers (copy an exact marker onto its own line only when the user asks to see the image):\n" + strings.Join(markers, "\n")
}

func (c *coreAgentCallbacks) knowledgeImageAssetBaseDir() string {
	if c != nil && c.knowledgeStore != nil {
		if provider, ok := c.knowledgeStore.(knowledgeImageAssetBaseDirProvider); ok {
			if baseDir := strings.TrimSpace(provider.ImageAssetBaseDir()); baseDir != "" {
				return baseDir
			}
		}
	}
	if c == nil || strings.TrimSpace(c.dataDir) == "" {
		return ""
	}
	return filepath.Join(c.dataDir, "knowledge_assets")
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
		switch n := v.(type) {
		case bool:
			return n
		case float64:
			return n != 0
		case int:
			return n != 0
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i != 0
			}
			if f, err := n.Float64(); err == nil {
				return f != 0
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(n)) {
			case "true", "1", "yes":
				return true
			case "false", "0", "no":
				return false
			}
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
		case string:
			raw := strings.TrimSpace(n)
			if i, err := strconv.Atoi(raw); err == nil {
				return i
			}
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return int(f)
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
