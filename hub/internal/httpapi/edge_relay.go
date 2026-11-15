package httpapi

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

// edgeDesktopRelayPrefix is Hub's public face of a node desktopd's CDP and
// noVNC tunnel: /api/v1/edge/desktop/<node>/v1/desktops/tunnel/<key>/...
// Only that tunnel path is forwarded; the desktopd API is not reachable here.
const edgeDesktopRelayPrefix = "/api/v1/edge/desktop/"

const edgeTunnelPath = "/v1/desktops/tunnel/"

type edgeOriginKey struct{}

// withEdgeOrigin remembers the Hub origin the caller reached, so a CDP URL on
// a node's virtual host can be handed back as a relay on that same origin.
func withEdgeOrigin(ctx context.Context, r *http.Request) context.Context {
	if edgeRegistry == nil || r == nil {
		return ctx
	}
	return context.WithValue(ctx, edgeOriginKey{}, edgeRequestOrigin(r))
}

// edgeRequestOrigin is HUB_EDGE_PUBLIC_URL when set, else the scheme and Host
// the request came in on (MaClawSrv dials Hub by MACLAW_HUB_URL).
func edgeRequestOrigin(r *http.Request) string {
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("HUB_EDGE_PUBLIC_URL")), "/"); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil {
			return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/")
		}
	}
	host := strings.TrimSpace(r.Host)
	if host == "" || strings.ContainsAny(host, " /\\@?#\r\n") {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); strings.EqualFold(proto, "https") {
		scheme = "https"
	}
	return scheme + "://" + host
}

// edgeRelayCDP rewrites a CDP URL on desktopd.<id>.node.invalid to Hub's
// relay. Any other URL is returned unchanged.
func edgeRelayCDP(ctx context.Context, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return raw
	}
	nodeID := edgenode.NodeIDFromHost(parsed.Host)
	if nodeID == "" {
		return raw
	}
	origin, _ := ctx.Value(edgeOriginKey{}).(string)
	if origin == "" {
		return raw
	}
	base, err := url.Parse(origin)
	if err != nil || base.Host == "" {
		return raw
	}
	tail := parsed.EscapedPath()
	if !strings.HasPrefix(tail, edgeTunnelPath) {
		return raw
	}
	out := *parsed
	out.Scheme = base.Scheme
	out.Host = base.Host
	rawPath := strings.TrimRight(base.EscapedPath(), "/") + edgeDesktopRelayPrefix + nodeID + tail
	if p, err := url.PathUnescape(rawPath); err == nil {
		out.Path = p
		out.RawPath = rawPath
	}
	return out.String()
}

// EdgeDesktopRelayHandler serves /api/v1/edge/desktop/{node}/{rest...}.
// The desktop gate token (Bearer) is the credential, exactly as on a public
// desktopd; Hub adds nothing and strips its own cookies.
func EdgeDesktopRelayHandler(reg *edgenode.Registry, broker *edgenode.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || broker == nil || !edgenode.Enabled() {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) {
			http.NotFound(w, r)
			return
		}
		tail := strings.TrimPrefix(r.URL.Path, edgeDesktopRelayPrefix)
		nodeID, rest, ok := strings.Cut(tail, "/")
		if !ok || nodeID == "" || !strings.HasPrefix("/"+rest, edgeTunnelPath) {
			http.NotFound(w, r)
			return
		}
		node, err := reg.Lookup(r.Context(), nodeID)
		if err != nil || !node.HasDesktopd {
			http.NotFound(w, r)
			return
		}
		client := broker.Client(node.TenantID, node.ID, edgelink.TargetDesktopd)
		if client == nil || !broker.Online(node.ID) {
			http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
			return
		}
		host := "desktopd." + node.ID + ".node.invalid"
		upstreamPath := "/" + rest
		proxy := &httputil.ReverseProxy{
			Transport: client.Transport,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(&url.URL{Scheme: "http", Host: host})
				pr.Out.URL.Path = upstreamPath
				pr.Out.URL.RawPath = ""
				pr.Out.URL.RawQuery = r.URL.RawQuery
				pr.Out.Host = host
				pr.Out.Header.Del("Cookie")
			},
			FlushInterval: -1,
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
				_ = err
			},
		}
		proxy.ServeHTTP(w, r)
	}
}
