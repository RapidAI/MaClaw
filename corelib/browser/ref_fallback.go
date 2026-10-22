package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Ref click fallback. A probe ref can come without any selector or backend
// node (seen live 10-22 on Zhihu's 发布 / topic controls: "ref @e42 has no
// selector candidates; run probe again", twice in one run). Instead of
// failing, the element is found again by its accessible name (and role) on
// the live page and clicked at its center with real mouse events; when no
// element by that name is visible, the probe bounding-box center is used.

const refByNameExpr = `(function(name, role){
const norm=s=>(s||"").replace(/\s+/g," ").trim().toLowerCase();
const want=norm(name); if(!want) return JSON.stringify({});
const sel='button,a[href],input,textarea,select,summary,label,[role],[tabindex],[contenteditable=""],[contenteditable=true]';
let best=null,score=0;
for(const el of document.querySelectorAll(sel)){
  const r=el.getBoundingClientRect(); if(r.width<1||r.height<1) continue;
  const st=getComputedStyle(el); if(st.visibility==="hidden"||st.display==="none") continue;
  const label=norm(el.getAttribute("aria-label")||el.getAttribute("placeholder")||el.innerText||el.value||el.title);
  let sc=0; if(label===want) sc=3; else if(label.startsWith(want)&&label.length<=want.length+4) sc=1; else continue;
  if(role && norm(el.getAttribute("role")||el.tagName)===norm(role)) sc+=1;
  if(sc>score){score=sc;best=el}
}
if(!best) return JSON.stringify({});
best.scrollIntoView({block:"center",inline:"center"});
const r=best.getBoundingClientRect();
const top=document.elementFromPoint(r.left+r.width/2,r.top+r.height/2);
return JSON.stringify({x:r.left+r.width/2,y:r.top+r.height/2,found:true,disabled:!!(best.disabled||best.getAttribute("aria-disabled")==="true"),covered:!!(top&&top!==best&&!best.contains(top))});
})(%s,%s)`

type refPoint struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Found    bool    `json:"found"`
	Disabled bool    `json:"disabled"`
	Covered  bool    `json:"covered"`
}

// refFallbackPoint locates a selector-less ref on the live page.
func (s *Session) refFallbackPoint(resolved *BrowserElementRef) (refPoint, string, error) {
	var p refPoint
	if resolved == nil {
		return p, "", fmt.Errorf("browser ref is nil")
	}
	name := strings.TrimSpace(resolved.Name)
	if name == "" {
		name = strings.TrimSpace(resolved.Text)
	}
	if name != "" && (resolved.FrameID == "" || resolved.FrameID == "main") {
		n, _ := json.Marshal(name)
		r, _ := json.Marshal(resolved.Role)
		if raw, err := s.eval(fmt.Sprintf(refByNameExpr, n, r), false, 3*time.Second); err == nil && json.Unmarshal([]byte(raw), &p) == nil && p.Found {
			if p.Disabled {
				return p, "name", fmt.Errorf("ref %s (%q) is disabled on the page; it cannot be clicked yet", resolved.Ref, name)
			}
			return p, "name", nil
		}
	}
	b := resolved.BoundingBox
	if b.Width >= 1 && b.Height >= 1 {
		return refPoint{X: b.X + b.Width/2, Y: b.Y + b.Height/2}, "bbox", nil
	}
	return p, "", fmt.Errorf("ref %s has no selector candidates and no name or box to find it again; run probe again", resolved.Ref)
}

// clickPoint sends a real left click at viewport coordinates.
func (s *Session) clickPoint(x, y float64) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return fmt.Errorf("browser session not connected")
	}
	_, _ = client.Send("Input.dispatchMouseEvent", map[string]interface{}{"type": "mouseMoved", "x": x, "y": y}, DefaultCmdTimeout)
	if _, err := client.Send("Input.dispatchMouseEvent", map[string]interface{}{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": 1}, DefaultCmdTimeout); err != nil {
		return fmt.Errorf("mousePressed: %w", err)
	}
	_, err := client.Send("Input.dispatchMouseEvent", map[string]interface{}{"type": "mouseReleased", "x": x, "y": y, "button": "left", "clickCount": 1}, DefaultCmdTimeout)
	return err
}

// clickRefFallback clicks a ref that has no selector and no backend node.
func (s *BrowserAgentSession) clickRefFallback(resolved *BrowserElementRef) (string, error) {
	if s == nil || s.session == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	p, how, err := s.session.refFallbackPoint(resolved)
	if err != nil {
		return "", err
	}
	if err := s.session.clickPoint(p.X, p.Y); err != nil {
		return "", err
	}
	log.Printf("[browser] ref %s had no selector; clicked by %s at %.0f,%.0f", resolved.Ref, how, p.X, p.Y)
	return fmt.Sprintf("%s (by %s at %.0f,%.0f)", resolved.Ref, how, p.X, p.Y), nil
}

// refFindableAgain reports a selector-less ref that the fallback can still
// locate (by name or by its probe box).
func refFindableAgain(r *BrowserElementRef) bool {
	if r == nil {
		return false
	}
	return strings.TrimSpace(r.Name) != "" || strings.TrimSpace(r.Text) != "" || (r.BoundingBox.Width >= 1 && r.BoundingBox.Height >= 1)
}
