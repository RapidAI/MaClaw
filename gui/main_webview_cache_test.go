package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func TestClearWebviewAssetCacheForFingerprintRemovesOnlyCacheDirs(t *testing.T) {
	root := t.TempDir()
	cacheFile := filepath.Join(root, "EBWebView", "Default", "Code Cache", "js", "chunk")
	localStorageFile := filepath.Join(root, "EBWebView", "Default", "Local Storage", "leveldb", "state")
	preferencesFile := filepath.Join(root, "EBWebView", "Default", "Preferences")
	for _, path := range []string{cacheFile, localStorageFile, preferencesFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	clearWebviewAssetCacheForFingerprint(root, func() (string, error) {
		return "build-1", nil
	})

	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Fatalf("cache file should be removed, stat err=%v", err)
	}
	for _, path := range []string{localStorageFile, preferencesFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persistent webview data should remain at %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, ".maclaw-frontend-build.sha256")); err != nil || string(got) != "build-1" {
		t.Fatalf("marker = %q, %v; want build-1", got, err)
	}
}

func TestClearWebviewAssetCacheForFingerprintSkipsMatchingBuild(t *testing.T) {
	root := t.TempDir()
	cacheFile := filepath.Join(root, "EBWebView", "Default", "Cache", "Cache_Data", "data_1")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("cached"), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".maclaw-frontend-build.sha256"), []byte("build-1"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	clearWebviewAssetCacheForFingerprint(root, func() (string, error) {
		return "build-1", nil
	})

	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("matching build should not clear cache file: %v", err)
	}
}

func TestClearWebviewAssetCacheForFingerprintClearsWhenFingerprintFails(t *testing.T) {
	root := t.TempDir()
	cacheFile := filepath.Join(root, "EBWebView", "Default", "Code Cache", "js", "chunk")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	clearWebviewAssetCacheForFingerprint(root, func() (string, error) {
		return "", os.ErrNotExist
	})
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Fatalf("cache should be cleared when fingerprint fails, stat err=%v", err)
	}
}

func TestSingleInstanceUniqueIDIsBrandScoped(t *testing.T) {
	// Default (non-OEM) build must keep historical lock id.
	if got := singleInstanceUniqueID(); got != "maclaw-lock" {
		t.Fatalf("default singleInstanceUniqueID() = %q, want maclaw-lock", got)
	}
}

func TestDefaultWebviewUserDataPathIsBrandScoped(t *testing.T) {
	if got := webviewProfileFolder(); got != "MaClaw" {
		t.Fatalf("default webviewProfileFolder() = %q, want MaClaw", got)
	}
	if goruntime.GOOS != "windows" {
		if got := defaultWebviewUserDataPath(); got != "" {
			t.Fatalf("non-windows path = %q, want empty", got)
		}
		return
	}
	got := defaultWebviewUserDataPath()
	if filepath.Base(got) != "MaClaw.exe" {
		t.Fatalf("default brand path base = %q, want MaClaw.exe (got full %q)", filepath.Base(got), got)
	}
}

func TestEmbeddedFrontendFingerprintIsDeterministic(t *testing.T) {
	a, err := embeddedFrontendFingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	b, err := embeddedFrontendFingerprint()
	if err != nil {
		t.Fatalf("fingerprint 2nd: %v", err)
	}
	if a == "" || a != b {
		t.Fatalf("fingerprint not stable: %q vs %q", a, b)
	}
	if len(a) != 64 { // sha256 hex
		t.Fatalf("fingerprint length = %d, want 64 hex chars", len(a))
	}
}

func TestNoStoreAssetMiddlewareSetsCacheHeaders(t *testing.T) {
	handler := noStoreAssetMiddleware(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q", got)
	}
	if got := rec.Header().Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q", got)
	}
}
