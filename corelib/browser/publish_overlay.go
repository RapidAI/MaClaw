package browser

import (
	"fmt"
	"strings"
	"time"
)

// openOverlayJS lists visible popups that sit over the page: suggestion
// lists, menus, dialogs, tooltips and fixed layers that the caret or a
// typed "#tag"/"@name" opened. Such a layer takes the next click, so a
// submit pressed while it is open never reaches the button.
const openOverlayJS = `(() => {
  const vis = e => { const r = e.getBoundingClientRect(), s = getComputedStyle(e);
    return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none' && +s.opacity !== 0; };
  const out = [];
  const seen = new Set();
  const add = (e, why) => { if (seen.has(e) || !vis(e)) return; seen.add(e);
    out.push(why + ':' + (e.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 60)); };
  document.querySelectorAll('[role=listbox],[role=menu],[role=dialog],[role=tooltip],[aria-modal=true]').forEach(e => add(e, e.getAttribute('role') || 'modal'));
  document.querySelectorAll('[aria-expanded=true]').forEach(e => { const id = e.getAttribute('aria-controls') || e.getAttribute('aria-owns'); const t = id && document.getElementById(id); if (t) add(t, 'expanded'); });
  return JSON.stringify(out.slice(0, 6));
})()`

// pageStateJS reads what the page says after a submit: live regions,
// alerts and toasts, plus the open overlays.
var pageStateJS = `(() => {
  const vis = e => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
  const said = [];
  document.querySelectorAll('[role=alert],[role=status],[aria-live]').forEach(e => { const t = (e.innerText || '').trim(); if (t && vis(e)) said.push(t.slice(0, 80)); });
  return JSON.stringify({said: said.slice(0, 5), overlays: JSON.parse(` + openOverlayJS + `), url: location.href});
})()`

// closeOverlays shuts open popups the generic way a person would: Escape,
// then leaving the field that opened them. It reports what it closed.
func (p *publishRun) closeOverlays() string {
	open, _ := p.eval(openOverlayJS)
	if open == "" || open == "[]" {
		return ""
	}
	for _, typ := range []string{"keyDown", "keyUp"} {
		_, _ = p.send("Input.dispatchKeyEvent", map[string]interface{}{"type": typ, "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	}
	_, _ = p.eval("document.activeElement && document.activeElement.blur && document.activeElement.blur(), 1")
	time.Sleep(300 * time.Millisecond)
	if still, _ := p.eval(openOverlayJS); still != "" && still != "[]" {
		return fmt.Sprintf("closed %s; still open %s", open, still)
	}
	return "closed " + open
}

// afterSubmitState names everything that could explain a submit that did
// not take: the page's own words, overlays still open, and every write the
// page sent after the click with its answer.
func (p *publishRun) afterSubmitState(since time.Time) string {
	var parts []string
	if st, _ := p.eval(pageStateJS); st != "" {
		parts = append(parts, "page "+clipRunes(st, 400))
	}
	if p.net != nil {
		w := p.net.writesSince(since)
		if len(w) == 0 {
			parts = append(parts, "no write was sent after the click")
		} else {
			parts = append(parts, "writes after the click: "+strings.Join(w, "; "))
		}
	}
	return strings.Join(parts, "; ")
}
