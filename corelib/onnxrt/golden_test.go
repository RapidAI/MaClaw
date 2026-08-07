package onnxrt

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// goldenTensor mirrors the JSON format written by testdata/run_golden.py.
type goldenTensor struct {
	Shape []int  `json:"shape"`
	DType string `json:"dtype"`
	Data  string `json:"data"`
}

type goldenInputs struct {
	Inputs map[string]goldenTensor `json:"inputs"`
}

type goldenOutputs struct {
	Outputs map[string]goldenTensor `json:"outputs"`
}

func (gt goldenTensor) tensor(t *testing.T) *Tensor {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(gt.Data)
	if err != nil {
		t.Fatal(err)
	}
	switch gt.DType {
	case "float32":
		if len(raw)%4 != 0 {
			t.Fatalf("bad float32 payload len %d", len(raw))
		}
		data := make([]float32, len(raw)/4)
		for i := range data {
			data[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return &Tensor{Shape: gt.Shape, DType: DFloat32, F32: data}
	case "int64":
		if len(raw)%8 != 0 {
			t.Fatalf("bad int64 payload len %d", len(raw))
		}
		data := make([]int64, len(raw)/8)
		for i := range data {
			data[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
		}
		return &Tensor{Shape: gt.Shape, DType: DInt64, I64: data}
	}
	t.Fatalf("unknown golden dtype %q", gt.DType)
	return nil
}

func readGolden[T any](t *testing.T, path string) *T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

// compareTensors checks shape equality and elementwise closeness, returning
// the max abs diff.
func compareTensors(t *testing.T, name string, got, want *Tensor, tol float64) float64 {
	t.Helper()
	if !shapeEqual(got.Shape, want.Shape) {
		t.Fatalf("%s: shape %v != golden %v", name, got.Shape, want.Shape)
	}
	if got.DType != want.DType {
		t.Fatalf("%s: dtype %v != golden %v", name, got.DType, want.DType)
	}
	maxDiff := 0.0
	if got.DType == DInt64 {
		for i := range got.I64 {
			if got.I64[i] != want.I64[i] {
				t.Fatalf("%s: int64 mismatch at %d: %d != %d", name, i, got.I64[i], want.I64[i])
			}
		}
		return 0
	}
	for i := range got.F32 {
		d := math.Abs(float64(got.F32[i] - want.F32[i]))
		if d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff > tol {
		t.Fatalf("%s: max abs diff %g exceeds tolerance %g", name, maxDiff, tol)
	}
	return maxDiff
}

// TestGoldenKernels runs every testdata case that has an onnxruntime
// outputs.json and compares this runtime against the golden output.
func TestGoldenKernels(t *testing.T) {
	root := "testdata"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skip("no testdata")
	}
	ran := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "outputs.json")); err != nil {
			continue // golden not generated (e.g. no onnxruntime on this host)
		}
		ran++
		t.Run(e.Name(), func(t *testing.T) {
			in := readGolden[goldenInputs](t, filepath.Join(dir, "inputs.json"))
			want := readGolden[goldenOutputs](t, filepath.Join(dir, "outputs.json"))

			var g *Graph
			var err error
			if _, statErr := os.Stat(filepath.Join(dir, "model.onnx")); statErr == nil {
				g, err = LoadGraph(filepath.Join(dir, "model.onnx"))
			} else {
				// real-model cases reference the downloaded model
				realPath := map[string]string{
					"real_det": filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_det.onnx"),
					"real_rec": filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx"),
				}[e.Name()]
				if realPath == "" {
					t.Fatalf("no model for case %s", e.Name())
				}
				if _, statErr := os.Stat(realPath); statErr != nil {
					t.Skipf("real model %s missing", realPath)
				}
				g, err = LoadGraph(realPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			inputs := map[string]*Tensor{}
			for name, gt := range in.Inputs {
				inputs[name] = gt.tensor(t)
			}
			outs, err := g.Run(inputs)
			if err != nil {
				t.Fatal(err)
			}
			tol := 1e-4
			if e.Name() == "real_det" || e.Name() == "real_rec" {
				tol = 1e-3
			}
			names := make([]string, 0, len(want.Outputs))
			for name := range want.Outputs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				got, ok := outs[name]
				if !ok {
					t.Fatalf("output %q missing", name)
				}
				d := compareTensors(t, name, got, want.Outputs[name].tensor(t), tol)
				t.Logf("output %s: max abs diff %g", name, d)
			}
		})
	}
	if ran == 0 {
		t.Skip("no golden outputs; run testdata/run_golden.py first")
	}
}
