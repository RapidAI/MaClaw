package agentservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

func mailboxToolContext(c *coreAgentCallbacks) context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (e *CoreAgentExecutor) mailboxDraftStore() *mailbox.DraftStore {
	if e == nil {
		return mailbox.NewDraftStore()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mailboxDrafts == nil {
		e.mailboxDrafts = mailbox.NewDraftStore()
	}
	return e.mailboxDrafts
}

func (e *CoreAgentExecutor) mailboxSecrets(principal Principal) mailbox.SecretResolver {
	if e != nil && e.MailboxSecretResolver != nil {
		return e.MailboxSecretResolver
	}
	dir := ""
	if e != nil {
		dir = e.MailboxSecretDir
	}
	return mailboxFileResolver(dir, principal.TenantID, principal.UserID)
}

func (e *CoreAgentExecutor) mailboxHost(ctx context.Context, req ExecuteRequest, budget *mailbox.TurnBudget) *mailbox.Host {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ctx
	reader, sender := mailboxPeers(e)
	principal := memoryOwnerIDForPrincipal(req.Principal)
	session := strings.TrimSpace(req.Session.ID)
	return &mailbox.Host{
		Accounts:   req.Config.MailboxAccounts,
		Signatures: req.Config.MailboxSignatures,
		Secrets:    e.mailboxSecrets(req.Principal),
		Reader:     reader,
		Sender:     sender,
		WorkingDir: strings.TrimSpace(req.Instance.Workspace),
		Budget:     budget,
		Drafts:     e.mailboxDraftStore(),
		Principal:  principal,
		Session:    session,
		OnUncertain: func(draft mailbox.Draft) {
			e.noteMailboxReview(principal, session, draft)
		},
	}
}

func mailboxPeers(e *CoreAgentExecutor) (mailbox.MailReader, mailbox.MailSender) {
	var reader mailbox.MailReader
	var sender mailbox.MailSender
	if e != nil {
		reader = e.MailboxToolReader
		sender = e.MailboxToolSender
	}
	// A test that injects either peer must not dial the other side. Production
	// leaves both nil and uses one network dialer for IMAP and SMTP.
	if reader == nil && sender == nil {
		dialer := mailbox.NewNetDialer()
		return dialer, dialer
	}
	return reader, sender
}

func (c *coreAgentCallbacks) mailboxRuntime() *mailbox.Host {
	if c == nil {
		return &mailbox.Host{Budget: mailbox.NewTurnBudget(), Drafts: mailbox.NewDraftStore()}
	}
	if c.mailboxHost != nil {
		return c.mailboxHost
	}
	reader, sender := mailboxPeers(c.executor)
	drafts := mailbox.NewDraftStore()
	var secrets mailbox.SecretResolver
	if c.executor != nil {
		drafts = c.executor.mailboxDraftStore()
		secrets = c.executor.mailboxSecrets(c.principal)
	}
	c.mailboxHost = &mailbox.Host{
		Accounts:   c.appCfg.MailboxAccounts,
		Signatures: c.appCfg.MailboxSignatures,
		Secrets:    secrets,
		Reader:     reader,
		Sender:     sender,
		WorkingDir: c.workspace,
		Budget:     mailbox.NewTurnBudget(),
		Drafts:     drafts,
		Principal:  memoryOwnerIDForPrincipal(c.principal),
		Session:    strings.TrimSpace(c.session.ID),
	}
	return c.mailboxHost
}

// consumeMailboxConfirmation sends or cancels a frozen draft before the model
// runs. Any other text leaves the draft in place and does not contact SMTP.
func (e *CoreAgentExecutor) consumeMailboxConfirmation(ctx context.Context, req ExecuteRequest) (string, bool) {
	if e == nil {
		return "", false
	}
	text := strings.TrimSpace(req.Message.Content)
	principal := memoryOwnerIDForPrincipal(req.Principal)
	session := strings.TrimSpace(req.Session.ID)
	if _, latched := e.mailboxReview(principal, session); latched {
		if mailboxReviewDismiss(text) {
			e.clearMailboxReview(principal, session)
			if draft, ok := e.mailboxDraftStore().Pending(principal, session); ok {
				e.mailboxDraftStore().Drop(draft.ID)
			}
			return "", false
		}
		return mailbox.MsgExternalReview, true
	}
	draft, ok := e.mailboxDraftStore().Pending(principal, session)
	if !ok {
		return "", false
	}
	if mailbox.IsApproveText(text) {
		result, class := e.mailboxHost(ctx, req, mailbox.NewTurnBudget()).Approve(ctx, draft.ID)
		if class == "approved" {
			result += "\n这封邮件已经按确认发送。不要再次调用 mailbox_send。"
		}
		if class == "uncertain" && !strings.Contains(result, mailbox.MsgExternalReview) {
			result += "\n" + mailbox.MsgExternalReview
		}
		return result, true
	}
	if mailbox.IsCancelText(text) {
		result, _ := e.mailboxHost(ctx, req, mailbox.NewTurnBudget()).Cancel(draft.ID)
		return result, true
	}
	return "", false
}

// mailboxExternalReview is the desktop-bot form of the GUI unfinished-slot
// review. A later confirmation or mailbox_send must not dial SMTP again.
type mailboxExternalReview struct {
	SlotID          string
	SideEffectState string
	RecoveryMode    string
	LastToolName    string
}

func mailboxReviewKey(principal, session string) string {
	return principal + "\x00" + session
}

func (e *CoreAgentExecutor) noteMailboxReview(principal, session string, draft mailbox.Draft) {
	if e == nil {
		return
	}
	slotID := "mailbox-" + strings.TrimSpace(draft.ID)
	if strings.TrimSpace(draft.ID) == "" {
		slotID = "mailbox-send-uncertain"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mailboxReviews == nil {
		e.mailboxReviews = map[string]mailboxExternalReview{}
	}
	e.mailboxReviews[mailboxReviewKey(principal, session)] = mailboxExternalReview{
		SlotID:          slotID,
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
		LastToolName:    "mailbox_send",
	}
}

func (e *CoreAgentExecutor) mailboxReview(principal, session string) (mailboxExternalReview, bool) {
	if e == nil {
		return mailboxExternalReview{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rev, ok := e.mailboxReviews[mailboxReviewKey(principal, session)]
	return rev, ok
}

func (e *CoreAgentExecutor) clearMailboxReview(principal, session string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.mailboxReviews, mailboxReviewKey(principal, session))
}

func mailboxReviewDismiss(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "__start_new_task__" || trimmed == "开始新任务" {
		return true
	}
	return strings.HasPrefix(trimmed, "__dismiss_unfinished__")
}

func (c *coreAgentCallbacks) mailboxReviewLatched() bool {
	if c == nil || c.executor == nil {
		return false
	}
	_, ok := c.executor.mailboxReview(memoryOwnerIDForPrincipal(c.principal), strings.TrimSpace(c.session.ID))
	return ok
}

// configureMailbox saves one account from the details the user told the agent.
// The password goes to the local secret file, not into the result or Hub.
func (c *coreAgentCallbacks) configureMailbox(args map[string]interface{}) string {
	if c == nil {
		return mailbox.MsgNotConfigured
	}
	next, saved, secrets, msg, err := mailbox.ApplyConfigure(c.appCfg.MailboxAccounts, args, time.Now())
	if err != nil {
		return configureMailboxErr(err, stringArg(args, "password"))
	}
	var svc *Service
	if c.executor != nil {
		svc = c.executor.getService()
	}
	if svc != nil {
		if _, err := svc.ReplaceMailboxAccounts(mailboxToolContext(c), c.principal, next, c.appCfg.MailboxSignatures); err != nil {
			return "邮箱没有保存"
		}
	}
	secretErr := ""
	if len(secrets) > 0 {
		dir := ""
		if c.executor != nil {
			dir = strings.TrimSpace(c.executor.MailboxSecretDir)
		}
		if dir == "" {
			secretErr = mailbox.MsgKeyring
		} else {
			for _, sec := range secrets {
				if err := WriteMailboxSecret(dir, c.principal.TenantID, c.principal.UserID, sec.ID, sec.Side, sec.Secret); err != nil {
					secretErr = mailbox.MsgKeyring
					break
				}
			}
		}
	}
	// A missing password must not hide an account that was already stored.
	// Later tools in this turn read c.appCfg, not the disk copy.
	if svc != nil || c.executor == nil || secretErr == "" {
		c.appCfg.MailboxAccounts = next
		if c.mailboxHost != nil {
			c.mailboxHost.Accounts = next
		}
	}
	if secretErr != "" {
		return fmt.Sprintf("邮箱「%s」已保存，但%s", saved.Name, secretErr)
	}
	return msg
}

func configureMailboxErr(err error, secret string) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if secret != "" && strings.Contains(text, secret) {
		return mailbox.MsgKeyring
	}
	return text
}

func mailboxFileResolver(dir, tenant, user string) mailbox.SecretResolver {
	return func(_ context.Context, ref string) (string, error) {
		path, err := mailboxSecretPath(dir, tenant, user, ref)
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		secret := string(raw)
		if strings.TrimSpace(secret) == "" {
			return "", errors.New("mailbox secret is empty")
		}
		return secret, nil
	}
}

// WriteMailboxSecret stores one mailbox password as a 0600 file. The secret
// is not included in the returned error.
func WriteMailboxSecret(dir, tenant, user, id, side, secret string) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New("mailbox secret is empty")
	}
	ref := mailbox.SecretRef(id, side)
	path, err := mailboxSecretPath(dir, tenant, user, ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(secret), 0o600)
}

func mailboxSecretPath(dir, tenant, user, ref string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("mailbox secret directory is not configured")
	}
	item, err := mailbox.ParseSecretRef(ref)
	if err != nil {
		return "", errors.New("invalid mailbox secret ref")
	}
	rest := strings.TrimPrefix(item, "mailbox/")
	id, side, ok := strings.Cut(rest, "/")
	if !ok || !mailbox.ValidID(id) || (side != mailbox.SideInbound && side != mailbox.SideOutbound) {
		return "", errors.New("invalid mailbox secret ref")
	}
	if !safeMailboxPathSegment(tenant) || !safeMailboxPathSegment(user) {
		return "", errors.New("invalid mailbox principal")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, tenant, user, id, side)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid mailbox secret path")
	}
	return path, nil
}

func safeMailboxPathSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return false
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, `/\`) || strings.Contains(value, ":") {
		return false
	}
	return true
}
