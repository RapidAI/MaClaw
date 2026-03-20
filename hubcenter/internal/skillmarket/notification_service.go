package skillmarket

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

const maxNotifications = 10

// NotificationSequence 代表一个通知序列。
type NotificationSequence struct {
	ID               string    `json:"id"`
	NotificationType string    `json:"notification_type"` // api_key_restock, pending_key_backlog
	TargetEmail      string    `json:"target_email"`
	TriggerContext   string    `json:"trigger_context"` // e.g. skill_id
	Subject          string    `json:"subject"`
	Body             string    `json:"body"`
	SentCount        int       `json:"sent_count"`
	NextSendAt       time.Time `json:"next_send_at"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// NotificationService 管理指数回退邮件通知。
type NotificationService struct {
	store  *Store
	mailer mail.Mailer
}

// NewNotificationService 创建 NotificationService。
func NewNotificationService(store *Store, mailer mail.Mailer) (*NotificationService, error) {
	svc := &NotificationService{store: store, mailer: mailer}
	if err := svc.migrate(); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *NotificationService) migrate() error {
	_, err := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS sm_notification_sequences (
		id                 TEXT PRIMARY KEY,
		notification_type  TEXT NOT NULL,
		target_email       TEXT NOT NULL,
		trigger_context    TEXT NOT NULL,
		subject            TEXT NOT NULL DEFAULT '',
		body               TEXT NOT NULL DEFAULT '',
		sent_count         INTEGER NOT NULL DEFAULT 0,
		next_send_at       TEXT NOT NULL,
		is_active          INTEGER NOT NULL DEFAULT 1,
		created_at         TEXT NOT NULL,
		updated_at         TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("notification migrate: %w", err)
	}
	_, _ = s.store.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_notif_active ON sm_notification_sequences(is_active, next_send_at)`)
	_, _ = s.store.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_notif_type_ctx ON sm_notification_sequences(notification_type, trigger_context) WHERE is_active = 1`)
	return nil
}

// StartSequence 创建新通知序列并立即发送第 1 封。
func (s *NotificationService) StartSequence(ctx context.Context, notifType, targetEmail, triggerCtx, subject, body string) error {
	// 检查是否已有活跃序列
	var existing int
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_notification_sequences WHERE notification_type = ? AND trigger_context = ? AND is_active = 1`,
		notifType, triggerCtx).Scan(&existing)
	if existing > 0 {
		return nil // 已有活跃序列，不重复创建
	}

	// 立即发送第 1 封
	if err := s.mailer.Send(ctx, []string{targetEmail}, subject, body); err != nil {
		return fmt.Errorf("send first notification: %w", err)
	}

	now := time.Now()
	nextSend := now.Add(1 * time.Hour) // 第 2 封间隔 = 2^(2-2) = 1 小时
	id := generateID()
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO sm_notification_sequences (id, notification_type, target_email, trigger_context, subject, body, sent_count, next_send_at, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, 1, ?, ?)`,
		id, notifType, targetEmail, triggerCtx, subject, body, fmtTime(nextSend), fmtTime(now), fmtTime(now))
	return err
}

// StopSequence 停止指定类型和上下文的活跃序列。
func (s *NotificationService) StopSequence(ctx context.Context, notifType, triggerCtx string) error {
	now := fmtTime(time.Now())
	_, err := s.store.db.ExecContext(ctx, `UPDATE sm_notification_sequences SET is_active = 0, updated_at = ? WHERE notification_type = ? AND trigger_context = ? AND is_active = 1`,
		now, notifType, triggerCtx)
	return err
}

// ProcessPendingNotifications 扫描到期通知并发送。
func (s *NotificationService) ProcessPendingNotifications(ctx context.Context) error {
	now := time.Now()
	nowStr := fmtTime(now)

	rows, err := s.store.readDB.QueryContext(ctx, `SELECT id, target_email, subject, body, sent_count FROM sm_notification_sequences WHERE is_active = 1 AND next_send_at <= ?`, nowStr)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct {
		id, email, subject, body string
		sentCount                int
	}
	var items []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.email, &p.subject, &p.body, &p.sentCount); err != nil {
			return err
		}
		items = append(items, p)
	}

	for _, p := range items {
		if p.sentCount >= maxNotifications {
			// 达到上限，停止序列
			_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_notification_sequences SET is_active = 0, updated_at = ? WHERE id = ?`, nowStr, p.id)
			continue
		}

		// 发送邮件
		if err := s.mailer.Send(ctx, []string{p.email}, p.subject, p.body); err != nil {
			continue // 发送失败跳过，下次重试
		}

		newCount := p.sentCount + 1
		if newCount >= maxNotifications {
			// 达到上限
			_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_notification_sequences SET sent_count = ?, is_active = 0, updated_at = ? WHERE id = ?`, newCount, nowStr, p.id)
		} else {
			// 计算下一次发送时间: 间隔 = 2^(n-1) 小时 (n 是下一封的序号，从 2 开始)
			nextInterval := time.Duration(math.Pow(2, float64(newCount-1))) * time.Hour
			nextSend := now.Add(nextInterval)
			_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_notification_sequences SET sent_count = ?, next_send_at = ?, updated_at = ? WHERE id = ?`,
				newCount, fmtTime(nextSend), nowStr, p.id)
		}
	}
	return nil
}

// CalcInterval 返回第 n 封邮件的发送间隔（n >= 2）。
func CalcInterval(n int) time.Duration {
	if n < 2 {
		return 0
	}
	return time.Duration(math.Pow(2, float64(n-2))) * time.Hour
}
