package agentservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

// ReplaceMailboxAccounts replaces only the mailbox account and signature
// slices on the saved user config. The rest of the config is kept. Secrets
// stay out of this body.
func (s *Service) ReplaceMailboxAccounts(ctx context.Context, p Principal, accounts []mailbox.Account, signatures []mailbox.Signature) (*UserConfig, error) {
	_ = ctx
	if s == nil {
		return nil, errors.New("service is nil")
	}
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	current, err := s.getOrLoadRawUserConfig(p.TenantID, p.UserID)
	if err != nil && !errors.Is(err, ErrUserConfigNotFound) {
		return nil, err
	}
	next := current.AppConfig
	if accounts == nil {
		accounts = []mailbox.Account{}
	}
	if signatures == nil {
		signatures = []mailbox.Signature{}
	}
	next.MailboxAccounts = accounts
	next.MailboxSignatures = signatures
	// Save the mailbox slices only. UpdateUserConfig would revoke dynamic
	// capability contracts and is the partial-config path that drops fields
	// the caller omitted.
	merged := normalizeLLMConfigForSave(current.AppConfig, next)
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	cfg := UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: merged, UpdatedAt: now}
	if err := s.store.SaveUserConfig(cfg); err != nil {
		return nil, err
	}
	if err := saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg); err != nil {
		return nil, err
	}
	if err := s.refreshUserInstanceReadinessCache(p.TenantID, p.UserID, ValidateAppConfig(cfg.AppConfig), cfg.UpdatedAt); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{
		TenantID: p.TenantID, UserID: p.UserID, Action: "mailbox.accounts_replaced",
		ResourceType: "mailbox", ResourceID: p.UserID, ActorType: "user",
		ActorTenantID: p.TenantID, ActorUserID: p.UserID,
		Metadata: map[string]string{
			"account_count":   fmt.Sprintf("%d", len(accounts)),
			"signature_count": fmt.Sprintf("%d", len(signatures)),
		},
	})
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
}

// MailboxConfigBody is an in-process account list. It is not an HTTP body
// and it never carries a password.
type MailboxConfigBody struct {
	Accounts   []mailbox.Account   `json:"accounts"`
	Signatures []mailbox.Signature `json:"signatures"`
}

// Normalize returns empty slices instead of nulls.
func (b MailboxConfigBody) Normalize() MailboxConfigBody {
	if b.Accounts == nil {
		b.Accounts = []mailbox.Account{}
	}
	if b.Signatures == nil {
		b.Signatures = []mailbox.Signature{}
	}
	return b
}

// AppConfigMailbox is the sanitized config returned to the desktop after a
// mailbox replace. It is not filtered down to the web-visible key set.
func AppConfigMailbox(cfg corelib.AppConfig) corelib.AppConfig {
	if cfg.MailboxAccounts == nil {
		cfg.MailboxAccounts = []mailbox.Account{}
	}
	if cfg.MailboxSignatures == nil {
		cfg.MailboxSignatures = []mailbox.Signature{}
	}
	return cfg
}
