// embbench: latency / throughput / concurrency benchmark for the local GGUF
// embedders (EmbeddingGemma or Qwen3-Embedding, picked by GGUF architecture).
// Every query goes through the production role path (EmbedAs, default
// RoleClassification = the intent Layer 2 regime). Local evaluation tool.
//
//	embbench -model X.gguf -texts short.txt,long.txt -mode latency|batch|conc [-n 200] [-dur 150s] [-workers 8]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func readLines(p string) []string {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func loadavg() string {
	b, _ := os.ReadFile("/proc/loadavg")
	f := strings.Fields(string(b))
	if len(f) >= 3 {
		return strings.Join(f[:3], " ")
	}
	return "?"
}

func rssMB() float64 {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			kb, _ := strconv.ParseFloat(f[1], 64)
			return kb / 1024
		}
	}
	return 0
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(math.Ceil(p/100*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return s[i]
}

func summary(lat []float64) map[string]float64 {
	var sum float64
	for _, v := range lat {
		sum += v
	}
	return map[string]float64{"n": float64(len(lat)), "mean_ms": sum / float64(len(lat)), "p50_ms": pct(lat, 50), "p95_ms": pct(lat, 95), "p99_ms": pct(lat, 99), "max_ms": pct(lat, 100)}
}

func main() {
	model := flag.String("model", "", "GGUF")
	texts := flag.String("texts", "", "comma-separated text files (one text per line)")
	mode := flag.String("mode", "latency", "latency|batch|conc|load")
	n := flag.Int("n", 200, "latency: calls per file")
	dur := flag.Duration("dur", 150*time.Second, "conc: duration")
	workers := flag.Int("workers", 8, "conc: goroutines")
	batch := flag.Int("batch", 64, "batch: texts per EmbedBatch call")
	rounds := flag.Int("rounds", 3, "batch: rounds")
	roleS := flag.String("role", "classification", "classification|query|document|none")
	cpuProf := flag.String("cpuprofile", "", "write a CPU profile of the measured phase to this file")
	flag.Parse()
	role := map[string]embedding.Role{"classification": embedding.RoleClassification, "query": embedding.RoleQuery,
		"document": embedding.RoleDocument, "none": embedding.RoleNone}[*roleS]
	res := map[string]any{"model": *model, "mode": *mode, "role": *roleS, "gomaxprocs": runtime.GOMAXPROCS(0), "load_before": loadavg()}
	r0 := rssMB()
	t0 := time.Now()
	emb, err := embedding.NewEmbedderFromFile(*model, embedding.DefaultEmbeddingDim)
	if err != nil {
		panic(err)
	}
	res["load_ms"] = float64(time.Since(t0).Microseconds()) / 1000
	res["rss_after_load_mb"] = rssMB()
	res["rss_before_load_mb"] = r0
	if m, ok := emb.(interface{ ModelID() string }); ok {
		res["model_id"] = m.ModelID()
	}
	t1 := time.Now()
	if _, err := embedding.EmbedAs(emb, "warm up", role); err != nil {
		panic(err)
	}
	res["first_embed_ms"] = float64(time.Since(t1).Microseconds()) / 1000
	var files []string
	if *texts != "" {
		files = strings.Split(*texts, ",")
	}
	if tk, ok := emb.(interface{ Tokenize(string) []int }); ok {
		for _, fp := range files {
			var toks []float64
			for _, t := range readLines(fp) {
				toks = append(toks, float64(len(tk.Tokenize(t))))
			}
			res["tokens:"+fp] = map[string]float64{"n": float64(len(toks)), "p50": pct(toks, 50), "p95": pct(toks, 95), "max": pct(toks, 100), "min": pct(toks, 0)}
		}
	}
	if *cpuProf != "" {
		f, err := os.Create(*cpuProf)
		if err != nil {
			panic(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer f.Close()
	}
	switch *mode {
	case "load":
	case "latency":
		for _, fp := range files {
			ts := readLines(fp)
			for i := 0; i < 3; i++ { // warm
				_, _ = embedding.EmbedAs(emb, ts[i%len(ts)], role)
			}
			la := loadavg()
			var lat []float64
			for i := 0; i < *n; i++ {
				s := time.Now()
				if _, err := embedding.EmbedAs(emb, ts[i%len(ts)], role); err != nil {
					panic(err)
				}
				lat = append(lat, float64(time.Since(s).Microseconds())/1000)
			}
			sm := summary(lat)
			var chars float64
			for _, t := range ts {
				chars += float64(len([]rune(t)))
			}
			res["latency:"+fp] = map[string]any{"stats": sm, "avg_chars": chars / float64(len(ts)), "load_start": la, "load_end": loadavg()}
		}
	case "batch":
		for _, fp := range files {
			ts := readLines(fp)
			b := ts
			for len(b) < *batch {
				b = append(b, ts...)
			}
			b = b[:*batch]
			_, _ = embedding.EmbedBatchAs(emb, b[:8], role)
			la := loadavg()
			var tps []float64
			for r := 0; r < *rounds; r++ {
				s := time.Now()
				if _, err := embedding.EmbedBatchAs(emb, b, role); err != nil {
					panic(err)
				}
				tps = append(tps, float64(len(b))/time.Since(s).Seconds())
			}
			res["batch:"+fp] = map[string]any{"texts_per_s": tps, "batch": len(b), "load_start": la, "load_end": loadavg()}
		}
	case "conc":
		var ts []string
		for _, fp := range files {
			ts = append(ts, readLines(fp)...)
		}
		ref := map[int][]float32{}
		for i := 0; i < 4; i++ {
			v, _ := embedding.EmbedAs(emb, ts[i], role)
			ref[i] = v
		}
		la := loadavg()
		var mu sync.Mutex
		var lat []float64
		var errs, mism, done int64
		stop := time.Now().Add(*dur)
		var maxRSS float64
		var wg sync.WaitGroup
		go func() {
			for time.Now().Before(stop) {
				if r := rssMB(); r > maxRSS {
					maxRSS = r
				}
				time.Sleep(time.Second)
			}
		}()
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				i := w
				for time.Now().Before(stop) {
					k := i % len(ts)
					s := time.Now()
					v, err := embedding.EmbedAs(emb, ts[k], role)
					d := float64(time.Since(s).Microseconds()) / 1000
					if err != nil {
						atomic.AddInt64(&errs, 1)
					} else if r, ok := ref[k]; ok {
						for j := range r {
							if r[j] != v[j] {
								atomic.AddInt64(&mism, 1)
								break
							}
						}
					}
					atomic.AddInt64(&done, 1)
					mu.Lock()
					lat = append(lat, d)
					mu.Unlock()
					i += *workers
				}
			}(w)
		}
		st := time.Now()
		wg.Wait()
		el := time.Since(st).Seconds()
		res["conc"] = map[string]any{"workers": *workers, "seconds": el, "calls": done, "calls_per_s": float64(done) / el, "errors": errs,
			"nondeterministic": mism, "lat": summary(lat), "max_rss_mb": maxRSS, "load_start": la, "load_end": loadavg()}
	}
	if *cpuProf != "" {
		pprof.StopCPUProfile()
	}
	res["rss_end_mb"] = rssMB()
	res["load_after"] = loadavg()
	b, _ := json.MarshalIndent(res, "", " ")
	fmt.Println(string(b))
}
