package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostKnowledgeIngester struct {
	text      string
	url       string
	path      string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostKnowledgeIngester) IngestReviewedHostKnowledge(_ context.Context, principal Principal, text, url, path string) (string, error) {
	f.principal = principal
	f.text = text
	f.url = url
	f.path = path
	return f.result, f.err
}

type recordingKnowledgeStore struct {
	noOpKnowledgeStore
	text        knowledge.TextSaveRequest
	url         knowledge.URLSaveRequest
	importReq   knowledge.DirectoryImportRequest
	importFiles []string
	importKind  string
}

func (r *recordingKnowledgeStore) SaveText(_ context.Context, req knowledge.TextSaveRequest) (knowledge.Source, error) {
	r.text = req
	return knowledge.Source{ID: "src-text", Title: "note"}, nil
}

func (r *recordingKnowledgeStore) SaveURL(_ context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error) {
	r.url = req
	return knowledge.Source{ID: "src-url", Title: "page"}, nil
}

func (r *recordingKnowledgeStore) ImportDirectory(_ context.Context, req knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	r.importKind = "dir"
	r.importReq = req
	return knowledge.DirectoryImportResult{Status: "ok", BatchID: "batch-dir", TotalFiles: 1, ImportedFiles: 1}, nil
}

func (r *recordingKnowledgeStore) ImportFiles(_ context.Context, req knowledge.DirectoryImportRequest, filePaths []string) (knowledge.DirectoryImportResult, error) {
	r.importKind = "files"
	r.importReq = req
	r.importFiles = append([]string(nil), filePaths...)
	return knowledge.DirectoryImportResult{Status: "ok", BatchID: "batch-file", TotalFiles: 1, ImportedFiles: 1}, nil
}

func TestReviewedHostKnowledgeWriteExecutesWithoutCoordinatorAndRejectsReadMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	ingester := &fakeHostKnowledgeIngester{result: "Text saved to knowledge base. Source ID: src-text"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{KnowledgeWrite: ingester})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ingest", Capability: CapabilityKnowledgeWrite, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("knowledge write plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityKnowledgeWrite {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host ingest must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"text\":\"remember this note\"}")
	if !result.Succeeded || result.Result != ingester.result || result.Unknown {
		t.Fatalf("knowledge write result=%#v", result)
	}
	if ingester.text != "remember this note" || ingester.url != "" || ingester.path != "" || ingester.principal.TenantID != principal.TenantID || ingester.principal.UserID != principal.UserID {
		t.Fatalf("ingester=%#v", ingester)
	}
	savedURL := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"url\":\"https://example.com/note\"}")
	if !savedURL.Succeeded || ingester.url != "https://example.com/note" || ingester.text != "" || ingester.path != "" {
		t.Fatalf("url ingest result=%#v ingester=%#v", savedURL, ingester)
	}
	savedPath := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"path\":\"notes.txt\"}")
	if !savedPath.Succeeded || ingester.path != "notes.txt" || ingester.text != "" || ingester.url != "" {
		t.Fatalf("path ingest result=%#v ingester=%#v", savedPath, ingester)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"text\":\"note\",\"url\":\"https://example.com/note\"}")
	if both.Succeeded {
		t.Fatalf("text and url together must fail closed, result=%#v", both)
	}
	textAndPath := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"text\":\"note\",\"path\":\"notes.txt\"}")
	if textAndPath.Succeeded {
		t.Fatalf("text and path together must fail closed, result=%#v", textAndPath)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], "{\"text\":\"note\",\"channel\":\"lansenger\"}")
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}

	readPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-read", TurnID: "turn-read", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "read", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(readPlan.Selections) != 0 {
		t.Fatalf("knowledge_read must not be satisfied by host ingest, plan=%#v err=%v", readPlan, err)
	}
}

func TestReviewedHostKnowledgeWriteIsAbsentWithoutIngester(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Knowledge: &fakeHostKnowledgeSearcher{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ingest", Capability: CapabilityKnowledgeWrite, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("knowledge write without ingester must stay unmet, plan=%#v err=%v", plan, err)
	}
	readPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-read", TurnID: "turn-read", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "read", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(readPlan.Selections) != 1 {
		t.Fatalf("knowledge read must still plan without an ingester, plan=%#v err=%v", readPlan, err)
	}
}

func TestProjectReviewedHostKnowledgeWriteRejectsPathAndChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostKnowledgeWriteProvider(&fakeHostKnowledgeIngester{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityKnowledgeWrite || provider.AdapterName == "knowledge_save_text" || provider.AdapterName == "knowledge_save_url" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["text"]; !ok {
		t.Fatalf("knowledge write schema missing text: %#v", props)
	}
	if _, ok := props["url"]; !ok {
		t.Fatalf("knowledge write schema missing url: %#v", props)
	}
	if _, ok := props["path"]; !ok || len(props) != 3 {
		t.Fatalf("knowledge write schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "file_path", "query", "save_path", "urls", "content", "title", "action"} {
		if _, ok := props[key]; ok {
			t.Fatalf("knowledge write schema leaked %s", key)
		}
	}
}

func TestReviewedHostKnowledgeWriteUsesTrustedPrincipalAndStore(t *testing.T) {
	store := &recordingKnowledgeStore{}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, knowledgeStore: store}
	out, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "remember this note", "", "")
	if err != nil || !strings.Contains(out, "src-text") || strings.Contains(out, "C:") {
		t.Fatalf("text ingest=%q err=%v", out, err)
	}
	if store.text.Text != "remember this note" || store.text.TenantID != principal.TenantID || store.text.OwnerID != principal.UserID {
		t.Fatalf("saved text=%#v", store.text)
	}
	if store.text.ProjectPath != "" {
		t.Fatal("ingest must not bind a project path from the model")
	}
	urlOut, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "", "https://example.com/note", "")
	if err != nil || !strings.Contains(urlOut, "src-url") {
		t.Fatalf("url ingest=%q err=%v", urlOut, err)
	}
	if store.url.URL != "https://example.com/note" || store.url.TenantID != principal.TenantID || store.url.OwnerID != principal.UserID {
		t.Fatalf("saved url=%#v", store.url)
	}
	if _, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "note", "https://example.com/note", ""); err == nil {
		t.Fatal("text and url together must fail closed")
	}
	if _, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "", "", ""); err == nil {
		t.Fatal("empty ingest must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal}
	if _, err := escaped.IngestReviewedHostKnowledge(context.Background(), principal, "note", "", ""); err == nil {
		t.Fatal("missing knowledge store must fail closed")
	}
	dataDirOnly := &coreAgentCallbacks{principal: principal, dataDir: t.TempDir()}
	services := dataDirOnly.reviewedHostOwnedServices()
	if services.KnowledgeWrite != nil {
		t.Fatal("dataDir-only host must not attach knowledge ingest")
	}
}

func TestReviewedHostKnowledgeWriteImportsWorkspacePathByFileType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello knowledge"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &recordingKnowledgeStore{}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, knowledgeStore: store, workspace: dir}
	fileOut, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "", "", "notes.txt")
	if err != nil || !strings.Contains(fileOut, "notes.txt") || strings.Contains(fileOut, dir) {
		t.Fatalf("file import=%q err=%v", fileOut, err)
	}
	if store.importKind != "files" || len(store.importFiles) != 1 || store.importReq.TenantID != principal.TenantID || store.importReq.OwnerID != principal.UserID {
		t.Fatalf("file import store=%#v", store)
	}
	if store.importReq.ProjectPath != "" {
		t.Fatal("path ingest must not bind a project path from the model")
	}
	dirOut, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "", "", "docs")
	if err != nil || !strings.Contains(dirOut, "docs") || store.importKind != "dir" {
		t.Fatalf("dir import=%q store=%#v err=%v", dirOut, store, err)
	}
	if _, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "", "", filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("workspace escape must fail closed")
	}
	noWorkspace := &coreAgentCallbacks{principal: principal, knowledgeStore: store}
	if _, err := noWorkspace.IngestReviewedHostKnowledge(context.Background(), principal, "", "", "notes.txt"); err == nil {
		t.Fatal("empty workspace must not import a path")
	}
	if _, err := cb.IngestReviewedHostKnowledge(context.Background(), principal, "note", "", "notes.txt"); err == nil {
		t.Fatal("text and path together must fail closed")
	}
}
