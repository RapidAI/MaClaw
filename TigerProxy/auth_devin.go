package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/windsurf"
)

// DevinLoginInfo is returned when the Devin account page has been opened.
type DevinLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	TokenPage     string `json:"token_page"`
	BrowserOpened bool   `json:"browser_opened"`
}

type devinLoginFlow struct {
	ctx      context.Context
	listener *windsurf.LoginListener
	finish   func()
}

// StartDevinLogin opens the same Devin account page the installed IDE uses.
func (a *App) StartDevinLogin() (DevinLoginInfo, error) {
	a.cancelDevinLogin()
	listener, err := windsurf.StartLogin()
	if err != nil {
		return DevinLoginInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	flow := &devinLoginFlow{
		ctx:      ctx,
		listener: listener,
		finish: func() {
			cancel()
			listener.Close()
		},
	}
	a.mu.Lock()
	previous := a.devinLogin
	a.devinLogin = flow
	a.mu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}
	info := DevinLoginInfo{AuthURL: listener.AuthURL(), TokenPage: windsurf.TokenPage}
	if err := openExternalURL(a.ctx, listener.AuthURL()); err != nil {
		log.Printf("[codexproxy] devin browser open failed: %s", browserOpenReason(err))
	} else if ctx.Err() == nil {
		info.BrowserOpened = true
	}
	if ctx.Err() != nil {
		a.dropDevinLogin(flow)
		return DevinLoginInfo{}, fmt.Errorf("Devin 登录已取消或超时")
	}
	return info, nil
}

// WaitDevinLogin blocks until the browser login finishes.
func (a *App) WaitDevinLogin() (Status, error) {
	a.mu.Lock()
	flow := a.devinLogin
	a.devinLogin = nil
	a.mu.Unlock()
	if flow == nil || flow.listener == nil || flow.finish == nil {
		return Status{}, fmt.Errorf("没有正在进行的 Devin 登录")
	}
	defer flow.finish()
	token, err := flow.listener.Wait(flow.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, windsurf.ErrLoginCancelled) {
			return Status{}, fmt.Errorf("Devin 登录已取消或超时")
		}
		return Status{}, err
	}
	session, err := windsurf.ResolveToken(flow.ctx, token)
	if err != nil {
		return Status{}, err
	}
	return a.applyDevinSession(session)
}

// CompleteDevinLoginWithToken accepts the backup token shown by Devin or Windsurf.
func (a *App) CompleteDevinLoginWithToken(token string) (Status, error) {
	session, err := windsurf.ResolveToken(context.Background(), token)
	if err != nil {
		return Status{}, err
	}
	a.cancelDevinLogin()
	return a.applyDevinSession(session)
}

// CancelDevinLogin stops an in-progress browser login.
func (a *App) CancelDevinLogin() {
	a.cancelDevinLogin()
}

func (a *App) cancelDevinLogin() {
	a.mu.Lock()
	flow := a.devinLogin
	a.devinLogin = nil
	a.mu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) dropDevinLogin(flow *devinLoginFlow) {
	a.mu.Lock()
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.mu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) applyDevinSession(session windsurf.Session) (Status, error) {
	if strings.TrimSpace(session.APIKey) == "" {
		return Status{}, fmt.Errorf("Devin 登录未返回访问令牌")
	}
	a.setRelaunchAfterAuth(true)
	return a.applyOAuthResultLabeled(AuthModeDevin, session.APIServerURL, &oauth.TokenResult{
		AccessToken:    session.APIKey,
		RawAccessToken: session.APIKey,
	}, session.Name, true)
}

func fetchDevinModels(baseURL, apiKey string) ([]ModelOption, error) {
	models, err := windsurf.ListModels(context.Background(), baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	options := make([]ModelOption, 0, len(models))
	for _, model := range models {
		options = append(options, ModelOption{ID: model.ID, Name: model.Name})
	}
	return normalizeModelOptions(options), nil
}
