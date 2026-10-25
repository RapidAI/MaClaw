package tablestruct

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
	xdraw "golang.org/x/image/draw"
)

const (
	longSide   = 1024
	modelFile  = "unet.onnx"
	modelBytes = 8335007
)

// ImageNet normalization used by the wired UNET. The source image is RGB.
var (
	mean = [3]float32{123.675, 116.28, 103.53}
	std  = [3]float32{58.395, 57.12, 57.375}
)

// Engine runs the wired table UNET and turns its line mask into HTML.
type Engine struct {
	graph *onnxrt.Graph
	input string
}

var (
	sharedMu   sync.Mutex
	shared     *Engine
	sharedPath string
)

// ModelPath is <Maclaw base>/models/table/unet.onnx.
func ModelPath() (string, error) {
	base := strings.TrimSpace(maclawpath.BaseDir())
	if base == "" {
		return "", fmt.Errorf("tablestruct: maclaw base dir unavailable")
	}
	return filepath.Join(base, "models", "table", modelFile), nil
}

// Available reports that the pinned UNET file is on disk.
func Available() bool {
	path, err := ModelPath()
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() == modelBytes
}

// Shared returns the process engine when the model file is present.
func Shared() (*Engine, error) {
	path, err := ModelPath()
	if err != nil {
		return nil, err
	}
	if err := verifyFile(path); err != nil {
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

// Open loads the wired UNET graph.
func Open(path string) (*Engine, error) {
	g, err := onnxrt.LoadGraph(path)
	if err != nil {
		return nil, err
	}
	if len(g.InputNames()) != 1 || len(g.OutputNames()) != 1 {
		return nil, fmt.Errorf("tablestruct: inputs %v outputs %v", g.InputNames(), g.OutputNames())
	}
	return &Engine{graph: g, input: g.InputNames()[0]}, nil
}

// HTML rescales the crop so its long side is 1024, runs the UNET, and
// fills a grid with lines. An empty string means the mask did not yield
// a ruled grid.
func (e *Engine) HTML(img image.Image, lines []Line) (string, error) {
	if e == nil || e.graph == nil {
		return "", fmt.Errorf("tablestruct: engine is not open")
	}
	if img == nil {
		return "", fmt.Errorf("tablestruct: no image")
	}
	b := img.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if origW < 2 || origH < 2 {
		return "", nil
	}
	maskW, maskH := fitLongSide(origW, origH, longSide)
	canvas := image.NewRGBA(image.Rect(0, 0, maskW, maskH))
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), img, b, xdraw.Src, nil)
	in := onnxrt.NewFloat(1, 3, maskH, maskW)
	plane := maskH * maskW
	for y := 0; y < maskH; y++ {
		row := canvas.Pix[y*canvas.Stride : y*canvas.Stride+maskW*4]
		for x := 0; x < maskW; x++ {
			dst := y*maskW + x
			in.F32[dst] = (float32(row[x*4]) - mean[0]) / std[0]
			in.F32[plane+dst] = (float32(row[x*4+1]) - mean[1]) / std[1]
			in.F32[2*plane+dst] = (float32(row[x*4+2]) - mean[2]) / std[2]
		}
	}
	outs, err := e.graph.Run(map[string]*onnxrt.Tensor{e.input: in})
	if err != nil {
		return "", err
	}
	mask, mw, mh, err := mask2D(outs[e.graph.OutputNames()[0]])
	if err != nil {
		return "", err
	}
	minH := mw / 8
	minV := mh / 8
	if minH < 8 {
		minH = 8
	}
	if minV < 8 {
		minV = 8
	}
	rows, cols := Separators(mask, mw, mh, minH, minV)
	scaleX := float64(origW) / float64(mw)
	scaleY := float64(origH) / float64(mh)
	for i, y := range rows {
		rows[i] = int(math.Round(float64(y) * scaleY))
	}
	for i, x := range cols {
		cols[i] = int(math.Round(float64(x) * scaleX))
	}
	return HTML(rows, cols, lines), nil
}

func fitLongSide(w, h, long int) (int, int) {
	if w >= h {
		nh := int(math.Round(float64(h) * float64(long) / float64(w)))
		if nh < 1 {
			nh = 1
		}
		return long, nh
	}
	nw := int(math.Round(float64(w) * float64(long) / float64(h)))
	if nw < 1 {
		nw = 1
	}
	return nw, long
}

func mask2D(t *onnxrt.Tensor) ([]int, int, int, error) {
	if t == nil || t.DType != onnxrt.DInt64 || len(t.Shape) < 2 {
		if t == nil {
			return nil, 0, 0, fmt.Errorf("tablestruct: no mask")
		}
		return nil, 0, 0, fmt.Errorf("tablestruct: mask shape %v", t.Shape)
	}
	shape := append([]int(nil), t.Shape...)
	for len(shape) > 2 && shape[0] == 1 {
		shape = shape[1:]
	}
	if len(shape) != 2 || shape[0] < 1 || shape[1] < 1 {
		return nil, 0, 0, fmt.Errorf("tablestruct: mask shape %v", t.Shape)
	}
	h, w := shape[0], shape[1]
	if len(t.I64) < h*w {
		return nil, 0, 0, fmt.Errorf("tablestruct: mask len %d", len(t.I64))
	}
	base := len(t.I64) - h*w
	out := make([]int, h*w)
	for i := range out {
		out[i] = int(t.I64[base+i])
	}
	return out, w, h, nil
}
