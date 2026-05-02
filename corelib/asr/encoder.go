package asr

import (
	"math"
	"runtime"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// encoderBufs holds reusable scratch buffers for encoder layers,
// eliminating per-layer allocations during inference.
type encoderBufs struct {
	q, k, v   []float32 // [maxFrames * dim]
	residual  []float32
	ffOut     []float32 // [maxFrames * ffDim]
	downOut   []float32
	attnOut   []float32
	projOut   []float32
	scores    []float32 // [maxFrames] for sdpa
	rotaryDim int       // cached, avoids recomputing per layer
	// RoPE precomputed cos/sin tables: [maxFrames][halfRotDim]
	ropeCos [][]float32
	ropeSin [][]float32
}

func newEncoderBufs(maxFrames, dim, ffDim, nHeads, headDim int, theta, partialRot float32) *encoderBufs {
	rotaryDim := int(float32(headDim) * partialRot)
	rotaryDim -= rotaryDim % 2
	halfRot := rotaryDim / 2

	eb := &encoderBufs{
		q:         make([]float32, maxFrames*dim),
		k:         make([]float32, maxFrames*dim),
		v:         make([]float32, maxFrames*dim),
		residual:  make([]float32, maxFrames*dim),
		ffOut:     make([]float32, maxFrames*ffDim),
		downOut:   make([]float32, maxFrames*dim),
		attnOut:   make([]float32, maxFrames*dim),
		projOut:   make([]float32, maxFrames*dim),
		scores:    make([]float32, maxFrames),
		rotaryDim: rotaryDim,
		ropeCos:   make([][]float32, maxFrames),
		ropeSin:   make([][]float32, maxFrames),
	}
	// Precompute frequency table (only depends on dimension index, not position)
	freqs := make([]float32, halfRot)
	for i := 0; i < halfRot; i++ {
		freqs[i] = 1.0 / float32(math.Pow(float64(theta), float64(2*i)/float64(headDim)))
	}
	// Precompute RoPE cos/sin tables for all positions
	for pos := 0; pos < maxFrames; pos++ {
		eb.ropeCos[pos] = make([]float32, halfRot)
		eb.ropeSin[pos] = make([]float32, halfRot)
		for i := 0; i < halfRot; i++ {
			angle := float32(pos) * freqs[i]
			eb.ropeCos[pos][i] = float32(math.Cos(float64(angle)))
			eb.ropeSin[pos][i] = float32(math.Sin(float64(angle)))
		}
	}
	return eb
}

func (m *MoonshineModel) encode(pcm []float32) ([]float32, int, error) {
	hp := m.hp
	w := &m.w
	dim := hp.EncoderDim
	inLen := len(pcm)

	// Conv frontend
	x := conv1dParallel(pcm, inLen, 1, w.conv1W, 127, dim, 64)
	nFrames := len(x) / dim
	if w.conv1B != nil {
		tensor.AddBias(x, nFrames, dim, w.conv1B)
	}
	tensor.Tanh(x)
	tensor.GroupNorm1(x, nFrames, dim, w.gnormW, w.gnormB, 1e-5)

	c1 := 2 * dim
	x = conv1dParallel(x, nFrames, dim, w.conv2W, 7, c1, 3)
	nFrames = len(x) / c1
	if w.conv2B != nil {
		tensor.AddBiasGELU(x, nFrames, c1, w.conv2B)
	} else {
		tensor.GELU(x)
	}

	x = conv1dParallel(x, nFrames, c1, w.conv3W, 3, dim, 2)
	nFrames = len(x) / dim
	if w.conv3B != nil {
		tensor.AddBiasGELU(x, nFrames, dim, w.conv3B)
	} else {
		tensor.GELU(x)
	}

	// Allocate encoder scratch buffers once for all layers
	ffDim := w.encLayers[0].ffUpW.Rows()
	bufs := newEncoderBufs(nFrames, dim, ffDim, hp.EncoderHeads, hp.EncoderHDim, hp.RopeTheta, hp.PartialRot)

	for li := 0; li < hp.EncoderDepth; li++ {
		m.encoderLayerOpt(x, nFrames, &w.encLayers[li], bufs)
	}

	// Final layer norm
	for f := 0; f < nFrames; f++ {
		off := f * dim
		tensor.LayerNorm(x[off:off+dim], x[off:off+dim], w.encFinalNormW, 1e-5)
	}
	return x, nFrames, nil
}

// conv1dParallel: parallelized conv1d with ggml kernel layout [outCh][inCh][kSize].
func conv1dParallel(input []float32, inLen, inCh int, kernel []float32, kSize, outCh, stride int) []float32 {
	outLen := (inLen-kSize)/stride + 1
	if outLen <= 0 {
		return nil
	}
	out := make([]float32, outLen*outCh)

	nWorkers := runtime.NumCPU()
	if nWorkers > outLen {
		nWorkers = outLen
	}
	if nWorkers <= 1 {
		conv1dRange(input, kernel, out, 0, outLen, inCh, kSize, outCh, stride)
		return out
	}

	var wg sync.WaitGroup
	chunk := (outLen + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		s, e := w*chunk, (w+1)*chunk
		if e > outLen {
			e = outLen
		}
		if s >= e {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			conv1dRange(input, kernel, out, s, e, inCh, kSize, outCh, stride)
		}(s, e)
	}
	wg.Wait()
	return out
}

// conv1dRange computes conv1d for output positions [s, e).
// Uses SIMD dot product when kernel*inCh is large enough.
func conv1dRange(input, kernel, out []float32, s, e, inCh, kSize, outCh, stride int) {
	patchSize := inCh * kSize
	for o := s; o < e; o++ {
		inStart := o * stride
		if patchSize >= 16 && inCh == 1 {
			// For inCh=1, input patch is contiguous — use SIMD dot
			patch := input[inStart : inStart+kSize]
			for oc := 0; oc < outCh; oc++ {
				kOff := oc * kSize
				out[o*outCh+oc] = vek32.Dot(patch, kernel[kOff:kOff+kSize])
			}
		} else {
			for oc := 0; oc < outCh; oc++ {
				var sum float32
				for ic := 0; ic < inCh; ic++ {
					kOff := (oc*inCh + ic) * kSize
					for k := 0; k < kSize; k++ {
						sum += input[(inStart+k)*inCh+ic] * kernel[kOff+k]
					}
				}
				out[o*outCh+oc] = sum
			}
		}
	}
}

// encoderLayerOpt is the optimized encoder layer using pre-allocated buffers.
// Writes result back into x in-place.
func (m *MoonshineModel) encoderLayerOpt(x []float32, nFrames int, l *encoderLayer, eb *encoderBufs) {
	dim := m.hp.EncoderDim
	nHeads := m.hp.EncoderHeads
	headDim := m.hp.EncoderHDim
	n := nFrames * dim

	// Residual = x
	copy(eb.residual[:n], x[:n])

	// Pre-attention LayerNorm (in-place on x)
	for f := 0; f < nFrames; f++ {
		off := f * dim
		tensor.LayerNorm(x[off:off+dim], x[off:off+dim], l.attnNormW, 1e-5)
	}

	// Q, K, V projections (Q8 quantized)
	q := eb.q[:n]
	k := eb.k[:n]
	v := eb.v[:n]
	matMulLinear(q, x[:n], l.attnQW, nFrames, dim, dim)
	matMulLinear(k, x[:n], l.attnKW, nFrames, dim, dim)
	matMulLinear(v, x[:n], l.attnVW, nFrames, dim, dim)

	// RoPE with precomputed tables
	rotaryDim := eb.rotaryDim
	for f := 0; f < nFrames; f++ {
		ropeInterleavedPrecomp(q[f*dim:(f+1)*dim], nHeads, headDim, rotaryDim, eb.ropeCos[f], eb.ropeSin[f])
		ropeInterleavedPrecomp(k[f*dim:(f+1)*dim], nHeads, headDim, rotaryDim, eb.ropeCos[f], eb.ropeSin[f])
	}

	// Multi-head attention
	attnOut := eb.attnOut[:n]
	sdpaMultiHeadOpt(q, k, v, attnOut, eb.scores[:nFrames], nFrames, nFrames, nHeads, headDim)

	// Output projection + residual
	projOut := eb.projOut[:n]
	matMulLinear(projOut, attnOut, l.attnOutW, nFrames, dim, dim)
	tensor.Add(x[:n], eb.residual[:n], projOut)

	// FFN residual
	copy(eb.residual[:n], x[:n])

	// Post-attention LayerNorm
	for f := 0; f < nFrames; f++ {
		off := f * dim
		tensor.LayerNorm(x[off:off+dim], x[off:off+dim], l.ffNormW, 1e-5)
	}

	// FFN up + GELU + down
	ffDim := l.ffUpW.Rows()
	ffOut := eb.ffOut[:nFrames*ffDim]
	matMulLinear(ffOut, x[:n], l.ffUpW, nFrames, ffDim, dim)
	if l.ffUpB != nil {
		tensor.AddBiasGELU(ffOut, nFrames, ffDim, l.ffUpB)
	} else {
		tensor.GELU(ffOut)
	}

	downOut := eb.downOut[:n]
	matMulLinear(downOut, ffOut, l.ffDownW, nFrames, dim, ffDim)
	if l.ffDownB != nil {
		tensor.AddBias(downOut, nFrames, dim, l.ffDownB)
	}
	tensor.Add(x[:n], eb.residual[:n], downOut)
}

// ropeInterleavedPrecomp applies interleaved RoPE using precomputed cos/sin tables.
// Avoids math.Pow/Cos/Sin per call — these are the hottest trig ops in the encoder.
func ropeInterleavedPrecomp(x []float32, nHeads, headDim, rotaryDim int, cosTable, sinTable []float32) {
	for h := 0; h < nHeads; h++ {
		off := h * headDim
		for i := 0; i < rotaryDim; i += 2 {
			ci := i / 2
			cos := cosTable[ci]
			sin := sinTable[ci]
			x0 := x[off+i]
			x1 := x[off+i+1]
			x[off+i] = x0*cos - x1*sin
			x[off+i+1] = x0*sin + x1*cos
		}
	}
}

// sdpaMultiHeadOpt: optimized multi-head attention with reusable scores buffer.
func sdpaMultiHeadOpt(q, k, v, out, scores []float32, seqQ, seqK, nHeads, headDim int) {
	dim := nHeads * headDim
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))

	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		for sq := 0; sq < seqQ; sq++ {
			qVec := q[sq*dim+hOff : sq*dim+hOff+headDim]
			for sk := 0; sk < seqK; sk++ {
				kOff := sk*dim + hOff
				scores[sk] = vek32.Dot(qVec, k[kOff:kOff+headDim]) * scale
			}
			outOff := sq*dim + hOff
			tensor.SoftmaxWeightedSumStrided(out[outOff:outOff+headDim], scores[:seqK], v[hOff:], seqK, dim, headDim)
		}
	}
}
