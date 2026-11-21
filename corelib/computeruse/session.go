package computeruse

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// Session holds Computer Use state for one agent conversation/tab.
type Session struct {
	mu sync.Mutex

	cfg    Config
	policy *Policy

	// windowResolver attributes elements to their owning window title at
	// observe time (injected by the host; nil disables attribution).
	windowResolver func(x, y int) string
	// windowIDResolver is the stable id for that same point (Windows HWND).
	// Nil leaves WindowID zero and the title comparison stays in force.
	windowIDResolver func(x, y int) uint64

	// Last observation (ref table valid until next action or new observe).
	last      *ObserveResult
	refsValid bool
	// labelsCurrent is false after a lightweight refresh until OCR or a
	// full observe replaces the boxes. Pay checks ignore the old boxes.
	labelsCurrent bool
	// cropWindowID is the cropped window from the last full observe.
	// A refresh drops elements, and the stale-document check still needs it.
	cropWindowID uint64
	screenshotN  int
	// actuated is the control the agent last clicked or typed into.
	// Observe does not clear it: a later screenshot does not move the caret.
	// actuatedWindow is the window that received that click. A key goes to the
	// foreground window, so a control from another window is not the caret.
	// actuatedWindowID is that window's stable id. Zero falls back to the title.
	actuated         string
	actuatedWindow   string
	actuatedWindowID uint64
	actuatedKind     string

	audit []ActionRecord
}

// NewSession creates a Computer Use session.
func NewSession(cfg Config) *Session {
	if cfg.Mode == "" {
		cfg.Mode = ObserveTextPrimary
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = DefaultConfig().MaxSteps
	}
	if cfg.OCRMaxChars <= 0 {
		cfg.OCRMaxChars = DefaultConfig().OCRMaxChars
	}
	if cfg.ElementsMaxInText <= 0 {
		cfg.ElementsMaxInText = DefaultConfig().ElementsMaxInText
	}
	return &Session{
		cfg:    cfg,
		policy: NewPolicy(cfg),
	}
}

// Config returns a copy of the session config.
func (s *Session) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetMode updates observe mode (text_primary vs vision_assist).
func (s *Session) SetMode(mode ObserveMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode != "" {
		s.cfg.Mode = mode
	}
}

// SetAllowPixelClick enables raw x,y clicks (required for vision models that
// see the screenshot and click in image pixel space).
func (s *Session) SetAllowPixelClick(allow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.AllowPixelClick = allow
	if s.policy != nil {
		s.policy.SetAllowPixelClick(allow)
	}
}

// ApplyPerceptionMode switches between LLM-vision and OmniParser/OCR observe.
func (s *Session) ApplyPerceptionMode(vision bool) {
	if vision {
		s.SetMode(ObserveVisionAssist)
		s.SetAllowPixelClick(true)
		return
	}
	s.SetMode(ObserveTextPrimary)
	s.SetAllowPixelClick(false)
}

// MapVisionClick maps coordinates from the screenshot sent to a vision model
// into capture/screen space used by InputSimulator.
func (s *Session) MapVisionClick(x, y int) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return x, y
	}
	return MapVisionClick(s.last.Meta, x, y)
}

// SetTargetApps updates the allowlist (empty = all non-blocked).
func (s *Session) SetTargetApps(apps []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.TargetApps = append([]string(nil), apps...)
	if s.policy != nil {
		s.policy.TargetApps = append([]string(nil), apps...)
	}
}

// SetWindowResolver injects a point→window-title resolver used to attribute
// each observed element to its owning top-level window (for click policy).
func (s *Session) SetWindowResolver(fn func(x, y int) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.windowResolver = fn
}

// SetWindowIDResolver injects a point→window-id resolver. The id, not the
// title, decides which window a later key talks to.
func (s *Session) SetWindowIDResolver(fn func(x, y int) uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.windowIDResolver = fn
}

// Policy exposes pause/resume controls.
func (s *Session) Policy() *Policy {
	return s.policy
}

// Pause soft-blocks interact tools (observe still allowed).
func (s *Session) Pause() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy.Pause()
	s.audit = append(s.audit, ActionRecord{At: time.Now(), Action: "pause", Detail: "operator", OK: true})
}

// Resume clears soft pause.
func (s *Session) Resume() error {
	if s == nil {
		return fmt.Errorf("no session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.policy.Resume(); err != nil {
		s.audit = append(s.audit, ActionRecord{At: time.Now(), Action: "resume", Detail: "operator", OK: false, Error: err.Error()})
		return err
	}
	s.audit = append(s.audit, ActionRecord{At: time.Now(), Action: "resume", Detail: "operator", OK: true})
	return nil
}

// Stop hard-blocks interact and invalidates refs until Reset.
func (s *Session) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy.Stop()
	s.refsValid = false
	s.audit = append(s.audit, ActionRecord{At: time.Now(), Action: "stop", Detail: "operator", OK: true})
}

// ResetControl clears stop/pause so a new task can use Computer Use again.
func (s *Session) ResetControl() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy.Reset(false)
	s.actuated = ""
	s.actuatedWindow = ""
	s.actuatedWindowID = 0
	s.actuatedKind = ""
	s.audit = append(s.audit, ActionRecord{At: time.Now(), Action: "reset", Detail: "operator", OK: true})
}

// ControlState returns pause/stop flags for UI.
func (s *Session) ControlState() (paused, stopped bool) {
	if s == nil || s.policy == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy.IsPaused(), s.policy.IsStopped()
}

// CommitObserve stores SoM marks from local perception and returns TextForModel.
// screenshotB64 is retained in-session only for optional later use; it is not
// placed into TextForModel for text-primary mode.
func (s *Session) CommitObserve(
	meta ScreenMeta,
	windows []string,
	elements []taskengine.UIElement,
	ocr []taskengine.OCRResult,
	screenshotB64 string,
) *ObserveResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Soft-pause still allows observe so operators/agents can inspect state.
	// Hard-stop blocks observe to force a clean reset.
	if s.policy != nil && s.policy.IsStopped() {
		res := &ObserveResult{
			Mode:       s.cfg.Mode,
			Meta:       meta,
			ObservedAt: time.Now(),
		}
		res.TextForModel = "computer_observe blocked: session is stopped by operator. Call ComputerUseReset (or ask user to Reset) before continuing."
		return res
	}

	if meta.ScaleFactor <= 0 {
		meta.ScaleFactor = 1.0
	}
	marks := BuildMarks(elements, ocr)
	if s.windowResolver != nil || s.windowIDResolver != nil {
		for i := range marks {
			sx, sy := MapCaptureToScreen(meta, marks[i].CenterX, marks[i].CenterY)
			if s.windowResolver != nil {
				marks[i].Window = s.windowResolver(sx, sy)
			}
			if s.windowIDResolver != nil {
				marks[i].WindowID = s.windowIDResolver(sx, sy)
			}
		}
	}
	excerpt := FormatOCRExcerpt(ocr, s.cfg.OCRMaxChars)
	res := &ObserveResult{
		Mode:          s.cfg.Mode,
		Meta:          meta,
		Windows:       append([]string(nil), windows...),
		Elements:      marks,
		OCRExcerpt:    excerpt,
		ScreenshotB64: screenshotB64,
		OCRLines:      append([]taskengine.OCRResult(nil), ocr...),
		ObservedAt:    time.Now(),
	}
	if s.cfg.Mode == ObserveVisionAssist {
		res.TextForModel = RenderVisionObserve(res)
	} else {
		res.TextForModel = RenderTextObserve(res, s.cfg.ElementsMaxInText)
	}
	s.last = res
	s.refsValid = true
	s.labelsCurrent = true
	// OCR lines alone have no window id. Keep the id from the last full
	// observe so a later title change is still stale. A shot that does name
	// windows replaces it, including a mixed shot that names none.
	if id := cropWindowID(marks, strings.TrimSpace(meta.CropTitle)); id != 0 {
		s.cropWindowID = id
	} else if marksHaveWindowID(marks) {
		s.cropWindowID = 0
	}
	s.screenshotN++
	s.audit = append(s.audit, ActionRecord{
		At:     time.Now(),
		Action: "observe",
		Detail: fmt.Sprintf("elements=%d ocr_chars=%d", len(marks), len(excerpt)),
		OK:     true,
	})
	return res
}

// ApplyRefreshCapture stores the geometry of a lightweight post-action crop
// so a later vision click maps into that image. It drops the previous
// element and OCR boxes: those coordinates belong to the previous image.
// screenshotB64 is that crop, kept for a later label read. Refs are stale,
// and this does not count as computer_observe. The cropped window id from
// the last full observe is kept so a title change is still stale.
func (s *Session) ApplyRefreshCapture(meta ScreenMeta, screenshotB64 string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta.ScaleFactor <= 0 {
		meta.ScaleFactor = 1
	}
	if s.last == nil {
		s.last = &ObserveResult{Mode: s.cfg.Mode, ObservedAt: time.Now()}
	}
	s.last.Meta = meta
	s.last.ObservedAt = time.Now()
	s.last.ScreenshotB64 = screenshotB64
	s.last.Elements = nil
	s.last.OCRLines = nil
	s.last.OCRExcerpt = ""
	s.refsValid = false
	s.labelsCurrent = false
}

// LabelsCurrent reports that element and OCR boxes describe the current crop.
func (s *Session) LabelsCurrent() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.labelsCurrent && s.last != nil
}

// StaleCropScreenshot is the lightweight crop whose labels are not current.
// A monitor fallback has no crop title and is not read here.
func (s *Session) StaleCropScreenshot() (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.labelsCurrent || s.last == nil {
		return "", false
	}
	if strings.TrimSpace(s.last.Meta.CropTitle) == "" || s.last.ScreenshotB64 == "" {
		return "", false
	}
	return s.last.ScreenshotB64, true
}

// AdoptOCRLabels replaces pay and secret labels with lines from the current
// crop. It does not publish click refs, does not record an observe, and does
// not change click geometry. An empty crop title is a monitor fallback.
func (s *Session) AdoptOCRLabels(ocr []taskengine.OCRResult) bool {
	if s == nil || len(ocr) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil || strings.TrimSpace(s.last.Meta.CropTitle) == "" {
		return false
	}
	copied := append([]taskengine.OCRResult(nil), ocr...)
	// A line with no box cannot be hit-tested. Treating it as current would
	// let Enter through on an empty label list.
	if !OCRLinesUsable(copied) {
		return false
	}
	s.last.OCRLines = copied
	s.last.OCRExcerpt = FormatOCRExcerpt(copied, s.cfg.OCRMaxChars)
	s.last.Elements = nil
	s.labelsCurrent = true
	return true
}

// InvalidateLabels drops pay and secret labels after a refresh that did not
// produce a new crop. The previous boxes describe the screen before the action.
func (s *Session) InvalidateLabels() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labelsCurrent = false
	s.refsValid = false
	if s.last == nil {
		return
	}
	s.last.Elements = nil
	s.last.OCRLines = nil
	s.last.OCRExcerpt = ""
	s.last.ScreenshotB64 = ""
}

// ReplaceLastElements swaps SoM marks on the last observe and refreshes
// TextForModel. Captioning must copy the slice first so HTTP work does not
// mutate session state without the lock.
func (s *Session) ReplaceLastElements(els []MarkedElement) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return
	}
	s.last.Elements = els
	if s.cfg.Mode == ObserveVisionAssist {
		s.last.TextForModel = RenderVisionObserve(s.last)
	} else {
		s.last.TextForModel = RenderTextObserve(s.last, s.cfg.ElementsMaxInText)
	}
}

// InvalidateRefs marks the last SoM table stale (call after successful actions).
func (s *Session) InvalidateRefs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refsValid = false
}

// ResolveClickRef returns screen coordinates for a ref from the last observe.
func (s *Session) ResolveClickRef(ref string) (x, y int, el *MarkedElement, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refsValid || s.last == nil {
		return 0, 0, nil, fmt.Errorf("stale_ref: no valid observation — call computer_observe first")
	}
	m, err := ResolveRef(s.last.Elements, ref)
	if err != nil {
		return 0, 0, nil, err
	}
	sx, sy := MapCaptureToScreen(s.last.Meta, m.CenterX, m.CenterY)
	return sx, sy, m, nil
}

// MapCaptureClick converts capture/image pixels (after any vision resize
// mapping) into virtual-desktop coordinates for InputSimulator.
func (s *Session) MapCaptureClick(x, y int) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return x, y
	}
	return MapCaptureToScreen(s.last.Meta, x, y)
}

// AppendElements adds synthesized elements (e.g. OCR text hits from
// computer_find) to the current ref table, assigning fresh eN refs.
// Returns the assigned refs. No-op when refs are stale or nothing to add.
func (s *Session) AppendElements(els []MarkedElement) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refsValid || s.last == nil || len(els) == 0 {
		return nil
	}
	refs := make([]string, 0, len(els))
	next := len(s.last.Elements)
	// Copy-then-swap instead of in-place append: holders of the old slice
	// header (LastObserve callers, event payloads) keep a consistent view.
	merged := make([]MarkedElement, 0, next+len(els))
	merged = append(merged, s.last.Elements...)
	for _, el := range els {
		el.Ref = fmt.Sprintf("e%d", next)
		next++
		merged = append(merged, el)
		refs = append(refs, el.Ref)
	}
	s.last.Elements = merged
	return refs
}

// ElementNameAtScreen returns the smallest observed control that contains
// a virtual-desktop point. Vision clicks have no ref, so the pay guard uses
// this name. An empty string means nothing labeled covers the point.
func (s *Session) ElementNameAtScreen(x, y int) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil || !s.labelsCurrent {
		return ""
	}
	cx, cy := MapScreenToCapture(s.last.Meta, x, y)
	best := ""
	bestArea := int(^uint(0) >> 1)
	for i := range s.last.Elements {
		el := s.last.Elements[i]
		name := strings.TrimSpace(el.Name)
		if name == "" {
			continue
		}
		x0, y0, w, h := el.BBox[0], el.BBox[1], el.BBox[2], el.BBox[3]
		if w <= 0 || h <= 0 {
			continue
		}
		if cx < x0 || cy < y0 || cx >= x0+w || cy >= y0+h {
			continue
		}
		area := w * h
		if area < bestArea {
			bestArea = area
			best = name
		}
	}
	return best
}

// OCRTextAtScreen returns the smallest short OCR line that contains a
// virtual-desktop point. A line longer than 80 runes is a paragraph, not a
// control, so it is ignored. An empty string means no short line covers the point.
func (s *Session) OCRTextAtScreen(x, y int) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil || !s.labelsCurrent {
		return ""
	}
	cx, cy := MapScreenToCapture(s.last.Meta, x, y)
	best := ""
	bestArea := int(^uint(0) >> 1)
	for i := range s.last.OCRLines {
		line := s.last.OCRLines[i]
		text := strings.TrimSpace(line.Text)
		if text == "" || len([]rune(text)) > 80 {
			continue
		}
		x0, y0, w, h := line.BBox[0], line.BBox[1], line.BBox[2], line.BBox[3]
		if w <= 0 || h <= 0 {
			continue
		}
		if cx < x0 || cy < y0 || cx >= x0+w || cy >= y0+h {
			continue
		}
		area := w * h
		if area < bestArea {
			bestArea = area
			best = text
		}
	}
	return best
}

// NoteActuated records the control the agent just clicked or typed into.
// window is the window that received the click. kind is the control type
// (button, edit, …). An empty name means the click landed on nothing labeled,
// so the previous control is no longer the one a later Enter would submit.
func (s *Session) NoteActuated(name, window, kind string) {
	s.NoteActuatedID(name, window, kind, 0)
}

// NoteActuatedID records the control and the stable id of the window that
// received it. A later title change on that same window keeps the caret.
func (s *Session) NoteActuatedID(name, window, kind string, windowID uint64) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		window = ""
		kind = ""
		windowID = 0
	}
	s.mu.Lock()
	s.actuated = name
	s.actuatedWindow = strings.TrimSpace(window)
	s.actuatedWindowID = windowID
	s.actuatedKind = strings.TrimSpace(kind)
	s.mu.Unlock()
}

// ActuatedName returns the control recorded by NoteActuated.
func (s *Session) ActuatedName() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actuated
}

// ActuatedInWindow returns the recorded control when it still belongs to the
// window that will receive the key. Another window drops it, including a
// second window of the same application. An empty recorded window matches,
// because the click could not be attributed.
func (s *Session) ActuatedInWindow(foreground string) (name, kind string) {
	return s.ActuatedInWindowID(foreground, 0)
}

// ActuatedInWindowID is ActuatedInWindow with the foreground window's stable
// id. Matching ids win over the title, so a browser title that gains a count
// stays the same window, and two windows with the same title stay apart.
// A zero id falls back to the title.
func (s *Session) ActuatedInWindowID(foreground string, windowID uint64) (name, kind string) {
	if s == nil {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.actuated == "" {
		return "", ""
	}
	if s.actuatedWindowID != 0 && windowID != 0 {
		if s.actuatedWindowID != windowID {
			return "", ""
		}
		return s.actuated, s.actuatedKind
	}
	win := strings.TrimSpace(s.actuatedWindow)
	fg := strings.TrimSpace(foreground)
	if win != "" && fg != "" && !sameControlWindow(win, fg) {
		return "", ""
	}
	return s.actuated, s.actuatedKind
}

// ActuatedDocumentCurrent reports that the recorded control's document title
// still describes the foreground window. An empty recorded title stays current.
// An unread count stays current. Navigation to another page does not, so a
// search, note, or password caret does not follow the new page.
func (s *Session) ActuatedDocumentCurrent(foreground string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	win := strings.TrimSpace(s.actuatedWindow)
	if s.actuated == "" || win == "" {
		return true
	}
	return sameDocumentWindow(win, foreground)
}

// ActuatedForKey returns the caret a key or untargeted type should trust.
// A different window drops it. The same window on a new document drops it
// too, including a password field left on the previous page. An unread count
// in the title is still the same document.
func (s *Session) ActuatedForKey(foreground string, windowID uint64) (string, string) {
	name, kind := s.ActuatedInWindowID(foreground, windowID)
	if name == "" || s.ActuatedDocumentCurrent(foreground) {
		return name, kind
	}
	return "", ""
}

// ObserveDocumentStale reports that the cropped window is showing a different
// document than the one just observed. Submit keys and typing must not use
// that crop: its labels describe the previous page. A different window, even
// one that appeared in the shot, is not stale. The returned crop is the
// observed title.
func (s *Session) ObserveDocumentStale(foreground string, windowID uint64) (crop string, stale bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return "", false
	}
	crop = strings.TrimSpace(s.last.Meta.CropTitle)
	fg := strings.TrimSpace(foreground)
	if crop == "" || fg == "" || sameDocumentWindow(crop, fg) {
		return crop, false
	}
	// The crop's own window, not every window that happened to be in the shot.
	// Another window's controls can sit in the margin; those keys stay usable.
	id := cropWindowID(s.last.Elements, crop)
	if id == 0 {
		id = s.cropWindowID
	}
	if windowID == 0 || windowID != id {
		return crop, false
	}
	return crop, true
}

// KeySurfaceLabels returns names in the window that will receive a key.
// Names from another window are left out, including another window of the
// same application. When no element carries a window, the whole observe is
// one surface. A short OCR line counts only when it is not text inside a
// larger control, so a chat word is not a button.
func (s *Session) KeySurfaceLabels(foreground string) []string {
	return s.KeySurfaceLabelsFor(foreground, 0)
}

// KeySurfaceLabelsFor is KeySurfaceLabels with the foreground window's stable
// id. The id keeps labels when that window's title text changes, and splits
// two windows that happen to share a title.
func (s *Session) KeySurfaceLabelsFor(foreground string, windowID uint64) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil || !s.labelsCurrent {
		return nil
	}
	fg := strings.TrimSpace(foreground)
	crop := strings.TrimSpace(s.last.Meta.CropTitle)
	sameID := windowID != 0 && elementsIncludeWindowID(s.last.Elements, windowID)
	if crop != "" && fg != "" && !sameControlWindow(crop, fg) && !sameID {
		return nil
	}
	attributed := false
	for i := range s.last.Elements {
		if strings.TrimSpace(s.last.Elements[i].Window) != "" {
			attributed = true
			break
		}
	}
	if attributed && fg == "" && !sameID {
		return nil
	}
	boxes := make([][4]int, 0, len(s.last.Elements))
	labels := make([]string, 0, len(s.last.Elements)+len(s.last.OCRLines))
	for i := range s.last.Elements {
		el := s.last.Elements[i]
		win := strings.TrimSpace(el.Window)
		if sameID {
			if el.WindowID != windowID {
				continue
			}
		} else if attributed && (win == "" || !sameControlWindow(win, fg)) {
			continue
		}
		if name := strings.TrimSpace(el.Name); name != "" {
			labels = append(labels, name)
		}
		if el.BBox[2] > 0 && el.BBox[3] > 0 {
			boxes = append(boxes, el.BBox)
		}
	}
	for i := range s.last.OCRLines {
		line := s.last.OCRLines[i]
		text := strings.TrimSpace(line.Text)
		if text == "" || len([]rune(text)) > 80 || line.BBox[2] <= 0 || line.BBox[3] <= 0 {
			continue
		}
		if (attributed || sameID) && !centerInsideAny(line.BBox, boxes) {
			continue
		}
		if ocrInsideLargerControl(line.BBox, boxes) {
			continue
		}
		labels = append(labels, text)
	}
	return labels
}

// cropWindowID is the top-level window the crop was taken from.
// Titled elements that name that document decide. One untitled id is the
// whole shot. Two different ids and no title that names the crop do not guess.
func cropWindowID(elements []MarkedElement, crop string) uint64 {
	var titled uint64
	titledSet := false
	var only uint64
	onlySet := false
	mixed := false
	for i := range elements {
		id := elements[i].WindowID
		if id == 0 {
			continue
		}
		if !onlySet {
			only = id
			onlySet = true
		} else if only != id {
			mixed = true
		}
		win := strings.TrimSpace(elements[i].Window)
		if win == "" || !sameDocumentWindow(win, crop) {
			continue
		}
		if !titledSet {
			titled = id
			titledSet = true
			continue
		}
		if titled != id {
			return 0
		}
	}
	if titledSet {
		return titled
	}
	if onlySet && !mixed {
		return only
	}
	return 0
}

func marksHaveWindowID(elements []MarkedElement) bool {
	for i := range elements {
		if elements[i].WindowID != 0 {
			return true
		}
	}
	return false
}

func elementsIncludeWindowID(elements []MarkedElement, windowID uint64) bool {
	if windowID == 0 {
		return false
	}
	for i := range elements {
		if elements[i].WindowID == windowID {
			return true
		}
	}
	return false
}

func centerInsideAny(inner [4]int, boxes [][4]int) bool {
	cx := inner[0] + inner[2]/2
	cy := inner[1] + inner[3]/2
	for _, box := range boxes {
		if cx >= box[0] && cy >= box[1] && cx < box[0]+box[2] && cy < box[1]+box[3] {
			return true
		}
	}
	return false
}

// ocrInsideLargerControl reports that a text line sits inside a control
// several times its size. That line is the control's content, not a button.
func ocrInsideLargerControl(line [4]int, boxes [][4]int) bool {
	cx := line[0] + line[2]/2
	cy := line[1] + line[3]/2
	lineArea := int64(line[2]) * int64(line[3])
	if lineArea <= 0 {
		return false
	}
	best := int64(1 << 62)
	hit := false
	for _, box := range boxes {
		if box[2] <= 0 || box[3] <= 0 {
			continue
		}
		if cx < box[0] || cy < box[1] || cx >= box[0]+box[2] || cy >= box[1]+box[3] {
			continue
		}
		area := int64(box[2]) * int64(box[3])
		if area < best {
			best = area
			hit = true
		}
	}
	return hit && best > 4*lineArea
}

// AllowPixelClick checks policy for raw coordinates.
func (s *Session) AllowPixelClick() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy.AllowPixelClick()
}

// BeginAction enforces step budget / pause before an interact action.
func (s *Session) BeginAction(action, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.policy.BeginStep(); err != nil {
		s.audit = append(s.audit, ActionRecord{
			At: time.Now(), Action: action, Detail: detail, OK: false, Error: err.Error(),
		})
		return err
	}
	return nil
}

// RecordAction appends an audit line and invalidates refs on successful interact.
func (s *Session) RecordAction(action, detail string, ok bool, errMsg string, invalidate bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, ActionRecord{
		At:     time.Now(),
		Action: action,
		Detail: detail,
		OK:     ok,
		Error:  errMsg,
	})
	if ok && invalidate {
		s.refsValid = false
	}
}

// CheckClickPolicy runs policy for a click at coordinates.
func (s *Session) CheckClickPolicy(x, y int, windowTitle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy.AllowClickAt(x, y, windowTitle)
}

// LastObserve returns the last result (copy of elements slice header only).
func (s *Session) LastObserve() *ObserveResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// LastValidObserve returns a snapshot of the last observe while refs are still
// valid. A concurrent click cannot sneak between RefsValid and LastObserve.
func (s *Session) LastValidObserve() *ObserveResult {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refsValid || s.last == nil {
		return nil
	}
	cp := *s.last
	if len(s.last.Elements) > 0 {
		cp.Elements = append([]MarkedElement(nil), s.last.Elements...)
	}
	if len(s.last.Windows) > 0 {
		cp.Windows = append([]string(nil), s.last.Windows...)
	}
	if len(s.last.OCRLines) > 0 {
		cp.OCRLines = append([]taskengine.OCRResult(nil), s.last.OCRLines...)
	}
	return &cp
}

// RefsValid reports whether click(ref) can be used.
func (s *Session) RefsValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refsValid && s.last != nil
}

// Audit returns a copy of the action log.
func (s *Session) Audit() []ActionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ActionRecord, len(s.audit))
	copy(out, s.audit)
	return out
}

// Playbook returns the system/tool guidance for text-primary Computer Use.
func Playbook() string {
	return PlaybookFor(ObserveTextPrimary)
}

// PlaybookVision returns operating rules when the chat model can see screenshots.
func PlaybookVision() string {
	return PlaybookFor(ObserveVisionAssist)
}

// PlaybookBeforePerception is the system-prompt playbook. That prompt is
// built before this turn's model is chosen. The vision flag written by the
// previous tool call must not decide it: a text-only model told to click
// x,y is rejected, and a vision model told that no screenshot exists ignores
// the image. computer_observe reports which result this turn actually got.
func PlaybookBeforePerception() string {
	return `Computer Use:
- Call computer_observe first. If that result attaches a screenshot, look at the image and computer_click with x,y in that image's pixels, or with ref=eN. If there is no image, the result lists eN refs; computer_click those refs and do not invent pixel coordinates.
- Pass window (an app title substring) to computer_observe or computer_focus to bring one app forward. If omitted, the foreground window is used. Do not pass screen_index=-1. A stitch larger than 8 megapixels is refused and the foreground window is captured instead.
- To find a person, button, or text, call computer_find query=... It searches labels and OCR and returns clickable eN refs.
- In an IM app or a long list, use that app's search box: focus the window, click the search field, computer_type the name, then computer_find the result. Otherwise computer_scroll. The scroll result includes a fresh foreground-window crop.
- A successful click, type, key, scroll, focus, select, drag, or wait already includes a fresh foreground-window crop of the window just operated, and a screenshot when this model can see images. Use that result. Call computer_observe only to switch windows, or when the result says the refresh failed.
- Use computer_key for shortcuts such as enter, ctrl+c, and alt+tab. Use computer_select for a list or tab item, and computer_scroll_into_view before clicking something off screen.
- Web pages use the browser tools. Office documents use office_read. Computer Use is for native desktop windows.
- Do not computer_click a pay or place-order control, and do not press Enter or Space to activate one. Web checkout uses browser confirm_order, then one browser click after the person confirms. The person clicks a desktop pay button.
- Do not computer_type a password, payment password, or verification code, and do not ask for it in chat. The person types it on the screen. The tool waits. Then computer_observe.
- Call computer_done when the desktop task is finished.`
}

// PlaybookFor returns Computer Use rules for the active perception mode.
func PlaybookFor(mode ObserveMode) string {
	if mode == ObserveVisionAssist {
		return `Computer Use (vision):
- The current model supports images. computer_observe attaches a screenshot with numbered SoM marks (a11y boxes). Look at the image.
- Click with computer_click x=<image_x> y=<image_y> in the attached screenshot's pixel space, or ref=eN for a drawn mark.
- A successful click, type, key, scroll, focus, select, drag, or wait already includes a fresh foreground-window crop of the window just operated and a new screenshot. Use that image. Call computer_observe only to switch windows, or when the result says the refresh failed.
- Do not pass screen_index=-1. A stitch larger than 8 megapixels is refused. Pass window=<app title> to observe one app.
- Use computer_focus with a window title substring before interacting with a specific app.
- To reach a person/chat in an IM or any long list: use the app's own search box (click the search field, computer_type the name). The type result includes the next screenshot.
- Prefer computer_key for shortcuts (enter, ctrl+c, alt+tab). Use computer_select for list/tab items and computer_scroll_into_view before clicking off-screen chrome.
- Web pages: prefer browser_* tools when available. Office documents: prefer office_read. Computer Use is for native desktop GUIs.
- Do not computer_click a pay or place-order control, and do not press Enter or Space to activate one. Web checkout uses browser confirm_order, then one browser click after the person confirms. The person clicks a desktop pay button.
- Do not computer_type a password, payment password, or verification code, and do not ask for it in chat. The person types it on the screen. The tool waits. Then computer_observe.`
	}
	return `Computer Use (text-primary):
- You may be a text-only model. Screenshots are NOT sent to you. Local OmniParser/OCR provide eN refs.
- Always call computer_observe first. It returns windows, eN elements (from local OmniParser/OCR/a11y), and ocr_excerpt.
- Pass the window parameter (app title substring) to computer_observe to focus a specific app's accessibility tree. If omitted, the foreground window is enumerated automatically.
- To locate a specific person, button, or text, call computer_find query=... first. It searches element labels AND raw OCR text, and returns clickable eN refs even for text that no element covers.
- To reach a person/chat in an IM or any long list: prefer the app's own search box (focus window → find/click the search field → computer_type the name → computer_find the result). Otherwise computer_scroll. The result includes a fresh foreground-window crop; computer_find on that result.
- Use computer_focus with a window title substring before interacting with a specific app.
- Click with computer_click ref=eN. Use computer_select for list/tab items and computer_scroll_into_view to reveal off-screen chrome. computer_drag moves from_ref to to_ref.
- Do NOT invent pixel coordinates unless explicitly allowed.
- A successful click, type, key, scroll, focus, select, drag, or wait already includes a fresh foreground-window crop of the window just operated. Use that crop. Call computer_observe only to switch windows, or when the result says the refresh failed.
- Do not pass screen_index=-1. A stitch larger than 8 megapixels is refused. Pass window=<app title> to observe one app.
- Prefer computer_key / computer_scroll for navigation when labels are missing.
- If elements are empty, report that local detection failed; do not guess clicks.
- Web pages: prefer browser_* tools when available. Office documents: prefer office_read. Computer Use is for native desktop GUIs.
- Do not computer_click a pay or place-order control, and do not press Enter or Space to activate one. Web checkout uses browser confirm_order, then one browser click after the person confirms. The person clicks a desktop pay button.
- Do not computer_type a password, payment password, or verification code, and do not ask for it in chat. The person types it on the screen. The tool waits. Then computer_observe.`
}
