package mailbox

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	imapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// NetDialer talks to the account's own IMAP and SMTP servers.
// Tests inject a Dialer instead of using this type, so they do not touch the network.
type NetDialer struct {
	Timeout time.Duration
}

func NewNetDialer() *NetDialer {
	return &NetDialer{Timeout: ToolConnectTimeout}
}

func (d *NetDialer) timeout() time.Duration {
	if d != nil && d.Timeout > 0 {
		return d.Timeout
	}
	return ToolConnectTimeout
}

func (d *NetDialer) DialInbound(ctx context.Context, ep Endpoint) (InboundSession, error) {
	c, err := d.dialIMAP(ctx, ep)
	if err != nil {
		return nil, err
	}
	return &imapProbe{c: c}, nil
}

func (d *NetDialer) DialOutbound(ctx context.Context, ep Endpoint) (OutboundSession, error) {
	c, err := d.dialSMTP(ctx, ep)
	if err != nil {
		return nil, err
	}
	return &smtpProbe{c: c}, nil
}

func (d *NetDialer) dialIMAP(ctx context.Context, ep Endpoint) (*imapclient.Client, error) {
	conn, err := d.dialConn(ctx, ep, true)
	if err != nil {
		return nil, err
	}
	c, err := imapclient.New(conn)
	if err != nil {
		_ = conn.Close()
		return nil, mapIMAP(err, "connect")
	}
	if ep.Security == SecuritySTARTTLS {
		if err := c.StartTLS(&tls.Config{ServerName: tlsServerName(ep.Host)}); err != nil {
			_ = c.Logout()
			return nil, mapIMAP(err, "tls")
		}
	}
	return c, nil
}

func (d *NetDialer) dialSMTP(ctx context.Context, ep Endpoint) (*smtp.Client, error) {
	conn, err := d.dialConn(ctx, ep, false)
	if err != nil {
		return nil, err
	}
	if ep.Security == SecuritySTARTTLS {
		c, err := smtp.NewClientStartTLS(conn, &tls.Config{ServerName: tlsServerName(ep.Host)})
		if err != nil {
			_ = conn.Close()
			return nil, mapSMTP(err, "tls")
		}
		return c, nil
	}
	return smtp.NewClient(conn), nil
}

func (d *NetDialer) dialConn(ctx context.Context, ep Endpoint, inbound bool) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	host := strings.TrimSpace(ep.Host)
	port := ep.Port
	if port == 0 {
		port = defaultPort(inbound, ep.Security)
	}
	dialer := net.Dialer{Timeout: d.timeout()}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, mapDial(err)
	}
	// IMAP clears a zero Timeout and SMTP replaces it with a five-minute
	// command budget. Keep the caller's deadline, or the read budget, so a
	// silent server cannot outlive the probe.
	deadline := sessionDeadline(ctx)
	conn = &deadlineConn{Conn: conn, deadline: deadline}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, mapDial(err)
	}
	if ep.Security != SecurityTLS {
		return conn, nil
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: tlsServerName(host)})
	if err := tlsConn.Handshake(); err != nil {
		_ = conn.Close()
		return nil, mapDial(err)
	}
	return tlsConn, nil
}

func sessionDeadline(ctx context.Context) time.Time {
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			return deadline
		}
	}
	return time.Now().Add(ToolReadTimeout)
}

// deadlineConn refuses to clear or extend the session deadline. Libraries
// call SetDeadline(time.Time{}) to mean "no limit".
type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	return c.Conn.SetDeadline(c.clamp(t))
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	return c.Conn.SetReadDeadline(c.clamp(t))
}

func (c *deadlineConn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(c.clamp(t))
}

func (c *deadlineConn) clamp(t time.Time) time.Time {
	if c == nil || c.deadline.IsZero() {
		return t
	}
	if t.IsZero() || t.After(c.deadline) {
		return c.deadline
	}
	return t
}

func tlsServerName(host string) string {
	host = strings.TrimSpace(host)
	return strings.Trim(host, "[]")
}

type imapProbe struct{ c *imapclient.Client }

func (s *imapProbe) Login(username, secret string) error {
	return mapIMAP(s.c.Login(username, secret), "auth")
}

func (s *imapProbe) Select(box string) error {
	_, err := s.c.Select(box, true)
	return mapIMAP(err, "select")
}

func (s *imapProbe) Close() error {
	if s == nil || s.c == nil {
		return nil
	}
	return s.c.Logout()
}

type smtpProbe struct{ c *smtp.Client }

func (s *smtpProbe) Auth(username, secret string) error {
	return authSMTP(s.c, username, secret)
}

func (s *smtpProbe) Noop() error { return mapSMTP(s.c.Noop(), "noop") }

func (s *smtpProbe) Quit() error {
	if s == nil || s.c == nil {
		return nil
	}
	return mapSMTP(s.c.Quit(), "quit")
}

// authSMTP uses the mechanism the server advertised. QQ, 163 and many
// enterprise relays implement LOGIN and answer PLAIN with a credential
// error even when the password is right.
func authSMTP(c *smtp.Client, username, secret string) error {
	if c == nil {
		return ErrAuth
	}
	order := smtpAuthOrder(c)
	var last error
	for i, mech := range order {
		err := c.Auth(smtpSASL(mech, username, secret))
		if err == nil {
			return nil
		}
		last = err
		if i == len(order)-1 || !smtpAuthRetryable(err) {
			break
		}
	}
	return mapSMTP(last, "auth")
}

func smtpAuthOrder(c *smtp.Client) []string {
	var order []string
	if c != nil && c.SupportsAuth("PLAIN") {
		order = append(order, "PLAIN")
	}
	if c != nil && c.SupportsAuth("LOGIN") {
		order = append(order, "LOGIN")
	}
	if len(order) == 0 {
		return []string{"PLAIN", "LOGIN"}
	}
	return order
}

func smtpSASL(mech, username, secret string) sasl.Client {
	if strings.EqualFold(mech, "LOGIN") {
		return &smtpLoginAuth{username: username, secret: secret}
	}
	return sasl.NewPlainClient("", username, secret)
}

// smtpLoginAuth answers the two LOGIN challenges in order. The stock
// go-sasl client sends the username up front and then accepts only the
// exact "Password:" prompt, which these relays do not send.
type smtpLoginAuth struct {
	username string
	secret   string
	step     int
}

func (a *smtpLoginAuth) Start() (string, []byte, error) {
	return sasl.Login, nil, nil
}

func (a *smtpLoginAuth) Next(_ []byte) ([]byte, error) {
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.secret), nil
	default:
		return nil, sasl.ErrUnexpectedServerChallenge
	}
}

func smtpAuthRetryable(err error) bool {
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		return false
	}
	switch smtpErr.Code {
	case 432, 454, 500, 501, 504, 534, 535, 538:
		return true
	default:
		return false
	}
}

func mapDial(err error) error {
	if err == nil {
		return nil
	}
	if isTLS(err) {
		return ErrTLS
	}
	return ErrConnect
}

func mapIMAP(err error, stage string) error {
	if err == nil {
		return nil
	}
	if isTimeout(err) {
		return ErrConnect
	}
	if stage == "tls" || isTLS(err) {
		return ErrTLS
	}
	if stage == "auth" {
		return ErrAuth
	}
	low := strings.ToLower(err.Error())
	if stage == "select" || strings.Contains(low, "no such") || strings.Contains(low, "nonexistent") || strings.Contains(low, "unknown mailbox") {
		return ErrFolder
	}
	if strings.Contains(low, "authentication") || strings.Contains(low, "invalid credentials") || strings.Contains(low, "login failed") {
		return ErrAuth
	}
	return ErrConnect
}

func mapSMTP(err error, stage string) error {
	if err == nil {
		return nil
	}
	if isTimeout(err) {
		return ErrConnect
	}
	if stage == "data" {
		return ErrUncertain
	}
	if stage == "tls" || isTLS(err) {
		return ErrTLS
	}
	var smtpErr *smtp.SMTPError
	if errors.As(err, &smtpErr) {
		if stage == "auth" || smtpErr.Code == 535 || smtpErr.Code == 534 {
			return ErrAuth
		}
		if smtpErr.Code >= 500 {
			return ErrRejected
		}
		return ErrConnect
	}
	if stage == "auth" {
		return ErrAuth
	}
	return ErrConnect
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isTLS(err error) bool {
	if err == nil {
		return false
	}
	var unknown tls.RecordHeaderError
	if errors.As(err, &unknown) {
		return true
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "x509") || strings.Contains(low, "certificate") || strings.HasPrefix(low, "tls:")
}
