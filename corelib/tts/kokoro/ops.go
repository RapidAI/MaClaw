package kokoro

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
)

var float32BufferPool sync.Pool

var (
	genericDbgOnce sync.Once
	genericDbgSeen map[string]bool
	genericDbgMu   sync.Mutex
)

func getFloat32Buffer(n int) []float32 {
	if n <= 0 {
		return nil
	}
	if !useKokoroBufferPool() {
		return make([]float32, n)
	}
	if v := float32BufferPool.Get(); v != nil {
		buf := v.([]float32)
		if cap(buf) >= n {
			return buf[:n]
		}
	}
	return make([]float32, n)
}

func putFloat32Buffer(buf []float32) {
	if !useKokoroBufferPool() {
		return
	}
	if cap(buf) == 0 || cap(buf) > 64*1024*1024 {
		return
	}
	float32BufferPool.Put(buf[:0])
}

var kokoroConvScratchPool sync.Pool

// getConvScratch returns a pooled float32 scratch buffer of length n. The
// buffer is exclusively owned by the caller until putConvScratch.
func getConvScratch(n int) []float32 {
	if n <= 0 {
		return nil
	}
	if v := kokoroConvScratchPool.Get(); v != nil {
		if b, ok := v.([]float32); ok && cap(b) >= n {
			return b[:n]
		}
	}
	return make([]float32, n)
}

func putConvScratch(b []float32) {
	if cap(b) == 0 || cap(b) > 256<<20 {
		return
	}
	kokoroConvScratchPool.Put(b[:0])
}

func zeroFloat32s(buf []float32) {
	for i := range buf {
		buf[i] = 0
	}
}

// parallelApply32 applies fn element-wise, splitting work across goroutines
// for large slices.
func parallelApply32(x []float32, fn func(float32) float32) {
	workers := runtime.GOMAXPROCS(0)
	if len(x) < 1<<15 || workers < 2 {
		for i := range x {
			x[i] = fn(x[i])
		}
		return
	}
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	chunk := (len(x) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > len(x) {
			end = len(x)
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				x[i] = fn(x[i])
			}
		}(start, end)
	}
	wg.Wait()
}

func sigmoid(x float32) float32 {
	return 1 / (1 + float32(math.Exp(float64(-x))))
}

func tanh(x float32) float32 {
	return float32(math.Tanh(float64(x)))
}

func gelu(x float32) float32 {
	return 0.5 * x * (1 + float32(math.Tanh(float64(0.7978845608028654*(x+0.044715*x*x*x)))))
}

func leakyReLU(x, slope float32) float32 {
	if x >= 0 {
		return x
	}
	return slope * x
}

func Linear(out, x, weight, bias []float32, in, outDim int) error {
	if len(x) != in {
		return fmt.Errorf("kokoro: linear input length %d, want %d", len(x), in)
	}
	if len(weight) != outDim*in {
		return fmt.Errorf("kokoro: linear weight length %d, want %d", len(weight), outDim*in)
	}
	if len(out) != outDim {
		return fmt.Errorf("kokoro: linear output length %d, want %d", len(out), outDim)
	}
	if useKokoroStridedFMA() && in >= 8 {
		// Four output rows share each x load (one FMA pass, taps=1).
		o := 0
		for ; o+4 <= outDim; o += 4 {
			r0, r1, r2, r3 := dotStridedFMA4(x, in, weight[o*in:], in, in, 1, in)
			if bias != nil {
				r0 += bias[o]
				r1 += bias[o+1]
				r2 += bias[o+2]
				r3 += bias[o+3]
			}
			out[o] = r0
			out[o+1] = r1
			out[o+2] = r2
			out[o+3] = r3
		}
		for ; o < outDim; o++ {
			sum := dot32(x, weight[o*in:(o+1)*in])
			if bias != nil {
				sum += bias[o]
			}
			out[o] = sum
		}
		return nil
	}
	for o := 0; o < outDim; o++ {
		sum := dot32(x, weight[o*in:(o+1)*in])
		if bias != nil {
			sum += bias[o]
		}
		out[o] = sum
	}
	return nil
}

func LinearSequence(out, x, weight, bias []float32, steps, in, outDim int) error {
	if len(x) != steps*in || len(out) != steps*outDim {
		return fmt.Errorf("kokoro: linear sequence shape mismatch")
	}
	for t := 0; t < steps; t++ {
		if err := Linear(out[t*outDim:(t+1)*outDim], x[t*in:(t+1)*in], weight, bias, in, outDim); err != nil {
			return err
		}
	}
	return nil
}

func LinearTensor(out, x []float32, weight *Tensor, bias []float32, in, outDim int) error {
	if weight == nil {
		return fmt.Errorf("kokoro: nil linear weight tensor")
	}
	if weight.DType == TensorQ8Rowwise && useKokoroQ8Direct() {
		if rows, cols, ok := weight.Q8Shape(); !ok || rows != outDim || cols != in {
			return fmt.Errorf("kokoro: q8 linear weight shape mismatch")
		}
		if len(x) != in || len(out) != outDim || (bias != nil && len(bias) < outDim) {
			return fmt.Errorf("kokoro: q8 linear input/output shape mismatch")
		}
		scratch := make([]float32, in)
		for o := 0; o < outDim; o++ {
			if err := weight.DequantQ8Row(o, scratch); err != nil {
				return err
			}
			sum := dot32(x, scratch)
			if bias != nil {
				sum += bias[o]
			}
			out[o] = sum
		}
		return nil
	}
	w, err := weight.Float32()
	if err != nil {
		return err
	}
	return Linear(out, x, w, bias, in, outDim)
}

func LinearSequenceTensor(out, x []float32, weight *Tensor, bias []float32, steps, in, outDim int) error {
	if len(x) != steps*in || len(out) != steps*outDim {
		return fmt.Errorf("kokoro: linear sequence tensor shape mismatch")
	}
	workers := runtime.GOMAXPROCS(0)
	if steps >= 8 && in*outDim >= 1<<16 && workers > 1 {
		if workers > steps {
			workers = steps
		}
		if workers > 8 {
			workers = 8
		}
		var wg sync.WaitGroup
		errCh := make(chan error, workers)
		chunk := (steps + workers - 1) / workers
		for w := 0; w < workers; w++ {
			start := w * chunk
			end := start + chunk
			if end > steps {
				end = steps
			}
			if start >= end {
				break
			}
			wg.Add(1)
			go func(t0, t1 int) {
				defer wg.Done()
				for t := t0; t < t1; t++ {
					if err := LinearTensor(out[t*outDim:(t+1)*outDim], x[t*in:(t+1)*in], weight, bias, in, outDim); err != nil {
						errCh <- err
						return
					}
				}
			}(start, end)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			return err
		}
		return nil
	}
	for t := 0; t < steps; t++ {
		if err := LinearTensor(out[t*outDim:(t+1)*outDim], x[t*in:(t+1)*in], weight, bias, in, outDim); err != nil {
			return err
		}
	}
	return nil
}

func Embedding(out []float32, ids []int, table []float32, vocab, dim int) error {
	if len(table) != vocab*dim || len(out) != len(ids)*dim {
		return fmt.Errorf("kokoro: embedding shape mismatch")
	}
	for i, id := range ids {
		if id < 0 || id >= vocab {
			return fmt.Errorf("kokoro: token id %d outside vocab %d", id, vocab)
		}
		copy(out[i*dim:(i+1)*dim], table[id*dim:(id+1)*dim])
	}
	return nil
}

func LayerNorm1D(out, x, gamma, beta []float32, eps float32) error {
	n := len(x)
	if len(out) != n || len(gamma) != n || (beta != nil && len(beta) != n) {
		return fmt.Errorf("kokoro: layernorm shape mismatch")
	}
	mean := sum32(x) / float32(n)
	variance := dot32(x, x)/float32(n) - mean*mean
	if variance < 0 {
		variance = 0
	}
	inv := 1 / float32(math.Sqrt(float64(variance+eps)))
	for i, v := range x {
		b := float32(0)
		if beta != nil {
			b = beta[i]
		}
		out[i] = (v-mean)*inv*gamma[i] + b
	}
	return nil
}

func LayerNormLastDim(out, x, gamma, beta []float32, rows, dim int, eps float32) error {
	if len(out) != rows*dim || len(x) != rows*dim {
		return fmt.Errorf("kokoro: layernorm rows shape mismatch")
	}
	for r := 0; r < rows; r++ {
		if err := LayerNorm1D(out[r*dim:(r+1)*dim], x[r*dim:(r+1)*dim], gamma, beta, eps); err != nil {
			return err
		}
	}
	return nil
}

// transposeChannelTime writes dst[t*channels+c] = x[c*T+t] using 8x8 AVX2
// blocks with scalar fallbacks for edge blocks.
func transposeChannelTime(dst, x []float32, channels, T int) {
	if useKokoroSIMD() && channels >= 8 && T >= 8 {
		cBlocks := channels / 8
		tBlocks := T / 8
		for tb := 0; tb < tBlocks; tb++ {
			t0 := tb * 8
			for cb := 0; cb < cBlocks; cb++ {
				c0 := cb * 8
				transpose8x8F32AVX2(x[c0*T+t0:], T, dst[t0*channels+c0:], channels)
			}
			// remainder channels for these time rows
			for c := cBlocks * 8; c < channels; c++ {
				for j := 0; j < 8; j++ {
					dst[(t0+j)*channels+c] = x[c*T+t0+j]
				}
			}
		}
		// remainder time rows (all channels)
		for t := tBlocks * 8; t < T; t++ {
			for c := 0; c < channels; c++ {
				dst[t*channels+c] = x[c*T+t]
			}
		}
		return
	}
	for c := 0; c < channels; c++ {
		src := x[c*T : (c+1)*T]
		for t, v := range src {
			dst[t*channels+c] = v
		}
	}
}

// Conv1D computes PyTorch-style Conv1d over [C,T] input and [Out, C/groups, K]
// weights, returning [Out,Tout].
func Conv1D(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, dilation, groups int) error {
	if groups <= 0 || inC%groups != 0 || outC%groups != 0 {
		return fmt.Errorf("kokoro: invalid conv groups")
	}
	outT := (inT+2*padding-dilation*(kernel-1)-1)/stride + 1
	if outT < 0 {
		outT = 0
	}
	if len(out) != outC*outT || len(x) != inC*inT || len(weight) != outC*(inC/groups)*kernel {
		return fmt.Errorf("kokoro: conv1d shape mismatch")
	}
	if useKokoroSIMD() && groups == 1 && kernel == 1 && stride == 1 && padding == 0 && dilation == 1 && inC >= 16 && outC*inT*inC > 50000 {
		return conv1DPointwiseSIMD(out, x, weight, bias, inC, inT, outC)
	}
	if useKokoroSIMD() && useKokoroConvMatMul() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		return conv1DMatMul(out, x, weight, bias, inC, inT, outC, kernel, stride, padding, dilation, outT)
	}
	if useKokoroSIMD() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		return conv1DSIMD(out, x, weight, bias, inC, inT, outC, kernel, stride, padding, dilation, outT)
	}
	if outC*outT*(inC/groups)*kernel > 200000 {
		return conv1DParallel(out, x, weight, bias, inC, inT, outC, kernel, stride, padding, dilation, groups, outT)
	}
	for oc := 0; oc < outC; oc++ {
		g := oc / (outC / groups)
		inStart := g * (inC / groups)
		for ot := 0; ot < outT; ot++ {
			sum := float32(0)
			if bias != nil {
				sum = bias[oc]
			}
			for icg := 0; icg < inC/groups; icg++ {
				ic := inStart + icg
				for k := 0; k < kernel; k++ {
					it := ot*stride + k*dilation - padding
					if it < 0 || it >= inT {
						continue
					}
					wv := weight[(oc*(inC/groups)+icg)*kernel+k]
					sum += x[ic*inT+it] * wv
				}
			}
			out[oc*outT+ot] = sum
		}
	}
	return nil
}

func conv1DMatMul(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, dilation, outT int) error {
	inputT := getConvScratch(inT * inC)
	defer putConvScratch(inputT)
	transposeChannelTime(inputT, x, inC, inT)
	partial := make([]float32, outT*outC)
	weightK := make([]float32, inC*outC)
	inputK := make([]float32, outT*inC)
	mm := make([]float32, outT*outC)
	for k := 0; k < kernel; k++ {
		for oc := 0; oc < outC; oc++ {
			for ic := 0; ic < inC; ic++ {
				weightK[ic*outC+oc] = weight[(oc*inC+ic)*kernel+k]
			}
		}
		for ot := 0; ot < outT; ot++ {
			it := ot*stride + k*dilation - padding
			dst := inputK[ot*inC : (ot+1)*inC]
			if it < 0 || it >= inT {
				for i := range dst {
					dst[i] = 0
				}
				continue
			}
			copy(dst, inputT[it*inC:(it+1)*inC])
		}
		matMulInto32(mm, inputK, weightK, inC)
		addInplace32(partial, mm)
	}
	for oc := 0; oc < outC; oc++ {
		b := float32(0)
		if bias != nil {
			b = bias[oc]
		}
		for ot := 0; ot < outT; ot++ {
			out[oc*outT+ot] = partial[ot*outC+oc] + b
		}
	}
	return nil
}

func conv1DPointwiseSIMD(out, x, weight, bias []float32, inC, inT, outC int) error {
	weightColMajor := make([]float32, inC*outC)
	transposePointwiseConv1DWeight(weightColMajor, weight, inC, outC)
	return conv1DPointwiseSIMDColMajor(out, x, weightColMajor, bias, inC, inT, outC)
}

func transposePointwiseConv1DWeight(weightColMajor, weight []float32, inC, outC int) {
	for oc := 0; oc < outC; oc++ {
		for ic := 0; ic < inC; ic++ {
			weightColMajor[ic*outC+oc] = weight[oc*inC+ic]
		}
	}
}

func conv1DPointwiseSIMDColMajor(out, x, weightColMajor, bias []float32, inC, inT, outC int) error {
	inputT := getConvScratch(inT * inC)
	defer putConvScratch(inputT)
	transposeChannelTime(inputT, x, inC, inT)
	outT := make([]float32, inT*outC)
	matMulInto32(outT, inputT, weightColMajor, inC)
	for oc := 0; oc < outC; oc++ {
		b := float32(0)
		if bias != nil {
			b = bias[oc]
		}
		for t := 0; t < inT; t++ {
			out[oc*inT+t] = outT[t*outC+oc] + b
		}
	}
	return nil
}

func ConvTranspose1D(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, outputPadding, groups int) error {
	if groups <= 0 || inC%groups != 0 || outC%groups != 0 {
		return fmt.Errorf("kokoro: invalid convtranspose groups")
	}
	outT := (inT-1)*stride - 2*padding + kernel + outputPadding
	if len(out) != outC*outT || len(x) != inC*inT || len(weight) != inC*(outC/groups)*kernel {
		return fmt.Errorf("kokoro: convtranspose1d shape mismatch")
	}
	if useKokoroSIMD() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		return convTranspose1DSIMD(out, x, weight, bias, inC, inT, outC, kernel, stride, padding, outT)
	}
	if outC*outT*(inC/groups)*kernel > 200000 && groups == 1 {
		return convTranspose1DParallel(out, x, weight, bias, inC, inT, outC, kernel, stride, padding, outputPadding, groups, outT)
	}
	for i := range out {
		out[i] = 0
	}
	for ic := 0; ic < inC; ic++ {
		g := ic / (inC / groups)
		outStart := g * (outC / groups)
		for it := 0; it < inT; it++ {
			v := x[ic*inT+it]
			for ocg := 0; ocg < outC/groups; ocg++ {
				oc := outStart + ocg
				for k := 0; k < kernel; k++ {
					ot := it*stride + k - padding
					if ot < 0 || ot >= outT {
						continue
					}
					out[oc*outT+ot] += v * weight[(ic*(outC/groups)+ocg)*kernel+k]
				}
			}
		}
	}
	if bias != nil {
		for oc := 0; oc < outC; oc++ {
			for ot := 0; ot < outT; ot++ {
				out[oc*outT+ot] += bias[oc]
			}
		}
	}
	return nil
}

func WeightNormConv1DWeight(out, v, g []float32, outC, inCPerGroup, kernel int) error {
	if len(out) != len(v) || len(v) != outC*inCPerGroup*kernel || len(g) < outC {
		return fmt.Errorf("kokoro: weightnorm shape mismatch")
	}
	for oc := 0; oc < outC; oc++ {
		base := oc * inCPerGroup * kernel
		row := v[base : base+inCPerGroup*kernel]
		norm := dot32(row, row)
		scale := g[oc] / float32(math.Sqrt(float64(norm+1e-12)))
		mulNumberInto32(out[base:base+inCPerGroup*kernel], row, scale)
	}
	return nil
}

func WeightNormConv1DWeightTransposed(out, v, g []float32, outC, inCPerGroup, kernel int) error {
	if len(out) != len(v) || len(v) != outC*inCPerGroup*kernel || len(g) < outC {
		return fmt.Errorf("kokoro: weightnorm transposed shape mismatch")
	}
	for oc := 0; oc < outC; oc++ {
		base := oc * inCPerGroup * kernel
		row := v[base : base+inCPerGroup*kernel]
		norm := dot32(row, row)
		scale := g[oc] / float32(math.Sqrt(float64(norm+1e-12)))
		for ic := 0; ic < inCPerGroup; ic++ {
			for k := 0; k < kernel; k++ {
				out[(oc*kernel+k)*inCPerGroup+ic] = v[base+ic*kernel+k] * scale
			}
		}
	}
	return nil
}
func conv1DSIMD(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, dilation, outT int) error {
	weightT := make([]float32, outC*kernel*inC)
	transposeConv1DWeight(weightT, weight, inC, outC, kernel)
	return conv1DSIMDTransposedWeight(out, x, weightT, bias, nil, inC, inT, outC, kernel, stride, padding, dilation, outT)
}

func transposeConv1DWeight(weightT, weight []float32, inC, outC, kernel int) {
	for oc := 0; oc < outC; oc++ {
		for ic := 0; ic < inC; ic++ {
			src := weight[(oc*inC+ic)*kernel : (oc*inC+ic+1)*kernel]
			for k, v := range src {
				weightT[(oc*kernel+k)*inC+ic] = v
			}
		}
	}
}

func conv1DSIMDTransposedWeight(out, x, weightT, bias, residual []float32, inC, inT, outC, kernel, stride, padding, dilation, outT int) error {
	// For the fused stride-1 path, surround the time-transposed input with a
	// zero halo of `padding` rows on both sides. Interior reads then satisfy
	// 0 <= it+padRows and it+padRows+(kernel-1)*dilation < inT+2*padRows for
	// every output position, so edges flow through the same blocked kernels as
	// the interior instead of a separate per-tap checked dot.
	padRows := 0
	if useKokoroStridedFMA() && padding > 0 {
		padRows = padding
	}
	inputT := getConvScratch((inT + 2*padRows) * inC)
	defer putConvScratch(inputT)
	if padRows > 0 {
		zeroFloat32s(inputT[:padRows*inC])
		zeroFloat32s(inputT[(padRows+inT)*inC:])
	}
	transposeChannelTime(inputT[padRows*inC:], x, inC, inT)
	dilInC := dilation * inC
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	// Split the time axis instead of the channel axis when the input is the
	// larger stream: channel-split re-reads the whole input once per channel
	// quad (outC/4 passes), while time-split streams the small weight set per
	// worker and reads each input byte exactly once.
	xBytes := int64(inT) * int64(inC) * 4
	wBytes := int64(outC) * int64(kernel) * int64(inC) * 4
	timeSplit := wBytes*int64(workers) < xBytes*int64(outC/4)
	var chunk int
	if timeSplit {
		if workers > outT/32 {
			workers = outT / 32
		}
		if workers < 1 {
			workers = 1
		}
		chunk = (outT + workers - 1) / workers
	} else {
		if workers > outC {
			workers = outC
		}
		// Align worker ranges to groups of 4 output channels so the blocked
		// kernels below never straddle a goroutine boundary.
		chunk = ((outC+workers-1)/workers + 3) &^ 3
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		ocStart, ocEnd, otStart, otEnd := 0, outC, 0, outT
		if timeSplit {
			otStart = w * chunk
			otEnd = otStart + chunk
			if otEnd > outT {
				otEnd = outT
			}
		} else {
			ocStart = w * chunk
			ocEnd = ocStart + chunk
			if ocEnd > outC {
				ocEnd = outC
			}
		}
		if ocStart >= ocEnd || otStart >= otEnd {
			break
		}
		wg.Add(1)
		go func(ocStart, ocEnd, otStart, otEnd int) {
			defer wg.Done()
			if useKokoroStridedFMA() {
				// Accumulate all kernel taps in one FMA pass per output, four
				// output channels at a time. The zero halo on inputT lets every
				// position, edges included, flow through these blocked kernels.
				// The time axis is tiled so each tile's x window stays resident
				// in this core's L2 while every channel quad of the worker
				// passes over it.
				winRows := (kernel-1)*dilation + 1
				tileRows := 512*1024/(inC*4) - winRows
				if tileRows < 8 {
					tileRows = 8
				}
				// Halo row index for output ot: it+padRows = ot*stride+xPadOff.
				xPadOff := padRows - padding
				ocQuadsEnd := ocStart + (ocEnd-ocStart)/4*4
				for qg := ocStart; qg < ocQuadsEnd; qg += 16 {
					qgEnd := qg + 16
					if qgEnd > ocQuadsEnd {
						qgEnd = ocQuadsEnd
					}
					for t0 := otStart; t0 < otEnd; t0 += tileRows {
						t1 := t0 + tileRows
						if t1 > otEnd {
							t1 = otEnd
						}
						for oc := qg; oc < qgEnd; oc += 4 {
							var b0, b1, b2, b3 float32
							if bias != nil {
								b0, b1, b2, b3 = bias[oc], bias[oc+1], bias[oc+2], bias[oc+3]
							}
							outRow0 := out[oc*outT : (oc+1)*outT]
							outRow1 := out[(oc+1)*outT : (oc+2)*outT]
							outRow2 := out[(oc+2)*outT : (oc+3)*outT]
							outRow3 := out[(oc+3)*outT : (oc+4)*outT]
							var res0, res1, res2, res3 []float32
							if residual != nil {
								res0 = residual[oc*outT : (oc+1)*outT]
								res1 = residual[(oc+1)*outT : (oc+2)*outT]
								res2 = residual[(oc+2)*outT : (oc+3)*outT]
								res3 = residual[(oc+3)*outT : (oc+4)*outT]
							}
							weightBase := oc * kernel * inC
							weightQuad := weightT[weightBase:]
							ot := t0
							if stride == 1 {
								for ; ot+1 < t1; ot += 2 {
									xRow := inputT[(ot+xPadOff)*inC:]
									r0, r1, r2, r3, r4, r5, r6, r7 := dotStridedFMA4x2(xRow, dilInC, weightQuad, inC, kernel*inC, kernel, inC)
									if residual != nil {
										r0 += res0[ot]
										r1 += res0[ot+1]
										r2 += res1[ot]
										r3 += res1[ot+1]
										r4 += res2[ot]
										r5 += res2[ot+1]
										r6 += res3[ot]
										r7 += res3[ot+1]
									}
									outRow0[ot] = b0 + r0
									outRow0[ot+1] = b0 + r1
									outRow1[ot] = b1 + r2
									outRow1[ot+1] = b1 + r3
									outRow2[ot] = b2 + r4
									outRow2[ot+1] = b2 + r5
									outRow3[ot] = b3 + r6
									outRow3[ot+1] = b3 + r7
								}
							} else {
								// stride>1: adjacent outputs sit `stride` rows
								// apart; the generalized kernel handles the
								// position offset and stores directly.
								var bias4 []float32
								if bias != nil {
									bias4 = bias[oc : oc+4]
								}
								for ; ot+1 < t1; ot += 2 {
									var res4 []float32
									if residual != nil {
										res4 = residual[oc*outT+ot:]
									}
									conv4x2G(inputT[(ot*stride+xPadOff)*inC:], dilInC, weightQuad, inC, kernel*inC, kernel, inC, stride*inC,
										out[oc*outT+ot:], outT, 1, bias4, res4)
								}
							}
							for ; ot < t1; ot++ {
								xRow := inputT[(ot*stride+xPadOff)*inC:]
								s0 := dotStridedFMA(xRow, dilInC, weightQuad, inC, kernel, inC)
								s1 := dotStridedFMA(xRow, dilInC, weightT[weightBase+kernel*inC:], inC, kernel, inC)
								s2 := dotStridedFMA(xRow, dilInC, weightT[weightBase+2*kernel*inC:], inC, kernel, inC)
								s3 := dotStridedFMA(xRow, dilInC, weightT[weightBase+3*kernel*inC:], inC, kernel, inC)
								if residual != nil {
									s0 += res0[ot]
									s1 += res1[ot]
									s2 += res2[ot]
									s3 += res3[ot]
								}
								outRow0[ot] = b0 + s0
								outRow1[ot] = b1 + s1
								outRow2[ot] = b2 + s2
								outRow3[ot] = b3 + s3
							}
						}
					}
				}
				// Remaining channels (fewer than 4): two outputs share
				// each weight load.
				for t0 := otStart; t0 < otEnd; t0 += tileRows {
					t1 := t0 + tileRows
					if t1 > otEnd {
						t1 = otEnd
					}
					for oc := ocQuadsEnd; oc < ocEnd; oc++ {
						b := float32(0)
						if bias != nil {
							b = bias[oc]
						}
						outRow := out[oc*outT : (oc+1)*outT]
						weightBase := oc * kernel * inC
						weightRow := weightT[weightBase : weightBase+kernel*inC]
						var resRow []float32
						if residual != nil {
							resRow = residual[oc*outT : (oc+1)*outT]
						}
						ot := t0
						if stride == 1 {
							for ; ot+1 < t1; ot += 2 {
								r0, r1 := dotStridedFMA2(inputT[(ot+xPadOff)*inC:], dilInC, weightRow, inC, kernel, inC)
								if residual != nil {
									r0 += resRow[ot]
									r1 += resRow[ot+1]
								}
								outRow[ot] = b + r0
								outRow[ot+1] = b + r1
							}
						}
						for ; ot < t1; ot++ {
							s := dotStridedFMA(inputT[(ot*stride+xPadOff)*inC:], dilInC, weightRow, inC, kernel, inC)
							if residual != nil {
								s += resRow[ot]
							}
							outRow[ot] = b + s
						}
					}
				}
				return
			}
			if os.Getenv("KOKORO_DBG_GENERIC") != "" {
				genericDbgOnce.Do(func() {
					genericDbgSeen = map[string]bool{}
				})
				key := fmt.Sprintf("conv1D fallback stride=%d k=%d inC=%d outC=%d inT=%d outT=%d dil=%d", stride, kernel, inC, outC, inT, outT, dilation)
				genericDbgMu.Lock()
				if !genericDbgSeen[key] {
					genericDbgSeen[key] = true
					println("GENERIC:", key)
				}
				genericDbgMu.Unlock()
			}
			for oc := ocStart; oc < ocEnd; oc++ {
				b := float32(0)
				if bias != nil {
					b = bias[oc]
				}
				outRow := out[oc*outT : (oc+1)*outT]
				weightBase := oc * kernel * inC
				for ot := otStart; ot < otEnd; ot++ {
					outRow[ot] = conv1DSIMDGenericDot(inputT, weightT, b, inC, inT, kernel, stride, padding, dilation, weightBase, ot)
				}
			}
		}(ocStart, ocEnd, otStart, otEnd)
	}
	wg.Wait()
	return nil
}

func conv1DSIMDEdgeDot(inputT, weightT []float32, bias float32, inC, inT, kernel, padding, weightBase, ot int) float32 {
	sum := bias
	for k := 0; k < kernel; k++ {
		it := ot + k - padding
		if it < 0 || it >= inT {
			continue
		}
		sum += dot32(inputT[it*inC:(it+1)*inC], weightT[weightBase+k*inC:weightBase+(k+1)*inC])
	}
	return sum
}

func conv1DSIMDGenericDot(inputT, weightT []float32, bias float32, inC, inT, kernel, stride, padding, dilation, weightBase, ot int) float32 {
	sum := bias
	for k := 0; k < kernel; k++ {
		it := ot*stride + k*dilation - padding
		if it < 0 || it >= inT {
			continue
		}
		sum += dot32(inputT[it*inC:(it+1)*inC], weightT[weightBase+k*inC:weightBase+(k+1)*inC])
	}
	return sum
}
func conv1DParallel(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, dilation, groups, outT int) error {
	workers := runtime.GOMAXPROCS(0)
	if workers > outC {
		workers = outC
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	chunk := (outC + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > outC {
			end = outC
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(ocStart, ocEnd int) {
			defer wg.Done()
			for oc := ocStart; oc < ocEnd; oc++ {
				g := oc / (outC / groups)
				inStart := g * (inC / groups)
				for ot := 0; ot < outT; ot++ {
					sum := float32(0)
					if bias != nil {
						sum = bias[oc]
					}
					for icg := 0; icg < inC/groups; icg++ {
						ic := inStart + icg
						for k := 0; k < kernel; k++ {
							it := ot*stride + k*dilation - padding
							if it < 0 || it >= inT {
								continue
							}
							sum += x[ic*inT+it] * weight[(oc*(inC/groups)+icg)*kernel+k]
						}
					}
					out[oc*outT+ot] = sum
				}
			}
		}(start, end)
	}
	wg.Wait()
	return nil
}

func convTranspose1DSIMD(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, outT int) error {
	inputT := getConvScratch(inT * inC)
	defer putConvScratch(inputT)
	transposeChannelTime(inputT, x, inC, inT)
	weightT := make([]float32, kernel*outC*inC)
	transposeConvTranspose1DWeight(weightT, weight, inC, outC, kernel)
	workers := runtime.GOMAXPROCS(0)
	if workers > outC {
		workers = outC
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	chunk := (outC + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > outC {
			end = outC
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(ocStart, ocEnd int) {
			defer wg.Done()
			for oc := ocStart; oc < ocEnd; oc++ {
				b := float32(0)
				if bias != nil {
					b = bias[oc]
				}
				outRow := out[oc*outT : (oc+1)*outT]
				for ot := 0; ot < outT; ot++ {
					sum := b
					for k := 0; k < kernel; k++ {
						itNumer := ot + padding - k
						if itNumer < 0 || itNumer%stride != 0 {
							continue
						}
						it := itNumer / stride
						if it < 0 || it >= inT {
							continue
						}
						sum += dot32(inputT[it*inC:(it+1)*inC], weightT[(k*outC+oc)*inC:(k*outC+oc+1)*inC])
					}
					outRow[ot] = sum
				}
			}
		}(start, end)
	}
	wg.Wait()
	return nil
}

func transposeConvTranspose1DWeight(weightT, weight []float32, inC, outC, kernel int) {
	for ic := 0; ic < inC; ic++ {
		for oc := 0; oc < outC; oc++ {
			src := weight[(ic*outC+oc)*kernel : (ic*outC+oc+1)*kernel]
			for k, v := range src {
				weightT[(k*outC+oc)*inC+ic] = v
			}
		}
	}
}

func convTranspose1DSIMDTransposedWeight(out, x, weightT, bias []float32, inC, inT, outC, kernel, stride, padding, outT int) error {
	// For the fused path, surround the time-transposed input with a zero halo
	// of H rows on both sides. Taps whose input position falls outside
	// [0, inT) then read zeros and contribute nothing, so every output
	// position evaluates exactly ktab[residue] taps with no clamping logic,
	// and positions `stride` apart (same residue class) share their weight
	// walk and can be evaluated as a pair.
	halo := 0
	if useKokoroStridedFMA() {
		halo = (kernel + stride) / stride
	}
	inputT := getConvScratch((inT + 2*halo) * inC)
	defer putConvScratch(inputT)
	if halo > 0 {
		zeroFloat32s(inputT[:halo*inC])
		zeroFloat32s(inputT[(halo+inT)*inC:])
	}
	transposeChannelTime(inputT[halo*inC:], x, inC, inT)
	workers := runtime.GOMAXPROCS(0)
	if workers > outC {
		workers = outC
	}
	if workers < 1 {
		workers = 1
	}
	// Align worker ranges to groups of 4 output channels so the blocked
	// kernel below never straddles a goroutine boundary.
	chunk := ((outC+workers-1)/workers + 3) &^ 3
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > outC {
			end = outC
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(ocStart, ocEnd int) {
			defer wg.Done()
			if useKokoroStridedFMA() {
				// Taps per position depend only on the residue class of
				// ot+padding mod stride; precompute them once.
				ktab := make([]int, stride)
				for k0 := 0; k0 < stride; k0++ {
					ktab[k0] = (kernel-1-k0)/stride + 1
				}
				wStride := -stride * outC * inC
				ocQuadsEnd := ocStart + (ocEnd-ocStart)/4*4
				for oc := ocStart; oc < ocQuadsEnd; oc += 4 {
					var bias4 []float32
					if bias != nil {
						bias4 = bias[oc : oc+4]
					}
					weightBase := (oc * inC)
					// Pair positions (ot, ot+stride): they share the
					// residue class, weights and tap count. Within each
					// residue class r (mod stride) the pair starts are
					// r, r+2*stride, ... — equivalently ot where
					// ot mod 2*stride < stride.
					ot := 0
					for otb := 0; otb+stride < outT; otb += 2 * stride {
						lim := otb + stride
						if lim > outT {
							lim = outT
						}
						for oti := otb; oti < lim; oti++ {
							ot := oti
							base := ot + padding
							k0 := base % stride
							count := ktab[k0]
							it0 := (base - k0) / stride
							kMax := k0 + (count-1)*stride
							// Walk taps from the largest k (lowest x address) up
							// so the x stride is positive; the negative weight
							// stride is handled directly by the kernel.
							conv4x2G(
								inputT[(it0-(count-1)+halo)*inC:], inC,
								weightT[(kMax*outC)*inC+weightBase:], wStride, inC,
								count, inC, inC,
								out[oc*outT+ot:], outT, stride, bias4, nil)
						}
					}
					for ; ot < outT; ot++ {
						base := ot + padding
						k0 := base % stride
						count := ktab[k0]
						it0 := (base - k0) / stride
						kMax := k0 + (count-1)*stride
						r0, r1, r2, r3 := dotStridedFMA4(
							inputT[(it0-(count-1)+halo)*inC:], inC,
							weightT[(kMax*outC)*inC+weightBase:], wStride, inC,
							count, inC)
						out[oc*outT+ot] = biasOrZero(bias4, 0) + r0
						out[(oc+1)*outT+ot] = biasOrZero(bias4, 1) + r1
						out[(oc+2)*outT+ot] = biasOrZero(bias4, 2) + r2
						out[(oc+3)*outT+ot] = biasOrZero(bias4, 3) + r3
					}
				}
				for oc := ocQuadsEnd; oc < ocEnd; oc++ {
					var bias1 []float32
					if bias != nil {
						bias1 = bias[oc : oc+1]
					}
					for ot := 0; ot < outT; ot++ {
						base := ot + padding
						k0 := base % stride
						count := ktab[k0]
						it0 := (base - k0) / stride
						kMax := k0 + (count-1)*stride
						out[oc*outT+ot] = biasOrZero(bias1, 0) + dotStridedFMA(
							inputT[(it0-(count-1)+halo)*inC:], inC,
							weightT[(kMax*outC+oc)*inC:], wStride,
							count, inC)
					}
				}
				return
			}
			for oc := ocStart; oc < ocEnd; oc++ {
				b := float32(0)
				if bias != nil {
					b = bias[oc]
				}
				outRow := out[oc*outT : (oc+1)*outT]
				for ot := 0; ot < outT; ot++ {
					sum := b
					for k := 0; k < kernel; k++ {
						itNumer := ot + padding - k
						if itNumer < 0 || itNumer%stride != 0 {
							continue
						}
						it := itNumer / stride
						if it < 0 || it >= inT {
							continue
						}
						sum += dot32(inputT[it*inC:(it+1)*inC], weightT[(k*outC+oc)*inC:(k*outC+oc+1)*inC])
					}
					outRow[ot] = sum
				}
			}
		}(start, end)
	}
	wg.Wait()
	return nil
}

func biasOrZero(bias []float32, i int) float32 {
	if bias == nil {
		return 0
	}
	return bias[i]
}

func convTranspose1DParallel(out, x, weight, bias []float32, inC, inT, outC, kernel, stride, padding, outputPadding, groups, outT int) error {
	workers := runtime.GOMAXPROCS(0)
	if workers > outC {
		workers = outC
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	chunk := (outC + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > outC {
			end = outC
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(ocStart, ocEnd int) {
			defer wg.Done()
			for oc := ocStart; oc < ocEnd; oc++ {
				if bias != nil {
					for ot := 0; ot < outT; ot++ {
						out[oc*outT+ot] = bias[oc]
					}
				} else {
					for ot := 0; ot < outT; ot++ {
						out[oc*outT+ot] = 0
					}
				}
				for ic := 0; ic < inC; ic++ {
					for it := 0; it < inT; it++ {
						v := x[ic*inT+it]
						for k := 0; k < kernel; k++ {
							ot := it*stride + k - padding
							if ot < 0 || ot >= outT {
								continue
							}
							out[oc*outT+ot] += v * weight[(ic*outC+oc)*kernel+k]
						}
					}
				}
			}
		}(start, end)
	}
	wg.Wait()
	return nil
}
