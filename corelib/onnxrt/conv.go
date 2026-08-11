package onnxrt

import (
	"fmt"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// convParams holds the common 2D convolution hyper-parameters.
type convParams struct {
	strides   [2]int
	pads      [4]int // top, left, bottom, right
	dilations [2]int
	group     int
	kH, kW    int
}

// parseConvParams reads shared conv attrs. kernel shape defaults come from
// the weight tensor (caller passes it).
func parseConvParams(n *Node, wShape []int, isTranspose bool) (*convParams, error) {
	p := &convParams{
		strides:   [2]int{1, 1},
		dilations: [2]int{1, 1},
		group:     1,
		kH:        wShape[2],
		kW:        wShape[3],
	}
	if s := attrInts(n, "strides"); s != nil {
		if len(s) != 2 {
			return nil, fmt.Errorf("strides len %d", len(s))
		}
		p.strides = [2]int{int(s[0]), int(s[1])}
	}
	if d := attrInts(n, "dilations"); d != nil {
		if len(d) != 2 {
			return nil, fmt.Errorf("dilations len %d", len(d))
		}
		p.dilations = [2]int{int(d[0]), int(d[1])}
	}
	if pd := attrInts(n, "pads"); pd != nil {
		if len(pd) != 4 {
			return nil, fmt.Errorf("pads len %d", len(pd))
		}
		for i := range p.pads {
			p.pads[i] = int(pd[i])
		}
	}
	p.group = int(attrInt(n, "group", 1))
	if k := attrInts(n, "kernel_shape"); k != nil {
		if len(k) != 2 {
			return nil, fmt.Errorf("kernel_shape len %d", len(k))
		}
		p.kH, p.kW = int(k[0]), int(k[1])
	}

	autoPad := attrStr(n, "auto_pad", "NOTSET")
	if autoPad != "" && autoPad != "NOTSET" && !isTranspose {
		// SAME/VALID: pad so that output = ceil(input/stride).
		// (ConvTranspose in these models always uses explicit pads.)
		for ax := 0; ax < 2; ax++ {
			switch autoPad {
			case "VALID":
				p.pads[ax] = 0
				p.pads[ax+2] = 0
			case "SAME_UPPER", "SAME_LOWER":
				// input size unknown here; computed later in convOutputSize
				// by splitting the total pad. Mark via negative sentinel.
			default:
				return nil, fmt.Errorf("unsupported auto_pad %q", autoPad)
			}
		}
	}
	return p, nil
}

// convAutoPads computes pads for auto_pad SAME_* given input/kernel/stride.
func convAutoPads(autoPad string, in, k, stride, dil int) (begin, end int) {
	kEff := (k-1)*dil + 1
	out := (in + stride - 1) / stride
	total := (out-1)*stride + kEff - in
	if total < 0 {
		total = 0
	}
	if autoPad == "SAME_LOWER" {
		begin = (total + 1) / 2
	} else { // SAME_UPPER
		begin = total / 2
	}
	return begin, total - begin
}

func opConv(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	x, w := args[0], args[1]
	if x.Rank() != 4 || w.Rank() != 4 {
		return nil, fmt.Errorf("conv: only 4D NCHW supported (x rank %d, w rank %d)", x.Rank(), w.Rank())
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	wf, err := w.Floats()
	if err != nil {
		return nil, err
	}
	var bias []float32
	if len(args) > 2 && args[2] != nil {
		if bias, err = args[2].Floats(); err != nil {
			return nil, err
		}
		if len(bias) == 0 {
			bias = nil // empty bias tensor == no bias (kernels index it otherwise)
		}
	}
	p, err := parseConvParams(n, w.Shape, false)
	if err != nil {
		return nil, err
	}
	autoPad := attrStr(n, "auto_pad", "NOTSET")
	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	M := w.Shape[0]
	if autoPad == "SAME_UPPER" || autoPad == "SAME_LOWER" {
		p.pads[0], p.pads[2] = convAutoPads(autoPad, H, p.kH, p.strides[0], p.dilations[0])
		p.pads[1], p.pads[3] = convAutoPads(autoPad, W, p.kW, p.strides[1], p.dilations[1])
	}
	if C%p.group != 0 || M%p.group != 0 {
		return nil, fmt.Errorf("conv: C=%d M=%d not divisible by group=%d", C, M, p.group)
	}
	Cg := w.Shape[1]
	if Cg*p.group != C {
		return nil, fmt.Errorf("conv: weight C/group %d * group %d != input C %d", Cg, p.group, C)
	}
	oH := (H+p.pads[0]+p.pads[2]-p.dilations[0]*(p.kH-1)-1)/p.strides[0] + 1
	oW := (W+p.pads[1]+p.pads[3]-p.dilations[1]*(p.kW-1)-1)/p.strides[1] + 1
	if oH <= 0 || oW <= 0 {
		return nil, fmt.Errorf("conv: non-positive output %dx%d", oH, oW)
	}
	if len(bias) > 0 && len(bias) != M {
		return nil, fmt.Errorf("conv: bias len %d != M %d", len(bias), M)
	}

	out := rc.newFloat(n, 0, N, M, oH, oW)

	switch {
	case p.group == C && Cg == 1 && C > 1 && M == C:
		// Depthwise with channel multiplier 1 (M == C). A multiplier > 1
		// (M == k*C) is left to convGeneral: the depthwise kernels map
		// output channel 1:1 to the input channel.
		convDepthwise(out.F32, xf, wf, bias, N, C, H, W, oH, oW, p)
	case p.kH == 1 && p.kW == 1 && p.strides == [2]int{1, 1} && p.dilations == [2]int{1, 1} && p.pads == [4]int{0, 0, 0, 0}:
		conv1x1(out.F32, xf, wf, bias, N, C, M, oH, oW, p)
	default:
		if err := convGeneral(out.F32, xf, wf, bias, N, C, M, H, W, oH, oW, p); err != nil {
			return nil, err
		}
	}
	applyEpilogue(out.F32, epilogueFor(rc, n))
	return []*Tensor{out}, nil
}

// convDepthwise handles group == C (one input channel per output channel).
func convDepthwise(out, x, w, bias []float32, N, C, H, W, oH, oW int, p *convParams) {
	sW := p.strides[1]
	if p.dilations == [2]int{1, 1} && sW >= 1 && sW <= 2 {
		convDepthwiseSIMD(out, x, w, bias, N, C, H, W, oH, oW, p)
		return
	}
	convDepthwiseScalar(out, x, w, bias, N, C, H, W, oH, oW, p)
}

// convDepthwiseSIMD vectorizes along the output W axis: each output row is
// bias plus kH*kW axpy calls over contiguous input segments. For stride-2
// the input plane is first deinterleaved into even/odd column planes so the
// segments stay contiguous.
func convDepthwiseSIMD(out, x, w, bias []float32, N, C, H, W, oH, oW int, p *convParams) {
	sH, sW := p.strides[0], p.strides[1]
	pT, pL := p.pads[0], p.pads[1]
	eW := (W + 1) / 2
	oWd := W / 2
	xt.ParallelRanges(N*C, func(start, end int) {
		var evenP, oddP []float32
		if sW == 2 {
			evenP = make([]float32, H*eW)
			oddP = make([]float32, H*oWd)
		}
		for nc := start; nc < end; nc++ {
			nI, c := nc/C, nc%C
			xBase := nCBase(nI, c, C, H, W)
			wBase := c * p.kH * p.kW
			outBase := nCBase(nI, c, C, oH, oW)
			bv := float32(0)
			if bias != nil {
				bv = bias[c]
			}
			if sW == 2 {
				for ih := 0; ih < H; ih++ {
					src := x[xBase+ih*W : xBase+ih*W+W]
					ev := evenP[ih*eW : (ih+1)*eW]
					od := oddP[ih*oWd : (ih+1)*oWd]
					for j := 0; j < oWd; j++ {
						ev[j] = src[2*j]
						od[j] = src[2*j+1]
					}
					if W%2 == 1 {
						ev[eW-1] = src[W-1]
					}
				}
			}
			for oh := 0; oh < oH; oh++ {
				outRow := out[outBase+oh*oW : outBase+oh*oW+oW]
				fillBias(outRow, bv)
				if sW == 1 && (p.kW == 3 || p.kW == 7) && oW >= 8 {
					// Fused-tap path: one pass per kh row for the W-interior
					// (all kernel taps in bounds), scalar fixups for the few
					// edge pixels. Bit-identical tap order to the per-tap
					// fmadd loop below.
					for kh := 0; kh < p.kH; kh++ {
						ih := oh*sH - pT + kh
						if ih < 0 || ih >= H {
							continue
						}
						row := x[xBase+ih*W:]
						wk := w[wBase+kh*p.kW:]
						i0 := pL
						if i0 < 0 {
							i0 = 0
						}
						i1 := W + pL - p.kW + 1
						if i1 > oW {
							i1 = oW
						}
						if i1 < i0 {
							i1 = i0
						}
						if i1 > i0 {
							switch p.kW {
							case 3:
								fmadd3Into(outRow[i0:i1], row[i0-pL:], wk[0], wk[1], wk[2])
							case 7:
								fmadd3Into(outRow[i0:i1], row[i0-pL:], wk[0], wk[1], wk[2])
								fmadd3Into(outRow[i0:i1], row[i0-pL+3:], wk[3], wk[4], wk[5])
								fmaddScalarInto(outRow[i0:i1], row[i0-pL+6:], wk[6])
							}
						}
						for ow := 0; ow < i0 && ow < oW; ow++ {
							dwEdgeTap(outRow, row, wk, ow, pL, W, p.kW)
						}
						for ow := i1; ow < oW; ow++ {
							dwEdgeTap(outRow, row, wk, ow, pL, W, p.kW)
						}
					}
					continue
				}
				for kh := 0; kh < p.kH; kh++ {
					ih := oh*sH - pT + kh
					if ih < 0 || ih >= H {
						continue
					}
					for kw := 0; kw < p.kW; kw++ {
						d := kw - pL
						var row []float32
						var s0, plen int
						if sW == 1 {
							row = x[xBase+ih*W:]
							s0 = d
							plen = W
						} else if d&1 == 0 {
							row = evenP[ih*eW:]
							s0 = d / 2
							plen = eW
						} else {
							row = oddP[ih*oWd:]
							s0 = (d - 1) / 2
							plen = oWd
						}
						dst0 := 0
						n := oW
						if s0 < 0 {
							dst0 = -s0
							n += s0
							s0 = 0
						}
						if s0+n > plen {
							n = plen - s0
						}
						if n <= 0 {
							continue
						}
						fmaddScalarInto(outRow[dst0:dst0+n], row[s0:s0+n], w[wBase+kh*p.kW+kw])
					}
				}
			}
		}
	})
}

// fillBias initializes a fresh output row with the bias value; the per-tap
// fmadd loop then ACCUMULATES into the row, so the baseline must be written
// explicitly — out tensors may come from the arena (unspecified contents),
// not from zeroed NewFloat memory. A zero bias clears the row; a non-zero
// bias is splatted with a doubling copy (memmove-speed, no SIMD needed).
func fillBias(row []float32, bv float32) {
	if len(row) == 0 {
		return
	}
	if bv == 0 {
		clear(row)
		return
	}
	row[0] = bv
	for n := 1; n < len(row); {
		n += copy(row[n:], row[:n])
	}
}

// dwEdgeTap applies the checked per-tap accumulation for one edge pixel.
func dwEdgeTap(outRow, row, wk []float32, ow, pL, W, kW int) {
	for kw := 0; kw < kW; kw++ {
		iw := ow - pL + kw
		if iw >= 0 && iw < W {
			outRow[ow] += wk[kw] * row[iw]
		}
	}
}

// convDepthwiseScalar is the general fallback (any stride/dilation).
func convDepthwiseScalar(out, x, w, bias []float32, N, C, H, W, oH, oW int, p *convParams) {
	xt.ParallelRanges(N*C, func(start, end int) {
		for nc := start; nc < end; nc++ {
			nI, c := nc/C, nc%C
			xBase := nCBase(nI, c, C, H, W)
			wBase := c * p.kH * p.kW
			outBase := nCBase(nI, c, C, oH, oW)
			bv := float32(0)
			if bias != nil {
				bv = bias[c]
			}
			for oh := 0; oh < oH; oh++ {
				for ow := 0; ow < oW; ow++ {
					sum := bv
					for kh := 0; kh < p.kH; kh++ {
						ih := oh*p.strides[0] - p.pads[0] + kh*p.dilations[0]
						if ih < 0 || ih >= H {
							continue
						}
						for kw := 0; kw < p.kW; kw++ {
							iw := ow*p.strides[1] - p.pads[1] + kw*p.dilations[1]
							if iw < 0 || iw >= W {
								continue
							}
							sum += x[xBase+ih*W+iw] * w[wBase+kh*p.kW+kw]
						}
					}
					out[outBase+oh*oW+ow] = sum
				}
			}
		}
	})
}

func nCBase(n, c, C, H, W int) int { return (n*C + c) * H * W }

// im2row fills B[0:rows*K] with the flattened kernel windows for output
// pixels [pix0, pix0+rows) (row-major over oW). Out-of-bounds taps are
// written as explicit zeros so pooled buffers need no clearing.
func im2row(B, x []float32, xG, pix0, rows, oW, H, W, Cg int, p *convParams) {
	kH, kW := p.kH, p.kW

	if p.dilations[1] == 1 && kH <= 8 && kW <= 8 {
		im2rowFast(B, x, xG, pix0, rows, oW, H, W, Cg, p)
		return
	}
	im2rowGeneric(B, x, xG, pix0, rows, oW, H, W, Cg, p)
}

// im2rowFast hoists everything invariant across an output row (kernel-row
// validity, channel plane bases, the W-interior pixel range) out of the
// pixel loop, so interior pixels cost only kW loads+stores per tap.
func im2rowFast(B, x []float32, xG, pix0, rows, oW, H, W, Cg int, p *convParams) {
	kH, kW := p.kH, p.kW
	sH, sW := p.strides[0], p.strides[1]
	pT, pL := p.pads[0], p.pads[1]
	dilH := p.dilations[0]
	K := Cg * kH * kW

	// W-interior output range: iw0 = ow*sW - pL in [0, W-kW].
	owA := 0
	if pL > 0 {
		owA = (pL + sW - 1) / sW
	}
	owB := oW
	if v := (W-kW+pL)/sW + 1; v < owB {
		owB = v
	}
	if owB < owA {
		owB = owA
	}

	oh := pix0 / oW
	ow := pix0 - oh*oW
	pi := 0
	for pi < rows {
		rowLen := oW - ow
		if rem := rows - pi; rowLen > rem {
			rowLen = rem
		}
		rowEnd := ow + rowLen

		// Per-kh kernel-row info for this output row: absolute offset of the
		// (c=0) source row start; -1 = padded row.
		var rowOff [8]int
		for kh := 0; kh < kH; kh++ {
			ih := oh*sH - pT + kh*dilH
			if ih >= 0 && ih < H {
				rowOff[kh] = ih * W
			} else {
				rowOff[kh] = -1
			}
		}

		// Interior segment: no range checks at all. Clamp to the block's
		// slice of this row: a block may start/end mid-row, and with wide
		// pads owA can exceed rowEnd on a tiny segment.
		s0, s1 := ow, rowEnd
		if s0 < owA {
			s0 = owA
		}
		if s0 > rowEnd {
			s0 = rowEnd
		}
		if s1 > owB {
			s1 = owB
		}
		if s1 < s0 {
			s1 = s0
		}
		for oc := s0; oc < s1; oc++ {
			dst := B[(pi+oc-ow)*K:]
			iw0 := oc*sW - pL
			i := 0
			xC := xG
			for c := 0; c < Cg; c++ {
				for kh := 0; kh < kH; kh++ {
					off := rowOff[kh]
					if off < 0 {
						for z := 0; z < kW; z++ {
							dst[i+z] = 0
						}
						i += kW
						continue
					}
					j := xC + off + iw0
					switch kW {
					case 3:
						dst[i], dst[i+1], dst[i+2] = x[j], x[j+1], x[j+2]
					case 2:
						dst[i], dst[i+1] = x[j], x[j+1]
					default:
						copy(dst[i:i+kW], x[j:j+kW])
					}
					i += kW
				}
				xC += H * W
			}
		}

		// Edge segments: per-tap checked walk (few pixels).
		im2rowRowEdge(B, x, xG, pi, oh, ow, s0, H, W, Cg, p)
		im2rowRowEdge(B, x, xG, pi+(s1-ow), oh, s1, rowEnd, H, W, Cg, p)

		pi += rowLen
		ow = 0
		oh++
	}
}

// im2rowRowEdge fills the checked per-tap path for output pixels
// [owFrom, owTo) of output row oh, with B rows starting at pi.
func im2rowRowEdge(B, x []float32, xG, pi, oh, owFrom, owTo, H, W, Cg int, p *convParams) {
	for oc := owFrom; oc < owTo; oc++ {
		dst := B[pi*Cg*p.kH*p.kW:][:Cg*p.kH*p.kW]
		i := 0
		for c := 0; c < Cg; c++ {
			xC := xG + c*H*W
			for kh := 0; kh < p.kH; kh++ {
				ih := oh*p.strides[0] - p.pads[0] + kh*p.dilations[0]
				if ih < 0 || ih >= H {
					for z := 0; z < p.kW; z++ {
						dst[i+z] = 0
					}
					i += p.kW
					continue
				}
				row := x[xC+ih*W : xC+ih*W+W]
				iw0 := oc*p.strides[1] - p.pads[1]
				for kw := 0; kw < p.kW; kw++ {
					iw := iw0 + kw*p.dilations[1]
					if iw >= 0 && iw < W {
						dst[i] = row[iw]
					} else {
						dst[i] = 0
					}
					i++
				}
			}
		}
		pi++
	}
}

// im2rowGeneric is the original per-pixel walk (any dilation/kernel size).
func im2rowGeneric(B, x []float32, xG, pix0, rows, oW, H, W, Cg int, p *convParams) {
	oh := pix0 / oW
	ow := pix0 - oh*oW
	for pi := 0; pi < rows; pi++ {
		dst := B[pi*Cg*p.kH*p.kW:][:Cg*p.kH*p.kW]
		i := 0
		for c := 0; c < Cg; c++ {
			xC := xG + c*H*W
			for kh := 0; kh < p.kH; kh++ {
				ih := oh*p.strides[0] - p.pads[0] + kh*p.dilations[0]
				if ih < 0 || ih >= H {
					for z := 0; z < p.kW; z++ {
						dst[i+z] = 0
					}
					i += p.kW
					continue
				}
				row := x[xC+ih*W : xC+ih*W+W]
				iw0 := ow*p.strides[1] - p.pads[1]
				if p.dilations[1] == 1 && iw0 >= 0 && iw0+p.kW <= W {
					s := row[iw0 : iw0+p.kW]
					d := dst[i : i+p.kW]
					switch p.kW {
					case 3:
						d[0], d[1], d[2] = s[0], s[1], s[2]
					case 2:
						d[0], d[1] = s[0], s[1]
					default:
						copy(d, s)
					}
					i += p.kW
					continue
				}
				for kw := 0; kw < p.kW; kw++ {
					iw := iw0 + kw*p.dilations[1]
					if iw >= 0 && iw < W {
						dst[i] = row[iw]
					} else {
						dst[i] = 0
					}
					i++
				}
			}
		}
		ow++
		if ow == oW {
			ow = 0
			oh++
		}
	}
}

// transposePlanes copies src [C, HW] (channel planes) into dst [HW, C]
// (one row per pixel), the B-operand layout MatMul expects.
func transposePlanes(dst, src []float32, C, HW int) {
	transposePlanesRange(dst, src, C, HW, 0, HW)
}

// transposePlanesRange transposes the pixel range [p0, p1) of src into dst.
// The walk is tiled (16 channels x 64 pixels) so reads stay sequential while
// the scattered writes hit a small, L1-resident set of output rows.
// On AVX2 the 8x8 block interiors go through a SIMD transpose kernel.
func transposePlanesRange(dst, src []float32, C, HW, p0, p1 int) {
	if hasAVX2FMA && C >= 8 && p1-p0 >= 8 {
		transposePlanesRange8(dst, src, C, HW, p0, p1)
		return
	}
	transposePlanesRangeScalar(dst, src, C, HW, p0, p1)
}

// transposePlanesRange8 transposes 8-channel x 8-pixel blocks with the SIMD
// kernel inside 64-pixel tiles; tile/channel remainders use scalar copies.
func transposePlanesRange8(dst, src []float32, C, HW, p0, p1 int) {
	const tp = 64
	for pp := p0; pp < p1; pp += tp {
		pEnd := pp + tp
		if pEnd > p1 {
			pEnd = p1
		}
		c := 0
		for ; c+8 <= C; c += 8 {
			p := pp
			for ; p+8 <= pEnd; p += 8 {
				transpose8x8F32(&dst[p*C+c], C, &src[c*HW+p], HW)
			}
			for ; p < pEnd; p++ {
				d := dst[p*C+c:]
				s := src[c*HW+p:]
				for i := 0; i < 8; i++ {
					d[i] = s[i*HW]
				}
			}
		}
		for ; c < C; c++ {
			srcC := src[c*HW+pp : c*HW+pEnd]
			d := dst[pp*C+c:]
			for p, v := range srcC {
				d[p*C] = v
			}
		}
	}
}

// transposePlanesRangeScalar is the portable tiled walk.
func transposePlanesRangeScalar(dst, src []float32, C, HW, p0, p1 int) {
	const tc, tp = 16, 64
	c := 0
	for ; c+tc <= C; c += tc {
		for pp := p0; pp < p1; pp += tp {
			pEnd := pp + tp
			if pEnd > p1 {
				pEnd = p1
			}
			for i := 0; i < tc; i++ {
				s := src[(c+i)*HW+pp : (c+i)*HW+pEnd]
				d := dst[pp*C+c+i:]
				for j, v := range s {
					d[j*C] = v
				}
			}
		}
	}
	for ; c < C; c++ {
		srcC := src[c*HW+p0 : c*HW+p1]
		d := dst[p0*C+c:]
		for p, v := range srcC {
			d[p*C] = v
		}
	}
}

// conv1x1 is the pointwise fast path: a pure GEMM per batch (and group).
func conv1x1(out, x, w, bias []float32, N, C, M, H, W int, p *convParams) {
	HW := H * W
	Cg := C / p.group
	Mg := M / p.group
	xT := getScratch(HW * Cg)
	defer putScratch(xT)
	for nI := 0; nI < N; nI++ {
		for gI := 0; gI < p.group; gI++ {
			// x group slice is [Cg, HW]; GEMM needs B as [HW, Cg].
			xBase := (nI*C + gI*Cg) * HW
			xg := x[xBase : xBase+Cg*HW]
			if HW*Cg >= parThreshold {
				// Large plane: split pixel ranges across the pool (each
				// worker writes disjoint dst rows). Runs on the caller's
				// goroutine, so this cannot nest inside a pool worker.
				parallelChunks(HW, func(s, e int) {
					transposePlanesRange(xT, xg, Cg, HW, s, e)
				})
			} else {
				transposePlanes(xT, xg, Cg, HW)
			}
			wSlice := w[gI*Mg*Cg : (gI+1)*Mg*Cg]
			outSlice := out[(nI*M+gI*Mg)*HW : (nI*M+(gI+1)*Mg)*HW]
			xt.MatMul(outSlice, wSlice, xT, Mg, HW, Cg)
		}
	}
	if bias != nil {
		for nI := 0; nI < N; nI++ {
			for m := 0; m < M; m++ {
				row := out[(nI*M+m)*HW : (nI*M+m+1)*HW]
				vek32.AddNumber_Inplace(row, bias[m])
			}
		}
	}
}

// convGeneral handles arbitrary kernels via tiled im2row + GEMM.
func convGeneral(out, x, w, bias []float32, N, C, M, H, W, oH, oW int, p *convParams) error {
	Cg := C / p.group
	Mg := M / p.group
	K := Cg * p.kH * p.kW
	OHW := oH * oW

	// Tile over output rows to bound the im2row buffer.
	maxElems := 4 << 20 // 4M floats = 16MB per block
	blockPixels := OHW
	if K > 0 && OHW*K > maxElems {
		blockPixels = maxElems / K
		if blockPixels < oW {
			blockPixels = oW
		}
		if blockPixels > OHW {
			blockPixels = OHW
		}
	}
	nBlocks := (OHW + blockPixels - 1) / blockPixels
	total := N * p.group * nBlocks

	// The per-block MatMul below submits nested jobs to the shared pool, so
	// the outer level must not run on pool workers (nested submission from a
	// blocked pool worker can deadlock the fixed-size pool).
	parallelOuter(total, func(start, end int) {
		var B, blkOut []float32
		defer func() {
			if B != nil {
				putScratch(B)
			}
			if blkOut != nil {
				putScratch(blkOut)
			}
		}()
		for task := start; task < end; task++ {
			blk := task % nBlocks
			ng := task / nBlocks
			gI := ng % p.group
			nI := ng / p.group

			pix0 := blk * blockPixels
			pix1 := pix0 + blockPixels
			if pix1 > OHW {
				pix1 = OHW
			}
			rows := pix1 - pix0

			// im2row: B[pixel][c*kH*kW + kh*kW + kw] = x[n, g*Cg+c, ih, iw]
			// Out-of-bounds taps are written as explicit zeros so pooled
			// buffers need no clearing. Retain B/blkOut across tasks of this
			// worker (all blocks share the size except the last): re-getting
			// per task would churn multi-MB panels through the allocator.
			if need := rows * K; cap(B) < need {
				if B != nil {
					putScratch(B)
				}
				B = getScratch(need)
			}
			B = B[:rows*K]
			xG := (nI*C + gI*Cg) * H * W
			im2row(B, x, xG, pix0, rows, oW, H, W, Cg, p)
			wSlice := w[gI*Mg*K : (gI+1)*Mg*K]
			outG := (nI*M + gI*Mg) * OHW
			// A full output-row block is already contiguous when this task owns
			// the entire output plane. Writing GEMM directly into the arena tensor
			// eliminates the temporary panel plus Mg copies (a common PP-OCR case:
			// 3x3 feature-map convs fit below maxElems). Conv bias is channel
			// (matrix-row) oriented, so it remains a SIMD vector pass.
			if pix0 == 0 && rows == OHW {
				dst := out[outG : outG+Mg*OHW]
				xt.MatMul(dst, wSlice, B, Mg, OHW, K)
				if bias != nil {
					for m := 0; m < Mg; m++ {
						vek32.AddNumber_Inplace(dst[m*OHW:(m+1)*OHW], bias[gI*Mg+m])
					}
				}
				continue
			}
			// Tiled output: [Mg, rows] temporary, then scatter into the final
			// [Mg, OHW] planes. Retain it across blocks handled by this worker.
			if need := Mg * rows; cap(blkOut) < need {
				if blkOut != nil {
					putScratch(blkOut)
				}
				blkOut = getScratch(need)
			}
			blkOut = blkOut[:Mg*rows]
			xt.MatMul(blkOut, wSlice, B, Mg, rows, K)
			if bias != nil {
				for m := 0; m < Mg; m++ {
					vek32.AddNumber_Inplace(blkOut[m*rows:(m+1)*rows], bias[gI*Mg+m])
				}
			}
			for m := 0; m < Mg; m++ {
				copy(out[outG+m*OHW+pix0:outG+m*OHW+pix1], blkOut[m*rows:(m+1)*rows])
			}
		}
	})
	return nil
}

func opConvTranspose(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	x, w := args[0], args[1]
	if x.Rank() != 4 || w.Rank() != 4 {
		return nil, fmt.Errorf("convtranspose: only 4D supported")
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	wf, err := w.Floats()
	if err != nil {
		return nil, err
	}
	var bias []float32
	if len(args) > 2 && args[2] != nil {
		if bias, err = args[2].Floats(); err != nil {
			return nil, err
		}
		if len(bias) == 0 {
			bias = nil // empty bias tensor == no bias (kernels index it otherwise)
		}
	}
	p, err := parseConvParams(n, w.Shape, true)
	if err != nil {
		return nil, err
	}
	outPad := [2]int{0, 0}
	if op := attrInts(n, "output_padding"); op != nil {
		if len(op) != 2 {
			return nil, fmt.Errorf("convtranspose: output_padding len %d", len(op))
		}
		outPad = [2]int{int(op[0]), int(op[1])}
	}
	// Weight layout: [C_in, C_out/group, kH, kW].
	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	if w.Shape[0] != C {
		return nil, fmt.Errorf("convtranspose: weight C_in %d != input C %d", w.Shape[0], C)
	}
	Mg := w.Shape[1] // output channels per group
	M := Mg * p.group
	Cg := C / p.group
	oH := p.strides[0]*(H-1) + outPad[0] + (p.kH-1)*p.dilations[0] + 1 - p.pads[0] - p.pads[2]
	oW := p.strides[1]*(W-1) + outPad[1] + (p.kW-1)*p.dilations[1] + 1 - p.pads[1] - p.pads[3]
	if oH <= 0 || oW <= 0 {
		return nil, fmt.Errorf("convtranspose: non-positive output %dx%d", oH, oW)
	}
	if len(bias) > 0 && len(bias) != M {
		return nil, fmt.Errorf("convtranspose: bias len %d != M %d", len(bias), M)
	}

	// The col2im scatter-add accumulates, so arena checkouts for this node
	// are zeroed (arenaPlan.needsZero); a heap fallback is already zeroed.
	out := rc.newFloat(n, 0, N, M, oH, oW)
	HW := H * W
	K := Cg
	Ncol := Mg * p.kH * p.kW

	for nI := 0; nI < N; nI++ {
		for gI := 0; gI < p.group; gI++ {
			// xT: [HW, Cg] from x[n, g*Cg:(g+1)*Cg]
			xT := getScratch(HW * Cg)
			xBase := (nI*C + gI*Cg) * HW
			transposePlanes(xT, xf[xBase:xBase+Cg*HW], Cg, HW)
			// wT: [Mg*kH*kW, Cg] from w[g*Cg:(g+1)*Cg][Mg][kH][kW]
			wT := getScratch(Ncol * Cg)
			wBase := gI * Cg * Mg * p.kH * p.kW
			for c := 0; c < Cg; c++ {
				for m := 0; m < Mg; m++ {
					for k := 0; k < p.kH*p.kW; k++ {
						wT[(m*p.kH*p.kW+k)*Cg+c] = wf[wBase+((c*Mg+m)*p.kH*p.kW)+k]
					}
				}
			}
			// cols[pixel][m*kH*kW+k]
			cols := getScratch(HW * Ncol)
			xt.MatMul(cols, xT, wT, HW, Ncol, K)
			putScratch(xT)
			putScratch(wT)
			// col2im scatter-add
			for pix := 0; pix < HW; pix++ {
				ih := pix / W
				iw := pix % W
				row := cols[pix*Ncol : (pix+1)*Ncol]
				for m := 0; m < Mg; m++ {
					outBase := (nI*M + gI*Mg + m) * oH * oW
					for kh := 0; kh < p.kH; kh++ {
						oh := ih*p.strides[0] - p.pads[0] + kh*p.dilations[0]
						if oh < 0 || oh >= oH {
							continue
						}
						for kw := 0; kw < p.kW; kw++ {
							ow := iw*p.strides[1] - p.pads[1] + kw*p.dilations[1]
							if ow < 0 || ow >= oW {
								continue
							}
							out.F32[outBase+oh*oW+ow] += row[m*p.kH*p.kW+kh*p.kW+kw]
						}
					}
				}
			}
			putScratch(cols)
		}
	}
	if bias != nil {
		spatial := oH * oW
		for nI := 0; nI < N; nI++ {
			for m := 0; m < M; m++ {
				row := out.F32[(nI*M+m)*spatial : (nI*M+m+1)*spatial]
				vek32.AddNumber_Inplace(row, bias[m])
			}
		}
	}
	applyEpilogue(out.F32, epilogueFor(rc, n))
	return []*Tensor{out}, nil
}

// epilogueFor returns the fused epilogue for a node (nil-safe).
func epilogueFor(rc *runCtx, n *Node) *epilogue {
	g := rc.graf()
	if g == nil {
		return nil
	}
	return g.epilogues[n]
}
