package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// errViewportNoCaret is returned when the person's text could not be written
// into a page field. The text itself stays out of the error.
var errViewportNoCaret = errors.New("文字没有进到页面上的输入框。先点那个输入框，再在下面输入。")

// errViewportNewCaret means the press focused a different editable than the
// one the bar was filling. The preview clears the bar. The text that was
// already typed stays out of this error.
var errViewportNewCaret = errors.New("viewport caret moved")

// ensurePageFocusEmulation makes a headless page accept clicks and typing.
// Without it the document is unfocused, the click does not create a caret,
// and a later write never lands in the field. Each caller sends it again
// because a navigation can drop the emulation. A failed command is ignored
// and is not treated as done.
func (s *Session) ensurePageFocusEmulation(client *CDPClient) {
	if s == nil || client == nil {
		return
	}
	_, _ = client.Send("Emulation.setFocusEmulationEnabled", map[string]interface{}{"enabled": true}, DefaultCmdTimeout)
}

// noteViewportCaretPoint records the CSS point of the press. It does not
// decide whether the field changed; the focus script does that.
func (s *Session) noteViewportCaretPoint(x, y float64) {
	s.mu.Lock()
	s.viewportCaretX = x
	s.viewportCaretY = y
	s.viewportCaretOK = true
	s.mu.Unlock()
}

// armViewportNewCaret forgets the bar value for the field that was just
// left and advances the caret generation. Only text stamped with the new
// generation is written. A new number may start with the same digits.
func (s *Session) armViewportNewCaret() {
	s.mu.Lock()
	s.viewportMirror = ""
	s.viewportGen++
	s.mu.Unlock()
}

// focusViewportCaret focuses the editable under the recorded point.
// same is true when that editable is the one already stamped. found is
// false when the press missed every editable; the previous stamp stays.
func (s *Session) focusViewportCaret(client *CDPClient) (same bool, found bool) {
	if s == nil || client == nil {
		return false, false
	}
	s.mu.Lock()
	ok := s.viewportCaretOK
	x, y := s.viewportCaretX, s.viewportCaretY
	s.mu.Unlock()
	if !ok {
		return false, false
	}
	result, err := client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    viewportFocusCaretJS(x, y),
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return false, false
	}
	var parsed struct {
		OK   bool `json:"ok"`
		Same bool `json:"same"`
	}
	if json.Unmarshal([]byte(extractStringValue(result)), &parsed) != nil || !parsed.OK {
		return false, false
	}
	return parsed.Same, true
}

// refocusStampedCaret focuses the field the last successful press stamped.
// It does not look under the pointer, so a keystroke cannot move the caret.
func (s *Session) refocusStampedCaret(client *CDPClient) {
	if s == nil || client == nil {
		return
	}
	_, _ = client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    viewportRefocusCaretJS,
		"returnByValue": true,
	}, DefaultCmdTimeout)
}

// applyViewportText writes the sidecar's full value into the field the person
// clicked. The write goes through the native value setter so a controlled
// input actually updates. The typed text is not logged.
func (s *Session) applyViewportText(client *CDPClient, text string, gen int) error {
	if s == nil || client == nil {
		return fmt.Errorf("browser session not connected")
	}
	s.ensurePageFocusEmulation(client)
	s.refocusStampedCaret(client)
	s.mu.Lock()
	mirror := s.viewportMirror
	current := s.viewportGen
	s.mu.Unlock()
	if gen != current {
		return errViewportNewCaret
	}
	if text == mirror {
		// Enter resends the bar even when it did not change. An empty match
		// must still prove a field is focused, or the key submits nothing.
		if _, err := s.readViewportField(client); err != nil {
			return errViewportNoCaret
		}
		return nil
	}
	before, _ := s.readViewportField(client)
	next := viewportNextValue(before, mirror, text)
	if err := s.evalCheck(viewportSetValueJS(next)); err != nil {
		return errViewportNoCaret
	}
	got, err := s.readViewportField(client)
	if err != nil || got != next {
		return errViewportNoCaret
	}
	s.mu.Lock()
	s.viewportMirror = text
	s.mu.Unlock()
	return nil
}

// viewportNextValue is the field value after the sidecar changes from mirror
// to text. An empty mirror means the bar was reset and its string is the
// whole new value. A mirror that is still a suffix of the field is the
// previous bar value, so a prefix the site already had stays put.
func viewportNextValue(before, mirror, text string) string {
	if mirror == "" {
		return text
	}
	if strings.HasSuffix(before, mirror) {
		return before[:len(before)-len(mirror)] + text
	}
	return text
}

func (s *Session) readViewportField(client *CDPClient) (string, error) {
	result, err := client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    viewportReadValueJS,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}
	var parsed struct {
		OK    bool   `json:"ok"`
		Value string `json:"value"`
	}
	if json.Unmarshal([]byte(extractStringValue(result)), &parsed) != nil || !parsed.OK {
		return "", errViewportNoCaret
	}
	return parsed.Value, nil
}

const viewportRefocusCaretJS = `(function() {
  // maclaw-viewport-refocus
` + viewportIsEditableJS + `  const el = document.querySelector("[data-maclaw-caret]");
  if (!isEd(el)) return JSON.stringify({ok:false});
  el.focus();
  try {
    if (el.setSelectionRange) {
      const n = (el.value || "").length;
      el.setSelectionRange(n, n);
    }
  } catch (e) {}
  return JSON.stringify({ok:true});
})()`

func viewportKeyCode(key string) (code string, vk int, ok bool) {
	switch key {
	case "Enter", "Return":
		return "Enter", 13, true
	case "Backspace":
		return "Backspace", 8, true
	case "Tab":
		return "Tab", 9, true
	case "Escape":
		return "Escape", 27, true
	default:
		return "", 0, false
	}
}

// viewportIsEditableJS is the caret test shared by focus, read, and write.
// The document body, and a contenteditable wrapper that contains a real
// input, are not the caret. Writing textContent there would erase the form.
const viewportIsEditableJS = `function isEd(el) {
  if (!el || el.nodeType !== 1) return false;
  const tag = el.tagName;
  if (tag === "BODY" || tag === "HTML") return false;
  if (tag === "INPUT" || tag === "TEXTAREA") {
    if (el.disabled || el.readOnly) return false;
    if (tag === "TEXTAREA") return true;
    const t = String(el.type || "text").toLowerCase();
    return t === "text" || t === "password" || t === "search" || t === "tel" || t === "url" || t === "email" || t === "number";
  }
  const flag = el.getAttribute && el.getAttribute("contenteditable");
  if (flag == null || flag === "false") return false;
  if (el.querySelector && el.querySelector("input,textarea")) return false;
  return !!el.isContentEditable;
}
`

const viewportReadValueJS = `(function() {
  // maclaw-viewport-read
` + viewportIsEditableJS + `  let el = document.querySelector("[data-maclaw-caret]");
  if (!isEd(el)) el = document.activeElement;
  if (el && el.tagName === "IFRAME") {
    try {
      const doc = el.contentDocument;
      el = doc && (doc.querySelector("[data-maclaw-caret]") || doc.activeElement);
    } catch (e) { el = null; }
  }
  if (!isEd(el)) return JSON.stringify({ok:false});
  const value = (el.tagName === "INPUT" || el.tagName === "TEXTAREA") ? (el.value || "") : (el.textContent || "");
  return JSON.stringify({ok:true, value: value});
})()`

func viewportFocusCaretJS(x, y float64) string {
	return fmt.Sprintf(`(function() {
  const x = %s, y = %s;
`, strconv.FormatFloat(x, 'f', 2, 64), strconv.FormatFloat(y, 'f', 2, 64)) + viewportIsEditableJS + `  function fromLabel(el) {
    if (!el || el.tagName !== "LABEL") return null;
    const doc = el.ownerDocument;
    if (el.htmlFor && doc) {
      const t = doc.getElementById(el.htmlFor);
      if (isEd(t)) return t;
    }
    const inner = el.querySelector("input,textarea,[contenteditable='true']");
    return isEd(inner) ? inner : null;
  }
  function pick(doc, px, py) {
    if (!doc || !doc.elementsFromPoint) return null;
    const stack = doc.elementsFromPoint(px, py) || [];
    for (const raw of stack) {
      let el = raw;
      if (el.shadowRoot && el.shadowRoot.elementsFromPoint) {
        const inner = el.shadowRoot.elementsFromPoint(px, py);
        if (inner && inner.length) el = inner[0];
      }
      if (isEd(el)) return el;
      const via = fromLabel(el) || (el.closest && fromLabel(el.closest("label")));
      if (via) return via;
      if (el.tagName === "IFRAME") {
        try {
          const r = el.getBoundingClientRect();
          const nested = pick(el.contentDocument, px - r.left, py - r.top);
          if (nested) return nested;
        } catch (e) {}
      }
    }
    return null;
  }
  const prev = document.querySelector("[data-maclaw-caret]");
  const el = pick(document, x, y);
  if (!el) return JSON.stringify({ok:false, same:false});
  const same = prev === el;
  const stamps = document.querySelectorAll("[data-maclaw-caret]");
  for (const node of stamps) if (node !== el) node.removeAttribute("data-maclaw-caret");
  try { el.setAttribute("data-maclaw-caret", "1"); } catch (e) {}
  el.focus();
  try {
    if (el.setSelectionRange) {
      const n = (el.value || "").length;
      el.setSelectionRange(n, n);
    }
  } catch (e) {}
  return JSON.stringify({ok:true, same: same});
})()`
}

func viewportSetValueJS(text string) string {
	quoted, _ := json.Marshal(text)
	return fmt.Sprintf(`(function() {
  const text = %s;
`, string(quoted)) + viewportIsEditableJS + `  let el = document.querySelector("[data-maclaw-caret]");
  if (!isEd(el)) el = document.activeElement;
  if (el && el.tagName === "IFRAME") {
    try {
      const doc = el.contentDocument;
      el = doc && (doc.querySelector("[data-maclaw-caret]") || doc.activeElement);
    } catch (e) { el = null; }
  }
  if (!isEd(el)) return JSON.stringify({error:"no focused editable element"});
  el.focus();
  if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
    const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const desc = Object.getOwnPropertyDescriptor(proto, "value");
    const tracker = el._valueTracker;
    if (tracker) tracker.setValue(text === el.value ? "" : el.value);
    if (desc && desc.set) desc.set.call(el, text); else el.value = text;
    el.dispatchEvent(new InputEvent("input", {bubbles:true, cancelable:true, inputType:"insertText", data:text}));
    try { el.setSelectionRange(text.length, text.length); } catch (e) {}
    return JSON.stringify({ok:true});
  }
  el.textContent = text;
  el.dispatchEvent(new InputEvent("input", {bubbles:true, cancelable:true, inputType:"insertText", data:text}));
  return JSON.stringify({ok:true});
})()`
}
