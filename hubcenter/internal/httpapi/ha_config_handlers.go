package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

type haConfigRequest struct {
	Enabled                         bool            `json:"enabled"`
	NodeID                          string          `json:"node_id"`
	NodeName                        string          `json:"node_name"`
	AdvertiseURL                    string          `json:"advertise_url"`
	ClusterSecret                   string          `json:"cluster_secret"`
	SyncIntervalSeconds             int             `json:"sync_interval_seconds"`
	PullBatchSize                   int             `json:"pull_batch_size"`
	HeartbeatSyncMinIntervalSeconds int             `json:"heartbeat_sync_min_interval_seconds"`
	Peers                           []haPeerRequest `json:"peers"`
}

type haPeerRequest struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
}

func GetHAConfigHandler(svc *ha.ConfigService) http.HandlerFunc {
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
		writeJSON(w, http.StatusOK, fromHAConfig(cfg))
	}
}

func UpdateHAConfigHandler(svc *ha.ConfigService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusInternalServerError, "HA_CONFIG_UNAVAILABLE", "HA config service is unavailable")
			return
		}
		var req haConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		saved, err := svc.SaveConfig(r.Context(), req.toConfig())
		if err != nil {
			writeError(w, http.StatusBadRequest, "HA_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, fromHAConfig(saved))
	}
}

func (r haConfigRequest) toConfig() config.HAConfig {
	peers := make([]config.HAPeerConfig, 0, len(r.Peers))
	for _, peer := range r.Peers {
		peers = append(peers, config.HAPeerConfig{
			NodeID:  peer.NodeID,
			Name:    peer.Name,
			BaseURL: peer.BaseURL,
			Enabled: peer.Enabled,
		})
	}
	return config.HAConfig{
		Enabled:                         r.Enabled,
		NodeID:                          r.NodeID,
		NodeName:                        r.NodeName,
		AdvertiseURL:                    r.AdvertiseURL,
		ClusterSecret:                   r.ClusterSecret,
		SyncIntervalSeconds:             r.SyncIntervalSeconds,
		PullBatchSize:                   r.PullBatchSize,
		HeartbeatSyncMinIntervalSeconds: r.HeartbeatSyncMinIntervalSeconds,
		Peers:                           peers,
	}
}
func fromHAConfig(cfg config.HAConfig) haConfigRequest {
	peers := make([]haPeerRequest, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peers = append(peers, haPeerRequest{
			NodeID:  peer.NodeID,
			Name:    peer.Name,
			BaseURL: peer.BaseURL,
			Enabled: peer.Enabled,
		})
	}
	return haConfigRequest{
		Enabled:                         cfg.Enabled,
		NodeID:                          cfg.NodeID,
		NodeName:                        cfg.NodeName,
		AdvertiseURL:                    cfg.AdvertiseURL,
		ClusterSecret:                   cfg.ClusterSecret,
		SyncIntervalSeconds:             cfg.SyncIntervalSeconds,
		PullBatchSize:                   cfg.PullBatchSize,
		HeartbeatSyncMinIntervalSeconds: cfg.HeartbeatSyncMinIntervalSeconds,
		Peers:                           peers,
	}
}
