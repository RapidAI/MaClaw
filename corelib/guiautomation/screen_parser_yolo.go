package guiautomation

import (
	"fmt"
	"os"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
	"github.com/RapidAI/CodeClaw/corelib/yolo"
)

// YOLOScreenParser implements taskengine.ScreenParser using the pure Go
// YOLOv8 inference engine. It detects interactable UI elements (buttons,
// icons, inputs, etc.) from screenshots without any external dependency.
type YOLOScreenParser struct {
	mu         sync.Mutex
	model      *yolo.Model
	modelPath  string
	confThresh float32
	iouThresh  float32
	loaded     bool
	loadErr    error
}

// NewYOLOScreenParser creates a YOLO-based screen parser.
// The model is lazily loaded on first Parse() call.
// modelPath: path to the .yolow weight file.
func NewYOLOScreenParser(modelPath string, confThresh, iouThresh float32) *YOLOScreenParser {
	if confThresh <= 0 {
		confThresh = 0.3
	}
	if iouThresh <= 0 {
		iouThresh = 0.5
	}
	return &YOLOScreenParser{
		modelPath:  modelPath,
		confThresh: confThresh,
		iouThresh:  iouThresh,
	}
}

// Parse implements taskengine.ScreenParser.
func (p *YOLOScreenParser) Parse(pngBase64 string) ([]taskengine.UIElement, error) {
	p.mu.Lock()
	if !p.loaded {
		p.model, p.loadErr = yolo.LoadModel(p.modelPath)
		p.loaded = true
	}
	model := p.model
	loadErr := p.loadErr
	p.mu.Unlock()

	if loadErr != nil {
		return nil, fmt.Errorf("YOLO model load: %w", loadErr)
	}

	dets, err := model.Detect(pngBase64, p.confThresh, p.iouThresh)
	if err != nil {
		return nil, fmt.Errorf("YOLO detect: %w", err)
	}

	elements := make([]taskengine.UIElement, len(dets))
	for i, d := range dets {
		elements[i] = taskengine.UIElement{
			Type:         "interactable", // OmniParser YOLO is single-class: "interactable region"
			Name:         fmt.Sprintf("element_%d", i),
			BBox:         [4]int{d.X, d.Y, d.W, d.H},
			Interactable: true,
			Confidence:   float64(d.Confidence),
			Source:       "yolo",
		}
	}
	return elements, nil
}

// IsAvailable implements taskengine.ScreenParser.
func (p *YOLOScreenParser) IsAvailable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.loadErr == nil
	}
	_, err := os.Stat(p.modelPath)
	return err == nil
}

// Compile-time interface check.
var _ taskengine.ScreenParser = (*YOLOScreenParser)(nil)
