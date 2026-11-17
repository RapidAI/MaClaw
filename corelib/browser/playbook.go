package browser

import "strings"

// Playbook is injected into the system prompt when LabelBrowser is active.
func Playbook() string {
	return `Browser Use:
- Drive web pages with the merged browser tool only. Do not use computer_* pixel clicks on Chrome/Edge, and do not call screenshot/eval/click_at.
- First browser(action="session_start") or connect. Every later action needs the returned session_id.
- Same-page batch: browser(action="probe") once, then one browser(action="script"). Example: await click("保存"); await type("#q", "weather"). The script may click, type, select, press, scroll, and waitFor every control that probe listed. Optional name, such as save-draft, stores it; skill_list and skill_run repeat that script on the same site without another probe. Do not read cookies, storage, or passwords, and do not type into a password or card field.
- Ref sequence: browser(action="task_run", steps=[...]) using those @eN refs and snapshot_id. Probe includes child frames. Steps may click, type, select, hover, or press. task_run performs them in one call and returns one observation at the end. It stops early on navigate, submit, dialog, a URL change, a same-URL view change, or an unchanged submit and returns status stopped; probe again before repeating a submit.
- One control only: click, type, or press still works, and each one observes. Refs go stale after the page changes.
- Prefer ref over CSS. If text matches several controls, probe again and click the specific @eN; do not guess.
- Hover before clicking menu items. Press Enter/Escape/Tab for dialogs and comboboxes. Use dialog accept/dismiss for native JS alerts.
- A lone submit/publish click needs expect=url_contains:… or text:…. Example: click Submit with expect=url_contains:/success. Inside task_run, put that submit last; the batch stops and returns the page, so expect is not required there. Links and tabs do not need expect. If missing_expect repeats on a lone click, add expect once and probe; never use computer_*.
- If page flags include captcha_widget, or the page is a QR sign-in or a passkey, call browser(action="request_takeover"). The preview takes over. Do not ask for the code in chat. After the person finishes, probe before any click/type. If the ask context has resume_task_id, continue the paused task_run with that id, then probe.
- A password, email/SMS code, or authenticator code uses browser(action="submit_secret", method, ref). method is password, otp, or authenticator. ref is the field from probe. submission is fill, enter, or click; click also needs submit_ref. Never pass the secret. The person types it on the card and the host fills the field. Do not type it and do not ask for it in chat. A sign-in page is not a takeover by itself.
- Before a pay or place-order click, call browser(action="confirm_order", item, shop, address, amount) with the goods, address, and payable total you already read. Wait for that card. Do not click pay until it says the person confirmed, and do not write the confirmation yourself.
- Pressing Enter or Space on a pay or place-order control, or Enter in a field on the order page, submits the order the same way. Do not do that until the card says the person confirmed.
- The managed page is shown in the MaClaw preview. Captcha, QR sign-in, and passkeys are solved there by request_takeover. Passwords and one-time codes are solved by submit_secret. After the person hands control back, probe before the next click or type.
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
