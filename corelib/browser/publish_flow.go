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
	"strconv"
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
	// LazyUpload: the site's verified experience or profile says it keeps
	// picked pictures local and sends them with the post. Only then is a
	// local preview without an upload request accepted, and the submit
	// answer or the published page must then carry remote picture URLs.
	LazyUpload    bool
	Title         string
	TitleSelector string
	Body          string
	BodySelector  string
	SubmitTexts   []string // exact button texts; default 发布/Publish/Post/提交
	ConfirmTexts  []string // a second confirm button, if the site asks
	VerifyURL     string   // page to open after submit where the post must be listed
	// Deadline bounds the whole run (the caller's own hard limit minus a
	// margin); every step's budget is clipped to it so the run always ends
	// with its own report instead of being cut off from outside.
	Deadline time.Time
	// EditorURL is the page where a new post is written; the run opens it
	// itself when the tab is elsewhere.
	EditorURL string
	// EditURL opens a published post for editing ({url} is the post URL);
	// with it a picture missing from the live post is put back.
	EditURL     string
	UpdateTexts []string // buttons that save an edited post (default 更新/发布/Update/Publish)
	// SuccessTexts: page text that says the post went out; only text that
	// appeared after the submit click counts (default publishSuccessTexts).
	SuccessTexts []string
	// SuccessURL: a regexp the page URL matches once the post is out.
	SuccessURL string
	// BodyKind: rich, markdown or plain ("" detects from the editor).
	BodyKind string
	// ImageMode: paste or gallery ("" detects).
	ImageMode string
	// ImageTabTexts: labels of a tab that switches to image posts.
	ImageTabTexts []string
	// PlainBold renders bold/heading runs in a plain body ($1 = the text).
	PlainBold  string
	StepBudget time.Duration
	// Tags and Category fill a publish settings panel's required fields
	// (empty: tags come from the body's #hashtags, then the title).
	Tags     []string
	Category string
	// VerifyAPI: a JSON endpoint (read with the page's own session) that
	// lists the account's posts; a post whose title is in it is published.
	VerifyAPI string
	// MinImages: pictures the post must carry (the task's plan); fewer in
	// the editor fails readback instead of publishing without them.
	MinImages int
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
	// Evidence names what proved the post published (response id, redirect,
	// post list, list API); empty unless State is published.
	Evidence string `json:"evidence,omitempty"`
	// NetLog is the page's writing requests with their answers (clipped).
	NetLog []string `json:"net_log,omitempty"`
	// Notice: a blocking notice the page showed ("…才能发布"), any frame.
	Notice string `json:"notice,omitempty"`
	// Method records which branch each primitive took (exploration
	// recording; see publishRun.note): the site experience stores it.
	Method map[string]string `json:"method,omitempty"`
}

// PublishState is the outcome of a publish run.
type PublishState string

const (
	// PublishNotSubmitted: stopped before the submit went out; nothing is
	// on the site and running the publish again is safe.
	PublishNotSubmitted PublishState = "not_submitted"
	// PublishSubmitted: a submission write went out but no evidence of a
	// post came back; it may be live. Never submit again for the same content.
	PublishSubmitted PublishState = "submitted_unconfirmed"
	// PublishDraftOnly: the click only saved a draft (every write after it
	// was a draft/autosave); nothing is published.
	PublishDraftOnly PublishState = "draft_only"
	// PublishLive: published, with evidence (an id or link in the site's
	// answer, a redirect to the post, or the post in the account's list).
	PublishLive PublishState = "published"
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
	discover := len(spec.SubmitTexts) == 0
	if discover {
		spec.SubmitTexts = defaultSubmitTexts
	}
	if len(spec.SuccessTexts) == 0 {
		spec.SuccessTexts = publishSuccessTexts
	}
	p := &publishRun{d: d, spec: spec, rep: &PublishReport{}, doc: parseRichBody(spec.Body, len(spec.Files), spec.PlainBold), discoverSubmit: discover}
	if b := p.doc.badAnchors; len(b) > 0 {
		p.rep.State = PublishNotSubmitted
		p.rep.Stuck = "spec"
		return p.rep, fmt.Errorf("publish not started: the body has [[img:%d]] but only %d picture file(s) were given; pass a file for every anchor or remove the anchor (nothing was filled or posted)", b[0], len(spec.Files))
	}
	if why := ParagraphProblem(spec.Body); why != "" {
		p.rep.State = PublishNotSubmitted
		p.rep.Stuck = "spec"
		return p.rep, fmt.Errorf("publish not started: %s", why)
	}
	p.net = startNetTracker(d)
	defer p.net.stop()
	defer p.recordMethod()
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
		if !spec.Deadline.IsZero() {
			if rem := time.Until(spec.Deadline); rem < budget {
				budget = maxDur(rem, time.Second)
			}
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
		if r.err != nil && st.name == "submit" && p.submitClicked.Load() && (spec.VerifyURL != "" || spec.VerifyAPI != "" || p.hasNetEvidence()) {
			// The click went out but the page gave no sign: the site's own
			// answer or the account's post list decides (XHS-33 left the
			// bot guessing).
			if vout, verr := p.verify(); verr == nil {
				p.rep.Steps = append(p.rep.Steps, PublishStep{Name: "verify_result", OK: true, Detail: "submit gave no sign; " + vout})
				p.rep.State = PublishLive
				p.rep.NetLog = p.net.logLines(publishNetLogMax)
				return p.rep, nil
			}
		}
		if r.err != nil {
			p.rep.Stuck = st.name
			p.rep.State = p.failedState()
			p.rep.NetLog = p.net.logLines(publishNetLogMax)
			err := r.err
			if n := p.blockingNotices(); n != "" {
				p.rep.Notice = n
				if !strings.Contains(err.Error(), n) {
					err = fmt.Errorf("%v; the page shows a blocking notice: %s", err, n)
				}
			}
			return p.rep, fmt.Errorf("publish step %s failed: %v", st.name, err)
		}
	}
	p.rep.NetLog = p.net.logLines(publishNetLogMax)
	if p.rep.Evidence == "" {
		// verify only succeeds with evidence; this is a safety net.
		p.rep.State = PublishSubmitted
		return p.rep, fmt.Errorf("publish ended without evidence of the post")
	}
	p.rep.State = PublishLive
	return p.rep, nil
}

// publishNetLogMax: writes kept in the report for the publish record.
const publishNetLogMax = 40

func (p *publishRun) hasNetEvidence() bool {
	id, u := p.net.evidenceSince(p.clickedAt)
	return id != "" || u != ""
}

// failedState names the outcome of a run that stopped at a failed step,
// from what the page sent: "submitted" needs a submission write after the
// click (not an upload, not a draft/autosave).
func (p *publishRun) failedState() PublishState {
	switch {
	case p.incomplete.Load() != nil:
		p.rep.Missing = p.incomplete.Load().(string)
		return PublishLiveIncomplete
	case !p.submitClicked.Load():
		if p.noEditor {
			return PublishNoEditor
		}
		return PublishNotSubmitted
	case p.net == nil:
		return PublishSubmitted
	}
	sub := p.net.submitWritesSince(p.clickedAt)
	refused := p.net.refusedSince(p.clickedAt)
	failed := p.net.failedSince(p.clickedAt)
	if len(sub) > 0 && len(refused)+len(failed) < len(sub) {
		return PublishSubmitted
	}
	if len(sub) > 0 {
		// Every submission write was refused or failed in its answer.
		return PublishNotSubmitted
	}
	if p.net.draftWritesSince(p.clickedAt) > 0 {
		if p.panelConfirmed || p.toastSeen != "" {
			// The site publishes through its draft endpoint and said or
			// was told to publish: it may be out.
			return PublishSubmitted
		}
		return PublishDraftOnly
	}
	if p.toastSeen != "" || p.clickHung {
		return PublishSubmitted
	}
	return PublishNotSubmitted
}

type publishRun struct {
	submitClicked atomic.Bool
	clickedAt     time.Time
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
	kind        string // detected body kind
	// discoverSubmit: no profile or caller named the submit button; when no
	// button has an exact default text, the best-scored one is used.
	discoverSubmit bool
	// seen: page URLs the editor had before the submit click (an autosave
	// renames the editor URL; none of these is a published post).
	seen map[string]bool
	// uploadVia: the method whose upload the site stored (recorded).
	uploadVia string
	// pasteLater: no file input showed the images; paste them into the
	// body editor after the text (paste_images step).
	pasteLater bool
	// panelConfirmed: a publish settings panel opened by the click was
	// filled and its own publish button pressed; toastSeen: a success text.
	panelConfirmed bool
	// clickHung: the page froze inside the submit click (outcome unknown).
	clickHung bool
	toastSeen string
	// doc is the structured body; richFilled when it went into a rich editor
	// as formatted text, imagesPlaced pictures at their anchors.
	doc          richDoc
	richFilled   bool
	imagesPlaced int
	gallery      bool
	// lazyImages: pictures stay local until the submit (spec.LazyUpload).
	lazyImages bool
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
const __all = (sel) => { const out = []; const walk = root => { root.querySelectorAll(sel).forEach(e => out.push(e)); root.querySelectorAll('*').forEach(n => { if (n.shadowRoot) walk(n.shadowRoot); }); }; walk(document); document.querySelectorAll('iframe,frame').forEach(f => { try { if (f.contentDocument) walk(f.contentDocument); } catch (e) {} }); return out; };
const __title = (sel) => { if (sel) return document.querySelector(sel); const c = __all('input:not([type]),input[type=text],textarea,[contenteditable="true"]').filter(__vis); const lab = e => [...(e.labels || [])].map(l => l.innerText).join(' '); return c.find(e => /标题|title/i.test((e.placeholder || '') + ' ' + (e.getAttribute('aria-label') || '') + ' ' + (e.getAttribute('data-placeholder') || ''))) || c.find(e => /标题|主题|title|subject|headline/i.test(lab(e) + ' ' + (e.name || '') + ' ' + (e.id || ''))) || null; };
const __body = (sel, title) => { if (sel) return document.querySelector(sel); const c = __all('[contenteditable="true"],[contenteditable=""],textarea,.CodeMirror').filter(e => __vis(e) && !(e.closest && e.parentElement && e.parentElement.closest('.CodeMirror')) && e !== title && __area(e) > 4000 && !(title && e.contains(title))); c.sort((a, b) => __area(b) - __area(a)); return c.find(e => e.isContentEditable) || c[0] || null; };
const __kind = e => { if (!e) return ''; if (e.closest('.CodeMirror,.cm-editor,.monaco-editor,.bytemd,[class*=markdown],[class*=Markdown],[class*=md-editor]')) return 'markdown'; if (e.tagName === 'TEXTAREA') return 'plain'; return e.isContentEditable ? 'rich' : 'plain'; };`

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
		if n := p.blockingNotices(); n != "" {
			p.rep.Notice = n
			return detail, fmt.Errorf("no editor on this page (%v); the page shows a blocking notice: %s", err, n)
		}
		return detail, fmt.Errorf("no editor on this page (%v); it may need a sign-in — open the site's editor or ask for a takeover", err)
	}
	if n := p.blockingNotices(); n != "" {
		p.rep.Notice = n
		detail += "; the page shows a notice: " + n
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
	p.see(o.URL)
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

// publishStateJS counts file inputs, pictures and previews in the page and
// every same-origin frame (editors in iframes preview there).
const publishStateJS = `(() => { const docs = [document]; const addFrames = d => { for (const f of d.querySelectorAll('iframe,frame')) { try { const c = f.contentDocument; if (c && !docs.includes(c)) { docs.push(c); addFrames(c); } } catch (e) {} } }; addFrames(document); let files = 0, blobs = 0, imgs = 0, media = 0, nodes = 0, bg = 0; const walk = root => { root.querySelectorAll('input[type=file]').forEach(e => files += (e.files ? e.files.length : 0)); root.querySelectorAll('*').forEach(n => { if (n.shadowRoot) walk(n.shadowRoot); }); }; for (const d of docs) { walk(d); const im = [...d.images]; blobs += im.filter(i => /^(blob|data):/.test(i.src)).length; imgs += im.length; media += d.querySelectorAll('video,canvas').length; nodes += d.getElementsByTagName('*').length; bg += d.querySelectorAll('[style*="blob:"]').length; } return JSON.stringify({files, blobs, imgs, media, nodes, bg}); })()`

// publishDropPreviewsJS removes local-only pictures (blob:/data:) that a
// preview-only input added, so a later method does not publish them.
const publishDropPreviewsJS = `(() => { const docs = [document]; const addFrames = d => { for (const f of d.querySelectorAll('iframe,frame')) { try { const c = f.contentDocument; if (c && !docs.includes(c)) { docs.push(c); addFrames(c); } } catch (e) {} } }; addFrames(document); const keep = new Set(window.__mcKeepPreviews || []); let n = 0; for (const d of docs) for (const i of [...d.images]) { const s = i.getAttribute('src') || ''; if (/^(blob|data):/.test(s) && !keep.has(s)) { i.remove(); n++; } } return String(n); })()`

// publishKeepPreviewsJS remembers the local pictures already on the page.
const publishKeepPreviewsJS = `(() => { const docs = [document]; const addFrames = d => { for (const f of d.querySelectorAll('iframe,frame')) { try { const c = f.contentDocument; if (c && !docs.includes(c)) { docs.push(c); addFrames(c); } } catch (e) {} } }; addFrames(document); window.__mcKeepPreviews = []; for (const d of docs) for (const i of d.images) { const s = i.getAttribute('src') || ''; if (/^(blob|data):/.test(s)) window.__mcKeepPreviews.push(s); } return '1'; })()`

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

// publishFileInputsJS ranks the page's file inputs (every same-origin
// frame) for the files and marks them data-mc-file=<rank>: the declared
// selector first, then inputs in the same document as the body editor (an
// editor in an iframe uploads through its own input; an input in the outer
// page may only make local previews), then accept match, then multiple.
func (p *publishRun) publishFileInputsJS() string {
	return "(() => {" + publishHelpersJS + ` const sel = ` + jsq(p.spec.UploadSelector) + `; const ext = ` + jsq(strings.ToLower(fileExt(p.spec.Files))) + `;
  const ok = e => !e.accept || !ext || e.accept.toLowerCase().split(',').some(a => { a = a.trim(); return a === ext || a === '*/*' || (a.endsWith('/*') && /^(\.(png|jpe?g|webp|gif|avif|heic)$)/.test(ext) && a.startsWith('image')) || (a.startsWith('image/') && ('.' + a.slice(6)) === ext) || (a === 'image/jpeg' && ext === '.jpg'); });
  let body = null; try { body = __body(` + jsq(p.spec.BodySelector) + `, null); } catch (e) {}
  const bdoc = body ? body.ownerDocument : null;
  const c = __all('input[type=file]').filter(e => e.id !== '__pub_picker' && ok(e));
  let pick = null; if (sel) { const e = document.querySelector(sel); if (e && e.type === 'file') pick = e; }
  const score = e => (e === pick ? 100 : 0) + (bdoc && e.ownerDocument === bdoc ? 10 : 0) + (e.accept ? 2 : 0) + (e.multiple ? 1 : 0);
  c.sort((a, b) => score(b) - score(a));
  __all('[data-mc-file]').forEach(e => e.removeAttribute('data-mc-file'));
  c.slice(0, 4).forEach((e, i) => e.setAttribute('data-mc-file', String(i)));
  return String(Math.min(c.length, 4));
})()`
}

// uploadWait is how long a set file input gets to start an upload request.
const uploadStartWait = 8 * time.Second

// upload sets the files on the best file input and follows the page's
// network until the site's upload requests completed. Local previews alone
// are not an upload (BILI-cv53625427: the outer page's input made image
// blocks and no request; the post went out without its pictures).
func (p *publishRun) upload() (string, error) {
	n := 0
	if v, err := p.eval(p.publishFileInputsJS()); err == nil {
		n, _ = strconv.Atoi(v)
	} else {
		return "", err
	}
	if n == 0 && isImageExt(fileExt(p.spec.Files)) {
		// Editors with media tabs (video first) only mount the image input
		// once the image tab is chosen; pick it once and look again.
		if v, _ := p.eval(publishImageTabJS(p.spec.ImageTabTexts)); v == "clicked" {
			time.Sleep(1500 * time.Millisecond)
			if v, err := p.eval(p.publishFileInputsJS()); err == nil {
				n, _ = strconv.Atoi(v)
			}
		}
	}
	images := isImageExt(fileExt(p.spec.Files))
	var tried []string
	for i := 0; i < n; i++ {
		obj, err := p.element("(() => {" + publishHelpersJS + " return __all('[data-mc-file=\"" + strconv.Itoa(i) + "\"]')[0] || null; })()")
		if err != nil || obj == "" {
			continue
		}
		_, _ = p.eval(publishKeepPreviewsJS)
		before := p.state()
		t0 := time.Now()
		if _, err := p.send("DOM.setFileInputFiles", map[string]interface{}{"objectId": obj, "files": p.spec.Files}); err != nil {
			tried = append(tried, fmt.Sprintf("input %d: setFileInputFiles %v", i+1, err))
			continue
		}
		out, done, err := p.waitUpload(t0, before, images, fmt.Sprintf("file input %d", i+1))
		if done {
			return joinTried(tried, out), err
		}
		if v, _ := p.eval(publishDropPreviewsJS); v != "" && v != "0" {
			out += " (removed its " + v + " local preview(s))"
		}
		tried = append(tried, out)
	}
	if p.spec.UploadTrigger != "" {
		_, _ = p.eval(publishKeepPreviewsJS)
		// A chooser opened on a detached input cannot deliver its change
		// event; keep such inputs in the document (hidden) before the click.
		_, _ = p.eval(publishAttachInputsJS)
		if err := p.d.ArmChooser(p.spec.Files); err != nil {
			return "", err
		}
		defer p.d.DisarmChooser()
		before := p.state()
		t0 := time.Now()
		if err := p.clickText([]string{p.spec.UploadTrigger}, false); err != nil {
			return "", err
		}
		ok, err := p.d.WaitChooser(5 * time.Second)
		if !ok || err != nil {
			return "", fmt.Errorf("clicking %q opened no file chooser (err=%v)", p.spec.UploadTrigger, err)
		}
		out, done, err := p.waitUpload(t0, before, images, "intercepted chooser")
		if done {
			return joinTried(tried, out), err
		}
		if v, _ := p.eval(publishDropPreviewsJS); v != "" && v != "0" {
			out += " (removed its " + v + " local preview(s))"
		}
		tried = append(tried, out)
	}
	if p.canPasteImages() {
		p.pasteLater = true
		if len(tried) == 0 {
			return "no image file input; will paste the images into the body editor after the text", nil
		}
		return strings.Join(tried, "; ") + "; will paste the images into the body editor after the text", nil
	}
	if len(tried) == 0 {
		return "", fmt.Errorf("no matching file input; pass upload_selector or upload_trigger")
	}
	return "", fmt.Errorf("%s; nothing was submitted. Use the editor's own image/upload button as upload_trigger (its visible text) and run publish again", strings.Join(tried, "; "))
}

// waitUpload follows one file hand-over. done=false means this input did
// nothing (the caller may try another); out then says what was seen.
func (p *publishRun) waitUpload(t0 time.Time, before uploadState, images bool, how string) (string, bool, error) {
	want := len(p.spec.Files)
	deadline := time.Now().Add(maxDur(p.spec.StepBudget, 20*time.Second))
	startBy := time.Now().Add(uploadStartWait)
	for {
		time.Sleep(300 * time.Millisecond)
		after := p.state()
		shown := uploadReflectedFor(before, after, images)
		if p.net == nil {
			if shown {
				p.uploadVia = how
				return fmt.Sprintf("uploaded %d file(s) via %s; page shows it (files=%d previews=%d imgs %d->%d)", want, how, after.Files, after.Blobs+after.Bg, before.Imgs, after.Imgs), true, nil
			}
			if time.Now().After(startBy) {
				return how + " showed nothing", false, nil
			}
			continue
		}
		started, ok, failed, open := p.net.uploadStats(t0)
		if failed > 0 {
			return "", true, fmt.Errorf("the site's upload via %s failed (%d of %d request(s)): %s; nothing was submitted", how, failed, started, strings.Join(p.net.failedSince(t0), "; "))
		}
		if started > 0 && open == 0 && ok >= minInt(want, 1) && (ok >= want || time.Now().After(startBy)) {
			p.uploadVia = how
			return fmt.Sprintf("uploaded %d file(s) via %s; the site stored them (%d upload request(s) answered ok; imgs %d->%d)", want, how, ok, before.Imgs, after.Imgs), true, nil
		}
		if started == 0 && time.Now().After(startBy) {
			if shown && p.spec.LazyUpload {
				// The site's profile says it sends the pictures with the
				// post; verify then requires remote picture URLs.
				p.lazyImages = true
				p.uploadVia = how + " (lazy)"
				return fmt.Sprintf("set %d file(s) via %s; page shows them locally and the site uploads at submit (lazy_upload); remote picture URLs are required after the submit", want, how), true, nil
			}
			if shown {
				// A preview with no request is not an upload
				// (BILI-cv53625427 went out without its pictures that way).
				return how + " only made a local preview and sent no upload request (not an upload)", false, nil
			}
			return how + " showed nothing and sent no upload request", false, nil
		}
		if time.Now().After(deadline) {
			return "", true, fmt.Errorf("the site's upload via %s did not finish (%d of %d request(s) still open); nothing was submitted", how, open, started)
		}
	}
}

func joinTried(tried []string, out string) string {
	if len(tried) == 0 {
		return out
	}
	return strings.Join(tried, "; ") + "; " + out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
// selects everything in it with a real Ctrl+A and deletes it. A field
// without a box (zero-size or hidden title proxies: "Could not compute box
// model") is focused and selected from script instead; the typed keys go
// to the focused element either way.
func (p *publishRun) focusAndClear(obj string) error {
	if err := p.clickObject(obj); err != nil {
		if _, ferr := p.call(obj, publishFocusSelectFn); ferr != nil {
			return fmt.Errorf("%v; script focus failed too: %v", err, ferr)
		}
		return p.key("Backspace", "Backspace", 8, 0, nil)
	}
	_, _ = p.call(obj, `function(){ this.focus(); }`)
	if err := p.key("a", "KeyA", 65, 2, []string{"selectAll"}); err != nil {
		return err
	}
	return p.key("Backspace", "Backspace", 8, 0, nil)
}

// publishFocusSelectFn focuses an element from script and selects its
// whole content (value or editable contents).
const publishFocusSelectFn = `function(){ try { this.scrollIntoView({block: 'center'}); } catch (e) {} this.focus(); if (this.select && (this.tagName === 'INPUT' || this.tagName === 'TEXTAREA')) { this.select(); return 'value'; } const d = this.ownerDocument; const s = d.getSelection(); const r = d.createRange(); r.selectNodeContents(this); s.removeAllRanges(); s.addRange(r); return 'range'; }`

// publishSetValueFn writes a value the way a framework-bound field accepts
// from script: the native value setter plus input/change events, or an
// editing command in a contenteditable.
const publishSetValueFn = `function(v){ if (this.tagName === 'INPUT' || this.tagName === 'TEXTAREA') { const proto = this.tagName === 'INPUT' ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype; const set = Object.getOwnPropertyDescriptor(proto, 'value').set; set.call(this, v); this.dispatchEvent(new Event('input', {bubbles: true})); this.dispatchEvent(new Event('change', {bubbles: true})); return 'setter'; } this.focus(); const d = this.ownerDocument; const s = d.getSelection(); const r = d.createRange(); r.selectNodeContents(this); s.removeAllRanges(); s.addRange(r); d.execCommand('insertText', false, v); return 'command'; }`

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

const readValueFn = `function(){ if (this.CodeMirror && this.CodeMirror.getValue) return String(this.CodeMirror.getValue()); if (this.cmView && this.cmView.view) return String(this.cmView.view.state.doc.toString()); const v = (this.tagName === 'INPUT' || this.tagName === 'TEXTAREA') ? String(this.value || '') : String(this.innerText || this.textContent || ''); return v; }`

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
	// Same element throughout; only the input method changes: trusted
	// typing twice, then the script setter (fields that ignore synthetic
	// keys while hidden or zero-size).
	var got string
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		how := "typed"
		if attempt < 2 {
			if err := p.focusAndClear(obj); err != nil {
				lastErr = err
				continue
			}
			if err := p.typeText(text, multiline); err != nil {
				lastErr = err
				continue
			}
		} else {
			v := text
			if !multiline {
				v = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
			}
			if _, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishSetValueFn, "arguments": []map[string]interface{}{{"value": v}}, "returnByValue": true}); err != nil {
				lastErr = err
				continue
			}
			how = "set from script"
		}
		time.Sleep(200 * time.Millisecond)
		got, _ = p.call(obj, readValueFn)
		if holds(got, text) {
			return fmt.Sprintf("%s holds %d chars (%s), starts %q", name, len([]rune(got)), how, clipRunes(got, 24)), nil
		}
	}
	if lastErr != nil && got == "" {
		return "", fmt.Errorf("%s could not be filled: %v", name, lastErr)
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
	if p.bodyKind() == "markdown" {
		return p.fillMarkdown()
	}
	// Pictures that went to a gallery (note-style sites) leave a plain
	// caption body: those editors do not keep bold or headings.
	// A multi-paragraph plain body in a contenteditable goes in as <p>
	// paragraphs too: typed Enter keys make <div>/<br> lines that many
	// editors merge or drop, so the article landed as one block.
	paras := !p.doc.rich && strings.Contains(strings.TrimSpace(p.spec.Body), "\n")
	if (p.doc.rich || paras) && !p.gallery {
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
					return d + " (editor keeps no formatting: plain text, key points kept as text)", err
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

// bodyKind is the declared body kind, else what the editor element looks
// like (code/markdown editor, plain field or rich contenteditable).
func (p *publishRun) bodyKind() string {
	if p.spec.BodyKind != "" {
		return p.spec.BodyKind
	}
	if p.kind == "" {
		v, _ := p.eval("(() => {" + publishHelpersJS + " return __kind(__body(" + jsq(p.spec.BodySelector) + ", __title(" + jsq(p.spec.TitleSelector) + "))); })()")
		p.kind = v
	}
	return p.kind
}

var imgAnchorRe = regexp.MustCompile(`\[\[img:\d+(\|[^\]]*)?\]\]\n?`)

// markdownText is the body for a markdown editor: the person's Markdown as
// written (picture anchors removed; the pictures are pasted after), or the
// plain form when the body was HTML.
func (p *publishRun) markdownText() string {
	if strings.HasPrefix(strings.TrimSpace(p.spec.Body), "<") {
		return p.doc.plain
	}
	return strings.TrimSpace(Paragraphize(imgAnchorRe.ReplaceAllString(p.spec.Body, "")))
}

// fillMarkdown writes the Markdown source into the markdown editor, first
// through the editor's own API (CodeMirror 5/6 hold the document; no key
// handling runs), else one trusted insert (typed keys would continue lists
// and quotes), else a text paste. A read-back that holds the text but
// gained quote lines (a "> " carried onto every following line) or lost
// picture links counts as a failed method. The editor stays the same.
func (p *publishRun) fillMarkdown() (string, error) {
	text := p.markdownText()
	obj, err := p.element(p.bodyExpr())
	if err != nil || obj == "" {
		return "", fmt.Errorf("no body field found; pass body_selector")
	}
	var got, why string
	for _, how := range []string{"api", "insert", "paste"} {
		switch how {
		case "api":
			v, _ := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishMarkdownAPIFn, "arguments": []map[string]interface{}{{"value": text}}, "returnByValue": true})
			if !strings.Contains(string(v), "set") {
				continue
			}
		case "insert":
			if err := p.focusAndClear(obj); err != nil {
				why = err.Error()
				continue
			}
			if _, err := p.send("Input.insertText", map[string]interface{}{"text": text}); err != nil {
				why = err.Error()
				continue
			}
		case "paste":
			if err := p.focusAndClear(obj); err != nil {
				why = err.Error()
				continue
			}
			_ = p.pasteHTML(obj, "", text)
		}
		time.Sleep(300 * time.Millisecond)
		got, _ = p.call(obj, readValueFn)
		if !holds(got, text) {
			why = fmt.Sprintf("read back %d chars (%q)", len([]rune(got)), clipRunes(got, 24))
			continue
		}
		if w, g := paraCount(text), paraCount(got); w > 2 && g*2 < w {
			why = fmt.Sprintf("the editor kept %d of %d paragraph(s) (blank lines lost)", g, w)
			continue
		}
		if bad := markdownDrift(text, got); bad != "" {
			why = bad
			continue
		}
		return fmt.Sprintf("markdown body holds %d chars (%s)", len([]rune(got)), how), nil
	}
	return "", fmt.Errorf("markdown body: %s", why)
}

// publishMarkdownAPIFn sets a CodeMirror document through its API.
const publishMarkdownAPIFn = `function(v){ let e = this; for (let i = 0; e && i < 4; i++, e = e.parentElement) { if (e.CodeMirror && e.CodeMirror.setValue) { e.CodeMirror.setValue(v); return 'set5'; } if (e.cmView && e.cmView.view) { const view = e.cmView.view; view.dispatch({changes: {from: 0, to: view.state.doc.length, insert: v}}); return 'set6'; } } return 'none'; }`

// markdownDrift names what an editor added or dropped while taking the
// source: more quote lines, or fewer picture links than written.
func markdownDrift(want, got string) string {
	q := func(s string) int {
		n := 0
		for _, ln := range strings.Split(s, "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), ">") {
				n++
			}
		}
		return n
	}
	if gq, wq := q(got), q(want); gq > wq+1 {
		return fmt.Sprintf("the editor turned %d line(s) into quotes (the text has %d)", gq, wq)
	}
	im := regexp.MustCompile(`!\[[^\]]*\]\(`)
	if g, w := len(im.FindAllString(got, -1)), len(im.FindAllString(want, -1)); g < w {
		return fmt.Sprintf("the editor kept %d of %d picture link(s)", g, w)
	}
	return ""
}

// bodyWant is the text the body editor must hold.
func (p *publishRun) bodyWant() string {
	if p.bodyKind() == "markdown" {
		return p.markdownText()
	}
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
	gallery := p.spec.ImageMode == "gallery" // the profile says pictures go to a gallery input
	if !gallery && p.bodyKind() == "markdown" && p.spec.Body != "" && isImageExt(fileExt(p.spec.Files)) {
		p.pasteLater = true
		return fmt.Sprintf("%d picture(s) are pasted into the markdown editor after the text", len(p.spec.Files)), nil
	}
	if !gallery && len(p.doc.anchors) > 0 && (p.spec.ImageMode == "paste" || p.canPasteImages()) {
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
	if err == nil && p.lazyImages {
		if sum, lerr := p.checkLazyImages(); lerr != nil {
			p.incomplete.Store(lerr.Error())
			return out, fmt.Errorf("published at %s but %v", p.rep.ResultURL, lerr)
		} else {
			out += "; " + sum
		}
	}
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
		if p.gallery && p.spec.MinImages > 0 && !p.lazyImages {
			// Pictures outside the body (their upload was confirmed by the
			// network): the page must still show them; an editor that
			// cleared them on fill dropped them.
			st := p.state()
			if n := st.Imgs + st.Bg; n < p.spec.MinImages {
				return "", fmt.Errorf("the page shows %d of the %d planned picture(s): pictures were dropped; nothing was submitted", n, p.spec.MinImages)
			}
		}
		if want := p.wantImages(); want > 0 {
			// Pasted pictures are checked for storage by the rich
			// read-back (they upload after the paste).
			n := p.imgCount(obj)
			if n < want {
				return "", fmt.Errorf("the body shows %d of the %d planned picture(s): pictures were dropped; nothing was submitted", n, want)
			} else {
				parts = append(parts, fmt.Sprintf("%d/%d picture(s)", n, want))
			}
		}
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

// wantImages is how many pictures the body must show before the submit:
// every file that went into the body (anchors or paste), and never fewer
// than the plan's MinImages. Gallery pictures are checked at upload.
func (p *publishRun) wantImages() int {
	want := 0
	if !p.gallery && (p.imagesPlaced > 0 || p.pasteLater) {
		want = len(p.spec.Files)
	}
	if p.spec.MinImages > want && !p.gallery {
		want = p.spec.MinImages
	}
	return want
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
	want := map[string]bool{}
	for _, t := range texts {
		want[normText(t)] = true
	}
	return p.findWhere(func(_ *domNode, txt string) bool { return want[txt] }, buttonsOnly)
}

// findWhere walks the DOM like findByText with a label test.
func (p *publishRun) findWhere(ok func(n *domNode, txt string) bool, buttonsOnly bool) ([]*domNode, error) {
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
			if clickable && txt != "" && ok(n, txt) && n.attr("disabled") == "" && n.attr("aria-disabled") != "true" {
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

// submitVerbs start a submit button's label on sites without a profile
// ("发布文章", "Publish now", "确定并发布"); submitNot marks look-alikes.
var (
	submitVerbs = []string{"发布", "发表", "发送", "提交", "确定并发布", "确认发布", "立即发布", "publish", "post", "submit", "send"}
	submitNot   = regexp.MustCompile(`(?i)设置|草稿|预览|定时|取消|保存|评论|回复|setting|draft|preview|schedul|cancel|save|comment|reply`)
)

// clickSubmit presses the submit button: an exact text first; without a
// declared text, the newest visible button whose short label starts with a
// submit verb.
func (p *publishRun) clickSubmit() error {
	if !p.discoverSubmit {
		return p.clickText(p.spec.SubmitTexts, true)
	}
	want := map[string]bool{}
	for _, t := range p.spec.SubmitTexts {
		want[normText(t)] = true
	}
	// Exact defaults and verb-led labels alike; the newest one (a panel's
	// own confirm button comes after the editor's) is pressed.
	hits, err := p.findWhere(func(n *domNode, txt string) bool {
		if want[txt] {
			return true
		}
		// Links are navigation ("Posts", "Published (3)"): exact only.
		if n.NodeName == "A" || len([]rune(txt)) > 10 || submitNot.MatchString(txt) {
			return false
		}
		low := strings.ToLower(txt)
		for _, v := range submitVerbs {
			if strings.HasPrefix(low, v) {
				return true
			}
		}
		return false
	}, true)
	if err != nil {
		return err
	}
	for i := len(hits) - 1; i >= 0; i-- {
		if x, y, berr := p.boxCenter(map[string]interface{}{"backendNodeId": hits[i].BackendNodeID}); berr == nil {
			return p.mouseClick(x, y)
		}
	}
	return fmt.Errorf("no visible enabled submit button (tried %q and labels starting with a submit verb); pass submit_texts", strings.Join(p.spec.SubmitTexts, "/"))
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

// publishSuccessTexts: generic discovery default; a site profile names its
// own. Only text new since the click is searched.
var publishSuccessTexts = []string{"发布成功", "已发布", "发表成功", "Published", "published successfully"}

// saysAny: the first of texts found in s ("" when none).
// publishToastWatchJS records text that nodes added after the click carry.
const publishToastWatchJS = `(() => { if (!document.body) return 'none'; window.__mcAdded = []; if (window.__mcObs) window.__mcObs.disconnect(); window.__mcObs = new MutationObserver(ms => { for (const m of ms) for (const n of m.addedNodes) { const t = ((n.innerText !== undefined ? n.innerText : n.textContent) || '').trim(); if (t && t.length < 200 && window.__mcAdded.length < 200) window.__mcAdded.push(t); } }); window.__mcObs.observe(document.body, {childList: true, subtree: true}); return 'ok'; })()`

// toastSays: a success text among the nodes added since the click.
func (p *publishRun) toastSays() string {
	v, err := p.evalWithin("(window.__mcAdded || []).join(' | ')", 2*time.Second)
	if err != nil || v == "" {
		return ""
	}
	return saysAny(v, p.spec.SuccessTexts)
}

func saysAny(s string, texts []string) string {
	for _, t := range texts {
		if t != "" && strings.Contains(strings.ToLower(s), strings.ToLower(t)) {
			return t
		}
	}
	return ""
}

func (p *publishRun) see(u string) {
	if u == "" {
		return
	}
	if p.seen == nil {
		p.seen = map[string]bool{}
	}
	p.seen[u] = true
}

// hasEditor: the page still shows the body (or title) editor; an error when
// the page does not answer within d.
func (p *publishRun) hasEditor(d time.Duration) (bool, error) {
	out, err := p.evalWithin("(() => {"+publishHelpersJS+" const t = __title("+jsq(p.spec.TitleSelector)+"); return String(!!(__body("+jsq(p.spec.BodySelector)+", t) || t)); })()", d)
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

func (p *publishRun) submit() (string, error) {
	before, _ := p.eval("document.body ? document.body.innerText.slice(0, 20000) : ''")
	// Set before the click: a page that freezes on publish never answers it.
	closed := p.closeOverlays()
	p.see(p.pageURL())
	// Toasts ("published") can live for a second or two, shorter than one
	// poll that also waits on hasEditor: record text added after the click.
	_, _ = p.eval(publishToastWatchJS)
	_, _ = p.evalWithin(publishPanelMarkJS, 3*time.Second)
	p.submitClicked.Store(true)
	clickedAt := time.Now()
	p.clickedAt = clickedAt
	if err := p.clickSubmit(); err != nil {
		if strings.Contains(err.Error(), "timeout") {
			// The page froze inside the click handler: the click went
			// in and its outcome is unknown, so it may not be repeated.
			p.clickHung = true
		}
		return "", err
	}
	confirmed := false
	reclicks, panels := 0, 0
	panelNote := ""
	lastClick := time.Now()
	deadline := time.Now().Add(2*p.spec.StepBudget - 5*time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		// The site's answer to the publish write is the signal: a refused
		// write ends the wait with the site's own reason.
		if p.net != nil {
			if f := p.net.failedSince(clickedAt); len(f) > 0 {
				return "", fmt.Errorf("clicked submit and the site refused it: %s; %s", strings.Join(f, "; "), p.afterSubmitState(clickedAt))
			}
		}
		if m := p.toastSays(); m != "" {
			p.toastSeen = m
			return "page says " + m, nil
		}
		url := p.pageURL()
		if p.spec.SuccessURL != "" && url != "" {
			if re, err := regexp.Compile(p.spec.SuccessURL); err == nil && re.MatchString(url) {
				return "page is the post: " + url, nil
			}
		}
		// A URL the editor never had, on a page without the editor, is the
		// post (an autosave also renames the editor's URL, and the editor
		// stays on the page then).
		if url != "" && !p.seen[url] {
			if ed, err := p.hasEditor(3 * time.Second); err != nil || !ed {
				return "page left the editor: " + url, nil
			}
			p.see(url)
		}
		txt, _ := p.evalWithin("document.body ? document.body.innerText.slice(0, 20000) : ''", 3*time.Second)
		if m := saysAny(newText(before, txt), p.spec.SuccessTexts); m != "" {
			p.toastSeen = m
			return "page says " + m, nil
		}
		if !confirmed && len(p.spec.ConfirmTexts) > 0 {
			if p.clickText(p.spec.ConfirmTexts, true) == nil {
				confirmed = true
				lastClick = time.Now()
			}
		}
		// Many sites answer the first click with a settings panel holding
		// required fields and its own publish button: fill and confirm it.
		if panels < 3 && time.Since(lastClick) > 1500*time.Millisecond {
			if d, ok := p.handlePanel(); d != "" {
				panelNote = d
				if ok {
					panels++
					p.panelConfirmed = true
					lastClick = time.Now()
					continue
				}
			}
		}
		// A click that sent no write and opened no panel may have hit a
		// button that was not ready: press it again. A click that sent a
		// write was taken: pressing again could post twice.
		if reclicks < 2 && !p.panelConfirmed && panelNote == "" && time.Since(lastClick) > 3*time.Second && (p.net == nil || (len(p.net.submitWritesSince(clickedAt)) == 0 && p.net.draftWritesSince(clickedAt) == 0)) {
			reclicks++
			lastClick = time.Now()
			_ = p.clickSubmit()
		}
	}
	after, _ := p.eval("document.body ? document.body.innerText.slice(0, 20000) : ''")
	state := p.afterSubmitState(clickedAt)
	// A JSON answer that refused the write (GraphQL errors, success:false
	// behind HTTP 200) is the site's reason when nothing else answered.
	if r := p.net.refusedSince(clickedAt); len(r) > 0 {
		state = "the site refused it: " + strings.Join(r, "; ") + "; " + state
	}
	if closed != "" {
		state = "before the click: " + closed + "; " + state
	}
	if panelNote != "" {
		state = panelNote + "; " + state
	}
	if n := p.blockingNotices(); n != "" {
		state = "the page shows a blocking notice: " + n + "; " + state
	}
	if said := newLines(before, after); said != "" {
		return "", fmt.Errorf("clicked submit but the page neither left the editor nor said it was published; after the click it showed: %q; %s", clipRunes(said, 200), state)
	}
	return "", fmt.Errorf("clicked submit but the page neither left the editor nor said it was published; %s", state)
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

// verify finds evidence that the post is published, in this order: an id
// or link in the answer to the submission write, the page the submit
// landed on being the post (a new URL without the editor, or the profile's
// success_url), the account's post list API (verify_api), the account's
// post list page (verify_url). A page that still shows the editor is never
// the post. Without any of them the run is not called published.
func (p *publishRun) verify() (string, error) {
	key := clipRunes(p.spec.Title, 12)
	if key == "" {
		key = clipRunes(p.spec.Body, 12)
	}
	p.net.settleAnswers(p.clickedAt, 5*time.Second)
	if id, link := p.net.evidenceSince(p.clickedAt); id != "" || link != "" {
		p.rep.PostID = id
		if link != "" {
			p.rep.ResultURL = strings.SplitN(link, "#", 2)[0]
			if strings.HasPrefix(p.rep.ResultURL, "//") {
				p.rep.ResultURL = "https:" + p.rep.ResultURL
			}
		}
		p.rep.Evidence = "response"
		return fmt.Sprintf("the site answered the submit with post id %q %s", id, link), nil
	}
	if u := p.pageURL(); u != "" {
		success := false
		if p.spec.SuccessURL != "" {
			if re, err := regexp.Compile(p.spec.SuccessURL); err == nil && re.MatchString(u) {
				success = true
			}
		}
		ed, err := p.hasEditor(3 * time.Second)
		if success || (err == nil && !ed && !p.seen[u]) {
			if out, cerr := p.checkResult(key, 6*time.Second, false); cerr == nil {
				p.rep.Evidence = "redirect"
				return out, nil
			}
		}
	}
	if p.spec.VerifyAPI != "" {
		out, err := p.checkAPI(key)
		if err == nil {
			p.rep.Evidence = "list_api"
			return out, nil
		}
		if p.spec.VerifyURL == "" {
			return "", err
		}
	}
	if p.spec.VerifyURL == "" {
		u := p.pageURL()
		if publishSubmittedURL.MatchString(u) {
			return "", fmt.Errorf("the site confirmed the submit in its URL (%s), so the post IS submitted: do NOT publish again. It is not confirmed yet: read it back once from the account's own post list (the site profile's verify_url) and reply with its link; a post still under review may not open publicly yet", u)
		}
		return "", fmt.Errorf("no evidence of the post: the site's answer carried no id, the page %s did not become the post, and no post list (verify_url / verify_api) is known for this site", u)
	}
	if err := p.navigate(p.spec.VerifyURL); err != nil {
		return "", err
	}
	out, err := p.checkResult(key, minDur(p.spec.StepBudget, 25*time.Second), true)
	if err == nil {
		p.rep.Evidence = "list"
	}
	return out, err
}

// publishAPIFindJS fetches a JSON list with the page's session and finds
// the object whose string fields hold the title key; it returns the id and
// link fields of that object.
const publishAPIFindJS = `(async (url, key) => {
  const n = s => String(s || '').replace(/\s+/g, '');
  let j;
  try { const r = await fetch(url, {credentials: 'include'}); j = await r.json(); } catch (e) { return JSON.stringify({err: String(e).slice(0, 120)}); }
  const idKeys = ['article_id','articleId','post_id','postId','note_id','noteId','aid','cvid','id'];
  const urlKeys = ['url','link','article_url','share_url','post_url'];
  let hit = null;
  const walk = (x, d) => {
    if (hit || !x || typeof x !== 'object' || d > 8) return;
    if (!Array.isArray(x)) {
      for (const v of Object.values(x)) if (typeof v === 'string' && n(v).includes(key) && v.length < 400) { hit = x; return; }
    }
    for (const v of Object.values(x)) walk(v, d + 1);
  };
  walk(j, 0);
  if (!hit) return JSON.stringify({found: false});
  let id = '', link = '';
  for (const k of idKeys) if (!id && (typeof hit[k] === 'string' || typeof hit[k] === 'number') && String(hit[k]).length >= 3) id = String(hit[k]);
  for (const k of urlKeys) if (!link && typeof hit[k] === 'string' && /^(https?:)?\/\//.test(hit[k])) link = hit[k];
  return JSON.stringify({found: true, id, link});
})`

// checkAPI reads the profile's post list API for the title.
func (p *publishRun) checkAPI(key string) (string, error) {
	deadline := time.Now().Add(minDur(p.spec.StepBudget, 25*time.Second))
	for {
		out, err := p.evalWithin(publishAPIFindJS+"("+jsq(p.spec.VerifyAPI)+", "+jsq(normText(key))+")", 10*time.Second)
		var o struct {
			Found bool   `json:"found"`
			ID    string `json:"id"`
			Link  string `json:"link"`
			Err   string `json:"err"`
		}
		_ = json.Unmarshal([]byte(out), &o)
		if err == nil && o.Found {
			p.rep.PostID = o.ID
			if o.Link != "" {
				if strings.HasPrefix(o.Link, "//") {
					o.Link = "https:" + o.Link
				}
				p.rep.ResultURL = o.Link
			}
			return fmt.Sprintf("post list API lists %q (id %s)", key, o.ID), nil
		}
		if time.Now().After(deadline) {
			why := o.Err
			if err != nil {
				why = err.Error()
			}
			return "", fmt.Errorf("post list API %s does not list %q %s", p.spec.VerifyAPI, key, why)
		}
		time.Sleep(2 * time.Second)
	}
}

// A landing URL that itself says the submit went through (?published=true).
var publishSubmittedURL = regexp.MustCompile(`(?i)[?&#](published|publish_success|success|status)=(true|1|success|ok)`)

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
// image post: the profile's labels when given, else generic ones ("图文",
// "图片", "Image post", "Photos").
func publishImageTabJS(texts []string) string {
	res := "[/^图文$|^(image|photo) posts?$/i, /^图片$|^(upload )?(images?|photos?)$/i]"
	if len(texts) > 0 {
		b, _ := json.Marshal(texts)
		res = "(" + string(b) + ").map(t => ({test: s => s === t}))"
	}
	return `(() => { const res = ` + res + `; const vis = e => e.getClientRects().length > 0; const all = [...document.querySelectorAll('[role=tab],button,a,span,div')].filter(e => vis(e) && e.children.length <= 2); let e = null; for (const re of res) { const c = all.filter(x => re.test((x.innerText || '').trim())); if (c.length) { e = c[c.length - 1]; break; } } if (!e) return 'none'; e.click(); return 'clicked'; })()`
}

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
	count := pictureCountFn
	before, _ := p.call(obj, count)
	// The browser reads the files itself (server and desktop disks differ;
	// ZN-7 could not paste because the server read the desktop path).
	if _, err := p.loadPicker(); err != nil {
		return "", err
	}
	var b int
	fmt.Sscan(before, &b)
	t0 := time.Now()
	pasted := 0
	var fails []string
	for k := range p.spec.Files {
		// One retry per picture: an editor busy with the previous upload
		// can drop a paste.
		err := p.pasteFile(obj, k)
		if err != nil {
			time.Sleep(time.Second)
			err = p.pasteFile(obj, k)
		}
		if err != nil {
			fails = append(fails, err.Error())
			continue
		}
		pasted++
	}
	a := p.imgCount(obj)
	if a-b < len(p.spec.Files) {
		return "", fmt.Errorf("pasted %d picture(s) but the editor shows %d new of %d (%s); nothing was submitted", pasted, a-b, len(p.spec.Files), strings.Join(fails, "; "))
	}
	if p.net != nil {
		started, ok, failed, _ := p.net.uploadStats(t0)
		if failed > 0 {
			return "", fmt.Errorf("pasted %d picture(s) but the site's upload failed (%s); nothing was submitted", pasted, strings.Join(p.net.failedSince(t0), "; "))
		}
		return fmt.Sprintf("pasted %d image(s) into the body; editor imgs %d->%d; upload requests %d (ok %d)", pasted, b, a, started, ok), nil
	}
	return fmt.Sprintf("pasted %d image(s) into the body; editor imgs %d->%d", pasted, b, a), nil
}

// newLines is the text lines that appear in after but not in before.
func newLines(before, after string) string {
	return clipRunes(newText(before, after), 200)
}

// newText is every line of after that before did not have, unclipped.
func newText(before, after string) string {
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
	return strings.Join(out, " | ")
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

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// pictureCountFn counts the pictures an editor holds: <img> elements, or
// Markdown image links in a code editor's text.
// checkLazyImages: a lazy-upload site must show the pictures stored: the
// submit answer names remote picture URLs, or the page now shows them.
func (p *publishRun) checkLazyImages() (string, error) {
	want := len(p.spec.Files)
	if p.spec.MinImages > want {
		want = p.spec.MinImages
	}
	if n := p.net.answerImagesSince(p.clickedAt); n >= want {
		return fmt.Sprintf("the submit answer names %d remote picture(s)", n), nil
	}
	v, _ := p.eval(`String([...document.images].filter(i => /^https?:/.test(i.src) && i.naturalWidth > 1).length)`)
	n, _ := strconv.Atoi(v)
	if n >= want {
		return fmt.Sprintf("the published page shows %d remote picture(s)", n), nil
	}
	return "", fmt.Errorf("the lazily uploaded pictures were not confirmed: the submit answer and the page show %d remote picture(s), %d planned", n, want)
}

const pictureCountFn = `function() { let n = this.querySelectorAll('img').length; let v = ''; if (this.CodeMirror && this.CodeMirror.getValue) v = this.CodeMirror.getValue(); else if (this.cmView && this.cmView.view) v = this.cmView.view.state.doc.toString(); else if (this.tagName === 'TEXTAREA') v = this.value; else if (this.closest && this.closest('.cm-editor,.CodeMirror,.bytemd,[class*=markdown]')) v = this.innerText; n += (v.match(/!\[[^\]]*\]\(/g) || []).length; return String(n); }`

// recordMethod writes which branch each primitive took into the report
// (exploration while doing real tasks; the hub keeps it per site).
func (p *publishRun) recordMethod() {
	m := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	via := p.uploadVia
	if via == "" && p.pasteLater {
		via = "paste"
	}
	if i := strings.Index(via, "file input "); i >= 0 {
		set("upload_input_rank", strings.TrimSpace(strings.Fields(via[i+len("file input "):] + " ")[0]))
		via = "file_input"
	} else if strings.HasPrefix(via, "intercepted chooser") {
		via = "chooser:" + p.spec.UploadTrigger
	}
	set("upload", via)
	if p.lazyImages {
		set("lazy_upload", "true")
	}
	set("body_kind", p.kind)
	if p.spec.BodyKind != "" {
		set("body_kind", p.spec.BodyKind)
	}
	if p.panelConfirmed {
		set("submit", "panel_confirm")
	} else if p.submitClicked.Load() {
		set("submit", "click")
	}
	set("evidence", p.rep.Evidence)
	for _, st := range p.rep.Steps {
		if st.Name == "fill_title" || st.Name == "fill_body" {
			if i := strings.Index(st.Detail, "("); i >= 0 {
				if j := strings.Index(st.Detail[i:], ")"); j > 0 {
					set(st.Name, st.Detail[i+1:i+j])
				}
			}
		}
	}
	if len(m) > 0 {
		p.rep.Method = m
	}
}

// ExplorePublish reads an editor page without writing anything: which
// fields, file inputs and publish buttons it has. No file is set, nothing
// is typed or clicked (beyond opening spec.EditorURL).
func ExplorePublish(d PublishDriver, spec PublishSpec) (map[string]string, error) {
	if spec.StepBudget <= 0 {
		spec.StepBudget = 20 * time.Second
	}
	if len(spec.SubmitTexts) == 0 {
		spec.SubmitTexts = defaultSubmitTexts
	}
	p := &publishRun{d: d, spec: spec, rep: &PublishReport{}, doc: parseRichBody("", 0, "")}
	detail, err := p.observe()
	out := map[string]string{"observe": detail}
	if p.rep.Notice != "" {
		out["notice"] = p.rep.Notice
	}
	if err != nil {
		return out, err
	}
	out["body_kind"] = p.bodyKind()
	if v, err := p.eval(publishExploreJS(spec.SubmitTexts)); err == nil {
		out["surface"] = v
	}
	return out, nil
}

func publishExploreJS(submit []string) string {
	b, _ := json.Marshal(submit)
	return `(() => { const want = ` + string(b) + `; const docs = [document]; const addFrames = d => { for (const f of d.querySelectorAll('iframe,frame')) { try { const c = f.contentDocument; if (c && !docs.includes(c)) { docs.push(c); addFrames(c); } } catch (e) {} } }; addFrames(document); const inputs = [], buttons = []; for (const d of docs) { for (const i of d.querySelectorAll('input[type=file]')) inputs.push({accept: i.accept || '', multiple: !!i.multiple, frame: d !== document}); for (const e of d.querySelectorAll('button,[role=button],a')) { const t = (e.innerText || '').trim(); if (t && t.length <= 12 && want.some(w => t.includes(w))) buttons.push(t); } } return JSON.stringify({frames: docs.length - 1, file_inputs: inputs, submit_buttons: [...new Set(buttons)].slice(0, 8)}); })()`
}
