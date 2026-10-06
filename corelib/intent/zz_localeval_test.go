package intent

// LOCAL EVAL HARNESS (local branch only, not for upstream).
// Modes (ZZ_MODE): defs | embed | eval
//   defs : dump DefaultDefinitions + tree prompt to ZZ_OUT
//   embed: embed every {"id","text"} line of ZZ_IN with the production
//          GemmaEmbedder (same Embed() L2 uses) -> ZZ_OUT (jsonl id+vec)
//   eval : run L2 raw + ClassifyContext under ZZ_L3 = none|down|hang|http
//          and, if ZZ_HEAD is set, the pure-Go classification head.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type zzIn struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func zzReadIn(t *testing.T, path string) []zzIn {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []zzIn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r zzIn
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func zzEmbedder(t *testing.T) embedding.Embedder {
	p := os.Getenv("ZZ_GGUF")
	t0 := time.Now()
	emb, err := embedding.NewGemmaEmbedder(p, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "ZZ embedder load %.0fms\n", float64(time.Since(t0).Microseconds())/1000)
	return emb
}

func zzRSSMB() float64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("VmRSS:")) {
			f := bytes.Fields(line)
			kb, _ := strconv.ParseFloat(string(f[1]), 64)
			return kb / 1024
		}
	}
	return 0
}

func TestZZLocalEval(t *testing.T) {
	mode := os.Getenv("ZZ_MODE")
	if mode == "" {
		t.Skip("ZZ_MODE not set")
	}
	out, err := os.Create(os.Getenv("ZZ_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	switch mode {
	case "defs":
		defs := DefaultDefinitions()
		type d struct {
			Label    string   `json:"label"`
			Domain   string   `json:"domain"`
			Embed    []string `json:"embed_texts"`
			Tree     string   `json:"tree_text"`
			Tools    []string `json:"tools"`
			Workflow bool     `json:"may_trigger_workflow"`
		}
		var all []d
		for _, x := range defs {
			all = append(all, d{string(x.Label), x.Domain, x.EmbedTexts, x.TreeText, x.ToolNames, x.MayTriggerWorkflow})
		}
		_ = enc.Encode(map[string]any{
			"defs":        all,
			"tree_prompt": BuildTreePrompt(BuildIntentTreeText(defs), "<<USER_MESSAGE>>"),
			"thresholds": map[string]float64{
				"EmbeddingConfidentMinScore": EmbeddingConfidentMinScore,
				"EmbeddingConfidentMinGap":   EmbeddingConfidentMinGap,
				"EmbeddingLookupMinScore":    EmbeddingLookupMinScore,
				"EmbeddingLookupMinGap":      EmbeddingLookupMinGap,
			},
		})
		return
	case "embed":
		emb := zzEmbedder(t)
		ins := zzReadIn(t, os.Getenv("ZZ_IN"))
		t0 := time.Now()
		for i, r := range ins {
			v, err := emb.Embed(r.Text)
			if err != nil {
				t.Fatalf("%s: %v", r.ID, err)
			}
			_ = enc.Encode(map[string]any{"id": r.ID, "vec": v})
			if i%200 == 0 {
				fmt.Fprintf(os.Stderr, "ZZ embed %d/%d %.1fs rss=%.0fMB\n", i, len(ins), time.Since(t0).Seconds(), zzRSSMB())
			}
		}
		return
	case "eval":
	default:
		t.Fatalf("bad mode %s", mode)
	}

	rssBefore := zzRSSMB()
	emb := zzEmbedder(t)
	l3 := os.Getenv("ZZ_L3")
	cfg := Config{Embedder: emb}
	var l3Calls, l3Errs int
	switch l3 {
	case "", "none":
	case "down":
		cfg.LLMContextFunc = func(ctx, parent context.Context, sys, user string) (string, error) {
			l3Calls++
			return "", errors.New("dial tcp 203.0.113.1:443: connect: network is unreachable")
		}
	case "hang":
		cfg.LLMContextFunc = func(ctx, parent context.Context, sys, user string) (string, error) {
			l3Calls++
			<-ctx.Done()
			return "", ctx.Err()
		}
	case "http":
		url := os.Getenv("ZZ_L3_URL")
		model := os.Getenv("ZZ_L3_MODEL")
		cfg.LLMContextFunc = func(ctx, parent context.Context, sys, user string) (string, error) {
			l3Calls++
			if os.Getenv("ZZ_L3_STATIC") == "1" {
				// Move the quoted message out of the system prompt so the whole
				// ~6.3k-token tree prefix stays in llama.cpp's KV cache.
				sys = strings.Replace(sys, "\""+user+"\"", "(the user message is given in the next user turn)", 1)
			}
			req0 := map[string]any{
				"model": model,
				"messages": []map[string]string{
					{"role": "system", "content": sys},
					{"role": "user", "content": user + " /no_think"},
				},
				"temperature": 0, "max_tokens": 120, "cache_prompt": true,
			}
			if os.Getenv("ZZ_L3_SCHEMA") == "1" {
				req0["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "tree", "schema": zzTreeSchema()}}
			}
			body, _ := json.Marshal(req0)
			req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				l3Errs++
				return "", err
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			var r struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(b, &r); err != nil || len(r.Choices) == 0 {
				l3Errs++
				return "", fmt.Errorf("bad response: %s", string(b))
			}
			return r.Choices[0].Message.Content, nil
		}
	case "head", "headllm":
		// Local L3 = calibrated classification head on the SAME embedder,
		// adapted to the tree protocol so every existing ClassifyContext guard
		// (weak-verdict retention, local contradiction check, composite
		// synthesis, fail-closed L2 hint) still applies unchanged.
		hd := zzLoadHead(t, os.Getenv("ZZ_HEAD"))
		tau := hd.Tau
		// Optional stricter threshold for sensitive labels (tau_s), e.g. ZZ_TAU_S=0.90.
		tauS, _ := strconv.ParseFloat(os.Getenv("ZZ_TAU_S"), 64)
		sensSet := map[string]bool{}
		for _, l := range hd.Sensitive {
			sensSet[l] = true
		}
		thrFor := func(label string) float64 {
			if sensSet[label] && tauS > tau {
				return tauS
			}
			return tau
		}
		treeByLabel := map[string]string{}
		for _, d := range DefaultDefinitions() {
			treeByLabel[string(d.Label)] = d.TreeText
		}
		url := os.Getenv("ZZ_L3_URL")
		cfg.LLMContextFunc = func(ctx, parent context.Context, sys, user string) (string, error) {
			l3Calls++
			v, err := zzHeadEmbed(emb, hd, user)
			if err != nil {
				return "", err
			}
			probs := hd.Predict(v)
			idx := make([]int, len(probs))
			for i := range idx {
				idx[i] = i
			}
			sort.Slice(idx, func(a, b int) bool { return probs[idx[a]] > probs[idx[b]] })
			type cand struct {
				Skill string  `json:"skill"`
				Score float64 `json:"score"`
				WF    string  `json:"workflow_type"`
			}
			if probs[idx[0]] >= thrFor(hd.Labels[idx[0]]) {
				var top []cand
				for _, j := range idx[:3] {
					if hd.Labels[j] == "unknown" {
						continue
					}
					if len(top) > 0 && probs[j] < 0.10 {
						break
					}
					top = append(top, cand{hd.Labels[j], probs[j], ""})
				}
				if len(top) == 0 { // head is confident it is chit-chat
					top = append(top, cand{"ambiguous", probs[idx[0]], ""})
				}
				b, _ := json.Marshal(map[string]any{"top": top})
				return string(b), nil
			}
			if l3 == "head" {
				l3Errs++
				return "", fmt.Errorf("local head uncertain: %s %.2f < %.2f", hd.Labels[idx[0]], probs[idx[0]], thrFor(hd.Labels[idx[0]]))
			}
			// headllm: ask the small local LLM to choose among the head's top-5.
			var opts []string
			var enum []string
			for _, j := range idx[:5] {
				l := hd.Labels[j]
				enum = append(enum, l)
				tt := treeByLabel[l]
				if l == "unknown" {
					tt = "闲聊、问候或没有明确任务的消息 (chit-chat / greeting / no actionable task)."
				}
				if r := []rune(tt); len(r) > 300 {
					tt = string(r[:300])
				}
				opts = append(opts, "- "+l+": "+tt)
			}
			sysmsg := "You are an intent classifier for a desktop agent. Choose which ONE intent best matches the user's message, or none_of_these if none fits. Candidates:\n" + strings.Join(opts, "\n") + "\nReturn ONLY JSON: {\"intent\": \"<name>\", \"score\": 0.0-1.0}"
			body, _ := json.Marshal(map[string]any{
				"messages":    []map[string]string{{"role": "system", "content": sysmsg}, {"role": "user", "content": user + " /no_think"}},
				"temperature": 0, "max_tokens": 60, "cache_prompt": true,
			})
			// Own sub-deadline, and never surface DeadlineExceeded: a slow local
			// LLM must not trip the tree-endpoint skip window, which would also
			// switch the (healthy, ~ms) head off for the next 10 s.
			subTO := 10 * time.Second
			if v := os.Getenv("ZZ_LLM_SUBTIMEOUT"); v != "" {
				subTO, _ = time.ParseDuration(v)
			}
			lctx, lcancel := context.WithTimeout(ctx, subTO)
			defer lcancel()
			req, _ := http.NewRequestWithContext(lctx, "POST", url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				l3Errs++
				return "", fmt.Errorf("local llm fallback unavailable: %s", err.Error())
			}
			defer resp.Body.Close()
			rb, _ := io.ReadAll(resp.Body)
			var r struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			_ = json.Unmarshal(rb, &r)
			if len(r.Choices) == 0 {
				l3Errs++
				return "", fmt.Errorf("bad llm response")
			}
			c := r.Choices[0].Message.Content
			var pick struct {
				Intent string `json:"intent"`
			}
			if i, j := strings.Index(c, "{"), strings.LastIndex(c, "}"); i >= 0 && j > i {
				_ = json.Unmarshal([]byte(c[i:j+1]), &pick)
			}
			ok := false
			for _, e := range enum {
				ok = ok || e == pick.Intent
			}
			if !ok || pick.Intent == "unknown" {
				l3Errs++
				return "", fmt.Errorf("local llm abstained: %q", c)
			}
			// LLM resolved an ambiguity the head could not: "fairly confident" band.
			b, _ := json.Marshal(map[string]any{"top": []cand{{pick.Intent, 0.70, ""}}})
			return string(b), nil
		}
	default:
		t.Fatalf("bad ZZ_L3 %s", l3)
	}
	if d := os.Getenv("ZZ_TREE_DEADLINE"); d != "" {
		dd, _ := time.ParseDuration(d)
		cfg.FusionTreeDeadline = dd
	}
	t0 := time.Now()
	u := New(cfg)
	for !u.Ready() {
		time.Sleep(20 * time.Millisecond)
	}
	coldMs := float64(time.Since(t0).Microseconds()) / 1000
	runtime.GC()
	fmt.Fprintf(os.Stderr, "ZZ anchors warm %.0fms rss=%.0fMB (before=%.0f)\n", coldMs, zzRSSMB(), rssBefore)

	var head *zzHead
	if hp := os.Getenv("ZZ_HEAD"); hp != "" {
		head = zzLoadHead(t, hp)
	}
	u.mu.RLock()
	anchors := u.anchors
	u.mu.RUnlock()

	ins := zzReadIn(t, os.Getenv("ZZ_IN"))
	for i, r := range ins {
		rec := map[string]any{"id": r.ID}
		ta := time.Now()
		l2, conf := classifyByEmbedding(emb, anchors, r.Text)
		rec["l2_ms"] = float64(time.Since(ta).Microseconds()) / 1000
		rec["l2_primary"] = l2.Primary
		rec["l2_conf"] = l2.Confidence
		rec["l2_confident"] = conf
		rec["l2_secondary"] = l2.Secondary
		rec["l2_runnerup"] = l2.RunnerUp
		ta = time.Now()
		res := u.ClassifyContext(context.Background(), MessageContext{Text: r.Text, UserID: "zz"})
		rec["cls_ms"] = float64(time.Since(ta).Microseconds()) / 1000
		rec["cls_primary"] = res.Primary
		rec["cls_conf"] = res.Confidence
		rec["cls_secondary"] = res.Secondary
		rec["cls_layer"] = res.Layer
		rec["cls_tools"] = res.ToolNames
		rec["cls_degraded"] = res.Degraded
		rec["cls_cpf"] = res.ControlPlaneFailure
		rec["cls_reason"] = res.Reason
		if head != nil {
			ta = time.Now()
			v, _ := zzHeadEmbed(emb, head, r.Text)
			embMs := float64(time.Since(ta).Microseconds()) / 1000
			ta = time.Now()
			probs := head.Predict(v)
			rec["head_ms"] = float64(time.Since(ta).Microseconds()) / 1000
			rec["head_embed_ms"] = embMs
			type kv struct {
				L string
				P float64
			}
			var top []kv
			for j, p := range probs {
				top = append(top, kv{head.Labels[j], p})
			}
			sort.Slice(top, func(a, b int) bool { return top[a].P > top[b].P })
			rec["head_top"] = top[:5]
		}
		_ = enc.Encode(rec)
		if i%100 == 0 {
			fmt.Fprintf(os.Stderr, "ZZ eval %d/%d rss=%.0fMB l3calls=%d errs=%d\n", i, len(ins), zzRSSMB(), l3Calls, l3Errs)
		}
	}
	fmt.Fprintf(os.Stderr, "ZZ done rss=%.0fMB l3calls=%d l3errs=%d\n", zzRSSMB(), l3Calls, l3Errs)
}

// zzHead is the pure-Go inference side of the classification head:
// optional standardisation, one hidden layer (ReLU) or none, softmax with
// a calibrated temperature. ~768*H MACs; no CGO.
type zzHead struct {
	Labels      []string    `json:"labels"`
	Mean        []float64   `json:"mean"`
	Scale       []float64   `json:"scale"`
	W1          [][]float64 `json:"w1"` // [H][D] (empty for linear)
	B1          []float64   `json:"b1"`
	W2          [][]float64 `json:"w2"` // [C][H or D]
	B2          []float64   `json:"b2"`
	Temperature float64     `json:"temperature"`
	Tau         float64     `json:"tau"`
	L2Norm      bool        `json:"l2norm"`
	Sensitive   []string    `json:"sensitive"`
	// EmbedRole "classification" = head trained on vectors embedded with the
	// EmbeddingGemma classification prompt (same space as Layer 2).
	EmbedRole string `json:"embed_role"`
}

// zzHeadEmbed embeds text in the vector space the head was trained on.
func zzHeadEmbed(emb embedding.Embedder, hd *zzHead, text string) ([]float32, error) {
	if hd != nil && hd.EmbedRole == "classification" {
		return embedding.EmbedAs(emb, text, embedding.RoleClassification)
	}
	return emb.Embed(text)
}

func zzLoadHead(t *testing.T, p string) *zzHead {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var h zzHead
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	if h.Temperature == 0 {
		h.Temperature = 1
	}
	return &h
}

func (h *zzHead) Predict(v []float32) []float64 {
	x := make([]float64, len(v))
	var n float64
	for i, f := range v {
		x[i] = float64(f)
		n += x[i] * x[i]
	}
	if h.L2Norm && n > 0 {
		n = math.Sqrt(n)
		for i := range x {
			x[i] /= n
		}
	}
	if len(h.Mean) == len(x) {
		for i := range x {
			x[i] = (x[i] - h.Mean[i]) / h.Scale[i]
		}
	}
	if len(h.W1) > 0 {
		hid := make([]float64, len(h.W1))
		for j, row := range h.W1 {
			s := h.B1[j]
			for i, w := range row {
				s += w * x[i]
			}
			if s < 0 {
				s = 0
			}
			hid[j] = s
		}
		x = hid
	}
	logits := make([]float64, len(h.W2))
	mx := math.Inf(-1)
	for c, row := range h.W2 {
		s := h.B2[c]
		for i, w := range row {
			s += w * x[i]
		}
		logits[c] = s / h.Temperature
		if logits[c] > mx {
			mx = logits[c]
		}
	}
	var z float64
	for c := range logits {
		logits[c] = math.Exp(logits[c] - mx)
		z += logits[c]
	}
	for c := range logits {
		logits[c] /= z
	}
	return logits
}

func zzTreeSchema() map[string]any {
	var labels []string
	for _, d := range DefaultDefinitions() {
		labels = append(labels, string(d.Label))
	}
	return map[string]any{
		"type": "object", "required": []string{"top"}, "additionalProperties": false,
		"properties": map[string]any{"top": map[string]any{
			"type": "array", "minItems": 1, "maxItems": 3,
			"items": map[string]any{"type": "object", "required": []string{"skill", "score", "workflow_type"}, "additionalProperties": false,
				"properties": map[string]any{
					"skill":         map[string]any{"type": "string", "enum": labels},
					"score":         map[string]any{"type": "number"},
					"workflow_type": map[string]any{"type": "string"},
				}},
		}},
	}
}
