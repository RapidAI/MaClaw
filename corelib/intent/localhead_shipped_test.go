package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Only the shipped r7 head is auto-selected from a head dir; legacy heads
// (xf / GK / KG) are never candidates, so before r7 lands there is no head.
func TestLocalHeadCandidatesOnlyShippedR7(t *testing.T) {
	t.Setenv(LocalHeadEnv, "")
	dir := t.TempDir()
	touch := func(n string) string {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	touch("head_mlp_qwen3go_xf.json")
	touch("head_mlp_qwen3go_GK.json")
	touch("head_mlp_qwen3go_KG.json")
	if got := LocalHeadCandidates(embedding.FamilyQwen3, dir); len(got) != 0 {
		t.Fatalf("legacy heads must not be candidates: %v", got)
	}
	r7 := touch(ShippedLocalHeadName)
	if got := LocalHeadCandidates(embedding.FamilyQwen3, dir); len(got) != 1 || got[0] != r7 {
		t.Fatalf("with r7: %v", got)
	}
	// An explicit file in MACLAW_INTENT_LOCAL_HEAD is still honoured as is.
	other := touch("custom_head.json")
	t.Setenv(LocalHeadEnv, other)
	if got := LocalHeadCandidates(embedding.FamilyQwen3, dir); len(got) != 1 || got[0] != other {
		t.Fatalf("env file: %v", got)
	}
}
