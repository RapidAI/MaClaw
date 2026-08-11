package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"golang.org/x/crypto/bcrypt"
)

func TestSanitizeKnowledgeDirectoryImportResultForAPIRedactsPaths(t *testing.T) {
	dataRoot := t.TempDir()
	localDoc := filepath.Join(dataRoot, "imports", "secret-import.md")
	result := knowledge.DirectoryImportResult{
		RootPath:       filepath.Dir(localDoc),
		CurrentFile:    localDoc,
		LastItemPath:   localDoc,
		LastItemReason: "failed token=last-secret path=" + dataRoot,
		Warnings:       []string{"failed token=warning-secret path=" + dataRoot},
		Items: []knowledge.ImportItem{{
			FilePath:     localDoc,
			RelativePath: localDoc,
			ErrorMessage: "failed token=item-secret path=" + dataRoot,
		}},
		FailedItems: []knowledge.ImportFailedItem{{
			FilePath: localDoc,
			Error:    "failed token=failed-item-secret path=" + dataRoot,
		}},
	}

	got := sanitizeKnowledgeDirectoryImportResultForAPI(dataRoot, result)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal import result: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "warning-secret", "item-secret", "last-secret", "failed-item-secret"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("expected import result to redact %q, got %s", leaked, body)
		}
	}
	if got.CurrentFile != filepath.Base(localDoc) || got.Items[0].FilePath != filepath.Base(localDoc) || got.Items[0].RelativePath != filepath.Base(localDoc) {
		t.Fatalf("expected import paths to use basename, got %#v", got)
	}
	if got.LastItemPath != filepath.Base(localDoc) || got.FailedItems[0].FilePath != filepath.Base(localDoc) {
		t.Fatalf("expected last/failed item paths to use basename, got %#v", got)
	}
}

func TestSanitizeKnowledgeSourceForAPIRedactsLocalPathsAndSecrets(t *testing.T) {
	dataRoot := t.TempDir()
	localDoc := filepath.Join(dataRoot, "imports", "secret-doc.md")
	source := knowledge.Source{
		URI:          localDoc,
		CanonicalURI: "https://example.test/doc?api_key=source-secret&trace=ok",
		Title:        localDoc,
		ProjectPath:  filepath.Join(dataRoot, "project", "alpha"),
		RelativePath: localDoc,
		ErrorMessage: "failed token=error-secret path=" + dataRoot,
	}
	got := sanitizeKnowledgeSourceForAPI(dataRoot, source)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "source-secret", "error-secret"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("expected knowledge source to redact %q, got %s", leaked, body)
		}
	}
	if got.URI != filepath.Base(localDoc) || got.Title != filepath.Base(localDoc) || got.RelativePath != filepath.Base(localDoc) {
		t.Fatalf("expected local path fields to use basename, got %#v", got)
	}
	if !strings.Contains(got.CanonicalURI, "trace=ok") {
		t.Fatalf("expected benign URL query to remain, got %#v", got)
	}
}

func TestSanitizeKnowledgeSourceForAPIRedactsFileURI(t *testing.T) {
	dataRoot := t.TempDir()
	localImage := filepath.Join(dataRoot, "imports", "private-diagram.png")
	got := sanitizeKnowledgeSourceForAPI(dataRoot, knowledge.Source{URI: "file:///" + filepath.ToSlash(localImage)})
	if strings.Contains(got.URI, dataRoot) || strings.Contains(got.URI, "file://") {
		t.Fatalf("file URI leaked host path: %#v", got)
	}
	if got.URI != filepath.Base(localImage) {
		t.Fatalf("file URI = %q, want filename %q", got.URI, filepath.Base(localImage))
	}
}

func TestSanitizeKnowledgeSourceForAPIProjectsImageSource(t *testing.T) {
	privatePath := `C:\private\knowledge_assets\private-diagram.png`
	got := sanitizeKnowledgeSourceForAPI(t.TempDir(), knowledge.Source{
		ID:           "private-image",
		Kind:         knowledge.SourceKindImage,
		URI:          privatePath,
		CanonicalURI: "file://" + privatePath,
		Title:        privatePath,
		ProjectPath:  privatePath,
		RelativePath: privatePath,
		ErrorMessage: "import failed at " + privatePath,
		Labels:       []string{"diagram"},
		Status:       knowledge.StatusParsed,
	})
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{privatePath, "file://", "private-diagram.png"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("image source API response leaked %q: %s", leaked, body)
		}
	}
	if got.ID != "private-image" || got.Kind != knowledge.SourceKindImage || len(got.Labels) != 1 || got.Status != knowledge.StatusParsed {
		t.Fatalf("image source projection lost safe identity: %#v", got)
	}
}

func TestSanitizeKnowledgeSearchResultsForAPIRedactsResultFields(t *testing.T) {
	dataRoot := t.TempDir()
	localDoc := filepath.Join(dataRoot, "imports", "secret-search.md")
	results := []knowledge.SearchResult{{
		Source:    knowledge.Source{URI: localDoc, Title: localDoc, RelativePath: localDoc},
		NodeTitle: localDoc,
		CardTitle: localDoc,
		Citation:  localDoc,
		Claim:     "claim token=claim-secret path=" + dataRoot,
		Summary:   "summary token=summary-secret path=" + dataRoot,
		Snippet:   "snippet token=snippet-secret path=" + dataRoot,
	}}

	got := sanitizeKnowledgeSearchResultsForAPI(dataRoot, results)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal search results: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "claim-secret", "summary-secret", "snippet-secret"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("expected search result to redact %q, got %s", leaked, body)
		}
	}
	if got[0].NodeTitle != filepath.Base(localDoc) || got[0].CardTitle != filepath.Base(localDoc) || got[0].Citation != filepath.Base(localDoc) {
		t.Fatalf("expected result path fields to use basename, got %#v", got[0])
	}
}

func TestSanitizeKnowledgeSearchResultsForAPIDropsUnsafeImageAssetID(t *testing.T) {
	for _, assetID := range []string{"../private-image", " image-source", "image-source "} {
		result := knowledge.SearchResult{
			NodeType: knowledge.NodeTypeImage,
			Source:   knowledge.Source{ID: "image-source", Kind: knowledge.SourceKindImage},
			Media:    &knowledge.SearchResultMedia{AssetID: assetID},
		}
		got := sanitizeKnowledgeSearchResultsForAPI(t.TempDir(), []knowledge.SearchResult{result})
		if len(got) != 1 || got[0].Media != nil {
			t.Fatalf("unsafe image asset %q was exposed: %#v", assetID, got)
		}
	}
}

func TestSanitizeKnowledgeSearchResultsForAPIDoesNotGuessOrBorrowImageAssetID(t *testing.T) {
	tests := []struct {
		name   string
		result knowledge.SearchResult
	}{
		{
			name: "document image without recorded asset ID",
			result: knowledge.SearchResult{
				NodeID:   "embedded-node",
				NodeType: knowledge.NodeTypeImage,
				Source:   knowledge.Source{ID: "document-source", Kind: knowledge.SourceKindDOCX},
			},
		},
		{
			name: "foreign recorded asset ID",
			result: knowledge.SearchResult{
				NodeType: knowledge.NodeTypeImage,
				Source:   knowledge.Source{ID: "document-source", Kind: knowledge.SourceKindDOCX},
				Media:    &knowledge.SearchResultMedia{AssetID: "other-source_embedded-node"},
			},
		},
		{
			name: "standalone source cannot borrow embedded asset",
			result: knowledge.SearchResult{
				NodeType: knowledge.NodeTypeImage,
				Source:   knowledge.Source{ID: "image-source", Kind: knowledge.SourceKindImage},
				Media:    &knowledge.SearchResultMedia{AssetID: "other-source_embedded-node"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeKnowledgeSearchResultsForAPI(t.TempDir(), []knowledge.SearchResult{tt.result})
			if len(got) != 1 || got[0].Media != nil {
				t.Fatalf("unexpected media projection: %#v", got)
			}
		})
	}

	standalone := knowledge.SearchResult{
		NodeType: knowledge.NodeTypeImage,
		Source:   knowledge.Source{ID: "image-source", Kind: knowledge.SourceKindImage},
	}
	got := sanitizeKnowledgeSearchResultsForAPI(t.TempDir(), []knowledge.SearchResult{standalone})
	if len(got) != 1 || got[0].Media == nil || got[0].Media.AssetID != "image-source" {
		t.Fatalf("standalone image lost its canonical asset: %#v", got)
	}
}

func TestSanitizeKnowledgeContextPackForAPIRedactsCitations(t *testing.T) {
	dataRoot := t.TempDir()
	localDoc := filepath.Join(dataRoot, "imports", "secret-pack.md")
	result := knowledge.ContextPackResult{
		Items: []knowledge.ContextPackItem{{
			Title:    "from " + localDoc,
			Citation: "see " + localDoc + " token=item-secret",
		}},
		Citations: []knowledge.Citation{{
			Label:        "path " + localDoc,
			SourceTitle:  "source " + localDoc,
			URI:          localDoc,
			RelativePath: localDoc,
			Snippet:      "snippet token=snippet-secret path=" + dataRoot,
		}},
	}

	got := sanitizeKnowledgeContextPackForAPI(dataRoot, result)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal context pack: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "item-secret", "snippet-secret"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("expected context pack to redact %q, got %s", leaked, body)
		}
	}
	if got.Citations[0].URI != filepath.Base(localDoc) || got.Citations[0].RelativePath != filepath.Base(localDoc) {
		t.Fatalf("expected citation paths to use basename, got %#v", got.Citations[0])
	}
}

func TestMultiKnowledgeStoreDefaultsToOwnScopeAndCanReadConfiguredSameTenantScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "alpha private deployment rule", Title: "alpha", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText user-a: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "beta shared payroll rule", Title: "beta", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	multi := newMultiKnowledgeStore(store, access)

	ownOnly, err := multi.Search(ctx, knowledge.SearchOptions{Query: "rule", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("Search ownOnly: %v", err)
	}
	if len(ownOnly) == 0 {
		t.Fatalf("expected own result")
	}
	for _, result := range ownOnly {
		if result.Source.OwnerID != "user-a" {
			t.Fatalf("default search leaked owner %q", result.Source.OwnerID)
		}
	}

	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	withTeam, err := multi.Search(ctx, knowledge.SearchOptions{Query: "payroll", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("Search withTeam: %v", err)
	}
	foundUserB := false
	for _, result := range withTeam {
		if result.Source.OwnerID == "user-b" {
			foundUserB = true
		}
	}
	if !foundUserB {
		t.Fatalf("expected configured user-b knowledge scope in results: %#v", withTeam)
	}
}

func TestMultiKnowledgeStoreSearchStructuredUsesAuthorizedScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	root := t.TempDir()
	ownCSV := filepath.Join(root, "own.csv")
	teamCSV := filepath.Join(root, "team.csv")
	privateCSV := filepath.Join(root, "private.csv")
	if err := os.WriteFile(ownCSV, []byte("姓名,部门\n张三,法务\n"), 0o644); err != nil {
		t.Fatalf("write own csv: %v", err)
	}
	if err := os.WriteFile(teamCSV, []byte("姓名,部门\n李四,财务\n"), 0o644); err != nil {
		t.Fatalf("write team csv: %v", err)
	}
	if err := os.WriteFile(privateCSV, []byte("姓名,部门\n王五,审计\n"), 0o644); err != nil {
		t.Fatalf("write private csv: %v", err)
	}
	importReq := knowledge.DirectoryImportRequest{TenantID: "tenant-a", SaveScope: knowledge.SaveScopePersonal, IncludeExts: []string{".csv"}, MaxFileBytes: 1024}
	importReq.OwnerID = "user-a"
	if _, err := store.ImportFiles(ctx, importReq, []string{ownCSV}); err != nil {
		t.Fatalf("ImportFiles user-a: %v", err)
	}
	importReq.OwnerID = "user-b"
	if _, err := store.ImportFiles(ctx, importReq, []string{teamCSV}); err != nil {
		t.Fatalf("ImportFiles user-b: %v", err)
	}
	importReq.OwnerID = "user-c"
	if _, err := store.ImportFiles(ctx, importReq, []string{privateCSV}); err != nil {
		t.Fatalf("ImportFiles user-c: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	multi := newMultiKnowledgeStore(store, access)

	ownOnly, err := multi.SearchStructured(ctx, knowledge.StructuredSearchOptions{
		TenantID:       "tenant-a",
		OwnerID:        "user-a",
		ColumnContains: map[string]string{"部门": ""},
		Query:          "张三",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured ownOnly: %v", err)
	}
	if !hasKnowledgeResultOwner(ownOnly, "user-a") || hasKnowledgeResultOwner(ownOnly, "user-b") || hasKnowledgeResultOwner(ownOnly, "user-c") {
		t.Fatalf("default structured search scope mismatch: %#v", ownOnly)
	}

	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	withTeam, err := multi.SearchStructured(ctx, knowledge.StructuredSearchOptions{
		TenantID:       "tenant-a",
		OwnerID:        "user-a",
		ColumnEquals:   map[string]string{"部门": "财务"},
		Limit:          10,
		SearchScope:    knowledge.SaveScopePersonal,
		SourceIDs:      nil,
		SourceID:       "",
		NumberRanges:   nil,
		DateRanges:     nil,
		ColumnContains: nil,
	})
	if err != nil {
		t.Fatalf("SearchStructured withTeam: %v", err)
	}
	if !hasKnowledgeResultOwner(withTeam, "user-b") {
		t.Fatalf("expected authorized user-b structured knowledge in results: %#v", withTeam)
	}
	if hasKnowledgeResultOwner(withTeam, "user-c") {
		t.Fatalf("structured search leaked unauthorized owner: %#v", withTeam)
	}
}

func TestMultiKnowledgeStoreStructuredCatalogUsesAuthorizedScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	root := t.TempDir()
	ownCSV := filepath.Join(root, "own-catalog.csv")
	teamCSV := filepath.Join(root, "team-catalog.csv")
	privateCSV := filepath.Join(root, "private-catalog.csv")
	if err := os.WriteFile(ownCSV, []byte("name,department\nAlice,Legal\n"), 0o644); err != nil {
		t.Fatalf("write own csv: %v", err)
	}
	if err := os.WriteFile(teamCSV, []byte("name,budget\nBob,1200\n"), 0o644); err != nil {
		t.Fatalf("write team csv: %v", err)
	}
	if err := os.WriteFile(privateCSV, []byte("name,audit\nCarol,restricted\n"), 0o644); err != nil {
		t.Fatalf("write private csv: %v", err)
	}
	importReq := knowledge.DirectoryImportRequest{TenantID: "tenant-a", SaveScope: knowledge.SaveScopePersonal, IncludeExts: []string{".csv"}, MaxFileBytes: 1024}
	importReq.OwnerID = "user-a"
	if _, err := store.ImportFiles(ctx, importReq, []string{ownCSV}); err != nil {
		t.Fatalf("ImportFiles user-a: %v", err)
	}
	importReq.OwnerID = "user-b"
	if _, err := store.ImportFiles(ctx, importReq, []string{teamCSV}); err != nil {
		t.Fatalf("ImportFiles user-b: %v", err)
	}
	importReq.OwnerID = "user-c"
	if _, err := store.ImportFiles(ctx, importReq, []string{privateCSV}); err != nil {
		t.Fatalf("ImportFiles user-c: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	multi := newMultiKnowledgeStore(store, access)

	ownOnly, err := multi.StructuredCatalog(ctx, knowledge.StructuredCatalogOptions{TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("StructuredCatalog ownOnly: %v", err)
	}
	if !hasStructuredCatalogSourceTitle(ownOnly, "own-catalog") || hasStructuredCatalogSourceTitle(ownOnly, "team-catalog") || hasStructuredCatalogSourceTitle(ownOnly, "private-catalog") {
		t.Fatalf("default structured catalog scope mismatch: %#v", ownOnly)
	}

	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	withTeam, err := multi.StructuredCatalog(ctx, knowledge.StructuredCatalogOptions{TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("StructuredCatalog withTeam: %v", err)
	}
	if !hasStructuredCatalogSourceTitle(withTeam, "own-catalog") || !hasStructuredCatalogSourceTitle(withTeam, "team-catalog") {
		t.Fatalf("expected own and authorized team tables in catalog: %#v", withTeam)
	}
	if hasStructuredCatalogSourceTitle(withTeam, "private-catalog") {
		t.Fatalf("structured catalog leaked unauthorized owner: %#v", withTeam)
	}
}

func TestKnowledgeManagerAgentStoreLazilySearchesAuthorizedScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "agent direct own retrieval", Title: "own", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText user-a: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "agent direct team retrieval", Title: "team", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "agent direct private retrieval", Title: "private", TenantID: "tenant-a", OwnerID: "user-c"}); err != nil {
		t.Fatalf("SaveText user-c: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	km := &knowledgeStoreManager{store: store, access: access}

	results, err := km.AgentStore().Search(ctx, knowledge.SearchOptions{Query: "agent direct", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("AgentStore Search: %v", err)
	}
	if !hasKnowledgeResultOwner(results, "user-a") || !hasKnowledgeResultOwner(results, "user-b") {
		t.Fatalf("expected own and authorized team knowledge in agent search: %#v", results)
	}
	if hasKnowledgeResultOwner(results, "user-c") {
		t.Fatalf("agent search leaked unauthorized owner: %#v", results)
	}
}

func TestKnowledgeManagerAgentStoreWithoutAccessFallsBackToSelfScope(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "self fallback marker", Title: "self", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText user-a: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "self fallback marker", Title: "other", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}

	km := &knowledgeStoreManager{store: store}
	results, err := km.AgentStore().Search(ctx, knowledge.SearchOptions{Query: "self fallback", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("AgentStore Search without access: %v", err)
	}
	if !hasKnowledgeResultOwner(results, "user-a") {
		t.Fatalf("expected self knowledge result without access service: %#v", results)
	}
	if hasKnowledgeResultOwner(results, "user-b") {
		t.Fatalf("fallback search leaked other owner without access service: %#v", results)
	}
}

func TestKnowledgeManagerAgentStoreRefreshesStaleAccess(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "stale access team marker", Title: "team", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}

	km := &knowledgeStoreManager{store: store, access: access, agent: newMultiKnowledgeStore(store, nil)}
	results, err := km.AgentStore().Search(ctx, knowledge.SearchOptions{Query: "stale access", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("AgentStore Search: %v", err)
	}
	if !hasKnowledgeResultOwner(results, "user-b") {
		t.Fatalf("expected refreshed agent store to use access scopes: %#v", results)
	}
}

func TestKnowledgeManagerAgentStoreRefreshesReplacedAccess(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "replacement access marker", Title: "team", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}

	oldAccess := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "old_access.json")))
	newAccess := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "new_access.json")))
	if err := newAccess.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	km := &knowledgeStoreManager{store: store, access: newAccess, agent: newMultiKnowledgeStore(store, oldAccess)}

	results, err := km.AgentStore().Search(ctx, knowledge.SearchOptions{Query: "replacement access", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("AgentStore Search: %v", err)
	}
	if !hasKnowledgeResultOwner(results, "user-b") {
		t.Fatalf("expected replaced agent store to use current access service: %#v", results)
	}
}

func TestListReadableKnowledgeSourcesWithoutAccessFallsBackToSelfScope(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "list self marker", Title: "self", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText user-a: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "list other marker", Title: "other", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}

	server := &HTTPServer{knowledgeMgr: &knowledgeStoreManager{store: store}}
	sources, err := server.listReadableKnowledgeSources(ctx, agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"})
	if err != nil {
		t.Fatalf("listReadableKnowledgeSources: %v", err)
	}
	if len(sources) != 1 || sources[0].OwnerID != "user-a" {
		t.Fatalf("expected only self source without access service, got %#v", sources)
	}
}

func TestMultiKnowledgeStoreSearchesOwnSharedAndSelectedPublicKnowledge(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-public", "Shared Handbook")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}

	for _, doc := range []knowledge.TextSaveRequest{
		{Text: "unified access own runbook", Title: "own", TenantID: "tenant-a", OwnerID: "user-a"},
		{Text: "unified access team runbook", Title: "team", TenantID: "tenant-a", OwnerID: "user-b"},
		{Text: "unified access public handbook", Title: "public", TenantID: library.TenantID, OwnerID: library.OwnerID},
	} {
		if _, err := store.SaveText(ctx, doc); err != nil {
			t.Fatalf("SaveText %s/%s: %v", doc.TenantID, doc.OwnerID, err)
		}
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{
		{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"},
		{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name},
	}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}

	results, err := newMultiKnowledgeStore(store, access).Search(ctx, knowledge.SearchOptions{Query: "unified access", TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, ownerID := range []string{"user-a", "user-b", library.OwnerID} {
		if !hasKnowledgeResultOwner(results, ownerID) {
			t.Fatalf("expected owner %s in unified search results: %#v", ownerID, results)
		}
	}
	if hasKnowledgeResultOwner(results, "user-c") {
		t.Fatalf("unexpected unconfigured owner in search results: %#v", results)
	}
}

func TestMultiKnowledgeStoreDoesNotIncludeDisabledSharedScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	own, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "disabled search marker", Title: "own disabled", TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("SaveText own: %v", err)
	}
	team, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "disabled search marker", Title: "team disabled", TenantID: "tenant-a", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("SaveText team: %v", err)
	}
	if _, err := store.DisableSource(ctx, own.ID); err != nil {
		t.Fatalf("DisableSource own: %v", err)
	}
	if _, err := store.DisableSource(ctx, team.ID); err != nil {
		t.Fatalf("DisableSource team: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	multi := newMultiKnowledgeStore(store, access)

	results, err := multi.Search(ctx, knowledge.SearchOptions{Query: "disabled search marker", TenantID: "tenant-a", OwnerID: "user-a", IncludeDisabled: true, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !hasKnowledgeResultOwner(results, "user-a") {
		t.Fatalf("expected own disabled source to be searchable with IncludeDisabled, got %#v", results)
	}
	if hasKnowledgeResultOwner(results, "user-b") {
		t.Fatalf("disabled shared scope should not be searchable even when requester includes disabled: %#v", results)
	}
}

func TestMultiKnowledgeStoreListSourcesDoesNotIncludeDisabledSharedScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	own, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "disabled list marker", Title: "own disabled", TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("SaveText own: %v", err)
	}
	team, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "disabled list marker", Title: "team disabled", TenantID: "tenant-a", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("SaveText team: %v", err)
	}
	if _, err := store.DisableSource(ctx, own.ID); err != nil {
		t.Fatalf("DisableSource own: %v", err)
	}
	if _, err := store.DisableSource(ctx, team.ID); err != nil {
		t.Fatalf("DisableSource team: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	multi := newMultiKnowledgeStore(store, access)

	sources, err := multi.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a", IncludeDisabled: true, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	ownerSeen := map[string]bool{}
	for _, source := range sources {
		ownerSeen[source.OwnerID] = true
	}
	if !ownerSeen["user-a"] {
		t.Fatalf("expected own disabled source to be listed with IncludeDisabled, got %#v", sources)
	}
	if ownerSeen["user-b"] {
		t.Fatalf("disabled shared scope should not be listed even when requester includes disabled: %#v", sources)
	}
}

func TestKnowledgeAccessCrossTenantRequiresAdminEnable(t *testing.T) {
	ctx := context.Background()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	crossTenantConfig := &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-b", OwnerID: "user-b", Name: "external"}}}

	err := access.SetUser(ctx, "tenant-a", "user-a", crossTenantConfig)
	if err == nil || !strings.Contains(err.Error(), "enable cross-tenant") {
		t.Fatalf("expected cross-tenant config to be rejected while disabled, got %v", err)
	}

	if err := access.SetCrossTenant(ctx, knowledgeCrossTenantConfig{Enabled: true}); err != nil {
		t.Fatalf("SetCrossTenant: %v", err)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", crossTenantConfig); err != nil {
		t.Fatalf("SetUser with cross tenant enabled: %v", err)
	}
	resolved := access.ResolveForUser(ctx, "tenant-a", "user-a")
	if !hasKnowledgeScope(resolved, "tenant-b", "user-b") {
		t.Fatalf("expected cross-tenant scope after enable: %#v", resolved)
	}

	if err := access.SetCrossTenant(ctx, knowledgeCrossTenantConfig{Enabled: false}); err != nil {
		t.Fatalf("disable cross tenant: %v", err)
	}
	resolved = access.ResolveForUser(ctx, "tenant-a", "user-a")
	if hasKnowledgeScope(resolved, "tenant-b", "user-b") {
		t.Fatalf("cross-tenant scope should be hidden after disable: %#v", resolved)
	}
}

func TestKnowledgeAccessResolvesOwnSharedAndPublicScopesTogether(t *testing.T) {
	ctx := context.Background()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-public", "Shared Handbook")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{
		{TenantID: "tenant-a", OwnerID: "user-b", Name: "teammate"},
		{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name},
	}}); err != nil {
		t.Fatalf("SetUser with same-tenant and public scope: %v", err)
	}
	resolved := access.ResolveForUser(ctx, "tenant-a", "user-a")
	for _, want := range []knowledgeScope{
		{TenantID: "tenant-a", OwnerID: "user-a"},
		{TenantID: "tenant-a", OwnerID: "user-b"},
		{TenantID: library.TenantID, OwnerID: library.OwnerID},
	} {
		if !hasKnowledgeScope(resolved, want.TenantID, want.OwnerID) {
			t.Fatalf("expected scope %s/%s in resolved access: %#v", want.TenantID, want.OwnerID, resolved)
		}
	}

	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{
		{TenantID: "tenant-b", OwnerID: "user-c", Name: "external"},
		{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name},
	}}); err == nil || !strings.Contains(err.Error(), "enable cross-tenant") {
		t.Fatalf("expected non-public cross-tenant scope to require admin enable, got %v", err)
	}
}

func TestKnowledgeAccessAllowsDisabledCrossTenantDraftWithoutAdminEnable(t *testing.T) {
	ctx := context.Background()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))

	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: false, ReadScopes: []knowledgeScope{{TenantID: "tenant-b", OwnerID: "user-b", Name: "draft"}}}); err != nil {
		t.Fatalf("disabled cross-tenant draft should be storable: %v", err)
	}
	if resolved := access.ResolveForUser(ctx, "tenant-a", "user-a"); hasKnowledgeScope(resolved, "tenant-b", "user-b") {
		t.Fatalf("disabled cross-tenant draft should not resolve while disabled: %#v", resolved)
	}
}

func TestKnowledgeAccessRejectsEmptyOwnerScopes(t *testing.T) {
	ctx := context.Background()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))

	err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a"}}})
	if err == nil || !strings.Contains(err.Error(), "owner_id is required") {
		t.Fatalf("expected empty owner scope to be rejected, got %v", err)
	}
}

func TestKnowledgeAccessResolveRequiresTenantAndUser(t *testing.T) {
	ctx := context.Background()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))

	if scopes := access.ResolveForUser(ctx, "tenant-a", ""); len(scopes) != 0 {
		t.Fatalf("expected empty user to resolve no scopes, got %#v", scopes)
	}
	if scopes := access.ResolveForUser(ctx, "", "user-a"); len(scopes) != 0 {
		t.Fatalf("expected empty tenant to resolve no scopes, got %#v", scopes)
	}
}

func TestMultiKnowledgeStoreContextPackUsesEmbeddedQuery(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "gamma deployment checklist", Title: "gamma", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	multi := newMultiKnowledgeStore(store, access)
	pack, err := multi.ContextPack(ctx, knowledge.ContextPackOptions{SearchOptions: knowledge.SearchOptions{Query: "gamma", TenantID: " tenant-a ", OwnerID: " user-a "}, MaxItems: 5})
	if err != nil {
		t.Fatalf("ContextPack: %v", err)
	}
	if pack.Count == 0 || pack.Query != "gamma" {
		t.Fatalf("expected context pack to use embedded query, got %#v", pack)
	}
	if hasKnowledgeNote(pack.Notes, "cross_user_authorized") {
		t.Fatalf("self-only context pack should not claim cross-user authorization: %#v", pack.Notes)
	}

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "delta team checklist", Title: "delta", TenantID: "tenant-a", OwnerID: "user-b"}); err != nil {
		t.Fatalf("SaveText team: %v", err)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	ownPackWithTeamScope, err := multi.ContextPack(ctx, knowledge.ContextPackOptions{SearchOptions: knowledge.SearchOptions{Query: "gamma", TenantID: "tenant-a", OwnerID: "user-a"}, MaxItems: 5})
	if err != nil {
		t.Fatalf("ContextPack own with team scope: %v", err)
	}
	if hasKnowledgeNote(ownPackWithTeamScope.Notes, "cross_user_authorized") {
		t.Fatalf("own-only results should not claim cross-user authorization just because extra scopes exist: %#v", ownPackWithTeamScope)
	}
	teamPack, err := multi.ContextPack(ctx, knowledge.ContextPackOptions{SearchOptions: knowledge.SearchOptions{Query: "delta", TenantID: "tenant-a", OwnerID: "user-a"}, MaxItems: 5})
	if err != nil {
		t.Fatalf("ContextPack team: %v", err)
	}
	if teamPack.Count == 0 || !hasKnowledgeNote(teamPack.Notes, "cross_user_authorized") {
		t.Fatalf("cross-user context pack should include authorization note, got %#v", teamPack)
	}
}

func TestMultiKnowledgeStoreContextPackDoesNotExposeSharedImagePaths(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	privatePath := `C:\\private\\knowledge_assets\\shared-gateway.png`
	if err := store.SaveSource(ctx, knowledge.Source{
		ID:           "shared-safe-image",
		Kind:         knowledge.SourceKindImage,
		URI:          privatePath,
		CanonicalURI: "file://" + privatePath,
		RelativePath: privatePath,
		Title:        privatePath,
		TenantID:     "tenant-a",
		OwnerID:      "user-b",
		Status:       knowledge.StatusParsed,
	}); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveDocumentNode(ctx, knowledge.DocumentNode{
		ID:       "shared-safe-image-node",
		SourceID: "shared-safe-image",
		Type:     knowledge.NodeTypeImage,
		Title:    privatePath,
		Text:     "gateway architecture image evidence",
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	pack, err := newMultiKnowledgeStore(store, access).ContextPack(ctx, knowledge.ContextPackOptions{
		SearchOptions: knowledge.SearchOptions{Query: "gateway architecture", TenantID: "tenant-a", OwnerID: "user-a", Limit: 5},
		MaxItems:      1,
		MaxChars:      1000,
	})
	if err != nil {
		t.Fatalf("ContextPack: %v", err)
	}
	if len(pack.Items) != 1 || len(pack.Citations) != 1 {
		t.Fatalf("context pack = %#v", pack)
	}
	item, citation := pack.Items[0], pack.Citations[0]
	for _, value := range []string{item.Title, item.Citation, citation.Label, citation.SourceTitle, citation.URI, citation.RelativePath} {
		if strings.Contains(value, privatePath) || strings.Contains(value, "file://") {
			t.Fatalf("shared image context pack leaked host metadata in %q", value)
		}
	}
	if item.Title != "shared-safe-image" || citation.SourceID != "shared-safe-image" {
		t.Fatalf("context pack lost safe image identity: item=%#v citation=%#v", item, citation)
	}
}

func TestKnowledgeResultKeysDistinguishStructuredRows(t *testing.T) {
	base := knowledge.SearchResult{
		Source:     knowledge.Source{ID: "src_csv"},
		ResultType: "table_row",
		TableID:    "table_1",
		SheetName:  "Sheet1",
		Citation:   "employees.csv / Sheet1 / table row",
	}
	rowA := base
	rowA.RowID = "row_1"
	rowA.RowRange = "2:2"
	rowB := base
	rowB.RowID = "row_2"
	rowB.RowRange = "3:3"

	if knowledgeResultKey(rowA) == knowledgeResultKey(rowB) {
		t.Fatalf("structured search result keys should distinguish table rows")
	}
	if knowledgeContextCitationKey(rowA) == knowledgeContextCitationKey(rowB) {
		t.Fatalf("structured context citation keys should distinguish table rows")
	}
}

func TestSortKnowledgeSearchResultsOrdersEqualScoresDeterministically(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		results := []knowledge.SearchResult{
			{ResultType: "table_row", RowID: "row-b", Source: knowledge.Source{ID: "source-b"}, Score: 2},
			{ResultType: "table_row", RowID: "row-a", Source: knowledge.Source{ID: "source-a"}, Score: 2},
		}
		sortKnowledgeSearchResults(results)
		if results[0].RowID != "row-a" || results[1].RowID != "row-b" {
			t.Fatalf("attempt %d equal-score ordering = %#v", attempt, results)
		}
	}
}

func TestMergeKnowledgeSearchResultsKeepsHighestDuplicateScore(t *testing.T) {
	base := knowledge.SearchResult{ResultType: "table_row", RowID: "row-1", Source: knowledge.Source{ID: "source-1"}, Score: 1}
	stronger := base
	stronger.Score = 3
	merged, seen := mergeKnowledgeSearchResults(nil, make(map[string]int), []knowledge.SearchResult{base, stronger})
	if len(merged) != 1 || merged[0].Score != 3 || seen[knowledgeResultKey(base)] != 0 {
		t.Fatalf("merged duplicate result = %#v, seen=%#v", merged, seen)
	}
}

func TestCanAccessSourceRequiresOwnership(t *testing.T) {
	server := &HTTPServer{}
	principal := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	if !server.canAccessSource(knowledge.Source{TenantID: "tenant-a", OwnerID: "user-a"}, principal) {
		t.Fatalf("expected owner to manage own source")
	}
	if !server.canAccessSource(knowledge.Source{TenantID: " tenant-a ", OwnerID: " user-a "}, principal) {
		t.Fatalf("expected ownership check to trim scope fields")
	}
	if server.canAccessSource(knowledge.Source{TenantID: "tenant-a", OwnerID: "user-b"}, principal) {
		t.Fatalf("same-tenant non-owner source should not be manageable")
	}
	if server.canAccessSource(knowledge.Source{TenantID: "tenant-a"}, principal) {
		t.Fatalf("tenant shared source should not be user-manageable")
	}
	if server.canAccessSource(knowledge.Source{}, agentservice.Principal{}) {
		t.Fatalf("empty source and empty principal should not be manageable")
	}
}

func TestListReadableKnowledgeSourcesIncludesConfiguredSameTenantAndManagementDenies(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	own, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "own runbook", Title: "own", TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("SaveText own: %v", err)
	}
	team, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "team runbook", Title: "team", TenantID: "tenant-a", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("SaveText team: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-public", "Shared Handbook")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	publicSource, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "public runbook", Title: "public", TenantID: library.TenantID, OwnerID: library.OwnerID})
	if err != nil {
		t.Fatalf("SaveText public: %v", err)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}, {TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	server := &HTTPServer{knowledgeMgr: &knowledgeStoreManager{store: store, access: access}}
	principal := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}

	sources, err := server.listReadableKnowledgeSources(ctx, principal)
	if err != nil {
		t.Fatalf("listReadableKnowledgeSources: %v", err)
	}
	if !hasKnowledgeSource(sources, own.ID) || !hasKnowledgeSource(sources, team.ID) || !hasKnowledgeSource(sources, publicSource.ID) {
		t.Fatalf("expected own and configured team sources, got %#v", sources)
	}
	if !server.canReadSource(ctx, team, principal) {
		t.Fatalf("expected configured team source to be readable")
	}
	if !server.canReadSource(ctx, publicSource, principal) {
		t.Fatalf("expected selected public source to be readable")
	}
	if server.canAccessSource(team, principal) {
		t.Fatalf("configured team source should not be manageable by reader")
	}
	disabledTeam, err := store.DisableSource(ctx, team.ID)
	if err != nil {
		t.Fatalf("DisableSource team: %v", err)
	}
	if server.canReadSource(ctx, disabledTeam, principal) {
		t.Fatalf("disabled team source should not be readable by configured reader")
	}
	afterDisable, err := server.listReadableKnowledgeSources(ctx, principal)
	if err != nil {
		t.Fatalf("listReadableKnowledgeSources after disable: %v", err)
	}
	if hasKnowledgeSource(afterDisable, team.ID) {
		t.Fatalf("disabled team source should not be listed for configured reader: %#v", afterDisable)
	}
}

func TestListReadableKnowledgeSourcesDoesNotUseDefaultPageLimit(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	for i := 0; i < 105; i++ {
		if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "runbook " + strconv.Itoa(i), Title: "doc " + strconv.Itoa(i), TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
			t.Fatalf("SaveText %d: %v", i, err)
		}
	}
	server := &HTTPServer{knowledgeMgr: &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))}}
	sources, err := server.listReadableKnowledgeSources(ctx, agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"})
	if err != nil {
		t.Fatalf("listReadableKnowledgeSources: %v", err)
	}
	if len(sources) != 105 {
		t.Fatalf("expected all readable sources beyond default ListSources limit, got %d", len(sources))
	}
}

func TestKnowledgeStatsFromSourcesUsesReadableScopeOnly(t *testing.T) {
	sources := []knowledge.Source{
		{Kind: "text", Status: knowledge.StatusDistilled, SiteName: "Docs.EXAMPLE", NodeCount: 2, CardCount: 3, FactCount: 4, Labels: []string{"team", "team", "ops"}},
		{Kind: "", Status: knowledge.StatusParsed},
	}
	stats := knowledgeStatsFromSources(sources)

	if stats.Sources != 2 || stats.DocumentNodes != 2 || stats.Cards != 3 || stats.Facts != 4 {
		t.Fatalf("unexpected aggregate stats: %#v", stats)
	}
	if stats.SourcesByKind["text"] != 1 || stats.SourcesByKind["unknown"] != 1 {
		t.Fatalf("unexpected kind buckets: %#v", stats.SourcesByKind)
	}
	if stats.SourcesByDomain["docs.example"] != 1 {
		t.Fatalf("unexpected domain buckets: %#v", stats.SourcesByDomain)
	}
	if stats.SourcesByLabel["team"] != 1 || stats.SourcesByLabel["ops"] != 1 {
		t.Fatalf("labels should count each source once per label: %#v", stats.SourcesByLabel)
	}
	if stats.SourcesWithoutNodes != 1 || stats.SourcesWithoutCards != 1 || stats.SourcesRebuildCards != 0 {
		t.Fatalf("unexpected rebuild counters: %#v", stats)
	}
}

func TestKnowledgeAccessConfigHasCrossTenantScope(t *testing.T) {
	sameTenant := &knowledgeAccessConfig{ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}
	if knowledgeAccessConfigHasCrossTenantScope("tenant-a", sameTenant) {
		t.Fatalf("same-tenant scope should not be marked cross-tenant")
	}
	crossTenant := &knowledgeAccessConfig{ReadScopes: []knowledgeScope{{TenantID: "tenant-b", OwnerID: "user-b"}}}
	if !knowledgeAccessConfigHasCrossTenantScope("tenant-a", crossTenant) {
		t.Fatalf("cross-tenant scope should be marked cross-tenant")
	}
}

func TestKnowledgeAccessGetMeDefaultsToSelfScope(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-access-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-access-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/access", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge access status = %d body = %s", w.Code, w.Body.String())
	}
	var got knowledgeAccessResolved
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0].TenantID != tenant.ID || got.Scopes[0].OwnerID != user.ID {
		t.Fatalf("expected self scope only, got %#v", got)
	}
}

func TestKnowledgeAccessGetMeWithoutAccessServiceFallsBackToSelfScope(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-access-no-service-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-access-no-service-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{})
	defer server.Close()
	defer func() { _ = svc.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/access", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge access status = %d body = %s", w.Code, w.Body.String())
	}
	var got knowledgeAccessResolvedView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0].ScopeType != "self" || got.Scopes[0].TenantID != tenant.ID || got.Scopes[0].OwnerID != user.ID {
		t.Fatalf("expected self scope view without access service, got %#v", got)
	}
}

func TestKnowledgeAccessGetMeClassifiesReadableKnowledgeOwners(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A", Email: "a@example.test"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B", Email: "b@example.test"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "API", APIKey: "knowledge-access-owner-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-access-owner-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, tenant.ID, "Policies")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	if err := access.SetUser(ctx, tenant.ID, userA.ID, &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{
		{TenantID: tenant.ID, OwnerID: userB.ID, Name: "team"},
		{TenantID: tenant.ID, OwnerID: library.OwnerID, Name: library.Name},
	}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: access})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/access", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge access status = %d body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Scopes []knowledgeAccessScopeView `json:"scopes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	assertScope := func(scopeType, ownerID, ownerName string) {
		t.Helper()
		for _, scope := range got.Scopes {
			if scope.ScopeType == scopeType && scope.OwnerID == ownerID && scope.OwnerName == ownerName && scope.TenantName == tenant.Name {
				return
			}
		}
		t.Fatalf("missing %s scope owner=%s name=%q in %#v", scopeType, ownerID, ownerName, got.Scopes)
	}
	assertScope("self", userA.ID, userA.Name)
	assertScope("user", userB.ID, userB.Name)
	assertScope("public", library.OwnerID, library.Name)
	if strings.Contains(w.Body.String(), "b@example.test") {
		t.Fatalf("knowledge access response should not expose other users' email: %s", w.Body.String())
	}
}

func TestKnowledgeSearchHTTPEnforcesPrincipalOverRequestScope(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "API", APIKey: "knowledge-search-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-search-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	sqlStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqlStore.Close()
	ownSource, err := sqlStore.SaveText(ctx, knowledge.TextSaveRequest{Text: "principal needle own runbook", Title: "own", TenantID: tenant.ID, OwnerID: userA.ID})
	if err != nil {
		t.Fatalf("SaveText user A: %v", err)
	}
	secondOwnSource, err := sqlStore.SaveText(ctx, knowledge.TextSaveRequest{Text: "principal needle own secondary runbook", Title: "own secondary", TenantID: tenant.ID, OwnerID: userA.ID})
	if err != nil {
		t.Fatalf("SaveText second user A: %v", err)
	}
	if _, err := sqlStore.SaveText(ctx, knowledge.TextSaveRequest{Text: "principal needle private runbook", Title: "private", TenantID: tenant.ID, OwnerID: userB.ID}); err != nil {
		t.Fatalf("SaveText user B: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	km := &knowledgeStoreManager{store: sqlStore, access: access, agent: newMultiKnowledgeStore(sqlStore, access)}
	server := NewHTTPServer(svc, "admin-secret", km)

	body := []byte(`{"query":"principal needle","tenant_id":"other-tenant","owner_id":"` + userB.ID + `","limit":10}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/search", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge search status = %d body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Results []knowledge.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if !hasKnowledgeResultOwner(got.Results, userA.ID) {
		t.Fatalf("expected own knowledge result, got %#v", got.Results)
	}
	if hasKnowledgeResultOwner(got.Results, userB.ID) {
		t.Fatalf("search leaked request owner knowledge despite principal enforcement: %#v", got.Results)
	}

	body = []byte(`{"query":"principal needle","source_id":"` + secondOwnSource.ID + `","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/search", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge source_id search status = %d body = %s", w.Code, w.Body.String())
	}
	got.Results = nil
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal source_id response: %v", err)
	}
	if len(got.Results) == 0 {
		t.Fatalf("expected source_id-scoped results")
	}
	for _, result := range got.Results {
		if result.Source.ID != secondOwnSource.ID {
			t.Fatalf("source_id search returned unexpected source %s (own primary %s): %#v", result.Source.ID, ownSource.ID, got.Results)
		}
	}
}

func TestKnowledgeReadHTTPHandlesMissingStore(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-missing-store-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-missing-store-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{})
	defer server.Close()
	defer func() { _ = svc.Close() }()

	for _, tc := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/v1/knowledge/search", body: `{"query":"needle"}`, want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/context-pack", body: `{"query":"needle"}`, want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/export", body: `{"description":"portable"}`, want: http.StatusServiceUnavailable},
		{method: http.MethodGet, path: "/api/v1/knowledge/sources/ksrc_missing", want: http.StatusNotFound},
		{method: http.MethodDelete, path: "/api/v1/knowledge/sources/ksrc_missing", want: http.StatusServiceUnavailable},
		{method: http.MethodPatch, path: "/api/v1/knowledge/sources/ksrc_missing", body: `{}`, want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/sources/ksrc_missing/disable", want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/sources/ksrc_missing/enable", want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/sources/ksrc_missing/refresh", want: http.StatusServiceUnavailable},
		{method: http.MethodGet, path: "/api/v1/knowledge/sources/ksrc_missing/thumbnail", want: http.StatusNotFound},
		{method: http.MethodGet, path: "/api/v1/knowledge/sources/ksrc_missing/image", want: http.StatusNotFound},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s status = %d body = %s, want %d", tc.method, tc.path, w.Code, w.Body.String(), tc.want)
		}
	}
}

func TestKnowledgeImportTextHTTPEnforcesPrincipalOwner(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "API", APIKey: "knowledge-import-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-import-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	sqlStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqlStore.Close()
	km := &knowledgeStoreManager{store: sqlStore, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json"))), agent: newMultiKnowledgeStore(sqlStore, nil)}
	server := NewHTTPServer(svc, "admin-secret", km)

	body := []byte(`{"text":"principal owned import text","title":"owned","tenant_id":"other-tenant","owner_id":"` + userB.ID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/import/text", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("knowledge text import status = %d body = %s", w.Code, w.Body.String())
	}
	own, err := sqlStore.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userA.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources own: %v", err)
	}
	if len(own) != 1 || own[0].OwnerID != userA.ID || own[0].TenantID != tenant.ID {
		t.Fatalf("expected text import to write current principal scope, got %#v", own)
	}
	other, err := sqlStore.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userB.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources other: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("text import wrote request owner scope: %#v", other)
	}
}

func TestKnowledgeImportEndpointsUsePrincipalScope(t *testing.T) {
	data, err := os.ReadFile("http_knowledge.go")
	if err != nil {
		t.Fatalf("ReadFile http_knowledge.go: %v", err)
	}
	body := string(data)
	for _, tc := range []struct {
		name string
		next string
		want []string
	}{
		{name: "func (s *HTTPServer) handleKnowledgeImportFile", next: "func isKnowledgeArchivePath", want: []string{"OwnerID:   p.UserID", "TenantID:  p.TenantID"}},
		{name: "func (s *HTTPServer) handleKnowledgeImportURL", next: "func (s *HTTPServer) handleKnowledgeImportURLs", want: []string{"OwnerID:   p.UserID", "TenantID:  p.TenantID"}},
		{name: "func (s *HTTPServer) handleKnowledgeImportURLs", next: "func (s *HTTPServer) handleKnowledgeImportText", want: []string{"OwnerID:   p.UserID", "TenantID:  p.TenantID", "OwnerID:        p.UserID", "TenantID:       p.TenantID"}},
	} {
		block := knowledgeHandlerBlock(t, body, tc.name, tc.next)
		for _, needle := range tc.want {
			if !strings.Contains(block, needle) {
				t.Fatalf("%s missing principal-scope marker %q", tc.name, needle)
			}
		}
	}
	if strings.Contains(body, `OwnerID:   req.OwnerID`) || strings.Contains(body, `TenantID:  req.TenantID`) {
		t.Fatalf("knowledge import endpoints must not trust request owner_id/tenant_id")
	}
}

func knowledgeHandlerBlock(t *testing.T, body, start, next string) string {
	t.Helper()
	startAt := strings.Index(body, start)
	if startAt < 0 {
		t.Fatalf("missing handler %q", start)
	}
	endAt := len(body)
	if next != "" {
		rel := strings.Index(body[startAt+len(start):], next)
		if rel < 0 {
			t.Fatalf("missing next handler marker %q", next)
		}
		endAt = startAt + len(start) + rel
	}
	return body[startAt:endAt]
}

func TestKnowledgeImageAssetEndpointsEnforceReadAccess(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "API", APIKey: "knowledge-image-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-image-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	sqlStore, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqlStore.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatalf("NewImageAssetManager: %v", err)
	}
	sqlStore.SetImageAssetManager(assets)
	source := knowledge.Source{ID: "private-image-source", Kind: knowledge.SourceKindImage, URI: "file://private-image.png", Title: "private image", TenantID: tenant.ID, OwnerID: userB.ID, Status: knowledge.StatusParsed}
	if err := sqlStore.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource user B: %v", err)
	}
	writeKnowledgeImageAsset(t, assets, source.ID)

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json")))
	km := &knowledgeStoreManager{store: sqlStore, access: access, agent: newMultiKnowledgeStore(sqlStore, access)}
	server := NewHTTPServer(svc, "admin-secret", km)
	defer server.Close()

	for _, path := range []string{
		"/api/v1/knowledge/images/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/images/" + source.ID,
		"/api/v1/knowledge/sources/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/sources/" + source.ID + "/image",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s unauthorized status = %d body = %s", path, w.Code, w.Body.String())
		}
	}

	if err := access.SetUser(ctx, tenant.ID, userA.ID, &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: tenant.ID, OwnerID: userB.ID, Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	for _, path := range []string{
		"/api/v1/knowledge/images/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/images/" + source.ID,
		"/api/v1/knowledge/sources/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/sources/" + source.ID + "/image",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s authorized status = %d body = %s", path, w.Code, w.Body.String())
		}
		if w.Body.Len() == 0 {
			t.Fatalf("%s authorized response was empty", path)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := w.Header().Get("Cache-Control"); got != "private, max-age=86400" {
			t.Fatalf("%s Cache-Control = %q, want private cache", path, got)
		}
		if got, want := w.Header().Get("Content-Length"), strconv.Itoa(w.Body.Len()); got != want {
			t.Fatalf("%s Content-Length = %q, want %q", path, got, want)
		}
	}

	assetPath := "/api/v1/knowledge/images/" + source.ID + "/thumbnail"
	headReq := httptest.NewRequest(http.MethodHead, assetPath, nil)
	headReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	head := httptest.NewRecorder()
	server.Handler().ServeHTTP(head, headReq)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("image HEAD status=%d bodyLen=%d length=%q", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}
	rangeReq := httptest.NewRequest(http.MethodGet, assetPath, nil)
	rangeReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	rangeReq.Header.Set("Range", "bytes=0-0")
	rangeResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(rangeResponse, rangeReq)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.Len() != 1 || rangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("image range status=%d bodyLen=%d contentRange=%q", rangeResponse.Code, rangeResponse.Body.Len(), rangeResponse.Header().Get("Content-Range"))
	}

	// A local cache file is not a trust boundary. The display endpoints must
	// not advertise arbitrary bytes as image/jpeg merely because the filename
	// matches a generated derivative.
	if err := os.WriteFile(assets.ThumbPath(source.ID), []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assets.PreviewPath(source.ID), []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/knowledge/images/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/images/" + source.ID + "/preview",
		"/api/v1/knowledge/sources/" + source.ID + "/thumbnail",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("corrupt derived %s status = %d body = %s", path, w.Code, w.Body.String())
		}
	}

	// The asset cache is local mutable state, not a trust boundary. Neither the
	// canonical asset routes nor the legacy source-ID aliases may follow a
	// replaced thumbnail or original into an arbitrary host file.
	if runtime.GOOS == "windows" {
		return // File symlink creation commonly requires elevated privileges.
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outside, mustKnowledgePNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(assets.ThumbPath(source.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, assets.ThumbPath(source.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(assets.OriginalPath(source.ID, ".png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, assets.OriginalPath(source.ID, ".png")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/knowledge/images/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/images/" + source.ID,
		"/api/v1/knowledge/sources/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/sources/" + source.ID + "/image",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("symlinked media %s status = %d body = %s", path, w.Code, w.Body.String())
		}
	}
}

func TestKnowledgeImageSourceAliasesRejectNonImageSource(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-image-alias-key", APISecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-image-alias-key", APISecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "ordinary document", Title: "ordinary document", TenantID: tenant.ID, OwnerID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	writeKnowledgeImageAsset(t, assets, source.ID)
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json"))), agent: newMultiKnowledgeStore(store, nil)})
	defer server.Close()

	for _, path := range []string{
		"/api/v1/knowledge/sources/" + source.ID + "/thumbnail",
		"/api/v1/knowledge/sources/" + source.ID + "/image",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("non-image source asset %s status = %d body = %s", path, w.Code, w.Body.String())
		}
	}
}

func TestKnowledgeImageAssetEndpointsRejectNonRasterOriginal(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer func() { _ = svc.Close() }()
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "vector-image-key", APISecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "vector-image-key", APISecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "vector", Title: "vector", TenantID: tenant.ID, OwnerID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	assetDir := assets.AssetDir(source.ID)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "original.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json")))
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: access, agent: newMultiKnowledgeStore(store, access)})
	defer server.Close()
	for _, path := range []string{
		"/api/v1/knowledge/images/" + source.ID,
		"/api/v1/knowledge/sources/" + source.ID + "/image",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, w.Code)
		}
	}
}

func TestKnowledgeImportImageRejectsOversizedFile(t *testing.T) {
	oldMax := knowledgeMaxUploadSize
	knowledgeMaxUploadSize = 8
	defer func() { knowledgeMaxUploadSize = oldMax }()

	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-image-upload-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-image-upload-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	sqlStore, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqlStore.Close()
	km := &knowledgeStoreManager{store: sqlStore, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json"))), agent: newMultiKnowledgeStore(sqlStore, nil)}
	server := NewHTTPServer(svc, "admin-secret", km)
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "too-large.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("123456789")); err != nil {
		t.Fatalf("Write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/import/image", &body)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized image status = %d body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "too large") {
		t.Fatalf("oversized image response should mention size, got %s", w.Body.String())
	}
}

func TestKnowledgeImportImageReturnsDisplayableAssetContract(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-image-contract-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-image-contract-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	sqlStore, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqlStore.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatalf("NewImageAssetManager: %v", err)
	}
	sqlStore.SetImageAssetManager(assets)
	km := &knowledgeStoreManager{store: sqlStore, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json"))), agent: newMultiKnowledgeStore(sqlStore, nil)}
	server := NewHTTPServer(svc, "admin-secret", km)
	defer server.Close()

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("Encode fixture: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "diagram.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(imageBytes.Bytes()); err != nil {
		t.Fatalf("Write image: %v", err)
	}
	if err := writer.WriteField("title", "Architecture diagram"); err != nil {
		t.Fatalf("Write title: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/import/image", &body)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import status = %d body = %s", w.Code, w.Body.String())
	}
	var response struct {
		Status    string                          `json:"status"`
		SourceIDs []string                        `json:"source_ids"`
		AssetIDs  []string                        `json:"asset_ids"`
		Media     []knowledge.SearchResultMedia   `json:"media"`
		Result    knowledge.DirectoryImportResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != knowledge.ImportStatusCompleted || len(response.SourceIDs) != 1 || len(response.AssetIDs) != 1 || response.SourceIDs[0] != response.AssetIDs[0] {
		t.Fatalf("unexpected import contract: %#v", response)
	}
	if len(response.Media) != 1 || response.Media[0].AssetID != response.AssetIDs[0] || response.Media[0].ThumbnailURL == "" || response.Media[0].PreviewURL == "" || response.Media[0].OriginalURL == "" {
		t.Fatalf("missing display media contract: %#v", response.Media)
	}
	if len(response.Result.Items) != 1 || response.Result.Items[0].SourceID != response.SourceIDs[0] {
		t.Fatalf("result item source linkage missing: %#v", response.Result.Items)
	}
	thumbnailReq := httptest.NewRequest(http.MethodGet, response.Media[0].ThumbnailURL, nil)
	thumbnailReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	thumbnail := httptest.NewRecorder()
	server.Handler().ServeHTTP(thumbnail, thumbnailReq)
	if thumbnail.Code != http.StatusOK || thumbnail.Body.Len() == 0 {
		t.Fatalf("thumbnail status = %d bodyLen = %d", thumbnail.Code, thumbnail.Body.Len())
	}
}

func TestKnowledgeImageSearchRespectsReadScopesAndReturnsMedia(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatal(err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "API", APIKey: "knowledge-image-search-key", APISecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-image-search-key", APISecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	if err := store.SaveSource(ctx, knowledge.Source{ID: "shared-image", Kind: knowledge.SourceKindImage, URI: "file://shared", Title: "Shared diagram", OwnerID: userB.ID, TenantID: tenant.ID, Status: knowledge.StatusParsed}); err != nil {
		t.Fatal(err)
	}
	const embeddedAssetID = "shared-image_embedded-gateway-figure"
	if err := store.SaveDocumentNode(ctx, knowledge.DocumentNode{ID: "shared-image-node", SourceID: "shared-image", Type: knowledge.NodeTypeImage, Title: "Gateway architecture", Text: "Gateway architecture diagram for production traffic", Metadata: map[string]string{knowledge.MetaImageAssetID: embeddedAssetID}}); err != nil {
		t.Fatal(err)
	}
	writeKnowledgeImageAsset(t, assets, embeddedAssetID)
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json")))
	km := &knowledgeStoreManager{store: store, access: access, agent: newMultiKnowledgeStore(store, access)}
	server := NewHTTPServer(svc, "admin-secret", km)
	defer server.Close()
	perform := func() *httptest.ResponseRecorder {
		body := bytes.NewBufferString(`{"query":"gateway architecture"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/images/search", body)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	unauthorized := perform()
	if unauthorized.Code != http.StatusOK || !strings.Contains(unauthorized.Body.String(), `"total":0`) {
		t.Fatalf("unshared image search = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	if err := access.SetUser(ctx, tenant.ID, userA.ID, &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: tenant.ID, OwnerID: userB.ID, Name: "shared"}}}); err != nil {
		t.Fatal(err)
	}
	shared := perform()
	if shared.Code != http.StatusOK {
		t.Fatalf("shared image search = %d %s", shared.Code, shared.Body.String())
	}
	var response struct {
		Mode    string                   `json:"mode"`
		Ranking string                   `json:"ranking"`
		Results []knowledge.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(shared.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Mode != "text_to_image" || response.Ranking != "hybrid_text_ocr_caption" || len(response.Results) != 1 || response.Results[0].Media == nil || response.Results[0].Media.AssetID != embeddedAssetID || response.Results[0].Media.ThumbnailURL == "" || response.Results[0].Media.PreviewURL == "" || response.Results[0].Media.OriginalURL == "" {
		t.Fatalf("unexpected image search response: %#v", response)
	}
	if strings.Contains(shared.Body.String(), dataRoot) || strings.Contains(shared.Body.String(), "knowledge_assets") {
		t.Fatalf("image search leaked an asset path: %s", shared.Body.String())
	}
	for variant, path := range map[string]string{
		"thumbnail": response.Results[0].Media.ThumbnailURL,
		"preview":   response.Results[0].Media.PreviewURL,
		"original":  response.Results[0].Media.OriginalURL,
	} {
		assetReq := httptest.NewRequest(http.MethodGet, path, nil)
		assetReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
		asset := httptest.NewRecorder()
		server.Handler().ServeHTTP(asset, assetReq)
		if asset.Code != http.StatusOK || asset.Body.Len() == 0 || asset.Header().Get("Cache-Control") != "private, max-age=86400" {
			t.Fatalf("shared embedded image %s status=%d bodyLen=%d cache=%q", variant, asset.Code, asset.Body.Len(), asset.Header().Get("Cache-Control"))
		}
	}
}

func TestKnowledgeCapabilitiesDeclareImageSearchModes(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "knowledge-capabilities-key", APISecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "knowledge-capabilities-key", APISecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	km := &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json"))), agent: newMultiKnowledgeStore(store, nil)}
	server := NewHTTPServer(svc, "admin-secret", km)
	defer server.Close()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d body = %s", w.Code, w.Body.String())
	}
	var caps knowledge.KnowledgeCapabilities
	if err := json.Unmarshal(w.Body.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	if !caps.ImageRetrieval.TextToImage || caps.ImageRetrieval.ImageToImage || caps.ImageRetrieval.AgentTool != "knowledge_image_search" {
		t.Fatalf("unexpected image retrieval capabilities: %#v", caps.ImageRetrieval)
	}
}

func writeKnowledgeImageAsset(t *testing.T, assets *knowledge.ImageAssetManager, sourceID string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	imagePath := filepath.Join(t.TempDir(), "source.png")
	f, err := os.Create(imagePath)
	if err != nil {
		t.Fatalf("Create image fixture: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("Encode image fixture: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close image fixture: %v", err)
	}
	if _, err := assets.SaveImageFromPath(sourceID, imagePath); err != nil {
		t.Fatalf("SaveImageFromPath: %v", err)
	}
}

func mustKnowledgePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("Encode symlink image fixture: %v", err)
	}
	return body.Bytes()
}

func TestAdminKnowledgeAccessUpdateWritesAudit(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	km := &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))}
	server := NewHTTPServer(svc, "admin-secret", km)

	body, err := json.Marshal(knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: tenant.ID, OwnerID: userB.ID}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+userA.ID, bytes.NewReader(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set knowledge access status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_access_user_updated"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one knowledge access audit event, got %#v", events)
	}
	if events[0].ResourceID != tenant.ID+"/"+userA.ID || events[0].Metadata["scope_count"] != "1" || events[0].Metadata["cross_tenant"] != "false" {
		t.Fatalf("unexpected audit event: %#v", events[0])
	}
}

func TestAdminKnowledgeAccessUpdateDefaultsScopeTenantBeforeUserValidation(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})
	body, err := json.Marshal(knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{OwnerID: userB.ID, Name: "same tenant"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+userA.ID, bytes.NewReader(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set knowledge access with implicit scope tenant status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminKnowledgeAccessCrossTenantAndDeleteWriteAudit(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/knowledge-access/cross-tenant", bytes.NewReader([]byte(`{"enabled":true}`)))
	putReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	putResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(putResp, putReq)
	if putResp.Code != http.StatusOK {
		t.Fatalf("set cross tenant status = %d body = %s", putResp.Code, putResp.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+user.ID, nil)
	deleteReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	deleteResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete access status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}

	crossEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_access_cross_tenant_updated"})
	if err != nil {
		t.Fatalf("ListAuditEvents cross tenant: %v", err)
	}
	if len(crossEvents) != 1 || crossEvents[0].Metadata["enabled"] != "true" {
		t.Fatalf("unexpected cross-tenant audit events: %#v", crossEvents)
	}
	deleteEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_access_user_deleted"})
	if err != nil {
		t.Fatalf("ListAuditEvents delete: %v", err)
	}
	if len(deleteEvents) != 1 || deleteEvents[0].ResourceID != tenant.ID+"/"+user.ID {
		t.Fatalf("unexpected delete audit events: %#v", deleteEvents)
	}
}

func TestAdminKnowledgeClearTenantRequiresConfirmAndWritesAudit(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "tenant runbook", Title: "runbook", TenantID: "tenant-a", OwnerID: "user-a"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/tenant-a/knowledge", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("clear knowledge without confirm = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/tenant-a/knowledge?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear knowledge with confirm = %d body = %s", w.Code, w.Body.String())
	}
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected tenant knowledge cleared, got %#v", sources)
	}
	events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_tenant_cleared"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].ResourceID != "tenant-a" || events[0].Metadata["deleted"] != "1" {
		t.Fatalf("unexpected clear audit events: %#v", events)
	}
}

func TestUserKnowledgeClearRequiresAdminPasswordAndOnlyClearsOwnKnowledge(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(" owner-password-123 "), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	if err := saveAdminUsers(dataRoot, []adminUserRecord{{ID: "admin-owner", Username: "owner", Role: "owner", Status: "active", PasswordHash: string(hash)}}); err != nil {
		t.Fatalf("saveAdminUsers: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A", Email: "a@example.test"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B", Email: "b@example.test"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	if _, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: userA.ID, Name: "api", APIKey: "clear-own-key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "clear-own-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "own clear marker", Title: "own", TenantID: tenant.ID, OwnerID: userA.ID}); err != nil {
		t.Fatalf("SaveText own: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "other must stay", Title: "other", TenantID: tenant.ID, OwnerID: userB.ID}); err != nil {
		t.Fatalf("SaveText other: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge", bytes.NewReader([]byte(`{"admin_password":" owner-password-123 "}`)))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("clear without confirm = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge?confirm=true", bytes.NewReader([]byte(`{"admin_password":"wrong-password"}`)))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("clear with wrong password = %d body = %s", w.Code, w.Body.String())
	}
	ownBefore, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userA.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources own before: %v", err)
	}
	if len(ownBefore) != 1 {
		t.Fatalf("wrong password should not clear own knowledge: %#v", ownBefore)
	}
	failedEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_user_clear_failed"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(failedEvents) != 1 || failedEvents[0].ResourceID != tenant.ID+"/"+userA.ID || failedEvents[0].Metadata["reason"] != "invalid_admin_authorization" {
		t.Fatalf("unexpected failed clear audit events: %#v", failedEvents)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge?confirm=true", bytes.NewReader([]byte(`{"admin_password":" owner-password-123 "}`)))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear with admin password = %d body = %s", w.Code, w.Body.String())
	}
	ownAfter, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userA.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources own after: %v", err)
	}
	otherAfter, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userB.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources other after: %v", err)
	}
	if len(ownAfter) != 0 || len(otherAfter) != 1 {
		t.Fatalf("clear should remove only current user's knowledge, own=%#v other=%#v", ownAfter, otherAfter)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "own secret marker", Title: "own-secret", TenantID: tenant.ID, OwnerID: userA.ID}); err != nil {
		t.Fatalf("SaveText own secret: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "admin_users.json"), []byte(`not valid json`), 0600); err != nil {
		t.Fatalf("corrupt admin users: %v", err)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge?confirm=true", bytes.NewReader([]byte(`{"admin_password":"admin-secret"}`)))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear with admin secret in single credential field = %d body = %s", w.Code, w.Body.String())
	}
	ownAfterSecret, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userA.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources own after secret: %v", err)
	}
	otherAfterSecret, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: userB.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources other after secret: %v", err)
	}
	if len(ownAfterSecret) != 0 || len(otherAfterSecret) != 1 {
		t.Fatalf("admin secret clear should still remove only current user's knowledge, own=%#v other=%#v", ownAfterSecret, otherAfterSecret)
	}
	events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_user_cleared"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("unexpected clear audit events: %#v", events)
	}
	seenPassword := false
	seenSecret := false
	for _, event := range events {
		if event.ResourceID != tenant.ID+"/"+userA.ID || event.Metadata["deleted"] != "1" {
			t.Fatalf("unexpected clear audit event: %#v", event)
		}
		if event.Metadata["auth_type"] == "admin_password" {
			seenPassword = true
		}
		if event.Metadata["auth_type"] == "admin_secret" {
			seenSecret = true
		}
	}
	if !seenPassword || !seenSecret {
		t.Fatalf("clear audits should include password and admin secret auth types: %#v", events)
	}
}

func hasKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) bool {
	for _, scope := range scopes {
		if scope.TenantID == tenantID && scope.OwnerID == ownerID {
			return true
		}
	}
	return false
}

func hasKnowledgeSource(sources []knowledge.Source, sourceID string) bool {
	for _, source := range sources {
		if source.ID == sourceID {
			return true
		}
	}
	return false
}

func hasKnowledgeResultOwner(results []knowledge.SearchResult, ownerID string) bool {
	for _, result := range results {
		if result.Source.OwnerID == ownerID {
			return true
		}
	}
	return false
}

func hasStructuredCatalogSourceTitle(result knowledge.StructuredCatalogResult, title string) bool {
	for _, table := range result.Tables {
		if table.SourceTitle == title {
			return true
		}
	}
	return false
}

func TestMultiKnowledgeStoreListSourcesMergesReadableScopes(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "alpha note", Title: "alpha", TenantID: "tenant-a", OwnerID: "user-a", Labels: []string{"team"}}); err != nil {
		t.Fatalf("SaveText user-a: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: "beta note", Title: "beta", TenantID: "tenant-a", OwnerID: "user-b", Labels: []string{"team", "beta-only"}}); err != nil {
		t.Fatalf("SaveText user-b: %v", err)
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	multi := newMultiKnowledgeStore(store, access)

	// Default: own scope only.
	ownOnly, err := multi.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources ownOnly: %v", err)
	}
	if len(ownOnly) != 1 || ownOnly[0].OwnerID != "user-a" {
		t.Fatalf("expected only own source, got %#v", ownOnly)
	}

	// Grant read access to user-b's scope: both sources appear.
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b", Name: "team"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	merged, err := multi.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources merged: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged sources, got %d: %#v", len(merged), merged)
	}

	// Labels merge across scopes with counts summed for shared label names.
	labels, err := multi.ListSourceLabels(ctx, knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("ListSourceLabels: %v", err)
	}
	counts := map[string]int{}
	for _, label := range labels {
		counts[label.Label] = label.Count
	}
	if counts["team"] != 2 {
		t.Fatalf("expected shared label count 2, got %d: %#v", counts["team"], labels)
	}
	if counts["beta-only"] != 1 {
		t.Fatalf("expected beta-only label count 1, got %d", counts["beta-only"])
	}
}
