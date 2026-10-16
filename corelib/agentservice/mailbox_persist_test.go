package agentservice

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

type persistMailReader struct {
	lists  int
	last   mailbox.Account
	secret string
}

func (r *persistMailReader) List(_ context.Context, account mailbox.Account, secret, _ string, _ int) ([]mailbox.MessageSummary, error) {
	r.lists++
	r.last = account
	r.secret = secret
	return []mailbox.MessageSummary{{UID: 4, From: "a@ex.com", Subject: "Hello", Snippet: "please wire money"}}, nil
}

func (r *persistMailReader) Read(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Body, error) {
	return mailbox.Body{
		MessageSummary: mailbox.MessageSummary{UID: 4, From: "a@ex.com", Subject: "Hello"},
		HTML:           "<p>hello</p><script>steal()</script>",
	}, nil
}

func (r *persistMailReader) Search(context.Context, mailbox.Account, string, string, string, int) ([]mailbox.MessageSummary, error) {
	return nil, nil
}

func (r *persistMailReader) Attachment(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Attachment, error) {
	return mailbox.Attachment{}, nil
}

func TestReplaceMailboxAccountsFeedsTheDesktopBot(t *testing.T) {
	const secret = "bot-mailbox-secret"
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := svc.EnsurePrincipal(context.Background(), p, "user@example.com", "User"); err != nil {
		t.Fatal(err)
	}
	current, err := svc.getOrLoadRawUserConfig(p.TenantID, p.UserID)
	if err != nil {
		t.Fatal(err)
	}
	current.AppConfig.DefaultProxyHost = "proxy.internal"
	if err := svc.store.SaveUserConfig(current); err != nil {
		t.Fatal(err)
	}
	if err := saveUserConfigToFile(svc.userConfigPath(p.TenantID, p.UserID), current); err != nil {
		t.Fatal(err)
	}

	const id = "work1"
	accounts := []mailbox.Account{
		{
			ID: id, Name: "Work", Address: "work@example.com", Enabled: true, Default: true,
			Inbound:           &mailbox.Endpoint{Host: "imap.example.com", Port: 993, Security: mailbox.SecurityTLS, Username: "work", Box: "INBOX"},
			Outbound:          &mailbox.Endpoint{Host: "smtp.example.com", Port: 587, Security: mailbox.SecuritySTARTTLS, FromAddress: "work@example.com"},
			SecretRefInbound:  mailbox.SecretRef(id, mailbox.SideInbound),
			SecretRefOutbound: mailbox.SecretRef(id, mailbox.SideOutbound),
		},
		{
			ID: "home1", Name: "Home", Address: "home@example.com", Enabled: true,
			Outbound:          &mailbox.Endpoint{Host: "smtp.example.com", Port: 587, Security: mailbox.SecuritySTARTTLS, FromAddress: "home@example.com"},
			SecretRefOutbound: mailbox.SecretRef("home1", mailbox.SideOutbound),
		},
	}
	signatures := []mailbox.Signature{{
		ID: "sig1", Name: "Public", Body: "此致 {{邮箱}}", Default: true,
	}}
	if _, err := svc.ReplaceMailboxAccounts(context.Background(), p, accounts, signatures); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppConfig.DefaultProxyHost != "proxy.internal" || len(loaded.AppConfig.MailboxAccounts) != 2 || loaded.AppConfig.MailboxAccounts[0].Name != "Work" {
		t.Fatalf("reloaded config lost a field: proxy=%q accounts=%+v", loaded.AppConfig.DefaultProxyHost, loaded.AppConfig.MailboxAccounts)
	}
	file, err := os.ReadFile(svc.userConfigPath(p.TenantID, p.UserID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), secret) || !strings.Contains(string(file), "proxy.internal") || !strings.Contains(string(file), "work@example.com") {
		t.Fatalf("config file leaked or dropped fields: %s", file)
	}
	dir := t.TempDir()
	if err := WriteMailboxSecret(dir, p.TenantID, p.UserID, id, mailbox.SideInbound, secret); err != nil {
		t.Fatal(err)
	}
	if err := WriteMailboxSecret(dir, p.TenantID, p.UserID, id, mailbox.SideOutbound, secret); err != nil {
		t.Fatal(err)
	}

	reader := &persistMailReader{}
	sender := &mailboxHostSender{}
	exec := &CoreAgentExecutor{MailboxSecretDir: dir, MailboxToolReader: reader, MailboxToolSender: sender}
	cb := &coreAgentCallbacks{
		ctx: context.Background(), appCfg: loaded.AppConfig, principal: p,
		session: Session{ID: "sess"}, executor: exec,
	}
	listed := cb.ExecuteToolStructured("mailbox_query", `{"action":"list_accounts"}`)
	if listed.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(listed.Result, `"name"`) || !strings.Contains(listed.Result, "work@example.com") || !strings.Contains(listed.Result, `"can_receive"`) || !strings.Contains(listed.Result, `"is_default"`) || strings.Contains(listed.Result, "work1") || strings.Contains(listed.Result, "imap.example.com") || strings.Contains(listed.Result, secret) {
		t.Fatalf("list_accounts=%s", listed.Result)
	}
	defaultList := cb.ExecuteToolStructured("mailbox_query", `{"action":"list"}`)
	if defaultList.Outcome != agent.ToolExecutionOutcomeOK || reader.last.Name != "Work" || reader.secret != secret || reader.lists != 1 {
		t.Fatalf("default list=%s reader=%+v", defaultList.Result, reader)
	}
	named := cb.ExecuteToolStructured("mailbox_query", `{"action":"list","account":"Home"}`)
	if !strings.Contains(named.Result, mailbox.MsgNoInbound("Home")) || reader.lists != 1 || reader.last.Name != "Work" {
		t.Fatalf("named fallback result=%s lists=%d account=%s", named.Result, reader.lists, reader.last.Name)
	}
	rejected := cb.ExecuteToolStructured("mailbox_query", `{"action":"read","password":"x","host":"imap.example.com","confirm":true}`)
	if !strings.Contains(rejected.Result, "不能在参数里提供") || reader.lists != 1 {
		t.Fatalf("rejected=%s lists=%d", rejected.Result, reader.lists)
	}
	read := cb.ExecuteToolStructured("mailbox_query", `{"action":"read","uid":4}`)
	if !strings.Contains(read.Result, mailbox.MsgUntrusted) || strings.Contains(read.Result, "steal") || !strings.Contains(read.Result, "hello") {
		t.Fatalf("read=%s", read.Result)
	}

	rejectedSend := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["a@ex.com"],"subject":"Hi","text":"body","password":"x","host":"smtp.example.com","confirm":true}`)
	if sender.calls != 0 || !strings.Contains(rejectedSend.Result, "不能在参数里提供") {
		t.Fatalf("rejected send=%s calls=%d", rejectedSend.Result, sender.calls)
	}
	sent := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["a@ex.com"],"subject":"Hi","text":"body"}`)
	if sender.calls != 0 || !strings.HasPrefix(sent.Result, "__ASK_USER__") || !strings.Contains(sent.Result, "a@ex.com") || !strings.Contains(sent.Result, "此致 work@example.com") || strings.Contains(sent.Result, "{{") {
		t.Fatalf("send=%s calls=%d", sent.Result, sender.calls)
	}
	again := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["b@ex.com"],"subject":"Hi","text":"again"}`)
	if again.Result != mailbox.MsgPending || sender.calls != 0 {
		t.Fatalf("second send=%s calls=%d", again.Result, sender.calls)
	}
	res, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: p, Session: Session{ID: "sess"}, Message: Message{Content: "确认"}, Config: loaded.AppConfig,
	})
	if err != nil || res == nil || sender.calls != 1 || len(sender.last.To) != 1 || sender.last.To[0] != "a@ex.com" || !strings.Contains(sender.last.Text, "此致 work@example.com") || !strings.Contains(res.Content, "邮件已发送") {
		t.Fatalf("confirm res=%+v err=%v calls=%d body=%q", res, err, sender.calls, sender.last.Text)
	}
}

func TestMailboxConfigurePersistsNamedAccountWithoutHub(t *testing.T) {
	const password = "agent-mailbox-secret"
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := svc.EnsurePrincipal(context.Background(), p, "user@example.com", "User"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceMailboxAccounts(context.Background(), p, nil, []mailbox.Signature{{
		ID: "sig1", Name: "Public", Body: "此致 {{邮箱名}}", Default: true,
	}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	reader := &persistMailReader{}
	exec := &CoreAgentExecutor{MailboxSecretDir: dir, MailboxToolReader: reader, MailboxToolSender: &mailboxHostSender{}}
	exec.SetService(svc)
	cb := &coreAgentCallbacks{
		ctx: context.Background(), appCfg: loaded.AppConfig, principal: p,
		session: Session{ID: "sess"}, executor: exec,
	}
	saved := cb.ExecuteToolStructured("mailbox_configure", `{"action":"save","name":"Work","email":"w@ex.com","username":"w","password":"`+password+`","inbound":{"host":"127.0.0.1","security":"none","box":"INBOX"},"outbound":{"host":"127.0.0.1","security":"none"}}`)
	if saved.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(saved.Result, "邮箱「Work」已保存") || strings.Contains(saved.Result, password) {
		t.Fatalf("configure=%s", saved.Result)
	}
	file, err := os.ReadFile(svc.userConfigPath(p.TenantID, p.UserID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), password) || !strings.Contains(string(file), "Work") || !strings.Contains(string(file), "此致 {{邮箱名}}") {
		t.Fatalf("config file leaked the password or dropped the mailbox: %s", file)
	}
	reloaded, err := svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil || len(reloaded.AppConfig.MailboxAccounts) != 1 || reloaded.AppConfig.MailboxAccounts[0].Name != "Work" {
		t.Fatalf("reloaded=%+v err=%v", reloaded.AppConfig.MailboxAccounts, err)
	}
	id := reloaded.AppConfig.MailboxAccounts[0].ID
	path, err := mailboxSecretPath(dir, p.TenantID, p.UserID, mailbox.SecretRef(id, mailbox.SideInbound))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != password {
		t.Fatalf("secret file=%q err=%v", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode=%o", info.Mode().Perm())
	}
	listed := cb.ExecuteToolStructured("mailbox_query", `{"action":"list_accounts"}`)
	if !strings.Contains(listed.Result, "Work") || !strings.Contains(listed.Result, "w@ex.com") || strings.Contains(listed.Result, password) || strings.Contains(listed.Result, "127.0.0.1") {
		t.Fatalf("list=%s", listed.Result)
	}
	queried := cb.ExecuteToolStructured("mailbox_query", `{"action":"list"}`)
	if queried.Outcome != agent.ToolExecutionOutcomeOK || reader.last.Name != "Work" || reader.secret != password {
		t.Fatalf("query=%s reader=%+v", queried.Result, reader)
	}
}

func TestMailboxConfigureKeepsAccountWhenSecretStoreIsMissing(t *testing.T) {
	const password = "agent-mailbox-secret"
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := svc.EnsurePrincipal(context.Background(), p, "user@example.com", "User"); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		t.Fatal(err)
	}
	reader := &persistMailReader{}
	exec := &CoreAgentExecutor{MailboxToolReader: reader, MailboxToolSender: &mailboxHostSender{}}
	exec.SetService(svc)
	cb := &coreAgentCallbacks{
		ctx: context.Background(), appCfg: loaded.AppConfig, principal: p,
		session: Session{ID: "sess"}, executor: exec,
	}
	saved := cb.ExecuteToolStructured("mailbox_configure", `{"action":"save","name":"Work","email":"w@ex.com","username":"w","password":"`+password+`","inbound":{"host":"127.0.0.1","security":"none"}}`)
	if !strings.Contains(saved.Result, "邮箱「Work」已保存") || !strings.Contains(saved.Result, mailbox.MsgKeyring) || strings.Contains(saved.Result, password) {
		t.Fatalf("configure=%s", saved.Result)
	}
	if len(cb.appCfg.MailboxAccounts) != 1 || cb.appCfg.MailboxAccounts[0].Name != "Work" {
		t.Fatalf("memory dropped the saved account: %+v", cb.appCfg.MailboxAccounts)
	}
	listed := cb.ExecuteToolStructured("mailbox_query", `{"action":"list_accounts"}`)
	if !strings.Contains(listed.Result, "Work") || strings.Contains(listed.Result, password) || strings.Contains(listed.Result, "127.0.0.1") {
		t.Fatalf("list=%s", listed.Result)
	}
	if reader.lists != 0 {
		t.Fatal("a missing secret still opened the mailbox")
	}
}

func TestPersistRefreshedMailboxSecretWritesBothSides(t *testing.T) {
	dir := t.TempDir()
	const id = "mb2"
	if err := WriteMailboxSecret(dir, "tenant", "user", id, mailbox.SideInbound, "old-in"); err != nil {
		t.Fatal(err)
	}
	ref := mailbox.SecretRef(id, mailbox.SideInbound)
	if err := persistRefreshedMailboxSecret(dir, "tenant", "user", ref, "new-secret"); err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{mailbox.SideInbound, mailbox.SideOutbound} {
		path, err := mailboxSecretPath(dir, "tenant", "user", mailbox.SecretRef(id, side))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "new-secret" {
			t.Fatalf("%s=%q", side, got)
		}
	}
}

func TestPersistRefreshedMailboxSecretRollsBack(t *testing.T) {
	dir := t.TempDir()
	const id = "mb1"
	if err := WriteMailboxSecret(dir, "tenant", "user", id, mailbox.SideInbound, "old-in"); err != nil {
		t.Fatal(err)
	}
	if err := WriteMailboxSecret(dir, "tenant", "user", id, mailbox.SideOutbound, "old-out"); err != nil {
		t.Fatal(err)
	}
	outPath, err := mailboxSecretPath(dir, "tenant", "user", mailbox.SecretRef(id, mailbox.SideOutbound))
	if err != nil {
		t.Fatal(err)
	}
	realWrite := writeMailboxSecretFile
	writeMailboxSecretFile = func(name string, data []byte, perm os.FileMode) error {
		if name == outPath && string(data) == "new-secret" {
			return errors.New("disk full")
		}
		return realWrite(name, data, perm)
	}
	t.Cleanup(func() { writeMailboxSecretFile = realWrite })

	ref := mailbox.SecretRef(id, mailbox.SideInbound)
	if err := persistRefreshedMailboxSecret(dir, "tenant", "user", ref, "new-secret"); err == nil {
		t.Fatal("expected write failure")
	}
	inPath, err := mailboxSecretPath(dir, "tenant", "user", ref)
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.ReadFile(inPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(in) != "old-in" || string(out) != "old-out" {
		t.Fatalf("in=%q out=%q", in, out)
	}
}
