package embedding

import (
	"os"
	"path/filepath"
)

// LegacyHeadFilenames are head files earlier releases put under the models
// dir (root, intent_heads/, tool_heads/) that nothing loads any more: the
// shipped heads are the ones pinned in release_assets.json (intent r13f1,
// tool r5, interrupt). Only these exact names (plus their download side
// files) are ever deleted by RemoveLegacyHeadFiles.
var LegacyHeadFilenames = []string{
	// intent heads
	"head_mlp_qwen3go_xf.json",
	"head_mlp_qwen3go_xf_avx512.json",
	"head_mlp_qwen3go_xf_avx2.json",
	"head_mlp_qwen3go_GK.json",
	"head_mlp_qwen3go_KG.json",
	"head_mlp_gemmacls_GK.json",
	"head_mlp_gemmacls_KG.json",
	"head_mlp_qwen3go_r7.json", // superseded by r13f1 (2026-10-15)
	// tool heads
	"tool_head_ovr_qwen3go.json",
	"tool_head_ovr_qwen3.json",
	"tool_head_ovr_gemma.json",
}

var legacyHeadDirs = []string{"", "intent_heads", "tool_heads"}

// RemoveLegacyHeadFiles deletes every LegacyHeadFilenames entry (and its
// .part / .tmp / .verified side files) from modelsDir, modelsDir/intent_heads
// and modelsDir/tool_heads. It returns the removed paths. Safe to call on
// every startup; a missing dir or file is not an error.
func RemoveLegacyHeadFiles(modelsDir string) []string {
	if modelsDir == "" {
		return nil
	}
	var removed []string
	for _, sub := range legacyHeadDirs {
		dir := filepath.Join(modelsDir, sub)
		for _, n := range LegacyHeadFilenames {
			base := filepath.Join(dir, n)
			for _, p := range []string{base, base + ".part", base + ".tmp", base + ".verified"} {
				if fi, err := os.Lstat(p); err == nil && !fi.IsDir() && os.Remove(p) == nil {
					removed = append(removed, p)
				}
			}
		}
	}
	return removed
}
