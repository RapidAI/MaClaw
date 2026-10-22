package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

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
	desktopURLRe            = regexp.MustCompile(`https?://[^\s;)'"]+`)
	desktopBodyCountRe      = regexp.MustCompile(`editor body has (\d+) chars|字数[:：]\s*(\d+)`)
	desktopArticleURLRe     = regexp.MustCompile(`zhuanlan\.zhihu\.com/p/\d+`)
	desktopPublishAttemptRe = regexp.MustCompile(`发布成功|zhuanlan\.zhihu\.com/write|clicked\s*(发布|更新|发布中|确认发布)`)
	desktopPublishedRe      = regexp.MustCompile(`发布成功|已发布|zhuanlan\.zhihu\.com/p/\d+(?:[?#\s;]|$)`)
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
	// publishTried: this run pressed 发布/更新 (or saw 发布成功). An article
	// URL seen before that is a page left open by an earlier task.
	publishTried bool
	staleWarned  bool
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

	if err == nil && desktopPublishAttemptRe.MatchString(out) {
		p.publishTried = true
	}
	if err == nil && !p.publishTried && !p.staleWarned && desktopArticleURLRe.MatchString(out) && !strings.Contains(out, "/edit") {
		p.staleWarned = true
		notes = append(notes, "That zhuanlan.zhihu.com/p/ article was already open before this task pressed 发布: it is an earlier article, not this task's result. Do not report it. If this task asks to publish, open https://zhuanlan.zhihu.com/write and make a new article.")
	}
	if err == nil && p.publishTried && !p.publishSeen && desktopPublishedRe.MatchString(out) && !strings.Contains(out, "/edit") {
		p.publishSeen = true
		notes = append(notes, "The article appears published. Verify once (open the article URL and check title and body length), then stop operating the desktop and report the URL. Do not keep editing the published article.")
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
	switch {
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
	res := "rich_fill: " + strings.Join(parts, "; ")
	if strings.Contains(out, desktopNotAppliedMarker) {
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
	if !hit {
		return
	}
	st, p := desktopProgressFor(scope)
	if st == nil {
		return
	}
	st.mu.Lock()
	p.publishTried = true
	st.mu.Unlock()
}
