package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/scrypt"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type KnowledgeImportJob struct {
	ID        string                          `json:"id"`
	Status    string                          `json:"status"`
	Error     string                          `json:"error,omitempty"`
	Result    knowledge.DirectoryImportResult `json:"result"`
	CreatedAt time.Time                       `json:"created_at"`
	UpdatedAt time.Time                       `json:"updated_at"`
}

type KnowledgeHubShareRequest struct {
	HubURL          string   `json:"hub_url"`
	HubToken        string   `json:"hub_token"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	VisibilityScope string   `json:"visibility_scope"`
	VisibilityUsers []string `json:"visibility_users"`
	TTL             string   `json:"ttl"`
	SourceIDs       []string `json:"source_ids"`
	RedactSensitive bool     `json:"redact_sensitive"`
	IncludeDisabled bool     `json:"include_disabled"`
}

type KnowledgeHubShareResult struct {
	KnowledgeID    string         `json:"knowledge_id"`
	ShareURL       string         `json:"share_url"`
	AgentImport    string         `json:"agent_import"`
	PackageURL     string         `json:"package_url,omitempty"`
	ExpiresAt      string         `json:"expires_at,omitempty"`
	HubURL         string         `json:"hub_url"`
	SourceCount    int            `json:"source_count"`
	ContentSources int            `json:"content_sources,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	SourceSummary  map[string]any `json:"source_summary,omitempty"`
	Raw            map[string]any `json:"raw,omitempty"`
}

type KnowledgeHubShareImportRequest struct {
	HubURL      string `json:"hub_url"`
	HubToken    string `json:"hub_token"`
	KnowledgeID string `json:"knowledge_id"`
	ShareLink   string `json:"share_link"`
	DryRun      bool   `json:"dry_run"`
}

type KnowledgeHubShareImportResult struct {
	KnowledgeID string         `json:"knowledge_id"`
	PackageID   string         `json:"package_id,omitempty"`
	Title       string         `json:"title,omitempty"`
	DryRun      bool           `json:"dry_run"`
	Imported    int            `json:"imported"`
	Skipped     int            `json:"skipped"`
	Warnings    []string       `json:"warnings,omitempty"`
	Share       map[string]any `json:"share,omitempty"`
}

type KnowledgeSyncRequest struct {
	HubURL           string `json:"hub_url"`
	HubToken         string `json:"hub_token"`
	Password         string `json:"password"`
	ConflictStrategy string `json:"conflict_strategy,omitempty"`
}

type KnowledgeSyncStatus struct {
	OwnerUserID         string         `json:"owner_user_id,omitempty"`
	TenantID            string         `json:"tenant_id,omitempty"`
	PackageID           string         `json:"package_id,omitempty"`
	PackageVersion      int            `json:"package_version,omitempty"`
	CompressedSizeBytes int64          `json:"compressed_size_bytes,omitempty"`
	StoredSizeBytes     int64          `json:"stored_size_bytes,omitempty"`
	CreatedAt           string         `json:"created_at,omitempty"`
	UpdatedAt           string         `json:"updated_at,omitempty"`
	ExpiresAt           string         `json:"expires_at,omitempty"`
	ServiceStatus       string         `json:"service_status"`
	ReadonlyReason      string         `json:"readonly_reason,omitempty"`
	LimitBytes          int64          `json:"limit_bytes"`
	RetentionDays       int            `json:"retention_days,omitempty"`
	Encryption          map[string]any `json:"encryption,omitempty"`
	HasPackage          bool           `json:"has_package"`
	Message             string         `json:"message,omitempty"`
}

type KnowledgeSyncResult struct {
	KnowledgeSyncStatus
	Imported           int                     `json:"imported,omitempty"`
	Skipped            int                     `json:"skipped,omitempty"`
	Warnings           []string                `json:"warnings,omitempty"`
	Conflicts          []KnowledgeSyncConflict `json:"conflicts,omitempty"`
	RequiresResolution bool                    `json:"requires_resolution,omitempty"`
}

type KnowledgeSyncConflict struct {
	RemoteID    string `json:"remote_id,omitempty"`
	LocalID     string `json:"local_id,omitempty"`
	Title       string `json:"title,omitempty"`
	URI         string `json:"uri,omitempty"`
	ConflictKey string `json:"conflict_key,omitempty"`
	Reason      string `json:"reason"`
}

type guiKnowledgePackageManifest struct {
	Format      string `json:"format"`
	Version     int    `json:"version"`
	PackageID   string `json:"package_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	TenantID    string `json:"tenant_id,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	SourceCount int    `json:"source_count"`
	Editable    bool   `json:"editable"`
	Notes       string `json:"notes,omitempty"`
}

type guiKnowledgePackageSource struct {
	ID           string   `json:"id,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	URI          string   `json:"uri,omitempty"`
	CanonicalURI string   `json:"canonical_uri,omitempty"`
	Title        string   `json:"title,omitempty"`
	Author       string   `json:"author,omitempty"`
	SiteName     string   `json:"site_name,omitempty"`
	TopicHint    string   `json:"topic_hint,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Status       string   `json:"status,omitempty"`
	RelativePath string   `json:"relative_path,omitempty"`
	BatchID      string   `json:"batch_id,omitempty"`
	ContentHash  string   `json:"content_hash,omitempty"`
	NodeCount    int      `json:"node_count,omitempty"`
	CardCount    int      `json:"card_count,omitempty"`
	FactCount    int      `json:"fact_count,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
	Content      string   `json:"content,omitempty"`
	ContentBytes int      `json:"content_bytes,omitempty"`
	Truncated    bool     `json:"content_truncated,omitempty"`
}

type guiKnowledgePackage struct {
	Manifest guiKnowledgePackageManifest `json:"manifest"`
	Sources  []guiKnowledgePackageSource `json:"sources"`
}

const (
	maxGUIKnowledgePackageSourceContentBytes = 8 << 20
	maxGUIKnowledgePackageTotalContentBytes  = 32 << 20
	maxGUIKnowledgePackageSourceNodes        = 1000
)

var knowledgeImportJobs sync.Map

func (a *App) knowledgeDBPath() string {
	return filepath.Join(a.GetDataDir(), "knowledge.db")
}

// KnowledgeClearAll removes all knowledge base content by deleting all records
// and running VACUUM to reclaim disk space. This avoids file-lock conflicts on
// Windows where deleting an open database file fails.
func (a *App) KnowledgeClearAll() error {
	// If the DB file doesn't exist, there's nothing to clear.
	if _, err := os.Stat(a.knowledgeDBPath()); os.IsNotExist(err) {
		return nil
	}

	// Close the cached auto-recall store so it doesn't hold stale data
	// and to avoid it reading partially-cleared state during purge.
	CloseAutoRecallStore()

	store, err := a.openKnowledgeStore()
	if err != nil {
		return fmt.Errorf("open knowledge store for purge: %w", err)
	}
	defer store.Close()

	ctx := a.knowledgeContext()
	if err := store.PurgeAll(ctx); err != nil {
		return fmt.Errorf("purge knowledge base: %w", err)
	}

	// Reset the source count cache so hasKnowledgeSources returns false immediately.
	atomic.StoreInt64(&knowledgeSourceCountCache, 0)
	atomic.StoreInt64(&knowledgeSourceCountTime, time.Now().Unix())
	log.Printf("[knowledge] ClearAll: all records deleted and database vacuumed")
	return nil
}

func (a *App) openKnowledgeStore() (*knowledge.SQLiteStore, error) {
	return a.openKnowledgeStoreWithRetry(a.knowledgeContext())
}

func (a *App) openKnowledgeStoreWithRetry(ctx context.Context) (*knowledge.SQLiteStore, error) {
	return openKnowledgeStoreWithRetry(ctx, func() (*knowledge.SQLiteStore, error) {
		store, err := knowledge.NewSQLiteStore(a.knowledgeDBPath())
		if err != nil {
			return nil, err
		}
		if distiller := a.buildKnowledgeCardDistiller(); distiller != nil {
			store.SetCardDistiller(distiller)
		}
		return store, nil
	}, sleepKnowledgeStoreRetry)
}

func openKnowledgeStoreWithRetry(ctx context.Context, open func() (*knowledge.SQLiteStore, error), wait func(context.Context, time.Duration) bool) (*knowledge.SQLiteStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if wait == nil {
		wait = sleepKnowledgeStoreRetry
	}
	delay := 50 * time.Millisecond
	for attempt := 0; attempt < 6; attempt++ {
		store, err := open()
		if err == nil || !knowledge.IsSQLiteLockedError(err) || attempt == 5 {
			return store, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !wait(ctx, delay) {
			return nil, ctx.Err()
		}
		delay *= 2
	}
	return open()
}

func sleepKnowledgeStoreRetry(ctx context.Context, delay time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *App) buildKnowledgeCardDistiller() knowledge.CardDistiller {
	if a == nil {
		return nil
	}
	if strings.TrimSpace(a.testHomeDir) != "" {
		return nil
	}
	caller := &knowledgeCardLLMCaller{
		cfg:     a.GetMaclawLLMConfig(),
		client:  &http.Client{Timeout: 45 * time.Second},
		timeout: 45 * time.Second,
	}
	if !caller.IsConfigured() {
		return nil
	}
	return &knowledge.LLMCardDistiller{Caller: caller, MaxInputRune: 12000}
}

type knowledgeCardLLMCaller struct {
	cfg     corelib.MaclawLLMConfig
	client  *http.Client
	timeout time.Duration
}

func (c *knowledgeCardLLMCaller) ChatCall(messages []map[string]string) (string, error) {
	return c.ChatCallContext(context.Background(), messages)
}

func (c *knowledgeCardLLMCaller) ChatCallContext(ctx context.Context, messages []map[string]string) (string, error) {
	if c == nil || !c.IsConfigured() {
		return "", fmt.Errorf("MaClaw LLM is not configured")
	}
	ifaces := make([]interface{}, len(messages))
	for i, message := range messages {
		ifaces[i] = message
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llm.WithRequestTraceIfMissing(ctx, "knowledge-card")
	resp, err := doSimpleLLMRequest(ctx, c.cfg, ifaces, client, timeout)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (c *knowledgeCardLLMCaller) IsConfigured() bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.cfg.URL) != "" && strings.TrimSpace(c.cfg.Model) != ""
}

func (a *App) knowledgeContext() context.Context {
	if a != nil && a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) SelectKnowledgeDirectory() string {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Knowledge Directory",
	})
	if err != nil {
		return ""
	}
	return selection
}

func (a *App) SelectKnowledgeFiles() []string {
	selections, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Knowledge Documents",
		Filters: []runtime.FileFilter{
			{DisplayName: "Documents (*.docx, *.pdf, *.pptx, *.xlsx, *.md, *.txt, *.doc, *.xls)", Pattern: "*.docx;*.pdf;*.pptx;*.xlsx;*.md;*.txt;*.doc;*.xls"},
		},
	})
	if err != nil {
		return []string{}
	}
	return selections
}

func (a *App) SelectKnowledgeSnapshotFile() string {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Knowledge Snapshot",
		Filters: []runtime.FileFilter{
			{DisplayName: "Knowledge Snapshot (*.jsonl)", Pattern: "*.jsonl"},
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return ""
	}
	return selection
}

func (a *App) SelectKnowledgeSnapshotExportPath() string {
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Knowledge Snapshot",
		DefaultFilename: fmt.Sprintf("maclaw-knowledge-%s.jsonl", time.Now().UTC().Format("20060102-150405")),
		Filters: []runtime.FileFilter{
			{DisplayName: "Knowledge Snapshot (*.jsonl)", Pattern: "*.jsonl"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return ""
	}
	return savePath
}

// ExportTextFile shows a save dialog and writes the given text content to the chosen file.
// Returns the saved file path, or empty string if the user cancelled.
func (a *App) ExportTextFile(content string, defaultFilename string) (string, error) {
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Report",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "Markdown (*.md)", Pattern: "*.md"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if savePath == "" {
		return "", nil
	}
	if err := os.WriteFile(savePath, []byte(content), 0644); err != nil {
		return "", err
	}
	return savePath, nil
}

func (a *App) KnowledgeStats() (knowledge.Stats, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Stats{}, err
	}
	defer store.Close()
	return store.Stats(a.knowledgeContext())
}

func (a *App) KnowledgeDoctor() (knowledge.DoctorResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DoctorResult{}, err
	}
	defer store.Close()
	return store.Doctor(a.knowledgeContext())
}

func (a *App) KnowledgeSourceQualityReport(opts knowledge.ListSourcesOptions) (knowledge.SourceQualityReport, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceQualityReport{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.SourceQualityReport(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeSourceQualityMaintenancePlan(opts knowledge.ListSourcesOptions) (knowledge.SourceQualityMaintenancePlan, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceQualityMaintenancePlan{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.SourceQualityMaintenancePlan(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeQualityMaintenancePolicies() []knowledge.SourceQualityMaintenancePolicy {
	return knowledge.SourceQualityMaintenancePolicies()
}

func (a *App) KnowledgeExecuteSourceQualityMaintenancePlan(req knowledge.SourceQualityMaintenanceExecuteRequest) (knowledge.SourceQualityMaintenanceExecuteResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceQualityMaintenanceExecuteResult{}, err
	}
	defer store.Close()
	req.Filter = a.normalizeKnowledgeListOptions(req.Filter)
	return store.ExecuteSourceQualityMaintenancePlan(a.knowledgeContext(), req)
}

func (a *App) KnowledgeCapabilities() knowledge.KnowledgeCapabilities {
	return knowledge.Capabilities()
}

func (a *App) KnowledgeListURLDomainPolicies() ([]knowledge.URLDomainPolicy, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListURLDomainPolicies(a.knowledgeContext())
}

func (a *App) KnowledgeUpdateURLDomainPolicies(req knowledge.URLDomainPolicyUpdateRequest) (knowledge.URLDomainPolicyUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.URLDomainPolicyUpdateResult{}, err
	}
	defer store.Close()
	return store.UpdateURLDomainPolicies(a.knowledgeContext(), req)
}

func (a *App) KnowledgeCheckURLDomainPolicy(rawURL string) (knowledge.URLDomainPolicyCheck, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.URLDomainPolicyCheck{}, err
	}
	defer store.Close()
	return store.CheckURLDomainPolicy(a.knowledgeContext(), rawURL)
}

func (a *App) KnowledgeMaintain(vacuum bool) (knowledge.MaintenanceResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.MaintenanceResult{}, err
	}
	defer store.Close()
	return store.Maintain(a.knowledgeContext(), vacuum), nil
}

func (a *App) KnowledgeExportSnapshot(outputPath string, redactSensitive bool) (knowledge.ExportResult, error) {
	return a.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{OutputPath: outputPath, RedactSensitive: redactSensitive})
}

func (a *App) KnowledgeExportSnapshotWithOptions(req knowledge.ExportOptions) (knowledge.ExportResult, error) {
	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		outputPath = filepath.Join(a.GetDataDir(), "knowledge-exports", fmt.Sprintf("knowledge-export-%s.jsonl", time.Now().UTC().Format("20060102-150405")))
	}
	req.OutputPath = outputPath
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	defer store.Close()
	return store.ExportSnapshot(a.knowledgeContext(), req)
}

func (a *App) KnowledgeImportSnapshot(req knowledge.SnapshotImportOptions) (knowledge.SnapshotImportResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SnapshotImportResult{}, err
	}
	defer store.Close()
	return store.ImportSnapshot(a.knowledgeContext(), req)
}

func (a *App) KnowledgeShareToHub(req KnowledgeHubShareRequest) (KnowledgeHubShareResult, error) {
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return KnowledgeHubShareResult{}, fmt.Errorf("knowledge description is required")
	}
	cfg, _ := a.LoadConfig()
	hubURL := strings.TrimRight(strings.TrimSpace(req.HubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	}
	if hubURL == "" {
		return KnowledgeHubShareResult{}, fmt.Errorf("hub_url is required")
	}
	if parsed, err := url.Parse(hubURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return KnowledgeHubShareResult{}, fmt.Errorf("hub_url must be an absolute URL")
	}
	token := strings.TrimSpace(req.HubToken)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	if token == "" {
		return KnowledgeHubShareResult{}, fmt.Errorf("hub token is required")
	}
	ttl := strings.TrimSpace(req.TTL)
	if ttl == "" {
		ttl = "7d"
	}

	store, err := a.openKnowledgeStore()
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	defer store.Close()

	opts := knowledge.ListSourcesOptions{
		SourceIDs: compactKnowledgeSourceIDStrings(req.SourceIDs),
		Limit:     5000,
	}
	if !req.IncludeDisabled {
		opts.Status = "active"
	}
	sources, err := store.ListSources(a.knowledgeContext(), opts)
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	if len(sources) == 0 {
		return KnowledgeHubShareResult{}, fmt.Errorf("no knowledge sources match the share request")
	}
	exportedSourceIDs := knowledgeSourceIDs(sources)
	pkg, packageWarnings, err := buildGUIKnowledgePackage(a.knowledgeContext(), store, cfg, strings.TrimSpace(req.Title), description, sources, req.RedactSensitive)
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	rawPackage, err := json.Marshal(pkg)
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	sourceSummary := map[string]any{
		"source_count":     len(sources),
		"source_ids":       exportedSourceIDs,
		"redact_sensitive": req.RedactSensitive,
		"include_disabled": req.IncludeDisabled,
		"package_format":   pkg.Manifest.Format,
		"package_id":       pkg.Manifest.PackageID,
		"generated_by":     "maclaw-gui",
		"generated_at":     pkg.Manifest.CreatedAt,
		"editable":         true,
		"content_sources":  countGUIKnowledgePackageContentSources(pkg),
		"warnings":         packageWarnings,
	}
	payload := map[string]any{
		"title":            strings.TrimSpace(req.Title),
		"description":      description,
		"visibility_scope": strings.TrimSpace(req.VisibilityScope),
		"visibility_users": compactKnowledgeShareStrings(req.VisibilityUsers),
		"ttl":              ttl,
		"package_json":     json.RawMessage(rawPackage),
		"source_summary":   sourceSummary,
	}
	if payload["visibility_scope"] == "" {
		payload["visibility_scope"] = "hub"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 30*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/api/knowledge/shares", bytes.NewReader(body))
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeHubShareResult{}, fmt.Errorf("share knowledge to hub: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeHubShareResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeHubShareResult{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var view map[string]any
	if err := json.Unmarshal(respBody, &view); err != nil {
		return KnowledgeHubShareResult{}, fmt.Errorf("decode hub share response: %w", err)
	}
	resultSourceSummary := sourceSummary
	if responseSummary, ok := view["source_summary"].(map[string]any); ok && len(responseSummary) > 0 {
		resultSourceSummary = mergeKnowledgeShareSummary(sourceSummary, responseSummary)
	}
	result := KnowledgeHubShareResult{
		KnowledgeID:    stringFromAny(view["knowledge_id"]),
		ShareURL:       absoluteHubShareField(hubURL, stringFromAny(view["share_url"])),
		AgentImport:    absoluteHubShareField(hubURL, stringFromAny(view["agent_import"])),
		PackageURL:     absoluteHubShareField(hubURL, stringFromAny(view["package_url"])),
		ExpiresAt:      stringFromAny(view["expires_at"]),
		HubURL:         hubURL,
		SourceCount:    len(sources),
		ContentSources: intFromAny(resultSourceSummary["content_sources"]),
		Warnings:       knowledgeShareStringSliceFromAny(resultSourceSummary["warnings"]),
		SourceSummary:  resultSourceSummary,
		Raw:            view,
	}
	if result.AgentImport == "" && result.KnowledgeID != "" {
		result.AgentImport = hubURL + "/api/knowledge/shares/" + url.PathEscape(result.KnowledgeID) + "?intent=import"
	}
	if result.ShareURL == "" && result.KnowledgeID != "" {
		result.ShareURL = hubURL + "/hub/knowledge/shares/" + url.PathEscape(result.KnowledgeID)
	}
	return result, nil
}

func (a *App) KnowledgeImportHubShare(req KnowledgeHubShareImportRequest) (KnowledgeHubShareImportResult, error) {
	cfg, _ := a.LoadConfig()
	hubURL := strings.TrimRight(strings.TrimSpace(req.HubURL), "/")
	token := strings.TrimSpace(req.HubToken)
	apiURL, knowledgeID, err := resolveGUIKnowledgeShareAPIURL(req.ShareLink, req.KnowledgeID, hubURL)
	if err != nil {
		if hubURL == "" {
			hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
		}
		apiURL, knowledgeID, err = resolveGUIKnowledgeShareAPIURL(req.ShareLink, req.KnowledgeID, hubURL)
		if err != nil {
			return KnowledgeHubShareImportResult{}, err
		}
	}
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	authHeader := knowledgeShareBearerToken(token)
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 45*time.Second)
	defer cancel()
	share, err := fetchGUIKnowledgeShareJSON(ctx, apiURL, authHeader)
	if err != nil {
		return KnowledgeHubShareImportResult{}, err
	}
	packageURL := resolveGUIKnowledgePackageURL(apiURL, stringFromAny(share["package_url"]))
	if packageURL == "" {
		return KnowledgeHubShareImportResult{}, fmt.Errorf("knowledge share does not expose a package_url")
	}
	pkg, err := fetchGUIKnowledgePackage(ctx, packageURL, authHeader)
	if err != nil {
		return KnowledgeHubShareImportResult{}, err
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return KnowledgeHubShareImportResult{}, fmt.Errorf("unsupported knowledge package format")
	}
	if len(pkg.Sources) == 0 {
		return KnowledgeHubShareImportResult{}, fmt.Errorf("knowledge package has no sources")
	}
	result := KnowledgeHubShareImportResult{
		KnowledgeID: knowledgeID,
		PackageID:   pkg.Manifest.PackageID,
		Title:       firstNonEmptyKnowledgeValue(pkg.Manifest.Title, stringFromAny(share["title"])),
		DryRun:      req.DryRun,
		Share:       share,
	}
	// Convert to canonical shared type.
	sources := make([]knowledge.PackageSource, 0, len(pkg.Sources))
	for _, item := range pkg.Sources {
		sources = append(sources, knowledge.PackageSource{
			ID:           item.ID,
			Kind:         item.Kind,
			URI:          item.URI,
			CanonicalURI: item.CanonicalURI,
			Title:        item.Title,
			TopicHint:    item.TopicHint,
			Labels:       item.Labels,
			Content:      item.Content,
		})
	}
	if req.DryRun {
		// Dry-run uses the same classification logic as real import — no store needed.
		importResult := knowledge.ImportPackageSources(ctx, nil, sources, knowledge.PackageImportOptions{
			DryRun: true,
		})
		result.Imported = importResult.Imported
		result.Skipped = importResult.Skipped
		result.Warnings = append(result.Warnings, importResult.Warnings...)
		// Append truncation warnings (dry-run specific — alerts user before commit).
		for _, item := range pkg.Sources {
			if item.Truncated {
				result.Warnings = append(result.Warnings, fmt.Sprintf("source %s content is truncated", firstNonEmptyKnowledgeValue(item.ID, item.Title, item.URI)))
			}
		}
		return result, nil
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return result, err
	}
	defer store.Close()
	importResult := knowledge.ImportPackageSources(ctx, store, sources, knowledge.PackageImportOptions{
		SaveScope: knowledge.SaveScopePersonal,
	})
	result.Imported = importResult.Imported
	result.Skipped = importResult.Skipped
	result.Warnings = append(result.Warnings, importResult.Warnings...)
	return result, nil
}

func (a *App) KnowledgeSyncStatus(req KnowledgeSyncRequest) (KnowledgeSyncStatus, error) {
	hubURL, token, err := a.resolveKnowledgeSyncHub(req)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+"/api/knowledge/sync/status", nil)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeSyncStatus{}, fmt.Errorf("query knowledge sync status: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeSyncStatus{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var status KnowledgeSyncStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return KnowledgeSyncStatus{}, fmt.Errorf("decode knowledge sync status: %w", err)
	}
	return status, nil
}

func (a *App) KnowledgeSyncUpload(req KnowledgeSyncRequest) (KnowledgeSyncResult, error) {
	password := strings.TrimSpace(req.Password)
	if password == "" {
		return KnowledgeSyncResult{}, fmt.Errorf("sync password is required")
	}
	hubURL, token, err := a.resolveKnowledgeSyncHub(req)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	defer store.Close()
	sources, err := store.ListSources(a.knowledgeContext(), knowledge.ListSourcesOptions{Limit: 5000})
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	if len(sources) == 0 {
		return KnowledgeSyncResult{}, fmt.Errorf("no knowledge sources to sync")
	}
	cfg, _ := a.LoadConfig()
	pkg, warnings, err := buildGUIKnowledgePackage(a.knowledgeContext(), store, cfg, "Knowledge Sync", "Encrypted manual knowledge sync package", sources, false)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	rawPackage, err := json.Marshal(pkg)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	encrypted, encryption, compressedSize, err := encryptKnowledgeSyncPackage(rawPackage, password)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	payload := map[string]any{
		"package_id":            pkg.Manifest.PackageID,
		"package_version":       pkg.Manifest.Version,
		"compressed_size_bytes": compressedSize,
		"encryption":            encryption,
		"payload_base64":        base64.StdEncoding.EncodeToString(encrypted),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 90*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, hubURL+"/api/knowledge/sync/package", bytes.NewReader(body))
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeSyncResult{}, fmt.Errorf("upload knowledge sync package: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeSyncResult{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var status KnowledgeSyncStatus
	if err := json.Unmarshal(respBody, &status); err != nil {
		return KnowledgeSyncResult{}, fmt.Errorf("decode knowledge sync upload response: %w", err)
	}
	return KnowledgeSyncResult{KnowledgeSyncStatus: status, Warnings: warnings}, nil
}

func (a *App) KnowledgeSyncDownload(req KnowledgeSyncRequest) (KnowledgeSyncResult, error) {
	password := strings.TrimSpace(req.Password)
	if password == "" {
		return KnowledgeSyncResult{}, fmt.Errorf("sync password is required")
	}
	status, rawPackage, err := a.fetchAndDecryptKnowledgeSyncPackage(req)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	var pkg guiKnowledgePackage
	if err := json.Unmarshal(rawPackage, &pkg); err != nil {
		return KnowledgeSyncResult{}, fmt.Errorf("decode knowledge sync package: %w", err)
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return KnowledgeSyncResult{}, fmt.Errorf("unsupported knowledge package format")
	}
	sources := make([]knowledge.PackageSource, 0, len(pkg.Sources))
	for _, item := range pkg.Sources {
		sources = append(sources, knowledge.PackageSource{
			ID:           item.ID,
			Kind:         item.Kind,
			URI:          item.URI,
			CanonicalURI: item.CanonicalURI,
			Title:        item.Title,
			TopicHint:    item.TopicHint,
			Labels:       item.Labels,
			Content:      item.Content,
		})
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	defer store.Close()
	ctx := a.knowledgeContext()
	conflicts, err := a.knowledgeSyncConflicts(ctx, store, pkg.Sources)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	strategy := strings.ToLower(strings.TrimSpace(req.ConflictStrategy))
	skippedConflicts := 0
	if strategy == "" {
		strategy = "check"
	}
	if strategy != "check" && strategy != "skip" && strategy != "import" {
		return KnowledgeSyncResult{}, fmt.Errorf("unsupported knowledge sync conflict strategy %q", strategy)
	}
	if strategy == "check" {
		return KnowledgeSyncResult{
			KnowledgeSyncStatus: status,
			Conflicts:           conflicts,
			RequiresResolution:  len(conflicts) > 0,
			Warnings:            knowledgeSyncConflictWarnings(conflicts),
		}, nil
	}
	if len(conflicts) > 0 && strategy == "skip" {
		conflictRemoteIDs := map[string]struct{}{}
		for _, conflict := range conflicts {
			if strings.TrimSpace(conflict.RemoteID) != "" {
				conflictRemoteIDs[normalizeKnowledgeSyncConflictKey(conflict.RemoteID)] = struct{}{}
			}
			if strings.TrimSpace(conflict.ConflictKey) != "" {
				conflictRemoteIDs[normalizeKnowledgeSyncConflictKey(conflict.ConflictKey)] = struct{}{}
			}
		}
		filtered := sources[:0]
		for _, source := range sources {
			sourceKeys := []string{
				normalizeKnowledgeSyncConflictKey(source.ID),
				normalizeKnowledgeSyncConflictKey(firstNonEmptyKnowledgeValue(source.CanonicalURI, source.URI)),
				normalizeKnowledgeSyncConflictKey(source.Title),
			}
			conflicted := false
			for _, key := range sourceKeys {
				if key == "" {
					continue
				}
				if _, ok := conflictRemoteIDs[key]; ok {
					conflicted = true
					break
				}
			}
			if conflicted {
				skippedConflicts++
				continue
			}
			filtered = append(filtered, source)
		}
		sources = filtered
	}
	importResult := knowledge.ImportPackageSources(ctx, store, sources, knowledge.PackageImportOptions{SaveScope: knowledge.SaveScopePersonal})
	warnings := append([]string{}, importResult.Warnings...)
	if len(conflicts) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d conflicting source(s) handled with strategy %q", len(conflicts), firstNonEmptyKnowledgeValue(strategy, "import")))
	}
	return KnowledgeSyncResult{
		KnowledgeSyncStatus: status,
		Imported:            importResult.Imported,
		Skipped:             importResult.Skipped + skippedConflicts,
		Warnings:            warnings,
		Conflicts:           conflicts,
	}, nil
}

func (a *App) KnowledgeSyncVerifyPassword(req KnowledgeSyncRequest) (KnowledgeSyncStatus, error) {
	if strings.TrimSpace(req.Password) == "" {
		return KnowledgeSyncStatus{}, fmt.Errorf("sync password is required")
	}
	status, rawPackage, err := a.fetchAndDecryptKnowledgeSyncPackage(req)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	var pkg guiKnowledgePackage
	if err := json.Unmarshal(rawPackage, &pkg); err != nil {
		return KnowledgeSyncStatus{}, fmt.Errorf("decode knowledge sync package: %w", err)
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return KnowledgeSyncStatus{}, fmt.Errorf("unsupported knowledge package format")
	}
	return status, nil
}

func (a *App) KnowledgeSyncDelete(req KnowledgeSyncRequest) (KnowledgeSyncStatus, error) {
	hubURL, token, err := a.resolveKnowledgeSyncHub(req)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, hubURL+"/api/knowledge/sync/package", nil)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeSyncStatus{}, fmt.Errorf("delete knowledge sync package: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeSyncStatus{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return a.KnowledgeSyncStatus(req)
}

func (a *App) fetchAndDecryptKnowledgeSyncPackage(req KnowledgeSyncRequest) (KnowledgeSyncStatus, []byte, error) {
	password := strings.TrimSpace(req.Password)
	if password == "" {
		return KnowledgeSyncStatus{}, nil, fmt.Errorf("sync password is required")
	}
	hubURL, token, err := a.resolveKnowledgeSyncHub(req)
	if err != nil {
		return KnowledgeSyncStatus{}, nil, err
	}
	status, err := a.KnowledgeSyncStatus(req)
	if err != nil {
		return KnowledgeSyncStatus{}, nil, err
	}
	if !status.HasPackage {
		return KnowledgeSyncStatus{}, nil, fmt.Errorf("no cloud sync package is available")
	}
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 90*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+"/api/knowledge/sync/package", nil)
	if err != nil {
		return KnowledgeSyncStatus{}, nil, err
	}
	httpReq.Header.Set("Accept", "application/octet-stream")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeSyncStatus{}, nil, fmt.Errorf("download knowledge sync package: %w", err)
	}
	defer resp.Body.Close()
	encrypted, err := io.ReadAll(io.LimitReader(resp.Body, 520<<20))
	if err != nil {
		return KnowledgeSyncStatus{}, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeSyncStatus{}, nil, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(encrypted)))
	}
	rawPackage, err := decryptKnowledgeSyncPackage(encrypted, password, status.Encryption)
	if err != nil {
		return KnowledgeSyncStatus{}, nil, err
	}
	return status, rawPackage, nil
}

func (a *App) knowledgeSyncConflicts(ctx context.Context, store *knowledge.SQLiteStore, remote []guiKnowledgePackageSource) ([]KnowledgeSyncConflict, error) {
	local, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 5000, Status: "all"})
	if err != nil {
		return nil, err
	}
	byID := map[string]knowledge.Source{}
	byURI := map[string]knowledge.Source{}
	byTitle := map[string]knowledge.Source{}
	for _, source := range local {
		if key := strings.TrimSpace(source.ID); key != "" {
			byID[key] = source
		}
		if key := normalizeKnowledgeSyncConflictKey(firstNonEmptyKnowledgeValue(source.CanonicalURI, source.URI)); key != "" {
			byURI[key] = source
		}
		if key := normalizeKnowledgeSyncConflictKey(source.Title); key != "" {
			byTitle[key] = source
		}
	}
	conflicts := make([]KnowledgeSyncConflict, 0)
	seen := map[string]struct{}{}
	add := func(remote guiKnowledgePackageSource, local knowledge.Source, reason, key string) {
		remoteID := strings.TrimSpace(remote.ID)
		localID := strings.TrimSpace(local.ID)
		seenKey := remoteID + "\x00" + localID + "\x00" + reason
		if _, ok := seen[seenKey]; ok {
			return
		}
		seen[seenKey] = struct{}{}
		conflicts = append(conflicts, KnowledgeSyncConflict{
			RemoteID:    remoteID,
			LocalID:     localID,
			Title:       firstNonEmptyKnowledgeValue(remote.Title, local.Title),
			URI:         firstNonEmptyKnowledgeValue(remote.CanonicalURI, remote.URI, local.CanonicalURI, local.URI),
			ConflictKey: key,
			Reason:      reason,
		})
	}
	for _, item := range remote {
		if local, ok := byID[strings.TrimSpace(item.ID)]; ok {
			add(item, local, "same_source_id", strings.TrimSpace(item.ID))
			continue
		}
		if key := normalizeKnowledgeSyncConflictKey(firstNonEmptyKnowledgeValue(item.CanonicalURI, item.URI)); key != "" {
			if local, ok := byURI[key]; ok {
				add(item, local, "same_uri", key)
				continue
			}
		}
		if key := normalizeKnowledgeSyncConflictKey(item.Title); key != "" {
			if local, ok := byTitle[key]; ok {
				add(item, local, "same_title", key)
			}
		}
	}
	return conflicts, nil
}

func normalizeKnowledgeSyncConflictKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func knowledgeSyncConflictWarnings(conflicts []KnowledgeSyncConflict) []string {
	if len(conflicts) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%d local knowledge conflict(s) found", len(conflicts))}
}

func (a *App) KnowledgeListSources(opts knowledge.ListSourcesOptions) ([]knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.ListSources(a.knowledgeContext(), opts)
}

func buildGUIKnowledgePackage(ctx context.Context, store *knowledge.SQLiteStore, cfg corelib.AppConfig, title, description string, sources []knowledge.Source, redactSensitive bool) (guiKnowledgePackage, []string, error) {
	now := time.Now().UTC()
	packageID := fmt.Sprintf("kxp_%s_%d", now.Format("20060102T150405Z"), now.UnixNano())
	items := make([]guiKnowledgePackageSource, 0, len(sources))
	warnings := make([]string, 0)
	remainingContentBytes := maxGUIKnowledgePackageTotalContentBytes
	for _, source := range sources {
		item := guiKnowledgePackageSource{
			ID:           source.ID,
			Kind:         source.Kind,
			URI:          knowledgeShareExportURI(source.URI, redactSensitive),
			CanonicalURI: knowledgeShareExportURI(source.CanonicalURI, redactSensitive),
			Title:        source.Title,
			Author:       source.Author,
			SiteName:     source.SiteName,
			TopicHint:    source.TopicHint,
			Labels:       append([]string(nil), source.Labels...),
			Status:       source.Status,
			RelativePath: knowledgeShareExportPath(source.RelativePath, redactSensitive),
			BatchID:      source.BatchID,
			ContentHash:  source.ContentHash,
			NodeCount:    source.NodeCount,
			CardCount:    source.CardCount,
			FactCount:    source.FactCount,
			CreatedAt:    knowledgeShareTime(source.CreatedAt),
			UpdatedAt:    knowledgeShareTime(source.UpdatedAt),
		}
		content, truncated, contentWarnings, err := guiKnowledgePackageSourceContent(ctx, store, source, redactSensitive, remainingContentBytes)
		warnings = append(warnings, contentWarnings...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("source %s content unavailable: %v", firstNonEmptyKnowledgeValue(source.ID, source.Title, source.URI), err))
		} else if content != "" {
			item.Content = content
			item.ContentBytes = len([]byte(content))
			item.Truncated = truncated
			remainingContentBytes -= item.ContentBytes
			if remainingContentBytes < 0 {
				remainingContentBytes = 0
			}
		}
		items = append(items, item)
	}
	return guiKnowledgePackage{
		Manifest: guiKnowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   packageID,
			Title:       title,
			Description: description,
			CreatedAt:   now.Format(time.RFC3339),
			TenantID:    cfg.RemoteTenantID,
			OwnerID:     firstNonEmptyKnowledgeValue(cfg.RemoteUserID, cfg.RemoteEmail),
			SourceCount: len(items),
			Editable:    true,
			Notes:       "Editable JSON package created by Maclaw GUI. Source content is included when available so share links can be imported by agents on another machine; sensitive text is redacted when requested.",
		},
		Sources: items,
	}, warnings, nil
}

func guiKnowledgePackageSourceContent(ctx context.Context, store *knowledge.SQLiteStore, source knowledge.Source, redactSensitive bool, remainingBudget int) (string, bool, []string, error) {
	sourceLabel := firstNonEmptyKnowledgeValue(source.ID, source.Title, source.URI)
	warnings := []string{}
	if store == nil || strings.TrimSpace(source.ID) == "" || remainingBudget <= 0 {
		if remainingBudget <= 0 {
			warnings = append(warnings, fmt.Sprintf("source %s content skipped: package content budget exhausted", sourceLabel))
		}
		return "", remainingBudget <= 0, warnings, nil
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, maxGUIKnowledgePackageSourceNodes)
	if err != nil {
		return "", false, warnings, err
	}
	truncated := source.NodeCount > len(nodes) && len(nodes) >= maxGUIKnowledgePackageSourceNodes
	if truncated {
		warnings = append(warnings, fmt.Sprintf("source %s content truncated: exported %d of %d nodes", sourceLabel, len(nodes), source.NodeCount))
	}
	limit := maxGUIKnowledgePackageSourceContentBytes
	if remainingBudget < limit {
		limit = remainingBudget
	}
	parts := make([]string, 0, len(nodes))
	used := 0
	for _, node := range nodes {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		if redactSensitive {
			text = knowledge.RedactSensitiveText(text)
		}
		if text == "" {
			continue
		}
		separatorBytes := 0
		if len(parts) > 0 {
			separatorBytes = 2
		}
		available := limit - used - separatorBytes
		if available <= 0 {
			truncated = true
			warnings = append(warnings, fmt.Sprintf("source %s content truncated: package content byte limit reached", sourceLabel))
			break
		}
		if len([]byte(text)) > available {
			text = truncateStringToUTF8Bytes(text, available)
			truncated = true
			warnings = append(warnings, fmt.Sprintf("source %s content truncated: package content byte limit reached", sourceLabel))
		}
		if text == "" {
			break
		}
		parts = append(parts, text)
		used += separatorBytes + len([]byte(text))
		if truncated {
			break
		}
	}
	return strings.Join(parts, "\n\n"), truncated, warnings, nil
}

func truncateStringToUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len([]byte(value)) <= maxBytes {
		return value
	}
	used := 0
	var builder strings.Builder
	builder.Grow(maxBytes)
	for _, r := range value {
		next := len(string(r))
		if used+next > maxBytes {
			break
		}
		builder.WriteRune(r)
		used += next
	}
	return strings.TrimSpace(builder.String())
}

func knowledgeSourceIDs(sources []knowledge.Source) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		if id := strings.TrimSpace(source.ID); id != "" {
			out = append(out, id)
		}
	}
	return compactKnowledgeSourceIDStrings(out)
}

func countGUIKnowledgePackageContentSources(pkg guiKnowledgePackage) int {
	count := 0
	for _, source := range pkg.Sources {
		if strings.TrimSpace(source.Content) != "" {
			count++
		}
	}
	return count
}

func mergeKnowledgeShareSummary(base, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		if shouldKeepBaseKnowledgeShareSummaryValue(key, out[key], value) {
			continue
		}
		out[key] = value
	}
	return out
}

func shouldKeepBaseKnowledgeShareSummaryValue(key string, base, overlay any) bool {
	if isEmptyKnowledgeShareSummaryValue(overlay) {
		return true
	}
	if knowledgeShareContentMetricKey(key) && intFromAny(base) > 0 && intFromAny(overlay) == 0 {
		return true
	}
	return false
}

func knowledgeShareContentMetricKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "content_sources", "content_source_count":
		return true
	default:
		return false
	}
}

func isEmptyKnowledgeShareSummaryValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

func knowledgeShareExportURI(value string, redact bool) string {
	value = strings.TrimSpace(value)
	if !redact {
		return value
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" {
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return value
		}
	}
	if strings.Contains(value, `:\`) || strings.HasPrefix(value, `/`) || strings.HasPrefix(value, `\\`) || strings.HasPrefix(strings.ToLower(value), "file:") {
		return ""
	}
	return value
}

func knowledgeShareExportPath(value string, redact bool) string {
	if redact {
		return filepath.Base(strings.TrimSpace(value))
	}
	return strings.TrimSpace(value)
}

func knowledgeShareTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func compactKnowledgeShareStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func compactKnowledgeSourceIDStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func knowledgeShareBearerToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func (a *App) resolveKnowledgeSyncHub(req KnowledgeSyncRequest) (string, string, error) {
	cfg, _ := a.LoadConfig()
	hubURL := strings.TrimRight(strings.TrimSpace(req.HubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	}
	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required")
	}
	if parsed, err := url.Parse(hubURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	token := strings.TrimSpace(req.HubToken)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	if token == "" {
		return "", "", fmt.Errorf("hub token is required")
	}
	return hubURL, token, nil
}

func encryptKnowledgeSyncPackage(raw []byte, password string) ([]byte, map[string]any, int64, error) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return nil, nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return nil, nil, 0, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, 0, err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, err
	}
	key, err := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return nil, nil, 0, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, 0, err
	}
	ciphertext := gcm.Seal(nil, nonce, compressed.Bytes(), []byte("maclaw.knowledge.sync.v1"))
	encryption := map[string]any{
		"algorithm": "AES-256-GCM",
		"kdf":       "scrypt",
		"n":         1 << 15,
		"r":         8,
		"p":         1,
		"salt":      base64.StdEncoding.EncodeToString(salt),
		"nonce":     base64.StdEncoding.EncodeToString(nonce),
	}
	return ciphertext, encryption, int64(compressed.Len()), nil
}

func decryptKnowledgeSyncPackage(encrypted []byte, password string, encryption map[string]any) ([]byte, error) {
	if len(encryption) == 0 {
		return nil, fmt.Errorf("sync package encryption metadata is missing")
	}
	salt, err := base64.StdEncoding.DecodeString(stringFromAny(encryption["salt"]))
	if err != nil {
		return nil, fmt.Errorf("invalid sync package salt")
	}
	nonce, err := base64.StdEncoding.DecodeString(stringFromAny(encryption["nonce"]))
	if err != nil {
		return nil, fmt.Errorf("invalid sync package nonce")
	}
	n := intFromAny(encryption["n"])
	r := intFromAny(encryption["r"])
	p := intFromAny(encryption["p"])
	if n <= 0 {
		n = 1 << 15
	}
	if r <= 0 {
		r = 8
	}
	if p <= 0 {
		p = 1
	}
	key, err := scrypt.Key([]byte(password), salt, n, r, p, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	compressed, err := gcm.Open(nil, nonce, encrypted, []byte("maclaw.knowledge.sync.v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt sync package: password is incorrect or package is corrupted")
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open compressed sync package: %w", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(io.LimitReader(gz, maxGUIKnowledgePackageTotalContentBytes*4))
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func stringFromAny(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func knowledgeShareStringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactKnowledgeShareStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringFromAny(item); text != "" {
				out = append(out, text)
			}
		}
		return compactKnowledgeShareStrings(out)
	}
	return nil
}

func absoluteHubShareField(hubURL, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(strings.TrimRight(hubURL, "/") + "/")
	if err != nil {
		return value
	}
	ref, err := url.Parse(value)
	if err != nil {
		return value
	}
	return base.ResolveReference(ref).String()
}

func resolveGUIKnowledgePackageURL(apiURL, packageURL string) string {
	packageURL = strings.TrimSpace(packageURL)
	if packageURL == "" {
		return ""
	}
	parsedPackage, err := url.Parse(packageURL)
	if err == nil && parsedPackage.IsAbs() {
		return parsedPackage.String()
	}
	base, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return packageURL
	}
	base.RawQuery = ""
	base.Fragment = ""
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	ref, err := url.Parse(packageURL)
	if err != nil {
		return packageURL
	}
	return base.ResolveReference(ref).String()
}

func resolveGUIKnowledgeShareAPIURL(shareLink, knowledgeID, hubURL string) (string, string, error) {
	shareLink = strings.TrimSpace(shareLink)
	knowledgeID = strings.TrimSpace(knowledgeID)
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if shareLink != "" {
		parsed, err := url.Parse(shareLink)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", "", fmt.Errorf("share_link must be an absolute URL")
		}
		if knowledgeID == "" {
			knowledgeID = knowledgeIDFromGUIKnowledgeShareURL(parsed)
		}
		if knowledgeID == "" {
			return "", "", fmt.Errorf("knowledge_id could not be determined from share_link")
		}
		return knowledgeShareAPIURLFromBase(parsed, knowledgeID), knowledgeID, nil
	}
	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required when share_link is not provided")
	}
	if knowledgeID == "" {
		return "", "", fmt.Errorf("knowledge_id is required")
	}
	parsed, err := url.Parse(hubURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	return knowledgeShareAPIURLFromBase(parsed, knowledgeID), knowledgeID, nil
}

func knowledgeIDFromGUIKnowledgeShareURL(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			decoded = part
		}
		if decoded == "shares" && i+1 < len(parts) {
			next, err := url.PathUnescape(parts[i+1])
			if err != nil {
				next = parts[i+1]
			}
			return strings.TrimSpace(next)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if last == "package" && len(parts) >= 2 {
		last = parts[len(parts)-2]
	}
	decoded, err := url.PathUnescape(last)
	if err != nil {
		decoded = last
	}
	return strings.TrimSpace(decoded)
}

func knowledgeShareAPIURLFromBase(parsed *url.URL, knowledgeID string) string {
	base := *parsed
	base.RawQuery = ""
	base.Fragment = ""
	base.Path = "/api/knowledge/shares/" + strings.TrimSpace(knowledgeID)
	return base.String() + "?intent=import"
}

func fetchGUIKnowledgeShareJSON(ctx context.Context, apiURL, authorization string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge share: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("knowledge share returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode knowledge share metadata: %w", err)
	}
	return out, nil
}

func fetchGUIKnowledgePackage(ctx context.Context, packageURL, authorization string) (guiKnowledgePackage, error) {
	var pkg guiKnowledgePackage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return pkg, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return pkg, fmt.Errorf("fetch knowledge package: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return pkg, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pkg, fmt.Errorf("knowledge package returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return pkg, fmt.Errorf("decode knowledge package: %w", err)
	}
	return pkg, nil
}

func firstNonEmptyKnowledgeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *App) KnowledgeListSourceLabels(opts knowledge.ListSourcesOptions) ([]knowledge.SourceLabelSummary, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.ListSourceLabels(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeGetSource(id string) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.GetSource(a.knowledgeContext(), id)
}

func (a *App) KnowledgeUpdateSourceMetadata(req knowledge.SourceUpdateRequest) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.UpdateSourceMetadata(a.knowledgeContext(), req)
}

func (a *App) KnowledgeUpdateSourceLabels(req knowledge.SourceLabelUpdateRequest) (knowledge.SourceLabelUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceLabelUpdateResult{}, err
	}
	defer store.Close()
	req.Filter = a.normalizeKnowledgeListOptions(req.Filter)
	return store.UpdateSourceLabels(a.knowledgeContext(), req)
}

func (a *App) KnowledgeBackfillSourceAutoLabels(req knowledge.SourceAutoLabelBackfillRequest) (knowledge.SourceLabelUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceLabelUpdateResult{}, err
	}
	defer store.Close()
	req.Filter = a.normalizeKnowledgeListOptions(req.Filter)
	return store.BackfillSourceAutoLabels(a.knowledgeContext(), req)
}

func (a *App) KnowledgeSearch(opts knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.Search(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeSearchStructured(opts knowledge.StructuredSearchOptions) ([]knowledge.SearchResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeStructuredSearchOptions(opts)
	return store.SearchStructured(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeStructuredCatalog(opts knowledge.StructuredCatalogOptions) (knowledge.StructuredCatalogResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.StructuredCatalogResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeStructuredCatalogOptions(opts)
	return store.StructuredCatalog(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeExplain(opts knowledge.SearchOptions) (knowledge.ExplainResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ExplainResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.Explain(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeSearchFacets(opts knowledge.SearchOptions) (knowledge.SearchFacetsResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SearchFacetsResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.SearchFacets(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeTopicRelevance(opts knowledge.SearchOptions) (knowledge.TopicRelevanceReport, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.TopicRelevanceReport{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.TopicRelevance(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeContextPack(opts knowledge.ContextPackOptions) (knowledge.ContextPackResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ContextPackResult{}, err
	}
	defer store.Close()
	opts.SearchOptions = a.normalizeKnowledgeSearchOptions(opts.SearchOptions)
	return store.ContextPack(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeFactGraph(opts knowledge.SearchOptions) (knowledge.FactGraphResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.FactGraphResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.FactGraph(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeFactIndex(opts knowledge.FactIndexOptions) (knowledge.FactIndexResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.FactIndexResult{}, err
	}
	defer store.Close()
	opts.SearchOptions = a.normalizeKnowledgeSearchOptions(opts.SearchOptions)
	return store.FactIndex(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeEntityProfile(opts knowledge.SearchOptions) (knowledge.EntityProfileResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.EntityProfileResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeSearchOptions(opts)
	return store.EntityProfile(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeSuggest(opts knowledge.KnowledgeSuggestOptions) (knowledge.KnowledgeSuggestResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.KnowledgeSuggestResult{}, err
	}
	defer store.Close()
	opts.SearchOptions = a.normalizeKnowledgeSearchOptions(opts.SearchOptions)
	return store.Suggest(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeDeleteSource(id string) error {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return err
	}
	defer store.Close()
	return store.DeleteSource(a.knowledgeContext(), id)
}

func (a *App) KnowledgeDisableSource(id string) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.DisableSource(a.knowledgeContext(), id)
}

func (a *App) KnowledgeDisableSources(ids []string) (knowledge.SourceStatusUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceStatusUpdateResult{}, err
	}
	defer store.Close()
	return store.DisableSources(a.knowledgeContext(), ids), nil
}

func (a *App) KnowledgeDisableSourcesByFilter(opts knowledge.ListSourcesOptions) (knowledge.SourceStatusUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceStatusUpdateResult{}, err
	}
	defer store.Close()
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	return store.DisableSourcesByFilter(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeEnableSource(id string) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.EnableSource(a.knowledgeContext(), id)
}

func (a *App) KnowledgeEnableSources(ids []string) (knowledge.SourceStatusUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceStatusUpdateResult{}, err
	}
	defer store.Close()
	return store.EnableSources(a.knowledgeContext(), ids), nil
}

func (a *App) KnowledgeEnableSourcesByFilter(opts knowledge.ListSourcesOptions) (knowledge.SourceStatusUpdateResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceStatusUpdateResult{}, err
	}
	defer store.Close()
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	return store.EnableSourcesByFilter(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeRefreshSource(id string) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.RefreshSource(a.knowledgeContext(), id)
}

func (a *App) KnowledgePreviewSourceRefresh(id string) (knowledge.SourceChangePreview, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceChangePreview{}, err
	}
	defer store.Close()
	return store.PreviewSourceRefresh(a.knowledgeContext(), id)
}

func (a *App) KnowledgePreviewSourcesRefresh(ids []string) (knowledge.SourceChangePreviewResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceChangePreviewResult{}, err
	}
	defer store.Close()
	return store.PreviewSourcesRefresh(a.knowledgeContext(), ids), nil
}

func (a *App) KnowledgePreviewSourcesRefreshByFilter(opts knowledge.ListSourcesOptions) (knowledge.SourceChangePreviewResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceChangePreviewResult{}, err
	}
	defer store.Close()
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	return store.PreviewSourcesRefreshByFilter(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeRefreshChangedSources(ids []string) (knowledge.ChangedSourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ChangedSourceRefreshResult{}, err
	}
	defer store.Close()
	return store.RefreshChangedSources(a.knowledgeContext(), ids), nil
}

func (a *App) KnowledgeRefreshChangedSourcesByFilter(opts knowledge.ListSourcesOptions) (knowledge.ChangedSourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ChangedSourceRefreshResult{}, err
	}
	defer store.Close()
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	return store.RefreshChangedSourcesByFilter(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeRefreshSources(ids []string) (knowledge.SourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceRefreshResult{}, err
	}
	defer store.Close()
	return store.RefreshSources(a.knowledgeContext(), ids), nil
}

func (a *App) KnowledgeRefreshSourcesByFilter(opts knowledge.ListSourcesOptions) (knowledge.SourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceRefreshResult{}, err
	}
	defer store.Close()
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	return store.RefreshSourcesByFilter(a.knowledgeContext(), opts)
}

func (a *App) KnowledgeRebuildSourceDerived(sourceID string, distillMode string) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	return store.RebuildSourceDerived(a.knowledgeContext(), sourceID, distillMode)
}

func (a *App) KnowledgeRebuildSourcesDerived(ids []string, distillMode string) (knowledge.SourceRebuildResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceRebuildResult{}, err
	}
	defer store.Close()
	return store.RebuildSourcesDerived(a.knowledgeContext(), ids, distillMode), nil
}

func (a *App) KnowledgeRebuildSourcesDerivedByFilter(opts knowledge.ListSourcesOptions, distillMode string) (knowledge.SourceRebuildResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceRebuildResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.RebuildSourcesDerivedByFilter(a.knowledgeContext(), opts, distillMode)
}

func (a *App) KnowledgeScanDirectory(req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	req = a.normalizeKnowledgeImportRequest(req)
	req.DryRun = true
	return store.ScanDirectory(a.knowledgeContext(), req)
}

func (a *App) KnowledgeScanFiles(req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error) {
	req = a.normalizeKnowledgeImportRequest(req)
	req.RootPath = ""
	req.DryRun = true
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	return store.ScanFiles(a.knowledgeContext(), req, filePaths)
}

func (a *App) KnowledgeImportFiles(req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error) {
	log.Printf("[knowledge] ImportFiles: %d files, include_exts=%v", len(filePaths), req.IncludeExts)
	req = a.normalizeKnowledgeImportRequest(req)
	req.RootPath = ""
	store, err := a.openKnowledgeStore()
	if err != nil {
		log.Printf("[knowledge] ImportFiles: openKnowledgeStore failed: %v", err)
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	result, err := store.ImportFiles(a.knowledgeContext(), req, filePaths)
	if err != nil {
		log.Printf("[knowledge] ImportFiles: failed: %v", err)
	} else {
		log.Printf("[knowledge] ImportFiles: done total=%d imported=%d skipped=%d failed=%d", result.TotalFiles, result.ImportedFiles, result.SkippedFiles, result.FailedFiles)
	}
	return result, err
}

func (a *App) KnowledgeListImportBatches(limit int) ([]knowledge.ImportBatch, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListImportBatches(a.knowledgeContext(), limit)
}

func (a *App) KnowledgeListImportItems(batchID string, limit int) ([]knowledge.ImportItem, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListImportItems(a.knowledgeContext(), batchID, limit)
}

func (a *App) KnowledgeRetryImportBatch(req knowledge.ImportRetryRequest) (knowledge.DirectoryImportResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	return store.RetryImportBatch(a.knowledgeContext(), req)
}

func (a *App) KnowledgeListNodesBySource(sourceID string, limit int) ([]knowledge.DocumentNode, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListNodesBySource(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeListSourceVersions(sourceID string, limit int) ([]knowledge.SourceVersion, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSourceVersions(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeListCardsBySource(sourceID string, limit int) ([]knowledge.Card, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListCardsBySource(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeListFactsBySource(sourceID string, limit int) ([]knowledge.Fact, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListFactsBySource(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeListSourceLinks(sourceID string, limit int) ([]knowledge.SourceLink, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSourceLinks(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeSourceGraph(opts knowledge.ListSourcesOptions, edgeLimit int) (knowledge.SourceGraphResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceGraphResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.SourceGraph(a.knowledgeContext(), opts, edgeLimit)
}

func (a *App) KnowledgeSourceNeighborhood(sourceID string, depth int, limit int, edgeLimit int) (knowledge.SourceGraphResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceGraphResult{}, err
	}
	defer store.Close()
	return store.SourceNeighborhood(a.knowledgeContext(), sourceID, depth, limit, edgeLimit)
}

func (a *App) KnowledgeSourcePath(fromSourceID string, toSourceID string, maxDepth int, edgeLimit int) (knowledge.SourcePathResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourcePathResult{}, err
	}
	defer store.Close()
	return store.SourcePath(a.knowledgeContext(), fromSourceID, toSourceID, maxDepth, edgeLimit)
}

func (a *App) KnowledgePreviewSourceTopicLinks(sourceID string, limit int) (knowledge.SourceTopicLinkBuildResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceTopicLinkBuildResult{}, err
	}
	defer store.Close()
	return store.PreviewSourceTopicLinks(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeLinkSources(link knowledge.SourceLink) (knowledge.SourceLink, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceLink{}, err
	}
	defer store.Close()
	return store.LinkSources(a.knowledgeContext(), link)
}

func (a *App) KnowledgeUnlinkSources(sourceID string, relatedSourceID string, relation string) (knowledge.SourceUnlinkResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceUnlinkResult{}, err
	}
	defer store.Close()
	return store.UnlinkSources(a.knowledgeContext(), sourceID, relatedSourceID, relation)
}

func (a *App) KnowledgeListSourceLinkEvents(sourceID string, limit int) ([]knowledge.SourceLinkEvent, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSourceLinkEvents(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeSourceTimeline(sourceID string, limit int) (knowledge.SourceTimelineResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceTimelineResult{}, err
	}
	defer store.Close()
	return store.SourceTimeline(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeSourceDigest(sourceID string, nodeLimit int, cardLimit int, factLimit int, linkLimit int) (knowledge.SourceDigestResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceDigestResult{}, err
	}
	defer store.Close()
	return store.SourceDigest(a.knowledgeContext(), sourceID, nodeLimit, cardLimit, factLimit, linkLimit)
}

func (a *App) KnowledgeRefreshSourceTopicLinks(sourceID string, limit int) (knowledge.SourceTopicLinkBuildResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceTopicLinkBuildResult{}, err
	}
	defer store.Close()
	return store.RefreshSourceTopicLinks(a.knowledgeContext(), sourceID, limit)
}

func (a *App) KnowledgeRefreshSourceTopicLinksByFilter(opts knowledge.ListSourcesOptions, limitPerSource int) (knowledge.SourceTopicLinkBuildResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceTopicLinkBuildResult{}, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.RefreshSourceTopicLinksByFilter(a.knowledgeContext(), opts, limitPerSource)
}

func (a *App) KnowledgeListDuplicateCards(limit int) ([]knowledge.DuplicateCardGroup, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListDuplicateCards(a.knowledgeContext(), limit)
}

func (a *App) KnowledgeSuppressDuplicateCards(req knowledge.DuplicateCardSuppressionRequest) (knowledge.CardSuppressionResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.CardSuppressionResult{}, err
	}
	defer store.Close()
	return store.SuppressDuplicateCards(a.knowledgeContext(), req)
}

func (a *App) KnowledgeSuppressCards(cardIDs []string, reason string) (knowledge.CardSuppressionResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.CardSuppressionResult{}, err
	}
	defer store.Close()
	return store.SuppressCards(a.knowledgeContext(), cardIDs, reason)
}

func (a *App) KnowledgeRestoreSuppressedCards(cardIDs []string) (knowledge.CardSuppressionResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.CardSuppressionResult{}, err
	}
	defer store.Close()
	return store.RestoreSuppressedCards(a.knowledgeContext(), cardIDs)
}

func (a *App) KnowledgeListSuppressedCards(limit int) ([]knowledge.CardSuppression, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSuppressedCards(a.knowledgeContext(), limit)
}

func (a *App) KnowledgeScanSensitiveContent(limit int) (knowledge.SensitiveScanResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SensitiveScanResult{}, err
	}
	defer store.Close()
	return store.ScanSensitiveContent(a.knowledgeContext(), limit)
}

func (a *App) KnowledgeDisableSensitiveSources(limit int) (knowledge.SensitiveIsolationResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SensitiveIsolationResult{}, err
	}
	defer store.Close()
	return store.DisableSensitiveSources(a.knowledgeContext(), limit)
}

func (a *App) KnowledgeStartImportDirectory(req knowledge.DirectoryImportRequest) (KnowledgeImportJob, error) {
	req = a.normalizeKnowledgeImportRequest(req)
	req.DryRun = false
	now := time.Now().UTC()
	job := KnowledgeImportJob{
		ID:        knowledge.NewID("kjob"),
		Status:    knowledge.ImportStatusRunning,
		Result:    knowledge.DirectoryImportResult{Status: knowledge.ImportStatusRunning, RootPath: req.RootPath},
		CreatedAt: now,
		UpdatedAt: now,
	}
	knowledgeImportJobs.Store(job.ID, job)

	go func(jobID string, req knowledge.DirectoryImportRequest) {
		store, err := a.openKnowledgeStore()
		if err != nil {
			finishKnowledgeImportJob(a, jobID, knowledge.DirectoryImportResult{Status: knowledge.ImportStatusFailed, RootPath: req.RootPath}, err)
			return
		}
		defer store.Close()
		store.SetImportProgressCallback(func(progress knowledge.DirectoryImportResult) {
			updateKnowledgeImportJobProgress(a, jobID, progress)
		})
		result, err := store.ImportDirectory(a.knowledgeContext(), req)
		finishKnowledgeImportJob(a, jobID, result, err)
	}(job.ID, req)

	return job, nil
}

func (a *App) KnowledgeStartImportFiles(req knowledge.DirectoryImportRequest, filePaths []string) (KnowledgeImportJob, error) {
	req = a.normalizeKnowledgeImportRequest(req)
	req.RootPath = ""
	req.DryRun = false
	now := time.Now().UTC()
	job := KnowledgeImportJob{
		ID:        knowledge.NewID("kjob"),
		Status:    knowledge.ImportStatusRunning,
		Result:    knowledge.DirectoryImportResult{Status: knowledge.ImportStatusRunning, TotalFiles: len(filePaths)},
		CreatedAt: now,
		UpdatedAt: now,
	}
	knowledgeImportJobs.Store(job.ID, job)

	go func(jobID string, req knowledge.DirectoryImportRequest, filePaths []string) {
		store, err := a.openKnowledgeStore()
		if err != nil {
			finishKnowledgeImportJob(a, jobID, knowledge.DirectoryImportResult{Status: knowledge.ImportStatusFailed}, err)
			return
		}
		defer store.Close()
		store.SetImportProgressCallback(func(progress knowledge.DirectoryImportResult) {
			updateKnowledgeImportJobProgress(a, jobID, progress)
		})
		result, err := store.ImportFiles(a.knowledgeContext(), req, filePaths)
		finishKnowledgeImportJob(a, jobID, result, err)
	}(job.ID, req, filePaths)

	return job, nil
}

func (a *App) KnowledgeImportJobStatus(id string) (KnowledgeImportJob, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return KnowledgeImportJob{}, fmt.Errorf("knowledge import job id is required")
	}
	value, ok := knowledgeImportJobs.Load(id)
	if !ok {
		return KnowledgeImportJob{}, fmt.Errorf("knowledge import job %s not found", id)
	}
	job, ok := value.(KnowledgeImportJob)
	if !ok {
		return KnowledgeImportJob{}, fmt.Errorf("knowledge import job %s has invalid state", id)
	}
	return job, nil
}

func updateKnowledgeImportJobProgress(a *App, id string, result knowledge.DirectoryImportResult) {
	value, ok := knowledgeImportJobs.Load(id)
	if !ok {
		return
	}
	job, ok := value.(KnowledgeImportJob)
	if !ok {
		return
	}
	prevProcessed := job.Result.ProcessedFiles
	prevFailed := job.Result.FailedFiles
	prevSkipped := job.Result.SkippedFiles
	if result.Status == "" {
		result.Status = knowledge.ImportStatusRunning
	}
	job.Result = result
	job.Status = result.Status
	job.UpdatedAt = time.Now().UTC()
	knowledgeImportJobs.Store(id, job)

	// Emit Wails event for real-time frontend updates
	if a != nil && a.ctx != nil {
		eventData := map[string]interface{}{
			"job_id":           id,
			"status":           result.Status,
			"total_files":      result.TotalFiles,
			"processed_files":  result.ProcessedFiles,
			"imported_files":   result.ImportedFiles,
			"skipped_files":    result.SkippedFiles,
			"failed_files":     result.FailedFiles,
			"current_file":     result.CurrentFile,
			"current_step":     result.CurrentStep,
			"step_progress":    result.StepProgress,
			"total_steps":      result.TotalSteps,
			"current_step_num": result.CurrentStepNum,
		}
		// When a file finishes processing (ProcessedFiles increments), emit item info
		if result.ProcessedFiles > prevProcessed && result.CurrentFile != "" {
			itemStatus := "imported"
			if result.FailedFiles > prevFailed {
				itemStatus = "failed"
			} else if result.SkippedFiles > prevSkipped {
				itemStatus = "skipped"
			}
			eventData["last_item_path"] = result.CurrentFile
			eventData["last_item_status"] = itemStatus
		}
		runtime.EventsEmit(a.ctx, "knowledge:import-progress", eventData)
	}
}

func finishKnowledgeImportJob(a *App, id string, result knowledge.DirectoryImportResult, err error) {
	value, ok := knowledgeImportJobs.Load(id)
	if !ok {
		return
	}
	job, ok := value.(KnowledgeImportJob)
	if !ok {
		return
	}
	if result.Status == "" {
		result.Status = knowledge.ImportStatusCompleted
	}
	job.Result = result
	job.Status = result.Status
	job.UpdatedAt = time.Now().UTC()
	if err != nil {
		job.Status = knowledge.ImportStatusFailed
		job.Error = err.Error()
		job.Result.Status = knowledge.ImportStatusFailed
	}
	knowledgeImportJobs.Store(id, job)

	// Emit final status event
	if a != nil && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "knowledge:import-progress", map[string]interface{}{
			"job_id":          id,
			"status":          job.Status,
			"total_files":     result.TotalFiles,
			"processed_files": result.ProcessedFiles,
			"imported_files":  result.ImportedFiles,
			"skipped_files":   result.SkippedFiles,
			"failed_files":    result.FailedFiles,
		})
	}
}

func (a *App) KnowledgeImportDirectory(req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	req = a.normalizeKnowledgeImportRequest(req)
	req.DryRun = false
	return store.ImportDirectory(a.knowledgeContext(), req)
}

func (a *App) KnowledgeSaveURL(rawURL string, saveScope string, topicHint string, distillMode string, labels []string, autoLabels bool) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	projectPath := ""
	if strings.TrimSpace(saveScope) == "" || saveScope == knowledge.SaveScopeProject {
		projectPath = a.GetCurrentProjectPath()
	}
	return store.SaveURL(a.knowledgeContext(), knowledge.URLSaveRequest{
		URL:         rawURL,
		SaveScope:   saveScope,
		ProjectPath: projectPath,
		TopicHint:   topicHint,
		DistillMode: distillMode,
		Labels:      labels,
		AutoLabels:  autoLabels,
	})
}

func (a *App) KnowledgeSaveURLs(rawURLs []string, saveScope string, topicHint string, distillMode string, labels []string, autoLabels bool) (knowledge.URLBatchSaveResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.URLBatchSaveResult{}, err
	}
	defer store.Close()
	projectPath := ""
	if saveScope == "" || saveScope == knowledge.SaveScopeProject {
		projectPath = a.GetCurrentProjectPath()
	}
	return store.SaveURLs(a.knowledgeContext(), knowledge.URLBatchSaveRequest{
		URLs:        rawURLs,
		SaveScope:   saveScope,
		ProjectPath: projectPath,
		TopicHint:   topicHint,
		DistillMode: distillMode,
		Labels:      labels,
		AutoLabels:  autoLabels,
	}), nil
}

func (a *App) KnowledgeDiscoverURLs(req knowledge.URLDiscoveryRequest) (knowledge.URLDiscoveryResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.URLDiscoveryResult{}, err
	}
	defer store.Close()
	return store.DiscoverURLs(a.knowledgeContext(), req)
}

func (a *App) KnowledgeSaveText(req knowledge.TextSaveRequest) (knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.Source{}, err
	}
	defer store.Close()
	req.SaveScope = strings.TrimSpace(req.SaveScope)
	if req.SaveScope == "" {
		req.SaveScope = knowledge.SaveScopeProject
	}
	if req.SaveScope == knowledge.SaveScopeProject && strings.TrimSpace(req.ProjectPath) == "" {
		req.ProjectPath = a.GetCurrentProjectPath()
	}
	if req.SaveScope == knowledge.SaveScopePersonal || req.SaveScope == knowledge.SaveScopeLocalOnly {
		req.ProjectPath = ""
	}
	return store.SaveText(a.knowledgeContext(), req)
}

func (a *App) normalizeKnowledgeImportRequest(req knowledge.DirectoryImportRequest) knowledge.DirectoryImportRequest {
	req = knowledge.NormalizeDirectoryImportRequest(req)
	if req.SaveScope == knowledge.SaveScopePersonal || req.SaveScope == knowledge.SaveScopeLocalOnly {
		req.ProjectPath = ""
		return req
	}
	if strings.TrimSpace(req.ProjectPath) == "" {
		req.ProjectPath = a.GetCurrentProjectPath()
	}
	return req
}

func (a *App) normalizeKnowledgeSearchOptions(opts knowledge.SearchOptions) knowledge.SearchOptions {
	opts.OwnerID = strings.TrimSpace(opts.OwnerID)
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.ProjectPath = strings.TrimSpace(opts.ProjectPath)
	opts.Query = strings.TrimSpace(opts.Query)
	opts.SearchScope = strings.TrimSpace(opts.SearchScope)
	opts.TopicHint = strings.TrimSpace(opts.TopicHint)
	opts.Domain = strings.TrimSpace(opts.Domain)
	opts.Entity = strings.TrimSpace(opts.Entity)
	opts.Predicate = strings.TrimSpace(opts.Predicate)
	opts.ContextTerms = normalizeKnowledgeOptionStrings(opts.ContextTerms)
	opts.ResultTypes = normalizeKnowledgeOptionStrings(opts.ResultTypes)
	opts.SourceKinds = normalizeKnowledgeOptionStrings(opts.SourceKinds)
	opts.SourceIDs = normalizeKnowledgeIDStrings(opts.SourceIDs)
	opts.Labels = normalizeKnowledgeOptionStrings(opts.Labels)
	switch scope := normalizeKnowledgeSearchScopeKind(opts.SearchScope); {
	case scope == knowledgeSearchScopeProject:
		if strings.TrimSpace(opts.ProjectPath) == "" {
			opts.ProjectPath = a.GetCurrentProjectPath()
		}
	case scope.ClearsProjectPath():
		opts.ProjectPath = ""
	default:
		opts.ProjectPath = a.normalizeKnowledgeScopePath(opts.ProjectPath)
	}
	return opts
}

func (a *App) normalizeKnowledgeStructuredSearchOptions(opts knowledge.StructuredSearchOptions) knowledge.StructuredSearchOptions {
	opts.OwnerID = strings.TrimSpace(opts.OwnerID)
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.ProjectPath = strings.TrimSpace(opts.ProjectPath)
	opts.Query = strings.TrimSpace(opts.Query)
	opts.SearchScope = strings.TrimSpace(opts.SearchScope)
	opts.SourceID = strings.TrimSpace(opts.SourceID)
	opts.SourceIDs = normalizeKnowledgeIDStrings(opts.SourceIDs)
	opts.SheetNames = normalizeKnowledgeStructuredOptionStrings(opts.SheetNames)
	opts.ColumnEquals = normalizeKnowledgeStructuredStringMap(opts.ColumnEquals)
	opts.ColumnContains = normalizeKnowledgeStructuredStringMap(opts.ColumnContains)
	opts.NumberRanges = normalizeKnowledgeNumberRanges(opts.NumberRanges)
	opts.DateRanges = normalizeKnowledgeDateRanges(opts.DateRanges)
	switch scope := normalizeKnowledgeSearchScopeKind(opts.SearchScope); {
	case scope == knowledgeSearchScopeProject:
		if strings.TrimSpace(opts.ProjectPath) == "" {
			opts.ProjectPath = a.GetCurrentProjectPath()
		}
	case scope.ClearsProjectPath():
		opts.ProjectPath = ""
	default:
		opts.ProjectPath = a.normalizeKnowledgeScopePath(opts.ProjectPath)
	}
	return opts
}

func (a *App) normalizeKnowledgeStructuredCatalogOptions(opts knowledge.StructuredCatalogOptions) knowledge.StructuredCatalogOptions {
	opts.OwnerID = strings.TrimSpace(opts.OwnerID)
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.ProjectPath = strings.TrimSpace(opts.ProjectPath)
	opts.SearchScope = strings.TrimSpace(opts.SearchScope)
	opts.SourceID = strings.TrimSpace(opts.SourceID)
	opts.SourceIDs = normalizeKnowledgeIDStrings(opts.SourceIDs)
	opts.SheetNames = normalizeKnowledgeStructuredOptionStrings(opts.SheetNames)
	switch scope := normalizeKnowledgeSearchScopeKind(opts.SearchScope); {
	case scope == knowledgeSearchScopeProject:
		if strings.TrimSpace(opts.ProjectPath) == "" {
			opts.ProjectPath = a.GetCurrentProjectPath()
		}
	case scope.ClearsProjectPath():
		opts.ProjectPath = ""
	default:
		opts.ProjectPath = a.normalizeKnowledgeScopePath(opts.ProjectPath)
	}
	return opts
}

func normalizeKnowledgeOptionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeKnowledgeIDStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
func normalizeKnowledgeStructuredOptionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeKnowledgeStructuredStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeKnowledgeNumberRanges(values map[string]knowledge.NumberRange) map[string]knowledge.NumberRange {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]knowledge.NumberRange, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || (value.Min == nil && value.Max == nil) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeKnowledgeDateRanges(values map[string]knowledge.DateRange) map[string]knowledge.DateRange {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]knowledge.DateRange, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value.Start = strings.TrimSpace(value.Start)
		value.End = strings.TrimSpace(value.End)
		if key == "" || (value.Start == "" && value.End == "") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *App) normalizeKnowledgeListOptions(opts knowledge.ListSourcesOptions) knowledge.ListSourcesOptions {
	opts.OwnerID = strings.TrimSpace(opts.OwnerID)
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.SearchScope = strings.TrimSpace(opts.SearchScope)
	opts.ProjectPath = strings.TrimSpace(opts.ProjectPath)
	opts.Status = strings.ToLower(strings.TrimSpace(opts.Status))
	opts.Kind = strings.ToLower(strings.TrimSpace(opts.Kind))
	opts.Domain = strings.TrimSpace(opts.Domain)
	opts.Label = strings.TrimSpace(opts.Label)
	opts.Query = strings.TrimSpace(opts.Query)
	opts.CoverageFilter = strings.TrimSpace(opts.CoverageFilter)
	opts.QualityGrade = strings.ToLower(strings.TrimSpace(opts.QualityGrade))
	opts.SourceIDs = normalizeKnowledgeIDStrings(opts.SourceIDs)
	opts.SourceKinds = normalizeKnowledgeOptionStrings(opts.SourceKinds)
	opts.Labels = normalizeKnowledgeOptionStrings(opts.Labels)
	opts.QualityGrades = normalizeKnowledgeOptionStrings(opts.QualityGrades)
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	switch scope := normalizeKnowledgeSearchScopeKind(opts.SearchScope); {
	case scope == knowledgeSearchScopeProject:
		if opts.ProjectPath == "" {
			opts.ProjectPath = a.GetCurrentProjectPath()
		}
	case scope.ClearsProjectPath():
		opts.ProjectPath = ""
	default:
		opts.ProjectPath = a.normalizeKnowledgeScopePath(opts.ProjectPath)
	}
	return opts
}

func (a *App) normalizeKnowledgeScopePath(projectPath string) string {
	if strings.TrimSpace(projectPath) != "" {
		return projectPath
	}
	return ""
}

// ---------------------------------------------------------------------------
// Deep Crawl Wails Bindings
func (a *App) beginKnowledgeDeepCrawl(mode string) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(a.knowledgeContext())
	a.deepCrawlMu.Lock()
	defer a.deepCrawlMu.Unlock()
	if a.deepCrawlCancel != nil {
		cancel()
		return nil, nil, fmt.Errorf("deep crawl already running in %s mode", firstNonEmptyKnowledgeString(a.deepCrawlMode, "unknown"))
	}
	a.deepCrawlCancel = cancel
	a.deepCrawlCtx = ctx
	a.deepCrawlMode = mode
	return ctx, cancel, nil
}

func (a *App) finishKnowledgeDeepCrawl(ctx context.Context, cancel context.CancelFunc) {
	a.deepCrawlMu.Lock()
	if a.deepCrawlCtx == ctx {
		a.deepCrawlCancel = nil
		a.deepCrawlCtx = nil
		a.deepCrawlMode = ""
	}
	a.deepCrawlMu.Unlock()
	cancel()
}

func firstNonEmptyKnowledgeString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ---------------------------------------------------------------------------

// KnowledgeDeepCrawl starts a full deep crawl.
func (a *App) KnowledgeDeepCrawl(req knowledge.DeepCrawlRequest) (knowledge.DeepCrawlResult, error) {
	ctx, cancel, err := a.beginKnowledgeDeepCrawl("crawl")
	if err != nil {
		return knowledge.DeepCrawlResult{}, err
	}
	defer a.finishKnowledgeDeepCrawl(ctx, cancel)

	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DeepCrawlResult{}, err
	}
	defer store.Close()

	// Create onProgress callback that emits Wails events
	onProgress := func(progress knowledge.DeepCrawlProgress) {
		if progress.Mode == "" {
			progress.Mode = "crawl"
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "knowledge:deep-crawl-progress", progress)
		}
	}

	engine := knowledge.NewDeepCrawlEngine(store, onProgress)
	result, err := engine.StartCrawl(ctx, req)

	return result, err
}

// KnowledgeDeepCrawl starts a full deep crawl.
func (a *App) KnowledgeDeepCrawlPreview(req knowledge.DeepCrawlRequest) (knowledge.DeepCrawlResult, error) {
	ctx, cancel, err := a.beginKnowledgeDeepCrawl("preview")
	if err != nil {
		return knowledge.DeepCrawlResult{}, err
	}
	defer a.finishKnowledgeDeepCrawl(ctx, cancel)

	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DeepCrawlResult{}, err
	}
	defer store.Close()

	// Create onProgress callback that emits Wails events
	onProgress := func(progress knowledge.DeepCrawlProgress) {
		if progress.Mode == "" {
			progress.Mode = "preview"
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "knowledge:deep-crawl-progress", progress)
		}
	}

	engine := knowledge.NewDeepCrawlEngine(store, onProgress)
	result, err := engine.Preview(ctx, req)

	return result, err
}

// KnowledgeDeepCrawl starts a full deep crawl.
func (a *App) KnowledgeDeepCrawlCancel() error {
	a.deepCrawlMu.Lock()
	cancel := a.deepCrawlCancel
	if cancel != nil {
		a.deepCrawlCancel = nil
		a.deepCrawlCtx = nil
		a.deepCrawlMode = ""
	}
	a.deepCrawlMu.Unlock()

	if cancel == nil {
		return fmt.Errorf("no active deep crawl to cancel")
	}

	cancel()
	return nil
}

// KnowledgeGetImageAssetPaths returns the thumbnail (as base64 data URL for WebView display),
// preview path, and original image path for a knowledge source.
// Returns: {"thumb_data_url": "data:image/jpeg;base64,...", "preview": "path", "original": "path"}
// Empty map if the source has no image assets.
func (a *App) KnowledgeGetImageAssetPaths(sourceID string) map[string]string {
	result := map[string]string{}
	if sourceID == "" {
		return result
	}
	// Security: reject path traversal attempts in sourceID.
	if strings.ContainsAny(sourceID, `/\`) || strings.Contains(sourceID, "..") {
		return result
	}
	dataDir := a.GetDataDir()
	baseDir := filepath.Join(dataDir, "knowledge_assets")
	assetDir := filepath.Join(baseDir, sourceID)

	// Double-check the resolved path is still within baseDir.
	if !strings.HasPrefix(filepath.Clean(assetDir)+string(filepath.Separator), filepath.Clean(baseDir)+string(filepath.Separator)) {
		return result
	}

	// Quick check: does the asset directory exist?
	info, err := os.Stat(assetDir)
	if err != nil || !info.IsDir() {
		return result
	}

	// Thumbnail: read file and return as base64 data URL (for WebView display).
	thumbPath := filepath.Join(assetDir, "thumb_120.jpg")
	if thumbData, err := os.ReadFile(thumbPath); err == nil && len(thumbData) > 0 {
		result["thumb_data_url"] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbData)
	}
	// Preview and original: return file paths (for opening with system viewer).
	previewPath := filepath.Join(assetDir, "preview_480.jpg")
	if _, err := os.Stat(previewPath); err == nil {
		result["preview"] = previewPath
	}
	// Find original (extension varies).
	entries, err := os.ReadDir(assetDir)
	if err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "original") {
				result["original"] = filepath.Join(assetDir, entry.Name())
				break
			}
		}
	}
	return result
}

// KnowledgeOpenImageFile opens an image file with the system default viewer.
// Security: only allows opening files within the knowledge_assets directory.
func (a *App) KnowledgeOpenImageFile(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	// Security: validate the path is within knowledge_assets.
	dataDir := a.GetDataDir()
	allowedBase := filepath.Clean(filepath.Join(dataDir, "knowledge_assets"))
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath+string(filepath.Separator), allowedBase+string(filepath.Separator)) &&
		!strings.HasPrefix(cleanPath, allowedBase+string(filepath.Separator)) {
		return fmt.Errorf("access denied: path must be within knowledge assets directory")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("file not found: %s", path)
	}
	return a.OpenFileOrShowInFolder(path)
}
