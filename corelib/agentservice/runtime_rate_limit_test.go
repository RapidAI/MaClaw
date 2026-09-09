package agentservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestPostMessageUsesSharedTenantRateLimiter(t *testing.T) {
	now := time.Unix(500, 0)
	svc, err := NewService(Config{
		DataRoot:              t.TempDir(),
		TokenSecret:           "test",
		RuntimeRateLimitRate:  1,
		RuntimeRateLimitBurst: 1,
	}, NewMemoryStore(), EchoExecutor{})
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
	if _, _, err := svc.PostMessage(context.Background(), p, inst.ID, sess.ID, PostMessageInput{Content: "first"}); err != nil {
		t.Fatalf("first PostMessage: %v", err)
	}
	if _, _, err := svc.PostMessage(context.Background(), p, inst.ID, sess.ID, PostMessageInput{Content: "second"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second PostMessage error = %v, want ErrRateLimited", err)
	}
	snapshot := svc.RuntimeMetrics().Snapshot()
	if snapshot.RateLimited != 1 || snapshot.TurnsAdmitted != 1 {
		t.Fatalf("unexpected rate-limit metrics: %#v", snapshot)
	}
}

type fakeDistributedLimiter struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeDistributedLimiter) Allow(ctx context.Context, tenant string) (bool, time.Duration, error) {
	if ctx == nil || tenant == "" {
		panic("distributed limiter received invalid request")
	}
	f.calls++
	return f.allowed, time.Second, f.err
}

func TestDistributedRuntimeRateLimiterIsAuthoritative(t *testing.T) {
	limiter := &fakeDistributedLimiter{}
	svc := &Service{distributedLimiter: limiter, metrics: agentruntime.NewRuntimeMetrics(10)}
	if err := svc.enforceRuntimeRateLimit(context.Background(), "tenant-1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("enforceRuntimeRateLimit error = %v, want ErrRateLimited", err)
	}
	if limiter.calls != 1 {
		t.Fatalf("distributed limiter calls = %d, want 1", limiter.calls)
	}
	limiter.allowed = true
	if err := svc.enforceRuntimeRateLimit(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("allowed distributed request: %v", err)
	}
	if limiter.calls != 2 {
		t.Fatalf("distributed limiter calls = %d, want 2", limiter.calls)
	}
}

func TestPostMessageIdempotentReplayDoesNotConsumeRateLimitToken(t *testing.T) {
	now := time.Unix(600, 0)
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test", RuntimeRateLimitRate: 1, RuntimeRateLimitBurst: 1}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.now = func() time.Time { return now }
	tenant, _ := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	user, _ := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	_, _ = svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "k", MaclawLLMModel: "m"})
	inst, _ := svc.CreateInstance(context.Background(), p, CreateInstanceInput{Name: "one"})
	sess, _ := svc.CreateSession(context.Background(), p, inst.ID, CreateSessionInput{Title: "s1"})
	first, _, err := svc.PostMessage(context.Background(), p, inst.ID, sess.ID, PostMessageInput{Content: "same", Metadata: map[string]string{"client_message_id": "stable"}})
	if err != nil {
		t.Fatalf("first PostMessage: %v", err)
	}
	replay, _, err := svc.PostMessage(context.Background(), p, inst.ID, sess.ID, PostMessageInput{Content: "same", Metadata: map[string]string{"client_message_id": "stable"}})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if replay == nil || first == nil || replay.ID != first.ID {
		t.Fatalf("replay did not return canonical run: first=%#v replay=%#v", first, replay)
	}
	if got := svc.RuntimeMetrics().Snapshot().RateLimited; got != 0 {
		t.Fatalf("idempotent replay consumed rate-limit token: %d", got)
	}
}
