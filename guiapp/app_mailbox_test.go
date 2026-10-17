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
	// Summary intentionally includes the servers the user saved (host, port,
	// security, username) so settings can edit them; it must not carry a
	// password or a secret_ref locator.
	if strings.Contains(string(listRaw), "app-password") || strings.Contains(string(listRaw), "should-not-leak") || strings.Contains(string(listRaw), "secret_ref") {
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

func TestMailboxAccountTestDraft(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	in := &mailboxInboundSpy{}
	out := &mailboxOutboundSpy{}
	oldDialer := mailboxProbeDialer
	mailboxProbeDialer = mailboxSpyDialer{in: in, out: out}
	t.Cleanup(func() { mailboxProbeDialer = oldDialer })
	_ = installFakeMailboxKeyring(t)

	text, err := app.MailboxAccountTestDraft(mailboxDraftTestDTO{
		Host: "127.0.0.1", Security: "none", Username: "typed@ex.com", Password: "typed-secret", Box: "INBOX",
	}, "inbound")
	if err != nil || text != mailbox.MsgReceiveOK || in.user != "typed@ex.com" || in.secret != "typed-secret" || in.box != "INBOX" {
		t.Fatalf("draft receive: %q %v spy=%+v", text, err, in)
	}

	text, err = app.MailboxAccountTestDraft(mailboxDraftTestDTO{
		Host: "127.0.0.1", Security: "none", Username: "typed@ex.com", Password: "out-secret",
	}, "outbound")
	if err != nil || text != mailbox.MsgSendOK || out.auth != 1 || out.noop != 1 || out.quit != 1 {
		t.Fatalf("draft send: %q %v spy=%+v", text, err, out)
	}

	if _, err := app.MailboxAccountTestDraft(mailboxDraftTestDTO{Host: "127.0.0.1"}, "inbound"); err == nil || err.Error() != mailbox.MsgNoSecret {
		t.Fatalf("no secret: %v", err)
	}

	id, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"inbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "username": "w", "box": "INBOX"},
	})
	if err != nil || id == "" {
		t.Fatalf("save: %v %s", err, id)
	}
	if err := app.MailboxAccountSetSecret(id, "inbound", "keyring-secret"); err != nil {
		t.Fatal(err)
	}
	in2 := &mailboxInboundSpy{}
	mailboxProbeDialer = mailboxSpyDialer{in: in2, out: out}
	text, err = app.MailboxAccountTestDraft(mailboxDraftTestDTO{
		AccountID: id, Host: "127.0.0.1", Security: "none", Username: "w", Box: "INBOX",
		UseSavedSecret: true,
	}, "inbound")
	if err != nil || text != mailbox.MsgReceiveOK || in2.user != "w" || in2.secret != "keyring-secret" {
		t.Fatalf("fallback saved secret: %q %v spy=%+v", text, err, in2)
	}
}

func TestMailboxOAuthLoginStoresTokenOutsideConfig(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeMailboxKeyring(t)
	prev := beginMailboxOAuth
	beginMailboxOAuth = func(context.Context, string, string, func(string) error) (mailbox.OAuthGrant, error) {
		return mailbox.OAuthGrant{
			Provider: mailbox.ProviderGmail,
			Email:    "a@gmail.com",
			Secret:   `{"kind":"oauth","provider":"gmail","access_token":"ya29.test","refresh_token":"refresh-1","expiry":"2099-01-01T00:00:00Z"}`,
		}, nil
	}
	t.Cleanup(func() { beginMailboxOAuth = prev })

	msg, err := app.MailboxOAuthLogin("gmail", "")
	if err != nil || msg != "Gmail 邮箱 a@gmail.com 已登录" {
		t.Fatalf("login: %v %q", err, msg)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MailboxAccounts) != 1 {
		t.Fatalf("accounts=%d", len(cfg.MailboxAccounts))
	}
	saved := cfg.MailboxAccounts[0]
	if saved.Auth != mailbox.AuthOAuth || saved.Provider != mailbox.ProviderGmail || saved.Address != "a@gmail.com" {
		t.Fatalf("account=%+v", saved)
	}
	if saved.Inbound == nil || saved.Inbound.Host != "imap.gmail.com" || saved.Outbound == nil || saved.Outbound.Host != "smtp.gmail.com" {
		t.Fatalf("endpoints in=%+v out=%+v", saved.Inbound, saved.Outbound)
	}
	raw, _ := json.Marshal(cfg.MailboxAccounts)
	if strings.Contains(string(raw), "ya29.test") || strings.Contains(string(raw), "refresh-1") {
		t.Fatal(string(raw))
	}
	secret, err := app.mailboxSecret(context.Background(), mailbox.SecretRef(saved.ID, mailbox.SideInbound))
	if err != nil || !strings.Contains(secret, "ya29.test") {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	other, err := app.mailboxSecret(context.Background(), mailbox.SecretRef(saved.ID, mailbox.SideOutbound))
	if err != nil || other != secret {
		t.Fatalf("outbound secret=%q err=%v", other, err)
	}
	if len(store) != 2 {
		t.Fatalf("keyring entries=%d", len(store))
	}
	rejected := `{"kind":"oauth","provider":"gmail","access_token":"ya29.old","refresh_token":"refresh-old","expiry":"2099-01-01T00:00:00Z"}`
	got, err := app.replaceMailboxOAuth(context.Background(), saved, rejected)
	if err != nil || !strings.Contains(got, "ya29.test") || strings.Contains(got, "refresh-old") {
		t.Fatalf("access refresh used the rejected token: %s err=%v", got, err)
	}
}

func TestWriteMailboxSecretBothSidesRollsBack(t *testing.T) {
	store := installFakeMailboxKeyring(t)
	id := "mbrollback"
	inItem, err := mailbox.KeyringItem(id, mailbox.SideInbound)
	if err != nil {
		t.Fatal(err)
	}
	outItem, err := mailbox.KeyringItem(id, mailbox.SideOutbound)
	if err != nil {
		t.Fatal(err)
	}
	key := mailbox.KeyringService + "\x00"
	if err := mailboxKeyringSet(mailbox.KeyringService, inItem, "old-in"); err != nil {
		t.Fatal(err)
	}
	if err := mailboxKeyringSet(mailbox.KeyringService, outItem, "old-out"); err != nil {
		t.Fatal(err)
	}
	realSet := mailboxKeyringSet
	writes := 0
	mailboxKeyringSet = func(service, item, secret string) error {
		writes++
		if item == outItem {
			return errors.New("keyring full")
		}
		return realSet(service, item, secret)
	}
	t.Cleanup(func() { mailboxKeyringSet = realSet })

	if err := writeMailboxSecretBothSides(id, "new-secret"); err == nil || err.Error() != mailbox.MsgKeyringLong {
		t.Fatal(err)
	}
	if store[key+inItem] != "old-in" || store[key+outItem] != "old-out" {
		t.Fatalf("store=%v writes=%d", store, writes)
	}
}

func TestWriteMailboxSecretBothSidesAbortsWhenReadFails(t *testing.T) {
	store := installFakeMailboxKeyring(t)
	id := "mbreadfail"
	inItem, err := mailbox.KeyringItem(id, mailbox.SideInbound)
	if err != nil {
		t.Fatal(err)
	}
	outItem, err := mailbox.KeyringItem(id, mailbox.SideOutbound)
	if err != nil {
		t.Fatal(err)
	}
	key := mailbox.KeyringService + "\x00"
	if err := mailboxKeyringSet(mailbox.KeyringService, inItem, "old-in"); err != nil {
		t.Fatal(err)
	}
	if err := mailboxKeyringSet(mailbox.KeyringService, outItem, "old-out"); err != nil {
		t.Fatal(err)
	}
	realGet := mailboxKeyringGet
	mailboxKeyringGet = func(service, item string) (string, error) {
		if item == outItem {
			return "", errors.New("keyring locked")
		}
		return realGet(service, item)
	}
	t.Cleanup(func() { mailboxKeyringGet = realGet })

	if err := writeMailboxSecretBothSides(id, "new-secret"); err == nil || err.Error() != mailbox.MsgKeyringLong {
		t.Fatal(err)
	}
	if store[key+inItem] != "old-in" || store[key+outItem] != "old-out" {
		t.Fatalf("store=%v", store)
	}
}

func TestPutMailboxKeyringSplitsLongSecret(t *testing.T) {
	store := installFakeMailboxKeyring(t)
	id := "mblong"
	secret := strings.Repeat("a", 5000)
	if err := writeMailboxSecretBothSides(id, secret); err != nil {
		t.Fatal(err)
	}
	inItem, err := mailbox.KeyringItem(id, mailbox.SideInbound)
	if err != nil {
		t.Fatal(err)
	}
	got, err := getMailboxKeyring(mailbox.KeyringService, inItem)
	if err != nil || got != secret {
		t.Fatalf("len=%d err=%v", len(got), err)
	}
	prefix := mailbox.KeyringService + "\x00"
	if !strings.HasPrefix(store[prefix+inItem], "maclaw-oauth-chunks:") {
		t.Fatalf("header=%q", store[prefix+inItem])
	}
	for key, value := range store {
		if len(value) > mailboxKeyringChunkLimit {
			t.Fatalf("key %s len %d", key, len(value))
		}
	}
}

type mailboxBrowseFake struct {
	box     string
	uid     uint32
	listErr error
}

func (f *mailboxBrowseFake) Folders(context.Context, mailbox.Account, string) ([]mailbox.Folder, error) {
	return []mailbox.Folder{{Name: "INBOX", Special: "inbox"}, {Name: "Junk", Special: "junk"}}, nil
}
func (f *mailboxBrowseFake) Browse(context.Context, mailbox.Account, string, string, int) (mailbox.BrowsePage, error) {
	return mailbox.BrowsePage{
		Folders:  []mailbox.Folder{{Name: "INBOX", Special: "inbox"}, {Name: "Junk", Special: "junk"}},
		Box:      "INBOX",
		Messages: []mailbox.MessageSummary{{UID: 3, From: "a@ex.com", Subject: "Hello"}},
	}, nil
}
func (f *mailboxBrowseFake) List(_ context.Context, _ mailbox.Account, _, box string, _ int) ([]mailbox.MessageSummary, error) {
	f.box = box
	if f.listErr != nil {
		return nil, f.listErr
	}
	if box == "Empty" {
		return nil, &mailbox.ClassError{Class: "empty", Message: mailbox.MsgEmpty}
	}
	return []mailbox.MessageSummary{{UID: 9, From: "a@ex.com", Subject: "Hello", Date: "2026-10-14"}}, nil
}
func (f *mailboxBrowseFake) Read(_ context.Context, _ mailbox.Account, _, _ string, uid uint32, _ int) (mailbox.Body, error) {
	f.uid = uid
	return mailbox.Body{MessageSummary: mailbox.MessageSummary{UID: uid, From: "a@ex.com", Subject: "Hello"}, Text: "plain note"}, nil
}

func TestMailboxBrowseListsFolderAndReadsMessage(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	installFakeMailboxKeyring(t)
	id, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"inbound": map[string]interface{}{"host": "imap.example.com", "port": 993, "security": "tls", "username": "w"},
	})
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(id, mailbox.SideInbound, "stored-secret"); err != nil {
		t.Fatal(err)
	}
	fake := &mailboxBrowseFake{}
	prev := mailboxToolBrowser
	mailboxToolBrowser = fake
	t.Cleanup(func() { mailboxToolBrowser = prev })

	started, err := app.MailboxBrowseStart(id)
	if err != nil || started["box"] != "INBOX" || len(started["messages"].([]map[string]interface{})) != 1 {
		t.Fatalf("start=%v err=%v", started, err)
	}
	folders, err := app.MailboxBrowseFolders(id)
	if err != nil || len(folders) != 2 || folders[0].Name != "INBOX" {
		t.Fatalf("folders=%+v err=%v", folders, err)
	}
	messages, err := app.MailboxBrowseMessages(id, "Junk", 10)
	if err != nil || fake.box != "Junk" || len(messages) != 1 || messages[0]["subject"] != "Hello" {
		t.Fatalf("messages=%v box=%s err=%v", messages, fake.box, err)
	}
	empty, err := app.MailboxBrowseMessages(id, "Empty", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	body, err := app.MailboxBrowseRead(id, "Junk", 9)
	if err != nil || fake.uid != 9 || body["body"] != "plain note" || strings.Contains(body["body"].(string), mailbox.MsgUntrusted) {
		t.Fatalf("body=%v err=%v", body, err)
	}
	fake.listErr = mailbox.ErrAuth
	if _, err := app.MailboxBrowseMessages(id, "INBOX", 10); err == nil || err.Error() != mailbox.MsgAuth {
		t.Fatalf("auth err=%v", err)
	}
}
