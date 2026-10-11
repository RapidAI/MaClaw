package guiapp

import (
	"fmt"
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

func semanticTrustedBrowserDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": semanticTrustedBrowserAdapter,
			// The actions are named here because the enum alone does not say
			// which of them needs a url, and a model that guesses "open" from
			// an unlabelled string wastes the turn on a rejection (2026-10-10).
			"description": "Perform one host-observed browser action: navigate to url, or snapshot the current page. Cookies and login state cannot be injected.",
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
			"action": map[string]interface{}{"type": "string", "enum": tool.BrowserControlActions()},
			"url":    map[string]interface{}{"type": "string"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func semanticTrustedBrowserArgsAllowed(args map[string]interface{}) (action, url string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("trusted_browser_arguments_rejected")
	}
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("trusted_browser_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		case "url":
			url = value
		default:
			return "", "", fmt.Errorf("trusted_browser_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	url = strings.TrimSpace(url)
	if !hasAction {
		return "", "", fmt.Errorf("trusted_browser_action_required")
	}
	// Re-checked against the enum the model was shown, because a boundary that
	// only exists in a rendered document is not a boundary. The set is the
	// shared browser contract, not a host copy: two copies of "which action
	// names are allowed" would drift, and drift here is the incident returning.
	if !tool.BrowserControlActionAllowed(action) {
		return "", "", fmt.Errorf("trusted_browser_action_rejected (%s)", tool.BrowserControlActionHint())
	}
	if tool.BrowserControlNeedsURL(action) && url == "" {
		return "", "", fmt.Errorf("trusted_browser_url_required")
	}
	if strings.Contains(strings.ToLower(url), "cookie") || strings.Contains(action, "cookie") {
		return "", "", fmt.Errorf("trusted_browser_cookie_rejected")
	}
	return action, url, nil
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
		if tool.BrowserControlLeavesAnEffect(action) {
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
	if h == nil {
		return "", fmt.Errorf("trusted_browser_session_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_browser_principal_required")
	}
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
	if !sess.IsTargetAlive() {
		return "", fmt.Errorf("trusted_browser_session_disconnected")
	}
	switch action {
	case "navigate":
		result, err := sess.Navigate(url)
		if err != nil {
			// The aliveness check above is the opposite fact -- nothing was
			// ever sent -- and keeps its own name for that reason.
			if lost := trustedBrowserLostSessionError(action, err); lost != nil {
				return "", lost
			}
			return "", err
		}
		if result == nil {
			return "", fmt.Errorf("trusted_browser_empty")
		}
		// Every refusal reachable here is decided before the request leaves --
		// the browser package only raises one from its pre-navigation policy
		// check -- so there is no effect to be uncertain about and this is a
		// definite failure rather than an unobserved outcome.
		if trustedBrowserActionRefused(result) {
			return "", fmt.Errorf("trusted_browser_action_refused")
		}
		text := strings.TrimSpace(result.Display)
		if text == "" {
			text = strings.TrimSpace(result.Detail)
		}
		if text == "" {
			return "", fmt.Errorf("trusted_browser_empty")
		}
		return text, nil
	case "snapshot":
		obs, err := sess.Observe(false)
		if err != nil {
			if lost := trustedBrowserLostSessionError(action, err); lost != nil {
				return "", lost
			}
			return "", err
		}
		if obs == nil {
			return "", fmt.Errorf("trusted_browser_empty")
		}
		text := strings.TrimSpace(obs.Display)
		if text == "" {
			return "", fmt.Errorf("trusted_browser_empty")
		}
		return text, nil
	default:
		// Unreachable while ArgsAllowed gates the same set, but if the two
		// ever drift the message must still tell the model the real names.
		return "", fmt.Errorf("trusted_browser_action_rejected (%s)", tool.BrowserControlActionHint())
	}
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
