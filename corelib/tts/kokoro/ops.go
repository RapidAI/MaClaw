package kokoro

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

var float32BufferPool sync.Pool

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

func zeroFloat32s(buf []float32) {
	for i := range buf {
		buf[i] = 0
	}
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
	inputT := make([]float32, inT*inC)
	for ic := 0; ic < inC; ic++ {
		src := x[ic*inT : (ic+1)*inT]
		for t, v := range src {
			inputT[t*inC+ic] = v
		}
	}
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
	inputT := make([]float32, inT*inC)
	for ic := 0; ic < inC; ic++ {
		src := x[ic*inT : (ic+1)*inT]
		for t, v := range src {
			inputT[t*inC+ic] = v
		}
	}
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
	return conv1DSIMDTransposedWeight(out, x, weightT, bias, inC, inT, outC, kernel, stride, padding, dilation, outT)
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

func conv1DSIMDTransposedWeight(out, x, weightT, bias []float32, inC, inT, outC, kernel, stride, padding, dilation, outT int) error {
	inputT := make([]float32, inT*inC)
	for ic := 0; ic < inC; ic++ {
		src := x[ic*inT : (ic+1)*inT]
		for t, v := range src {
			inputT[t*inC+ic] = v
		}
	}
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
				weightBase := oc * kernel * inC
				if stride == 1 && dilation == 1 {
					fullStart := padding
					if fullStart < 0 {
						fullStart = 0
					}
					fullEnd := inT + padding - kernel + 1
					if fullEnd > outT {
						fullEnd = outT
					}
					if fullEnd < fullStart {
						fullEnd = fullStart
					}
					for ot := 0; ot < fullStart; ot++ {
						outRow[ot] = conv1DSIMDEdgeDot(inputT, weightT, b, inC, inT, kernel, padding, weightBase, ot)
					}
					weightRow := weightT[weightBase : weightBase+kernel*inC]
					for ot := fullStart; ot < fullEnd; ot++ {
						it := ot - padding
						outRow[ot] = b + dot32(inputT[it*inC:(it+kernel)*inC], weightRow)
					}
					for ot := fullEnd; ot < outT; ot++ {
						outRow[ot] = conv1DSIMDEdgeDot(inputT, weightT, b, inC, inT, kernel, padding, weightBase, ot)
					}
					continue
				}
				if useKokoroFusedConvASM() && stride == 1 && kernel == 3 {
					fullStart := padding
					if fullStart < 0 {
						fullStart = 0
					}
					fullEnd := inT + padding - (kernel-1)*dilation
					if fullEnd > outT {
						fullEnd = outT
					}
					if fullEnd < fullStart {
						fullEnd = fullStart
					}
					for ot := 0; ot < fullStart; ot++ {
						outRow[ot] = conv1DSIMDGenericDot(inputT, weightT, b, inC, inT, kernel, stride, padding, dilation, weightBase, ot)
					}
					w0 := weightT[weightBase : weightBase+inC]
					w1 := weightT[weightBase+inC : weightBase+2*inC]
					w2 := weightT[weightBase+2*inC : weightBase+3*inC]
					for ot := fullStart; ot < fullEnd; ot++ {
						it := ot - padding
						outRow[ot] = b + dot3Fused(
							inputT[it*inC:(it+1)*inC],
							inputT[(it+dilation)*inC:(it+dilation+1)*inC],
							inputT[(it+2*dilation)*inC:(it+2*dilation+1)*inC],
							w0,
							w1,
							w2,
						)
					}
					for ot := fullEnd; ot < outT; ot++ {
						outRow[ot] = conv1DSIMDGenericDot(inputT, weightT, b, inC, inT, kernel, stride, padding, dilation, weightBase, ot)
					}
					continue
				}
				for ot := 0; ot < outT; ot++ {
					outRow[ot] = conv1DSIMDGenericDot(inputT, weightT, b, inC, inT, kernel, stride, padding, dilation, weightBase, ot)
				}
			}
		}(start, end)
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
	inputT := make([]float32, inT*inC)
	for ic := 0; ic < inC; ic++ {
		src := x[ic*inT : (ic+1)*inT]
		for t, v := range src {
			inputT[t*inC+ic] = v
		}
	}
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
	inputT := make([]float32, inT*inC)
	for ic := 0; ic < inC; ic++ {
		src := x[ic*inT : (ic+1)*inT]
		for t, v := range src {
			inputT[t*inC+ic] = v
		}
	}
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
