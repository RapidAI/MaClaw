package desktopd

import (
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

const viewerPrefix = "/v1/desktops/viewer/"

// serveViewer is the intranet picture. The ticket is signed with the extra
// hub key from enrollment, or the primary token when that key was never
// issued. A WebSocket also asks Hub, so a withdrawn view stops on the next
// connection. POST /v1/desktops/view is a different route.
func (s *Service) serveViewer(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.HubLink == nil || r.URL == nil {
		http.NotFound(w, r)
		return
	}
	// The GUI probes this host from its own page. Without these headers the
	// browser hides the status and the picture stays on the Hub relay.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, viewerPrefix)
	ticket, rest, ok := strings.Cut(tail, "/")
	if !ok || ticket == "" || rest == "" {
		http.NotFound(w, r)
		return
	}
	claims, err := verifyViewerTicket(s.HubLink, ticket, time.Now())
	if err != nil || claims.NodeID != s.HubLink.NodeID() {
		http.NotFound(w, r)
		return
	}
	if isWebsocket(r) {
		if err := s.HubLink.ConfirmViewer(r.Context(), claims.ID); err != nil {
			http.Error(w, "viewer was withdrawn", http.StatusForbidden)
			return
		}
	}
	token, ok := desktopGates.tokenFor(claims.TenantID, claims.UserID)
	if !ok {
		http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
		return
	}
	upstream := "/v1/desktops/tunnel/" + claims.UserKey + "/vnc/" + strings.TrimPrefix(rest, "/")
	clone := r.Clone(r.Context())
	clone.URL.Path = upstream
	clone.URL.RawPath = ""
	clone.Header = r.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	if !claims.Control {
		query := clone.URL.Query()
		query.Set("view_only", "1")
		clone.URL.RawQuery = query.Encode()
	}
	// The container page often sends X-Frame-Options. The GUI embeds this
	// response, so that header would leave a blank frame. The socket upgrade
	// still reaches the real writer through Unwrap.
	s.serveTunnel(&frameOK{ResponseWriter: w}, clone)
}

// frameOK lets the desktop page be embedded. Hijack and flush follow the
// inner writer so a noVNC socket still upgrades.
type frameOK struct {
	http.ResponseWriter
}

func (f *frameOK) WriteHeader(code int) {
	f.relax()
	f.ResponseWriter.WriteHeader(code)
}

func (f *frameOK) Write(p []byte) (int, error) {
	f.relax()
	return f.ResponseWriter.Write(p)
}

func (f *frameOK) relax() {
	h := f.Header()
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", "frame-ancestors *")
}

func (f *frameOK) Unwrap() http.ResponseWriter {
	if f == nil {
		return nil
	}
	return f.ResponseWriter
}

func verifyViewerTicket(link *HubLink, ticket string, now time.Time) (edgelink.ViewerClaims, error) {
	if link == nil {
		return edgelink.VerifyViewer("", ticket, now)
	}
	return edgelink.VerifyViewer(link.signingKey(), ticket, now)
}

func isWebsocket(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}
