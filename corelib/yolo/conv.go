package yolo

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

	for n := 0; n < N; n++ {
		outOff := n * c.OutC * spatialSize

		if c.KH == 1 && c.KW == 1 && c.Stride == 1 && c.Padding == 0 {
			// 1x1 conv: input [InC, H*W] is already the "col" matrix.
			// Weight [OutC, InC] × Input [InC, H*W] → Out [OutC, H*W]
			// Input is contiguous per-channel, so we can use it directly
			// after transposing to [H*W, InC] for vek32.Dot.
			inOff := n * input.Stride[0]
			inSlice := input.Data[inOff : inOff+c.InC*spatialSize]
			matmulConv(wData, inSlice, c.Bias, out.Data[outOff:outOff+c.OutC*spatialSize], c.OutC, c.InC, spatialSize)
		} else {
			// General conv: im2col + matmul
			col := getBuf(colSize * spatialSize)
			im2colParallel(input, n, c.KH, c.KW, c.Stride, c.Padding, outH, outW, col)
			matmulConv(wData, col, c.Bias, out.Data[outOff:outOff+c.OutC*spatialSize], c.OutC, colSize, spatialSize)
			putBuf(col)
		}
	}

	if c.UseSiLU {
		out.SiLU()
	}
	return out
}

// forwardGrouped handles grouped/depthwise convolution.
func (c *Conv2dBNSiLU) forwardGrouped(input *Tensor, groups int) *Tensor {
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
		out.SiLU()
	}
	return out
}
