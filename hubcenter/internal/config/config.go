package config

import (
	"fmt"
	"strings"
)

type Config struct {
	Server struct {
		ListenHost    string `yaml:"listen_host"`
		ListenPort    int    `yaml:"listen_port"`
		PublicBaseURL string `yaml:"public_base_url"`
	} `yaml:"server"`

	HA HAConfig `yaml:"ha"`

	Database struct {
		Driver            string `yaml:"driver"`
		DSN               string `yaml:"dsn"`
		WAL               bool   `yaml:"wal"`
		BusyTimeoutMS     int    `yaml:"busy_timeout_ms"`
		MaxReadOpenConns  int    `yaml:"max_read_open_conns"`
		MaxReadIdleConns  int    `yaml:"max_read_idle_conns"`
		MaxWriteOpenConns int    `yaml:"max_write_open_conns"`
		MaxWriteIdleConns int    `yaml:"max_write_idle_conns"`
		BatchFlushMS      int    `yaml:"batch_flush_ms"`
		BatchMaxSize      int    `yaml:"batch_max_size"`
		BatchQueueSize    int    `yaml:"batch_queue_size"`
	} `yaml:"database"`

	Mail struct {
		Enabled    bool   `yaml:"enabled"`
		Provider   string `yaml:"provider"`
		SMTPHost   string `yaml:"smtp_host"`
		SMTPPort   int    `yaml:"smtp_port"`
		Encryption string `yaml:"smtp_encryption"`
		Username   string `yaml:"smtp_username"`
		Password   string `yaml:"smtp_password"`
		FromName   string `yaml:"from_name"`
		FromEmail  string `yaml:"from_email"`
	} `yaml:"mail"`

	Logging struct {
		Level string `yaml:"level"`
		Dir   string `yaml:"dir"`
	} `yaml:"logging"`
}

type HAPeerConfig struct {
	NodeID  string `yaml:"node_id"`
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Enabled bool   `yaml:"enabled"`
}

type HAConfig struct {
	Enabled                         bool           `yaml:"enabled"`
	NodeID                          string         `yaml:"node_id"`
	NodeName                        string         `yaml:"node_name"`
	AdvertiseURL                    string         `yaml:"advertise_url"`
	ClusterSecret                   string         `yaml:"cluster_secret"`
	SyncIntervalSeconds             int            `yaml:"sync_interval_seconds"`
	PullBatchSize                   int            `yaml:"pull_batch_size"`
	HeartbeatSyncMinIntervalSeconds int            `yaml:"heartbeat_sync_min_interval_seconds"`
	Peers                           []HAPeerConfig `yaml:"peers"`
}

func Default() *Config {
	cfg := &Config{}
	cfg.Server.ListenHost = "0.0.0.0"
	cfg.Server.ListenPort = 9388
	cfg.Server.PublicBaseURL = "http://127.0.0.1:9388"
	cfg.HA.SyncIntervalSeconds = 3
	cfg.HA.PullBatchSize = 200
	cfg.HA.HeartbeatSyncMinIntervalSeconds = 10
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = "./data/MaClaw-hubcenter.db"
	cfg.Database.WAL = true
	cfg.Database.BusyTimeoutMS = 5000
	cfg.Database.MaxReadOpenConns = 8
	cfg.Database.MaxReadIdleConns = 4
	cfg.Database.MaxWriteOpenConns = 1
	cfg.Database.MaxWriteIdleConns = 1
	cfg.Database.BatchFlushMS = 250
	cfg.Database.BatchMaxSize = 64
	cfg.Database.BatchQueueSize = 1024
	cfg.Mail.Provider = "smtp"
	cfg.Mail.FromName = "MaClaw Hub Center"
	cfg.Logging.Level = "info"
	cfg.Logging.Dir = "./data/logs"
	return cfg
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if !c.HA.Enabled {
		return nil
	}
	nodeID := strings.TrimSpace(c.HA.NodeID)
	if nodeID == "" {
		return fmt.Errorf("ha.node_id is required when ha.enabled=true")
	}
	if strings.TrimSpace(c.HA.AdvertiseURL) == "" {
		return fmt.Errorf("ha.advertise_url is required when ha.enabled=true")
	}
	if strings.TrimSpace(c.HA.ClusterSecret) == "" {
		return fmt.Errorf("ha.cluster_secret is required when ha.enabled=true")
	}
	seenPeerIDs := map[string]struct{}{}
	seenPeerURLs := map[string]struct{}{}
	enabledPeers := 0
	for i, peer := range c.HA.Peers {
		if !peer.Enabled {
			continue
		}
		enabledPeers++
		peerID := strings.TrimSpace(peer.NodeID)
		peerURL := strings.TrimSpace(peer.BaseURL)
		if peerID == "" {
			return fmt.Errorf("ha.peers[%d].node_id is required for enabled peer", i)
		}
		if peerID == nodeID {
			return fmt.Errorf("ha.peers[%d].node_id must not equal ha.node_id (%s)", i, nodeID)
		}
		if peerURL == "" {
			return fmt.Errorf("ha.peers[%d].base_url is required for enabled peer %s", i, peerID)
		}
		if _, ok := seenPeerIDs[peerID]; ok {
			return fmt.Errorf("duplicate ha peer node_id: %s", peerID)
		}
		if _, ok := seenPeerURLs[peerURL]; ok {
			return fmt.Errorf("duplicate ha peer base_url: %s", peerURL)
		}
		seenPeerIDs[peerID] = struct{}{}
		seenPeerURLs[peerURL] = struct{}{}
	}
	if enabledPeers == 0 {
		return fmt.Errorf("at least one enabled ha.peers entry is required when ha.enabled=true")
	}
	return nil
}
