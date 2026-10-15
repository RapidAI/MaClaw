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
	l.server = &http.Server{Handler: mux}
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
func (l *LoginListener) Wait(ctx context.Context) (string, error) {
	if l == nil {
		return "", fmt.Errorf("没有正在进行的 Devin 登录")
	}
	select {
	case token := <-l.tokenCh:
		return token, nil
	case err := <-l.errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
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
	token := firstNonEmpty(q.Get("firebase_id_token"), q.Get("access_token"), q.Get("token"), q.Get("id_token"))
	if token == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(fragmentBridgeHTML))
		return
	}
	if q.Get("state") != l.state {
		http.Error(w, "登录状态不匹配，请回到 CodexProxy 重新登录。", http.StatusBadRequest)
		return
	}
	if errText := strings.TrimSpace(q.Get("error")); errText != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		if desc == "" {
			desc = errText
		}
		select {
		case l.errCh <- fmt.Errorf("Devin 登录失败: %s", desc):
		default:
		}
		writeLoginPage(w, false, "登录失败，请回到 CodexProxy 重试。")
		return
	}
	select {
	case l.tokenCh <- token:
	default:
	}
	writeLoginPage(w, true, "Devin 登录成功，可以关闭此页面。")
}

func writeLoginPage(w http.ResponseWriter, ok bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	color := "#ff8585"
	title := "登录失败"
	if ok {
		color = "#71d784"
		title = "登录成功"
	}
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>CodexProxy</title></head>
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

const fragmentBridgeHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>CodexProxy</title></head>
<body style="font-family:sans-serif;text-align:center;padding:64px">正在完成 Devin 登录…</body>
<script>(function(){var h=window.location.hash.replace(/^#/,'');if(!h){document.body.textContent='没有收到授权令牌，请回到 CodexProxy 粘贴令牌。';return}window.location.replace('/auth?'+h);})();</script></html>`
