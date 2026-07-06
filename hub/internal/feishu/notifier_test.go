package feishu

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type stubSystemSettingsRepo struct {
	values map[string]string
}

func (r *stubSystemSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = valueJSON
	return nil
}

func (r *stubSystemSettingsRepo) Get(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

// stubUserRepo implements store.UserRepository for testing.
type stubUserRepo struct {
	users map[string]*store.User
}

func (r *stubUserRepo) Create(_ context.Context, _ *store.User) error { return nil }
func (r *stubUserRepo) GetByID(_ context.Context, id string) (*store.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, nil
	}
	return u, nil
}
func (r *stubUserRepo) GetByEmail(_ context.Context, _ string) (*store.User, error) {
	return nil, nil
}
func (r *stubUserRepo) GetByTenantEmail(_ context.Context, tenantID string, email string) (*store.User, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	for _, u := range r.users {
		if store.NormalizeTenantID(u.TenantID) == tenantID && u.Email == email {
			return u, nil
		}
	}
	return nil, nil
}
func (r *stubUserRepo) List(_ context.Context) ([]*store.User, error) {
	users := make([]*store.User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, user)
	}
	return users, nil
}
func (r *stubUserRepo) ListByTenant(_ context.Context, tenantID string) ([]*store.User, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	var users []*store.User
	for _, user := range r.users {
		if store.NormalizeTenantID(user.TenantID) == tenantID {
			users = append(users, user)
		}
	}
	return users, nil
}
func (r *stubUserRepo) DeleteByEmail(_ context.Context, _ string) error { return nil }
func (r *stubUserRepo) DeleteByTenantEmail(_ context.Context, _ string, _ string) error {
	return nil
}
func (r *stubUserRepo) UpdateSmartRoute(_ context.Context, _ string, _ bool) error { return nil }
func (r *stubUserRepo) MarkEmailVerified(_ context.Context, _ string, _ string) error {
	return nil
}
func (r *stubUserRepo) GetByTenantIdentity(_ context.Context, _, _, _ string) (*store.User, error) {
	return nil, nil
}
func (r *stubUserRepo) ListIdentitiesByUser(_ context.Context, _, _ string) ([]*store.UserIdentity, error) {
	return nil, nil
}
func (r *stubUserRepo) UpsertIdentity(_ context.Context, _ *store.UserIdentity) error {
	return nil
}

func TestNewReturnsNotifierWithNilBot(t *testing.T) {
	n := New("", "", &stubUserRepo{}, nil, nil)
	if n == nil {
		t.Fatal("expected non-nil notifier (bot should be nil inside)")
	}
	if n.bot != nil {
		t.Fatal("expected nil bot when credentials are empty")
	}
}

func TestHandleEventNilNotifier(t *testing.T) {
	// Should not panic.
	var n *Notifier
	n.HandleEvent(session.Event{Type: "session.created"})
}

func TestResolveEmail(t *testing.T) {
	repo := &stubUserRepo{users: map[string]*store.User{
		"u1": {ID: "u1", Email: "alice@example.com"},
	}}
	n := &Notifier{users: repo, idCache: make(map[string]string), oidCache: make(map[string]BindingInfo), pending: make(map[string]*pendingBind)}

	email := n.resolveEmail("u1")
	if email != "alice@example.com" {
		t.Fatalf("expected alice@example.com, got %s", email)
	}

	// Second call should hit cache.
	email = n.resolveEmail("u1")
	if email != "alice@example.com" {
		t.Fatalf("cache miss: expected alice@example.com, got %s", email)
	}

	// Unknown user.
	email = n.resolveEmail("unknown")
	if email != "" {
		t.Fatalf("expected empty for unknown user, got %s", email)
	}
}

func TestTenantOpenIDBindingsAreIsolated(t *testing.T) {
	system := &stubSystemSettingsRepo{}
	n := New("", "", &stubUserRepo{}, system, nil)
	t.Cleanup(n.StopEventLoop)

	n.BindOpenIDForTenant(store.DefaultTenantID, "same@example.com", "open-default", "")
	n.BindOpenIDForTenant("tenant_a", "same@example.com", "open-tenant-a", "")

	if got := n.ResolveOpenIDByTenantEmail(store.DefaultTenantID, "same@example.com"); got != "open-default" {
		t.Fatalf("default tenant binding = %q", got)
	}
	if got := n.ResolveOpenIDByTenantEmail("tenant_a", "same@example.com"); got != "open-tenant-a" {
		t.Fatalf("tenant binding = %q", got)
	}

	n.RemoveOpenIDForTenant("tenant_a", "same@example.com")
	if got := n.ResolveOpenIDByTenantEmail("tenant_a", "same@example.com"); got != "" {
		t.Fatalf("tenant binding after remove = %q", got)
	}
	if got := n.ResolveOpenIDByTenantEmail(store.DefaultTenantID, "same@example.com"); got != "open-default" {
		t.Fatalf("default tenant binding after tenant remove = %q", got)
	}
}

func TestFeishuEmailBindingResolvesUniqueTenant(t *testing.T) {
	system := &stubSystemSettingsRepo{}
	repo := &stubUserRepo{users: map[string]*store.User{
		"u1": {ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
	}}
	n := New("", "", repo, system, nil)
	t.Cleanup(n.StopEventLoop)

	n.pendMu.Lock()
	n.pending["open-a"] = &pendingBind{TenantID: "tenant_a", Email: "same@example.com", Code: "123456", Expiry: time.Now().Add(time.Minute)}
	n.pendMu.Unlock()

	handleVerifyCode(n, "open-a", "123456")

	if got := n.ResolveOpenIDByTenantEmail("tenant_a", "same@example.com"); got != "open-a" {
		t.Fatalf("tenant binding = %q", got)
	}
	if got := n.ResolveOpenIDByTenantEmail(store.DefaultTenantID, "same@example.com"); got != "" {
		t.Fatalf("default binding should remain empty, got %q", got)
	}
}

func TestFeishuEmailBindingDoesNotDefaultAmbiguousEmail(t *testing.T) {
	system := &stubSystemSettingsRepo{}
	repo := &stubUserRepo{users: map[string]*store.User{
		"u1": {ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		"u2": {ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}
	n := New("", "", repo, system, nil)
	t.Cleanup(n.StopEventLoop)

	handleEmailSubmit(n, "open-ambiguous", "same@example.com")

	n.pendMu.Lock()
	_, pending := n.pending["open-ambiguous"]
	n.pendMu.Unlock()
	if pending {
		t.Fatal("ambiguous email should not create a pending binding")
	}
	if got := n.ResolveOpenIDByTenantEmail(store.DefaultTenantID, "same@example.com"); got != "" {
		t.Fatalf("ambiguous email should not bind default tenant, got %q", got)
	}
}

func TestBuildCardJSON(t *testing.T) {
	json := buildCardJSON("Test Title", "blue", []cardField{
		{"Key1", "Value1"},
		{"Key2", ""},
		{"Key3", "Value3"},
	})
	if json == "" {
		t.Fatal("expected non-empty card JSON")
	}
	// Should contain the title and non-empty fields.
	if !contains(json, "Test Title") {
		t.Error("card JSON missing title")
	}
	if !contains(json, "Key1") || !contains(json, "Value1") {
		t.Error("card JSON missing Key1/Value1")
	}
	if contains(json, "Key2") {
		t.Error("card JSON should not contain empty-value field Key2")
	}
	if !contains(json, "Key3") {
		t.Error("card JSON missing Key3")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	result := truncate("hello world", 5)
	if result != "hello…" {
		t.Errorf("expected 'hello…', got '%s'", result)
	}
}

func TestTruncateUnicode(t *testing.T) {
	// Chinese characters: each is one rune.
	result := truncate("你好世界测试", 4)
	if result != "你好世界…" {
		t.Errorf("expected '你好世界…', got '%s'", result)
	}
}

func TestStatusHelpers(t *testing.T) {
	if statusEmoji("running") != "🔄" {
		t.Error("wrong emoji for running")
	}
	if statusColor("error") != "red" {
		t.Error("wrong color for error")
	}
	if severityColor("warning") != "orange" {
		t.Error("wrong severity color for warning")
	}
}

func TestOnSessionSummarySkipsRunning(t *testing.T) {
	// This test verifies that running status without WaitingForUser is skipped.
	// We can't easily verify no message was sent without a mock bot, but we
	// can at least ensure no panic.
	repo := &stubUserRepo{users: map[string]*store.User{
		"u1": {ID: "u1", Email: "test@example.com"},
	}}
	n := &Notifier{users: repo, idCache: make(map[string]string), oidCache: make(map[string]BindingInfo), pending: make(map[string]*pendingBind)}
	// bot is nil so sendToUser will fail gracefully — that's fine for this test.

	n.onSessionSummary(session.Event{
		Type:   "session.summary",
		UserID: "u1",
		Summary: &session.SessionSummary{
			Status:         "running",
			WaitingForUser: false,
			Tool:           "claude",
		},
	})
}

func TestShortID(t *testing.T) {
	if shortID("abcdefghij") != "abcdefgh" {
		t.Error("expected 8-char short ID")
	}
	if shortID("abc") != "abc" {
		t.Error("short ID should not truncate short strings")
	}
}

func TestDefaultStr(t *testing.T) {
	_ = time.Now() // ensure time import is used
	if defaultStr("", "fallback") != "fallback" {
		t.Error("expected fallback")
	}
	if defaultStr("value", "fallback") != "value" {
		t.Error("expected value")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
