package oauth

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

const oauthHTTPTimeout = 15 * time.Second

var (
	httpClientMu     sync.RWMutex
	sharedHTTPClient = newOAuthHTTPClient(proxyutil.Config{})

	proxyCfgMu sync.RWMutex
	proxyCfg   proxyutil.Config
)

// ApplyProxyFromAppConfig installs the MaClaw LLM-scope proxy for OAuth HTTP
// calls (OIDC discovery, token exchange, and refresh). When the proxy is off
// or the MaClaw scope is disabled, requests fall back to HTTP_PROXY/HTTPS_PROXY.
func ApplyProxyFromAppConfig(cfg corelib.AppConfig) {
	SetProxy(proxyutil.Config{
		Enabled:  cfg.DefaultProxyEnabled && cfg.DefaultProxyScopeMaclaw,
		Protocol: cfg.DefaultProxyProtocol,
		Host:     cfg.DefaultProxyHost,
		Port:     cfg.DefaultProxyPort,
		Username: cfg.DefaultProxyUsername,
		Password: cfg.DefaultProxyPassword,
		Bypass:   cfg.DefaultProxyBypass,
	})
}

func normalizeProxyConfig(cfg proxyutil.Config) proxyutil.Config {
	return proxyutil.EnabledNormalized(cfg)
}

// SetProxy swaps the shared OAuth HTTP client for one that uses cfg.
// The client is replaced as a whole so in-flight requests keep their transport.
// Unchanged configs are a no-op so login/refresh can re-apply cheaply.
func SetProxy(cfg proxyutil.Config) {
	cfg = normalizeProxyConfig(cfg)
	proxyCfgMu.Lock()
	if proxyCfg == cfg {
		proxyCfgMu.Unlock()
		return
	}
	proxyCfg = cfg
	proxyCfgMu.Unlock()

	next := newOAuthHTTPClient(cfg)
	httpClientMu.Lock()
	prev := sharedHTTPClient
	sharedHTTPClient = next
	httpClientMu.Unlock()
	if prev != nil {
		if tr, ok := prev.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

func currentProxyConfig() proxyutil.Config {
	proxyCfgMu.RLock()
	defer proxyCfgMu.RUnlock()
	return proxyCfg
}

func httpClient() *http.Client {
	httpClientMu.RLock()
	defer httpClientMu.RUnlock()
	return sharedHTTPClient
}

// Do sends req with the proxy-aware OAuth client. TLS and network failures use
// the same annotated errors as token refresh.
func Do(req *http.Request) (*http.Response, error) {
	return do(req, true)
}

// DoNoFollow sends req without following redirects. Token requests use it so a
// 307 cannot replay a refresh token or device code onto another host.
func DoNoFollow(req *http.Request) (*http.Response, error) {
	return do(req, false)
}

func do(req *http.Request, followRedirects bool) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("oauth request is nil")
	}
	client := httpClient()
	if !followRedirects {
		base := client
		client = &http.Client{
			Timeout:   base.Timeout,
			Transport: base.Transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, annotateOAuthNetworkError("连接授权服务失败", err)
	}
	return resp, nil
}

// DoNoFollowWithin is DoNoFollow with caller-chosen budgets. headerTimeout
// bounds the wait for response headers; totalTimeout bounds the whole
// exchange, including the body. Token calls stay on the shared 15s client.
// Generation probes must use this: some chat gateways withhold headers until
// the first token, and the shared client aborts that wait with
// "Client.Timeout exceeded while awaiting headers".
func DoNoFollowWithin(req *http.Request, headerTimeout, totalTimeout time.Duration) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("oauth request is nil")
	}
	if headerTimeout <= 0 {
		headerTimeout = oauthHTTPTimeout
	}
	if totalTimeout < headerTimeout {
		totalTimeout = headerTimeout
	}
	transport := newProbeTransport(currentProxyConfig(), headerTimeout)
	client := &http.Client{
		Timeout:   totalTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, annotateOAuthNetworkError("连接服务失败", err)
	}
	resp.Body = &closeIdleBody{ReadCloser: resp.Body, transport: transport}
	return resp, nil
}

// closeIdleBody drops the one-shot transport's idle pool once the caller
// finishes the body, so a probe does not keep a connection until process exit.
type closeIdleBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (b *closeIdleBody) Close() error {
	err := b.ReadCloser.Close()
	if b.transport != nil {
		b.transport.CloseIdleConnections()
	}
	return err
}

func newOAuthHTTPClient(cfg proxyutil.Config) *http.Client {
	return &http.Client{Timeout: oauthHTTPTimeout, Transport: newOAuthTransport(cfg, oauthHTTPTimeout)}
}

// newProbeTransport is the one-shot client for generation probes. The dial
// budget stays short so a black-holed host or proxy fails in seconds instead
// of consuming the whole generation wait. HTTP/2 stays on so the probe uses
// the same protocol as a live chat. SOCKS5 installs Dial; replacing it with
// DialContext would skip the proxy, so the existing dial is wrapped instead.
func newProbeTransport(cfg proxyutil.Config, headerTimeout time.Duration) *http.Transport {
	t := newOAuthTransport(cfg, headerTimeout)
	if t.Dial != nil {
		t.Dial = dialWithTimeout(t.Dial, probeDialTimeout)
		return t
	}
	t.DialContext = (&net.Dialer{Timeout: probeDialTimeout, KeepAlive: 30 * time.Second}).DialContext
	return t
}

const probeDialTimeout = corelib.LLMProbeDialTimeout

// dialWithTimeout bounds a blocking dial. A connection that arrives after the
// deadline is closed so the dial goroutine cannot leak it.
func dialWithTimeout(dial func(string, string) (net.Conn, error), timeout time.Duration) func(string, string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		type result struct {
			c   net.Conn
			err error
		}
		ch := make(chan result, 1)
		go func() {
			c, err := dial(network, addr)
			ch <- result{c, err}
		}()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case r := <-ch:
			return r.c, r.err
		case <-timer.C:
			go func() {
				r := <-ch
				if r.c != nil {
					_ = r.c.Close()
				}
			}()
			return nil, fmt.Errorf("dial %s timed out", addr)
		}
	}
}

func newOAuthTransport(cfg proxyutil.Config, headerTimeout time.Duration) *http.Transport {
	if headerTimeout <= 0 {
		headerTimeout = oauthHTTPTimeout
	}
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if cfg.Enabled {
		proxyutil.WrapTransport(t, cfg)
	}
	return t
}

func annotateOAuthNetworkError(op string, err error) error {
	if err == nil {
		return nil
	}
	var hostnameErr x509.HostnameError
	var unknownAuth x509.UnknownAuthorityError
	var certInvalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &hostnameErr) || isTLSNameMismatch(err.Error()):
		return fmt.Errorf("%s: 无法安全连接，当前网络把目标域名劫持到了其他站点。请开启可用的系统代理/VPN（建议 TUN），并在设置中启用代理且勾选「MaClaw 使用代理」后重试: %w", op, err)
	case errors.As(err, &unknownAuth) || errors.As(err, &certInvalid) || isTLSVerifyFailure(err.Error()):
		return fmt.Errorf("%s: TLS 证书校验失败。请检查代理/VPN 或系统时间后重试: %w", op, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

func isTLSNameMismatch(msg string) bool {
	return strings.Contains(msg, "certificate is valid for") ||
		strings.Contains(msg, "certificate is not valid for")
}

func isTLSVerifyFailure(msg string) bool {
	return strings.Contains(msg, "tls: failed to verify certificate") ||
		strings.Contains(msg, "certificate signed by unknown authority") ||
		strings.Contains(msg, "certificate has expired") ||
		strings.Contains(msg, "certificate is not yet valid")
}
