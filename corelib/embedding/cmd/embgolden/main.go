// Command embgolden produces golden embeddings + timing from the reference
// Go Gemma-embedding implementation, so the C++ port can be diffed against it.
//
// Usage:
//
//	embgolden -model <path.gguf> -dim 768 -bench 10 -out golden.bin
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

var corpus = []string{
	"hello world",
	"你好世界",
	"embedding test",
	"The quick brown fox jumps over the lazy dog.",
	"Machine learning models convert text into dense vector representations.",
	"A cat sits on the mat.",
	"A dog sits on the rug.",
	"The weather in Beijing is cold in January.",
	"股票市场今天大幅上涨。",
	"人工智能正在改变世界。",
}

func main() {
	model := flag.String("model", "", "path to embeddinggemma-300M-Q8_0.gguf")
	dim := flag.Int("dim", 768, "output embedding dim (MRL truncation)")
	bench := flag.Int("bench", 0, "benchmark iterations (0 = off)")
	out := flag.String("out", "", "write golden embeddings to this binary file")
	flag.Parse()

	if *model == "" {
		home, _ := os.UserHomeDir()
		*model = home + "/.maclaw/models/embeddinggemma-300M-Q8_0.gguf"
	}

	t0 := time.Now()
	emb, err := embedding.NewGemmaEmbedder(*model, *dim)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load failed:", err)
		os.Exit(1)
	}
	defer emb.Close()

	// Adopt the embedder's *effective* width.  NewGemmaEmbedder maps dim <= 0 to
	// 256 and clamps dim > modelDim down to the model's dim, but the flag value
	// was used for the golden header and the printed "dim=%d" below.  Writing
	// uint32(*dim) while writing the full r.vec produced a header that disagreed
	// with the payload for `-dim 0` (header says 0, records hold 256 floats) and
	// for `-dim > modelDim` (header says e.g. 769, records hold 768) -- a file no
	// reader can parse correctly.  The C++ port had the mirror-image bug.
	*dim = emb.Dim()
	fmt.Printf("loaded model in %v (dim=%d, GOMAXPROCS=%d)\n", time.Since(t0), emb.Dim(), runtime.NumCPU())

	// ---- golden embeddings -------------------------------------------------
	type rec struct {
		text string
		vec  []float32
		ms   float64
	}
	recs := make([]rec, 0, len(corpus))
	for _, text := range corpus {
		start := time.Now()
		vec, err := emb.Embed(text)
		el := time.Since(start)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Embed(%q) failed: %v\n", text, err)
			os.Exit(1)
		}
		recs = append(recs, rec{text, vec, float64(el.Microseconds()) / 1000.0})
	}

	fmt.Println()
	fmt.Println("--- golden embeddings ---")
	for _, r := range recs {
		var norm float64
		for _, v := range r.vec {
			norm += float64(v) * float64(v)
		}
		// Print at most four components.  `-dim 1..3` yields fewer than four, and
		// indexing r.vec[0..3] unconditionally panicked ("index out of range"),
		// which made every dim below 4 impossible to golden.
		n := len(r.vec)
		if n > 4 {
			n = 4
		}
		head := ""
		for i := 0; i < n; i++ {
			if i > 0 {
				head += " "
			}
			head += strconv.FormatFloat(float64(r.vec[i]), 'f', 6, 64)
		}
		fmt.Printf("%-62q dim=%d |v|=%.6f  v[0:%d]=[%s]  %.1fms\n",
			trunc(r.text, 60), len(r.vec), math.Sqrt(norm), n, head, r.ms)
	}

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// header: count(uint32), dim(uint32), then per record:
		//   textLen(uint32), text bytes, dim float32
		binary.Write(f, binary.LittleEndian, uint32(len(recs)))
		binary.Write(f, binary.LittleEndian, uint32(*dim))
		for _, r := range recs {
			b := []byte(r.text)
			binary.Write(f, binary.LittleEndian, uint32(len(b)))
			f.Write(b)
			binary.Write(f, binary.LittleEndian, r.vec)
		}
		f.Close()
		fmt.Printf("\nwrote %d golden vectors (%d dims) to %s\n", len(recs), *dim, *out)
	}

	if *bench <= 0 {
		return
	}

	// ---- benchmark: single-text latency -----------------------------------
	text := corpus[3]
	for i := 0; i < 3; i++ {
		emb.Embed(text)
	}
	lats := make([]float64, 0, *bench)
	start := time.Now()
	for i := 0; i < *bench; i++ {
		t := time.Now()
		if _, err := emb.Embed(text); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lats = append(lats, float64(time.Since(t).Microseconds())/1000.0)
	}
	total := time.Since(start)
	sort.Float64s(lats)
	fmt.Printf("\n--- single-text latency (dim=%d, %d iters, len=%d chars) ---\n", *dim, *bench, len(text))
	fmt.Printf("mean=%.2fms  p50=%.2fms  p90=%.2fms  min=%.2fms  max=%.2fms  throughput=%.1f texts/s\n",
		total.Seconds()*1000/float64(*bench),
		lats[len(lats)/2], lats[len(lats)*9/10], lats[0], lats[len(lats)-1],
		float64(*bench)/total.Seconds())

	// ---- benchmark: batch throughput --------------------------------------
	batch := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		batch = append(batch, corpus[i%len(corpus)])
	}
	emb.EmbedBatch(batch[:8])
	start = time.Now()
	if _, err := emb.EmbedBatch(batch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bt := time.Since(start)
	fmt.Printf("--- batch throughput (64 texts, EmbedBatch concurrent) ---\n")
	fmt.Printf("total=%.1fms  throughput=%.1f texts/s\n", bt.Seconds()*1000, 64/bt.Seconds())
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
