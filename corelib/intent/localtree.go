package intent

// localtree.go — production adapter: LocalHead as Layer 3 via SetLLMContextFunc.
//
//	head, err := intent.LoadLocalHead(path)
//	uic.SetLLMContextFunc(intent.LocalTreeFunc(embedder, head, nil))
//
// Uses the same tree-protocol JSON as the remote L3 so every ClassifyContext
// guard (weak-verdict retention, local contradiction check, composite
// synthesis, fail-closed L2 hint) still applies. Reuses the Layer 2 query
// vector via IntentQueryEmbedding (no second embed on the upgrade path).
// Uncertain / fallback failures return a non-DeadlineExceeded error so the
// 10 s tree-endpoint skip window is never armed for a healthy local head.
//
// Shadow mode (shadow.go) stays env-gated off by default; this adapter is
// the production "local L3" switch. Pure Go, CGO-free.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// LocalFallback resolves a head-uncertain turn among the head's top-k labels
// (e.g. a small local LLM). Optional; nil → fail closed to L2 hint/unknown.
type LocalFallback func(ctx context.Context, text string, candidates []string) (label string, err error)

// errLocalUncertain is deliberately NOT context.DeadlineExceeded / HTTP 5xx
// so isTreeEndpointFailure never opens the skip window for the head.
// label/prob carry the head's (sub-threshold) top-1 so ClassifyContext can
// veto a weak L2 hint the head disagrees with (head_hint_fix.go).
type errLocalUncertain struct {
	msg   string
	label string
	prob  float64
}

func (e errLocalUncertain) Error() string { return e.msg }

// NewLocalHeadUncertainError is the error a local-head L3 returns when its
// top-1 is below threshold. It carries the head's top label so the classifier
// can apply the head/L2 hint fix; it is never a DeadlineExceeded / 5xx, so the
// tree-endpoint skip window stays closed. Exported for alternative local-head
// adapters (audit harnesses).
func NewLocalHeadUncertainError(label string, prob, threshold float64) error {
	return errLocalUncertain{msg: fmt.Sprintf("local head uncertain: %s %.2f < %.2f", label, prob, threshold), label: label, prob: prob}
}

// LocalHeadUncertainTop reports the head's sub-threshold top-1 carried by a
// local-head uncertain error (ok=false for any other error, including a
// remote tree failure or a head that never produced probabilities).
func LocalHeadUncertainTop(err error) (label string, prob float64, ok bool) {
	var e errLocalUncertain
	if errors.As(err, &e) && e.label != "" {
		return e.label, e.prob, true
	}
	return "", 0, false
}

// LocalHeadProbeFunc returns the local head's top-1 for text without
// applying any threshold: label, its probability, and the head's strong
// (sensitive) threshold max(Tau, TauS). ok=false when no head is attached or
// the embed/predict fails. Used by the head/L2 composite override.
type LocalHeadProbeFunc func(ctx context.Context, text string) (label string, prob, strongThreshold float64, ok bool)

// LocalHeadProbe builds a LocalHeadProbeFunc on the same query vector the
// Layer 2 pass produced (IntentQueryEmbedding reuses it from ctx).
func LocalHeadProbe(emb embedding.Embedder, h *LocalHead) LocalHeadProbeFunc {
	if h == nil || emb == nil {
		return nil
	}
	return func(ctx context.Context, text string) (string, float64, float64, bool) {
		v, _, err := IntentQueryEmbedding(ctx, emb, text)
		if err != nil {
			return "", 0, 0, false
		}
		probs, err := h.Predict(v)
		if err != nil {
			return "", 0, 0, false
		}
		top := h.TopK(probs, 1)
		if len(top) == 0 {
			return "", 0, 0, false
		}
		strong := h.Tau
		if h.TauS > strong {
			strong = h.TauS
		}
		return top[0].Label, top[0].Prob, strong, true
	}
}

// LocalTreeFunc returns an LLMClassifyContextFunc that runs head on the
// Layer 2 query vector (or a fresh classification-role embed on miss).
// fb may be nil. Concurrent-safe after LoadLocalHead.
func LocalTreeFunc(emb embedding.Embedder, h *LocalHead, fb LocalFallback) LLMClassifyContextFunc {
	if h == nil {
		return func(context.Context, context.Context, string, string) (string, error) {
			return "", errLocalUncertain{msg: "local head not loaded"}
		}
	}
	return func(ctx, _ context.Context, _ string, userText string) (string, error) {
		v, _, err := IntentQueryEmbedding(ctx, emb, userText)
		if err != nil {
			return "", errLocalUncertain{msg: "embed: " + err.Error()}
		}
		probs, err := h.Predict(v)
		if err != nil {
			return "", errLocalUncertain{msg: "predict: " + err.Error()}
		}
		label, prob, decision, thr := h.Decide(probs)
		type cand struct {
			Skill string  `json:"skill"`
			Score float64 `json:"score"`
			WF    string  `json:"workflow_type"`
		}
		switch decision {
		case LocalHeadAccept:
			top := h.TopK(probs, 3)
			cs := make([]cand, 0, 3)
			for _, s := range top {
				if s.Label == "unknown" {
					continue
				}
				if len(cs) > 0 && s.Prob < 0.10 {
					break
				}
				cs = append(cs, cand{s.Label, s.Prob, ""})
			}
			if len(cs) == 0 {
				// Confident capability accept with only "unknown" in top-k
				// should not happen; treat as ambiguous to fail closed.
				cs = append(cs, cand{string(LabelAmbiguous), prob, ""})
			}
			b, _ := json.Marshal(map[string]any{"top": cs})
			return string(b), nil
		case LocalHeadUnknown:
			// Confident chit-chat: report ambiguous so a weak tree verdict
			// cannot promote a near-zero runner-up over an L2 capability hint.
			b, _ := json.Marshal(map[string]any{"top": []cand{{string(LabelAmbiguous), prob, ""}}})
			return string(b), nil
		default: // LocalHeadAmbiguous
			if fb == nil {
				return "", NewLocalHeadUncertainError(label, prob, thr)
			}
			top := h.TopK(probs, 5)
			cands := make([]string, 0, len(top))
			for _, s := range top {
				cands = append(cands, s.Label)
			}
			picked, ferr := fb(ctx, userText, cands)
			if ferr != nil || picked == "" || picked == "unknown" {
				return "", errLocalUncertain{msg: "local fallback abstained"}
			}
			ok := false
			for _, c := range cands {
				if c == picked {
					ok = true
					break
				}
			}
			if !ok {
				return "", errLocalUncertain{msg: fmt.Sprintf("local fallback out of candidates: %q", picked)}
			}
			b, _ := json.Marshal(map[string]any{"top": []cand{{picked, 0.70, ""}}})
			return string(b), nil
		}
	}
}
