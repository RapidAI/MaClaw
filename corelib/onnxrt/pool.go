package onnxrt

import (
	"fmt"
	"math"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// poolParams holds 2D pooling hyper-parameters.
type poolParams struct {
	kernel  [2]int
	strides [2]int
	pads    [4]int
	ceil    bool
}

func parsePoolParams(n *Node) (*poolParams, error) {
	p := &poolParams{strides: [2]int{1, 1}}
	k := attrInts(n, "kernel_shape")
	if len(k) != 2 {
		return nil, fmt.Errorf("pool: kernel_shape len %d", len(k))
	}
	p.kernel = [2]int{int(k[0]), int(k[1])}
	if s := attrInts(n, "strides"); s != nil {
		if len(s) != 2 {
			return nil, fmt.Errorf("pool: strides len %d", len(s))
		}
		p.strides = [2]int{int(s[0]), int(s[1])}
	}
	if pd := attrInts(n, "pads"); pd != nil {
		if len(pd) != 4 {
			return nil, fmt.Errorf("pool: pads len %d", len(pd))
		}
		for i := range p.pads {
			p.pads[i] = int(pd[i])
		}
	}
	if d := attrInts(n, "dilations"); d != nil {
		if len(d) != 2 {
			return nil, fmt.Errorf("pool: dilations len %d", len(d))
		}
		if d[0] != 1 || d[1] != 1 {
			// Not implemented; error instead of silently ignoring them.
			return nil, fmt.Errorf("pool: dilations %v not supported", d)
		}
	}
	p.ceil = attrInt(n, "ceil_mode", 0) != 0
	return p, nil
}

// poolOutputSize computes one spatial output dim with ceil/floor mode.
func poolOutputSize(in, k, stride, padB, padE int, ceilMode bool) int {
	num := in + padB + padE - k
	var out int
	if ceilMode {
		out = (num+stride-1)/stride + 1
	} else {
		out = num/stride + 1
	}
	// ensure the last window starts inside the (padded) input
	if (out-1)*stride >= in+padB {
		out--
	}
	if out < 1 {
		out = 1
	}
	return out
}

func opMaxPool(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return pool2D(rc, n, args, true)
}

func opAveragePool(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return pool2D(rc, n, args, false)
}

func pool2D(rc *runCtx, n *Node, args []*Tensor, isMax bool) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	if x.Rank() != 4 {
		return nil, fmt.Errorf("pool: only 4D supported")
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	p, err := parsePoolParams(n)
	if err != nil {
		return nil, err
	}
	autoPad := attrStr(n, "auto_pad", "NOTSET")
	if autoPad == "SAME_UPPER" || autoPad == "SAME_LOWER" {
		p.pads[0], p.pads[2] = convAutoPads(autoPad, x.Shape[2], p.kernel[0], p.strides[0], 1)
		p.pads[1], p.pads[3] = convAutoPads(autoPad, x.Shape[3], p.kernel[1], p.strides[1], 1)
	} else if autoPad == "VALID" {
		p.pads = [4]int{}
	}
	countIncludePad := attrInt(n, "count_include_pad", 0) != 0

	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oH := poolOutputSize(H, p.kernel[0], p.strides[0], p.pads[0], p.pads[2], p.ceil)
	oW := poolOutputSize(W, p.kernel[1], p.strides[1], p.pads[1], p.pads[3], p.ceil)
	out := rc.newFloat(n, 0, N, C, oH, oW)

	if isMax && p.strides[1] == 1 {
		maxPoolRowVec(out.F32, xf, N, C, H, W, oH, oW, p)
		return []*Tensor{out}, nil
	}

	xt.ParallelRanges(N*C, func(start, end int) {
		for nc := start; nc < end; nc++ {
			nI, c := nc/C, nc%C
			xBase := (nI*C + c) * H * W
			outBase := (nI*C + c) * oH * oW
			for oh := 0; oh < oH; oh++ {
				for ow := 0; ow < oW; ow++ {
					h0 := oh*p.strides[0] - p.pads[0]
					w0 := ow*p.strides[1] - p.pads[1]
					h1 := h0 + p.kernel[0]
					w1 := w0 + p.kernel[1]
					ih0, iw0 := h0, w0
					ih1, iw1 := h1, w1
					if ih0 < 0 {
						ih0 = 0
					}
					if iw0 < 0 {
						iw0 = 0
					}
					if ih1 > H {
						ih1 = H
					}
					if iw1 > W {
						iw1 = W
					}
					if isMax {
						best := float32(math.Inf(-1))
						for ih := ih0; ih < ih1; ih++ {
							for iw := iw0; iw < iw1; iw++ {
								v := xf[xBase+ih*W+iw]
								if v > best {
									best = v
								}
							}
						}
						out.F32[outBase+oh*oW+ow] = best
					} else {
						var sum float32
						for ih := ih0; ih < ih1; ih++ {
							for iw := iw0; iw < iw1; iw++ {
								sum += xf[xBase+ih*W+iw]
							}
						}
						cnt := (ih1 - ih0) * (iw1 - iw0)
						if countIncludePad {
							cnt = p.kernel[0] * p.kernel[1]
						}
						out.F32[outBase+oh*oW+ow] = sum / float32(cnt)
					}
				}
			}
		}
	})
	return []*Tensor{out}, nil
}

// maxPoolRowVec vectorizes max pooling along output rows: each row starts
// at -Inf and accumulates elementwise maxima over each kernel tap's
// (clipped) contiguous segment. Requires horizontal stride 1.
func maxPoolRowVec(out, x []float32, N, C, H, W, oH, oW int, p *poolParams) {
	negInf := float32(math.Inf(-1))
	xt.ParallelRanges(N*C, func(start, end int) {
		for nc := start; nc < end; nc++ {
			nI, c := nc/C, nc%C
			xBase := (nI*C + c) * H * W
			outBase := (nI*C + c) * oH * oW
			for oh := 0; oh < oH; oh++ {
				acc := out[outBase+oh*oW : outBase+oh*oW+oW]
				for i := range acc {
					acc[i] = negInf
				}
				for kh := 0; kh < p.kernel[0]; kh++ {
					ih := oh*p.strides[0] - p.pads[0] + kh
					if ih < 0 || ih >= H {
						continue
					}
					row := x[xBase+ih*W:]
					for kw := 0; kw < p.kernel[1]; kw++ {
						s0 := kw - p.pads[1]
						dst0 := 0
						n := oW
						if s0 < 0 {
							dst0 = -s0
							n += s0
							s0 = 0
						}
						if s0+n > W {
							n = W - s0
						}
						if n <= 0 {
							continue
						}
						vek32.Maximum_Inplace(acc[dst0:dst0+n], row[s0:s0+n])
					}
				}
			}
		}
	})
}

func opGlobalAveragePool(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	if x.Rank() < 3 {
		return nil, fmt.Errorf("globalaveragepool: rank %d", x.Rank())
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	N, C := x.Shape[0], x.Shape[1]
	spatial := numElements(x.Shape[2:])
	outShape := []int{N, C}
	for range x.Shape[2:] {
		outShape = append(outShape, 1)
	}
	out := rc.newFloat(n, 0, outShape...)
	xt.ParallelRanges(N*C, func(start, end int) {
		for nc := start; nc < end; nc++ {
			base := nc * spatial
			var sum float64
			for i := 0; i < spatial; i++ {
				sum += float64(xf[base+i])
			}
			out.F32[nc] = float32(sum / float64(spatial))
		}
	})
	return []*Tensor{out}, nil
}

func opReduceMean(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	rank := x.Rank()
	// axes from attr (opset <= 12) or second input (opset 13+ / 18).
	var axesList []int64
	hasAxes := false
	if ax := attrInts(n, "axes"); ax != nil {
		axesList, hasAxes = ax, true
	} else if len(args) > 1 && args[1] != nil {
		ax, err := tensorIntsArg(args[1])
		if err != nil {
			return nil, err
		}
		axesList, hasAxes = ax, true
	}
	keepdims := attrInt(n, "keepdims", 1) != 0

	reduce := make([]bool, rank)
	if !hasAxes {
		for d := range reduce {
			reduce[d] = true
		}
	} else {
		for _, a := range axesList {
			na, err := normalizeAxis(a, rank)
			if err != nil {
				return nil, err
			}
			reduce[na] = true
		}
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}

	// Compute output shape (kept dims set to 1 first, removed after).
	keptShape := make([]int, rank)
	var outShape []int
	for d := 0; d < rank; d++ {
		if reduce[d] {
			keptShape[d] = 1
		} else {
			keptShape[d] = x.Shape[d]
		}
		if !reduce[d] || keepdims {
			outShape = append(outShape, keptShape[d])
		}
	}

	// Fast path: the reduced axes form a contiguous suffix (e.g. [2,3] on
	// NCHW feature maps or [-1] layer-norm rows), so every output element is
	// the mean of a contiguous run of `inner` floats — vectorized sum instead
	// of the generic per-element index walk.
	k := 0
	for k < rank && reduce[rank-1-k] {
		k++
	}
	suffixReduce := k > 0
	for d := 0; d < rank-k; d++ {
		if reduce[d] {
			suffixReduce = false
			break
		}
	}
	if suffixReduce {
		inner := numElements(x.Shape[rank-k:])
		outer := numElements(x.Shape[:rank-k])
		if inner > 0 && outer*inner == len(xf) {
			out := rc.newFloat(n, 0, keptShape...)
			rowFn := func(o0, o1 int) {
				for o := o0; o < o1; o++ {
					out.F32[o] = vek32.Sum(xf[o*inner:(o+1)*inner]) / float32(inner)
				}
			}
			if outer >= 4 && outer*inner >= parThreshold {
				xt.ParallelRanges(outer, rowFn)
			} else {
				rowFn(0, outer)
			}
			if !shapeEqual(keptShape, outShape) {
				out = out.Reshape(outShape...)
			}
			return []*Tensor{out}, nil
		}
	}

	// The accumulation below relies on a zeroed output; arena checkouts for
	// this node are cleared (arenaPlan.needsZero), heap fallbacks are zeroed.
	out := rc.newFloat(n, 0, keptShape...)

	// Accumulate: iterate source, map to kept index.
	srcStrides := make([]int, rank)
	acc := 1
	for d := rank - 1; d >= 0; d-- {
		srcStrides[d] = acc
		acc *= x.Shape[d]
	}
	keptStrides := make([]int, rank)
	acc = 1
	for d := rank - 1; d >= 0; d-- {
		keptStrides[d] = acc
		acc *= keptShape[d]
	}
	counts := make([]int, out.NumElements())
	idx := make([]int, rank)
	off := 0
	nSrc := x.NumElements()
	for si := 0; si < nSrc; si++ {
		out.F32[off] += xf[si]
		counts[off]++
		for d := rank - 1; d >= 0; d-- {
			idx[d]++
			if !reduce[d] {
				off += keptStrides[d]
			}
			if idx[d] < x.Shape[d] {
				break
			}
			idx[d] = 0
			if !reduce[d] {
				off -= keptStrides[d] * x.Shape[d]
			}
		}
	}
	for i := range out.F32 {
		if counts[i] > 0 {
			out.F32[i] /= float32(counts[i])
		}
	}
	if !shapeEqual(keptShape, outShape) {
		out = out.Reshape(outShape...)
	}
	return []*Tensor{out}, nil
}
