package chat

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ConnSender abstracts sending a JSON message to a WebSocket connection.
type ConnSender interface {
	SendJSON(v any) error
}

// PushDispatcher abstracts sending push notifications to offline users.
type PushDispatcher interface {
	SendPushForTenant(ctx context.Context, tenantID, userID, title, body string) error
}

// Notifier broadcasts WS hints to online users and dispatches push
// notifications to offline users.
type Notifier struct {
	store *Store
	push  PushDispatcher // nil = push disabled
	mu    sync.RWMutex
	conns map[string]ConnSender // userID → WS connection
}

// NewNotifier creates a Notifier.
func NewNotifier(store *Store, push PushDispatcher) *Notifier {
	return &Notifier{
		store: store,
		push:  push,
		conns: make(map[string]ConnSender),
	}
}

// Register adds a user's WS connection.
func (n *Notifier) Register(userID string, conn ConnSender) {
	n.RegisterForTenant(store.DefaultTenantID, userID, conn)
}

func (n *Notifier) RegisterForTenant(tenantID, userID string, conn ConnSender) {
	n.mu.Lock()
	n.conns[tenantUserKey(tenantID, userID)] = conn
	n.mu.Unlock()
	_ = n.store.SetPresenceForTenant(tenantID, userID, true)
}

// Unregister removes a user's WS connection.
func (n *Notifier) Unregister(userID string) {
	n.UnregisterForTenant(store.DefaultTenantID, userID)
}

func (n *Notifier) UnregisterForTenant(tenantID, userID string) {
	n.mu.Lock()
	delete(n.conns, tenantUserKey(tenantID, userID))
	n.mu.Unlock()
	_ = n.store.SetPresenceForTenant(tenantID, userID, false)
}

// IsOnline checks if a user has an active WS connection.
func (n *Notifier) IsOnline(userID string) bool {
	return n.IsOnlineForTenant(store.DefaultTenantID, userID)
}

func (n *Notifier) IsOnlineForTenant(tenantID, userID string) bool {
	n.mu.RLock()
	_, ok := n.conns[tenantUserKey(tenantID, userID)]
	n.mu.RUnlock()
	return ok
}

// NotifyNewMessage sends a "msg" hint to all channel members.
func (n *Notifier) NotifyNewMessage(channelID, senderID, msgID string, seq int64) {
	hint := WsHint{Type: "msg", ChannelID: channelID, Seq: seq}
	n.broadcastToChannel(channelID, senderID, hint, "New message")
}

// NotifyRecall sends a "recall" hint to all channel members.
func (n *Notifier) NotifyRecall(channelID, msgID string, seq int64) {
	hint := WsHint{Type: "recall", ChannelID: channelID, MsgID: msgID, Seq: seq}
	n.broadcastToChannel(channelID, "", hint, "")
}

// NotifyTyping sends a "typing" hint to channel members (online only, no push).
func (n *Notifier) NotifyTyping(channelID, userID string) {
	hint := WsHint{Type: "typing", ChannelID: channelID, UserID: userID, Expire: 3}
	members, err := n.store.GetMembers(channelID)
	if err != nil {
		return
	}
	// Only send typing to small channels.
	if len(members) > 20 {
		return
	}
	tenantID := store.DefaultTenantID
	if ch, err := n.store.GetChannel(channelID); err == nil && ch != nil {
		tenantID = store.NormalizeTenantID(ch.TenantID)
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, m := range members {
		if m.UserID == userID {
			continue
		}
		if conn, ok := n.conns[tenantUserKey(tenantID, m.UserID)]; ok {
			_ = conn.SendJSON(hint)
		}
	}
}

// NotifyCallEvent sends a voice call signaling hint.
func (n *Notifier) NotifyCallEvent(targetUserID string, hint WsHint, pushTitle, pushBody string) {
	n.NotifyCallEventForTenant(store.DefaultTenantID, targetUserID, hint, pushTitle, pushBody)
}

func (n *Notifier) NotifyCallEventForTenant(tenantID, targetUserID string, hint WsHint, pushTitle, pushBody string) {
	n.mu.RLock()
	conn, online := n.conns[tenantUserKey(tenantID, targetUserID)]
	n.mu.RUnlock()

	if online {
		_ = conn.SendJSON(hint)
	} else if n.push != nil && pushTitle != "" {
		if err := n.push.SendPushForTenant(context.Background(), tenantID, targetUserID, pushTitle, pushBody); err != nil {
			log.Printf("[chat/notifier] push to %s failed: %v", targetUserID, err)
		}
	}
}

// broadcastToChannel sends a hint to all members except excludeUID.
// Offline members get a push notification if pushBody is non-empty.
func (n *Notifier) broadcastToChannel(channelID, excludeUID string, hint WsHint, pushBody string) {
	members, err := n.store.GetMembers(channelID)
	if err != nil {
		log.Printf("[chat/notifier] get members for %s: %v", channelID, err)
		return
	}

	n.mu.RLock()
	defer n.mu.RUnlock()
	tenantID := store.DefaultTenantID
	if ch, err := n.store.GetChannel(channelID); err == nil && ch != nil {
		tenantID = store.NormalizeTenantID(ch.TenantID)
	}

	for _, m := range members {
		if m.UserID == excludeUID {
			continue
		}
		if conn, ok := n.conns[tenantUserKey(tenantID, m.UserID)]; ok {
			_ = conn.SendJSON(hint)
		} else if !m.Mute && n.push != nil && pushBody != "" {
			go func(tenantID, uid string) {
				if err := n.push.SendPushForTenant(context.Background(), tenantID, uid, "MaClaw Chat", pushBody); err != nil {
					log.Printf("[chat/notifier] push to %s failed: %v", uid, err)
				}
			}(tenantID, m.UserID)
		}
	}
}

// SendRaw sends an arbitrary JSON payload to a specific user's WS.
func (n *Notifier) SendRaw(userID string, payload any) bool {
	return n.SendRawForTenant(store.DefaultTenantID, userID, payload)
}

func (n *Notifier) SendRawForTenant(tenantID, userID string, payload any) bool {
	n.mu.RLock()
	conn, ok := n.conns[tenantUserKey(tenantID, userID)]
	n.mu.RUnlock()
	if !ok {
		return false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	_ = data // SendJSON handles marshaling internally
	return conn.SendJSON(payload) == nil
}

func tenantUserKey(tenantID, userID string) string {
	return store.NormalizeTenantID(tenantID) + "\x00" + strings.TrimSpace(userID)
}
