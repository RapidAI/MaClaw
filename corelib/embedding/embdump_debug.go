// embdump_debug.go — stage-by-stage forward-pass tracer used to validate the
// C++ port of the Gemma embedding inference path against this Go reference.
//
// It mirrors forwardWithScratch exactly but writes every intermediate buffer to
// a binary file so the two implementations can be diffed layer by layer.
//
// Record format (all little-endian):
//
//	uint32 nameLen, name bytes, uint32 n, n * float32
package embedding

import (
	"bufio"
	"encoding/binary"
	"math"
	"os"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

type traceWriter struct {
	f   *os.File
	buf *bufio.Writer
	rec []byte
}

func newTraceWriter(path string) (*traceWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &traceWriter{f: f, buf: bufio.NewWriterSize(f, 1<<20)}, nil
}

func (t *traceWriter) put(name string, v []float32) {
	t.rec = t.rec[:0]
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(name)))
	t.buf.Write(hdr[:])
	t.buf.WriteString(name)
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(v)))
	t.buf.Write(hdr[:])
	for _, x := range v {
		binary.LittleEndian.PutUint32(hdr[:], math.Float32bits(x))
		t.buf.Write(hdr[:])
	}
}

func (t *traceWriter) close() {
	t.buf.Flush()
	t.f.Close()
}

// DumpForwardTrace runs the reference forward pass for tokenIDs and writes all
// intermediate activations to path.
func (g *GemmaEmbedder) DumpForwardTrace(tokenIDs []int, path string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	tw, err := newTraceWriter(path)
	if err != nil {
		return err
	}
	defer tw.close()

	hp := g.hp
	seq := len(tokenIDs)
	dim := hp.Dim
	kvDim := hp.KVDim
	headDim := hp.HeadDim
	nHeads := hp.NHeads
	nKVHeads := hp.NKVHeads
	ffDim := hp.FFDim

	sc := g.ensureScratch(seq)

	// --- token embedding ---
	x := sc.x[:seq*dim]
	embScale := float32(math.Sqrt(float64(dim)))
	for si, id := range tokenIDs {
		dst := x[si*dim : (si+1)*dim]
		if cached := g.tokenCache.Get(id); cached != nil {
			copy(dst, cached)
		} else {
			g.weights.tokenEmb.DequantRow(id, sc.rowBuf)
			copy(dst, sc.rowBuf)
		}
		tensor.Scale(dst, embScale)
	}
	tw.put("emb", x)

	normed := sc.normed[:seq*dim]
	q := sc.q[:seq*dim]
	k := sc.k[:seq*kvDim]
	v := sc.v[:seq*kvDim]
	attnOut := sc.attnOut[:seq*dim]
	projOut := sc.projOut[:seq*dim]
	ffGate := sc.ffGate[:seq*ffDim]
	ffUp := sc.ffUp[:seq*ffDim]
	ffDown := sc.ffDown[:seq*dim]
	halfDim := headDim / 2

	nLayers := hp.NLayers
	if ee := int(atomic.LoadInt32(&g.earlyExit)); ee > 0 && ee < nLayers {
		nLayers = ee
	}

	for l := 0; l < nLayers; l++ {
		layer := &g.weights.layers[l]
		p := "l" + itoa(l) + "."

		// Same per-layer config as layerLoop: the window width and the RoPE base
		// both vary by layer, so the trace has to apply the same ones or the
		// dumps stop being comparable with a real forward pass.
		halfW := hp.HalfWindowFor(l)
		ropeCos, ropeSin := sc.ropeCos, sc.ropeSin
		if len(sc.ropeCosSwa) > 0 && hp.IsSwaLayer(l) {
			ropeCos, ropeSin = sc.ropeCosSwa, sc.ropeSinSwa
		}

		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.attnNormW, hp.RMSNormEps)
		}
		tw.put(p+"attnNorm", normed)

		tensor.MatMulQ8(q, normed, &layer.attnQWeight, seq, dim, dim)
		tensor.MatMulQ8(k, normed, &layer.attnKWeight, seq, kvDim, dim)
		tensor.MatMulQ8(v, normed, &layer.attnVWeight, seq, kvDim, dim)
		tw.put(p+"q", q)
		tw.put(p+"k", k)
		tw.put(p+"v", v)

		for s := 0; s < seq; s++ {
			for h := 0; h < nHeads; h++ {
				off := s*dim + h*headDim
				tensor.RMSNorm(q[off:off+headDim], q[off:off+headDim], layer.attnQNormW, hp.RMSNormEps)
			}
			for h := 0; h < nKVHeads; h++ {
				off := s*kvDim + h*headDim
				tensor.RMSNorm(k[off:off+headDim], k[off:off+headDim], layer.attnKNormW, hp.RMSNormEps)
			}
			cosTab := ropeCos[s*halfDim : (s+1)*halfDim]
			sinTab := ropeSin[s*halfDim : (s+1)*halfDim]
			tensor.RoPEPrecomputed(q[s*dim:(s+1)*dim], nHeads, headDim, cosTab, sinTab)
			tensor.RoPEPrecomputed(k[s*kvDim:(s+1)*kvDim], nKVHeads, headDim, cosTab, sinTab)
		}
		tw.put(p+"qrope", q)
		tw.put(p+"krope", k)

		g.gqaAttention(attnOut, q, k, v, seq, nHeads, nKVHeads, headDim, dim, kvDim, halfW)
		tw.put(p+"attnOut", attnOut)

		tensor.MatMulQ8(projOut, attnOut, &layer.attnOutWeight, seq, dim, dim)
		tw.put(p+"projOut", projOut)

		for s := 0; s < seq; s++ {
			tensor.RMSNorm(projOut[s*dim:(s+1)*dim], projOut[s*dim:(s+1)*dim], layer.postAttnNormW, hp.RMSNormEps)
		}
		tw.put(p+"postAttnNorm", projOut)
		tensor.Add(x, x, projOut)
		tw.put(p+"x1", x)

		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.ffNormW, hp.RMSNormEps)
		}
		tw.put(p+"ffNorm", normed)

		tensor.MatMulQ8(ffGate, normed, &layer.ffGateWeight, seq, ffDim, dim)
		tensor.MatMulQ8(ffUp, normed, &layer.ffUpWeight, seq, ffDim, dim)
		tw.put(p+"ffGate", ffGate)
		tw.put(p+"ffUp", ffUp)
		tensor.GeluMul(ffGate, ffUp)
		tw.put(p+"ffSilu", ffGate)

		tensor.MatMulQ8(ffDown, ffGate, &layer.ffDownWeight, seq, dim, ffDim)
		tw.put(p+"ffDown", ffDown)

		for s := 0; s < seq; s++ {
			tensor.RMSNorm(ffDown[s*dim:(s+1)*dim], ffDown[s*dim:(s+1)*dim], layer.postFFNNormW, hp.RMSNormEps)
		}
		tw.put(p+"postFFNNorm", ffDown)
		tensor.Add(x, x, ffDown)
		tw.put(p+"x2", x)
	}

	for s := 0; s < seq; s++ {
		tensor.RMSNorm(x[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], g.weights.outputNorm, hp.RMSNormEps)
	}
	tw.put("finalNorm", x)

	out := sc.poolOut[:dim]
	for i := range out {
		out[i] = 0
	}
	for s := 0; s < seq; s++ {
		tensor.Add(out, out, x[s*dim:(s+1)*dim])
	}
	tensor.Scale(out, 1.0/float32(seq))
	tw.put("pool", out)

	return nil
}

// TokenIDs exposes the tokenizer so debug tools can dump token sequences.
func (g *GemmaEmbedder) TokenIDs(text string) []int {
	return g.tokenizer.Encode(text)
}

// RopeTables exposes the precomputed RoPE cos/sin tables for debugging.
func (g *GemmaEmbedder) RopeTables(seq int) (cos, sin []float32) {
	sc := newGemmaScratch(g.hp, seq)
	return sc.ropeCos, sc.ropeSin
}

// HParams exposes hyperparameters for debug tools.
func (g *GemmaEmbedder) HParams() GemmaHParams { return g.hp }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
