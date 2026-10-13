package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

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
	{
		qa := make([]float32, 13*3072)
		for i := range qa {
			qa[i] = float32(rng.NormFloat64()) * float32(1+i%7)
			if i%97 == 0 {
				qa[i] = 0
			}
		}
		for i := 0; i < 32; i++ {
			qa[i] = 0 // all-zero block
		}
		qd, sd := tensor.Q8PQuantSelfTest(qa, 13, 3072)
		fmt.Printf("quant qdiff=%d sdiff=%d\n", qd, sd)
		if qd != 0 || sd != 0 {
			os.Exit(4)
		}
	}
	if os.Getenv("Q8P_BENCH") != "" {
		for _, sh := range [][3]int{{28, 1024, 1024}, {28, 3072, 1024}, {28, 1024, 3072}, {128, 3072, 1024}, {377, 4096, 1024}, {377, 1024, 3072}, {26, 6144, 1024}} {
			M, N, K := sh[0], sh[1], sh[2]
			w := make([]float32, N*K)
			for i := range w {
				w[i] = float32(rng.NormFloat64()) * 0.05
			}
			wt := tensor.QuantizeToQ8(w, N, K)
			wt.PrepareScales()
			wp := tensor.PackQ8P(wt)
			a := make([]float32, M*K)
			for i := range a {
				a[i] = float32(rng.NormFloat64())
			}
			act := tensor.QuantizeQ8Act(nil, a, M, K)
			out := make([]float32, M*N)
			for _, nw := range []int{1, 0} {
				tensor.MatMulQ8P(out, act, wp, false, nw)
				n := 0
				t0 := time.Now()
				for time.Since(t0) < 500*time.Millisecond {
					tensor.MatMulQ8P(out, act, wp, false, nw)
					n++
				}
				el := time.Since(t0).Seconds() / float64(n)
				fmt.Printf("bench M=%d N=%d K=%d workers=%d: %.3f ms  %.1f GOPS\n", M, N, K, nw, el*1e3, 2*float64(M*N*K)/el/1e9)
			}
		}
	}
	fmt.Println("ok")
}
