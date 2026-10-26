// Package locall3 is the host-side wiring for the fully local intent path:
// one shared embedder (Qwen3, the only local model; no fallback model) and
// the matching calibrated MLP head as Layer 3 via intent.LocalTreeFunc.
//
// It is pure Go (CGO-free) so the exact wiring guiapp uses can be exercised
// by tests and by cmd/locall3smoke on any platform, independent of the
// GUI's cgo-only files.
//
//	rt := locall3.New(headDir)                    // reads MACLAW_INTENT_LOCAL_L3
//	uic := intent.New(intent.Config{
//	        LLMFunc:        rt.LLMFunc(remoteFn),
//	        LLMContextFunc: rt.ContextFunc(remoteCtxFn), ...})
//	load, _ := locall3.LoadEmbedder(slotPath, true)
//	uic.SetEmbedder(load.Embedder)
//	rt.Attach(load.Embedder)                      // picks + validates the head
package locall3

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// LoadEmbedder is the single shared embedder factory (see
// embedding.LoadPreferredEmbedder).
func LoadEmbedder(slotPath string, smoke bool) (embedding.PreferredLoad, error) {
	return embedding.LoadPreferredEmbedder(slotPath, embedding.PreferredOptions{SmokeTest: smoke})
}

// DefaultHeadDir is <modelsDir>/intent_heads.
func DefaultHeadDir(modelsDir string) string {
	if modelsDir == "" {
		return ""
	}
	return filepath.Join(modelsDir, intent.LocalHeadsDirName)
}

type state struct {
	emb  embedding.Embedder
	head *intent.LocalHead
	fn   intent.LLMClassifyContextFunc
}

// Runtime owns the active (embedder, head) pair. Safe for concurrent use;
// every classifier built from the same Runtime switches atomically when a new
// embedder is attached.
type Runtime struct {
	mode    string
	headDir string
	st      atomic.Pointer[state]
	mu      sync.Mutex
	lastErr error
	// Logf receives per-turn L3 path lines; nil → log.Printf.
	Logf func(string, ...any)
}

func (r *Runtime) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// New reads intent.LocalL3Env once.
func New(headDir string) *Runtime {
	return &Runtime{mode: intent.LocalL3Mode(), headDir: headDir}
}

// NewWithMode is New with an explicit mode (tests).
func NewWithMode(headDir, mode string) *Runtime {
	return &Runtime{mode: mode, headDir: headDir}
}

// Mode is intent.LocalL3On / LocalL3Off / LocalL3Strict.
func (r *Runtime) Mode() string { return r.mode }

// Attach selects and validates the head for emb and makes it the live L3.
// Mode off: no-op. On failure the previous local L3 is dropped (classifiers
// then use the remote tree LLM in mode on, or fail closed in strict mode)
// and the error is returned for logging.
func (r *Runtime) Attach(emb embedding.Embedder) (*intent.LocalHead, error) {
	if r == nil || r.mode == intent.LocalL3Off {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur := r.st.Load(); cur != nil && intent.SameEmbedder(cur.emb, emb) {
		return cur.head, nil
	}
	fam := embedding.EmbedderFamily(emb)
	h, err := intent.SelectLocalHead(emb, intent.LocalHeadCandidates(fam, r.headDir))
	if err != nil {
		r.st.Store(nil)
		r.lastErr = err
		return nil, err
	}
	intent.ApplyProductTauS(h)
	r.st.Store(&state{emb: emb, head: h, fn: intent.LocalTreeFunc(emb, h, nil)})
	r.lastErr = nil
	return h, nil
}

// Detach drops the local L3 (e.g. embedder closed).
func (r *Runtime) Detach() {
	if r != nil {
		r.st.Store(nil)
	}
}

// Active returns the live head and embedder (nil, nil when not attached).
func (r *Runtime) Active() (*intent.LocalHead, embedding.Embedder) {
	if r == nil {
		return nil, nil
	}
	if s := r.st.Load(); s != nil {
		return s.head, s.emb
	}
	return nil, nil
}

// ProbeFunc returns the head/L2 composite-override probe for
// intent.Config.LocalHeadProbe / SetLocalHeadProbe. It reads the live
// attachment on every call (ok=false while no head is attached); mode off
// returns nil so the classifier never probes.
func (r *Runtime) ProbeFunc() intent.LocalHeadProbeFunc {
	if r == nil || r.mode == intent.LocalL3Off {
		return nil
	}
	return func(ctx context.Context, text string) (string, float64, float64, bool) {
		s := r.st.Load()
		if s == nil || s.head == nil {
			return "", 0, 0, false
		}
		if p := intent.LocalHeadProbe(s.emb, s.head); p != nil {
			return p(ctx, text)
		}
		return "", 0, 0, false
	}
}

// ErrNotReady is returned in strict mode before a head is attached. It is
// not a DeadlineExceeded / 5xx so the tree-endpoint skip window stays closed.
var ErrNotReady = errors.New("local L3 not ready (embedder or head not loaded)")

// ContextFunc returns the L3 callback for intent.Config.LLMContextFunc /
// SetLLMContextFunc. Off → remote unchanged. On → local head when attached
// (remote tree LLM when the head abstains; head error kept if the remote
// fails), otherwise remote. Strict → local head or ErrNotReady (never remote).
func (r *Runtime) ContextFunc(remote intent.LLMClassifyContextFunc) intent.LLMClassifyContextFunc {
	if r == nil || r.mode == intent.LocalL3Off {
		return remote
	}
	strict := r.mode == intent.LocalL3Strict
	return func(ctx, parent context.Context, sys, user string) (string, error) {
		if s := r.st.Load(); s != nil {
			out, err := s.fn(ctx, parent, sys, user)
			if err == nil {
				r.logf("[intent] L3 decided by local head (%s)", s.head.ID)
				return out, nil
			}
			if strict || remote == nil || !intent.IsLocalHeadUncertain(err) {
				return "", err
			}
			// Local head abstained: ask the remote tree LLM. If the remote
			// is unavailable, return the head's uncertain error so the
			// classifier keeps its L2-hint / unknown behaviour (and the
			// head veto) exactly as without a remote.
			rout, rerr := remote(ctx, parent, sys, user)
			if rerr != nil {
				r.logf("[intent] L3 local head abstained (%v); remote fallback failed: %v", err, rerr)
				return "", err
			}
			r.logf("[intent] L3 decided by remote fallback (local head abstained: %v)", err)
			return rout, nil
		}
		if strict || remote == nil {
			return "", ErrNotReady
		}
		return remote(ctx, parent, sys, user)
	}
}

// LLMFunc returns the non-context L3 callback. ClassifyByTreeContext prefers
// the context callback, so this is only reached by callers that never set
// one; strict mode removes the remote path entirely.
func (r *Runtime) LLMFunc(remote intent.LLMClassifyFunc) intent.LLMClassifyFunc {
	if r != nil && r.mode == intent.LocalL3Strict {
		return nil
	}
	return remote
}

// Describe is a one-line status for logs.
func (r *Runtime) Describe() string {
	if r == nil {
		return "local L3: nil runtime"
	}
	if r.mode == intent.LocalL3Off {
		return fmt.Sprintf("local L3 off (%s=0): remote tree LLM is L3", intent.LocalL3Env)
	}
	if s := r.st.Load(); s != nil {
		cal := s.head.CalibrationCompute
		if cal == "" {
			cal = "offline"
		}
		warn := ""
		if live := embedding.EmbedderComputeMode(s.emb); s.head.CalibrationCompute != "" && s.head.CalibrationCompute != live {
			warn = " note: head calibrated under a different compute mode (decisions within ~0.01 of tau may differ; r7 avx2:1pass vs avx512:2pass: 13/1320 accept flips)"
		}
		return fmt.Sprintf("local L3 %s: head=%s family=%s dim=%d tau=%.3f tau_s=%.3f embedder=%s compute=%s head_calibrated_for=%s%s",
			r.mode, s.head.ID, intent.HeadFamily(s.head), s.head.Dim(), s.head.Tau, s.head.TauS, embedding.EmbedderFamily(s.emb),
			embedding.EmbedderComputeMode(s.emb), cal, warn)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastErr != nil {
		return fmt.Sprintf("local L3 %s: not attached (%v)", r.mode, r.lastErr)
	}
	return fmt.Sprintf("local L3 %s: waiting for embedder", r.mode)
}
