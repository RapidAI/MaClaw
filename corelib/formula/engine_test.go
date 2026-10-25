package formula

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

func TestRecognizeSmoke(t *testing.T) {
	dir := formulaModelDir()
	if dir == "" {
		t.Skip("formula models not present (set FORMULA_MODEL_DIR or keep .tmp/formula-models)")
	}
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.SetMaxTokens(2)
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	for y := 12; y < 18; y++ {
		for x := 8; x < 56; x++ {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 0, 0, 0
		}
	}
	pix, err := preprocess(img)
	if err != nil {
		t.Fatal(err)
	}
	encOut, err := e.enc.Run(map[string]*onnxrt.Tensor{e.encIn: pix})
	if err != nil {
		t.Fatal(err)
	}
	hidden := encOut[e.encOut]
	if hidden == nil || hidden.Rank() != 3 || hidden.Shape[0] != 1 || hidden.Shape[2] == 0 {
		t.Fatalf("encoder hidden %v", shapeOf(hidden))
	}
	t.Logf("accel %s enc_seq %d", Accel(), hidden.Shape[1])
	step, err := e.dec.Run(map[string]*onnxrt.Tensor{
		e.decIDs:    onnxrt.IntFrom([]int64{idBOS}, 1, 1),
		e.decHidden: hidden,
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := argmaxLast(step[e.decLogits])
	if err != nil {
		t.Fatal(err)
	}
	if e.layers < 1 {
		t.Fatal("no decoder layers")
	}
	dk0 := step[e.decPDK[0]]
	if dk0 == nil || dk0.Rank() != 4 || dk0.Shape[2] != 1 {
		t.Fatalf("present decoder key %v", shapeOf(dk0))
	}
	in := map[string]*onnxrt.Tensor{
		e.pastIDs: onnxrt.IntFrom([]int64{tok}, 1, 1),
	}
	for i := 0; i < e.layers; i++ {
		in[e.pastDK[i]] = step[e.decPDK[i]]
		in[e.pastDV[i]] = step[e.decPDV[i]]
		in[e.pastEK[i]] = step[e.decPEK[i]]
		in[e.pastEV[i]] = step[e.decPEV[i]]
	}
	step2, err := e.past.Run(in)
	if err != nil {
		t.Fatal(err)
	}
	dk1 := step2[e.pastPDK[0]]
	if dk1 == nil || dk1.Rank() != 4 || dk1.Shape[2] != 2 {
		t.Fatalf("present decoder key after past step %v", shapeOf(dk1))
	}
	latex, err := e.Recognize(img)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("recognize %q", latex)
}

func shapeOf(t *onnxrt.Tensor) []int {
	if t == nil {
		return nil
	}
	return t.Shape
}

func formulaModelDir() string {
	if d := os.Getenv("FORMULA_MODEL_DIR"); d != "" {
		if formulaDirHasEncoder(d) {
			return d
		}
	}
	for _, c := range []string{
		filepath.Join("..", "..", ".tmp", "formula-models"),
		filepath.Join(".tmp", "formula-models"),
	} {
		if formulaDirHasEncoder(c) {
			return c
		}
	}
	return ""
}

func formulaDirHasEncoder(dir string) bool {
	for _, name := range []string{fileEncoder, legacyEncoder} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.Size() > 1000 {
			return true
		}
	}
	return false
}
