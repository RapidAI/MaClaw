package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

type mailboxInboundSpy struct {
	user, secret, box string
}

func (s *mailboxInboundSpy) Login(username, secret string) error {
	s.user, s.secret = username, secret
	return nil
}
func (s *mailboxInboundSpy) Select(box string) error { s.box = box; return nil }
func (s *mailboxInboundSpy) Close() error            { return nil }

type mailboxOutboundSpy struct {
	auth, noop, quit, sent int
}

func (s *mailboxOutboundSpy) Auth(string, string) error { s.auth++; return nil }
func (s *mailboxOutboundSpy) Noop() error               { s.noop++; return nil }
func (s *mailboxOutboundSpy) Quit() error               { s.quit++; return nil }

type mailboxSpyDialer struct {
	in  *mailboxInboundSpy
	out *mailboxOutboundSpy
}

func (d mailboxSpyDialer) DialInbound(context.Context, mailbox.Endpoint) (mailbox.InboundSession, error) {
	return d.in, nil
}
func (d mailboxSpyDialer) DialOutbound(context.Context, mailbox.Endpoint) (mailbox.OutboundSession, error) {
	return d.out, nil
}

func installFakeMailboxKeyring(t *testing.T) map[string]string {
	t.Helper()
	store := map[string]string{}
	oldSet, oldGet, oldDelete := mailboxKeyringSet, mailboxKeyringGet, mailboxKeyringDelete
	mailboxKeyringSet = func(service, item, secret string) error {
		store[service+"\x00"+item] = secret
		return nil
	}
	mailboxKeyringGet = func(service, item string) (string, error) {
		secret, ok := store[service+"\x00"+item]
		if !ok {
			return "", errors.New("secret not found")
		}
		return secret, nil
	}
	mailboxKeyringDelete = func(service, item string) error {
		delete(store, service+"\x00"+item)
		return nil
	}
	t.Cleanup(func() {
		mailboxKeyringSet, mailboxKeyringGet, mailboxKeyringDelete = oldSet, oldGet, oldDelete
	})
	return store
}

func TestMailboxAccountSaveDefaultSecretAndProbe(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeMailboxKeyring(t)
	in := &mailboxInboundSpy{}
	out := &mailboxOutboundSpy{}
	oldDialer := mailboxProbeDialer
	mailboxProbeDialer = mailboxSpyDialer{in: in, out: out}
	t.Cleanup(func() { mailboxProbeDialer = oldDialer })

	sigID, err := app.MailboxSignatureSave(map[string]interface{}{
		"name": "公共", "body": "{{邮箱名}} <{{邮箱}}>", "default": true,
	})
	if err != nil || sigID == "" {
		t.Fatalf("signature: %v %s", err, sigID)
	}
	id, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"inbound":  map[string]interface{}{"host": "127.0.0.1", "security": "none", "username": "w", "box": "INBOX"},
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "username": "w", "from_address": "w@ex.com"},
	})
	if err != nil || id == "" {
		t.Fatalf("save: %v %s", err, id)
	}
	if _, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Home", "address": "h@ex.com", "signature_id": sigID,
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(id, "inbound", "app-password-should-not-leak"); err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(id, "outbound", "smtp-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg.MailboxAccounts)
	if strings.Contains(string(raw), "app-password-should-not-leak") || strings.Contains(string(raw), "smtp-secret") {
		t.Fatalf("password stored in config: %s", raw)
	}
	listed, err := app.MailboxAccountsList()
	if err != nil {
		t.Fatal(err)
	}
	listRaw, _ := json.Marshal(listed)
	if strings.Contains(string(listRaw), "app-password") || strings.Contains(string(listRaw), "127.0.0.1") || strings.Contains(string(listRaw), "secret_ref") {
		t.Fatalf("list leaked: %s", listRaw)
	}
	if len(cfg.MailboxAccounts) != 2 || !cfg.MailboxAccounts[0].Default || cfg.MailboxAccounts[1].Default {
		t.Fatalf("defaults: %+v", cfg.MailboxAccounts)
	}
	text, err := app.MailboxAccountTest(id, "inbound")
	if err != nil || text != mailbox.MsgReceiveOK || in.box != "INBOX" || in.secret != "app-password-should-not-leak" {
		t.Fatalf("receive %q %v spy=%+v", text, err, in)
	}
	text, err = app.MailboxAccountTest(id, "outbound")
	if err != nil || text != mailbox.MsgSendOK || out.auth != 1 || out.noop != 1 || out.quit != 1 || out.sent != 0 {
		t.Fatalf("send %q %v spy=%+v", text, err, out)
	}
	if _, ok := store[mailbox.KeyringService+"\x00mailbox/"+id+"/inbound"]; !ok {
		t.Fatalf("keyring: %#v", store)
	}
	sigs, err := app.MailboxSignaturesList()
	if err != nil || len(sigs) != 1 || sigs[0]["default"] != true {
		t.Fatalf("sigs %+v %v", sigs, err)
	}
	_ = time.Second
}

type captureMailSender struct {
	calls int
	last  mailbox.OutboundMessage
}

func (s *captureMailSender) Send(_ context.Context, _ mailbox.Account, _ string, msg mailbox.OutboundMessage) error {
	s.calls++
	s.last = msg
	return nil
}

func TestMailboxToolHostUsesPublicOrMailboxSignature(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	installFakeMailboxKeyring(t)
	if _, err := app.MailboxSignatureSave(map[string]interface{}{
		"name": "公共", "body": "此致 {{邮箱名}} <{{邮箱}}>",
	}); err != nil {
		t.Fatal(err)
	}
	personal, err := app.MailboxSignatureSave(map[string]interface{}{
		"name": "个人", "body": "From {{发件人}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "from_address": "w@ex.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	home, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Home", "address": "h@ex.com", "signature_id": personal,
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "from_address": "h@ex.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(work, "outbound", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(home, "outbound", "secret"); err != nil {
		t.Fatal(err)
	}
	sender := &captureMailSender{}
	oldSender := mailboxToolSender
	mailboxToolSender = sender
	t.Cleanup(func() { mailboxToolSender = oldSender })

	host := app.MailboxToolHost("user", "session", t.TempDir(), mailbox.NewTurnBudget())
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "Hi", "text": "body",
	})
	if sender.calls != 0 || !strings.Contains(card, "Work") || !strings.Contains(card, "w@ex.com") || strings.Contains(card, "{{") {
		t.Fatalf("public signature card=%s calls=%d", card, sender.calls)
	}
	draft, ok := guiMailboxDrafts.Pending("user", "session")
	if !ok {
		t.Fatal("missing frozen draft")
	}
	if _, class := host.Approve(context.Background(), draft.ID); class != "approved" || sender.calls != 1 || !strings.Contains(sender.last.Text, "此致 Work <w@ex.com>") {
		t.Fatalf("approve class calls=%d body=%q", sender.calls, sender.last.Text)
	}

	host = app.MailboxToolHost("user", "session-2", t.TempDir(), mailbox.NewTurnBudget())
	card = host.Send(context.Background(), map[string]interface{}{
		"action": "send", "account": "Home", "to": []any{"a@ex.com"}, "subject": "Hi", "text": "body",
	})
	if sender.calls != 1 || !strings.Contains(card, "From h@ex.com") || strings.Contains(card, "此致") {
		t.Fatalf("mailbox signature card=%s", card)
	}
}
