package agentservice

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// recordUserCheckoutConfirmation lets the user's own message, and nothing
// the model writes, unlock one order-submit or payment click. The browser
// guard binds it to the amount visible on the page.
func recordUserCheckoutConfirmation(req ExecuteRequest) {
	text := strings.TrimSpace(req.Message.Content)
	if text == "" {
		return
	}
	browser.RecordCheckoutConfirmation(memoryOwnerIDForPrincipal(req.Principal), text)
}
