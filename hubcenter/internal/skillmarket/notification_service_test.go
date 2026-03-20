package skillmarket

import (
	"context"
	"testing"
	"time"
)

// mockMailer 用于测试的邮件发送器。
type mockMailer struct {
	sent []mockMail
}

type mockMail struct {
	to      []string
	subject string
	body    string
}

func (m *mockMailer) Send(_ context.Context, to []string, subject, body string) error {
	m.sent = append(m.sent, mockMail{to: to, subject: subject, body: body})
	return nil
}

func (m *mockMailer) SendHubRegistrationConfirmation(_ context.Context, to string, confirmURL string, hubName string) error {
	return nil
}

// ── Task 41.5: 通知服务属性测试 ─────────────────────────────────────────

func TestCalcInterval_ExponentialIncrease(t *testing.T) {
	// 第 n 封间隔 = 2^(n-2) 小时
	expected := []struct {
		n     int
		hours float64
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 4},
		{5, 8},
		{6, 16},
		{7, 32},
		{8, 64},
		{9, 128},
		{10, 256},
	}
	for _, tc := range expected {
		got := CalcInterval(tc.n)
		gotHours := got.Hours()
		if gotHours != tc.hours {
			t.Errorf("CalcInterval(%d) = %.0f hours, want %.0f", tc.n, gotHours, tc.hours)
		}
	}
}

func TestCalcInterval_MaxNotifications(t *testing.T) {
	// maxNotifications = 10, 验证不超过
	if maxNotifications != 10 {
		t.Errorf("maxNotifications: got %d, want 10", maxNotifications)
	}
}

// ── Task 41.6: 通知服务单元测试 ─────────────────────────────────────────

func TestNotificationService_StartSequence(t *testing.T) {
	store := newTestStore(t)
	mailer := &mockMailer{}
	svc, err := NewNotificationService(store, mailer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 首次触发应立即发送
	if err := svc.StartSequence(ctx, "api_key_restock", "seller@test.com", "skill-1", "补货提醒", "请补充 API Key"); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 {
		t.Errorf("expected 1 email sent, got %d", len(mailer.sent))
	}

	// 重复创建同类型同上下文不应重复
	if err := svc.StartSequence(ctx, "api_key_restock", "seller@test.com", "skill-1", "补货提醒", "请补充 API Key"); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 {
		t.Errorf("duplicate start should not send again, got %d emails", len(mailer.sent))
	}
}

func TestNotificationService_StopSequence(t *testing.T) {
	store := newTestStore(t)
	mailer := &mockMailer{}
	svc, _ := NewNotificationService(store, mailer)
	ctx := context.Background()

	_ = svc.StartSequence(ctx, "api_key_restock", "seller@test.com", "skill-1", "补货", "body")
	if err := svc.StopSequence(ctx, "api_key_restock", "skill-1"); err != nil {
		t.Fatal(err)
	}

	// 停止后处理不应发送
	beforeCount := len(mailer.sent)
	_ = svc.ProcessPendingNotifications(ctx)
	if len(mailer.sent) != beforeCount {
		t.Error("stopped sequence should not send more emails")
	}
}

func TestNotificationService_ProcessPending(t *testing.T) {
	store := newTestStore(t)
	mailer := &mockMailer{}
	svc, _ := NewNotificationService(store, mailer)
	ctx := context.Background()

	_ = svc.StartSequence(ctx, "test_notif", "user@test.com", "ctx-1", "Subject", "Body")

	// 手动将 next_send_at 设为过去
	_, _ = store.db.ExecContext(ctx,
		`UPDATE sm_notification_sequences SET next_send_at = ? WHERE notification_type = 'test_notif'`,
		time.Now().Add(-1*time.Hour).Format(timeFmt))

	// 处理到期通知
	if err := svc.ProcessPendingNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	// 应该发送了第 2 封
	if len(mailer.sent) != 2 {
		t.Errorf("expected 2 emails total, got %d", len(mailer.sent))
	}
}

func TestNotificationService_MaxLimit(t *testing.T) {
	store := newTestStore(t)
	mailer := &mockMailer{}
	svc, _ := NewNotificationService(store, mailer)
	ctx := context.Background()

	_ = svc.StartSequence(ctx, "test_max", "user@test.com", "ctx-max", "Subject", "Body")

	// 手动设置 sent_count 为 9（接近上限 10）
	_, _ = store.db.ExecContext(ctx,
		`UPDATE sm_notification_sequences SET sent_count = 9, next_send_at = ? WHERE notification_type = 'test_max'`,
		time.Now().Add(-1*time.Hour).Format(timeFmt))

	_ = svc.ProcessPendingNotifications(ctx)
	// 发送第 10 封后应停止
	// 验证序列已停止
	var isActive int
	_ = store.readDB.QueryRowContext(ctx,
		`SELECT is_active FROM sm_notification_sequences WHERE notification_type = 'test_max'`).Scan(&isActive)
	if isActive != 0 {
		t.Error("sequence should be deactivated after 10 notifications")
	}
}
