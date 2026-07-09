package tensor

import (
	"math"
	"testing"
)

func TestElemMul_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	ElemMul(a, a, b)
	want := []float32{20, 60, 120}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestElemMul_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	ElemMul(b, a, b)
	want := []float32{20, 60, 120}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	Add(a, a, b)
	want := []float32{12, 23, 34}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	Add(b, a, b)
	want := []float32{12, 23, 34}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAddBiasGELU_MatchesAddBiasThenGELU(t *testing.T) {
	data := []float32{-2, -1, 0, 1, 2, 3}
	bias := []float32{0.5, -0.25, 1}
	got := append([]float32(nil), data...)
	want := append([]float32(nil), data...)

	AddBiasGELU(got, 2, 3, bias)
	AddBias(want, 2, 3, bias)
	GELU(want)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-6 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestWeightedSumStrided(t *testing.T) {
	weights := []float32{0.25, 0.5, -0.75}
	values := []float32{
		1, 2, 3, 99,
		4, 5, 6, 99,
		7, 8, 9, 99,
	}
	got := make([]float32, 3)
	WeightedSumStrided(got, weights, values, 3, 4, 3)

	want := []float32{-3, -3, -3}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-6 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSoftmaxWeightedSumStrided_MatchesSoftmaxThenWeightedSum(t *testing.T) {
	scores := []float32{-2, 0.5, 3, -1.25}
	values := []float32{
		1, 2, 3, 99,
		4, 5, 6, 99,
		7, 8, 9, 99,
		-1, -2, -3, 99,
	}
	got := make([]float32, 3)
	want := make([]float32, 3)
	gotScores := append([]float32(nil), scores...)
	wantScores := append([]float32(nil), scores...)

	SoftmaxWeightedSumStrided(got, gotScores, values, 4, 4, 3)
	Softmax(wantScores)
	WeightedSumStrided(want, wantScores, values, 4, 4, 3)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSoftmaxWeightedSumBatched8_MatchesSequential(t *testing.T) {
	const rows, dim, nQ = 16, 128, 8
	// Contiguous V [rows][dim]
	values := make([]float32, rows*dim)
	for i := range values {
		values[i] = float32((i%17)-8) * 0.1
	}
	// scores [nQ][rows]
	scoresBatched := make([]float32, nQ*rows)
	scoresSeq := make([]float32, nQ*rows)
	for t := 0; t < nQ; t++ {
		for r := 0; r < rows; r++ {
			v := float32(t-3)*0.2 + float32(r-8)*0.05
			scoresBatched[t*rows+r] = v
			scoresSeq[t*rows+r] = v
		}
	}
	// out: [nQ][dim] with outStride=dim, hOff=0, qf=0
	got := make([]float32, nQ*dim)
	want := make([]float32, nQ*dim)
	SoftmaxWeightedSumBatched(got, scoresBatched, values, nQ, rows, dim, dim, dim, 0, 0)
	for t := 0; t < nQ; t++ {
		SoftmaxWeightedSumStrided(want[t*dim:(t+1)*dim], scoresSeq[t*rows:(t+1)*rows], values, rows, dim, dim)
	}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
			t.Fatalf("idx %d: got %v want %v diff %v", i, got[i], want[i], diff)
		}
	}
}

func TestLayerNorm_MatchesReference(t *testing.T) {
	x := []float32{-1.5, 0.25, 2, -0.75, 1.25, 3.5, -2.25, 0.5}
	weight := []float32{1, 0.5, -1, 2, 1.5, -0.25, 0.75, 1.25}
	got := make([]float32, len(x))
	want := make([]float32, len(x))

	LayerNorm(got, x, weight, 1e-5)
	layerNormReference(want, x, weight, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLayerNorm_InPlaceMatchesReference(t *testing.T) {
	x := []float32{-1.5, 0.25, 2, -0.75, 1.25, 3.5, -2.25, 0.5}
	weight := []float32{1, 0.5, -1, 2, 1.5, -0.25, 0.75, 1.25}
	got := append([]float32(nil), x...)
	want := make([]float32, len(x))

	LayerNorm(got, got, weight, 1e-5)
	layerNormReference(want, x, weight, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestGroupNorm1_MatchesReference(t *testing.T) {
	data := []float32{-1, 2, 0.5, 3, -2, 1.5, 0.25, -0.75, 2.5, -1.5, 1, 0}
	weight := []float32{1, 0.5, -1, 2}
	bias := []float32{0.1, -0.2, 0.3, -0.4}
	got := append([]float32(nil), data...)
	want := append([]float32(nil), data...)

	GroupNorm1(got, 3, 4, weight, bias, 1e-5)
	groupNorm1Reference(want, 3, 4, weight, bias, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func layerNormReference(out, x, weight []float32, eps float32) {
	n := len(x)
	var mean float32
	for _, v := range x {
		mean += v
	}
	mean /= float32(n)
	var variance float32
	for _, v := range x {
		d := v - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for i := range x {
		out[i] = (x[i] - mean) * scale * weight[i]
	}
}

func groupNorm1Reference(data []float32, time, channels int, weight, bias []float32, eps float32) {
	n := time * channels
	var mean float32
	for _, v := range data[:n] {
		mean += v
	}
	mean /= float32(n)
	var variance float32
	for _, v := range data[:n] {
		d := v - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for t := 0; t < time; t++ {
		off := t * channels
		for c := 0; c < channels; c++ {
			v := (data[off+c] - mean) * scale
			if weight != nil {
				v *= weight[c]
			}
			if bias != nil {
				v += bias[c]
			}
			data[off+c] = v
		}
	}
}
