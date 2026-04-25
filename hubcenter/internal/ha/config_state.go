package ha

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const systemKeyHAConfig = "ha_config"

type ConfigService struct {
	fallback config.HAConfig
	settings store.SystemSettingsRepository
}

func NewConfigService(fallback config.HAConfig, settings store.SystemSettingsRepository) *ConfigService {
	return &ConfigService{fallback: normalizeHAConfig(config.Default().HA, fallback), settings: settings}
}

func (s *ConfigService) CurrentConfig(ctx context.Context) (config.HAConfig, error) {
	if s == nil {
		return config.Default().HA, nil
	}
	if s.settings == nil {
		return normalizeHAConfig(config.Default().HA, s.fallback), nil
	}
	raw, err := s.settings.Get(ctx, systemKeyHAConfig)
	if err != nil {
		return config.HAConfig{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return normalizeHAConfig(config.Default().HA, s.fallback), nil
	}
	var saved config.HAConfig
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return config.HAConfig{}, err
	}
	return normalizeHAConfig(config.Default().HA, mergeHAConfig(s.fallback, saved)), nil
}

func (s *ConfigService) SaveConfig(ctx context.Context, cfg config.HAConfig) (config.HAConfig, error) {
	base := config.Default().HA
	if s != nil {
		base = s.fallback
	}
	normalized := normalizeHAConfig(config.Default().HA, mergeHAConfig(base, cfg))
	if err := validateHAConfig(normalized); err != nil {
		return config.HAConfig{}, err
	}
	if s == nil || s.settings == nil {
		s.fallback = normalized
		return normalized, nil
	}
	if err := s.settings.Set(ctx, systemKeyHAConfig, mustJSON(normalized)); err != nil {
		return config.HAConfig{}, err
	}
	return normalized, nil
}

func validateHAConfig(cfg config.HAConfig) error {
	tmp := config.Default()
	tmp.HA = cfg
	return tmp.Validate()
}

func mergeHAConfig(base, override config.HAConfig) config.HAConfig {
	merged := base
	merged.Enabled = override.Enabled
	if strings.TrimSpace(override.SelfFQDN) != "" {
		merged.SelfFQDN = override.SelfFQDN
	}
	if strings.TrimSpace(override.PrivateKeyPath) != "" {
		merged.PrivateKeyPath = override.PrivateKeyPath
	}
	if strings.TrimSpace(override.NodeID) != "" {
		merged.NodeID = override.NodeID
	}
	if strings.TrimSpace(override.NodeName) != "" {
		merged.NodeName = override.NodeName
	}
	if strings.TrimSpace(override.AdvertiseURL) != "" {
		merged.AdvertiseURL = override.AdvertiseURL
	}
	if strings.TrimSpace(override.ClusterSecret) != "" {
		merged.ClusterSecret = override.ClusterSecret
	}
	if override.SyncIntervalSeconds > 0 {
		merged.SyncIntervalSeconds = override.SyncIntervalSeconds
	}
	if override.PullBatchSize > 0 {
		merged.PullBatchSize = override.PullBatchSize
	}
	if override.HeartbeatSyncMinIntervalSeconds > 0 {
		merged.HeartbeatSyncMinIntervalSeconds = override.HeartbeatSyncMinIntervalSeconds
	}
	if override.Nodes != nil {
		merged.Nodes = append([]config.HANodeConfig(nil), override.Nodes...)
	}
	if override.Peers != nil {
		merged.Peers = append([]config.HAPeerConfig(nil), override.Peers...)
	}
	return merged
}

func normalizeHAConfig(defaults, cfg config.HAConfig) config.HAConfig {
	if cfg.SyncIntervalSeconds <= 0 {
		cfg.SyncIntervalSeconds = defaults.SyncIntervalSeconds
	}
	if cfg.PullBatchSize <= 0 {
		cfg.PullBatchSize = defaults.PullBatchSize
	}
	if cfg.HeartbeatSyncMinIntervalSeconds <= 0 {
		cfg.HeartbeatSyncMinIntervalSeconds = defaults.HeartbeatSyncMinIntervalSeconds
	}
	cfg.SelfFQDN = config.NormalizeHAFQDN(cfg.SelfFQDN)
	cfg.PrivateKeyPath = strings.TrimSpace(cfg.PrivateKeyPath)
	cfg.NodeID = strings.TrimSpace(cfg.NodeID)
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
	cfg.AdvertiseURL = config.NormalizeHAURL(cfg.AdvertiseURL)
	cfg.ClusterSecret = strings.TrimSpace(cfg.ClusterSecret)
	if !cfg.Enabled {
		return cfg
	}
	resolved, err := config.ResolveHAConfig(cfg)
	if err != nil {
		return cfg
	}
	if strings.TrimSpace(resolved.PrivateKeyPath) == "" {
		resolved.PrivateKeyPath = cfg.PrivateKeyPath
	}
	return resolved
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}
