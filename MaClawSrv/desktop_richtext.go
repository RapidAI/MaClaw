package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// Rich-text editor rules (seen live 10-22 on zhuanlan.zhihu.com: page
// scripts "filled" title and body, every step said ok, the editor showed
// 字数 0, and the next steps went on to topics and the publish button).
//
//  1. A page script that writes text into a contenteditable body is refused
//     (rich_text_dom_write): Draft.js / ProseMirror editors drop DOM writes.
//  2. After a paste flow (clipboard_set of a long text, then a script, app_run
//     or computer step that pastes) the editor body length is read back; far
//     below the clipboard length is 未生效 and feeds the failure-class guard.
//  3. A page script that throws is a failed step, not ok.
//  4. Clicking a publish / 发布 / 发送 control is refused unless the person
//     pre-approved publishing in their message; the bot asks the person to
//     take over and press it (MACLAW_DESKTOP_PUBLISH_CONFIRM=off disables).

const desktopRichBodyMinClip = 100

var (
	desktopRichTargetRe = regexp.MustCompile(`(?i)contenteditable|DraftEditor|public-DraftEditor|ProseMirror|ql-editor|notranslate|w-e-text|ne-viewer|editor-body|RichText`)
	desktopRichWriteRe  = regexp.MustCompile(`(?i)(innerHTML|innerText|textContent|outerHTML)\s*(\+?=[^=]|\+=)|insertAdjacent|appendChild|\.append\(|createTextNode|ClipboardEvent|DataTransfer|dispatchEvent|\btype\s*\(|\bfill\s*\(|setText|\.value\s*=`)
	desktopPasteRe      = regexp.MustCompile(`(?i)paste|ctrl\+v|control\+v|ctrl-v|clipboard`)
	desktopPublishRe    = regexp.MustCompile(`(?i)^\s*(发布|发表|立即发布|确认发布|发布文章|发布笔记|发布动态|发布回答|发送|确认发送|提交|更新|发布更新|确认更新|立即更新|保存并发布|publish|publish now|update|post|submit|send)\s*$`)
	desktopScriptLitRe  = regexp.MustCompile("[\"'`]([^\"'`\\n]{1,20})[\"'`]")
	desktopPreapproveRe = regexp.MustCompile(`(?i)直接发布|直接发表|直接发出|无需确认|不用确认|不需要确认|不必确认|自动发布|发布前不用问|publish (it )?directly|without (asking|confirmation)|no need to confirm`)
)

// desktopScriptWritesRichBody reports a page script that writes into a rich
// editor body (the forbidden method).
func desktopScriptWritesRichBody(script string) bool {
	return desktopRichTargetRe.MatchString(script) && desktopRichWriteRe.MatchString(script)
}

func desktopIsPublishLabel(label string) bool {
	label = strings.TrimSpace(label)
	return label != "" && utf8.RuneCountInString(label) <= 12 && desktopPublishRe.MatchString(label)
}

// desktopScriptClicksPublish reports a page script that clicks a publish
// control by its label.
func desktopScriptClicksPublish(script string) bool {
	if !strings.Contains(strings.ToLower(script), "click") {
		return false
	}
	for _, m := range desktopScriptLitRe.FindAllStringSubmatch(script, -1) {
		if desktopIsPublishLabel(m[1]) {
			return true
		}
	}
	return false
}

type desktopPublishApprovedKey struct{}

func desktopPublishConfirmEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_PUBLISH_CONFIRM"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// desktopPublishPreapproved reads the turn message for an explicit
// "publish without asking".
func desktopPublishPreapproved(request agentruntime.TurnRequest) bool {
	text := ""
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		text = input.Message.Content
	case *agentservice.ExecuteRequest:
		if input != nil {
			text = input.Message.Content
		}
	}
	return desktopPreapproveRe.MatchString(text)
}

func desktopPublishAllowed(ctx context.Context) bool {
	if !desktopPublishConfirmEnabled() {
		return true
	}
	ok, _ := ctx.Value(desktopPublishApprovedKey{}).(bool)
	return ok
}

const desktopPublishRefusal = "refused: publish_needs_confirmation — publishing is public and the person has not approved it in this task. Do not click 发布/发送 yourself and do not try another way to press it. First make sure title and body are really in the editor (probe the body length), then call action=request_takeover with reason \"请检查内容并亲自点发布\" and end your turn."

const desktopRichWriteRefusal = "refused: rich_text_dom_write — this is a rich-text editor (Draft.js/ProseMirror style); script/DOM writes into its body are dropped even when the script says ok. Use real input: click the body by its ref (action=click), action=clipboard_set with the text, then app_run steps [{\"key\":\"ctrl+v\"}] — or the editor's 导入 → 导入文档 with the .docx. Then probe the body length."

// desktopRichPreCheck refuses forbidden browser steps before they run.
func desktopRichPreCheck(ctx context.Context, session *browser.BrowserAgentSession, action string, args map[string]any) error {
	switch action {
	case "script":
		script := desktopArg(args, "script")
		if desktopScriptWritesRichBody(script) {
			log.Printf("[desktop-guard] refused rich_text_dom_write")
			return fmt.Errorf("%s", desktopRichWriteRefusal)
		}
		if desktopScriptClicksPublish(script) || desktopScriptSubmits(script) {
			if !desktopPublishAllowed(ctx) {
				log.Printf("[desktop-guard] refused publish_needs_confirmation (script)")
				return fmt.Errorf("%s", desktopPublishRefusal)
			}
			return desktopPublishBodyCheck(session)
		}
	case "click":
		label := desktopArg(args, "text")
		if ref := desktopArg(args, "ref"); ref != "" && session != nil {
			if el, err := session.ResolveRef(desktopArg(args, "snapshot_id"), ref); err == nil && el != nil {
				label = el.Name
				if strings.TrimSpace(label) == "" {
					label = el.Text
				}
			}
		}
		if desktopIsPublishLabel(label) && desktopPublishAllowed(ctx) {
			return desktopPublishBodyCheck(session)
		}
		if desktopIsPublishLabel(label) {
			log.Printf("[desktop-guard] refused publish_needs_confirmation (click %q)", label)
			return fmt.Errorf("%s", desktopPublishRefusal)
		}
	case "press":
		if desktopPublishKeyRe.MatchString(desktopArg(args, "key")) && !desktopPublishAllowed(ctx) {
			return fmt.Errorf("%s", desktopPublishRefusal)
		}
	}
	return nil
}

// desktopNoteClipboard remembers the length of the last clipboard_set text.
func desktopNoteClipboard(owner agentruntime.Scope, args map[string]any) {
	if st := desktopRunFor(owner); st != nil {
		n := utf8.RuneCountInString(strings.Join(strings.Fields(desktopRawArg(args, "text")), ""))
		st.mu.Lock()
		st.lastClipLen = n
		st.mu.Unlock()
	}
}

func desktopLastClipLen(owner agentruntime.Scope) int {
	if st := desktopRunFor(owner); st != nil {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.lastClipLen
	}
	return 0
}

// desktopStepPastes reports a step that pastes (script, app_run, computer).
func desktopStepPastes(action string, args map[string]any) bool {
	switch action {
	case "script":
		return desktopPasteRe.MatchString(desktopArg(args, "script"))
	case "app_run", "computer", "press":
		raw, _ := json.Marshal(args)
		return desktopPasteRe.MatchString(string(raw))
	}
	return false
}

// desktopVerifyRichBody reads the editor body after a paste flow.
func desktopVerifyRichBody(owner agentruntime.Scope, session *browser.BrowserAgentSession, action string, args map[string]any, text string, err error) (string, error) {
	if err != nil || !desktopVerifyEnabled() || session == nil || !desktopStepPastes(action, args) {
		return text, err
	}
	want := desktopLastClipLen(owner)
	if want < desktopRichBodyMinClip {
		return text, err
	}
	body, ok := session.RichBodyLen()
	if !ok {
		return text, err
	}
	got := body.Len()
	floor := want * 3 / 10
	if floor < 20 {
		floor = 20
	}
	if got >= floor {
		return strings.TrimRight(text, "\n") + fmt.Sprintf("\n(read back: editor body has %d chars of %d pasted)", got, want), err
	}
	log.Printf("[desktop-verify] rich body not applied got=%d want=%d action=%s", got, want, action)
	return strings.TrimRight(text, "\n") + fmt.Sprintf("\n\n%s Editor body has %d chars but %d were on the clipboard.", browser.NotAppliedAdvice, got, want), err
}

// desktopExperienceOnFirstVisit returns the site guidance the first time a
// run acts on site (not only on navigate), else "".
func desktopExperienceOnFirstVisit(scope agentruntime.Scope, site string) string {
	st := desktopRunFor(desktopOwner(scope))
	if st == nil || site == "" {
		return ""
	}
	st.mu.Lock()
	if st.expSites == nil {
		st.expSites = map[string]bool{}
	}
	seen := st.expSites[site]
	st.expSites[site] = true
	st.mu.Unlock()
	if seen {
		return ""
	}
	return desktopExperienceText(scope, site)
}

var (
	desktopPublishKeyRe  = regexp.MustCompile(`(?i)(ctrl|control|cmd|command|meta|super)\s*[+-]\s*(enter|return|kp_enter)`)
	desktopBashAutomRe   = regexp.MustCompile(`(?i)xdotool|:9222|devtools|Runtime\.evaluate|playwright|puppeteer|selenium|chrome-remote|cdp|websocat`)
	desktopBashPublishRe = regexp.MustCompile(`(?i)发布|发表|更新|publish|submit`)
)

// desktopPublishSideCheck refuses the other ways to press publish: keyboard
// submit shortcuts and shell-driven browser automation. Runs before any
// desktop action.
func desktopPublishSideCheck(ctx context.Context, action string, args map[string]any) error {
	if desktopPublishAllowed(ctx) {
		return nil
	}
	switch action {
	case "app_run", "computer", "press", "task_run":
		raw, _ := json.Marshal(args)
		if desktopPublishKeyRe.Match(raw) {
			log.Printf("[desktop-guard] refused publish_needs_confirmation (submit shortcut via %s)", action)
			return fmt.Errorf("%s (Ctrl/Cmd+Enter submits the form.)", desktopPublishRefusal)
		}
	case "bash":
		cmd := desktopArg(args, "command")
		if desktopBashAutomRe.MatchString(cmd) && desktopBashPublishRe.MatchString(cmd) {
			log.Printf("[desktop-guard] refused publish_needs_confirmation (bash automation)")
			return fmt.Errorf("%s (Driving the browser from the shell to publish is the same click.)", desktopPublishRefusal)
		}
	}
	return nil
}

const desktopPublishEmptyRefusal = "refused: publish_body_empty — the editor body is empty or only repeats the title, so publishing would post a blank/broken article. Put the real body in first (click the body, clipboard_set, app_run key ctrl+v), check 字数, then hand over with request_takeover."

// desktopPublishBodyCheck refuses publishing a body that is empty or ≈ title,
// even when publishing was pre-approved.
func desktopPublishBodyCheck(session *browser.BrowserAgentSession) error {
	if session == nil {
		return nil
	}
	b, ok := session.RichBodyLen()
	if !ok {
		return nil
	}
	if desktopBodyLooksEmpty(b) {
		log.Printf("[desktop-guard] refused publish_body_empty len=%d", b.Len())
		return fmt.Errorf("%s", desktopPublishEmptyRefusal)
	}
	return nil
}

func desktopBodyLooksEmpty(b browser.RichBody) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	body, title := norm(b.Body), norm(b.Title)
	if b.Len() < 20 && utf8.RuneCountInString(body) < 20 {
		return true
	}
	if title != "" && utf8.RuneCountInString(body) <= utf8.RuneCountInString(title)+10 {
		short := utf8.RuneCountInString(title)
		if n := utf8.RuneCountInString(body); n < short {
			short = n
		}
		if short > 0 && desktopLongestCommon(body, title)*10 >= short*8 {
			return true
		}
	}
	return false
}

func desktopLongestCommon(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	best := 0
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best = cur[j]
				}
			}
		}
		prev = cur
	}
	return best
}

// desktopClickHitsPublish OCRs the screen and reports a coordinate click
// step that lands on a publish label.
func desktopClickHitsPublish(ctx context.Context, scope agentruntime.Scope, display string, raw any) (string, bool) {
	if desktopPublishAllowed(ctx) || desktopOCRLines == nil {
		return "", false
	}
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return "", false
	}
	var pts [][2]float64
	for _, st := range steps {
		if (st.Kind == "click" || st.Kind == "mouse_down") && st.HasXY {
			pts = append(pts, [2]float64{st.X, st.Y})
		}
	}
	if len(pts) == 0 {
		return "", false
	}
	frame := desktopFrameFor(scope)
	_, opts := frame.get()
	shot, err := desktopGroundShot(ctx, scope, display, opts)
	if err != nil || len(shot.Data) == 0 {
		return "", false
	}
	scale := shot.Scale
	if scale <= 0 {
		scale = 1
	}
	lines := desktopOCRLines(ctx, shot.Data)
	for _, p := range pts {
		for _, q := range [][2]float64{p, {p[0] * scale, p[1] * scale}} {
			for _, l := range lines {
				b := l.BBox
				if desktopIsPublishLabel(l.Text) && q[0] >= float64(b[0]-6) && q[0] <= float64(b[0]+b[2]+6) && q[1] >= float64(b[1]-6) && q[1] <= float64(b[1]+b[3]+6) {
					return l.Text, true
				}
			}
		}
	}
	return "", false
}

var desktopScriptSubmitRe = regexp.MustCompile(`(?i)\.submit\s*\(|requestSubmit|(ctrl|control|meta)\s*[+-]?\s*(enter|return)|ctrlKey\s*:\s*true[^}]*Enter|Enter[^}]*ctrlKey\s*:\s*true`)

// desktopScriptSubmits reports a script that submits a form or sends the
// submit shortcut.
func desktopScriptSubmits(script string) bool { return desktopScriptSubmitRe.MatchString(script) }
