package embedding

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"testing"
)

var qwen3PrefixTexts = []string{
	"你好", "帮我把这个文件发给张三", "I'll check the logs and won't restart it",
	"  leading and trailing spaces  ", "line one\nline two\r\nline three", "1234567890 数字",
	"emoji 👍🏽 ok", "ＦＵＬＬＷＩＤＴＨ　テスト", `C:\Users\me\Desktop\report.docx`, "tab\there  double  space",
	"<|endoftext|> literal", "café “quoted” naïve", "https://example.com/a?b=c", "x", " starts with space", ":colon first",
	"\nnewline first", "请把昨天会议的纪要整理成一份PDF，然后发到项目群里，同时提醒大家周五之前反馈意见，顺便查一下明天杭州的天气怎么样，要不要带伞，以及帮我订一个下午三点的会议室。",
}

func cos32(a, b []float32) float64 { return dot(a, b) }

// TestQwen3PrefixKVMatchesFullForward: with the instruct-prefix KV cache the
// vectors equal the full forward (cos >= 0.99999) and the token ids of the
// full prompt really start with the cached prefix ids.
func TestQwen3PrefixKVMatchesFullForward(t *testing.T) {
	e := loadQwen3(t)
	// The prefix cache is CPU-only machinery; a GPU backend bypasses it and
	// would make this comparison vacuous.
	disableGPU(t, e)
	for _, role := range []Role{RoleClassification, RoleQuery} {
		qwen3PrefixKVEnabled = false
		var ref [][]float32
		for _, txt := range qwen3PrefixTexts {
			v, err := e.EmbedWithRole(txt, role)
			if err != nil {
				t.Fatal(err)
			}
			ref = append(ref, v)
		}
		qwen3PrefixKVEnabled = true
		e.ensurePrefix(role)
		pfx := e.tokenizer.Encode(qwen3InstructPrefix(role))
		pfx = pfx[:len(pfx)-1]
		minCos := 1.0
		for i, txt := range qwen3PrefixTexts {
			ids := e.Tokenize(ApplyQwen3Prompt(txt, role))
			p := e.matchPrefix(ids)
			if p == nil || p.n != len(pfx) {
				t.Fatalf("role %v %q: prompt ids do not start with the cached prefix", role, txt)
			}
			v, err := e.EmbedWithRole(txt, role)
			if err != nil {
				t.Fatal(err)
			}
			c := cos32(v, ref[i])
			if c < minCos {
				minCos = c
			}
			if c < 0.99999 {
				t.Errorf("role %v %q: cos(prefix-KV, full) = %.7f", role, txt, c)
			}
		}
		t.Logf("role %v: prefix %d tokens, min cos %.8f over %d texts", role, len(pfx), minCos, len(qwen3PrefixTexts))
		// Batch path uses the cache too.
		vs, err := e.EmbedBatchWithRole(qwen3PrefixTexts, role)
		if err != nil {
			t.Fatal(err)
		}
		for i := range vs {
			if c := cos32(vs[i], ref[i]); c < 0.99999 {
				t.Errorf("batch role %v %q: cos = %.7f", role, qwen3PrefixTexts[i], c)
			}
		}
	}
	if n := e.prefixCacheLen(); n != 2 {
		t.Fatalf("prefix cache has %d entries, want 2", n)
	}
	// Roles without an instruct (document) never use the cache.
	if p := e.matchPrefix(e.Tokenize(ApplyQwen3Prompt("plain doc", RoleDocument))); p != nil {
		t.Fatal("document text matched an instruct prefix")
	}
}

// TestQwen3PrefixKVConcurrent: concurrent first use builds each prefix once
// and every goroutine gets the same vector.
func TestQwen3PrefixKVConcurrent(t *testing.T) {
	e, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// The prefix cache is CPU-only machinery; a GPU backend bypasses it and
	// would make this comparison vacuous.
	disableGPU(t, e)
	want, _ := e.EmbedWithRole("帮我把这个文件发给张三", RoleClassification)
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	// The forward pass reorders float32 accumulation with the worker pool's
	// reduction split, so every vector — including concurrent runs of the
	// same text — jitters at ~1e-7 cos. Cache coherence is what this test
	// pins: every run lands within the batch tolerance of the first build,
	// and the prefix cache still holds exactly one entry afterwards.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				v, err := e.EmbedWithRole("帮我把这个文件发给张三", RoleClassification)
				if err != nil {
					errs <- err.Error()
					return
				}
				if cos32(v, want) < 0.99999 {
					errs <- "concurrent vector diverged from the first build"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for m := range errs {
		t.Fatal(m)
	}
	if n := e.prefixCacheLen(); n != 1 {
		t.Fatalf("prefix cache has %d entries, want 1", n)
	}
}

// TestQwen3PrefixTokenBoundaryFile checks, for every text of a JSONL file
// (field "text"; MACLAW_QWEN3_PREFIX_CHECK_FILE), that the classification
// prompt's ids start with the cached prefix ids. Tokenizer only; fast.
func TestQwen3PrefixTokenBoundaryFile(t *testing.T) {
	path := os.Getenv("MACLAW_QWEN3_PREFIX_CHECK_FILE")
	if path == "" {
		t.Skip("MACLAW_QWEN3_PREFIX_CHECK_FILE not set")
	}
	e := loadQwen3(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, role := range []Role{RoleClassification, RoleQuery} {
		pfx := e.tokenizer.Encode(qwen3InstructPrefix(role))
		pfx = pfx[:len(pfx)-1]
		p := &qwen3PrefixKV{ids: pfx, n: len(pfx)}
		f.Seek(0, 0)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		n, bad := 0, 0
		for sc.Scan() {
			var r struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			n++
			if !p.matches(e.Tokenize(ApplyQwen3Prompt(r.Text, role))) {
				bad++
				t.Errorf("role %v: prefix boundary differs for %q", role, r.Text)
			}
		}
		t.Logf("role %v: %d/%d texts start with the %d-token cached prefix", role, n-bad, n, len(pfx))
	}
}
