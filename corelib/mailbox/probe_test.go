package mailbox

import (
	"context"
	"errors"
	"testing"
)

type inboundSpy struct {
	login, sel string
	err        error
	selected   string
}

func (s *inboundSpy) Login(username, secret string) error {
	s.login = username + ":" + secret
	if s.err != nil && s.selected == "" {
		return s.err
	}
	return nil
}
func (s *inboundSpy) Select(box string) error {
	s.selected = box
	return s.err
}
func (s *inboundSpy) Close() error { return nil }

type outboundSpy struct {
	auth, noop, quit int
	err              error
	sent             int
}

func (s *outboundSpy) Auth(username, secret string) error {
	s.auth++
	return s.err
}
func (s *outboundSpy) Noop() error { s.noop++; return nil }
func (s *outboundSpy) Quit() error { s.quit++; return nil }

type spyDialer struct {
	in  *inboundSpy
	out *outboundSpy
}

func (d spyDialer) DialInbound(context.Context, Endpoint) (InboundSession, error) {
	return d.in, nil
}
func (d spyDialer) DialOutbound(context.Context, Endpoint) (OutboundSession, error) {
	return d.out, nil
}

func TestProbeSuccessDoesNotSend(t *testing.T) {
	account := Account{
		Name: "Work", Address: "w@ex.com",
		Inbound:  &Endpoint{Host: "127.0.0.1", Port: 993, Security: SecurityTLS, Username: "w", Box: "INBOX"},
		Outbound: &Endpoint{Host: "127.0.0.1", Port: 587, Security: SecuritySTARTTLS, Username: "w"},
	}
	in := &inboundSpy{}
	out := &outboundSpy{}
	got, err := TestReceive(context.Background(), spyDialer{in: in}, account, "secret")
	if err != nil || got != MsgReceiveOK || in.selected != "INBOX" || in.login != "w:secret" {
		t.Fatalf("receive %q %v login=%s box=%s", got, err, in.login, in.selected)
	}
	got, err = TestSend(context.Background(), spyDialer{out: out}, account, "secret")
	if err != nil || got != MsgSendOK || out.auth != 1 || out.noop != 1 || out.quit != 1 || out.sent != 0 {
		t.Fatalf("send %q %v spy=%+v", got, err, out)
	}
}

func TestProbeChineseFailures(t *testing.T) {
	account := Account{Name: "Work", Address: "w@ex.com", Inbound: &Endpoint{Host: "127.0.0.1", Box: "Missing"}}
	in := &inboundSpy{err: ErrAuth}
	if _, err := TestReceive(context.Background(), spyDialer{in: in}, account, "bad"); err == nil || err.Error() != MsgAuth {
		t.Fatal(err)
	}
	in.err = ErrFolder
	in.selected = ""
	if _, err := TestReceive(context.Background(), spyDialer{in: in}, account, "secret"); err == nil || err.Error() != MsgFolder {
		t.Fatal(err)
	}
	in.err = errors.New("dial tcp: refused")
	if _, err := TestReceive(context.Background(), spyDialer{in: in}, account, "secret"); err == nil || err.Error() != MsgConnectLong {
		t.Fatal(err)
	}
	in.err = ErrTLS
	if _, err := TestReceive(context.Background(), spyDialer{in: in}, account, "secret"); err == nil || err.Error() != MsgTLS {
		t.Fatal(err)
	}
	if _, err := TestReceive(context.Background(), spyDialer{in: in}, Account{Name: "Work"}, "secret"); err == nil || err.Error() != MsgNoInboundUI {
		t.Fatal(err)
	}
	if _, err := TestSend(context.Background(), spyDialer{}, Account{Name: "Work"}, "secret"); err == nil || err.Error() != MsgNoOutboundUI {
		t.Fatal(err)
	}
}
