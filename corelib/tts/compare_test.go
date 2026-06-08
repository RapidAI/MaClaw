package tts

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestComparePhonemeIDs verifies the stable Go G2P boundary/separator behavior.
func TestComparePhonemeIDs(t *testing.T) {
	// Go G2P keeps one sentence boundary at each edge and omits internal blank separators.
	wantIDs := []int{0, 49, 127, 70, 80, 0}

	pt := NewPhonemeTable()
	g2p := TextToPhonemes("Hello", pt, LangEN)

	t.Logf("Go phoneme IDs (%d): %v", len(g2p.PhonemeIDs), g2p.PhonemeIDs)
	t.Logf("Expected phoneme IDs (%d): %v", len(wantIDs), wantIDs)

	if len(g2p.PhonemeIDs) != len(wantIDs) {
		t.Fatalf("length mismatch: Go=%d, expected=%d", len(g2p.PhonemeIDs), len(wantIDs))
	}

	for i := range wantIDs {
		if g2p.PhonemeIDs[i] != wantIDs[i] {
			t.Errorf("ID[%d]: Go=%d, expected=%d", i, g2p.PhonemeIDs[i], wantIDs[i])
		}
	}
}

// TestCompareEmbedding compares Go embedding output with Python reference.
func TestCompareEmbedding(t *testing.T) {
	refPath := filepath.Join("testdata", "pyref_embedding_output.bin")
	if _, err := os.Stat(refPath); os.IsNotExist(err) {
		t.Skipf("Reference data not found: %s (run dump_emb_weights.py first)", refPath)
	}

	// Load Python reference embedding output (raw float32 binary)
	pyEmb, err := loadBinFloat32(refPath)
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}
	t.Logf("Python embedding: %d elements, mean=%.4f, std=%.4f",
		len(pyEmb), mean(pyEmb), std(pyEmb))

	// Load Python weights (same checkpoint)
	embW, _ := loadBinFloat32(filepath.Join("testdata", "pyweight_emb.bin"))
	toneW, _ := loadBinFloat32(filepath.Join("testdata", "pyweight_tone_emb.bin"))
	langW, _ := loadBinFloat32(filepath.Join("testdata", "pyweight_lang_emb.bin"))

	if embW == nil || toneW == nil || langW == nil {
		t.Skip("Python weight files not found")
	}

	// Use same phoneme IDs as Python
	phoneIDs := []int{0, 49, 0, 127, 0, 70, 0, 80, 0}
	T := len(phoneIDs)
	hidden := 192
	sqrtH := float32(math.Sqrt(float64(hidden)))

	// Compute embedding using Python weights
	goEmb := make([]float32, hidden*T)
	for t := 0; t < T; t++ {
		pid := phoneIDs[t]
		for h := 0; h < hidden; h++ {
			v := embW[pid*hidden+h] + toneW[0*hidden+h] + langW[2*hidden+h]
			goEmb[h*T+t] = v * sqrtH // [hidden, T] layout
		}
	}

	t.Logf("Go embedding: %d elements, mean=%.4f, std=%.4f",
		len(goEmb), mean(goEmb), std(goEmb))

	// Compare — should be exact match (same weights, same computation)
	if len(goEmb) != len(pyEmb) {
		t.Fatalf("size mismatch: Go=%d, Py=%d", len(goEmb), len(pyEmb))
	}

	maxDiff, avgDiff := compareFloat32(goEmb, pyEmb)
	t.Logf("Embedding comparison: maxDiff=%.8f, avgDiff=%.8f", maxDiff, avgDiff)

	if maxDiff > 1e-4 {
		t.Errorf("embedding mismatch: maxDiff=%.6f (expected < 1e-4)", maxDiff)
		// Print first few diffs
		for i := 0; i < 10 && i < len(goEmb); i++ {
			t.Logf("  [%d] Go=%.6f Py=%.6f diff=%.6f", i, goEmb[i], pyEmb[i], goEmb[i]-pyEmb[i])
		}
	}
}

// ── Helpers ──

func mean(x []float32) float32 {
	if len(x) == 0 {
		return 0
	}
	var s float32
	for _, v := range x {
		s += v
	}
	return s / float32(len(x))
}

func std(x []float32) float32 {
	if len(x) == 0 {
		return 0
	}
	m := mean(x)
	var s float32
	for _, v := range x {
		d := v - m
		s += d * d
	}
	return float32(math.Sqrt(float64(s / float32(len(x)))))
}

func compareFloat32(a, b []float32) (maxDiff, avgDiff float32) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sumDiff float32
	for i := 0; i < n; i++ {
		d := float32(math.Abs(float64(a[i] - b[i])))
		if d > maxDiff {
			maxDiff = d
		}
		sumDiff += d
	}
	if n > 0 {
		avgDiff = sumDiff / float32(n)
	}
	return
}

// loadBinFloat32 loads raw float32 binary data.
func loadBinFloat32(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return result, nil
}

// loadNpzFloat32 loads a float32 array from a .npz file using archive/zip.
func loadNpzFloat32(npzPath, arrayName string) ([]float32, error) {
	r, err := zip.OpenReader(npzPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	target := arrayName + ".npy"
	for _, f := range r.File {
		if f.Name != target {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		return parseNpy(data)
	}
	return nil, fmt.Errorf("array %q not found in %s", arrayName, npzPath)
}

// parseNpy parses a minimal .npy file (float32, C-order).
func parseNpy(data []byte) ([]float32, error) {
	if len(data) < 10 || data[0] != 0x93 || string(data[1:6]) != "NUMPY" {
		return nil, fmt.Errorf("not a valid .npy file")
	}
	// Version
	major := data[6]
	headerLen := 0
	headerStart := 0
	if major == 1 {
		headerLen = int(binary.LittleEndian.Uint16(data[8:]))
		headerStart = 10
	} else {
		headerLen = int(binary.LittleEndian.Uint32(data[8:]))
		headerStart = 12
	}

	// Skip header (contains dtype, shape, order info)
	dataStart := headerStart + headerLen

	// Read as float32
	nFloats := (len(data) - dataStart) / 4
	result := make([]float32, nFloats)
	for i := 0; i < nFloats; i++ {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[dataStart+i*4:]))
	}
	return result, nil
}
