package localrelay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Grant kinds.
const (
	KindToCloud = "to_cloud" // a local file into the cloud desktop
	KindToLocal = "to_local" // a cloud file onto the person's computer
	KindForward = "forward"  // container 127.0.0.1:port -> one LAN host:port
	KindSocks   = "socks"    // container SOCKS5 -> any approved LAN host:port
)

// ConnectPath is where the container helper connects with a grant token.
const ConnectPath = "/api/v1/desktop-services/relay/connect"

// TransferGrantTTL is how long a transfer token waits for the container.
const TransferGrantTTL = 15 * time.Minute

// TunnelMaxLife caps one tunnel even when it is busy.
const TunnelMaxLife = 24 * time.Hour

// UserKey names one person's link: tenant and owner user id.
func UserKey(tenant, user string) string {
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(user)
}

// Grant is one token the cloud desktop may present to Hub.
type Grant struct {
	Token      string    `json:"-"`
	ID         string    `json:"id"`
	Key        string    `json:"-"`
	BotID      string    `json:"bot_id"`
	BotName    string    `json:"bot_name,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
	Kind       string    `json:"kind"`
	LocalPath  string    `json:"local_path,omitempty"`
	Name       string    `json:"name,omitempty"`
	Remote     string    `json:"remote,omitempty"`
	Size       int64     `json:"size,omitempty"`
	MaxSize    int64     `json:"max_size,omitempty"`
	Host       string    `json:"host,omitempty"`
	Port       int       `json:"port,omitempty"`
	Created    time.Time `json:"created"`
	Expires    time.Time `json:"expires"`
	LastUsed   time.Time `json:"last_used"`
	Idle       time.Duration
	IdleMin    int    `json:"idle_minutes,omitempty"`
	Uses       int    `json:"uses"`
	MaxUses    int    `json:"-"`
	Active     int    `json:"active"`
	BytesUp    int64  `json:"bytes_to_lan"`
	BytesDown  int64  `json:"bytes_from_lan"`
	LastTarget string `json:"last_target,omitempty"`
}

type hubLink struct {
	sess    *yamux.Session
	machine string
	since   time.Time
}

// Broker is Hub's side: the person's links and the grants minted for their
// bots. Everything is in memory; a Hub restart drops grants, and the bot is
// told to open the tunnel again.
type Broker struct {
	mu     sync.Mutex
	links  map[string]*hubLink
	grants map[string]*Grant
	// Now is replaced by tests.
	Now func() time.Time
	// OpenTimeout bounds waiting for the GUI's first answer, which includes
	// the person reading a question. Default 10 minutes.
	OpenTimeout time.Duration
}

func (b *Broker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Attach runs one GUI link until it ends. A newer link of the same person
// replaces the older one (the person reopened the app or switched computer).
func (b *Broker) Attach(ctx context.Context, key, machine string, conn net.Conn) error {
	sess, err := yamux.Client(conn, YamuxConfig())
	if err != nil {
		conn.Close()
		return err
	}
	link := &hubLink{sess: sess, machine: machine, since: b.now()}
	b.mu.Lock()
	if b.links == nil {
		b.links = map[string]*hubLink{}
	}
	old := b.links[key]
	b.links[key] = link
	b.mu.Unlock()
	if old != nil {
		old.sess.Close()
	}
	select {
	case <-ctx.Done():
	case <-sess.CloseChan():
	}
	sess.Close()
	b.mu.Lock()
	if b.links[key] == link {
		delete(b.links, key)
	}
	b.mu.Unlock()
	return nil
}

// Online reports the person's link.
func (b *Broker) Online(key string) (bool, string, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.links[key]
	if l == nil || l.sess.IsClosed() {
		return false, "", time.Time{}
	}
	return true, l.machine, l.since
}

// ErrOffline is returned while the person's MaClaw app is not connected.
var ErrOffline = &Err{Code: CodeOffline, Message: "the MaClaw app on the person's computer is not connected to the server right now (closed, asleep, offline, or an older version without the local relay), so nothing on that computer can be reached; ask the person to open MaClaw on that computer"}

// Open sends req on a new stream and waits for the GUI's answer. On success
// the stream is returned for the bytes that follow.
func (b *Broker) Open(ctx context.Context, key string, req Request) (net.Conn, Response, error) {
	b.mu.Lock()
	l := b.links[key]
	b.mu.Unlock()
	if l == nil || l.sess.IsClosed() {
		return nil, Response{}, ErrOffline
	}
	st, err := l.sess.OpenStream()
	if err != nil {
		return nil, Response{}, ErrOffline
	}
	if req.ID == "" {
		req.ID = newID()
	}
	wait := b.OpenTimeout
	if wait <= 0 {
		wait = 10 * time.Minute
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = st.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { st.Close() })
	defer stop()
	if err := WriteLine(st, req); err != nil {
		st.Close()
		return nil, Response{}, &Err{Code: CodeFailed, Message: "the link to the person's computer broke: " + err.Error()}
	}
	var resp Response
	if err := ReadLine(st, &resp); err != nil {
		st.Close()
		if ctx.Err() != nil || isTimeout(err) {
			return nil, Response{}, &Err{Code: CodeTimeout, Message: "the person's computer did not answer in time"}
		}
		return nil, Response{}, &Err{Code: CodeFailed, Message: "the link to the person's computer broke before it answered"}
	}
	if err := resp.AsErr(); err != nil {
		st.Close()
		return nil, resp, err
	}
	_ = st.SetDeadline(time.Time{})
	return st, resp, nil
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

// Call is Open for requests without a body.
func (b *Broker) Call(ctx context.Context, key string, req Request) (Response, error) {
	st, resp, err := b.Open(ctx, key, req)
	if st != nil {
		st.Close()
	}
	return resp, err
}

func newToken() string {
	var raw [32]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// Mint stores g with a fresh token and id and returns it (Token set).
func (b *Broker) Mint(g Grant) Grant {
	now := b.now()
	g.Token = newToken()
	g.ID = "lr_" + newID()[:10]
	g.Created, g.LastUsed = now, now
	switch g.Kind {
	case KindToCloud, KindToLocal:
		g.MaxUses = 1
		if g.Expires.IsZero() {
			g.Expires = now.Add(TransferGrantTTL)
		}
	default:
		if g.Idle <= 0 {
			g.Idle = time.Duration(DefaultTunnelIdleMinutes) * time.Minute
		}
		g.IdleMin = int(g.Idle / time.Minute)
		if g.Expires.IsZero() {
			g.Expires = now.Add(TunnelMaxLife)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	if b.grants == nil {
		b.grants = map[string]*Grant{}
	}
	cp := g
	b.grants[g.Token] = &cp
	return g
}

func (b *Broker) sweepLocked(now time.Time) {
	for tok, g := range b.grants {
		if g.Active == 0 && b.deadLocked(g, now) != "" {
			delete(b.grants, tok)
		}
	}
}

func (b *Broker) deadLocked(g *Grant, now time.Time) string {
	if now.After(g.Expires) {
		return "expired"
	}
	if g.MaxUses > 0 && g.Uses >= g.MaxUses {
		return "used"
	}
	if g.Idle > 0 && g.Active == 0 && now.Sub(g.LastUsed) > g.Idle {
		return "idle"
	}
	return ""
}

// use claims one connection on a grant.
func (b *Broker) use(token string) (Grant, error) {
	token = strings.TrimSpace(token)
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	var g *Grant
	for tok, cand := range b.grants {
		if len(tok) == len(token) && subtle.ConstantTimeCompare([]byte(tok), []byte(token)) == 1 {
			g = cand
			break
		}
	}
	if g == nil || token == "" {
		return Grant{}, &Err{Code: CodeExpired, Message: "relay token is unknown or was closed (the server may have restarted); open it again"}
	}
	if why := b.deadLocked(g, now); why != "" {
		delete(b.grants, g.Token)
		return Grant{}, &Err{Code: CodeExpired, Message: "relay token is " + why + "; open it again"}
	}
	g.Uses++
	g.Active++
	g.LastUsed = now
	return *g, nil
}

func (b *Broker) release(token string, up, down int64, target string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if g := b.grants[token]; g != nil {
		g.Active--
		if g.Active < 0 {
			g.Active = 0
		}
		g.BytesUp += up
		g.BytesDown += down
		g.LastUsed = b.now()
		if target != "" {
			g.LastTarget = target
		}
		if g.MaxUses > 0 && g.Uses >= g.MaxUses && g.Active == 0 {
			delete(b.grants, token)
		}
	}
}

// Revoke drops one grant of this person by id (or token). It reports
// whether one was found.
func (b *Broker) Revoke(key, id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for tok, g := range b.grants {
		if g.Key == key && (g.ID == id || tok == id) {
			delete(b.grants, tok)
			return true
		}
	}
	return false
}

// List returns this person's live grants (one bot when botID is set).
func (b *Broker) List(key, botID string) []Grant {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	var out []Grant
	for _, g := range b.grants {
		if g.Key != key || (botID != "" && g.BotID != botID) {
			continue
		}
		cp := *g
		cp.Token = ""
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

type connectErr struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func writeConnectErr(w http.ResponseWriter, err error) {
	code := CodeOf(err)
	status := http.StatusBadGateway
	switch code {
	case CodeExpired:
		status = http.StatusGone
	case CodeDenied, CodeDisabled:
		status = http.StatusForbidden
	case CodeInvalid, CodeTooLarge:
		status = http.StatusBadRequest
	case CodeNotFound:
		status = http.StatusNotFound
	case CodeTimeout:
		status = http.StatusGatewayTimeout
	case CodeOffline:
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(connectErr{Code: code, Error: err.Error()})
}

type bufConn struct {
	net.Conn
	r io.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *bufConn) CloseWrite() error {
	if hc, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return hc.CloseWrite()
	}
	return nil
}

// ServeConnect is the container helper's endpoint. The request carries
// X-Relay-Token (and X-Relay-Target for SOCKS, X-Relay-Size for a file sent
// to the person's computer) and asks to upgrade to "maclaw-relay". After the
// GUI accepts, Hub answers 101 with X-Relay-Info (base64 JSON Response) and
// the connection carries the bytes.
func (b *Broker) ServeConnect(w http.ResponseWriter, r *http.Request) {
	g, err := b.use(r.Header.Get("X-Relay-Token"))
	if err != nil {
		writeConnectErr(w, err)
		return
	}
	var up, down int64
	target := ""
	defer func() { b.release(g.Token, up, down, target) }()
	req := Request{BotID: g.BotID, BotName: g.BotName, RunID: g.RunID, Remote: g.Remote}
	switch g.Kind {
	case KindToCloud:
		req.Op, req.Path = OpRead, g.LocalPath
	case KindToLocal:
		size, perr := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Relay-Size")), 10, 64)
		if perr != nil || size < 0 {
			writeConnectErr(w, &Err{Code: CodeInvalid, Message: "X-Relay-Size is required"})
			return
		}
		if g.MaxSize > 0 && size > g.MaxSize {
			writeConnectErr(w, &Err{Code: CodeTooLarge, Message: fmt.Sprintf("the file is %s, above the %s limit", HumanSize(size), HumanSize(g.MaxSize))})
			return
		}
		req.Op, req.Path, req.Name, req.Size = OpWrite, g.LocalPath, g.Name, size
	case KindForward:
		req.Op, req.Host, req.Port = OpDial, g.Host, g.Port
	case KindSocks:
		host, portText, serr := net.SplitHostPort(strings.TrimSpace(r.Header.Get("X-Relay-Target")))
		port, perr := strconv.Atoi(portText)
		if serr != nil || perr != nil {
			writeConnectErr(w, &Err{Code: CodeInvalid, Message: "X-Relay-Target host:port is required"})
			return
		}
		req.Op, req.Host, req.Port = OpDial, host, port
	default:
		writeConnectErr(w, &Err{Code: CodeInvalid, Message: "unknown grant"})
		return
	}
	if req.Host != "" {
		target = net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeConnectErr(w, &Err{Code: CodeFailed, Message: "this server cannot upgrade the connection"})
		return
	}
	st, resp, err := b.Open(r.Context(), g.Key, req)
	if err != nil {
		writeConnectErr(w, err)
		return
	}
	defer st.Close()
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{})
	info, _ := json.Marshal(resp)
	head := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: maclaw-relay\r\nConnection: Upgrade\r\nX-Relay-Info: " + base64.StdEncoding.EncodeToString(info) + "\r\n\r\n"
	if _, err := io.WriteString(conn, head); err != nil {
		return
	}
	client := &bufConn{Conn: conn, r: rw.Reader}
	switch req.Op {
	case OpRead:
		down, _ = io.CopyN(client, st, resp.Size)
	case OpWrite:
		up, _ = io.CopyN(st, client, req.Size)
		// FIN tells the GUI the bytes ended, short or not; reads stay open.
		closeWrite(st)
		var final Response
		_ = st.SetReadDeadline(time.Now().Add(2 * time.Minute))
		if err := ReadLine(st, &final); err != nil {
			final = Response{Code: CodeFailed, Error: "the person's computer did not confirm the saved file"}
		}
		_ = WriteLine(client, final)
	case OpDial:
		up, down = Pipe(client, st)
	}
}
