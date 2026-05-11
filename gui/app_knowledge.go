package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type KnowledgeImportJob struct {
	ID        string                          `json:"id"`
	Status    string                          `json:"status"`
	Error     string                          `json:"error,omitempty"`
	Result    knowledge.DirectoryImportResult `json:"result"`
	CreatedAt time.Time                       `json:"created_at"`
	UpdatedAt time.Time                       `json:"updated_at"`
}

var knowledgeImportJobs sync.Map

func (a *App) knowledgeDBPath() string {
	return filepath.Join(a.GetDataDir(), "knowledge.db")
}

// KnowledgeClearAll removes all knowledge base content by deleting the database file.
// The database will be recreated automatically on next access.
func (a *App) KnowledgeClearAll() error {
	// Close the cached auto-recall store before deleting the DB
	CloseAutoRecallStore()
	dbPath := a.knowledgeDBPath()
	// Remove main DB file and any SQLite auxiliary files (-wal, -shm, -journal)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		p := dbPath + suffix
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}
	// Reset the source count cache so hasKnowledgeSources returns false immediately
	atomic.StoreInt64(&knowledgeSourceCountCache, 0)
	atomic.StoreInt64(&knowledgeSourceCountTime, time.Now().Unix())
	log.Printf("[knowledge] ClearAll: database removed at %s", dbPath)
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

func (a *App) KnowledgeListSources(opts knowledge.ListSourcesOptions) ([]knowledge.Source, error) {
	store, err := a.openKnowledgeStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	opts = a.normalizeKnowledgeListOptions(opts)
	return store.ListSources(a.knowledgeContext(), opts)
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
	opts.SourceIDs = normalizeKnowledgeOptionStrings(opts.SourceIDs)
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
	opts.SourceIDs = normalizeKnowledgeOptionStrings(opts.SourceIDs)
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
