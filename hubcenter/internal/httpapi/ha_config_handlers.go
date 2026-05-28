package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

var haPublicKeyHTTPClient = &http.Client{Timeout: 3 * time.Second}

const haPublicKeyResponseBodyLimit = 1 << 20

type haConfigRequest struct {
	Enabled                         bool            `json:"enabled"`
	SelfFQDN                        string          `json:"self_fqdn,omitempty"`
	SuggestedSelfFQDN               string          `json:"suggested_self_fqdn,omitempty"`
	PrivateKeyPath                  string          `json:"private_key_path,omitempty"`
	NodeID                          string          `json:"node_id"`
	NodeName                        string          `json:"node_name"`
	AdvertiseURL                    string          `json:"advertise_url"`
	ClusterSecret                   string          `json:"cluster_secret"`
	SyncIntervalSeconds             int             `json:"sync_interval_seconds"`
	PushDebounceSeconds             int             `json:"push_debounce_seconds"`
	PullBatchSize                   int             `json:"pull_batch_size"`
	HeartbeatSyncMinIntervalSeconds int             `json:"heartbeat_sync_min_interval_seconds"`
	HistoryRetentionDays            float64         `json:"history_retention_days"`
	HistoryMaxRetainedOps           int             `json:"history_max_retained_ops"`
	HistoryPruneIntervalMinutes     int             `json:"history_prune_interval_minutes"`
	HistoryPruneBatchSize           int             `json:"history_prune_batch_size"`
	Nodes                           []haNodeRequest `json:"nodes,omitempty"`
	Peers                           []haPeerRequest `json:"peers"`
}

type haNodeRequest struct {
	FQDN         string `json:"fqdn"`
	NodeID       string `json:"node_id"`
	NodeName     string `json:"node_name"`
	AdvertiseURL string `json:"advertise_url"`
	PublicKeyPEM string `json:"public_key,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type haPeerRequest struct {
	NodeID       string `json:"node_id"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	PublicKeyPEM string `json:"public_key,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type haKeyProvider interface {
	NodeID() string
	PublicKeyPEM() string
}

type haPeerKeyProvider interface {
	haKeyProvider
	ClusterSecret() string
	SignPeerRequest(req *http.Request) error
	AuthenticatePeerRequest(r *http.Request) error
}

type haPublicKeyView struct {
	NodeID         string `json:"node_id"`
	NodeName       string `json:"node_name,omitempty"`
	SelfFQDN       string `json:"self_fqdn,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	PublicKeyPEM   string `json:"public_key,omitempty"`
}

type haNodePublicKeyView struct {
	Enabled        bool   `json:"enabled"`
	NodeID         string `json:"node_id,omitempty"`
	NodeName       string `json:"node_name,omitempty"`
	FQDN           string `json:"fqdn,omitempty"`
	AdvertiseURL   string `json:"advertise_url,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	PublicKeyPEM   string `json:"public_key,omitempty"`
	Source         string `json:"source,omitempty"`
	Reachable      bool   `json:"reachable"`
	Error          string `json:"error,omitempty"`
}

type haPublicKeyCollectionView struct {
	Nodes          []haNodePublicKeyView `json:"nodes"`
	Total          int                   `json:"total"`
	CollectedCount int                   `json:"collected_count"`
	MissingCount   int                   `json:"missing_count"`
}

func GetHAConfigHandler(svc *ha.ConfigService, keySvc haKeyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusInternalServerError, "HA_CONFIG_UNAVAILABLE", "HA config service is unavailable")
			return
		}
		cfg, err := svc.CurrentConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_CONFIG_FAILED", err.Error())
			return
		}
		resp := applySelfHAKeyMaterial(fromHAConfig(cfg), keySvc)
		resp.SuggestedSelfFQDN = requestHAFQDNHint(r)
		writeJSON(w, http.StatusOK, resp)
	}
}

func UpdateHAConfigHandler(svc *ha.ConfigService, keySvc haKeyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusInternalServerError, "HA_CONFIG_UNAVAILABLE", "HA config service is unavailable")
			return
		}
		var req haConfigRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		req = expandLegacyHAPeersToNodes(req)
		req = applySelfHAKeyMaterial(req, keySvc)
		saved, err := svc.SaveConfig(r.Context(), req.toConfig())
		if err != nil {
			writeError(w, http.StatusBadRequest, "HA_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		resp := fromHAConfig(saved)
		resp.SuggestedSelfFQDN = requestHAFQDNHint(r)
		writeJSON(w, http.StatusOK, resp)
	}
}

func expandLegacyHAPeersToNodes(req haConfigRequest) haConfigRequest {
	if len(req.Nodes) > 0 || strings.TrimSpace(req.SelfFQDN) == "" || len(req.Peers) == 0 {
		return req
	}
	nodes := make([]haNodeRequest, 0, len(req.Peers)+1)
	nodes = append(nodes, haNodeRequest{
		FQDN:         req.SelfFQDN,
		NodeID:       req.NodeID,
		NodeName:     req.NodeName,
		AdvertiseURL: req.AdvertiseURL,
		Enabled:      true,
	})
	for _, peer := range req.Peers {
		if !peer.Enabled && strings.TrimSpace(peer.NodeID) == "" && strings.TrimSpace(peer.Name) == "" && strings.TrimSpace(peer.BaseURL) == "" && strings.TrimSpace(peer.PublicKeyPEM) == "" {
			continue
		}
		nodes = append(nodes, haNodeRequest{
			FQDN:         peer.BaseURL,
			NodeID:       peer.NodeID,
			NodeName:     peer.Name,
			AdvertiseURL: peer.BaseURL,
			PublicKeyPEM: peer.PublicKeyPEM,
			Enabled:      peer.Enabled,
		})
	}
	req.Nodes = nodes
	return req
}

func HAKeyMaterialHandler(cfgSvc *ha.ConfigService, keySvc haKeyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfgSvc == nil || keySvc == nil {
			writeError(w, http.StatusNotImplemented, "HA_KEY_UNAVAILABLE", "HA key service is unavailable")
			return
		}
		view, err := buildSelfHAPublicKeyView(r.Context(), cfgSvc, keySvc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_KEY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func HAInternalKeyMaterialHandler(cfgSvc *ha.ConfigService, keySvc haKeyProvider, authSvc haPeerKeyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfgSvc == nil || keySvc == nil || authSvc == nil {
			writeError(w, http.StatusNotImplemented, "HA_KEY_UNAVAILABLE", "HA key service is unavailable")
			return
		}
		if err := authSvc.AuthenticatePeerRequest(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		view, err := buildSelfHAPublicKeyView(r.Context(), cfgSvc, keySvc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_KEY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func HACollectedPublicKeysHandler(cfgSvc *ha.ConfigService, keySvc haKeyProvider, peerSvc haPeerKeyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfgSvc == nil || keySvc == nil {
			writeError(w, http.StatusNotImplemented, "HA_KEY_UNAVAILABLE", "HA key service is unavailable")
			return
		}
		cfg, err := cfgSvc.CurrentConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_KEY_FAILED", err.Error())
			return
		}
		collection := collectHANodePublicKeys(r.Context(), cfg, keySvc, peerSvc)
		writeJSON(w, http.StatusOK, collection)
	}
}

func buildSelfHAPublicKeyView(ctx context.Context, cfgSvc *ha.ConfigService, keySvc haKeyProvider) (haPublicKeyView, error) {
	if cfgSvc == nil || keySvc == nil {
		return haPublicKeyView{}, fmt.Errorf("ha key service is unavailable")
	}
	cfg, err := cfgSvc.CurrentConfig(ctx)
	if err != nil {
		return haPublicKeyView{}, err
	}
	view := haPublicKeyView{
		NodeID:         strings.TrimSpace(cfg.NodeID),
		NodeName:       strings.TrimSpace(cfg.NodeName),
		SelfFQDN:       strings.TrimSpace(cfg.SelfFQDN),
		PrivateKeyPath: strings.TrimSpace(cfg.PrivateKeyPath),
		PublicKeyPEM:   strings.TrimSpace(keySvc.PublicKeyPEM()),
	}
	if view.NodeID == "" {
		view.NodeID = strings.TrimSpace(keySvc.NodeID())
	}
	if view.PublicKeyPEM == "" {
		for _, node := range cfg.Nodes {
			if strings.TrimSpace(node.NodeID) == view.NodeID && strings.TrimSpace(node.PublicKeyPEM) != "" {
				view.PublicKeyPEM = strings.TrimSpace(node.PublicKeyPEM)
				break
			}
		}
	}
	return view, nil
}

func collectHANodePublicKeys(ctx context.Context, cfg config.HAConfig, keySvc haKeyProvider, peerSvc haPeerKeyProvider) haPublicKeyCollectionView {
	nodes := make([]haNodePublicKeyView, 0, len(cfg.Nodes))
	selfView := haPublicKeyView{
		NodeID:         strings.TrimSpace(cfg.NodeID),
		NodeName:       strings.TrimSpace(cfg.NodeName),
		SelfFQDN:       strings.TrimSpace(cfg.SelfFQDN),
		PrivateKeyPath: strings.TrimSpace(cfg.PrivateKeyPath),
	}
	if keySvc != nil {
		selfView.PublicKeyPEM = strings.TrimSpace(keySvc.PublicKeyPEM())
		if selfView.NodeID == "" {
			selfView.NodeID = strings.TrimSpace(keySvc.NodeID())
		}
	}
	for _, node := range cfg.Nodes {
		item := haNodePublicKeyView{
			Enabled:      node.Enabled,
			NodeID:       strings.TrimSpace(node.NodeID),
			NodeName:     strings.TrimSpace(node.NodeName),
			FQDN:         config.NormalizeHAFQDN(node.FQDN),
			AdvertiseURL: strings.TrimRight(strings.TrimSpace(node.AdvertiseURL), "/"),
			PublicKeyPEM: strings.TrimSpace(node.PublicKeyPEM),
			Source:       "config",
		}
		if isSelfHANode(item, cfg, selfView) {
			item.NodeID = firstNonEmpty(item.NodeID, selfView.NodeID)
			item.NodeName = firstNonEmpty(item.NodeName, selfView.NodeName)
			item.FQDN = firstNonEmpty(item.FQDN, config.NormalizeHAFQDN(selfView.SelfFQDN))
			item.PrivateKeyPath = firstNonEmpty(item.PrivateKeyPath, selfView.PrivateKeyPath)
			item.PublicKeyPEM = firstNonEmpty(selfView.PublicKeyPEM, item.PublicKeyPEM)
			item.Source = "local"
			item.Reachable = true
		} else if item.AdvertiseURL != "" && peerSvc != nil {
			remote, err := fetchRemoteHAPublicKey(ctx, item.AdvertiseURL, peerSvc)
			if err == nil {
				item.NodeID = firstNonEmpty(remote.NodeID, item.NodeID)
				item.NodeName = firstNonEmpty(remote.NodeName, item.NodeName)
				item.FQDN = firstNonEmpty(config.NormalizeHAFQDN(remote.SelfFQDN), item.FQDN)
				item.PrivateKeyPath = firstNonEmpty(remote.PrivateKeyPath, item.PrivateKeyPath)
				item.PublicKeyPEM = firstNonEmpty(remote.PublicKeyPEM, item.PublicKeyPEM)
				item.Source = "remote"
				item.Reachable = true
			} else {
				item.Error = err.Error()
			}
		}
		nodes = append(nodes, item)
	}
	collection := haPublicKeyCollectionView{Nodes: nodes, Total: len(nodes)}
	for _, node := range nodes {
		if strings.TrimSpace(node.PublicKeyPEM) != "" {
			collection.CollectedCount++
		} else {
			collection.MissingCount++
		}
	}
	return collection
}

func fetchRemoteHAPublicKey(ctx context.Context, baseURL string, peerSvc haPeerKeyProvider) (haPublicKeyView, error) {
	if peerSvc == nil {
		return haPublicKeyView{}, fmt.Errorf("ha peer auth is unavailable")
	}
	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/internal/ha/public-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return haPublicKeyView{}, err
	}
	if secret := strings.TrimSpace(peerSvc.ClusterSecret()); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if err := peerSvc.SignPeerRequest(req); err != nil {
		return haPublicKeyView{}, err
	}
	resp, err := haPublicKeyHTTPClient.Do(req)
	if err != nil {
		return haPublicKeyView{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return haPublicKeyView{}, fmt.Errorf("remote status %d: %s", resp.StatusCode, msg)
	}
	var view haPublicKeyView
	if err := json.NewDecoder(io.LimitReader(resp.Body, haPublicKeyResponseBodyLimit)).Decode(&view); err != nil {
		return haPublicKeyView{}, err
	}
	return view, nil
}

func isSelfHANode(node haNodePublicKeyView, cfg config.HAConfig, self haPublicKeyView) bool {
	nodeFQDN := config.NormalizeHAFQDN(node.FQDN)
	selfFQDN := config.NormalizeHAFQDN(firstNonEmpty(self.SelfFQDN, cfg.SelfFQDN))
	if nodeFQDN != "" && selfFQDN != "" && nodeFQDN == selfFQDN {
		return true
	}
	nodeID := strings.TrimSpace(node.NodeID)
	selfNodeID := strings.TrimSpace(firstNonEmpty(self.NodeID, cfg.NodeID))
	return nodeID != "" && selfNodeID != "" && nodeID == selfNodeID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func applySelfHAKeyMaterial(req haConfigRequest, keySvc haKeyProvider) haConfigRequest {
	if keySvc == nil {
		return req
	}
	publicKey := strings.TrimSpace(keySvc.PublicKeyPEM())
	if publicKey == "" {
		return req
	}
	selfFQDN := config.NormalizeHAFQDN(req.SelfFQDN)
	selfNodeID := strings.TrimSpace(req.NodeID)
	providerNodeID := strings.TrimSpace(keySvc.NodeID())

	for i := range req.Nodes {
		node := &req.Nodes[i]
		if !node.Enabled {
			continue
		}
		nodeFQDN := config.NormalizeHAFQDN(node.FQDN)
		nodeID := strings.TrimSpace(node.NodeID)
		matchesSelf := selfFQDN != "" && nodeFQDN == selfFQDN
		matchesNodeID := selfNodeID != "" && nodeID == selfNodeID
		matchesProviderNodeID := providerNodeID != "" && nodeID == providerNodeID
		if !matchesSelf && !matchesNodeID && !matchesProviderNodeID {
			continue
		}
		if strings.TrimSpace(node.PublicKeyPEM) == "" {
			node.PublicKeyPEM = publicKey
		}
		break
	}
	return req
}

func (r haConfigRequest) toConfig() config.HAConfig {
	nodes := make([]config.HANodeConfig, 0, len(r.Nodes))
	for _, node := range r.Nodes {
		nodes = append(nodes, config.HANodeConfig{
			FQDN:         node.FQDN,
			NodeID:       node.NodeID,
			NodeName:     node.NodeName,
			AdvertiseURL: node.AdvertiseURL,
			PublicKeyPEM: node.PublicKeyPEM,
			Enabled:      node.Enabled,
		})
	}
	peers := make([]config.HAPeerConfig, 0, len(r.Peers))
	for _, peer := range r.Peers {
		peers = append(peers, config.HAPeerConfig{
			NodeID:       peer.NodeID,
			Name:         peer.Name,
			BaseURL:      peer.BaseURL,
			PublicKeyPEM: peer.PublicKeyPEM,
			Enabled:      peer.Enabled,
		})
	}
	return config.HAConfig{
		Enabled:                         r.Enabled,
		SelfFQDN:                        r.SelfFQDN,
		PrivateKeyPath:                  r.PrivateKeyPath,
		NodeID:                          r.NodeID,
		NodeName:                        r.NodeName,
		AdvertiseURL:                    r.AdvertiseURL,
		ClusterSecret:                   r.ClusterSecret,
		SyncIntervalSeconds:             r.SyncIntervalSeconds,
		PushDebounceSeconds:             r.PushDebounceSeconds,
		PullBatchSize:                   r.PullBatchSize,
		HeartbeatSyncMinIntervalSeconds: r.HeartbeatSyncMinIntervalSeconds,
		HistoryRetentionDays:            r.HistoryRetentionDays,
		HistoryMaxRetainedOps:           r.HistoryMaxRetainedOps,
		HistoryPruneIntervalMinutes:     r.HistoryPruneIntervalMinutes,
		HistoryPruneBatchSize:           r.HistoryPruneBatchSize,
		Nodes:                           nodes,
		Peers:                           peers,
	}
}

func fromHAConfig(cfg config.HAConfig) haConfigRequest {
	nodes := make([]haNodeRequest, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		nodes = append(nodes, haNodeRequest{
			FQDN:         node.FQDN,
			NodeID:       node.NodeID,
			NodeName:     node.NodeName,
			AdvertiseURL: node.AdvertiseURL,
			PublicKeyPEM: node.PublicKeyPEM,
			Enabled:      node.Enabled,
		})
	}
	peers := make([]haPeerRequest, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peers = append(peers, haPeerRequest{
			NodeID:       peer.NodeID,
			Name:         peer.Name,
			BaseURL:      peer.BaseURL,
			PublicKeyPEM: peer.PublicKeyPEM,
			Enabled:      peer.Enabled,
		})
	}
	return haConfigRequest{
		Enabled:                         cfg.Enabled,
		SelfFQDN:                        cfg.SelfFQDN,
		PrivateKeyPath:                  cfg.PrivateKeyPath,
		NodeID:                          cfg.NodeID,
		NodeName:                        cfg.NodeName,
		AdvertiseURL:                    cfg.AdvertiseURL,
		ClusterSecret:                   cfg.ClusterSecret,
		SyncIntervalSeconds:             cfg.SyncIntervalSeconds,
		PushDebounceSeconds:             cfg.PushDebounceSeconds,
		PullBatchSize:                   cfg.PullBatchSize,
		HeartbeatSyncMinIntervalSeconds: cfg.HeartbeatSyncMinIntervalSeconds,
		HistoryRetentionDays:            cfg.HistoryRetentionDays,
		HistoryMaxRetainedOps:           cfg.HistoryMaxRetainedOps,
		HistoryPruneIntervalMinutes:     cfg.HistoryPruneIntervalMinutes,
		HistoryPruneBatchSize:           cfg.HistoryPruneBatchSize,
		Nodes:                           nodes,
		Peers:                           peers,
	}
}
