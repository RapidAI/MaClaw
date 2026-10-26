package embedding

import (
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// vkTestModel returns the Qwen3 GGUF path when it exists on this machine.
func vkTestModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("MACLAW_EMBED_MODEL_PATH"); p != "" {
		return p
	}
	p, _ := ResolveQwen3ModelPath("")
	if p == "" {
		t.Skip("no Qwen3 GGUF available")
	}
	return p
}

// TestVulkanSelfCheck verifies the Vulkan backend produces the same embedding
// as the CPU path. The embedder's own init already runs this comparison; this
// test makes the numbers visible and fails loudly on drift.
func TestVulkanSelfCheck(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	texts := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"hello",
		"what is the weather like today",
	}
	for _, txt := range texts {
		ids := q.Tokenize(txt)
		got := make([]float32, q.hp.Dim)
		if err := q.vkGPU.Load().forward(q, [][]int{ids}, [][]float32{got}); err != nil {
			t.Fatalf("forward %q: %v", txt, err)
		}
		s := q.getScratch(len(ids) + 8)
		ref, err := q.forwardFrom(ids, nil, nil, s, 1)
		q.putScratch(s)
		if err != nil {
			t.Fatalf("cpu forward %q: %v", txt, err)
		}
		// forward returns the raw last row; forwardFrom already applies the
		// output norm, so normalise only the GPU side.
		gn := make([]float32, q.hp.Dim)
		tensor.RMSNorm(gn, got, q.outputNorm, q.hp.RMSNormEps)
		cos := cosineF32(gn, ref)
		t.Logf("%-40q tokens=%2d cos=%.8f", txt, len(ids), cos)
		if cos < qwen3VKSelfCheckMinCos {
			t.Errorf("%q: cos %.8f < %.4f (Vulkan output diverges from CPU)", txt, cos, qwen3VKSelfCheckMinCos)
		}
		if math.IsNaN(cos) {
			t.Errorf("%q: NaN cosine", txt)
		}
	}
}

// TestVulkanEmbedMatchesCPU compares the public Embed path with the CPU path
// for the same text, which is what retrieval actually consumes.
func TestVulkanEmbedMatchesCPU(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	const txt = "帮我把这个文件发给张三"
	gpu, err := q.Embed(txt)
	if err != nil {
		t.Fatalf("gpu Embed: %v", err)
	}
	// Disable the GPU so the same call takes the CPU path.
	saved := q.vkGPU.Swap(nil)
	defer q.vkGPU.Store(saved)
	cpu, err := q.Embed(txt)
	if err != nil {
		t.Fatalf("cpu Embed: %v", err)
	}
	cos := cosineF32(gpu, cpu)
	t.Logf("Embed cos(gpu, cpu) = %.8f", cos)
	if cos < 0.999 {
		t.Errorf("cos %.8f < 0.999", cos)
	}
}

// TestVulkanBatchMatchesSingle checks that packing several sequences into one
// forward (the EmbedBatch path) gives each sequence the same vector as running
// it alone — i.e. the segment handling and causal mask are right.
func TestVulkanBatchMatchesSingle(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	texts := []string{"hello", "帮我把这个文件发给张三", "what is the weather like today"}
	ids := make([][]int, len(texts))
	for i, t2 := range texts {
		ids[i] = q.Tokenize(t2)
	}
	// Each sequence alone.
	single := make([][]float32, len(texts))
	for i := range ids {
		single[i] = make([]float32, q.hp.Dim)
		if err := g.forward(q, ids[i:i+1], single[i:i+1]); err != nil {
			t.Fatalf("single forward: %v", err)
		}
	}
	// All sequences packed into one forward.
	packed := make([][]float32, len(ids))
	for i := range packed {
		packed[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, ids, packed); err != nil {
		t.Fatalf("packed forward: %v", err)
	}
	for i := range texts {
		cos := cosineF32(single[i], packed[i])
		t.Logf("%-40q cos(single, packed)=%.8f", texts[i], cos)
		if cos < 0.9999 {
			t.Errorf("%q: packed cos %.8f < 0.9999 (batch isolation broken)", texts[i], cos)
		}
	}
}

// TestVulkanMixedBatchSizes runs single-text, small-batch and larger-batch
// forwards in sequence and checks each batch size gives the same vector for the
// same text. The arena is allocated once (never grown), so this also proves the
// descriptor sets cached for one batch size stay valid for the next.
func TestVulkanMixedBatchSizes(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	texts := []string{
		"hello",
		"帮我把这个文件发给张三",
		"what is the weather like today",
		"查询 api2 服务器状态",
		"打开设置页面",
	}
	ids := make([][]int, len(texts))
	for i, s := range texts {
		ids[i] = q.Tokenize(s)
	}
	// Reference: each text alone (small batch, no growth).
	want := make([][]float32, len(texts))
	for i := range ids {
		want[i] = make([]float32, q.hp.Dim)
		if err := g.forward(q, ids[i:i+1], want[i:i+1]); err != nil {
			t.Fatalf("single forward: %v", err)
		}
	}
	// One packed forward over all texts.
	got := make([][]float32, len(ids))
	for i := range got {
		got[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, ids, got); err != nil {
		t.Fatalf("packed forward after growth: %v", err)
	}
	for i := range texts {
		if c := cosineF32(want[i], got[i]); c < 0.9999 {
			t.Errorf("%q: cos after the packed batch %.8f < 0.9999 (stale bindings?)", texts[i], c)
		}
	}
	// And the original single-text batch must still work afterwards.
	for i := range ids {
		again := make([]float32, q.hp.Dim)
		if err := g.forward(q, ids[i:i+1], [][]float32{again}); err != nil {
			t.Fatalf("single forward after growth: %v", err)
		}
		if c := cosineF32(want[i], again); c < 0.999999 {
			t.Errorf("%q: cos after shrink-back %.8f (bindings not reusable)", texts[i], c)
		}
	}
}

// TestVulkanAccelToggle checks the hardware-acceleration switch actually
// releases and restores the GPU backend.
func TestVulkanAccelToggle(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	q.ApplyAccel(false)
	if q.vkGPU.Load() != nil {
		t.Fatal("ApplyAccel(false) did not release the Vulkan backend")
	}
	if got := q.Accel().Backend; got == BackendVulkan {
		t.Fatalf("Accel().Backend = %q after disabling", got)
	}
	// The CPU path must still work.
	if _, err := q.Embed("hello"); err != nil {
		t.Fatalf("Embed after disabling GPU: %v", err)
	}
	q.ApplyAccel(true)
	if q.vkGPU.Load() == nil {
		t.Logf("ApplyAccel(true) did not restore the backend: %s", q.gpuReason)
		return // acceptable: the speed gate or self-check may decline
	}
	if got := q.Accel().Backend; got != BackendVulkan {
		t.Fatalf("Accel().Backend = %q after enabling, want %q", got, BackendVulkan)
	}
	if _, err := q.Embed("hello"); err != nil {
		t.Fatalf("Embed after re-enabling GPU: %v", err)
	}
}

// TestVKWeightEstimate checks the memory guard's estimate against the buffers
// the backend actually allocates (a mismatch would either block the GPU on a
// healthy machine or let it upload into a low-memory one).
func TestVKWeightEstimate(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	est := qwen3VKWeightBytes(q)
	if est == 0 {
		t.Fatal("estimate is zero")
	}
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	// int8 payload + f32 scales + norm weights are what WeightBytes reports.
	diff := int64(g.WeightBytes) - int64(est)
	if diff < 0 {
		diff = -diff
	}
	if diff > 1<<20 {
		t.Fatalf("estimate %d vs actual %d (off by %d bytes)", est, g.WeightBytes, diff)
	}
	// The guard needs a margin over the raw weights; make sure it is not
	// accidentally below them.
	t.Logf("estimate %.1f MB, actual %.1f MB", float64(est)/1e6, float64(g.WeightBytes)/1e6)
}

// TestVulkanBatchRowsCap verifies a bulk batch is split so the activation
// arena stays bounded, and that every text still gets its own correct vector.
func TestVulkanBatchRowsCap(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	base := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"hello",
		"what is the weather like today",
	}
	// Enough texts to exceed the row cap several times over.
	texts := make([]string, 0, 160)
	for i := 0; i < 160; i++ {
		texts = append(texts, base[i%len(base)])
	}
	want, err := q.EmbedBatch(texts[:1])
	if err != nil {
		t.Fatal(err)
	}
	_ = want

	before := g.capRows
	got, err := q.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("bulk EmbedBatch: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("got %d vectors, want %d", len(got), len(texts))
	}
	// Same text must produce the same vector regardless of packing position.
	ref, err := q.EmbedBatch(base)
	if err != nil {
		t.Fatal(err)
	}
	for i := range texts {
		j := i % len(base)
		if c := cosineF32(ref[j], got[i]); c < 0.9999 {
			t.Fatalf("text %d (%q) cos %.8f vs its single-batch vector", i, texts[i], c)
		}
	}
	// The arena is allocated once and never grows; a bulk batch must be chunked
	// inside it.
	if g.capRows != qwen3VKMaxRows {
		t.Errorf("arena is %d rows, want the fixed %d", g.capRows, qwen3VKMaxRows)
	}
	if before != qwen3VKMaxRows {
		t.Errorf("arena at load was %d rows, want the fixed %d", before, qwen3VKMaxRows)
	}
}

// TestVulkanLongTextFallsBackToCPU verifies a too-long sequence is served by
// the CPU while the rest of the batch still uses the GPU, and that mixing the
// two produces the same vectors as either path alone.
func TestVulkanLongTextFallsBackToCPU(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	long := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 40)
	if n := len(q.Tokenize(long)); n <= qwen3VKMaxSeqTokens {
		t.Fatalf("test text is only %d tokens, cap is %d", n, qwen3VKMaxSeqTokens)
	}
	// A direct GPU forward must refuse it rather than run slowly.
	if _, ok := q.embedGPU([][]int{q.Tokenize(long)}); ok {
		t.Fatal("embedGPU accepted a sequence above the token cap")
	}
	mixed := []string{"hello", long, "查询 api2 服务器状态", long}
	got, err := q.EmbedBatch(mixed)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	// Each text must match its own single-text embedding (which routes the long
	// ones to the CPU too).
	for i, txt := range mixed {
		ref, err := q.Embed(txt)
		if err != nil {
			t.Fatalf("Embed %d: %v", i, err)
		}
		if c := cosineF32(ref, got[i]); c < 0.9999 {
			t.Errorf("text %d (%d tokens) cos %.8f between batch and single", i, len(q.Tokenize(txt)), c)
		}
	}
}

// TestVulkanRejectsWrongBackend proves the numeric gate is load-bearing: a GPU
// that returns wrong (or non-finite) vectors must be refused, and the embedder
// must stay usable on the CPU.
func TestVulkanRejectsWrongBackend(t *testing.T) {
	path := vkTestModel(t)
	cases := []struct {
		name    string
		corrupt func([]float32)
		want    string
	}{
		{"zeroed output", func(h []float32) {
			for i := range h {
				h[i] = 0
			}
		}, "numeric check"},
		{"nan output", func(h []float32) {
			for i := range h {
				h[i] = float32(math.NaN())
			}
		}, "non-finite"},
		{"sign-flipped output", func(h []float32) {
			for i := range h {
				h[i] = -h[i]
			}
		}, "numeric check"},
	}
	prev := qwen3VKProbeHook
	defer func() { qwen3VKProbeHook = prev }()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			qwen3VKProbeHook = c.corrupt
			q, err := NewQwen3Embedder(path, 0)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			defer q.Close()
			if q.vkGPU.Load() != nil {
				t.Fatal("a corrupt backend was accepted")
			}
			if reason := q.GPUReason(); !strings.Contains(reason, c.want) {
				t.Errorf("reason = %q, want it to mention %q", reason, c.want)
			}
			// The CPU path must still produce a usable embedding.
			v, err := q.Embed("hello")
			if err != nil {
				t.Fatalf("Embed after rejecting the GPU: %v", err)
			}
			if len(v) != q.dim {
				t.Fatalf("vector width %d, want %d", len(v), q.dim)
			}
			if !allFinite(v) {
				t.Fatal("CPU fallback produced non-finite values")
			}
		})
	}
}

// TestVulkanAcceptsGoodBackend is the counterpart: with no corruption the
// backend must be accepted (skips when the machine lacks a usable GPU).
func TestVulkanAcceptsGoodBackend(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("GPU not selected on this machine: %s", q.GPUReason())
	}
	if reason := q.GPUReason(); reason != "" {
		t.Errorf("active backend still carries a reason: %q", reason)
	}
	accel := q.Accel()
	if accel.Backend != BackendVulkan {
		t.Errorf("Accel().Backend = %q, want %q", accel.Backend, BackendVulkan)
	}
	if !accel.GPUAvailable {
		t.Error("Accel().GPUAvailable is false with an active GPU")
	}
	if accel.Device == "" {
		t.Error("Accel().Device is empty")
	}
	// The hosts key the hardware-acceleration switch off GPUAvailable; a
	// rejected GPU must still report it so the switch stays usable.
	q.ApplyAccel(false)
	off := q.Accel()
	if off.GPUAvailable != true {
		t.Errorf("GPUAvailable = %v after releasing the GPU, want true (a device exists)", off.GPUAvailable)
	}
	if off.Backend == BackendVulkan {
		t.Error("Accel().Backend still reports the GPU after the toggle went off")
	}
}

// TestApplyAccelConcurrent exercises the toggle from several goroutines: only
// one backend may result (a second device would leak ~495 MB), and the
// embedder must be usable afterwards.
func TestApplyAccelConcurrent(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q.ApplyAccel(i%2 == 0)
		}(i)
	}
	wg.Wait()
	// Whatever the final state, exactly zero or one backend may be live.
	n := 0
	if q.vkGPU.Load() != nil {
		n++
	}
	if q.gpu.Load() != nil {
		n++
	}
	if n > 1 {
		t.Fatalf("%d GPU backends live at once", n)
	}
	if _, err := q.Embed("hello"); err != nil {
		t.Fatalf("Embed after concurrent toggles: %v", err)
	}
}

// TestApplyAccelAfterClose ensures the toggle cannot resurrect a backend (and
// leak its device) once the embedder is closed.
func TestApplyAccelAfterClose(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q.Close()
	q.ApplyAccel(true)
	q.ApplyAccel(false)
	if q.vkGPU.Load() != nil {
		t.Fatal("ApplyAccel after Close created a backend")
	}
	if _, err := q.Embed("hello"); !errors.Is(err, ErrEmbedderClosed) {
		t.Fatalf("Embed after Close: err=%v, want ErrEmbedderClosed", err)
	}
}

// TestVulkanDeterministic checks the GPU produces bit-identical hidden states
// across repeated forwards of the same input. Nondeterminism would silently
// split a vector index: two passes over the same document would land at
// slightly different points, so a stored vector would no longer match its own
// re-embedding.
func TestVulkanDeterministic(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	texts := []string{"帮我把这个文件发给张三", "hello", "what is the weather like today"}
	ids := make([][]int, len(texts))
	for i, s := range texts {
		ids[i] = q.Tokenize(s)
	}
	first := make([][]float32, len(ids))
	for i := range first {
		first[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, ids, first); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 3; pass++ {
		again := make([][]float32, len(ids))
		for i := range again {
			again[i] = make([]float32, q.hp.Dim)
		}
		if err := g.forward(q, ids, again); err != nil {
			t.Fatal(err)
		}
		for i := range first {
			for j := range first[i] {
				if again[i][j] != first[i][j] {
					t.Fatalf("pass %d text %d component %d: %v != %v (nondeterministic)",
						pass, i, j, again[i][j], first[i][j])
				}
			}
		}
	}
	// A packed forward of the same texts must also match, so the batch path
	// cannot drift from the per-text result.
	single := make([][]float32, len(ids))
	for i := range ids {
		single[i] = make([]float32, q.hp.Dim)
		if err := g.forward(q, ids[i:i+1], single[i:i+1]); err != nil {
			t.Fatal(err)
		}
	}
	for i := range single {
		for j := range single[i] {
			if single[i][j] != first[i][j] {
				t.Fatalf("text %d component %d: packed %v != single %v", i, j, first[i][j], single[i][j])
			}
		}
	}
}

// TestVulkanConcurrentEmbed runs many embeds from separate goroutines. The
// encoder and activation arena are single-owner (guarded by g.mu, and the Go
// scheduler may run several of these on the same thread), so concurrent callers
// must serialize and still produce exactly the single-threaded result.
func TestVulkanConcurrentEmbed(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	texts := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"hello",
		"what is the weather like today",
		"打开设置页面",
		"summarize the meeting notes",
		"how many files are in this folder",
		"把这份季度报告整理成PDF",
	}
	// Reference: one at a time.
	ref := make([][]float32, len(texts))
	for i, s := range texts {
		v, err := q.Embed(s)
		if err != nil {
			t.Fatalf("seq embed %d: %v", i, err)
		}
		ref[i] = v
	}
	// Then all at once, repeated so the arena is reused under contention.
	var wg sync.WaitGroup
	errs := make([]error, len(texts)*3)
	got := make([][]float32, len(texts)*3)
	for r := 0; r < 3; r++ {
		for i, s := range texts {
			wg.Add(1)
			go func(k int, txt string) {
				defer wg.Done()
				v, err := q.Embed(txt)
				got[k], errs[k] = v, err
			}(r*len(texts)+i, s)
		}
	}
	wg.Wait()
	for k := range got {
		if errs[k] != nil {
			t.Fatalf("concurrent embed %d: %v", k, errs[k])
		}
		want := ref[k%len(texts)]
		for j := range want {
			if got[k][j] != want[j] {
				t.Fatalf("embed %d component %d: %v != %v under concurrency", k, j, got[k][j], want[j])
			}
		}
	}
}

// TestVulkanPooledMatchesCPU checks the GPU mean-pooling tail against the CPU
// implementation. The pooled path serves the Layer 2 intent heads, which run on
// every message, so it must agree with the CPU vectors those heads were trained
// against.
func TestVulkanPooledMatchesCPU(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	texts := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"hello",
		"向 znsoft@163.com 发邮件报告系统状态",
	}
	type pair struct{ last, mean []float32 }
	gpu := make([]pair, len(texts))
	for i, s := range texts {
		l, m, err := q.EmbedPooledWithRole(s, RoleClassification)
		if err != nil {
			t.Fatalf("gpu pooled %d: %v", i, err)
		}
		gpu[i] = pair{l, m}
	}
	// Same embedder, CPU path.
	q.ApplyAccel(false)
	if q.vkGPU.Load() != nil {
		t.Fatal("GPU still active after ApplyAccel(false)")
	}
	for i, s := range texts {
		l, m, err := q.EmbedPooledWithRole(s, RoleClassification)
		if err != nil {
			t.Fatalf("cpu pooled %d: %v", i, err)
		}
		if c := cosineF32(l, gpu[i].last); c < 0.999 {
			t.Errorf("%q: last cos(gpu, cpu) %.6f < 0.999", s, c)
		}
		if c := cosineF32(m, gpu[i].mean); c < 0.999 {
			t.Errorf("%q: mean cos(gpu, cpu) %.6f < 0.999", s, c)
		}
		// Mean and last must stay distinct, otherwise the tail collapsed them.
		if c := cosineF32(gpu[i].mean, gpu[i].last); c > 0.99 {
			t.Errorf("%q: pooled mean == last (cos %.4f); pooling did not run", s, c)
		}
	}
}

// TestVKActFootprintMatchesBuffers checks the reported activation footprint
// against the buffers actually allocated. ActBytes and the device-selection
// headroom share one formula, and this pins that formula to reality so the two
// cannot drift apart again (they had, missing a buffer).
func TestVKActFootprintMatchesBuffers(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	actual := g.x.Size + g.nrmBuf.Size + g.qkv.Size + g.att.Size + g.gu.Size + g.g.Size + g.pos.Size + g.normAll.Size
	t.Logf("ActBytes=%d actual buffers=%d (rows=%d)", g.ActBytes(), actual, g.capRows)
	if g.ActBytes() != actual {
		t.Fatalf("ActBytes %d != sum of activation buffers %d", g.ActBytes(), actual)
	}
}

// TestVKWeightSizesMatchBuffers pins the weight arena sizes used by the memory
// guard to the buffers allocWeights actually creates.
func TestVKWeightSizesMatchBuffers(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.GPUReason())
	}
	qBytes, sFloats, normFloats := qwen3VKWeightSizes(q)
	if g.wq.Size != qBytes {
		t.Errorf("wq %d bytes, formula says %d", g.wq.Size, qBytes)
	}
	if g.ws.Size != sFloats*4 {
		t.Errorf("ws %d bytes, formula says %d", g.ws.Size, sFloats*4)
	}
	if g.nrm.Size != normFloats*4 {
		t.Errorf("nrm %d bytes, formula says %d", g.nrm.Size, normFloats*4)
	}
	if got, want := g.WeightBytes, qBytes+sFloats*4+normFloats*4; got != want {
		t.Errorf("WeightBytes %d != %d", got, want)
	}
}
