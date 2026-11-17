package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/chatcard"
)

func TestChatCardResumeDoesNotReplaySideEffect(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	cards, err := chatcard.Open(t.TempDir() + "/cards.db")
	if err != nil {
		t.Fatal(err)
	}
	defer cards.Close()
	svc.SetChatCards(cards)
	now := time.Now().UTC()
	run := Run{
		ID: "run-side", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		SessionID: "sess-side", Status: RunStatusSucceeded, StartedAt: now, CompletedAt: &now,
		Metadata: map[string]string{"side_effect": "invoice-1", "bot_id": "bot-a"},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	card, err := cards.Put(chatcard.OpenInput{
		TenantID: principal.TenantID, UserID: principal.UserID, BotID: "bot-a", TaskID: "task-side", RunID: run.ID,
		Kind: chatcard.KindConfirm, Question: "继续这一步？",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cards.Submit(chatcard.Actor{Kind: chatcard.ActorUser, TenantID: principal.TenantID, UserID: principal.UserID, Authenticated: true}, chatcard.Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{chatcard.ChoiceApprove}, OperationVersion: card.Operation.Version,
		PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
	})
	if err != nil || !out.Resume {
		t.Fatalf("submit = %#v %v", out, err)
	}
	got, err := svc.DeliverChatCardResume(context.Background(), principal, inst.ID, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusSucceeded || got.Metadata["side_effect"] != "invoice-1" || got.Metadata["chat_card_resume_id"] != out.ResumeID {
		t.Fatalf("delivered run = %#v", got)
	}
	again, err := svc.DeliverChatCardResume(context.Background(), principal, inst.ID, out)
	if err != nil {
		t.Fatal(err)
	}
	if again.Metadata["side_effect"] != "invoice-1" || again.Status != RunStatusSucceeded {
		t.Fatalf("second delivery = %#v", again)
	}
	queued, err := cards.QueuedResumes()
	if err != nil || len(queued) != 0 {
		t.Fatalf("queue = %#v %v", queued, err)
	}
}
