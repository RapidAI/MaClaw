// Package computeruse implements a text-primary Computer Use session:
// local OmniParser (YOLO) + OCR (+ optional accessibility) turn the screen
// into a set-of-marks description for text-only LLMs. Multimodal vision is
// optional and never required for the core loop.
package computeruse

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// ObserveMode controls what is returned to the LLM.
type ObserveMode string

const (
	// ObserveTextPrimary returns only structured text (default for non-vision models).
	ObserveTextPrimary ObserveMode = "text_primary"
	// ObserveVisionAssist may attach annotated/low-res images when the endpoint supports vision.
	ObserveVisionAssist ObserveMode = "vision_assist"
)

// MarkedElement is one SoM entry exposed to the model as ref eN.
type MarkedElement struct {
	Ref          string  `json:"ref"` // e0, e1, ...
	Type         string  `json:"type"`
	Name         string  `json:"name"`
	Value        string  `json:"value,omitempty"`
	BBox         [4]int  `json:"bbox"` // x,y,w,h
	CenterX      int     `json:"center_x"`
	CenterY      int     `json:"center_y"`
	Confidence   float64 `json:"confidence"`
	Source       string  `json:"source"`
	Interactable bool    `json:"interactable"`
	// Handle is a platform locator (UIA AutomationId, AX path). Empty means
	// actuation falls back to the element's screen-space center.
	Handle string `json:"handle,omitempty"`
	// Patterns lists known accessibility patterns (invoke, value, toggle, …).
	Patterns []string `json:"patterns,omitempty"`
	// Window is the title of the top-level window owning this element's center
	// ("" when unknown). Policy metadata — kept out of TextForModel.
	Window string `json:"window,omitempty"`
}

// ScreenMeta describes the coordinate space of the last capture.
type ScreenMeta struct {
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	ScaleFactor float64 `json:"scale_factor"` // 1.0 when unknown
	ScreenIndex int     `json:"screen_index"` // -1 = all/stitched
	OriginX     int     `json:"origin_x"`
	OriginY     int     `json:"origin_y"`
	// VisionWidth/Height are the pixel size of the screenshot sent to a vision
	// model. Zero means the model image matches Width/Height (no resize).
	VisionWidth  int `json:"vision_width,omitempty"`
	VisionHeight int `json:"vision_height,omitempty"`
	// CropTitle is set when observe captured a single top-level window instead
	// of a full monitor. Empty means the capture is a monitor/desktop region.
	CropTitle string `json:"crop_title,omitempty"`
}

// ObserveResult is the structured outcome of one observation step.
// TextForModel is what goes into the tool result for the LLM.
type ObserveResult struct {
	Mode         ObserveMode     `json:"mode"`
	Meta         ScreenMeta      `json:"meta"`
	Windows      []string        `json:"windows,omitempty"`
	Elements     []MarkedElement `json:"elements"`
	OCRExcerpt   string          `json:"ocr_excerpt,omitempty"`
	TextForModel string          `json:"-"`
	// ScreenshotB64 is kept in-session only; never dump into text-primary tool results.
	ScreenshotB64 string `json:"-"`
	// OCRLines keeps the raw OCR results (with boxes) in-session so text search
	// (computer_find) can synthesize clickable targets for text no element covers.
	OCRLines   []taskengine.OCRResult `json:"-"`
	ObservedAt time.Time              `json:"observed_at"`
}

// ActionRecord is an audit entry for one computer action.
type ActionRecord struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
}

// Config holds session limits and policy defaults.
type Config struct {
	Mode              ObserveMode
	MaxSteps          int
	MaxScreenshots    int
	TargetApps        []string // window title substrings; empty = allow all (MVP)
	AllowPixelClick   bool     // default false for text models
	OCRMaxChars       int      // ocr_excerpt cap
	ElementsMaxInText int      // max elements listed in TextForModel
}

// DefaultConfig returns safe MVP defaults.
func DefaultConfig() Config {
	return Config{
		Mode:              ObserveTextPrimary,
		MaxSteps:          40,
		MaxScreenshots:    20,
		AllowPixelClick:   false,
		OCRMaxChars:       2000,
		ElementsMaxInText: 80,
	}
}

// FromUIElements converts taskengine detections into a working slice (copy).
func FromUIElements(els []taskengine.UIElement) []taskengine.UIElement {
	if len(els) == 0 {
		return nil
	}
	out := make([]taskengine.UIElement, len(els))
	copy(out, els)
	return out
}
