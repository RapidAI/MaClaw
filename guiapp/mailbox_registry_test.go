package guiapp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

type registryMailReader struct {
	lists  int
	last   mailbox.Account
	secret string
}

func (r *registryMailReader) List(_ context.Context, account mailbox.Account, secret, _ string, _ int) ([]mailbox.MessageSummary, error) {
	r.lists++
	r.last = account
	r.secret = secret
	return []mailbox.MessageSummary{{UID: 9, From: "a@ex.com", Subject: "Hello"}}, nil
}

func (r *registryMailReader) Read(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Body, error) {
	return mailbox.Body{
		MessageSummary: mailbox.MessageSummary{UID: 9, From: "a@ex.com", Subject: "Hello"},
		HTML:           "<p>hello</p><script>steal()</script>",
	}, nil
}

func (r *registryMailReader) Search(context.Context, mailbox.Account, string, string, string, int) ([]mailbox.MessageSummary, error) {
	return nil, nil
}

func (r *registryMailReader) Attachment(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Attachment, error) {
	return mailbox.Attachment{}, nil
}

func TestMailboxRegistryQueryUsesSavedAccount(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	installFakeMailboxKeyring(t)
	reader := &registryMailReader{}
	oldReader := mailboxToolReader
	mailboxToolReader = reader
	t.Cleanup(func() { mailboxToolReader = oldReader })
	sender := &captureMailSender{}
	installMailboxSender(t, sender)

	if _, err := app.MailboxSignatureSave(map[string]interface{}{
		"name": "公共", "body": "此致 {{邮箱名}}", "default": true,
	}); err != nil {
		t.Fatal(err)
	}
	workID, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Work", "address": "w@ex.com",
		"inbound":  map[string]interface{}{"host": "127.0.0.1", "security": "none", "username": "w", "box": "INBOX"},
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none", "from_address": "w@ex.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.MailboxAccountSave(map[string]interface{}{
		"name": "Home", "address": "h@ex.com",
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(workID, "inbound", "imap-secret"); err != nil {
		t.Fatal(err)
	}
	if err := app.MailboxAccountSetSecret(workID, "outbound", "smtp-secret"); err != nil {
		t.Fatal(err)
	}

	h := &IMMessageHandler{app: app}
	registry := NewToolRegistry()
	registerBuiltinTools(registry, h)
	query, ok := registry.Get("mailbox_query")
	send, sendOK := registry.Get("mailbox_send")
	if !ok || !sendOK || query.HandlerCtx == nil || send.HandlerCtx == nil {
		t.Fatal("mailbox tools are not registered")
	}
	ctx := withMailboxOwner(context.Background(), "registry-user")
	listed := query.HandlerCtx(ctx, map[string]interface{}{"action": "list"}, nil)
	if reader.lists != 1 || reader.last.Name != "Work" || reader.secret != "imap-secret" || strings.Contains(listed, workID) || strings.Contains(listed, "127.0.0.1") || strings.Contains(listed, "imap-secret") {
		t.Fatalf("list=%s reader=%+v", listed, reader)
	}
	named := query.HandlerCtx(ctx, map[string]interface{}{"action": "list", "account": "Home"}, nil)
	if !strings.Contains(named, mailbox.MsgNoInbound("Home")) || reader.lists != 1 {
		t.Fatalf("named=%s lists=%d", named, reader.lists)
	}
	rejected := query.HandlerCtx(ctx, map[string]interface{}{"action": "read", "password": "x", "host": "127.0.0.1", "confirm": true, "folder": "INBOX"}, nil)
	if !strings.Contains(rejected, "不能在参数里提供") || reader.lists != 1 {
		t.Fatalf("rejected=%s", rejected)
	}
	read := query.HandlerCtx(ctx, map[string]interface{}{"action": "read", "uid": float64(9)}, nil)
	if !strings.Contains(read, mailbox.MsgUntrusted) || strings.Contains(read, "steal") || !strings.Contains(read, "hello") {
		t.Fatalf("read=%s", read)
	}
	card := send.HandlerCtx(ctx, map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "Hi", "text": "body",
	}, nil)
	if sender.calls != 0 || !strings.HasPrefix(card, "__ASK_USER__") || !strings.Contains(card, "a@ex.com") || !strings.Contains(card, "此致 Work") || strings.Contains(card, "{{") || strings.Contains(card, "smtp-secret") {
		t.Fatalf("card=%s calls=%d", card, sender.calls)
	}
}

func TestMailboxConfigureSavesNameInKeyringOnly(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeMailboxKeyring(t)
	h := &IMMessageHandler{app: app}
	registry := NewToolRegistry()
	registerBuiltinTools(registry, h)
	configure, ok := registry.Get("mailbox_configure")
	if !ok || configure.HandlerCtx == nil {
		t.Fatal("mailbox_configure is not registered")
	}
	const password = "agent-mailbox-secret"
	ctx := withMailboxOwner(context.Background(), "registry-user")
	msg := configure.HandlerCtx(ctx, map[string]interface{}{
		"action": "save", "name": "Work", "email": "w@ex.com", "username": "w", "password": password,
		"inbound":  map[string]interface{}{"host": "127.0.0.1", "security": "none", "box": "INBOX"},
		"outbound": map[string]interface{}{"host": "127.0.0.1", "security": "none"},
	}, nil)
	if !strings.Contains(msg, "邮箱「Work」已保存") || strings.Contains(msg, password) {
		t.Fatalf("configure=%s", msg)
	}
	cfg, err := app.LoadConfig()
	if err != nil || len(cfg.MailboxAccounts) != 1 || cfg.MailboxAccounts[0].Name != "Work" {
		t.Fatalf("accounts=%+v err=%v", cfg.MailboxAccounts, err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(raw), password) {
		t.Fatalf("config leaked the password: %s", raw)
	}
	found := false
	for key, secret := range store {
		if secret == password {
			found = true
			if !strings.Contains(key, mailbox.KeyringService) {
				t.Fatalf("secret stored outside the maclaw keyring: %s", key)
			}
		}
	}
	if !found {
		t.Fatal("keyring does not hold the mailbox password")
	}
	query, ok := registry.Get("mailbox_query")
	if !ok {
		t.Fatal("mailbox_query is not registered")
	}
	listed := query.HandlerCtx(ctx, map[string]interface{}{"action": "list_accounts"}, nil)
	if !strings.Contains(listed, "Work") || !strings.Contains(listed, "w@ex.com") || strings.Contains(listed, password) {
		t.Fatalf("list=%s", listed)
	}
}
