package desktopd

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

// viewerFallbackScript runs before the player. Three socket closes without a
// lasting open tell the GUI to leave this intranet page and use the Hub address.
const viewerFallbackScript = `<script>(function(){var N=window.WebSocket;if(!N||N.__maclawWrapped)return;var fails=0;function W(url,proto){var ws=proto===undefined?new N(url):new N(url,proto);ws.addEventListener("open",function(){fails=0});ws.addEventListener("close",function(){fails++;if(fails>=3){try{parent.postMessage({type:"maclaw-desktop-down"},"*")}catch(e){}}});return ws}W.prototype=N.prototype;W.CONNECTING=N.CONNECTING;W.OPEN=N.OPEN;W.CLOSING=N.CLOSING;W.CLOSED=N.CLOSED;W.__maclawWrapped=true;window.WebSocket=W})();</script>`

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
	// The player pages are buffered so a reconnect hook can be inserted
	// before their own scripts. Sockets and scripts are not buffered.
	if viewerPageWantsFallback(rest) && r.Method == http.MethodGet && !isWebsocket(r) {
		page := &viewerHTMLWriter{ResponseWriter: &frameOK{ResponseWriter: w}}
		s.serveTunnel(page, clone)
		page.finish()
		return
	}
	s.serveTunnel(&frameOK{ResponseWriter: w}, clone)
}

func viewerPageWantsFallback(rest string) bool {
	base := rest
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return base == "vnc.html" || base == "h264.html"
}

// injectViewerFallback places the reconnect hook at the start of the page
// so it wraps WebSocket before the player creates one.
func injectViewerFallback(body []byte) []byte {
	if bytes.Contains(body, []byte("maclaw-desktop-down")) {
		return body
	}
	script := []byte(viewerFallbackScript)
	lower := bytes.ToLower(body)
	if i := bytes.Index(lower, []byte("<head>")); i >= 0 {
		at := i + len("<head>")
		out := make([]byte, 0, len(body)+len(script))
		out = append(out, body[:at]...)
		out = append(out, script...)
		out = append(out, body[at:]...)
		return out
	}
	out := make([]byte, 0, len(body)+len(script))
	out = append(out, script...)
	out = append(out, body...)
	return out
}

// viewerHTMLWriter holds a player document until the proxy is done, then
// inserts the reconnect hook. Anything that is not an HTML document is
// written through at once, so a script or an error page is unchanged.
type viewerHTMLWriter struct {
	http.ResponseWriter
	buf   bytes.Buffer
	code  int
	html  bool
	wrote bool
}

func (w *viewerHTMLWriter) WriteHeader(code int) {
	if w == nil || w.code != 0 || w.wrote {
		return
	}
	w.code = code
	ctype := strings.ToLower(w.Header().Get("Content-Type"))
	w.html = code >= 200 && code < 300 && (strings.Contains(ctype, "text/html") || ctype == "")
	if !w.html {
		w.wrote = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *viewerHTMLWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, nil
	}
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.html {
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

func (w *viewerHTMLWriter) finish() {
	if w == nil || w.wrote || !w.html {
		return
	}
	if w.code == 0 && w.buf.Len() == 0 {
		return
	}
	body := injectViewerFallback(w.buf.Bytes())
	w.Header().Del("Content-Length")
	if w.code == 0 {
		w.code = http.StatusOK
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(w.code)
	_, _ = w.ResponseWriter.Write(body)
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
