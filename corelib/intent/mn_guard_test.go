package intent

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const bundledHeadDir = "/workspace/maclaw_reranker/models/intent_heads"

func mnHeadDir(t *testing.T) string {
	if d := os.Getenv("MACLAW_MN_GUARD_DIR"); d != "" {
		return d
	}
	if !MNGuardHeadAvailable(bundledHeadDir) {
		t.Skip("bundled MN guard head not present")
	}
	return bundledHeadDir
}

// Exported ensemble head reproduces the Python C_qo seed-mean + TC softmax.
func TestMNGuardHeadParity(t *testing.T) {
	h, err := LoadMNGuardHead(mnHeadDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if h.Tau != MNGuardTau || len(h.Labels) != 53 || len(h.InputBlocks) != 2 {
		t.Fatalf("tau %v labels %d dim %d", h.Tau, len(h.Labels), h.Dim())
	}
	b, err := os.ReadFile(filepath.Join("testdata", "mn_guard_parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Vecs  [][]float32 `json:"vecs"`
		Probs [][]float64 `json:"probs"`
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	for i, v := range fx.Vecs {
		p, err := h.Predict(v)
		if err != nil {
			t.Fatal(err)
		}
		for k := range p {
			if math.Abs(p[k]-fx.Probs[i][k]) > 1e-4 {
				t.Fatalf("vec %d class %s: go %.6f py %.6f", i, h.Labels[k], p[k], fx.Probs[i][k])
			}
		}
	}
	for _, s := range []string{"ssh", "shell_command", "mailbox_send", "file_delete", "browser"} {
		if !h.IsSensitive(s) {
			t.Fatalf("%s must be sensitive", s)
		}
	}
}

func guardHead(t *testing.T) *LocalHead {
	t.Helper()
	m := map[string]any{"labels": []string{"file_write", "ssh", "search"},
		"w2": [][]float64{{1, 0}, {0, 1}, {0, 0}}, "b2": []float64{0, 0, 0},
		"temperature": 1.0, "tau": MNGuardTau, "tau_s": 2.0, "l2norm": true, "sensitive": []string{"ssh"}}
	b, _ := json.Marshal(m)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestMNGuardDecide(t *testing.T) {
	h := guardHead(t)
	cases := []struct {
		p      []float64
		label  string
		accept bool
	}{
		{[]float64{.98, .01, .01}, "file_write", true},
		{[]float64{.971, .019, .01}, "file_write", true},
		{[]float64{.96, .03, .01}, "file_write", false}, // below τ
		{[]float64{.001, .998, .001}, "ssh", false},     // sensitive never local
	}
	for _, c := range cases {
		l, _, ok := MNGuardDecide(h, c.p)
		if l != c.label || ok != c.accept {
			t.Fatalf("%v: got %s/%v want %s/%v", c.p, l, ok, c.label, c.accept)
		}
	}
}
