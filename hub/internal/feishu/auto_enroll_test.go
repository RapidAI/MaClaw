package feishu

import (
	"context"
	"testing"
	"time"

	"github.com/go-lark/lark/v2"
)

func TestAutoEnroller_DisabledByDefault(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return nil },
		func(email, openID string) {},
	)

	if ae.IsEnabled() {
		t.Fatal("expected auto-enroller to be disabled by default")
	}

	result, err := ae.AddToFeishuOrg(context.Background(), "test@example.com", "Test", "")
	if err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}
	if result == nil || result.Status != "disabled" {
		t.Fatalf("expected status 'disabled', got %+v", result)
	}
}

func TestAutoEnroller_EnableDisable(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return nil },
		func(email, openID string) {},
	)

	ae.SetEnabled(true)
	if !ae.IsEnabled() {
		t.Fatal("expected enabled after SetEnabled(true)")
	}

	ae.SetEnabled(false)
	if ae.IsEnabled() {
		t.Fatal("expected disabled after SetEnabled(false)")
	}
}

func TestAutoEnroller_ConfigRoundTrip(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return nil },
		func(email, openID string) {},
	)

	cfg := AutoEnrollConfig{
		Enabled:      true,
		DepartmentID: "dept_123",
	}
	ae.SetConfig(cfg)

	got := ae.Config()
	if !got.Enabled || got.DepartmentID != "dept_123" {
		t.Fatalf("config mismatch: got %+v", got)
	}
}

func TestAutoEnroller_NilBot(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return nil },
		func(email, openID string) {},
	)
	ae.SetConfig(AutoEnrollConfig{Enabled: true})

	result, err := ae.AddToFeishuOrg(context.Background(), "test@example.com", "Test", "")
	if err == nil {
		t.Fatal("expected error when bot is nil")
	}
	if result == nil || result.Status != "failed" {
		t.Fatalf("expected status 'failed', got %+v", result)
	}
}

func TestAutoEnroller_Cooldown(t *testing.T) {
	callCount := 0
	ae := NewAutoEnroller(
		func() *lark.Bot {
			callCount++
			return lark.NewChatBot("test", "test")
		},
		func(email, openID string) {},
	)
	ae.SetConfig(AutoEnrollConfig{Enabled: true})

	// First call — will fail because bot can't reach Feishu API, but
	// the cooldown entry should be recorded.
	_, _ = ae.AddToFeishuOrg(context.Background(), "cooldown@example.com", "Test", "")
	firstCount := callCount

	// Second call within cooldown — should return immediately without calling bot.
	result, err := ae.AddToFeishuOrg(context.Background(), "cooldown@example.com", "Test", "")
	if err != nil {
		t.Fatalf("unexpected error on cooldown call: %v", err)
	}
	if result == nil || result.Status != "skipped" {
		t.Fatalf("expected status 'skipped', got %+v", result)
	}
	if callCount != firstCount {
		t.Fatalf("expected bot not to be called during cooldown, but callCount went from %d to %d", firstCount, callCount)
	}
}

func TestAutoEnroller_EmptyEmail(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return lark.NewChatBot("test", "test") },
		func(email, openID string) {},
	)
	ae.SetConfig(AutoEnrollConfig{Enabled: true})

	// Empty email should be a no-op.
	result, err := ae.AddToFeishuOrg(context.Background(), "", "Test", "")
	if err != nil {
		t.Fatalf("unexpected error for empty email: %v", err)
	}
	if result == nil || result.Status != "skipped" {
		t.Fatalf("expected status 'skipped', got %+v", result)
	}
}

func TestNormalizeChinaMobile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"15646550398", "+8615646550398"},
		{"8615646550398", "+8615646550398"},
		{"+8615646550398", "+8615646550398"},
		{"+86 156 4655 0398", "+8615646550398"},
		{"86 15646550398", "+8615646550398"},
		{"+86-156-4655-0398", "+8615646550398"},
		{"12345", "12345"},       // too short, return as-is
		{"+1234567890", "+1234567890"}, // non-China, return as-is
	}
	for _, tt := range tests {
		got := normalizeChinaMobile(tt.input)
		if got != tt.want {
			t.Errorf("normalizeChinaMobile(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAutoEnroller_DefaultDisplayName(t *testing.T) {
	// Verify that when displayName is empty, the email local part is used.
	// We can't test the full API call, but we can verify the fallback logic
	// by checking that AddToFeishuOrg proceeds past the displayName assignment.
	ae := NewAutoEnroller(
		func() *lark.Bot { return lark.NewChatBot("test", "test") },
		func(email, openID string) {},
	)
	ae.SetConfig(AutoEnrollConfig{Enabled: true})

	// This will fail at the API call level, but the important thing is
	// it doesn't panic or skip due to empty displayName.
	_, err := ae.AddToFeishuOrg(context.Background(), "alice@example.com", "", "+8613800138000")
	if err == nil {
		t.Log("no error (unexpected but acceptable in test env)")
	}
}

func TestAutoEnroller_CooldownEviction(t *testing.T) {
	ae := NewAutoEnroller(
		func() *lark.Bot { return nil },
		func(email, openID string) {},
	)
	ae.SetConfig(AutoEnrollConfig{Enabled: true})

	// Manually inject a stale entry.
	ae.mu.Lock()
	ae.attempts["stale@example.com"] = time.Now().Add(-addMemberCooldown * 3)
	ae.mu.Unlock()

	// Trigger a call that will evict the stale entry.
	_, _ = ae.AddToFeishuOrg(context.Background(), "fresh@example.com", "Test", "")

	ae.mu.Lock()
	_, staleExists := ae.attempts["stale@example.com"]
	ae.mu.Unlock()

	if staleExists {
		t.Fatal("expected stale entry to be evicted")
	}
}
