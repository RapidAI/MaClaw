package desktopd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const tunnelPrefix = "/v1/desktops/tunnel/"

// ParsePublishBind accepts an empty value (127.0.0.1), a loopback address, or
// an RFC1918 private address. 0.0.0.0 and public addresses are refused: Docker
// would otherwise publish the browser and screen gates on every interface,
// and its iptables rules bypass the host firewall. IPv6 is refused because
// the docker publish form for those addresses is a different syntax.
func ParsePublishBind(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "127.0.0.1", nil
	}
	ip := net.ParseIP(raw)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return "", fmt.Errorf("%w: DESKTOPD_PUBLISH_BIND must be an IPv4 loopback or private address", ErrInvalid)
	}
	return ip.String(), nil
}

func (s *Service) publishBind() (string, error) {
	raw := ""
	if s != nil {
		raw = s.PublishBind
	}
	return ParsePublishBind(raw)
}

// publishSpec is the docker run --publish value. The empty host port lets
// Docker pick a random one. The address is the only part this process chooses.
func publishSpec(bind, containerPort string) string {
	return bind + "::" + containerPort + "/tcp"
}

// ParsePublicURL checks the optional DESKTOPD_PUBLIC_URL override. The result
// has no userinfo and no query: the gate token is added per session.
func ParsePublicURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("%w: DESKTOPD_PUBLIC_URL must be an http or https URL", ErrInvalid)
	}
	if strings.Contains(parsed.Path, "..") || strings.Contains(parsed.Path, `\`) {
		return "", fmt.Errorf("%w: DESKTOPD_PUBLIC_URL must be an http or https URL", ErrInvalid)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// publicBase is where Hub and MaClawSrv dial the tunnel. An explicit PublicURL
// wins. Otherwise the scheme is https and the host is the advertise name,
// which nginx already proxies to this process.
func (s *Service) publicBase() (string, error) {
	raw := ""
	if s != nil {
		raw = strings.TrimSpace(s.PublicURL)
	}
	if raw != "" {
		return ParsePublicURL(raw)
	}
	host, err := s.advertiseHost()
	if err != nil {
		return "", err
	}
	return "https://" + host, nil
}

// tunnelEndpoint builds one CDP or noVNC URL. kind is "cdp" or "vnc".
// The token is URL userinfo only. tail is a file such as vnc.html.
func tunnelEndpoint(base, userKey, kind, token, tail, rawQuery string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("%w: public desktop url is invalid", ErrInvalid)
	}
	if !desktop.ValidUserKey(userKey) || (kind != "cdp" && kind != "vnc") {
		return "", fmt.Errorf("%w: desktop tunnel is invalid", ErrInvalid)
	}
	if strings.Contains(tail, "..") || strings.Contains(tail, `\`) || strings.Contains(tail, "?") {
		return "", fmt.Errorf("%w: desktop tunnel is invalid", ErrInvalid)
	}
	if token != "" {
		parsed.User = url.UserPassword("desktop", token)
	}
	path := strings.TrimRight(parsed.Path, "/") + tunnelPrefix + userKey + "/" + kind
	if tail != "" {
		path += "/" + strings.TrimPrefix(tail, "/")
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = rawQuery
	return parsed.String(), nil
}

func (s *Service) rememberPublished(userKey, containerPort, hostPort string) {
	if s == nil || !desktop.ValidUserKey(userKey) || hostPort == "" {
		return
	}
	if _, err := strconv.Atoi(hostPort); err != nil {
		return
	}
	s.publishedPorts.Store(userKey+"/"+containerPort, hostPort)
}

func (s *Service) forgetPublished(userKey string) {
	if s == nil || userKey == "" {
		return
	}
	s.publishedPorts.Delete(userKey + "/" + desktop.ProxyPort)
	s.publishedPorts.Delete(userKey + "/" + desktop.VNCPort)
}

// publishedHostPort returns the host port Docker assigned to this user's
// container. A cached value from OpenSession is used. A miss (process
// restart, or a run that dropped the cache) asks docker again. The address
// Docker printed is ignored: 0.0.0.0 is not a dial target.
func (s *Service) publishedHostPort(ctx context.Context, userKey, containerPort string) (string, error) {
	if s == nil || !desktop.ValidUserKey(userKey) {
		return "", fmt.Errorf("%w: desktop tunnel is invalid", ErrInvalid)
	}
	cacheKey := userKey + "/" + containerPort
	if value, ok := s.publishedPorts.Load(cacheKey); ok {
		if port, ok := value.(string); ok && port != "" {
			return port, nil
		}
	}
	out, err := s.docker(ctx, "port", "maclaw-desktop-"+userKey, containerPort+"/tcp")
	if err != nil {
		return "", err
	}
	port, err := publishedPort(out)
	if err != nil {
		return "", err
	}
	s.publishedPorts.Store(cacheKey, port)
	return port, nil
}

// serveTunnel proxies one user's CDP or noVNC gate. It does not check the
// desktopd API token: the CDP client sends the gate token as Bearer, and the
// container gate is what accepts or rejects it. The path cannot choose a
// host or a port. A hit does not refresh last_activity; an unauthenticated
// scan must not keep the machine awake. The token is not logged.
func (s *Service) serveTunnel(w http.ResponseWriter, r *http.Request) {
	if s == nil || r.URL == nil {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) || strings.Contains(r.URL.RawPath, `\`) {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, tunnelPrefix) {
		http.NotFound(w, r)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, tunnelPrefix)
	userKey, rest, ok := strings.Cut(tail, "/")
	if !ok || !desktop.ValidUserKey(userKey) {
		http.NotFound(w, r)
		return
	}
	kind, restPath, found := strings.Cut(rest, "/")
	if !found {
		kind = rest
		restPath = ""
	}
	var containerPort string
	switch kind {
	case "cdp":
		containerPort = desktop.ProxyPort
	case "vnc":
		containerPort = desktop.VNCPort
	default:
		http.NotFound(w, r)
		return
	}
	if strings.Contains(restPath, "..") || strings.Contains(restPath, `\`) {
		http.NotFound(w, r)
		return
	}
	hostPort, err := s.publishedHostPort(r.Context(), userKey, containerPort)
	if err != nil {
		http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
		return
	}
	bind, err := s.publishBind()
	if err != nil {
		http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
		return
	}
	// Chromium rejects a DNS Host. This hop is the gate, so the header is an
	// IP. The browser's own Host stays the public name; see cdpChromeHost.
	upstreamHost := net.JoinHostPort(bind, hostPort)
	target := &url.URL{Scheme: "http", Host: upstreamHost}
	upstreamPath := "/" + restPath
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = upstreamPath
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.Out.Host = upstreamHost
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
			_ = err
		},
	}
	proxy.ServeHTTP(w, r)
}
