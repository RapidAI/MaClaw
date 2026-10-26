package intent

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
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

// headProbEps bounds the difference between a head's probabilities and an
// independent reference. Head weights are float32 so the dense layers can use
// the SIMD dot kernels; that shifts probabilities by ~2e-8. The head's
// calibrated thresholds are O(1e-2), and a structural error (wrong layering,
// missing bias, wrong activation) would move probabilities by O(0.1), so this
// bound still catches real regressions.
const headProbEps = 1e-6

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
			if math.Abs(got[i]-want[i]) > headProbEps {
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
	// This test pins the float32 forward against the reference implementation;
	// the auto policy would route a head larger than L3 through int8 and shift
	// probabilities by ~1e-3. Int8 parity has its own test.
	t.Setenv("MACLAW_HEAD_Q8P", "0")
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
				if math.Abs(got[i]-want[i]) > headProbEps {
					t.Fatalf("%s prob %d: %v vs %v", name, i, got[i], want[i])
				}
			}
		}
		t.Logf("%s id=%s dim=%d labels=%d tau=%.3f tau_s=%.3f", name, h.ID, h.Dim(), len(h.Labels), h.Tau, h.TauS)
	}
}

// TestLocalHeadQ8PMatchesFloatPath pins the optional VNNI hidden-layer path
// (MACLAW_HEAD_Q8P) against the float32 one. The head's thresholds are
// calibrated, so this path must stay numerically close or it silently changes
// routing decisions on machines where it is enabled.
func TestLocalHeadQ8PMatchesFloatPath(t *testing.T) {
	if !tensor.Q8PAvailable() {
		t.Skip("Q8P kernel unavailable on this CPU")
	}
	// dim must be a multiple of 32 for the Q8P tile; hidden is padded.
	labels := []string{"a", "b", "c"}
	var w1 [][]float64
	rnd := rand.New(rand.NewSource(5))
	for h := 0; h < 8; h++ {
		row := make([]float64, 32)
		for i := range row {
			row[i] = rnd.NormFloat64() * 0.2
		}
		w1 = append(w1, row)
	}
	w2 := make([][]float64, len(labels))
	b2 := make([]float64, len(labels))
	for k := range w2 {
		row := make([]float64, 8)
		for i := range row {
			row[i] = rnd.NormFloat64() * 0.3
		}
		w2[k] = row
	}
	file := map[string]any{
		"labels": labels, "w1": w1,
		"b1": make([]float64, len(w1)),
		"w2": w2, "b2": b2,
		"temperature": 1.0, "tau": 0.5, "l2norm": true, "embed_role": "classification",
	}
	b, _ := json.Marshal(file)

	t.Setenv("MACLAW_HEAD_Q8P", "0")
	ref, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	v := make([]float32, 32)
	for i := range v {
		v[i] = float32(rnd.NormFloat64())
	}
	want, err := ref.Predict(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"1", "2"} {
		t.Setenv("MACLAW_HEAD_Q8P", mode)
		q8, err := ParseLocalHead(b)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if q8.w1q == nil {
			t.Fatalf("mode %s: Q8P path did not engage", mode)
		}
		got, err := q8.Predict(v)
		if err != nil {
			t.Fatal(err)
		}
		maxD := 0.0
		for i := range want {
			if d := math.Abs(want[i] - got[i]); d > maxD {
				maxD = d
			}
		}
		// Single-pass activations are coarser than two-pass; both must stay
		// well inside the O(1e-2) threshold band the heads are calibrated for.
		limit := 0.02
		if mode == "2" {
			limit = 0.01
		}
		if maxD > limit {
			t.Fatalf("mode %s: max probability delta %.4g > %.2g", mode, maxD, limit)
		}
		t.Logf("mode %s: max delta %.4g", mode, maxD)
	}
}

// L3CacheBytesForTest exposes the detected last-level cache size so the
// platform probes can be checked on real hardware.
func L3CacheBytesForTest() int64 { return l3CacheBytes() }

// TestHeadQ8PPolicy pins the auto decision at the boundaries that matter.
func TestHeadQ8PPolicy(t *testing.T) {
	l3 := l3CacheBytes()
	t.Logf("detected L3 = %d MB", l3>>20)
	// Force auto for every case.
	t.Setenv("MACLAW_HEAD_Q8P", "auto")
	use, two, forced := headQ8PPolicy(22_800_000) // the shipped r13 hidden matrix
	if l3 <= 0 {
		if use {
			t.Fatal("unknown L3 must keep float32")
		}
		return
	}
	want := int64(22_800_000) > l3
	if use != want {
		t.Fatalf("f32 22.8MB with L3 %d: use=%v want=%v", l3, use, want)
	}
	if forced {
		t.Fatal("auto must not report itself as forced")
	}
	if use && !two {
		t.Fatal("auto must start on two-pass (verification may downgrade it)")
	}
	// A matrix that fits in L3 must keep float32: int8 would only save a
	// cache-resident read while costing accuracy.
	if u, _, _ := headQ8PPolicy(l3 / 2); u {
		t.Fatalf("f32 %d bytes inside L3 %d must stay float32", l3/2, l3)
	}
	// Explicit overrides win regardless of hardware.
	for _, c := range []struct {
		env      string
		use, two bool
	}{
		{"0", false, false}, {"off", false, false},
		{"1", true, false}, {"single", true, false},
		{"2", true, true}, {"two", true, true},
	} {
		t.Setenv("MACLAW_HEAD_Q8P", c.env)
		u, tw, f := headQ8PPolicy(1 << 30)
		if u != c.use || tw != c.two || !f {
			t.Errorf("MACLAW_HEAD_Q8P=%q: (%v,%v,%v) want (%v,%v,true)", c.env, u, tw, f, c.use, c.two)
		}
	}
}

// TestChooseQ8PMode pins the accuracy gates of the load-time self-check.
func TestChooseQ8PMode(t *testing.T) {
	for _, c := range []struct {
		single, two float64
		wantTwo     bool
		wantOK      bool
	}{
		{0, 5e-3, false, true},         // exact enough: take the cheap mode
		{9e-4, 9e-3, false, true},      // at the single-pass gate
		{2e-3, 5e-3, true, true},       // single too loose, two-pass fine
		{2e-3, 2e-2, false, false},     // neither passes
		{math.NaN(), 5e-3, true, true}, // NaN single falls through to two-pass
		{math.Inf(1), math.Inf(1), false, false},
	} {
		two, ok := chooseQ8PMode(c.single, c.two)
		if two != c.wantTwo || ok != c.wantOK {
			t.Errorf("chooseQ8PMode(%v,%v) = (%v,%v) want (%v,%v)",
				c.single, c.two, two, ok, c.wantTwo, c.wantOK)
		}
	}
}

// TestFinishQ8PSetupVerifies checks that an auto-packaged int8 path is checked
// against float32 before the float copy is dropped, and that the float path is
// restored when the chosen mode does not match.
func TestFinishQ8PSetupVerifies(t *testing.T) {
	// Load without int8 first so the float weights survive for the probe.
	t.Setenv("MACLAW_HEAD_Q8P", "0")
	h, err := ParseLocalHead(tinyHeadJSON(t, []string{"a", "b", "c"}, 32, 4, 0.5, 0.8, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_HEAD_Q8P", "1")
	h.setupHeadQ8P()
	if h.w1q == nil {
		t.Fatal("forced int8 path was not packed")
	}
	// Pretend auto selected it: the loader then verifies before dropping w1.
	// The probe is deterministic, so measuring now must match its verdict.
	single, two := h.q8pProbeDeltas(h.q8pProbeInputs(), false)
	h.q8pPending = true
	h.finishQ8PSetup()
	if h.w1q == nil {
		// Rejected outright is a valid outcome; the float path must still work.
		if len(h.w1) == 0 {
			t.Fatal("int8 rejected but float32 weights were dropped")
		}
		if _, err := h.Predict(make([]float32, 32)); err != nil {
			t.Fatalf("float path after rejection: %v", err)
		}
		return
	}
	if h.w1 != nil {
		t.Fatal("float32 copy must be dropped after a verified int8 path")
	}
	useTwo, ok := chooseQ8PMode(single, two)
	if !ok {
		t.Fatal("int8 kept although neither mode passes the accuracy gates")
	}
	if useTwo != h.headQ8P2 {
		t.Fatalf("chosen two-pass=%v, probe says %v", h.headQ8P2, useTwo)
	}
}
