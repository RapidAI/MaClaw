package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type oversizedJSONBody struct {
	remaining int64
}

func (r *oversizedJSONBody) Read(p []byte) (int, error) {
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

func TestKnowledgeShareFetchesForwardAuthorization(t *testing.T) {
	const wantAuth = "Bearer hub-viewer-token"
	var metadataAuth string
	var packageAuth string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/knowledge/shares/kn_test":
			metadataAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"knowledge_id": "kn_test",
				"package_url":  "/api/knowledge/shares/kn_test/package",
			})
		case "/api/knowledge/shares/kn_test/package":
			packageAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(knowledgePackage{
				Manifest: knowledgePackageManifest{Format: "maclaw.knowledge.package", Version: 1, PackageID: "kxp_test", SourceCount: 1, Editable: true},
				Sources:  []knowledgePackageSource{{Kind: "text", Title: "Intro", Content: "hello"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hub.Close)

	authHeader := knowledgeShareAuthorizationHeader("hub-viewer-token", "")
	if authHeader != wantAuth {
		t.Fatalf("authorization header = %q, want %q", authHeader, wantAuth)
	}
	apiURL := hub.URL + "/api/knowledge/shares/kn_test?intent=import"
	share, err := fetchKnowledgeShareMetadata(context.Background(), apiURL, authHeader)
	if err != nil {
		t.Fatalf("fetch metadata: %v", err)
	}
	packageURL := knowledgeSharePackageURL(apiURL, share)
	if packageURL != hub.URL+"/api/knowledge/shares/kn_test/package" {
		t.Fatalf("package url = %q", packageURL)
	}
	pkg, err := fetchKnowledgePackage(context.Background(), packageURL, authHeader)
	if err != nil {
		t.Fatalf("fetch package: %v", err)
	}
	if pkg.Manifest.PackageID != "kxp_test" {
		t.Fatalf("package id = %q", pkg.Manifest.PackageID)
	}
	if metadataAuth != wantAuth || packageAuth != wantAuth {
		t.Fatalf("authorization not forwarded, metadata=%q package=%q", metadataAuth, packageAuth)
	}
}

func TestKnowledgeShareAuthorizationHeaderPrefersExplicitAuthorization(t *testing.T) {
	got := knowledgeShareAuthorizationHeader("hub-token", "Bearer explicit-token")
	if got != "Bearer explicit-token" {
		t.Fatalf("authorization header = %q", got)
	}
	got = knowledgeShareAuthorizationHeader("", "raw-token")
	if got != "Bearer raw-token" {
		t.Fatalf("raw authorization header = %q", got)
	}
}

func TestReadJSONBodyWithLimitRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/import/package", &oversizedJSONBody{remaining: maxKnowledgePackageJSONBodyBytes + 1})
	var pkg knowledgePackage
	err := readJSONBodyWithLimit(req, &pkg, maxKnowledgePackageJSONBodyBytes)
	if err == nil || !strings.Contains(err.Error(), "request body too large") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestFitKnowledgeExportPackageJSONTruncatesEscapedContent(t *testing.T) {
	pkg := knowledgePackage{
		Manifest: knowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   "kxp_fit",
			Description: "fit package",
			SourceCount: 1,
			Editable:    true,
		},
		Sources: []knowledgePackageSource{{
			ID:      "ksrc_fit",
			Kind:    "text",
			Title:   "Fit Source",
			Content: strings.Repeat("\"", 4096),
		}},
	}

	fitKnowledgeExportPackageJSON(&pkg, 1024)
	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	if len(raw) > 1024 {
		t.Fatalf("package len=%d, want <= 1024", len(raw))
	}
	if !pkg.Sources[0].ContentTruncated || len(pkg.Sources[0].Content) >= 4096 {
		t.Fatalf("source should be truncated: %#v", pkg.Sources[0])
	}
}

func TestKnowledgeExportPackageIncludesLargeInlineContent(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	text := strings.Repeat("large portable knowledge\n", 30_000)
	source, err := store.SaveText(ctx, knowledge.TextSaveRequest{Text: text, Title: "Large Portable", TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	pkg := buildKnowledgeExportPackageWithStore(ctx, store, t.TempDir(), agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, "Large Export", "portable package", []knowledge.Source{source})

	if len(pkg.Sources) != 1 {
		t.Fatalf("sources len = %d", len(pkg.Sources))
	}
	if pkg.Sources[0].ContentTruncated {
		t.Fatalf("large inline content should fit current export budget, content bytes=%d", len([]byte(pkg.Sources[0].Content)))
	}
	if !strings.Contains(pkg.Sources[0].Content, "large portable knowledge") || len([]byte(pkg.Sources[0].Content)) <= 512*1024 {
		t.Fatalf("exported content looks unexpectedly short, bytes=%d", len([]byte(pkg.Sources[0].Content)))
	}
}
