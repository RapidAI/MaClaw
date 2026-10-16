package intent

// LOCAL EVAL HARNESS: timed concurrent soak for embed+head (pure Go path).

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestZZHeadConcSoak(t *testing.T) {
	if os.Getenv("ZZ_CSOAK_SEC") == "" {
		t.Skip()
	}
	sec, _ := strconv.Atoi(os.Getenv("ZZ_CSOAK_SEC"))
	workers, _ := strconv.Atoi(os.Getenv("ZZ_WORKERS"))
	if workers == 0 {
		workers = 16
	}
	emb := zzEmbedder(t)
	hd := zzLoadHead(t, os.Getenv("ZZ_HEAD"))
	ins := zzReadIn(t, os.Getenv("ZZ_IN"))
	texts := make([]string, len(ins))
	for i, r := range ins {
		texts[i] = r.Text
	}
	// reference outputs for probe texts (exact repeats, determinism under load)
	probes := texts[:20]
	ref := make([][]float64, len(probes))
	for i, s := range probes {
		v, err := emb.Embed(s)
		if err != nil {
			t.Fatal(err)
		}
		ref[i] = hd.Predict(v)
	}
	runtime.GC()
	r0 := zzRSSMB()
	fmt.Fprintf(os.Stderr, "CSOAK start workers=%d sec=%d rss=%.0fMB goroutines=%d\n", workers, sec, r0, runtime.NumGoroutine())
	var mu sync.Mutex
	var lat []float64
	var calls, errs, probeRuns, probeMism, seq int64
	deadline := time.Now().Add(time.Duration(sec) * time.Second)
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		st := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				fmt.Fprintf(os.Stderr, "CSOAK t=%.0fs calls=%d errs=%d rss=%.0fMB goroutines=%d\n", time.Since(st).Seconds(), atomic.LoadInt64(&calls), atomic.LoadInt64(&errs), zzRSSMB(), runtime.NumGoroutine())
			}
		}
	}()
	a := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var local []float64
			for time.Now().Before(deadline) {
				k := atomic.AddInt64(&seq, 1)
				isProbe := k%10 == 0
				var s string
				if isProbe {
					s = probes[int(k/10)%len(probes)]
				} else {
					s = texts[int(k)%len(texts)] + " #" + strconv.FormatInt(k, 10)
				}
				st := time.Now()
				v, err := emb.Embed(s)
				if err != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				p := hd.Predict(v)
				local = append(local, float64(time.Since(st).Microseconds())/1000)
				atomic.AddInt64(&calls, 1)
				if isProbe {
					r := ref[int(k/10)%len(probes)]
					d := 0.0
					for j := range p {
						d = math.Max(d, math.Abs(p[j]-r[j]))
					}
					atomic.AddInt64(&probeRuns, 1)
					if d > 1e-9 {
						atomic.AddInt64(&probeMism, 1)
					}
				}
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	close(stop)
	el := time.Since(a).Seconds()
	runtime.GC()
	fmt.Fprintf(os.Stderr, "CSOAK done: workers=%d %.0fs calls=%d (%.1f qps) errs=%d probe_repeats=%d probe_mismatch=%d rss %.0f -> %.0f MB, latency ms p50=%.1f p95=%.1f p99=%.1f max=%.1f goroutines=%d\n",
		workers, el, calls, float64(calls)/el, errs, probeRuns, probeMism, r0, zzRSSMB(), zzPct(lat, 50), zzPct(lat, 95), zzPct(lat, 99), zzPct(lat, 100), runtime.NumGoroutine())
}
