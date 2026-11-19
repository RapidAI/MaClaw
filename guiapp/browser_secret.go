package guiapp

import (
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// presentBrowserSubmitSecret opens the secret card. The model names the field
// and the method. The person types the secret on the card. This call never
// reads a secret out of the model arguments.
func presentBrowserSubmitSecret(args map[string]interface{}) string {
	sessionID := strings.TrimSpace(stringArgForMergedBrowser(args, "session_id"))
	if sessionID == "" {
		return "browser action submit_secret requires session_id. First call browser(action=\"session_start\")."
	}
	sess, err := browser.GetAgentSession(sessionID)
	if err != nil {
		return "browser submit_secret failed: " + err.Error()
	}
	state := sess.State()
	card := browser.SecretCard{
		Method:         stringArgForMergedBrowser(args, "method"),
		Digits:         intArgForMergedBrowser(args, "digits"),
		Submission:     stringArgForMergedBrowser(args, "submission"),
		Ref:            stringArgForMergedBrowser(args, "ref"),
		Selector:       stringArgForMergedBrowser(args, "selector"),
		SubmitRef:      stringArgForMergedBrowser(args, "submit_ref"),
		SubmitSelector: stringArgForMergedBrowser(args, "submit_selector"),
		SnapshotID:     stringArgForMergedBrowser(args, "snapshot_id"),
		PageURL:        state.CurrentURL,
	}
	if card.SnapshotID == "" {
		card.SnapshotID = state.LastSnapshotID
	}
	msg, err := sess.OfferSecretCard(card)
	if err != nil {
		return "browser submit_secret failed: " + err.Error()
	}
	return msg
}

// presentBrowserTakeover hands the preview to the person for a captcha, QR
// sign-in, passkey, or any step the agent cannot finish. It does not collect
// a secret.
func presentBrowserTakeover(args map[string]interface{}) string {
	sessionID := strings.TrimSpace(stringArgForMergedBrowser(args, "session_id"))
	if sessionID == "" {
		return "browser action request_takeover requires session_id. First call browser(action=\"session_start\")."
	}
	sess, err := browser.GetAgentSession(sessionID)
	if err != nil {
		return "browser request_takeover failed: " + err.Error()
	}
	msg, err := sess.RequestTakeover(stringArgForMergedBrowser(args, "reason"))
	if err != nil {
		return "browser request_takeover failed: " + err.Error()
	}
	return msg
}

func intArgForMergedBrowser(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}
