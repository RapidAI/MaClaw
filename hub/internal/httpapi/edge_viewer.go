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
	if page, ok := reuseViewerGrant(node.ID, tenantID, userID, control); ok {
		return page
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
	page := strings.TrimRight(node.DirectBase, "/") + "/v1/desktops/viewer/" + ticket + "/vnc.html?autoconnect=1&resize=scale"
	if !control {
		page += "&view_only=1"
	}
	rememberViewerGrant(claims.ID, viewerGrant{
		Page:     page,
		NodeID:   node.ID,
		TenantID: tenantID,
		UserID:   userID,
		Control:  control,
		Expires:  time.Unix(claims.Exp, 0),
	})
	return page
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

func reuseViewerGrant(nodeID, tenantID, userID string, control bool) (string, bool) {
	viewerGrants.mu.Lock()
	defer viewerGrants.mu.Unlock()
	now := time.Now()
	for id, grant := range viewerGrants.items {
		if !now.Before(grant.Expires) {
			delete(viewerGrants.items, id)
			continue
		}
		if grant.NodeID == nodeID && grant.TenantID == tenantID && grant.UserID == userID && grant.Control == control {
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
