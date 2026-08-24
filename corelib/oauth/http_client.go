package oauth

import (
	"crypto/x509"
	"errors"
	"fmt"
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

func newOAuthHTTPClient(cfg proxyutil.Config) *http.Client {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: oauthHTTPTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if cfg.Enabled {
		proxyutil.WrapTransport(t, cfg)
	}
	return &http.Client{Timeout: oauthHTTPTimeout, Transport: t}
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
