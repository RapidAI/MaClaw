package ocr

import (
	"fmt"
	"image"
	"math"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

// Result is one recognized text region in original image coordinates.
type Result struct {
	Text       string    `json:"text"`
	Confidence float64   `json:"confidence"`
	BBox       [4]int    `json:"bbox"` // x, y, width, height (axis-aligned envelope)
	Box        [4][2]int `json:"box"`  // detected quad corners (TL, TR, BR, BL)
}

// Engine runs PP-OCRv6 detection + recognition natively via onnxrt.
// It is safe for concurrent use; calls are serialized internally.
type Engine struct {
	det *onnxrt.Graph
	rec *onnxrt.Graph
	mu  sync.Mutex
	sc  engineScratch // guarded by mu; big pooled buffers, dropped by Close
}

// NewEngine loads the det and rec ONNX models.
func NewEngine(detPath, recPath string) (*Engine, error) {
	det, err := onnxrt.LoadGraph(detPath)
	if err != nil {
		return nil, fmt.Errorf("ocr: load det model: %w", err)
	}
	rec, err := onnxrt.LoadGraph(recPath)
	if err != nil {
		return nil, fmt.Errorf("ocr: load rec model: %w", err)
	}
	return &Engine{det: det, rec: rec}, nil
}

// Recognize runs the full det -> crop -> rec pipeline on img and returns
// results in reading order (top-to-bottom, left-to-right).
func (e *Engine) Recognize(img image.Image) ([]Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.det == nil || e.rec == nil {
		return nil, fmt.Errorf("ocr: engine is closed")
	}

	src := toRGBAS(img, &e.sc)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w == 0 || h == 0 {
		return nil, nil
	}

	input, _, _ := detPreprocessS(src, &e.sc)
	outputs, err := e.det.Run(map[string]*onnxrt.Tensor{"x": input})
	if err != nil {
		return nil, fmt.Errorf("ocr: det run: %w", err)
	}
	var prob *onnxrt.Tensor
	for _, name := range e.det.OutputNames() {
		if t := outputs[name]; t != nil && t.Rank() == 4 && t.Shape[0] == 1 && t.Shape[1] == 1 {
			prob = t
			break
		}
	}
	if prob == nil {
		return nil, fmt.Errorf("ocr: det output tensor not found")
	}
	ph, pw := prob.Shape[2], prob.Shape[3]

	boxes := dbPostProcessS(prob.F32, pw, ph, w, h, &e.sc)
	sortBoxesReadingOrder(boxes)

	// Group by output-frame bucket. Samples retain their exact resized pixels;
	// only the final (<8px) right edge is padded before batched inference.
	byWidth := make(map[int][]int, len(boxes))
	exactWidth := make([]int, len(boxes))
	for i, box := range boxes {
		rw := recWidthForBox(box.Points)
		exactWidth[i] = rw
		bucket := (rw + 7) &^ 7
		if bucket > recMaxWidth {
			bucket = recMaxWidth
		}
		byWidth[bucket] = append(byWidth[bucket], i)
	}
	texts := make([]string, len(boxes))
	confs := make([]float64, len(boxes))
	for bucket, idxs := range byWidth {
		for len(idxs) > 0 {
			n := len(idxs)
			if n > recBatchMaxLines {
				n = recBatchMaxLines
			}
			if err := recognizeBatchPadded(e.rec, src, boxes, exactWidth, idxs[:n], bucket, &e.sc, texts, confs); err != nil {
				return nil, err
			}
			idxs = idxs[n:]
		}
	}

	results := make([]Result, 0, len(boxes))
	for i, box := range boxes {
		r := Result{Text: texts[i], Confidence: confs[i]}
		minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
		maxX, maxY := float32(0), float32(0)
		for j := 0; j < 4; j++ {
			px, py := box.Points[j][0], box.Points[j][1]
			r.Box[j] = [2]int{int(px + 0.5), int(py + 0.5)}
			minX = min(minX, px)
			minY = min(minY, py)
			maxX = max(maxX, px)
			maxY = max(maxY, py)
		}
		r.BBox = [4]int{int(minX + 0.5), int(minY + 0.5), int(maxX - minX + 0.5), int(maxY - minY + 0.5)}
		results = append(results, r)
	}
	return results, nil
}

// Close releases the engine. onnxrt graphs are pure Go, so this drops the
// references for the GC; the pooled scratch (det/rec planes, RGBA targets,
// contour grids) is released too so a Manager idle-unload frees it.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.det = nil
	e.rec = nil
	e.sc.release()
}
