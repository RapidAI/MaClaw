package agentservice

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
)

type checkpointExec struct {
	mu        sync.Mutex
	executed  int
	continued int
}

func (e *checkpointExec) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	_ = ctx
	if req.ChatCardContinuation != nil {
		return nil, errors.New("a continuation must not replay the original turn")
	}
	e.mu.Lock()
	e.executed++
	e.mu.Unlock()
	return &ExecuteResult{Content: "issued invoice-1", OutputType: "text/plain", Metadata: map[string]string{"side_effect": "invoice-1"}}, nil
}

func (e *checkpointExec) ContinueChatCard(ctx context.Context, req ExecuteRequest, in ChatCardContinuation) (ChatCardContinuationResult, error) {
	_ = ctx
	_ = req
	e.mu.Lock()
	e.continued++
	e.mu.Unlock()
	result, err := continueChatCard(in)
	if err != nil {
		return ChatCardContinuationResult{}, err
	}
	if containsEffect(in.PriorEffects, "follow-up") {
		return result, nil
	}
	result.Effects = []string{"follow-up"}
	return result, nil
}

func TestChatCardResumeDoesNotReplaySideEffect(t *testing.T) {
	exec := &checkpointExec{}
	svc, principal, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()
	page := "当前页面：订单仍未支付"
	svc.SetChatCardObserver(ChatCardObserverFunc(func(context.Context, Principal, string, string) (ChatCardEvidence, error) {
		return ChatCardEvidence{Source: "page", Text: page}, nil
	}))
	cards, err := chatcard.Open(filepath.Join(t.TempDir(), "cards.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cards.Close()
	svc.SetChatCards(cards)
	ctx := context.Background()
	_, run, _, err := svc.SendMessage(ctx, principal, inst.ID, SendMessageInput{Content: "开票", Metadata: map[string]string{"bot_id": "bot-a"}})
	if err != nil || run == nil {
		t.Fatal(err)
	}
	if exec.executed != 1 {
		t.Fatalf("original turn ran %d times", exec.executed)
	}
	if err := svc.store.SaveRun(withEffect(*run, "invoice-1")); err != nil {
		t.Fatal(err)
	}
	card := putHandoffCard(t, cards, principal, "bot-a", run.SessionID, run.ID)
	if err := svc.NoteChatCardCheckpoint(ctx, principal, inst.ID, run.ID, card.ID, "invoice-1"); err != nil {
		t.Fatal(err)
	}
	out := submitHandoff(t, cards, principal, card, inst.ID)
	if _, err := svc.DeliverChatCardResume(ctx, principal, inst.ID, out); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeliverChatCardResume(ctx, principal, inst.ID, out); err != nil {
		t.Fatal(err)
	}
	if exec.executed != 1 || exec.continued != 1 {
		t.Fatalf("execute=%d continue=%d", exec.executed, exec.continued)
	}
	parent, err := svc.GetRun(ctx, principal, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != RunStatusSucceeded || parent.Metadata["side_effect"] != "invoice-1" {
		t.Fatalf("parent = %#v", parent.Metadata)
	}
	cont, err := svc.GetRun(ctx, principal, inst.ID, parent.Metadata["chat_card_continued"])
	if err != nil {
		t.Fatal(err)
	}
	if cont.Metadata["applied_effects"] != "follow-up" || cont.Metadata["task_outcome_evidence"] != page || cont.Metadata["task_outcome"] == "done" {
		t.Fatalf("continuation = %#v", cont.Metadata)
	}
	owner := memoryOwnerIDForPrincipal(principal)
	browser.ClearCheckoutConfirmation(owner)
	if _, ok := browser.PendingCheckoutConfirmation(owner); ok {
		t.Fatal("continuation stored a payment authorization")
	}
}

func TestChatCardRestartDeliversContinuation(t *testing.T) {
	dir := t.TempDir()
	page := "旧页面"
	observer := ChatCardObserverFunc(func(context.Context, Principal, string, string) (ChatCardEvidence, error) {
		return ChatCardEvidence{Source: "page", Text: page}, nil
	})
	ctx := context.Background()
	exec := &checkpointExec{}
	svc, principal, inst, cards := openCheckpointService(t, dir, exec)
	svc.SetChatCardObserver(observer)
	_, run, _, err := svc.SendMessage(ctx, principal, inst.ID, SendMessageInput{Content: "开票", Metadata: map[string]string{"bot_id": "bot-a"}})
	if err != nil || run == nil {
		t.Fatal(err)
	}
	if err := svc.store.SaveRun(withEffect(*run, "invoice-1")); err != nil {
		t.Fatal(err)
	}
	card := putHandoffCard(t, cards, principal, "bot-a", run.SessionID, run.ID)
	if err := svc.NoteChatCardCheckpoint(ctx, principal, inst.ID, run.ID, card.ID, "invoice-1"); err != nil {
		t.Fatal(err)
	}
	out := submitHandoff(t, cards, principal, card, inst.ID)
	page = "当前页面：订单仍未支付"
	svc.chatCardBeforeFinish = func() error { return errors.New("crash before commit") }
	if _, err := svc.DeliverChatCardResume(ctx, principal, inst.ID, out); err == nil {
		t.Fatal("delivery committed through the crash")
	}
	queued, err := cards.QueuedResumes()
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue after crash = %#v %v", queued, err)
	}
	svc.Close()
	cards.Close()

	page = "当前页面：登录框还在"
	exec2 := &checkpointExec{}
	svc2, principal2, inst2, cards2 := openCheckpointService(t, dir, exec2)
	defer svc2.Close()
	defer cards2.Close()
	svc2.SetChatCardObserver(observer)
	if err := svc2.ReconcileChatCardOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if exec2.executed != 0 || exec2.continued != 1 {
		t.Fatalf("restart execute=%d continue=%d", exec2.executed, exec2.continued)
	}
	parent, err := svc2.GetRun(ctx, principal2, inst2.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != RunStatusSucceeded || parent.Metadata["side_effect"] != "invoice-1" {
		t.Fatalf("parent after restart = %#v", parent)
	}
	cont, err := svc2.GetRun(ctx, principal2, inst2.ID, parent.Metadata["chat_card_continued"])
	if err != nil {
		t.Fatal(err)
	}
	if cont.Metadata["task_outcome_evidence"] != "当前页面：登录框还在" || cont.Metadata["applied_effects"] != "follow-up" {
		t.Fatalf("restart continuation = %#v", cont.Metadata)
	}
	if cont.Metadata["task_outcome_evidence"] == "旧页面" || cont.Metadata["task_outcome_evidence"] == "当前页面：订单仍未支付" {
		t.Fatal("continuation kept a stale observation")
	}
	if err := svc2.ReconcileChatCardOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if exec2.continued != 1 || exec2.executed != 0 {
		t.Fatalf("second reconcile execute=%d continue=%d", exec2.executed, exec2.continued)
	}
	left, err := cards2.QueuedResumes()
	if err != nil || len(left) != 0 {
		t.Fatalf("queue = %#v %v", left, err)
	}
}

func openCheckpointService(t *testing.T, dir string, exec Executor) (*Service, Principal, Instance, *chatcard.Store) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(dir, "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{DataRoot: filepath.Join(dir, "data"), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, exec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	tenants, err := svc.ListTenants(ctx, ListTenantsInput{})
	if err != nil {
		t.Fatal(err)
	}
	var principal Principal
	var inst Instance
	if len(tenants) == 0 {
		tenant, err := svc.CreateTenant(ctx, CreateTenantInput{Name: "Tenant"})
		if err != nil {
			t.Fatal(err)
		}
		user, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "User"})
		if err != nil {
			t.Fatal(err)
		}
		principal = Principal{TenantID: tenant.ID, UserID: user.ID}
		cfg := corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}
		if _, err := svc.UpdateUserConfig(ctx, principal, cfg); err != nil {
			t.Fatal(err)
		}
		created, err := svc.CreateInstance(ctx, principal, CreateInstanceInput{Name: "Instance"})
		if err != nil {
			t.Fatal(err)
		}
		inst = *created
	} else {
		users, err := svc.ListUsers(ctx, tenants[0].ID, ListUsersAdminInput{})
		if err != nil || len(users) == 0 {
			t.Fatal(err)
		}
		principal = Principal{TenantID: tenants[0].ID, UserID: users[0].ID}
		instances, err := svc.ListInstances(ctx, principal)
		if err != nil || len(instances) == 0 {
			t.Fatal(err)
		}
		inst = instances[0]
	}
	cards, err := chatcard.Open(filepath.Join(dir, "cards.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetChatCards(cards)
	return svc, principal, inst, cards
}

func withEffect(run Run, effect string) Run {
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata["side_effect"] = effect
	run.Metadata["chat_card_prior_effects"] = effect
	return run
}

func putHandoffCard(t *testing.T, cards *chatcard.Store, principal Principal, botID, taskID, runID string) chatcard.Card {
	t.Helper()
	card, err := cards.Put(chatcard.OpenInput{
		TenantID: principal.TenantID, UserID: principal.UserID, BotID: botID, TaskID: taskID, RunID: runID,
		Kind: chatcard.KindNativeHandoff, Question: "请在这台电脑上完成这一步",
		LeaseID: "lease-a", DesktopMode: "isolated",
		Target: &chatcard.Target{URL: "https://shop.example/pay", Origin: "shop.example", Frame: "pay-frame"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func submitHandoff(t *testing.T, cards *chatcard.Store, principal Principal, card chatcard.Card, instanceID string) chatcard.Outcome {
	t.Helper()
	out, err := cards.Submit(chatcard.Actor{Kind: chatcard.ActorUser, TenantID: principal.TenantID, UserID: principal.UserID, Authenticated: true}, chatcard.Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID, InstanceID: instanceID,
		ChoiceIDs: []string{chatcard.ChoiceCompleted}, OperationVersion: card.Operation.Version,
		PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash, TargetFrame: "pay-frame",
	})
	if err != nil || !out.Resume || out.Mode != "verify" {
		t.Fatalf("submit = %#v %v", out, err)
	}
	return out
}
