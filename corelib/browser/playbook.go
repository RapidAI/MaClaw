package browser

import "strings"

// Playbook is injected into the system prompt when LabelBrowser is active.
func Playbook() string {
	return `Browser Use:
- Drive web pages with the merged browser tool only. Do not use computer_* pixel clicks on Chrome/Edge, and do not call screenshot/eval/click_at.
- First browser(action="session_start") or connect. Every later action needs the returned session_id.
- Loop: observe → choose @eN (role+name) → click/type/select/hover/press → observe again. Refs go stale after every action.
- Prefer ref over CSS. If text matches several controls, observe again and click the specific @eN; do not guess.
- Hover before clicking menu items. Press Enter/Escape/Tab for dialogs and comboboxes. Use dialog accept/dismiss for native JS alerts.
- Submit/publish clicks need expect=url_contains:… or text:…. Example: click Submit with expect=url_contains:/success. Links and tabs do not need expect. If missing_expect repeats on the same control, add expect once and observe; never use computer_*.
- If page flags include captcha_widget, stop and ask the user to solve it in the browser. After they continue, observe before any click/type. If the ask context has resume_task_id, continue the paused task_run with that id, then observe. login_wall and MFA/OTP are not automatic stops — type credentials or the verification code.
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
