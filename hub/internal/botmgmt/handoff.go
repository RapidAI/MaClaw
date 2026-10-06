package botmgmt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type handoffView struct {
	origin string
	until  time.Time
	// bearer is the Docker-side noVNC gate token taken from the raw URL's
	// userinfo. The proxy attaches it so the published port can require it.
	bearer string
}

// gateDesktopHandoff hides the Docker host's noVNC port behind Hub.
// The GUI loads this path on Hub. The dockerd domain keeps proxying only the API.
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
	s.handoff[token] = handoffView{origin: parsed.Scheme + "://" + parsed.Host, until: now.Add(2 * time.Hour), bearer: bearer}
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
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = "/" + rest
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
