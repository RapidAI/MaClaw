package guiautomation

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// GUIStateObserver implements taskengine.StateObserver for desktop GUI
// applications. It uses the accessibility bridge for structured state
// queries and OCR for visual text recognition.
type GUIStateObserver struct {
	bridge       accessibility.Bridge
	ocr          taskengine.OCRProvider
	screenParser taskengine.ScreenParser
	screenshotFn func() (string, error)
	logger       func(string)
}

// NewGUIStateObserver creates a GUIStateObserver. All dependencies are optional.
func NewGUIStateObserver(
	bridge accessibility.Bridge,
	ocr taskengine.OCRProvider,
	screenshotFn func() (string, error),
	logger func(string),
) *GUIStateObserver {
	return &GUIStateObserver{
		bridge:       bridge,
		ocr:          ocr,
		screenshotFn: screenshotFn,
		logger:       logger,
	}
}

// SetScreenParser injects a ScreenParser (e.g. YOLOScreenParser) for
// vision-based UI element detection. Called after construction because
// the parser may depend on a model file that's loaded lazily.
func (o *GUIStateObserver) SetScreenParser(sp taskengine.ScreenParser) {
	o.screenParser = sp
}

// ScreenParser returns the injected ScreenParser, or nil if none is set.
func (o *GUIStateObserver) ScreenParser() taskengine.ScreenParser {
	return o.screenParser
}

// Snapshot captures the current desktop GUI state. It collects the foreground
// window title, focused element info, and optionally a screenshot + OCR text.
// Errors in individual data sources are silently ignored — the snapshot
// contains whatever was successfully collected.
func (o *GUIStateObserver) Snapshot() (*taskengine.StateSnapshot, error) {
	snap := &taskengine.StateSnapshot{}

	// Screenshot
	if o.screenshotFn != nil {
		if img, err := o.screenshotFn(); err == nil {
			snap.ScreenshotB64 = img
		}
	}

	// ScreenParser: structured UI elements from vision model
	if o.screenParser != nil && o.screenParser.IsAvailable() && snap.ScreenshotB64 != "" {
		if elements, err := o.screenParser.Parse(snap.ScreenshotB64); err == nil {
			snap.UIElements = elements
		}
	}

	// OCR from screenshot
	if o.ocr != nil && o.ocr.IsAvailable() && snap.ScreenshotB64 != "" {
		if results, err := o.ocr.Recognize(snap.ScreenshotB64); err == nil {
			var texts []string
			for _, r := range results {
				texts = append(texts, r.Text)
			}
			snap.OCRText = strings.Join(texts, " ")
		}
	}

	return snap, nil
}

// Verify checks a set of criteria against the current desktop GUI state.
//
// Supported criterion types:
//   - text_contains:     screenshot → OCR → check text contains pattern
//   - ocr_contains:      alias for text_contains (backward compat with GUICriterionSpec)
//   - element_exists:    accessibility bridge → find element by role::name in window
//   - element_value:     find element → check its Value contains pattern
//   - window_exists:     enumerate top-level windows → check title contains pattern
//   - window_title:      alias for window_exists
//   - screenshot_match:  (reserved for Phase 6 visual regression)
func (o *GUIStateObserver) Verify(criteria []taskengine.CriterionSpec) (*taskengine.VerifyResult, error) {
	// Take one screenshot for all criteria that need it, avoiding redundant captures.
	var cachedScreenshot string
	var cachedElements []taskengine.UIElement
	getScreenshot := func() string {
		if cachedScreenshot == "" && o.screenshotFn != nil {
			cachedScreenshot, _ = o.screenshotFn()
		}
		return cachedScreenshot
	}
	getElements := func() []taskengine.UIElement {
		if cachedElements == nil && o.screenParser != nil && o.screenParser.IsAvailable() {
			if img := getScreenshot(); img != "" {
				cachedElements, _ = o.screenParser.Parse(img)
			}
			if cachedElements == nil {
				cachedElements = []taskengine.UIElement{} // mark as attempted
			}
		}
		return cachedElements
	}

	result := &taskengine.VerifyResult{Passed: true}
	// OCR runs at most once per Verify call: every text_contains criterion
	// matches against the same cached screenshot, so repeated Recognize calls
	// would be redundant — and with a vision-LLM-backed provider each one is
	// a full model request.
	var cachedOCR []taskengine.OCRResult
	var ocrAttempted bool
	var ocrErr error
	getOCRResults := func() ([]taskengine.OCRResult, error) {
		if ocrAttempted {
			return cachedOCR, ocrErr
		}
		ocrAttempted = true
		switch {
		case o.ocr == nil || !o.ocr.IsAvailable():
			ocrErr = fmt.Errorf("OCR not available")
		case getScreenshot() == "":
			ocrErr = fmt.Errorf("screenshot not available")
		default:
			cachedOCR, ocrErr = o.ocr.Recognize(cachedScreenshot)
			if ocrErr != nil {
				ocrErr = fmt.Errorf("OCR: %w", ocrErr)
			}
		}
		return cachedOCR, ocrErr
	}
	for _, c := range criteria {
		cr := o.checkOne(c, getScreenshot, getElements, getOCRResults)
		result.Details = append(result.Details, cr)
		if !cr.Passed {
			result.Passed = false
		}
	}
	return result, nil
}

// WaitForStable waits until the desktop GUI appears visually stable.
// It takes periodic screenshots and compares consecutive frames by byte
// length as a fast proxy for visual change. When the length is unchanged
// for 1 second, the GUI is considered stable.
//
// Note: exact base64 string comparison is unreliable because PNG compression
// can produce different bytes for identical images. Byte length comparison
// is a pragmatic heuristic — if the screen content changes, the compressed
// size almost always changes too. For pixel-accurate comparison, a future
// implementation should decode to raw pixels and compare.
func (o *GUIStateObserver) WaitForStable(timeout time.Duration) error {
	if o.screenshotFn == nil {
		return nil // no screenshot capability — assume stable
	}

	deadline := time.Now().Add(timeout)
	prevLen := -1
	stableSince := time.Time{}

	for time.Now().Before(deadline) {
		img, err := o.screenshotFn()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		curLen := len(img)
		if prevLen >= 0 && curLen == prevLen {
			// Same compressed size — screen likely unchanged
			if stableSince.IsZero() {
				stableSince = time.Now()
			} else if time.Since(stableSince) >= time.Second {
				return nil // stable for 1 second
			}
		} else {
			stableSince = time.Time{} // reset
		}

		prevLen = curLen
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("GUI not stable within %v", timeout)
}

// TakeCheckpoint captures a checkpoint after a step completes.
func (o *GUIStateObserver) TakeCheckpoint(stepIndex int) taskengine.Checkpoint {
	cp := taskengine.Checkpoint{
		StepIndex: stepIndex,
		Timestamp: time.Now(),
	}

	if o.screenshotFn != nil {
		if img, err := o.screenshotFn(); err == nil {
			cp.ScreenshotB64 = img
		}
	}

	return cp
}

// ── criterion checks ──

func (o *GUIStateObserver) checkOne(c taskengine.CriterionSpec, getScreenshot func() string, getElements func() []taskengine.UIElement, getOCRResults func() ([]taskengine.OCRResult, error)) taskengine.CriterionResult {
	switch c.Type {
	case "text_contains", "ocr_contains":
		return o.checkTextContains(c, getOCRResults)
	case "element_exists":
		return o.checkElementExists(c, getElements)
	case "element_value":
		return o.checkElementValue(c)
	case "window_exists", "window_title":
		return o.checkWindowExists(c)
	default:
		return taskengine.CriterionResult{
			Criterion: c,
			Passed:    false,
			Error:     fmt.Sprintf("unsupported desktop criterion type: %s", c.Type),
		}
	}
}

// checkTextContains: screenshot → OCR → check text contains pattern.
// OCR results come from the caller's per-Verify cache so multiple
// text_contains criteria share a single recognition pass.
func (o *GUIStateObserver) checkTextContains(c taskengine.CriterionSpec, getOCRResults func() ([]taskengine.OCRResult, error)) taskengine.CriterionResult {
	results, err := getOCRResults()
	if err != nil {
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: err.Error()}
	}

	var allText []string
	for _, r := range results {
		if strings.Contains(r.Text, c.Pattern) {
			return taskengine.CriterionResult{Criterion: c, Passed: true, Actual: r.Text}
		}
		allText = append(allText, r.Text)
	}

	return taskengine.CriterionResult{
		Criterion: c,
		Passed:    false,
		Actual:    truncateStr(strings.Join(allText, " | "), 300),
		Error:     fmt.Sprintf("OCR text does not contain %q", c.Pattern),
	}
}

// checkElementExists: accessibility bridge → find element by role::name.
// Selector format: "role::name" (e.g. "Button::确定", "Edit::用户名").
// If Selector is empty, uses Pattern as the element name with any role.
func (o *GUIStateObserver) checkElementExists(c taskengine.CriterionSpec, getElements func() []taskengine.UIElement) taskengine.CriterionResult {
	// Tier 1: Accessibility bridge
	if o.bridge != nil {
		role, name := parseSelector(c.Selector, c.Pattern)
		el, err := o.bridge.FindElement(c.Window, role, name)
		if err == nil && el != nil {
			return taskengine.CriterionResult{
				Criterion: c,
				Passed:    true,
				Actual:    fmt.Sprintf("%s::%s at (%d,%d)", el.Role, el.Name, el.Bounds.X, el.Bounds.Y),
			}
		}
	}

	// Tier 2: ScreenParser (vision-based fallback, uses cached elements)
	// Note: YOLO detects interactable regions by bounding box, not by name.
	// For element_exists with a specific name, YOLO can only confirm that
	// an interactable region exists at approximately the right location.
	// For element_exists without a name (just checking "any element exists"),
	// YOLO provides useful results.
	elements := getElements()
	if len(elements) > 0 {
		_, name := parseSelector(c.Selector, c.Pattern)
		if name == "" {
			// No specific name — any detected element counts
			el := elements[0]
			return taskengine.CriterionResult{
				Criterion: c,
				Passed:    true,
				Actual:    fmt.Sprintf("%d interactable elements detected [%s]", len(elements), el.Source),
			}
		}
		// With a specific name — can't match by name from YOLO, fall through to fail
	}

	role, name := parseSelector(c.Selector, c.Pattern)
	return taskengine.CriterionResult{
		Criterion: c,
		Passed:    false,
		Error:     fmt.Sprintf("element %s::%s not found in window %q", role, name, c.Window),
	}
}

// checkElementValue: find element → check its Value contains pattern.
func (o *GUIStateObserver) checkElementValue(c taskengine.CriterionSpec) taskengine.CriterionResult {
	if o.bridge == nil {
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: "accessibility bridge not available"}
	}

	role, name := parseSelector(c.Selector, "")
	window := c.Window

	el, err := o.bridge.FindElement(window, role, name)
	if err != nil || el == nil {
		errMsg := "element not found"
		if err != nil {
			errMsg = err.Error()
		}
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: errMsg}
	}

	val, err := o.bridge.GetValue(el)
	if err != nil {
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("get value: %v", err)}
	}

	if c.Pattern == "" || strings.Contains(val, c.Pattern) {
		return taskengine.CriterionResult{Criterion: c, Passed: true, Actual: truncateStr(val, 200)}
	}

	return taskengine.CriterionResult{
		Criterion: c,
		Passed:    false,
		Actual:    truncateStr(val, 200),
		Error:     fmt.Sprintf("element value does not contain %q", c.Pattern),
	}
}

// checkWindowExists: enumerate top-level windows → check title contains pattern.
func (o *GUIStateObserver) checkWindowExists(c taskengine.CriterionSpec) taskengine.CriterionResult {
	if o.bridge == nil {
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: "accessibility bridge not available"}
	}

	pattern := c.Pattern
	if pattern == "" {
		pattern = c.Window
	}

	// EnumElements with empty title returns top-level windows on some implementations.
	// We try the pattern as a window title directly via FindElement with empty role/name.
	// If that fails, we try EnumElements("") and scan titles.
	elements, err := o.bridge.EnumElements(pattern)
	if err == nil && len(elements) > 0 {
		return taskengine.CriterionResult{
			Criterion: c,
			Passed:    true,
			Actual:    elements[0].Name,
		}
	}

	// Fallback: enumerate all top-level windows
	allElements, err := o.bridge.EnumElements("")
	if err != nil {
		return taskengine.CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("enum windows: %v", err)}
	}

	for _, el := range allElements {
		if strings.Contains(el.Name, pattern) {
			return taskengine.CriterionResult{Criterion: c, Passed: true, Actual: el.Name}
		}
	}

	return taskengine.CriterionResult{
		Criterion: c,
		Passed:    false,
		Error:     fmt.Sprintf("no window with title containing %q", pattern),
	}
}

// ── helpers ──

// parseSelector parses "role::name" format. If no "::", role is empty
// and the entire string is treated as name. If selector is empty,
// fallbackName is used as name.
func parseSelector(selector, fallbackName string) (role, name string) {
	if selector == "" {
		return "", fallbackName
	}
	parts := strings.SplitN(selector, "::", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", selector
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Compile-time interface check.
var _ taskengine.StateObserver = (*GUIStateObserver)(nil)
