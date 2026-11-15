package edgelink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	EnrollPath = "/api/v1/edge/enroll"
	LinkPath   = "/api/v1/edge/link"
)

// Credentials is what one enrolled node keeps. NodeSecret opens the link.
// HubToken is the tenant-locked desktop API token for a local MaClawSrv.
type Credentials struct {
	HubURL     string `json:"hub_url"`
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
	HubToken   string `json:"hub_token"`
	// TenantID is the tenant this desktop is bound to after enrollment.
	// Empty means the process is not registered and still serves every tenant.
	TenantID string `json:"tenant_id,omitempty"`
	// SrvAddr is the local MaClawSrv this node forwards to. It is not a secret.
	SrvAddr string `json:"srv_addr,omitempty"`
	// DirectBase is the intranet address a GUI on that network can open.
	// Empty means pictures stay on the Hub address.
	DirectBase string `json:"direct_base,omitempty"`
}

// EnrollRequest is the node side of one enrollment. The code is one-time.
type EnrollRequest struct {
	Code        string
	DesktopdKey string
	SrvAdmin    string
	Version     string
	HasDesktopd bool
	HasSrv      bool
	// DirectBase is the intranet address a GUI on that network can probe.
	DirectBase string
	// HostRAMBytes and HostCPUs describe the machine desktopd is running on.
	// Zero leaves the desktop size unchanged.
	HostRAMBytes uint64
	HostCPUs     int
}

// NormalizeHubURL keeps scheme, host and path; it drops query and fragment.
func NormalizeHubURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("hub url must be an http:// or https:// address")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// Enroll trades a one-time code for node credentials.
func Enroll(ctx context.Context, client *http.Client, hubURL string, in EnrollRequest) (Credentials, error) {
	hub, err := NormalizeHubURL(hubURL)
	if err != nil {
		return Credentials{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := json.Marshal(map[string]any{
		"code":             strings.TrimSpace(in.Code),
		"desktopd_key":     in.DesktopdKey,
		"srv_admin_secret": in.SrvAdmin,
		"version":          in.Version,
		"has_desktopd":     in.HasDesktopd,
		"has_srv":          in.HasSrv,
		"direct_base":      strings.TrimSpace(in.DirectBase),
		"host_ram_bytes":   in.HostRAMBytes,
		"host_cpus":        in.HostCPUs,
	})
	if err != nil {
		return Credentials{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hub+EnrollPath, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var shaped struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(raw, &shaped) == nil {
			if shaped.Message != "" {
				msg = shaped.Message
			} else if shaped.Error != "" {
				msg = shaped.Error
			}
		}
		if msg == "" {
			msg = resp.Status
		}
		return Credentials{}, fmt.Errorf("enrollment failed: %s", msg)
	}
	var out Credentials
	if err := json.Unmarshal(raw, &out); err != nil {
		return Credentials{}, err
	}
	if out.NodeID == "" || out.NodeSecret == "" || (in.HasSrv && out.HubToken == "") {
		return Credentials{}, errors.New("enrollment response is missing a secret")
	}
	out.HubURL = hub
	return out, nil
}

// LinkURL is the websocket address of the node link on hub.
func LinkURL(hub string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(hub))
	if err != nil || u.Host == "" {
		return "", errors.New("hub url is invalid")
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", errors.New("hub url must start with http:// or https://")
	}
	u.Path = strings.TrimRight(u.Path, "/") + LinkPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// ServeLink holds one link until it ends. onUp runs after the websocket opens.
func ServeLink(ctx context.Context, link, nodeID, secret string, routes map[string]string, onUp func()) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+secret)
	header.Set("X-Node-Id", nodeID)
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 20 * time.Second}
	ws, _, err := dialer.DialContext(ctx, link, header)
	if err != nil {
		return err
	}
	defer ws.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-done:
		}
	}()
	if onUp != nil {
		onUp()
	}
	err = Serve(NewWSConn(ws), routes)
	close(done)
	return err
}

// RunLink reconnects until ctx ends. state is told about every up and down.
func RunLink(ctx context.Context, creds Credentials, routes map[string]string, state func(up bool, err error)) error {
	link, err := LinkURL(creds.HubURL)
	if err != nil {
		return err
	}
	wait := time.Duration(0)
	for {
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		up := false
		err := ServeLink(ctx, link, creds.NodeID, creds.NodeSecret, routes, func() {
			up = true
			if state != nil {
				state(true, nil)
			}
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state != nil {
			state(false, err)
		}
		wait = nextLinkWait(wait, up)
	}
}

// nextLinkWait is the reconnect delay after one link attempt. A connection
// that came up starts again at 2s. A failed attempt doubles, and never waits
// longer than 30s.
func nextLinkWait(prev time.Duration, connected bool) time.Duration {
	const (
		start = 2 * time.Second
		limit = 30 * time.Second
	)
	if connected || prev < start {
		return start
	}
	next := prev * 2
	if next > limit {
		return limit
	}
	return next
}

// LoadCredentials reads a state file. A missing file is an empty result.
func LoadCredentials(path string) (Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, nil
		}
		return Credentials{}, err
	}
	var out Credentials
	if err := json.Unmarshal(raw, &out); err != nil {
		return Credentials{}, err
	}
	return out, nil
}

// SaveCredentials writes path atomically with mode 0600.
func SaveCredentials(path string, creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WSConn adapts a websocket to a byte stream of binary messages for yamux.
type WSConn struct {
	ws   *websocket.Conn
	rmu  sync.Mutex
	wmu  sync.Mutex
	cur  io.Reader
	once sync.Once
}

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
