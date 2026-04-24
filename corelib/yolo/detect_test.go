package yolo

import (
	"testing"
)

func TestNMS(t *testing.T) {
	dets := []Detection{
		{X: 10, Y: 10, W: 100, H: 100, Confidence: 0.9, Class: 0},
		{X: 15, Y: 15, W: 100, H: 100, Confidence: 0.8, Class: 0}, // overlaps with first
		{X: 200, Y: 200, W: 50, H: 50, Confidence: 0.7, Class: 0}, // no overlap
	}

	result := nms(dets, 0.5)
	if len(result) != 2 {
		t.Fatalf("expected 2 detections after NMS, got %d", len(result))
	}
	// Should keep the highest confidence (0.9) and the non-overlapping (0.7)
	if result[0].Confidence != 0.9 {
		t.Errorf("first detection should have conf 0.9, got %f", result[0].Confidence)
	}
	if result[1].Confidence != 0.7 {
		t.Errorf("second detection should have conf 0.7, got %f", result[1].Confidence)
	}
}

func TestNMS_DifferentClasses(t *testing.T) {
	dets := []Detection{
		{X: 10, Y: 10, W: 100, H: 100, Confidence: 0.9, Class: 0},
		{X: 15, Y: 15, W: 100, H: 100, Confidence: 0.8, Class: 1}, // same box, different class
	}

	result := nms(dets, 0.5)
	// Different classes should not suppress each other
	if len(result) != 2 {
		t.Fatalf("expected 2 detections (different classes), got %d", len(result))
	}
}

func TestIOU(t *testing.T) {
	a := Detection{X: 0, Y: 0, W: 100, H: 100}
	b := Detection{X: 50, Y: 50, W: 100, H: 100}

	v := iou(a, b)
	// Intersection: 50x50 = 2500
	// Union: 10000 + 10000 - 2500 = 17500
	// IoU = 2500/17500 ≈ 0.1429
	expected := float32(2500.0 / 17500.0)
	if v < expected-0.01 || v > expected+0.01 {
		t.Errorf("IoU = %f, expected ~%f", v, expected)
	}
}

func TestIOU_NoOverlap(t *testing.T) {
	a := Detection{X: 0, Y: 0, W: 10, H: 10}
	b := Detection{X: 100, Y: 100, W: 10, H: 10}

	if iou(a, b) != 0 {
		t.Errorf("expected 0 IoU for non-overlapping boxes")
	}
}

func TestDFLDecode(t *testing.T) {
	// Simple test: regMax=4, 1 anchor, 1 batch
	// For each of 4 coordinates, softmax over 4 bins → weighted sum
	regMax := 4
	input := NewTensor(1, 1, regMax*4)
	// Set all to 0 → softmax = uniform → weighted sum = (0+1+2+3)/4 = 1.5
	out := dflDecode(input, regMax, 1, 1)
	if out.Shape[2] != 4 {
		t.Fatalf("expected 4 coordinates, got %d", out.Shape[2])
	}
	for d := 0; d < 4; d++ {
		v := out.At(0, 0, d)
		if v < 1.4 || v > 1.6 {
			t.Errorf("DFL decode coord %d = %f, expected ~1.5", d, v)
		}
	}
}

func TestPostProcess_Empty(t *testing.T) {
	// All predictions below threshold
	preds := NewTensor(1, 10, 5) // 10 anchors, 4 box + 1 class
	// All class scores = 0 (below any threshold)
	dets := PostProcess(preds, 0.5, 0.5, 640, 480, 640)
	if len(dets) != 0 {
		t.Errorf("expected 0 detections, got %d", len(dets))
	}
}
