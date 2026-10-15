package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

// WorkBuddyLoginInfo is returned when the WorkBuddy authorization page is ready.
type WorkBuddyLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
	Edition       string `json:"edition"`
}

type workbuddyLoginFlow struct {
	ctx     context.Context
	cancel  context.CancelFunc
	session *workbuddy.LoginSession
	profile workbuddy.Profile
	mode    string
	claim   sync.Once
	stop    sync.Once
}

// StartWorkBuddyLogin opens the official browser login for one WorkBuddy edition.
// mode is workbuddy_cn or workbuddy_global.
func (a *App) StartWorkBuddyLogin(mode string) (WorkBuddyLoginInfo, error) {
	profile, ok := workbuddyProfileForMode(mode)
	if !ok {
		return WorkBuddyLoginInfo{}, fmt.Errorf("未知的 WorkBuddy 版本")
	}
	a.cancelWorkBuddyLogin()
	session, err := workbuddy.BeginLogin(context.Background(), profile, nil)
	if err != nil {
		return WorkBuddyLoginInfo{}, err
	}
	authURL := session.AuthURL()
	if !workbuddyLoginURLAllowed(profile, authURL) {
		session.Close()
		return WorkBuddyLoginInfo{}, fmt.Errorf("WorkBuddy 登录链接不受信任")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	flow := &workbuddyLoginFlow{
		ctx:     ctx,
		cancel:  cancel,
		session: session,
		profile: profile,
		mode:    mode,
	}
	a.mu.Lock()
	a.workbuddyLogin = flow
	a.mu.Unlock()
	info := WorkBuddyLoginInfo{AuthURL: authURL, Edition: profile.Name}
	if err := openExternalURL(a.ctx, authURL); err != nil {
		log.Printf("[codexproxy] workbuddy browser open failed: %s", browserOpenReason(err))
	} else if ctx.Err() == nil {
		info.BrowserOpened = true
	}
	if ctx.Err() != nil {
		a.dropWorkBuddyLogin(flow)
		return WorkBuddyLoginInfo{}, fmt.Errorf("WorkBuddy 登录已取消或超时")
	}
	return info, nil
}

// WaitWorkBuddyLogin blocks until the browser approval finishes.
// The flow stays stored so CancelWorkBuddyLogin can still reach it.
func (a *App) WaitWorkBuddyLogin() (Status, error) {
	a.mu.Lock()
	flow := a.workbuddyLogin
	a.mu.Unlock()
	if flow == nil || flow.session == nil || flow.cancel == nil {
		return Status{}, fmt.Errorf("没有正在进行的 WorkBuddy 登录")
	}
	won := false
	flow.claim.Do(func() { won = true })
	if !won {
		return Status{}, fmt.Errorf("WorkBuddy 登录已在等待结果")
	}
	defer a.finishWorkBuddyLogin(flow)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cred, pending, err := flow.session.Poll(flow.ctx)
		if err != nil || flow.ctx.Err() != nil {
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, workbuddy.ErrLoginTimeout) {
				return Status{}, fmt.Errorf("WorkBuddy 登录已取消或超时")
			}
			return Status{}, err
		}
		if cred != nil && strings.TrimSpace(cred.AccessToken) != "" {
			return a.applyWorkBuddyCredential(flow.mode, flow.profile, *cred)
		}
		if !pending {
			return Status{}, fmt.Errorf("WorkBuddy 登录未完成")
		}
		select {
		case <-flow.ctx.Done():
			return Status{}, fmt.Errorf("WorkBuddy 登录已取消或超时")
		case <-ticker.C:
		}
	}
}

// CancelWorkBuddyLogin stops an in-progress browser login.
func (a *App) CancelWorkBuddyLogin() {
	a.cancelWorkBuddyLogin()
}

func (a *App) cancelWorkBuddyLogin() {
	a.mu.Lock()
	flow := a.workbuddyLogin
	a.workbuddyLogin = nil
	a.mu.Unlock()
	stopWorkBuddyFlow(flow)
}

func (a *App) dropWorkBuddyLogin(flow *workbuddyLoginFlow) {
	a.finishWorkBuddyLogin(flow)
}

func (a *App) finishWorkBuddyLogin(flow *workbuddyLoginFlow) {
	if flow == nil {
		return
	}
	a.mu.Lock()
	if a.workbuddyLogin == flow {
		a.workbuddyLogin = nil
	}
	a.mu.Unlock()
	stopWorkBuddyFlow(flow)
}

func stopWorkBuddyFlow(flow *workbuddyLoginFlow) {
	if flow == nil {
		return
	}
	flow.stop.Do(func() {
		if flow.cancel != nil {
			flow.cancel()
		}
		if flow.session != nil {
			flow.session.Close()
		}
	})
}

func (a *App) applyWorkBuddyCredential(mode string, profile workbuddy.Profile, cred workbuddy.AccountCredential) (Status, error) {
	if strings.TrimSpace(cred.AccessToken) == "" {
		return Status{}, fmt.Errorf("WorkBuddy 登录未返回访问令牌")
	}
	a.setRelaunchAfterAuth(true)
	status, err := a.applyVendorLogin(vendorCredential{
		Mode:         mode,
		BaseURL:      profile.ChatURL,
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		Email:        cred.Nickname,
		UserID:       cred.UserID,
		EnterpriseID: cred.EnterpriseID,
		Domain:       cred.Domain,
		ExpiresAt:    cred.ExpiresAt,
	})
	if err != nil {
		a.setRelaunchAfterAuth(false)
		return Status{}, err
	}
	return status, nil
}

func fetchWorkBuddyModels(profile workbuddy.Profile, account AuthProfile) ([]ModelOption, error) {
	specs := workbuddy.Allowlist(profile.ID)
	if strings.TrimSpace(account.AccessToken) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		upstream, err := workbuddy.FetchModels(ctx, profile, workbuddy.AccountCredential{
			AccessToken:  account.AccessToken,
			RefreshToken: account.RefreshToken,
			UserID:       account.UserID,
			EnterpriseID: account.EnterpriseID,
			Domain:       account.Domain,
		}, nil)
		cancel()
		if err == nil {
			specs = workbuddy.MergeCatalog(profile.ID, upstream)
		}
	}
	if len(specs) == 0 && profile.DefaultModel != "" {
		specs = []workbuddy.ModelSpec{{ID: profile.DefaultModel, Name: profile.DefaultModel}}
	}
	return normalizeModelOptions(workbuddyModelOptions(specs)), nil
}

func workbuddyModelOptions(specs []workbuddy.ModelSpec) []ModelOption {
	options := make([]ModelOption, 0, len(specs))
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			name = id
		}
		options = append(options, ModelOption{ID: id, Name: name, ContextWindow: int(spec.ContextLength)})
	}
	return options
}

func workbuddyProfileForMode(mode string) (workbuddy.Profile, bool) {
	switch strings.TrimSpace(mode) {
	case AuthModeWorkBuddyCN:
		return workbuddy.ChinaProfile(), true
	case AuthModeWorkBuddyGlobal:
		return workbuddy.GlobalProfile(), true
	default:
		return workbuddy.Profile{}, false
	}
}

// workbuddyLoginURLAllowed accepts only the official HTTPS login hosts for the
// selected edition. The state inside the URL is a login capability, so an
// unexpected host is refused before the browser opens.
func workbuddyLoginURLAllowed(profile workbuddy.Profile, raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	if profile.ID == workbuddy.ChinaProfile().ID {
		return hostAllowed(host, "codebuddy.cn", "tencent.com", "qq.com")
	}
	return hostAllowed(host, "workbuddy.ai")
}

func hostAllowed(host string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
