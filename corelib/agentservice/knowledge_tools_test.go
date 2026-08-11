package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// mgmtFakeKnowledgeStore records calls and returns canned data for the
// knowledge management tool handlers.
type mgmtFakeKnowledgeStore struct {
	noOpKnowledgeStore

	lastListOpts    knowledge.ListSourcesOptions
	lastLabelReq    knowledge.SourceLabelUpdateRequest
	deletedIDs      []string
	enabledIDs      []string
	disabledIDs     []string
	refreshedIDs    []string
	previewedIDs    []string
	refreshPolicies []*agent.OfficeReadConfig
	previewPolicies []*agent.OfficeReadConfig
	savedURLReqs    []knowledge.URLSaveRequest
	sources         map[string]knowledge.Source
	batches         []knowledge.ImportBatch
	listSourcesErr  error
}

func (f *mgmtFakeKnowledgeStore) ListSources(_ context.Context, opts knowledge.ListSourcesOptions) ([]knowledge.Source, error) {
	f.lastListOpts = opts
	if f.listSourcesErr != nil {
		return nil, f.listSourcesErr
	}
	if opts.SourceID != "" {
		if src, ok := f.sources[opts.SourceID]; ok {
			// Mirror real store behavior: scope filters apply.
			if src.TenantID == opts.TenantID && src.OwnerID == opts.OwnerID {
				return []knowledge.Source{src}, nil
			}
		}
		return nil, nil
	}
	out := make([]knowledge.Source, 0, len(f.sources))
	for _, src := range f.sources {
		// Mirror real store behavior: scope filters apply.
		if opts.TenantID != "" && src.TenantID != opts.TenantID {
			continue
		}
		if opts.OwnerID != "" && src.OwnerID != opts.OwnerID {
			continue
		}
		out = append(out, src)
	}
	return out, nil
}

func (f *mgmtFakeKnowledgeStore) GetSource(_ context.Context, id string) (knowledge.Source, error) {
	if src, ok := f.sources[id]; ok {
		return src, nil
	}
	return knowledge.Source{}, fmt.Errorf("source %s not found", id)
}

func (f *mgmtFakeKnowledgeStore) UpdateSourceLabels(_ context.Context, req knowledge.SourceLabelUpdateRequest) (knowledge.SourceLabelUpdateResult, error) {
	f.lastLabelReq = req
	return knowledge.SourceLabelUpdateResult{}, nil
}

func (f *mgmtFakeKnowledgeStore) EnableSource(_ context.Context, id string) (knowledge.Source, error) {
	f.enabledIDs = append(f.enabledIDs, id)
	return f.sources[id], nil
}

func (f *mgmtFakeKnowledgeStore) DisableSource(_ context.Context, id string) (knowledge.Source, error) {
	f.disabledIDs = append(f.disabledIDs, id)
	return f.sources[id], nil
}

func (f *mgmtFakeKnowledgeStore) DeleteSource(_ context.Context, id string) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func (f *mgmtFakeKnowledgeStore) RefreshSource(_ context.Context, id string) (knowledge.Source, error) {
	f.refreshedIDs = append(f.refreshedIDs, id)
	return f.sources[id], nil
}

func (f *mgmtFakeKnowledgeStore) PreviewSourceRefresh(_ context.Context, id string) (knowledge.SourceChangePreview, error) {
	f.previewedIDs = append(f.previewedIDs, id)
	return knowledge.SourceChangePreview{SourceID: id, Refreshable: true}, nil
}

func (f *mgmtFakeKnowledgeStore) RefreshSourceWithOfficeReadConfig(_ context.Context, id string, policy *agent.OfficeReadConfig) (knowledge.Source, error) {
	f.refreshedIDs = append(f.refreshedIDs, id)
	f.refreshPolicies = append(f.refreshPolicies, policy)
	return f.sources[id], nil
}

func (f *mgmtFakeKnowledgeStore) PreviewSourceRefreshWithOfficeReadConfig(_ context.Context, id string, policy *agent.OfficeReadConfig) (knowledge.SourceChangePreview, error) {
	f.previewedIDs = append(f.previewedIDs, id)
	f.previewPolicies = append(f.previewPolicies, policy)
	return knowledge.SourceChangePreview{SourceID: id, Refreshable: true}, nil
}

func (f *mgmtFakeKnowledgeStore) SaveURL(_ context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error) {
	f.savedURLReqs = append(f.savedURLReqs, req)
	return knowledge.Source{ID: "src-" + req.URL, URI: req.URL, OwnerID: req.OwnerID, TenantID: req.TenantID}, nil
}

func (f *mgmtFakeKnowledgeStore) ListImportBatches(_ context.Context, _ int) ([]knowledge.ImportBatch, error) {
	return f.batches, nil
}

func newMgmtCallbacks(store *mgmtFakeKnowledgeStore) *coreAgentCallbacks {
	return &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: store,
		principal:      Principal{TenantID: "tenant-a", UserID: "user-a"},
	}
}

func ownSource(id string) knowledge.Source {
	return knowledge.Source{ID: id, TenantID: "tenant-a", OwnerID: "user-a"}
}

func sharedSource(id string) knowledge.Source {
	return knowledge.Source{ID: id, TenantID: "tenant-a", OwnerID: "user-b"}
}

func TestKnowledgeListSourcesInjectsPrincipalScope(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{"s1": ownSource("s1")}}
	cb := newMgmtCallbacks(store)
	out := cb.executeKnowledgeListSources(map[string]interface{}{"query": "docs", "limit": float64(10)})
	if strings.HasPrefix(out, "Error:") {
		t.Fatalf("unexpected error: %s", out)
	}
	if store.lastListOpts.TenantID != "tenant-a" || store.lastListOpts.OwnerID != "user-a" {
		t.Fatalf("scope not injected: %+v", store.lastListOpts)
	}
	if store.lastListOpts.Query != "docs" || store.lastListOpts.Limit != 10 {
		t.Fatalf("filters not passed through: %+v", store.lastListOpts)
	}
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil || payload.Count != 1 {
		t.Fatalf("bad payload %q err=%v", out, err)
	}
}

func TestKnowledgeSourceDetailRejectsInaccessibleSource(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"s1": sharedSource("s1"),
	}}
	// Not visible in the caller's readable list either.
	cb := newMgmtCallbacks(store)
	out := cb.executeKnowledgeSourceDetail(map[string]interface{}{"source_id": "s1"})
	if !strings.HasPrefix(out, "Error:") {
		t.Fatalf("expected access error, got %s", out)
	}
}

func TestKnowledgeWriteOpsRejectSharedScopeSources(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"own":    ownSource("own"),
		"shared": sharedSource("shared"),
	}}
	cb := newMgmtCallbacks(store)

	out := cb.executeKnowledgeDeleteSource(map[string]interface{}{"source_ids": []interface{}{"own", "shared"}})
	if len(store.deletedIDs) != 1 || store.deletedIDs[0] != "own" {
		t.Fatalf("expected only own source deleted, got %v", store.deletedIDs)
	}
	if !strings.Contains(out, "failures") {
		t.Fatalf("expected failures entry for shared source, got %s", out)
	}

	out = cb.executeKnowledgeSetSourceStatus(map[string]interface{}{"source_id": "shared"}, false)
	if len(store.disabledIDs) != 0 {
		t.Fatalf("shared source must not be disabled by another user, got %v", store.disabledIDs)
	}
	if !strings.HasPrefix(out, "Error:") {
		t.Fatalf("all-failed update should return an error outcome, got %s", out)
	}

	out = cb.executeKnowledgeSetSourceStatus(map[string]interface{}{"source_id": "own"}, true)
	if len(store.enabledIDs) != 1 || store.enabledIDs[0] != "own" {
		t.Fatalf("expected own source enabled, got %v", store.enabledIDs)
	}
}

func TestKnowledgeRefreshSourceDryRunDefaultsTrue(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{"s1": ownSource("s1")}}
	cb := newMgmtCallbacks(store)

	out := cb.executeKnowledgeRefreshSource(map[string]interface{}{"source_id": "s1"})
	if len(store.previewedIDs) != 1 || len(store.refreshedIDs) != 0 {
		t.Fatalf("default should preview only, previewed=%v refreshed=%v", store.previewedIDs, store.refreshedIDs)
	}
	if !strings.Contains(out, `"dry_run":true`) {
		t.Fatalf("expected dry_run marker, got %s", out)
	}

	_ = cb.executeKnowledgeRefreshSource(map[string]interface{}{"source_id": "s1", "dry_run": false})
	if len(store.refreshedIDs) != 1 {
		t.Fatalf("dry_run=false should refresh, refreshed=%v", store.refreshedIDs)
	}
}

func TestKnowledgeRefreshSourceUsesRequestScopedOfficeReadPolicy(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{"s1": ownSource("s1")}}
	cb := newMgmtCallbacks(store)
	emitMarkdown := true
	cb.appCfg = corelib.AppConfig{
		OfficeReadEngine:       "officeread",
		OfficeReadFormats:      []string{"docx"},
		OfficeReadEmitMarkdown: &emitMarkdown,
	}

	_ = cb.executeKnowledgeRefreshSource(map[string]interface{}{"source_id": "s1"})
	_ = cb.executeKnowledgeRefreshSource(map[string]interface{}{"source_id": "s1", "dry_run": false})
	if len(store.previewPolicies) != 1 || len(store.refreshPolicies) != 1 {
		t.Fatalf("scoped calls preview=%d refresh=%d", len(store.previewPolicies), len(store.refreshPolicies))
	}
	for _, policy := range []*agent.OfficeReadConfig{store.previewPolicies[0], store.refreshPolicies[0]} {
		if policy == nil || policy.Engine != "officeread" || !strings.EqualFold(policy.Formats[0], "docx") || policy.EmitMarkdown == nil || !*policy.EmitMarkdown {
			t.Fatalf("OfficeRead refresh policy = %#v", policy)
		}
	}
}

func TestKnowledgeUpdateSourceLabelsValidatesExplicitIDs(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"own":    ownSource("own"),
		"shared": sharedSource("shared"),
	}}
	cb := newMgmtCallbacks(store)

	out := cb.executeKnowledgeUpdateSourceLabels(map[string]interface{}{
		"source_ids": []interface{}{"shared"},
		"add_labels": []interface{}{"x"},
	})
	if !strings.HasPrefix(out, "Error:") {
		t.Fatalf("expected ownership error, got %s", out)
	}

	out = cb.executeKnowledgeUpdateSourceLabels(map[string]interface{}{
		"source_ids": []interface{}{"own"},
		"add_labels": []interface{}{"x"},
	})
	if strings.HasPrefix(out, "Error:") {
		t.Fatalf("unexpected error: %s", out)
	}
	if store.lastLabelReq.Filter.TenantID != "tenant-a" || store.lastLabelReq.Filter.OwnerID != "user-a" {
		t.Fatalf("filter scope not injected: %+v", store.lastLabelReq.Filter)
	}
}

func TestKnowledgeListImportBatchesFiltersOtherUsers(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{
		sources: map[string]knowledge.Source{},
		batches: []knowledge.ImportBatch{
			{ID: "b1", TenantID: "tenant-a", OwnerID: "user-a"},
			{ID: "b2", TenantID: "tenant-a", OwnerID: "user-b"},
		},
	}
	cb := newMgmtCallbacks(store)
	out := cb.executeKnowledgeListImportBatches(map[string]interface{}{})
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("bad payload %q: %v", out, err)
	}
	if payload.Count != 1 {
		t.Fatalf("expected only own batch, got %d (%s)", payload.Count, out)
	}
}

func TestKnowledgeSaveURLsBatchAndCap(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{}}
	cb := newMgmtCallbacks(store)

	out := cb.executeKnowledgeSaveURLs(map[string]interface{}{
		"urls": []interface{}{"https://a.example", "https://b.example"},
	})
	if strings.HasPrefix(out, "Error:") {
		t.Fatalf("unexpected error: %s", out)
	}
	if len(store.savedURLReqs) != 2 {
		t.Fatalf("expected 2 saves, got %d", len(store.savedURLReqs))
	}
	if store.savedURLReqs[0].TenantID != "tenant-a" || store.savedURLReqs[0].OwnerID != "user-a" {
		t.Fatalf("scope not injected into save: %+v", store.savedURLReqs[0])
	}

	urls := make([]interface{}, 0, 21)
	for i := 0; i < 21; i++ {
		urls = append(urls, "https://x.example/"+strings.Repeat("a", i+1))
	}
	out = cb.executeKnowledgeSaveURLs(map[string]interface{}{"urls": urls})
	if !strings.HasPrefix(out, "Error:") {
		t.Fatalf("expected cap error, got %s", out)
	}
}

func TestKnowledgeManagementToolsRegistered(t *testing.T) {
	cb := &coreAgentCallbacks{knowledgeStore: noOpKnowledgeStore{}}
	names := map[string]bool{}
	for _, spec := range cb.coreToolSpecs() {
		names[spec.Name] = spec.Enabled
	}
	for _, tool := range []string{
		"knowledge_list_sources", "knowledge_source_detail", "knowledge_stats",
		"knowledge_list_source_labels", "knowledge_update_source_labels",
		"knowledge_update_source_metadata", "knowledge_enable_source",
		"knowledge_disable_source", "knowledge_delete_source",
		"knowledge_refresh_source", "knowledge_list_import_batches",
		"knowledge_list_import_items", "knowledge_retry_import_batch",
		"knowledge_delete_import_batch", "knowledge_save_urls",
		"knowledge_import_hub_share",
	} {
		if !names[tool] {
			t.Errorf("tool %s not registered/enabled", tool)
		}
	}
}

func TestKnowledgeManagementToolsDisabledWithoutStore(t *testing.T) {
	cb := &coreAgentCallbacks{}
	for _, spec := range cb.knowledgeManagementToolSpecs() {
		if spec.Enabled {
			t.Errorf("tool %s should be disabled without knowledge store", spec.Name)
		}
		if spec.DisabledReason == "" {
			t.Errorf("tool %s missing disabled reason", spec.Name)
		}
	}
}

func TestKnowledgeStatsScopedToPrincipal(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"s1": {ID: "s1", TenantID: "tenant-a", OwnerID: "user-a", Kind: "url", Status: "parsed", NodeCount: 3, CardCount: 2},
		"s2": {ID: "s2", TenantID: "tenant-a", OwnerID: "user-b", Kind: "file", Status: "parsed"},
	}}
	cb := newMgmtCallbacks(store)
	out := cb.executeKnowledgeStats(map[string]interface{}{})
	var payload struct {
		Stats knowledge.Stats `json:"stats"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("bad payload %q: %v", out, err)
	}
	// Scoped aggregation: only the caller's own sources count.
	if payload.Stats.Sources != 1 {
		t.Fatalf("expected 1 scoped source, got %d (%s)", payload.Stats.Sources, out)
	}
	if payload.Stats.DocumentNodes != 3 || payload.Stats.Cards != 2 {
		t.Fatalf("bad aggregate counts: %+v", payload.Stats)
	}
	if payload.Stats.SourcesByKind["url"] != 1 {
		t.Fatalf("bad kind breakdown: %+v", payload.Stats.SourcesByKind)
	}
}

func TestKnowledgeManagementToolsDispatch(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"s1": ownSource("s1"),
	}}
	cb := newMgmtCallbacks(store)

	cases := []struct {
		name     string
		argsJSON string
	}{
		{"knowledge_list_sources", `{}`},
		{"knowledge_source_detail", `{"source_id":"s1"}`},
		{"knowledge_stats", `{}`},
		{"knowledge_list_source_labels", `{}`},
		{"knowledge_refresh_source", `{"source_id":"s1"}`},
		{"knowledge_list_import_batches", `{}`},
	}
	for _, tc := range cases {
		result := cb.ExecuteToolStructured(tc.name, tc.argsJSON)
		if result.Outcome == agent.ToolExecutionOutcomeError {
			t.Errorf("%s dispatch failed: %s", tc.name, result.Result)
		}
	}

	// knowledge_import_hub_share must default dry_run=true through dispatch.
	if result := cb.ExecuteToolStructured("knowledge_import_hub_share", `{}`); !strings.HasPrefix(result.Result, "Error:") {
		t.Errorf("hub share without link should error, got %s", result.Result)
	}
}
