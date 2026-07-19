package websearch

import (
	"crypto/tls"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

var (
	sharedClient *http.Client
	clientOnce   sync.Once
	clientMu     sync.RWMutex

	proxyCfgMu sync.RWMutex
	proxyCfg   proxyutil.Config
)

// currentProxyConfig returns the last proxy config passed to SetProxy, so the
// uTLS (chrome-fingerprint) dialer can honor the same proxy settings.
func currentProxyConfig() proxyutil.Config {
	proxyCfgMu.RLock()
	defer proxyCfgMu.RUnlock()
	return proxyCfg
}

func newTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
		MaxConnsPerHost:     5,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

func sharedCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return http.ErrUseLastResponse
	}
	if len(via) > 0 {
		req.Header.Set("User-Agent", via[0].Header.Get("User-Agent"))
	}
	return nil
}

// httpClient returns the shared HTTP client. The pointer is swapped atomically
// (under clientMu) by SetProxy, so in-flight requests are never exposed to a
// half-reconfigured client.
func httpClient() *http.Client {
	clientOnce.Do(func() {
		// Cookie jar lets redirect chains carry Set-Cookie (some anti-bot
		// gateways set a clearance cookie then redirect back).
		jar, _ := cookiejar.New(nil)
		clientMu.Lock()
		sharedClient = &http.Client{
			Timeout:       30 * time.Second,
			Jar:           jar,
			Transport:     newTransport(),
			CheckRedirect: sharedCheckRedirect,
		}
		clientMu.Unlock()
	})
	clientMu.RLock()
	defer clientMu.RUnlock()
	return sharedClient
}

// SetProxy swaps the shared HTTP client for one whose transport uses the
// given proxy. The client is replaced as a whole (never mutated in place):
// assigning sharedClient.Transport directly would race in-flight requests.
// Safe to call multiple times.
func SetProxy(cfg proxyutil.Config) {
	proxyCfgMu.Lock()
	proxyCfg = cfg
	proxyCfgMu.Unlock()

	prev := httpClient()

	t := newTransport()
	if cfg.Enabled {
		proxyutil.WrapTransport(t, cfg)
	}
	clientMu.Lock()
	sharedClient = &http.Client{
		Timeout:       30 * time.Second,
		Jar:           prev.Jar, // keep accumulated cookies across swaps
		Transport:     t,
		CheckRedirect: prev.CheckRedirect,
	}
	clientMu.Unlock()
	if old, ok := prev.Transport.(*http.Transport); ok {
		old.CloseIdleConnections()
	}
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
}

func pickUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}
