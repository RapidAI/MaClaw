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
