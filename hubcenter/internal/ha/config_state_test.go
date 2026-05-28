package ha

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func newHAConfigServiceTestStore(t *testing.T) (*ConfigService, *sqlite.Provider) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ha-config.db")
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	fallback := config.HAConfig{Enabled: false, SyncIntervalSeconds: 3, PushDebounceSeconds: 4, PullBatchSize: 200, HeartbeatSyncMinIntervalSeconds: 10}
	return NewConfigService(fallback, st.System), provider
}

func TestConfigServiceCurrentConfigFallsBackToDefaults(t *testing.T) {
	svc, provider := newHAConfigServiceTestStore(t)
	defer provider.Close()
	cfg, err := svc.CurrentConfig(context.Background())
	if err != nil {
		t.Fatalf("CurrentConfig() error = %v", err)
	}
	if cfg.SyncIntervalSeconds != 3 || cfg.PushDebounceSeconds != 4 || cfg.PullBatchSize != 200 || cfg.HeartbeatSyncMinIntervalSeconds != 10 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestConfigServiceSaveAndLoad(t *testing.T) {
	svc, provider := newHAConfigServiceTestStore(t)
	defer provider.Close()
	want := config.HAConfig{
		Enabled:                         true,
		NodeID:                          "hc-1",
		NodeName:                        "HubCenter 1",
		AdvertiseURL:                    "https://hubs.mypapers.top",
		ClusterSecret:                   "shared-secret",
		SyncIntervalSeconds:             5,
		PushDebounceSeconds:             6,
		PullBatchSize:                   300,
		HeartbeatSyncMinIntervalSeconds: 9,
		Peers: []config.HAPeerConfig{
			{Enabled: true, NodeID: "hc-2", Name: "HubCenter 2", BaseURL: "https://hubs.maclaw.top"},
			{Enabled: true, NodeID: "hc-3", Name: "HubCenter 3", BaseURL: "https://hubs2.maclaw.top"},
		},
	}
	if _, err := svc.SaveConfig(context.Background(), want); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	got, err := svc.CurrentConfig(context.Background())
	if err != nil {
		t.Fatalf("CurrentConfig() error = %v", err)
	}
	if got.NodeID != want.NodeID || got.AdvertiseURL != want.AdvertiseURL || got.PushDebounceSeconds != 6 || len(got.Peers) != 2 {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestConfigServiceRejectsInvalidEnabledConfig(t *testing.T) {
	svc, provider := newHAConfigServiceTestStore(t)
	defer provider.Close()
	_, err := svc.SaveConfig(context.Background(), config.HAConfig{Enabled: true, NodeID: "hc-1"})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigServiceSaveNormalizesURLsAndPeers(t *testing.T) {
	svc, provider := newHAConfigServiceTestStore(t)
	defer provider.Close()

	got, err := svc.SaveConfig(context.Background(), config.HAConfig{
		Enabled:                         true,
		NodeID:                          " hc-1 ",
		NodeName:                        " HubCenter 1 ",
		AdvertiseURL:                    "https://hubs.mypapers.top/ ",
		ClusterSecret:                   " shared-secret ",
		SyncIntervalSeconds:             0,
		PushDebounceSeconds:             0,
		PullBatchSize:                   0,
		HeartbeatSyncMinIntervalSeconds: 0,
		Peers: []config.HAPeerConfig{
			{Enabled: false, NodeID: "", Name: "", BaseURL: ""},
			{Enabled: true, NodeID: " hc-2 ", Name: " HubCenter 2 ", BaseURL: "https://hubs.maclaw.top/ "},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if got.NodeID != "hc-1" || got.NodeName != "HubCenter 1" {
		t.Fatalf("unexpected node identity: %+v", got)
	}
	if got.AdvertiseURL != "https://hubs.mypapers.top" {
		t.Fatalf("AdvertiseURL = %q", got.AdvertiseURL)
	}
	if got.ClusterSecret != "shared-secret" {
		t.Fatalf("ClusterSecret = %q", got.ClusterSecret)
	}
	if got.SyncIntervalSeconds != 3 || got.PushDebounceSeconds != 4 || got.PullBatchSize != 200 || got.HeartbeatSyncMinIntervalSeconds != 10 {
		t.Fatalf("unexpected defaults after normalize: %+v", got)
	}
	if len(got.Peers) != 1 || got.Peers[0].NodeID != "hc-2" || got.Peers[0].BaseURL != "https://hubs.maclaw.top" {
		t.Fatalf("unexpected peers after normalize: %+v", got.Peers)
	}
}
