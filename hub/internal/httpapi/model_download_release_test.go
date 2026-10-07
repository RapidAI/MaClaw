package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestHubMirrorsReleaseAssets(t *testing.T) {
	files := modelDownloadExpectedFiles()
	for _, a := range embedding.ReleaseAssets() {
		found := false
		for _, f := range files {
			found = found || f == a.Filename
		}
		if !found {
			t.Fatalf("hub does not mirror release asset %s (expected files %v)", a.Filename, files)
		}
		if !isAllowedModelFilename(a.Filename) {
			t.Fatalf("hub endpoint refuses %s", a.Filename)
		}
	}
	if isAllowedModelFilename("secrets.json") || isAllowedModelFilename("../x.gguf") {
		t.Fatal("only pinned .json assets may be served")
	}
	args := modelDownloadArgs([]string{"a.gguf", embedding.Qwen3ModelFilename})
	if args[0] != "a.gguf" || !strings.HasPrefix(args[1], embedding.Qwen3ModelFilename+"#") || len(args[1]) != len(embedding.Qwen3ModelFilename)+65 {
		t.Fatalf("args %v", args)
	}
}

// The hub cache populates from the GitHub release (here a fake), verifies
// sha256, refuses a mismatching body, and re-fetches a corrupt cached copy.
func TestHubCachePopulatesFromReleaseWithShaCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh script")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	good := []byte(`{"head":"ok"}`)
	sum := sha256.Sum256(good)
	want := hex.EncodeToString(sum[:])
	serveBad := false
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasPrefix(r.URL.Path, "/Model_Release/") {
			http.NotFound(w, r)
			return
		}
		if serveBad {
			_, _ = w.Write([]byte(`{"head":"tampered"}`))
			return
		}
		_, _ = w.Write(good)
	}))
	defer srv.Close()
	root := t.TempDir()
	script := filepath.Join(root, "download-models.sh")
	if err := ensureModelDownloadScript(script); err != nil {
		t.Fatal(err)
	}
	models, home := filepath.Join(root, "models"), filepath.Join(root, "home")
	run := func() error {
		cmd := exec.Command("sh", script, srv.URL+"/Model_Release", models, home, filepath.Join(root, "sentinel"), filepath.Join(root, "lock"), "head.json#"+want)
		out, err := cmd.CombinedOutput()
		t.Logf("script: %s", out)
		return err
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(models, "head.json")); string(b) != string(good) {
		t.Fatalf("cached %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "sentinel")); err != nil {
		t.Fatal("sentinel not written on success")
	}
	// Cached + valid: no new request.
	n := hits
	if err := run(); err != nil || hits != n {
		t.Fatalf("re-download of a valid cache: err %v hits %d->%d", err, n, hits)
	}
	// Corrupt cache + tampered upstream: refused, nothing installed.
	_ = os.WriteFile(filepath.Join(models, "head.json"), []byte("corrupt"), 0o644)
	serveBad = true
	if err := run(); err == nil {
		t.Fatal("sha mismatch must fail")
	}
	if _, err := os.Stat(filepath.Join(models, "head.json")); !os.IsNotExist(err) {
		t.Fatal("corrupt/tampered file must not stay in the cache")
	}
	// Upstream fixed: corrupt cache replaced.
	serveBad = false
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(models, "head.json")); string(b) != string(good) {
		t.Fatalf("cached %q", b)
	}
}
