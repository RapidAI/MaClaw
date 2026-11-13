package main

import (
	"context"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopTimeoutURL reads the page a timed-out step is sitting on.
// Tests replace it. Production uses a one-second read.
var desktopTimeoutURL = func(scope agentruntime.Scope) string {
	session := desktopLiveSession(scope)
	if session == nil {
		return ""
	}
	return session.LocationQuick()
}

// desktopNativeDialogTitles lists window titles that may be a browser
// dialog. Tests replace it. An empty list means no dialog was seen.
var desktopNativeDialogTitles = func(context.Context, agentruntime.Scope) []string {
	return nil
}

// desktopNativeDialogKind reports a Chromium dialog that is not a web page:
// an external-protocol prompt or a permission prompt. Shop names are not part
// of the match.
func desktopNativeDialogKind(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return ""
	}
	switch {
	case strings.Contains(t, "xdg-open") || strings.Contains(t, "external protocol") || strings.Contains(t, "open external") || strings.Contains(title, "外部协议") || strings.Contains(title, "打开外部应用"):
		return "external_protocol"
	case strings.Contains(t, "wants to") || strings.Contains(t, "permission") || strings.Contains(title, "想要访问") || strings.Contains(title, "权限"):
		return "permission"
	}
	return ""
}

// desktopBlockingDialog reports a native browser dialog that is already in
// front of the page. The step returns this instead of waiting on the page.
func desktopBlockingDialog(scope agentruntime.Scope) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, title := range desktopNativeDialogTitles(ctx, scope) {
		kind := desktopNativeDialogKind(title)
		if kind == "" {
			continue
		}
		return "a browser dialog is in front of the page (" + kind + ": " + strings.TrimSpace(title) + "). It was dismissed. Stay on the web page; do not wait for it and do not open an external application.", true
	}
	return "", false
}

// desktopTimeoutHandoff is what a hard timeout should say instead of "the
// page is slow". ok is false when neither a login wall nor a native dialog
// was seen, and the caller keeps the slow-page path.
func desktopTimeoutHandoff(scope agentruntime.Scope, action string) (text string, ask bool, ok bool) {
	if action != "navigate" && action != "probe" && action != "screenshot" && action != "click" {
		return "", false, false
	}
	if text, blocked := desktopBlockingDialog(scope); blocked {
		return text, false, true
	}
	raw := strings.TrimSpace(desktopTimeoutURL(scope))
	if raw != "" && browser.LoginWallURL(raw) != "" {
		return "page flags: login_wall\nurl " + raw, true, true
	}
	return "", false, false
}
