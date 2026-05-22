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
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// BindingLookup can resolve an email to a platform-specific UID.
// Each IM plugin that supports bindings implements this.
type BindingLookup interface {
	// LookupByEmail returns the platform UID for the given email, or "" if not bound.
	LookupByEmail(email string) string
}

// TenantBindingLookup resolves bindings within one Hub tenant. Plugins that
// only implement BindingLookup are treated as legacy default-tenant bindings.
type TenantBindingLookup interface {
	LookupByTenantEmail(tenantID, email string) string
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
	return b.BroadcastVerifyCodeForTenant(ctx, store.DefaultTenantID, email, code, excludePlatform)
}

func (b *NotifyBroadcaster) BroadcastVerifyCodeForTenant(ctx context.Context, tenantID, email, code, excludePlatform string) (sentTo string, err error) {
	tenantID = normalizeTenantID(tenantID)
	ctx = WithTenant(ctx, tenantID)
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
		plugins := b.adapter.PluginsForTenant(tenantID)

		msg := fmt.Sprintf("🔑 MaClaw Hub 绑定验证码: %s\n\n请在发起绑定的 IM 中回复此验证码完成绑定（5 分钟内有效）。", code)

		for name, plugin := range plugins {
			if name == excludePlatform {
				continue
			}
			uid := lookupBindingUID(plugin, tenantID, email)
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
	return b.BroadcastLoginLinkForTenant(ctx, store.DefaultTenantID, email, confirmURL)
}

func (b *NotifyBroadcaster) BroadcastLoginLinkForTenant(ctx context.Context, tenantID, email, confirmURL string) []string {
	tenantID = normalizeTenantID(tenantID)
	ctx = WithTenant(ctx, tenantID)
	if b.adapter == nil {
		return nil
	}

	plugins := b.adapter.PluginsForTenant(tenantID)

	msg := fmt.Sprintf("🔐 MaClaw Hub 登录确认\n\n请点击以下链接完成登录:\n%s\n\n链接 15 分钟内有效，如非本人操作请忽略。", confirmURL)

	var channels []string
	for name, plugin := range plugins {
		uid := lookupBindingUID(plugin, tenantID, email)
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
	b.BroadcastTextForTenant(ctx, store.DefaultTenantID, email, subject, text)
}

func (b *NotifyBroadcaster) BroadcastTextForTenant(ctx context.Context, tenantID, email, subject, text string) {
	tenantID = normalizeTenantID(tenantID)
	ctx = WithTenant(ctx, tenantID)
	imSent := false

	// 1. Try all bound IM platforms first.
	if b.adapter != nil {
		plugins := b.adapter.PluginsForTenant(tenantID)

		for name, plugin := range plugins {
			uid := lookupBindingUID(plugin, tenantID, email)
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
	b.BroadcastFileForTenant(ctx, store.DefaultTenantID, email, b64Data, fileName, mimeType, message)
}

func (b *NotifyBroadcaster) BroadcastFileForTenant(ctx context.Context, tenantID, email, b64Data, fileName, mimeType, message string) {
	tenantID = normalizeTenantID(tenantID)
	ctx = WithTenant(ctx, tenantID)
	if b.adapter == nil {
		return
	}

	plugins := b.adapter.PluginsForTenant(tenantID)

	for name, plugin := range plugins {
		uid := lookupBindingUID(plugin, tenantID, email)
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

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return store.DefaultTenantID
	}
	return tenantID
}

func lookupBindingUID(plugin IMPlugin, tenantID, email string) string {
	if lookup, ok := plugin.(TenantBindingLookup); ok {
		return lookup.LookupByTenantEmail(tenantID, email)
	}
	if tenantID != store.DefaultTenantID {
		return ""
	}
	lookup, ok := plugin.(BindingLookup)
	if !ok {
		return ""
	}
	return lookup.LookupByEmail(email)
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
	b.SendToActiveForTenant(ctx, store.DefaultTenantID, userID, email, subject, text)
}

func (b *NotifyBroadcaster) SendToActiveForTenant(ctx context.Context, tenantID, userID, email, subject, text string) {
	tenantID = normalizeTenantID(tenantID)
	ctx = WithTenant(ctx, tenantID)
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
		plugins := b.adapter.PluginsForTenant(tenantID)

		preferred := []string{"weixin", "qqbot_remote", "telegram", "feishu", "qqbot", "openclaw"}
		for _, name := range preferred {
			plugin, ok := plugins[name]
			if !ok {
				continue
			}
			uid := lookupBindingUID(plugin, tenantID, email)
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
	GetEmail(ctx context.Context, tenantID, userID string) (string, error)
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
func (p *ProactiveSender) SendProactiveMessage(ctx context.Context, tenantID, userID, text string) error {
	tenantID = normalizeTenantID(tenantID)
	email, err := p.users.GetEmail(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("resolve user email for %s: %w", userID, err)
	}
	p.broadcaster.SendToActiveForTenant(ctx, tenantID, userID, email, "MaClaw 通知", text)
	return nil
}

// SendProactiveFile resolves the user's email and broadcasts a file to all IM channels.
// Used for Swarm PDF document delivery.
func (p *ProactiveSender) SendProactiveFile(ctx context.Context, tenantID, userID, b64Data, fileName, mimeType, message string) error {
	tenantID = normalizeTenantID(tenantID)
	email, err := p.users.GetEmail(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("resolve user email for %s: %w", userID, err)
	}
	p.broadcaster.BroadcastFileForTenant(ctx, tenantID, email, b64Data, fileName, mimeType, message)
	return nil
}
