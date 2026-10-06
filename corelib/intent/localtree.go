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
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// LocalFallback resolves a head-uncertain turn among the head's top-k labels
// (e.g. a small local LLM). Optional; nil → fail closed to L2 hint/unknown.
type LocalFallback func(ctx context.Context, text string, candidates []string) (label string, err error)

// errLocalUncertain is deliberately NOT context.DeadlineExceeded / HTTP 5xx
// so isTreeEndpointFailure never opens the skip window for the head.
type errLocalUncertain struct{ msg string }

func (e errLocalUncertain) Error() string { return e.msg }

// LocalTreeFunc returns an LLMClassifyContextFunc that runs head on the
// Layer 2 query vector (or a fresh classification-role embed on miss).
// fb may be nil. Concurrent-safe after LoadLocalHead.
func LocalTreeFunc(emb embedding.Embedder, h *LocalHead, fb LocalFallback) LLMClassifyContextFunc {
	if h == nil {
		return func(context.Context, context.Context, string, string) (string, error) {
			return "", errLocalUncertain{"local head not loaded"}
		}
	}
	return func(ctx, _ context.Context, _ string, userText string) (string, error) {
		v, _, err := IntentQueryEmbedding(ctx, emb, userText)
		if err != nil {
			return "", errLocalUncertain{"embed: " + err.Error()}
		}
		probs, err := h.Predict(v)
		if err != nil {
			return "", errLocalUncertain{"predict: " + err.Error()}
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
				return "", errLocalUncertain{fmt.Sprintf("local head uncertain: %s %.2f < %.2f", label, prob, thr)}
			}
			top := h.TopK(probs, 5)
			cands := make([]string, 0, len(top))
			for _, s := range top {
				cands = append(cands, s.Label)
			}
			picked, ferr := fb(ctx, userText, cands)
			if ferr != nil || picked == "" || picked == "unknown" {
				return "", errLocalUncertain{"local fallback abstained"}
			}
			ok := false
			for _, c := range cands {
				if c == picked {
					ok = true
					break
				}
			}
			if !ok {
				return "", errLocalUncertain{fmt.Sprintf("local fallback out of candidates: %q", picked)}
			}
			b, _ := json.Marshal(map[string]any{"top": []cand{{picked, 0.70, ""}}})
			return string(b), nil
		}
	}
}
