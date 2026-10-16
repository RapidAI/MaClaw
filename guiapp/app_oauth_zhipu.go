package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/zhipu"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// Zhipu (智谱) online login — Wails Bindings
//
// Mirrors the open-source ZCode CLI's "zcode login bigmodel" flow: the user
// authorizes at bigmodel.cn in the browser, and the resolved coding-plan API
// key is saved onto the 智谱编程 provider. The credential is a normal
// "{id}.{secret}" API key, so the provider keeps its api-key auth type and
// the regular Test & Save path stays authoritative.
// ─────────────────────────────────────────────────────────────────────────────

// ZhipuCodingLoginInfo is returned to the frontend for the browser login.
type ZhipuCodingLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// zhipuLoginFlow is one in-flight browser approval. claim persists the key
// only when this login is still the active OAuth flow.
type zhipuLoginFlow struct {
	ctx        context.Context
	login      *zhipu.Login
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// StartZhipuCodingOAuth begins the 智谱在线登录 and opens the approval page.
// The frontend then calls WaitZhipuCodingOAuth to block until the user
// completes it.
func (a *App) StartZhipuCodingOAuth() (ZhipuCodingLoginInfo, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	// The approval window is bounded by the server's expires_at; give the flow
	// slot a small buffer on top so a slow browser open cannot eat it.
	parent, finish, claim := a.beginOAuthFlow(zhipuLoginFlowTimeout + 45*time.Second)
	// Own the single-flight slot before the init request so Cancel can abort
	// the network call, and so a caller that never reaches Wait does not leave
	// a later login blocked.
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	a.zhipuOwnedGen = ownedGen
	a.oauthMu.Unlock()
	login, err := zhipu.StartLogin(parent)
	if err != nil {
		finish()
		a.clearZhipuOwnedGen(ownedGen)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
			return ZhipuCodingLoginInfo{}, fmt.Errorf("智谱登录已取消或超时")
		}
		return ZhipuCodingLoginInfo{}, err
	}
	approvalURL := login.AuthURL()
	if !zhipu.IsAuthorizeURL(approvalURL) {
		finish()
		a.clearZhipuOwnedGen(ownedGen)
		return ZhipuCodingLoginInfo{}, fmt.Errorf("智谱授权地址无效")
	}
	if err := parent.Err(); err != nil {
		finish()
		a.clearZhipuOwnedGen(ownedGen)
		return ZhipuCodingLoginInfo{}, fmt.Errorf("智谱登录已取消或超时")
	}
	flow := &zhipuLoginFlow{
		ctx:        parent,
		login:      login,
		generation: ownedGen,
		finish:     finish,
		claim:      claim,
	}
	// The flow and its generation slot must be published together, and only
	// while the flow context is still alive. A concurrent second login cancels
	// this parent before bumping the global slot; storing a dead flow here
	// would otherwise replace a live one and fail both logins.
	a.oauthMu.Lock()
	if parent.Err() != nil {
		a.oauthMu.Unlock()
		finish()
		a.clearZhipuOwnedGen(ownedGen)
		return ZhipuCodingLoginInfo{}, fmt.Errorf("智谱登录已取消或超时")
	}
	previous := a.zhipuLogin
	a.zhipuLogin = flow
	a.zhipuOwnedGen = ownedGen
	a.oauthMu.Unlock()
	if previous != nil && previous.finish != nil {
		// A newer login replaces any older in-flight one.
		previous.finish()
	}
	info := ZhipuCodingLoginInfo{AuthURL: approvalURL}
	if err := browser.OpenURL(approvalURL); err != nil {
		// The failure reason may embed the approval URL; keep the reason only.
		log.Printf("[zhipu] browser open failed: %s", browserOpenReason(err))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// WaitZhipuCodingOAuth blocks until the browser approval finishes, then
// resolves the coding-plan API key and saves it onto the 智谱编程 provider.
func (a *App) WaitZhipuCodingOAuth() (string, error) {
	a.oauthMu.Lock()
	flow := a.zhipuLogin
	a.zhipuLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.login == nil || flow.finish == nil || flow.claim == nil {
		return "", fmt.Errorf("没有正在进行的智谱登录")
	}
	defer func() {
		flow.finish()
		a.clearZhipuOwnedGen(flow.generation)
	}()
	token, err := zhipu.PollUntilReady(flow.ctx, flow.login)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("智谱登录已取消或超时")
		}
		return "", fmt.Errorf("智谱登录失败: %w", err)
	}
	apiKey, err := zhipu.ResolveCodingPlanAPIKey(flow.ctx, token.AccessToken)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("智谱登录已取消或超时")
		}
		return "", fmt.Errorf("智谱登录成功但获取 API Key 失败: %w", err)
	}
	providerName := zhipuCodingProviderName
	if err := flow.claim(func() error {
		name, saveErr := a.saveZhipuCodingLogin(apiKey)
		if saveErr != nil {
			return saveErr
		}
		// The list may hold the provider under a GUI or TUI alias; the
		// post-login probe resolves providers by exact name, so use the
		// name that actually matched.
		providerName = name
		return nil
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(providerName, providerName+" ZCode 登录成功")
}

// CancelZhipuCodingOAuth stops an in-progress 智谱在线登录. A login that has
// not reached Wait is dropped here so it cannot poll later.
func (a *App) CancelZhipuCodingOAuth() {
	a.oauthMu.Lock()
	flow := a.zhipuLogin
	owns := a.zhipuOwnedGen != 0 && a.zhipuOwnedGen == a.oauthGeneration
	// A stored flow from an older attempt must not hide the slot consumed by
	// the login that is currently starting. Unlike the Kimi/Qoder variants,
	// the stale entry is removed from the field too: nothing can complete it
	// anymore, and a stray Wait would otherwise pop a dead flow and poll a
	// cancelled context instead of failing fast.
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
		a.zhipuLogin = nil
	} else if flow != nil {
		a.zhipuLogin = nil
	}
	var cancel context.CancelFunc
	if owns && flow == nil && a.oauthCancel != nil {
		// Wait owns the flow: its poll context is the global OAuth context, so
		// cancel it before releasing the lock, or a newer login could be the
		// one that gets cancelled.
		a.oauthGeneration++
		cancel = a.oauthCancel
		a.oauthCancel = nil
		a.zhipuOwnedGen = 0
	} else if owns {
		a.zhipuOwnedGen = 0
	}
	a.oauthMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if flow != nil && flow.finish != nil {
		// This flow's own finish cancels its context only; a login that
		// already replaced it keeps running.
		flow.finish()
	}
}

func (a *App) clearZhipuOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.zhipuOwnedGen == gen {
		a.zhipuOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

// zhipuLoginFlowTimeout caps the whole approval. The server's own expires_at
// (about 5 minutes per observation) is normally the tighter bound; the cap
// only stops a bogus deadline from reserving the flow slot forever.
const zhipuLoginFlowTimeout = 5 * time.Minute

// saveZhipuCodingLogin persists the resolved API key onto the 智谱编程
// provider through the normal provider snapshot, returning the provider name
// that matched (GUI or TUI alias). Connection evidence is cleared: the
// post-login probe in oauthLoginSuccessMessage re-establishes it.
func (a *App) saveZhipuCodingLogin(apiKey string) (string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("智谱登录未返回 API Key")
	}
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if !corelib.IsZhipuCodingProviderName(p.Name) {
			continue
		}
		if strings.TrimSpace(data.Providers[i].URL) == "" {
			data.Providers[i].URL = "https://open.bigmodel.cn/api/anthropic"
		}
		if strings.TrimSpace(data.Providers[i].Protocol) == "" {
			data.Providers[i].Protocol = "anthropic"
		}
		if strings.TrimSpace(data.Providers[i].Model) == "" {
			data.Providers[i].Model = zhipuCodingDefaultModel
		}
		if data.Providers[i].ContextLength <= 0 {
			data.Providers[i].ContextLength = 400000
		}
		data.Providers[i].Key = apiKey
		data.Providers[i].ConnectionTestPassed = false
		if err := a.SaveMaclawLLMProviders(data.Providers, p.Name); err != nil {
			return "", fmt.Errorf("保存 %s 登录配置失败: %w", p.Name, err)
		}
		return p.Name, nil
	}
	return "", fmt.Errorf("未找到 %s provider", zhipuCodingProviderName)
}
