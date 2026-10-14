package embedding

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// qwen3GoldenTexts covers the retrieval (query/none), Layer 2 classification
// and KB-similarity roles, short and long, with and without the prefix cache.
var qwen3GoldenTexts = []string{"向 znsoft@163.com 发邮件报告系统状态", "查询 api2 服务器状态", "hello", "帮我查一下明天北京的天气",
	"The quick brown fox jumps over the lazy dog while the build server reruns the full integration suite for the third time today.",
	"把这份季度报告整理成PDF然后发给财务部的王经理，抄送给我自己"}

// TestQwen3RetrievalGolden: MACLAW_QWEN3_GOLDEN=path. If the file is missing
// it is written (run on the base commit); otherwise Embed / EmbedWithRole /
// EmbedBatchWithRole must reproduce it byte for byte.
func TestQwen3RetrievalGolden(t *testing.T) {
	gp := os.Getenv("MACLAW_QWEN3_GOLDEN")
	if gp == "" {
		t.Skip("MACLAW_QWEN3_GOLDEN not set")
	}
	// own instance: keeps the shared embedder's prefix cache untouched for other tests
	e, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var buf bytes.Buffer
	put := func(v []float32, err error) {
		if err != nil {
			t.Fatal(err)
		}
		binary.Write(&buf, binary.LittleEndian, v)
	}
	for pass := 0; pass < 2; pass++ { // pass 1: prefix caches warm
		for _, txt := range qwen3GoldenTexts {
			put(e.Embed(txt))
			for _, r := range []Role{RoleQuery, RoleClassification, RoleSimilarity} {
				put(e.EmbedWithRole(txt, r))
			}
		}
		for _, r := range []Role{RoleNone, RoleQuery, RoleClassification} {
			vs, err := e.EmbedBatchWithRole(qwen3GoldenTexts, r)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vs {
				put(v, nil)
			}
		}
	}
	old, err := os.ReadFile(gp)
	if os.IsNotExist(err) {
		if err := os.WriteFile(gp, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s (%d bytes)", gp, buf.Len())
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, buf.Bytes()) {
		t.Fatalf("retrieval/L2 vectors changed vs golden %s (%d vs %d bytes)", gp, len(old), buf.Len())
	}
	t.Logf("byte-identical to golden (%d bytes)", buf.Len())
}
