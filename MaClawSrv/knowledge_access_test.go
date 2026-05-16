package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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

func hasKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) bool {
	for _, scope := range scopes {
		if scope.TenantID == tenantID && scope.OwnerID == ownerID {
			return true
		}
	}
	return false
}
