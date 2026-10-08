package embedding

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestGemmaLayerWindow(t *testing.T) {
	hp := GemmaHParams{SlidingWindow: 512, SWAPeriod: 6}
	if hp.windowHalf(0) != 256 || hp.windowHalf(4) != 256 || hp.windowHalf(22) != 256 {
		t.Fatalf("local half got %d %d %d", hp.windowHalf(0), hp.windowHalf(4), hp.windowHalf(22))
	}
	for _, layer := range []int{5, 11, 17, 23} {
		if hp.windowHalf(layer) != 0 {
			t.Fatalf("layer %d half = %d, want 0", layer, hp.windowHalf(layer))
		}
	}
	lo, hi := gemmaKeySpan(0, 258, 256)
	if lo != 0 || hi != 257 {
		t.Fatalf("q0 span = %d:%d", lo, hi)
	}
	lo, hi = gemmaKeySpan(257, 258, 256)
	if lo != 1 || hi != 258 {
		t.Fatalf("q257 span = %d:%d", lo, hi)
	}
	lo, hi = gemmaKeySpan(10, 257, 256)
	if lo != 0 || hi != 257 {
		t.Fatalf("short span = %d:%d", lo, hi)
	}
}

func TestDualRoPETablesDiverge(t *testing.T) {
	hp := GemmaHParams{
		Dim: 768, KVDim: 256, HeadDim: 256, FFDim: 1152,
		RopeTheta: 1e6, RopeThetaLocal: 10000, SWAPeriod: 6,
	}
	s := newGemmaScratch(hp, 32)
	const pos, freq = 20, 1
	half := hp.HeadDim / 2
	angle := func(theta float64) float64 {
		return float64(pos) / math.Pow(theta, float64(2*freq)/float64(hp.HeadDim))
	}
	wantG := math.Cos(angle(1e6))
	wantL := math.Cos(angle(10000))
	cos0, sin0 := ropeForLayer(s, hp, 0)
	gotL := float64(cos0[pos*half+freq])
	if &cos0[0] != &s.ropeCos[0] {
		t.Fatal("local layer must use the arena RoPE table")
	}
	cos5, sin5 := ropeForLayer(s, hp, 5)
	gotG := float64(cos5[pos*half+freq])
	if &cos5[0] == &s.ropeCos[0] {
		t.Fatal("global theta must be parked outside the arena table")
	}
	if len(s.arena) != scratchArenaFloats(hp, s.seqCap) {
		t.Fatalf("parking global RoPE grew the arena to %d", len(s.arena))
	}
	if math.Abs(gotG-wantG) > 1e-5 || math.Abs(gotL-wantL) > 1e-5 {
		t.Fatalf("global %g want %g; local %g want %g", gotG, wantG, gotL, wantL)
	}
	t.Logf("pos=20 freq=1 local=%g global=%g", gotL, gotG)
	if gotG == gotL {
		t.Fatal("local and global angles matched")
	}
	cosBack, sinBack := ropeForLayer(s, hp, 6)
	if &cosBack[0] != &cos0[0] || &sinBack[0] != &sin0[0] {
		t.Fatal("switching back to a local layer must return the same table")
	}
	if cosBack[pos*half+freq] != cos0[pos*half+freq] {
		t.Fatal("local table changed across a global layer")
	}
	if math.Abs(float64(cosBack[pos*half+freq])-wantL) > 1e-5 {
		t.Fatal("switching back to a local layer did not restore theta 10000")
	}
	// Every angle must match the float32 pow formula, including after the
	// cached inverse-frequency refill.
	matchRoPE := func(cosRow, sinRow []float32, theta float64) {
		t.Helper()
		for p := 0; p < s.ropeSeq; p++ {
			for i := 0; i < half; i++ {
				inv := 1.0 / float32(math.Pow(theta, float64(2*i)/float64(hp.HeadDim)))
				angle := float32(p) * inv
				wantC := float32(math.Cos(float64(angle)))
				wantS := float32(math.Sin(float64(angle)))
				if cosRow[p*half+i] != wantC || sinRow[p*half+i] != wantS {
					t.Fatalf("theta=%g pos=%d i=%d cos %g want %g sin %g want %g", theta, p, i, cosRow[p*half+i], wantC, sinRow[p*half+i], wantS)
				}
			}
		}
	}
	matchRoPE(cosBack, sinBack, 10000)
	cos5b, sin5b := ropeForLayer(s, hp, 11)
	if &cos5b[0] != &cos5[0] || &sin5b[0] != &sin5[0] {
		t.Fatal("global theta must return the same parked table")
	}
	matchRoPE(cos5b, sin5b, 1e6)
}

func TestGQAWindowDropsFarKey(t *testing.T) {
	const seq, headDim, nHeads = 258, 4, 1
	q, k, v := attnSpike(seq, nHeads, headDim)
	windowed := make([]float32, len(q))
	full := make([]float32, len(q))
	g := &GemmaEmbedder{}
	g.gqaAttention(windowed, q, k, v, seq, nHeads, 1, headDim, nHeads*headDim, headDim, 256)
	g.gqaAttention(full, q, k, v, seq, nHeads, 1, headDim, nHeads*headDim, headDim, 0)
	t.Logf("seq=258 half=256 q0 windowed=%g full=%g q257 windowed=%g", windowed[0], full[0], windowed[257*headDim])
	if windowed[0] > 0.2 {
		t.Fatalf("windowed q0 picked up the far value: %v", windowed[:headDim])
	}
	if full[0] < 4 {
		t.Fatalf("full q0 missed the far value: %v", full[:headDim])
	}
	last := 257 * headDim
	if windowed[last] < 4 {
		t.Fatalf("windowed q257 missed its own key: %v", windowed[last:last+headDim])
	}
}

func TestGQAWindowMatchesFullWhenSeqFits(t *testing.T) {
	const seq, headDim, nHeads = 257, 4, 3
	q, k, v := attnSpike(seq, nHeads, headDim)
	windowed := make([]float32, len(q))
	full := make([]float32, len(q))
	g := &GemmaEmbedder{}
	stride := nHeads * headDim
	g.gqaAttention(windowed, q, k, v, seq, nHeads, 1, headDim, stride, headDim, 256)
	g.gqaAttention(full, q, k, v, seq, nHeads, 1, headDim, stride, headDim, 0)
	for i := range full {
		if windowed[i] != full[i] {
			t.Fatalf("seq=%d index %d windowed %g full %g", seq, i, windowed[i], full[i])
		}
	}
}

func TestGQAWindowDim256MatchesStrided(t *testing.T) {
	const seq, headDim, nHeads, half = 260, 256, 3, 256
	stride := nHeads * headDim
	q := make([]float32, seq*stride)
	k := make([]float32, seq*headDim)
	v := make([]float32, seq*headDim)
	var state uint32 = 1
	next := func() float32 {
		state = state*1664525 + 1013904223
		return float32(int32(state>>8)%2001-1000) / 1000
	}
	for i := range q {
		q[i] = next()
	}
	for i := range k {
		k[i] = next()
		v[i] = next()
	}
	got := make([]float32, len(q))
	g := &GemmaEmbedder{}
	g.gqaAttention(got, q, k, v, seq, nHeads, 1, headDim, stride, headDim, half)

	want := make([]float32, len(q))
	scale := float32(1 / math.Sqrt(float64(headDim)))
	for h := 0; h < nHeads; h++ {
		for qq := 0; qq < seq; qq++ {
			lo, hi := gemmaKeySpan(qq, seq, half)
			scores := make([]float32, hi-lo)
			qv := q[qq*stride+h*headDim : qq*stride+(h+1)*headDim]
			for i := range scores {
				sk := lo + i
				kv := k[sk*headDim : (sk+1)*headDim]
				scores[i] = tensor.Dot(qv, kv) * scale
			}
			dst := want[qq*stride+h*headDim : qq*stride+(h+1)*headDim]
			tensor.SoftmaxWeightedSumStrided(dst, scores, v[lo*headDim:], hi-lo, headDim, headDim)
		}
	}
	maxd := 0.0
	for i := range got {
		if d := math.Abs(float64(got[i] - want[i])); d > maxd {
			maxd = d
		}
	}
	t.Logf("seq=%d headDim=256 max abs diff %g", seq, maxd)
	if maxd > 1e-4 {
		t.Fatalf("max abs diff %g", maxd)
	}
}

func TestGQAWindowDim256DropsFarKey(t *testing.T) {
	const seq, headDim, nHeads = 258, 256, 3
	q, k, v := attnSpike(seq, nHeads, headDim)
	out := make([]float32, len(q))
	g := &GemmaEmbedder{}
	g.gqaAttention(out, q, k, v, seq, nHeads, 1, headDim, nHeads*headDim, headDim, 256)
	for h := 0; h < nHeads; h++ {
		if out[h*headDim] > 0.2 {
			t.Fatalf("head %d q0 = %g", h, out[h*headDim])
		}
		last := 257*nHeads*headDim + h*headDim
		if out[last] < 4 {
			t.Fatalf("head %d q257 = %g", h, out[last])
		}
	}
}

func TestGQAWindowParallelHeadsDropFarKey(t *testing.T) {
	const seq, headDim, nHeads = 258, 4, 3
	q, k, v := attnSpike(seq, nHeads, headDim)
	out := make([]float32, len(q))
	g := &GemmaEmbedder{}
	g.gqaAttention(out, q, k, v, seq, nHeads, 1, headDim, nHeads*headDim, headDim, 256)
	for h := 0; h < nHeads; h++ {
		if out[h*headDim] > 0.2 {
			t.Fatalf("head %d q0 = %g", h, out[h*headDim])
		}
		last := 257*nHeads*headDim + h*headDim
		if out[last] < 4 {
			t.Fatalf("head %d q257 = %g", h, out[last])
		}
	}
}

func attnSpike(seq, nHeads, headDim int) (q, k, v []float32) {
	q = make([]float32, seq*nHeads*headDim)
	k = make([]float32, seq*headDim)
	v = make([]float32, seq*headDim)
	for i := range q {
		q[i] = 0.01
	}
	for i := range k {
		k[i] = 0.01
		v[i] = 0.01
	}
	lastQ := (seq - 1) * nHeads * headDim
	for h := 0; h < nHeads; h++ {
		for d := 0; d < headDim; d++ {
			q[h*headDim+d] = 20
			q[lastQ+h*headDim+d] = 20
		}
	}
	base := (seq - 1) * headDim
	for d := 0; d < headDim; d++ {
		k[base+d] = 20
		v[base+d] = 5
	}
	return q, k, v
}

func TestKeyLengthMismatchFailsLoad(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad-key.gguf")
	writeHeaderGGUF(t, bad, map[string]any{
		"general.architecture":                 "gemma-embedding",
		"gemma-embedding.embedding_length":     uint32(768),
		"gemma-embedding.attention.head_count": uint32(3),
		"gemma-embedding.attention.key_length": uint32(128),
	})
	_, err := NewGemmaEmbedder(bad, 768)
	if err == nil || !strings.Contains(err.Error(), "attention.key_length 128") {
		t.Fatalf("mismatch load err=%v", err)
	}
	t.Logf("key_length mismatch: %v", err)

	okHead := filepath.Join(dir, "matching-key.gguf")
	writeHeaderGGUF(t, okHead, map[string]any{
		"general.architecture":                 "gemma-embedding",
		"gemma-embedding.embedding_length":     uint32(768),
		"gemma-embedding.attention.head_count": uint32(3),
		"gemma-embedding.attention.key_length": uint32(256),
	})
	_, err = NewGemmaEmbedder(okHead, 768)
	if err == nil || strings.Contains(err.Error(), "attention.key_length") {
		t.Fatalf("matching key_length should pass the check, err=%v", err)
	}
}

func writeHeaderGGUF(t *testing.T, path string, meta map[string]any) {
	t.Helper()
	var buf []byte
	putU32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	putU64 := func(v uint64) { buf = binary.LittleEndian.AppendUint64(buf, v) }
	putStr := func(s string) {
		putU64(uint64(len(s)))
		buf = append(buf, s...)
	}
	putU32(0x46554747)
	putU32(3)
	putU64(0)
	putU64(uint64(len(meta)))
	for k, v := range meta {
		putStr(k)
		switch x := v.(type) {
		case string:
			putU32(8)
			putStr(x)
		case uint32:
			putU32(4)
			putU32(x)
		default:
			t.Fatalf("unsupported meta %T", v)
		}
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}
