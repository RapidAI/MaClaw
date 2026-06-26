package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
