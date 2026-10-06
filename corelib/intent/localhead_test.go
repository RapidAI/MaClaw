package intent

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// tinyHeadJSON builds a deterministic random head (dim D, hidden H) over labels.
func tinyHeadJSON(t testing.TB, labels []string, d, hid int, tau, tauS float64, sensitive []string) []byte {
	t.Helper()
	r := rand.New(rand.NewSource(7))
	mat := func(rows, cols int) [][]float64 {
		m := make([][]float64, rows)
		for i := range m {
			m[i] = make([]float64, cols)
			for j := range m[i] {
				m[i][j] = r.NormFloat64()
			}
		}
		return m
	}
	vec := func(n int) []float64 {
		v := make([]float64, n)
		for i := range v {
			v[i] = r.NormFloat64() * 0.1
		}
		return v
	}
	scale := make([]float64, d)
	for i := range scale {
		scale[i] = 0.5 + r.Float64()
	}
	f := map[string]any{
		"labels": labels, "w1": mat(hid, d), "b1": vec(hid), "w2": mat(len(labels), hid), "b2": vec(len(labels)),
		"mean": vec(d), "scale": scale, "temperature": 1.3, "tau": tau, "tau_s": tauS, "l2norm": true,
		"sensitive": sensitive, "embed_role": "classification",
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLocalHeadMatchesReferenceForward(t *testing.T) {
	labels := []string{"search", "ssh", "unknown", "office"}
	b := tinyHeadJSON(t, labels, 16, 8, 0.6, 0.9, []string{"ssh"})
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	var ref zzHead
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 50; n++ {
		v := make([]float32, 16)
		for i := range v {
			v[i] = float32(r.NormFloat64())
		}
		got, err := h.Predict(v)
		if err != nil {
			t.Fatal(err)
		}
		want := ref.Predict(v)
		var sum float64
		for i := range got {
			sum += got[i]
			if math.Abs(got[i]-want[i]) > 1e-12 {
				t.Fatalf("prob %d: got %v want %v", i, got[i], want[i])
			}
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatalf("probs sum %v", sum)
		}
	}
	if _, err := h.Predict(make([]float32, 15)); err == nil {
		t.Fatal("dim mismatch must error")
	}
}

func TestLocalHeadDecide(t *testing.T) {
	labels := []string{"search", "ssh", "unknown"}
	h, err := ParseLocalHead(tinyHeadJSON(t, labels, 4, 3, 0.6, 0.9, []string{"ssh"}))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		p    []float64
		dec  string
		thr  float64
		want string
	}{
		{[]float64{0.7, 0.2, 0.1}, LocalHeadAccept, 0.6, "search"},
		{[]float64{0.5, 0.3, 0.2}, LocalHeadAmbiguous, 0.6, "search"},
		{[]float64{0.1, 0.85, 0.05}, LocalHeadAmbiguous, 0.9, "ssh"}, // sensitive uses τ_s
		{[]float64{0.02, 0.95, 0.03}, LocalHeadAccept, 0.9, "ssh"},
		{[]float64{0.1, 0.1, 0.8}, LocalHeadUnknown, 0.6, "unknown"},
	}
	for _, c := range cases {
		l, _, dec, thr := h.Decide(c.p)
		if l != c.want || dec != c.dec || thr != c.thr {
			t.Fatalf("%v: got %s %s %v", c.p, l, dec, thr)
		}
	}
	top := h.TopK([]float64{0.1, 0.7, 0.2}, 2)
	if len(top) != 2 || top[0].Label != "ssh" || top[1].Label != "unknown" {
		t.Fatalf("topk %v", top)
	}
}

func TestLocalHeadRejectsBadShapes(t *testing.T) {
	for _, s := range []string{
		`{}`,
		`{"labels":["a","b"],"w2":[[1,2]],"b2":[0,0]}`,
		`{"labels":["a","b"],"w1":[[1,2],[3]],"b1":[0,0],"w2":[[1,2],[1,2]],"b2":[0,0]}`,
		`{"labels":["a","b"],"w2":[[1,2],[1,2]],"b2":[0,0],"mean":[0]}`,
	} {
		if _, err := ParseLocalHead([]byte(s)); err == nil {
			t.Fatalf("expected error for %s", s)
		}
	}
	h, err := ParseLocalHead([]byte(`{"labels":["a","b"],"w2":[[1,0],[0,1]],"b2":[0,0],"tau":0.5}`))
	if err != nil || h.Dim() != 2 || h.TauS != 0.5 {
		t.Fatalf("linear head: %v %+v", err, h)
	}
}

// TestLocalHeadRealFiles loads the trained heads when MACLAW_TEST_HEAD_DIR
// points at them (local only) and checks parity with the eval-harness forward.
func TestLocalHeadRealFiles(t *testing.T) {
	dir := os.Getenv("MACLAW_TEST_HEAD_DIR")
	if dir == "" {
		t.Skip("MACLAW_TEST_HEAD_DIR not set")
	}
	for _, name := range []string{"head_mlp_qwen3go_GK.json", "head_mlp_gemmacls_GK.json"} {
		p := filepath.Join(dir, name)
		h, err := LoadLocalHead(p)
		if err != nil {
			t.Fatal(err)
		}
		ref := zzLoadHead(t, p)
		r := rand.New(rand.NewSource(3))
		for n := 0; n < 20; n++ {
			v := make([]float32, h.Dim())
			for i := range v {
				v[i] = float32(r.NormFloat64())
			}
			got, _ := h.Predict(v)
			want := ref.Predict(v)
			for i := range got {
				if math.Abs(got[i]-want[i]) > 1e-12 {
					t.Fatalf("%s prob %d: %v vs %v", name, i, got[i], want[i])
				}
			}
		}
		t.Logf("%s id=%s dim=%d labels=%d tau=%.3f tau_s=%.3f", name, h.ID, h.Dim(), len(h.Labels), h.Tau, h.TauS)
	}
}
