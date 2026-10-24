package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Run coaching (post-mortem of run_4d3e6297, 10-22: 200 actions in 35 min,
// 57 page scripts and 57 probes, the body only landed at step 126 after the
// first real Ctrl+V, publish at 150, then 50 more steps re-pasting into the
// published article until the budget ran out without clicking 更新).
//
//  - budget: at 70% and from 90% of the run's action budget the result says
//    how many actions are left and to save/更新 and report.
//  - no progress: a step that changes nothing the run can see (same page
//    URL, same editor length, no read-back, no navigation, no delivery) is
//    counted; at MACLAW_DESKTOP_NOPROGRESS_WARN (default 12) the model is
//    told to change approach or ask; at MACLAW_DESKTOP_NOPROGRESS_STOP
//    (default 30) further desktop actions are refused except takeover and
//    deliver.
//  - ladder: after 2 未生效 results the next input rung is named.
//  - published: a result showing a published article URL or 发布成功 says to
//    verify once and stop.

var (
	desktopURLRe       = regexp.MustCompile(`https?://[^\s;)'"]+`)
	desktopBodyCountRe = regexp.MustCompile(`editor body has (\d+) chars|字数[:：]\s*(\d+)`)
	// Generic (every site): a submit was reported by the step output.
	desktopPublishAttemptRe = regexp.MustCompile(`(?i)(发布|发表|提交|发送)成功|\bclicked\s*\S{0,6}(发布|发表|提交|更新|publish|post|submit|send)|\b(published|submitted)\b`)
	// Editor/compose pages: a result is never verified while still on one.
	desktopEditURLRe = regexp.MustCompile(`(?i)/(write|edit|editor|compose|new|draft|drafts|create|publish)(?:[/?#]|$)`)
)

var desktopReadOnlyActions = map[string]bool{
	"probe": true, "screenshot": true, "clipboard_get": true, "app_probe": true, "app_list": true,
	"wait": true, "file_list": true, "file_read": true, "window_list": true, "screen_info": true, "skill_list": true,
}

type desktopProgress struct {
	lastURL     string
	lastBody    int
	idle        int
	notApplied  int
	warned70    bool
	warned90    bool
	publishSeen bool
	// publishVerified: action=publish returned PUBLISHED and verified;
	// afterPublish counts the desktop steps taken after it.
	publishVerified bool
	afterPublish    int
	// publishTried: this run pressed 发布/更新 (or saw 发布成功). An article
	// URL seen before that is a page left open by an earlier task.
	publishTried bool
	staleWarned  bool
	// Generic verified-result detection: snippets of the text this run
	// filled, and the page URL it was filled on. A result is verified when,
	// after a submit, the page is a different non-editor URL and a read-back
	// shows the filled text.
	filled         []string
	writeURL       string
	readBackHinted bool
	// Batch-default path (generic, every site): observe once, then one
	// action=batch for the rest. These counters enforce that outside a plan.
	observed       bool
	singleMutating int
	batchStarted   bool
	batchHinted    bool
}

func desktopEnvIntDefault(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v >= 0 {
		return v
	}
	return def
}

func desktopNoProgressWarn() int { return desktopEnvIntDefault("MACLAW_DESKTOP_NOPROGRESS_WARN", 12) }
func desktopNoProgressStop() int { return desktopEnvIntDefault("MACLAW_DESKTOP_NOPROGRESS_STOP", 30) }

func desktopProgressFor(scope agentruntime.Scope) (*desktopRunState, *desktopProgress) {
	st := desktopRunFor(scope)
	if st == nil {
		return nil, nil
	}
	if st.progress == nil {
		st.progress = &desktopProgress{lastBody: -1}
	}
	return st, st.progress
}

// desktopNoProgressCheck refuses work after a long stretch without change.
func desktopNoProgressCheckFull(scope agentruntime.Scope, action string, publishOK, publishStep bool) error {
	stop := desktopNoProgressStop()
	if stop <= 0 || action == "request_takeover" || action == "deliver" {
		return nil
	}
	st, p := desktopProgressFor(scope)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if p.idle >= stop {
		if publishOK && p.lastBody > 0 && !p.publishTried {
			if publishStep {
				return nil
			}
			return fmt.Errorf("no progress: the last %d desktop actions changed nothing visible. The editor already has the body (%d) and the person asked to publish: stop the side work (topics, formatting, checks) and press 发布 now — one click on the 发布 button or one script that clicks it; then open the /p/<id> page and report it", p.idle, p.lastBody)
		}
		return fmt.Errorf("no progress: the last %d desktop actions changed nothing visible (same page, same editor length, nothing read back). Stop operating the desktop: tell the person what is done and what is stuck, or call request_takeover", p.idle)
	}
	return nil
}

// desktopNoProgressCheckStep is desktopNoProgressCheck that still lets an
// approved publish through once the body is in.
func desktopNoProgressCheckStep(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) error {
	publishStep := false
	switch action {
	case "script":
		publishStep = desktopScriptClicksPublish(desktopArg(args, "script"))
	case "click":
		publishStep = desktopIsPublishLabel(desktopArg(args, "text"))
	}
	return desktopNoProgressCheckFull(scope, action, desktopPublishAllowed(ctx), publishStep)
}

func desktopNoProgressCheck(scope agentruntime.Scope, action string) error {
	return desktopNoProgressCheckFull(scope, action, false, false)
}

// desktopProgressNote updates progress after a step and returns coaching text
// to append to the result ("" when nothing to say).
func desktopProgressNote(scope agentruntime.Scope, action string, out string, err error) string {
	st, p := desktopProgressFor(scope)
	if st == nil {
		return ""
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	var notes []string

	progressed := false
	if err == nil {
		if action == "navigate" || action == "deliver" || action == "request_takeover" || action == "file_write" {
			progressed = true
		}
		if u := desktopFirstURL(out); u != "" && u != p.lastURL {
			if p.lastURL != "" {
				progressed = true
			}
			p.lastURL = u
		}
		if m := desktopBodyCountRe.FindStringSubmatch(out); m != nil {
			n, _ := strconv.Atoi(m[1] + m[2])
			if n != p.lastBody {
				if p.lastBody >= 0 {
					progressed = true
				}
				p.lastBody = n
			}
		}
		if strings.Contains(out, desktopAppliedMarker) {
			progressed = true
		}
		if action == "bash" && !desktopReadOnlyActions[action] {
			progressed = true // shell work (files, conversion) is not page progress but is real work
		}
	}
	if strings.Contains(out, desktopNotAppliedMarker) || (err != nil && strings.Contains(err.Error(), "未生效")) {
		p.notApplied++
		if p.notApplied == 2 {
			notes = append(notes, "Input ladder: this method failed twice. Next rung, in order: (1) action=rich_fill (real click + clipboard + real Ctrl+V + read-back), (2) app_run type with real keystrokes, (3) the site's import (导入文档) with a file. Do not go back to page scripts for text.")
		}
	}
	if progressed {
		p.idle = 0
	} else {
		p.idle++
	}
	if p.observed && !p.batchStarted && !p.batchHinted && action == "probe" {
		p.batchHinted = true
		notes = append(notes, "Page observed. Put the remaining form/flow into ONE action=batch plan (script/rich_fill/click/type/press/wait steps), then one read-back. Do not click or type step by step.")
	}
	if w := desktopNoProgressWarn(); w > 0 && p.idle == w {
		notes = append(notes, fmt.Sprintf("No progress in the last %d actions (nothing on the page changed). Stop repeating probes/screenshots/scripts: pick a different method from the input ladder, or call request_takeover and say what is stuck.", p.idle))
	}

	if max := desktopRunMaxActions(); max > 0 {
		left := max - st.actions
		switch {
		case st.actions*10 >= max*9 && left >= 0:
			if !p.warned90 || left%5 == 0 {
				notes = append(notes, fmt.Sprintf("Budget: %d of %d desktop actions used, %d left. Wrap up now: save or press 更新/保存 only if the person approved publishing (else request_takeover), then deliver a summary. Do not start new attempts.", st.actions, max, left))
			}
			p.warned90 = true
		case st.actions*10 >= max*7 && !p.warned70:
			p.warned70 = true
			notes = append(notes, fmt.Sprintf("Budget: %d of %d desktop actions used (70%%). Keep the last 10%% for saving and reporting; stop exploring and use the most direct method.", st.actions, max))
		}
	}

	if err == nil && len(p.filled) > 0 && desktopPublishAttemptRe.MatchString(out) {
		p.publishTried = true
	}
	if p.publishTried {
		desktopNoteEditLink(scope.RunID, p.lastURL)
	}
	if err == nil && p.publishTried && !p.publishSeen && !p.readBackHinted && p.lastURL != p.writeURL && !desktopEditURLRe.MatchString(strings.SplitN(p.lastURL, "?", 2)[0]) && !desktopVerifiedResult(p, out) {
		p.readBackHinted = true
		notes = append(notes, "Submitted and the page moved to "+p.lastURL+". Read it back ONCE (one script returning location.href, the title and the first lines of the body), then report.")
	}
	if err == nil && action == "publish" && strings.Contains(out, "PUBLISHED and verified") {
		p.publishVerified = true
		p.publishSeen = true
		desktopNotePublishedLink(scope.RunID, out)
	}
	if err == nil && desktopVerifiedResult(p, out) {
		p.publishSeen = true
		desktopNotePublishedURL(scope.RunID, p.lastURL)
		desktopRecipeValidate(scope)
		notes = append(notes, "Verified: the page moved to "+p.lastURL+" and it shows the text this task filled. Stop operating the desktop and report that URL. Do not keep editing the published result.")
	}
	if len(notes) == 0 {
		return ""
	}
	log.Printf("[desktop-coach] run=%s actions=%d idle=%d notes=%d", scope.RunID, st.actions, p.idle, len(notes))
	return "\n\n" + strings.Join(notes, "\n")
}

func desktopFirstURL(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Open browser pages") || strings.HasPrefix(strings.TrimSpace(line), "- ") {
			continue
		}
		if u := desktopURLRe.FindString(line); u != "" {
			return u
		}
	}
	return ""
}

// rich_fill: a generic rich-editor fill. Focus the editor with a real click
// (probe ref) or by clicking the given screen point, put the text on the
// clipboard, paste with a real Ctrl+V, and read back the editor length.
func desktopRichFill(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	text := desktopRawArg(args, "text")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("rich_fill needs text")
	}
	var parts []string
	if note := desktopRaiseBrowser(ctx, scope); note != "" {
		parts = append(parts, "raised the browser ("+note+")")
	}
	step := func(sub map[string]any) error {
		out, err := operateDesktopWithTimeout(ctx, scope, sub)
		if err != nil {
			return fmt.Errorf("rich_fill %s: %w", desktopArg(sub, "action"), err)
		}
		if a := desktopArg(sub, "action"); a == "click" {
			parts = append(parts, "focused the editor")
		}
		_ = out
		return nil
	}
	// Focus the page's main rich editor through the page first. A pixel
	// click needs screen coordinates, and the model often passes the
	// viewport ones a script reported (run_bcc21d49 clicked y=356 for an
	// editor at viewport y=343: the browser toolbar, so the body stayed 0).
	focused := false
	sel := desktopArg(args, "selector")
	if out, err := operateDesktopWithTimeout(ctx, scope, map[string]any{"action": "script", "script": desktopRichFocusScriptFor(sel)}); err == nil && strings.Contains(out, "RICH_FOCUSED") {
		focused = true
		parts = append(parts, "focused the editor")
	}
	switch {
	case focused:
	case desktopArg(args, "ref") != "":
		click := map[string]any{"action": "click", "ref": desktopArg(args, "ref")}
		if sid := desktopArg(args, "snapshot_id"); sid != "" {
			click["snapshot_id"] = sid
		}
		if err := step(click); err != nil {
			return "", err
		}
	case args["x"] != nil && args["y"] != nil:
		if err := step(map[string]any{"action": "app_run", "steps": []any{map[string]any{"action": "click", "x": args["x"], "y": args["y"]}}}); err != nil {
			return "", err
		}
		parts = append(parts, "focused the editor")
	default:
		return "", fmt.Errorf("rich_fill needs ref (from probe) or x,y of the editor body")
	}
	if err := step(map[string]any{"action": "clipboard_set", "text": text}); err != nil {
		return "", err
	}
	desktopNoteClipboard(desktopOwner(scope), map[string]any{"text": text})
	if b, _ := args["replace"].(bool); b || strings.EqualFold(desktopArg(args, "replace"), "true") {
		if err := step(map[string]any{"action": "app_run", "steps": []any{map[string]any{"action": "key", "key": "ctrl+a"}}}); err != nil {
			return "", err
		}
	}
	out, err := operateDesktopWithTimeout(ctx, scope, map[string]any{"action": "app_run", "steps": []any{map[string]any{"action": "key", "key": "ctrl+v"}}})
	if err != nil {
		return "", fmt.Errorf("rich_fill paste: %w", err)
	}
	parts = append(parts, fmt.Sprintf("pasted %d chars with a real Ctrl+V", len([]rune(text))))
	if m := desktopBodyCountRe.FindString(out); m != "" {
		parts = append(parts, m)
	}
	if rb, err := operateDesktopWithTimeout(ctx, scope, map[string]any{"action": "script", "script": desktopRichReadbackScript(sel, text)}); err == nil {
		if line := desktopRichReadbackLine(rb); line != "" {
			parts = append(parts, line)
			parts = append(parts, desktopRichFillNext(scope, line, text))
		}
	}
	res := "rich_fill: " + strings.Join(parts, "; ")
	if strings.Contains(out, desktopNotAppliedMarker) {
		if !focused {
			res += "; the page editor could not be focused by script, so the paste went elsewhere — call rich_fill again with only text and replace=true (no x,y); do not type the body in chunks"
		}
		return res + "\n" + out[strings.Index(out, desktopNotAppliedMarker):], nil
	}
	return res + "\n" + out, nil
}

// desktopNotePublishAttempt marks the run as having pressed publish when the
// step itself is a publish click or a script that clicks publish.
func desktopNotePublishAttempt(scope agentruntime.Scope, action string, args map[string]any) {
	hit := false
	switch action {
	case "script":
		hit = desktopScriptClicksPublish(desktopArg(args, "script"))
	case "click":
		hit = desktopIsPublishLabel(desktopArg(args, "text"))
	}
	var texts []string
	desktopCollectFilled(action, args, &texts, &hit)
	if !hit && len(texts) == 0 {
		return
	}
	st, p := desktopProgressFor(scope)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range texts {
		if sn := desktopFilledSnippet(t); sn != "" {
			if len(p.filled) == 0 {
				p.writeURL = p.lastURL
			}
			if len(p.filled) < 8 {
				p.filled = append(p.filled, sn)
			}
		}
	}
	if hit && len(p.filled) > 0 {
		p.publishTried = true
	}
}

// desktopCollectFilled gathers the text a write step puts on the page
// (rich_fill/type text, and the same inside a batch plan) and whether a step
// presses a submit control.
func desktopCollectFilled(action string, args map[string]any, texts *[]string, hit *bool) {
	switch action {
	case "rich_fill", "type":
		if t := desktopRawArg(args, "text"); t != "" {
			*texts = append(*texts, t)
		}
	case "script":
		if desktopScriptClicksPublish(desktopArg(args, "script")) || desktopScriptSubmits(desktopArg(args, "script")) {
			*hit = true
		}
	case "click":
		if desktopIsPublishLabel(desktopArg(args, "text")) {
			*hit = true
		}
	case "batch":
		plan, _ := args["plan"].([]any)
		for _, it := range plan {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			a := strings.ToLower(strings.TrimSpace(desktopArg(m, "action")))
			if a != "batch" {
				desktopCollectFilled(a, m, texts, hit)
			}
		}
	}
}

var desktopSnippetTrimRe = regexp.MustCompile(`^[\s#>*\-+|\d.、]+`)

// desktopFilledSnippet: the first meaningful line of filled text, markup
// stripped, whitespace removed, at most 12 runes ("" when too short).
func desktopFilledSnippet(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = desktopSnippetTrimRe.ReplaceAllString(line, "")
		line = strings.NewReplacer("*", "", "`", "", "[", "", "]", "").Replace(line)
		line = strings.Join(strings.Fields(line), "")
		r := []rune(line)
		if len(r) < 4 {
			continue
		}
		if len(r) > 12 {
			r = r[:12]
		}
		return string(r)
	}
	return ""
}

// desktopVerifiedResult (generic, every site): after this run filled text
// and pressed a submit control, the page moved to a different non-editor URL
// and the step output (a read-back) contains text this run filled.
func desktopVerifiedResult(p *desktopProgress, out string) bool {
	if p.publishSeen || !p.publishTried || len(p.filled) == 0 {
		return false
	}
	u := desktopFirstURL(out)
	if u == "" {
		u = p.lastURL
	}
	if u == "" || desktopEditURLRe.MatchString(strings.SplitN(u, "?", 2)[0]) {
		return false
	}
	if p.writeURL != "" && strings.SplitN(u, "#", 2)[0] == strings.SplitN(p.writeURL, "#", 2)[0] {
		return false
	}
	flat := strings.Join(strings.Fields(out), "")
	for _, sn := range p.filled {
		if strings.Contains(flat, sn) {
			return true
		}
	}
	return false
}

var desktopOfficeFileRe = regexp.MustCompile(`(?i)\.(docx?|odt|pdf|pptx?|xlsx?)\b|python-docx|\b(libreoffice|lowriter|soffice|pandoc)\b`)

// desktopAfterPublishCheck stops a run that has already published and read
// back its article from starting a side job: writing a Word/PDF copy, opening
// LibreOffice, and closing its recovery dialogs (run_ddcd9539 spent its last
// minutes on that after /p/2096841432191575571 was verified). A file the
// person asked for still goes out with action=deliver, which stays allowed.
func desktopAfterPublishCheck(scope agentruntime.Scope, action string, args map[string]any) error {
	st, p := desktopProgressFor(scope)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	done := p.publishSeen
	st.mu.Unlock()
	if !done {
		return nil
	}
	text := ""
	switch action {
	case "app_open":
		text = desktopArg(args, "program") + " " + desktopArg(args, "args")
	case "bash":
		text = desktopArg(args, "command")
	case "file_write":
		text = desktopArg(args, "path")
	default:
		return nil
	}
	if !desktopOfficeFileRe.MatchString(text) {
		return nil
	}
	return fmt.Errorf("refused: already_published — the article is published and read back; this task did not need a document copy. If your last desktop action is not already a page script on /p/<id> returning the h1 and the article body text length, run exactly that one script now (it is the proof the result check reads; no screenshot, OCR or probe); then give the final answer (title, /p/<id> link, date used). Only if the person asked for a file in this task, send it with action=deliver")
}

// desktopRichFocusScript focuses the largest visible contenteditable (the
// article body of Draft.js / ProseMirror / Quill editors) and reports whether
// keyboard focus is in it.
func desktopRichFocusScriptFor(sel string) string {
	if strings.TrimSpace(sel) == "" {
		return desktopRichFocusScript
	}
	q, _ := json.Marshal(sel)
	return `const ed = document.querySelector(` + string(q) + `);
if (!ed) return 'RICH_NONE';
ed.scrollIntoView({block: 'center'});
ed.focus();
const ae = document.activeElement;
return (ae === ed || ed.contains(ae)) ? 'RICH_FOCUSED' : 'RICH_NOFOCUS';`
}

// desktopRichReadbackScript reads what the filled field holds now: the
// selector's element, else the focused field or contenteditable root, else
// the largest visible editor. It reports kind, length, whether the start of
// the pasted text is present (whitespace-insensitive) and the first chars.
func desktopRichReadbackScript(sel, want string) string {
	q, _ := json.Marshal(sel)
	w := []rune(want)
	if len(w) > 200 {
		w = w[:200]
	}
	wq, _ := json.Marshal(string(w))
	return `const sel = ` + string(q) + `, want = ` + string(wq) + `;
const isField = e => e && (e.tagName === 'INPUT' || e.tagName === 'TEXTAREA');
let ed = sel ? document.querySelector(sel) : null;
if (!ed) { const ae = document.activeElement; if (isField(ae)) ed = ae; else if (ae && ae.closest) ed = ae.closest('[contenteditable="true"],[contenteditable=""]'); }
if (!ed) { const eds = [...document.querySelectorAll('[contenteditable="true"]')].filter(e => { const r = e.getBoundingClientRect(); return r.width > 200 && r.height > 20; }); eds.sort((a, b) => { const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect(); return rb.width * rb.height - ra.width * ra.height; }); ed = eds[0]; }
if (!ed) return 'RICH_READBACK kind=none len=0 has=no head=""';
const v = isField(ed) ? String(ed.value || '') : String(ed.innerText || ed.textContent || '');
const norm = s => s.replace(/\s+/g, '');
const key = norm(want).slice(0, 20);
return 'RICH_READBACK kind=' + (isField(ed) ? 'field' : 'editor') + ' len=' + v.length + ' has=' + (key && norm(v).includes(key) ? 'yes' : 'no') + ' head=' + JSON.stringify(v.slice(0, 30));`
}

var desktopRichReadbackRe = regexp.MustCompile(`RICH_READBACK kind=(\w+) len=(\d+) has=(yes|no) head=("(?:[^"\\]|\\.)*")`)

// desktopRichReadbackLine turns the script output into the read-back the
// model sees.
func desktopRichReadbackLine(out string) string {
	m := desktopRichReadbackRe.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("read back: the %s now holds %s chars, your text present=%s, starts %s", m[1], m[2], m[3], m[4])
}

var desktopRichFills sync.Map // run key -> *desktopRichFillState

type desktopRichFillState struct {
	mu          sync.Mutex
	title, body bool
}

// desktopRichFillNext records a verified fill (short single-line text or a
// form field counts as the title, anything else as the body) and says what
// comes next.
func desktopRichFillNext(scope agentruntime.Scope, line, text string) string {
	if !strings.Contains(line, "present=yes") {
		return "NOT verified: the text is not in the field — fill it once more with replace=true (selector for a title field); do not probe or script around it"
	}
	v, _ := desktopRichFills.LoadOrStore(desktopTrajRunKey(scope), &desktopRichFillState{})
	st := v.(*desktopRichFillState)
	st.mu.Lock()
	defer st.mu.Unlock()
	if strings.Contains(line, "the field ") || (utf8.RuneCountInString(strings.TrimSpace(text)) <= 100 && !strings.Contains(strings.TrimSpace(text), "\n")) {
		st.title = true
	} else {
		st.body = true
	}
	switch {
	case st.title && st.body:
		return "VERIFIED: title and body are both in place. NEXT STEP: action=publish with the same title= and body= and submit_texts=[the publish button text]; it re-checks, clicks publish and verifies the result. No more fills, probes or checks before it"
	case st.body:
		return "VERIFIED body. Next: fill the title the same way (rich_fill with selector of the title field) if it is still empty, then click submit/publish"
	default:
		return "VERIFIED title. Next: rich_fill the body, then click submit/publish"
	}
}

const desktopRichFocusScript = `const eds = [...document.querySelectorAll('[contenteditable="true"]')].filter(e => { const r = e.getBoundingClientRect(); return r.width > 200 && r.height > 20; });
eds.sort((a, b) => { const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect(); return rb.width * rb.height - ra.width * ra.height; });
const ed = eds[0];
if (!ed) return 'RICH_NONE';
ed.scrollIntoView({block: 'center'});
ed.focus();
const ae = document.activeElement;
return (ae === ed || ed.contains(ae)) ? 'RICH_FOCUSED' : 'RICH_NOFOCUS';`

// desktopAskedForFileRe matches a request that wants a file back.
var desktopAskedForFileRe = regexp.MustCompile(`(?i)(文件|文档|附件|下载|导出|docx|\bword\b|pdf|markdown|\.md\b|\bfile\b|document|attach|export|download)`)

// desktopAfterPublishDeliverCheck refuses an unasked file once the article is
// published: the published link is the result.
func desktopAfterPublishDeliverCheck(scope agentruntime.Scope, request string) error {
	st, p := desktopProgressFor(scope)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	done := p.publishSeen
	st.mu.Unlock()
	if !done || strings.TrimSpace(request) == "" || desktopAskedForFileRe.MatchString(request) {
		return nil
	}
	return fmt.Errorf("refused: already_published — the request did not ask for a file; the published link is the result. Give the final answer now (title, /p/<id> link, date used) with no attachment")
}

var desktopAskedForAppRe = regexp.MustCompile(`(?i)libreoffice|wps|word|excel|powerpoint|终端|terminal|编辑器|editor|打开.{0,6}(应用|软件|程序)|\bapp\b`)

// desktopDriftCheck (generic, every site): once a run is working in the
// browser, it may not wander into desktop apps or make office documents the
// request did not ask for (RUN38 opened a document app mid-task; XHS-1 spent
// minutes in other windows). Images and plain files for an upload stay
// allowed, and so does anything the request names.
func desktopDriftCheck(scope agentruntime.Scope, action string, args map[string]any, request string) error {
	st, p := desktopProgressFor(scope)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	inBrowser := p.lastURL != ""
	published := p.publishSeen
	over := false
	if p.publishVerified && action != "publish" {
		p.afterPublish++
		over = p.afterPublish > 2
	}
	st.mu.Unlock()
	if over {
		return fmt.Errorf("refused: already_published — action=publish already published and verified the post. Do not look for more (no probes, scripts, screenshots or fetches); give the final answer now with exactly what that publish result said (its link, or its post id, or the list page and title). This refusal is expected, not a problem: do not mention it to the user")
	}
	if err := desktopManualImageCheck(action, args, request, published); err != nil {
		return err
	}
	if published && action == "bash" && !desktopAskedForFileRe.MatchString(request) && desktopUserDirWriteRe.MatchString(desktopArg(args, "command")) {
		return fmt.Errorf("refused: unasked_file — the result is already published and the request did not ask for a file; do not save a copy. Give the final answer with the link now")
	}
	if published && action == "file_write" && !desktopAskedForFileRe.MatchString(request) && !desktopScratchPathRe.MatchString(desktopArg(args, "path")) {
		return fmt.Errorf("refused: unasked_file — the result is already published and the request did not ask for a file; do not save a copy. Give the final answer with the link now")
	}
	if !inBrowser && desktopBrowserTaskRe.MatchString(request) {
		inBrowser = true // the request is a web task even before the first page
	}
	if !inBrowser || strings.TrimSpace(request) == "" {
		return nil
	}
	switch action {
	case "app_open":
		prog := strings.ToLower(desktopArg(args, "program"))
		if desktopWindowIsBrowser(prog) || strings.Contains(prog, "chrom") || desktopAskedForAppRe.MatchString(request) || desktopAskedForFileRe.MatchString(request) {
			return nil
		}
		return fmt.Errorf("refused: off_task_app — this is a browser task and the request did not ask for a desktop app. Do the work in the browser (page scripts, action=upload for files, rich_fill for text); use bash only for data and images")
	case "bash", "file_write":
		text := desktopArg(args, "command") + " " + desktopArg(args, "path")
		if !desktopOfficeFileRe.MatchString(text) || desktopAskedForFileRe.MatchString(request) {
			return nil
		}
		return fmt.Errorf("refused: unasked_file — this is a browser task and the request did not ask for a document; do not make Word/PDF/office files. Put the text on the page directly (rich_fill)")
	}
	return nil
}

// Scratch locations a run may write to without the person asking.
var desktopScratchPathRe = regexp.MustCompile(`^(/tmp/|/var/tmp/|/home/desktop/tasks/|/home/desktop/\.cache/)`)

// A request that names a web destination or a publish/upload is a browser
// task from its first step (XHS-2 opened an image viewer before any page).
var desktopBrowserTaskRe = regexp.MustCompile(`(?i)https?://|www\.|\.(com|cn|org|net|io)\b|网页|网站|浏览器|发到|发布|发表|发一篇|上传|post to|publish`)

// A shell write (redirect, tee, cp/mv) into the person's own folders.
var desktopUserDirWriteRe = regexp.MustCompile(`(>>?|\btee\b|\bcp\b|\bmv\b|open\(|write_text\()[^|;&]*(/home/desktop/(Desktop|Documents|桌面|文档)|~/(Desktop|Documents|桌面|文档))`)
