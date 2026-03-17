// Package im — cross-channel notification broadcaster.
//
// NotifyBroadcaster sends verification codes and other notifications to all
// channels where a user is reachable: email + any IM platforms where the
// user's email is already bound.
package im

import (
	"context"
	"fmt"
	"log"
)

// BindingLookup can resolve an email to a platform-specific UID.
// Each IM plugin that supports bindings implements this.
type BindingLookup interface {
	// LookupByEmail returns the platform UID for the given email, or "" if not bound.
	LookupByEmail(email string) string
}

// Mailer is the subset of mail.Service needed for sending emails.
type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

// NotifyBroadcaster sends a message to all reachable channels for a given email.
type NotifyBroadcaster struct {
	adapter *Adapter
	mailer  Mailer
}

// NewNotifyBroadcaster creates a broadcaster wired to the IM Adapter and mailer.
func NewNotifyBroadcaster(adapter *Adapter, mailer Mailer) *NotifyBroadcaster {
	return &NotifyBroadcaster{adapter: adapter, mailer: mailer}
}

// BroadcastVerifyCode sends a verification code to all channels where the
// user (identified by email) is reachable. This includes:
//   - Email (always, if mailer is configured)
//   - Any IM platform where the email is already bound (cross-IM verification)
//
// The excludePlatform parameter skips the platform that initiated the binding
// (the user is already chatting there, so we don't need to send the code there).
//
// Returns a human-readable summary of where the code was sent (e.g.
// "邮箱 + 飞书" or "邮箱") for the caller to display.
func (b *NotifyBroadcaster) BroadcastVerifyCode(ctx context.Context, email, code, excludePlatform string) (sentTo string, err error) {
	var channels []string
	var firstErr error

	// 1. Send via email (always attempt)
	if b.mailer != nil {
		subject := "MaClaw Hub 绑定验证码"
		body := fmt.Sprintf(
			"您好，\r\n\r\n您正在绑定 IM 账号到 MaClaw Hub。\r\n\r\n验证码: %s\r\n\r\n请在 5 分钟内将此验证码回复给对应的 Bot 完成绑定。\r\n如非本人操作，请忽略此邮件。\r\n",
			code,
		)
		if err := b.mailer.Send(ctx, []string{email}, subject, body); err != nil {
			log.Printf("[im/notify] send verification email to %s failed: %v", email, err)
			firstErr = err
		} else {
			channels = append(channels, "邮箱")
		}
	}

	// 2. Send to all other bound IM platforms
	if b.adapter != nil {
		b.adapter.mu.RLock()
		plugins := make(map[string]IMPlugin, len(b.adapter.plugins))
		for k, v := range b.adapter.plugins {
			plugins[k] = v
		}
		b.adapter.mu.RUnlock()

		msg := fmt.Sprintf("🔑 MaClaw Hub 绑定验证码: %s\n\n请在发起绑定的 IM 中回复此验证码完成绑定（5 分钟内有效）。", code)

		for name, plugin := range plugins {
			if name == excludePlatform {
				continue
			}
			// Check if this plugin supports binding lookup
			lookup, ok := plugin.(BindingLookup)
			if !ok {
				continue
			}
			uid := lookup.LookupByEmail(email)
			if uid == "" {
				continue
			}
			// Send the code via this platform
			target := UserTarget{PlatformUID: uid}
			if err := plugin.SendText(ctx, target, msg); err != nil {
				log.Printf("[im/notify] send verification to %s (uid=%s) failed: %v", name, uid, err)
			} else {
				channels = append(channels, platformDisplayName(name))
			}
		}
	}

	if len(channels) == 0 {
		if firstErr != nil {
			return "", fmt.Errorf("无法发送验证码: %w", firstErr)
		}
		return "", fmt.Errorf("无法发送验证码: 邮件服务未配置且无其他已绑定的 IM 通道")
	}

	return joinChannels(channels), nil
}

// BroadcastText sends a plain text message to all channels where the user
// (identified by email) is reachable. Useful for login confirmations, alerts, etc.
func (b *NotifyBroadcaster) BroadcastText(ctx context.Context, email, subject, text string) {
	// Email
	if b.mailer != nil {
		if err := b.mailer.Send(ctx, []string{email}, subject, text); err != nil {
			log.Printf("[im/notify] broadcast email to %s failed: %v", email, err)
		}
	}

	// All bound IM platforms
	if b.adapter != nil {
		b.adapter.mu.RLock()
		plugins := make(map[string]IMPlugin, len(b.adapter.plugins))
		for k, v := range b.adapter.plugins {
			plugins[k] = v
		}
		b.adapter.mu.RUnlock()

		for name, plugin := range plugins {
			lookup, ok := plugin.(BindingLookup)
			if !ok {
				continue
			}
			uid := lookup.LookupByEmail(email)
			if uid == "" {
				continue
			}
			target := UserTarget{PlatformUID: uid}
			if err := plugin.SendText(ctx, target, text); err != nil {
				log.Printf("[im/notify] broadcast to %s (uid=%s) failed: %v", name, uid, err)
			}
		}
	}
}

func platformDisplayName(name string) string {
	switch name {
	case "feishu":
		return "飞书"
	case "qqbot":
		return "QQ"
	default:
		return name
	}
}

func joinChannels(channels []string) string {
	if len(channels) == 0 {
		return ""
	}
	if len(channels) == 1 {
		return channels[0]
	}
	result := channels[0]
	for _, ch := range channels[1:] {
		result += " + " + ch
	}
	return result
}
