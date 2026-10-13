package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// The shipped r7 head, when present, outranks any legacy *_xf*.json head on
// disk; the legacy head is only a fallback while r7 is missing.
func TestLocalHeadCandidatesPreferShippedR7(t *testing.T) {
	t.Setenv(LocalHeadEnv, "")
	dir := t.TempDir()
	touch := func(n string) string {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	xf := touch("head_mlp_qwen3go_xf.json")
	if got := LocalHeadCandidates(embedding.FamilyQwen3, dir); len(got) != 1 || got[0] != xf {
		t.Fatalf("without r7: %v", got)
	}
	r7 := touch(ShippedLocalHeadName)
	if got := LocalHeadCandidates(embedding.FamilyQwen3, dir); len(got) != 1 || got[0] != r7 {
		t.Fatalf("with r7: %v", got)
	}
}
