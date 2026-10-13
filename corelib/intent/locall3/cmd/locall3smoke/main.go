// Command locall3smoke builds the intent classifiers exactly the way guiapp
// does (guiapp/app_local_intent.go + app_embedding.go: one embedder from
// locall3.LoadEmbedder, a UIC created with the locall3 L3 callbacks, the
// embedder + head attached later, a second per-device UIC on the same
// embedder) and classifies a few zh/en turns with the real models. It
// prints RSS and every *.gguf file mapped into the process, to show that
// only one model is resident. The remote tree LLM is a stub that counts
// calls (it must stay at 0 when the local head is attached).
//
//	go run ./corelib/intent/locall3/cmd/locall3smoke -models DIR -heads DIR
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/intent/locall3"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func rssMB() (rss, hwm float64) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return -1, -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var kb float64
		if n, _ := fmt.Sscanf(sc.Text(), "VmRSS: %f kB", &kb); n == 1 {
			rss = kb / 1024
		}
		if n, _ := fmt.Sscanf(sc.Text(), "VmHWM: %f kB", &kb); n == 1 {
			hwm = kb / 1024
		}
	}
	return
}

func mappedModels() []string {
	b, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return []string{"(no /proc/self/maps)"}
	}
	seen := map[string]bool{}
	for _, ln := range strings.Split(string(b), "\n") {
		if i := strings.Index(ln, "/"); i >= 0 && strings.HasSuffix(ln, ".gguf") {
			seen[ln[i:]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func report(stage string) {
	r, h := rssMB()
	fmt.Printf("[rss] %-28s VmRSS=%.0fMB VmHWM=%.0fMB gguf_mapped=%v\n", stage, r, h, mappedModels())
}

func main() {
	models := flag.String("models", "", "models dir (contains Qwen3-Embedding-0.6B-Q8_0.gguf)")
	heads := flag.String("heads", "", "head dir (default <models>/intent_heads)")
	flag.Parse()
	if *heads == "" {
		*heads = locall3.DefaultHeadDir(*models)
	}
	slotPath := filepath.Join(*models, embedding.DefaultModelFilename)
	report("start")

	var remoteCalls atomic.Int64
	remote := func(context.Context, context.Context, string, string) (string, error) {
		remoteCalls.Add(1)
		return "", fmt.Errorf("stub remote tree LLM (should not be reached)")
	}
	remoteLegacy := func(string, string) (string, error) {
		remoteCalls.Add(1)
		return "", fmt.Errorf("stub remote")
	}

	// initEarlyClassifier: UIC with noop embedder and the locall3 callbacks.
	rt := locall3.New(*heads)
	fmt.Printf("[l3] mode=%s %s\n", rt.Mode(), rt.Describe())
	uic := intent.New(intent.Config{
		Embedder:           embedding.NoopEmbedder{},
		LLMFunc:            rt.LLMFunc(remoteLegacy),
		LLMContextFunc:     rt.ContextFunc(remote),
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Second,
	})

	// ensureEmbeddingEngineSync / activate*: one embedder for everything.
	t0 := time.Now()
	load, err := locall3.LoadEmbedder(slotPath, false)
	if err != nil {
		fmt.Println("load error:", err)
		os.Exit(1)
	}
	fmt.Printf("[embed] %s load=%.0fms\n", load, float64(time.Since(t0).Microseconds())/1000)
	emb := load.Embedder
	report("after embedder load")
	if h, err := rt.Attach(emb); err != nil {
		fmt.Println("[l3] attach error:", err)
	} else if h != nil {
		fmt.Printf("[l3] attached %s\n", h.ID)
	}
	fmt.Printf("[l3] %s\n", rt.Describe())
	uic.SetEmbedder(emb)
	uic.SetLLMFunc(rt.LLMFunc(remoteLegacy))
	uic.SetLLMContextFunc(rt.ContextFunc(remote))
	// newHardwareIntentClassifier: per-device UIC on the same embedder.
	hw := intent.New(intent.Config{Embedder: emb, LLMFunc: rt.LLMFunc(remoteLegacy), LLMContextFunc: rt.ContextFunc(remote),
		LLMTimeout: 30 * time.Second, FusionTreeDeadline: 30 * time.Second})
	for i := 0; i < 600 && !(uic.Ready() && hw.Ready()); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("[uic] anchors ready desktop=%v hardware=%v after %.1fs\n", uic.Ready(), hw.Ready(), time.Since(t0).Seconds())
	report("after anchor warmup x2")

	// Track A stand-in: tool descriptions embedded by the same instance
	// (guiapp hands this instance to toolRouter.SetEmbedder).
	tools := []string{"web_search: search the web", "read_file: read a local file", "send_email: send an email", "创建日程：在日历中添加事件"}
	if _, err := emb.EmbedBatch(tools); err != nil {
		fmt.Println("tool embed error:", err)
	}
	report("after tool-description embeds")

	// First-party tool head (same vector as intent L2).
	var toolHeadPaths []string
	for _, dir := range []string{filepath.Join(*models, "tool_heads"), *heads, *models} {
		toolHeadPaths = append(toolHeadPaths, filepath.Join(dir, "tool_head_ovr_qwen3go_r5.json"), filepath.Join(dir, "tool_head_ovr_qwen3go.json"))
	}
	for _, p := range toolHeadPaths {
		if h, err := tool.LoadToolHead(p); err == nil {
			if err := h.CheckEmbedderOf(emb); err != nil {
				fmt.Printf("[toolhead] %s refused: %v\n", h.ID, err)
				break
			}
			v, _, _ := intent.IntentQueryEmbedding(context.Background(), emb, "帮我查一下明天北京的天气")
			probs, _ := h.Predict(v)
			top := h.TopK(probs, h.TopKDefault, h.Theta)
			fmt.Printf("[toolhead] %s θ=%.2f k=%d top=%v\n", h.ID, h.Theta, h.TopKDefault, top)
			break
		}
	}

	turns := []string{
		"帮我查一下明天北京的天气",
		"把这个 PDF 翻译成英文",
		"给张三发封邮件说会议改到三点",
		"你好呀",
		"删除桌面上所有的临时文件",
		"Search the web for the latest Go release notes",
		"Write a Python script that renames all jpg files",
		"Schedule a meeting with Alice tomorrow at 10am",
		"what's 2+2",
		"Turn off the living room lights",
	}
	for i, t := range turns {
		c := uic
		if i%2 == 1 {
			c = hw
		}
		s := time.Now()
		r := c.ClassifyContext(context.Background(), intent.MessageContext{Text: t, UserID: "smoke"})
		ms := float64(time.Since(s).Microseconds()) / 1000
		head := "-"
		if h, _ := rt.Active(); h != nil {
			if v, _, err := intent.IntentQueryEmbedding(context.Background(), emb, t); err == nil {
				if p, err := h.Predict(v); err == nil {
					lb, pr, dec, thr := h.Decide(p)
					head = fmt.Sprintf("%s/%.2f/%s(thr %.2f)", lb, pr, dec, thr)
				}
			}
		}
		fmt.Printf("[cls] %-52q primary=%-14s conf=%.2f layer=%d degraded=%-5v %5.0fms head=%s\n", t, r.Primary, r.Confidence, r.Layer, r.Degraded, ms, head)
	}
	fmt.Printf("[l3] remote tree LLM calls: %d\n", remoteCalls.Load())
	report("after classify")
	emb.Close()
}
