package tensor_test

import (
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestPackQKV4Heads128_MatchesPerHead(t *testing.T) {
	const nFrames, hidden, headDim, nHeads = 5, 512, 128, 4
	scale := float32(0.088388347)
	qkv := make([]float32, 3*hidden)
	for i := range qkv {
		qkv[i] = float32(i%97) * 0.01
	}
	qGot := make([]float32, nHeads*nFrames*headDim)
	kGot := make([]float32, nHeads*nFrames*headDim)
	vGot := make([]float32, nHeads*nFrames*headDim)
	qWant := make([]float32, nHeads*nFrames*headDim)
	kWant := make([]float32, nHeads*nFrames*headDim)
	vWant := make([]float32, nHeads*nFrames*headDim)
	f := 2
	tensor.PackQKV4Heads128(qGot, kGot, vGot, qkv, nFrames, f, scale)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		d := (h*nFrames + f) * headDim
		tensor.PackQKV128(
			qWant[d:d+headDim], kWant[d:d+headDim], vWant[d:d+headDim],
			qkv[hOff:hOff+headDim], qkv[hidden+hOff:hidden+hOff+headDim], qkv[2*hidden+hOff:2*hidden+hOff+headDim],
			scale,
		)
	}
	for i := range qWant {
		if math.Abs(float64(qGot[i]-qWant[i])) > 1e-6 ||
			math.Abs(float64(kGot[i]-kWant[i])) > 1e-6 ||
			math.Abs(float64(vGot[i]-vWant[i])) > 1e-6 {
			t.Fatalf("mismatch at %d: q %v/%v k %v/%v v %v/%v", i, qGot[i], qWant[i], kGot[i], kWant[i], vGot[i], vWant[i])
		}
	}
}
