package yolo

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestLoadModel_OmniParserV2(t *testing.T) {
	weightsPath := "weights/omniparser-v2.yolow"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		t.Skip("weights not found, run convert_weights.py first")
	}

	t.Log("Loading model...")
	start := time.Now()
	model, err := LoadModel(weightsPath)
	if err != nil {
		t.Fatalf("LoadModel failed: %v", err)
	}
	t.Logf("Model loaded in %v", time.Since(start))
	t.Logf("InputSize=%d, NC=%d", model.InputSize, model.NC)

	if model.InputSize != 1280 {
		t.Errorf("expected InputSize=1280, got %d", model.InputSize)
	}
	if model.NC != 1 {
		t.Errorf("expected NC=1, got %d", model.NC)
	}

	// Verify key layers are populated
	if model.B0 == nil {
		t.Error("B0 (stem conv) is nil")
	}
	if model.B10 == nil {
		t.Error("B10 (C2PSA) is nil")
	}
	if model.Head == nil {
		t.Error("Head is nil")
	}
	if len(model.Head.CV2) != 3 {
		t.Errorf("expected 3 detect scales, got %d", len(model.Head.CV2))
	}

	fmt.Printf("Model structure OK: %d backbone layers, 3 detect scales\n", 11)
}

func TestForward_SmallInput(t *testing.T) {
	weightsPath := "weights/omniparser-v2.yolow"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		t.Skip("weights not found")
	}

	model, err := LoadModel(weightsPath)
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}

	// Use 640x640 input for speed
	inputSize := 640
	input := NewTensor(1, 3, inputSize, inputSize)
	gray := float32(0.5)
	for i := range input.Data {
		input.Data[i] = gray
	}

	t.Log("Running forward pass on 640x640 input...")
	start := time.Now()
	preds := model.Forward(input)
	elapsed := time.Since(start)
	t.Logf("Forward pass: %v", elapsed)
	t.Logf("Output shape: %v", preds.Shape)

	if preds.Dim() != 3 {
		t.Fatalf("expected 3D output, got %dD", preds.Dim())
	}
	if preds.Shape[0] != 1 {
		t.Errorf("expected batch=1, got %d", preds.Shape[0])
	}
	if preds.Shape[2] < 5 { // at minimum 4 box + 1 class
		t.Errorf("expected at least 5 output dims, got %d", preds.Shape[2])
	}
	t.Logf("Total anchors: %d", preds.Shape[1])
}

func TestDetect_SyntheticScreenshot(t *testing.T) {
	weightsPath := "weights/omniparser-v2.yolow"
	b64Path := "weights/test_screenshot.b64"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		t.Skip("weights not found")
	}
	if _, err := os.Stat(b64Path); os.IsNotExist(err) {
		t.Skip("test screenshot not found")
	}

	model, err := LoadModel(weightsPath)
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}

	b64Data, err := os.ReadFile(b64Path)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}

	t.Log("Running detection on synthetic screenshot...")
	start := time.Now()
	dets, err := model.Detect(string(b64Data), 0.2, 0.5)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	t.Logf("Detection completed in %v", elapsed)
	t.Logf("Found %d detections (conf>0.2)", len(dets))

	// Also check with lower threshold
	detsLow, _ := model.Detect(string(b64Data), 0.05, 0.5)
	t.Logf("Found %d detections (conf>0.05)", len(detsLow))
	for i, d := range detsLow {
		if i >= 15 {
			t.Logf("  ... and %d more", len(detsLow)-15)
			break
		}
		t.Logf("  [%d] box=(%d,%d,%d,%d) conf=%.3f class=%d",
			i, d.X, d.Y, d.W, d.H, d.Confidence, d.Class)
	}
}
