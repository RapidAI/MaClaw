package asr

import (
"math"
"runtime"
"sync"

"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
"github.com/viterin/vek/vek32"
)

func (m *MoonshineModel) encode(pcm []float32) ([]float32, int, error) {
hp := m.hp
w := &m.w
dim := hp.EncoderDim
inLen := len(pcm)
x := conv1dParallel(pcm, inLen, 1, w.conv1W, 127, dim, 64)
nFrames := len(x) / dim
if w.conv1B != nil { tensor.AddBias(x, nFrames, dim, w.conv1B) }
tensor.Tanh(x)
tensor.GroupNorm1(x, nFrames, dim, w.gnormW, w.gnormB, 1e-5)
c1 := 2 * dim
x = conv1dParallel(x, nFrames, dim, w.conv2W, 7, c1, 3)
nFrames = len(x) / c1
if w.conv2B != nil { tensor.AddBias(x, nFrames, c1, w.conv2B) }
tensor.GELU(x)
x = conv1dParallel(x, nFrames, c1, w.conv3W, 3, dim, 2)
nFrames = len(x) / dim
if w.conv3B != nil { tensor.AddBias(x, nFrames, dim, w.conv3B) }
tensor.GELU(x)
for li := 0; li < hp.EncoderDepth; li++ {
x = m.encoderLayer(x, nFrames, &w.encLayers[li])
}
for f := 0; f < nFrames; f++ {
off := f * dim
tensor.LayerNorm(x[off:off+dim], x[off:off+dim], w.encFinalNormW, 1e-5)
}
return x, nFrames, nil
}

// conv1dParallel: parallelized conv1d with ggml kernel layout [outCh][inCh][kSize].
func conv1dParallel(input []float32, inLen, inCh int, kernel []float32, kSize, outCh, stride int) []float32 {
outLen := (inLen - kSize) / stride + 1
if outLen <= 0 { return nil }
out := make([]float32, outLen*outCh)
nWorkers := runtime.NumCPU()
if nWorkers > outLen { nWorkers = outLen }
var wg sync.WaitGroup
chunk := (outLen + nWorkers - 1) / nWorkers
for w := 0; w < nWorkers; w++ {
s, e := w*chunk, (w+1)*chunk
if e > outLen { e = outLen }
if s >= e { break }
wg.Add(1)
go func(s, e int) {
defer wg.Done()
for o := s; o < e; o++ {
inStart := o * stride
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
}(s, e)
}
wg.Wait()
return out
}

func (m *MoonshineModel) encoderLayer(x []float32, nFrames int, l *encoderLayer) []float32 {
dim := m.hp.EncoderDim
nHeads := m.hp.EncoderHeads
headDim := m.hp.EncoderHDim
residual := make([]float32, len(x))
copy(residual, x)
for f := 0; f < nFrames; f++ {
off := f * dim
tensor.LayerNorm(x[off:off+dim], x[off:off+dim], l.attnNormW, 1e-5)
}
q := make([]float32, nFrames*dim)
k := make([]float32, nFrames*dim)
v := make([]float32, nFrames*dim)
tensor.MatMul(q, x, l.attnQW, nFrames, dim, dim)
tensor.MatMul(k, x, l.attnKW, nFrames, dim, dim)
tensor.MatMul(v, x, l.attnVW, nFrames, dim, dim)
for f := 0; f < nFrames; f++ {
tensor.RoPEInterleaved(q[f*dim:(f+1)*dim], nHeads, headDim, f, m.hp.RopeTheta, m.hp.PartialRot)
tensor.RoPEInterleaved(k[f*dim:(f+1)*dim], nHeads, headDim, f, m.hp.RopeTheta, m.hp.PartialRot)
}
attnOut := sdpaMultiHead(q, k, v, nFrames, nFrames, nHeads, headDim)
projOut := make([]float32, nFrames*dim)
tensor.MatMul(projOut, attnOut, l.attnOutW, nFrames, dim, dim)
tensor.Add(residual, residual, projOut)
copy(x, residual)
for f := 0; f < nFrames; f++ {
off := f * dim
tensor.LayerNorm(x[off:off+dim], x[off:off+dim], l.ffNormW, 1e-5)
}
ffDim := len(l.ffUpW) / dim
ffOut := make([]float32, nFrames*ffDim)
tensor.MatMul(ffOut, x, l.ffUpW, nFrames, ffDim, dim)
if l.ffUpB != nil { tensor.AddBias(ffOut, nFrames, ffDim, l.ffUpB) }
tensor.GELU(ffOut)
downOut := make([]float32, nFrames*dim)
tensor.MatMul(downOut, ffOut, l.ffDownW, nFrames, dim, ffDim)
if l.ffDownB != nil { tensor.AddBias(downOut, nFrames, dim, l.ffDownB) }
tensor.Add(residual, residual, downOut)
return residual
}

// sdpaMultiHead with SIMD-accelerated dot products via vek32.Dot.
func sdpaMultiHead(q, k, v []float32, seqQ, seqK, nHeads, headDim int) []float32 {
dim := nHeads * headDim
out := make([]float32, seqQ*dim)
scale := 1.0 / float32(math.Sqrt(float64(headDim)))
scores := make([]float32, seqK)
for h := 0; h < nHeads; h++ {
hOff := h * headDim
for sq := 0; sq < seqQ; sq++ {
qVec := q[sq*dim+hOff : sq*dim+hOff+headDim]
for sk := 0; sk < seqK; sk++ {
kOff := sk*dim + hOff
scores[sk] = vek32.Dot(qVec, k[kOff:kOff+headDim]) * scale
}
tensor.Softmax(scores[:seqK])
outOff := sq*dim + hOff
for i := 0; i < headDim; i++ { out[outOff+i] = 0 }
for sk := 0; sk < seqK; sk++ {
vOff := sk*dim + hOff
w := scores[sk]
for i := 0; i < headDim; i++ { out[outOff+i] += w * v[vOff+i] }
}
}
}
return out
}
