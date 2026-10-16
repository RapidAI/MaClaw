package mailbox

import (
	"context"
	"errors"
	"strings"
)

// InboundSession is an IMAP login used by the availability test.
// Select checks the configured folder and does not fetch a body.
type InboundSession interface {
	Login(username, secret string) error
	Select(box string) error
	Close() error
}

// OutboundSession is an SMTP availability session. It has no send method,
// so a probe cannot transmit a message.
type OutboundSession interface {
	Auth(username, secret string) error
	Noop() error
	Quit() error
}

// Dialer opens probe sessions. Tests inject it and never dial the public internet.
type Dialer interface {
	DialInbound(ctx context.Context, ep Endpoint) (InboundSession, error)
	DialOutbound(ctx context.Context, ep Endpoint) (OutboundSession, error)
}

func TestReceive(ctx context.Context, dialer Dialer, account Account, secret string) (string, error) {
	if account.Inbound == nil {
		return "", classErr("no_inbound", MsgNoInboundUI)
	}
	if strings.TrimSpace(secret) == "" {
		return "", classErr("no_secret", MsgNoSecret)
	}
	if dialer == nil {
		return "", classErr("connect", MsgConnectLong)
	}
	session, err := dialer.DialInbound(ctx, *account.Inbound)
	if err != nil {
		return "", probeErr(err, true)
	}
	defer session.Close()
	user := account.Inbound.Username
	if user == "" {
		user = account.Address
	}
	if err := session.Login(user, secret); err != nil {
		return "", probeErr(err, true)
	}
	box := account.Inbound.Box
	if box == "" {
		box = "INBOX"
	}
	if err := session.Select(box); err != nil {
		return "", probeErr(err, true)
	}
	return MsgReceiveOK, nil
}

func TestSend(ctx context.Context, dialer Dialer, account Account, secret string) (string, error) {
	if account.Outbound == nil {
		return "", classErr("no_outbound", MsgNoOutboundUI)
	}
	if strings.TrimSpace(secret) == "" {
		return "", classErr("no_secret", MsgNoSecret)
	}
	if dialer == nil {
		return "", classErr("connect", MsgConnectLong)
	}
	session, err := dialer.DialOutbound(ctx, *account.Outbound)
	if err != nil {
		return "", probeErr(err, true)
	}
	user := account.Outbound.Username
	if user == "" {
		user = account.Address
	}
	if err := session.Auth(user, secret); err != nil {
		_ = session.Quit()
		return "", probeErr(err, true)
	}
	if err := session.Noop(); err != nil {
		_ = session.Quit()
		return "", probeErr(err, true)
	}
	if err := session.Quit(); err != nil {
		return "", probeErr(err, true)
	}
	return MsgSendOK, nil
}

func probeErr(err error, settings bool) error {
	class, msg := classifyTransport(err, settings)
	return classErr(class, msg)
}

func classifyTransport(err error, settings bool) (string, string) {
	switch {
	case err == nil:
		return "", ""
	case errors.Is(err, ErrAuth):
		return "auth", MsgAuth
	case errors.Is(err, ErrTLS):
		return "tls", MsgTLS
	case errors.Is(err, ErrFolder):
		return "folder", MsgFolder
	case errors.Is(err, ErrRejected):
		return "rejected", MsgRejected
	case errors.Is(err, ErrUncertain):
		return "uncertain", MsgUncertain
	case errors.Is(err, ErrEmpty):
		return "empty", MsgEmpty
	case errors.Is(err, ErrNotFound):
		return "not_found", MsgNotFound
	case errors.Is(err, ErrKeyring):
		if settings {
			return "keyring", MsgKeyringLong
		}
		return "keyring", MsgKeyring
	default:
		if settings {
			return "connect", MsgConnectLong
		}
		return "connect", MsgConnect
	}
}
