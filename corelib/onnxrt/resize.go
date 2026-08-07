package onnxrt

import (
	"fmt"
	"math"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// opResize implements the opset-14 Resize signature:
// inputs X, roi (optional, ignored), scales (optional), sizes (optional).
// Only 4D NCHW with nearest/linear modes is supported (the modes the OCR
// models use); nearest_mode and coordinate_transformation_mode are honored.
func opResize(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	if x.Rank() != 4 {
		return nil, fmt.Errorf("resize: only 4D NCHW supported")
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	mode := attrStr(n, "mode", "nearest")
	ctm := attrStr(n, "coordinate_transformation_mode", "half_pixel")
	nearestMode := attrStr(n, "nearest_mode", "round_prefer_floor")

	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]

	// Determine output size from sizes or scales.
	var oH, oW int
	var scales []float32
	if len(args) > 3 && args[3] != nil && args[3].NumElements() > 0 {
		sizes, err := tensorIntsArg(args[3])
		if err != nil {
			return nil, fmt.Errorf("sizes: %w", err)
		}
		if len(sizes) != 4 {
			return nil, fmt.Errorf("resize: sizes len %d, want 4", len(sizes))
		}
		oH, oW = int(sizes[2]), int(sizes[3])
	} else if len(args) > 2 && args[2] != nil && args[2].NumElements() > 0 {
		scales, err = args[2].Floats()
		if err != nil {
			return nil, fmt.Errorf("scales: %w", err)
		}
		if len(scales) != 4 {
			return nil, fmt.Errorf("resize: scales len %d, want 4", len(scales))
		}
		oH = int(math.Floor(float64(float32(H) * scales[2])))
		oW = int(math.Floor(float64(float32(W) * scales[3])))
	} else {
		return nil, fmt.Errorf("resize: neither scales nor sizes given")
	}
	if oH <= 0 || oW <= 0 {
		return nil, fmt.Errorf("resize: non-positive output %dx%d", oH, oW)
	}

	// Effective scales for coordinate mapping. When the caller provided
	// scales, onnxruntime uses them as-is for the transform (the output size
	// is floor(in*scale), so out/input would lose the remainder); only
	// sizes-based resize derives the scale from the dimension ratio.
	scaleH := float64(oH) / float64(H)
	scaleW := float64(oW) / float64(W)
	if scales != nil {
		scaleH = float64(scales[2])
		scaleW = float64(scales[3])
	}

	// srcCoord maps a destination coordinate to the source space.
	coord := func(dst int, scale float64, outSize, inSize int) float64 {
		switch ctm {
		case "asymmetric":
			return float64(dst) / scale
		case "align_corners":
			if outSize == 1 {
				return 0
			}
			return float64(dst) * float64(inSize-1) / float64(outSize-1)
		case "pytorch_half_pixel":
			if outSize == 1 {
				return 0
			}
			return (float64(dst)+0.5)/scale - 0.5
		case "tf_half_pixel_for_nn":
			return (float64(dst) + 0.5) / scale
		default: // half_pixel
			return (float64(dst)+0.5)/scale - 0.5
		}
	}

	nearestIdx := func(dst int, scale float64, outSize, inSize int) int {
		v := coord(dst, scale, outSize, inSize)
		var idx int
		switch nearestMode {
		case "floor":
			idx = int(math.Floor(v))
		case "ceil":
			idx = int(math.Ceil(v))
		case "round_prefer_ceil":
			idx = int(math.Floor(v + 0.5))
		default: // round_prefer_floor: nearest, exact .5 rounds down
			idx = int(math.Ceil(v - 0.5))
		}
		if idx < 0 {
			idx = 0
		}
		if idx > inSize-1 {
			idx = inSize - 1
		}
		return idx
	}

	out := rc.newFloat(n, 0, N, C, oH, oW)
	switch mode {
	case "nearest":
		// Precompute index tables once; per-pixel work becomes a gather.
		rowTab := make([]int, oH)
		for oh := 0; oh < oH; oh++ {
			rowTab[oh] = nearestIdx(oh, scaleH, oH, H)
		}
		colTab := make([]int, oW)
		for ow := 0; ow < oW; ow++ {
			colTab[ow] = nearestIdx(ow, scaleW, oW, W)
		}
		xt.ParallelRanges(N*C, func(start, end int) {
			for nc := start; nc < end; nc++ {
				xBase := nc * H * W
				outBase := nc * oH * oW
				for oh := 0; oh < oH; oh++ {
					src := xf[xBase+rowTab[oh]*W:]
					dst := out.F32[outBase+oh*oW : outBase+oh*oW+oW]
					for ow := 0; ow < oW; ow++ {
						dst[ow] = src[colTab[ow]]
					}
				}
			}
		})
	case "linear":
		// bilinear over the two spatial dims. The W-direction coordinates are
		// invariant across rows/channels: precompute them once (identical
		// values to the per-pixel coord() walk, so results are unchanged).
		w0Tab := make([]int, oW)
		w1Tab := make([]int, oW)
		fwTab := make([]float32, oW)
		for ow := 0; ow < oW; ow++ {
			vw := coord(ow, scaleW, oW, W)
			w0 := int(math.Floor(vw))
			fw := float32(vw - float64(w0))
			w1 := w0 + 1
			if w0 < 0 {
				w0 = 0
			}
			if w0 > W-1 {
				w0 = W - 1
			}
			if w1 < 0 {
				w1 = 0
			}
			if w1 > W-1 {
				w1 = W - 1
			}
			w0Tab[ow] = w0
			w1Tab[ow] = w1
			fwTab[ow] = fw
		}
		xt.ParallelRanges(N*C, func(start, end int) {
			for nc := start; nc < end; nc++ {
				xBase := nc * H * W
				outBase := nc * oH * oW
				for oh := 0; oh < oH; oh++ {
					vh := coord(oh, scaleH, oH, H)
					h0 := int(math.Floor(vh))
					fh := float32(vh - float64(h0))
					h1 := h0 + 1
					if h0 < 0 {
						h0 = 0
					}
					if h0 > H-1 {
						h0 = H - 1
					}
					if h1 < 0 {
						h1 = 0
					}
					if h1 > H-1 {
						h1 = H - 1
					}
					row0 := xf[xBase+h0*W:]
					row1 := xf[xBase+h1*W:]
					dst := out.F32[outBase+oh*oW : outBase+oh*oW+oW]
					for ow := 0; ow < oW; ow++ {
						w0 := w0Tab[ow]
						p00 := row0[w0]
						p01 := row0[w1Tab[ow]]
						p10 := row1[w0]
						p11 := row1[w1Tab[ow]]
						fw := fwTab[ow]
						top := p00 + (p01-p00)*fw
						bot := p10 + (p11-p10)*fw
						dst[ow] = top + (bot-top)*fh
					}
				}
			}
		})
	default:
		return nil, fmt.Errorf("resize: unsupported mode %q", mode)
	}
	return []*Tensor{out}, nil
}
