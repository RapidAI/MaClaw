// Package commands 包含 TUI/CLI 的子命令实现。
package commands

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// HubClient 封装与 Hub 的 WebSocket 通信。
type HubClient struct {
	hubURL string
	token  string
	conn   *websocket.Conn
}

// Envelope 是 Hub WebSocket 消息信封。
type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// NewHubClient 创建 Hub 客户端。
func NewHubClient(hubURL, token string) *HubClient {
	return &HubClient{hubURL: hubURL, token: token}
}

// Connect 连接到 Hub 并完成认证。
func (c *HubClient) Connect() error {
	wsURL := strings.Replace(c.hubURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimRight(wsURL, "/")

	u, err := url.Parse(wsURL + "/ws/cli")
	if err != nil {
		return fmt.Errorf("invalid hub URL: %w", err)
	}
	q := u.Query()
	q.Set("token", c.token)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: websocket.DefaultDialer.HandshakeTimeout}
	if strings.HasPrefix(u.Scheme, "wss") {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("connect to Hub failed: %w", err)
	}
	c.conn = conn

	// 发送认证
	authPayload, _ := json.Marshal(map[string]string{"token": c.token})
	authMsg := Envelope{Type: "auth.cli", Payload: authPayload}
	if err := c.sendEnvelope(authMsg); err != nil {
		c.conn.Close()
		return fmt.Errorf("send auth: %w", err)
	}

	// 读取认证响应
	resp, err := c.readEnvelope()
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("read auth response: %w", err)
	}
	if resp.Type == "error" {
		c.conn.Close()
		return fmt.Errorf("auth failed: %s", string(resp.Payload))
	}
	return nil
}

// Close 关闭连接。
func (c *HubClient) Close() {
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
		c.conn = nil
	}
}

// Request 发送请求并读取响应。
func (c *HubClient) Request(msgType string, payload interface{}) (json.RawMessage, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqID := requestID()
	env := Envelope{Type: msgType, RequestID: reqID, Payload: p}
	if err := c.sendEnvelope(env); err != nil {
		return nil, err
	}
	resp, err := c.readEnvelope()
	if err != nil {
		return nil, err
	}
	if resp.Type == "error" {
		return nil, fmt.Errorf("hub error: %s", string(resp.Payload))
	}
	return resp.Payload, nil
}

// ReadRaw 读取一条原始消息。
func (c *HubClient) ReadRaw() (Envelope, error) {
	return c.readEnvelope()
}

// SendRaw 发送一条原始消息。
func (c *HubClient) SendRaw(env Envelope) error {
	return c.sendEnvelope(env)
}

func (c *HubClient) sendEnvelope(env Envelope) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteJSON(env)
}

func (c *HubClient) readEnvelope() (Envelope, error) {
	var env Envelope
	if c.conn == nil {
		return env, fmt.Errorf("not connected")
	}
	err := c.conn.ReadJSON(&env)
	return env, err
}

// timeNow 返回当前时间的 UnixNano，用于生成请求 ID。
func requestID() string {
	return fmt.Sprintf("cli-%d", time.Now().UnixNano())
}
