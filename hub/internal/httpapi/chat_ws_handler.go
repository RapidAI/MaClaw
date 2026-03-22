package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/gorilla/websocket"
)

// chatWSConn wraps a gorilla websocket.Conn to implement chat.ConnSender.
// gorilla/websocket WriteJSON is not concurrency-safe, so we protect with a mutex.
type chatWSConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *chatWSConn) SendJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteJSON(v)
}

var chatUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ChatWSHandler returns an http.HandlerFunc for the chat WebSocket endpoint.
// Protocol:
//  1. Client connects to /api/chat/ws
//  2. Client sends auth frame: {"type":"auth","token":"<bearer_token>"}
//  3. Server responds: {"type":"auth_ok","user_id":"..."}  or  {"type":"auth_fail"}
//  4. Server pushes WsHint frames; client sends ping/typing frames.
func ChatWSHandler(identity *auth.IdentityService, notifier *chat.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := chatUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[chat/ws] upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Auth timeout.
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// 1. Read auth frame.
		var authFrame struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		if err := conn.ReadJSON(&authFrame); err != nil {
			return
		}
		if authFrame.Type != "auth" || authFrame.Token == "" {
			_ = conn.WriteJSON(map[string]string{"type": "auth_fail"})
			return
		}

		// Validate token.
		token := strings.TrimSpace(authFrame.Token)
		vp, err := identity.AuthenticateViewer(r.Context(), token)
		if err != nil {
			_ = conn.WriteJSON(map[string]string{"type": "auth_fail"})
			return
		}

		// Auth OK.
		_ = conn.WriteJSON(map[string]any{"type": "auth_ok", "user_id": vp.UserID})

		// Register with notifier.
		sender := &chatWSConn{ws: conn}
		notifier.Register(vp.UserID, sender)
		defer notifier.Unregister(vp.UserID)

		log.Printf("[chat/ws] user %s connected", vp.UserID)

		// Ping/pong keepalive.
		const pongWait = 90 * time.Second
		conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		// Read loop: handle client frames.
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var peek struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &peek) != nil {
				continue
			}
			switch peek.Type {
			case "ping":
				_ = conn.WriteJSON(map[string]string{"type": "pong"})
			case "typing":
				var t struct {
					ChannelID string `json:"channel_id"`
				}
				_ = json.Unmarshal(raw, &t)
				if t.ChannelID != "" {
					notifier.NotifyTyping(t.ChannelID, vp.UserID)
				}
			}
		}

		log.Printf("[chat/ws] user %s disconnected", vp.UserID)
	}
}
