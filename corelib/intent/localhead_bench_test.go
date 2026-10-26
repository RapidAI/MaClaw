package intent

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// shippedHeadPath returns the installed r13 head, or "" when the model slot is
// absent (CI). It mirrors what the GUI resolves at startup.
func shippedHeadPath(tb testing.TB) string {
	tb.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"head_mlp_qwen3go_r13f1.json", "head_mlp_qwen3go_GK.json"} {
		p := filepath.Join(home, ".maclaw", "models", "intent_heads", name)
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			return p
		}
	}
	return ""
}

// BenchmarkLocalHeadPredict measures one prediction through the configured
// path (float32 or int8 per MACLAW_HEAD_Q8P and the auto policy). The shipped
// 2560×2048 head is used when installed, otherwise an equivalent synthetic one
// so the benchmark still runs on CI.
func BenchmarkLocalHeadPredict(b *testing.B) {
	// The sub-benchmark's *testing.B must be passed in: the outer b runs one
	// iteration, so a closure over the outer b reports time/N as near zero.
	bench := func(b *testing.B, h *LocalHead, dim int) {
		b.Helper()
		r := rand.New(rand.NewSource(3))
		v := make([]float32, dim)
		for i := range v {
			v[i] = float32(r.NormFloat64()) / float32(dim/32)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			if _, err := h.Predict(v); err != nil {
				b.Fatal(err)
			}
		}
	}
	if p := shippedHeadPath(b); p != "" {
		h, err := LoadLocalHead(p)
		if err != nil {
			b.Fatal(err)
		}
		dim := h.Dim()
		if n := len(h.InputBlocks); n > 1 {
			dim *= n
		}
		b.Run("shipped", func(b *testing.B) { bench(b, h, dim) })
		return
	}
	// Synthetic head with the shipped shape: dim 2048, hidden 2560.
	h, err := ParseLocalHead(tinyHeadJSON(b, []string{"a", "b", "c"}, 2048, 2560, 0.5, 0.8, nil))
	if err != nil {
		b.Fatal(err)
	}
	b.Run("synthetic", func(b *testing.B) { bench(b, h, h.Dim()) })
}

// BenchmarkLocalHeadPredictFloat is the float32 reference for the same head,
// so the int8 gain is visible in one `go test -bench` run.
func BenchmarkLocalHeadPredictFloat(b *testing.B) {
	b.Setenv("MACLAW_HEAD_Q8P", "0")
	BenchmarkLocalHeadPredict(b)
}
