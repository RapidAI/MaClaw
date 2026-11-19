package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// presentBrowserConfirmOrder shows the goods, address, and amount the model
// already read. The person confirms on the card. This call does not click pay
// and ignores any approve flag the model adds.
func presentBrowserConfirmOrder(args map[string]interface{}) string {
	sessionID := strings.TrimSpace(stringArgForMergedBrowser(args, "session_id"))
	if sessionID == "" {
		return "browser action confirm_order requires session_id. First call browser(action=\"session_start\")."
	}
	sess, err := browser.GetAgentSession(sessionID)
	if err != nil {
		return "browser confirm_order failed: " + err.Error()
	}
	card := sess.AlignCheckoutCard(browser.CheckoutCard{
		Item:    stringArgForMergedBrowser(args, "item"),
		Shop:    stringArgForMergedBrowser(args, "shop"),
		Address: stringArgForMergedBrowser(args, "address"),
		Amount:  stringArgForMergedBrowser(args, "amount"),
		Detail:  stringArgForMergedBrowser(args, "detail"),
	})
	amount, err := sess.OfferCheckoutCard(card)
	if err != nil {
		if !browser.GUIEmbedEnabled() {
			return browser.MsgCheckoutBlocked
		}
		return "browser confirm_order failed: " + err.Error()
	}
	return "用户已在确认卡上确认支付 ¥" + amount + "。先 probe，核对页面金额仍是这个数，再点击一次支付。不要自己再写确认。"
}
