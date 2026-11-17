package chatcard

import (
	"errors"
	"testing"
	"time"
)

func pngBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x5f, 0xec, 0x8c,
		0x6a, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func user() Actor {
	return Actor{Kind: ActorUser, TenantID: "t", UserID: "u", Authenticated: true}
}

func handoff(t *testing.T, task string) (Card, *Store) {
	t.Helper()
	store := newStore(t)
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: task, RunID: "run-" + task,
		Kind: KindNativeHandoff, Question: "请在这台电脑上完成登录",
		LeaseID: "lease-a", DesktopMode: "isolated",
		ShotBytes: pngBytes(),
		Target:    &Target{URL: "https://shop.example/pay", Origin: "shop.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return card, store
}

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir() + "/cards.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNativeHandoffCompletedVerifiesOnce(t *testing.T) {
	card, store := handoff(t, "task-a")
	if card.Shot == nil || card.Spotlight || card.Handoff != HandoffFullPage || len(card.Fields) != 0 {
		t.Fatalf("card = %#v", card)
	}
	pub := Public(card)
	if pub.Shot != nil && pub.Shot.Digest == "" {
		t.Fatal("public shot lost its digest")
	}
	sub := Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{ChoiceCompleted}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash,
		ShotID: card.Shot.ID, CapturedAt: card.Shot.CapturedAt, LeaseID: card.LeaseID,
		TargetOrigin: "shop.example", TargetURL: "https://shop.example/pay",
	}
	first, err := store.Submit(user(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Resume || first.Done || first.Mode != "verify" || first.RunID != card.RunID || first.TaskID != card.TaskID || len(first.Granted) != 0 {
		t.Fatalf("first = %#v", first)
	}
	if Grants(mustGet(t, store, card.ID), "purchase") || Grants(mustGet(t, store, card.ID), "pay") {
		t.Fatal("completion granted a payment")
	}
	second, err := store.Submit(user(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if second.Resume || !second.Already || mustGet(t, store, card.ID).ResumeCount != 1 {
		t.Fatalf("second = %#v count %d", second, mustGet(t, store, card.ID).ResumeCount)
	}
	got, err := store.RecordResult(card.ID, "", true)
	if err == nil || got.Status == StatusDone {
		t.Fatal("empty evidence marked the task done")
	}
	got, err = store.RecordResult(card.ID, "page shows the order is unpaid", true)
	if err != nil || got.Status != StatusDone || got.Result.Evidence == "" {
		t.Fatalf("result = %#v %v", got, err)
	}
}

func TestHandoffRejectsTamperStaleWrongOwnerAndSibling(t *testing.T) {
	card, store := handoff(t, "task-a")
	other, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-b", TaskID: "task-b", RunID: "run-b",
		Kind: KindNativeHandoff, Question: "另一台电脑",
		LeaseID: "lease-b", DesktopMode: "shared", ShotBytes: pngBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{ChoiceCompleted}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash,
		ShotID: card.Shot.ID, CapturedAt: card.Shot.CapturedAt, LeaseID: card.LeaseID,
	}
	if _, err := store.Submit(Actor{Kind: ActorModel, TenantID: "t", UserID: "u", Authenticated: true}, base); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("model actor err = %v", err)
	}
	if _, err := store.Submit(Actor{Kind: ActorUser, TenantID: "t", UserID: "other", Authenticated: true}, base); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other user err = %v", err)
	}
	bad := base
	bad.ChoiceIDs = []string{"pay-now"}
	if _, err := store.Submit(user(), bad); err == nil {
		t.Fatal("tampered choice accepted")
	}
	bad = base
	bad.PayloadHash = "deadbeef"
	if _, err := store.Submit(user(), bad); !errors.Is(err, ErrStale) {
		t.Fatalf("stale hash err = %v", err)
	}
	bad = base
	bad.LeaseID = other.LeaseID
	if _, err := store.Submit(user(), bad); !errors.Is(err, ErrForbidden) {
		t.Fatalf("sibling lease err = %v", err)
	}
	bad = base
	bad.TaskID = other.TaskID
	if _, err := store.Submit(user(), bad); !errors.Is(err, ErrForbidden) {
		t.Fatalf("sibling task err = %v", err)
	}
	if _, err := store.Desktop(user(), card.ID, other.BotID, other.LeaseID, card.Shot.CapturedAt); !errors.Is(err, ErrForbidden) {
		t.Fatalf("open sibling desktop err = %v", err)
	}
	bind, err := store.Desktop(user(), card.ID, card.BotID, card.LeaseID, card.Shot.CapturedAt)
	if err != nil || bind.Wake || bind.Attach != AttachWatch || bind.BotID != "bot-a" || bind.LeaseID != "lease-a" || bind.DesktopMode != "isolated" {
		t.Fatalf("bind = %#v %v", bind, err)
	}
	if _, err := store.Desktop(user(), card.ID, card.BotID, card.LeaseID, card.Shot.CapturedAt.Add(time.Second)); !errors.Is(err, ErrStale) {
		t.Fatalf("stale shot err = %v", err)
	}
	cancel := base
	cancel.ChoiceIDs = []string{ChoiceCancel}
	out, err := store.Submit(user(), cancel)
	if err != nil || out.Resume || out.Mode != "cancel" || len(out.Granted) != 0 {
		t.Fatalf("cancel = %#v %v", out, err)
	}
	if mustGet(t, store, other.ID).Status != StatusPending {
		t.Fatal("sibling card changed")
	}
}

func TestConfirmIsNotPurchaseAndTextRejectsSecrets(t *testing.T) {
	store := newStore(t)
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-c", RunID: "run-c",
		Kind: KindConfirm, Question: "继续这一步？",
		Operation: Operation{Kind: "continue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.Submit(user(), Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{ChoiceApprove}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash,
	})
	if err != nil || !out.Resume || out.Done || len(out.Granted) != 0 || Grants(mustGet(t, store, card.ID), "purchase") {
		t.Fatalf("confirm = %#v %v", out, err)
	}
	_, err = store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-d", RunID: "run-d",
		Kind: KindText, Question: "收款密码",
		Fields: []Field{{ID: "password", Label: "密码"}},
	})
	if err == nil {
		t.Fatal("password field was accepted")
	}
	_, err = store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-e", RunID: "run-e",
		Kind: KindText, Question: "送到哪里？",
		Lines: []Line{{Text: "地址 上海市", EvidenceID: "missing"}},
	})
	if err == nil {
		t.Fatal("invented address was accepted")
	}
}

func TestReopenKeepsTheDecision(t *testing.T) {
	path := t.TempDir() + "/cards.db"
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-f", RunID: "run-f",
		Kind: KindChoice, Question: "选一个",
		Choices: []Choice{{ID: "opt_0", Label: "甲"}, {ID: "opt_1", Label: "乙"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(user(), Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{"opt_1"}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.Get(card.ID)
	if err != nil || got.Status != StatusProcessing || got.ResumeCount != 1 || got.Decision == nil || got.Decision.ChoiceIDs[0] != "opt_1" {
		t.Fatalf("reopened = %#v %v", got, err)
	}
}

func mustGet(t *testing.T, store *Store, id string) Card {
	t.Helper()
	card, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return card
}
