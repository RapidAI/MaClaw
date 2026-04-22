package httpapi

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

func TestHAConfigEndpointsRoundTrip(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	payload := map[string]any{
		"enabled":                             true,
		"node_id":                             "hc-1",
		"node_name":                           "HubCenter 1",
		"advertise_url":                       "https://hubs.mypapers.top",
		"cluster_secret":                      "shared-secret",
		"sync_interval_seconds":               4,
		"pull_batch_size":                     180,
		"heartbeat_sync_min_interval_seconds": 8,
		"peers": []map[string]any{
			{"enabled": true, "node_id": "hc-2", "name": "HubCenter 2", "base_url": "https://hubs.maclaw.top"},
		},
	}
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", payload, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", resp.Code, resp.Body.String())
	}
	read := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/ha/config", nil, token)
	if read.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", read.Code, read.Body.String())
	}
	body := decodeJSON(t, read.Body.Bytes())
	if body["node_id"] != "hc-1" {
		t.Fatalf("node_id = %v", body["node_id"])
	}
}

func TestHAConfigEndpointRejectsInvalidEnabledConfig(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{"enabled": true, "node_id": "hc-1"}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHAConfigEndpointRejectsSelfPeer(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{
		"enabled":        true,
		"node_id":        "hc-1",
		"advertise_url":  "https://hubs.mypapers.top",
		"cluster_secret": "shared-secret",
		"peers": []map[string]any{{
			"enabled":  true,
			"node_id":  "hc-1",
			"base_url": "https://hubs.maclaw.top",
		}},
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHAConfigEndpointRejectsDuplicatePeerURL(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{
		"enabled":        true,
		"node_id":        "hc-1",
		"advertise_url":  "https://hubs.mypapers.top",
		"cluster_secret": "shared-secret",
		"peers": []map[string]any{
			{"enabled": true, "node_id": "hc-2", "base_url": "https://hubs.maclaw.top"},
			{"enabled": true, "node_id": "hc-3", "base_url": "https://hubs.maclaw.top"},
		},
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}
func TestHAConfigEndpointRejectsDuplicatePeerID(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{
		"enabled":        true,
		"node_id":        "hc-1",
		"advertise_url":  "https://hubs.mypapers.top",
		"cluster_secret": "shared-secret",
		"peers": []map[string]any{
			{"enabled": true, "node_id": "hc-2", "base_url": "https://hubs.maclaw.top"},
			{"enabled": true, "node_id": "hc-2", "base_url": "https://hubs2.maclaw.top"},
		},
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}
