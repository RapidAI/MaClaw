// Package im — remote_gateway_plugin.go implements an IMPlugin that delegates
// message I/O to a client-side IM gateway (QQ Bot, Telegram, etc.) via the
// existing Hub↔Client WebSocket connection.
//
// The client runs the actual bot gateway (WebSocket to QQ, long-polling to
// Telegram) and forwards incoming messages to Hub as "im.gateway_message".
// Hub routes them through the standard IM Adapter pipeline (identity binding,
// /call, /discuss, agent routing). Outbound replies are sent back to the
// client as "im.gateway_reply" so the client can deliver them via the
// platform-specific API.
//
// This makes client-side IM bots behave identically to Hub-native plugins
// like Feishu — supporting multi-machine routing, @name targeting, /discuss,
// and all other IM Adapter features.
package im

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ---------------------------------------------------------------------------
// Gateway lock — only one client may hold the gateway for a given platform.
// ---------------------------------------------------------------------------

type gatewayOwner struct {
	MachineID string
	UserID    string
	ClaimedAt time.Time
}

// ---------------------------------------------------------------------------
// MachineMessageSender abstracts sending a JSON message to a specific machine.
// ---------------------------------------------------------------------------

type MachineMessageSender interface {
	SendToMachine(machineID string, msg any) error
}

// ---------------------------------------------------------------------------
// RemoteGatewayPlugin
// ---------------------------------------------------------------------------

// RemoteGatewayPlugin implements IMPlugin for client-side IM gateways.
// One instance is created per platform name (e.g. "qqbot", "telegram").
type RemoteGatewayPlugin struct {
	platform string // "qqbot" or "telegram"
	sender   MachineMessageSender
	users    store.UserRepository
	system   store.SystemSettingsRepository

	mu             sync.RWMutex
	owner          *gatewayOwner          // current gateway holder
	messageHandler func(msg IncomingMessage) // set by IM Adapter via ReceiveMessage

	// email↔platformUID bindings (persisted in system_settings)
	bindMu   sync.RWMutex
	bindings map[string]string // platformUID → email

	// pending email verification
	pendingMu sync.Mutex
	pending   map[string]*pendingRemoteBind // platformUID → pending
}

type pendingRemoteBind struct {
	Email     string
	Code      string
	ExpiresAt time.Time
	Attempts  int
}


// NewRemoteGatewayPlugin creates a new remote gateway plugin for the given
// platform name (e.g. "qqbot", "telegram").
func NewRemoteGatewayPlugin(platform string, sender MachineMessageSender, users store.UserRepository, system store.SystemSettingsRepository) *RemoteGatewayPlugin {
	p := &RemoteGatewayPlugin{
		platform: platform,
		sender:   sender,
		users:    users,
		system:   system,
		bindings: make(map[string]string),
		pending:  make(map[string]*pendingRemoteBind),
	}
	p.loadBindings()
	return p
}

// ---------------------------------------------------------------------------
// IMPlugin interface
// ---------------------------------------------------------------------------

func (p *RemoteGatewayPlugin) Name() string { return p.platform }

func (p *RemoteGatewayPlugin) ReceiveMessage(handler func(msg IncomingMessage)) {
	p.mu.Lock()
	p.messageHandler = handler
	p.mu.Unlock()
}

func (p *RemoteGatewayPlugin) SendText(ctx context.Context, target UserTarget, text string) error {
	return p.sendToGatewayOwner("text", map[string]any{
		"platform_uid": target.PlatformUID,
		"text":         text,
	})
}

func (p *RemoteGatewayPlugin) SendCard(ctx context.Context, target UserTarget, card OutgoingMessage) error {
	// Client-side gateways (QQ/TG) don't support rich cards — fall back to text.
	fallback := card.FallbackText
	if fallback == "" {
		fallback = fmt.Sprintf("%s %s\n%s", card.StatusIcon, card.Title, card.Body)
	}
	return p.SendText(ctx, target, fallback)
}

func (p *RemoteGatewayPlugin) SendImage(ctx context.Context, target UserTarget, imageKey string, caption string) error {
	return p.sendToGatewayOwner("image", map[string]any{
		"platform_uid": target.PlatformUID,
		"image_data":   imageKey,
		"caption":      caption,
	})
}

func (p *RemoteGatewayPlugin) SendFile(ctx context.Context, target UserTarget, fileData, fileName, mimeType string) error {
	return p.sendToGatewayOwner("file", map[string]any{
		"platform_uid": target.PlatformUID,
		"file_data":    fileData,
		"file_name":    fileName,
		"mime_type":    mimeType,
	})
}

func (p *RemoteGatewayPlugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	p.bindMu.RLock()
	email, ok := p.bindings[platformUID]
	p.bindMu.RUnlock()
	if !ok || email == "" {
		return "", fmt.Errorf("%s: user %s not bound", p.platform, platformUID)
	}
	user, err := p.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		return "", fmt.Errorf("%s: no hub user for email %s", p.platform, email)
	}
	return user.ID, nil
}

func (p *RemoteGatewayPlugin) Capabilities() CapabilityDeclaration {
	return CapabilityDeclaration{
		SupportsRichCard:    false,
		SupportsMarkdown:    false,
		SupportsImage:       true,
		SupportsFile:        true,
		SupportsButton:      false,
		SupportsMessageEdit: false,
		MaxTextLength:       4000,
	}
}

func (p *RemoteGatewayPlugin) Start(ctx context.Context) error { return nil }
func (p *RemoteGatewayPlugin) Stop(ctx context.Context) error  { return nil }


// ---------------------------------------------------------------------------
// Gateway claim / release — lock management
// ---------------------------------------------------------------------------

// ClaimGateway attempts to register a machine as the gateway owner for this
// platform. Returns (true, "") on success, (false, reason) if already claimed.
func (p *RemoteGatewayPlugin) ClaimGateway(machineID, userID string) (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.owner != nil && p.owner.MachineID != machineID {
		log.Printf("[remote-gw/%s] claim DENIED: already held by machine=%s, requester=%s", p.platform, p.owner.MachineID, machineID)
		return false, fmt.Sprintf("gateway already held by machine %s (since %s)",
			p.owner.MachineID, p.owner.ClaimedAt.Format("15:04:05"))
	}
	p.owner = &gatewayOwner{
		MachineID: machineID,
		UserID:    userID,
		ClaimedAt: time.Now(),
	}
	log.Printf("[remote-gw/%s] gateway CLAIMED by machine=%s user=%s", p.platform, machineID, userID)
	// Note: cannot send diag here because sendToGatewayOwner requires the lock
	// which we already hold. The claim result is sent by the WS handler.
	return true, ""
}

// ReleaseGateway releases the gateway lock for the given machine.
func (p *RemoteGatewayPlugin) ReleaseGateway(machineID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.owner != nil && p.owner.MachineID == machineID {
		log.Printf("[remote-gw/%s] gateway released by machine=%s", p.platform, machineID)
		p.owner = nil
	}
}

// ReleaseAllForMachine releases any gateway held by the given machine.
// Called when a machine disconnects.
func (p *RemoteGatewayPlugin) ReleaseAllForMachine(machineID string) {
	p.ReleaseGateway(machineID)
}

// GatewayOwner returns the current owner machine ID, or "" if none.
func (p *RemoteGatewayPlugin) GatewayOwner() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.owner == nil {
		return ""
	}
	return p.owner.MachineID
}

// ---------------------------------------------------------------------------
// Inbound message handling — called when client forwards a QQ/TG message
// ---------------------------------------------------------------------------

// HandleGatewayMessage is called when a client sends "im.gateway_message".
// It converts the payload to IncomingMessage and dispatches to the IM Adapter.
func (p *RemoteGatewayPlugin) HandleGatewayMessage(machineID string, payload json.RawMessage) {
	p.mu.RLock()
	owner := p.owner
	handler := p.messageHandler
	p.mu.RUnlock()

	ownerID := ""
	if owner != nil {
		ownerID = owner.MachineID
	}
	log.Printf("[remote-gw/%s] HandleGatewayMessage: from_machine=%s owner=%s handler_nil=%v", p.platform, machineID, ownerID, handler == nil)

	if owner == nil || owner.MachineID != machineID {
		log.Printf("[remote-gw/%s] REJECTED: message from non-owner machine=%s (owner=%s)", p.platform, machineID, ownerID)
		return
	}
	if handler == nil {
		log.Printf("[remote-gw/%s] REJECTED: no message handler registered", p.platform)
		return
	}

	var msg struct {
		PlatformUID string `json:"platform_uid"`
		Text        string `json:"text"`
		MessageType string `json:"message_type"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[remote-gw/%s] parse gateway_message failed: %v", p.platform, err)
		return
	}

	log.Printf("[remote-gw/%s] dispatching: uid=%s type=%s text_len=%d", p.platform, msg.PlatformUID, msg.MessageType, len(msg.Text))

	// Check if this is a binding flow message (email or verify code).
	if p.handleBindingFlow(msg.PlatformUID, msg.Text) {
		return
	}

	msgType := msg.MessageType
	if msgType == "" {
		msgType = "text"
	}

	handler(IncomingMessage{
		PlatformName: p.platform,
		PlatformUID:  msg.PlatformUID,
		MessageType:  msgType,
		Text:         msg.Text,
		RawPayload:   payload,
		Timestamp:    time.Now(),
	})
}

// ---------------------------------------------------------------------------
// Send to gateway owner via WebSocket
// ---------------------------------------------------------------------------

func (p *RemoteGatewayPlugin) sendToGatewayOwner(replyType string, payload map[string]any) error {
	p.mu.RLock()
	owner := p.owner
	p.mu.RUnlock()
	if owner == nil {
		return fmt.Errorf("%s: no gateway owner", p.platform)
	}
	payload["reply_type"] = replyType
	// Embed platform inside the payload so the client can parse it from
	// msg.Payload (inboundHubEnvelope.Payload is the "payload" JSON field).
	payload["platform"] = p.platform
	return p.sender.SendToMachine(owner.MachineID, map[string]any{
		"type":     "im.gateway_reply",
		"platform": p.platform,
		"payload":  payload,
	})
}


// ---------------------------------------------------------------------------
// Email binding flow (same logic as qqbot plugin)
// ---------------------------------------------------------------------------

func (p *RemoteGatewayPlugin) handleBindingFlow(platformUID, text string) bool {
	if text == "/unbind" {
		p.handleUnbind(platformUID)
		return true
	}
	if looksLikeEmailAddr(text) {
		p.handleEmailSubmit(platformUID, text)
		return true
	}
	if isVerifyCodeStr(text) {
		p.pendingMu.Lock()
		pb, ok := p.pending[platformUID]
		p.pendingMu.Unlock()
		if ok {
			return p.handleVerifyCode(platformUID, text, pb)
		}
	}
	return false
}

func (p *RemoteGatewayPlugin) handleEmailSubmit(platformUID, email string) {
	// Check if already bound
	p.bindMu.RLock()
	existing := p.bindings[platformUID]
	p.bindMu.RUnlock()
	if existing != "" {
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			fmt.Sprintf("您已绑定邮箱 %s。如需更换，请先发送 /unbind 解绑。", existing))
		return
	}

	// Verify email exists in Hub
	user, err := p.users.GetByEmail(context.Background(), email)
	if err != nil || user == nil {
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			"该邮箱未在 Hub 注册，请检查后重试。")
		return
	}

	code := generateBindCode()
	p.pendingMu.Lock()
	p.pending[platformUID] = &pendingRemoteBind{
		Email:     email,
		Code:      code,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	p.pendingMu.Unlock()

	_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
		fmt.Sprintf("验证码已发送到 %s，请在 10 分钟内回复验证码完成绑定。\n\n（验证码: %s）", email, code))
}

func (p *RemoteGatewayPlugin) handleVerifyCode(platformUID, code string, pb *pendingRemoteBind) bool {
	p.pendingMu.Lock()
	if time.Now().After(pb.ExpiresAt) {
		delete(p.pending, platformUID)
		p.pendingMu.Unlock()
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			"验证码已过期，请重新发送邮箱地址。")
		return true
	}
	pb.Attempts++
	if pb.Attempts > 5 {
		delete(p.pending, platformUID)
		p.pendingMu.Unlock()
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			"验证次数过多，请重新发送邮箱地址。")
		return true
	}
	if code != pb.Code {
		p.pendingMu.Unlock()
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			"验证码不正确，请重试。")
		return true
	}
	// Code matches — remove pending entry
	email := pb.Email
	delete(p.pending, platformUID)
	p.pendingMu.Unlock()

	p.bindMu.Lock()
	p.bindings[platformUID] = email
	p.bindMu.Unlock()
	p.saveBindings()

	_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
		fmt.Sprintf("✅ 绑定成功！邮箱: %s\n\n现在可以直接发消息与 AI 助手对话了。\n输入 /help 查看可用命令。", email))
	return true
}

func (p *RemoteGatewayPlugin) handleUnbind(platformUID string) {
	p.bindMu.Lock()
	email, ok := p.bindings[platformUID]
	if ok {
		delete(p.bindings, platformUID)
	}
	p.bindMu.Unlock()
	if ok {
		p.saveBindings()
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			fmt.Sprintf("已解绑邮箱 %s。", email))
	} else {
		_ = p.SendText(context.Background(), UserTarget{PlatformUID: platformUID},
			"您尚未绑定邮箱。")
	}
}

// LookupByEmail returns the platformUID bound to the given email, or "".
func (p *RemoteGatewayPlugin) LookupByEmail(email string) string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	for uid, e := range p.bindings {
		if e == email {
			return uid
		}
	}
	return ""
}

// GetBindings returns a copy of the current bindings map.
func (p *RemoteGatewayPlugin) GetBindings() map[string]string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	out := make(map[string]string, len(p.bindings))
	for k, v := range p.bindings {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Persistence — store bindings in system_settings as JSON
// ---------------------------------------------------------------------------

func (p *RemoteGatewayPlugin) loadBindings() {
	key := fmt.Sprintf("im_%s_bindings", p.platform)
	raw, err := p.system.Get(context.Background(), key)
	if err != nil || raw == "" {
		return
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil {
		p.bindMu.Lock()
		p.bindings = m
		p.bindMu.Unlock()
	}
}

func (p *RemoteGatewayPlugin) saveBindings() {
	p.bindMu.RLock()
	data, _ := json.Marshal(p.bindings)
	p.bindMu.RUnlock()
	key := fmt.Sprintf("im_%s_bindings", p.platform)
	_ = p.system.Set(context.Background(), key, string(data))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func looksLikeEmailAddr(s string) bool {
	// Simple check: contains @ and a dot after @
	at := -1
	for i, c := range s {
		if c == '@' {
			at = i
		}
	}
	if at < 1 || at >= len(s)-1 {
		return false
	}
	for i := at + 1; i < len(s); i++ {
		if s[i] == '.' && i > at+1 && i < len(s)-1 {
			return true
		}
	}
	return false
}

func isVerifyCodeStr(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func generateBindCode() string {
	// Use crypto/rand for security
	var buf [3]byte
	_, _ = cryptoRandRead(buf[:])
	code := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
	return fmt.Sprintf("%06d", code%1000000)
}

// cryptoRandRead wraps crypto/rand.Read.
var cryptoRandRead = func(b []byte) (int, error) { return rand.Read(b) }
