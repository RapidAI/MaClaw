package embedding

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func findModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no gemma embedding model found")
	return ""
}

func TestGemmaEmbedder_Load(t *testing.T) {
	modelPath := findModel(t)
	t.Logf("loading model: %s", modelPath)

	start := time.Now()
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}
	defer emb.Close()
	t.Logf("model loaded in %v, dim=%d", time.Since(start), emb.Dim())
}

func TestGemmaEmbedder_Embed(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	texts := []string{"hello world", "你好世界", "embedding test"}
	for _, text := range texts {
		start := time.Now()
		vec, err := emb.Embed(text)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Embed(%q) failed: %v", text, err)
		}
		if len(vec) != 256 {
			t.Fatalf("Embed(%q) returned %d dims, want 256", text, len(vec))
		}

		// Check L2 norm ~= 1.0
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		norm = math.Sqrt(norm)
		t.Logf("Embed(%q): dim=%d norm=%.4f time=%v first5=%v",
			text, len(vec), norm, elapsed, vec[:5])

		if math.Abs(norm-1.0) > 0.01 {
			t.Errorf("L2 norm = %f, want ~1.0", norm)
		}

		// Check not all zeros
		allZero := true
		for _, v := range vec {
			if v != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Error("all-zero vector")
		}
	}
}

func TestGemmaEmbedder_Similarity(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	pairs := [][2]string{
		{"hello world", "hi there"},
		{"hello world", "quantum physics"},
		{"cat", "dog"},
		{"cat", "airplane"},
	}

	for _, pair := range pairs {
		v1, err := emb.Embed(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		v2, err := emb.Embed(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		sim := cosine(v1, v2)
		t.Logf("cosine(%q, %q) = %.4f", pair[0], pair[1], sim)
	}
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestGemmaEmbedder_PrintVec(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	vec, err := emb.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}
	// Print first 10 values for comparison with C++ version
	fmt.Printf("Go embedding first 10 values for 'hello world':\n")
	for i := 0; i < 10 && i < len(vec); i++ {
		fmt.Printf("  [%d] = %.8f\n", i, vec[i])
	}
}

func findModelBench(b *testing.B) string {
	b.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	b.Skip("no gemma embedding model found")
	return ""
}

// BenchmarkEmbed_Short benchmarks single short text embedding.
func BenchmarkEmbed_Short(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.Embed("hello world")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer() // Close/unmap is not Embed
}

// BenchmarkEmbed_Medium benchmarks a medium-length text (~50 tokens).
func BenchmarkEmbed_Medium(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	text := "The quick brown fox jumps over the lazy dog. This is a medium length sentence that should produce around fifty tokens for benchmarking the embedding model inference performance."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.Embed(text)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer() // Close/unmap is not Embed
}

// BenchmarkEmbedBatch benchmarks batch embedding of 8 texts.
func BenchmarkEmbedBatch(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	texts := []string{
		"hello world",
		"machine learning",
		"natural language processing",
		"deep neural networks",
		"transformer architecture",
		"attention mechanism",
		"embedding vectors",
		"semantic similarity",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.EmbedBatch(texts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEmbed_Long benchmarks a long text (~450 tokens) where attention
// (O(seq²·heads·dim)) starts to rival the GEMMs — the regime that motivates
// int8 Q·K attention kernels.
func BenchmarkEmbed_Long(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	sentence := "The quick brown fox jumps over the lazy dog while seventeen curious mathematicians " +
		"quietly rearrange their wooden bookshelves, sip lukewarm green tea, and argue about " +
		"whether deterministic finite automata can dream of non-deterministic sheep. "
	var sb strings.Builder
	for sb.Len() < 3200 {
		sb.WriteString(sentence)
	}
	text := sb.String()
	if n := len(emb.tokenizer.Encode(text)); n < 300 {
		b.Fatalf("long text tokenizes to only %d tokens, want >=300", n)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.Embed(text)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer() // Close/unmap is not Embed
}

func TestEmbedBatchDoesNotMutateMatMulParallel(t *testing.T) {
	tensor.SetMatMulMaxParallel(7)
	defer tensor.SetMatMulMaxParallel(0)
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if _, err := emb.EmbedBatch([]string{"hello", "world", "test"}); err != nil {
		t.Fatal(err)
	}
	if tensor.MatMulMaxParallelForTest() != 7 {
		t.Fatalf("EmbedBatch mutated process-global cap to %d", tensor.MatMulMaxParallelForTest())
	}
}

func TestEmbedBatchMatchesSerialCosine(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	texts := []string{"hello world", "machine learning", "attention mechanism"}
	batched, err := emb.EmbedBatch(texts)
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		serial, err := emb.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		cos := cosine32(batched[i], serial)
		if cos < 0.999 {
			t.Fatalf("%q packed-vs-serial cosine=%g want >=0.999", text, cos)
		}
	}
}

func TestEmbedTokenStates_WidthIsModelDim(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	states, seq, dim, err := emb.EmbedTokenStates("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if dim != 768 {
		t.Fatalf("EmbedTokenStates dim=%d want model dim 768 (not MRL 256)", dim)
	}
	if seq <= 0 {
		t.Fatal("empty token sequence")
	}
	if len(states) != seq*dim {
		t.Fatalf("states len=%d want seq*dim=%d", len(states), seq*dim)
	}
	pooled, err := emb.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(pooled) != 256 {
		t.Fatalf("Embed dim=%d want 256", len(pooled))
	}
}

// TestFusionOffVsOnCosine pins the fused FFN/attention path against the plain
// one.  The fused paths are a deliberate approximation (per-row activation
// quantization at 2-3 tokens; see the comment in packedDualOutGemmaShort), so
// this is a "the approximation stays small" gate, not an equality gate.
//
// The gate was 0.999 and is now 0.998, on measurement rather than convenience.
// The FFN activation was SiLU and is now GELU (tensor.GeluMul), which took this
// corpus from 0.976 to 0.9995 against llama.cpp -- but a *shared* error of that
// size was also what held the two paths together here.  With it removed the
// remaining difference between them is the fused path's own approximation, which
// was there all along and is now visible:
//
//	"你好" (seq=3, the worst case)   fused-vs-non-fused   fused-vs-llama.cpp
//	  before (SiLU, shared error)        0.999559            0.975968727
//	  after  (GELU, error removed)       0.998957            0.999505482
//	  non-fused, after                    --                0.999910649
//
// So the fused path costs ~4.9e-4 in cosine against llama.cpp on the shortest
// input, against 8.9e-5 for the plain path.  Tightening this back up means
// making the fused path accurate, not tightening the gate -- the per-row VNNI
// path for seq==3 measures 0.999755 with packedDualOutGemmaShort disabled, so
// roughly half of the residual is that kernel's quantization.
func TestFusionOffVsOnCosine(t *testing.T) {
	path := findModel(t)
	on, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer on.Close()
	off, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer off.Close()
	off.fusionOff = true
	raw, err := os.ReadFile(filepath.Join("testdata", "embed_gate_zh.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			texts = append(texts, line)
		}
	}
	if len(texts) == 0 {
		t.Fatal("empty gate corpus")
	}
	for _, text := range texts {
		a, err := on.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		bvec, err := off.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != 256 || len(bvec) != 256 {
			t.Fatalf("%q dim on=%d off=%d want 256", text, len(a), len(bvec))
		}
		var na, nb float64
		for i := range a {
			na += float64(a[i]) * float64(a[i])
			nb += float64(bvec[i]) * float64(bvec[i])
		}
		na, nb = math.Sqrt(na), math.Sqrt(nb)
		if math.Abs(na-1) > 1e-3 || math.Abs(nb-1) > 1e-3 {
			t.Fatalf("%q L2 on=%.6f off=%.6f want 1±1e-3", text, na, nb)
		}
		cos := cosine32(a, bvec)
		t.Logf("%q dim=%d L2_on=%.4f L2_off=%.4f cosine=%.6f", text, len(a), na, nb, cos)
		if cos < 0.998 {
			t.Fatalf("%q cosine=%g want >=0.998 (fused path approximation; see the "+
				"numbers on TestFusionOffVsOnCosine)", text, cos)
		}
	}
}

func cosine32(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// TestFusionOpCost attributes the reference's fused-path approximation to
// individual fused ops, and pins the total.
//
// Measured against llama.cpp on the built-in corpus (tools/go_ref_vs_llamacpp.py
// for the totals, the MACLAW_NO_FUSE_* switches for the per-op split), the fused
// path is worth minCos 0.99891 and turning all four ops off gives 0.99989.  That
// makes it the largest single residual in the reference -- 10x the port's total
// error against llama.cpp (0.9999999) -- and it is not inherent to fusing: the
// port uses an activation panel for the same "one quantization of A feeds both
// weight streams" idea and still lands within 1.1e-7 of llama.cpp.
//
// The cost is spread over all four ops rather than concentrated in one, which is
// why this test measures each separately.  On the built-in corpus, minCos against
// llama.cpp: all fused 0.998912713, QKV off 0.999254070, ATTRES off 0.998838346,
// FFN off 0.999171824, FFNRES off 0.999509473, all off 0.999892791.  Note ATTRES
// off is *worse* than fused -- turning off a fusion cannot be assumed to help.
//
// It is a tradeoff, not a free win: measured interleaved, turning fusion off
// costs 1.72x single-text latency and 1.63x batch throughput (p50 9.79 -> 16.79
// ms, 99.6 -> 61.1 t/s), which is why the default stays fused.
//
// This test has no llama.cpp golden to compare against, so it pins the *shape*
// instead: every switch must change the output -- a silently dead switch is the
// failure mode this project has hit twice -- and the all-off configuration must
// differ from the fused one by more than noise.  On this corpus (dim 256) that
// difference is minCos 0.999493085, i.e. 5.07e-4; the gate below is 2.5x looser
// than the measurement so it catches the fused path being disabled or drifting
// without failing on a corpus change.
func TestFusionOpCost(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the model seven times")
	}
	path := findModel(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "embed_gate_zh.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			texts = append(texts, line)
		}
	}
	if len(texts) == 0 {
		t.Fatal("empty gate corpus")
	}

	// Restore the switches afterwards: they are package-level because the env
	// vars behind them are read once at init.
	defer func(q, a, f, r bool) {
		fuseOffQKV, fuseOffAttRes, fuseOffFFN, fuseOffFFNRes = q, a, f, r
	}(fuseOffQKV, fuseOffAttRes, fuseOffFFN, fuseOffFFNRes)

	embed := func() [][]float32 {
		g, err := NewGemmaEmbedder(path, 256)
		if err != nil {
			t.Fatal(err)
		}
		defer g.Close()
		out := make([][]float32, len(texts))
		for i, tx := range texts {
			v, err := g.Embed(tx)
			if err != nil {
				t.Fatal(err)
			}
			out[i] = v
		}
		return out
	}

	fuseOffQKV, fuseOffAttRes, fuseOffFFN, fuseOffFFNRes = false, false, false, false
	base := embed()

	configs := []struct {
		name                string
		qkv, attr, ffn, res bool
	}{
		{"all fused", false, false, false, false},
		{"QKV off", true, false, false, false},
		{"ATTRES off", false, true, false, false},
		{"FFN off", false, false, true, false},
		{"FFNRES off", false, false, false, true},
		{"all off", true, true, true, true},
	}
	for _, c := range configs {
		fuseOffQKV, fuseOffAttRes, fuseOffFFN, fuseOffFFNRes = c.qkv, c.attr, c.ffn, c.res
		got := embed()
		minCos, worst := 1.0, -1
		// "Did this switch do anything" is a bitwise question, not a cosine
		// one: a dead switch returns the identical vectors, and a cosine
		// threshold would either pass a 1e-16 wobble or fail on rounding.
		identical := true
		for i := range base {
			for j := range base[i] {
				if base[i][j] != got[i][j] {
					identical = false
					break
				}
			}
			if cos := cosine32(base[i], got[i]); cos < minCos {
				minCos, worst = cos, i
			}
		}
		t.Logf("%-12s minCos vs all-fused = %.9f (worst text %d)", c.name, minCos, worst)
		if c.name == "all fused" {
			// The baseline must reproduce itself, or the switches are not what
			// is being measured.
			if !identical {
				t.Fatal("baseline is not deterministic")
			}
			continue
		}
		// A switch that changes nothing is either dead or the op is unused on
		// this shape; either way it must not pass silently.
		if identical {
			t.Fatalf("%s changed nothing -- the switch is not live", c.name)
		}
	}
	// The whole-path number, which is the one quoted against llama.cpp.
	fuseOffQKV, fuseOffAttRes, fuseOffFFN, fuseOffFFNRes = true, true, true, true
	off := embed()
	minCos := 1.0
	for i := range base {
		if cos := cosine32(base[i], off[i]); cos < minCos {
			minCos = cos
		}
	}
	if minCos > 0.9998 {
		t.Fatalf("fused vs non-fused minCos=%.9f; the fused path measured 0.999493085 "+
			"here (5.07e-4), so a gap this small means it is no longer doing anything",
			minCos)
	}
}

// ---------------------------------------------------------------------------
// Sliding-window attention.
//
// The window is one of the two remaining deviations from the C++ port, and both
// ways of getting it wrong -- n_swa instead of n_swa/2, and an exclusive far
// edge -- shift the band by a constant.  A shifted band still produces a
// plausible cosine, so neither shows up as a failure; they have to be pinned by
// construction.  TestSwaRange pins the range helper against the mask predicate
// itself, and TestGqaAttentionMatchesMaskedReference pins the whole attention
// path against a -inf-masked float64 reference, which is the definition the
// slice-based implementation is claiming to be equivalent to.
// ---------------------------------------------------------------------------

func TestSwaRange(t *testing.T) {
	// The masked condition in llama.cpp is |p1 - p0| > n_swa/2, so the visible
	// set is exactly {q : |q-p| <= halfW}, clipped to [0,seq).  Check the helper
	// against that predicate for every (p, seq, halfW) rather than against a few
	// hand-written expectations -- the off-by-one lives in the clipping.
	for _, seq := range []int{1, 2, 3, 8, 9, 16, 17, 40} {
		for _, halfW := range []int{0, 1, 2, 3, 7, 8, 255, 256, 512, 1000} {
			for p := 0; p < seq; p++ {
				lo, hi := swaRange(p, seq, halfW)
				for q := 0; q < seq; q++ {
					d := q - p
					if d < 0 {
						d = -d
					}
					want := d <= halfW
					got := q >= lo && q < hi
					if want != got {
						t.Fatalf("seq=%d halfW=%d p=%d q=%d: visible=%v want %v (lo=%d hi=%d)",
							seq, halfW, p, q, got, want, lo, hi)
					}
				}
				if lo < 0 || hi > seq || lo > hi {
					t.Fatalf("seq=%d halfW=%d p=%d: bad range [%d,%d)", seq, halfW, p, lo, hi)
				}
				if !(p >= lo && p < hi) {
					t.Fatalf("seq=%d halfW=%d p=%d: query excluded from its own band", seq, halfW, p)
				}
			}
		}
	}
	// Full attention is the halfW < 0 sentinel, and must span the sequence.
	for _, seq := range []int{0, 1, 40, 794} {
		for p := 0; p < seq; p++ {
			if lo, hi := swaRange(p, seq, -1); lo != 0 || hi != seq {
				t.Fatalf("full attention seq=%d p=%d: got [%d,%d) want [0,%d)", seq, p, lo, hi, seq)
			}
		}
	}
}

func TestSwaLayerPattern(t *testing.T) {
	// llama.cpp's gemma-embedding loader calls load_swa_pattern(ml, 6), whose
	// set_swa_pattern(n, dense_first=false) gives il%n < n-1.  For 24 layers the
	// global ones are 5, 11, 17 and 23 -- llama.cpp's own dump prints
	// `is_swa = 1` five times then `is_swa = 0`, repeating.
	hp := GemmaHParams{NSwa: 512, SwaPattern: 6, RopeTheta: 1e6, RopeThetaSwa: 1e4}
	const want = "111110111110111110111110"
	var got strings.Builder
	for il := 0; il < 24; il++ {
		if hp.IsSwaLayer(il) {
			got.WriteByte('1')
		} else {
			got.WriteByte('0')
		}
	}
	if got.String() != want {
		t.Fatalf("pattern-6 layer map = %s want %s", got.String(), want)
	}
	// HalfWindowFor halves: the declared 512 is a +-256 band, not +-512.
	for il := 0; il < 24; il++ {
		want := 256
		if il == 5 || il == 11 || il == 17 || il == 23 {
			want = -1
		}
		if got := hp.HalfWindowFor(il); got != want {
			t.Fatalf("layer %d: HalfWindowFor=%d want %d", il, got, want)
		}
	}
	// An explicit per-layer array wins over the scalar pattern, and a file that
	// declares no window at all gets full attention everywhere.
	hp.SwaLayers = []uint8{0, 1, 1, 0}
	for il, want := range []int{-1, 256, 256, -1} {
		if got := hp.HalfWindowFor(il); got != want {
			t.Fatalf("explicit array layer %d: HalfWindowFor=%d want %d", il, got, want)
		}
	}
	hp2 := GemmaHParams{NSwa: 0, SwaPattern: 6, RopeTheta: 1e6, RopeThetaSwa: 1e4}
	for il := 0; il < 24; il++ {
		if got := hp2.HalfWindowFor(il); got != -1 {
			t.Fatalf("n_swa=0 layer %d: HalfWindowFor=%d want -1", il, got)
		}
	}
}

func TestScratchSwaRopeTables(t *testing.T) {
	base := GemmaHParams{Dim: 8, NLayers: 2, NHeads: 2, NKVHeads: 1, HeadDim: 4, KVDim: 4,
		FFDim: 16, RopeTheta: 1e6, RopeThetaSwa: 1e6, NSwa: 512, SwaPattern: 6}
	same := newGemmaScratch(base, 16)
	if same.ropeCosSwa != nil || same.ropeSinSwa != nil {
		t.Fatalf("equal bases must leave the second pair nil, got %d/%d",
			len(same.ropeCosSwa), len(same.ropeSinSwa))
	}
	// With a second base the arena grows by exactly 2*S*halfDim and both pairs
	// are filled for the whole bucket.
	diff := base
	diff.RopeThetaSwa = 1e4
	s := newGemmaScratch(diff, 16)
	S := scratchBucket(16)
	if len(s.ropeCosSwa) != S*2 || len(s.ropeSinSwa) != S*2 {
		t.Fatalf("swa pair len %d/%d want %d", len(s.ropeCosSwa), len(s.ropeSinSwa), S*2)
	}
	if got, want := scratchArenaFloats(diff, S)-scratchArenaFloats(base, S), 2*S*2; got != want {
		t.Fatalf("arena grew by %d floats want %d", got, want)
	}
	// Position 0 is angle 0 for every base; the tables must differ at a position
	// and lane where the base actually enters.  At lane i=0 the frequency is
	// theta^0 = 1 for every base, so cos(pos) is base-independent there and
	// comparing it proves nothing -- the first lane where the base matters is
	// (pos=1, i=1), at index halfDim+1.
	if s.ropeCosSwa[0] != 1.0 || s.ropeSinSwa[0] != 0.0 {
		t.Fatalf("swa pair position 0 = (%g,%g) want (1,0)", s.ropeCosSwa[0], s.ropeSinSwa[0])
	}
	const probe = 1*2 + 1 // pos=1, i=1 for halfDim=2
	if s.ropeCos[probe] == s.ropeCosSwa[probe] {
		t.Fatalf("1e6 and 1e4 tables agree at (pos=1,i=1) = %g; the second base is not taking effect",
			s.ropeCos[probe])
	}
}

// refSlicedAttention is the same *kernel* as gqaAttention but with the visible
// range found by scanning the mask predicate itself, never through swaRange.
// It shares tensor.SoftmaxWeightedSumStrided, so the Schraudolph exp cancels
// and any difference left is the range and tile arithmetic -- which is exactly
// what this port adds.  Deriving the range independently is the point: a
// reference that called swaRange too would agree with a broken swaRange.
func refSlicedAttention(out, q, k, v []float32, seq, nHeads, nKVHeads, headDim, halfW int) {
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	headsPerGroup := nHeads / nKVHeads
	qStride := nHeads * headDim
	kvStride := nKVHeads * headDim
	scores := make([]float32, seq)
	for h := 0; h < nHeads; h++ {
		kvH := h / headsPerGroup
		vBase := v[kvH*headDim:]
		hOff := h * headDim
		for p := 0; p < seq; p++ {
			lo, hi := seq, 0
			for sk := 0; sk < seq; sk++ {
				d := sk - p
				if d < 0 {
					d = -d
				}
				if halfW < 0 || d <= halfW {
					if sk < lo {
						lo = sk
					}
					if sk+1 > hi {
						hi = sk + 1
					}
				}
			}
			qVec := q[p*qStride+hOff : p*qStride+hOff+headDim]
			for sk := lo; sk < hi; sk++ {
				scores[sk-lo] = tensor.Dot(qVec, k[sk*kvStride+kvH*headDim:sk*kvStride+kvH*headDim+headDim]) * scale
			}
			tensor.SoftmaxWeightedSumStrided(out[p*qStride+hOff:p*qStride+hOff+headDim],
				scores[:hi-lo], vBase[lo*kvStride:], hi-lo, kvStride, headDim)
		}
	}
}

// refExactMaskedAttention is the *definition* the slice trick claims to equal:
// softmax over exp(score - max) with -inf written into the masked lanes, in
// float64 and with no tiling.  It shares no code with the implementation, so it
// is a real independent check -- but it uses exact exp while the kernel uses
// Schraudolph, so it can only be compared loosely.
func refExactMaskedAttention(out, q, k, v []float32, seq, nHeads, nKVHeads, headDim, halfW int) {
	scale := 1.0 / math.Sqrt(float64(headDim))
	headsPerGroup := nHeads / nKVHeads
	qStride := nHeads * headDim
	kvStride := nKVHeads * headDim
	for h := 0; h < nHeads; h++ {
		kvH := h / headsPerGroup
		for p := 0; p < seq; p++ {
			scores := make([]float64, seq)
			max := math.Inf(-1)
			for sk := 0; sk < seq; sk++ {
				if halfW >= 0 {
					d := sk - p
					if d < 0 {
						d = -d
					}
					if d > halfW {
						scores[sk] = math.Inf(-1)
						continue
					}
				}
				var acc float64
				for i := 0; i < headDim; i++ {
					acc += float64(q[p*qStride+h*headDim+i]) * float64(k[sk*kvStride+kvH*headDim+i])
				}
				acc *= scale
				scores[sk] = acc
				if acc > max {
					max = acc
				}
			}
			var sum float64
			for sk := range scores {
				if math.IsInf(scores[sk], -1) {
					scores[sk] = 0
					continue
				}
				scores[sk] = math.Exp(scores[sk] - max)
				sum += scores[sk]
			}
			for i := 0; i < headDim; i++ {
				var acc float64
				for sk := 0; sk < seq; sk++ {
					acc += scores[sk] * float64(v[sk*kvStride+kvH*headDim+i])
				}
				out[p*qStride+h*headDim+i] = float32(acc / sum)
			}
		}
	}
}

// maxAbsDiff is the largest elementwise |a-b|.
func maxAbsDiff(a, b []float32) float64 {
	var m float64
	for i := range a {
		if d := math.Abs(float64(a[i]) - float64(b[i])); d > m {
			m = d
		}
	}
	return m
}

func TestGqaAttentionMatchesMaskedReference(t *testing.T) {
	// seq values are chosen to cross every internal boundary: 21 exercises the
	// nQ=8 tile, the nQ=4 leftover tile and the single-query tail; 70 crosses
	// the seq>=64 parallel branch; 3 and 8 are the short-Embed shapes.
	for _, tc := range []struct {
		seq, nHeads, nKVHeads, headDim, halfW int
	}{
		{3, 3, 1, 16, 256},
		{8, 3, 1, 16, 2},
		{21, 3, 1, 16, 0},
		{21, 3, 1, 16, 1},
		{21, 3, 1, 16, 2},
		{21, 3, 1, 16, 4},
		{21, 3, 1, 16, 256},
		{21, 3, 1, 16, -1},
		{40, 2, 2, 8, 3},
		{70, 3, 1, 16, 5},
		{70, 3, 1, 16, -1},
	} {
		seq, nHeads, nKVHeads, headDim, halfW := tc.seq, tc.nHeads, tc.nKVHeads, tc.headDim, tc.halfW
		qStride := nHeads * headDim
		kvStride := nKVHeads * headDim
		q := make([]float32, seq*qStride)
		k := make([]float32, seq*kvStride)
		v := make([]float32, seq*kvStride)
		// Deterministic fill; a fixed LCG keeps this reproducible without
		// pulling in math/rand's global state.
		seed := uint32(12345)
		next := func() float32 {
			seed = seed*1664525 + 1013904223
			return float32(int32(seed>>8)%2000-1000) / 1000.0
		}
		for i := range q {
			q[i] = next()
		}
		for i := range k {
			k[i] = next()
		}
		for i := range v {
			v[i] = next()
		}
		run := func(hw int) []float32 {
			out := make([]float32, seq*qStride)
			(&GemmaEmbedder{}).gqaAttention(out, q, k, v, seq, nHeads, nKVHeads, headDim, qStride, kvStride, hw)
			return out
		}

		got := run(halfW)

		// (1) The window and tile arithmetic, against an independently derived
		// range and the same kernel.  A range off by one changes which keys are
		// in the softmax, so this catches it; only the dot kernel's accumulation
		// order (MultiDot8 tiles vs scalar Dot) is left, hence the tolerance.
		sliced := make([]float32, seq*qStride)
		refSlicedAttention(sliced, q, k, v, seq, nHeads, nKVHeads, headDim, halfW)
		if d := maxAbsDiff(got, sliced); d > 1e-5 {
			t.Fatalf("seq=%d heads=%d/%d headDim=%d halfW=%d: differs from the predicate-derived "+
				"slice reference by %g", seq, nHeads, nKVHeads, headDim, halfW, d)
		}

		// (2) That it is really attention, against exact float64 arithmetic.
		// Loose on purpose: the kernel's exp is Schraudolph, and the error grows
		// as the window narrows (fewer terms to average over), so this is a
		// sanity bound, not a precision claim.
		exact := make([]float32, seq*qStride)
		refExactMaskedAttention(exact, q, k, v, seq, nHeads, nKVHeads, headDim, halfW)
		dExact := maxAbsDiff(got, exact)
		if dExact > 0.05 {
			t.Fatalf("seq=%d halfW=%d: differs from exact float64 attention by %g",
				seq, halfW, dExact)
		}

		// (3) The window has to be a no-op when it is at least as wide as the
		// sequence, and to actually clip when it is not -- otherwise every
		// assertion above would pass with the window silently ignored.
		full := run(-1)
		dFull := maxAbsDiff(got, full)
		switch {
		case halfW >= seq-1:
			if dFull != 0 {
				t.Fatalf("seq=%d halfW=%d: wide window differs from full attention by %g",
					seq, halfW, dFull)
			}
		case halfW >= 0:
			if dFull < 0.02 {
				t.Fatalf("seq=%d halfW=%d: window changed the output by only %g; not clipping",
					seq, halfW, dFull)
			}
		}
		t.Logf("seq=%d heads=%d/%d headDim=%d halfW=%d: vs_slice=%g vs_exact=%g vs_full=%g",
			seq, nHeads, nKVHeads, headDim, halfW, maxAbsDiff(got, sliced), dExact, dFull)
	}
}

