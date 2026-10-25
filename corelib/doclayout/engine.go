package doclayout

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
	xdraw "golang.org/x/image/draw"
)

const (
	inputSide      = 800
	scoreThreshold = 0.5
	modelFile      = "PP-DocLayoutV3.onnx"
	modelBytes     = 5088167
	weightBytes    = 136774480
)

// Engine runs PP-DocLayoutV3. The graph file and its sibling .onnx.data
// stay in the model directory. Missing files leave the caller on the
// searchable-PDF path.
type Engine struct {
	graph *onnxrt.Graph
	input string
}

var (
	sharedMu   sync.Mutex
	shared     *Engine
	sharedPath string
)

// ModelPath is <Maclaw base>/models/layout/PP-DocLayoutV3.onnx.
func ModelPath() (string, error) {
	base := strings.TrimSpace(maclawpath.BaseDir())
	if base == "" {
		return "", fmt.Errorf("doclayout: maclaw base dir unavailable")
	}
	return filepath.Join(base, "models", "layout", modelFile), nil
}

// Available reports that the pinned graph and external weights are on disk.
func Available() bool {
	path, err := ModelPath()
	return err == nil && ready(path)
}

func ready(path string) bool {
	if sizeOf(path) != modelBytes {
		return false
	}
	return sizeOf(path+".data") == weightBytes
}

func sizeOf(path string) int64 {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0
	}
	return st.Size()
}

// Shared returns the process engine when the model files are present.
func Shared() (*Engine, error) {
	path, err := ModelPath()
	if err != nil {
		return nil, err
	}
	if !ready(path) {
		return nil, os.ErrNotExist
	}
	if err := verifyFiles(path); err != nil {
		sharedMu.Lock()
		shared = nil
		sharedPath = ""
		sharedMu.Unlock()
		return nil, err
	}
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if shared != nil && sharedPath == path {
		return shared, nil
	}
	eng, err := Open(path)
	if err != nil {
		return nil, err
	}
	shared = eng
	sharedPath = path
	return eng, nil
}

// Open loads a PP-DocLayoutV3 graph. path is the .onnx file; the weights
// file must sit beside it.
func Open(path string) (*Engine, error) {
	g, err := onnxrt.LoadGraph(path)
	if err != nil {
		return nil, err
	}
	if len(g.InputNames()) != 1 || len(g.OutputNames()) < 3 {
		return nil, fmt.Errorf("doclayout: inputs %v outputs %v", g.InputNames(), g.OutputNames())
	}
	return &Engine{graph: g, input: g.InputNames()[0]}, nil
}

// Analyze stretches img to 800×800, scales pixels by 1/255, and decodes
// reading order. The model card uses mean 0 and std 1 after that rescale.
func (e *Engine) Analyze(img image.Image) ([]Region, error) {
	if e == nil || e.graph == nil {
		return nil, fmt.Errorf("doclayout: engine is not open")
	}
	if img == nil {
		return nil, fmt.Errorf("doclayout: no image")
	}
	b := img.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if origW < 1 || origH < 1 {
		return nil, fmt.Errorf("doclayout: image size %dx%d", origW, origH)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, inputSide, inputSide))
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), img, b, xdraw.Src, nil)
	in := onnxrt.NewFloat(1, 3, inputSide, inputSide)
	plane := inputSide * inputSide
	for y := 0; y < inputSide; y++ {
		row := canvas.Pix[y*canvas.Stride : y*canvas.Stride+inputSide*4]
		for x := 0; x < inputSide; x++ {
			dst := y*inputSide + x
			in.F32[dst] = float32(row[x*4]) / 255
			in.F32[plane+dst] = float32(row[x*4+1]) / 255
			in.F32[2*plane+dst] = float32(row[x*4+2]) / 255
		}
	}
	outs, err := e.graph.Run(map[string]*onnxrt.Tensor{e.input: in})
	if err != nil {
		return nil, err
	}
	logits, boxes, order, err := layoutOutputs(outs)
	if err != nil {
		return nil, err
	}
	return Decode(logits, boxes, order, origW, origH, scoreThreshold)
}

func layoutOutputs(outs map[string]*onnxrt.Tensor) (logits, boxes, order *onnxrt.Tensor, err error) {
	for _, t := range outs {
		if t == nil || t.DType != onnxrt.DFloat32 || len(t.Shape) < 2 {
			continue
		}
		switch t.Shape[len(t.Shape)-1] {
		case len(Labels):
			logits = t
		case 4:
			boxes = t
		}
	}
	if logits == nil || boxes == nil {
		return nil, nil, nil, fmt.Errorf("doclayout: outputs missing logits or boxes")
	}
	queries := logits.Shape[len(logits.Shape)-2]
	for _, t := range outs {
		if t == nil || len(t.Shape) < 2 {
			continue
		}
		if t.Shape[len(t.Shape)-1] == queries && t.Shape[len(t.Shape)-2] == queries {
			order = t
		}
	}
	if order == nil {
		return nil, nil, nil, fmt.Errorf("doclayout: outputs missing order")
	}
	return logits, boxes, order, nil
}
