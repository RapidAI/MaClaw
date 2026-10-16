package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/qoder"
)

// loadQoderModels is the catalog call. Tests replace it.
var loadQoderModels = qoder.ListModels

// QoderLoginInfo is returned when the Qoder approval page has been opened.
type QoderLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
	Edition       string `json:"edition"`
}

type qoderLoginFlow struct {
	ctx     context.Context
	cancel  context.CancelFunc
	login   *qoder.DeviceLogin
	profile qoder.Profile
	mode    string
	claim   sync.Once
}

// StartQoderLogin opens the official device-login page for one Qoder edition.
// mode is qoder_cn or qoder_global.
func (a *App) StartQoderLogin(mode string) (QoderLoginInfo, error) {
	profile, ok := qoderProfileForMode(mode)
	if !ok {
		return QoderLoginInfo{}, fmt.Errorf("未知的 Qoder 版本")
	}
	a.cancelQoderLogin()
	login, err := qoder.StartLogin(qoder.MachineID())
	if err != nil {
		return QoderLoginInfo{}, err
	}
	authURL := login.AuthURL(profile)
	if !qoder.IsApprovalURL(authURL) {
		return QoderLoginInfo{}, fmt.Errorf("Qoder 登录链接不受信任")
	}
	ctx, cancel := context.WithTimeout(context.Background(), qoder.LoginLifetime())
	flow := &qoderLoginFlow{
		ctx:     ctx,
		cancel:  cancel,
		login:   login,
		profile: profile,
		mode:    mode,
	}
	a.mu.Lock()
	a.qoderLogin = flow
	a.mu.Unlock()
	info := QoderLoginInfo{AuthURL: authURL, Edition: profile.Name}
	if err := openExternalURL(a.ctx, authURL); err != nil {
		log.Printf("[codexproxy] qoder browser open failed: %s", browserOpenReason(err))
	} else if ctx.Err() == nil {
		info.BrowserOpened = true
	}
	if ctx.Err() != nil {
		a.dropQoderLogin(flow)
		return QoderLoginInfo{}, fmt.Errorf("Qoder 登录已取消或超时")
	}
	return info, nil
}

// WaitQoderLogin blocks until the browser approval finishes.
// The flow stays stored so CancelQoderLogin can still reach it.
func (a *App) WaitQoderLogin() (Status, error) {
	a.mu.Lock()
	flow := a.qoderLogin
	a.mu.Unlock()
	if flow == nil || flow.login == nil || flow.cancel == nil {
		return Status{}, fmt.Errorf("没有正在进行的 Qoder 登录")
	}
	won := false
	flow.claim.Do(func() { won = true })
	if !won {
		return Status{}, fmt.Errorf("Qoder 登录已在等待结果")
	}
	defer a.finishQoderLogin(flow)
	token, err := qoder.PollUntilAuthorized(flow.ctx, flow.profile, flow.login)
	if err != nil || flow.ctx.Err() != nil {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Status{}, fmt.Errorf("Qoder 登录已取消或超时")
		}
		return Status{}, err
	}
	return a.applyQoderToken(flow.mode, flow.profile, token)
}

// CancelQoderLogin stops an in-progress browser login.
func (a *App) CancelQoderLogin() {
	a.cancelQoderLogin()
}

func (a *App) cancelQoderLogin() {
	a.mu.Lock()
	flow := a.qoderLogin
	a.qoderLogin = nil
	a.mu.Unlock()
	if flow != nil && flow.cancel != nil {
		flow.cancel()
	}
}

func (a *App) dropQoderLogin(flow *qoderLoginFlow) {
	a.finishQoderLogin(flow)
}

func (a *App) finishQoderLogin(flow *qoderLoginFlow) {
	if flow == nil {
		return
	}
	a.mu.Lock()
	if a.qoderLogin == flow {
		a.qoderLogin = nil
	}
	a.mu.Unlock()
	if flow.cancel != nil {
		flow.cancel()
	}
}

func (a *App) applyQoderToken(mode string, profile qoder.Profile, token qoder.Token) (Status, error) {
	if strings.TrimSpace(token.AccessToken) == "" {
		return Status{}, fmt.Errorf("Qoder 登录未返回访问令牌")
	}
	a.setRelaunchAfterAuth(true)
	status, err := a.applyVendorLogin(vendorCredential{
		Mode:         mode,
		BaseURL:      profile.ChatURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Email:        token.UserName,
		UserID:       token.UserID,
		ExpiresAt:    token.ExpiresAt,
	})
	if err != nil {
		a.setRelaunchAfterAuth(false)
		return Status{}, err
	}
	return status, nil
}

func fetchQoderModels(ctx context.Context, profile qoder.Profile, userID, apiKey string) ([]ModelOption, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	models, defaultKey, err := loadQoderModels(ctx, profile, userID, apiKey)
	if err != nil {
		return nil, err
	}
	options := make([]ModelOption, 0, len(models))
	for _, model := range models {
		if !model.Enabled {
			continue
		}
		id := strings.TrimSpace(model.Key)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = id
		}
		options = append(options, ModelOption{ID: id, Name: name, ContextWindow: model.MaxInput})
	}
	options = normalizeModelOptions(options)
	if len(options) == 0 {
		return nil, errors.New("Qoder 模型列表为空")
	}
	return preferModelFirst(options, defaultKey), nil
}

func preferModelFirst(options []ModelOption, id string) []ModelOption {
	id = strings.TrimSpace(id)
	if id == "" || len(options) < 2 || options[0].ID == id {
		return options
	}
	for i := range options {
		if options[i].ID != id {
			continue
		}
		options[0], options[i] = options[i], options[0]
		break
	}
	return options
}

func qoderProfileForMode(mode string) (qoder.Profile, bool) {
	switch strings.TrimSpace(mode) {
	case AuthModeQoderCN:
		return qoder.CNProfile(), true
	case AuthModeQoderGlobal:
		return qoder.GlobalProfile(), true
	default:
		return qoder.Profile{}, false
	}
}
