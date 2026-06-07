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
		RootPath:    filepath.Dir(localDoc),
		CurrentFile: localDoc,
		Warnings:    []string{"failed token=warning-secret path=" + dataRoot},
		Items: []knowledge.ImportItem{{
			FilePath:     localDoc,
			RelativePath: localDoc,
			ErrorMessage: "failed token=item-secret path=" + dataRoot,
		}},
	}

	got := sanitizeKnowledgeDirectoryImportResultForAPI(dataRoot, result)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal import result: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "warning-secret", "item-secret"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("expected import result to redact %q, got %s", leaked, body)
		}
	}
	if got.CurrentFile != filepath.Base(localDoc) || got.Items[0].FilePath != filepath.Base(localDoc) || got.Items[0].RelativePath != filepath.Base(localDoc) {
		t.Fatalf("expected import paths to use basename, got %#v", got)
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

	for _, tc := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/v1/knowledge/search", body: `{"query":"needle"}`, want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/api/v1/knowledge/context-pack", body: `{"query":"needle"}`, want: http.StatusServiceUnavailable},
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
	source, err := sqlStore.SaveText(ctx, knowledge.TextSaveRequest{Text: "private image source", Title: "private image", TenantID: tenant.ID, OwnerID: userB.ID})
	if err != nil {
		t.Fatalf("SaveText user B: %v", err)
	}
	writeKnowledgeImageAsset(t, assets, source.ID)

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json")))
	km := &knowledgeStoreManager{store: sqlStore, access: access, agent: newMultiKnowledgeStore(sqlStore, access)}
	server := NewHTTPServer(svc, "admin-secret", km)

	for _, path := range []string{
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
