// embdoctor: one-shot health report for the local embedding backend.
//
// It answers the questions that decide whether the GPU backend is worth having
// on a particular machine:
//
//	embdoctor -model Qwen3-Embedding-0.6B-Q8_0.gguf
//
// Reports, in order: environment (cores, thread budget, ISA), the startup gate
// verdict with its measured numbers, the CPU-vs-GPU latency at several input
// lengths, and the memory footprint. The GPU is measured first, then released
// with the hardware-acceleration toggle, then the CPU is measured on the same
// texts, so both numbers come from one process and one model load.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// bucketWords builds a text whose token count lands near want. The tokenizer
// is the authority, so the reported count is the measured one.
var bucketWords = strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron")

func textOfTokens(q *embedding.Qwen3Embedder, want int) (string, int) {
	var b strings.Builder
	for i := 0; ; i++ {
		b.WriteString(bucketWords[i%len(bucketWords)])
		b.WriteByte(' ')
		if i%8 == 7 {
			b.WriteString(". ")
		}
		n := len(q.Tokenize(b.String()))
		if n >= want || b.Len() > 64*want+256 {
			return b.String(), n
		}
	}
}

func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func msOf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

type row struct {
	Tokens int     `json:"tokens"`
	CPUMs  float64 `json:"cpu_ms"`
	GPUMs  float64 `json:"gpu_ms"`
	Ratio  float64 `json:"gpu_vs_cpu"` // cpu/gpu; >1 means the GPU is faster
}

func main() {
	model := flag.String("model", "", "Qwen3 embedding GGUF path (default: resolve from the model slot)")
	n := flag.Int("n", 12, "samples per length")
	lengths := flag.String("tokens", "8,32,64,128,380", "comma-separated token counts to compare")
	asJSON := flag.Bool("json", false, "emit JSON instead of a table")
	flag.Parse()

	res := map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0)}
	path := *model
	if path == "" {
		p, tried := embedding.ResolveQwen3ModelPath("")
		if p == "" {
			fmt.Fprintln(os.Stderr, "no model found; pass -model. Candidates:")
			for _, c := range tried {
				fmt.Fprintf(os.Stderr, "  %s (%s)\n", c.Path, c.Skip)
			}
			os.Exit(2)
		}
		path = p
	}
	res["model"] = path

	t0 := time.Now()
	emb, err := embedding.NewEmbedderFromFile(path, embedding.DefaultEmbeddingDim)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
	defer emb.Close()
	res["load_ms"] = msOf(time.Since(t0))
	q, ok := emb.(*embedding.Qwen3Embedder)
	if !ok {
		fmt.Fprintln(os.Stderr, "embdoctor needs the Qwen3 embedder")
		os.Exit(1)
	}

	// ---- environment ----
	env := map[string]any{
		"logical_cores":  runtime.NumCPU(),
		"physical_cores": embedding.PhysicalCores(),
		"embed_threads":  embedding.EmbedThreads(),
		"isa":            tensor.Q8PISAName(),
		"compute_mode":   q.ComputeMode(),
		"model_id":       q.ModelID(),
		"dim":            q.Dim(),
	}
	res["environment"] = env

	// ---- gate + backend ----
	gate := q.GPUGateInfo()
	accel := q.Accel()
	active, reason := q.GPUBackend()
	gw, ga := q.GPUMemoryBytes()
	backend := map[string]any{
		"active":         active,
		"reason":         reason,
		"accel_backend":  accel.Backend,
		"accel_device":   accel.Device,
		"gate":           gate,
		"gpu_weights_mb": float64(gw) / 1e6,
		"gpu_acts_mb":    float64(ga) / 1e6,
	}
	res["backend"] = backend

	// ---- latency by length ----
	var want []int
	for _, s := range strings.Split(*lengths, ",") {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); err == nil && v > 0 {
			want = append(want, v)
		}
	}
	texts := make([]string, len(want))
	tokens := make([]int, len(want))
	for i, w := range want {
		texts[i], tokens[i] = textOfTokens(q, w)
	}

	measure := func() []time.Duration {
		out := make([]time.Duration, len(texts))
		for i, txt := range texts {
			var ds []time.Duration
			for k := 0; k < *n; k++ {
				st := time.Now()
				if _, err := q.Embed(txt); err != nil {
					fmt.Fprintln(os.Stderr, "embed:", err)
					os.Exit(1)
				}
				ds = append(ds, time.Since(st))
			}
			out[i] = median(ds)
		}
		return out
	}

	// The pooled pass is the Layer 2 intent path: it runs on every message, so
	// it is the number that decides whether a low-end machine stays usable.
	poolText := "帮我把这个文件发给张三，然后打开设置页面确认一下"
	measurePooled := func() time.Duration {
		var ds []time.Duration
		for k := 0; k < *n; k++ {
			st := time.Now()
			if _, _, err := q.EmbedPooledWithRole(poolText, embedding.RoleClassification); err != nil {
				fmt.Fprintln(os.Stderr, "pooled embed:", err)
				os.Exit(1)
			}
			ds = append(ds, time.Since(st))
		}
		return median(ds)
	}
	var gpuDur []time.Duration
	var gpuPooled time.Duration
	if active {
		gpuDur = measure()
		gpuPooled = measurePooled()
	}
	// Release the GPU so the same process measures the CPU path. ApplyAccel
	// waits for in-flight forwards, so no request is cut in half.
	q.ApplyAccel(false)
	cpuDur := measure()
	cpuPooled := measurePooled()

	var rows []row
	for i := range texts {
		r := row{Tokens: tokens[i], CPUMs: msOf(cpuDur[i])}
		if gpuDur != nil {
			r.GPUMs = msOf(gpuDur[i])
			if r.GPUMs > 0 {
				r.Ratio = r.CPUMs / r.GPUMs
			}
		}
		rows = append(rows, r)
	}
	res["latency"] = rows
	res["pooled"] = map[string]any{"cpu_ms": msOf(cpuPooled), "gpu_ms": msOf(gpuPooled)}

	// ---- memory ----
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	mem := map[string]any{
		"go_sys_mb":    float64(ms.Sys) / 1e6,
		"go_heap_mb":   float64(ms.HeapAlloc) / 1e6,
		"gpu_total_mb": float64(gw+ga) / 1e6,
	}
	if avail, ok := embedding.AvailableMemory(); ok {
		mem["available_mb"] = float64(avail) / 1e6
	}
	res["memory"] = mem

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", " ")
		_ = enc.Encode(res)
		return
	}

	// ---- human report ----
	w := os.Stdout
	fmt.Fprintf(w, "embdoctor  %s/%s  gomaxprocs=%d\n", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0))
	fmt.Fprintf(w, "model      %s (%.1f MB load)\n", path, res["load_ms"].(float64))
	fmt.Fprintf(w, "\nCPU\n")
	fmt.Fprintf(w, "  cores      %d logical / %d physical -> %d embed threads\n",
		env["logical_cores"], env["physical_cores"], env["embed_threads"])
	fmt.Fprintf(w, "  kernels    %s on %s\n", env["isa"], env["compute_mode"])

	fmt.Fprintf(w, "\nGPU backend\n")
	if gate.Backend == "" {
		fmt.Fprintf(w, "  not probed  %s\n", reason)
	} else if active {
		fmt.Fprintf(w, "  ACTIVE      %s\n", accel.Device)
		fmt.Fprintf(w, "  gate        cos %.6f (floor %.4f), cpu %.1f ms vs gpu %.1f ms = %.2fx (floor %.2f)\n",
			gate.MinCos, 0.999, gate.CPUMs, gate.GPUMs, gate.Ratio, gate.MinRatio)
	} else {
		fmt.Fprintf(w, "  REJECTED    %s\n", reason)
		if gate.Ratio > 0 {
			fmt.Fprintf(w, "  gate        cos %.6f, cpu %.1f ms vs gpu %.1f ms = %.2fx (floor %.2f)\n",
				gate.MinCos, gate.CPUMs, gate.GPUMs, gate.Ratio, gate.MinRatio)
		}
	}

	fmt.Fprintf(w, "\nLatency, median of %d embeds\n", *n)
	if gpuDur == nil {
		fmt.Fprintf(w, "  %6s %10s\n", "tokens", "cpu ms")
		for _, r := range rows {
			fmt.Fprintf(w, "  %6d %10.2f\n", r.Tokens, r.CPUMs)
		}
	} else {
		fmt.Fprintf(w, "  %6s %10s %10s %8s\n", "tokens", "cpu ms", "gpu ms", "cpu/gpu")
		cap := embedding.VulkanMaxSeqTokens()
		for _, r := range rows {
			note := ""
			switch {
			case cap > 0 && r.Tokens > cap:
				note = "  (above the GPU token cap: both columns are the CPU)"
			case r.Ratio > 0 && r.Ratio < 1:
				note = "  (cpu faster)"
			}
			fmt.Fprintf(w, "  %6d %10.2f %10.2f %8.2f%s\n", r.Tokens, r.CPUMs, r.GPUMs, r.Ratio, note)
		}
		if len(rows) > 0 && accel.Backend == embedding.BackendVulkan {
			fmt.Fprintf(w, "  Vulkan handles inputs up to %d tokens; longer texts run on the CPU.\n", cap)
		}
	}

	fmt.Fprintf(w, "\nPooled (Layer 2 intent: last+mean, runs on every message)\n")
	fmt.Fprintf(w, "  %10s %10s %8s\n", "cpu ms", "gpu ms", "cpu/gpu")
	pr := 0.0
	if gpuPooled > 0 {
		pr = msOf(cpuPooled) / msOf(gpuPooled)
	}
	fmt.Fprintf(w, "  %10.2f %10.2f %8.2f\n", msOf(cpuPooled), msOf(gpuPooled), pr)

	fmt.Fprintf(w, "\nMemory\n")
	fmt.Fprintf(w, "  gpu        %.0f MB weights + %.0f MB activations\n", backend["gpu_weights_mb"], backend["gpu_acts_mb"])
	fmt.Fprintf(w, "  process    %.0f MB (go heap %.0f MB)\n", mem["go_sys_mb"], mem["go_heap_mb"])
	if v, ok := mem["available_mb"]; ok {
		fmt.Fprintf(w, "  system     %.0f MB available\n", v)
	}
}
