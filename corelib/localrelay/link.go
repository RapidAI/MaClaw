package localrelay

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

// LinkPath is the GUI's WebSocket on Hub.
const LinkPath = "/api/v1/bots/relay/link"

// WSConn adapts a WebSocket to a byte stream (binary messages) for yamux.
type WSConn struct {
	ws   *websocket.Conn
	rmu  sync.Mutex
	wmu  sync.Mutex
	cur  io.Reader
	once sync.Once
}

// NewWSConn wraps ws.
func NewWSConn(ws *websocket.Conn) *WSConn { return &WSConn{ws: ws} }

func (c *WSConn) Read(b []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for {
		if c.cur == nil {
			kind, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				continue
			}
			c.cur = r
		}
		n, err := c.cur.Read(b)
		if errors.Is(err, io.EOF) {
			c.cur = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *WSConn) Write(b []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *WSConn) Close() error {
	var err error
	c.once.Do(func() {
		c.wmu.Lock()
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		c.wmu.Unlock()
		err = c.ws.Close()
	})
	return err
}

func (c *WSConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *WSConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }
func (c *WSConn) SetDeadline(t time.Time) error {
	_ = c.ws.SetReadDeadline(t)
	return c.ws.SetWriteDeadline(t)
}
func (c *WSConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *WSConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

// YamuxConfig is shared by both ends. Keepalives every 20 s keep proxies in
// front of Hub from closing an idle link and notice a dead one in ~1 min.
func YamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.StreamOpenTimeout = 30 * time.Second
	cfg.MaxStreamWindowSize = 1 << 20
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)
	return cfg
}

// ServeGUI runs the person's side of one link until it ends: Hub opens
// streams, the Agent answers each one.
func (a *Agent) ServeGUI(ctx context.Context, conn net.Conn) error {
	cfg := YamuxConfig()
	sess, err := yamux.Server(conn, cfg)
	if err != nil {
		return err
	}
	defer sess.Close()
	go func() {
		select {
		case <-ctx.Done():
			sess.Close()
		case <-sess.CloseChan():
		}
	}()
	for {
		st, err := sess.AcceptStream()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go a.ServeStream(ctx, st)
	}
}
