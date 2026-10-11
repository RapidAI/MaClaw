package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// failingExecutor returns real output alongside an error, the way a run cut
// short by an upstream throttle does: the work of earlier rounds is done and
// only the closing model call failed.
type failingExecutor struct {
	content string
	err     error
}

func (e failingExecutor) DescribeCapabilities(context.Context, ExecuteRequest) (*AgentCapabilities, error) {
	return &AgentCapabilities{Executor: "failing"}, nil
}

func (e failingExecutor) Execute(context.Context, ExecuteRequest) (*ExecuteResult, error) {
	if strings.TrimSpace(e.content) == "" {
		return nil, e.err
	}
	return &ExecuteResult{
		Content:    e.content,
		OutputType: "text/plain",
	}, e.err
}

// newPartialResultFixture builds a service with one tenant, user, instance and
// session, ready for PostMessage.
func newPartialResultFixture(t *testing.T, executor Executor) (*Service, Principal, string, string) {
	t.Helper()
	now := time.Unix(500, 0)
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.now = func() time.Time { return now }
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "k", MaclawLLMModel: "m"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), p, CreateInstanceInput{Name: "one"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), p, inst.ID, CreateSessionInput{Title: "s1"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return svc, p, inst.ID, sess.ID
}

// TestFailedRunKeepsPartialOutput is the regression guard for a run killed
// mid-way: the text it already produced must survive, not vanish behind a bare
// terminal status. Before this the executor's output was dropped with the error
// and the person saw an empty turn.
func TestFailedRunKeepsPartialOutput(t *testing.T) {
	boom := errors.New("LLM call failed: MaClaw 官方周期限流：当前周期额度已用尽，约 6 小时 后恢复。")
	svc, p, instanceID, sessionID := newPartialResultFixture(t, failingExecutor{
		content: "仓库已推送，v1.0.1 三个平台产物已发布。",
		err:     boom,
	})

	run, _, err := svc.PostMessage(context.Background(), p, instanceID, sessionID, PostMessageInput{Content: "go"})
	if !errors.Is(err, boom) {
		t.Fatalf("PostMessage error = %v, want the executor error", err)
	}
	if run.Status != RunStatusFailed {
		t.Fatalf("status = %q, want failed", run.Status)
	}
	if run.Error != boom.Error() {
		t.Fatalf("run.Error = %q, want the real cause %q", run.Error, boom.Error())
	}
	if run.AssistantMessageID == "" {
		t.Fatal("run has no assistant message: the partial output was discarded")
	}

	messages, err := svc.store.ListMessages(sessionID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	var saved *Message
	for i := range messages {
		if messages[i].ID == run.AssistantMessageID {
			saved = &messages[i]
		}
	}
	if saved == nil {
		t.Fatalf("assistant message %q not persisted", run.AssistantMessageID)
	}
	if !strings.Contains(saved.Content, "v1.0.1") {
		t.Fatalf("saved content = %q, want the partial output", saved.Content)
	}
	if saved.Metadata[metaPartialResult] != "true" {
		t.Fatalf("message not marked partial, metadata = %#v", saved.Metadata)
	}
}

// TestFailedRunWithoutOutputStaysTerminalOnly keeps the old shape when there is
// nothing to show: no message invented, terminal event only.
func TestFailedRunWithoutOutputStaysTerminalOnly(t *testing.T) {
	boom := errors.New("boom")
	svc, p, instanceID, sessionID := newPartialResultFixture(t, failingExecutor{err: boom})

	run, _, err := svc.PostMessage(context.Background(), p, instanceID, sessionID, PostMessageInput{Content: "go"})
	if !errors.Is(err, boom) {
		t.Fatalf("PostMessage error = %v, want %v", err, boom)
	}
	if run.AssistantMessageID != "" {
		t.Fatalf("no output should mean no assistant message, got %q", run.AssistantMessageID)
	}
	messages, err := svc.store.ListMessages(sessionID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	for _, m := range messages {
		if m.Role == MessageRoleAssistant {
			t.Fatalf("assistant message %q invented for a run with no output", m.ID)
		}
	}
}

// TestCancelledRunKeepsExecutorReason is the wording fix: a run whose context
// was cancelled but whose executor reports a concrete cause must keep that
// cause. "run cancelled" alone leaves the reader with nothing to act on.
//
// It drives the branch through a host EarlyStop-style cancellation by
// cancelling the parent context the moment the executor is entered.
func TestCancelledRunKeepsExecutorReason(t *testing.T) {
	throttled := errors.New("LLM call failed: MaClaw 官方周期限流：当前周期额度已用尽，约 6 小时 后恢复。")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, p, instanceID, sessionID := newPartialResultFixture(t, &cancelAndFailExecutor{err: throttled, cancel: cancel})

	run, _, err := svc.PostMessage(ctx, p, instanceID, sessionID, PostMessageInput{Content: "go"})
	if err == nil {
		t.Fatal("PostMessage should surface the executor error")
	}
	if run.Status != RunStatusCancelled {
		t.Fatalf("status = %q, want cancelled", run.Status)
	}
	if !strings.Contains(run.Error, throttled.Error()) {
		t.Fatalf("run.Error = %q, want it to name the real cause %q", run.Error, throttled.Error())
	}
	if run.Error == "run cancelled" {
		t.Fatal("the real cause was overwritten with a bare \"run cancelled\"")
	}
}

// cancelAndFailExecutor cancels the caller's context and returns a concrete
// error, reproducing a long run whose caller gave up while the provider was
// throttling: the context is cancelled, but the executor still knows why.
type cancelAndFailExecutor struct {
	err    error
	cancel func()
}

func (e *cancelAndFailExecutor) DescribeCapabilities(context.Context, ExecuteRequest) (*AgentCapabilities, error) {
	return &AgentCapabilities{Executor: "cancel"}, nil
}

func (e *cancelAndFailExecutor) Execute(_ context.Context, _ ExecuteRequest) (*ExecuteResult, error) {
	if e.cancel != nil {
		e.cancel()
	}
	return nil, e.err
}
