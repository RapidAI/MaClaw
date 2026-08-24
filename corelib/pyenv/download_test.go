package pyenv

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyDownloadSize(t *testing.T) {
	if err := verifyDownloadSize(1024, 1024); err != nil {
		t.Fatalf("verifyDownloadSize() complete download error = %v", err)
	}
	if err := verifyDownloadSize(1024, -1); err != nil {
		t.Fatalf("verifyDownloadSize() unknown content length error = %v", err)
	}
	err := verifyDownloadSize(1024, 2048)
	if err == nil || !isIncompleteDownloadError(err) || !errors.Is(err, errDownloadIncomplete) {
		t.Fatalf("verifyDownloadSize() incomplete error = %v, want errDownloadIncomplete", err)
	}
}

func TestDownloadWithFallbackSwitchesSourceAfterTruncatedResponse(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2048")
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer broken.Close()

	want := []byte("complete runtime archive")
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(want)
	}))
	defer fallback.Close()

	dest := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if err := downloadWithFallback([]string{broken.URL, fallback.URL}, dest, nil); err != nil {
		t.Fatalf("downloadWithFallback() error = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("downloadWithFallback() = %q, want %q", got, want)
	}
	if _, err := os.Stat(dest + ".download"); !os.IsNotExist(err) {
		t.Fatalf("temporary download file exists after fallback: %v", err)
	}
}

func TestDownloadSingleURLRestartsWhenContentRangeIsInvalid(t *testing.T) {
	want := []byte("complete runtime archive")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("invalid append"))
			return
		}
		_, _ = w.Write(want)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if err := os.WriteFile(dest+".download", make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadSingleURL(server.URL, dest, nil); err != nil {
		t.Fatalf("downloadSingleURL() error = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("downloadSingleURL() = %q, want %q", got, want)
	}
}

func TestParseContentRange(t *testing.T) {
	start, total, ok := parseContentRange("bytes 1024-2047/4096")
	if !ok || start != 1024 || total != 4096 {
		t.Fatalf("parseContentRange() = (%d, %d, %v)", start, total, ok)
	}
	for _, value := range []string{"", "bytes 1024-2047/*", "bytes 2047-1024/4096", "items 0-1/2"} {
		if _, _, ok := parseContentRange(value); ok {
			t.Fatalf("parseContentRange(%q) unexpectedly succeeded", value)
		}
	}
}

func TestStandalonePythonURLsUseReachableFallbacks(t *testing.T) {
	wasUsingChinaMirror := useChinaMirror.Load()
	t.Cleanup(func() { SetUseChinaMirror(wasUsingChinaMirror) })
	SetUseChinaMirror(false)
	urls, err := standalonePythonURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "https://github.com/") {
		t.Fatalf("non-China URLs = %v, want GitHub primary plus fallback", urls)
	}
	for _, url := range urls {
		if strings.Contains(url, "npmmirror.com") || strings.Contains(url, "cnb.cool") {
			t.Fatalf("unreachable Python mirror retained: %s", url)
		}
	}
}

func TestReplacePrivatePythonInstall(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	extractedDir := filepath.Join(root, "extracted")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "new"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replacePrivatePythonInstall(installDir, extractedDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "new")); err != nil || string(got) != "new" {
		t.Fatalf("replacement content = %q, %v; want new", got, err)
	}
	if err := commitPrivatePythonInstall(installDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installDir + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("old-install backup remains: %v", err)
	}
}

func TestRollbackPrivatePythonInstall(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	backupDir := installDir + ".previous"
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "new"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rollbackPrivatePythonInstall(installDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "old")); err != nil || string(got) != "old" {
		t.Fatalf("rolled-back install content = %q, %v; want old", got, err)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("backup remains after rollback: %v", err)
	}
}

func TestRollbackPrivatePythonInstallRemovesFailedFreshInstall(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "failed"), []byte("failed"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := rollbackPrivatePythonInstall(installDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("failed fresh install remains after rollback: %v", err)
	}
}

func TestCommitPrivatePythonInstallWithoutBackup(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := commitPrivatePythonInstall(installDir); err != nil {
		t.Fatalf("commitPrivatePythonInstall() error = %v", err)
	}
}

func TestReplacePrivatePythonInstallRestoresPreviousInstallOnMoveFailure(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	missingExtractDir := filepath.Join(root, "missing-extracted")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replacePrivatePythonInstall(installDir, missingExtractDir); err == nil {
		t.Fatal("replacePrivatePythonInstall() succeeded for missing extracted directory")
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "old")); err != nil || string(got) != "old" {
		t.Fatalf("restored install content = %q, %v; want old", got, err)
	}
	if _, err := os.Stat(installDir + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("old-install backup remains after restore: %v", err)
	}
}

func TestReplacePrivatePythonInstallPreservesInterruptedBackupOnMoveFailure(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	backupDir := installDir + ".previous"
	missingExtractDir := filepath.Join(root, "missing-extracted")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replacePrivatePythonInstall(installDir, missingExtractDir); err == nil {
		t.Fatal("replacePrivatePythonInstall() succeeded for missing extracted directory")
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "old")); err != nil || string(got) != "old" {
		t.Fatalf("restored interrupted backup = %q, %v; want old", got, err)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("backup remains after restore: %v", err)
	}
}

func TestResetRuntimeExtractDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extract")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leftover"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := resetRuntimeExtractDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("extract directory remains after reset: %v", err)
	}
}
