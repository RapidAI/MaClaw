package embedding

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// TestQwen3Q8PMatchesFloatPath: the int8-activation (Q8P) runner vs the
// float-activation runner on the same model. Gate: cos ≥ 0.995 per text
// for single-pass (section 12 measured min 0.9964 over 2365 texts), and
// ≥ 0.99999 for two-pass (the default when MACLAW_QWEN3_Q8P_2PASS is unset).
func TestQwen3Q8PMatchesFloatPath(t *testing.T) {
	if !tensor.Q8PAvailable() {
		t.Skip("Q8P kernel unavailable on this CPU")
	}
	path := qwen3TestModel(t)
	t.Setenv("MACLAW_QWEN3_Q8P", "0")
	ref, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	t.Setenv("MACLAW_QWEN3_Q8P", "1")
	texts := []string{"打开计算器吧", "open the door now", "帮我截个图",
		"背景是这样:下午三点要跟 vendor 开会,我把上次通话的记录已经贴在前面对话里了,现在要打印出来人手一份。麻烦排得正式一点,加个封面写 'Q2 Vendor Sync',导出 PDF 到 Downloads",
		"a", "C:\\Users\\me\\Desktop\\report.docx 帮我转成 pdf"}
	old := qwen3Q8PTwoPass
	defer func() { qwen3Q8PTwoPass = old }()
	for _, two := range []bool{false, true} {
		qwen3Q8PTwoPass = qwen3TwoPass{two, two, two, two}
		gate := 0.995
		if two {
			gate = 0.99999
		}
		// Fresh instance per mode: the instruct-prefix KV cache is built
		// with the mode active at first use.
		q8, err := NewQwen3Embedder(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer q8.Close()
		if ref.q8p || !q8.q8p {
			t.Fatalf("q8p flags: ref=%v q8=%v", ref.q8p, q8.q8p)
		}
		for _, s := range texts {
			for _, role := range []Role{RoleNone, RoleClassification} {
				a, err := EmbedAs(ref, s, role)
				if err != nil {
					t.Fatal(err)
				}
				b, err := EmbedAs(q8, s, role)
				if err != nil {
					t.Fatal(err)
				}
				if c := dot(a, b); c < gate {
					t.Fatalf("two=%v role=%v %q: cos %.6f < %.5f", two, role, s, c, gate)
				} else {
					t.Logf("two=%v role=%v %q: cos %.7f", two, role, s, c)
				}
			}
		}
	}
}
