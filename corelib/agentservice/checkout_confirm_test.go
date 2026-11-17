package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestUserMessageDoesNotRecordCheckoutConfirmation(t *testing.T) {
	p := Principal{TenantID: "t", UserID: "u-checkout"}
	owner := memoryOwnerIDForPrincipal(p)
	browser.ClearCheckoutConfirmation(owner)
	if _, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatal("a user started with a payment authorization")
	}
	amt, ok := browser.RecordCheckoutConfirmation(owner, "确认支付 ¥99")
	if !ok || amt != "99.00" {
		t.Fatalf("explicit one-operation record = %q %v", amt, ok)
	}
	if got, ok := browser.PendingCheckoutConfirmation(owner); !ok || got != "99.00" {
		t.Fatalf("pending = %q %v", got, ok)
	}
	browser.ClearCheckoutConfirmation(owner)
	if _, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatal("cleared confirmation was still pending")
	}
}
