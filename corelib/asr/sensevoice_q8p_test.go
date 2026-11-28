package asr

import (
	"math/rand"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestSenseVoiceQ8PDispatchMatchesDirect(t *testing.T) {
	if !tensor.Q8PAvailable() {
		t.Skip("no Q8P kernel on this CPU")
	}
	const M, N, K = 5, 64, 96
	rng := rand.New(rand.NewSource(11))
	raw := make([]float32, N*K)
	a := make([]float32, M*K)
	bias := make([]float32, N)
	for i := range raw {
		raw[i] = float32(rng.NormFloat64()) * 0.05
	}
	for i := range a {
		a[i] = float32(rng.NormFloat64())
	}
	for i := range bias {
		bias[i] = float32(rng.Float64())
	}
	q := tensor.QuantizeToQ8(raw, N, K)
	q.PrepareScales()
	w := linearWeight{q8: q, q8p: tensor.PackQ8P(q), rows: N}
	if w.q8p == nil {
		t.Fatal("pack failed")
	}
	got := make([]float32, M*N)
	direct := make([]float32, M*N)
	matMulLinearBias(got, a, w, bias, M, N, K)
	if !tensor.MatMulQ8PBias(direct, a, w.q8p, bias, M, N, K, false, false) {
		t.Fatal("direct q8p declined")
	}
	for i := range got {
		if got[i] != direct[i] {
			t.Fatalf("dispatch diverged at %d: got %g direct %g", i, got[i], direct[i])
		}
	}

	// A weight without the repack must stay on the float-activation kernel.
	plain := linearWeight{q8: q, rows: N}
	floatOut := make([]float32, M*N)
	ref := make([]float32, M*N)
	matMulLinearBias(floatOut, a, plain, bias, M, N, K)
	tensor.MatMulQ8Bias(ref, a, q, bias, M, N, K)
	for i := range floatOut {
		if floatOut[i] != ref[i] {
			t.Fatalf("unpacked path diverged at %d: got %g ref %g", i, floatOut[i], ref[i])
		}
	}
}
