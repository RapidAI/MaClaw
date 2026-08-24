package computeruse

import (
	"fmt"
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

	// Last observation (ref table valid until next action or new observe).
	last        *ObserveResult
	refsValid   bool
	screenshotN int

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
	if s.windowResolver != nil {
		for i := range marks {
			sx, sy := MapCaptureToScreen(meta, marks[i].CenterX, marks[i].CenterY)
			marks[i].Window = s.windowResolver(sx, sy)
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
	s.screenshotN++
	s.audit = append(s.audit, ActionRecord{
		At:     time.Now(),
		Action: "observe",
		Detail: fmt.Sprintf("elements=%d ocr_chars=%d", len(marks), len(excerpt)),
		OK:     true,
	})
	return res
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

// PlaybookFor returns Computer Use rules for the active perception mode.
func PlaybookFor(mode ObserveMode) string {
	if mode == ObserveVisionAssist {
		return `Computer Use (vision):
- The current model supports images. computer_observe attaches a screenshot with numbered SoM marks (a11y boxes). Look at the image.
- Click with computer_click x=<image_x> y=<image_y> in the attached screenshot's pixel space, or ref=eN for a drawn mark.
- After every click/type/key/scroll/focus/select/drag, call computer_observe again so you see the new screen.
- Use computer_focus with a window title substring before interacting with a specific app.
- To reach a person/chat in an IM or any long list: use the app's own search box (click the search field, computer_type the name, re-observe).
- Prefer computer_key for shortcuts (enter, ctrl+c, alt+tab). Use computer_select for list/tab items and computer_scroll_into_view before clicking off-screen chrome.
- Web pages: prefer browser_* tools when available. Office documents: prefer office_read. Computer Use is for native desktop GUIs.`
	}
	return `Computer Use (text-primary):
- You may be a text-only model. Screenshots are NOT sent to you. Local OmniParser/OCR provide eN refs.
- Always call computer_observe first. It returns windows, eN elements (from local OmniParser/OCR/a11y), and ocr_excerpt.
- Pass the window parameter (app title substring) to computer_observe to focus a specific app's accessibility tree. If omitted, the foreground window is enumerated automatically.
- To locate a specific person, button, or text, call computer_find query=... first. It searches element labels AND raw OCR text, and returns clickable eN refs even for text that no element covers.
- To reach a person/chat in an IM or any long list: prefer the app's own search box (focus window → find/click the search field → computer_type the name → computer_find the result). Otherwise computer_scroll and re-observe/re-find to page through the list.
- Use computer_focus with a window title substring before interacting with a specific app.
- Click with computer_click ref=eN. Use computer_select for list/tab items and computer_scroll_into_view to reveal off-screen chrome. computer_drag moves from_ref to to_ref.
- Do NOT invent pixel coordinates unless explicitly allowed.
- After every click/type/key/scroll/focus, call computer_observe again (refs become stale).
- Prefer computer_key / computer_scroll for navigation when labels are missing.
- If elements are empty, report that local detection failed; do not guess clicks.
- Web pages: prefer browser_* tools when available. Office documents: prefer office_read. Computer Use is for native desktop GUIs.`
}
