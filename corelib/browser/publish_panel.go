package browser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Publish settings panels. Many sites answer the first publish click with
// a dialog or drawer (发文设置, "Publish settings") that holds required
// fields (tags, a category) and a second publish button of its own; the
// post only goes out once that panel is complete and confirmed. The run
// handles every such panel the same way: find the layer that became
// visible after the click, list its required fields, fill them from the
// spec (Tags, Category) or the content, and press the panel's own publish
// button. Nothing here names a site.

// publishPanelMarkJS marks the layers visible before the click, so the
// panel that the click opened is the visible one without the mark.
const publishPanelCandidates = `'[role=dialog],[aria-modal=true],[class*=dialog i],[class*=modal i],[class*=drawer i],[class*=popup i],[class*=panel i],[class*=setting i],[class*=publish i]'`

const publishPanelMarkJS = `(() => {` + publishHelpersJS + `
  let n = 0;
  for (const e of __all(` + publishPanelCandidates + `)) { if (__vis(e)) { e.setAttribute('data-mc-pre', '1'); n++; } }
  return String(n);
})()`

// publishPanelScanJS finds the newest visible panel that holds a publish
// verb button and is not the editor; it marks it data-mc-panel, its
// confirm button data-mc-confirm and its fields data-mc-field=<i>, and
// lists the fields with their label, kind (tags/category/other), whether
// they are required and still empty.
const publishPanelScanJS = `((verbs, not) => {` + publishHelpersJS + `
  const vre = new RegExp('^(' + verbs.map(v => v.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|') + ')', 'i');
  const nre = new RegExp(not, 'i');
  const z = s => String(s || '').replace(/\s+/g, '');
  const body = __body('', null);
  const btnOf = p => { const bs = [...p.querySelectorAll('button,[role=button],a,input[type=submit],input[type=button]')].filter(b => __vis(b) && !b.disabled && b.getAttribute('aria-disabled') !== 'true'); const ok = bs.filter(b => { const t = z(b.innerText || b.value); return t && t.length <= 10 && vre.test(t) && !nre.test(t); }); return ok[ok.length - 1] || null; };
  let best = null, bestArea = 0;
  for (const e of __all(` + publishPanelCandidates + `)) {
    if (e.getAttribute('data-mc-pre') || !__vis(e) || (body && (e.contains(body) || e === body))) continue;
    const a = __area(e); if (a < 20000) continue;
    const b = btnOf(e); if (!b) continue;
    if (!best || a < bestArea) { best = e; bestArea = a; }
  }
  document.querySelectorAll('[data-mc-panel],[data-mc-confirm],[data-mc-field]').forEach(e => { e.removeAttribute('data-mc-panel'); e.removeAttribute('data-mc-confirm'); e.removeAttribute('data-mc-field'); });
  if (!best) return JSON.stringify({panel: false});
  best.setAttribute('data-mc-panel', '1');
  btnOf(best).setAttribute('data-mc-confirm', '1');
  const rows = [];
  const seen = new Set();
  const rowOf = el => { let r = el; for (let i = 0; i < 5 && r.parentElement && r.parentElement !== best; i++) { r = r.parentElement; if (z(r.innerText).length > 2 && r.querySelector('label,[class*=label i],[class*=title i],span,div')) { const t = z(r.innerText); if (t.length < 200) break; } } return r; };
  const labelOf = r => z(r.innerText).slice(0, 40);
  const fields = [...best.querySelectorAll('input:not([type=hidden]):not([type=file]),textarea,select,[contenteditable=true]')];
  // Rows marked required with a star whose input appears only after an
  // "add" button: list the button.
  for (const lab of best.querySelectorAll('label,[class*=label i],[class*=title i],[class*=required i],span,div')) {
    const t = z(lab.innerText);
    if (!t || t.length > 20 || !/[*＊]/.test(t + (getComputedStyle(lab, '::before').content || '') + (getComputedStyle(lab, '::after').content || '')) && !/required/i.test(lab.className || '')) continue;
    const r = rowOf(lab);
    if (seen.has(r)) continue;
    seen.add(r);
    rows.push({row: r, required: true});
  }
  for (const f of fields) { if (!__vis(f)) continue; const r = rowOf(f); if (seen.has(r)) continue; seen.add(r); rows.push({row: r, required: !!(f.required || f.getAttribute('aria-required') === 'true' || /required/i.test(r.className || ''))}); }
  const out = [];
  rows.forEach((x, i) => {
    const r = x.row;
    const label = labelOf(r);
    const kind = /标签|话题|关键词|tag|topic|keyword/i.test(label) ? 'tags' : /分类|栏目|类型|类别|category|column|type/i.test(label) ? 'category' : 'other';
    const input = [...r.querySelectorAll('input:not([type=hidden]):not([type=file]):not([type=radio]):not([type=checkbox]),textarea,[contenteditable=true]')].find(__vis);
    const radios = [...r.querySelectorAll('input[type=radio],[role=radio]')];
    const select = r.querySelector('select');
    const chips = [...r.querySelectorAll('[class*=tag i],[class*=chip i],[class*=selected i]')].filter(e => __vis(e) && z(e.innerText) && !/添加|add|\+/i.test(z(e.innerText))).length;
    let empty = true;
    if (input && (input.value || (input.isContentEditable && z(input.innerText)))) empty = false;
    if (chips > 0) empty = false;
    if (radios.length && radios.some(o => o.checked || o.getAttribute('aria-checked') === 'true')) empty = false;
    if (select && select.value) empty = false;
    const add = [...r.querySelectorAll('button,[role=button],span,a,div')].filter(e => __vis(e) && e.children.length <= 2 && /^(\+|＋)?(添加|新增|add)/i.test(z(e.innerText)) && z(e.innerText).length <= 12).pop();
    r.setAttribute('data-mc-field', String(i));
    if (input) input.setAttribute('data-mc-input', String(i));
    if (add) add.setAttribute('data-mc-add', String(i));
    out.push({i, label, kind, required: x.required, empty, input: !!input, add: !!add, radios: radios.length, select: !!select, options: select ? [...select.options].map(o => o.text).slice(0, 20) : radios.map(o => z((o.closest('label') || o.parentElement || o).innerText)).slice(0, 20)});
  });
  return JSON.stringify({panel: true, text: z(best.innerText).slice(0, 160), confirm: z(btnOf(best).innerText || btnOf(best).value), fields: out});
})`

type panelField struct {
	I        int      `json:"i"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Empty    bool     `json:"empty"`
	Input    bool     `json:"input"`
	Add      bool     `json:"add"`
	Radios   int      `json:"radios"`
	Select   bool     `json:"select"`
	Options  []string `json:"options"`
}

type panelScan struct {
	Panel   bool         `json:"panel"`
	Text    string       `json:"text"`
	Confirm string       `json:"confirm"`
	Fields  []panelField `json:"fields"`
}

func (p *publishRun) scanPanel() (panelScan, error) {
	verbs, _ := json.Marshal(append(append([]string(nil), p.spec.SubmitTexts...), submitVerbs...))
	out, err := p.evalWithin(publishPanelScanJS+"("+string(verbs)+", "+jsq(strings.TrimPrefix(submitNot.String(), "(?i)"))+")", 5*time.Second)
	var sc panelScan
	if err != nil {
		return sc, err
	}
	err = json.Unmarshal([]byte(out), &sc)
	return sc, err
}

// hashtagRe: #tags written in the body ("#AI #论文").
var hashtagRe = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_+.-]{1,20})`)

// panelTags is what goes into a tag field: the spec's tags, else the body's
// #hashtags, else up to two short pieces of the title.
func (p *publishRun) panelTags() []string {
	if len(p.spec.Tags) > 0 {
		return p.spec.Tags
	}
	return contentTags(p.spec.Title, p.spec.Body)
}

func contentTags(title, body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] && len(out) < 3 {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	for _, m := range hashtagRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	if len(out) > 0 {
		return out
	}
	for _, part := range regexp.MustCompile(`[\s:：|｜,，。、;；!！?？'"“”‘’()（）《》\[\]【】—-]+`).Split(title, -1) {
		if r := []rune(part); len(r) >= 2 && len(r) <= 10 {
			add(part)
		}
		if len(out) >= 2 {
			break
		}
	}
	return out
}

// pickOption: the option naming want, else the first non-placeholder one.
func pickOption(options []string, want string) int {
	if want != "" {
		for i, o := range options {
			if strings.Contains(normText(o), normText(want)) {
				return i
			}
		}
	}
	for i, o := range options {
		if t := normText(o); t != "" && !regexp.MustCompile(`请选择|select|choose|--`).MatchString(strings.ToLower(t)) {
			return i
		}
	}
	return -1
}

// typeInto focuses an element (real click, else focus()) and types text.
func (p *publishRun) typeInto(obj, text string, enter bool) error {
	if err := p.clickObject(obj); err != nil {
		_, _ = p.call(obj, `function(){ this.scrollIntoView({block:'center'}); this.focus(); }`)
	}
	if _, err := p.send("Input.insertText", map[string]interface{}{"text": text}); err != nil {
		return err
	}
	if enter {
		time.Sleep(400 * time.Millisecond)
		return p.key("Enter", "Enter", 13, 0, nil)
	}
	return nil
}

// fillPanel fills the panel's required empty fields; it returns what it did.
func (p *publishRun) fillPanel(sc panelScan) ([]string, []string) {
	var did, left []string
	for _, f := range sc.Fields {
		if !f.Required || !f.Empty {
			continue
		}
		sel := fmt.Sprintf(`[data-mc-field="%d"]`, f.I)
		switch {
		case f.Kind == "tags" || (f.Input && f.Kind == "other" && f.Radios == 0 && !f.Select):
			vals := p.panelTags()
			if f.Kind == "other" {
				left = append(left, f.Label)
				continue
			}
			if len(vals) == 0 {
				left = append(left, f.Label+" (no tags in the spec or content)")
				continue
			}
			for _, tag := range vals {
				obj, _ := p.element(`document.querySelector('[data-mc-input="` + fmt.Sprint(f.I) + `"]')`)
				if obj == "" || !f.Input {
					// The input shows after the row's add button.
					if add, _ := p.element(`document.querySelector('[data-mc-add="` + fmt.Sprint(f.I) + `"]')`); add != "" {
						if err := p.clickObject(add); err != nil {
							_, _ = p.call(add, `function(){ this.click(); }`)
						}
						time.Sleep(600 * time.Millisecond)
					}
					obj, _ = p.element(`(() => { const a = document.activeElement; if (a && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA' || a.isContentEditable)) return a; ` + publishHelpersJS + ` const r = document.querySelector('` + sel + `'); const c = __all('input:not([type=hidden]):not([type=file]):not([type=radio]):not([type=checkbox])').filter(e => __vis(e) && !e.value && (!r || r.contains(e) || /标签|tag|话题|回车|enter|搜索/i.test((e.placeholder || '')))); return c[c.length - 1] || null; })()`)
				}
				if obj == "" {
					left = append(left, f.Label+" (no input appeared)")
					break
				}
				if err := p.typeInto(obj, tag, true); err == nil {
					did = append(did, f.Label+"="+tag)
				}
				time.Sleep(500 * time.Millisecond)
			}
		case f.Radios > 0 || f.Select:
			i := pickOption(f.Options, p.spec.Category)
			if i < 0 {
				left = append(left, f.Label)
				continue
			}
			v, _ := p.eval(`(() => { const r = document.querySelector('` + sel + `'); if (!r) return 'gone'; const s = r.querySelector('select'); if (s) { s.selectedIndex = ` + fmt.Sprint(i) + `; s.dispatchEvent(new Event('input', {bubbles: true})); s.dispatchEvent(new Event('change', {bubbles: true})); return s.options[s.selectedIndex].text; } const o = [...r.querySelectorAll('input[type=radio],[role=radio]')][` + fmt.Sprint(i) + `]; if (!o) return 'gone'; (o.closest('label') || o).click(); return (o.closest('label') || o.parentElement || o).innerText; })()`)
			did = append(did, f.Label+"="+clipRunes(v, 20))
		default:
			left = append(left, f.Label)
		}
	}
	return did, left
}

// handlePanel fills and confirms a settings panel the click opened. ok is
// true when the panel's own publish button was pressed.
func (p *publishRun) handlePanel() (string, bool) {
	sc, err := p.scanPanel()
	if err != nil || !sc.Panel {
		return "", false
	}
	did, left := p.fillPanel(sc)
	if len(did) > 0 {
		time.Sleep(500 * time.Millisecond)
		if again, err := p.scanPanel(); err == nil && again.Panel {
			sc = again
		}
	}
	var still []string
	for _, f := range sc.Fields {
		if f.Required && f.Empty {
			still = append(still, f.Label)
		}
	}
	detail := fmt.Sprintf("settings panel %q", clipRunes(sc.Text, 40))
	if len(did) > 0 {
		detail += " filled " + strings.Join(did, ", ")
	}
	if len(still) > 0 {
		return detail + "; required field(s) still empty: " + strings.Join(still, ", ") + " " + strings.Join(left, ", "), false
	}
	obj, _ := p.element(`document.querySelector('[data-mc-confirm]')`)
	if obj == "" {
		return detail + "; its publish button is gone", false
	}
	if err := p.clickObject(obj); err != nil {
		return detail + "; its publish button could not be clicked: " + err.Error(), false
	}
	return detail + "; pressed " + sc.Confirm, true
}
