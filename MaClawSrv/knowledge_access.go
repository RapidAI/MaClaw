package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type knowledgeScope struct {
	TenantID string `json:"tenant_id"`
	OwnerID  string `json:"owner_id"`
	Name     string `json:"name,omitempty"`
}

type knowledgeAccessConfig struct {
	Enabled    bool             `json:"enabled"`
	ReadScopes []knowledgeScope `json:"read_scopes"`
}

type knowledgeCrossTenantConfig struct {
	Enabled bool `json:"enabled"`
}

type knowledgeAccessResolved struct {
	TenantID           string           `json:"tenant_id"`
	UserID             string           `json:"user_id"`
	CrossTenantEnabled bool             `json:"cross_tenant_enabled"`
	Scopes             []knowledgeScope `json:"scopes"`
}

type knowledgeAccessService struct {
	kv         *fileKVStore
	mu         sync.RWMutex
	cache      map[string]*knowledgeAccessConfig
	crossReady bool
	cross      knowledgeCrossTenantConfig
	publicMu   sync.Mutex
}

const (
	knowledgeAccessKeyPrefix      = "knowledge_access_user_"
	knowledgeAccessKeyCrossTenant = "knowledge_access_cross_tenant"
)

func newKnowledgeAccessService(kv *fileKVStore) *knowledgeAccessService {
	return &knowledgeAccessService{kv: kv, cache: make(map[string]*knowledgeAccessConfig)}
}

func (s *knowledgeAccessService) GetCrossTenant(ctx context.Context) (knowledgeCrossTenantConfig, error) {
	s.mu.RLock()
	if s.crossReady {
		cfg := s.cross
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	raw, err := s.kv.Get(ctx, knowledgeAccessKeyCrossTenant)
	if err != nil || raw == "" {
		return knowledgeCrossTenantConfig{}, err
	}
	var cfg knowledgeCrossTenantConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return knowledgeCrossTenantConfig{}, fmt.Errorf("parse knowledge cross-tenant config: %w", err)
	}
	s.mu.Lock()
	s.cross = cfg
	s.crossReady = true
	s.mu.Unlock()
	return cfg, nil
}

func (s *knowledgeAccessService) SetCrossTenant(ctx context.Context, cfg knowledgeCrossTenantConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.kv.Set(ctx, knowledgeAccessKeyCrossTenant, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.cross = cfg
	s.crossReady = true
	s.mu.Unlock()
	return nil
}

func (s *knowledgeAccessService) crossTenantEnabled(ctx context.Context) bool {
	cfg, err := s.GetCrossTenant(ctx)
	return err == nil && cfg.Enabled
}

func (s *knowledgeAccessService) GetUser(ctx context.Context, tenantID, userID string) (*knowledgeAccessConfig, error) {
	key, err := knowledgeAccessKey(tenantID, userID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	if cfg, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return cloneKnowledgeAccessConfig(cfg), nil
	}
	s.mu.RUnlock()
	raw, err := s.kv.Get(ctx, key)
	if err != nil || raw == "" {
		return nil, err
	}
	var cfg knowledgeAccessConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse knowledge access config %q: %w", key, err)
	}
	if err := normalizeKnowledgeAccessConfig(tenantID, &cfg); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cloneKnowledgeAccessConfig(&cfg)
	s.mu.Unlock()
	return &cfg, nil
}

func (s *knowledgeAccessService) SetUser(ctx context.Context, tenantID, userID string, cfg *knowledgeAccessConfig) error {
	key, err := knowledgeAccessKey(tenantID, userID)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &knowledgeAccessConfig{}
	}
	if err := normalizeKnowledgeAccessConfig(tenantID, cfg); err != nil {
		return err
	}
	for i, scope := range cfg.ReadScopes {
		if !isPublicKnowledgeScope(scope) {
			continue
		}
		registered, err := s.publicKnowledgeScopeRegistered(ctx, scope)
		if err != nil {
			return err
		}
		if !registered {
			return fmt.Errorf("read_scopes[%d] targets unknown public knowledge library", i)
		}
	}
	if cfg.Enabled && !s.crossTenantEnabled(ctx) {
		for i, scope := range cfg.ReadScopes {
			if scope.TenantID != strings.TrimSpace(tenantID) && !isPublicKnowledgeScope(scope) {
				return fmt.Errorf("read_scopes[%d] targets tenant %q; enable cross-tenant knowledge access first", i, scope.TenantID)
			}
		}
	}
	return s.saveUserConfig(ctx, key, cfg)
}

func (s *knowledgeAccessService) saveUserConfig(ctx context.Context, key string, cfg *knowledgeAccessConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.kv.Set(ctx, key, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = cloneKnowledgeAccessConfig(cfg)
	s.mu.Unlock()
	return nil
}

func (s *knowledgeAccessService) DeleteUser(ctx context.Context, tenantID, userID string) error {
	key, err := knowledgeAccessKey(tenantID, userID)
	if err != nil {
		return err
	}
	if err := s.kv.Set(ctx, key, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
	return nil
}

func (s *knowledgeAccessService) ResolveForUser(ctx context.Context, tenantID, userID string) []knowledgeScope {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return nil
	}
	self := knowledgeScope{TenantID: tenantID, OwnerID: userID, Name: "self"}
	scopes := []knowledgeScope{self}
	if cfg, err := s.GetUser(ctx, tenantID, userID); err == nil && cfg != nil && cfg.Enabled {
		scopes = append(scopes, cfg.ReadScopes...)
	}
	if !s.crossTenantEnabled(ctx) {
		scopes = filterKnowledgeScopesByTenant(scopes, tenantID)
	}
	scopes = s.filterRegisteredPublicKnowledgeScopes(ctx, scopes)
	return uniqueKnowledgeScopes(scopes)
}

func (s *knowledgeAccessService) ResolveResponse(ctx context.Context, tenantID, userID string) knowledgeAccessResolved {
	return knowledgeAccessResolved{
		TenantID:           strings.TrimSpace(tenantID),
		UserID:             strings.TrimSpace(userID),
		CrossTenantEnabled: s.crossTenantEnabled(ctx),
		Scopes:             s.ResolveForUser(ctx, tenantID, userID),
	}
}

func knowledgeAccessKey(tenantID, userID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return "", fmt.Errorf("tenant_id and user_id are required")
	}
	return knowledgeAccessKeyPrefix + tenantID + "/" + userID, nil
}

func normalizeKnowledgeAccessConfig(defaultTenantID string, cfg *knowledgeAccessConfig) error {
	defaultTenantID = strings.TrimSpace(defaultTenantID)
	for i := range cfg.ReadScopes {
		cfg.ReadScopes[i].TenantID = strings.TrimSpace(cfg.ReadScopes[i].TenantID)
		cfg.ReadScopes[i].OwnerID = strings.TrimSpace(cfg.ReadScopes[i].OwnerID)
		cfg.ReadScopes[i].Name = strings.TrimSpace(cfg.ReadScopes[i].Name)
		if cfg.ReadScopes[i].TenantID == "" {
			cfg.ReadScopes[i].TenantID = defaultTenantID
		}
		if cfg.ReadScopes[i].TenantID == "" {
			return fmt.Errorf("read_scopes[%d].tenant_id is required", i)
		}
		if cfg.ReadScopes[i].OwnerID == "" {
			return fmt.Errorf("read_scopes[%d].owner_id is required", i)
		}
	}
	cfg.ReadScopes = uniqueKnowledgeScopes(cfg.ReadScopes)
	return nil
}

func filterKnowledgeScopesByTenant(scopes []knowledgeScope, tenantID string) []knowledgeScope {
	tenantID = strings.TrimSpace(tenantID)
	filtered := make([]knowledgeScope, 0, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(scope.TenantID) == tenantID || isPublicKnowledgeScope(scope) {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func isPublicKnowledgeScope(scope knowledgeScope) bool {
	return strings.HasPrefix(strings.TrimSpace(scope.OwnerID), publicKnowledgeOwnerPrefix)
}

func uniqueKnowledgeScopes(scopes []knowledgeScope) []knowledgeScope {
	seen := make(map[string]struct{}, len(scopes))
	result := make([]knowledgeScope, 0, len(scopes))
	for _, scope := range scopes {
		scope.TenantID = strings.TrimSpace(scope.TenantID)
		scope.OwnerID = strings.TrimSpace(scope.OwnerID)
		scope.Name = strings.TrimSpace(scope.Name)
		if scope.TenantID == "" || scope.OwnerID == "" {
			continue
		}
		key := knowledgeScopeKey(scope.TenantID, scope.OwnerID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, scope)
	}
	return result
}

func knowledgeScopeKey(tenantID, ownerID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(ownerID)
}

func cloneKnowledgeAccessConfig(cfg *knowledgeAccessConfig) *knowledgeAccessConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.ReadScopes = append([]knowledgeScope(nil), cfg.ReadScopes...)
	return &clone
}

func knowledgeAccessConfigHasCrossTenantScope(tenantID string, cfg *knowledgeAccessConfig) bool {
	tenantID = strings.TrimSpace(tenantID)
	if cfg == nil {
		return false
	}
	for _, scope := range cfg.ReadScopes {
		if strings.TrimSpace(scope.TenantID) != "" && strings.TrimSpace(scope.TenantID) != tenantID {
			return true
		}
	}
	return false
}

type multiKnowledgeStore struct {
	store  *knowledge.SQLiteStore
	access *knowledgeAccessService
}

func newMultiKnowledgeStore(store *knowledge.SQLiteStore, access *knowledgeAccessService) *multiKnowledgeStore {
	return &multiKnowledgeStore{store: store, access: access}
}

func (s *multiKnowledgeStore) resolveScopes(ctx context.Context, tenantID, ownerID string) []knowledgeScope {
	tenantID = strings.TrimSpace(tenantID)
	ownerID = strings.TrimSpace(ownerID)
	if tenantID == "" || ownerID == "" {
		return nil
	}
	if s.access == nil {
		return []knowledgeScope{{TenantID: tenantID, OwnerID: ownerID, Name: "self"}}
	}
	return s.access.ResolveForUser(ctx, tenantID, ownerID)
}

func (s *multiKnowledgeStore) Search(ctx context.Context, opts knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	requestTenantID := strings.TrimSpace(opts.TenantID)
	requestOwnerID := strings.TrimSpace(opts.OwnerID)
	scopes := s.resolveScopes(ctx, requestTenantID, requestOwnerID)
	merged := make([]knowledge.SearchResult, 0, limit)
	seen := make(map[string]int)
	for _, scope := range scopes {
		queryOpts := opts
		queryOpts.TenantID = scope.TenantID
		queryOpts.OwnerID = scope.OwnerID
		queryOpts.Limit = limit
		if scope.TenantID != requestTenantID || scope.OwnerID != requestOwnerID {
			queryOpts.IncludeDisabled = false
		}
		results, err := s.store.Search(ctx, queryOpts)
		if err != nil {
			return nil, err
		}
		merged, seen = mergeKnowledgeSearchResults(merged, seen, results)
	}
	sortKnowledgeSearchResults(merged)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (s *multiKnowledgeStore) SearchStructured(ctx context.Context, opts knowledge.StructuredSearchOptions) ([]knowledge.SearchResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	requestTenantID := strings.TrimSpace(opts.TenantID)
	requestOwnerID := strings.TrimSpace(opts.OwnerID)
	scopes := s.resolveScopes(ctx, requestTenantID, requestOwnerID)
	merged := make([]knowledge.SearchResult, 0, limit)
	seen := make(map[string]int)
	for _, scope := range scopes {
		queryOpts := opts
		queryOpts.TenantID = scope.TenantID
		queryOpts.OwnerID = scope.OwnerID
		queryOpts.Limit = limit
		if scope.TenantID != requestTenantID || scope.OwnerID != requestOwnerID {
			queryOpts.IncludeDisabled = false
		}
		results, err := s.store.SearchStructured(ctx, queryOpts)
		if err != nil {
			return nil, err
		}
		merged, seen = mergeKnowledgeSearchResults(merged, seen, results)
	}
	sortKnowledgeSearchResults(merged)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (s *multiKnowledgeStore) StructuredCatalog(ctx context.Context, opts knowledge.StructuredCatalogOptions) (knowledge.StructuredCatalogResult, error) {
	if s == nil || s.store == nil {
		return knowledge.StructuredCatalogResult{}, fmt.Errorf("knowledge store is not configured")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	requestTenantID := strings.TrimSpace(opts.TenantID)
	requestOwnerID := strings.TrimSpace(opts.OwnerID)
	scopes := s.resolveScopes(ctx, requestTenantID, requestOwnerID)
	merged := knowledge.StructuredCatalogResult{Tables: make([]knowledge.StructuredTableCatalog, 0, limit)}
	seen := make(map[string]struct{})
	for _, scope := range scopes {
		queryOpts := opts
		queryOpts.TenantID = scope.TenantID
		queryOpts.OwnerID = scope.OwnerID
		queryOpts.Limit = limit
		if scope.TenantID != requestTenantID || scope.OwnerID != requestOwnerID {
			queryOpts.IncludeDisabled = false
		}
		result, err := s.store.StructuredCatalog(ctx, queryOpts)
		if err != nil {
			return knowledge.StructuredCatalogResult{}, err
		}
		for _, table := range result.Tables {
			key := table.ID
			if key == "" {
				key = strings.Join([]string{table.SourceID, table.SheetName, table.TableTitle}, "\x00")
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged.Tables = append(merged.Tables, table)
			if len(merged.Tables) >= limit {
				break
			}
		}
		if len(merged.Tables) >= limit {
			break
		}
	}
	merged.Count = len(merged.Tables)
	return merged, nil
}

func (s *multiKnowledgeStore) ContextPack(ctx context.Context, opts knowledge.ContextPackOptions) (knowledge.ContextPackResult, error) {
	if s == nil || s.store == nil {
		return knowledge.ContextPackResult{}, fmt.Errorf("knowledge store is not configured")
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return knowledge.ContextPackResult{Query: query, Notes: []string{"local_context_pack_no_llm"}}, nil
	}
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 8
	}
	if maxItems > 30 {
		maxItems = 30
	}
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = 6000
	}
	if maxChars > 20000 {
		maxChars = 20000
	}
	searchOpts := opts.SearchOptions
	searchOpts.Query = query
	searchOpts.TenantID = strings.TrimSpace(searchOpts.TenantID)
	searchOpts.OwnerID = strings.TrimSpace(searchOpts.OwnerID)
	if searchOpts.Limit <= 0 || searchOpts.Limit < maxItems {
		searchOpts.Limit = maxItems * 2
	}
	if searchOpts.Limit > 50 {
		searchOpts.Limit = 50
	}
	results, err := s.Search(ctx, searchOpts)
	if err != nil {
		return knowledge.ContextPackResult{}, err
	}
	notes := []string{"local_context_pack_no_llm", "card_fact_node_ranked", "budgeted_context"}
	pack := knowledge.ContextPackResult{Query: query, Items: make([]knowledge.ContextPackItem, 0, maxItems), Citations: make([]knowledge.Citation, 0, maxItems), Notes: notes}
	crossUserContextUsed := false
	seenCitations := make(map[string]struct{})
	for _, result := range results {
		if len(pack.Items) >= maxItems || pack.CharacterCount >= maxChars {
			break
		}
		text := multiContextPackText(result)
		if text == "" {
			continue
		}
		text, truncated := truncateKnowledgeContextText(text, maxChars-pack.CharacterCount)
		if text == "" {
			break
		}
		if truncated && !hasKnowledgeNote(pack.Notes, "truncated_to_budget") {
			pack.Notes = append(pack.Notes, "truncated_to_budget")
		}
		label := fmt.Sprintf("K%d", len(pack.Items)+1)
		if result.Source.TenantID != searchOpts.TenantID || result.Source.OwnerID != searchOpts.OwnerID {
			crossUserContextUsed = true
		}
		pack.Items = append(pack.Items, knowledge.ContextPackItem{Label: label, ResultType: result.ResultType, Title: multiContextPackTitle(result), Text: text, SourceID: result.Source.ID, Citation: result.Citation, Score: result.Score})
		pack.CharacterCount += len([]rune(text))
		citation := multiCitationFromResult(result)
		key := knowledgeContextCitationKey(result)
		if _, ok := seenCitations[key]; !ok {
			seenCitations[key] = struct{}{}
			citation.Label = label
			pack.Citations = append(pack.Citations, citation)
		}
	}
	if crossUserContextUsed && !hasKnowledgeNote(pack.Notes, "cross_user_authorized") {
		pack.Notes = append(pack.Notes, "cross_user_authorized")
	}
	pack.Count = len(pack.Items)
	return pack, nil
}

func (s *multiKnowledgeStore) SaveURL(ctx context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error) {
	return s.store.SaveURL(ctx, req)
}
func (s *multiKnowledgeStore) SaveText(ctx context.Context, req knowledge.TextSaveRequest) (knowledge.Source, error) {
	return s.store.SaveText(ctx, req)
}
func (s *multiKnowledgeStore) Stats(ctx context.Context) (knowledge.Stats, error) {
	return s.store.Stats(ctx)
}
func (s *multiKnowledgeStore) ScanDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	return s.store.ScanDirectory(ctx, req)
}
func (s *multiKnowledgeStore) ScanFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error) {
	return s.store.ScanFiles(ctx, req, filePaths)
}
func (s *multiKnowledgeStore) ImportDirectory(ctx context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	return s.store.ImportDirectory(ctx, req)
}
func (s *multiKnowledgeStore) ImportFiles(ctx context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error) {
	return s.store.ImportFiles(ctx, req, filePaths)
}

// --- Management capabilities (agentservice.KnowledgeStore alignment) ---
//
// Read/list operations merge results across the caller's readable scopes
// (own + shared + public), mirroring Search. Single-source and write
// operations delegate to the underlying store; the agentservice tool handlers
// pre-validate ownership against the caller's principal before invoking them.

func (s *multiKnowledgeStore) ListSources(ctx context.Context, opts knowledge.ListSourcesOptions) ([]knowledge.Source, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	scopes := s.resolveScopes(ctx, opts.TenantID, opts.OwnerID)
	merged := make([]knowledge.Source, 0, limit)
	seen := make(map[string]struct{})
	for _, scope := range scopes {
		queryOpts := opts
		queryOpts.TenantID = scope.TenantID
		queryOpts.OwnerID = scope.OwnerID
		if scope.TenantID != opts.TenantID || scope.OwnerID != opts.OwnerID {
			queryOpts.IncludeDisabled = false
		}
		sources, err := s.store.ListSources(ctx, queryOpts)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if _, ok := seen[source.ID]; ok {
				continue
			}
			seen[source.ID] = struct{}{}
			merged = append(merged, source)
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (s *multiKnowledgeStore) ListSourceLabels(ctx context.Context, opts knowledge.ListSourcesOptions) ([]knowledge.SourceLabelSummary, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	scopes := s.resolveScopes(ctx, opts.TenantID, opts.OwnerID)
	merged := make([]knowledge.SourceLabelSummary, 0, 32)
	seen := make(map[string]int)
	for _, scope := range scopes {
		queryOpts := opts
		queryOpts.TenantID = scope.TenantID
		queryOpts.OwnerID = scope.OwnerID
		if scope.TenantID != opts.TenantID || scope.OwnerID != opts.OwnerID {
			queryOpts.IncludeDisabled = false
		}
		labels, err := s.store.ListSourceLabels(ctx, queryOpts)
		if err != nil {
			return nil, err
		}
		for _, label := range labels {
			key := strings.ToLower(strings.TrimSpace(label.Label))
			if idx, ok := seen[key]; ok {
				merged[idx].Count += label.Count
				continue
			}
			seen[key] = len(merged)
			merged = append(merged, label)
		}
	}
	return merged, nil
}

func (s *multiKnowledgeStore) GetSource(ctx context.Context, id string) (knowledge.Source, error) {
	return s.store.GetSource(ctx, id)
}
func (s *multiKnowledgeStore) UpdateSourceMetadata(ctx context.Context, req knowledge.SourceUpdateRequest) (knowledge.Source, error) {
	return s.store.UpdateSourceMetadata(ctx, req)
}
func (s *multiKnowledgeStore) UpdateSourceLabels(ctx context.Context, req knowledge.SourceLabelUpdateRequest) (knowledge.SourceLabelUpdateResult, error) {
	return s.store.UpdateSourceLabels(ctx, req)
}
func (s *multiKnowledgeStore) EnableSource(ctx context.Context, id string) (knowledge.Source, error) {
	return s.store.EnableSource(ctx, id)
}
func (s *multiKnowledgeStore) DisableSource(ctx context.Context, id string) (knowledge.Source, error) {
	return s.store.DisableSource(ctx, id)
}
func (s *multiKnowledgeStore) DeleteSource(ctx context.Context, id string) error {
	return s.store.DeleteSource(ctx, id)
}
func (s *multiKnowledgeStore) RefreshSource(ctx context.Context, id string) (knowledge.Source, error) {
	return s.store.RefreshSource(ctx, id)
}
func (s *multiKnowledgeStore) PreviewSourceRefresh(ctx context.Context, id string) (knowledge.SourceChangePreview, error) {
	return s.store.PreviewSourceRefresh(ctx, id)
}
func (s *multiKnowledgeStore) ListImportBatches(ctx context.Context, limit int) ([]knowledge.ImportBatch, error) {
	return s.store.ListImportBatches(ctx, limit)
}
func (s *multiKnowledgeStore) GetImportBatch(ctx context.Context, batchID string) (knowledge.ImportBatch, error) {
	return s.store.GetImportBatch(ctx, batchID)
}
func (s *multiKnowledgeStore) ListImportItems(ctx context.Context, batchID string, limit int) ([]knowledge.ImportItem, error) {
	return s.store.ListImportItems(ctx, batchID, limit)
}
func (s *multiKnowledgeStore) RetryImportBatch(ctx context.Context, req knowledge.ImportRetryRequest) (knowledge.DirectoryImportResult, error) {
	return s.store.RetryImportBatch(ctx, req)
}
func (s *multiKnowledgeStore) DeleteImportBatch(ctx context.Context, req knowledge.ImportBatchDeleteRequest) (knowledge.ImportBatchDeleteResult, error) {
	return s.store.DeleteImportBatch(ctx, req)
}

func (s *multiKnowledgeStore) CreateImportBatch(ctx context.Context, batch knowledge.ImportBatch) error {
	return s.store.CreateImportBatch(ctx, batch)
}
func (s *multiKnowledgeStore) UpdateImportBatch(ctx context.Context, batch knowledge.ImportBatch) error {
	return s.store.UpdateImportBatch(ctx, batch)
}
func (s *multiKnowledgeStore) CreateImportItem(ctx context.Context, item knowledge.ImportItem) error {
	return s.store.CreateImportItem(ctx, item)
}

func knowledgeResultKey(r knowledge.SearchResult) string {
	return strings.Join([]string{r.Source.ID, r.ResultType, r.NodeID, r.CardID, r.FactID, r.TableID, r.RowID, r.SheetName, r.RowRange, r.ColRange, r.Citation}, "\x00")
}

// mergeKnowledgeSearchResults keeps the strongest score if an equivalent
// entity is encountered twice. This is defensive for overlapping authorized
// scopes and duplicate route results: first-seen deduplication makes ranking
// depend on scope traversal order.
func mergeKnowledgeSearchResults(merged []knowledge.SearchResult, seen map[string]int, incoming []knowledge.SearchResult) ([]knowledge.SearchResult, map[string]int) {
	for _, result := range incoming {
		key := knowledgeResultKey(result)
		if index, ok := seen[key]; ok {
			if result.Score > merged[index].Score {
				merged[index] = result
			}
			continue
		}
		seen[key] = len(merged)
		merged = append(merged, result)
	}
	return merged, seen
}

// sortKnowledgeSearchResults gives merged authorized scopes a deterministic
// ordering. Stable sorting by score alone inherits scope resolution order for
// ties, which can vary as access rules change even when the result set does not.
func sortKnowledgeSearchResults(results []knowledge.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return knowledgeResultKey(results[i]) < knowledgeResultKey(results[j])
	})
}

func knowledgeContextCitationKey(r knowledge.SearchResult) string {
	return strings.Join([]string{r.Source.ID, r.ResultType, r.NodeID, r.CardID, r.FactID, r.TableID, r.RowID, r.SheetName, r.RowRange, r.ColRange, r.Citation}, "\x00")
}

func multiContextPackTitle(result knowledge.SearchResult) string {
	for _, candidate := range []string{result.CardTitle, result.NodeTitle, result.Source.Title, result.Source.RelativePath, result.Source.CanonicalURI, result.Source.URI} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return result.ResultType
}

func multiContextPackText(result knowledge.SearchResult) string {
	switch result.ResultType {
	case "card":
		return strings.TrimSpace(strings.Join(nonEmptyKnowledgeStrings(result.Claim, result.Summary, result.Snippet), "\n"))
	case "fact":
		fact := strings.TrimSpace(strings.Join(nonEmptyKnowledgeStrings(result.Subject, result.Predicate, result.Object), " "))
		return strings.TrimSpace(strings.Join(nonEmptyKnowledgeStrings(fact, result.Snippet), "\n"))
	default:
		return strings.TrimSpace(strings.Join(nonEmptyKnowledgeStrings(result.Snippet, result.Summary, result.Claim), "\n"))
	}
}

func nonEmptyKnowledgeStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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
		result = append(result, value)
	}
	return result
}

func truncateKnowledgeContextText(text string, maxChars int) (string, bool) {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || text == "" {
		return "", false
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	if maxChars <= 3 {
		return string(runes[:maxChars]), true
	}
	return strings.TrimSpace(string(runes[:maxChars-3])) + "...", true
}

func hasKnowledgeNote(notes []string, note string) bool {
	for _, item := range notes {
		if item == note {
			return true
		}
	}
	return false
}

func multiCitationFromResult(result knowledge.SearchResult) knowledge.Citation {
	return knowledge.Citation{SourceID: result.Source.ID, SourceTitle: result.Source.Title, SourceKind: result.Source.Kind, URI: result.Source.URI, RelativePath: result.Source.RelativePath, ResultType: result.ResultType, NodeID: result.NodeID, CardID: result.CardID, FactID: result.FactID, Page: result.Page, SheetName: result.SheetName, RowRange: result.RowRange, ColRange: result.ColRange, Snippet: result.Snippet, Score: result.Score}
}
