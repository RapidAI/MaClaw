package embedding

// Latency/throughput harness (skipped unless ZZ_BENCH_MODEL is set):
//
//	ZZ_BENCH_MODEL=<gguf> ZZ_BENCH_IN=<jsonl {"k","t"}> [ZZ_BENCH_OUT=<prefix>] go test -run TestZZBench -v
//
// Reports the kernel ISA, median short/long single-text latency and
// EmbedBatch items/s over ZZ_BENCH_IN; optionally dumps the batch vectors.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func zzMedian(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

func TestZZBench(t *testing.T) {
	model := os.Getenv("ZZ_BENCH_MODEL")
	if model == "" {
		t.Skip("ZZ_BENCH_MODEL not set")
	}
	emb, err := NewEmbedderFromFile(model, DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	q, _ := emb.(*Qwen3Embedder)
	if q != nil {
		t.Logf("isa=%s q8p=%v fallback=%q GOMAXPROCS=%d NumCPU=%d", tensor.Q8PISAName(), q.q8p, q.q8pFallback, runtime.GOMAXPROCS(0), runtime.NumCPU())
	}
	short := ApplyQwen3Prompt("帮我把这个文件发给张三", RoleClassification)
	long := ApplyQwen3Prompt(strings.Repeat("The quick brown fox jumps over the lazy dog while the committee reviews the quarterly budget report. ", 20), RoleClassification)
	if q != nil {
		t.Logf("tokens short=%d long=%d", len(q.Tokenize(short)), len(q.Tokenize(long)))
	}
	time1 := func(s string, n int) time.Duration {
		emb.Embed(s)
		var d []time.Duration
		for i := 0; i < n; i++ {
			t0 := time.Now()
			if _, err := emb.Embed(s); err != nil {
				t.Fatal(err)
			}
			d = append(d, time.Since(t0))
		}
		return zzMedian(d)
	}
	t.Logf("RESULT short_ms=%.2f", float64(time1(short, 30).Microseconds())/1000)
	if os.Getenv("ZZ_BENCH_SHORT_ONLY") != "" {
		t.Logf("RESULT short_ms=%.2f (x300)", float64(time1(short, 300).Microseconds())/1000)
		return
	}
	t.Logf("RESULT long_ms=%.2f", float64(time1(long, 7).Microseconds())/1000)
	in := os.Getenv("ZZ_BENCH_IN")
	if in == "" {
		return
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct{ K, T string }
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, r.T)
	}
	f.Close()
	t0 := time.Now()
	var all [][]float32
	for i := 0; i < len(texts); i += 32 {
		vs, err := emb.EmbedBatch(texts[i:min(i+32, len(texts))])
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, vs...)
	}
	el := time.Since(t0)
	t.Logf("RESULT batch_n=%d batch_s=%.2f items_per_s=%.2f", len(texts), el.Seconds(), float64(len(texts))/el.Seconds())
	if out := os.Getenv("ZZ_BENCH_OUT"); out != "" {
		vf, _ := os.Create(out + ".f32")
		w := bufio.NewWriter(vf)
		buf := make([]byte, 4)
		for _, v := range all {
			for _, x := range v {
				binary.LittleEndian.PutUint32(buf, math.Float32bits(x))
				w.Write(buf)
			}
		}
		w.Flush()
		vf.Close()
	}
}
