package yolo

import "math"

// Attention implements multi-head self-attention with depthwise positional encoding.
// Used inside PSABlock in the C2PSA module.
//
// Architecture:
//   1. QKV projection: 1x1 conv → split into Q, K, V (each dim/2 channels)
//   2. Reshape to [N, numHeads, seqLen, headDim]
//   3. Attention: softmax(Q @ K^T / sqrt(headDim)) @ V
//   4. Positional encoding: depthwise 3x3 conv on V
//   5. Add PE to attention output
//   6. Project back: 1x1 conv
type Attention struct {
	QKV      *Conv2dBNSiLU // 1x1 conv, InC → 2*InC (Q+K concat, V separate)
	Proj     *Conv2dBNSiLU // 1x1 conv, InC/2 → InC/2
	PE       *Conv2dBNSiLU // depthwise 3x3 conv for positional encoding
	NumHeads int
}

// Forward runs attention on input [N, C, H, W].
// C2PSA.cv1 already expanded channels. PSABlock splits: first half → attention, second half → passthrough.
func (a *Attention) Forward(x *Tensor) *Tensor {
	N, _, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]

	// QKV projection: x [N, halfC, H, W] → qkv [N, 2*halfC, H, W]
	qkv := a.QKV.Forward(x)
	halfC := qkv.Shape[1] / 2
	q := qkv.SliceChannel(0, halfC)
	k := qkv.SliceChannel(halfC, 2*halfC)
	v := x.Clone() // V comes from input directly

	headDim := halfC / a.NumHeads
	if headDim == 0 {
		headDim = 1
	}
	scale := float32(1.0 / math.Sqrt(float64(headDim)))
	seqLen := H * W

	// Compute attention per batch
	out := NewTensor(N, halfC, H, W)

	for n := 0; n < N; n++ {
		for h := 0; h < a.NumHeads; h++ {
			chStart := h * headDim
			// Extract Q, K, V for this head: [headDim, seqLen]
			qHead := extractHead(q, n, chStart, headDim, seqLen)
			kHead := extractHead(k, n, chStart, headDim, seqLen)
			vHead := extractHead(v, n, chStart, headDim, seqLen)

			// Attention scores: Q^T @ K → [seqLen, seqLen]
			scores := make([]float32, seqLen*seqLen)
			for i := 0; i < seqLen; i++ {
				for j := 0; j < seqLen; j++ {
					sum := float32(0)
					for d := 0; d < headDim; d++ {
						sum += qHead[d*seqLen+i] * kHead[d*seqLen+j]
					}
					scores[i*seqLen+j] = sum * scale
				}
			}

			// Softmax along last dim (each row)
			for i := 0; i < seqLen; i++ {
				rowOff := i * seqLen
				maxVal := float32(-math.MaxFloat32)
				for j := 0; j < seqLen; j++ {
					if scores[rowOff+j] > maxVal {
						maxVal = scores[rowOff+j]
					}
				}
				sumExp := float32(0)
				for j := 0; j < seqLen; j++ {
					scores[rowOff+j] = float32(math.Exp(float64(scores[rowOff+j] - maxVal)))
					sumExp += scores[rowOff+j]
				}
				for j := 0; j < seqLen; j++ {
					scores[rowOff+j] /= sumExp
				}
			}

			// Weighted sum: scores @ V^T → [seqLen, headDim]
			for i := 0; i < seqLen; i++ {
				for d := 0; d < headDim; d++ {
					sum := float32(0)
					for j := 0; j < seqLen; j++ {
						sum += scores[i*seqLen+j] * vHead[d*seqLen+j]
					}
					// Write to output
					ch := chStart + d
					oh := i / W
					ow := i % W
					out.Set(sum, n, ch, oh, ow)
				}
			}
		}
	}

	// Positional encoding on V
	pe := a.PE.Forward(v) // depthwise 3x3 conv
	out.Add(pe)

	// Project
	out = a.Proj.Forward(out)
	return out
}

// extractHead extracts [headDim, seqLen] data for one head from a [N, C, H, W] tensor.
func extractHead(t *Tensor, n, chStart, headDim, seqLen int) []float32 {
	data := make([]float32, headDim*seqLen)
	HW := t.Shape[2] * t.Shape[3]
	for d := 0; d < headDim; d++ {
		srcOff := n*t.Stride[0] + (chStart+d)*t.Stride[1]
		dstOff := d * seqLen
		copy(data[dstOff:dstOff+HW], t.Data[srcOff:srcOff+HW])
	}
	return data
}

// PSABlock is a Partial Self-Attention block: Attention + FFN with residual connections.
// Input is already the first half of C2PSA's cv1 output (halfC channels).
type PSABlock struct {
	Attn *Attention
	FFN0 *Conv2dBNSiLU // 1x1 conv, expand
	FFN1 *Conv2dBNSiLU // 1x1 conv, reduce
}

func (p *PSABlock) Forward(x *Tensor) *Tensor {
	// Attention + residual
	attnOut := p.Attn.Forward(x)
	attnOut.Add(x)

	// FFN + residual
	ffnOut := p.FFN0.Forward(attnOut)
	ffnOut = p.FFN1.Forward(ffnOut)
	ffnOut.Add(attnOut)

	return ffnOut
}

// C2PSA is the C2-style module with Partial Self-Attention.
// Architecture:
//   1. cv1: 1x1 conv → [N, C, H, W]
//   2. Split into two halves: first half → PSABlock, second half → passthrough
//   3. Concat halves
//   4. cv2: 1x1 conv
type C2PSA struct {
	CV1    *Conv2dBNSiLU
	CV2    *Conv2dBNSiLU
	Blocks []*PSABlock
}

func (c *C2PSA) Forward(x *Tensor) *Tensor {
	y := c.CV1.Forward(x)
	halfC := y.Shape[1] / 2
	y0 := y.SliceChannel(0, halfC)
	y1 := y.SliceChannel(halfC, y.Shape[1])

	// PSA blocks on first half
	for _, b := range c.Blocks {
		y0 = b.Forward(y0)
	}

	cat := ConcatChannel(y0, y1)
	return c.CV2.Forward(cat)
}
