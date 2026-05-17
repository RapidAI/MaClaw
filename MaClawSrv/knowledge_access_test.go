package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

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
	if server.canAccessSource(knowledge.Source{TenantID: "tenant-a", OwnerID: "user-b"}, principal) {
		t.Fatalf("same-tenant non-owner source should not be manageable")
	}
	if server.canAccessSource(knowledge.Source{TenantID: "tenant-a"}, principal) {
		t.Fatalf("tenant shared source should not be user-manageable")
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
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	server := &HTTPServer{knowledgeMgr: &knowledgeStoreManager{store: store, access: access}}
	principal := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}

	sources, err := server.listReadableKnowledgeSources(ctx, principal)
	if err != nil {
		t.Fatalf("listReadableKnowledgeSources: %v", err)
	}
	if !hasKnowledgeSource(sources, own.ID) || !hasKnowledgeSource(sources, team.ID) {
		t.Fatalf("expected own and configured team sources, got %#v", sources)
	}
	if !server.canReadSource(ctx, team, principal) {
		t.Fatalf("expected configured team source to be readable")
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

func TestAdminKnowledgeAccessUpdateWritesAudit(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	km := &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))}
	server := NewHTTPServer(svc, "admin-secret", km)

	body, err := json.Marshal(knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-a", OwnerID: "user-b"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/knowledge-access/tenants/tenant-a/users/user-a", bytes.NewReader(body))
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
	if events[0].ResourceID != "tenant-a/user-a" || events[0].Metadata["scope_count"] != "1" || events[0].Metadata["cross_tenant"] != "false" {
		t.Fatalf("unexpected audit event: %#v", events[0])
	}
}

func TestAdminKnowledgeAccessCrossTenantAndDeleteWriteAudit(t *testing.T) {
	ctx := context.Background()
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/knowledge-access/cross-tenant", bytes.NewReader([]byte(`{"enabled":true}`)))
	putReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	putResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(putResp, putReq)
	if putResp.Code != http.StatusOK {
		t.Fatalf("set cross tenant status = %d body = %s", putResp.Code, putResp.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/tenant-a/users/user-a", nil)
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
	if len(deleteEvents) != 1 || deleteEvents[0].ResourceID != "tenant-a/user-a" {
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
