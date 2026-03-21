package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// 成功页面 HTML 模板。
const successHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>MaClaw</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>✅ 授权成功</h2>
  <p>请返回 MaClaw 继续使用。此页面将自动关闭。</p>
  <script>setTimeout(()=>window.close(),3000)</script>
</body></html>`

// 错误页面 HTML 模板（使用 fmt.Sprintf 填充 error 和 description）。
const errorHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>MaClaw</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>❌ 授权失败</h2>
  <p>错误: %s</p>
  <p>%s</p>
  <p>请关闭此页面并重试。</p>
  <script>setTimeout(()=>window.close(),3000)</script>
</body></html>`

// CallbackServer 管理本地 HTTP 回调服务器，用于接收 OAuth 授权回调。
type CallbackServer struct {
	listener net.Listener
	port     int
	codeCh   chan string
	errCh    chan error
	server   *http.Server
}

// NewCallbackServer 创建一个新的 CallbackServer 实例。
func NewCallbackServer() *CallbackServer {
	return &CallbackServer{
		codeCh: make(chan string, 1),
		errCh:  make(chan error, 1),
	}
}

// Start 在 127.0.0.1:0 上启动 HTTP 回调服务器（OS 自动分配端口），
// 并在 callbackPath 上注册回调处理器。
func (s *CallbackServer) Start(callbackPath string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("callback server: listen failed: %w", err)
	}
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, s.handleCallback)

	s.server = &http.Server{Handler: mux}
	go s.server.Serve(ln) //nolint:errcheck
	return nil
}

// Port 返回实际监听的端口号。
func (s *CallbackServer) Port() int {
	return s.port
}

// WaitForCode 阻塞等待授权码或超时。
func (s *CallbackServer) WaitForCode(timeout time.Duration) (string, error) {
	select {
	case code := <-s.codeCh:
		return code, nil
	case err := <-s.errCh:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("callback server: timed out waiting for authorization code")
	}
}

// Stop 关闭 HTTP 服务器并释放端口。
func (s *CallbackServer) Stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.server.Shutdown(ctx) //nolint:errcheck
	}
}

// handleCallback 处理 OAuth 回调请求。
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if errParam := q.Get("error"); errParam != "" {
		desc := q.Get("error_description")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, errorHTML, errParam, desc)
		s.errCh <- fmt.Errorf("oauth error: %s: %s", errParam, desc)
		return
	}

	code := q.Get("code")
	if code == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, errorHTML, "missing_code", "回调请求中缺少 code 参数")
		s.errCh <- fmt.Errorf("oauth error: missing code parameter in callback")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, successHTML)
	s.codeCh <- code
}
