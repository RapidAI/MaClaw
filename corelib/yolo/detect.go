package yolo

import (
	"math"
	"sort"
)

// Detection represents a single detected bounding box.
type Detection struct {
	X, Y, W, H int     // bounding box in original image coordinates
	Confidence  float32 // objectness * class confidence
	Class       int     // class index (0 for OmniParser single-class)
}

// DetectHead is the YOLOv8/YOLO11 anchor-free detection head.
// It processes multi-scale feature maps and produces detections.
type DetectHead struct {
	// Per-scale conv layers: cv2 for box regression, cv3 for classification
	CV2 [][]Layer // [nscales][3] — box regression branch
	CV3 [][]Layer // [nscales][3] — classification branch (may contain DWSepConv)

	NC     int // number of classes
	RegMax int // DFL reg_max (default 16)
	Stride []int // [8, 16, 32] for 3-scale detection
}

// Layer is a generic forward-pass layer.
type Layer interface {
	Forward(x *Tensor) *Tensor
}

// Forward processes multi-scale features and returns raw predictions.
// features: list of feature maps at different scales.
// Returns [N, totalAnchors, 4+NC] tensor.
func (d *DetectHead) Forward(features []*Tensor) *Tensor {
	regMax := d.RegMax
	if regMax == 0 {
		regMax = 16
	}

	var allPreds []*Tensor

	for i, feat := range features {
		// Box regression branch: layers → [N, regMax*4, H, W]
		box := feat
		for _, layer := range d.CV2[i] {
			box = layer.Forward(box)
		}

		// Classification branch: layers → [N, NC, H, W]
		cls := feat
		for _, layer := range d.CV3[i] {
			cls = layer.Forward(cls)
		}

		N := feat.Shape[0]
		H := feat.Shape[2]
		W := feat.Shape[3]
		numAnchors := H * W

		// Reshape box: [N, regMax*4, H, W] → [N, numAnchors, regMax*4]
		boxFlat := box.Reshape(N, regMax*4, numAnchors).Transpose2D()
		// Reshape cls: [N, NC, H, W] → [N, numAnchors, NC]
		clsFlat := cls.Reshape(N, d.NC, numAnchors).Transpose2D()

		// DFL decode: [N, numAnchors, regMax*4] → [N, numAnchors, 4]
		boxDecoded := dflDecode(boxFlat, regMax, numAnchors, N)

		// Convert from dist (left, top, right, bottom) to (cx, cy, w, h)
		// relative to grid, then scale by stride
		stride := float32(d.Stride[i])
		for n := 0; n < N; n++ {
			for a := 0; a < numAnchors; a++ {
				gridX := float32(a % W)
				gridY := float32(a / W)
				off := n*numAnchors*4 + a*4
				lt := boxDecoded.Data[off+0]   // left
				tp := boxDecoded.Data[off+1]   // top
				rt := boxDecoded.Data[off+2]   // right
				bt := boxDecoded.Data[off+3]   // bottom
				// YOLOv8 dist2bbox: x1 = grid - left, x2 = grid + right
				x1 := (gridX + 0.5 - lt) * stride
				y1 := (gridY + 0.5 - tp) * stride
				x2 := (gridX + 0.5 + rt) * stride
				y2 := (gridY + 0.5 + bt) * stride
				boxDecoded.Data[off+0] = (x1 + x2) / 2 // cx
				boxDecoded.Data[off+1] = (y1 + y2) / 2 // cy
				boxDecoded.Data[off+2] = x2 - x1        // w
				boxDecoded.Data[off+3] = y2 - y1        // h
			}
		}

		// Sigmoid on class scores
		clsFlat.Sigmoid()

		// Concat box + cls: [N, numAnchors, 4+NC]
		pred := NewTensor(N, numAnchors, 4+d.NC)
		for n := 0; n < N; n++ {
			for a := 0; a < numAnchors; a++ {
				dstOff := n*numAnchors*(4+d.NC) + a*(4+d.NC)
				boxOff := n*numAnchors*4 + a*4
				clsOff := n*numAnchors*d.NC + a*d.NC
				copy(pred.Data[dstOff:dstOff+4], boxDecoded.Data[boxOff:boxOff+4])
				copy(pred.Data[dstOff+4:dstOff+4+d.NC], clsFlat.Data[clsOff:clsOff+d.NC])
			}
		}
		allPreds = append(allPreds, pred)
	}

	// Concat all scales: [N, totalAnchors, 4+NC]
	totalAnchors := 0
	N := features[0].Shape[0]
	for _, p := range allPreds {
		totalAnchors += p.Shape[1]
	}
	result := NewTensor(N, totalAnchors, allPreds[0].Shape[2])
	predSize := allPreds[0].Shape[2]
	offset := 0
	for _, p := range allPreds {
		nAnchors := p.Shape[1]
		for n := 0; n < N; n++ {
			srcOff := n * nAnchors * predSize
			dstOff := n*totalAnchors*predSize + offset*predSize
			copy(result.Data[dstOff:dstOff+nAnchors*predSize], p.Data[srcOff:srcOff+nAnchors*predSize])
		}
		offset += nAnchors
	}
	return result
}

// dflDecode applies Distribution Focal Loss decoding.
// Input: [N, numAnchors, regMax*4] → Output: [N, numAnchors, 4]
// For each of the 4 box coordinates, applies softmax over regMax bins
// and computes weighted sum: sum(softmax(x) * [0, 1, ..., regMax-1]).
func dflDecode(input *Tensor, regMax, numAnchors, N int) *Tensor {
	out := NewTensor(N, numAnchors, 4)
	for n := 0; n < N; n++ {
		for a := 0; a < numAnchors; a++ {
			for d := 0; d < 4; d++ {
				// Extract regMax values for this coordinate
				srcOff := n*numAnchors*regMax*4 + a*regMax*4 + d*regMax
				// Softmax
				maxVal := float32(-math.MaxFloat32)
				for r := 0; r < regMax; r++ {
					if input.Data[srcOff+r] > maxVal {
						maxVal = input.Data[srcOff+r]
					}
				}
				sum := float32(0)
				vals := make([]float32, regMax)
				for r := 0; r < regMax; r++ {
					vals[r] = float32(math.Exp(float64(input.Data[srcOff+r] - maxVal)))
					sum += vals[r]
				}
				// Weighted sum
				wsum := float32(0)
				for r := 0; r < regMax; r++ {
					wsum += (vals[r] / sum) * float32(r)
				}
				out.Data[n*numAnchors*4+a*4+d] = wsum
			}
		}
	}
	return out
}

// PostProcess applies confidence thresholding and NMS to raw predictions.
// preds: [N, totalAnchors, 4+NC] from DetectHead.Forward.
// Returns detections for batch element 0 (single image inference).
// imgW, imgH: original image dimensions for coordinate scaling.
// inputSize: model input size (e.g. 640).
func PostProcess(preds *Tensor, confThresh, iouThresh float32, imgW, imgH, inputSize int) []Detection {
	nc := preds.Shape[2] - 4
	numAnchors := preds.Shape[1]

	// Scale factor from model input to original image
	scale := float32(inputSize) / float32(max(imgW, imgH))
	padX := float32(inputSize-int(float32(imgW)*scale)) / 2
	padY := float32(inputSize-int(float32(imgH)*scale)) / 2

	var dets []Detection
	for a := 0; a < numAnchors; a++ {
		off := a * (4 + nc)
		cx := preds.Data[off+0]
		cy := preds.Data[off+1]
		w := preds.Data[off+2]
		h := preds.Data[off+3]

		// Find best class
		bestConf := float32(0)
		bestCls := 0
		for c := 0; c < nc; c++ {
			conf := preds.Data[off+4+c]
			if conf > bestConf {
				bestConf = conf
				bestCls = c
			}
		}

		if bestConf < confThresh {
			continue
		}

		// Convert from model coordinates to original image coordinates
		x1 := (cx - w/2 - padX) / scale
		y1 := (cy - h/2 - padY) / scale
		x2 := (cx + w/2 - padX) / scale
		y2 := (cy + h/2 - padY) / scale

		// Clamp to image bounds
		x1 = clamp(x1, 0, float32(imgW))
		y1 = clamp(y1, 0, float32(imgH))
		x2 = clamp(x2, 0, float32(imgW))
		y2 = clamp(y2, 0, float32(imgH))

		dets = append(dets, Detection{
			X:          int(x1),
			Y:          int(y1),
			W:          int(x2 - x1),
			H:          int(y2 - y1),
			Confidence: bestConf,
			Class:      bestCls,
		})
	}

	// NMS
	return nms(dets, iouThresh)
}

// nms applies Non-Maximum Suppression.
func nms(dets []Detection, iouThresh float32) []Detection {
	// Sort by confidence descending
	sort.Slice(dets, func(i, j int) bool {
		return dets[i].Confidence > dets[j].Confidence
	})

	keep := make([]bool, len(dets))
	for i := range keep {
		keep[i] = true
	}

	for i := 0; i < len(dets); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(dets); j++ {
			if !keep[j] {
				continue
			}
			if dets[i].Class != dets[j].Class {
				continue
			}
			if iou(dets[i], dets[j]) > iouThresh {
				keep[j] = false
			}
		}
	}

	var result []Detection
	for i, d := range dets {
		if keep[i] {
			result = append(result, d)
		}
	}
	return result
}

func iou(a, b Detection) float32 {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.W, b.X+b.W)
	y2 := min(a.Y+a.H, b.Y+b.H)

	if x2 <= x1 || y2 <= y1 {
		return 0
	}

	inter := float32((x2 - x1) * (y2 - y1))
	areaA := float32(a.W * a.H)
	areaB := float32(b.W * b.H)
	return inter / (areaA + areaB - inter)
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
