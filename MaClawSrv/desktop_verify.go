package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// Verify-after-act (MACLAW_DESKTOP_VERIFY, default on). After a browser type,
// a page script that writes literal text, or desktop steps that type/paste
// text, the target is read back once. When the text is not there the result
// carries desktopNotAppliedMarker and advice; reading problems are ignored.

const desktopVerifyEnv = "MACLAW_DESKTOP_VERIFY"

const desktopNotAppliedMarker = "未生效 (not applied)"

// desktopAppliedMarker follows a result whose text was read back in place.
const desktopAppliedMarker = "(read back: the text is in the field)"

func desktopVerifyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopVerifyEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// desktopWithReadback appends the not-applied advice to a successful result.
func desktopWithReadback(kind, text string, err error, verdict string) (string, error) {
	if err == nil && verdict == browser.ReadbackApplied {
		return strings.TrimRight(text, "\n") + "\n" + desktopAppliedMarker, nil
	}
	if err != nil || verdict != browser.ReadbackNotApplied {
		return text, err
	}
	log.Printf("[desktop-verify] %s not applied", kind)
	return strings.TrimRight(text, "\n") + "\n\n" + browser.NotAppliedAdvice, nil
}

// desktopVerifyBrowserType reads back the field a browser type wrote into.
func desktopVerifyBrowserType(session *browser.BrowserAgentSession, typed, text string, err error) (string, error) {
	if err != nil || !desktopVerifyEnabled() || session == nil {
		return text, err
	}
	return desktopWithReadback("type", text, err, session.ReadBack(typed, false))
}

// desktopVerifyBrowserScript reads back the literal text a script wrote.
func desktopVerifyBrowserScript(session *browser.BrowserAgentSession, script, text string, err error) (string, error) {
	if err != nil || !desktopVerifyEnabled() || session == nil {
		return text, err
	}
	written := browser.ScriptWrittenText(script)
	if written == "" {
		return text, err
	}
	return desktopWithReadback("script", text, err, session.ReadBack(written, true))
}

// desktopStepsInsertedText is the text of the last type/paste/set_text step.
func desktopStepsInsertedText(raw any) string {
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return ""
	}
	last := ""
	for _, step := range steps {
		switch step.Kind {
		case "type", "paste", "set_text":
			if strings.TrimSpace(step.Text) != "" {
				last = step.Text
			}
		}
	}
	return last
}

// desktopFocusedA11yFields asks the supervisor for the focused fields' text
// (AT-SPI). ok is false when the image cannot tell. Several fields mean
// several active windows.
func desktopFocusedA11yFields(ctx context.Context, scope agentruntime.Scope, display string) ([]string, bool) {
	out, err := desktopCtlCall(ctx, scope, display, map[string]any{"op": desktop.CtlA11yProbe, "focused_text": true})
	if err != nil {
		return nil, false
	}
	if raw, ok := out["focused_texts"].([]any); ok {
		fields := make([]string, 0, len(raw))
		for _, item := range raw {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text != "" {
				fields = append(fields, text)
			}
		}
		return fields, true
	}
	text, ok := out["focused_text"].(string)
	if !ok {
		return nil, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, true
	}
	return []string{text}, true
}

// desktopA11yFieldsVerdict is applied when every field shows the text, and
// not_applied when none do. A mix is unknown: the hit may be a different
// window from the one that received the keystrokes.
func desktopA11yFieldsVerdict(inserted string, fields []string) string {
	if len(fields) == 0 {
		return browser.ReadbackUnknown
	}
	hits := 0
	for _, field := range fields {
		if browser.ReadbackVerdict(inserted, []string{field}) == browser.ReadbackApplied {
			hits++
		}
	}
	switch {
	case hits == 0:
		return browser.ReadbackNotApplied
	case hits == len(fields):
		return browser.ReadbackApplied
	default:
		return browser.ReadbackUnknown
	}
}

// desktopReadbackVerdict joins the two readers. The page is authoritative
// when it holds the keyboard focus, because that is where the keystrokes
// went. The accessibility fields are used when the page cannot be read.
func desktopReadbackVerdict(inserted string, fields []string, page func(string) string) string {
	if page != nil {
		if got := page(inserted); got != "" && got != browser.ReadbackUnknown {
			return got
		}
	}
	return desktopA11yFieldsVerdict(inserted, fields)
}

// desktopVerifySteps reads back desktop-level typing. The focused page
// decides when it holds the keyboard focus. Accessibility fields are used
// when that page cannot be read.
func desktopVerifySteps(ctx context.Context, scope agentruntime.Scope, display string, raw any, session func() *browser.BrowserAgentSession, text string, err error) (string, error) {
	if err != nil || !desktopVerifyEnabled() {
		return text, err
	}
	inserted := desktopStepsInsertedText(raw)
	if browser.ReadbackProbe(inserted) == "" {
		return text, err
	}
	// The page answers in one eval when it has focus. The accessibility walk
	// runs only when that eval cannot tell, so a browser field does not wait
	// on the desktop tree.
	var page func(string) string
	pageVerdict := browser.ReadbackUnknown
	if session != nil {
		if s := session(); s != nil {
			pageVerdict = s.ReadBackIfFocused(inserted)
			page = func(string) string { return pageVerdict }
		}
	}
	var fields []string
	if page == nil || pageVerdict == "" || pageVerdict == browser.ReadbackUnknown {
		fields, _ = desktopFocusedA11yFields(ctx, scope, display)
	}
	return desktopWithReadback("app_run", text, err, desktopReadbackVerdict(inserted, fields, page))
}
