package mailbox

// POP3 inbound (RFC 1939, STLS RFC 2595, AUTH XOAUTH2). POP3 has one folder
// and no flags, so only list, search, read and attachments work; organize and
// delete report that the server cannot do them. Message numbers change between
// sessions, so the uid handed to the model is a 32-bit FNV hash of the UIDL,
// resolved again on every read.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"mime"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
)

const (
	ProtocolIMAP = "imap"
	ProtocolPOP3 = "pop3"
	// pop3SearchWindow is how many newest messages a POP3 search scans.
	pop3SearchWindow = 300
)

// MsgPOP3Unsupported is the answer for folder, flag and delete actions.
const MsgPOP3Unsupported = "这个邮箱用 POP3 收信，服务器只有收件箱，不支持文件夹、标签、已读标记或删除。需要这些功能请改用 IMAP 接入。"

func isPOP3(account Account) bool {
	return account.Inbound != nil && strings.EqualFold(strings.TrimSpace(account.Inbound.Protocol), ProtocolPOP3)
}

func normalizeProtocol(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "imap", "imap4", "imaps":
		return "", nil
	case "pop", "pop3", "pop3s":
		return ProtocolPOP3, nil
	}
	return "", classErr("invalid", "收件协议只能是 imap 或 pop3")
}

func pop3DefaultPort(sec string) int {
	if sec == SecurityTLS {
		return 995
	}
	return 110
}

// pop3UID maps a UIDL to the uid the tools expose.
func pop3UID(uidl string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(uidl))
	v := h.Sum32()
	if v == 0 {
		v = 1
	}
	return v
}

type pop3Conn struct {
	conn net.Conn
	r    *bufio.Reader
}

func newPOP3Conn(conn net.Conn) (*pop3Conn, error) {
	p := &pop3Conn{conn: conn, r: bufio.NewReader(conn)}
	if _, err := p.status(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return p, nil
}

var errPOP3 = errors.New("pop3 -ERR")

func (p *pop3Conn) status() (string, error) {
	line, err := p.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, "+OK") {
		return strings.TrimSpace(strings.TrimPrefix(line, "+OK")), nil
	}
	if strings.HasPrefix(line, "+ ") || line == "+" {
		return strings.TrimSpace(strings.TrimPrefix(line, "+")), nil
	}
	return "", fmt.Errorf("%w: %s", errPOP3, strings.TrimSpace(strings.TrimPrefix(line, "-ERR")))
}

func (p *pop3Conn) cmd(format string, args ...interface{}) (string, error) {
	if _, err := fmt.Fprintf(p.conn, format+"\r\n", args...); err != nil {
		return "", err
	}
	return p.status()
}

// multi reads a dot-terminated block with dot-unstuffing.
func (p *pop3Conn) multi() ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := p.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			return buf.Bytes(), nil
		}
		if strings.HasPrefix(trimmed, "..") {
			trimmed = trimmed[1:]
		}
		buf.WriteString(trimmed)
		buf.WriteString("\r\n")
	}
}

func (p *pop3Conn) multiCmd(format string, args ...interface{}) ([]byte, error) {
	if _, err := p.cmd(format, args...); err != nil {
		return nil, err
	}
	return p.multi()
}

func (p *pop3Conn) login(user, secret string) error {
	if token, ok := OAuthAccessToken(secret); ok {
		raw := "user=" + user + "\x01auth=Bearer " + token + "\x01\x01"
		_, err := p.cmd("AUTH XOAUTH2 %s", base64.StdEncoding.EncodeToString([]byte(raw)))
		return err
	}
	if strings.ContainsAny(user+secret, "\r\n") {
		return ErrAuth
	}
	if _, err := p.cmd("USER %s", user); err != nil {
		return err
	}
	_, err := p.cmd("PASS %s", secret)
	return err
}

// uidl returns message number -> UIDL, oldest first.
func (p *pop3Conn) uidl() ([]pop3Entry, error) {
	raw, err := p.multiCmd("UIDL")
	if err != nil {
		return nil, err
	}
	var out []pop3Entry
	for _, line := range strings.Split(string(raw), "\r\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		out = append(out, pop3Entry{num: n, uidl: f[1], uid: pop3UID(f[1])})
	}
	return out, nil
}

func (p *pop3Conn) quit() {
	_, _ = p.cmd("QUIT")
	_ = p.conn.Close()
}

type pop3Entry struct {
	num  int
	uidl string
	uid  uint32
}

func (d *NetDialer) pop3Open(ctx context.Context, account Account, secret string) (*pop3Conn, error) {
	ep := *account.Inbound
	if ep.Port == 0 {
		ep.Port = pop3DefaultPort(ep.Security)
	}
	user := ep.Username
	if user == "" {
		user = account.Address
	}
	dial := func(sec string) (*pop3Conn, error) {
		conn, err := d.dialConn(ctx, ep, true)
		if err != nil {
			return nil, err
		}
		p, err := newPOP3Conn(conn)
		if err != nil {
			return nil, ErrConnect
		}
		if ep.Security == SecuritySTARTTLS {
			if _, err := p.cmd("STLS"); err != nil {
				_ = conn.Close()
				return nil, ErrTLS
			}
			tc := tls.Client(conn, &tls.Config{ServerName: tlsServerName(ep.Host)})
			if err := tc.Handshake(); err != nil {
				_ = conn.Close()
				return nil, ErrTLS
			}
			p = &pop3Conn{conn: tc, r: bufio.NewReader(tc)}
		}
		if err := p.login(user, sec); err != nil {
			_ = p.conn.Close()
			if isTimeout(err) {
				return nil, ErrConnect
			}
			return nil, ErrAuth
		}
		return p, nil
	}
	return dialWithOAuthRetry(ctx, d, account, secret, dial)
}

func pop3BoxOK(box string) error {
	b := strings.TrimSpace(box)
	if b == "" || strings.EqualFold(b, "INBOX") {
		return nil
	}
	return classErr("unsupported", MsgPOP3Unsupported)
}

func (d *NetDialer) pop3Summaries(ctx context.Context, account Account, secret, box, query string, limit int) ([]MessageSummary, error) {
	if err := pop3BoxOK(box); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	p, err := d.pop3Open(ctx, account, secret)
	if err != nil {
		return nil, err
	}
	defer p.quit()
	entries, err := p.uidl()
	if err != nil {
		return nil, ErrConnect
	}
	q := strings.ToLower(strings.TrimSpace(query))
	window := limit
	if q != "" {
		window = pop3SearchWindow
	}
	var out []MessageSummary
	for i := len(entries) - 1; i >= 0 && len(out) < limit && len(entries)-1-i < window; i-- {
		e := entries[i]
		lines := 0
		if q != "" {
			lines = 20
		}
		raw, err := p.multiCmd("TOP %d %d", e.num, lines)
		if err != nil {
			continue
		}
		sum := summaryFromRaw(raw)
		sum.UID = e.uid
		if q != "" {
			hay := strings.ToLower(sum.Subject + " " + sum.From + " " + sum.To + " " + sum.Snippet)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

func (d *NetDialer) pop3Retr(ctx context.Context, account Account, secret, box string, uid uint32) ([]byte, error) {
	if err := pop3BoxOK(box); err != nil {
		return nil, err
	}
	p, err := d.pop3Open(ctx, account, secret)
	if err != nil {
		return nil, err
	}
	defer p.quit()
	entries, err := p.uidl()
	if err != nil {
		return nil, ErrConnect
	}
	for _, e := range entries {
		if e.uid == uid {
			raw, err := p.multiCmd("RETR %d", e.num)
			if err != nil {
				return nil, ErrConnect
			}
			return raw, nil
		}
	}
	return nil, classErr("not_found", "没有找到这封邮件，可能已被删除或移走。请重新列出邮件。")
}

func (d *NetDialer) pop3Read(ctx context.Context, account Account, secret, box string, uid uint32) (Body, error) {
	raw, err := d.pop3Retr(ctx, account, secret, box, uid)
	if err != nil {
		return Body{}, err
	}
	body, _ := parseRawMessage(raw)
	body.UID = uid
	return body, nil
}

func (d *NetDialer) pop3Attachment(ctx context.Context, account Account, secret, box string, uid uint32, index int) (Attachment, error) {
	raw, err := d.pop3Retr(ctx, account, secret, box, uid)
	if err != nil {
		return Attachment{}, err
	}
	_, atts := parseRawMessage(raw)
	if index < 0 || index >= len(atts) {
		return Attachment{}, classErr("not_found", "没有这个附件序号")
	}
	return atts[index], nil
}

func summaryFromRaw(raw []byte) MessageSummary {
	var s MessageSummary
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return s
	}
	h := mr.Header
	s.Subject, _ = h.Subject()
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		s.From = from[0].String()
		s.FromAddr = from[0].Address
	}
	if to, err := h.AddressList("To"); err == nil {
		var parts []string
		for _, a := range to {
			parts = append(parts, a.String())
			s.ToAddrs = append(s.ToAddrs, a.Address)
		}
		s.To = strings.Join(parts, ", ")
	}
	if cc, err := h.AddressList("Cc"); err == nil {
		for _, a := range cc {
			s.CcAddrs = append(s.CcAddrs, a.Address)
		}
	}
	if t, err := h.Date(); err == nil {
		s.Date = t.Format(time.RFC3339)
	}
	s.MessageID, _ = h.MessageID()
	s.References = h.Get("References")
	s.ReplyTo = h.Get("Reply-To")
	ct := strings.ToLower(h.Get("Content-Type"))
	s.HasAttachment = strings.HasPrefix(ct, "multipart/mixed")
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if ih, ok := part.Header.(*mail.InlineHeader); ok {
			t, _, _ := ih.ContentType()
			if t == "text/plain" && s.Snippet == "" {
				b, _ := io.ReadAll(io.LimitReader(part.Body, 600))
				s.Snippet = strings.Join(strings.Fields(string(b)), " ")
			}
		}
	}
	return s
}

// parseRawMessage turns an RFC 5322 message into the body the tools show and
// its attachments, in order.
func parseRawMessage(raw []byte) (Body, []Attachment) {
	body := Body{MessageSummary: summaryFromRaw(raw)}
	var atts []Attachment
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		body.Text = string(raw)
		return body, nil
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			t, _, _ := h.ContentType()
			b, _ := io.ReadAll(io.LimitReader(part.Body, 4<<20))
			switch {
			case t == "text/plain" && body.Text == "":
				body.Text = string(b)
			case t == "text/html" && body.HTML == "":
				body.HTML = string(b)
			}
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			if name == "" {
				_, params, _ := mime.ParseMediaType(h.Get("Content-Type"))
				name = params["name"]
			}
			ct, _, _ := h.ContentType()
			b, _ := io.ReadAll(io.LimitReader(part.Body, 32<<20))
			body.Attachments = append(body.Attachments, AttachmentMeta{Index: len(atts), Name: name, Size: int64(len(b)), ContentType: ct})
			atts = append(atts, Attachment{Name: name, Bytes: b})
		}
	}
	body.HasAttachment = len(atts) > 0
	return body, atts
}

// pop3Probe is the availability session for a POP3 account.
type pop3Probe struct{ p *pop3Conn }

func (s *pop3Probe) Login(username, secret string) error {
	if err := s.p.login(username, secret); err != nil {
		return ErrAuth
	}
	return nil
}

func (s *pop3Probe) Select(box string) error {
	if err := pop3BoxOK(box); err != nil {
		return ErrFolder
	}
	if _, err := s.p.cmd("STAT"); err != nil {
		return ErrConnect
	}
	return nil
}

func (s *pop3Probe) Close() error { s.p.quit(); return nil }

func (d *NetDialer) dialPOP3Probe(ctx context.Context, ep Endpoint) (InboundSession, error) {
	if ep.Port == 0 {
		ep.Port = pop3DefaultPort(ep.Security)
	}
	conn, err := d.dialConn(ctx, ep, true)
	if err != nil {
		return nil, err
	}
	p, err := newPOP3Conn(conn)
	if err != nil {
		return nil, ErrConnect
	}
	if ep.Security == SecuritySTARTTLS {
		if _, err := p.cmd("STLS"); err != nil {
			_ = conn.Close()
			return nil, ErrTLS
		}
		tc := tls.Client(conn, &tls.Config{ServerName: tlsServerName(ep.Host)})
		if err := tc.Handshake(); err != nil {
			_ = conn.Close()
			return nil, ErrTLS
		}
		p = &pop3Conn{conn: tc, r: bufio.NewReader(tc)}
	}
	return &pop3Probe{p: p}, nil
}
