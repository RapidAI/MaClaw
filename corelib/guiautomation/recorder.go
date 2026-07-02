package guiautomation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/accessibility"
)

// GUIRecorder records GUI operations with three-tier locator information.
type GUIRecorder struct {
	mu           sync.Mutex
	bridge       accessibility.Bridge
	screenshotFn func() (string, error)
	recording    bool
	steps        []GUIRecordedStep
	startTime    time.Time
	flowDir      string // <MaclawBaseDir>/gui_flows/
}

// NewGUIRecorder creates a GUIRecorder.
func NewGUIRecorder(bridge accessibility.Bridge, screenshotFn func() (string, error)) *GUIRecorder {
	return &GUIRecorder{
		bridge:       bridge,
		screenshotFn: screenshotFn,
		flowDir:      filepath.Join(corelib.MaclawBaseDir(), "gui_flows"),
	}
}

// Start enters recording mode.
func (r *GUIRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("already recording")
	}

	r.recording = true
	r.startTime = time.Now()
	r.steps = nil
	return nil
}

// RecordStep records a single GUI operation step with all three locator tiers.
// Accessibility and screenshot failures are silently ignored — coords are always recorded.
func (r *GUIRecorder) RecordStep(action, windowTitle string, coords [2]int, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return fmt.Errorf("not recording")
	}

	step := GUIRecordedStep{
		Action:      action,
		WindowTitle: windowTitle,
		Timestamp:   time.Since(r.startTime),
		Coords:      coords,
		Text:        text,
	}

	// Tier 1: Accessibility — fail silently
	if r.bridge != nil {
		elements, err := r.bridge.EnumElements(windowTitle)
		if err == nil && len(elements) > 0 {
			if best := findNearestElement(elements, coords[0], coords[1]); best != nil {
				step.AccessibilityID = &AccessibilityRef{
					Role:  best.Role,
					Name:  best.Name,
					Value: best.Value,
				}
			}
		}
	}

	// Tier 2: Screenshot — fail silently
	if r.screenshotFn != nil {
		if imgB64, err := r.screenshotFn(); err == nil {
			step.SnapshotRef = imgB64
		}
	}

	r.steps = append(r.steps, step)
	return nil
}

// Stop stops recording and saves the flow to disk.
func (r *GUIRecorder) Stop(name, description string) (*GUIRecordedFlow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil, fmt.Errorf("not recording")
	}

	r.recording = false

	flow := &GUIRecordedFlow{
		Name:        name,
		Description: description,
		RecordedAt:  r.startTime,
		Steps:       r.steps,
	}

	if err := os.MkdirAll(r.flowDir, 0o755); err != nil {
		return flow, fmt.Errorf("create flow dir: %w", err)
	}

	if err := SaveFlow(flow, r.flowDir); err != nil {
		return flow, fmt.Errorf("save flow: %w", err)
	}

	r.steps = nil
	return flow, nil
}

// ListFlows returns all saved GUI flows.
func (r *GUIRecorder) ListFlows() ([]GUIRecordedFlow, error) {
	entries, err := os.ReadDir(r.flowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var flows []GUIRecordedFlow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		flow, err := LoadFlow(r.flowDir, e.Name())
		if err != nil {
			continue
		}
		flows = append(flows, *flow)
	}
	return flows, nil
}

// LoadFlow loads a specific GUI flow by name.
func (r *GUIRecorder) LoadFlow(name string) (*GUIRecordedFlow, error) {
	return LoadFlow(r.flowDir, name)
}

// findNearestElement finds the element whose center is closest to (x, y).
func findNearestElement(elements []accessibility.Element, x, y int) *accessibility.Element {
	var best *accessibility.Element
	bestDist := int(^uint(0) >> 1) // max int

	var walk func(els []accessibility.Element)
	walk = func(els []accessibility.Element) {
		for i := range els {
			el := &els[i]
			cx := el.Bounds.X + el.Bounds.Width/2
			cy := el.Bounds.Y + el.Bounds.Height/2
			dx := cx - x
			dy := cy - y
			dist := dx*dx + dy*dy
			if dist < bestDist {
				bestDist = dist
				best = el
			}
			if len(el.Children) > 0 {
				walk(el.Children)
			}
		}
	}
	walk(elements)
	return best
}
