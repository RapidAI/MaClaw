package guiapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// beginMailboxOAuth is the browser login. Tests replace it so they do not
// open a window or talk to Google and Microsoft.
var beginMailboxOAuth = mailbox.LoginOAuthWithHint

// MailboxOAuthLogin signs in to Gmail or Hotmail in the browser and saves the
// mailbox. hint is the address of an existing mailbox being signed in again.
// The user does not type a server, port, or password.
func (a *App) MailboxOAuthLogin(provider, hint string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	grant, err := beginMailboxOAuth(ctx, strings.TrimSpace(provider), strings.TrimSpace(hint), func(raw string) error {
		return openMailboxLoginURL(a.ctx, raw)
	})
	if err != nil {
		return "", err
	}
	saved, created, err := a.saveMailboxOAuthAccount(grant)
	if err != nil {
		return "", err
	}
	if err := writeMailboxSecretBothSides(saved.ID, grant.Secret); err != nil {
		if created {
			_ = a.PatchConfig(func(cfg *corelib.AppConfig) {
				next, _, delErr := mailbox.DeleteAccount(cfg.MailboxAccounts, saved.ID)
				if delErr == nil {
					cfg.MailboxAccounts = next
				}
			})
		}
		return "", err
	}
	label := "Gmail"
	if saved.Provider == mailbox.ProviderHotmail {
		label = "Hotmail"
	}
	return fmt.Sprintf("%s 邮箱 %s 已登录", label, saved.Address), nil
}

func (a *App) saveMailboxOAuthAccount(grant mailbox.OAuthGrant) (mailbox.Account, bool, error) {
	var saved mailbox.Account
	var created bool
	var saveErr error
	err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		saveErr = nil
		existed := false
		for _, account := range cfg.MailboxAccounts {
			if strings.EqualFold(account.Address, grant.Email) {
				existed = true
				break
			}
		}
		next, account, err := mailbox.SaveOAuthAccount(cfg.MailboxAccounts, grant.Email, grant.Provider, time.Now())
		if err != nil {
			saveErr = err
			return
		}
		cfg.MailboxAccounts = next
		saved = account
		created = !existed
	})
	if err != nil {
		return mailbox.Account{}, false, err
	}
	if saveErr != nil {
		return mailbox.Account{}, false, saveErr
	}
	return saved, created, nil
}

func openMailboxLoginURL(wailsCtx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("无法打开浏览器")
	}
	if err := browser.OpenURL(raw); err != nil {
		if wailsCtx != nil {
			runtime.BrowserOpenURL(wailsCtx, raw)
			return nil
		}
		return fmt.Errorf("无法打开浏览器")
	}
	return nil
}
