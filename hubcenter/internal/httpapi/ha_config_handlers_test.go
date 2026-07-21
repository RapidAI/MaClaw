package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

func TestHAConfigEndpointsRoundTrip(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	payload := map[string]any{
		"enabled":                             true,
		"self_fqdn":                           "hubs.maclaw.top",
		"cluster_secret":                      "shared-secret",
		"sync_interval_seconds":               4,
		"push_debounce_seconds":               7,
		"pull_batch_size":                     180,
		"heartbeat_sync_min_interval_seconds": 8,
		"nodes": []map[string]any{
			{"enabled": true, "fqdn": "hubs.mypapers.top", "node_id": "hc-1", "node_name": "HubCenter 1", "advertise_url": "https://hubs.mypapers.top"},
			{"enabled": true, "fqdn": "hubs.maclaw.top", "node_id": "hc-2", "node_name": "HubCenter 2", "advertise_url": "https://hubs.maclaw.top"},
			{"enabled": true, "fqdn": "hubs2.maclaw.top", "node_id": "hc-3", "node_name": "HubCenter 3", "advertise_url": "https://hubs2.maclaw.top"},
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
	if body["self_fqdn"] != "hubs.maclaw.top" {
		t.Fatalf("self_fqdn = %v", body["self_fqdn"])
	}
	if body["node_id"] != "hc-2" {
		t.Fatalf("node_id = %v", body["node_id"])
	}
	if body["push_debounce_seconds"] != float64(7) {
		t.Fatalf("push_debounce_seconds = %v", body["push_debounce_seconds"])
	}
	peers, _ := body["peers"].([]any)
	if len(peers) != 2 {
		t.Fatalf("peer count = %d", len(peers))
	}
}

func TestHAConfigEndpointExpandsLegacyPeersWhenSelfFQDNIsPosted(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	payload := map[string]any{
		"enabled":                             true,
		"self_fqdn":                           "hubs.maclaw.top",
		"node_id":                             "hc-2",
		"node_name":                           "HubCenter 2",
		"advertise_url":                       "https://hubs.maclaw.top",
		"cluster_secret":                      "shared-secret",
		"sync_interval_seconds":               4,
		"push_debounce_seconds":               7,
		"pull_batch_size":                     180,
		"heartbeat_sync_min_interval_seconds": 8,
		"peers": []map[string]any{
			{"enabled": true, "node_id": "hc-1", "name": "HubCenter 1", "base_url": "https://hubs.mypapers.top"},
			{"enabled": true, "node_id": "hc-3", "name": "HubCenter 3", "base_url": "https://hubs2.maclaw.top"},
		},
	}
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", payload, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d body=%s", len(nodes), resp.Body.String())
	}
	if body["node_id"] != "hc-2" {
		t.Fatalf("node_id = %v", body["node_id"])
	}
	peers, _ := body["peers"].([]any)
	if len(peers) != 2 {
		t.Fatalf("peers = %d body=%s", len(peers), resp.Body.String())
	}
}

func TestHAConfigSaveAutoBackfillsSelfNodePublicKey(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	keySvc := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", nil)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keySvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: privateKey})
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc, keySvc)

	payload := map[string]any{
		"enabled":        true,
		"self_fqdn":      "hubs.maclaw.top",
		"cluster_secret": "shared-secret",
		"nodes": []map[string]any{
			{"enabled": true, "fqdn": "hubs.mypapers.top", "node_id": "hc-1", "node_name": "HubCenter 1", "advertise_url": "https://hubs.mypapers.top"},
			{"enabled": true, "fqdn": "hubs.maclaw.top", "node_id": "hc-2", "node_name": "HubCenter 2", "advertise_url": "https://hubs.maclaw.top"},
		},
	}
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", payload, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d", len(nodes))
	}
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node["node_id"] == "hc-2" {
			pub, _ := node["public_key"].(string)
			if pub == "" {
				t.Fatalf("self node public_key not backfilled")
			}
			return
		}
	}
	t.Fatalf("self node not found in response")
}

func TestHAConfigSavePreservesExplicitSelfNodePublicKey(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	keySvc := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", nil)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keySvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: privateKey})
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc, keySvc)

	const manualPEM = "-----BEGIN PUBLIC KEY-----\nMANUAL\n-----END PUBLIC KEY-----"
	payload := map[string]any{
		"enabled":        true,
		"self_fqdn":      "hubs.maclaw.top",
		"cluster_secret": "shared-secret",
		"nodes": []map[string]any{
			{"enabled": true, "fqdn": "hubs.mypapers.top", "node_id": "hc-1", "node_name": "HubCenter 1", "advertise_url": "https://hubs.mypapers.top"},
			{"enabled": true, "fqdn": "hubs.maclaw.top", "node_id": "hc-2", "node_name": "HubCenter 2", "advertise_url": "https://hubs.maclaw.top", "public_key": manualPEM},
		},
	}
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", payload, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	nodes, _ := body["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node["node_id"] == "hc-2" {
			pub, _ := node["public_key"].(string)
			if pub != manualPEM {
				t.Fatalf("public_key = %q, want manual value", pub)
			}
			return
		}
	}
	t.Fatalf("self node not found in response")
}

func TestHAConfigGetAutoBackfillsSelfNodePublicKey(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	keySvc := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", nil)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keySvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: privateKey})
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc, keySvc)

	_, err = haCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:       true,
		SelfFQDN:      "hubs.maclaw.top",
		ClusterSecret: "shared-secret",
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: "https://hubs.maclaw.top"},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/ha/config", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	nodes, _ := body["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node["node_id"] == "hc-2" {
			pub, _ := node["public_key"].(string)
			if pub == "" {
				t.Fatalf("self node public_key not backfilled on GET")
			}
			return
		}
	}
	t.Fatalf("self node not found in response")
}
func TestHAConfigGetIncludesSuggestedFQDNFromHost(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequestWithHost(t, svc.handler, http.MethodGet, "/api/admin/ha/config", nil, token, "https://hubs2.maclaw.top/admin")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	if body["suggested_self_fqdn"] != "hubs2.maclaw.top" {
		t.Fatalf("suggested_self_fqdn = %v", body["suggested_self_fqdn"])
	}
}

func TestHAConfigEndpointRejectsInvalidEnabledConfig(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{"enabled": true, "self_fqdn": "hc-1.example.com"}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHAConfigEndpointRejectsMissingSelfFQDNMatch(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/ha/config", map[string]any{
		"enabled":        true,
		"self_fqdn":      "hubs4.maclaw.top",
		"cluster_secret": "shared-secret",
		"nodes": []map[string]any{{
			"enabled":       true,
			"fqdn":          "hubs.maclaw.top",
			"node_id":       "hc-1",
			"advertise_url": "https://hubs.maclaw.top",
		}},
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHAAdminPublicKeyEndpointReturnsLiveKeyMaterial(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	keySvc := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", nil)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keySvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: privateKey, PrivateKeyPath: "/data/ha_keys/hc-2.pem"})
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc, keySvc)

	_, err = haCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:        true,
		SelfFQDN:       "hubs.maclaw.top",
		PrivateKeyPath: "/data/ha_keys/hc-2.pem",
		ClusterSecret:  "shared-secret",
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: "https://hubs.maclaw.top"},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/ha/public-key", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	if body["node_id"] != "hc-2" {
		t.Fatalf("node_id = %v", body["node_id"])
	}
	if body["self_fqdn"] != "hubs.maclaw.top" {
		t.Fatalf("self_fqdn = %v", body["self_fqdn"])
	}
	if body["private_key_path"] != "/data/ha_keys/hc-2.pem" {
		t.Fatalf("private_key_path = %v", body["private_key_path"])
	}
	pub, _ := body["public_key"].(string)
	if pub == "" {
		t.Fatalf("public_key is empty")
	}
}

func TestHAInternalPublicKeyEndpointAcceptsSignedPeerRequest(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	sender := ha.NewService("hc-1", "HubCenter 1", "https://hubs.mypapers.top", "shared-secret", nil)
	senderKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() sender error = %v", err)
	}
	sender.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: senderKey})
	receiver := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", []ha.StaticPeer{{NodeID: "hc-1", NodeName: "HubCenter 1", BaseURL: "https://hubs.mypapers.top", PublicKeyPEM: sender.PublicKeyPEM()}})
	receiverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() receiver error = %v", err)
	}
	receiver.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: receiverKey, PrivateKeyPath: "/data/ha_keys/hc-2.pem"})
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc, receiver)

	_, err = haCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:        true,
		SelfFQDN:       "hubs.maclaw.top",
		PrivateKeyPath: "/data/ha_keys/hc-2.pem",
		ClusterSecret:  "shared-secret",
		NodeID:         "hc-2",
		NodeName:       "HubCenter 2",
		Peers: []config.HAPeerConfig{
			{Enabled: true, NodeID: "hc-1", Name: "HubCenter 1", BaseURL: "https://hubs.mypapers.top", PublicKeyPEM: sender.PublicKeyPEM()},
		},
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: "https://hubs.maclaw.top"},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/public-key", nil)
	req.Header.Set("Authorization", "Bearer shared-secret")
	if err := sender.SignPeerRequest(req); err != nil {
		t.Fatalf("SignPeerRequest() error = %v", err)
	}
	resp := httptest.NewRecorder()
	svc.handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	if body["node_id"] != "hc-2" {
		t.Fatalf("node_id = %v", body["node_id"])
	}
	if _, ok := body["public_key"].(string); !ok || body["public_key"] == "" {
		t.Fatalf("public_key missing: %v", body["public_key"])
	}
}

func TestHAAdminPublicKeysEndpointCollectsRemoteNodeKeys(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	localCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	localSvc := ha.NewService("hc-1", "HubCenter 1", "https://hubs.mypapers.top", "shared-secret", nil)
	localKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() local error = %v", err)
	}
	localSvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: localKey, PrivateKeyPath: "/data/ha_keys/hc-1.pem"})

	remoteCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, nil)
	remoteSvc := ha.NewService("hc-2", "HubCenter 2", "https://hubs.maclaw.top", "shared-secret", []ha.StaticPeer{{NodeID: "hc-1", NodeName: "HubCenter 1", BaseURL: "https://hubs.mypapers.top", PublicKeyPEM: localSvc.PublicKeyPEM()}})
	remoteKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() remote error = %v", err)
	}
	remoteSvc.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: remoteKey, PrivateKeyPath: "/data/ha_keys/hc-2.pem"})
	_, err = remoteCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:        true,
		SelfFQDN:       "hubs.maclaw.top",
		PrivateKeyPath: "/data/ha_keys/hc-2.pem",
		ClusterSecret:  "shared-secret",
		NodeID:         "hc-2",
		NodeName:       "HubCenter 2",
		Peers: []config.HAPeerConfig{
			{Enabled: true, NodeID: "hc-1", Name: "HubCenter 1", BaseURL: "https://hubs.mypapers.top", PublicKeyPEM: localSvc.PublicKeyPEM()},
		},
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: "https://hubs.maclaw.top"},
		},
	})
	if err != nil {
		t.Fatalf("remote SaveConfig() error = %v", err)
	}
	remoteServer := httptest.NewServer(HAInternalKeyMaterialHandler(remoteCfgSvc, remoteSvc, remoteSvc))
	defer remoteServer.Close()

	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, localCfgSvc, localSvc)
	_, err = localCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:        true,
		SelfFQDN:       "hubs.mypapers.top",
		PrivateKeyPath: "/data/ha_keys/hc-1.pem",
		ClusterSecret:  "shared-secret",
		NodeID:         "hc-1",
		NodeName:       "HubCenter 1",
		Peers: []config.HAPeerConfig{
			{Enabled: true, NodeID: "hc-2", Name: "HubCenter 2", BaseURL: remoteServer.URL, PublicKeyPEM: remoteSvc.PublicKeyPEM()},
		},
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: remoteServer.URL},
		},
	})
	if err != nil {
		t.Fatalf("local SaveConfig() error = %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/ha/public-keys", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Nodes          []map[string]any `json:"nodes"`
		Total          int              `json:"total"`
		CollectedCount int              `json:"collected_count"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 2 {
		t.Fatalf("total = %d payload=%s", payload.Total, resp.Body.String())
	}
	if payload.CollectedCount != 2 {
		t.Fatalf("collected_count = %d payload=%s", payload.CollectedCount, resp.Body.String())
	}
	remoteFound := false
	for _, node := range payload.Nodes {
		if node["node_id"] == "hc-2" {
			remoteFound = true
			if node["source"] != "remote" {
				t.Fatalf("remote source = %v", node["source"])
			}
			if node["public_key"] == "" {
				t.Fatalf("remote public_key empty: %v", node)
			}
		}
	}
	if !remoteFound {
		t.Fatalf("remote node not found: %s", resp.Body.String())
	}
}

func TestClientHubCentersEndpointReturnsConfiguredNodeCatalog(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	haCfgSvc := ha.NewConfigService(config.HAConfig{SyncIntervalSeconds: 3, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}, svc.store.System)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, haCfgSvc)

	_, err := haCfgSvc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:       true,
		SelfFQDN:      "hubs.maclaw.top",
		ClusterSecret: "shared-secret",
		Nodes: []config.HANodeConfig{
			{Enabled: true, FQDN: "hubs.mypapers.top", NodeID: "hc-1", NodeName: "HubCenter 1", AdvertiseURL: "https://hubs.mypapers.top"},
			{Enabled: true, FQDN: "hubs.maclaw.top", NodeID: "hc-2", NodeName: "HubCenter 2", AdvertiseURL: "https://hubs.maclaw.top"},
			{Enabled: true, FQDN: "hubs2.maclaw.top", NodeID: "hc-3", NodeName: "HubCenter 3", AdvertiseURL: "https://hubs2.maclaw.top"},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/client/hubcenters", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		URLs  []string         `json:"urls"`
		Nodes []map[string]any `json:"nodes"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Nodes) != 3 {
		t.Fatalf("nodes = %d", len(payload.Nodes))
	}
	if len(payload.URLs) != 3 {
		t.Fatalf("urls = %d, payload=%s", len(payload.URLs), resp.Body.String())
	}
	if payload.Count != 3 {
		t.Fatalf("count = %d", payload.Count)
	}
	if payload.URLs[0] != "https://hubs.mypapers.top" || payload.URLs[1] != "https://hubs.maclaw.top" || payload.URLs[2] != "https://hubs2.maclaw.top" {
		t.Fatalf("unexpected urls = %v", payload.URLs)
	}
	currentCount := 0
	for _, node := range payload.Nodes {
		if current, _ := node["current"].(bool); current {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("current node count = %d", currentCount)
	}
}

func TestBuildClientHubCentersViewPrefersLegacyPeerPublicURL(t *testing.T) {
	view := buildClientHubCentersView(config.HAConfig{
		Enabled:      true,
		NodeID:       "hc-1",
		NodeName:     "HubCenter 1",
		AdvertiseURL: "http://10.0.0.1:9388",
		Peers: []config.HAPeerConfig{{
			Enabled:   true,
			NodeID:    "hc-2",
			Name:      "HubCenter 2",
			BaseURL:   "http://10.0.0.2:9388",
			PublicURL: "https://hubs-2.example.com",
		}},
	})
	if len(view.URLs) != 2 || view.URLs[1] != "https://hubs-2.example.com" {
		t.Fatalf("legacy URLs = %v", view.URLs)
	}
}
