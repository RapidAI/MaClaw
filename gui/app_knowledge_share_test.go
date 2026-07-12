package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type oversizedGUIKnowledgePackageBody struct {
	remaining int64
}

func (r *oversizedGUIKnowledgePackageBody) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	for i := range p {
		p[i] = ' '
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestKnowledgeExportPackageFormatWritesExchangeJSON(t *testing.T) {
	t.Parallel()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteTenantID: "tenant-a", RemoteUserID: "user-a"},
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = store.SaveText(context.Background(), knowledge.TextSaveRequest{
		Text:     "package export body",
		Title:    "Package Note",
		OwnerID:  "user-a",
		TenantID: "tenant-a",
	})
	_ = store.Close()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.knowledge.json")
	result, err := app.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{
		OutputPath:      out,
		Format:          "package",
		RedactSensitive: false,
		Title:           "Pkg",
		Description:     "desc",
	})
	if err != nil {
		t.Fatalf("export package: %v", err)
	}
	if result.Format != "package" || result.Sources < 1 || result.OutputPath != out {
		t.Fatalf("result = %#v", result)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "maclaw.knowledge.package") || !strings.Contains(string(raw), "package export body") {
		t.Fatalf("package file unexpected: %s", raw)
	}
	// Path inference without format field
	out2 := filepath.Join(t.TempDir(), "infer.knowledge.json")
	result2, err := app.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{
		OutputPath: out2,
		// Format empty → inferred from extension
	})
	if err != nil {
		t.Fatalf("export inferred package: %v", err)
	}
	if result2.Format != "package" {
		t.Fatalf("inferred format = %q", result2.Format)
	}
}

func TestNormalizeKnowledgeExportFormat(t *testing.T) {
	t.Parallel()
	if got := normalizeKnowledgeExportFormat("package", ""); got != "package" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeKnowledgeExportFormat("", "x.knowledge.json"); got != "package" {
		t.Fatalf("path infer got %q", got)
	}
	if got := normalizeKnowledgeExportFormat("", "x.jsonl"); got != "jsonl" {
		t.Fatalf("jsonl got %q", got)
	}
}

func TestKnowledgeListMyHubSharesNormalizesItems(t *testing.T) {
	t.Parallel()
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"total":1,"offset":0,"limit":20,
			"items":[{
				"knowledge_id":"kn_mine",
				"title":"My Share",
				"description":"notes for team",
				"visibility_scope":"hub",
				"status":"active",
				"share_url":"/hub/knowledge/shares/kn_mine",
				"agent_import":"/api/knowledge/shares/kn_mine?intent=import",
				"view_count":3,
				"import_count":1,
				"updated_at":"2026-07-01T00:00:00Z",
				"source_summary":{"source_count":2}
			}]
		}`))
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	result, err := app.KnowledgeListMyHubShares(KnowledgeHubShareListRequest{Limit: 20})
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.HasPrefix(gotPath, "/api/knowledge/shares/mine?") || !strings.Contains(gotPath, "limit=20") {
		t.Fatalf("path = %q", gotPath)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Items[0]
	if item.KnowledgeID != "kn_mine" || item.Title != "My Share" || item.SourceCount != 2 || item.ImportCount != 1 {
		t.Fatalf("item = %#v", item)
	}
	if !strings.HasPrefix(item.ShareURL, server.URL+"/hub/knowledge/shares/") {
		t.Fatalf("share_url = %q", item.ShareURL)
	}
}

func TestKnowledgeUpdateHubSharePatchesDescription(t *testing.T) {
	t.Parallel()
	var method, path, auth string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"knowledge_id":"kn_edit",
			"title":"Updated Title",
			"description":"Updated description",
			"visibility_scope":"public",
			"status":"active",
			"share_url":"/hub/knowledge/shares/kn_edit",
			"agent_import":"/api/knowledge/shares/kn_edit?intent=import"
		}`))
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	item, err := app.KnowledgeUpdateHubShare(KnowledgeHubShareUpdateRequest{
		KnowledgeID:     "kn_edit",
		Title:           "Updated Title",
		Description:     "Updated description",
		VisibilityScope: "public",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if method != http.MethodPatch || path != "/api/knowledge/shares/kn_edit" {
		t.Fatalf("request = %s %s", method, path)
	}
	if auth != "Bearer viewer-token" {
		t.Fatalf("auth = %q", auth)
	}
	if body["description"] != "Updated description" || body["visibility_scope"] != "public" {
		t.Fatalf("body = %#v", body)
	}
	if item.KnowledgeID != "kn_edit" || item.Description != "Updated description" || !strings.Contains(item.ShareURL, server.URL) {
		t.Fatalf("item = %#v", item)
	}
}

func TestKnowledgeDeleteHubShareCallsDELETE(t *testing.T) {
	t.Parallel()
	var method, path, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	if err := app.KnowledgeDeleteHubShare(KnowledgeHubShareDeleteRequest{KnowledgeID: "kn_del"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if method != http.MethodDelete || path != "/api/knowledge/shares/kn_del" {
		t.Fatalf("request = %s %s", method, path)
	}
	if auth != "Bearer viewer-token" {
		t.Fatalf("auth = %q", auth)
	}
}

func TestKnowledgeShareToHubPostsPackageAndTTL(t *testing.T) {
	t.Parallel()

	var seen struct {
		Authorization string
		Body          map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/knowledge/shares" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		seen.Authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&seen.Body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"knowledge_id":"kn_123","share_url":"/hub/knowledge/shares/kn_123","agent_import":"/api/knowledge/shares/kn_123?intent=import","package_url":"/api/knowledge/shares/kn_123/package","expires_at":"2026-07-03T00:00:00Z","source_summary":{"source_count":1,"source_ids":[],"content_sources":0,"warnings":[],"hub_accepted":true}}`))
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
			RemoteTenantID:    "tenant-a",
			RemoteUserID:      "user-a",
		},
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = store.SaveText(context.Background(), knowledge.TextSaveRequest{
		Text:     "portable knowledge",
		Title:    "Portable Note",
		OwnerID:  "user-a",
		TenantID: "tenant-a",
		Labels:   []string{"portable"},
	})
	_ = store.Close()
	if err != nil {
		t.Fatalf("save text: %v", err)
	}

	result, err := app.KnowledgeShareToHub(KnowledgeHubShareRequest{
		Title:           "Team Export",
		Description:     "Knowledge for another machine",
		VisibilityScope: "tenant",
		TTL:             "month",
		RedactSensitive: true,
	})
	if err != nil {
		t.Fatalf("share to hub: %v", err)
	}
	if seen.Authorization != "Bearer viewer-token" {
		t.Fatalf("authorization = %q", seen.Authorization)
	}
	if seen.Body["description"] != "Knowledge for another machine" || seen.Body["visibility_scope"] != "tenant" || seen.Body["ttl"] != "month" {
		t.Fatalf("unexpected request body: %#v", seen.Body)
	}
	pkg, ok := seen.Body["package_json"].(map[string]any)
	if !ok {
		t.Fatalf("package_json missing or wrong type: %#v", seen.Body["package_json"])
	}
	manifest, _ := pkg["manifest"].(map[string]any)
	if manifest["format"] != "maclaw.knowledge.package" || manifest["editable"] != true {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	sources, _ := pkg["sources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("sources len = %d", len(sources))
	}
	firstSource, _ := sources[0].(map[string]any)
	if firstSource["content"] != "portable knowledge" {
		t.Fatalf("shared source content = %#v", firstSource["content"])
	}
	summary, _ := seen.Body["source_summary"].(map[string]any)
	if summary["content_sources"] != float64(1) {
		t.Fatalf("unexpected source summary: %#v", summary)
	}
	sourceIDs, _ := summary["source_ids"].([]any)
	if len(sourceIDs) != 1 || strings.TrimSpace(stringFromAny(sourceIDs[0])) == "" {
		t.Fatalf("source summary should include exported source ids: %#v", summary)
	}
	if result.KnowledgeID != "kn_123" || result.ContentSources != 1 || !strings.HasPrefix(result.ShareURL, server.URL+"/hub/knowledge/shares/") || !strings.Contains(result.AgentImport, "intent=import") {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.SourceSummary["hub_accepted"] != true || result.SourceSummary["content_sources"] != 1 {
		t.Fatalf("response summary should merge with local content metadata: %#v", result.SourceSummary)
	}
	if got := knowledgeShareStringSliceFromAny(result.SourceSummary["source_ids"]); len(got) != 1 || strings.TrimSpace(got[0]) == "" {
		t.Fatalf("response summary should keep local source ids when hub returns an empty list: %#v", result.SourceSummary)
	}
}

func TestCompactKnowledgeSourceIDStringsPreservesCaseSensitiveSourceIDs(t *testing.T) {
	t.Parallel()

	got := compactKnowledgeSourceIDStrings([]string{" Src_Mixed_001 ", "Src_Mixed_001", "src_mixed_001", ""})
	want := []string{"Src_Mixed_001", "src_mixed_001"}
	assertKnowledgeShareStrings(t, got, want)
}

func TestCompactKnowledgeShareStringsDeduplicatesCaseInsensitiveUsers(t *testing.T) {
	t.Parallel()

	got := compactKnowledgeShareStrings([]string{" User@Example.com ", "user@example.com", "Other@Example.com"})
	want := []string{"User@Example.com", "Other@Example.com"}
	assertKnowledgeShareStrings(t, got, want)
}

func assertKnowledgeShareStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("compact strings len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("compact strings = %#v, want %#v", got, want)
		}
	}
}

func TestValidateGUIKnowledgePackageJSONSize(t *testing.T) {
	t.Parallel()

	if err := validateGUIKnowledgePackageJSONSize(make([]byte, maxGUIKnowledgeHubPackageJSONBytes)); err != nil {
		t.Fatalf("exact limit should be accepted: %v", err)
	}
	if err := validateGUIKnowledgePackageJSONSize(make([]byte, maxGUIKnowledgeHubPackageJSONBytes+1)); err == nil || !strings.Contains(err.Error(), "hub accepts at most") {
		t.Fatalf("expected oversized package error, got %v", err)
	}
}

func TestMarshalGUIKnowledgePackageWithinLimitTruncatesContent(t *testing.T) {
	t.Parallel()

	pkg := guiKnowledgePackage{
		Manifest: guiKnowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   "kxp_fit",
			Description: "fit package",
			SourceCount: 1,
			Editable:    true,
		},
		Sources: []guiKnowledgePackageSource{{
			ID:           "ksrc_fit",
			Kind:         "text",
			Title:        "Fit Source",
			Content:      strings.Repeat("\"", 4096),
			ContentBytes: 4096,
		}},
	}

	raw, warnings, err := marshalGUIKnowledgePackageWithinLimit(&pkg, 1024)
	if err != nil {
		t.Fatalf("marshal within limit: %v", err)
	}
	if len(raw) > 1024 {
		t.Fatalf("raw package len=%d, want <= 1024", len(raw))
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "fit hub package size limit") {
		t.Fatalf("expected fit warning, got %#v", warnings)
	}
	if !pkg.Sources[0].Truncated || pkg.Sources[0].ContentBytes >= 4096 {
		t.Fatalf("source should be truncated: %#v", pkg.Sources[0])
	}
}

func TestBuildGUIKnowledgeSharePayloadWithinLimitsTruncatesForRequestLimit(t *testing.T) {
	t.Parallel()

	pkg := guiKnowledgePackage{
		Manifest: guiKnowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   "kxp_request_fit",
			Description: "fit request package",
			SourceCount: 1,
			Editable:    true,
		},
		Sources: []guiKnowledgePackageSource{{
			ID:           "ksrc_request_fit",
			Kind:         "text",
			Title:        "Request Fit Source",
			Content:      strings.Repeat("portable knowledge ", 2048),
			ContentBytes: len([]byte(strings.Repeat("portable knowledge ", 2048))),
		}},
	}

	body, summary, err := buildGUIKnowledgeSharePayloadWithinLimits(&pkg, nil, knowledgeSharePayloadOptions{
		SourceCount:       1,
		SourceIDs:         []string{"ksrc_request_fit"},
		Description:       "fit request",
		TTL:               "7d",
		PackageLimit:      128 << 10,
		ShareRequestLimit: 12 << 10,
	})
	if err != nil {
		t.Fatalf("build payload within limits: %v", err)
	}
	if len(body) > 12<<10 {
		t.Fatalf("payload len=%d, want <= %d", len(body), 12<<10)
	}
	if !pkg.Sources[0].Truncated {
		t.Fatalf("source should be truncated for request fit")
	}
	warnings := knowledgeShareStringSliceFromAny(summary["warnings"])
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "hub share request size limit") {
		t.Fatalf("expected request fit warning, got %#v", summary["warnings"])
	}
	seen := map[string]bool{}
	for _, warning := range warnings {
		if seen[warning] {
			t.Fatalf("duplicate warning %q in %#v", warning, warnings)
		}
		seen[warning] = true
	}
}

func TestValidateGUIKnowledgeShareRequestSize(t *testing.T) {
	t.Parallel()

	if err := validateGUIKnowledgeShareRequestSize(make([]byte, maxGUIKnowledgeHubShareRequestBytes)); err != nil {
		t.Fatalf("exact request limit should be accepted: %v", err)
	}
	if err := validateGUIKnowledgeShareRequestSize(make([]byte, maxGUIKnowledgeHubShareRequestBytes+1)); err == nil || !strings.Contains(err.Error(), "knowledge share request") {
		t.Fatalf("expected oversized request error, got %v", err)
	}
}

func TestFetchGUIKnowledgePackageRejectsOversizedPackage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, &oversizedGUIKnowledgePackageBody{remaining: int64(maxGUIKnowledgeHubPackageJSONBytes) + 1})
	}))
	defer server.Close()

	_, err := fetchGUIKnowledgePackage(context.Background(), server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "knowledge package is too large") {
		t.Fatalf("expected oversized package error, got %v", err)
	}
}

func TestResolveGUIKnowledgeShareAPIURLAcceptsHumanAgentAndPackageLinks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		link   string
		wantID string
	}{
		{name: "human", link: "https://hub.example/hub/knowledge/shares/kn_123", wantID: "kn_123"},
		{name: "agent", link: "https://hub.example/api/knowledge/shares/kn_456?intent=import", wantID: "kn_456"},
		{name: "package", link: "https://hub.example/api/knowledge/shares/kn_789/package", wantID: "kn_789"},
		{name: "escaped", link: "https://hub.example/hub/knowledge/shares/kn%20space", wantID: "kn space"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiURL, knowledgeID, err := resolveGUIKnowledgeShareAPIURL(tc.link, "", "")
			if err != nil {
				t.Fatalf("resolve share URL: %v", err)
			}
			if knowledgeID != tc.wantID {
				t.Fatalf("knowledgeID = %q, want %q", knowledgeID, tc.wantID)
			}
			wantURL := "https://hub.example/api/knowledge/shares/" + url.PathEscape(tc.wantID) + "?intent=import"
			if apiURL != wantURL {
				t.Fatalf("apiURL = %q, want %q", apiURL, wantURL)
			}
		})
	}
}
func TestKnowledgeImportHubShareImportsTextPackage(t *testing.T) {
	t.Parallel()

	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/knowledge/shares/kn_import":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"knowledge_id":"kn_import","title":"Importable","package_url":"/api/knowledge/shares/kn_import/package"}`))
		case "/api/knowledge/shares/kn_import/package":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"manifest":{"format":"maclaw.knowledge.package","version":1,"package_id":"kxp_import","title":"Importable","description":"Importable package","source_count":1,"editable":true},"sources":[{"kind":"text","title":"Imported Note","content":"shared portable knowledge","labels":["shared"],"content_truncated":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	dryRun, err := app.KnowledgeImportHubShare(KnowledgeHubShareImportRequest{KnowledgeID: "kn_import", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if !dryRun.DryRun || dryRun.Imported != 1 || dryRun.Skipped != 0 || !knowledgeShareWarningsContain(dryRun.Warnings, "content is truncated") {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}
	result, err := app.KnowledgeImportHubShare(KnowledgeHubShareImportRequest{ShareLink: server.URL + "/hub/knowledge/shares/kn_import"})
	if err != nil {
		t.Fatalf("import share: %v", err)
	}
	if result.Imported != 1 || result.PackageID != "kxp_import" {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if !knowledgeShareWarningsContain(result.Warnings, "content is truncated") {
		t.Fatalf("expected real import truncation warning, got %#v", result.Warnings)
	}
	for _, header := range authHeaders {
		if header != "Bearer viewer-token" {
			t.Fatalf("authorization header = %q", header)
		}
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	sources, err := store.ListSources(context.Background(), knowledge.ListSourcesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || sources[0].Title != "Imported Note" {
		t.Fatalf("unexpected imported sources: %#v", sources)
	}
}

func TestKnowledgeImportHubShareResolvesRelativePackageURLFromShareAPI(t *testing.T) {
	t.Parallel()

	var requestedPackagePath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/knowledge/shares/kn_relative":
			_, _ = w.Write([]byte(`{"knowledge_id":"kn_relative","title":"Relative","package_url":"package"}`))
		case "/api/knowledge/shares/kn_relative/package":
			requestedPackagePath = r.URL.Path
			_, _ = w.Write([]byte(`{"manifest":{"format":"maclaw.knowledge.package","version":1,"package_id":"kxp_relative","title":"Relative","description":"Relative package","source_count":1,"editable":true},"sources":[{"kind":"text","title":"Relative Note","content":"relative package content"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL: server.URL,
		},
	}
	result, err := app.KnowledgeImportHubShare(KnowledgeHubShareImportRequest{KnowledgeID: "kn_relative", DryRun: true})
	if err != nil {
		t.Fatalf("import relative package URL: %v", err)
	}
	if result.Imported != 1 || result.PackageID != "kxp_relative" {
		t.Fatalf("unexpected relative package import result: %#v", result)
	}
	if requestedPackagePath != "/api/knowledge/shares/kn_relative/package" {
		t.Fatalf("package path = %q, want share-relative package path", requestedPackagePath)
	}
}

func knowledgeShareWarningsContain(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}
func TestKnowledgeHubShareAgentTools(t *testing.T) {
	t.Parallel()

	var seenTTL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/knowledge/shares":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode share request: %v", err)
			}
			seenTTL, _ = body["ttl"].(string)
			_, _ = w.Write([]byte(`{"knowledge_id":"kn_tool","share_url":"/hub/knowledge/shares/kn_tool","agent_import":"/api/knowledge/shares/kn_tool?intent=import","package_url":"/api/knowledge/shares/kn_tool/package"}`))
		case "/api/knowledge/shares/kn_tool":
			_, _ = w.Write([]byte(`{"knowledge_id":"kn_tool","title":"Tool Import","package_url":"/api/knowledge/shares/kn_tool/package"}`))
		case "/api/knowledge/shares/kn_tool/package":
			_, _ = w.Write([]byte(`{"manifest":{"format":"maclaw.knowledge.package","version":1,"package_id":"kxp_tool","title":"Tool Import","description":"Tool import","source_count":1,"editable":true},"sources":[{"kind":"text","title":"Tool Note","content":"agent tool import content"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = store.SaveText(context.Background(), knowledge.TextSaveRequest{Text: "tool share content", Title: "Tool Share"})
	_ = store.Close()
	if err != nil {
		t.Fatalf("save text: %v", err)
	}

	shareJSON := app.toolKnowledgeShareToHub(map[string]any{"description": "Shared by agent tool"})
	if !strings.Contains(shareJSON, `"knowledge_id": "kn_tool"`) || !strings.Contains(shareJSON, `"content_sources": 1`) {
		t.Fatalf("unexpected share tool response: %s", shareJSON)
	}
	if seenTTL != "7d" {
		t.Fatalf("ttl = %q, want 7d", seenTTL)
	}

	importJSON := app.toolKnowledgeImportHubShare(map[string]any{"knowledge_id": "kn_tool"})
	if !strings.Contains(importJSON, `"dry_run": true`) || !strings.Contains(importJSON, `"imported": 1`) {
		t.Fatalf("unexpected import tool response: %s", importJSON)
	}
}

func TestTruncateStringToUTF8BytesKeepsValidRunes(t *testing.T) {
	t.Parallel()

	input := "abc你好xyz"
	got := truncateStringToUTF8Bytes(input, len([]byte("abc你")))
	if got != "abc你" {
		t.Fatalf("truncated text = %q", got)
	}
	if got := truncateStringToUTF8Bytes(input, 0); got != "" {
		t.Fatalf("zero budget truncation = %q", got)
	}
}
