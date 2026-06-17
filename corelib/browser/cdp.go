// Package browser provides a lightweight CDP (Chrome DevTools Protocol) client
// for browser automation. It connects to Chrome/Edge via the remote debugging port
// and exposes high-level operations (navigate, click, type, screenshot, etc.).
// Zero external dependencies beyond gorilla/websocket (already in go.mod).
package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// CDPClient is a low-level Chrome DevTools Protocol client over WebSocket.
type CDPClient struct {
	conn            *websocket.Conn
	mu              sync.Mutex
	nextID          atomic.Int64
	pending         map[int64]chan json.RawMessage
	pendMu          sync.Mutex
	events          chan CDPEvent
	closed          chan struct{}
	closeErr        error
	stopPing        chan struct{} // signals the keepalive goroutine to stop
	closeOnce       sync.Once    // ensures closed channel is closed exactly once
	closeEventsOnce sync.Once    // ensures events channel is closed exactly once
}

// CDPEvent is a CDP event pushed by the browser.
type CDPEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// TargetDomainEvent is a lightweight structured Target.* event.
type TargetDomainEvent struct {
	TargetInfo *TargetInfo `json:"target_info,omitempty"`
	TargetID   string      `json:"target_id,omitempty"`
	Type       string      `json:"type,omitempty"`
}

// NetworkDomainEvent is a lightweight structured Network.* event.
type NetworkDomainEvent struct {
	RequestID string `json:"request_id,omitempty"`
	Method    string `json:"method,omitempty"`
	URL       string `json:"url,omitempty"`
	Status    int    `json:"status,omitempty"`
}

// ConsoleDomainEvent is a lightweight structured Runtime console event.
type ConsoleDomainEvent struct {
	Type string   `json:"type,omitempty"`
	Text []string `json:"text,omitempty"`
}

// cdpError holds a CDP protocol error.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cdpMessage is used for initial parsing to distinguish responses from events.
type cdpMessage struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
	Params json.RawMessage `json:"params"`
}

// DiscoverTargets queries the /json endpoint to find debuggable page targets.
func DiscoverTargets(cdpHTTP string) ([]TargetInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(cdpHTTP + "/json")
	if err != nil {
		return nil, fmt.Errorf("discover targets: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover targets: unexpected HTTP %s body_len=%d", resp.Status, len(body))
	}
	var targets []TargetInfo
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("parse targets: %w body_len=%d", err, len(body))
	}
	return targets, nil
}

// TargetInfo describes a browser target (page, worker, etc.).
type TargetInfo struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	Title             string `json:"title"`
	URL               string `json:"url"`
	WebSocketDebugURL string `json:"webSocketDebuggerUrl"`
}

// ConnectCDP connects to a CDP WebSocket endpoint.
func ConnectCDP(wsURL string) (*CDPClient, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp dial: %w", err)
	}
	c := &CDPClient{
		conn:     conn,
		pending:  make(map[int64]chan json.RawMessage),
		events:   make(chan CDPEvent, 64),
		closed:   make(chan struct{}),
		stopPing: make(chan struct{}),
	}
	go c.readLoop()
	go c.keepAlive()
	return c, nil
}

// IsAlive checks if the CDP connection is still functional by sending a
// lightweight Browser.getVersion command with a short timeout.
func (c *CDPClient) IsAlive() bool {
	select {
	case <-c.closed:
		return false
	default:
	}
	_, err := c.Send("Browser.getVersion", nil, 2*time.Second)
	return err == nil
}

// keepAlive sends periodic WebSocket pings to prevent idle disconnection.
// Runs until the connection is closed or stopPing is signalled.
func (c *CDPClient) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			err := c.conn.WriteControl(
				websocket.PingMessage, nil,
				time.Now().Add(5*time.Second),
			)
			c.mu.Unlock()
			if err != nil {
				// Ping failed — connection is dead, let readLoop handle cleanup.
				return
			}
		case <-c.stopPing:
			return
		case <-c.closed:
			return
		}
	}
}

// Send sends a CDP command and waits for the response (up to timeout).
func (c *CDPClient) Send(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	// Early exit if connection is already closed.
	select {
	case <-c.closed:
		return nil, fmt.Errorf("cdp connection closed")
	default:
	}

	id := c.nextID.Add(1)

	msg := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		msg["params"] = params
	}

	ch := make(chan json.RawMessage, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	defer func() {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
	}()

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal cdp: %w", err)
	}

	c.mu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write cdp: %w", err)
	}

	select {
	case result := <-ch:
		// Check for CDP protocol error encoded by readLoop.
		if isCDPProtocolError(result) {
			return nil, parseCDPProtocolError(result)
		}
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("cdp timeout: %s (id=%d)", method, id)
	case <-c.closed:
		return nil, fmt.Errorf("cdp connection closed")
	}
}

// isCDPProtocolError checks if a result from readLoop is a CDP protocol error.
func isCDPProtocolError(result json.RawMessage) bool {
	// Fast check: sentinel marker must be in the first portion of the JSON.
	n := len(result)
	if n < 20 {
		return false
	}
	if n > 40 {
		n = 40
	}
	return bytes.Contains(result[:n], []byte(`"__cdp_error__"`))
}

// parseCDPProtocolError extracts the error message from a CDP protocol error.
func parseCDPProtocolError(result json.RawMessage) error {
	var payload struct {
		Error string `json:"error"`
		Code  int    `json:"code"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return fmt.Errorf("cdp protocol error (unparsable)")
	}
	return fmt.Errorf("cdp error %d: %s", payload.Code, payload.Error)
}

// Events returns the channel for receiving CDP events.
func (c *CDPClient) Events() <-chan CDPEvent {
	return c.events
}

// Close closes the WebSocket connection.
func (c *CDPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return c.closeErr
	default:
	}
	// Stop keepalive goroutine first.
	select {
	case <-c.stopPing:
	default:
		close(c.stopPing)
	}
	c.closeErr = c.conn.Close()
	log.Printf("[browser] CDP websocket close err=%v", c.closeErr)
	c.closeOnce.Do(func() { close(c.closed) })
	return c.closeErr
}

func parseTargetDomainEvent(method string, params json.RawMessage) *TargetDomainEvent {
	if params == nil {
		return nil
	}
	var payload struct {
		TargetInfo *TargetInfo `json:"targetInfo,omitempty"`
		TargetID   string      `json:"targetId,omitempty"`
		Type       string      `json:"type,omitempty"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	return &TargetDomainEvent{TargetInfo: payload.TargetInfo, TargetID: payload.TargetID, Type: payload.Type}
}

func parseNetworkDomainEvent(method string, params json.RawMessage) *NetworkDomainEvent {
	if params == nil {
		return nil
	}
	var payload struct {
		RequestID string `json:"requestId,omitempty"`
		Request   struct {
			Method string `json:"method,omitempty"`
			URL    string `json:"url,omitempty"`
		} `json:"request,omitempty"`
		Response struct {
			Status int `json:"status,omitempty"`
		} `json:"response,omitempty"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	return &NetworkDomainEvent{RequestID: payload.RequestID, Method: payload.Request.Method, URL: payload.Request.URL, Status: payload.Response.Status}
}

func parseConsoleDomainEvent(method string, params json.RawMessage) *ConsoleDomainEvent {
	if params == nil {
		return nil
	}
	var payload struct {
		Type string `json:"type,omitempty"`
		Args []struct {
			Value interface{} `json:"value,omitempty"`
		} `json:"args,omitempty"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	out := &ConsoleDomainEvent{Type: payload.Type, Text: []string{}}
	for _, arg := range payload.Args {
		out.Text = append(out.Text, fmt.Sprint(arg.Value))
	}
	return out
}

func (c *CDPClient) readLoop() {
	defer func() {
		// Signal closure. closeOnce guarantees exactly-once semantics even
		// if Close() and readLoop race (conn.Close() can cause ReadMessage
		// to error out and readLoop to exit its defer concurrently with
		// Close() executing close(c.closed)).
		c.closeOnce.Do(func() { close(c.closed) })
		// Close events channel so any goroutine selecting on Events() will
		// unblock and observe ok=false. This prevents event pump goroutine
		// leaks when the CDP connection dies and a new one is established.
		c.closeEventsOnce.Do(func() { close(c.events) })
	}()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[CDP] bad message: %v", err)
			continue
		}
		if msg.ID > 0 {
			// Response to a command.
			c.pendMu.Lock()
			ch, ok := c.pending[msg.ID]
			c.pendMu.Unlock()
			if ok {
				if msg.Error != nil {
					// CDP protocol error: encode as a sentinel JSON value that
					// Send() can detect and convert to a Go error. The prefix
					// allows Send() to distinguish errors from valid results.
					errJSON, _ := json.Marshal(map[string]interface{}{
						"__cdp_error__": true,
						"error":         msg.Error.Message,
						"code":          msg.Error.Code,
					})
					ch <- json.RawMessage(errJSON)
				} else {
					ch <- msg.Result
				}
			}
		} else if msg.Method != "" {
			// Event.
			_ = parseTargetDomainEvent(msg.Method, msg.Params)
			_ = parseNetworkDomainEvent(msg.Method, msg.Params)
			_ = parseConsoleDomainEvent(msg.Method, msg.Params)
			select {
			case c.events <- CDPEvent{Method: msg.Method, Params: msg.Params}:
			default:
				// Drop if buffer full.
			}
		}
	}
}
