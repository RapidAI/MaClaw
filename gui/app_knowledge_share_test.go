package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

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

	input := "abc你好🙂xyz"
	got := truncateStringToUTF8Bytes(input, len([]byte("abc你")))
	if got != "abc你" {
		t.Fatalf("truncated text = %q", got)
	}
	if got := truncateStringToUTF8Bytes(input, 0); got != "" {
		t.Fatalf("zero budget truncation = %q", got)
	}
}
