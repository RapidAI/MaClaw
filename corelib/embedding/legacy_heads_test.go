package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLegacyHeadFilesOnlyKnownNames(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	legacy := []string{
		mk("intent_heads/head_mlp_qwen3go_xf.json"),
		mk("intent_heads/head_mlp_qwen3go_xf.json.verified"),
		mk("intent_heads/head_mlp_qwen3go_GK.json"),
		mk("tool_heads/tool_head_ovr_qwen3go.json"),
		mk("tool_head_ovr_qwen3go.json"),
	}
	keep := []string{
		mk("intent_heads/head_mlp_qwen3go_r7.json"),
		mk("intent_heads/head_mlp_qwen3go_xf_custom.json"),
		mk("tool_heads/tool_head_ovr_qwen3go_r5.json"),
		mk("interrupt_heads/interrupt_relevance_qwen3go.json"),
	}
	got := RemoveLegacyHeadFiles(dir)
	if len(got) != len(legacy) {
		t.Fatalf("removed %v", got)
	}
	for _, p := range legacy {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("legacy file kept: %s", p)
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("non-legacy file removed: %s", p)
		}
	}
	for _, a := range ReleaseAssets() {
		for _, n := range LegacyHeadFilenames {
			if a.Filename == n {
				t.Fatalf("%s is both a release asset and legacy", n)
			}
		}
	}
}
