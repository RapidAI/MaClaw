package desktopd

import (
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

const viewerPrefix = "/v1/desktops/viewer/"

// serveViewer is the intranet picture. The ticket is signed with this
// desktopd's API token. A WebSocket also asks Hub, so a withdrawn view
// stops on the next connection. POST /v1/desktops/view is a different route.
func (s *Service) serveViewer(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.HubLink == nil || r.URL == nil {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) {
		http.NotFound(w, r)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, viewerPrefix)
	ticket, rest, ok := strings.Cut(tail, "/")
	if !ok || ticket == "" || rest == "" {
		http.NotFound(w, r)
		return
	}
	claims, err := edgelink.VerifyViewer(s.HubLink.Key, ticket, time.Now())
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
	s.serveTunnel(w, clone)
}

func isWebsocket(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}
