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
	adapter        *Adapter
	mailer         Mailer
	activeProvider ActiveUserProvider // optional; when set, SendToActive targets only the last active IM
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

// BroadcastLoginLink sends a login confirmation link to all IM channels where
// the user (identified by email) is bound. Email is handled separately by the
// caller (mailer.SendLoginConfirmation), so this only covers IM platforms.
// Returns a list of channel display names where the link was sent.
func (b *NotifyBroadcaster) BroadcastLoginLink(ctx context.Context, email, confirmURL string) []string {
	if b.adapter == nil {
		return nil
	}

	b.adapter.mu.RLock()
	plugins := make(map[string]IMPlugin, len(b.adapter.plugins))
	for k, v := range b.adapter.plugins {
		plugins[k] = v
	}
	b.adapter.mu.RUnlock()

	msg := fmt.Sprintf("🔐 MaClaw Hub 登录确认\n\n请点击以下链接完成登录:\n%s\n\n链接 15 分钟内有效，如非本人操作请忽略。", confirmURL)

	var channels []string
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
		if err := plugin.SendText(ctx, target, msg); err != nil {
			log.Printf("[im/notify] send login link to %s (uid=%s) failed: %v", name, uid, err)
		} else {
			channels = append(channels, platformDisplayName(name))
		}
	}
	return channels
}

// BroadcastText sends a plain text message to the user's reachable channels.
// It tries IM platforms first; if at least one IM delivery succeeds, email is
// skipped to avoid duplicate / spammy notifications. Email is only used as a
// fallback when no IM channel is configured or all IM sends fail.
func (b *NotifyBroadcaster) BroadcastText(ctx context.Context, email, subject, text string) {
	imSent := false

	// 1. Try all bound IM platforms first.
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
			} else {
				imSent = true
			}
		}
	}

	// 2. Fall back to email only when no IM channel delivered successfully.
	if !imSent && b.mailer != nil {
		if err := b.mailer.Send(ctx, []string{email}, subject, text); err != nil {
			log.Printf("[im/notify] broadcast email to %s failed: %v", email, err)
		}
	}
}

// BroadcastFile sends a file to all IM channels where the user is reachable.
// If the platform supports file sending, the file is delivered directly;
// otherwise the accompanying message text is sent as a fallback.
// Used for Swarm PDF document delivery.
func (b *NotifyBroadcaster) BroadcastFile(ctx context.Context, email, b64Data, fileName, mimeType, message string) {
	if b.adapter == nil {
		return
	}

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
		caps := plugin.Capabilities()

		// 先发送附带的文本消息
		if message != "" {
			if err := plugin.SendText(ctx, target, message); err != nil {
				log.Printf("[im/notify] broadcast file text to %s (uid=%s) failed: %v", name, uid, err)
			}
		}

		// 发送文件
		if caps.SupportsFile {
			if err := plugin.SendFile(ctx, target, b64Data, fileName, mimeType); err != nil {
				log.Printf("[im/notify] broadcast file to %s (uid=%s) failed: %v", name, uid, err)
			}
		} else {
			// 平台不支持文件，发送文本提示
			fallback := fmt.Sprintf("📎 文件「%s」已生成，但当前平台不支持文件发送。请在桌面端查看。", fileName)
			_ = plugin.SendText(ctx, target, fallback)
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

// ActiveUserProvider returns the last active IM platform for a user.
type ActiveUserProvider interface {
	GetActiveUser(userID string) (platformName, platformUID string, ok bool)
}

// SetActiveUserProvider wires the active user provider so SendToActive
// can target only the user's last active IM platform.
func (b *NotifyBroadcaster) SetActiveUserProvider(p ActiveUserProvider) {
	b.activeProvider = p
}

// SendToActive sends a text message only to the user's last active IM
// platform. Falls back to sending to a single bound IM channel (first
// found) if no active platform is known, to avoid duplicate notifications.
func (b *NotifyBroadcaster) SendToActive(ctx context.Context, userID, email, subject, text string) {
	if b.activeProvider != nil {
		platformName, platformUID, ok := b.activeProvider.GetActiveUser(userID)
		if ok && b.adapter != nil {
			b.adapter.DeliverProgress(ctx, platformName, userID, platformUID, text)
			return
		}
	}
	// No active IM known — send to the first bound IM channel only (not all).
	// Prefer remote gateways (weixin, qqbot_remote, telegram) over hub-native
	// plugins since they are more commonly used in multi-machine setups.
	if b.adapter != nil {
		b.adapter.mu.RLock()
		plugins := make(map[string]IMPlugin, len(b.adapter.plugins))
		for k, v := range b.adapter.plugins {
			plugins[k] = v
		}
		b.adapter.mu.RUnlock()

		preferred := []string{"weixin", "qqbot_remote", "telegram", "feishu", "qqbot", "openclaw"}
		for _, name := range preferred {
			plugin, ok := plugins[name]
			if !ok {
				continue
			}
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
				log.Printf("[im/notify] SendToActive fallback to %s (uid=%s) failed: %v", name, uid, err)
				continue
			}
			return // sent to one channel, done
		}
	}
	// Last resort: email
	if b.mailer != nil {
		_ = b.mailer.Send(ctx, []string{email}, subject, text)
	}
}

// UserLookup resolves an internal user ID to an email address.
type UserLookup interface {
	GetEmail(ctx context.Context, userID string) (string, error)
}

// ProactiveSender implements ws.IMProactiveSender by resolving userID → email
// and then broadcasting via NotifyBroadcaster.
type ProactiveSender struct {
	broadcaster *NotifyBroadcaster
	users       UserLookup
}

// NewProactiveSender creates a ProactiveSender wired to the broadcaster and user lookup.
func NewProactiveSender(broadcaster *NotifyBroadcaster, users UserLookup) *ProactiveSender {
	return &ProactiveSender{broadcaster: broadcaster, users: users}
}

// SendProactiveMessage resolves the user's email and sends the text to the
// user's last active IM platform. Falls back to broadcasting if no active
// platform is known.
func (p *ProactiveSender) SendProactiveMessage(ctx context.Context, userID, text string) error {
	email, err := p.users.GetEmail(ctx, userID)
	if err != nil {
		return fmt.Errorf("resolve user email for %s: %w", userID, err)
	}
	p.broadcaster.SendToActive(ctx, userID, email, "MaClaw 通知", text)
	return nil
}

// SendProactiveFile resolves the user's email and broadcasts a file to all IM channels.
// Used for Swarm PDF document delivery.
func (p *ProactiveSender) SendProactiveFile(ctx context.Context, userID, b64Data, fileName, mimeType, message string) error {
	email, err := p.users.GetEmail(ctx, userID)
	if err != nil {
		return fmt.Errorf("resolve user email for %s: %w", userID, err)
	}
	p.broadcaster.BroadcastFile(ctx, email, b64Data, fileName, mimeType, message)
	return nil
}
