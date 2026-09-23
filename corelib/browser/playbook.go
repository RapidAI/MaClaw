package browser

import "strings"

// Playbook is injected into the system prompt when LabelBrowser is active.
func Playbook() string {
	return `Browser Use:
- Drive web pages with the merged browser tool only. Do not use computer_* pixel clicks on Chrome/Edge, and do not call screenshot/eval/click_at.
- First browser(action="session_start") or connect. Every later action needs the returned session_id.
- Same-page multi-step: browser(action="probe") once, then one browser(action="task_run", steps=[...]) using those @eN refs and snapshot_id. Probe includes child frames. Steps may click, type, select, hover, or press. task_run performs them in one call and returns one observation at the end. It stops early on navigate, submit, dialog, a URL change, a same-URL view change, or an unchanged submit and returns status stopped; probe again before repeating a submit.
- One control only: click, type, or press still works, and each one observes. Refs go stale after the page changes.
- Prefer ref over CSS. If text matches several controls, probe again and click the specific @eN; do not guess.
- Hover before clicking menu items. Press Enter/Escape/Tab for dialogs and comboboxes. Use dialog accept/dismiss for native JS alerts.
- A lone submit/publish click needs expect=url_contains:… or text:…. Example: click Submit with expect=url_contains:/success. Inside task_run, put that submit last; the batch stops and returns the page, so expect is not required there. Links and tabs do not need expect. If missing_expect repeats on a lone click, add expect once and probe; never use computer_*.
- If page flags include captcha_widget, stop and ask the user to solve it in the browser. After they continue, probe before any click/type. If the ask context has resume_task_id, continue the paused task_run with that id, then probe. login_wall and MFA/OTP are not automatic stops — type credentials or the verification code.
- Persistent mode keeps login/cookies. Isolated is clean debug only.`
}

func ParseExpect(raw string) ExpectSpec {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ExpectSpec{}
	}
	typeName, pattern, ok := strings.Cut(raw, ":")
	if !ok {
		return ExpectSpec{Type: strings.ToLower(raw)}
	}
	return ExpectSpec{Type: strings.ToLower(strings.TrimSpace(typeName)), Pattern: strings.TrimSpace(pattern)}
}
