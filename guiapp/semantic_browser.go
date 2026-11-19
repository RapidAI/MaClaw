package guiapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedBrowserAdapter        = "semantic_control_trusted_browser"
	semanticTrustedBrowserImplementation = "trusted-browser-control-v1"
)

func semanticUnpublishedLegacyBrowserProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityBrowserControlWeb {
			return true
		}
	}
	return false
}

func semanticTrustedBrowserPublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedBrowser != nil || trustedBrowserServable(h))
}

// trustedBrowserServable reports whether this host can serve a browser-control
// turn at all. The ladder is live session → discoverable CDP runtime →
// installed Chrome/Edge that the managed launcher could start. The first two
// answer "is a runtime already alive"; the third answers "could the host make
// one alive on demand". Treating "not yet alive" as infeasible was the
// 2026-10-10 production incident: after an app restart, before any browser
// session had ever been opened, a confident browser turn planned
// browser.control.web, found no provider, and HostRejected the whole turn as
// no_feasible_provider even though the host owns the launcher that would have
// created the runtime. Only a host with no browser installed — where a launch
// attempt could never succeed — stays unservable, because that is the one
// state where publication would still end in the same refusal.
func trustedBrowserServable(h *IMMessageHandler) bool {
	if h == nil || h.app == nil {
		return false
	}
	if h.trustedBrowserServableOverride != nil {
		return h.trustedBrowserServableOverride()
	}
	if trustedBrowserRuntimeAlive() {
		return true
	}
	return browser.DetectBrowserAvailable()
}

// trustedBrowserRuntimeAlive reports a browser runtime the host could attach
// to right now: an agent session with a live target, or any discoverable CDP
// endpoint. It is the "already alive" prefix of trustedBrowserServable.
func trustedBrowserRuntimeAlive() bool {
	for _, sess := range browser.ListAgentSessions() {
		if sess != nil && sess.IsTargetAlive() {
			return true
		}
	}
	_, err := browser.DiscoverCDPAddr()
	return err == nil
}

func trustedBrowserBoundSession(principalID string) *browser.BrowserAgentSession {
	principalID = strings.TrimSpace(principalID)
	var fallback *browser.BrowserAgentSession
	for _, sess := range browser.ListAgentSessions() {
		if sess == nil || !sess.IsTargetAlive() {
			continue
		}
		owner := strings.TrimSpace(sess.State().OwnerID)
		if owner == principalID {
			return sess
		}
		if owner == "" && fallback == nil {
			fallback = sess
		}
	}
	return fallback
}

// semanticBrowserActions is the GUI managed browser vocabulary. It includes
// the shared navigate/snapshot floor and the page actions the preview can
// finish: probe, click, type, press, scroll, plus the OpenBot handoffs
// submit_secret, request_takeover, and confirm_order. The Hub host browser
// keeps the shared floor only; this list is what the desktop model is shown.
func semanticBrowserActions() []string {
	return []string{
		"navigate", "snapshot", "probe", "click", "type", "press", "scroll",
		"submit_secret", "request_takeover", "confirm_order",
	}
}

func semanticBrowserActionAllowed(action string) bool {
	for _, allowed := range semanticBrowserActions() {
		if action == allowed {
			return true
		}
	}
	return false
}

func semanticBrowserActionHint() string {
	return "allowed: " + strings.Join(semanticBrowserActions(), ", ")
}

// semanticBrowserActionLeavesAnEffect reports whether a lost answer might
// mean the page already changed. A snapshot or probe leaves nothing behind.
func semanticBrowserActionLeavesAnEffect(action string) bool {
	switch action {
	case "snapshot", "probe":
		return false
	default:
		return true
	}
}

func semanticTrustedBrowserDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": semanticTrustedBrowserAdapter,
			// The actions are named here because the enum alone does not say
			// which of them needs a url, and a model that guesses "open" from
			// an unlabelled string wastes the turn on a rejection (2026-10-10).
			"description": "Drive the MaClaw preview browser. navigate opens a url. snapshot and probe read the page. click, type, press, and scroll operate it. submit_secret collects a password or one-time code on the card; do not pass the secret. request_takeover hands over a captcha, QR sign-in, or passkey. confirm_order shows the payable total before pay. Cookies and login state cannot be injected. The host binds the session.",
			"parameters":  semanticTrustedBrowserInvocationSchema(),
		},
	}
}

func semanticTrustedBrowserInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			// The enum is what stops the model inventing an action name. As a
			// bare string it had nothing to go on and guessed -- "open" was the
			// guess that became trusted_browser_action_rejected on 2026-10-10 --
			// and no rejection code ever told it which name would have worked.
			// The canonicalizer does not read enum, so this is documentation for
			// the model and the check in ArgsAllowed stays the boundary.
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        semanticBrowserActions(),
				"description": "navigate opens url. snapshot reads the page. probe lists interactive refs. click and type use ref from probe; type the account name yourself. press sends key (Enter, Tab, Escape). scroll uses delta_y. submit_secret: method is password, otp, or authenticator, ref is the field, submission is fill, enter, or click (click also needs submit_ref). Never pass the secret. request_takeover: reason is captcha, qr, or passkey. confirm_order: item, shop, address, amount are the payable total the person reviews.",
			},
			"url":        map[string]interface{}{"type": "string", "description": "Page URL for navigate."},
			"ref":        map[string]interface{}{"type": "string", "description": "Element ref from probe. submit_secret points it at the password or code field."},
			"text":       map[string]interface{}{"type": "string", "description": "Visible text for click, or the account name for type. Never a password or one-time code."},
			"key":        map[string]interface{}{"type": "string", "description": "press: Enter, Tab, or Escape."},
			"method":     map[string]interface{}{"type": "string", "description": "submit_secret: password, otp, or authenticator. Never the secret itself."},
			"digits":     map[string]interface{}{"type": "integer", "description": "submit_secret: code length from 4 to 12. Omit it for a password."},
			"submission": map[string]interface{}{"type": "string", "description": "submit_secret: fill, enter, or click."},
			"submit_ref": map[string]interface{}{"type": "string", "description": "submit_secret: the login or continue button when submission is click."},
			"reason":     map[string]interface{}{"type": "string", "description": "request_takeover: captcha, qr, or passkey."},
			"item":       map[string]interface{}{"type": "string", "description": "confirm_order: goods the person should review."},
			"shop":       map[string]interface{}{"type": "string", "description": "confirm_order: shop name."},
			"address":    map[string]interface{}{"type": "string", "description": "confirm_order: ship-to line already on the page. Not a host path."},
			"amount":     map[string]interface{}{"type": "string", "description": "confirm_order: payable total including shipping, such as 9.98."},
			"detail":     map[string]interface{}{"type": "string", "description": "confirm_order: short non-secret note, such as shipping time."},
			"query":      map[string]interface{}{"type": "string", "description": "probe: optional filter on name, text, or role."},
			"delta_y":    map[string]interface{}{"type": "integer", "description": "scroll: vertical delta. Positive scrolls down."},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func semanticTrustedBrowserArgsAllowed(args map[string]interface{}) (map[string]interface{}, error) {
	if args == nil {
		return nil, fmt.Errorf("trusted_browser_action_required")
	}
	cleaned := make(map[string]interface{}, len(args))
	hasAction := false
	for key, raw := range args {
		switch key {
		case "action", "url", "ref", "text", "key", "method", "submission", "submit_ref", "reason", "item", "shop", "address", "amount", "detail", "query":
			value, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("trusted_browser_arguments_rejected")
			}
			value = strings.TrimSpace(value)
			if key == "action" {
				value = strings.ToLower(value)
				hasAction = true
			}
			cleaned[key] = value
		case "digits", "delta_y":
			n, ok := semanticBrowserInt(raw)
			if !ok {
				return nil, fmt.Errorf("trusted_browser_arguments_rejected")
			}
			cleaned[key] = n
		case "password", "secret", "code", "pin", "cookie", "cookies", "headers", "session_id", "selector", "approve", "confirmation", "otp":
			// The value is the thing the card exists to keep off this call.
			return nil, fmt.Errorf("trusted_browser_secret_rejected")
		default:
			return nil, fmt.Errorf("trusted_browser_arguments_rejected")
		}
	}
	action, _ := cleaned["action"].(string)
	url, _ := cleaned["url"].(string)
	if !hasAction {
		return nil, fmt.Errorf("trusted_browser_action_required")
	}
	if action == "" || !semanticBrowserActionAllowed(action) {
		return nil, fmt.Errorf("trusted_browser_action_rejected (%s)", semanticBrowserActionHint())
	}
	if action == "navigate" && url == "" {
		return nil, fmt.Errorf("trusted_browser_url_required")
	}
	if action == "press" && semanticBrowserField(cleaned, "key") == "" {
		return nil, fmt.Errorf("trusted_browser_key_required")
	}
	if action == "type" && semanticBrowserField(cleaned, "text") == "" {
		return nil, fmt.Errorf("trusted_browser_text_required")
	}
	if action == "click" && semanticBrowserField(cleaned, "ref") == "" && semanticBrowserField(cleaned, "text") == "" {
		return nil, fmt.Errorf("trusted_browser_target_required")
	}
	if action == "submit_secret" && semanticBrowserField(cleaned, "method") == "" {
		return nil, fmt.Errorf("trusted_browser_secret_method_required")
	}
	if action == "submit_secret" && semanticBrowserField(cleaned, "ref") == "" {
		return nil, fmt.Errorf("trusted_browser_secret_ref_required")
	}
	if strings.Contains(strings.ToLower(url), "cookie") || strings.Contains(action, "cookie") {
		return nil, fmt.Errorf("trusted_browser_cookie_rejected")
	}
	return cleaned, nil
}

func semanticBrowserField(cleaned map[string]interface{}, key string) string {
	value, _ := cleaned[key].(string)
	return value
}

func semanticBrowserInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// trustedBrowserLostSessionError names a session that vanished while carrying
// an action, and returns nil for anything that is not a lost session.
//
// The choice between the two names is made here, once, because it depends on
// something only this layer knows: whether the action could have left an
// effect behind. Deciding it at each call site is how the two facts drifted
// into sharing a name in the first place.
func trustedBrowserLostSessionError(action string, err error) error {
	if err == nil {
		return nil
	}
	// Navigating issues a request that may consume a one-time URL or move
	// server state, so a lost answer leaves an effect that might already hold
	// and must not be reported as a definite failure. Observing leaves nothing
	// behind: there the lost answer is the whole loss, and claiming
	// uncertainty would only discourage a harmless retry.
	//
	// Whether the request left at all is the browser package's fact, not a
	// property of the message: the command that timed out on the wire and the
	// connection that was already gone beforehand read almost alike.
	// Which action can leave an effect behind is the contract's fact, not this
	// layer's: a host that named it separately would keep answering for the old
	// action set the moment a new one is added.
	if browser.IsOutcomeUnobserved(err) {
		if semanticBrowserActionLeavesAnEffect(action) {
			return fmt.Errorf("trusted_browser_outcome_unobserved")
		}
		return fmt.Errorf("trusted_browser_session_disconnected")
	}
	// What remains are the pre-dispatch refusals: the target was already gone
	// when the action was picked up, so nothing was sent and there is no
	// effect to be uncertain about, whichever action it was.
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "gone") && !strings.Contains(lower, "disconnect") {
		return nil
	}
	return fmt.Errorf("trusted_browser_session_disconnected")
}

// trustedBrowserActionRefused reports whether a browser action came back
// refused rather than performed.
//
// A refusal arrives as a nil error carrying a readable Display, so a caller
// that only checks the error hands that refusal text to the model as the
// contents of a page it never reached.
//
// Which statuses mean "performed" is the browser package's own vocabulary, so
// it answers that question. This wrapper exists only to keep the call site
// reading as a question about refusal.
func trustedBrowserActionRefused(result *browser.BrowserActionResult) bool {
	return !browser.BrowserActionExecuted(result)
}

// ensureTrustedBrowserSession bootstraps a browser session for a turn that
// has none. The ladder is the same one the publication gate trusts: attach to
// the user's already-running debug-enabled Chrome first (nothing new is
// started), then fall back to the managed persistent runtime — the same
// default the browser panel's session_start uses, whose durable profile keeps
// the login state flows like account registration need. Only a host where
// both fail returns unavailable; navigate and snapshot both route through
// here because a snapshot without a session observes nothing.
func (h *IMMessageHandler) ensureTrustedBrowserSession(principalID string) (*browser.BrowserAgentSession, error) {
	if h == nil || h.app == nil {
		return nil, fmt.Errorf("trusted_browser_session_unavailable")
	}
	if h.trustedBrowserLauncher != nil {
		started, err := h.trustedBrowserLauncher(principalID)
		if err != nil || started == nil {
			return nil, fmt.Errorf("trusted_browser_session_unavailable")
		}
		return started, nil
	}
	// SessionModeConnectUser resolves its own ConnectUserChrome probe and
	// never launches a browser, so a failure here means "no attachable user
	// runtime" and the managed persistent launch is the one remaining path.
	started, err := browser.StartAgentSessionForOwner(principalID, "", browser.BrowserPolicy{}, true, browser.SessionModeConnectUser)
	if err == nil && started != nil {
		return started, nil
	}
	started, err = browser.StartAgentSessionForOwner(principalID, "", browser.BrowserPolicy{}, true, browser.SessionModePersistent)
	if err != nil || started == nil {
		return nil, fmt.Errorf("trusted_browser_session_unavailable")
	}
	return started, nil
}

func (h *IMMessageHandler) controlTrustedBrowser(principalID, action, url string) (string, error) {
	args := map[string]interface{}{"action": action}
	if strings.TrimSpace(url) != "" {
		args["url"] = url
	}
	return h.performTrustedBrowser(principalID, args)
}

func (h *IMMessageHandler) performTrustedBrowser(principalID string, args map[string]interface{}) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_browser_session_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_browser_principal_required")
	}
	action := semanticBrowserField(args, "action")
	url := semanticBrowserField(args, "url")
	if h.semanticTrustedBrowser != nil {
		return h.semanticTrustedBrowser(principalID, action, url)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_browser_session_unavailable")
	}
	sess := trustedBrowserBoundSession(principalID)
	if sess == nil {
		started, err := h.ensureTrustedBrowserSession(principalID)
		if err != nil {
			return "", err
		}
		sess = started
	}
	if sess == nil || !sess.IsTargetAlive() {
		return "", fmt.Errorf("trusted_browser_session_disconnected")
	}
	switch action {
	case "navigate":
		result, err := sess.Navigate(url)
		return finishTrustedBrowserAction(action, result, err)
	case "snapshot":
		obs, err := sess.Observe(false)
		return finishTrustedBrowserObservation(action, obs, err)
	case "probe":
		obs, err := sess.ProbeForModel(semanticBrowserField(args, "query"))
		return finishTrustedBrowserObservation(action, obs, err)
	case "click":
		snap := sess.State().LastSnapshotID
		if ref := semanticBrowserField(args, "ref"); ref != "" {
			result, err := sess.Click(snap, ref, "")
			return finishTrustedBrowserAction(action, result, err)
		}
		result, err := sess.ClickText(snap, semanticBrowserField(args, "text"))
		return finishTrustedBrowserAction(action, result, err)
	case "type":
		snap := sess.State().LastSnapshotID
		result, err := sess.Type(snap, semanticBrowserField(args, "ref"), "", semanticBrowserField(args, "text"))
		return finishTrustedBrowserAction(action, result, err)
	case "press":
		result, err := sess.Press(semanticBrowserField(args, "key"))
		return finishTrustedBrowserAction(action, result, err)
	case "scroll":
		delta, _ := args["delta_y"].(int)
		if delta == 0 {
			delta = 480
		}
		result, err := sess.ScrollBy(sess.State().LastSnapshotID, semanticBrowserField(args, "ref"), "", 0, delta)
		return finishTrustedBrowserAction(action, result, err)
	case "submit_secret":
		state := sess.State()
		card := browser.SecretCard{
			Method:     semanticBrowserField(args, "method"),
			Digits:     semanticBrowserFieldInt(args, "digits"),
			Submission: semanticBrowserField(args, "submission"),
			Ref:        semanticBrowserField(args, "ref"),
			SubmitRef:  semanticBrowserField(args, "submit_ref"),
			SnapshotID: state.LastSnapshotID,
			PageURL:    state.CurrentURL,
		}
		msg, err := sess.OfferSecretCard(card)
		if err != nil {
			return "", err
		}
		return msg, nil
	case "request_takeover":
		msg, err := sess.RequestTakeover(semanticBrowserField(args, "reason"))
		if err != nil {
			return "", err
		}
		return msg, nil
	case "confirm_order":
		card := sess.AlignCheckoutCard(browser.CheckoutCard{
			Item:    semanticBrowserField(args, "item"),
			Shop:    semanticBrowserField(args, "shop"),
			Address: semanticBrowserField(args, "address"),
			Amount:  semanticBrowserField(args, "amount"),
			Detail:  semanticBrowserField(args, "detail"),
		})
		amount, err := sess.OfferCheckoutCard(card)
		if err != nil {
			if !browser.GUIEmbedEnabled() {
				return "", fmt.Errorf("%s", browser.MsgCheckoutBlocked)
			}
			return "", err
		}
		return "用户已在确认卡上确认支付 ¥" + amount + "。先 probe，核对页面金额仍是这个数，再点击一次支付。不要自己再写确认。", nil
	default:
		return "", fmt.Errorf("trusted_browser_action_rejected (%s)", semanticBrowserActionHint())
	}
}

func semanticBrowserFieldInt(args map[string]interface{}, key string) int {
	n, _ := args[key].(int)
	return n
}

func finishTrustedBrowserAction(action string, result *browser.BrowserActionResult, err error) (string, error) {
	if err != nil {
		if lost := trustedBrowserLostSessionError(action, err); lost != nil {
			return "", lost
		}
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("trusted_browser_empty")
	}
	// A refusal decided before the request leaves has no effect to be
	// uncertain about. The display is the instruction (confirm_order,
	// request_takeover); dropping it is how a pay click becomes a bare
	// code the model cannot follow.
	if trustedBrowserActionRefused(result) {
		display := strings.TrimSpace(result.Display)
		if display == "" {
			return "", fmt.Errorf("trusted_browser_action_refused")
		}
		return "", fmt.Errorf("trusted_browser_action_refused: %s", display)
	}
	text := strings.TrimSpace(result.Display)
	if text == "" {
		text = strings.TrimSpace(result.Detail)
	}
	if text == "" {
		return "", fmt.Errorf("trusted_browser_empty")
	}
	if !trustedBrowserResultAlreadyProbes(text) {
		text += "\n先 probe，再继续。"
	}
	return text, nil
}

func trustedBrowserResultAlreadyProbes(text string) bool {
	return strings.Contains(text, "先 probe") || strings.Contains(text, `action="probe"`) || strings.Contains(text, "action=probe")
}

func finishTrustedBrowserObservation(action string, obs *browser.BrowserObservation, err error) (string, error) {
	if err != nil {
		if lost := trustedBrowserLostSessionError(action, err); lost != nil {
			return "", lost
		}
		return "", err
	}
	if obs == nil {
		return "", fmt.Errorf("trusted_browser_empty")
	}
	text := trustedBrowserObservationText(obs)
	if text == "" {
		return "", fmt.Errorf("trusted_browser_empty")
	}
	return text, nil
}

// trustedBrowserObservationText is what the model reads after probe or
// snapshot. Display alone is a one-line count, so click, type, and
// submit_secret would have no ref to name. Selectors, session ids, and
// password or code values stay off this text.
func trustedBrowserObservationText(obs *browser.BrowserObservation) string {
	if obs == nil {
		return ""
	}
	var b strings.Builder
	if display := strings.TrimSpace(obs.Display); display != "" {
		b.WriteString(display)
	}
	if lines := trustedBrowserRefLines(obs.Snapshot.Refs); lines != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lines)
	}
	if excerpt := trustedBrowserPageExcerpt(obs.Snapshot.PageTextExcerpt); excerpt != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("page_text:\n")
		b.WriteString(excerpt)
	}
	hint := trustedBrowserPageHint(obs.Snapshot.PageFlags)
	if hint == "" && trustedBrowserHasSecretField(obs.Snapshot.Refs) {
		hint = "密码或验证码框用 browser(action=\"submit_secret\", method, ref)。不要把密码放进参数。"
	}
	if hint != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(hint)
	}
	return b.String()
}

func trustedBrowserRefLines(refs []browser.BrowserElementRef) string {
	ranked, truncated := browser.ModelRefs(refs)
	if len(ranked) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("refs:\n")
	for _, ref := range ranked {
		line := trustedBrowserRefLine(ref)
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString("refs truncated; probe again with query\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// modelExcerptLimit sits above the probe pack. That pack keeps a 1600-character
// prefix plus payable and address windows. Clipping it again at 1200 drops
// the ship-to line the pack just kept.
const modelExcerptLimit = 4800

// trustedBrowserPageExcerpt keeps the leading page and the payable total
// payableAmount reads. A payable line past the prefix is appended. A page
// with no payable line keeps that prefix, including an early 合计.
func trustedBrowserPageExcerpt(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	const limit = modelExcerptLimit
	if len(runes) <= limit {
		return text
	}
	keyAt := trustedBrowserPayableRune(text)
	if keyAt < 0 || keyAt+80 <= limit {
		return string(runes[:limit])
	}
	start := keyAt - 40
	if start < 0 {
		start = 0
	}
	end := keyAt + 160
	if end > len(runes) {
		end = len(runes)
	}
	win := runes[start:end]
	budget := limit - len(win) - 1
	if budget < 1 || len(win) >= limit {
		if len(win) > limit {
			win = win[:limit]
		}
		return string(win)
	}
	if start <= budget && end <= limit {
		return string(runes[:limit])
	}
	return string(runes[:budget]) + "…" + string(win)
}

func trustedBrowserPayableRune(text string) int {
	byteAt := browser.PayableMarkerIndex(text)
	if byteAt < 0 {
		return -1
	}
	return len([]rune(text[:byteAt]))
}

func trustedBrowserRefLine(ref browser.BrowserElementRef) string {
	id := strings.TrimSpace(ref.Ref)
	if id == "" {
		return ""
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		name = strings.TrimSpace(ref.Text)
	}
	name = clipTrustedBrowserRunes(name, 80)
	parts := []string{id}
	if role := strings.TrimSpace(ref.Role); role != "" {
		parts = append(parts, role)
	}
	// The preview keeps a commit prefix ahead of the page. The rank bonus
	// is not visible in the line, so the line itself names the reason.
	if browser.RefIsPayControl(ref) {
		parts = append(parts, "commit=pay")
	}
	if browser.RefIsSecretControl(ref) {
		parts = append(parts, "commit=secret")
	}
	if input := strings.ToLower(strings.TrimSpace(ref.InputType)); input != "" {
		parts = append(parts, "input="+input)
	}
	if name != "" {
		parts = append(parts, strconv.Quote(name))
	}
	if value := trustedBrowserRefValue(ref); value != "" {
		parts = append(parts, "value="+strconv.Quote(value))
	}
	if ref.Disabled {
		parts = append(parts, "disabled")
	}
	return strings.Join(parts, " ")
}

func trustedBrowserRefValue(ref browser.BrowserElementRef) string {
	input := strings.ToLower(strings.TrimSpace(ref.InputType))
	if input == "password" || input == "hidden" {
		return ""
	}
	value := strings.TrimSpace(ref.Value)
	if value == "" || trustedBrowserLooksLikeCode(value) {
		return ""
	}
	return clipTrustedBrowserRunes(value, 40)
}

func trustedBrowserLooksLikeCode(value string) bool {
	n := len([]rune(value))
	if n < 4 || n > 12 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func trustedBrowserHasSecretField(refs []browser.BrowserElementRef) bool {
	for _, ref := range refs {
		if strings.EqualFold(strings.TrimSpace(ref.InputType), "password") {
			return true
		}
		role := strings.ToLower(strings.TrimSpace(ref.Role))
		tag := strings.ToLower(strings.TrimSpace(ref.Tag))
		if role == "button" || tag == "button" || tag == "a" {
			continue
		}
		name := strings.ToLower(ref.Name + " " + ref.Text)
		if strings.Contains(name, "密码") || strings.Contains(name, "验证码") || strings.Contains(name, "password") || strings.Contains(name, "passcode") {
			return true
		}
	}
	return false
}

func trustedBrowserPageHint(flags browser.BrowserPageFlags) string {
	switch {
	case flags.CaptchaWidget || flags.Captcha || flags.LoginQR:
		return "调用 browser(action=\"request_takeover\")。不要在对话里索要验证码。"
	case flags.PaymentConfirm:
		return "付款前调用 browser(action=\"confirm_order\", item, shop, address, amount)。不要自己点击支付。"
	case flags.LoginWall || flags.MFA:
		return "账号名称用 type。密码或验证码用 submit_secret，method 和 ref 来自上面的列表，不要把密码放进参数。"
	default:
		return ""
	}
}

func clipTrustedBrowserRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func semanticTrustedBrowserResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_browser_delivery_token")
	}
	if strings.Contains(strings.ToLower(text), "set-cookie") {
		return "", fmt.Errorf("trusted_browser_cookie_rejected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_browser_empty")
	}
	return text, nil
}
