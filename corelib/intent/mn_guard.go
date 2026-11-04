package intent

// mn_guard.go — Mongolian (Cyrillic) confidence + sensitive guard.
//
// The Cyrillic bypass (script_bypass.go) sends every Cyrillic message to the
// remote L3. The MN guard head (round-1 control C_qo, 5-seed ensemble, TC
// calibration, exported by scripts/export_mn_guard_head.py) passed its
// pre-registered gates on Go-Q8 features of the sealed MN_test set
// (τ=.971: coverage 28.9%, acc@accepted .953, sensitive mis-exposure 0;
// research repo STATUS_mn_conf_guard 2003fda / REPORT e5783de). With that head
// attached, a Cyrillic message is decided locally only when the calibrated
// top-1 probability is ≥ τ AND the label is not sensitive; everything else
// still goes to the remote L3. Permissions and confirmations for locally
// accepted Mongolian requests are exactly those of zh/en (no MN-only prompt).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// MNGuardHeadName is the bundled head file, next to the other intent heads.
const MNGuardHeadName = "head_mlp_qwen3go_mn_guard.json"

// MNGuardTau is the pre-registered acceptance threshold (frozen).
const MNGuardTau = 0.971

// LoadMNGuardHead loads and validates the MN guard head from dir.
func LoadMNGuardHead(dir string) (*LocalHead, error) {
	h, err := LoadLocalHead(filepath.Join(dir, MNGuardHeadName))
	if err != nil {
		return nil, err
	}
	if h.Tau < MNGuardTau {
		return nil, fmt.Errorf("mn guard head %s: tau %.3f < %.3f", h.ID, h.Tau, MNGuardTau)
	}
	if h.TauS <= 1 {
		return nil, fmt.Errorf("mn guard head %s: tau_s %.3f must exceed 1 (sensitive labels never local)", h.ID, h.TauS)
	}
	if len(h.sensitive) == 0 || len(h.InputBlocks) != 2 {
		return nil, fmt.Errorf("mn guard head %s: needs sensitive list and last/mean input blocks", h.ID)
	}
	return h, nil
}

// MNGuardHeadAvailable reports whether dir holds the MN guard head.
func MNGuardHeadAvailable(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, MNGuardHeadName))
	return err == nil
}

// MNGuardDecide applies the guard: accept iff top-1 p ≥ τ and not sensitive.
func MNGuardDecide(h *LocalHead, probs []float64) (label string, prob float64, accept bool) {
	best := 0
	for i, p := range probs {
		if p > probs[best] {
			best = i
		}
	}
	label, prob = h.Labels[best], probs[best]
	accept = prob >= h.Tau && prob >= MNGuardTau && !h.IsSensitive(label)
	return label, prob, accept
}

// MNGuardVerdict runs the guard head on text: top-1 label, its calibrated
// probability, and whether the guard would accept it locally.
func MNGuardVerdict(ctx context.Context, emb embedding.Embedder, h *LocalHead, text string) (label string, prob float64, accept bool, err error) {
	if h == nil || emb == nil {
		return "", 0, false, errLocalUncertain{msg: "mn guard head not loaded"}
	}
	v, scores, err := HeadQueryInput(ctx, emb, h, text)
	if err != nil {
		return "", 0, false, errLocalUncertain{msg: "embed: " + err.Error()}
	}
	probs, err := h.PredictWithAnchors(v, scores)
	if err != nil {
		return "", 0, false, errLocalUncertain{msg: "predict: " + err.Error()}
	}
	label, prob, accept = MNGuardDecide(h, probs)
	return label, prob, accept, nil
}

// MNGuardTreeFunc is the L3 callback for Cyrillic input: local tree output on
// a guard accept, otherwise a local-head-uncertain error (caller → remote L3).
func MNGuardTreeFunc(emb embedding.Embedder, h *LocalHead) LLMClassifyContextFunc {
	return func(ctx, _ context.Context, _ string, userText string) (string, error) {
		label, prob, ok, err := MNGuardVerdict(ctx, emb, h, userText)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", NewLocalHeadUncertainError(label, prob, h.Threshold(label))
		}
		top := label
		if label == string(LabelUnknown) {
			top = string(LabelAmbiguous) // same as LocalTreeFunc for confident chit-chat
		}
		b, _ := json.Marshal(map[string]any{"top": []map[string]any{{"skill": top, "score": prob, "workflow_type": ""}}})
		return string(b), nil
	}
}

// MNLocalEnv selects what the MN guard head does for Cyrillic input:
//
//	shadow (default) — remote L3 decides; the guard verdict is only logged
//	on               — guard accepts locally (p ≥ τ, non-sensitive), else remote
//	off              — guard not run at all
const MNLocalEnv = "MACLAW_INTENT_MN_LOCAL"

const (
	MNLocalShadow = "shadow"
	MNLocalOn     = "on"
	MNLocalOff    = "off"
)

// MNLocalMode reads MNLocalEnv (default shadow).
func MNLocalMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(MNLocalEnv))) {
	case "on", "1", "true", "yes", "local":
		return MNLocalOn
	case "off", "0", "false", "no":
		return MNLocalOff
	default:
		return MNLocalShadow
	}
}
