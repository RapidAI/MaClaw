package windsurf

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrLoginCancelled means the local callback was closed before Devin redirected back.
var ErrLoginCancelled = errors.New("devin login cancelled")

// LoginListener is the localhost page the Devin account site redirects to.
type LoginListener struct {
	authURL string
	state   string
	server  *http.Server
	tokenCh chan string
	errCh   chan error
	once    sync.Once
}

// StartLogin listens on 127.0.0.1 and returns the Devin account login URL.
func StartLogin() (*LoginListener, error) {
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("Devin 登录回调启动失败: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/auth", port)
	l := &LoginListener{
		authURL: loginURL(Website, redirect, state),
		state:   state,
		tokenCh: make(chan string, 1),
		errCh:   make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", l.handle)
	l.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = l.server.Serve(ln) }()
	return l, nil
}

// AuthURL is the page to open in the browser.
func (l *LoginListener) AuthURL() string {
	if l == nil {
		return ""
	}
	return l.authURL
}

// Wait blocks until the browser redirects back with a token.
// A token that arrived in the same moment as cancel or a page error is kept.
func (l *LoginListener) Wait(ctx context.Context) (string, error) {
	if l == nil {
		return "", fmt.Errorf("没有正在进行的 Devin 登录")
	}
	select {
	case token := <-l.tokenCh:
		return token, nil
	case err := <-l.errCh:
		if token, ok := l.takeToken(); ok {
			return token, nil
		}
		return "", err
	case <-ctx.Done():
		if token, ok := l.takeToken(); ok {
			return token, nil
		}
		return "", ctx.Err()
	}
}

func (l *LoginListener) takeToken() (string, bool) {
	select {
	case token := <-l.tokenCh:
		token = strings.TrimSpace(token)
		return token, token != ""
	default:
		return "", false
	}
}

func (l *LoginListener) fail(err error) {
	select {
	case l.errCh <- err:
	default:
	}
}

// Close stops the callback server.
func (l *LoginListener) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		select {
		case l.errCh <- ErrLoginCancelled:
		default:
		}
		if l.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = l.server.Shutdown(ctx)
		}
	})
}

func (l *LoginListener) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	token := firstCallbackToken(q)
	errText := firstQuery(q, "error")
	if token == "" && errText == "" {
		writeFragmentBridge(w, false)
		return
	}
	// The browser does not send the URL fragment. A code can arrive in the query
	// while the matching state is still in the fragment. Serve the bridge once
	// so the page can append that fragment before the login is failed.
	if !queryStateMatches(q, l.state) {
		if strings.TrimSpace(q.Get("devin_checked")) == "" {
			writeFragmentBridge(w, true)
			return
		}
		l.fail(fmt.Errorf("Devin 登录失败: 登录状态不匹配"))
		http.Error(w, "登录状态不匹配，请重新登录。", http.StatusBadRequest)
		return
	}
	if errText != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		if desc == "" {
			desc = errText
		}
		l.fail(fmt.Errorf("Devin 登录失败: %s", desc))
		writeLoginPage(w, false, "登录失败，请回到应用重试。")
		return
	}
	select {
	case l.tokenCh <- token:
	default:
	}
	writeLoginPage(w, true, "已收到授权，可以关闭此页面。")
}

func writeFragmentBridge(w http.ResponseWriter, retry bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	flag := "false"
	if retry {
		flag = "true"
	}
	_, _ = w.Write([]byte(strings.Replace(fragmentBridgeHTML, "__RETRY__", flag, 1)))
}

func firstCallbackToken(q url.Values) string {
	for _, key := range []string{"firebase_id_token", "access_token", "token", "id_token", "devin_code", "code"} {
		for _, value := range q[key] {
			value = strings.TrimSpace(value)
			if value == "" || isOAuthSchemeWord(value) {
				continue
			}
			return value
		}
	}
	return ""
}

func isOAuthSchemeWord(value string) bool {
	switch strings.ToLower(value) {
	case "bearer", "basic", "mac":
		return true
	default:
		return false
	}
}

func firstQuery(q url.Values, keys ...string) string {
	for _, key := range keys {
		for _, value := range q[key] {
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func queryStateMatches(q url.Values, state string) bool {
	if state == "" {
		return false
	}
	for _, value := range q["state"] {
		if value == state {
			return true
		}
	}
	return false
}

func writeLoginPage(w http.ResponseWriter, ok bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	color := "#ff8585"
	title := "登录失败"
	if ok {
		color = "#71d784"
		title = "已收到授权"
	}
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Devin</title></head>
<body style="font-family:sans-serif;text-align:center;padding:64px;background:#0b0d12;color:#e7e9ee">
<h2 style="color:%s">%s</h2><p>%s</p>
<script>setTimeout(()=>window.close(),2500)</script></body></html>`, color, title, html.EscapeString(message))
}

func randomState() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

const fragmentBridgeHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Devin</title></head>
<body style="font-family:sans-serif;text-align:center;padding:64px">正在完成 Devin 登录…</body>
<script>(function(){var retry=__RETRY__;var h=window.location.hash.replace(/^#/,'').replace(/^\?/,'').replace(/\+/g,'%2B');var q=window.location.search.replace(/^\?/,'').replace(/\+/g,'%2B');if(h){window.location.replace('/auth?'+(q?q+'&':'')+h);return}if(retry&&q.indexOf('devin_checked=')<0){window.location.replace('/auth?'+(q?q+'&':'')+'devin_checked=1');return}document.body.textContent='没有收到授权令牌，请回到应用粘贴令牌。';})();</script></html>`
