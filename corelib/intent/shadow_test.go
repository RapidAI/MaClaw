package intent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// shadowTestHead is a tiny 8-dim head over the labels the spy classifier uses.
func shadowTestHead(t testing.TB) *LocalHead {
	t.Helper()
	h, err := ParseLocalHead(tinyHeadJSON(t, []string{"office", "coding", "unknown"}, 8, 4, 0.5, 0.9, []string{"coding"}))
	if err != nil {
		t.Fatal(err)
	}
	h.ID = "test-head@0"
	return h
}

// newShadowSpyClassifier builds a classifier whose Layer 2 is always ambiguous
// for ordinary text (two orthogonal anchors on the spy embedder), so every
// turn escalates to llm.
func newShadowSpyClassifier(t testing.TB, llm LLMClassifyContextFunc, shadow *ShadowConfig) *UnifiedIntentClassifier {
	t.Helper()
	if shadow == nil {
		shadow = &ShadowConfig{} // disabled; do not read the environment
	}
	uic := New(Config{Embedder: &roleSpyEmbedder{modelID: "spy:8"}, LLMContextFunc: llm, FusionTreeDeadline: 80 * time.Millisecond, Shadow: shadow})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}}},
		{Label: LabelCoding, Vecs: [][]float32{{0, 1, 0, 0, 0, 0, 0, 0}}},
	}
	uic.mu.Unlock()
	t.Cleanup(uic.CloseShadow)
	return uic
}

func okOfficeLLM(ctx, parent context.Context, _, _ string) (string, error) {
	return `{"top":[{"skill":"office","score":0.92},{"skill":"coding","score":0.30}]}`, nil
}

func readShadowRecords(t testing.TB, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad jsonl line %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

func get(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func TestShadowOffIsNoop(t *testing.T) {
	t.Setenv("MACLAW_INTENT_SHADOW", "")
	dir := t.TempDir()
	t.Setenv("MACLAW_INTENT_SHADOW_LOG", dir)
	uic := New(Config{Embedder: &roleSpyEmbedder{modelID: "spy:8"}, LLMContextFunc: okOfficeLLM})
	if uic.shadow.Load() != nil {
		t.Fatal("shadow must be off by default")
	}
	if _, ok := uic.ShadowStats(); ok {
		t.Fatal("ShadowStats ok when off")
	}
	if uic.ShadowLogPath() != "" {
		t.Fatal("log path when off")
	}
	uic.CloseShadow() // safe when off
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("shadow off created files: %v", entries)
	}
}

func TestShadowEnvEnables(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_INTENT_SHADOW", "1")
	t.Setenv("MACLAW_INTENT_SHADOW_LOG", dir)
	t.Setenv("MACLAW_INTENT_SHADOW_TEXT", "none")
	t.Setenv("MACLAW_INTENT_SHADOW_L2_SAMPLE", "0.25")
	cfg, ok := ShadowConfigFromEnv()
	if !ok || cfg.TextMode != "none" || cfg.L2SampleRate != 0.25 {
		t.Fatalf("env config %+v %v", cfg, ok)
	}
	uic := New(Config{Embedder: &roleSpyEmbedder{modelID: "spy:8"}, LLMContextFunc: okOfficeLLM})
	defer uic.CloseShadow()
	if got, want := uic.ShadowLogPath(), filepath.Join(dir, "intent_shadow.jsonl"); got != want {
		t.Fatalf("log path %q want %q", got, want)
	}
}

// TestShadowRecordsAndIdenticalResult checks the record contents and that the
// shadow-on classifier returns exactly what the shadow-off one returns.
func TestShadowRecordsAndIdenticalResult(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "s.jsonl")
	head := shadowTestHead(t)
	on := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: head, LogPath: logPath, StoreEmbedding: true, Salt: []byte("0123456789abcdef")})
	off := newShadowSpyClassifier(t, okOfficeLLM, nil)
	texts := []string{"please help me sort out the quarterly numbers for the team", "帮我整理一下这个季度的数据表格"}
	for _, text := range texts {
		a := on.ClassifyContext(context.Background(), MessageContext{Text: text})
		b := off.ClassifyContext(context.Background(), MessageContext{Text: text})
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("shadow changed the result:\n on=%+v\noff=%+v", a, b)
		}
		if a.Primary != LabelOffice || a.Layer != 3 {
			t.Fatalf("unexpected result %+v", a)
		}
	}
	on.CloseShadow()
	st, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v", st.Mode().Perm())
	}
	recs := readShadowRecords(t, logPath)
	if len(recs) != len(texts) {
		t.Fatalf("records %d want %d", len(recs), len(texts))
	}
	r := recs[0]
	checks := map[string]any{
		"kind": "l3", "model_id": "spy:8", "head_id": "test-head@0", "decided_by": "l3",
	}
	for k, v := range checks {
		if r[k] != v {
			t.Fatalf("%s = %v want %v (rec %v)", k, r[k], v, r)
		}
	}
	if get(r, "remote", "outcome") != "ok" || get(r, "remote", "label") != "office" || get(r, "remote", "called") != true {
		t.Fatalf("remote section %v", r["remote"])
	}
	if get(r, "final", "label") != "office" {
		t.Fatalf("final %v", r["final"])
	}
	if topk, _ := get(r, "l2", "topk").([]any); len(topk) != 2 {
		t.Fatalf("l2 topk %v", get(r, "l2"))
	}
	if get(r, "local", "label") == nil || get(r, "local", "decision") == nil {
		t.Fatalf("local section %v", r["local"])
	}
	if topk, _ := get(r, "local", "topk").([]any); len(topk) != 3 {
		t.Fatalf("local topk %v", get(r, "local"))
	}
	if _, ok := r["agree"].(bool); !ok {
		t.Fatalf("agree missing: %v", r)
	}
	if get(r, "text", "mode") != "hash" || get(r, "text", "full") != nil {
		t.Fatalf("text %v", r["text"])
	}
	if h, _ := get(r, "text", "hash").(string); len(h) != 32 {
		t.Fatalf("hash %v", r["text"])
	}
	// Local head result must match a direct forward on the same vector.
	vec, _ := embedIntentQuery(&roleSpyEmbedder{}, texts[0])
	probs, _ := head.Predict(vec)
	label, p, dec, _ := head.Decide(probs)
	if get(r, "local", "label") != label || math.Abs(get(r, "local", "p").(float64)-p) > 1e-12 || get(r, "local", "decision") != dec {
		t.Fatalf("local %v, direct %s %.6f %s", r["local"], label, p, dec)
	}
	// float16 embedding round-trips within f16 precision.
	raw, err := base64.StdEncoding.DecodeString(get(r, "emb", "b64").(string))
	if err != nil || len(raw) != 16 {
		t.Fatalf("emb %v %v", err, len(raw))
	}
	for i := range vec {
		got := f16ToFloat32(binary.LittleEndian.Uint16(raw[2*i:]))
		if math.Abs(float64(got-vec[i])) > 1e-3*math.Abs(float64(vec[i]))+1e-4 {
			t.Fatalf("f16[%d] %v vs %v", i, got, vec[i])
		}
	}
}

func f16ToFloat32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch {
	case exp == 0:
		return math.Float32frombits(sign) + float32(math.Ldexp(float64(mant), -24))*float32(1-2*int(sign>>31))
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
	return math.Float32frombits(sign | (exp-15+127)<<23 | mant<<13)
}

func TestShadowTextModes(t *testing.T) {
	for _, mode := range []string{ShadowTextFull, ShadowTextNone} {
		dir := t.TempDir()
		p := filepath.Join(dir, "s.jsonl")
		u := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p, TextMode: mode})
		const text = "please help me sort out the quarterly numbers"
		u.ClassifyContext(context.Background(), MessageContext{Text: text})
		u.CloseShadow()
		r := readShadowRecords(t, p)[0]
		switch mode {
		case ShadowTextFull:
			if get(r, "text", "full") != text {
				t.Fatalf("full text missing: %v", r["text"])
			}
		case ShadowTextNone:
			if get(r, "text", "full") != nil || get(r, "text", "hash") != nil {
				t.Fatalf("none mode leaked: %v", r["text"])
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".intent_shadow_salt")); err == nil {
			t.Fatalf("%s mode must not create a salt", mode)
		}
	}
	// hash mode creates a 0600 salt file and hashes are stable across reopen
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	var hashes []any
	for i := 0; i < 2; i++ {
		u := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, LogPath: p})
		u.ClassifyContext(context.Background(), MessageContext{Text: "same text every time please"})
		u.CloseShadow()
	}
	for _, r := range readShadowRecords(t, p) {
		hashes = append(hashes, get(r, "text", "hash"))
		if get(r, "local", "error") != "no_head" {
			t.Fatalf("no-head record %v", r["local"])
		}
	}
	if len(hashes) != 2 || hashes[0] != hashes[1] || hashes[0] == nil {
		t.Fatalf("hashes %v", hashes)
	}
	st, err := os.Stat(filepath.Join(dir, ".intent_shadow_salt"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("salt %v %v", err, st)
	}
}

// TestShadowRemoteTimeoutAndFailure: outcomes are classified, the skip
// window behaves exactly as with shadow off, and later skipped turns are logged.
func TestShadowRemoteTimeoutAndFailure(t *testing.T) {
	cases := []struct {
		name     string
		llm      LLMClassifyContextFunc
		outcome  string
		endpoint bool
	}{
		{"timeout", func(ctx, _ context.Context, _, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() }, "timeout", true},
		{"5xx", func(ctx, _ context.Context, _, _ string) (string, error) { return "", &httpStatusError{status: 503} }, "http_5xx", true},
		{"4xx", func(ctx, _ context.Context, _, _ string) (string, error) { return "", &httpStatusError{status: 401} }, "http_4xx", false},
		{"error", func(ctx, _ context.Context, _, _ string) (string, error) {
			return "", errors.New("dial tcp: connection refused")
		}, "error", false},
		{"protocol", func(ctx, _ context.Context, _, _ string) (string, error) { return "I think it is office.", nil }, "protocol", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "s.jsonl")
			on := newShadowSpyClassifier(t, c.llm, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p})
			off := newShadowSpyClassifier(t, c.llm, nil)
			msgs := []string{"first ambiguous request about numbers", "second different request entirely"}
			for _, m := range msgs {
				a := on.ClassifyContext(context.Background(), MessageContext{Text: m})
				b := off.ClassifyContext(context.Background(), MessageContext{Text: m})
				a.Reason, b.Reason = "", "" // reasons embed the measured timeout duration text only
				if !reflect.DeepEqual(a, b) {
					t.Fatalf("result differs on/off:\n on=%+v\noff=%+v", a, b)
				}
			}
			if (on.treeEndpointFailAt.Load() != 0) != (off.treeEndpointFailAt.Load() != 0) {
				t.Fatal("skip-window state differs with shadow on")
			}
			if (on.treeEndpointFailAt.Load() != 0) != c.endpoint {
				t.Fatalf("skip window armed=%v want %v", on.treeEndpointFailAt.Load() != 0, c.endpoint)
			}
			on.CloseShadow()
			recs := readShadowRecords(t, p)
			if len(recs) != 2 {
				t.Fatalf("records %d", len(recs))
			}
			r := recs[0]
			if get(r, "remote", "outcome") != c.outcome || get(r, "remote", "called") != true {
				t.Fatalf("remote %v", r["remote"])
			}
			if ep, _ := get(r, "remote", "endpoint_failure").(bool); ep != c.endpoint {
				t.Fatalf("endpoint_failure %v want %v", ep, c.endpoint)
			}
			if get(r, "remote", "error") != nil {
				t.Fatalf("error text logged outside full mode: %v", r["remote"])
			}
			if get(r, "local", "label") == nil {
				t.Fatalf("local head must still run: %v", r["local"])
			}
			if _, ok := r["agree"]; ok {
				t.Fatal("agree must be absent when remote failed")
			}
			if c.endpoint {
				if get(recs[1], "remote", "outcome") != "skipped_endpoint_window" || get(recs[1], "remote", "called") == true {
					t.Fatalf("second turn should be skipped by the window: %v", recs[1]["remote"])
				}
			}
		})
	}
}

func TestShadowCancelledTurn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	llm := func(c, _ context.Context, _, _ string) (string, error) { cancel(); <-c.Done(); return "", c.Err() }
	u := newShadowSpyClassifier(t, llm, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p})
	u.ClassifyContext(ctx, MessageContext{Text: "a request that gets cancelled"})
	if u.treeEndpointFailAt.Load() != 0 {
		t.Fatal("cancellation must not arm the skip window")
	}
	u.CloseShadow()
	r := readShadowRecords(t, p)[0]
	if get(r, "remote", "outcome") != "cancelled" {
		t.Fatalf("remote %v", r["remote"])
	}
}

func TestShadowDropsWhenQueueFull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	u := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p, QueueSize: 1})
	l := u.shadow.Load()
	release := make(chan struct{})
	entered := make(chan struct{}, 16)
	hook := func() { entered <- struct{}{}; <-release }
	l.testHook.Store(&hook)
	const n = 6
	start := time.Now()
	u.ClassifyContext(context.Background(), MessageContext{Text: "turn number 0 please"})
	<-entered // worker holds job 0
	for i := 1; i < n; i++ {
		u.ClassifyContext(context.Background(), MessageContext{Text: fmt.Sprintf("turn number %d please", i)})
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("classifier blocked on a full shadow queue")
	}
	s, _ := u.ShadowStats()
	if s.Dropped != n-2 || s.Enqueued != 2 {
		t.Fatalf("stats %+v, want enqueued 2 dropped %d", s, n-2)
	}
	close(release)
	u.CloseShadow() // flushes the queued record
	if recs := readShadowRecords(t, p); len(recs) != 2 {
		t.Fatalf("records after close %d, want 2", len(recs))
	}
	if s := l.stats(); s.Written != 2 {
		t.Fatalf("written %d", s.Written)
	}
}

func TestShadowHeadPanicDoesNotAffectClassifier(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	head := shadowTestHead(t)
	head.w2 = []float64{0} // corrupt: Predict indexes out of range
	on := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: head, LogPath: p})
	off := newShadowSpyClassifier(t, okOfficeLLM, nil)
	const text = "please help me sort out the quarterly numbers for the team"
	a := on.ClassifyContext(context.Background(), MessageContext{Text: text})
	b := off.ClassifyContext(context.Background(), MessageContext{Text: text})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("head panic changed the result: %+v vs %+v", a, b)
	}
	// A panicking worker hook is also contained.
	l := on.shadow.Load()
	var once sync.Once
	hook := func() { once.Do(func() { panic("boom") }) }
	l.testHook.Store(&hook)
	on.ClassifyContext(context.Background(), MessageContext{Text: "another ambiguous turn here"})
	on.ClassifyContext(context.Background(), MessageContext{Text: "and a third ambiguous turn"})
	on.CloseShadow()
	recs := readShadowRecords(t, p)
	if len(recs) != 2 {
		t.Fatalf("records %d want 2 (one lost to the hook panic)", len(recs))
	}
	if get(recs[0], "local", "error") != "panic" || get(recs[0], "remote", "label") != "office" {
		t.Fatalf("panic record %v", recs[0])
	}
	if s := l.stats(); s.Errors != 3 { // two head panics + one hook panic
		t.Fatalf("errors %d want 3", s.Errors)
	}
}

func TestShadowL2Sample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	u := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p, L2SampleRate: 1})
	const text = "make the slides"
	vec, _ := embedIntentQuery(&roleSpyEmbedder{}, text)
	u.mu.Lock()
	u.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{vec}},
		{Label: LabelCoding, Vecs: [][]float32{{0, 0, 0, 0, 0, 0, 0, 1}}},
	}
	u.mu.Unlock()
	res := u.ClassifyContext(context.Background(), MessageContext{Text: text})
	if res.Layer != 2 {
		t.Fatalf("expected a confident L2 turn, got %+v", res)
	}
	u.CloseShadow()
	r := readShadowRecords(t, p)[0]
	if r["kind"] != "l2_sample" || r["decided_by"] != "l2" || get(r, "remote", "outcome") != "not_reached" || get(r, "l2", "confident") != true {
		t.Fatalf("l2 sample record %v", r)
	}
	if get(r, "local", "label") == nil {
		t.Fatal("local head must run on sampled L2 turns")
	}
	// rate 0: confident L2 turns are not logged
	p2 := filepath.Join(t.TempDir(), "s.jsonl")
	u2 := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, LogPath: p2})
	u2.mu.Lock()
	u2.anchors = u.anchors
	u2.mu.Unlock()
	u2.ClassifyContext(context.Background(), MessageContext{Text: text})
	u2.CloseShadow()
	if recs := readShadowRecords(t, p2); len(recs) != 0 {
		t.Fatalf("rate 0 logged %d", len(recs))
	}
}

func TestShadowRotation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	u := newShadowSpyClassifier(t, okOfficeLLM, &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p, MaxBytes: 1500, MaxBackups: 2, QueueSize: 64})
	for i := 0; i < 20; i++ {
		u.ClassifyContext(context.Background(), MessageContext{Text: fmt.Sprintf("rotation turn %d with some words", i)})
	}
	u.CloseShadow()
	total := 0
	for _, name := range []string{"s.jsonl", "s.jsonl.1", "s.jsonl.2"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", name, st.Mode().Perm())
		}
		if st.Size() > 1500 {
			t.Fatalf("%s size %d > max", name, st.Size())
		}
		total += len(readShadowRecords(t, filepath.Join(dir, name)))
	}
	if _, err := os.Stat(filepath.Join(dir, "s.jsonl.3")); err == nil {
		t.Fatal("more backups than configured")
	}
	if total == 0 || total >= 20 {
		t.Fatalf("kept %d records (oldest should be dropped)", total)
	}
}

// TestShadowConcurrent runs many turns from several goroutines across two
// classifiers sharing one log file (run with -race).
func TestShadowConcurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	cfg := &ShadowConfig{Enabled: true, Head: shadowTestHead(t), LogPath: p, QueueSize: 4096, L2SampleRate: 0.5}
	a := newShadowSpyClassifier(t, okOfficeLLM, cfg)
	b := newShadowSpyClassifier(t, okOfficeLLM, cfg)
	if a.shadow.Load() != b.shadow.Load() {
		t.Fatal("same log path must share one logger")
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			u := a
			if g%2 == 1 {
				u = b
			}
			for i := 0; i < 25; i++ {
				u.ClassifyContext(context.Background(), MessageContext{Text: fmt.Sprintf("goroutine %d turn %d text", g, i)})
			}
		}(g)
	}
	// Close one classifier mid-flight: the shared logger stays open for the other.
	a.CloseShadow()
	wg.Wait()
	l := b.shadow.Load()
	b.CloseShadow()
	s := l.stats()
	recs := readShadowRecords(t, p)
	if uint64(len(recs)) != s.Written || s.Written != s.Enqueued || s.Errors != 0 {
		t.Fatalf("records %d stats %+v", len(recs), s)
	}
	if s.Written+s.Dropped == 0 {
		t.Fatal("nothing logged")
	}
}
