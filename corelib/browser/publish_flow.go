package browser

// Generic publish pipeline: observe -> upload -> title -> body -> readback ->
// submit -> verify result. Every step has a real success check and a time
// budget; the first failing step stops the flow and is named in the report.
// Nothing here knows a site: selectors, texts and URLs are parameters, and
// the defaults are generic heuristics (placeholder 标题/title, largest
// visible editor, an exact-text submit button found through every shadow
// root, closed ones included, via the CDP DOM tree).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// PublishDriver is the browser surface the pipeline needs.
type PublishDriver interface {
	Send(method string, params interface{}) (json.RawMessage, error)
	ArmChooser(files []string) error
	WaitChooser(timeout time.Duration) (bool, error)
	DisarmChooser()
}

// PublishSpec is everything the model supplies; no exploration.
type PublishSpec struct {
	Files          []string
	UploadSelector string
	UploadTrigger  string // visible text of the upload button when no file input exists yet
	Title          string
	TitleSelector  string
	Body           string
	BodySelector   string
	SubmitTexts    []string // exact button texts; default 发布/Publish/Post/提交
	ConfirmTexts   []string // a second confirm button, if the site asks
	VerifyURL      string   // page to open after submit where the post must be listed
	// EditorURL is the page where a new post is written; the run opens it
	// itself when the tab is elsewhere.
	EditorURL string
	// EditURL opens a published post for editing ({url} is the post URL);
	// with it a picture missing from the live post is put back.
	EditURL     string
	UpdateTexts []string // buttons that save an edited post (default 更新/发布/Update/Publish)
	StepBudget  time.Duration
}

type PublishStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	TookMS int64  `json:"took_ms"`
}

type PublishReport struct {
	Steps     []PublishStep `json:"steps"`
	ResultURL string        `json:"result_url,omitempty"`
	// PostID is an id-like attribute value found on the listed post when the
	// list has no direct link to it (verify_url pages often render cards).
	PostID string `json:"post_id,omitempty"`
	Stuck  string `json:"stuck,omitempty"`
	// State is the one answer to "is it published?", set from what the run
	// observed (see PublishState). Missing names what a live post lacks.
	State   PublishState `json:"state"`
	Missing string       `json:"missing,omitempty"`
}

// PublishState is the outcome of a publish run.
type PublishState string

const (
	// PublishNotSubmitted: stopped before the submit went out; nothing is
	// on the site and running the publish again is safe.
	PublishNotSubmitted PublishState = "not_submitted"
	// PublishSubmitted: the submit went out but the post was not confirmed;
	// it may be live. Never submit again for the same content.
	PublishSubmitted PublishState = "submitted"
	// PublishLive: the post was found and read back as filled.
	PublishLive PublishState = "live"
	// PublishLiveIncomplete: the post is live but lacks something (Missing).
	PublishLiveIncomplete PublishState = "live_incomplete"
	// PublishNoEditor: no editor to write in (wrong page or signed out).
	PublishNoEditor PublishState = "no_editor"
)

func (r *PublishReport) String() string {
	var b strings.Builder
	for _, s := range r.Steps {
		mark := "ok"
		if !s.OK {
			mark = "FAILED"
		}
		fmt.Fprintf(&b, "%s: %s (%dms) %s\n", s.Name, mark, s.TookMS, s.Detail)
	}
	if r.Stuck != "" {
		fmt.Fprintf(&b, "publish stopped at step %q; nothing after it ran", r.Stuck)
	} else if r.ResultURL != "" {
		fmt.Fprintf(&b, "published: %s", r.ResultURL)
		if r.PostID != "" {
			fmt.Fprintf(&b, " (post id %s)", r.PostID)
		}
	}
	return strings.TrimSpace(b.String())
}

var defaultSubmitTexts = []string{"发布", "发表", "Publish", "Post", "提交"}

// RunPublish runs the pipeline. err is non-nil exactly when a step failed.
func RunPublish(d PublishDriver, spec PublishSpec) (*PublishReport, error) {
	if spec.StepBudget <= 0 {
		spec.StepBudget = 30 * time.Second
	}
	if len(spec.SubmitTexts) == 0 {
		spec.SubmitTexts = defaultSubmitTexts
	}
	p := &publishRun{d: d, spec: spec, rep: &PublishReport{}, doc: parseRichBody(spec.Body, len(spec.Files))}
	p.net = startNetTracker(d)
	defer p.net.stop()
	steps := []struct {
		name string
		skip bool
		fn   func() (string, error)
	}{
		{"observe", false, p.observe},
		{"upload", len(spec.Files) == 0, p.uploadOrInline},
		{"fill_title", spec.Title == "", p.fillTitle},
		{"fill_body", spec.Body == "", p.fillBody},
		{"paste_images", len(spec.Files) == 0 || spec.Body == "", p.pasteImages},
		{"readback", spec.Title == "" && spec.Body == "", p.readback},
		{"submit", false, p.submit},
		{"verify_result", false, p.verifyAll},
	}
	for _, st := range steps {
		if st.skip {
			continue
		}
		start := time.Now()
		type res struct {
			detail string
			err    error
		}
		ch := make(chan res, 1)
		go func(fn func() (string, error)) {
			d, e := fn()
			ch <- res{d, e}
		}(st.fn)
		var r res
		budget := spec.StepBudget
		if st.name == "submit" || st.name == "verify_result" || st.name == "readback" || st.name == "observe" {
			// These steps end on the page's own events (uploads stored,
			// draft saved, post found); the budget is only a safety net.
			budget = publishSafetyBudget(spec.StepBudget)
		}
		select {
		case r = <-ch:
		case <-time.After(budget + 5*time.Second):
			r = res{"", fmt.Errorf("step exceeded its %s budget", budget)}
		}
		step := PublishStep{Name: st.name, OK: r.err == nil, Detail: r.detail, TookMS: time.Since(start).Milliseconds()}
		if r.err != nil {
			step.Detail = strings.TrimSpace(r.detail + " " + r.err.Error())
		}
		p.rep.Steps = append(p.rep.Steps, step)
		if strings.HasPrefix(st.name, "fill_") || st.name == "paste_images" {
			p.lastEdit = time.Now()
		}
		if r.err != nil {
			p.rep.Stuck = st.name
			p.rep.State = PublishNotSubmitted
			switch {
			case p.incomplete.Load() != nil:
				p.rep.State, p.rep.Missing = PublishLiveIncomplete, p.incomplete.Load().(string)
			case p.submitClicked.Load():
				p.rep.State = PublishSubmitted
			case p.noEditor:
				p.rep.State = PublishNoEditor
			}
			return p.rep, fmt.Errorf("publish step %s failed: %v", st.name, r.err)
		}
	}
	p.rep.State = PublishLive
	return p.rep, nil
}

type publishRun struct {
	submitClicked atomic.Bool
	// incomplete: the post is live but its re-read found something missing.
	incomplete atomic.Value // string
	// net follows the page's writing requests (nil: driver has no events).
	net *netTracker
	// lastEdit: when the run last changed the editor content.
	lastEdit time.Time
	// noEditor: the run found no editor to write in.
	noEditor bool
	// lastMissing: anchor indexes the last structure check found missing.
	lastMissing []int
	d           PublishDriver
	spec        PublishSpec
	rep         *PublishReport
	editURL     string
	// pasteLater: no file input showed the images; paste them into the
	// body editor after the text (paste_images step).
	pasteLater bool
	// doc is the structured body; richFilled when it went into a rich editor
	// as formatted text, imagesPlaced pictures at their anchors.
	doc          richDoc
	richFilled   bool
	imagesPlaced int
	gallery      bool
}

func (p *publishRun) send(method string, params interface{}) (json.RawMessage, error) {
	return p.d.Send(method, params)
}

// eval returns a page expression's value as a string.
func (p *publishRun) eval(expr string) (string, error) {
	raw, err := p.send("Runtime.evaluate", map[string]interface{}{"expression": expr, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.ExceptionDetails != nil {
		return "", fmt.Errorf("page error: %s", out.ExceptionDetails.Text)
	}
	if s, ok := out.Result.Value.(string); ok {
		return s, nil
	}
	b, _ := json.Marshal(out.Result.Value)
	return string(b), nil
}

// element evaluates an expression yielding an element (or null) and returns
// its remote objectId.
func (p *publishRun) element(expr string) (string, error) {
	raw, err := p.send("Runtime.evaluate", map[string]interface{}{"expression": expr, "returnByValue": false})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			ObjectID string `json:"objectId"`
			Subtype  string `json:"subtype"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Result.ObjectID == "" || out.Result.Subtype == "null" {
		return "", nil
	}
	return out.Result.ObjectID, nil
}

func (p *publishRun) call(obj, fn string) (string, error) {
	raw, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": fn, "returnByValue": true})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &out)
	if s, ok := out.Result.Value.(string); ok {
		return s, nil
	}
	b, _ := json.Marshal(out.Result.Value)
	return string(b), nil
}

const publishHelpersJS = `const __vis = e => { if (!e) return false; const r = e.getBoundingClientRect(); const s = getComputedStyle(e); return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; };
const __area = e => { const r = e.getBoundingClientRect(); return r.width * r.height; };
const __all = (sel) => { const out = []; const walk = root => { root.querySelectorAll(sel).forEach(e => out.push(e)); root.querySelectorAll('*').forEach(n => { if (n.shadowRoot) walk(n.shadowRoot); }); }; walk(document); return out; };
const __title = (sel) => { if (sel) return document.querySelector(sel); const c = __all('input:not([type]),input[type=text],textarea,[contenteditable="true"]').filter(__vis); return c.find(e => /标题|title/i.test((e.placeholder || '') + ' ' + (e.getAttribute('aria-label') || '') + ' ' + (e.getAttribute('data-placeholder') || ''))) || null; };
const __body = (sel, title) => { if (sel) return document.querySelector(sel); const c = __all('[contenteditable="true"],[contenteditable=""],textarea').filter(e => __vis(e) && e !== title && __area(e) > 4000 && !(title && e.contains(title))); c.sort((a, b) => __area(b) - __area(a)); return c.find(e => e.isContentEditable) || c[0] || null; };`

func jsq(s string) string { b, _ := json.Marshal(s); return string(b) }

func (p *publishRun) titleExpr() string {
	return "(() => {" + publishHelpersJS + " return __title(" + jsq(p.spec.TitleSelector) + "); })()"
}

func (p *publishRun) bodyExpr() string {
	return "(() => {" + publishHelpersJS + " return __body(" + jsq(p.spec.BodySelector) + ", __title(" + jsq(p.spec.TitleSelector) + ")); })()"
}

// observe opens the editor (EditorURL) when the tab is elsewhere and waits
// for it to show; a page without one is the named outcome no_editor.
func (p *publishRun) observe() (string, error) {
	if p.spec.EditorURL != "" {
		cur, _ := p.eval("location.href")
		if !samePage(cur, p.spec.EditorURL) {
			if err := p.navigate(p.spec.EditorURL); err != nil {
				p.noEditor = true
				return "", fmt.Errorf("could not open the editor %s: %v", p.spec.EditorURL, err)
			}
		}
	}
	var detail string
	var err error
	end := time.Now().Add(minDur(p.spec.StepBudget, 20*time.Second))
	for {
		detail, err = p.observeOnce()
		if err == nil || time.Now().After(end) {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		p.noEditor = true
		return detail, fmt.Errorf("no editor on this page (%v); it may need a sign-in — open the site's editor or ask for a takeover", err)
	}
	return detail, nil
}

// samePage: same host and path, ignoring query and trailing slash.
func samePage(a, b string) bool {
	n := func(u string) string {
		u = strings.SplitN(strings.SplitN(u, "#", 2)[0], "?", 2)[0]
		return strings.TrimRight(u, "/")
	}
	return n(a) == n(b)
}

func (p *publishRun) observeOnce() (string, error) {
	out, err := p.eval("(() => {" + publishHelpersJS + ` return JSON.stringify({url: location.href, ready: document.readyState, files: __all('input[type=file]').length, title: !!__title(` + jsq(p.spec.TitleSelector) + `), body: !!__body(` + jsq(p.spec.BodySelector) + `, null)}); })()`)
	if err != nil {
		return "", fmt.Errorf("page did not answer: %v", err)
	}
	var o struct {
		URL   string `json:"url"`
		Files int    `json:"files"`
		Title bool   `json:"title"`
		Body  bool   `json:"body"`
	}
	_ = json.Unmarshal([]byte(out), &o)
	p.editURL = o.URL
	detail := fmt.Sprintf("%s file_inputs=%d title_field=%v editor=%v", o.URL, o.Files, o.Title, o.Body)
	if len(p.spec.Files) == 0 {
		if p.spec.Title != "" && !o.Title {
			return detail, fmt.Errorf("no title field found; pass title_selector")
		}
		if p.spec.Body != "" && !o.Body {
			return detail, fmt.Errorf("no editor found; pass body_selector")
		}
	}
	if len(p.spec.Files) > 0 && o.Files == 0 && p.spec.UploadTrigger == "" && p.spec.UploadSelector == "" && !(o.Body && isImageExt(fileExt(p.spec.Files))) {
		return detail, fmt.Errorf("no file input on the page; pass upload_trigger (the upload button text)")
	}
	return detail, nil
}

const publishStateJS = `(() => { let files = 0; const walk = root => { root.querySelectorAll('input[type=file]').forEach(e => files += (e.files ? e.files.length : 0)); root.querySelectorAll('*').forEach(n => { if (n.shadowRoot) walk(n.shadowRoot); }); }; walk(document); const imgs = [...document.images]; return JSON.stringify({files, blobs: imgs.filter(i => /^(blob|data):/.test(i.src)).length, imgs: imgs.length, media: document.querySelectorAll('video,canvas').length, nodes: document.getElementsByTagName('*').length, bg: [...document.querySelectorAll('[style*="blob:"]')].length}); })()`

type uploadState struct{ Files, Blobs, Imgs, Media, Nodes, Bg int }

func (p *publishRun) state() uploadState {
	var s uploadState
	out, err := p.eval(publishStateJS)
	if err == nil {
		_ = json.Unmarshal([]byte(out), &s)
	}
	return s
}

func uploadReflected(before, after uploadState) bool {
	return uploadReflectedFor(before, after, false)
}

// uploadReflectedFor: for images, a page that only grew DOM nodes has not
// shown the picture (Zhihu ZN-1 published with 0 images that way); require a
// file, preview, background or <img> increase.
func uploadReflectedFor(before, after uploadState, images bool) bool {
	if after.Files > before.Files || after.Blobs > before.Blobs || after.Bg > before.Bg || after.Imgs > before.Imgs || after.Media > before.Media {
		return true
	}
	return !images && after.Nodes > before.Nodes+20
}

func (p *publishRun) upload() (string, error) {
	before := p.state()
	inputExpr := "(() => {" + publishHelpersJS + ` const sel = ` + jsq(p.spec.UploadSelector) + `; if (sel) { const e = document.querySelector(sel); if (e && e.type === 'file') return e; } const ext = ` + jsq(strings.ToLower(fileExt(p.spec.Files))) + `; const c = __all('input[type=file]'); const ok = e => !e.accept || !ext || e.accept.toLowerCase().split(',').some(a => { a = a.trim(); return a === ext || a === '*/*' || (a.endsWith('/*') && /^(\.(png|jpe?g|webp|gif|avif|heic)$)/.test(ext) && a.startsWith('image')) || (a.startsWith('image/') && ('.' + a.slice(6)) === ext) || (a === 'image/jpeg' && ext === '.jpg'); }); return c.find(e => ok(e) && e.multiple) || c.find(ok) || null; })()`
	how := ""
	obj, err := p.element(inputExpr)
	if err != nil {
		return "", err
	}
	if obj == "" && isImageExt(fileExt(p.spec.Files)) {
		// Editors with media tabs (video first) only mount the image input
		// once the image tab is chosen; pick it once and look again.
		if v, _ := p.eval(publishImageTabJS); v == "clicked" {
			time.Sleep(1500 * time.Millisecond)
			if obj, err = p.element(inputExpr); err != nil {
				return "", err
			}
		}
	}
	if obj != "" {
		if _, err := p.send("DOM.setFileInputFiles", map[string]interface{}{"objectId": obj, "files": p.spec.Files}); err != nil {
			return "", fmt.Errorf("setFileInputFiles: %v", err)
		}
		how = "file input"
	} else if p.spec.UploadTrigger != "" {
		// A chooser opened on a detached input cannot deliver its change
		// event; keep such inputs in the document (hidden) before the click.
		_, _ = p.eval(publishAttachInputsJS)
		if err := p.d.ArmChooser(p.spec.Files); err != nil {
			return "", err
		}
		defer p.d.DisarmChooser()
		if err := p.clickText([]string{p.spec.UploadTrigger}, false); err != nil {
			return "", err
		}
		ok, err := p.d.WaitChooser(5 * time.Second)
		if !ok || err != nil {
			return "", fmt.Errorf("clicking %q opened no file chooser (err=%v)", p.spec.UploadTrigger, err)
		}
		how = "intercepted chooser"
	} else if p.canPasteImages() {
		p.pasteLater = true
		return "no image file input; will paste the images into the body editor after the text", nil
	} else {
		return "", fmt.Errorf("no matching file input; pass upload_selector or upload_trigger")
	}
	deadline := time.Now().Add(minDur(p.spec.StepBudget, 15*time.Second))
	for {
		after := p.state()
		if uploadReflectedFor(before, after, isImageExt(fileExt(p.spec.Files))) {
			return fmt.Sprintf("uploaded %d file(s) via %s; page shows it (files=%d previews=%d imgs %d->%d)", len(p.spec.Files), how, after.Files, after.Blobs+after.Bg, before.Imgs, after.Imgs), nil
		}
		if time.Now().After(deadline) {
			if p.canPasteImages() {
				p.pasteLater = true
				return fmt.Sprintf("files set via %s showed nothing; will paste the images into the body editor after the text", how), nil
			}
			return "", fmt.Errorf("files set via %s but the page shows no file, preview or image for them; nothing was submitted. Use the editor's own image/upload button as upload_trigger (its visible text) and run publish again", how)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

const publishAttachInputsJS = `(() => { if (window.__pubAttach) return 1; window.__pubAttach = true; const keep = function () { if ((this.type || '').toLowerCase() === 'file' && !this.isConnected) { this.style.display = 'none'; document.body.appendChild(this); } }; for (const m of ['click', 'showPicker']) { const o = HTMLInputElement.prototype[m]; if (!o) continue; HTMLInputElement.prototype[m] = function () { keep.call(this); return o.apply(this, arguments); }; } return 1; })()`

func fileExt(files []string) string {
	if len(files) == 0 {
		return ""
	}
	f := files[0]
	if i := strings.LastIndex(f, "."); i >= 0 {
		return f[i:]
	}
	return ""
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// focusAndClear focuses the element with a real click at its centre, then
// selects everything in it with a real Ctrl+A and deletes it.
func (p *publishRun) focusAndClear(obj string) error {
	if err := p.clickObject(obj); err != nil {
		return err
	}
	_, _ = p.call(obj, `function(){ this.focus(); }`)
	if err := p.key("a", "KeyA", 65, 2, []string{"selectAll"}); err != nil {
		return err
	}
	return p.key("Backspace", "Backspace", 8, 0, nil)
}

func (p *publishRun) key(key, code string, vk, mods int, commands []string) error {
	down := map[string]interface{}{"type": "rawKeyDown", "key": key, "code": code, "windowsVirtualKeyCode": vk, "modifiers": mods}
	if len(commands) > 0 {
		down["commands"] = commands
	}
	if key == "Enter" {
		down["type"] = "keyDown"
		down["text"] = "\r"
	}
	if _, err := p.send("Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	_, err := p.send("Input.dispatchKeyEvent", map[string]interface{}{"type": "keyUp", "key": key, "code": code, "windowsVirtualKeyCode": vk, "modifiers": mods})
	return err
}

// typeText inserts text with trusted input events, a real Enter per line.
func (p *publishRun) typeText(text string, multiline bool) error {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if !multiline {
		lines = []string{strings.Join(lines, " ")}
	}
	for i, ln := range lines {
		if ln != "" {
			if _, err := p.send("Input.insertText", map[string]interface{}{"text": ln}); err != nil {
				return err
			}
		}
		if i < len(lines)-1 {
			if err := p.key("Enter", "Enter", 13, 0, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

const readValueFn = `function(){ const v = (this.tagName === 'INPUT' || this.tagName === 'TEXTAREA') ? String(this.value || '') : String(this.innerText || this.textContent || ''); return v; }`

var wsRe = regexp.MustCompile(`\s+`)

func normText(s string) string { return wsRe.ReplaceAllString(s, "") }

// holds reports whether got contains want's start and end (whitespace-free).
func holds(got, want string) bool {
	g, w := []rune(normText(got)), []rune(normText(want))
	if len(w) == 0 {
		return true
	}
	n := 20
	if len(w) < n {
		n = len(w)
	}
	return strings.Contains(string(g), string(w[:n])) && strings.Contains(string(g), string(w[len(w)-n:]))
}

func (p *publishRun) fill(name, expr, text string, multiline bool) (string, error) {
	// The editor often appears only after the upload; give it a moment.
	var obj string
	for wait := time.Now().Add(minDur(p.spec.StepBudget/2, 8*time.Second)); ; {
		var err error
		if obj, err = p.element(expr); err != nil {
			return "", err
		}
		if obj != "" || time.Now().After(wait) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if obj == "" {
		return "", fmt.Errorf("no %s field found; pass %s_selector", name, name)
	}
	var got string
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.focusAndClear(obj); err != nil {
			return "", err
		}
		if err := p.typeText(text, multiline); err != nil {
			return "", err
		}
		time.Sleep(200 * time.Millisecond)
		got, _ = p.call(obj, readValueFn)
		if holds(got, text) {
			return fmt.Sprintf("%s holds %d chars, starts %q", name, len([]rune(got)), clipRunes(got, 24)), nil
		}
	}
	return "", fmt.Errorf("%s read back %d chars (%q) instead of the text", name, len([]rune(got)), clipRunes(got, 24))
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func (p *publishRun) fillTitle() (string, error) {
	return p.fill("title", p.titleExpr(), p.spec.Title, false)
}

func (p *publishRun) fillBody() (string, error) {
	// Pictures that went to a gallery (note-style sites) leave a plain
	// caption body: those editors do not keep bold or headings.
	if p.doc.rich && !p.gallery {
		obj, _ := p.element(p.bodyExpr())
		if obj != "" {
			if v, _ := p.call(obj, `function() { return this.isContentEditable ? 'yes' : 'no'; }`); v == "yes" {
				p.richFilled = true
				out, err := p.fillRich(obj)
				if err != nil || len(p.doc.anchors) > 0 || (len(p.doc.bold) == 0 && p.doc.headings == 0) {
					return out, err
				}
				// A plain-text editor (note sites) drops every bold and
				// heading: write the plain form with its 【】/• markers
				// instead of failing; partial loss still fails readback.
				if _, n, b, h := p.structureCounts(obj); n == 0 && b == 0 && h == 0 {
					p.richFilled = false
					d, err := p.fill("body", p.bodyExpr(), p.doc.plain, true)
					return d + " (editor keeps no formatting: plain text with 【】 key points)", err
				}
				return out, nil
			}
		}
	}
	if p.doc.rich {
		return p.fill("body", p.bodyExpr(), p.doc.plain, true)
	}
	return p.fill("body", p.bodyExpr(), p.spec.Body, true)
}

// bodyWant is the text the body editor must hold.
func (p *publishRun) bodyWant() string {
	switch {
	case p.richFilled:
		return p.doc.text
	case p.doc.rich:
		return p.doc.plain
	}
	return p.spec.Body
}

// uploadOrInline: pictures anchored in the body of a rich editor go in at
// their anchors during fill_body instead of through a file input.
func (p *publishRun) uploadOrInline() (string, error) {
	if len(p.doc.anchors) > 0 && p.canPasteImages() {
		return fmt.Sprintf("%d picture(s) go into the body at their anchors", len(p.doc.anchors)), nil
	}
	out, err := p.upload()
	p.gallery = err == nil && !p.pasteLater
	return out, err
}

// verifyAll is verify plus, for a rich article that opened as its own page,
// a re-read of its pictures and formatting.
func (p *publishRun) verifyAll() (string, error) {
	out, err := p.verify()
	if err != nil || !p.richFilled || p.rep.PostID != "" || (p.spec.VerifyURL != "" && strings.TrimRight(p.rep.ResultURL, "/") == strings.TrimRight(p.spec.VerifyURL, "/")) {
		return out, err
	}
	sum, cerr := p.checkPublished()
	if cerr != nil && len(p.lastMissing) > 0 && p.spec.EditURL != "" {
		rs, rerr := p.repair(append([]int(nil), p.lastMissing...))
		if rerr == nil {
			return out + "; " + sum + "; " + rs, nil
		}
		cerr = fmt.Errorf("%v (repair failed: %v)", cerr, rerr)
	}
	if cerr != nil {
		p.incomplete.Store(cerr.Error())
		return out + "; " + sum, fmt.Errorf("published at %s but %v", p.rep.ResultURL, cerr)
	}
	return out + "; " + sum, nil
}

func (p *publishRun) readback() (string, error) {
	var parts []string
	if p.spec.Title != "" {
		obj, _ := p.element(p.titleExpr())
		got, _ := p.call(obj, readValueFn)
		if obj == "" || !holds(got, p.spec.Title) {
			return "", fmt.Errorf("title no longer holds the text (%q)", clipRunes(got, 24))
		}
		parts = append(parts, fmt.Sprintf("title %d chars", len([]rune(got))))
	}
	if p.spec.Body != "" {
		obj, _ := p.element(p.bodyExpr())
		got, _ := p.call(obj, readValueFn)
		if obj == "" || !holds(got, p.bodyWant()) {
			return "", fmt.Errorf("body no longer holds the text (%d chars)", len([]rune(got)))
		}
		parts = append(parts, fmt.Sprintf("body %d chars", len([]rune(got))))
		if p.richFilled {
			sum, err := p.checkStructure(obj, "editor")
			if err != nil {
				return "", fmt.Errorf("%v; nothing was submitted", err)
			}
			parts = append(parts, sum)
			if err := p.waitSaved(); err != nil {
				return "", fmt.Errorf("%v; nothing was submitted", err)
			}
		}
	}
	return strings.Join(parts, ", ") + " verified", nil
}

// domNode is the part of a CDP DOM node the text search needs.
type domNode struct {
	BackendNodeID int        `json:"backendNodeId"`
	NodeType      int        `json:"nodeType"`
	NodeName      string     `json:"nodeName"`
	NodeValue     string     `json:"nodeValue"`
	Attributes    []string   `json:"attributes"`
	Children      []*domNode `json:"children"`
	ShadowRoots   []*domNode `json:"shadowRoots"`
	ContentDoc    *domNode   `json:"contentDocument"`
}

func (n *domNode) attr(k string) string {
	for i := 0; i+1 < len(n.Attributes); i += 2 {
		if n.Attributes[i] == k {
			return n.Attributes[i+1]
		}
	}
	return ""
}

func (n *domNode) text() string {
	if n.NodeType == 3 {
		return n.NodeValue
	}
	if n.NodeName == "STYLE" || n.NodeName == "SCRIPT" {
		return ""
	}
	var b strings.Builder
	for _, c := range n.ShadowRoots {
		b.WriteString(c.text())
	}
	for _, c := range n.Children {
		b.WriteString(c.text())
	}
	return b.String()
}

// findByText walks the whole DOM, every shadow root (closed too) and
// same-process frames, for clickable nodes whose text is exactly one of texts.
func (p *publishRun) findByText(texts []string, buttonsOnly bool) ([]*domNode, error) {
	raw, err := p.send("DOM.getDocument", map[string]interface{}{"depth": -1, "pierce": true})
	if err != nil {
		return nil, err
	}
	var doc struct {
		Root *domNode `json:"root"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, t := range texts {
		want[normText(t)] = true
	}
	var hits []*domNode
	var walk func(n *domNode)
	walk = func(n *domNode) {
		if n == nil {
			return
		}
		if n.NodeType == 1 {
			clickable := n.NodeName == "BUTTON" || n.NodeName == "A" || n.attr("role") == "button" || (n.NodeName == "INPUT" && (n.attr("type") == "submit" || n.attr("type") == "button"))
			if !buttonsOnly {
				clickable = clickable || n.NodeName == "DIV" || n.NodeName == "SPAN" || n.NodeName == "LABEL"
			}
			txt := normText(n.text())
			if n.NodeName == "INPUT" {
				txt = normText(n.attr("value"))
			}
			if clickable && want[txt] && n.attr("disabled") == "" && n.attr("aria-disabled") != "true" {
				hits = append(hits, n)
			}
		}
		for _, c := range n.ShadowRoots {
			walk(c)
		}
		for _, c := range n.Children {
			walk(c)
		}
		walk(n.ContentDoc)
	}
	walk(doc.Root)
	return hits, nil
}

func (p *publishRun) boxCenter(params map[string]interface{}) (float64, float64, error) {
	_, _ = p.send("DOM.scrollIntoViewIfNeeded", params)
	raw, err := p.send("DOM.getBoxModel", params)
	if err != nil {
		return 0, 0, err
	}
	var bm struct {
		Model struct {
			Border []float64 `json:"border"`
			Width  float64   `json:"width"`
			Height float64   `json:"height"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &bm); err != nil || len(bm.Model.Border) < 8 || bm.Model.Width == 0 || bm.Model.Height == 0 {
		return 0, 0, fmt.Errorf("element has no box (hidden)")
	}
	q := bm.Model.Border
	return (q[0] + q[4]) / 2, (q[1] + q[5]) / 2, nil
}

func (p *publishRun) mouseClick(x, y float64) error {
	for _, t := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		ev := map[string]interface{}{"type": t, "x": x, "y": y, "button": "left", "clickCount": 1}
		if t == "mouseMoved" {
			ev["button"] = "none"
			ev["clickCount"] = 0
		}
		if _, err := p.send("Input.dispatchMouseEvent", ev); err != nil {
			return err
		}
	}
	return nil
}

func (p *publishRun) clickObject(obj string) error {
	x, y, err := p.boxCenter(map[string]interface{}{"objectId": obj})
	if err != nil {
		return err
	}
	return p.mouseClick(x, y)
}

// clickText clicks the last visible node with that exact text.
func (p *publishRun) clickText(texts []string, buttonsOnly bool) error {
	hits, err := p.findByText(texts, buttonsOnly)
	if err != nil {
		return err
	}
	for i := len(hits) - 1; i >= 0; i-- {
		x, y, err := p.boxCenter(map[string]interface{}{"backendNodeId": hits[i].BackendNodeID})
		if err != nil {
			continue
		}
		return p.mouseClick(x, y)
	}
	return fmt.Errorf("no visible enabled button with text %q (searched all shadow roots)", strings.Join(texts, "/"))
}

var publishDoneRe = regexp.MustCompile(`发布成功|已发布|发表成功|Published|published successfully`)

func (p *publishRun) submit() (string, error) {
	before, _ := p.eval("document.body ? document.body.innerText.slice(0, 20000) : ''")
	// Set before the click: a page that freezes on publish never answers it.
	p.submitClicked.Store(true)
	if err := p.clickText(p.spec.SubmitTexts, true); err != nil {
		return "", err
	}
	confirmed := false
	reclicks := 0
	lastClick := time.Now()
	deadline := time.Now().Add(2*p.spec.StepBudget - 5*time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		url := p.pageURL()
		// An autosaved draft also changes the URL (/write -> /p/1/edit):
		// only a page outside the editor counts as published.
		if url != "" && url != p.editURL && !publishEditorURL.MatchString(url) {
			return "page left the editor: " + url, nil
		}
		txt, _ := p.evalWithin("document.body ? document.body.innerText.slice(0, 20000) : ''", 3*time.Second)
		if m := publishDoneRe.FindString(txt); m != "" {
			return "page says " + m, nil
		}
		if !confirmed && len(p.spec.ConfirmTexts) > 0 {
			if p.clickText(p.spec.ConfirmTexts, true) == nil {
				confirmed = true
				lastClick = time.Now()
			}
		}
		// Many sites answer the first click with a settings panel whose
		// own button carries the same text; press the newest one again.
		if reclicks < 2 && time.Since(lastClick) > 3*time.Second {
			reclicks++
			lastClick = time.Now()
			_ = p.clickText(p.spec.SubmitTexts, true)
		}
	}
	after, _ := p.eval("document.body ? document.body.innerText.slice(0, 20000) : ''")
	if said := newLines(before, after); said != "" {
		return "", fmt.Errorf("clicked submit but the page neither left the editor nor said it was published; after the click it showed: %q (fix that, e.g. a length limit or a required field, then run publish again)", said)
	}
	return "", fmt.Errorf("clicked submit but the page neither left the editor nor said it was published")
}

// evalWithin is eval bounded by d: a page that navigates away or freezes
// right after a submit may never answer Runtime.evaluate.
func (p *publishRun) evalWithin(expr string, d time.Duration) (string, error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() { s, err := p.eval(expr); ch <- res{s, err} }()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(d):
		// A JS dialog (beforeunload, alert) blocks every evaluate.
		go func() { _, _ = p.send("Page.handleJavaScriptDialog", map[string]interface{}{"accept": true}) }()
		return "", fmt.Errorf("page did not answer in %s", d)
	}
}

// pageURL reads the current URL from the browser's navigation history
// (answered by the browser even while the page's JS is busy), falling back
// to a bounded location.href.
func (p *publishRun) pageURL() string {
	type res struct{ u string }
	ch := make(chan res, 1)
	go func() {
		raw, err := p.send("Page.getNavigationHistory", map[string]interface{}{})
		var h struct {
			CurrentIndex int `json:"currentIndex"`
			Entries      []struct {
				URL string `json:"url"`
			} `json:"entries"`
		}
		if err == nil && json.Unmarshal(raw, &h) == nil && h.CurrentIndex >= 0 && h.CurrentIndex < len(h.Entries) {
			ch <- res{h.Entries[h.CurrentIndex].URL}
			return
		}
		ch <- res{""}
	}()
	select {
	case r := <-ch:
		if r.u != "" {
			return r.u
		}
	case <-time.After(3 * time.Second):
	}
	u, _ := p.evalWithin("location.href", 3*time.Second)
	return u
}

func (p *publishRun) navigate(url string) error {
	if _, err := p.send("Page.navigate", map[string]interface{}{"url": url}); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		st, _ := p.eval("document.readyState + ' ' + location.href")
		if strings.HasPrefix(st, "complete ") && !strings.Contains(st, " about:blank") {
			return nil
		}
	}
	return fmt.Errorf("verify page did not load")
}

func (p *publishRun) verify() (string, error) {
	key := clipRunes(p.spec.Title, 12)
	if key == "" {
		key = clipRunes(p.spec.Body, 12)
	}
	// The page the submit landed on is the post itself on many sites
	// (/p/<id>); when it shows the title and is not an editor, that is the
	// result. Otherwise open verify_url, where the post must be listed.
	if out, err := p.checkResult(key, 6*time.Second, false); err == nil {
		if !publishEditorURL.MatchString(p.rep.ResultURL) || p.spec.VerifyURL == "" {
			return out, nil
		}
	}
	if p.spec.VerifyURL == "" {
		out, err := p.checkResult(key, minDur(p.spec.StepBudget, 20*time.Second), false)
		if err != nil && publishSubmittedURL.MatchString(err.Error()) {
			return "", fmt.Errorf("%v. The site confirmed the submit in its URL, so the post IS submitted: do NOT publish again. Read it back once from the account's own post list (pass verify_url next time) and reply with its link; a post still under review may not open publicly yet", err)
		}
		return out, err
	}
	if err := p.navigate(p.spec.VerifyURL); err != nil {
		return "", err
	}
	return p.checkResult(key, minDur(p.spec.StepBudget, 25*time.Second), true)
}

// A landing URL that itself says the submit went through (?published=true).
var publishSubmittedURL = regexp.MustCompile(`(?i)[?&#](published|publish_success|success|status)=(true|1|success|ok)`)

var publishEditorURL = regexp.MustCompile(`(?i)/(edit|write|publish|creator|draft)`)

func (p *publishRun) checkResult(key string, budget time.Duration, listed bool) (string, error) {
	deadline := time.Now().Add(budget)
	reloaded := false
	for {
		out, _ := p.eval(`(() => { const key = ` + jsq(normText(key)) + `; const n = s => (s || '').replace(/\s+/g, ''); const has = n(document.body ? document.body.innerText : '').includes(key); const a = [...document.querySelectorAll('a[href]')].find(a => n(a.innerText).includes(key) || n(a.closest('section,li,article,div') ? a.closest('section,li,article,div').innerText : '').includes(key)); let id = ''; if (has && !a) { const els = [...document.querySelectorAll('body *')].filter(e => e.children.length < 3 && n(e.innerText).includes(key)); let e = els[0]; for (let i = 0; e && i < 6 && !id; i++, e = e.parentElement) { for (const x of [e, ...e.querySelectorAll('*')]) { for (const at of x.attributes) { const m = (at.value || '').match(/\b([0-9a-f]{24}|[0-9]{15,20})\b/); if (m) { id = m[1]; break; } } if (id) break; } } } return JSON.stringify({url: location.href, has, link: a ? a.href : '', id}); })()`)
		var o struct {
			URL  string `json:"url"`
			Has  bool   `json:"has"`
			Link string `json:"link"`
			ID   string `json:"id"`
		}
		_ = json.Unmarshal([]byte(out), &o)
		if o.Has {
			p.rep.ResultURL = o.URL
			if listed && o.Link != "" {
				p.rep.ResultURL = o.Link
			} else if listed {
				p.rep.PostID = o.ID
			}
			return fmt.Sprintf("result page %s shows %q", o.URL, key), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("result page %s does not show %q", o.URL, key)
		}
		// Lists load lazily; one reload halfway catches a stale first render.
		if listed && !reloaded && time.Until(deadline) < budget/2 {
			reloaded = true
			_, _ = p.d.Send("Page.reload", map[string]any{})
		}
		time.Sleep(700 * time.Millisecond)
	}
}

func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif", ".heic":
		return true
	}
	return false
}

// publishImageTabJS clicks a visible, short tab/button whose label names an
// image post ("上传图文", "图文", "Image", "Photo").
const publishImageTabJS = `(() => { const res = [/^(上传)?图文$|^(image|photo) posts?$/i, /^(上传)?图片$|^(upload )?(images?|photos?)$/i]; const vis = e => e.getClientRects().length > 0; const all = [...document.querySelectorAll('[role=tab],button,a,span,div')].filter(e => vis(e) && e.children.length <= 2); let e = null; for (const re of res) { const c = all.filter(x => re.test((x.innerText || '').trim())); if (c.length) { e = c[c.length - 1]; break; } } if (!e) return 'none'; e.click(); return 'clicked'; })()`

// canPasteImages: image files and a rich body editor; editors such as Zhihu's
// take pictures pasted into the text rather than through a file input.
func (p *publishRun) canPasteImages() bool {
	if p.spec.Body == "" || !isImageExt(fileExt(p.spec.Files)) {
		return false
	}
	obj, err := p.element(p.bodyExpr())
	if err != nil || obj == "" {
		return false
	}
	v, _ := p.call(obj, `function() { return this.isContentEditable ? 'yes' : 'no'; }`)
	return v == "yes"
}

// pasteImages pastes the files at the end of the body editor as a clipboard
// paste of image files, then waits for the editor to show more <img>.
func (p *publishRun) pasteImages() (string, error) {
	if p.imagesPlaced > 0 {
		return fmt.Sprintf("%d picture(s) already at their anchors", p.imagesPlaced), nil
	}
	if !p.pasteLater {
		return "not needed", nil
	}
	obj, err := p.element(p.bodyExpr())
	if err != nil || obj == "" {
		return "", fmt.Errorf("body editor not found for pasting images")
	}
	count := `function() { return String(this.querySelectorAll('img').length); }`
	before, _ := p.call(obj, count)
	// The browser reads the files itself (server and desktop disks differ;
	// ZN-7 could not paste because the server read the desktop path).
	if _, err := p.loadPicker(); err != nil {
		return "", err
	}
	pasted := 0
	for k := range p.spec.Files {
		if err := p.pasteFile(obj, k); err != nil {
			return "", fmt.Errorf("pasted %d of %d: %v; nothing was submitted", pasted, len(p.spec.Files), err)
		}
		pasted++
	}
	deadline := time.Now().Add(minDur(p.spec.StepBudget, 20*time.Second))
	for {
		after, _ := p.call(obj, count)
		var a, b int
		fmt.Sscan(after, &a)
		fmt.Sscan(before, &b)
		if a > b {
			return fmt.Sprintf("pasted %d image(s) into the body; editor imgs %d->%d", pasted, b, a), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("pasted %d image(s) but the editor shows no new image (imgs %d); nothing was submitted", pasted, a)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// newLines is the text lines that appear in after but not in before.
func newLines(before, after string) string {
	seen := map[string]bool{}
	for _, l := range strings.Split(before, "\n") {
		seen[strings.TrimSpace(l)] = true
	}
	var out []string
	for _, l := range strings.Split(after, "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			out = append(out, l)
		}
	}
	return clipRunes(strings.Join(out, " | "), 200)
}

// publishSafetyBudget caps a step that ends on page events; it is reached
// only when the page stops answering.
func publishSafetyBudget(step time.Duration) time.Duration {
	return 2*step + 4*time.Minute
}

// waitSaved: editors that save drafts on the server publish the saved
// draft, so a submit right after the last edit can lose it. When the page
// makes writing requests, wait until one started after the last edit has
// finished and the page is quiet; editors that never save stay instant.
func (p *publishRun) waitSaved() error {
	if p.net == nil {
		return nil
	}
	if fails := p.net.failedSince(p.lastEdit); len(fails) > 0 {
		return fmt.Errorf("the page's save/upload request failed: %s", strings.Join(fails, "; "))
	}
	if !p.net.wroteSince(time.Time{}) {
		return nil
	}
	end := time.Now().Add(10 * time.Second)
	for !p.net.wroteSince(p.lastEdit) && time.Now().Before(end) {
		time.Sleep(200 * time.Millisecond)
	}
	if !p.net.waitQuiet(time.Second, 60*time.Second) {
		return fmt.Errorf("the page kept saving for 60s after the last edit")
	}
	return nil
}
