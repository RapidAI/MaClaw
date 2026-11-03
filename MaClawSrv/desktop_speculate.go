package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Speculative execution (d78): safe prechecks that stay quiet when nothing
// useful turns up. Never publish, pay, delete, submit, or take any other
// irreversible action. Results that matter are written into the desktop
// ledger (outside the container) so a later turn can use them; empty
// outcomes produce no user-facing chatter.

const (
	desktopSpeculateCheckLogin   = "check_login"
	desktopSpeculateCheckPosted  = "check_posted"
	desktopSpeculatePrepareDraft = "prepare_draft"
	desktopSpeculateSnapshotPage = "snapshot_page"
)

var desktopSpeculateSafe = map[string]bool{
	desktopSpeculateCheckLogin:   true,
	desktopSpeculateCheckPosted:  true,
	desktopSpeculatePrepareDraft: true,
	desktopSpeculateSnapshotPage: true,
}

// desktopSpeculateForbidden is refused even if the model invents a synonym.
var desktopSpeculateForbidden = []string{
	"publish", "post", "submit", "pay", "payment", "buy", "checkout",
	"delete", "remove", "destroy", "send", "share", "transfer",
	"click_publish", "confirm_pay", "order",
}

var desktopSpeculateOnce sync.Map // run id -> done

func desktopSpeculateCheckName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// desktopSpeculateAllowed reports whether check is a safe speculative kind.
func desktopSpeculateAllowed(check string) bool {
	check = desktopSpeculateCheckName(check)
	if check == "" {
		return false
	}
	// Allowlist first so safe names like check_posted are never blocked by
	// a substring of a forbidden verb ("post").
	if desktopSpeculateSafe[check] {
		return true
	}
	for _, bad := range desktopSpeculateForbidden {
		if check == bad || strings.HasPrefix(check, bad+"_") || strings.HasSuffix(check, "_"+bad) || strings.Contains(check, "_"+bad+"_") {
			return false
		}
	}
	return false
}

// desktopSpeculateRefuse explains why a speculative check was blocked.
func desktopSpeculateRefuse(check string) string {
	check = desktopSpeculateCheckName(check)
	if check == "" {
		return "speculate refused: check required (check_login, check_posted, prepare_draft, snapshot_page)"
	}
	if desktopSpeculateSafe[check] {
		return ""
	}
	for _, bad := range desktopSpeculateForbidden {
		if check == bad || strings.HasPrefix(check, bad+"_") || strings.HasSuffix(check, "_"+bad) || strings.Contains(check, "_"+bad+"_") {
			return "speculate refused: " + check + " is irreversible; only check_login, check_posted, prepare_draft, snapshot_page are allowed"
		}
	}
	return "speculate refused: unknown check " + check + "; only check_login, check_posted, prepare_draft, snapshot_page are allowed"
}

// desktopSpeculateQuiet runs one safe precheck. An empty string means
// nothing useful (caller stays silent — no Say, no notify_main).
func desktopSpeculateQuiet(scope agentruntime.Scope, session *browser.BrowserAgentSession, check string, args map[string]any) (string, error) {
	check = desktopSpeculateCheckName(check)
	if !desktopSpeculateAllowed(check) {
		return "", fmt.Errorf("%s", desktopSpeculateRefuse(check))
	}
	switch check {
	case desktopSpeculateCheckLogin:
		return desktopSpeculateLogin(scope, session)
	case desktopSpeculateCheckPosted:
		return desktopSpeculatePosted(scope, session, args)
	case desktopSpeculatePrepareDraft:
		return desktopSpeculateDraft(scope, args)
	case desktopSpeculateSnapshotPage:
		return desktopSpeculateSnapshot(scope, session)
	default:
		return "", fmt.Errorf("%s", desktopSpeculateRefuse(check))
	}
}

func desktopSpeculateLogin(scope agentruntime.Scope, session *browser.BrowserAgentSession) (string, error) {
	if session == nil || !desktopSessionConnected(session) {
		return "", nil // quiet: no page yet
	}
	flags, err := session.PeekPageFlags()
	if err != nil {
		log.Printf("[desktop-speculate] check_login peek: %v", err)
		return "", nil
	}
	host := desktopLedgerHost(session.CurrentURL())
	key := desktopLedgerKey(scope)
	switch {
	case flags.LoginWall || flags.LoginQR:
		detail := "login_needed"
		if flags.LoginQR {
			detail = "login_qr"
		}
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "login_state", Host: host, URL: session.CurrentURL(), Run: scope.RunID, Detail: detail})
		return "speculate: page needs sign-in on " + host + " (" + detail + "); ask for takeover before filling.", nil
	case flags.CaptchaWidget || flags.Captcha:
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "login_state", Host: host, URL: session.CurrentURL(), Run: scope.RunID, Detail: "captcha"})
		return "speculate: captcha on " + host + "; person must clear it.", nil
	case flags.PaymentConfirm:
		// Informative only — never proceed to pay.
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "login_state", Host: host, URL: session.CurrentURL(), Run: scope.RunID, Detail: "payment_wall"})
		return "speculate: payment confirmation on " + host + "; do not pay.", nil
	default:
		// Signed-in or no wall: record once so resume knows, stay mostly quiet.
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "login_state", Host: host, URL: session.CurrentURL(), Run: scope.RunID, Detail: "ok"})
		return "", nil
	}
}

func desktopSpeculatePosted(scope agentruntime.Scope, session *browser.BrowserAgentSession, args map[string]any) (string, error) {
	spec := desktopTaskSpecFor(scope.RunID)
	title := strings.TrimSpace(desktopArg(args, "title"))
	if title == "" {
		title = strings.TrimSpace(desktopArg(args, "body"))
	}
	host := desktopLedgerHost(firstNonEmptyStr(spec.EditorURL, ""))
	if session != nil && desktopSessionConnected(session) {
		if u := session.CurrentURL(); u != "" {
			host = desktopLedgerHost(firstNonEmptyStr(host, u))
		}
	}
	key := desktopLedgerKey(scope)
	entries := desktopLedgerEntries(key)
	now := desktopLedgerNow()
	if e, ok := desktopLedgerRecentPost(entries, host, title, now); ok {
		out := "speculate: already posted 「" + e.Title + "」 on " + e.Host + " at " + e.At.Format("01-02 15:04")
		if e.URL != "" {
			out += " link " + e.URL
		}
		out += " (ledger). Do not publish again."
		return out, nil
	}
	// Live list look only when verify targets and a session exist — still read-only.
	if session == nil || !desktopSessionConnected(session) {
		return "", nil
	}
	ps := browser.PublishSpec{Title: title, EditorURL: spec.EditorURL, VerifyURL: spec.VerifyURL}
	if ps.Title == "" || (ps.VerifyURL == "" && spec.Profile == nil) {
		return "", nil
	}
	if spec.Profile != nil && ps.VerifyURL == "" {
		ps.VerifyURL = spec.Profile.VerifyURL
		ps.VerifyAPI = spec.Profile.VerifyAPI
	}
	if ps.VerifyURL == "" && ps.VerifyAPI == "" {
		return "", nil
	}
	rep, err := browser.FindPosted(session.PublishDriver(), ps)
	found := err == nil && rep != nil && rep.State == browser.PublishLive
	link := ""
	if rep != nil {
		link = rep.ResultURL
	}
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "checked", Host: host, Title: title, URL: link, Run: scope.RunID, Found: found, Detail: "speculate"})
	if !found {
		return "", nil
	}
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "posted", Host: host, Title: title, URL: link, Run: scope.RunID, Detail: "speculate_found"})
	out := "speculate: 「" + title + "」 is already in the account's post list"
	if link != "" {
		out += ", link " + link
	}
	return out + ". Do not publish again.", nil
}

func desktopSpeculateDraft(scope agentruntime.Scope, args map[string]any) (string, error) {
	title := strings.TrimSpace(desktopArg(args, "title"))
	body := strings.TrimSpace(desktopArg(args, "body"))
	if title == "" && body == "" {
		return "", nil
	}
	host := desktopLedgerHost(desktopTaskSpecFor(scope.RunID).EditorURL)
	desktopLedgerRememberProgress(scope, "", title, body, "draft")
	msg := "speculate: draft kept outside the desktop"
	if host != "" {
		msg += " for " + host
	}
	if title != "" {
		msg += " title 「" + title + "」"
	}
	if body != "" {
		msg += " body " + fmt.Sprintf("%d chars", utf8.RuneCountInString(body))
	}
	return msg + ". Resume can refill from the ledger; nothing was published.", nil
}

func desktopSpeculateSnapshot(scope agentruntime.Scope, session *browser.BrowserAgentSession) (string, error) {
	if session == nil || !desktopSessionConnected(session) {
		return "", nil
	}
	url := strings.TrimSpace(session.CurrentURL())
	if url == "" {
		return "", nil
	}
	desktopLedgerRememberProgress(scope, url, "", "", "page_state")
	return "speculate: page URL kept outside the desktop: " + url, nil
}

// desktopSpeculateAutoOnce runs the quiet login + posted prechecks once per
// run when a browser session first appears. Useful notes go to the ledger
// and are returned for the caller to prepend to a tool result; empty means
// stay silent.
func desktopSpeculateAutoOnce(scope agentruntime.Scope, session *browser.BrowserAgentSession) string {
	if scope.RunID == "" || session == nil || !desktopSessionConnected(session) {
		return ""
	}
	if _, loaded := desktopSpeculateOnce.LoadOrStore(scope.RunID, true); loaded {
		return ""
	}
	var parts []string
	if s, err := desktopSpeculateQuiet(scope, session, desktopSpeculateCheckLogin, nil); err == nil && s != "" {
		parts = append(parts, s)
	}
	spec := desktopTaskSpecFor(scope.RunID)
	if spec.EditorURL != "" || spec.VerifyURL != "" {
		args := map[string]any{}
		if s, err := desktopSpeculateQuiet(scope, session, desktopSpeculateCheckPosted, args); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	if s := desktopRestoreFormFields(scope, session); s != "" {
		parts = append(parts, s)
	}
	if u := session.CurrentURL(); u != "" {
		if s, err := desktopSpeculateQuiet(scope, session, desktopSpeculateSnapshotPage, nil); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// operateDesktopSpeculate is action=speculate: safe prechecks only. Empty
// useful output is reported as a quiet "nothing useful" line so the model
// does not invent irreversible follow-ups; the person is never notified.
func operateDesktopSpeculate(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	check := desktopSpeculateCheckName(desktopArg(args, "check"))
	if check == "" {
		check = desktopSpeculateCheckName(desktopArg(args, "kind"))
	}
	if !desktopSpeculateAllowed(check) {
		return "", fmt.Errorf("%s", desktopSpeculateRefuse(check))
	}
	var session *browser.BrowserAgentSession
	// prepare_draft needs no browser; others prefer a live session when one exists.
	if check != desktopSpeculatePrepareDraft {
		if endpoint, err := desktopSession(ctx, scope, ""); err == nil {
			userKey, _ := desktopUserKey(desktopOwner(scope))
			if userKey != "" {
				binding := desktopBindingFor(userKey)
				session, _ = desktopBrowserSession(binding, scope, userKey, endpoint.CDP)
			}
		}
	}
	out, err := desktopSpeculateQuiet(scope, session, check, args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		// Quiet toward the person; the model gets a one-liner so it stops.
		return "speculate: nothing useful", nil
	}
	return out, nil
}

// desktopNoteAppliedProgress persists filled editor/form text outside the
// container when a write was confirmed by read-back.
func desktopNoteAppliedProgress(scope agentruntime.Scope, session *browser.BrowserAgentSession, written, text string, err error) {
	if err != nil || !strings.Contains(text, desktopAppliedMarker) {
		return
	}
	written = strings.TrimSpace(written)
	if utf8.RuneCountInString(written) < 8 {
		return
	}
	url := ""
	if session != nil && desktopSessionConnected(session) {
		url = session.CurrentURL()
	}
	field := ""
	if url != "" {
		field = desktopFocusedFieldLocator(session)
	}
	if field != "" {
		desktopLedgerRememberField(scope, url, written, field)
	} else {
		desktopLedgerRememberProgress(scope, url, "", written, "draft")
	}
	if url != "" {
		desktopLedgerRememberProgress(scope, url, "", "", "page_state")
	}
}
