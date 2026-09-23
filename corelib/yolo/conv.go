package yolo

import (
	"os"
	"runtime"
	"sync"
)

// Conv2dBNSiLU is a fused Conv2d + BatchNorm + SiLU layer.
// BatchNorm is folded into the convolution weights at load time:
//   w_fused = w * (gamma / sqrt(var + eps))
//   b_fused = (b - mean) * (gamma / sqrt(var + eps)) + beta
// This eliminates BatchNorm as a separate runtime operation.
type Conv2dBNSiLU struct {
	Weight     *Tensor          // [OutC, InC/Groups, KH, KW]
	Bias       []float32        // [OutC] — fused bias
	OutC       int
	InC        int
	KH, KW     int
	Stride     int
	Padding    int
	Groups     int              // 1 = normal conv, InC = depthwise conv
	UseSiLU    bool             // false for the final conv in Detect head
	WinoFilter *WinogradFilter  // pre-transformed Winograd filters (nil if not applicable)
	Wino2x3    *WinoFilter2x3   // F(2,3) Winograd weights (nil if not applicable)
}

// InitWinograd pre-computes Winograd filter transforms for eligible layers.
// Only applies to 3×3 stride=1 groups=1 convolutions where Winograd is faster
// than im2col+matmul. Winograd wins when spatial size is large and channel count
// is moderate (the accumulation loop scales with InC * numTiles).
func (c *Conv2dBNSiLU) InitWinograd() {
	if c.KH != 3 || c.KW != 3 || c.Stride != 1 || c.Padding != 1 {
		return
	}
	if c.Groups > 1 {
		return
	}
	// Heuristic: Winograd is faster when InC is small relative to spatial size.
	// For InC >= 256, the accumulation loop dominates and im2col+SIMD is faster.
	// Winograd F(2,3) for cross-correlation: mathematically correct (verified),
	// but slower than im2col+SIMD for this model due to 16 internal transposes.
	// Kept as infrastructure for future models with different channel/spatial ratios.
	// To enable: remove the early return below.
	_ = c.WinoFilter
	return
}

// Forward runs the fused convolution on input [N, InC, H, W].
// Returns [N, OutC, outH, outW].
func (c *Conv2dBNSiLU) Forward(input *Tensor) *Tensor {
	groups := c.Groups
	if groups <= 0 {
		groups = 1
	}

	// Winograd F(2,3) path for 3×3 stride=1 convolutions (2.25x fewer MACs,
	// bias+SiLU fused into the output transform). Disabled by default: the
	// scalar transforms only beat the padded im2col+GEMM path at small
	// spatial sizes — set YOLO_WINOGRAD=1 to enable.
	if c.Wino2x3 != nil && os.Getenv("YOLO_NO_WINOGRAD") == "" {
		if out := c.forwardWinograd2x3(input); out != nil {
			return out
		}
	}

	// Winograd path for 3×3 stride=1 convolutions
	if c.WinoFilter != nil {
		return Conv3x3Winograd(input, c.WinoFilter, c.Bias, c.UseSiLU)
	}

	if groups == 1 {
		return c.forwardNormal(input)
	}
	return c.forwardGrouped(input, groups)
}

// forwardNormal handles standard (non-grouped) convolution via im2col + matmul.
func (c *Conv2dBNSiLU) forwardNormal(input *Tensor) *Tensor {
	N := input.Shape[0]
	H := input.Shape[2]
	W := input.Shape[3]
	outH := (H+2*c.Padding-c.KH)/c.Stride + 1
	outW := (W+2*c.Padding-c.KW)/c.Stride + 1

	colSize := c.InC * c.KH * c.KW
	spatialSize := outH * outW

	out := NewTensor(N, c.OutC, outH, outW)
	wData := c.Weight.Data // [OutC, colSize] row-major

	siluFused := false
	for n := 0; n < N; n++ {
		outOff := n * c.OutC * spatialSize

		if c.KH == 1 && c.KW == 1 && c.Stride == 1 && c.Padding == 0 {
			// 1x1 conv: input [InC, H*W] is already the "col" matrix.
			// When the channel stride resonates with the 4K page size
			// (stride*4 % 4096 == 0), copy into a padded scratch so the
			// GEMM's B rows don't alias in L1.
			inOff := n * input.Stride[0]
			inSlice := input.Data[inOff : inOff+c.InC*spatialSize]
			ldb := spatialSize
			if hasAVX2FMA && spatialSize%16 == 0 && spatialSize%1024 == 0 && os.Getenv("YOLO_NO_LDBPAD") == "" {
				pad := getBufUnzeroed(c.InC * (spatialSize + 16))
				ldb = spatialSize + 16
				for k := 0; k < c.InC; k++ {
					copy(pad[k*ldb:k*ldb+spatialSize], inSlice[k*spatialSize:(k+1)*spatialSize])
				}
				siluFused = matmulConvActiv(wData, pad, c.Bias, out.Data[outOff:outOff+c.OutC*spatialSize], c.OutC, c.InC, spatialSize, ldb, c.UseSiLU)
				putBuf(pad)
			} else {
				siluFused = matmulConvActiv(wData, inSlice, c.Bias, out.Data[outOff:outOff+c.OutC*spatialSize], c.OutC, c.InC, spatialSize, ldb, c.UseSiLU)
			}
		} else {
			// General conv: im2col + matmul. The col buffer uses a padded
			// row stride (4K-aliasing avoidance) only when the AVX2 GEMM
			// will run; the legacy fallback reads a packed matrix.
			ldb := spatialSize
			if hasAVX2FMA && spatialSize%16 == 0 && spatialSize >= 16 && os.Getenv("YOLO_NO_LDBPAD") == "" {
				ldb = spatialSize + 16
			}
			col := getBufUnzeroed(colSize * ldb)
			im2colParallelLdb(input, n, c.KH, c.KW, c.Stride, c.Padding, outH, outW, col, ldb)
			siluFused = matmulConvActiv(wData, col, c.Bias, out.Data[outOff:outOff+c.OutC*spatialSize], c.OutC, colSize, spatialSize, ldb, c.UseSiLU)
			putBuf(col)
		}
	}

	if c.UseSiLU && !siluFused {
		siluSlice(out.Data)
	}
	return out
}

// forwardGrouped handles grouped/depthwise convolution.
func (c *Conv2dBNSiLU) forwardGrouped(input *Tensor, groups int) *Tensor {
	// Depthwise (groups == InC == OutC, one channel per group): vectorized
	// per-channel axpy over the W axis, parallel across channels. Only
	// stride-1 with modest kernels takes this path; anything else falls back
	// to the scalar loop below.
	if groups == c.InC && c.OutC == c.InC && c.Stride == 1 &&
		c.KH >= 1 && c.KH <= 7 && c.KW >= 1 && c.KW <= 7 {
		return c.forwardDepthwiseSIMD(input)
	}
	return c.forwardGroupedScalar(input, groups)
}

// forwardDepthwiseSIMD computes a stride-1 depthwise conv: each output row is
// bias plus kH*kW axpy passes over contiguous input segments (padding clipped
// per tap).
func (c *Conv2dBNSiLU) forwardDepthwiseSIMD(input *Tensor) *Tensor {
	N := input.Shape[0]
	H, W := input.Shape[2], input.Shape[3]
	outH := (H + 2*c.Padding - c.KH) + 1
	outW := (W + 2*c.Padding - c.KW) + 1
	C := c.InC
	out := NewTensor(N, c.OutC, outH, outW)
	pL := c.Padding

	total := N * C
	nWorkers := runtime.NumCPU()
	if nWorkers > total {
		nWorkers = total
	}
	if total < 16 {
		nWorkers = 1
	}
	var wg sync.WaitGroup
	per := (total + nWorkers - 1) / nWorkers
	for wk := 0; wk < nWorkers; wk++ {
		s := wk * per
		e := s + per
		if e > total {
			e = total
		}
		if s >= e {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for nc := s; nc < e; nc++ {
				nI, ch := nc/C, nc%C
				xBase := (nI*C + ch) * H * W
				outBase := (nI*c.OutC + ch) * outH * outW
				bv := float32(0)
				if c.Bias != nil {
					bv = c.Bias[ch]
				}
				wBase := ch * c.KH * c.KW
				for oh := 0; oh < outH; oh++ {
					outRow := out.Data[outBase+oh*outW : outBase+oh*outW+outW]
					fillBiasRow(outRow, bv)
					for kh := 0; kh < c.KH; kh++ {
						ih := oh - pL + kh // stride 1
						if ih < 0 || ih >= H {
							continue
						}
						row := input.Data[xBase+ih*W:]
						wk2 := c.Weight.Data[wBase+kh*c.KW:]
						for kw := 0; kw < c.KW; kw++ {
							s0, n2 := kw-pL, outW
							dst0 := 0
							if s0 < 0 {
								dst0 = -s0
								n2 += s0
								s0 = 0
							}
							if s0+n2 > W {
								n2 = W - s0
							}
							if n2 <= 0 {
								continue
							}
							axpySlice(outRow[dst0:dst0+n2], row[s0:s0+n2], wk2[kw])
						}
					}
				}
			}
		}(s, e)
	}
	wg.Wait()

	if c.UseSiLU {
		siluSlice(out.Data)
	}
	return out
}

// fillBiasRow initializes a fresh output row with the bias value; the per-tap
// axpy loop then ACCUMULATES into the row.
func fillBiasRow(row []float32, bv float32) {
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

func (c *Conv2dBNSiLU) forwardGroupedScalar(input *Tensor, groups int) *Tensor {
	N := input.Shape[0]
	H := input.Shape[2]
	W := input.Shape[3]
	outH := (H+2*c.Padding-c.KH)/c.Stride + 1
	outW := (W+2*c.Padding-c.KW)/c.Stride + 1

	out := NewTensor(N, c.OutC, outH, outW)
	inCPerGroup := c.InC / groups
	outCPerGroup := c.OutC / groups
	inStride0 := input.Stride[0]
	inStride1 := input.Stride[1]

	for n := 0; n < N; n++ {
		for g := 0; g < groups; g++ {
			inCStart := g * inCPerGroup
			outCStart := g * outCPerGroup
			for oc := 0; oc < outCPerGroup; oc++ {
				absOC := outCStart + oc
				bias := c.Bias[absOC]
				wOff := absOC * inCPerGroup * c.KH * c.KW
				outBase := n*out.Stride[0] + absOC*out.Stride[1]
				for oh := 0; oh < outH; oh++ {
					for ow := 0; ow < outW; ow++ {
						sum := bias
						for ic := 0; ic < inCPerGroup; ic++ {
							absIC := inCStart + ic
							inBase := n*inStride0 + absIC*inStride1
							wBase := wOff + ic*c.KH*c.KW
							for kh := 0; kh < c.KH; kh++ {
								ih := oh*c.Stride - c.Padding + kh
								if ih < 0 || ih >= H {
									continue
								}
								inRowOff := inBase + ih*W
								wRowOff := wBase + kh*c.KW
								for kw := 0; kw < c.KW; kw++ {
									iw := ow*c.Stride - c.Padding + kw
									if iw < 0 || iw >= W {
										continue
									}
									sum += input.Data[inRowOff+iw] * c.Weight.Data[wRowOff+kw]
								}
							}
						}
						out.Data[outBase+oh*outW+ow] = sum
					}
				}
			}
		}
	}

	if c.UseSiLU {
		siluSlice(out.Data)
	}
	return out
}
