package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func main() {
	fmt.Printf("Q8PAvailable=%v ISA=%s\n", tensor.Q8PAvailable(), tensor.Q8PISAName())
	rng := rand.New(rand.NewSource(1))
	K, N := 1024, 64
	w := make([]float32, N*K)
	for i := range w {
		w[i] = float32(rng.NormFloat64()) * 0.05
	}
	wt := tensor.QuantizeToQ8(w, N, K)
	wt.PrepareScales()
	wp := tensor.PackQ8P(wt)
	fmt.Printf("Signed=%v\n", wp.Signed)
	M := 7
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32(rng.NormFloat64())
	}
	act := tensor.QuantizeQ8Act(nil, a, M, K)
	out := make([]float32, M*N)
	tensor.MatMulQ8P(out, act, wp, false, 1)
	ref := make([]float32, M*N)
	tensor.MatMulQ8N(ref, a, wt, M, N, K, 1)
	var se, sr float64
	for i := range ref {
		d := float64(out[i] - ref[i])
		se += d * d
		sr += float64(ref[i]) * float64(ref[i])
	}
	rel := math.Sqrt(se / sr)
	fmt.Printf("relFro=%.3e\n", rel)
	if rel > 0.02 {
		os.Exit(2)
	}
	maxRel := 0.0
	for p := 0; p < 2; p++ {
		for m0 := 0; m0 < M; m0 += 4 {
			if r := tensor.Q8PSelfTestTile(wp, act, m0, p); r > maxRel {
				maxRel = r
			}
		}
	}
	fmt.Printf("tile_max_rel=%.3e\n", maxRel)
	if maxRel > 1e-5 {
		os.Exit(3)
	}
	if tensor.Q8PAvailable() {
		fmt.Println("sdot_path_used")
	}
	fmt.Println("ok")
}
