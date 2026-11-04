package locall3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// Live release check (network): fetches the MN guard head through the client's
// release-asset path (GitHub Model_Release first; the hub URL is unreachable so
// the GitHub copy must satisfy size+sha256), then loads it with the real Qwen3
// embedder and checks the default mode is shadow.
//
//	MACLAW_LIVE_RELEASE=1 MACLAW_LIVE_MODELS=<dir with Qwen3 GGUF> \
//	MACLAW_LIVE_MAIN_HEAD=<shipped zh/en head> go test -run TestLiveMNGuardRelease
func TestLiveMNGuardRelease(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_RELEASE") != "1" {
		t.Skip("set MACLAW_LIVE_RELEASE=1")
	}
	models, mainHead := os.Getenv("MACLAW_LIVE_MODELS"), os.Getenv("MACLAW_LIVE_MAIN_HEAD")
	var asset embedding.ReleaseAsset
	for _, a := range embedding.ReleaseAssets() {
		if a.Filename == intent.MNGuardHeadName {
			asset = a
		}
	}
	if asset.Kind != "mn_guard_head" {
		t.Fatalf("asset not pinned: %+v", asset)
	}
	root := t.TempDir()
	f := newFetcherForTest(root, asset)
	urls := f.Spec().URLs
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/") {
		t.Fatalf("fetch order: %v", urls)
	}
	path, err := f.Ensure(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := embedding.VerifyModelFile(path, f.Spec()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Logf("fetched %s via %s", path, urls[0])
	headDir := filepath.Dir(path)
	if err := os.Symlink(mainHead, filepath.Join(headDir, intent.ShippedLocalHeadName)); err != nil {
		t.Fatal(err)
	}

	t.Setenv(intent.LocalHeadEnv, "")
	os.Unsetenv(intent.MNLocalEnv)
	if m := intent.MNLocalMode(); m != intent.MNLocalShadow {
		t.Fatalf("default MN mode = %q, want shadow", m)
	}
	logp := filepath.Join(t.TempDir(), "mn.jsonl")
	t.Setenv(MNShadowLogEnv, logp)
	load, err := LoadEmbedder(filepath.Join(models, embedding.DefaultModelFilename), false)
	if err != nil {
		t.Fatal(err)
	}
	defer load.Embedder.Close()
	rt := NewWithMode(headDir, intent.LocalL3On)
	rt.Logf = t.Logf
	if rt.MNMode() != intent.MNLocalShadow {
		t.Fatalf("runtime MN mode = %q", rt.MNMode())
	}
	if _, err := rt.Attach(load.Embedder); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	ctx := context.Background()
	if _, err := fn(ctx, ctx, "", "Одоо хэдэн цаг болж байна?"); err != nil || calls != 1 {
		t.Fatalf("shadow must decide remotely: err=%v calls=%d", err, calls)
	}
	rt.mnWG.Wait()
	recs := readMNLog(t, logp)
	if len(recs) != 1 || recs[0].Mode != "shadow" || recs[0].DecidedBy != "remote" ||
		!strings.HasPrefix(recs[0].Head, intent.MNGuardHeadName+"@732eee75488f") || recs[0].LocalLabel == "" {
		t.Fatalf("shadow log: %+v", recs)
	}
	t.Logf("shadow record: head=%s label=%s p=%.5f would_accept=%v", recs[0].Head, recs[0].LocalLabel, recs[0].LocalProb, recs[0].WouldAccept)
}

// newFetcherForTest mirrors guiapp startHeadAssetFetch with an unreachable hub,
// so only the GitHub release URL can succeed.
func newFetcherForTest(root string, a embedding.ReleaseAsset) *embedding.ModelFetcher {
	return embedding.NewReleaseAssetFetcher(root, a, "http://127.0.0.1:1")
}
