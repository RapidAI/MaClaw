package botmgmt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"
)

type handoffView struct {
	origin string
	// base is the upstream directory in front of vnc.html. Empty means the
	// page is at the origin root, which is the historical host:port URL.
	// A tunnel URL keeps /v1/desktops/tunnel/<key>/vnc so websockify is not
	// requested from the API root.
	base  string
	until time.Time
	// bearer is the Docker-side noVNC gate token taken from the raw URL's
	// userinfo. The proxy attaches it so the published port can require it.
	bearer string
}

// gateDesktopHandoff hides the Docker host's noVNC port behind Hub.
// The GUI loads this path on Hub. The dockerd domain keeps proxying only the API.
// The token means the person is looking at the screen for two hours. Looking
// it up does not extend that time, does not refresh last_used, and does not
// stop or start the container. Sleep stays on the idle path.
func (s *Service) gateDesktopHandoff(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf[:])
	bearer := ""
	if parsed.User != nil {
		bearer, _ = parsed.User.Password()
	}
	base, ok := handoffBase(parsed.Path)
	if !ok {
		return ""
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handoff == nil {
		s.handoff = map[string]handoffView{}
	}
	for key, view := range s.handoff {
		if !now.Before(view.until) {
			delete(s.handoff, key)
		}
	}
	s.handoff[token] = handoffView{origin: parsed.Scheme + "://" + parsed.Host, base: base, until: now.Add(2 * time.Hour), bearer: bearer}
	socketPath := "api/v1/desktop-handoff/" + token + "/websockify"
	return "/api/v1/desktop-handoff/" + token + "/vnc.html?autoconnect=1&resize=scale&reconnect=1&reconnect_delay=1000&path=" + url.QueryEscape(socketPath)
}

func (s *Service) handoffOrigin(token string) string {
	if s == nil || !validHandoffToken(token) {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view, ok := s.handoff[token]
	if !ok || !s.now().Before(view.until) {
		delete(s.handoff, token)
		return ""
	}
	return view.origin
}

// handoffViewFor returns the full stored view, including the upstream bearer.
func (s *Service) handoffViewFor(token string) (handoffView, bool) {
	if s == nil || !validHandoffToken(token) {
		return handoffView{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view, ok := s.handoff[token]
	if !ok || !s.now().Before(view.until) {
		delete(s.handoff, token)
		return handoffView{}, false
	}
	return view, true
}

// handoffBaseFromRaw recovers the upstream directory from the saved noVNC
// URL. A restart keeps the token, and the tunnel prefix has to come back
// with it or the picture is requested from the origin root.
func handoffBaseFromRaw(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	base, ok := handoffBase(parsed.Path)
	if !ok {
		return ""
	}
	return base
}

// handoffBase is the directory of the upstream noVNC page. "/" means the
// historical URL had no prefix, so the proxy keeps requesting /vnc.html.
// A path that climbs out is refused.
func handoffBase(urlPath string) (string, bool) {
	if strings.Contains(urlPath, "..") || strings.Contains(urlPath, `\`) {
		return "", false
	}
	dir := path.Dir(urlPath)
	if dir == "/" || dir == "." || dir == "" {
		return "", true
	}
	return dir, true
}

func validHandoffToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, ch := range token {
		switch {
		case ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'f':
		default:
			return false
		}
	}
	return true
}

// ProxyDesktopHandoff serves noVNC through Hub for one issued token.
func (s *Service) ProxyDesktopHandoff(w http.ResponseWriter, r *http.Request, token, rest string) {
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" || strings.Contains(rest, "..") || strings.Contains(rest, "\\") {
		http.NotFound(w, r)
		return
	}
	view, ok := s.handoffViewFor(token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	target, err := url.Parse(view.origin)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	upstreamPath := "/" + rest
	if view.base != "" {
		upstreamPath = strings.TrimRight(view.base, "/") + "/" + rest
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = upstreamPath
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.Out.Host = target.Host
			// The published noVNC port requires the Docker-side gate token.
			// The person's browser cannot present it; Hub carries it here.
			if view.bearer != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+view.bearer)
			}
		},
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("X-Frame-Options")
			resp.Header.Set("Content-Security-Policy", "frame-ancestors *")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "desktop handoff unavailable", http.StatusBadGateway)
			_ = err
		},
	}
	proxy.ServeHTTP(w, r)
}

// ParseHandoffPath splits a gated noVNC URL into the token and the upstream path.
func ParseHandoffPath(raw string) (token, rest string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	const prefix = "/api/v1/desktop-handoff/"
	if !strings.HasPrefix(parsed.Path, prefix) {
		return "", "", fmt.Errorf("not a desktop handoff")
	}
	tail := strings.TrimPrefix(parsed.Path, prefix)
	token, rest, ok := strings.Cut(tail, "/")
	if !ok || !validHandoffToken(token) || rest == "" {
		return "", "", fmt.Errorf("not a desktop handoff")
	}
	return token, rest, nil
}
