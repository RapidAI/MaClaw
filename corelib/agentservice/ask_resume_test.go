package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// scriptedAsk resumes the first call as an ask and the later calls as a
// plain reply. It records the run id of every execute.
type scriptedAsk struct {
	calls int
	runs  []string
}

func (e *scriptedAsk) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	e.calls++
	e.runs = append(e.runs, req.RunID)
	if e.calls == 1 {
		return &ExecuteResult{
			Content:    "Which one?",
			OutputType: "text/plain",
			Metadata:   map[string]string{metaResponseSource: string(responseSourceAskUser)},
		}, nil
	}
	return &ExecuteResult{Content: "Continuing that same run.", OutputType: "text/plain"}, nil
}

func TestAskAnswerResumesTheWaitingRun(t *testing.T) {
	exec := &scriptedAsk{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), exec)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer func() { _ = svc.Close() }()
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1:9", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	asked, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "Help me choose"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if asked == nil || !asked.WaitingForUser {
		t.Fatalf("ask run waiting=%v", asked)
	}
	answered, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content:  "the first one",
		Metadata: map[string]string{"bot_ask_run": asked.ID},
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if answered == nil || answered.ID != asked.ID {
		t.Fatalf("answer run=%v ask=%s", answered, asked.ID)
	}
	if answered.WaitingForUser {
		t.Fatal("resumed run still waiting")
	}
	if len(exec.runs) != 2 || exec.runs[0] != asked.ID || exec.runs[1] != asked.ID {
		t.Fatalf("executes=%v", exec.runs)
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, asked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WaitingForUser || stored.Metadata["resumed_from_ask"] != "1" {
		t.Fatalf("stored waiting=%v meta=%v", stored.WaitingForUser, stored.Metadata)
	}
}

func TestNewerMessageClearsWaitingRuns(t *testing.T) {
	exec := &scriptedAsk{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), exec)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer func() { _ = svc.Close() }()
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1:9", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatal(err)
	}
	asked, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "Help me choose"})
	if err != nil {
		t.Fatal(err)
	}
	if asked == nil || !asked.WaitingForUser {
		t.Fatalf("waiting=%v", asked)
	}
	next, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "do something else"})
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID == asked.ID {
		t.Fatalf("a message that is not the answer must be its own run, got %v", next)
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, asked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WaitingForUser || stored.Metadata["ask_superseded"] != "1" {
		t.Fatalf("old ask still waiting: waiting=%v meta=%v", stored.WaitingForUser, stored.Metadata)
	}
}

func TestEndedRunClearsWaitingAsk(t *testing.T) {
	exec := &scriptedAsk{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1:9", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatal(err)
	}
	asked, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "Help me choose"})
	if err != nil || asked == nil || !asked.WaitingForUser {
		t.Fatalf("ask=%v err=%v", asked, err)
	}
	current, err := svc.GetSession(context.Background(), principal, inst.ID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := current.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	meta["client_session_key"] = "bot_4"
	if _, err := svc.UpdateSession(context.Background(), principal, inst.ID, sess.ID, UpdateSessionInput{Metadata: meta}); err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteBotAsk(context.Background(), principal, inst.ID, "bot_4", BotContextAsk{
		RunID: asked.ID, Question: "Which one?", Options: []string{"tea", "coffee"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteBotTask(context.Background(), principal, inst.ID, "bot_4", BotContextTask{
		RunID: asked.ID, Instruction: "Help me choose", Status: "cancelled",
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, asked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WaitingForUser || stored.Metadata["ask_superseded"] != "run_ended" {
		t.Fatalf("ended run still waiting: waiting=%v meta=%v", stored.WaitingForUser, stored.Metadata)
	}
	snap, err := svc.ReadBotContext(context.Background(), principal, inst.ID, "bot_4")
	if err != nil {
		t.Fatal(err)
	}
	open := 0
	for _, ask := range snap.Asks {
		if !ask.Withdrawn {
			open++
		}
	}
	if open != 0 {
		t.Fatalf("open asks after the run ended: %+v", snap.Asks)
	}
}
