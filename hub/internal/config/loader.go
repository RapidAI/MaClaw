package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config file: %w", err)
	}

	return cfg, nil
}

// SaveTLSEnabled updates the tls.enabled field in the YAML config file,
// preserving all other content. When the tls section doesn't exist yet,
// it writes the full set of default TLS fields so that yaml.Unmarshal
// won't zero-out defaults (cert_file, key_file, auto_generate) on reload.
func SaveTLSEnabled(path string, enabled bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	// Preserve original file permissions.
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config file: %w", err)
	}
	perm := fi.Mode().Perm()

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	tlsSection, ok := raw["tls"].(map[string]any)
	if !ok {
		// First time: populate all TLS defaults so they survive
		// yaml.Unmarshal into the typed Config struct on next load.
		defaults := Default()
		tlsSection = map[string]any{
			"cert_file":     defaults.TLS.CertFile,
			"key_file":      defaults.TLS.KeyFile,
			"auto_generate": defaults.TLS.AutoGenerate,
		}
	}
	tlsSection["enabled"] = enabled
	raw["tls"] = tlsSection

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Atomic write: write to temp file in same directory, then rename.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, out, perm); err != nil {
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}
