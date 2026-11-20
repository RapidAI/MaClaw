package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// BR-20261120-8A708E128485 — a run that stops to wait for the person is 待决策,
// i.e. paused. Before this change every such run was persisted as
// RunStatusSucceeded, so any consumer that reads run status alone counted it as
// finished work: the person saw a 完成 task that was really waiting on them.

func TestRunWaitingForUserIsPausedNotSucceeded(t *testing.T) {
	// The status a waiting run gets must be its own value, and it must not be
	// possible to mistake it for the success value.
	if RunStatusPaused == RunStatusSucceeded {
		t.Fatal("RunStatusPaused must be a distinct run status")
	}
	if RunStatusPaused == "" {
		t.Fatal("RunStatusPaused must have a stable wire value")
	}
	if RunStatusPaused != "paused" {
		t.Fatalf("RunStatusPaused wire value %q, want %q", RunStatusPaused, "paused")
	}
}

// A paused run stays in the transcript as answered history: the person was
// shown a question and the turn is not an abandoned request. The model must not
// be told to answer it again alongside the next, unrelated turn.
func TestPausedRunCountsAsAnsweredHistory(t *testing.T) {
	h := []Message{
		{ID: "u1", Role: MessageRoleUser, Content: "把结果发到 CSDN"},
		{ID: "a1", Role: MessageRoleAssistant, Content: "要发到哪个专栏？"},
		{ID: "u2", Role: MessageRoleUser, Content: "推荐一首歌"},
	}
	runs := []Run{
		// The run asked the person something and stopped; no follow-up yet.
		{ID: "r1", UserMessageID: "u1", Status: RunStatusPaused},
		{ID: "r2", UserMessageID: "u2", Status: RunStatusRunning},
	}
	got := dropAbandonedUserTurns(h, runs, "u2")
	if len(got) != 3 {
		t.Fatalf("a paused turn was dropped from history: %+v", got)
	}
}

// A failed or cancelled run must still count as abandoned, so adding the paused
// case did not flatten the distinction between "waiting on the person" and
// "the run died".
func TestCancelledRunStillDroppedWhilePausedIsKept(t *testing.T) {
	h := []Message{
		{ID: "u1", Role: MessageRoleUser, Content: "查一下天气"},
		{ID: "u2", Role: MessageRoleUser, Content: "把结果发到 CSDN"},
		{ID: "a2", Role: MessageRoleAssistant, Content: "选哪个专栏？"},
		{ID: "u3", Role: MessageRoleUser, Content: "推荐一首歌"},
	}
	runs := []Run{
		{ID: "r1", UserMessageID: "u1", Status: RunStatusCancelled},
		{ID: "r2", UserMessageID: "u2", Status: RunStatusPaused},
		{ID: "r3", UserMessageID: "u3", Status: RunStatusRunning},
	}
	got := dropAbandonedUserTurns(h, runs, "u3")
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	for _, id := range ids {
		if id == "u1" {
			t.Fatalf("cancelled turn still reaches the model: %v", ids)
		}
	}
	// The paused turn, its question and the assistant answer stay.
	if len(got) != 3 || got[0].ID != "u2" || got[1].ID != "a2" || got[2].ID != "u3" {
		t.Fatalf("paused turn must stay in history: %v", ids)
	}
}

// The desktop treats a paused run as terminal for events and receipts, but the
// long-job follower only delivers files for a run that actually succeeded.
func TestPausedIsTerminalButNotSuccess(t *testing.T) {
	if RunStatusPaused == RunStatusSucceeded {
		t.Fatal("paused must not equal succeeded")
	}
	if !TaskOutcomeNeedsUser(TaskOutcomeAwaitingUser) {
		t.Fatal("awaiting_user must count as needing the user")
	}
	if TaskOutcomeNeedsUser(TaskOutcomeDone) {
		t.Fatal("done must not count as needing the user")
	}
}

// A paused run must not be counted as succeeded in the metrics and audit that
// other consumers read. Before this fix the tail of PostMessage hard-coded
// terminalStatus = "succeeded" and recorded a "run.succeeded" audit event even
// for a waiting run, so the Prometheus turns_succeeded and the
// run_succeeded_events counters both counted a 待决策 run as finished work.
func TestPausedRunDoesNotReadAsSucceededInMetricsAndAudit(t *testing.T) {
	ex := &outcomeExecutor{res: ExecuteResult{Content: "要发到哪个专栏？", OutputType: "text/plain", Metadata: map[string]string{metaResponseSource: "ask_user"}}}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, nil, ex)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	svc.AssistantMessageMetadataHook = func(ctx context.Context, p Principal, inst Instance, sess Session, run Run, msg Message, cfg corelib.AppConfig) map[string]string {
		return map[string]string{TaskOutcomeKey: TaskOutcomeAwaitingUser}
	}
	ctx := context.Background()
	tenant, _ := svc.CreateTenant(ctx, CreateTenantInput{Name: "T"})
	user, _ := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "U"})
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, p, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1:9", MaclawLLMKey: "k", MaclawLLMModel: "m"}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(ctx, p, CreateInstanceInput{Name: "I"})
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := svc.CreateSession(ctx, p, inst.ID, CreateSessionInput{Title: "S"})
	run, _, err := svc.PostMessage(ctx, p, inst.ID, sess.ID, PostMessageInput{Content: "把文章发到 CSDN"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusPaused {
		t.Fatalf("run status %s, want %s", run.Status, RunStatusPaused)
	}

	// The runtime metric for the turn must be paused, not succeeded and not
	// failed.
	metrics := svc.RuntimeMetrics().Snapshot()
	if metrics.TurnsSucceeded != 0 {
		t.Fatalf("paused run counted as succeeded: %#v", metrics)
	}
	if metrics.TurnsFailed != 0 {
		t.Fatalf("paused run counted as failed: %#v", metrics)
	}
	if metrics.TurnsPaused != 1 {
		t.Fatalf("paused run not counted as paused: %#v", metrics)
	}

	// The audit trail must record the real terminal action, not the success
	// that a status-blind reader would count.
	pausedEvents, err := svc.ListAuditEvents(ctx, ListAuditEventsInput{Action: "run.paused", ResourceType: "run", ResourceID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(pausedEvents) != 1 {
		t.Fatalf("expected one run.paused audit event, got %#v", pausedEvents)
	}
	succeededEvents, err := svc.ListAuditEvents(ctx, ListAuditEventsInput{Action: "run.succeeded", ResourceType: "run", ResourceID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(succeededEvents) != 0 {
		t.Fatalf("paused run recorded run.succeeded audit event: %#v", succeededEvents)
	}
}

// The desktop receipt is decided after the first save. That save already wrote
// succeeded; PersistRunReceipt then marks the run waiting. A later reader of
// status alone must still see paused, not a completed run.
func TestPersistRunReceiptPausesSucceededWaitingRun(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, &outcomeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	run := &Run{ID: "run_receipt", Status: RunStatusSucceeded, WaitingForUser: false}
	msg := &Message{ID: "msg_receipt", Metadata: map[string]string{TaskOutcomeKey: TaskOutcomeAwaitingUser}}
	if err := svc.PersistRunReceipt(run, msg); err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusPaused || !run.WaitingForUser {
		t.Fatalf("receipt left status %s waiting %v, want paused true", run.Status, run.WaitingForUser)
	}
	stored := store.runs[run.ID]
	if stored.Status != RunStatusPaused || !stored.WaitingForUser {
		t.Fatalf("stored receipt status %s waiting %v", stored.Status, stored.WaitingForUser)
	}
}

func TestPersistRunReceiptDoesNotPauseFailedRun(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), &outcomeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	run := &Run{ID: "run_failed", Status: RunStatusFailed}
	msg := &Message{ID: "msg_failed", Metadata: map[string]string{TaskOutcomeKey: TaskOutcomeBlocked}}
	if err := svc.PersistRunReceipt(run, msg); err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusFailed || !run.WaitingForUser {
		t.Fatalf("failed run status %s waiting %v, want failed true", run.Status, run.WaitingForUser)
	}
}
