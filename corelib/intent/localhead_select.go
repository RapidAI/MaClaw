package intent

// localhead_select.go — pick the local L3 head that matches the live embedder.
//
// A head is only valid in the vector space it was trained on. Selection
// therefore checks both the embedder family (Qwen3 vs Gemma, from the
// embedder's type / ModelID) and the width; a head for the other family, or
// with a different dimension, is refused rather than run on foreign vectors.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Environment for the production local L3 (read by hosts at wiring time).
const (
	// LocalL3Env: unset / "1" / "on" → local head is L3 (default);
	// "0" / "off" / "remote" → old remote tree LLM (rollback);
	// "strict" → local only, never fall back to the remote tree LLM.
	LocalL3Env = "MACLAW_INTENT_LOCAL_L3"
	// LocalHeadEnv: comma-separated head JSON paths (or directories); the one
	// matching the embedder is used. Default: <models dir>/intent_heads/.
	LocalHeadEnv = "MACLAW_INTENT_LOCAL_HEAD"
	// LocalTauSEnv overrides τ_s (sensitive-label threshold).
	LocalTauSEnv = "MACLAW_INTENT_LOCAL_TAU_S"
	// DefaultLocalTauS is the product τ_s for heads fitted offline.
	DefaultLocalTauS = 0.95
	// LocalHeadsDirName is the default head directory under the models dir.
	LocalHeadsDirName = "intent_heads"
)

// Local L3 modes.
const (
	LocalL3On     = "on"
	LocalL3Off    = "off"
	LocalL3Strict = "strict"
)

// LocalL3Mode returns the normalised LocalL3Env value.
func LocalL3Mode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(LocalL3Env))) {
	case "0", "off", "false", "no", "remote":
		return LocalL3Off
	case "strict", "local-only", "local_only":
		return LocalL3Strict
	default:
		return LocalL3On
	}
}

// ErrHeadMismatch marks a head refused for the live embedder.
var ErrHeadMismatch = errors.New("local head does not match embedder")

// HeadFamily is the embedder family a head was trained for: the explicit
// "embed_model" field, else a hint in the head ID / file name, else width.
func HeadFamily(h *LocalHead) string {
	if h == nil {
		return ""
	}
	if f := normFamily(h.EmbedModel); f != "" {
		return f
	}
	id := strings.ToLower(h.ID)
	switch {
	case strings.Contains(id, "qwen3"):
		return embedding.FamilyQwen3
	case strings.Contains(id, "gemma"):
		return embedding.FamilyGemma
	}
	switch h.Dim() {
	case 1024:
		return embedding.FamilyQwen3
	case 768:
		return embedding.FamilyGemma
	}
	return ""
}

func normFamily(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "qwen3"):
		return embedding.FamilyQwen3
	case strings.Contains(s, "gemma"):
		return embedding.FamilyGemma
	}
	return s
}

// CheckLocalHead returns nil when h can run on emb's vectors.
func CheckLocalHead(emb embedding.Embedder, h *LocalHead) error {
	if h == nil {
		return fmt.Errorf("%w: no head", ErrHeadMismatch)
	}
	if emb == nil || embedding.IsNoop(emb) {
		return fmt.Errorf("%w: no embedder", ErrHeadMismatch)
	}
	if emb.Dim() != h.Dim() {
		return fmt.Errorf("%w: head %s dim %d != embedder dim %d", ErrHeadMismatch, h.ID, h.Dim(), emb.Dim())
	}
	ef, hf := embedding.EmbedderFamily(emb), HeadFamily(h)
	if ef != "" && hf != "" && ef != hf {
		return fmt.Errorf("%w: head %s is for %s, embedder is %s", ErrHeadMismatch, h.ID, hf, ef)
	}
	return nil
}

// ShippedLocalHeadName is the intent head pinned in
// corelib/embedding/release_assets.json (r7: 20-net merged MLP, per-class
// temperature folded into w2/b2, τ 0.80 / τ_s 0.97, calibration "oof_rule").
// When it is present it is the only "head_mlp_qwen3go_*" candidate besides
// the GK/KG defaults: legacy "*_xf*.json" heads already on disk are used only
// while it is missing (first run before the download lands, or a failed
// download), so a compute-mode-matched legacy head never outranks it.
const ShippedLocalHeadName = "head_mlp_qwen3go_r7.json"

// DefaultLocalHeadNames lists, in preference order, the shipped head file
// names for a family. Any "*_xf*.json" retrained head for the family found in
// the same directory is preferred over these (newest file name first).
func DefaultLocalHeadNames(family string) []string {
	switch family {
	case embedding.FamilyQwen3:
		return []string{"head_mlp_qwen3go_GK.json", "head_mlp_qwen3go_KG.json"}
	}
	// No shipped heads for any other family: the retired EmbeddingGemma heads
	// are never auto-selected (Qwen3 is the only local model).
	return nil
}

func familyPrefix(family string) string {
	switch family {
	case embedding.FamilyQwen3:
		return "head_mlp_qwen3go_"
	}
	return ""
}

// LocalHeadCandidates expands LocalHeadEnv (or, when unset, defaultDir) into
// an ordered list of head files to try for family.
func LocalHeadCandidates(family, defaultDir string) []string {
	var roots []string
	if v := strings.TrimSpace(os.Getenv(LocalHeadEnv)); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roots = append(roots, p)
			}
		}
	} else if defaultDir != "" {
		roots = []string{defaultDir}
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, r := range roots {
		fi, err := os.Stat(r)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			add(r)
			continue
		}
		if family == embedding.FamilyQwen3 {
			if p := filepath.Join(r, ShippedLocalHeadName); fileExists(p) {
				add(p)
				for _, n := range DefaultLocalHeadNames(family) {
					if p := filepath.Join(r, n); fileExists(p) {
						add(p)
					}
				}
				continue
			}
		}
		if pre := familyPrefix(family); pre != "" {
			xf, _ := filepath.Glob(filepath.Join(r, pre+"xf*.json"))
			sort.Sort(sort.Reverse(sort.StringSlice(xf)))
			for _, p := range xf {
				add(p)
			}
		}
		for _, n := range DefaultLocalHeadNames(family) {
			if p := filepath.Join(r, n); fileExists(p) {
				add(p)
			}
		}
	}
	return out
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// SelectLocalHead loads the first candidate that matches emb (family + dim).
// Mismatching heads are skipped; if none match, the returned error wraps
// ErrHeadMismatch (or reports that no candidate exists).
func SelectLocalHead(emb embedding.Embedder, candidates []string) (*LocalHead, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no local head file found for %s embedder", embedding.EmbedderFamily(emb))
	}
	var errs []string
	var first *LocalHead
	live := embedding.EmbedderComputeMode(emb)
	for _, p := range candidates {
		h, err := LoadLocalHead(p)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := CheckLocalHead(emb, h); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		// τ is pass-mode sensitive: a head recalibrated under the live
		// compute mode wins over earlier candidates calibrated elsewhere.
		if h.CalibrationCompute != "" && h.CalibrationCompute == live {
			return h, nil
		}
		if first == nil {
			first = h
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrHeadMismatch, strings.Join(errs, "; "))
}

// CalibrationSourceOOFRule marks a head whose τ/τ_s were chosen by the r4
// locked out-of-family rule and gated end-to-end at exactly those values;
// production then uses the head's own τ_s (like "human_labels").
const CalibrationSourceOOFRule = "oof_rule"

// ApplyProductTauS sets τ_s for production: LocalTauSEnv if set, else the
// head's own τ_s when it was re-fitted on human labels or carries
// calibration.source "oof_rule", else DefaultLocalTauS. τ_s never drops below τ. Returns the τ_s in force.
func ApplyProductTauS(h *LocalHead) float64 {
	if h == nil {
		return 0
	}
	ts := DefaultLocalTauS
	if (h.CalibrationSource == "human_labels" || h.CalibrationSource == CalibrationSourceOOFRule) && h.TauS > 0 {
		ts = h.TauS
	}
	if v := strings.TrimSpace(os.Getenv(LocalTauSEnv)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			ts = f
		}
	}
	if ts < h.Tau {
		ts = h.Tau
	}
	h.TauS = ts
	return ts
}
