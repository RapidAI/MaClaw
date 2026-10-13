package intent

// LOCAL EVAL HARNESS: determinism / concurrency / soak for embed+head.

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func zzPct(a []float64, q float64) float64 {
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	if len(b) == 0 {
		return 0
	}
	return b[int(math.Min(float64(len(b)-1), q/100*float64(len(b))))]
}

func TestZZHeadStability(t *testing.T) {
	if os.Getenv("ZZ_STAB") == "" {
		t.Skip()
	}
	t0 := time.Now()
	emb := zzEmbedder(t)
	hd := zzLoadHead(t, os.Getenv("ZZ_HEAD"))
	fmt.Fprintf(os.Stderr, "STAB cold start embedder+head load %.0fms rss=%.0fMB\n", float64(time.Since(t0).Microseconds())/1000, zzRSSMB())
	ins := zzReadIn(t, os.Getenv("ZZ_IN"))
	texts := make([]string, len(ins))
	for i, r := range ins {
		texts[i] = r.Text
	}
	run := func(s string) ([]float64, float64) {
		a := time.Now()
		v, err := emb.Embed(s)
		if err != nil {
			t.Fatal(err)
		}
		return hd.Predict(v), float64(time.Since(a).Microseconds()) / 1000
	}
	// 1. determinism (sequential, 3 passes)
	ref := make([][]float64, len(texts))
	var seqLat []float64
	maxDiff := 0.0
	for pass := 0; pass < 3; pass++ {
		for i, s := range texts {
			p, ms := run(s)
			seqLat = append(seqLat, ms)
			if pass == 0 {
				ref[i] = p
				continue
			}
			for j := range p {
				maxDiff = math.Max(maxDiff, math.Abs(p[j]-ref[i][j]))
			}
		}
	}
	fmt.Fprintf(os.Stderr, "STAB determinism: 3 passes x %d texts, max |dp| = %g; seq latency ms p50=%.1f p95=%.1f p99=%.1f\n",
		len(texts), maxDiff, zzPct(seqLat, 50), zzPct(seqLat, 95), zzPct(seqLat, 99))
	// 2. concurrency
	workers, _ := strconv.Atoi(os.Getenv("ZZ_WORKERS"))
	if workers == 0 {
		workers = 8
	}
	var mu sync.Mutex
	var conLat []float64
	mism := 0
	a := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < len(texts); i += workers {
				p, ms := run(texts[i])
				d := 0.0
				for j := range p {
					d = math.Max(d, math.Abs(p[j]-ref[i][j]))
				}
				mu.Lock()
				conLat = append(conLat, ms)
				if d > 1e-9 {
					mism++
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	el := time.Since(a).Seconds()
	fmt.Fprintf(os.Stderr, "STAB concurrency: %d workers, %d calls in %.1fs (%.1f qps), mismatches vs sequential=%d, latency ms p50=%.1f p95=%.1f p99=%.1f\n",
		workers, len(texts), el, float64(len(texts))/el, mism, zzPct(conLat, 50), zzPct(conLat, 95), zzPct(conLat, 99))
	// 3. soak
	n, _ := strconv.Atoi(os.Getenv("ZZ_SOAK"))
	runtime.GC()
	r0 := zzRSSMB()
	var soakLat []float64
	a = time.Now()
	for k := 0; k < n; k++ {
		s := texts[k%len(texts)] + " #" + strconv.Itoa(k) // defeat any cache
		_, ms := run(s)
		soakLat = append(soakLat, ms)
		if k%1000 == 0 {
			fmt.Fprintf(os.Stderr, "STAB soak %d rss=%.0fMB\n", k, zzRSSMB())
		}
	}
	runtime.GC()
	fmt.Fprintf(os.Stderr, "STAB soak: %d calls %.0fs, rss %.0f -> %.0f MB, latency ms p50=%.1f p95=%.1f p99=%.1f max=%.1f\n",
		n, time.Since(a).Seconds(), r0, zzRSSMB(), zzPct(soakLat, 50), zzPct(soakLat, 95), zzPct(soakLat, 99), zzPct(soakLat, 100))
}
