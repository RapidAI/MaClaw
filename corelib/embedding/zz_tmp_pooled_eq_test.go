package embedding

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestZZPooledLastEqualsEmbed(t *testing.T) {
	p := os.Getenv("MACLAW_EMBED_MODEL_PATH")
	if p == "" {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, ".maclaw", "models", Qwen3ModelFilename)
	}
	q, err := NewQwen3Embedder(p, 0)
	if err != nil {
		t.Skipf("no model: %v", err)
	}
	defer q.Close()
	texts := []string{
		"帮我把这个文件发给张三，然后打开设置页面确认一下",
		"查询 api2 服务器状态",
		"read the config and restart the service",
	}
	for _, text := range texts {
		plain, err := EmbedAs(q, text, RoleClassification)
		if err != nil {
			t.Fatal(err)
		}
		last, mean, err := q.EmbedPooledWithRole(text, RoleClassification)
		if err != nil {
			t.Fatal(err)
		}
		var dot, na, nb float64
		for i := range plain {
			dot += float64(plain[i]) * float64(last[i])
			na += float64(plain[i]) * float64(plain[i])
			nb += float64(last[i]) * float64(last[i])
		}
		cos := dot / (math.Sqrt(na) * math.Sqrt(nb))
		var maxDiff float64
		for i := range plain {
			if d := math.Abs(float64(plain[i] - last[i])); d > maxDiff {
				maxDiff = d
			}
		}
		t.Logf("%-30.30q cos=%.10f maxDiff=%.3g len=%d mean=%d", text, cos, maxDiff, len(plain), len(mean))
	}
}
