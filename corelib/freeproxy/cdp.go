package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// CDPClient manages a WebSocket connection to Chrome's DevTools Protocol.
type CDPClient struct {
	debugURL string // e.g. "http://localhost:9222"

	mu      sync.Mutex
	conn    *websocket.Conn
	writeMu sync.Mutex // serializes WebSocket writes
	nextID  atomic.Int64
	pending map[int64]chan json.RawMessage
	done    chan struct{} // closed when readLoop exits
	connTab string       // ID of the currently connected tab
}

type cdpEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewCDPClient creates a new CDP client connecting to Chrome at the given debug URL.
func NewCDPClient(debugURL string) *CDPClient {
	return &CDPClient{
		debugURL: strings.TrimRight(debugURL, "/"),
		pending:  make(map[int64]chan json.RawMessage),
	}
}

// TabInfo describes a Chrome tab.
type TabInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Type         string `json:"type"`
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

// ListTabs returns all open tabs from Chrome.
func (c *CDPClient) ListTabs(ctx context.Context) ([]TabInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.debugURL+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Chrome at %s: %w (is Chrome running with --remote-debugging-port=9222?)", c.debugURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tabs []TabInfo
	if err := json.Unmarshal(body, &tabs); err != nil {
		return nil, fmt.Errorf("parse tabs: %w", err)
	}
	return tabs, nil
}

// FindTabByDomain finds an existing tab whose URL contains the given domain.
func (c *CDPClient) FindTabByDomain(ctx context.Context, domain string) (*TabInfo, error) {
	tabs, err := c.ListTabs(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tabs {
		if t.Type == "page" && strings.Contains(t.URL, domain) {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("no tab found for domain %q — please open the website in Chrome first", domain)
}

// ConnectTab establishes a WebSocket connection to a specific tab.
// If already connected to the same tab, it reuses the existing connection.
func (c *CDPClient) ConnectTab(ctx context.Context, tab *TabInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Reuse existing connection if same tab
	if c.conn != nil && c.connTab == tab.ID {
		return nil
	}

	// Close previous connection if any
	c.closeLocked()

	wsURL := tab.WebSocketURL
	if wsURL == "" {
		return fmt.Errorf("tab %s has no WebSocket URL", tab.ID)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to tab: %w", err)
	}
	c.conn = conn
	c.connTab = tab.ID
	c.done = make(chan struct{})

	// Start reader goroutine
	go c.readLoop(conn, c.done)
	return nil
}

func (c *CDPClient) readLoop(conn *websocket.Conn, done chan struct{}) {
	defer close(done)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or error — cancel all pending requests
			c.mu.Lock()
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		// Try as response first
		var resp cdpResponse
		if json.Unmarshal(msg, &resp) == nil && resp.ID > 0 {
			c.mu.Lock()
			ch, ok := c.pending[resp.ID]
			if ok {
				delete(c.pending, resp.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- msg
			}
			continue
		}
	}
}

// Send sends a CDP command and waits for the response.
func (c *CDPClient) Send(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan json.RawMessage, 1)

	c.mu.Lock()
	c.pending[id] = ch
	conn := c.conn
	done := c.done
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	msg := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)

	// Serialize writes to the WebSocket connection
	c.writeMu.Lock()
	err := conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-done:
		// readLoop exited — connection lost
		return nil, fmt.Errorf("connection closed")
	case raw, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("connection closed")
		}
		var resp cdpResponse
		json.Unmarshal(raw, &resp)
		if resp.Error != nil {
			return nil, fmt.Errorf("CDP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Evaluate runs JavaScript in the tab and returns the result.
func (c *CDPClient) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	result, err := c.Send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	var evalResult struct {
		Result struct {
			Value json.RawMessage `json:"value"`
			Type  string          `json:"type"`
		} `json:"result"`
		ExceptionDetails *json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		return nil, err
	}
	if evalResult.ExceptionDetails != nil {
		return nil, fmt.Errorf("JS exception: %s", string(*evalResult.ExceptionDetails))
	}
	return evalResult.Result.Value, nil
}

// closeLocked closes the connection. Caller must hold c.mu.
func (c *CDPClient) closeLocked() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.connTab = ""
		// Wait for readLoop to finish (with timeout to avoid blocking)
		if c.done != nil {
			done := c.done
			c.mu.Unlock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			c.mu.Lock()
		}
	}
}

// Close closes the WebSocket connection.
func (c *CDPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}
