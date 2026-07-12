package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSanitizeMobileOriginalFilename(t *testing.T) {
	got := sanitizeMobileOriginalFilename("report.docx")
	if got != "report.docx" {
		t.Fatalf("got=%q", got)
	}
	got = sanitizeMobileOriginalFilename("a:b?.png")
	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("unsafe chars remain: %q", got)
	}
	if got == "" {
		t.Fatal("empty")
	}
	if sanitizeMobileOriginalFilename("") != "original.bin" {
		t.Fatal("empty default")
	}
}

func TestFetchMobileDocumentOriginalViaHub(t *testing.T) {
	raw := []byte("original-bytes-from-hub")
	var sawAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d1", func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draft": {
				"id":"d1",
				"title":"T",
				"has_original":true,
				"source_filename":"shot.png",
				"source_download_url":"/api/mobile/documents/drafts/d1/source"
			}
		}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d1/source", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}

	name, body, err := app.fetchMobileDocumentOriginal("d1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if name != "shot.png" {
		t.Fatalf("name=%q", name)
	}
	if string(body) != string(raw) {
		t.Fatalf("body=%q", body)
	}
	if sawAuth != "Bearer viewer-token" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestSaveMobileDocumentOriginalWithoutDialog(t *testing.T) {
	raw := []byte("save-me")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d2", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d2","has_original":true,"source_filename":"note.txt","source_download_url":"/api/mobile/documents/drafts/d2/source"}}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d2/source", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	app := &App{
		// ctx nil → no SaveFileDialog, writes to temp
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	path, err := app.SaveMobileDocumentOriginal("d2")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw) {
		t.Fatalf("file=%q", data)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	_ = filepath.Base(path)
}

func TestFetchMobileDocumentOriginalRejectsMissingOriginal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d3", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d3","has_original":false,"title":"text only"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	_, _, err := app.fetchMobileDocumentOriginal("d3")
	if err == nil {
		t.Fatal("expected error for no original")
	}
}
