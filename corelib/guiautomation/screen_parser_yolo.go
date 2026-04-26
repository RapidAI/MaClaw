package guiautomation

import (
	"fmt"
	"os"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
	"github.com/RapidAI/CodeClaw/corelib/yolo"
)

const defaultYOLOUnloadDelay = 3 * time.Minute

// YOLOScreenParser implements taskengine.ScreenParser using the pure Go
// YOLOv8 inference engine. It detects interactable UI elements (buttons,
// icons, inputs, etc.) from screenshots without any external dependency.
//
// The model is lazily loaded on first Parse() call and automatically
// unloaded after an idle timeout (default 3 minutes) to free memory.
// Subsequent Parse() calls after unload will reload the model transparently.
type YOLOScreenParser struct {
	mm         *modelmanager.Manager[*yolo.Model]
	modelPath  string
	confThresh float32
	iouThresh  float32
}

// NewYOLOScreenParser creates a YOLO-based screen parser.
// The model is lazily loaded on first Parse() call and auto-unloaded after idle.
// modelPath: path to the .yolow weight file.
func NewYOLOScreenParser(modelPath string, confThresh, iouThresh float32) *YOLOScreenParser {
	if confThresh <= 0 {
		confThresh = 0.3
	}
	if iouThresh <= 0 {
		iouThresh = 0.5
	}
	mm := modelmanager.New(modelmanager.Config[*yolo.Model]{
		Name:        "yolo",
		Load:        func() (*yolo.Model, error) { return yolo.LoadModel(modelPath) },
		Close:       nil, // no explicit cleanup — GC reclaims float32 weights
		UnloadDelay: defaultYOLOUnloadDelay,
	})
	return &YOLOScreenParser{
		mm:         mm,
		modelPath:  modelPath,
		confThresh: confThresh,
		iouThresh:  iouThresh,
	}
}

// SetUnloadDelay configures idle timeout before the model is unloaded from memory.
func (p *YOLOScreenParser) SetUnloadDelay(d time.Duration) {
	p.mm.SetUnloadDelay(d)
}

// Unload releases the model from memory. The model will be reloaded on next Parse().
func (p *YOLOScreenParser) Unload() {
	p.mm.Unload()
}

// Loaded returns true if the model is currently in memory.
func (p *YOLOScreenParser) Loaded() bool {
	return p.mm.Loaded()
}

// Parse implements taskengine.ScreenParser.
// Loads the model on demand and schedules auto-unload after idle timeout.
func (p *YOLOScreenParser) Parse(pngBase64 string) ([]taskengine.UIElement, error) {
	model, done, err := p.mm.Acquire()
	if err != nil {
		return nil, err
	}
	defer done()

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
	if p.mm.Loaded() {
		return true
	}
	_, err := os.Stat(p.modelPath)
	return err == nil
}

// Compile-time interface check.
var _ taskengine.ScreenParser = (*YOLOScreenParser)(nil)
