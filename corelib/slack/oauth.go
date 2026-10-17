// Package slack connects a desktop IM channel to a Slack workspace.
// Login is a browser OAuth install. The bot token stays on this machine.
package slack

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultListenAddr is the fixed loopback port. Slack requires the
	// redirect URL to be registered exactly, so the port cannot be random.
	DefaultListenAddr = "127.0.0.1:43127"
	// DefaultRedirectURI must be added to the Slack app's Redirect URLs.
	DefaultRedirectURI = "http://127.0.0.1:43127/slack/callback"
	// BotScopes are requested at install time. The same list must be saved
	// on the Slack app as Bot Token Scopes before login.
	BotScopes = "chat:write,im:history,im:read,im:write,mpim:history,mpim:read,users:read"

	// ProductClientID and ProductClientSecret belong to the MaClaw Slack app.
	// End users sign in with this registration. They do not type these values.
	ProductClientID     = "268449519393.12327676208435"
	ProductClientSecret = "1105c55a5398564fadd365a77967c95a"

	authorizeEndpoint = "https://slack.com/oauth/v2/authorize"
	defaultTokenURL   = "https://slack.com/api/oauth.v2.access"
)

// Session is the workspace install produced by a finished login.
type Session struct {
	BotToken     string
	TeamID       string
	TeamName     string
	BotUserID    string
	AuthedUserID string
}

// LoginOptions overrides endpoints in tests. Zero values use the production
// Slack URLs and the fixed loopback redirect.
type LoginOptions struct {
	ListenAddr  string
	RedirectURI string
	TokenURL    string
}

// Login opens Slack's authorize page and waits until the browser returns.
// open receives the authorize URL. The caller opens it in the system browser.
func Login(ctx context.Context, clientID, clientSecret string, open func(string) error, opt LoginOptions) (Session, error) {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return Session{}, fmt.Errorf("请先填写 Slack 应用的 Client ID 和 Client Secret")
	}
	if open == nil {
		return Session{}, fmt.Errorf("无法打开浏览器")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listenAddr := strings.TrimSpace(opt.ListenAddr)
	if listenAddr == "" {
		listenAddr = DefaultListenAddr
	}
	redirectURI := strings.TrimSpace(opt.RedirectURI)
	tokenURL := strings.TrimSpace(opt.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}

	state, err := randomState()
	if err != nil {
		return Session{}, fmt.Errorf("无法开始登录")
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return Session{}, fmt.Errorf("无法开始登录，请确认 %s 没有被占用", DefaultListenAddr)
	}
	if redirectURI == "" {
		if listenAddr == DefaultListenAddr {
			redirectURI = DefaultRedirectURI
		} else if port := listenerPort(ln); port != 0 {
			redirectURI = fmt.Sprintf("http://127.0.0.1:%d/slack/callback", port)
		} else {
			_ = ln.Close()
			return Session{}, fmt.Errorf("无法开始登录")
		}
	}

	done := make(chan loginResult, 1)
	srv := &http.Server{
		Handler: callbackHandler(state, done, func(code string) (Session, error) {
			return exchangeCode(ctx, tokenURL, clientID, clientSecret, code, redirectURI)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// The token exchange runs before the response is written, so this
		// has to cover Slack, not just the callback page.
		WriteTimeout: 60 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := open(authorizeURL(clientID, redirectURI, state)); err != nil {
		return Session{}, fmt.Errorf("无法打开浏览器")
	}

	select {
	case <-ctx.Done():
		return Session{}, fmt.Errorf("登录已取消")
	case result := <-done:
		if result.err != nil {
			return Session{}, result.err
		}
		return result.session, nil
	}
}

type loginResult struct {
	session Session
	err     error
}

func authorizeURL(clientID, redirectURI, state string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("scope", BotScopes)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	return authorizeEndpoint + "?" + params.Encode()
}

func callbackHandler(state string, done chan<- loginResult, exchange func(code string) (Session, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/slack/callback" {
			http.NotFound(w, r)
			return
		}
		finish := func(msg string, result loginResult) {
			writeCallbackPage(w, msg)
			select {
			case done <- result:
			default:
			}
		}
		q := r.URL.Query()
		gotState := q.Get("state")
		if gotState == "" || subtle.ConstantTimeCompare([]byte(gotState), []byte(state)) != 1 {
			http.Error(w, "登录回调无效", http.StatusBadRequest)
			select {
			case done <- loginResult{err: fmt.Errorf("登录回调无效")}:
			default:
			}
			return
		}
		if slackErr := strings.TrimSpace(q.Get("error")); slackErr != "" {
			msg := "登录失败，授权没有完成"
			if slackErr == "access_denied" {
				msg = "登录已取消"
			}
			finish(msg, loginResult{err: fmt.Errorf("%s", msg)})
			return
		}
		code := strings.TrimSpace(q.Get("code"))
		if code == "" {
			finish("登录失败，授权没有完成", loginResult{err: fmt.Errorf("登录失败，授权没有完成")})
			return
		}
		session, err := exchange(code)
		if err != nil {
			finish(err.Error(), loginResult{err: err})
			return
		}
		finish("已登录 Slack，可以关闭这个页面。", loginResult{session: session})
	})
}

func writeCallbackPage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html><meta charset=\"utf-8\"><title>Slack</title><p>"+html.EscapeString(message)+"</p>")
}

func exchangeCode(ctx context.Context, tokenURL, clientID, clientSecret, code, redirectURI string) (Session, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Session{}, fmt.Errorf("登录失败，授权没有完成")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("登录失败，Slack 没有响应")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return Session{}, fmt.Errorf("登录失败，授权没有完成")
	}
	var payload struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		BotUserID   string `json:"bot_user_id"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		AuthedUser struct {
			ID string `json:"id"`
		} `json:"authed_user"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || !payload.OK || strings.TrimSpace(payload.AccessToken) == "" {
		if payload.Error != "" {
			return Session{}, fmt.Errorf("Slack 授权失败：%s", payload.Error)
		}
		return Session{}, fmt.Errorf("登录失败，授权没有完成")
	}
	token := strings.TrimSpace(payload.AccessToken)
	if payload.TokenType == "user" || strings.HasPrefix(token, "xoxp-") || strings.HasPrefix(token, "xoxe.xoxp-") {
		return Session{}, fmt.Errorf("登录没有拿到机器人令牌，请重新授权")
	}
	return Session{
		BotToken:     token,
		TeamID:       payload.Team.ID,
		TeamName:     payload.Team.Name,
		BotUserID:    payload.BotUserID,
		AuthedUserID: payload.AuthedUser.ID,
	}, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func listenerPort(ln net.Listener) int {
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr == nil {
		return 0
	}
	return addr.Port
}
