package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// edgeCDPPrefix is the ticket relay a srv uses when its desktopd is on
// another node. The gate token stays on Hub; the ticket is the credential.
const edgeCDPPrefix = "/api/v1/desktop-cdp/"

// cdpTicketTTL is how long a relay ticket opens that user's screen.
var cdpTicketTTL = 2 * time.Hour

type edgeCallerKey struct{}

type cdpTicket struct {
	NodeID    string
	TenantID  string
	UserID    string
	Display   string
	UserKey   string
	GateToken string
	Expires   time.Time
}

var cdpTickets struct {
	mu    sync.Mutex
	items map[string]cdpTicket
}

func withEdgeCaller(ctx context.Context, r *http.Request) context.Context {
	if edgeRegistry == nil || r == nil {
		return ctx
	}
	id := edgeRegistry.NodeIDForHubToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return context.WithValue(ctx, edgeCallerKey{}, id)
}

func edgeCallerNode(ctx context.Context) string {
	id, _ := ctx.Value(edgeCallerKey{}).(string)
	return id
}

// cdpURLForSession picks the CDP address MaClawSrv can dial.
// A public desktop is unchanged. A srv on the desktop's own node gets the
// intranet address. Every other caller gets a Hub ticket, not the gate token.
func cdpURLForSession(ctx context.Context, tenantID, userID string, session desktoppool.DesktopSession) (string, error) {
	nodeID := strings.TrimSpace(session.NodeID)
	if nodeID == "" || !edgenode.Enabled() {
		return edgeRelayCDP(ctx, session.CDP), nil
	}
	if edgeCallerNode(ctx) != "" && edgeCallerNode(ctx) == nodeID {
		if local := strings.TrimSpace(session.LocalCDP); local != "" {
			return local, nil
		}
	}
	issued, err := issueCDPTicketURL(ctx, tenantID, userID, session)
	if err != nil {
		return "", err
	}
	if issued != "" {
		return issued, nil
	}
	return edgeRelayCDP(ctx, session.CDP), nil
}

func issueCDPTicketURL(ctx context.Context, tenantID, userID string, session desktoppool.DesktopSession) (string, error) {
	origin, _ := ctx.Value(edgeOriginKey{}).(string)
	if origin == "" {
		return "", nil
	}
	userKey, token, ok := tunnelCDPParts(session.LocalCDP)
	if !ok {
		userKey, token, ok = tunnelCDPParts(session.CDP)
	}
	if !ok {
		return "", errCDPRelay
	}
	want, err := desktop.UserKey(tenantID, userID)
	if err != nil || want != userKey {
		return "", errCDPRelay
	}
	id, err := randomTicket()
	if err != nil {
		return "", err
	}
	cdpTickets.mu.Lock()
	if cdpTickets.items == nil {
		cdpTickets.items = map[string]cdpTicket{}
	}
	cdpTickets.items[id] = cdpTicket{
		NodeID:    strings.TrimSpace(session.NodeID),
		TenantID:  store.NormalizeTenantID(tenantID),
		UserID:    strings.TrimSpace(userID),
		Display:   strings.TrimSpace(session.Display),
		UserKey:   userKey,
		GateToken: token,
		Expires:   time.Now().Add(cdpTicketTTL),
	}
	cdpTickets.mu.Unlock()
	return strings.TrimRight(origin, "/") + edgeCDPPrefix + id, nil
}

var errCDPRelay = errors.New("desktop cdp relay is unavailable")

func randomTicket() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func lookupCDPTicket(id string) (cdpTicket, bool) {
	id = strings.TrimSpace(id)
	cdpTickets.mu.Lock()
	defer cdpTickets.mu.Unlock()
	ticket, ok := cdpTickets.items[id]
	if !ok {
		return cdpTicket{}, false
	}
	if !time.Now().Before(ticket.Expires) {
		delete(cdpTickets.items, id)
		return cdpTicket{}, false
	}
	return ticket, true
}

func dropCDPTickets(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	cdpTickets.mu.Lock()
	defer cdpTickets.mu.Unlock()
	for id, ticket := range cdpTickets.items {
		if ticket.NodeID == nodeID {
			delete(cdpTickets.items, id)
		}
	}
}

func tunnelCDPParts(raw string) (userKey, token string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	path := parsed.EscapedPath()
	const marker = "/v1/desktops/tunnel/"
	at := strings.Index(path, marker)
	if at < 0 {
		return "", "", false
	}
	rest := path[at+len(marker):]
	key, tail, found := strings.Cut(rest, "/")
	if !found || !desktop.ValidUserKey(key) || (tail != "cdp" && !strings.HasPrefix(tail, "cdp/")) {
		return "", "", false
	}
	if parsed.User != nil {
		token, _ = parsed.User.Password()
	}
	return key, token, true
}

// EdgeCDPRelayHandler serves /api/v1/desktop-cdp/{ticket}/{rest...}.
// The ticket is the credential. Hub adds the desktop gate token on the hop
// through the node tunnel and rewrites Chrome's debugger socket.
func EdgeCDPRelayHandler(reg *edgenode.Registry, broker *edgenode.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || broker == nil || !edgenode.Enabled() {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) {
			http.NotFound(w, r)
			return
		}
		ticketID := strings.TrimSpace(r.PathValue("ticket"))
		rest := strings.TrimPrefix(r.PathValue("rest"), "/")
		ticket, ok := lookupCDPTicket(ticketID)
		if !ok || ticket.NodeID == "" || !desktop.ValidUserKey(ticket.UserKey) {
			http.NotFound(w, r)
			return
		}
		node, err := reg.Lookup(r.Context(), ticket.NodeID)
		if err != nil || !node.HasDesktopd || store.NormalizeTenantID(node.TenantID) != ticket.TenantID {
			http.NotFound(w, r)
			return
		}
		client := broker.Client(ticket.TenantID, ticket.NodeID, edgelink.TargetDesktopd)
		if client == nil || client.Transport == nil || !broker.Online(ticket.NodeID) {
			http.Error(w, "desktop tunnel unavailable", http.StatusBadGateway)
			return
		}
		host := "desktopd." + ticket.NodeID + ".node.invalid"
		upstreamPath := "/v1/desktops/tunnel/" + ticket.UserKey + "/cdp"
		if rest != "" {
			upstreamPath += "/" + rest
		}
		public := edgeRequestOrigin(r)
		if public == "" {
			public = "http://" + r.Host
		}
		public += edgeCDPPrefix + ticketID
		proxy := &httputil.ReverseProxy{
			Transport: client.Transport,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(&url.URL{Scheme: "http", Host: host})
				pr.Out.URL.Path = upstreamPath
				pr.Out.URL.RawPath = ""
				pr.Out.URL.RawQuery = r.URL.RawQuery
				pr.Out.Host = host
				pr.Out.Header.Del("Cookie")
				pr.Out.Header.Del("Authorization")
				if ticket.GateToken != "" {
					pr.Out.Header.Set("Authorization", "Bearer "+ticket.GateToken)
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				return rewriteCDPDebuggerResponse(resp, rest, public)
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

func rewriteCDPDebuggerResponse(resp *http.Response, rest, public string) error {
	if resp == nil || resp.StatusCode == http.StatusSwitchingProtocols || resp.Body == nil {
		return nil
	}
	if !cdpJSONPath(rest) {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}
	rewritten := rewriteCDPDebuggerURLs(body, public)
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	return nil
}

func cdpJSONPath(rest string) bool {
	rest = strings.Trim(rest, "/")
	switch rest {
	case "json", "json/version", "json/list", "json/new":
		return true
	default:
		return strings.HasPrefix(rest, "json/")
	}
}

func rewriteCDPDebuggerURLs(body []byte, public string) []byte {
	base, err := url.Parse(strings.TrimSpace(public))
	if err != nil || base.Host == "" {
		return body
	}
	var parsed any
	if json.Unmarshal(body, &parsed) != nil {
		return body
	}
	if !rewriteDebugValue(parsed, base) {
		return body
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	return out
}

func rewriteDebugValue(v any, base *url.URL) bool {
	switch node := v.(type) {
	case map[string]any:
		changed := false
		for key, child := range node {
			if key == "webSocketDebuggerUrl" {
				raw, _ := child.(string)
				if next := cdpDebugURLOnTicket(base, raw); next != "" && next != raw {
					node[key] = next
					changed = true
				}
				continue
			}
			if rewriteDebugValue(child, base) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, child := range node {
			if rewriteDebugValue(child, base) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func cdpDebugURLOnTicket(base *url.URL, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Path == "" {
		return ""
	}
	idx := strings.Index(parsed.Path, "/devtools/")
	if idx < 0 {
		return ""
	}
	out := *parsed
	if base.Scheme == "https" {
		out.Scheme = "wss"
	} else {
		out.Scheme = "ws"
	}
	out.Host = base.Host
	out.User = nil
	out.Path = strings.TrimRight(base.EscapedPath(), "/") + parsed.Path[idx:]
	out.RawPath = ""
	out.RawQuery = ""
	out.Fragment = ""
	return out.String()
}
