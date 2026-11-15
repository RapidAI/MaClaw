package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// viewerGrant is one signed screen ticket Hub will still confirm.
type viewerGrant struct {
	Page     string
	NodeID   string
	TenantID string
	UserID   string
	Control  bool
	Expires  time.Time
	// Key is the desktopd key that signed this ticket. A rotation must
	// not keep handing out a page the desktop will reject.
	Key string
}

var viewerGrants struct {
	mu    sync.Mutex
	items map[string]viewerGrant
}

// viewerStillOpen reports that this user's picture is still published.
// Nil means a confirmed ticket stays valid until it expires.
var viewerStillOpen func(tenantID, userID string) bool

// edgeDirectURL is the intranet noVNC page for a node desktop, or "" when
// this view stays on the Hub relay. The gate token is not in the URL.
func edgeDirectURL(tenantID, userID, raw string, control bool) string {
	if edgeRegistry == nil || !edgenode.Enabled() {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	nodeID := edgenode.NodeIDFromHost(parsed.Host)
	if nodeID == "" {
		return ""
	}
	userKey := tunnelUserKey(parsed.EscapedPath())
	if userKey == "" {
		return ""
	}
	node, err := edgeRegistry.Lookup(context.Background(), nodeID)
	if err != nil || node.DesktopdKey == "" || strings.TrimSpace(node.DirectBase) == "" {
		return ""
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	dropStaleViewerGrants(node.ID, node.DesktopdKey)
	if page, ok := reuseViewerGrant(node.ID, tenantID, userID, node.DesktopdKey, control); ok {
		return page
	}
	if p, err := edgeRegistry.Policy(context.Background(), tenantID); err == nil && p.MaxViews > 0 && liveViewerCount(tenantID) >= p.MaxViews {
		return ""
	}
	ticket, err := edgelink.SignViewer(node.DesktopdKey, edgelink.ViewerClaims{
		NodeID:   node.ID,
		TenantID: tenantID,
		UserID:   userID,
		UserKey:  userKey,
		Display:  "desktop",
		Control:  control,
		Exp:      time.Now().Add(2 * time.Hour).Unix(),
	})
	if err != nil {
		return ""
	}
	claims, err := edgelink.VerifyViewer(node.DesktopdKey, ticket, time.Now())
	if err != nil || claims.ID == "" {
		return ""
	}
	page := viewerPage(node.DirectBase, ticket, control)
	rememberViewerGrant(claims.ID, viewerGrant{
		Page:     page,
		NodeID:   node.ID,
		TenantID: tenantID,
		UserID:   userID,
		Control:  control,
		Expires:  time.Unix(claims.Exp, 0),
		Key:      node.DesktopdKey,
	})
	return page
}

// viewerPage is the noVNC document. path keeps the socket under this
// ticket. Without it, noVNC opens /websockify on the desktopd root.
func viewerPage(base, ticket string, control bool) string {
	socket := "v1/desktops/viewer/" + ticket + "/websockify"
	page := strings.TrimRight(base, "/") + "/v1/desktops/viewer/" + ticket + "/vnc.html?autoconnect=1&resize=scale&reconnect=1&reconnect_delay=1000&path=" + url.QueryEscape(socket)
	if !control {
		page += "&view_only=1"
	}
	return page
}

func liveViewerCount(tenantID string) int {
	tenantID = store.NormalizeTenantID(tenantID)
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	now := time.Now()
	n := 0
	for id, grant := range viewerGrants.items {
		if !now.Before(grant.Expires) {
			delete(viewerGrants.items, id)
			continue
		}
		if store.NormalizeTenantID(grant.TenantID) == tenantID {
			n++
		}
	}
	return n
}

func tunnelUserKey(path string) string {
	const marker = "/v1/desktops/tunnel/"
	at := strings.Index(path, marker)
	if at < 0 {
		return ""
	}
	rest := path[at+len(marker):]
	key, tail, ok := strings.Cut(rest, "/")
	if !ok || !desktop.ValidUserKey(key) || !strings.HasPrefix(tail, "vnc") {
		return ""
	}
	return key
}

func dropStaleViewerGrants(nodeID, key string) {
	nodeID = strings.TrimSpace(nodeID)
	key = strings.TrimSpace(key)
	if nodeID == "" || key == "" {
		return
	}
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	for id, grant := range viewerGrants.items {
		if grant.NodeID == nodeID && grant.Key != key {
			delete(viewerGrants.items, id)
		}
	}
}

func reuseViewerGrant(nodeID, tenantID, userID, key string, control bool) (string, bool) {
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	now := time.Now()
	key = strings.TrimSpace(key)
	for id, grant := range viewerGrants.items {
		if !now.Before(grant.Expires) {
			delete(viewerGrants.items, id)
			continue
		}
		if grant.NodeID == nodeID && grant.Key == key && grant.TenantID == tenantID && grant.UserID == userID && grant.Control == control {
			// A page without path= asks noVNC for /websockify at the host
			// root, which is not this ticket. Drop it and sign a new one.
			if !strings.Contains(grant.Page, "websockify") {
				delete(viewerGrants.items, id)
				continue
			}
			return grant.Page, true
		}
	}
	return "", false
}

func rememberViewerGrant(id string, grant viewerGrant) {
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	if viewerGrants.items == nil {
		viewerGrants.items = map[string]viewerGrant{}
	}
	viewerGrants.items[id] = grant
}

func lookupViewerGrant(id string) (viewerGrant, bool) {
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	grant, ok := viewerGrants.items[id]
	if !ok || !time.Now().Before(grant.Expires) {
		delete(viewerGrants.items, id)
		return viewerGrant{}, false
	}
	return grant, true
}

// EdgeViewerConfirmHandler GET /api/v1/edge/viewer/{id}
// The node asks with its node secret when a direct WebSocket opens.
func EdgeViewerConfirmHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			http.NotFound(w, r)
			return
		}
		nodeID := strings.TrimSpace(r.Header.Get("X-Node-Id"))
		secret := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		node, err := reg.Authenticate(r.Context(), nodeID, secret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "EDGE_AUTH", "node secret is required")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		grant, ok := lookupViewerGrant(id)
		if !ok || grant.NodeID != node.ID {
			http.NotFound(w, r)
			return
		}
		if viewerStillOpen != nil && !viewerStillOpen(grant.TenantID, grant.UserID) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "control": grant.Control})
	}
}
