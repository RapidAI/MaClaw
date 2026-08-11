package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var configWriteMu sync.Mutex

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
	configWriteMu.Lock()
	defer configWriteMu.Unlock()
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

	// Atomic write: use a unique temp file in the same directory. A fixed .tmp
	// name is unsafe when configuration updates run concurrently.
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.WriteFile(tmpPath, out, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}

// SaveCenterInstallationID records the stable Hub installation identity in
// config.yaml. Unlike system_settings this file normally survives a Hub data
// directory rebuild, allowing Hub Center to recognize a reinstalled Hub.
func SaveCenterInstallationID(path, installationID string) error {
	return SaveCenterRecoveryIdentity(path, installationID, "", "")
}

// SaveCenterRecoveryIdentity records the stable Hub identity required to
// recover Hub Center-backed device bindings after the local database is
// rebuilt. Empty fields leave the corresponding existing config value intact.
func SaveCenterRecoveryIdentity(path, installationID, ownerEmail, recoverySecret string) error {
	installationID = strings.TrimSpace(installationID)
	ownerEmail = strings.TrimSpace(ownerEmail)
	recoverySecret = strings.TrimSpace(recoverySecret)
	if path == "" || (installationID == "" && ownerEmail == "" && recoverySecret == "") {
		return nil
	}
	configWriteMu.Lock()
	defer configWriteMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config file: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	center, ok := raw["center"].(map[string]any)
	if !ok {
		center = map[string]any{}
	}
	if installationID != "" {
		center["installation_id"] = installationID
	}
	if ownerEmail != "" {
		center["owner_email"] = ownerEmail
	}
	if recoverySecret != "" {
		center["recovery_secret"] = recoverySecret
	}
	raw["center"] = center
	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.WriteFile(tmpPath, out, fi.Mode().Perm()); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}
