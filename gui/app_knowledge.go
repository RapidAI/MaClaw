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
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/scrypt"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
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

// KnowledgeHubShareListRequest loads the current user's Hub knowledge shares.
type KnowledgeHubShareListRequest struct {
	HubURL   string `json:"hub_url"`
	HubToken string `json:"hub_token"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

// KnowledgeHubShareListItem is a compact row for the export-tab "My shares" panel.
type KnowledgeHubShareListItem struct {
	KnowledgeID     string `json:"knowledge_id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	VisibilityScope string `json:"visibility_scope,omitempty"`
	Status          string `json:"status,omitempty"`
	ShareURL        string `json:"share_url,omitempty"`
	AgentImport     string `json:"agent_import,omitempty"`
	SourceCount     int    `json:"source_count,omitempty"`
	ViewCount       int64  `json:"view_count,omitempty"`
	ImportCount     int64  `json:"import_count,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
}

// KnowledgeHubShareListResult is returned by KnowledgeListMyHubShares.
type KnowledgeHubShareListResult struct {
	Items  []KnowledgeHubShareListItem `json:"items"`
	Total  int                         `json:"total"`
	Offset int                         `json:"offset"`
	Limit  int                         `json:"limit"`
	HubURL string                      `json:"hub_url"`
}

// KnowledgeHubShareDeleteRequest removes one of the current user's Hub shares.
type KnowledgeHubShareDeleteRequest struct {
	HubURL      string `json:"hub_url"`
	HubToken    string `json:"hub_token"`
	KnowledgeID string `json:"knowledge_id"`
}

// KnowledgeHubShareUpdateRequest patches metadata of one owned Hub share.
// Empty fields are left unchanged by the Hub when omitted appropriately;
// we always send title/description/visibility when provided.
type KnowledgeHubShareUpdateRequest struct {
	HubURL          string   `json:"hub_url"`
	HubToken        string   `json:"hub_token"`
	KnowledgeID     string   `json:"knowledge_id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	VisibilityScope string   `json:"visibility_scope"`
	VisibilityUsers []string `json:"visibility_users,omitempty"`
	TTL             string   `json:"ttl,omitempty"`
}

type KnowledgeHubShareImportResult struct {
	ImportStatus      string         `json:"import_status"`
	KnowledgeID       string         `json:"knowledge_id"`
	PackageID         string         `json:"package_id,omitempty"`
	Title             string         `json:"title,omitempty"`
	DryRun            bool           `json:"dry_run"`
	Imported          int            `json:"imported"`
	Skipped           int            `json:"skipped"`
	Failed            int            `json:"failed"`
	ImportedSourceIDs []string       `json:"imported_source_ids,omitempty"`
	SkippedSourceIDs  []string       `json:"skipped_source_ids,omitempty"`
	FailedSourceIDs   []string       `json:"failed_source_ids,omitempty"`
	RetrySourceIDs    []string       `json:"retry_source_ids,omitempty"`
	Warnings          []string       `json:"warnings,omitempty"`
	Share             map[string]any `json:"share,omitempty"`
}

type KnowledgeSyncRequest struct {
	HubURL           string `json:"hub_url"`
	HubToken         string `json:"hub_token"`
	TenantID         string `json:"tenant_id,omitempty"`
	Email            string `json:"email,omitempty"`
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
	PasswordVerifier    map[string]any `json:"password_verifier,omitempty"`
	HasPackage          bool           `json:"has_package"`
	Message             string         `json:"message,omitempty"`
}

type KnowledgeSyncResult struct {
	KnowledgeSyncStatus
	ImportStatus       string                  `json:"import_status,omitempty"`
	Imported           int                     `json:"imported,omitempty"`
	Skipped            int                     `json:"skipped,omitempty"`
	Failed             int                     `json:"failed,omitempty"`
	ImportedSourceIDs  []string                `json:"imported_source_ids,omitempty"`
	SkippedSourceIDs   []string                `json:"skipped_source_ids,omitempty"`
	FailedSourceIDs    []string                `json:"failed_source_ids,omitempty"`
	RetrySourceIDs     []string                `json:"retry_source_ids,omitempty"`
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
	maxGUIKnowledgePackageSourceContentBytes = 16 << 20
	maxGUIKnowledgePackageTotalContentBytes  = 40 << 20
	maxGUIKnowledgePackageSourceNodes        = 5000
	maxGUIKnowledgeHubPackageJSONBytes       = 50 << 20
	maxGUIKnowledgeHubShareRequestBytes      = 52 << 20
)

var knowledgeImportJobs sync.Map

// knowledgeImportJobsMu serializes load-modify-store on knowledgeImportJobs so
// concurrent progress ticks / finish / cancel cannot lose terminal status updates.
var knowledgeImportJobsMu sync.Mutex

// knowledgeImportActiveStores holds the live SQLiteStore for in-flight import jobs
// so KnowledgeCancelImportIndexing can abort background post-work.
var knowledgeImportActiveStores sync.Map // map[string]*knowledge.SQLiteStore

// knowledgeImportProgressLastEmit tracks last Wails progress emit time per job ID.
// Used to throttle high-frequency per-file callbacks during large imports.
var knowledgeImportProgressLastEmit sync.Map // map[string]time.Time

// knowledgeImportToastSent tracks job IDs that already emitted a completion toast
// so finish + post-work progress cannot double-toast under races.
var knowledgeImportToastSent sync.Map // map[string]struct{}

const knowledgeImportProgressMinInterval = 500 * time.Millisecond

// knowledgeImportProgressShouldEmit returns whether a progress event should be
// pushed to the frontend. force bypasses the interval (final status, failed items).
func knowledgeImportProgressShouldEmit(jobID string, now time.Time, force bool) bool {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return force
	}
	if force {
		knowledgeImportProgressLastEmit.Store(jobID, now)
		return true
	}
	if last, ok := knowledgeImportProgressLastEmit.Load(jobID); ok {
		if t, ok := last.(time.Time); ok && now.Sub(t) < knowledgeImportProgressMinInterval {
			return false
		}
	}
	knowledgeImportProgressLastEmit.Store(jobID, now)
	return true
}

func clearKnowledgeImportProgressThrottle(jobID string) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}
	knowledgeImportProgressLastEmit.Delete(jobID)
}

func knowledgeImportToastOnce(a *App, jobID string, result knowledge.DirectoryImportResult, err error) {
	jobID = strings.TrimSpace(jobID)
	if jobID != "" {
		if _, loaded := knowledgeImportToastSent.LoadOrStore(jobID, struct{}{}); loaded {
			return
		}
	}
	emitKnowledgeImportDoneToast(a, result, err)
}

func knowledgeImportStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case knowledge.ImportStatusCompleted, knowledge.ImportStatusFailed:
		return true
	default:
		return false
	}
}

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
		// Image nodes are useful only when their assets can be persisted and later
		// rendered by the agent UI. Configure this for every store instance, not
		// only for the callers that happen to import images.
		assets, err := knowledge.NewImageAssetManager(a.GetDataDir())
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		store.SetImageAssetManager(assets)
		if distiller := a.buildKnowledgeCardDistiller(); distiller != nil {
			store.SetCardDistiller(distiller)
		}
		a.configureKnowledgeImageDescriber(store)
		// Attach the app embedding runtime when available so Search() can hybrid
		// FTS + vector (auto-recall embedding fallback). Without this, stores opened
		// after activateEmbedderAsync never receive an embedder.
		a.attachKnowledgeEmbedder(store)
		return store, nil
	}, sleepKnowledgeStoreRetry)
}

// knowledgeEmbedder returns the best available non-noop embedder for knowledge search.
func (a *App) knowledgeEmbedder() embedding.Embedder {
	if a == nil {
		return nil
	}
	if a.memoryStore != nil {
		if emb := a.memoryStore.Embedder(); emb != nil && !embedding.IsNoop(emb) {
			return emb
		}
	}
	a.embeddingMu.Lock()
	emb := a.intentEmbedder
	a.embeddingMu.Unlock()
	if emb != nil && !embedding.IsNoop(emb) {
		return emb
	}
	return nil
}

// attachKnowledgeEmbedder wires the shared embedding model into a knowledge store.
func (a *App) attachKnowledgeEmbedder(store *knowledge.SQLiteStore) {
	if a == nil || store == nil {
		return
	}
	if emb := a.knowledgeEmbedder(); emb != nil {
		store.SetEmbedder(emb)
	}
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
			{DisplayName: "Documents (*.docx, *.pdf, *.ppt, *.pptx, *.xlsx, *.csv, *.md, *.txt, *.doc, *.xls)", Pattern: "*.docx;*.pdf;*.ppt;*.pptx;*.xlsx;*.csv;*.md;*.txt;*.doc;*.xls"},
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

// SelectKnowledgeSnapshotExportPath opens a save dialog for local knowledge export.
// format is "jsonl" (full snapshot) or "package" (exchange package JSON).
func (a *App) SelectKnowledgeSnapshotExportPath(format string) string {
	format = normalizeKnowledgeExportFormat(format, "")
	stamp := time.Now().UTC().Format("20060102-150405")
	opts := runtime.SaveDialogOptions{
		Title: "Export Knowledge Snapshot",
		Filters: []runtime.FileFilter{
			{DisplayName: "Knowledge Snapshot (*.jsonl)", Pattern: "*.jsonl"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
		DefaultFilename: fmt.Sprintf("maclaw-knowledge-%s.jsonl", stamp),
	}
	if format == "package" {
		opts.Title = "Export Knowledge Package"
		opts.DefaultFilename = fmt.Sprintf("maclaw-knowledge-%s.knowledge.json", stamp)
		opts.Filters = []runtime.FileFilter{
			{DisplayName: "Knowledge Package (*.knowledge.json)", Pattern: "*.knowledge.json"},
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		}
	}
	savePath, err := runtime.SaveFileDialog(a.ctx, opts)
	if err != nil {
		return ""
	}
	return savePath
}

// ExportTextFile shows a save dialog and writes the given text content to the chosen file.
// Returns the saved file path, or empty string if the user cancelled.
// Dialog filters are chosen from defaultFilename extension when possible.
func (a *App) ExportTextFile(content string, defaultFilename string) (string, error) {
	defaultFilename = strings.TrimSpace(defaultFilename)
	if defaultFilename == "" {
		defaultFilename = "export.txt"
	}
	ext := strings.ToLower(filepath.Ext(defaultFilename))
	filters := []runtime.FileFilter{{DisplayName: "All Files (*.*)", Pattern: "*.*"}}
	switch ext {
	case ".csv":
		filters = append([]runtime.FileFilter{{DisplayName: "CSV (*.csv)", Pattern: "*.csv"}}, filters...)
	case ".json":
		filters = append([]runtime.FileFilter{{DisplayName: "JSON (*.json)", Pattern: "*.json"}}, filters...)
	case ".jsonl":
		filters = append([]runtime.FileFilter{{DisplayName: "JSON Lines (*.jsonl)", Pattern: "*.jsonl"}}, filters...)
	case ".md", ".markdown":
		filters = append([]runtime.FileFilter{{DisplayName: "Markdown (*.md)", Pattern: "*.md"}}, filters...)
	case ".txt":
		filters = append([]runtime.FileFilter{{DisplayName: "Text (*.txt)", Pattern: "*.txt"}}, filters...)
	}
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export",
		DefaultFilename: defaultFilename,
		Filters:         filters,
	})
	if err != nil {
		return "", err
	}
	if savePath == "" {
		return "", nil
	}
	// If the user omitted an extension, append the default one.
	if filepath.Ext(savePath) == "" && ext != "" {
		savePath += ext
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
	format := normalizeKnowledgeExportFormat(req.Format, outputPath)
	if outputPath == "" {
		stamp := time.Now().UTC().Format("20060102-150405")
		if format == "package" {
			outputPath = filepath.Join(a.GetDataDir(), "knowledge-exports", fmt.Sprintf("knowledge-export-%s.knowledge.json", stamp))
		} else {
			outputPath = filepath.Join(a.GetDataDir(), "knowledge-exports", fmt.Sprintf("knowledge-export-%s.jsonl", stamp))
		}
	}
	req.OutputPath = outputPath
	req.Format = format
	if format == "package" {
		return a.exportKnowledgePackageFile(req)
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	defer store.Close()
	result, err := store.ExportSnapshot(a.knowledgeContext(), req)
	if err != nil {
		return result, err
	}
	if result.Format == "" {
		result.Format = "jsonl"
	}
	return result, nil
}

// exportKnowledgePackageFile writes an editable maclaw.knowledge.package JSON file
// (same shape used for Hub share), suitable for agent/Hub re-import.
func (a *App) exportKnowledgePackageFile(req knowledge.ExportOptions) (knowledge.ExportResult, error) {
	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return knowledge.ExportResult{}, fmt.Errorf("output path is required")
	}
	cfg, _ := a.LoadConfig()
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	defer store.Close()

	opts := knowledge.ListSourcesOptions{
		SourceIDs:       compactKnowledgeSourceIDStrings(req.SourceIDs),
		Limit:           5000,
		IncludeDisabled: true,
	}
	sources, err := store.ListSources(a.knowledgeContext(), opts)
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	if len(sources) == 0 {
		return knowledge.ExportResult{}, fmt.Errorf("no knowledge sources match the export request")
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fmt.Sprintf("Knowledge export %s", time.Now().UTC().Format("2006-01-02"))
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Local knowledge package exported from Maclaw."
	}
	pkg, warnings, err := buildGUIKnowledgePackage(a.knowledgeContext(), store, cfg, title, description, sources, req.RedactSensitive)
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	raw, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return knowledge.ExportResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return knowledge.ExportResult{}, err
	}
	if err := os.WriteFile(outputPath, raw, 0o644); err != nil {
		return knowledge.ExportResult{}, err
	}
	info, _ := os.Stat(outputPath)
	bytes := int64(len(raw))
	if info != nil {
		bytes = info.Size()
	}
	var nodes, cards, facts int
	for _, src := range sources {
		nodes += src.NodeCount
		cards += src.CardCount
		facts += src.FactCount
	}
	result := knowledge.ExportResult{
		OutputPath:      outputPath,
		Format:          "package",
		RedactSensitive: req.RedactSensitive,
		Scoped:          len(req.SourceIDs) > 0,
		SourceIDs:       knowledgeSourceIDs(sources),
		Sources:         len(sources),
		Nodes:           nodes,
		Cards:           cards,
		Facts:           facts,
		Bytes:           bytes,
		GeneratedAt:     time.Now().UTC(),
	}
	_ = warnings // retained for share path; local export success card uses counts/path
	return result, nil
}

func normalizeKnowledgeExportFormat(format, outputPath string) string {
	f := strings.ToLower(strings.TrimSpace(format))
	switch f {
	case "package", "knowledge_package", "mckb", "maclaw.knowledge.package", "knowledge.json":
		return "package"
	}
	lowerPath := strings.ToLower(strings.TrimSpace(outputPath))
	if strings.HasSuffix(lowerPath, ".knowledge.json") || strings.HasSuffix(lowerPath, ".mckb.json") {
		return "package"
	}
	return "jsonl"
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
		SourceIDs:       compactKnowledgeSourceIDStrings(req.SourceIDs),
		Limit:           5000,
		IncludeDisabled: true,
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
	body, sourceSummary, err := buildGUIKnowledgeSharePayloadWithinLimits(&pkg, packageWarnings, knowledgeSharePayloadOptions{
		SourceCount:       len(sources),
		SourceIDs:         exportedSourceIDs,
		RedactSensitive:   req.RedactSensitive,
		IncludeDisabled:   req.IncludeDisabled,
		Title:             strings.TrimSpace(req.Title),
		Description:       description,
		VisibilityScope:   strings.TrimSpace(req.VisibilityScope),
		VisibilityUsers:   compactKnowledgeShareStrings(req.VisibilityUsers),
		TTL:               ttl,
		PackageLimit:      maxGUIKnowledgeHubPackageJSONBytes,
		ShareRequestLimit: maxGUIKnowledgeHubShareRequestBytes,
	})
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

// KnowledgeListMyHubShares lists knowledge shares owned by the current Hub user.
// Calls GET /api/knowledge/shares/mine on the configured Hub.
func (a *App) KnowledgeListMyHubShares(req KnowledgeHubShareListRequest) (KnowledgeHubShareListResult, error) {
	hubURL, token, err := a.resolveKnowledgeHubAuth(req.HubURL, req.HubToken)
	if err != nil {
		return KnowledgeHubShareListResult{}, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	apiURL := fmt.Sprintf("%s/api/knowledge/shares/mine?limit=%d&offset=%d", hubURL, limit, offset)
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return KnowledgeHubShareListResult{}, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeHubShareListResult{}, fmt.Errorf("list hub knowledge shares: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeHubShareListResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeHubShareListResult{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload struct {
		Items  []map[string]any `json:"items"`
		Total  int              `json:"total"`
		Offset int              `json:"offset"`
		Limit  int              `json:"limit"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return KnowledgeHubShareListResult{}, fmt.Errorf("decode hub share list: %w", err)
	}
	items := make([]KnowledgeHubShareListItem, 0, len(payload.Items))
	for _, view := range payload.Items {
		item := knowledgeHubShareListItemFromView(hubURL, view)
		if item.KnowledgeID == "" {
			continue
		}
		items = append(items, item)
	}
	total := payload.Total
	if total == 0 {
		total = len(items)
	}
	return KnowledgeHubShareListResult{
		Items:  items,
		Total:  total,
		Offset: payload.Offset,
		Limit:  payload.Limit,
		HubURL: hubURL,
	}, nil
}

// KnowledgeUpdateHubShare patches title/description/visibility of an owned Hub share.
// Calls PATCH /api/knowledge/shares/{knowledgeID}. Does not re-upload the package.
func (a *App) KnowledgeUpdateHubShare(req KnowledgeHubShareUpdateRequest) (KnowledgeHubShareListItem, error) {
	knowledgeID := strings.TrimSpace(req.KnowledgeID)
	if knowledgeID == "" {
		return KnowledgeHubShareListItem{}, fmt.Errorf("knowledge_id is required")
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return KnowledgeHubShareListItem{}, fmt.Errorf("knowledge description is required")
	}
	hubURL, token, err := a.resolveKnowledgeHubAuth(req.HubURL, req.HubToken)
	if err != nil {
		return KnowledgeHubShareListItem{}, err
	}
	bodyMap := map[string]any{
		"description": description,
	}
	if title := strings.TrimSpace(req.Title); title != "" {
		bodyMap["title"] = title
	}
	if scope := strings.TrimSpace(req.VisibilityScope); scope != "" {
		bodyMap["visibility_scope"] = scope
	}
	if req.VisibilityUsers != nil {
		bodyMap["visibility_users"] = compactKnowledgeShareStrings(req.VisibilityUsers)
	}
	if ttl := strings.TrimSpace(req.TTL); ttl != "" {
		bodyMap["ttl"] = ttl
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return KnowledgeHubShareListItem{}, err
	}
	apiURL := hubURL + "/api/knowledge/shares/" + url.PathEscape(knowledgeID)
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, apiURL, bytes.NewReader(body))
	if err != nil {
		return KnowledgeHubShareListItem{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return KnowledgeHubShareListItem{}, fmt.Errorf("update hub knowledge share: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return KnowledgeHubShareListItem{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KnowledgeHubShareListItem{}, fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var view map[string]any
	if err := json.Unmarshal(respBody, &view); err != nil {
		return KnowledgeHubShareListItem{}, fmt.Errorf("decode hub share update: %w", err)
	}
	item := knowledgeHubShareListItemFromView(hubURL, view)
	if item.KnowledgeID == "" {
		item.KnowledgeID = knowledgeID
	}
	return item, nil
}

// KnowledgeDeleteHubShare deletes one Hub knowledge share owned by the current user.
// Calls DELETE /api/knowledge/shares/{knowledgeID}. Does not affect the local knowledge base.
func (a *App) KnowledgeDeleteHubShare(req KnowledgeHubShareDeleteRequest) error {
	knowledgeID := strings.TrimSpace(req.KnowledgeID)
	if knowledgeID == "" {
		return fmt.Errorf("knowledge_id is required")
	}
	hubURL, token, err := a.resolveKnowledgeHubAuth(req.HubURL, req.HubToken)
	if err != nil {
		return err
	}
	apiURL := hubURL + "/api/knowledge/shares/" + url.PathEscape(knowledgeID)
	ctx, cancel := context.WithTimeout(a.knowledgeContext(), 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("delete hub knowledge share: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (a *App) resolveKnowledgeHubAuth(hubURLIn, tokenIn string) (hubURL, token string, err error) {
	cfg, _ := a.LoadConfig()
	hubURL = strings.TrimRight(strings.TrimSpace(hubURLIn), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	}
	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required")
	}
	if parsed, parseErr := url.Parse(hubURL); parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	token = strings.TrimSpace(tokenIn)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	if token == "" {
		return "", "", fmt.Errorf("hub token is required")
	}
	return hubURL, token, nil
}

func knowledgeHubShareListItemFromView(hubURL string, view map[string]any) KnowledgeHubShareListItem {
	item := KnowledgeHubShareListItem{
		KnowledgeID:     stringFromAny(view["knowledge_id"]),
		Title:           stringFromAny(view["title"]),
		Description:     stringFromAny(view["description"]),
		VisibilityScope: stringFromAny(view["visibility_scope"]),
		Status:          stringFromAny(view["status"]),
		ShareURL:        absoluteHubShareField(hubURL, stringFromAny(view["share_url"])),
		AgentImport:     absoluteHubShareField(hubURL, stringFromAny(view["agent_import"])),
		ViewCount:       int64(intFromAny(view["view_count"])),
		ImportCount:     int64(intFromAny(view["import_count"])),
		CreatedAt:       stringFromAny(view["created_at"]),
		UpdatedAt:       stringFromAny(view["updated_at"]),
		ExpiresAt:       stringFromAny(view["expires_at"]),
	}
	if summary, ok := view["source_summary"].(map[string]any); ok {
		if n := intFromAny(summary["source_count"]); n > 0 {
			item.SourceCount = n
		} else if ids := knowledgeShareStringSliceFromAny(summary["source_ids"]); len(ids) > 0 {
			item.SourceCount = len(ids)
		}
	}
	if item.ShareURL == "" && item.KnowledgeID != "" {
		item.ShareURL = hubURL + "/hub/knowledge/shares/" + url.PathEscape(item.KnowledgeID)
	}
	if item.AgentImport == "" && item.KnowledgeID != "" {
		item.AgentImport = hubURL + "/api/knowledge/shares/" + url.PathEscape(item.KnowledgeID) + "?intent=import"
	}
	if item.Title == "" {
		item.Title = item.KnowledgeID
	}
	return item
}

func validateGUIKnowledgePackageJSONSize(rawPackage []byte) error {
	if len(rawPackage) <= maxGUIKnowledgeHubPackageJSONBytes {
		return nil
	}
	return fmt.Errorf("knowledge package JSON is %.1fMB; hub accepts at most %.0fMB, reduce selected sources or use knowledge sync for large transfers", float64(len(rawPackage))/(1024*1024), float64(maxGUIKnowledgeHubPackageJSONBytes)/(1024*1024))
}

func validateGUIKnowledgeShareRequestSize(body []byte) error {
	if len(body) <= maxGUIKnowledgeHubShareRequestBytes {
		return nil
	}
	return fmt.Errorf("knowledge share request is %.1fMB; reduce selected sources or use knowledge sync for large transfers", float64(len(body))/(1024*1024))
}

type knowledgeSharePayloadOptions struct {
	SourceCount       int
	SourceIDs         []string
	RedactSensitive   bool
	IncludeDisabled   bool
	Title             string
	Description       string
	VisibilityScope   string
	VisibilityUsers   []string
	TTL               string
	PackageLimit      int
	ShareRequestLimit int
}

func buildGUIKnowledgeSharePayloadWithinLimits(pkg *guiKnowledgePackage, baseWarnings []string, opts knowledgeSharePayloadOptions) ([]byte, map[string]any, error) {
	if pkg == nil {
		return nil, nil, fmt.Errorf("knowledge package is nil")
	}
	if opts.PackageLimit <= 0 {
		opts.PackageLimit = maxGUIKnowledgeHubPackageJSONBytes
	}
	if opts.ShareRequestLimit <= 0 {
		opts.ShareRequestLimit = maxGUIKnowledgeHubShareRequestBytes
	}
	warnings := append([]string(nil), baseWarnings...)
	packageFitWarningsAppended := false
	for attempts := 0; attempts < len(pkg.Sources)*4+16; attempts++ {
		rawPackage, fitWarnings, err := marshalGUIKnowledgePackageWithinLimit(pkg, opts.PackageLimit)
		if err != nil {
			return nil, nil, err
		}
		if !packageFitWarningsAppended {
			warnings = append(warnings, fitWarnings...)
			packageFitWarningsAppended = true
		}
		sourceSummary := guiKnowledgeShareSourceSummary(pkg, warnings, opts)
		payload := map[string]any{
			"title":            opts.Title,
			"description":      opts.Description,
			"visibility_scope": opts.VisibilityScope,
			"visibility_users": opts.VisibilityUsers,
			"ttl":              opts.TTL,
			"package_json":     json.RawMessage(rawPackage),
			"source_summary":   sourceSummary,
		}
		if payload["visibility_scope"] == "" {
			payload["visibility_scope"] = "hub"
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		if len(body) <= opts.ShareRequestLimit {
			return body, sourceSummary, nil
		}
		idx := largestGUIKnowledgePackageContentSource(pkg.Sources)
		if idx < 0 {
			return nil, nil, validateGUIKnowledgeShareRequestSize(body)
		}
		currentBytes := len([]byte(pkg.Sources[idx].Content))
		if currentBytes == 0 {
			return nil, nil, validateGUIKnowledgeShareRequestSize(body)
		}
		excess := len(body) - opts.ShareRequestLimit
		warning := truncateGUIKnowledgePackageSourceContentForFit(pkg, idx, excess+(256<<10), "hub share request size limit")
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	rawPackage, _, err := marshalGUIKnowledgePackageWithinLimit(pkg, opts.PackageLimit)
	if err != nil {
		return nil, nil, err
	}
	sourceSummary := guiKnowledgeShareSourceSummary(pkg, warnings, opts)
	body, err := json.Marshal(map[string]any{
		"title":            opts.Title,
		"description":      opts.Description,
		"visibility_scope": firstNonEmptyKnowledgeValue(opts.VisibilityScope, "hub"),
		"visibility_users": opts.VisibilityUsers,
		"ttl":              opts.TTL,
		"package_json":     json.RawMessage(rawPackage),
		"source_summary":   sourceSummary,
	})
	if err != nil {
		return nil, nil, err
	}
	if len(body) > opts.ShareRequestLimit {
		return nil, nil, validateGUIKnowledgeShareRequestSize(body)
	}
	return body, sourceSummary, nil
}

func guiKnowledgeShareSourceSummary(pkg *guiKnowledgePackage, warnings []string, opts knowledgeSharePayloadOptions) map[string]any {
	return map[string]any{
		"source_count":     opts.SourceCount,
		"source_ids":       opts.SourceIDs,
		"redact_sensitive": opts.RedactSensitive,
		"include_disabled": opts.IncludeDisabled,
		"package_format":   pkg.Manifest.Format,
		"package_id":       pkg.Manifest.PackageID,
		"generated_by":     "maclaw-gui",
		"generated_at":     pkg.Manifest.CreatedAt,
		"editable":         true,
		"content_sources":  countGUIKnowledgePackageContentSources(*pkg),
		"warnings":         warnings,
	}
}

func marshalGUIKnowledgePackageWithinLimit(pkg *guiKnowledgePackage, limitBytes int) ([]byte, []string, error) {
	if pkg == nil {
		return nil, nil, fmt.Errorf("knowledge package is nil")
	}
	warnings := []string{}
	for attempts := 0; attempts < len(pkg.Sources)*4+16; attempts++ {
		raw, err := json.Marshal(pkg)
		if err != nil {
			return nil, warnings, err
		}
		if len(raw) <= limitBytes {
			return raw, warnings, nil
		}
		idx := largestGUIKnowledgePackageContentSource(pkg.Sources)
		if idx < 0 {
			return nil, warnings, validateGUIKnowledgePackageJSONSize(raw)
		}
		currentBytes := len([]byte(pkg.Sources[idx].Content))
		if currentBytes == 0 {
			return nil, warnings, validateGUIKnowledgePackageJSONSize(raw)
		}
		warning := truncateGUIKnowledgePackageSourceContentForFit(pkg, idx, len(raw)-limitBytes+(1<<20), "hub package size limit")
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		return nil, warnings, err
	}
	if len(raw) > limitBytes {
		return nil, warnings, validateGUIKnowledgePackageJSONSize(raw)
	}
	return raw, warnings, nil
}

func truncateGUIKnowledgePackageSourceContentForFit(pkg *guiKnowledgePackage, idx int, cutBytes int, reason string) string {
	if pkg == nil || idx < 0 || idx >= len(pkg.Sources) {
		return ""
	}
	currentBytes := len([]byte(pkg.Sources[idx].Content))
	if currentBytes == 0 {
		return ""
	}
	minCut := currentBytes / 10
	if minCut < 64<<10 {
		minCut = 64 << 10
	}
	if cutBytes < minCut {
		cutBytes = minCut
	}
	nextBytes := currentBytes - cutBytes
	if nextBytes < 0 {
		nextBytes = 0
	}
	sourceLabel := firstNonEmptyKnowledgeValue(pkg.Sources[idx].ID, pkg.Sources[idx].Title, pkg.Sources[idx].URI)
	if sourceLabel == "" {
		sourceLabel = "unknown source"
	}
	pkg.Sources[idx].Content = truncateStringToUTF8Bytes(pkg.Sources[idx].Content, nextBytes)
	pkg.Sources[idx].ContentBytes = len([]byte(pkg.Sources[idx].Content))
	pkg.Sources[idx].Truncated = true
	return fmt.Sprintf("source %s content truncated further to fit %s", sourceLabel, reason)
}

func largestGUIKnowledgePackageContentSource(sources []guiKnowledgePackageSource) int {
	idx := -1
	maxBytes := 0
	for i, source := range sources {
		contentBytes := len([]byte(source.Content))
		if contentBytes > maxBytes {
			idx = i
			maxBytes = contentBytes
		}
	}
	return idx
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
	downloadCtx, cancelDownload := context.WithTimeout(a.knowledgeContext(), 45*time.Second)
	share, err := fetchGUIKnowledgeShareJSON(downloadCtx, apiURL, authHeader)
	if err != nil {
		cancelDownload()
		return KnowledgeHubShareImportResult{}, err
	}
	packageURL := resolveGUIKnowledgePackageURL(apiURL, stringFromAny(share["package_url"]))
	if packageURL == "" {
		cancelDownload()
		return KnowledgeHubShareImportResult{}, fmt.Errorf("knowledge share does not expose a package_url")
	}
	pkg, err := fetchGUIKnowledgePackage(downloadCtx, packageURL, authHeader)
	cancelDownload()
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
			ID:               item.ID,
			Kind:             item.Kind,
			URI:              item.URI,
			CanonicalURI:     item.CanonicalURI,
			Title:            item.Title,
			TopicHint:        item.TopicHint,
			Labels:           item.Labels,
			Content:          item.Content,
			ContentTruncated: item.Truncated,
		})
	}
	if req.DryRun {
		// Dry-run uses the same classification logic as real import — no store needed.
		importResult := knowledge.ImportPackageSources(a.knowledgeContext(), nil, sources, knowledge.PackageImportOptions{
			DryRun:    true,
			TopicHint: pkg.Manifest.Title,
			RootPath:  "share://" + knowledgeID,
		})
		result.Imported = importResult.Imported
		result.ImportStatus = importResult.Status
		result.Skipped = importResult.Skipped
		result.Failed = importResult.Failed
		result.ImportedSourceIDs = importResult.ImportedSourceIDs
		result.SkippedSourceIDs = importResult.SkippedSourceIDs
		result.FailedSourceIDs = importResult.FailedSourceIDs
		result.RetrySourceIDs = importResult.RetrySourceIDs
		result.Warnings = append(result.Warnings, importResult.Warnings...)
		return result, nil
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return result, err
	}
	defer store.Close()
	importResult := knowledge.ImportPackageSources(a.knowledgeContext(), store, sources, knowledge.PackageImportOptions{
		SaveScope: knowledge.SaveScopePersonal,
		TopicHint: pkg.Manifest.Title,
		RootPath:  "share://" + knowledgeID,
	})
	result.Imported = importResult.Imported
	result.ImportStatus = importResult.Status
	result.Skipped = importResult.Skipped
	result.Failed = importResult.Failed
	result.ImportedSourceIDs = importResult.ImportedSourceIDs
	result.SkippedSourceIDs = importResult.SkippedSourceIDs
	result.FailedSourceIDs = importResult.FailedSourceIDs
	result.RetrySourceIDs = importResult.RetrySourceIDs
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
	setKnowledgeSyncIdentityHeaders(httpReq, req)
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

func setKnowledgeSyncIdentityHeaders(httpReq *http.Request, req KnowledgeSyncRequest) {
	if httpReq == nil {
		return
	}
	if tenantID := strings.TrimSpace(req.TenantID); tenantID != "" {
		httpReq.Header.Set("X-Maclaw-Tenant-ID", tenantID)
	}
	if email := strings.TrimSpace(req.Email); email != "" {
		httpReq.Header.Set("X-Maclaw-User-Email", email)
	}
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
	sources, err := store.ListSources(a.knowledgeContext(), knowledge.ListSourcesOptions{Limit: 5000, IncludeDisabled: true})
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
	passwordVerifier, err := encryptKnowledgeSyncPasswordVerifier(password)
	if err != nil {
		return KnowledgeSyncResult{}, err
	}
	payload := map[string]any{
		"package_id":            pkg.Manifest.PackageID,
		"package_version":       pkg.Manifest.Version,
		"compressed_size_bytes": compressedSize,
		"encryption":            encryption,
		"password_verifier":     passwordVerifier,
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
	setKnowledgeSyncIdentityHeaders(httpReq, req)
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
			ID:               item.ID,
			Kind:             item.Kind,
			URI:              item.URI,
			CanonicalURI:     item.CanonicalURI,
			Title:            item.Title,
			TopicHint:        item.TopicHint,
			Labels:           item.Labels,
			Content:          item.Content,
			ContentTruncated: item.Truncated,
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
	skippedConflictSourceIDs := make([]string, 0)
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
				skippedConflictSourceIDs = append(skippedConflictSourceIDs, firstNonEmptyKnowledgeValue(source.ID, source.CanonicalURI, source.URI, source.Title))
				continue
			}
			filtered = append(filtered, source)
		}
		sources = filtered
	}
	importResult := knowledge.ImportPackageSources(ctx, store, sources, knowledge.PackageImportOptions{
		SaveScope: knowledge.SaveScopePersonal,
		TopicHint: pkg.Manifest.Title,
		RootPath:  "sync://" + pkg.Manifest.PackageID,
	})
	warnings := append([]string{}, importResult.Warnings...)
	if len(conflicts) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d conflicting source(s) handled with strategy %q", len(conflicts), firstNonEmptyKnowledgeValue(strategy, "import")))
	}
	return KnowledgeSyncResult{
		KnowledgeSyncStatus: status,
		ImportStatus:        importResult.Status,
		Imported:            importResult.Imported,
		Skipped:             importResult.Skipped + skippedConflicts,
		Failed:              importResult.Failed,
		ImportedSourceIDs:   importResult.ImportedSourceIDs,
		SkippedSourceIDs:    append(skippedConflictSourceIDs, importResult.SkippedSourceIDs...),
		FailedSourceIDs:     importResult.FailedSourceIDs,
		RetrySourceIDs:      importResult.RetrySourceIDs,
		Warnings:            warnings,
		Conflicts:           conflicts,
	}, nil
}

func (a *App) KnowledgeSyncVerifyPassword(req KnowledgeSyncRequest) (KnowledgeSyncStatus, error) {
	if strings.TrimSpace(req.Password) == "" {
		return KnowledgeSyncStatus{}, fmt.Errorf("sync password is required")
	}
	status, err := a.KnowledgeSyncStatus(req)
	if err != nil {
		return KnowledgeSyncStatus{}, err
	}
	if !status.HasPackage {
		return KnowledgeSyncStatus{}, fmt.Errorf("no cloud sync package is available")
	}
	if len(status.PasswordVerifier) > 0 {
		if err := decryptKnowledgeSyncPasswordVerifier(strings.TrimSpace(req.Password), status.PasswordVerifier); err != nil {
			return KnowledgeSyncStatus{}, err
		}
		return status, nil
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
	setKnowledgeSyncIdentityHeaders(httpReq, req)
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
	setKnowledgeSyncIdentityHeaders(httpReq, req)
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

func encryptKnowledgeSyncPasswordVerifier(password string) (map[string]any, error) {
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
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
	ciphertext := gcm.Seal(nil, nonce, []byte("maclaw.knowledge.sync.password-ok.v1"), []byte("maclaw.knowledge.sync.password.v1"))
	return map[string]any{
		"algorithm":  "AES-256-GCM",
		"kdf":        "scrypt",
		"n":          1 << 15,
		"r":          8,
		"p":          1,
		"salt":       base64.StdEncoding.EncodeToString(salt),
		"nonce":      base64.StdEncoding.EncodeToString(nonce),
		"ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptKnowledgeSyncPasswordVerifier(password string, verifier map[string]any) error {
	if len(verifier) == 0 {
		return fmt.Errorf("sync password verifier is missing")
	}
	salt, err := base64.StdEncoding.DecodeString(stringFromAny(verifier["salt"]))
	if err != nil {
		return fmt.Errorf("invalid sync password verifier salt")
	}
	nonce, err := base64.StdEncoding.DecodeString(stringFromAny(verifier["nonce"]))
	if err != nil {
		return fmt.Errorf("invalid sync password verifier nonce")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(stringFromAny(verifier["ciphertext"]))
	if err != nil {
		return fmt.Errorf("invalid sync password verifier payload")
	}
	n := intFromAny(verifier["n"])
	r := intFromAny(verifier["r"])
	p := intFromAny(verifier["p"])
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
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte("maclaw.knowledge.sync.password.v1"))
	if err != nil || string(plaintext) != "maclaw.knowledge.sync.password-ok.v1" {
		return fmt.Errorf("sync password is incorrect")
	}
	return nil
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGUIKnowledgeHubPackageJSONBytes+1))
	if err != nil {
		return pkg, err
	}
	if len(body) > maxGUIKnowledgeHubPackageJSONBytes {
		return pkg, fmt.Errorf("knowledge package is too large")
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
	results, personalErr := store.Search(a.knowledgeContext(), opts)
	// Merge enterprise digital assets (active libraries only) into UI search.
	// Still attempt enterprise when personal search fails so Hub cache remains usable.
	if q := strings.TrimSpace(opts.Query); q != "" {
		if ent, entErr := a.EnterpriseKnowledgeSearch(q, ""); entErr == nil && len(ent) > 0 {
			limit := opts.Limit
			if limit <= 0 {
				limit = 20
			}
			results = enterpriseknowledge.MergeSearchResults(results, ent, limit, true)
			return results, nil
		}
	}
	if personalErr != nil {
		return nil, personalErr
	}
	return results, nil
}

// KnowledgeSearchImages searches only locally imported image nodes. Enterprise
// digital assets currently synchronize textual evidence only, so their search
// results cannot be rendered as authenticated local image assets and are not
// mixed into this display-capable route.
func (a *App) KnowledgeSearchImages(opts knowledge.ImageSearchOptions) ([]knowledge.SearchResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts.SearchOptions = a.normalizeKnowledgeSearchOptions(opts.SearchOptions)
	return store.SearchImages(a.knowledgeContext(), opts)
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
	return store.RefreshSourceWithOfficeReadConfig(a.knowledgeContext(), id, guiOfficeReadConfigPtr(a.peekConfigOrEmpty()))
}

func (a *App) KnowledgePreviewSourceRefresh(id string) (knowledge.SourceChangePreview, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceChangePreview{}, err
	}
	defer store.Close()
	return store.PreviewSourceRefreshWithOfficeReadConfig(a.knowledgeContext(), id, guiOfficeReadConfigPtr(a.peekConfigOrEmpty()))
}

func (a *App) KnowledgePreviewSourcesRefresh(ids []string) (knowledge.SourceChangePreviewResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceChangePreviewResult{}, err
	}
	defer store.Close()
	return store.PreviewSourcesRefreshWithOfficeReadConfig(a.knowledgeContext(), ids, guiOfficeReadConfigPtr(a.peekConfigOrEmpty())), nil
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
	return store.PreviewSourcesRefreshByFilterWithOfficeReadConfig(a.knowledgeContext(), opts, guiOfficeReadConfigPtr(a.peekConfigOrEmpty()))
}

func (a *App) KnowledgeRefreshChangedSources(ids []string) (knowledge.ChangedSourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.ChangedSourceRefreshResult{}, err
	}
	defer store.Close()
	return store.RefreshChangedSourcesWithOfficeReadConfig(a.knowledgeContext(), ids, guiOfficeReadConfigPtr(a.peekConfigOrEmpty())), nil
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
	return store.RefreshChangedSourcesByFilterWithOfficeReadConfig(a.knowledgeContext(), opts, guiOfficeReadConfigPtr(a.peekConfigOrEmpty()))
}

func (a *App) KnowledgeRefreshSources(ids []string) (knowledge.SourceRefreshResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.SourceRefreshResult{}, err
	}
	defer store.Close()
	return store.RefreshSourcesWithOfficeReadConfig(a.knowledgeContext(), ids, guiOfficeReadConfigPtr(a.peekConfigOrEmpty())), nil
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
	return store.RefreshSourcesByFilterWithOfficeReadConfig(a.knowledgeContext(), opts, guiOfficeReadConfigPtr(a.peekConfigOrEmpty()))
}

// guiOfficeReadConfigPtr snapshots the live GUI policy for a single knowledge
// operation. A copy prevents later settings changes from mutating a refresh
// that is already parsing its private document snapshot.
func guiOfficeReadConfigPtr(cfg corelib.AppConfig) *agent.OfficeReadConfig {
	return agent.CloneOfficeReadConfigPtr(&agent.OfficeReadConfig{
		Engine:       cfg.OfficeReadEngine,
		Formats:      cfg.OfficeReadFormats,
		Fallback:     cfg.OfficeReadFallback,
		EmitMarkdown: cfg.OfficeReadEmitMarkdown,
	})
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
	store.SetScanProgressCallback(a.knowledgeScanProgressEmitter())
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
	store.SetScanProgressCallback(a.knowledgeScanProgressEmitter())
	return store.ScanFiles(a.knowledgeContext(), req, filePaths)
}

// knowledgeScanProgressEmitter throttles scan precheck events so large trees
// do not flood the frontend.
func (a *App) knowledgeScanProgressEmitter() knowledge.ScanProgressFunc {
	var mu sync.Mutex
	var lastPhase string
	var lastAt time.Time
	var lastDone int
	const minInterval = 150 * time.Millisecond
	return func(phase string, done, total int, path string) {
		if a == nil || a.ctx == nil {
			return
		}
		mu.Lock()
		now := time.Now()
		force := phase != lastPhase || done <= 1 || (total > 0 && done >= total) || done-lastDone >= 16
		if !force && !lastAt.IsZero() && now.Sub(lastAt) < minInterval {
			mu.Unlock()
			return
		}
		lastPhase = phase
		lastAt = now
		lastDone = done
		mu.Unlock()
		a.emitEvent("knowledge:scan-progress", map[string]interface{}{
			"phase":        phase,
			"done":         done,
			"total":        total,
			"current_path": path,
		})
	}
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

// KnowledgePreviewNodesBySource returns bounded display data for the GUI
// source inspector. It intentionally does not reopen a source file or expose
// full node metadata and text through the Wails bridge.
func (a *App) KnowledgePreviewNodesBySource(sourceID string, limit int) ([]knowledge.DocumentNodePreview, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListNodePreviewsBySource(a.knowledgeContext(), sourceID, limit)
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
		knowledgeImportActiveStores.Store(jobID, store)
		// Keep store registered until close finishes so CancelBackground works
		// during post-work wait (do not Delete before Wait/Close).
		// WaitBackground BEFORE Close: Close cancels post-work, which would abort
		// linking/embedding the moment ingest returns "indexing".
		defer func() {
			store.WaitBackground()
			_ = store.Close()
			knowledgeImportActiveStores.Delete(jobID)
		}()
		store.SetImportProgressCallback(func(progress knowledge.DirectoryImportResult) {
			updateKnowledgeImportJobProgress(a, jobID, progress)
		})
		// Scan phase during import (walk/hash) — same events as dry-run precheck.
		store.SetScanProgressCallback(a.knowledgeScanProgressEmitter())
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
		knowledgeImportActiveStores.Store(jobID, store)
		// WaitBackground before Close so async indexing is not cancelled on return.
		defer func() {
			store.WaitBackground()
			_ = store.Close()
			knowledgeImportActiveStores.Delete(jobID)
		}()
		store.SetImportProgressCallback(func(progress knowledge.DirectoryImportResult) {
			updateKnowledgeImportJobProgress(a, jobID, progress)
		})
		store.SetScanProgressCallback(a.knowledgeScanProgressEmitter())
		result, err := store.ImportFiles(a.knowledgeContext(), req, filePaths)
		finishKnowledgeImportJob(a, jobID, result, err)
	}(job.ID, req, filePaths)

	return job, nil
}

// KnowledgeCancelImportIndexing aborts background topic-linking / embedding for a
// job that is already in the "indexing" phase. Imported files remain in the DB.
// File-ingest ("running") cannot be cancelled mid-transaction.
func (a *App) KnowledgeCancelImportIndexing(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("knowledge import job id is required")
	}
	knowledgeImportJobsMu.Lock()
	v, ok := knowledgeImportJobs.Load(id)
	if !ok {
		knowledgeImportJobsMu.Unlock()
		return fmt.Errorf("knowledge import job %s not found", id)
	}
	job, ok := v.(KnowledgeImportJob)
	if !ok {
		knowledgeImportJobsMu.Unlock()
		return fmt.Errorf("knowledge import job %s has invalid state", id)
	}
	st := strings.ToLower(strings.TrimSpace(job.Status))
	// Only indexing (post-work) is cancellable. Running means the write txn is open.
	if st != knowledge.ImportStatusIndexing {
		knowledgeImportJobsMu.Unlock()
		if knowledgeImportStatusTerminal(st) {
			return nil // already finished
		}
		return fmt.Errorf("import job is not in indexing phase (status=%s)", job.Status)
	}
	// Optimistically reflect skip; keep failed if file ingest had failures.
	terminal := knowledge.ImportStatusCompleted
	if job.Result.FailedFiles > 0 {
		terminal = knowledge.ImportStatusFailed
	}
	job.Status = terminal
	job.Result.Status = terminal
	job.Result.CurrentStep = ""
	job.Result.StepProgress = 0
	job.UpdatedAt = time.Now().UTC()
	knowledgeImportJobs.Store(id, job)
	resultSnap := job.Result
	knowledgeImportJobsMu.Unlock()

	// Cancel outside the job mutex so WaitBackground on the import goroutine
	// can finish without contending with this critical section.
	if storeV, ok := knowledgeImportActiveStores.Load(id); ok {
		if store, ok := storeV.(*knowledge.SQLiteStore); ok && store != nil {
			store.CancelBackground()
		}
	}
	if a != nil && a.ctx != nil {
		a.emitEvent("knowledge:import-progress", map[string]interface{}{
			"job_id":          id,
			"status":          terminal,
			"total_files":     resultSnap.TotalFiles,
			"processed_files": resultSnap.ProcessedFiles,
			"imported_files":  resultSnap.ImportedFiles,
			"skipped_files":   resultSnap.SkippedFiles,
			"failed_files":    resultSnap.FailedFiles,
			"current_step":    "",
			"step_progress":   0,
		})
		// Toast now: post-work may finish with prevStatus already terminal (no toast).
		knowledgeImportToastOnce(a, id, resultSnap, nil)
		clearKnowledgeImportProgressThrottle(id)
	}
	return nil
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
	knowledgeImportJobsMu.Lock()
	defer knowledgeImportJobsMu.Unlock()
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
	prevStatus := job.Status
	prevStep := job.Result.CurrentStep
	prevStepProgress := job.Result.StepProgress
	if result.Status == "" {
		result.Status = knowledge.ImportStatusRunning
	}
	// Never regress a terminal job (cancel/skip can complete before late indexing ticks).
	if knowledgeImportStatusTerminal(prevStatus) {
		switch result.Status {
		case knowledge.ImportStatusIndexing, knowledge.ImportStatusRunning, knowledge.ImportStatusQueued, "pending":
			return
		case knowledge.ImportStatusCompleted:
			// Prefer failed over completed when we already recorded failures.
			if prevStatus == knowledge.ImportStatusFailed {
				return
			}
		}
	}
	job.Result = result
	job.Status = result.Status
	job.UpdatedAt = time.Now().UTC()
	knowledgeImportJobs.Store(id, job)

	// Emit Wails event for real-time frontend updates (throttled for large batches).
	// Job state above is always updated so polling still sees accurate counters.
	if a != nil && a.ctx != nil {
		// Prefer store-provided last-item fields (include skip/fail reason).
		lastPath := ""
		lastStatus := ""
		lastReason := ""
		if result.LastItemPath != "" && result.LastItemStatus != "" {
			lastPath = result.LastItemPath
			lastStatus = result.LastItemStatus
			lastReason = result.LastItemReason
		} else if result.ProcessedFiles > prevProcessed && result.CurrentFile != "" {
			// Fallback: infer status from counter deltas when LastItem* unset.
			lastPath = result.CurrentFile
			lastStatus = "imported"
			if result.FailedFiles > prevFailed {
				lastStatus = "failed"
			} else if result.SkippedFiles > prevSkipped {
				lastStatus = "skipped"
			}
		}
		// Force-emit only meaningful transitions — not every indexing percent tick
		// (bulk linking would otherwise flood the UI bus).
		forceEmit := lastStatus == "failed" ||
			result.Status == knowledge.ImportStatusCompleted ||
			result.Status == knowledge.ImportStatusFailed ||
			(result.Status == knowledge.ImportStatusIndexing &&
				(prevStatus != knowledge.ImportStatusIndexing ||
					result.CurrentStep != prevStep ||
					result.StepProgress == 0 ||
					result.StepProgress >= 100 ||
					result.StepProgress-prevStepProgress >= 10))
		if !knowledgeImportProgressShouldEmit(id, time.Now(), forceEmit) {
			return
		}
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
		if lastPath != "" && lastStatus != "" {
			eventData["last_item_path"] = lastPath
			eventData["last_item_status"] = lastStatus
			if lastReason != "" {
				eventData["last_item_reason"] = lastReason
			}
		}
		a.emitEvent("knowledge:import-progress", eventData)

		// Toast once when post-work (or any path) reaches a true terminal state.
		// Accept prev running/indexing so a fast post-work race before finish still toasts.
		if knowledgeImportStatusTerminal(result.Status) && result.CurrentStep == "" &&
			(prevStatus == knowledge.ImportStatusIndexing ||
				prevStatus == knowledge.ImportStatusRunning ||
				prevStatus == knowledge.ImportStatusQueued ||
				prevStatus == "pending" ||
				prevStatus == "") {
			knowledgeImportToastOnce(a, id, result, nil)
			clearKnowledgeImportProgressThrottle(id)
		}
	}
}

func finishKnowledgeImportJob(a *App, id string, result knowledge.DirectoryImportResult, err error) {
	knowledgeImportJobsMu.Lock()
	defer knowledgeImportJobsMu.Unlock()
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
	// Critical race: background post-work / cancel may reach a terminal state
	// BEFORE this finish call. Never regress terminal -> non-terminal, and never
	// overwrite failed with completed.
	if err == nil && knowledgeImportStatusTerminal(job.Status) {
		switch result.Status {
		case knowledge.ImportStatusIndexing, knowledge.ImportStatusRunning, knowledge.ImportStatusQueued, "pending":
			if len(result.FailedItems) > 0 && len(job.Result.FailedItems) == 0 {
				job.Result.FailedItems = result.FailedItems
				job.Result.FailedFiles = result.FailedFiles
				knowledgeImportJobs.Store(id, job)
			}
			return
		case knowledge.ImportStatusCompleted:
			if job.Status == knowledge.ImportStatusFailed {
				return
			}
		}
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

	// Emit status event (always immediate; includes failure details).
	if a != nil && a.ctx != nil {
		eventData := map[string]interface{}{
			"job_id":           id,
			"status":           job.Status,
			"total_files":      result.TotalFiles,
			"processed_files":  result.ProcessedFiles,
			"imported_files":   result.ImportedFiles,
			"skipped_files":    result.SkippedFiles,
			"failed_files":     result.FailedFiles,
			"current_step":     result.CurrentStep,
			"step_progress":    result.StepProgress,
			"total_steps":      result.TotalSteps,
			"current_step_num": result.CurrentStepNum,
		}
		if job.Error != "" {
			eventData["error"] = job.Error
		}
		if len(result.FailedItems) > 0 {
			eventData["failed_items"] = result.FailedItems
		}
		a.emitEvent("knowledge:import-progress", eventData)
		// Defer toast until post-work finishes when still indexing.
		if job.Status != knowledge.ImportStatusIndexing {
			knowledgeImportToastOnce(a, id, result, err)
			clearKnowledgeImportProgressThrottle(id)
		}
		return
	}
	clearKnowledgeImportProgressThrottle(id)
}

// knowledgeImportDoneToast builds toast message/type/duration for import completion.
// Pure helper for unit tests; used by emitKnowledgeImportDoneToast.
func knowledgeImportDoneToast(result knowledge.DirectoryImportResult, err error) (message, typ string, duration int) {
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "unknown error"
		}
		if len([]rune(msg)) > 120 {
			msg = string([]rune(msg)[:117]) + "..."
		}
		return fmt.Sprintf("知识库导入失败：%s", msg), "error", 5000
	}
	if result.ImportedFiles > 0 && result.FailedFiles > 0 {
		return fmt.Sprintf("知识库导入完成：%d 个文件已导入，%d 个失败", result.ImportedFiles, result.FailedFiles), "warning", 5000
	}
	if result.FailedFiles > 0 || result.Status == knowledge.ImportStatusFailed {
		if result.FailedFiles > 0 {
			return fmt.Sprintf("知识库导入失败：%d 个文件失败", result.FailedFiles), "error", 5000
		}
		return "知识库导入失败", "error", 5000
	}
	msg := fmt.Sprintf("知识库导入完成：%d 个文件已导入", result.ImportedFiles)
	if result.SkippedFiles > 0 {
		msg = fmt.Sprintf("知识库导入完成：%d 个文件已导入，%d 个跳过", result.ImportedFiles, result.SkippedFiles)
	}
	return msg, "success", 4000
}

func emitKnowledgeImportDoneToast(a *App, result knowledge.DirectoryImportResult, err error) {
	if a == nil {
		return
	}
	message, typ, duration := knowledgeImportDoneToast(result, err)
	a.emitEvent("show-toast", map[string]interface{}{
		"message":  message,
		"type":     typ,
		"duration": duration,
	})
}

func (a *App) KnowledgeImportDirectory(req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	defer store.Close()
	req = a.normalizeKnowledgeImportRequest(req)
	req.DryRun = false
	result, err := store.ImportDirectory(a.knowledgeContext(), req)
	return result, err
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
	source, err := store.SaveURL(a.knowledgeContext(), knowledge.URLSaveRequest{
		URL:         rawURL,
		SaveScope:   saveScope,
		ProjectPath: projectPath,
		TopicHint:   topicHint,
		DistillMode: distillMode,
		Labels:      labels,
		AutoLabels:  autoLabels,
	})
	return source, err
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
	result := store.SaveURLs(a.knowledgeContext(), knowledge.URLBatchSaveRequest{
		URLs:        rawURLs,
		SaveScope:   saveScope,
		ProjectPath: projectPath,
		TopicHint:   topicHint,
		DistillMode: distillMode,
		Labels:      labels,
		AutoLabels:  autoLabels,
	})
	return result, nil
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
	source, err := store.SaveText(a.knowledgeContext(), req)
	return source, err
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
		key := value
		if strings.HasPrefix(strings.ToLower(value), "ksrc_") {
			value = strings.ToLower(value)
			key = value
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
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
			a.emitEvent("knowledge:deep-crawl-progress", progress)
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
			a.emitEvent("knowledge:deep-crawl-progress", progress)
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

// KnowledgeGetImageAssetPaths returns a display-safe thumbnail data URL only
// for an image asset that is currently registered in the local knowledge
// store. Opening the original remains a separate asset-ID-only operation via
// KnowledgeOpenImageAsset, so no host file path crosses the WebView boundary.
func (a *App) KnowledgeGetImageAssetPaths(assetID string) map[string]string {
	result := map[string]string{}
	if !knowledge.IsSafeImageAssetID(assetID) {
		return result
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return result
	}
	defer store.Close()
	if _, err := store.FindImageAssetSource(a.knowledgeContext(), assetID); err != nil {
		return result
	}
	assets := store.ImageAssets()
	if assets == nil {
		return result
	}
	// Keep the WebView boundary on the same managed-asset reader as agent
	// markers and HTTP endpoints; never rebuild a thumbnail path from an ID.
	if thumbData, err := knowledge.ReadKnowledgeImageThumbnail(assets.BaseDir(), assetID); err == nil {
		result["thumb_data_url"] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbData)
	}
	return result
}

// KnowledgeOpenImageFile is retained only as a Wails bridge compatibility
// shim. Presentation callers must use KnowledgeOpenImageAsset: accepting a
// host path from a WebView or model response would weaken the opaque-ID asset
// boundary, even when that path appears to be below knowledge_assets.
func (a *App) KnowledgeOpenImageFile(path string) error {
	_ = path
	return fmt.Errorf("opening knowledge images by path is not supported; use an image asset ID")
}

// KnowledgeOpenImageAsset opens an imported image by its opaque asset ID.
// Unlike KnowledgeOpenImageFile, this is safe to call from agent-rendered
// content because callers never supply a filesystem path.
func (a *App) KnowledgeOpenImageAsset(assetID string) error {
	if !knowledge.IsSafeImageAssetID(assetID) {
		return fmt.Errorf("invalid image asset ID")
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.FindImageAssetSource(a.knowledgeContext(), assetID); err != nil {
		return fmt.Errorf("image asset not found")
	}
	assets := store.ImageAssets()
	if assets == nil {
		return fmt.Errorf("image assets not configured")
	}
	path, err := knowledgeImageAssetOriginalPathFromManager(assets, assetID)
	if err != nil {
		return err
	}
	return a.OpenFileOrShowInFolder(path)
}

// knowledgeImageAssetOriginalPath resolves an imported image's original file
// from its opaque asset ID. It is deliberately path-free at the caller
// boundary, so agent-rendered content cannot select an arbitrary local file.
func knowledgeImageAssetOriginalPath(dataDir, assetID string) (string, error) {
	if !knowledge.IsSafeImageAssetID(assetID) {
		return "", fmt.Errorf("invalid image asset ID")
	}
	assets, err := knowledge.NewImageAssetManager(dataDir)
	if err != nil {
		return "", err
	}
	return knowledgeImageAssetOriginalPathFromManager(assets, assetID)
}

func knowledgeImageAssetOriginalPathFromManager(assets *knowledge.ImageAssetManager, assetID string) (string, error) {
	if assets == nil {
		return "", fmt.Errorf("image assets not configured")
	}
	path, err := assets.OriginalImagePath(assetID)
	if err != nil {
		return "", fmt.Errorf("image original not found")
	}
	return path, nil
}
