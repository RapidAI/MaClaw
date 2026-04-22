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
	cfg.NodeID = strings.TrimSpace(cfg.NodeID)
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
	cfg.AdvertiseURL = strings.TrimRight(strings.TrimSpace(cfg.AdvertiseURL), "/")
	cfg.ClusterSecret = strings.TrimSpace(cfg.ClusterSecret)
	peers := make([]config.HAPeerConfig, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		normalized := config.HAPeerConfig{
			NodeID:  strings.TrimSpace(peer.NodeID),
			Name:    strings.TrimSpace(peer.Name),
			BaseURL: strings.TrimRight(strings.TrimSpace(peer.BaseURL), "/"),
			Enabled: peer.Enabled,
		}
		if !normalized.Enabled && normalized.NodeID == "" && normalized.Name == "" && normalized.BaseURL == "" {
			continue
		}
		peers = append(peers, normalized)
	}
	cfg.Peers = peers
	return cfg
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}
