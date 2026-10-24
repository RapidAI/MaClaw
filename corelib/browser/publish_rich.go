package browser

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Structured publish bodies. The model writes the body as light Markdown
// (## headings, **bold**, lists, > quotes) or HTML, and marks where each
// picture goes with an anchor on its own line: [[img:N]] or
// [[img:N|caption]] places files[N-1] there. Rich editors get the HTML by a
// paste (the editor converts it, keeping bold and headings) with each picture
// pasted at its anchor; plain-text editors get readable text (bold becomes
// 【…】). The read-back fails the publish when a picture is missing or out of
// place, or the bold/headings did not survive.

type richDoc struct {
	segments []string // HTML before the first anchor, between anchors, after the last
	texts    []string // plain text of each segment
	anchors  []int    // file index (0-based) for the picture after segments[i]
	captions []string
	plain    string // whole body as plain text for non-rich editors
	text     string // visible text in a rich editor (no markup)
	bold     []string
	headings int
	rich     bool // the body carries structure or anchors
}

var (
	richAnchorRe   = regexp.MustCompile(`^\s*\[\[\s*(?:img|image|图)\s*[:：]\s*(\d+)\s*(?:\|([^\]]*))?\]\]\s*$`)
	richHeadRe     = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	richULRe       = regexp.MustCompile(`^\s*[-*•]\s+(.+)$`)
	richOLRe       = regexp.MustCompile(`^\s*(\d+)[.)、]\s+(.+)$`)
	richQuoteRe    = regexp.MustCompile(`^\s*>\s?(.*)$`)
	richBoldRe     = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	richItalRe     = regexp.MustCompile(`(^|[^*])\*([^*\n]+?)\*`)
	richLinkRe     = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
	richTagRe      = regexp.MustCompile(`(?i)</?(p|h[1-6]|strong|b|em|ul|ol|li|blockquote|br)\b[^>]*>`)
	richAnyTagRe   = regexp.MustCompile(`<[^>]+>`)
	richHTMLBoldRe = regexp.MustCompile(`(?is)<(strong|b)\b[^>]*>(.*?)</(strong|b)>`)
	richHTMLHeadRe = regexp.MustCompile(`(?i)<h[1-6]\b`)
)

func parseRichBody(body string, files int) richDoc {
	var d richDoc
	body = strings.ReplaceAll(body, "\r\n", "\n")
	isHTML := richTagRe.MatchString(body)
	var cur []string
	flush := func() {
		seg := strings.Join(cur, "\n")
		cur = nil
		var h string
		if isHTML {
			h = seg
		} else {
			h = richMarkdownHTML(seg)
		}
		d.segments = append(d.segments, h)
		d.texts = append(d.texts, richHTMLText(h))
	}
	for _, ln := range strings.Split(body, "\n") {
		if m := richAnchorRe.FindStringSubmatch(ln); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n >= 1 && (files == 0 || n <= files) {
				flush()
				d.anchors = append(d.anchors, n-1)
				d.captions = append(d.captions, strings.TrimSpace(m[2]))
				continue
			}
		}
		cur = append(cur, ln)
	}
	flush()
	// a caption is the first line of the next segment, in italics
	for i, c := range d.captions {
		if c != "" {
			d.segments[i+1] = "<p><em>" + html.EscapeString(c) + "</em></p>" + d.segments[i+1]
			d.texts[i+1] = c + "\n" + d.texts[i+1]
		}
	}
	all := strings.Join(d.segments, "")
	for _, m := range richHTMLBoldRe.FindAllStringSubmatch(all, -1) {
		if t := strings.TrimSpace(richHTMLText(m[2])); t != "" {
			d.bold = append(d.bold, t)
		}
	}
	d.headings = len(richHTMLHeadRe.FindAllString(all, -1))
	d.rich = len(d.anchors) > 0 || len(d.bold) > 0 || d.headings > 0 || isHTML
	d.text = strings.Join(d.texts, "\n")
	d.plain = richHTMLPlain(all)
	if !d.rich {
		d.plain = body
	}
	return d
}

func richInline(s string) string {
	s = html.EscapeString(s)
	s = richLinkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = richBoldRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := richBoldRe.FindStringSubmatch(m)
		t := sm[1]
		if t == "" {
			t = sm[2]
		}
		return "<strong>" + t + "</strong>"
	})
	s = richItalRe.ReplaceAllString(s, "$1<em>$2</em>")
	return s
}

// richMarkdownHTML converts the light Markdown the pipeline accepts.
func richMarkdownHTML(md string) string {
	var b strings.Builder
	var para []string
	list := ""
	endPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + strings.Join(para, "<br>") + "</p>")
			para = nil
		}
	}
	endList := func() {
		if list != "" {
			b.WriteString("</" + list + ">")
			list = ""
		}
	}
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case t == "":
			endPara()
			endList()
		case richHeadRe.MatchString(t):
			endPara()
			endList()
			m := richHeadRe.FindStringSubmatch(t)
			lv := len(m[1]) + 1
			if lv > 4 {
				lv = 4
			}
			fmt.Fprintf(&b, "<h%d>%s</h%d>", lv, richInline(strings.Trim(m[2], "# ")), lv)
		case richULRe.MatchString(ln) && !strings.HasPrefix(t, "**"):
			endPara()
			if list != "ul" {
				endList()
				b.WriteString("<ul>")
				list = "ul"
			}
			b.WriteString("<li>" + richInline(richULRe.FindStringSubmatch(ln)[1]) + "</li>")
		case richOLRe.MatchString(ln):
			endPara()
			if list != "ol" {
				endList()
				b.WriteString("<ol>")
				list = "ol"
			}
			b.WriteString("<li>" + richInline(richOLRe.FindStringSubmatch(ln)[2]) + "</li>")
		case richQuoteRe.MatchString(ln):
			endPara()
			endList()
			b.WriteString("<blockquote>" + richInline(richQuoteRe.FindStringSubmatch(ln)[1]) + "</blockquote>")
		default:
			endList()
			para = append(para, richInline(t))
		}
	}
	endPara()
	endList()
	return b.String()
}

var richBlockEndRe = regexp.MustCompile(`(?i)</(p|h[1-6]|li|blockquote|ul|ol)>|<br\s*/?>`)

func richHTMLText(h string) string {
	h = richBlockEndRe.ReplaceAllString(h, "\n")
	h = richAnyTagRe.ReplaceAllString(h, "")
	return strings.TrimSpace(html.UnescapeString(h))
}

// richHTMLPlain renders HTML for a plain-text editor: headings and bold stay
// visible as 【…】, list items get a bullet.
func richHTMLPlain(h string) string {
	h = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`).ReplaceAllString(h, "\n【$1】\n")
	h = richHTMLBoldRe.ReplaceAllString(h, "【$2】")
	h = regexp.MustCompile(`(?i)<li[^>]*>`).ReplaceAllString(h, "• ")
	h = regexp.MustCompile(`(?i)<a [^>]*href="([^"]+)"[^>]*>(.*?)</a>`).ReplaceAllString(h, "$2 $1")
	h = richBlockEndRe.ReplaceAllString(h, "\n")
	h = richAnyTagRe.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	h = regexp.MustCompile(`\n{3,}`).ReplaceAllString(h, "\n\n")
	return strings.TrimSpace(h)
}

// --- browser side ---

// publishPasteHTMLFn pastes HTML at the end of the editor; editors that
// ignore a synthetic paste get it through insertHTML.
const publishPasteHTMLFn = `function(h, t) { this.focus(); const end = () => { const s = getSelection(); const r = document.createRange(); r.selectNodeContents(this); r.collapse(false); s.removeAllRanges(); s.addRange(r); }; end(); const before = (this.innerText || '').length; const dt = new DataTransfer(); dt.setData('text/html', h); dt.setData('text/plain', t); const ev = new ClipboardEvent('paste', {clipboardData: dt, bubbles: true, cancelable: true}); this.dispatchEvent(ev); if (!ev.defaultPrevented || (this.innerText || '').length <= before) { end(); document.execCommand('insertHTML', false, h); } return String((this.innerText || '').length - before); }`

// publishPickerJS makes (once) a hidden file input of our own; the browser
// reads the files there, so the server never needs them on its disk.
const publishPickerJS = `(() => { let e = document.getElementById('__pub_picker'); if (!e) { e = document.createElement('input'); e.type = 'file'; e.multiple = true; e.id = '__pub_picker'; e.style.display = 'none'; document.documentElement.appendChild(e); } return e; })()`

func (p *publishRun) loadPicker() (string, error) {
	obj, err := p.element(publishPickerJS)
	if err != nil || obj == "" {
		return "", fmt.Errorf("no picker: %v", err)
	}
	if _, err := p.send("DOM.setFileInputFiles", map[string]interface{}{"objectId": obj, "files": p.spec.Files}); err != nil {
		return "", fmt.Errorf("browser could not read the files: %v", err)
	}
	n, _ := p.eval(`String(document.getElementById('__pub_picker').files.length)`)
	if n != strconv.Itoa(len(p.spec.Files)) {
		return "", fmt.Errorf("browser read %s of %d files", n, len(p.spec.Files))
	}
	return obj, nil
}

func (p *publishRun) imgCount(obj string) int {
	v, _ := p.call(obj, `function() { return String(this.querySelectorAll('img').length); }`)
	n, _ := strconv.Atoi(v)
	return n
}

// pasteFile pastes picker file k at the end of the editor and waits for one
// more <img>.
func (p *publishRun) pasteFile(obj string, k int) error {
	before := p.imgCount(obj)
	fn := `function(k) { const f = document.getElementById('__pub_picker').files[k]; this.focus(); const s = getSelection(); const r = document.createRange(); r.selectNodeContents(this); r.collapse(false); s.removeAllRanges(); s.addRange(r); const dt = new DataTransfer(); dt.items.add(f); this.dispatchEvent(new ClipboardEvent('paste', {clipboardData: dt, bubbles: true, cancelable: true})); return 'ok'; }`
	if _, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": fn, "arguments": []map[string]interface{}{{"value": k}}, "returnByValue": true}); err != nil {
		return err
	}
	deadline := time.Now().Add(minDur(p.spec.StepBudget, 20*time.Second))
	for time.Now().Before(deadline) {
		if p.imgCount(obj) > before {
			time.Sleep(300 * time.Millisecond)
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("picture %d did not show in the editor", k+1)
}

func (p *publishRun) pasteHTML(obj, h, text string) error {
	if strings.TrimSpace(text) == "" && !strings.Contains(h, "<") {
		return nil
	}
	_, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishPasteHTMLFn, "arguments": []map[string]interface{}{{"value": h}, {"value": text}}, "returnByValue": true})
	time.Sleep(250 * time.Millisecond)
	return err
}

// fillRich writes a structured body into a rich editor: each segment pasted
// as HTML, each anchored picture pasted right after its segment.
func (p *publishRun) fillRich(obj string) (string, error) {
	if err := p.focusAndClear(obj); err != nil {
		return "", err
	}
	if len(p.doc.anchors) > 0 {
		if _, err := p.loadPicker(); err != nil {
			return "", err
		}
	}
	for i, seg := range p.doc.segments {
		if err := p.pasteHTML(obj, seg, p.doc.texts[i]); err != nil {
			return "", err
		}
		if i < len(p.doc.anchors) {
			if err := p.pasteFile(obj, p.doc.anchors[i]); err != nil {
				return "", err
			}
		}
	}
	p.imagesPlaced = len(p.doc.anchors)
	got, _ := p.call(obj, readValueFn)
	if !holds(got, p.doc.text) {
		return "", fmt.Errorf("body read back %d chars (%q) instead of the text", len([]rune(got)), clipRunes(got, 24))
	}
	return fmt.Sprintf("body holds %d chars as formatted text with %d picture(s) at their anchors", len([]rune(got)), len(p.doc.anchors)), nil
}

// publishStructureFn checks a container (the editor, or the published
// article) for the pictures in order at their anchors and for the bold and
// heading formatting. It returns JSON {imgs, misplaced:[...], bold, heads}.
const publishStructureFn = `function(spec) { const n = s => (s || '').replace(/\s+/g, ''); let txt = ''; const imgs = []; const w = document.createTreeWalker(this, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT); for (let x = w.currentNode; x; x = w.nextNode()) { if (x.nodeType === 3) { const p = x.parentElement; if (p && p.closest('script,style,noscript')) continue; txt += n(x.nodeValue); } else if (x.tagName === 'IMG') { const r = x.getBoundingClientRect(); const big = Math.max(x.naturalWidth || 0, r.width || 0) >= 60; if (big) imgs.push(txt.length); } } const mis = []; for (let i = 0; i < spec.anchors; i++) { const at = imgs[i]; if (at === undefined) { mis.push('picture ' + (i + 1) + ' missing'); continue; } const a = n(spec.before[i]).slice(-12), b = n(spec.after[i]).slice(0, 12); const ia = a ? txt.indexOf(a) : 0; const ib = b ? txt.indexOf(b, ia < 0 ? 0 : ia) : txt.length; if (ia < 0 || ib < 0) { mis.push('text around picture ' + (i + 1) + ' not found'); continue; } if (at < ia + a.length - 1 || at > ib + 1) mis.push('picture ' + (i + 1) + ' is not after "' + spec.before[i].slice(-12) + '"'); } const bel = [...this.querySelectorAll('strong,b')].concat([...this.querySelectorAll('span,em')].filter(e => parseInt(getComputedStyle(e).fontWeight) >= 600)); const btxt = n(bel.map(e => e.innerText).join('|')); const boldOK = spec.bold.filter(b => btxt.includes(n(b).slice(0, 8))).length; return JSON.stringify({imgs: imgs.length, misplaced: mis, bold: boldOK, heads: this.querySelectorAll('h1,h2,h3,h4,h5,h6').length}); }`

type structureResult struct {
	Imgs      int      `json:"imgs"`
	Misplaced []string `json:"misplaced"`
	Bold      int      `json:"bold"`
	Heads     int      `json:"heads"`
}

func (p *publishRun) structureSpec() map[string]interface{} {
	var before, after []string
	for i := range p.doc.anchors {
		before = append(before, p.doc.texts[i])
		after = append(after, p.doc.texts[i+1])
	}
	bold := p.doc.bold
	if bold == nil {
		bold = []string{}
	}
	if before == nil {
		before, after = []string{}, []string{}
	}
	return map[string]interface{}{"anchors": len(p.doc.anchors), "before": before, "after": after, "bold": bold}
}

// checkStructure runs publishStructureFn on obj and names what is wrong.
func (p *publishRun) checkStructure(obj, where string) (string, error) {
	raw, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishStructureFn, "arguments": []map[string]interface{}{{"value": p.structureSpec()}}, "returnByValue": true})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &out)
	var r structureResult
	_ = json.Unmarshal([]byte(out.Result.Value), &r)
	var bad []string
	bad = append(bad, r.Misplaced...)
	if nb := len(p.doc.bold); nb > 0 && r.Bold*2 < nb {
		bad = append(bad, fmt.Sprintf("bold kept on %d of %d key points", r.Bold, nb))
	}
	if p.doc.headings > 0 && r.Heads == 0 {
		bad = append(bad, "headings were lost")
	}
	sum := fmt.Sprintf("%s: %d picture(s), %d/%d bold, %d heading(s)", where, r.Imgs, r.Bold, len(p.doc.bold), r.Heads)
	if len(bad) > 0 {
		return sum, fmt.Errorf("%s: %s", where, strings.Join(bad, "; "))
	}
	return sum + " — pictures in place, formatting kept", nil
}

// articleExpr finds the published article: the smallest element holding the
// start and the end of the body text.
func (p *publishRun) articleExpr() string {
	a := clipRunes(normText(p.doc.texts[0]), 10)
	z := []rune(normText(p.doc.text))
	if len(z) > 10 {
		z = z[len(z)-10:]
	}
	return `(() => { const n = s => (s || '').replace(/\s+/g, ''); const a = ` + jsq(a) + `, z = ` + jsq(string(z)) + `; let best = null; for (const e of document.querySelectorAll('article,main,section,div')) { const t = n(e.innerText); if (t.includes(a) && t.includes(z) && (!best || t.length < n(best.innerText).length)) best = e; } return best; })()`
}

// checkPublished re-reads the published page for pictures and formatting.
func (p *publishRun) checkPublished() (string, error) {
	var last error
	var sum string
	deadline := time.Now().Add(minDur(p.spec.StepBudget, 15*time.Second))
	for {
		obj, err := p.element(p.articleExpr())
		if err == nil && obj != "" {
			_, _ = p.eval(`window.scrollTo(0, document.body.scrollHeight)`)
			sum, last = p.checkStructure(obj, "published page")
			if last == nil {
				return sum, nil
			}
		} else {
			last = fmt.Errorf("published page: article text not found")
		}
		if time.Now().After(deadline) {
			return sum, last
		}
		time.Sleep(time.Second)
	}
}
