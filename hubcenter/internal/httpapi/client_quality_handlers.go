package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

type clientQualityProvider interface {
	GetClientQuality(ctx context.Context) (*ha.ClientQualityView, error)
	ListClientEndpoints(ctx context.Context) (*ha.EndpointsView, error)
}

type clientHAConfigProvider interface {
	CurrentConfig(ctx context.Context) (config.HAConfig, error)
}

type clientHubCenterNodeView struct {
	FQDN         string `json:"fqdn,omitempty"`
	NodeID       string `json:"node_id"`
	NodeName     string `json:"node_name,omitempty"`
	BaseURL      string `json:"base_url"`
	PublicKeyPEM string `json:"public_key,omitempty"`
	Current      bool   `json:"current"`
	Configured   bool   `json:"configured"`
}

type clientHubCentersView struct {
	OK         bool                      `json:"ok"`
	URLs       []string                  `json:"urls"`
	Nodes      []clientHubCenterNodeView `json:"nodes"`
	Count      int                       `json:"count"`
	TTLSeconds int                       `json:"ttl_seconds"`
	ServerTime time.Time                 `json:"server_time"`
}

func ClientQualityHandler(svc clientQualityProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             true,
				"node_id":        "",
				"node_name":      "",
				"service_status": "healthy",
				"quality_score":  100,
				"routable":       true,
				"cluster": map[string]any{
					"reachable_nodes": 1,
					"total_nodes":     1,
					"status":          "healthy",
				},
				"sync": map[string]any{
					"enabled":         false,
					"lag_seconds":     0,
					"backlog":         0,
					"last_success_at": nil,
				},
				"features": map[string]any{
					"can_register":  true,
					"can_heartbeat": true,
					"can_resolve":   true,
				},
				"server_time": time.Now().UTC(),
				"ttl_seconds": 15,
			})
			return
		}

		view, err := svc.GetClientQuality(r.Context())
		if err != nil {
			writeClientAwareError(w, http.StatusInternalServerError, "CLIENT_QUALITY_FAILED", err.Error(), true, true)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func ClientEndpointsHandler(svc clientQualityProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":          true,
				"nodes":       []any{},
				"ttl_seconds": 60,
				"server_time": time.Now().UTC(),
			})
			return
		}

		view, err := svc.ListClientEndpoints(r.Context())
		if err != nil {
			writeClientAwareError(w, http.StatusInternalServerError, "CLIENT_ENDPOINTS_FAILED", err.Error(), true, true)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func ClientHubCentersHandler(svc clientHAConfigProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, clientHubCentersView{OK: true, URLs: []string{}, Nodes: []clientHubCenterNodeView{}, Count: 0, TTLSeconds: 300, ServerTime: time.Now().UTC()})
			return
		}
		cfg, err := svc.CurrentConfig(r.Context())
		if err != nil {
			writeClientAwareError(w, http.StatusInternalServerError, "CLIENT_HUBCENTERS_FAILED", err.Error(), true, true)
			return
		}
		writeJSON(w, http.StatusOK, buildClientHubCentersView(cfg))
	}
}

func buildClientHubCentersView(cfg config.HAConfig) clientHubCentersView {
	nodes := make([]clientHubCenterNodeView, 0)
	if len(cfg.Nodes) > 0 {
		for _, node := range cfg.Nodes {
			if !node.Enabled {
				continue
			}
			nodes = append(nodes, clientHubCenterNodeView{
				FQDN:         node.FQDN,
				NodeID:       node.NodeID,
				NodeName:     node.NodeName,
				BaseURL:      normalizeClientHubCenterURL(node.ClientFacingURL()),
				PublicKeyPEM: node.PublicKeyPEM,
				Current:      node.NodeID == cfg.NodeID || (node.FQDN != "" && strings.EqualFold(node.FQDN, cfg.SelfFQDN)),
				Configured:   true,
			})
		}
	} else if cfg.NodeID != "" || cfg.AdvertiseURL != "" || cfg.SelfFQDN != "" {
		// Legacy path (no node catalog): PublicURL is optional for compatibility.
		// Prefer it whenever configured so clients never receive an HA transport
		// address that is reachable only between cluster nodes.
		// For node-catalog deployments (len(cfg.Nodes) > 0), ClientFacingURL() is used above.
		nodes = append(nodes, clientHubCenterNodeView{NodeID: cfg.NodeID, NodeName: cfg.NodeName, FQDN: cfg.SelfFQDN, BaseURL: normalizeClientHubCenterURL(cfg.AdvertiseURL), Current: true, Configured: cfg.Enabled})
		for _, peer := range cfg.Peers {
			if !peer.Enabled {
				continue
			}
			peerURL := peer.PublicURL
			if strings.TrimSpace(peerURL) == "" {
				peerURL = peer.BaseURL
			}
			nodes = append(nodes, clientHubCenterNodeView{NodeID: peer.NodeID, NodeName: peer.Name, BaseURL: normalizeClientHubCenterURL(peerURL), PublicKeyPEM: peer.PublicKeyPEM, Current: false, Configured: cfg.Enabled})
		}
	}
	urls := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		url := normalizeClientHubCenterURL(node.BaseURL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	return clientHubCentersView{OK: true, URLs: urls, Nodes: nodes, Count: len(urls), TTLSeconds: 300, ServerTime: time.Now().UTC()}
}

func normalizeClientHubCenterURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	return strings.TrimRight(value, "/")
}
