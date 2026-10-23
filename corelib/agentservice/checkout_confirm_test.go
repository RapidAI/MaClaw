package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestUserMessageRecordsCheckoutConfirmation(t *testing.T) {
	p := Principal{TenantID: "t", UserID: "u-checkout"}
	owner := memoryOwnerIDForPrincipal(p)
	browser.ClearCheckoutConfirmation(owner)
	recordUserCheckoutConfirmation(ExecuteRequest{Principal: p, Message: Message{Content: "帮我把购物车结算一下"}})
	if _, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatal("ordinary request must not confirm a payment")
	}
	recordUserCheckoutConfirmation(ExecuteRequest{Principal: p, Message: Message{Content: "确认支付 ¥99"}})
	if amt, ok := browser.PendingCheckoutConfirmation(owner); !ok || amt != "99.00" {
		t.Fatalf("got %q %v", amt, ok)
	}
	browser.ClearCheckoutConfirmation(owner)
}
