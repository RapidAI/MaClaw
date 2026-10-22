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

func desktopVerifyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopVerifyEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// desktopWithReadback appends the not-applied advice to a successful result.
func desktopWithReadback(kind, text string, err error, verdict string) (string, error) {
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

// desktopFocusedA11yText asks the supervisor for the focused element's text
// (AT-SPI). ok is false when the image cannot tell.
func desktopFocusedA11yText(ctx context.Context, scope agentruntime.Scope, display string) (string, bool) {
	out, err := desktopCtlCall(ctx, scope, display, map[string]any{"op": desktop.CtlA11yProbe, "focused_text": true})
	if err != nil {
		return "", false
	}
	text, ok := out["focused_text"].(string)
	if !ok {
		return "", false
	}
	return text, true
}

// desktopVerifySteps reads back desktop-level typing: the focused AT-SPI
// element first, then the browser page when it holds the focus.
func desktopVerifySteps(ctx context.Context, scope agentruntime.Scope, display string, raw any, session func() *browser.BrowserAgentSession, text string, err error) (string, error) {
	if err != nil || !desktopVerifyEnabled() {
		return text, err
	}
	inserted := desktopStepsInsertedText(raw)
	if browser.ReadbackProbe(inserted) == "" {
		return text, err
	}
	verdict := browser.ReadbackUnknown
	if focused, ok := desktopFocusedA11yText(ctx, scope, display); ok && strings.TrimSpace(focused) != "" {
		verdict = browser.ReadbackVerdict(inserted, []string{focused})
	}
	if verdict == browser.ReadbackUnknown && session != nil {
		if s := session(); s != nil {
			verdict = s.ReadBackIfFocused(inserted)
		}
	}
	return desktopWithReadback("app_run", text, err, verdict)
}
