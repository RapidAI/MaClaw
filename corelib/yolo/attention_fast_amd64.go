//go:build amd64

package yolo

import (
	"math"
	"runtime"
	"sync"
)

// tryForwardFast runs the C2PSA attention with the register-blocked GEMM and
// vectorized softmax instead of the per-element loops. Falls back (ok=false)
// when the AVX2 kernel cannot be used: scores need seq%16==0 (GEMM N) and
// headDim%16==0 (GEMM N of the scores@V product), plus sane head geometry.
//
// Layout per head h:
//
//	qT  [seq, headDim]  (transposed extraction of q channels)
//	kH  [headDim, seq]  (natural extraction of k channels)
//	vT  [seq, headDim]  (transposed extraction of v = input x channels)
//	scores = qT @ kH              [seq, seq]
//	scores = softmax(scores * scale)  row-wise
//	attn   = scores @ vT          [seq, headDim]  → scattered to out planes
func (a *Attention) tryForwardFast(x *Tensor) (*Tensor, bool) {
	N := x.Shape[0]
	H, W := x.Shape[2], x.Shape[3]

	qkv := a.QKV.Forward(x) // [N, 2*halfC, H, W], no activation
	halfC := qkv.Shape[1] / 2
	headDim := halfC / a.NumHeads
	seqLen := H * W
	if !hasAVX2FMA || N != 1 || a.NumHeads < 1 || headDim < 16 ||
		seqLen%16 != 0 || headDim%16 != 0 || halfC%a.NumHeads != 0 {
		return nil, false
	}
	scale := float32(1.0 / math.Sqrt(float64(headDim)))

	v := x // V comes from the input directly (matches forwardSlow)
	out := NewTensor(N, halfC, H, W)

	// Parallel over heads; each head owns its buffers (scores and
	// outPerHead are allocated per goroutine — heads run concurrently).
	var wg sync.WaitGroup
	nWorkers := min(runtime.NumCPU(), a.NumHeads)
	per := (a.NumHeads + nWorkers - 1) / nWorkers
	for wk := 0; wk < nWorkers; wk++ {
		h0 := wk * per
		h1 := min(h0+per, a.NumHeads)
		if h0 >= h1 {
			break
		}
		wg.Add(1)
		go func(h0, h1 int) {
			defer wg.Done()
			qT := getBufUnzeroed(seqLen * headDim)
			defer putBuf(qT)
			kH := getBufUnzeroed(headDim * seqLen)
			defer putBuf(kH)
			vT := getBufUnzeroed(seqLen * headDim)
			defer putBuf(vT)
			outPerHead := getBufUnzeroed(seqLen * headDim)
			defer putBuf(outPerHead)
			scores := getBufUnzeroed(seqLen * seqLen)
			defer putBuf(scores)
			zeroBias := make([]float32, seqLen)
			for h := h0; h < h1; h++ {
				chStart := h * headDim
				// qT[i*headDim+d] = q[n=0, chStart+d, i]; kH natural rows.
				for d := 0; d < headDim; d++ {
					srcOff := (chStart + d) * seqLen
					for i := 0; i < seqLen; i++ {
						qT[i*headDim+d] = qkv.Data[srcOff+i]
					}
				}
				for d := 0; d < headDim; d++ {
					srcOff := (halfC + chStart + d) * seqLen
					copy(kH[d*seqLen:d*seqLen+seqLen], qkv.Data[srcOff:srcOff+seqLen])
				}
				for d := 0; d < headDim; d++ {
					srcOff := (chStart + d) * seqLen
					for i := 0; i < seqLen; i++ {
						vT[i*headDim+d] = v.Data[srcOff+i]
					}
				}

				// scores = qT @ kH (fused bias = 0), then scale + softmax.
				gemmF32(scores, qT, kH, zeroBias, seqLen, headDim, seqLen)
				scaleSlice(scores, scale)
				softmaxRows(scores, seqLen)

				// attn = scores @ vT → [seq, headDim], scatter to out planes.
				gemmF32(outPerHead, scores, vT, zeroBias, seqLen, seqLen, headDim)
				for d := 0; d < headDim; d++ {
					dst := out.Data[chStart*seqLen+d*seqLen:]
					for i := 0; i < seqLen; i++ {
						dst[i] = outPerHead[i*headDim+d]
					}
				}
			}
		}(h0, h1)
	}
	wg.Wait()

	// Positional encoding on V, then project (matches forwardSlow).
	pe := a.PE.Forward(v) // depthwise 3x3 conv
	out.Add(pe)
	return a.Proj.Forward(out), true
}

// softmaxRows applies row-wise softmax in place on a [rows, cols] row-major
// matrix. Vectorized exp; max and sum are scalar (cols ~ hundreds).
func softmaxRows(m []float32, cols int) {
	rows := len(m) / cols
	for r := 0; r < rows; r++ {
		row := m[r*cols : (r+1)*cols]
		maxVal := row[0]
		for _, v := range row[1:] {
			if v > maxVal {
				maxVal = v
			}
		}
		for i := range row {
			row[i] -= maxVal
		}
		expSlice(row, row)
		sum := float32(0)
		for _, v := range row {
			sum += v
		}
		scaleSlice(row, 1/sum)
	}
}
