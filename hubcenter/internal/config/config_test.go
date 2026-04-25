package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidatesHAConfig(t *testing.T) {
	t.Run("valid ha config", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  node_id: hc-1
  advertise_url: http://10.0.0.11:9388
  cluster_secret: shared-secret
  peers:
    - node_id: hc-2
      base_url: http://10.0.0.12:9388
      enabled: true
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.HA.Enabled || cfg.HA.NodeID != "hc-1" {
			t.Fatalf("unexpected cfg: %#v", cfg.HA)
		}
	})

	t.Run("resolves self fqdn from node catalog", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  self_fqdn: https://Hubs.Maclaw.Top/admin
  cluster_secret: shared-secret
  nodes:
    - fqdn: hubs.mypapers.top
      node_id: hc-1
      advertise_url: https://hubs.mypapers.top
    - fqdn: hubs.maclaw.top
      node_id: hc-2
      advertise_url: https://hubs.maclaw.top
    - fqdn: hubs2.maclaw.top
      node_id: hc-3
      advertise_url: https://hubs2.maclaw.top
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got := cfg.HA.SelfFQDN; got != "hubs.maclaw.top" {
			t.Fatalf("self_fqdn = %q", got)
		}
		if got := cfg.HA.NodeID; got != "hc-2" {
			t.Fatalf("node_id = %q", got)
		}
		if got := cfg.HA.AdvertiseURL; got != "https://hubs.maclaw.top" {
			t.Fatalf("advertise_url = %q", got)
		}
		if len(cfg.HA.Peers) != 2 {
			t.Fatalf("peer count = %d", len(cfg.HA.Peers))
		}
	})

	t.Run("missing cluster secret", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  node_id: hc-1
  advertise_url: http://10.0.0.11:9388
  peers:
    - node_id: hc-2
      base_url: http://10.0.0.12:9388
      enabled: true
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "ha.cluster_secret") {
			t.Fatalf("Load() error = %v, want cluster_secret validation error", err)
		}
	})

	t.Run("missing self fqdn match", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  self_fqdn: hubs.unknown.top
  cluster_secret: shared-secret
  nodes:
    - fqdn: hubs.mypapers.top
      node_id: hc-1
      advertise_url: https://hubs.mypapers.top
    - fqdn: hubs.maclaw.top
      node_id: hc-2
      advertise_url: https://hubs.maclaw.top
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "does not match any enabled ha.nodes entry") {
			t.Fatalf("Load() error = %v, want self_fqdn match error", err)
		}
	})

	t.Run("peer points to self", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  node_id: hc-1
  advertise_url: http://10.0.0.11:9388
  cluster_secret: shared-secret
  peers:
    - node_id: hc-1
      base_url: http://10.0.0.12:9388
      enabled: true
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "must not equal ha.node_id") {
			t.Fatalf("Load() error = %v, want self-peer validation error", err)
		}
	})

	t.Run("duplicate peer ids", func(t *testing.T) {
		path := writeConfigFile(t, `
ha:
  enabled: true
  node_id: hc-1
  advertise_url: http://10.0.0.11:9388
  cluster_secret: shared-secret
  peers:
    - node_id: hc-2
      base_url: http://10.0.0.12:9388
      enabled: true
    - node_id: hc-2
      base_url: http://10.0.0.13:9388
      enabled: true
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "duplicate ha peer node_id") {
			t.Fatalf("Load() error = %v, want duplicate peer id error", err)
		}
	})
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
