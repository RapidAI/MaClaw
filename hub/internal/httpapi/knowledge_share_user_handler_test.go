package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func newKnowledgeShareHandlerTestDeps(t *testing.T) (*store.Store, *auth.IdentityService, string) {
	t.Helper()
	dbPath := t.TempDir() + `\hub-knowledge-share-test.db`
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	st := sqlite.NewStore(provider)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	return st, identity, t.TempDir()
}

func doKnowledgeShareJSON(t *testing.T, handler http.HandlerFunc, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Host = "hub.example.test"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func doKnowledgeSharePathRequest(t *testing.T, handler http.HandlerFunc, method, target, knowledgeID, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.SetPathValue("knowledgeID", knowledgeID)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestKnowledgeShareUserPackageFlow(t *testing.T) {
	st, identity, packageDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "owner@example.com")
	pkg := map[string]any{
		"manifest": map[string]any{
			"format":       "maclaw.knowledge.package",
			"version":      1,
			"package_id":   "kxp_test",
			"description":  "Useful public docs",
			"source_count": 1,
			"editable":     true,
		},
		"sources": []map[string]any{{"kind": "text", "title": "Intro", "content": "hello knowledge"}},
	}

	createRec := doKnowledgeShareJSON(t, CreateKnowledgeShareHandler(st.KnowledgeShares, identity, packageDir), http.MethodPost, "/api/knowledge/shares", viewerToken, map[string]any{
		"title":            "Docs",
		"description":      "Useful public docs",
		"visibility_scope": "public",
		"package_json":     pkg,
		"source_summary": map[string]any{
			"content_sources": 1,
			"content_body":    "should not be stored",
			"warnings":        []string{" first warning ", "first warning", "", "second warning"},
		},
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create share status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created KnowledgeShareUserView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created share: %v", err)
	}
	if created.KnowledgeID == "" || created.PackageURL == "" || !strings.Contains(created.ShareURL, created.KnowledgeID) {
		t.Fatalf("created share missing ids/urls: %+v", created)
	}
	if created.SourceSummary["content_sources"] != float64(1) {
		t.Fatalf("created share should preserve safe content count summary: %#v", created.SourceSummary)
	}
	if _, ok := created.SourceSummary["content_body"]; ok {
		t.Fatalf("created share should redact content body summary: %#v", created.SourceSummary)
	}
	if warnings, ok := created.SourceSummary["warnings"].([]any); !ok || len(warnings) != 2 || warnings[0] != "first warning" || warnings[1] != "second warning" {
		t.Fatalf("created share should preserve warnings as a string array: %#v", created.SourceSummary["warnings"])
	}
	if created.ExpiresAt == "" {
		t.Fatalf("created share should default to a 7-day expiry: %+v", created)
	}

	listRec := doKnowledgeShareJSON(t, ListMyKnowledgeSharesHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/mine", viewerToken, nil)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), created.KnowledgeID) {
		t.Fatalf("list mine status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	publicRec := doKnowledgeSharePathRequest(t, GetKnowledgeSharePublicHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/"+created.KnowledgeID+"?intent=import", created.KnowledgeID, "")
	if publicRec.Code != http.StatusOK || !strings.Contains(publicRec.Body.String(), `"package_url"`) {
		t.Fatalf("public metadata status=%d body=%s", publicRec.Code, publicRec.Body.String())
	}
	publicPageRec := doKnowledgeSharePathRequest(t, KnowledgeSharePublicPageHandler(st.KnowledgeShares, identity), http.MethodGet, "/hub/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, "")
	if publicPageRec.Code != http.StatusOK || !strings.Contains(publicPageRec.Body.String(), "MaClaw Knowledge Share") {
		t.Fatalf("public page status=%d body=%s", publicPageRec.Code, publicPageRec.Body.String())
	}
	if got := publicPageRec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("public page content-type = %q", got)
	}
	if got := publicPageRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("public page cache-control = %q", got)
	}
	if got := publicPageRec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("public page nosniff header = %q", got)
	}
	if got := publicPageRec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("public page referrer policy = %q", got)
	}

	packageRec := doKnowledgeSharePathRequest(t, DownloadKnowledgeSharePackageHandler(st.KnowledgeShares, identity, packageDir), http.MethodGet, created.PackageURL, created.KnowledgeID, "")
	if packageRec.Code != http.StatusOK || !strings.Contains(packageRec.Body.String(), "hello knowledge") {
		t.Fatalf("download package status=%d body=%s", packageRec.Code, packageRec.Body.String())
	}

	updateRec := doKnowledgeShareJSON(t, func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("knowledgeID", created.KnowledgeID)
		UpdateMyKnowledgeShareHandler(st.KnowledgeShares, identity, packageDir)(w, r)
	}, http.MethodPatch, "/api/knowledge/shares/"+created.KnowledgeID, viewerToken, map[string]any{
		"description":      "Updated description",
		"visibility_scope": "public",
	})
	if updateRec.Code != http.StatusOK || !strings.Contains(updateRec.Body.String(), "Updated description") {
		t.Fatalf("update share status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	cancelRec := doKnowledgeShareJSON(t, func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("knowledgeID", created.KnowledgeID)
		UpdateMyKnowledgeShareHandler(st.KnowledgeShares, identity, packageDir)(w, r)
	}, http.MethodPatch, "/api/knowledge/shares/"+created.KnowledgeID, viewerToken, map[string]any{
		"description":      "Updated description",
		"visibility_scope": "private",
		"visibility_users": []string{},
		"ttl":              "permanent",
	})
	if cancelRec.Code != http.StatusOK || !strings.Contains(cancelRec.Body.String(), `"visibility_scope":"private"`) {
		t.Fatalf("cancel share status=%d body=%s", cancelRec.Code, cancelRec.Body.String())
	}
	cancelledAnonRec := doKnowledgeSharePathRequest(t, GetKnowledgeSharePublicHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, "")
	if cancelledAnonRec.Code != http.StatusForbidden {
		t.Fatalf("cancelled private share should be hidden from anonymous viewers, status=%d body=%s", cancelledAnonRec.Code, cancelledAnonRec.Body.String())
	}
	cancelledOwnerRec := doKnowledgeSharePathRequest(t, GetKnowledgeSharePublicHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, viewerToken)
	if cancelledOwnerRec.Code != http.StatusOK || !strings.Contains(cancelledOwnerRec.Body.String(), created.KnowledgeID) {
		t.Fatalf("cancelled share should remain visible to owner, status=%d body=%s", cancelledOwnerRec.Code, cancelledOwnerRec.Body.String())
	}
	listCancelledRec := doKnowledgeShareJSON(t, ListMyKnowledgeSharesHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/mine", viewerToken, nil)
	if listCancelledRec.Code != http.StatusOK || !strings.Contains(listCancelledRec.Body.String(), `"visibility_scope":"private"`) {
		t.Fatalf("cancelled share should remain in owner list, status=%d body=%s", listCancelledRec.Code, listCancelledRec.Body.String())
	}

	deleteRec := doKnowledgeSharePathRequest(t, DeleteMyKnowledgeShareHandler(st.KnowledgeShares, identity), http.MethodDelete, "/api/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, viewerToken)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete share status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	deletedRec := doKnowledgeSharePathRequest(t, GetKnowledgeSharePublicHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, "")
	if deletedRec.Code != http.StatusNotFound {
		t.Fatalf("deleted share should not be visible, status=%d body=%s", deletedRec.Code, deletedRec.Body.String())
	}
}

func TestListMyKnowledgeSharesMatchesOwnerByEmailWhenUserIDChanged(t *testing.T) {
	st, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, enroll := issueViewerToken(t, identity, "owner-email-match@example.com")
	now := time.Now().UTC()
	share := &store.KnowledgeShare{
		KnowledgeID:         "kn_email_owner_match",
		TenantID:            enroll.TenantID,
		OwnerUserID:         "legacy-user-id",
		OwnerUserEmail:      "Owner-Email-Match@Example.com",
		Title:               "Legacy owner record",
		Description:         "Record created before user ID migration",
		VisibilityScope:     "hub",
		VisibilityUsersJSON: "[]",
		SourceSummaryJSON:   "{}",
		ShareURL:            "/hub/knowledge/shares/kn_email_owner_match",
		Status:              "active",
		CreatedAt:           now,
		UpdatedAt:           now,
		PublishedAt:         now,
	}
	if err := st.KnowledgeShares.Create(context.Background(), share); err != nil {
		t.Fatalf("create legacy share: %v", err)
	}

	listRec := doKnowledgeShareJSON(t, ListMyKnowledgeSharesHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/mine", viewerToken, nil)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), share.KnowledgeID) {
		t.Fatalf("list mine should match owner by email when user id changed, status=%d body=%s", listRec.Code, listRec.Body.String())
	}
}

func TestPrivateKnowledgeSharePackageRequiresVisibleViewer(t *testing.T) {
	st, identity, packageDir := newKnowledgeShareHandlerTestDeps(t)
	ownerToken, _ := issueViewerToken(t, identity, "private-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "private-other@example.com")

	createRec := doKnowledgeShareJSON(t, CreateKnowledgeShareHandler(st.KnowledgeShares, identity, packageDir), http.MethodPost, "/api/knowledge/shares", ownerToken, map[string]any{
		"title":            "Private",
		"description":      "Private docs",
		"visibility_scope": "private",
		"ttl":              "permanent",
		"package_json": map[string]any{
			"manifest": map[string]any{"format": "maclaw.knowledge.package", "version": 1, "package_id": "kxp_private", "source_count": 1, "editable": true},
			"sources":  []map[string]any{{"kind": "text", "title": "Secret", "content": "secret knowledge"}},
		},
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create private share status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created KnowledgeShareUserView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created share: %v", err)
	}

	anonRec := doKnowledgeSharePathRequest(t, DownloadKnowledgeSharePackageHandler(st.KnowledgeShares, identity, packageDir), http.MethodGet, created.PackageURL, created.KnowledgeID, "")
	if anonRec.Code != http.StatusForbidden {
		t.Fatalf("anonymous private package status=%d body=%s", anonRec.Code, anonRec.Body.String())
	}
	otherRec := doKnowledgeSharePathRequest(t, DownloadKnowledgeSharePackageHandler(st.KnowledgeShares, identity, packageDir), http.MethodGet, created.PackageURL, created.KnowledgeID, otherToken)
	if otherRec.Code != http.StatusForbidden {
		t.Fatalf("other private package status=%d body=%s", otherRec.Code, otherRec.Body.String())
	}
	ownerRec := doKnowledgeSharePathRequest(t, DownloadKnowledgeSharePackageHandler(st.KnowledgeShares, identity, packageDir), http.MethodGet, created.PackageURL, created.KnowledgeID, ownerToken)
	if ownerRec.Code != http.StatusOK || !strings.Contains(ownerRec.Body.String(), "secret knowledge") {
		t.Fatalf("owner private package status=%d body=%s", ownerRec.Code, ownerRec.Body.String())
	}

	got, err := st.KnowledgeShares.Get(context.Background(), created.KnowledgeID)
	if err != nil {
		t.Fatalf("get share: %v", err)
	}
	if got == nil || got.ImportCount == 0 {
		t.Fatalf("expected package download to increment import count, got %#v", got)
	}
	if got.ExpiresAt != nil {
		t.Fatalf("permanent share should not have expires_at, got %v", got.ExpiresAt)
	}
}

func TestExpiredKnowledgeShareIsHiddenAndCleanupDeletesIt(t *testing.T) {
	st, identity, packageDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "expired-owner@example.com")
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	createRec := doKnowledgeShareJSON(t, CreateKnowledgeShareHandler(st.KnowledgeShares, identity, packageDir), http.MethodPost, "/api/knowledge/shares", viewerToken, map[string]any{
		"title":            "Temporary",
		"description":      "Temporary docs",
		"visibility_scope": "public",
		"expires_at":       expiresAt,
		"package_json": map[string]any{
			"manifest": map[string]any{"format": "maclaw.knowledge.package", "version": 1, "package_id": "kxp_expiring", "source_count": 1, "editable": true},
			"sources":  []map[string]any{{"kind": "text", "title": "Soon gone", "content": "temporary knowledge"}},
		},
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create expiring share status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created KnowledgeShareUserView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created share: %v", err)
	}
	share, err := st.KnowledgeShares.Get(context.Background(), created.KnowledgeID)
	if err != nil {
		t.Fatalf("get created share: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	share.ExpiresAt = &expiredAt
	if err := st.KnowledgeShares.UpdateOwner(context.Background(), share); err != nil {
		t.Fatalf("mark share expired: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		updated, err := st.KnowledgeShares.Get(context.Background(), created.KnowledgeID)
		if err != nil {
			t.Fatalf("get updated share: %v", err)
		}
		if updated != nil && updated.ExpiresAt != nil && updated.ExpiresAt.Before(time.Now().UTC()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("share did not become expired, got %#v", updated)
		}
		time.Sleep(10 * time.Millisecond)
	}
	hiddenRec := doKnowledgeSharePathRequest(t, GetKnowledgeSharePublicHandler(st.KnowledgeShares, identity), http.MethodGet, "/api/knowledge/shares/"+created.KnowledgeID, created.KnowledgeID, "")
	if hiddenRec.Code != http.StatusNotFound {
		t.Fatalf("expired share should be hidden, status=%d body=%s", hiddenRec.Code, hiddenRec.Body.String())
	}
	deleted, err := st.KnowledgeShares.DeleteExpired(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected expired share cleanup to delete at least one share")
	}
	got, err := st.KnowledgeShares.Get(context.Background(), created.KnowledgeID)
	if err != nil {
		t.Fatalf("get expired share: %v", err)
	}
	if got == nil || got.Status != "deleted" {
		t.Fatalf("expired share should be soft deleted, got %#v", got)
	}
}

func TestKnowledgeShareAdminManagementTenantScopeAndSorting(t *testing.T) {
	st, _, _ := newKnowledgeShareHandlerTestDeps(t)
	now := time.Now().UTC()
	shares := []*store.KnowledgeShare{
		{
			KnowledgeID:     "kn_tenant_a_low",
			TenantID:        "tenant-a",
			OwnerUserID:     "user-a",
			OwnerUserEmail:  "alice@example.com",
			Title:           "A Low",
			Description:     "Tenant A lower traffic",
			VisibilityScope: "public",
			ShareURL:        "/hub/knowledge/shares/kn_tenant_a_low",
			HubID:           "hub-local",
			StorageRef:      "knowledge-shares/kn_tenant_a_low/package.json",
			Status:          "active",
			ViewCount:       2,
			CreatedAt:       now.Add(-2 * time.Hour),
			UpdatedAt:       now.Add(-2 * time.Hour),
			PublishedAt:     now.Add(-2 * time.Hour),
		},
		{
			KnowledgeID:       "kn_tenant_a_high",
			TenantID:          "tenant-a",
			OwnerUserID:       "user-b",
			OwnerUserEmail:    "bob@example.com",
			Title:             "A High",
			Description:       "Tenant A higher traffic",
			VisibilityScope:   "tenant",
			ShareURL:          "/hub/knowledge/shares/kn_tenant_a_high",
			HubID:             "hub-local",
			StorageRef:        "knowledge-shares/kn_tenant_a_high/package.json",
			SourceSummaryJSON: `{"source_ids": ["secret_source"], "content": "hidden body"}`,
			Status:            "active",
			ViewCount:         9,
			CreatedAt:         now.Add(-time.Hour),
			UpdatedAt:         now.Add(-time.Hour),
			PublishedAt:       now.Add(-time.Hour),
		},
		{
			KnowledgeID:     "kn_tenant_b",
			TenantID:        "tenant-b",
			OwnerUserID:     "user-c",
			OwnerUserEmail:  "carol@example.com",
			Title:           "B",
			Description:     "Tenant B traffic",
			VisibilityScope: "public",
			ShareURL:        "/hub/knowledge/shares/kn_tenant_b",
			HubID:           "hub-local",
			StorageRef:      "knowledge-shares/kn_tenant_b/package.json",
			Status:          "active",
			ViewCount:       99,
			CreatedAt:       now,
			UpdatedAt:       now,
			PublishedAt:     now,
		},
	}
	for _, share := range shares {
		if err := st.KnowledgeShares.Create(context.Background(), share); err != nil {
			t.Fatalf("create share %s: %v", share.KnowledgeID, err)
		}
	}

	tenantAdmin := &store.AdminUser{ID: "admin-a", Scope: "tenant", TenantID: "tenant-a", Role: "admin", Status: "active"}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/knowledge/shares?sort=view_count_desc&limit=50", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, tenantAdmin))
	rec := httptest.NewRecorder()
	ListKnowledgeSharesAdminHandler(st.KnowledgeShares)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "source_summary") || strings.Contains(rec.Body.String(), "secret_source") || strings.Contains(rec.Body.String(), "hidden body") {
		t.Fatalf("admin list leaked source summary: %s", rec.Body.String())
	}
	var listed struct {
		Items []KnowledgeShareAdminView `json:"items"`
		Total int                       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listed.Total != 2 || len(listed.Items) != 2 {
		t.Fatalf("tenant admin should only see tenant-a shares, got total=%d items=%+v", listed.Total, listed.Items)
	}
	if listed.Items[0].KnowledgeID != "kn_tenant_a_high" || listed.Items[1].KnowledgeID != "kn_tenant_a_low" {
		t.Fatalf("shares not sorted by view count desc: %+v", listed.Items)
	}
	for _, item := range listed.Items {
		if item.TenantID != "tenant-a" {
			t.Fatalf("tenant admin saw cross-tenant share: %+v", item)
		}
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/admin/knowledge/shares?user=alice", nil)
	filterReq = filterReq.WithContext(context.WithValue(filterReq.Context(), adminUserContextKey, tenantAdmin))
	filterRec := httptest.NewRecorder()
	ListKnowledgeSharesAdminHandler(st.KnowledgeShares)(filterRec, filterReq)
	if filterRec.Code != http.StatusOK || !strings.Contains(filterRec.Body.String(), "alice@example.com") || strings.Contains(filterRec.Body.String(), "bob@example.com") {
		t.Fatalf("user filter status=%d body=%s", filterRec.Code, filterRec.Body.String())
	}

	forbiddenReq := httptest.NewRequest(http.MethodDelete, "/api/admin/knowledge/shares/kn_tenant_b", bytes.NewReader([]byte(`{"reason":"policy"}`)))
	forbiddenReq.SetPathValue("knowledgeID", "kn_tenant_b")
	forbiddenReq = forbiddenReq.WithContext(context.WithValue(forbiddenReq.Context(), adminUserContextKey, tenantAdmin))
	forbiddenRec := httptest.NewRecorder()
	ForceDeleteKnowledgeShareAdminHandler(st.KnowledgeShares, nil)(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin cross-tenant force delete status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	globalAdmin := &store.AdminUser{ID: "owner", Scope: "global", Role: "owner", Status: "active"}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/knowledge/shares/kn_tenant_b", bytes.NewReader([]byte(`{"reason":"policy"}`)))
	deleteReq.SetPathValue("knowledgeID", "kn_tenant_b")
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), adminUserContextKey, globalAdmin))
	deleteRec := httptest.NewRecorder()
	ForceDeleteKnowledgeShareAdminHandler(st.KnowledgeShares, nil)(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("global admin force delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	deleted, err := st.KnowledgeShares.Get(context.Background(), "kn_tenant_b")
	if err != nil {
		t.Fatalf("get deleted share: %v", err)
	}
	if deleted == nil || deleted.Status != "deleted" || deleted.ForcedDeletedReason != "policy" {
		t.Fatalf("force delete did not mark share deleted: %+v", deleted)
	}
}

func TestRenderKnowledgeSharePublicHTMLBilingualContract(t *testing.T) {
	html := renderKnowledgeSharePublicHTML(KnowledgeShareUserView{
		KnowledgeID:     "kn_public_html",
		Title:           "API <Guide>",
		Description:     "Public API docs with <script>alert(1)</script>",
		VisibilityScope: "hub",
		SourceSummary: map[string]any{
			"source_count":    3,
			"content_sources": 2,
		},
		ShareURL:    "https://hub.example.test/hub/knowledge/shares/kn_public_html",
		AgentImport: "/api/knowledge/shares/kn_public_html?intent=import",
		PublishedAt: "2026-06-26T08:00:00Z",
		ViewCount:   9,
		ImportCount: 4,
	})
	required := []string{
		`class="lang-toggle"`,
		`data-set-lang="zh"`,
		`data-set-lang="en"`,
		`Agent 导入 JSON`,
		`Agent import JSON`,
		`property="og:title" content="API &lt;Guide&gt;"`,
		`property="og:description" content="Public API docs with &lt;script&gt;alert(1)&lt;/script&gt;"`,
		`property="og:url" content="https://hub.example.test/hub/knowledge/shares/kn_public_html"`,
		`name="twitter:card" content="summary"`,
		`复制分享链接`,
		`Copy share link`,
		`人可阅读，Agent 可导入`,
		`Readable by people, importable by agents`,
		`Knowledge share statistics`,
		`<strong>3</strong><span data-lang="zh">来源条目</span>`,
		`<strong>2</strong><span data-lang="zh">可导入内容</span>`,
		`<strong>9</strong><span data-lang="zh">浏览次数</span>`,
		`<strong>4</strong><span data-lang="zh">导入次数</span>`,
		`data-copy="https://hub.example.test/hub/knowledge/shares/kn_public_html"`,
		`/api/knowledge/shares/kn_public_html?intent=import`,
		`rel="alternate" type="application/json" href="/api/knowledge/shares/kn_public_html?intent=import"`,
		`data-manage-shares`,
		`node.setAttribute('href', '/hub/knowledge/shares/mine' + tokenSuffix);`,
		`localStorage.getItem('maclawKnowledgeShareLang')`,
		`document.execCommand('copy')`,
	}
	for _, want := range required {
		if !strings.Contains(html, want) {
			t.Fatalf("public share html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script>alert(1)</script>") || !strings.Contains(html, "API &lt;Guide&gt;") {
		t.Fatalf("public share html did not escape user content:\n%s", html)
	}
}

func TestRenderKnowledgeSharePublicHTMLSanitizesLinksAndStats(t *testing.T) {
	html := renderKnowledgeSharePublicHTML(KnowledgeShareUserView{
		KnowledgeID:     "kn_safe_html",
		Description:     "Safe page",
		VisibilityScope: "public",
		SourceSummary: map[string]any{
			"source_count":    float64(-3),
			"content_sources": math.NaN(),
		},
		ShareURL:    `javascript:alert("share")`,
		AgentImport: `data:text/html,agent`,
		ViewCount:   -9,
		ImportCount: -2,
	})
	if strings.Contains(html, "javascript:") || strings.Contains(html, "data:text/html") {
		t.Fatalf("public share html kept unsafe href:\n%s", html)
	}
	if !strings.Contains(html, `href="#"`) || !strings.Contains(html, `data-copy="#"`) {
		t.Fatalf("public share html did not replace unsafe links with safe fallback:\n%s", html)
	}
	if strings.Contains(html, ">-") || strings.Contains(html, "NaN") {
		t.Fatalf("public share html rendered unsafe stats:\n%s", html)
	}
}

func TestRenderKnowledgeSharePublicHTMLDoesNotPromoteKnowledgeIDFallbackToTitle(t *testing.T) {
	html := renderKnowledgeSharePublicHTML(KnowledgeShareUserView{
		KnowledgeID:     "kn_b0942c48eeea66e09f1429255",
		Description:     "Knowledge ID should stay in the share details panel only.",
		VisibilityScope: "hub",
	})
	if strings.Contains(html, `<h1`) || strings.Contains(html, `class="hero-title`) {
		t.Fatalf("public share html should not render a hero title when only knowledge ID is available:\n%s", html)
	}
	if !strings.Contains(html, `<code>kn_b0942c48eeea66e09f1429255</code>`) {
		t.Fatalf("public share html should still show knowledge ID in share details:\n%s", html)
	}
	if !strings.Contains(html, `property="og:title" content="Knowledge Share"`) {
		t.Fatalf("public share html should use a generic metadata title without leaking the ID:\n%s", html)
	}
}
