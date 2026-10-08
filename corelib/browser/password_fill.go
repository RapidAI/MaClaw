package browser

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// FillPasswordField inserts a secret into the focused control with one CDP
// Input.insertText call. It never falls through to a page script, and an
// error is only not_password_field, no_focus, or unavailable.
func FillPasswordField(focused bool, inputType, value string, send func(method string, params map[string]any) error) error {
	kind := ClassifyFocusedKind(inputType)
	switch kind {
	case "cross-origin", "closed-shadow":
		// An unreadable frame is not an empty page. no_focus would invite
		// another type into the same control.
		return errFill("not_password_field")
	case "unreadable":
		return errFill("unavailable")
	}
	if !focused {
		return errFill("no_focus")
	}
	if kind != "password" {
		return errFill("not_password_field")
	}
	if strings.TrimSpace(value) == "" || send == nil {
		return errFill("unavailable")
	}
	if err := send("Input.insertText", map[string]any{"text": value}); err != nil {
		return errFill("unavailable")
	}
	return nil
}

func errFill(code string) error {
	return fillError(code)
}

// focusedInputKindJS follows the focused element through nested same-origin
// frames and open shadow roots, the same descent observe uses. A cross-origin
// frame or a closed shadow is reported as its own kind so the caller can
// refuse it. An empty kind means nothing is focused.
const focusedInputKindJS = `(function () {
  function nativeKind(el) {
    if (!el) return '';
    const tag = String(el.tagName || '').toUpperCase();
    if (tag === 'TEXTAREA') return 'textarea';
    if (tag === 'SELECT') return 'select';
    return String((el.getAttribute && el.getAttribute('type')) || el.type || '').trim().toLowerCase();
  }
  function editableHost(el) {
    if (!el) return false;
    const tag = String(el.tagName || '').toUpperCase();
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable === true;
  }
  function walk(el, depth) {
    if (!el || depth > 16) return 'closed-shadow';
    const tag = String(el.tagName || '').toUpperCase();
    if (tag === 'IFRAME' || tag === 'FRAME') {
      let doc = null;
      try { doc = el.contentDocument; }
      catch (e) { return 'cross-origin'; }
      if (!doc) return 'cross-origin';
      const inner = doc.activeElement;
      if (!inner || inner === doc.body || inner === doc.documentElement) return '';
      return walk(inner, depth + 1);
    }
    if (el.shadowRoot) {
      if (String(el.shadowRoot.mode || '') === 'closed') return 'closed-shadow';
      const inner = el.shadowRoot.activeElement;
      if (inner && inner !== el) {
        const nested = walk(inner, depth + 1);
        if (nested) return nested;
      }
    } else if (!editableHost(el)) {
      let selfFocus = true;
      try { selfFocus = el.matches(':focus'); } catch (e) { selfFocus = false; }
      // Retargeted hosts stay activeElement while :focus stays on the inner
      // control. delegatesFocus is the exception: the host matches :focus
      // and has no tabindex of its own. A light-DOM component the person
      // focused does have tabindex, so it stays typeable.
      if (!selfFocus) return 'closed-shadow';
      const tab = el.getAttribute ? el.getAttribute('tabindex') : null;
      if (tab === null && tag.indexOf('-') !== -1) return 'closed-shadow';
    }
    return nativeKind(el);
  }
  const el = document.activeElement;
  if (!el || el === document.body || el === document.documentElement) return JSON.stringify({kind: ''});
  return JSON.stringify({kind: walk(el, 0)});
})()`

// ClassifyFocusedKind normalizes a frame-walk result. Empty means nothing is
// focused. password, cross-origin, and closed-shadow stay distinct so a model
// type can refuse them instead of treating them as an empty field.
func ClassifyFocusedKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

// FocusBlocksModelType reports whether a model type must stop before insertion.
// A password field, a cross-origin frame, and a closed shadow all refuse.
// The refusal code is not_password_field. An empty kind does not refuse.
func FocusBlocksModelType(kind string) bool {
	switch ClassifyFocusedKind(kind) {
	case "password", "cross-origin", "closed-shadow", "unreadable":
		return true
	default:
		return false
	}
}

// modelTypeRefAllowed reports whether a ref is a text control the model may
// type into while some other control is a password field or is unreadable.
// A password ref is never allowed. A textarea has no type attribute. An
// input whose type was not recorded is not treated as text, so the focused
// control is still checked.
func modelTypeRefAllowed(ref *BrowserElementRef) bool {
	if ref == nil || FocusBlocksModelType(ref.InputType) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ref.Tag)) {
	case "textarea":
		return true
	case "input":
		switch ClassifyFocusedKind(ref.InputType) {
		case "text", "email", "search", "tel", "url", "number":
			return true
		default:
			return false
		}
	default:
		return strings.EqualFold(strings.TrimSpace(ref.Role), "textbox")
	}
}

// backendNodeTypeAllowed reports whether a backend node is a text control
// whose type was recorded. A password field is often exposed as a textbox
// with no type, so that node is not a place to insert model text.
func backendNodeTypeAllowed(ref *BrowserElementRef) bool {
	if ref == nil || ref.BackendNodeID == 0 || FocusBlocksModelType(ref.InputType) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ref.Tag)) {
	case "textarea":
		return true
	case "input":
		switch ClassifyFocusedKind(ref.InputType) {
		case "text", "email", "search", "tel", "url", "number":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// modelTypeRefForInsert drops a backend node the model must not type into.
// Selector candidates still run. The caller's ref is left unchanged.
func modelTypeRefForInsert(blocked bool, ref *BrowserElementRef) *BrowserElementRef {
	if !blocked || backendNodeTypeAllowed(ref) || ref == nil || ref.BackendNodeID == 0 {
		return ref
	}
	copied := *ref
	copied.BackendNodeID = 0
	return &copied
}

// modelTypeCandidateBlocked stops a model type that would clear a password
// or an unreadable control. Every selector candidate is classified with the
// same descent prepareEditable uses, before that descent clears the matched
// field. A candidate that matches nothing does not hide a later password
// match. A text, email, textarea, or textbox ref skips only the focus read,
// so a focused password does not block a selector that receives into that
// field. Any other ref still reads the focused control when no candidate
// refused, because keystrokes can land there. The error is only
// not_password_field.
func modelTypeCandidateBlocked(resolved *BrowserElementRef, candidates []string, focus func() string, read func(selector string) (string, error)) error {
	if resolved != nil && FocusBlocksModelType(resolved.InputType) {
		return errors.New("not_password_field")
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if read == nil {
			return errors.New("not_password_field")
		}
		kind, err := read(candidate)
		if err != nil || receiveKindBlocks(kind) {
			return errors.New("not_password_field")
		}
	}
	if modelTypeRefAllowed(resolved) {
		return nil
	}
	if resolved != nil && focus != nil && FocusBlocksModelType(focus()) {
		return errors.New("not_password_field")
	}
	return nil
}

// KeyInsertsText reports whether a key chord types a character or pastes.
// xdotool accepts both aliases (shift, ctrl) and keysym names (Shift_L,
// Control_L), and named keys such as plus. A chord that can insert text has
// to be read against the focused control. Return, Tab, and plain shortcuts
// such as ctrl+a do not insert.
func KeyInsertsText(key string) bool {
	parts, ok := canonicalKeyParts(key)
	if !ok {
		return false
	}
	if chordIsPaste(parts) {
		return true
	}
	name := parts[len(parts)-1]
	shortcut := false
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "shift":
		case "ctrl", "alt", "meta", "super":
			shortcut = true
		default:
			// ISO_Level3_Shift and similar keysyms still produce characters.
			return !navigationKey(name)
		}
	}
	if shortcut {
		return false
	}
	return !navigationKey(name)
}

func canonicalKeyParts(key string) ([]string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	raw := strings.Split(strings.ToLower(key), "+")
	parts := make([]string, 0, len(raw))
	for _, token := range raw {
		token = canonicalKeyToken(token)
		if token == "" {
			return nil, false
		}
		parts = append(parts, token)
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

// canonicalKeyToken maps xdotool aliases and left/right keysyms onto one name.
// Keypad keys use the same names as the main keys, so KP_Enter is Return and
// KP_Add stays a character key.
func canonicalKeyToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "kp_")
	switch token {
	case "shift", "shift_l", "shift_r":
		return "shift"
	case "ctrl", "control", "control_l", "control_r":
		return "ctrl"
	case "alt", "alt_l", "alt_r":
		return "alt"
	case "meta", "meta_l", "meta_r":
		return "meta"
	case "super", "super_l", "super_r":
		return "super"
	case "enter", "return":
		return "return"
	case "esc":
		return "escape"
	case "del":
		return "delete"
	case "prior", "page_up":
		return "page_up"
	case "next", "page_down":
		return "page_down"
	case "arrowup", "up":
		return "up"
	case "arrowdown", "down":
		return "down"
	case "arrowleft", "left":
		return "left"
	case "arrowright", "right":
		return "right"
	case "iso_left_tab":
		return "tab"
	default:
		return token
	}
}

func chordIsPaste(parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	hasCtrl := false
	hasShift := false
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "ctrl":
			hasCtrl = true
		case "shift":
			hasShift = true
		default:
			return false
		}
	}
	if last == "v" && hasCtrl {
		return true
	}
	return last == "insert" && hasShift
}

func navigationKey(key string) bool {
	switch key {
	case "return", "tab", "escape", "backspace", "delete", "insert",
		"home", "end", "page_up", "page_down", "begin",
		"up", "down", "left", "right",
		"shift", "ctrl", "alt", "meta", "super",
		"caps_lock", "num_lock", "scroll_lock",
		"pause", "break", "print", "sys_req", "menu":
		return true
	}
	if len(key) < 2 || key[0] != 'f' {
		return false
	}
	n, err := strconv.Atoi(key[1:])
	return err == nil && n >= 1 && n <= 24
}

// receiveTargetKindFn classifies the element that prepareEditable will type
// into. A non-editable match descends to its first input, which is the same
// descent that clears the field, so a form whose first field is a password
// is refused before that field is wiped.
const receiveTargetKindFn = `function(selector, frameName, frameURL, framePath, frameLimited) {
  function nativeKind(el) {
    if (!el) return '';
    const tag = String(el.tagName || '').toUpperCase();
    if (tag === 'TEXTAREA') return 'textarea';
    if (tag === 'SELECT') return 'select';
    return String((el.getAttribute && el.getAttribute('type')) || el.type || '').trim().toLowerCase();
  }
  function receive(el) {
    if (!el) return '';
    const tag = String(el.tagName || '').toUpperCase();
    const editable = el.isContentEditable === true || tag === 'TEXTAREA' || tag === 'INPUT';
    if (!editable && el.querySelector) {
      const inner = el.querySelector('textarea,input,[contenteditable="true"],[contenteditable="plaintext-only"],[contenteditable=""]');
      if (inner) el = inner;
    }
    return nativeKind(el);
  }
  if (selector) {
    let el = null;
    if (frameLimited) {
      // The same selector can match an earlier element in the parent. TypeInFrame
      // only searches the ref's frame, so a parent email must not hide a password there.
      const path = Array.isArray(framePath) ? framePath : [];
      let scoped = null;
      if (path.length) scoped = docAtPath(path);
      if (!scoped && (frameName || frameURL)) scoped = findFrameChain(document, frameName || '', frameURL || '', []);
      if (!scoped || !scoped.doc) return JSON.stringify({kind: 'cross-origin'});
      el = findDeep(scoped.doc, selector);
    } else {
      el = findInFrames(document, selector);
    }
    if (!el) return JSON.stringify({kind: 'missing'});
    return JSON.stringify({kind: receive(el)});
  }
  const el = document.activeElement;
  if (!el || el === document.body || el === document.documentElement) return JSON.stringify({kind: ''});
  return JSON.stringify({kind: receive(el)});
}`

func receiveTargetKindJS(selector string) string {
	return receiveTargetKindJSScoped(selector, frameScope{}, false)
}

// receiveTargetKindJSScoped classifies the element prepareEditable will type
// into. frameLimited uses the same frame name, URL, and path as TypeInFrame.
// An unreadable frame is cross-origin, not missing, because a missing selector
// would otherwise let the frame session type.
func receiveTargetKindJSScoped(selector string, scope frameScope, frameLimited bool) string {
	encoded, _ := json.Marshal(selector)
	name, _ := json.Marshal(scope.Name)
	url, _ := json.Marshal(scope.URL)
	path := scope.Path
	if path == nil {
		path = []int{}
	}
	pathJSON, _ := json.Marshal(path)
	limited := "false"
	if frameLimited {
		limited = "true"
	}
	return "(function(){\n" + pierceFindJS + "\nreturn (" + receiveTargetKindFn + ")(" + string(encoded) + "," + string(name) + "," + string(url) + "," + string(pathJSON) + "," + limited + ");\n})()"
}

// receiveKindBlocks reports whether the element about to receive model text
// must be refused. A missing selector is not a password; the type then fails
// on its own. password, cross-origin, closed-shadow, and unreadable refuse.
func receiveKindBlocks(kind string) bool {
	if ClassifyFocusedKind(kind) == "missing" {
		return false
	}
	return FocusBlocksModelType(kind)
}

// receiveTargetKind reads the element prepareEditable would insert into.
// A failed read is unreadable, so the type stops. The offered text is not
// part of the expression or the error.
func (s *BrowserAgentSession) receiveTargetKind(selector string) (string, error) {
	return s.receiveTargetKindInFrame("", selector)
}

// receiveTargetKindInFrame reads the element TypeInFrame would insert into.
// The main frame searches every same-origin frame. A child frame searches
// only that frame, or the attached frame session when the frame is cross-origin.
// The frame lookup runs before the session lock because it takes that lock itself.
func (s *BrowserAgentSession) receiveTargetKindInFrame(frameID, selector string) (string, error) {
	if s == nil || !s.DesktopConnected() {
		return "", errors.New("unreadable")
	}
	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()
	if session == nil {
		return "", errors.New("unreadable")
	}
	frameID = strings.TrimSpace(frameID)
	js := receiveTargetKindJS(selector)
	sessionID := ""
	if frameID != "" && frameID != "main" {
		if id := session.frameSessionID(frameID); id != "" {
			// TypeInFrame evaluates in the child target. Its document is the frame.
			sessionID = id
		} else {
			scope, ok := session.scopeFor(frameID)
			if !ok {
				return "", errors.New("unreadable")
			}
			js = receiveTargetKindJSScoped(selector, scope, true)
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if sessionID != "" {
		return evalStringOnSession(session.client, sessionID, js, "kind")
	}
	return session.evalString(js, "kind")
}

// focusKindFromEval turns one frame-walk result into the focused flag and
// kind. A failed read is unreadable, so a model type stops. An empty kind
// means nothing is focused.
func focusKindFromEval(kind string, evalErr error) (bool, string) {
	if evalErr != nil {
		return true, "unreadable"
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return false, ""
	}
	return true, kind
}

// BlockModelPasswordTyping makes later Type calls refuse a focused password
// field before any insertion. It does not affect InsertPasswordText.
func (s *BrowserAgentSession) BlockModelPasswordTyping() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.blockModelPasswordTyping = true
	s.mu.Unlock()
}

func (s *BrowserAgentSession) modelPasswordTypingBlocked() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	blocked := s.blockModelPasswordTyping
	s.mu.RUnlock()
	return blocked
}

// FocusedInputKind reports the focused control. A cross-origin frame is
// reported as cross-origin and is not filled. The secret value is not read.
func (s *BrowserAgentSession) FocusedInputKind() (bool, string) {
	if s == nil || !s.DesktopConnected() {
		return false, ""
	}
	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()
	if session == nil {
		return false, ""
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	kind, err := session.evalString(focusedInputKindJS, "kind")
	return focusKindFromEval(kind, err)
}

// InsertPasswordText sends one CDP Input.insertText. It does not fall
// through to a page script and it does not log the text.
func (s *BrowserAgentSession) InsertPasswordText(text string) error {
	if s == nil || strings.TrimSpace(text) == "" {
		return errors.New("unavailable")
	}
	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()
	if session == nil {
		return errors.New("unavailable")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.client == nil || !session.client.IsAlive() {
		return errors.New("unavailable")
	}
	if _, err := session.client.Send("Input.insertText", map[string]interface{}{"text": text}, DefaultCmdTimeout); err != nil {
		return errors.New("unavailable")
	}
	return nil
}

type fillError string

func (e fillError) Error() string { return string(e) }
