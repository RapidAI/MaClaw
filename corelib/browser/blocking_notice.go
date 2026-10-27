package browser

import (
	"encoding/json"
	"strings"
	"time"
)

// Blocking notices: a short visible line that says the account may not do
// the action yet ("转正答题后才能发布专栏", "You cannot post until ...").
// Sites often render them inside a same-origin iframe (the editor frame),
// where a main-document text read never sees them, so the walk covers
// every same-origin document. The wording is generic; no site is named.
const blockingNoticeFnJS = `(() => {
  const re = /(才能|请先|无法|不能|暂不|暂时不|没有权限|无权限|权限不足|受限|禁止|未开通|未认证|未实名|cannot|can't|not allowed|unable to|no permission|permission denied|restricted|not eligible)/i;
  const docs = [];
  const walk = (d, depth) => { if (!d || depth > 3) return; docs.push(d); for (const f of d.querySelectorAll('iframe,frame')) { let cd = null; try { cd = f.contentDocument; } catch (e) {} if (cd) walk(cd, depth + 1); } };
  walk(document, 0);
  const vis = e => { const w = e.ownerDocument.defaultView; const r = e.getBoundingClientRect(); if (r.width <= 0 || r.height <= 0) return false; for (let x = e; x && x.nodeType === 1; x = x.parentElement) { const s = w.getComputedStyle(x); if (s.display === 'none' || s.visibility === 'hidden' || Number(s.opacity) === 0) return false; } return true; };
  const out = [];
  for (const d of docs) {
    if (!d.body) continue;
    for (const e of d.body.querySelectorAll('p,span,div,li,h1,h2,h3,h4,label,strong,em,a,button,[role=alert],[role=status]')) {
      if (out.length >= 3) break;
      if (e.children.length > 3 || e.closest('[contenteditable=true],textarea,.CodeMirror,.ProseMirror')) continue;
      const t = (e.innerText || '').replace(/\s+/g, ' ').trim();
      if (t.length < 4 || t.length > 80 || !re.test(t) || /^(cookie|隐私|privacy)/i.test(t)) continue;
      if (!vis(e) || out.some(o => o.includes(t) || t.includes(o))) continue;
      out.push(t);
    }
  }
  return out;
})`

// blockingNotices reads the notices of the current page, "" when none.
func (p *publishRun) blockingNotices() string {
	out, err := p.evalWithin("JSON.stringify("+blockingNoticeFnJS+"())", 4*time.Second)
	if err != nil {
		return ""
	}
	var lines []string
	_ = json.Unmarshal([]byte(out), &lines)
	return strings.Join(lines, " | ")
}
