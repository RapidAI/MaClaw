package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

type mailboxHostReader struct {
	account mailbox.Account
	secret  string
	box     string
}

func (r *mailboxHostReader) List(_ context.Context, account mailbox.Account, secret, box string, _ int) ([]mailbox.MessageSummary, error) {
	r.account = account
	r.secret = secret
	r.box = box
	return []mailbox.MessageSummary{{
		UID: 7, From: "a@ex.com", Subject: "Hello", Date: "today",
	}}, nil
}

func (r *mailboxHostReader) Read(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Body, error) {
	return mailbox.Body{}, nil
}

func (r *mailboxHostReader) Search(context.Context, mailbox.Account, string, string, string, int) ([]mailbox.MessageSummary, error) {
	return nil, nil
}

func (r *mailboxHostReader) Attachment(context.Context, mailbox.Account, string, string, uint32, int) (mailbox.Attachment, error) {
	return mailbox.Attachment{}, nil
}

type mailboxHostSender struct {
	calls int
	last  mailbox.OutboundMessage
	err   error
}

func (s *mailboxHostSender) Send(_ context.Context, _ mailbox.Account, _ string, msg mailbox.OutboundMessage) error {
	s.calls++
	s.last = msg
	return s.err
}

func mailboxHostAccount(secret string) (corelib.AppConfig, string) {
	const id = "work1"
	cfg := corelib.AppConfig{
		MailboxAccounts: []mailbox.Account{{
			ID: id, Name: "Work", Address: "work@example.com", Enabled: true, Default: true,
			Inbound:           &mailbox.Endpoint{Host: "imap.example.com", Port: 993, Security: mailbox.SecurityTLS, Username: "work", Box: "INBOX"},
			Outbound:          &mailbox.Endpoint{Host: "smtp.example.com", Port: 587, Security: mailbox.SecuritySTARTTLS, FromAddress: "work@example.com"},
			SecretRefInbound:  mailbox.SecretRef(id, mailbox.SideInbound),
			SecretRefOutbound: mailbox.SecretRef(id, mailbox.SideOutbound),
		}},
		MailboxSignatures: []mailbox.Signature{{
			ID: "sig1", Name: "Public", Body: "此致 {{邮箱名}}", Default: true,
		}},
	}
	return cfg, secret
}

func mailboxHostExecutor(secret string) (*CoreAgentExecutor, *mailboxHostReader, *mailboxHostSender, corelib.AppConfig) {
	cfg, _ := mailboxHostAccount(secret)
	reader := &mailboxHostReader{}
	sender := &mailboxHostSender{}
	exec := &CoreAgentExecutor{
		MailboxToolReader: reader,
		MailboxToolSender: sender,
		MailboxSecretResolver: func(_ context.Context, ref string) (string, error) {
			if ref == cfg.MailboxAccounts[0].SecretRefInbound || ref == cfg.MailboxAccounts[0].SecretRefOutbound {
				return secret, nil
			}
			return "", errors.New("missing mailbox secret")
		},
	}
	return exec, reader, sender, cfg
}

func mailboxHostCallback(exec *CoreAgentExecutor, cfg corelib.AppConfig) *coreAgentCallbacks {
	return &coreAgentCallbacks{
		ctx:       context.Background(),
		appCfg:    cfg,
		principal: Principal{TenantID: "tenant", UserID: "user"},
		session:   Session{ID: "sess"},
		executor:  exec,
	}
}

func TestMailboxHostToolsResolveSavedAccount(t *testing.T) {
	const secret = "super-secret-value"
	exec, reader, sender, cfg := mailboxHostExecutor(secret)
	raw, err := json.Marshal(SanitizeAppConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("sanitized config contains the mailbox secret: %s", raw)
	}

	cb := mailboxHostCallback(exec, cfg)
	tools := cb.BuildTools("")
	seen := map[string]bool{}
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		seen[name] = true
	}
	if !seen["mailbox_query"] || !seen["mailbox_send"] {
		t.Fatalf("tool surface = %#v", seen)
	}

	listed := cb.ExecuteToolStructured("mailbox_query", `{"action":"list_accounts"}`)
	if listed.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(listed.Result, "Work") || !strings.Contains(listed.Result, "work@example.com") || strings.Contains(listed.Result, secret) || strings.Contains(listed.Result, "work1") {
		t.Fatalf("list_accounts outcome=%s result=%s", listed.Outcome, listed.Result)
	}
	queried := cb.ExecuteToolStructured("mailbox_query", `{"action":"list"}`)
	if queried.Outcome != agent.ToolExecutionOutcomeOK || reader.secret != secret || reader.account.Name != "Work" || reader.box != "INBOX" || !strings.Contains(queried.Result, "Hello") || strings.Contains(queried.Result, secret) {
		t.Fatalf("list outcome=%s result=%s reader=%+v", queried.Outcome, queried.Result, reader)
	}

	sent := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["a@ex.com"],"subject":"Hi","text":"body"}`)
	if sender.calls != 0 || !strings.HasPrefix(sent.Result, "__ASK_USER__") || !strings.Contains(sent.Result, "此致 Work") || strings.Contains(sent.Result, "{{") {
		t.Fatalf("send result=%s calls=%d", sent.Result, sender.calls)
	}
	draft, ok := exec.mailboxDraftStore().Pending("tenant:user", "sess")
	if !ok || strings.Contains(sent.Result, draft.ID) {
		t.Fatalf("draft ok=%v result=%s", ok, sent.Result)
	}

	res, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "确认"},
		Config:    cfg,
	})
	if err != nil || res == nil || !strings.Contains(res.Content, "邮件已发送") || sender.calls != 1 || !strings.Contains(sender.last.Text, "此致 Work") {
		t.Fatalf("confirm res=%+v err=%v calls=%d body=%q", res, err, sender.calls, sender.last.Text)
	}
}

func TestMailboxHostCancelAndPendingDoNotDial(t *testing.T) {
	const secret = "super-secret-value"
	exec, _, sender, cfg := mailboxHostExecutor(secret)
	cb := mailboxHostCallback(exec, cfg)
	first := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["a@ex.com"],"subject":"Hi","text":"body"}`)
	if !strings.HasPrefix(first.Result, "__ASK_USER__") || sender.calls != 0 {
		t.Fatalf("first=%s calls=%d", first.Result, sender.calls)
	}
	res, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "not a confirmation"},
		Config:    cfg,
	})
	if err == nil || !strings.Contains(err.Error(), "llm config is incomplete") || sender.calls != 0 || res != nil {
		t.Fatalf("other res=%+v err=%v calls=%d", res, err, sender.calls)
	}
	if _, ok := exec.mailboxDraftStore().Pending("tenant:user", "sess"); !ok {
		t.Fatal("other text dropped the pending draft")
	}
	pending := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["b@ex.com"],"subject":"Hi","text":"again"}`)
	if pending.Result != mailbox.MsgPending || sender.calls != 0 {
		t.Fatalf("pending=%s calls=%d", pending.Result, sender.calls)
	}

	cancelled, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "取消"},
		Config:    cfg,
	})
	if err != nil || cancelled == nil || !strings.Contains(cancelled.Content, mailbox.MsgCancelled) || sender.calls != 0 {
		t.Fatalf("cancel res=%+v err=%v calls=%d", cancelled, err, sender.calls)
	}
	if _, ok := exec.mailboxDraftStore().Pending("tenant:user", "sess"); ok {
		t.Fatal("cancel left the draft pending")
	}
}

func TestMailboxUncertainApprovalBlocksReplay(t *testing.T) {
	const secret = "super-secret-value"
	exec, _, sender, cfg := mailboxHostExecutor(secret)
	sender.err = mailbox.ErrUncertain
	cb := mailboxHostCallback(exec, cfg)
	sent := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["a@ex.com"],"subject":"Hi","text":"body"}`)
	if sender.calls != 0 || !strings.HasPrefix(sent.Result, "__ASK_USER__") {
		t.Fatalf("send=%s calls=%d", sent.Result, sender.calls)
	}
	draft, ok := exec.mailboxDraftStore().Pending("tenant:user", "sess")
	if !ok {
		t.Fatal("confirm had no frozen draft")
	}
	res, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "确认"},
		Config:    cfg,
	})
	if err != nil || res == nil || sender.calls != 1 || !strings.Contains(res.Content, mailbox.MsgExternalReview) {
		t.Fatalf("confirm res=%+v err=%v calls=%d", res, err, sender.calls)
	}
	rev, latched := exec.mailboxReview("tenant:user", "sess")
	if !latched || rev.SlotID != "mailbox-"+draft.ID || rev.SideEffectState != "external_uncertain" || rev.RecoveryMode != "requires_review" || rev.LastToolName != "mailbox_send" {
		t.Fatalf("review=%+v latched=%v", rev, latched)
	}
	resume, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "继续上次未完成任务"},
		Config:    cfg,
	})
	if err != nil || resume == nil || resume.Content != mailbox.MsgExternalReview || sender.calls != 1 {
		t.Fatalf("resume res=%+v err=%v calls=%d", resume, err, sender.calls)
	}
	again, err := exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "确认"},
		Config:    cfg,
	})
	if err != nil || again == nil || again.Content != mailbox.MsgExternalReview || sender.calls != 1 {
		t.Fatalf("second confirm res=%+v err=%v calls=%d", again, err, sender.calls)
	}
	blocked := cb.ExecuteToolStructured("mailbox_send", `{"action":"send","to":["b@ex.com"],"subject":"Hi","text":"again"}`)
	if blocked.Result != mailbox.MsgExternalReview || sender.calls != 1 {
		t.Fatalf("blocked=%s calls=%d", blocked.Result, sender.calls)
	}
	_, _ = exec.Execute(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "tenant", UserID: "user"},
		Session:   Session{ID: "sess"},
		Message:   Message{Content: "__start_new_task__"},
		Config:    cfg,
	})
	if _, still := exec.mailboxReview("tenant:user", "sess"); still || sender.calls != 1 {
		t.Fatalf("dismiss left the review latched, calls=%d", sender.calls)
	}
}

func TestMailboxFileSecretResolverReadsScopedFile(t *testing.T) {
	dir := t.TempDir()
	const secret = "file-secret-value"
	if err := WriteMailboxSecret(dir, "tenant", "user", "work1", mailbox.SideInbound, secret); err != nil {
		t.Fatal(err)
	}
	if err := WriteMailboxSecret(dir, "tenant/../other", "user", "work1", mailbox.SideInbound, secret); err == nil {
		t.Fatal("path traversal was accepted")
	}
	cfg, _ := mailboxHostAccount(secret)
	reader := &mailboxHostReader{}
	exec := &CoreAgentExecutor{MailboxSecretDir: dir, MailboxToolReader: reader, MailboxToolSender: &mailboxHostSender{}}
	cb := mailboxHostCallback(exec, cfg)
	out := cb.ExecuteToolStructured("mailbox_query", `{"action":"list"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || reader.secret != secret || strings.Contains(out.Result, secret) {
		t.Fatalf("outcome=%s result=%s secret=%q", out.Outcome, out.Result, reader.secret)
	}
}
