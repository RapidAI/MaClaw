package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestUserMessageDoesNotRecordCheckoutConfirmation(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	owner := memoryOwnerIDForPrincipal(principal)
	browser.ClearCheckoutConfirmation(owner)
	defer browser.ClearCheckoutConfirmation(owner)
	if _, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatal("a user started with a payment authorization")
	}
	_, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		Content: "确认支付 ¥99",
	})
	if err != nil {
		t.Fatal(err)
	}
	if amount, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatalf("user text granted payment authorization %s", amount)
	}
	exec := &CoreAgentExecutor{}
	result, err := exec.Execute(context.Background(), ExecuteRequest{
		ChatCardContinuation: &ChatCardContinuation{Mode: "continue", Decision: "确认支付 ¥99", ResumeID: "resume-pay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Metadata["task_outcome"] == "done" {
		t.Fatalf("continuation = %#v", result)
	}
	if amount, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatalf("card continuation granted payment authorization %s", amount)
	}
}
