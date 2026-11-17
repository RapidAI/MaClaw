package chatcard

import (
	"errors"
	"sync"
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
		ChoiceIDs: []string{ChoiceCompleted}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
		ShotID: card.Shot.ID, CapturedAt: card.Shot.CapturedAt, LeaseID: card.LeaseID,
		TargetOrigin: "shop.example", TargetURL: "https://shop.example/pay",
	}
	first, err := store.Submit(user(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Resume || first.Done || first.Mode != "verify" || first.ResumeID == "" || first.RunID != card.RunID || first.TaskID != card.TaskID {
		t.Fatalf("first = %#v", first)
	}
	if Grants(mustGet(t, store, card.ID), "purchase") || Grants(mustGet(t, store, card.ID), "pay") {
		t.Fatal("completion granted a payment")
	}
	queued, err := store.QueuedResumes()
	if err != nil || len(queued) != 1 || queued[0].ID != first.ResumeID {
		t.Fatalf("queued = %#v %v", queued, err)
	}
	second, err := store.Submit(user(), sub)
	if err != nil || !second.Resume || second.ResumeID != first.ResumeID {
		t.Fatalf("retry before delivery = %#v %v", second, err)
	}
	if err := store.MarkDelivered(first.ResumeID); err != nil {
		t.Fatal(err)
	}
	third, err := store.Submit(user(), sub)
	if err != nil || third.Resume || !third.Already || third.ResumeID != first.ResumeID || mustGet(t, store, card.ID).ResumeCount != 1 {
		t.Fatalf("after delivery = %#v count %d %v", third, mustGet(t, store, card.ID).ResumeCount, err)
	}
	if mustGet(t, store, card.ID).Status == StatusDone {
		t.Fatal("the click marked the card done")
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
		ChoiceIDs: []string{ChoiceCompleted}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
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
	if err != nil || out.Resume || out.Mode != "cancel" {
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
		ChoiceIDs: []string{ChoiceApprove}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
	})
	if err != nil || !out.Resume || out.Done || Grants(mustGet(t, store, card.ID), "purchase") {
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
		ChoiceIDs: []string{"opt_1"}, OperationVersion: card.Operation.Version, PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
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

func TestResumeOutboxSurvivesCrashAndRestart(t *testing.T) {
	path := t.TempDir() + "/cards.db"
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-crash", RunID: "run-crash",
		Kind: KindConfirm, Question: "继续这一步？",
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{ChoiceApprove}, OperationVersion: card.Operation.Version,
		PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
	}
	store.BeforeCommit = func() error { return errors.New("crash before commit") }
	if _, err := store.Submit(user(), sub); err == nil {
		t.Fatal("commit succeeded during the crash")
	}
	if mustGet(t, store, card.ID).Status != StatusPending {
		t.Fatal("failed commit changed the card")
	}
	queued, err := store.QueuedResumes()
	if err != nil || len(queued) != 0 {
		t.Fatalf("outbox after crash = %#v %v", queued, err)
	}
	store.BeforeCommit = nil
	first, err := store.Submit(user(), sub)
	if err != nil || !first.Resume || first.ResumeID == "" {
		t.Fatalf("submit after crash = %#v %v", first, err)
	}
	_ = store.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	queued, err = again.QueuedResumes()
	if err != nil || len(queued) != 1 || queued[0].ID != first.ResumeID || queued[0].RunID != card.RunID {
		t.Fatalf("restart queue = %#v %v", queued, err)
	}
	if err := again.MarkDelivered(first.ResumeID); err != nil {
		t.Fatal(err)
	}
	if err := again.MarkDelivered(first.ResumeID); err != nil {
		t.Fatal(err)
	}
	left, err := again.QueuedResumes()
	if err != nil || len(left) != 0 {
		t.Fatalf("delivered queue = %#v %v", left, err)
	}
	third, err := again.Submit(user(), sub)
	if err != nil || third.Resume || !third.Already || third.ResumeID != first.ResumeID {
		t.Fatalf("retry after restart delivery = %#v %v", third, err)
	}
}

func TestParallelClientsShareOneResume(t *testing.T) {
	store := newStore(t)
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-par", RunID: "run-par",
		Kind: KindConfirm, Question: "继续这一步？",
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{ChoiceApprove}, OperationVersion: card.Operation.Version,
		PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash,
	}
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := store.Submit(user(), sub)
			errs[i] = err
			ids[i] = out.ResumeID
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("client %d id %q err %v", i, ids[i], err)
		}
	}
	queued, err := store.QueuedResumes()
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue = %#v %v", queued, err)
	}
}

func TestDisplayedChangeInvalidatesConsent(t *testing.T) {
	store := newStore(t)
	card, err := store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-hash", RunID: "run-hash",
		Kind: KindChoice, Question: "选一个",
		Choices:   []Choice{{ID: "opt_0", Label: "甲"}, {ID: "opt_1", Label: "乙"}},
		Lines:     []Line{{Text: "下一步"}},
		Target:    &Target{URL: "https://shop.example/item", Origin: "shop.example", Frame: "frame-1"},
		Operation: Operation{Kind: "continue", Payload: []byte(`{"step":"look"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.ConsentHash == "" || card.ConsentHash != ConsentOf(card) {
		t.Fatalf("hash = %s", card.ConsentHash)
	}
	base := Submission{
		RequestID: card.ID, TaskID: card.TaskID, BotID: card.BotID, RunID: card.RunID,
		ChoiceIDs: []string{"opt_0"}, OperationVersion: card.Operation.Version,
		PayloadHash: card.Operation.Hash, ConsentHash: card.ConsentHash, TargetFrame: "frame-1",
	}
	changed := card
	changed.Lines = []Line{{Text: "价格 ¥10"}}
	base.ConsentHash = ConsentOf(changed)
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("changed line err = %v", err)
	}
	changed = card
	changed.Choices[1].Label = "丙"
	base.ConsentHash = ConsentOf(changed)
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("changed option err = %v", err)
	}
	changed = card
	changed.Target = &Target{URL: "https://evil.example", Origin: "evil.example", Frame: "frame-1"}
	base.ConsentHash = ConsentOf(changed)
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("changed url err = %v", err)
	}
	changed = card
	changed.Operation.Payload = []byte(`{"step":"pay"}`)
	base.ConsentHash = ConsentOf(changed)
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("changed payload err = %v", err)
	}
	base.ConsentHash = card.ConsentHash
	base.TargetFrame = ""
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("missing frame err = %v", err)
	}
	base.TargetFrame = "other-frame"
	if _, err := store.Submit(user(), base); !errors.Is(err, ErrStale) {
		t.Fatalf("wrong frame err = %v", err)
	}
	base.TargetFrame = "frame-1"
	if _, err := store.Submit(user(), base); err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-pay", RunID: "run-pay",
		Kind: KindConfirm, Question: "确认支付 ¥99", Operation: Operation{Kind: "continue"},
	})
	if err == nil {
		t.Fatal("a payment sentence was accepted as a card")
	}
	_, err = store.Put(OpenInput{
		TenantID: "t", UserID: "u", BotID: "bot-a", TaskID: "task-grant", RunID: "run-grant",
		Kind: KindConfirm, Question: "继续？", Operation: Operation{Kind: "purchase"},
	})
	if err == nil {
		t.Fatal("a purchase operation was accepted")
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
