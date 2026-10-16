package desktopd

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

//go:embed admin.html
var adminPage []byte

// The admin UI is a small operator panel served from the desktopd process
// itself. First use creates the admin account (setup), after which the panel
// shows the API tokens Hub authenticates with and can add or remove extra
// ones. It is meant for the Docker host's loopback interface or an SSH
// tunnel — the deploy script keeps DESKTOPD_ADDR on 127.0.0.1 and the public
// reverse proxy forwards only /v1/*.

const (
	adminSessionCookie = "desktopd_admin_session"
	adminSessionTTL    = 24 * time.Hour
	adminCSRFHeader    = "X-Desktopd-Admin"
)

type adminCredentials struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	CreatedAt    string `json:"created_at"`
}

// AdminToken is one API key the /v1/* bearer check accepts. The token is
// stored in clear text on purpose: the panel's job is to show it again, and
// the primary DESKTOPD_TOKEN already lives in clear text in the .env file.
type AdminToken struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

type adminTokenFile struct {
	Tokens []AdminToken `json:"tokens"`
}

type adminSession struct {
	expires time.Time
}

type adminServer struct {
	svc       *Service
	primary   string
	stateDir  string
	page      []byte
	proxyKeys *ProxyKeySource
	egress    *EgressSource
	mu        sync.Mutex
	sessions  map[string]time.Time
	failures  map[string]*failures
	// writeMu serialises state-file mutations so two concurrent panel
	// requests cannot lose each other's changes. It is taken *after* the
	// session check, never while mu is held.
	writeMu sync.Mutex
	// tokens cache avoids re-reading api_tokens.json on every /v1 request;
	// invalidated by size+mtime comparison.
	cacheMu   sync.Mutex
	cacheSize int64
	cacheMod  time.Time
	cacheTok  []AdminToken
	// allowRemoteSetup relaxes setup's loopback-only rule, for operators who
	// must run the one-time setup over the network. Default is local only so
	// an exposed panel cannot be claimed by the first remote visitor.
	allowRemoteSetup bool
}

type failures struct {
	count    int
	blocked  time.Time
	lastFail time.Time
}

func newAdminServer(svc *Service, primary, stateDir string, page []byte, proxyKeys *ProxyKeySource, egress *EgressSource) *adminServer {
	return &adminServer{
		svc:       svc,
		primary:   primary,
		stateDir:  stateDir,
		page:      page,
		proxyKeys: proxyKeys,
		egress:    egress,
		sessions:  map[string]time.Time{},
		failures:  map[string]*failures{},
		// First-run setup is local-only by default: an exposed panel must
		// not be claimable by the first remote visitor. Operators who need
		// one remote setup window enable DESKTOPD_ALLOW_REMOTE_SETUP.
		allowRemoteSetup: strings.EqualFold(strings.TrimSpace(os.Getenv("DESKTOPD_ALLOW_REMOTE_SETUP")), "1"),
	}
}

// ServeHTTP routes /admin and /admin/*.
func (a *adminServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	switch {
	case path == "" || path == "/" || path == "/index.html":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(a.page)
	case path == "/api/state":
		a.handleState(w, r)
	case path == "/api/setup":
		a.handleSetup(w, r)
	case path == "/api/login":
		a.handleLogin(w, r)
	case path == "/api/logout":
		a.handleLogout(w, r)
	case path == "/api/overview":
		a.withSession(w, r, a.handleOverview)
	case path == "/api/tokens":
		a.withSession(w, r, a.handleTokens)
	case strings.HasPrefix(path, "/api/tokens/"):
		a.withSession(w, r, a.handleTokenDelete)
	case path == "/api/proxy-token":
		a.withSession(w, r, a.handleProxyToken)
	case path == "/api/egress-proxy":
		a.withSession(w, r, a.handleEgressProxy)
	case path == "/api/egress-proxy/test":
		a.withSession(w, r, a.handleEgressProxyTest)
	default:
		http.NotFound(w, r)
	}
}

// ---------------------------------------------------------------------------
// state helpers

func (a *adminServer) credentials() (*adminCredentials, error) {
	raw, err := os.ReadFile(filepath.Join(a.stateDir, "admin.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var creds adminCredentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, err
	}
	if creds.Username == "" || creds.PasswordHash == "" {
		return nil, nil
	}
	return &creds, nil
}

func (a *adminServer) saveCredentials(creds *adminCredentials) error {
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return a.writeStateFile("admin.json", raw)
}

func (a *adminServer) tokens() ([]AdminToken, error) {
	path := filepath.Join(a.stateDir, "api_tokens.json")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	a.cacheMu.Lock()
	if a.cacheTok != nil && a.cacheSize == info.Size() && a.cacheMod.Equal(info.ModTime()) {
		tokens := a.cacheTok
		a.cacheMu.Unlock()
		return tokens, nil
	}
	a.cacheMu.Unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file adminTokenFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	a.cacheMu.Lock()
	a.cacheTok = file.Tokens
	a.cacheSize = info.Size()
	a.cacheMod = info.ModTime()
	a.cacheMu.Unlock()
	return file.Tokens, nil
}

func (a *adminServer) saveTokens(tokens []AdminToken) error {
	if tokens == nil {
		tokens = []AdminToken{}
	}
	raw, err := json.MarshalIndent(adminTokenFile{Tokens: tokens}, "", "  ")
	if err != nil {
		return err
	}
	if err := a.writeStateFile("api_tokens.json", raw); err != nil {
		return err
	}
	// Keep the cache exact rather than invalidating it: the next stat may
	// still report the old mtime granularity on coarse filesystems.
	a.cacheMu.Lock()
	a.cacheTok = tokens
	if info, err := os.Stat(filepath.Join(a.stateDir, "api_tokens.json")); err == nil {
		a.cacheSize = info.Size()
		a.cacheMod = info.ModTime()
	}
	a.cacheMu.Unlock()
	return nil
}

func (a *adminServer) writeStateFile(name string, raw []byte) error {
	return writeStateFile(a.stateDir, name, raw)
}

// writeStateFile atomically writes a state JSON file: tmp + rename inside the
// state directory, 0600, directory created on demand.
func writeStateFile(dir, name string, raw []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// validTokenCharset restricts operator-issued keys to printable ASCII with no
// whitespace. The token travels in an Authorization header, where control or
// non-ASCII bytes would make Hub's request impossible to send.
func validTokenCharset(token string) bool {
	for _, ch := range token {
		if ch < 0x21 || ch > 0x7e {
			return false
		}
	}
	return len(token) > 0
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ---------------------------------------------------------------------------
// sessions

func (a *adminServer) newSession(w http.ResponseWriter) {
	token, err := randomHex(32)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(adminSessionTTL)
	for key, expires := range a.sessions {
		if time.Now().After(expires) {
			delete(a.sessions, key)
		}
	}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(adminSessionTTL),
	})
}

func (a *adminServer) sessionValid(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.sessions[cookie.Value]
	if !ok || time.Now().After(expires) {
		delete(a.sessions, cookie.Value)
		return false
	}
	return true
}

func (a *adminServer) endSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminSessionCookie, Value: "", Path: "/admin", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (a *adminServer) withSession(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter, *http.Request)) {
	if !a.sessionValid(r) {
		writeErr(w, http.StatusUnauthorized, "admin login required")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(adminCSRFHeader) != "1" {
		writeErr(w, http.StatusForbidden, "missing admin header")
		return
	}
	next(w, r)
}

// ---------------------------------------------------------------------------
// handlers

func (a *adminServer) handleState(w http.ResponseWriter, r *http.Request) {
	creds, err := a.credentials()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin state is unreadable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup": creds == nil,
		"logged_in":   a.sessionValid(r),
	})
}

func (a *adminServer) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// First-run admin setup is local-only: the deploy story is loopback or
	// an SSH tunnel, and the first visitor to an exposed panel must not be
	// able to claim the account the operator still needs to create.
	if !a.allowRemoteSetup && !isLoopbackRemote(r) {
		writeErr(w, http.StatusForbidden, "admin setup is allowed from the server itself only; use loopback or an SSH tunnel")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeMessage(w, r, "invalid setup request", &in) {
		return
	}
	// Serialise the whole exists-check + write so two simultaneous first
	// requests cannot both pass.
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	creds, err := a.credentials()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin state is unreadable")
		return
	}
	if creds != nil {
		writeErr(w, http.StatusConflict, "admin account already exists")
		return
	}
	if len(in.Password) < 8 || len(in.Password) > 128 {
		writeErr(w, http.StatusBadRequest, "password must be 8-128 characters")
		return
	}
	hash, err := hashAdminPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "password hashing failed")
		return
	}
	created := &adminCredentials{
		Username:     strings.TrimSpace(in.Username),
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if created.Username == "" {
		writeErr(w, http.StatusBadRequest, "username is required")
		return
	}
	if err := a.saveCredentials(created); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin state is not writable")
		return
	}
	a.newSession(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *adminServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ip := clientIP(r)
	if !a.allowLogin(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed logins; try again later")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeMessage(w, r, "invalid login request", &in) {
		return
	}
	creds, err := a.credentials()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin state is unreadable")
		return
	}
	if creds == nil {
		writeErr(w, http.StatusPreconditionRequired, "admin setup required")
		return
	}
	// A real bcrypt comparison runs for every attempt against the stored
	// hash, so the response time does not reveal which half failed.
	hashErr := bcrypt.CompareHashAndPassword([]byte(creds.PasswordHash), []byte(in.Password))
	userOK := subtle.ConstantTimeCompare([]byte(creds.Username), []byte(strings.TrimSpace(in.Username))) == 1
	if hashErr != nil || !userOK {
		a.recordFailure(ip)
		// One real bcrypt comparison always ran, so the response time does
		// not reveal which half failed.
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	a.newSession(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *adminServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.endSession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type overviewDesktop struct {
	Container string `json:"container"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"`
}

func (a *adminServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.tokens()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "api tokens are unreadable")
		return
	}
	out := map[string]any{
		"advertise_host": a.svc.advertiseHostOrEmpty(),
		"primary_token":  a.primary,
		"image":          DefaultImage,
		"image_sources":  a.svc.ImagePullStatuses(),
		"memory":         DefaultMemory,
		"cpus":           DefaultCPUs,
		"shm_size":       DefaultShmSize,
		"tokens":         tokens,
		"desktops":       a.desktops(r),
		// Forward-proxy display state comes straight from the process env:
		// the panel shows this configuration, it does not own it.
		"proxy": strings.EqualFold(strings.TrimSpace(os.Getenv("DESKTOPD_PROXY")), "1"),
		"proxy_addr": func() string {
			if addr := strings.TrimSpace(os.Getenv("DESKTOPD_PROXY_ADDR")); addr != "" {
				return addr
			}
			return DefaultForwardProxyAddr
		}(),
		"desktop_proxy": a.svc.DesktopProxyURL,
	}
	out["docker_ok"], out["docker_version"] = a.dockerProbe(r)
	// The proxy key is displayed like the primary token: clear text on
	// purpose, since .env already holds it that way.
	if a.proxyKeys != nil {
		if key, fromPanel, err := a.proxyKeys.Get(); err == nil {
			out["proxy_token"] = key
			if fromPanel {
				out["proxy_token_source"] = "panel"
			} else {
				out["proxy_token_source"] = "env"
			}
		}
	}
	// The consumer egress config (which upstream to chain through, which URL
	// desktop containers get). Host and key decompose the stored URL so the
	// panel never makes the operator assemble a URL by hand.
	if a.egress != nil {
		if cfg, fromPanel, err := a.egress.Get(); err == nil {
			out["egress_upstream"] = cfg.Upstream
			out["egress_desktop_url"] = cfg.DesktopProxyURL
			if host, key, parseErr := splitUpstreamCredentials(cfg.Upstream); parseErr == nil {
				out["egress_upstream_host"] = host
				out["egress_upstream_key"] = key
			}
			if fromPanel {
				out["egress_source"] = "panel"
			} else {
				out["egress_source"] = "env"
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// splitUpstreamCredentials decomposes a composed proxy URL into its host part
// (scheme://host:port) and embedded key, for the panel's two inputs. The user
// name is always "docker"; a URL without credentials yields an empty key.
func splitUpstreamCredentials(raw string) (host, key string, err error) {
	u, err := parseUpstreamURL(raw)
	if err != nil {
		return "", "", err
	}
	host = u.Scheme + "://" + u.Host
	if u.User != nil {
		key, _ = u.User.Password()
	}
	return host, key, nil
}

// composeUpstreamURL builds the stored proxy URL from the panel's two inputs:
// host (optionally carrying an explicit scheme, defaulting to https) and the
// optional apikey. url.URL percent-encodes the userinfo, so keys with
// characters like "/" or "+" stay safe in proxy URLs.
// composeUpstreamURL builds the stored proxy URL from the panel's two inputs:
// host (optionally carrying its own http(s):// scheme, defaulting to https)
// and the optional apikey. url.UserPassword percent-encodes the userinfo, so
// keys with reserved characters like "/" stay safe in proxy URLs, while "+"
// and "=" are legal userinfo characters and need no escaping.
func composeUpstreamURL(host, key string) (string, error) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return "", fmt.Errorf("upstream host is required")
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		// Any other scheme prefix is a typo (ftp://, socks5://...) and must
		// not be silently mangled into a hostname.
		if i := strings.Index(trimmed, "://"); i > 0 {
			return "", fmt.Errorf("upstream host %q uses unsupported scheme %q: use http(s):// or a bare host", trimmed, trimmed[:i])
		}
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil || u.Host == "" {
		return "", fmt.Errorf("upstream host %q does not parse as a host name", host)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	if key != "" {
		u.User = url.UserPassword("docker", key)
	} else {
		u.User = url.User("docker")
	}
	return u.String(), nil
}

// egressProbeURLs mirror the desktop app's proxy test: a connectivity check
// that only answers through a working outbound proxy, plus an egress-IP
// tracer to show which machine the traffic left from.
var (
	egressProbeURL     = "https://www.google.com/generate_204"
	egressTraceURL     = "https://1.1.1.1/cdn-cgi/trace"
	testEgressUpstream = func(rawURL string) (latencyMS int64, egressIP string, err error) {
		u, err := parseUpstreamURL(rawURL)
		if err != nil {
			return 0, "", err
		}
		transport := &http.Transport{
			Proxy:                 http.ProxyURL(u),
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		}
		client := &http.Client{Timeout: 20 * time.Second, Transport: transport}
		defer transport.CloseIdleConnections()
		started := time.Now()
		resp, err := client.Get(egressProbeURL)
		if err != nil {
			return 0, "", err
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			return int64(time.Since(started) / time.Millisecond), "", fmt.Errorf("通过代理访问 %s 得到 HTTP %d", egressProbeURL, resp.StatusCode)
		}
		latency := int64(time.Since(started) / time.Millisecond)
		traceResp, err := client.Get(egressTraceURL)
		if err != nil {
			return latency, "", nil // the probe succeeded; the trace is extra
		}
		defer traceResp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(traceResp.Body, 4<<10))
		if err != nil {
			return latency, "", nil
		}
		for _, line := range strings.Split(string(body), "\n") {
			if ip, found := strings.CutPrefix(strings.TrimSpace(line), "ip="); found && net.ParseIP(ip) != nil {
				return latency, ip, nil
			}
		}
		return latency, "", nil
	}
)

// dockerProxyDropIn is where the dockerd egress drop-in lives; the panel's
// egress sync writes the same file remote_deploy.sh manages.
const dockerProxyDropIn = "/etc/systemd/system/docker.service.d/maclaw-proxy.conf"

// dockerProxyNoProxyDefault matches remote_deploy.sh's list: Tencent's
// metadata and internal mirrors must stay off the proxy.
const dockerProxyNoProxyDefault = "localhost,127.0.0.1,::1,*.tencentyun.com,.tencentyun.com,mirror.ccs.tencentyun.com,mirrors.tencentyun.com,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

// applyDockerProxy rewrites the dockerd systemd drop-in and restarts docker
// when the content changed. An empty upstream means the env state has no
// pull proxy: the drop-in is removed rather than filled with empty values.
// It runs real systemctl; tests replace it.
var applyDockerProxy = func(upstream, noProxy string) (restarted bool, err error) {
	if _, lookupErr := exec.LookPath("systemctl"); lookupErr != nil {
		return false, fmt.Errorf("no systemd on this host; configure dockerd env manually")
	}
	restart := func() (bool, error) {
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			return false, fmt.Errorf("daemon-reload: %v: %s", err, out)
		}
		if out, err := exec.Command("systemctl", "restart", "docker").CombinedOutput(); err != nil {
			return false, fmt.Errorf("restart docker: %v: %s", err, out)
		}
		return true, nil
	}
	if upstream == "" {
		if _, statErr := os.Stat(dockerProxyDropIn); statErr != nil {
			if os.IsNotExist(statErr) {
				return false, nil
			}
			return false, statErr
		}
		if err := os.Remove(dockerProxyDropIn); err != nil {
			return false, err
		}
		return restart()
	}
	desired := "[Service]\n" +
		"Environment=\"HTTP_PROXY=" + upstream + "\"\n" +
		"Environment=\"HTTPS_PROXY=" + upstream + "\"\n" +
		"Environment=\"NO_PROXY=" + noProxy + "\"\n"
	if current, readErr := os.ReadFile(dockerProxyDropIn); readErr == nil && string(current) == desired {
		return false, nil
	}
	if err := writeStateFile(filepath.Dir(dockerProxyDropIn), filepath.Base(dockerProxyDropIn), []byte(desired)); err != nil {
		return false, err
	}
	return restart()
}

// handleEgressProxy replaces (POST) or clears (DELETE) the panel's consumer
// egress config: which upstream proxy to chain through and which URL desktop
// containers receive. Both resolve per use, so a change takes effect without
// a restart; sync_dockerd additionally rewrites the dockerd drop-in, which
// restarts docker and cuts open desktop sessions.
func (a *adminServer) handleEgressProxy(w http.ResponseWriter, r *http.Request) {
	if a.egress == nil {
		writeErr(w, http.StatusInternalServerError, "egress management is disabled (no state directory)")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var in struct {
			Upstream        string `json:"upstream"`
			UpstreamHost    string `json:"upstream_host"`
			UpstreamKey     string `json:"upstream_key"`
			DesktopProxyURL string `json:"desktop_proxy_url"`
			SyncDocker      bool   `json:"sync_dockerd"`
		}
		if !decodeMessage(w, r, "invalid egress request", &in) {
			return
		}
		upstream := strings.TrimSpace(in.Upstream)
		// The panel's simplified form sends host + key and never a URL.
		if host := strings.TrimSpace(in.UpstreamHost); host != "" {
			composed, err := composeUpstreamURL(host, strings.TrimSpace(in.UpstreamKey))
			if err != nil {
				writeErr(w, http.StatusBadRequest, redactEgressMessage(err.Error(), in.UpstreamKey))
				return
			}
			upstream = composed
		}
		cfg := EgressConfig{Upstream: upstream, DesktopProxyURL: strings.TrimSpace(in.DesktopProxyURL)}
		// Validate before touching the store; normalizeEgressConfig's errors
		// are input errors and map to 400. They can echo the composed URL,
		// which carries the key, so the message is redacted.
		if _, err := normalizeEgressConfig(cfg); err != nil {
			writeErr(w, http.StatusBadRequest, redactEgressMessage(err.Error(), in.UpstreamKey))
			return
		}
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.egress.Set(cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := map[string]any{"ok": true}
		if in.SyncDocker {
			restarted, err := applyDockerProxy(cfg.Upstream, dockerProxyNoProxyDefault)
			if err != nil {
				// The file change is already live for the chain and the
				// desktops; dockerd keeps its old env until this is fixed.
				out["docker_error"] = err.Error()
			}
			out["docker_restarted"] = restarted
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodDelete:
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.egress.Clear(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Restoring the env state also restores the dockerd drop-in, so the
		// pull path follows the same fallback as the chain and the desktops.
		envFallback, _, err := a.egress.Get()
		out := map[string]any{"ok": true}
		if err == nil && envFallback.Upstream != "" {
			restarted, applyErr := applyDockerProxy(envFallback.Upstream, dockerProxyNoProxyDefault)
			out["docker_restarted"] = restarted
			if applyErr != nil {
				out["docker_error"] = applyErr.Error()
			}
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// redactEgressMessage keeps the apikey out of panel-visible errors: the
// composed URL embeds it as userinfo, and parse failures echo that URL.
func redactEgressMessage(msg, key string) string {
	for _, variant := range []string{key, url.QueryEscape(key)} {
		if variant != "" {
			msg = strings.ReplaceAll(msg, variant, "***")
		}
	}
	return msg
}

// egressTestInput is the shape both the egress save and its test take: the
// simplified panel form (host + apikey) with the desktop URL along for
// validation.
type egressTestInput struct {
	UpstreamHost string `json:"upstream_host"`
	UpstreamKey  string `json:"upstream_key"`
}

// composeUpstreamFromInput validates the simplified form and returns the
// stored proxy URL. Input errors map to 400; the message is redacted.
func composeUpstreamFromInput(w http.ResponseWriter, in egressTestInput) string {
	if strings.TrimSpace(in.UpstreamHost) == "" {
		writeErr(w, http.StatusBadRequest, "upstream host is required")
		return ""
	}
	composed, err := composeUpstreamURL(strings.TrimSpace(in.UpstreamHost), strings.TrimSpace(in.UpstreamKey))
	if err != nil {
		writeErr(w, http.StatusBadRequest, redactEgressMessage(err.Error(), in.UpstreamKey))
		return ""
	}
	return composed
}

// handleEgressProxyTest verifies the panel's upstream host + key by actually
// connecting through them: a connectivity probe plus an egress-IP lookup, so
// the operator sees which machine the traffic would leave from.
func (a *adminServer) handleEgressProxyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in egressTestInput
	if !decodeMessage(w, r, "invalid egress request", &in) {
		return
	}
	composed := composeUpstreamFromInput(w, in)
	if composed == "" {
		return
	}
	latency, egressIP, err := testEgressUpstream(composed)
	result := map[string]any{"latency_ms": latency}
	if egressIP != "" {
		result["egress_ip"] = egressIP
	}
	if err != nil {
		result["ok"] = false
		result["message"] = redactEgressMessage(err.Error(), strings.TrimSpace(in.UpstreamKey))
	} else {
		result["ok"] = true
		result["message"] = "ok"
	}
	writeJSON(w, http.StatusOK, result)
}

// handleProxyToken replaces (POST) or clears (DELETE) the panel-set proxy
// key. A POST with an empty token generates one; both take effect on the
// next proxied connection without a restart.
func (a *adminServer) handleProxyToken(w http.ResponseWriter, r *http.Request) {
	if a.proxyKeys == nil {
		writeErr(w, http.StatusInternalServerError, "proxy key management is disabled (no state directory)")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var in struct {
			Token string `json:"token"`
		}
		if !decodeMessage(w, r, "invalid proxy token request", &in) {
			return
		}
		token := strings.TrimSpace(in.Token)
		if token == "" {
			generated, err := randomHex(24)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "token generation failed")
				return
			}
			token = generated
		}
		if !validTokenCharset(token) {
			// Validate the input before touching the store or the lock.
			writeErr(w, http.StatusBadRequest, "token must be printable ASCII without whitespace")
			return
		}
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.proxyKeys.Set(token); err != nil {
			// Set re-validates; a failure here is storage, not input.
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
	case http.MethodDelete:
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.proxyKeys.Clear(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *adminServer) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tokens, err := a.tokens()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "api tokens are unreadable")
			return
		}
		if tokens == nil {
			tokens = []AdminToken{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
	case http.MethodPost:
		var in struct {
			Label string `json:"label"`
			Token string `json:"token"`
		}
		if !decodeMessage(w, r, "invalid token request", &in) {
			return
		}
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		label := strings.TrimSpace(in.Label)
		if label == "" {
			writeErr(w, http.StatusBadRequest, "label is required")
			return
		}
		token := strings.TrimSpace(in.Token)
		if token == "" {
			generated, err := randomHex(24)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "token generation failed")
				return
			}
			token = generated
		}
		if len(token) < 16 || len(token) > 128 || !validTokenCharset(token) {
			writeErr(w, http.StatusBadRequest, "token must be 16-128 printable ASCII characters without whitespace")
			return
		}
		existing, err := a.tokens()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "api tokens are unreadable")
			return
		}
		for _, item := range existing {
			if item.Token == token {
				writeErr(w, http.StatusConflict, "this token already exists")
				return
			}
		}
		id, err := randomHex(8)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "token generation failed")
			return
		}
		entry := AdminToken{ID: id, Label: label, Token: token, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		if err := a.saveTokens(append(existing, entry)); err != nil {
			writeErr(w, http.StatusInternalServerError, "api tokens are not writable")
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *adminServer) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/admin"), "/api/tokens/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "token id is required")
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	existing, err := a.tokens()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "api tokens are unreadable")
		return
	}
	kept := existing[:0:0]
	found := false
	for _, item := range existing {
		if item.ID == id {
			found = true
			continue
		}
		kept = append(kept, item)
	}
	if !found {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	if err := a.saveTokens(kept); err != nil {
		writeErr(w, http.StatusInternalServerError, "api tokens are not writable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// tokenAccepted reports whether a /v1/* bearer credential is the primary env
// token or one issued through the admin panel.
func (a *adminServer) tokenAccepted(token string) bool {
	if token == "" {
		return false
	}
	if a.primary != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.primary)) == 1 {
		return true
	}
	tokens, err := a.tokens()
	if err != nil {
		return false
	}
	for _, item := range tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(item.Token)) == 1 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// docker introspection for the overview

// dockerProbe shells out once and reports both availability and version.
func (a *adminServer) dockerProbe(r *http.Request) (bool, string) {
	version, err := a.svc.docker(r.Context(), "version", "--format", "{{.Server.Version}}")
	return err == nil, strings.TrimSpace(version)
}

func (a *adminServer) desktops(r *http.Request) []overviewDesktop {
	out, err := a.svc.docker(r.Context(), "ps", "--filter", "label=maclaw.tenant", "--format", "{{.Names}}\\t{{.Label \"maclaw.tenant\"}}\\t{{.Label \"maclaw.user\"}}\\t{{.Status}}")
	if err != nil {
		return []overviewDesktop{}
	}
	desktops := []overviewDesktop{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 4 {
			continue
		}
		desktops = append(desktops, overviewDesktop{
			Container: fields[0], TenantID: fields[1], UserID: fields[2], Status: fields[3],
		})
	}
	return desktops
}

// ---------------------------------------------------------------------------
// login throttling

// clientIP extracts the peer host for login throttling. Bracketed IPv6 stays
// intact so loopback covers ::1.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// isLoopbackRemote reports whether the request reaches the server locally or
// through an SSH tunnel (which presents the server's own loopback as the
// peer). Used to gate first-run admin setup.
func isLoopbackRemote(r *http.Request) bool {
	host := clientIP(r)
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *adminServer) allowLogin(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.failures[ip]
	if !ok {
		return true
	}
	if time.Now().After(entry.blocked) && time.Since(entry.lastFail) > 5*time.Minute {
		delete(a.failures, ip)
		return true
	}
	if !entry.blocked.IsZero() && time.Now().Before(entry.blocked) {
		return false
	}
	return entry.count < 10
}

func (a *adminServer) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.failures[ip]
	if !ok {
		entry = &failures{}
		a.failures[ip] = entry
	}
	entry.count++
	entry.lastFail = time.Now()
	if entry.count >= 10 {
		entry.blocked = time.Now().Add(5 * time.Minute)
		entry.count = 0
	}
}

// ---------------------------------------------------------------------------
// password hashing

func hashAdminPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password is too short")
	}
	if len(password) > 128 {
		return "", errors.New("password is too long")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}
