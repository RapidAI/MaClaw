package config

import (
	"fmt"
	"net"
	"net/url"
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
	NodeID       string `yaml:"node_id"`
	Name         string `yaml:"name"`
	BaseURL      string `yaml:"base_url"`
	PublicKeyPEM string `yaml:"public_key"`
	Enabled      bool   `yaml:"enabled"`
}

type HANodeConfig struct {
	FQDN         string `yaml:"fqdn"`
	NodeID       string `yaml:"node_id"`
	NodeName     string `yaml:"node_name"`
	AdvertiseURL string `yaml:"advertise_url"`
	PublicKeyPEM string `yaml:"public_key"`
	Enabled      bool   `yaml:"enabled"`
}

type HAConfig struct {
	Enabled                         bool           `yaml:"enabled"`
	SelfFQDN                        string         `yaml:"self_fqdn"`
	PrivateKeyPath                  string         `yaml:"private_key_path"`
	NodeID                          string         `yaml:"node_id"`
	NodeName                        string         `yaml:"node_name"`
	AdvertiseURL                    string         `yaml:"advertise_url"`
	ClusterSecret                   string         `yaml:"cluster_secret"`
	SyncIntervalSeconds             int            `yaml:"sync_interval_seconds"`
	PullBatchSize                   int            `yaml:"pull_batch_size"`
	HeartbeatSyncMinIntervalSeconds int            `yaml:"heartbeat_sync_min_interval_seconds"`
	Nodes                           []HANodeConfig `yaml:"nodes"`
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
	resolved, err := ResolveHAConfig(c.HA)
	if err != nil {
		return err
	}
	c.HA = resolved
	return nil
}

func ResolveHAConfig(cfg HAConfig) (HAConfig, error) {
	resolved := normalizeResolvedHAConfig(cfg)
	if usesHANodeCatalog(resolved) {
		return resolveHANodeCatalog(resolved)
	}
	if err := validateResolvedLegacyHAConfig(resolved); err != nil {
		return HAConfig{}, err
	}
	return resolved, nil
}

func usesHANodeCatalog(cfg HAConfig) bool {
	return strings.TrimSpace(cfg.SelfFQDN) != "" || len(cfg.Nodes) > 0
}

func resolveHANodeCatalog(cfg HAConfig) (HAConfig, error) {
	selfFQDN := NormalizeHAFQDN(cfg.SelfFQDN)
	if selfFQDN == "" {
		return HAConfig{}, fmt.Errorf("ha.self_fqdn is required when ha.nodes is configured")
	}
	if len(cfg.Nodes) == 0 {
		return HAConfig{}, fmt.Errorf("ha.nodes is required when ha.self_fqdn is configured")
	}
	seenFQDNs := map[string]struct{}{}
	seenNodeIDs := map[string]struct{}{}
	seenURLs := map[string]struct{}{}
	peers := make([]HAPeerConfig, 0, len(cfg.Nodes))
	var self *HANodeConfig

	for i := range cfg.Nodes {
		node := cfg.Nodes[i]
		if !node.Enabled {
			continue
		}
		if node.FQDN == "" {
			return HAConfig{}, fmt.Errorf("ha.nodes[%d].fqdn is required for enabled node", i)
		}
		if node.NodeID == "" {
			return HAConfig{}, fmt.Errorf("ha.nodes[%d].node_id is required for enabled node %s", i, node.FQDN)
		}
		if node.AdvertiseURL == "" {
			return HAConfig{}, fmt.Errorf("ha.nodes[%d].advertise_url is required for enabled node %s", i, node.NodeID)
		}
		if _, ok := seenFQDNs[node.FQDN]; ok {
			return HAConfig{}, fmt.Errorf("duplicate ha node fqdn: %s", node.FQDN)
		}
		if _, ok := seenNodeIDs[node.NodeID]; ok {
			return HAConfig{}, fmt.Errorf("duplicate ha node node_id: %s", node.NodeID)
		}
		if _, ok := seenURLs[node.AdvertiseURL]; ok {
			return HAConfig{}, fmt.Errorf("duplicate ha node advertise_url: %s", node.AdvertiseURL)
		}
		seenFQDNs[node.FQDN] = struct{}{}
		seenNodeIDs[node.NodeID] = struct{}{}
		seenURLs[node.AdvertiseURL] = struct{}{}

		if node.FQDN == selfFQDN {
			if self != nil {
				return HAConfig{}, fmt.Errorf("ha.self_fqdn matches multiple ha.nodes entries: %s", selfFQDN)
			}
			copy := node
			self = &copy
			continue
		}
		peers = append(peers, HAPeerConfig{NodeID: node.NodeID, Name: node.NodeName, BaseURL: node.AdvertiseURL, PublicKeyPEM: node.PublicKeyPEM, Enabled: true})
	}

	if self == nil {
		return HAConfig{}, fmt.Errorf("ha.self_fqdn %q does not match any enabled ha.nodes entry", selfFQDN)
	}
	resolved := cfg
	resolved.SelfFQDN = selfFQDN
	resolved.NodeID = self.NodeID
	resolved.NodeName = self.NodeName
	resolved.AdvertiseURL = self.AdvertiseURL
	resolved.Peers = peers
	if err := validateResolvedLegacyHAConfig(resolved); err != nil {
		return HAConfig{}, err
	}
	return resolved, nil
}

func validateResolvedLegacyHAConfig(cfg HAConfig) error {
	nodeID := strings.TrimSpace(cfg.NodeID)
	if nodeID == "" {
		return fmt.Errorf("ha.node_id is required when ha.enabled=true")
	}
	if strings.TrimSpace(cfg.AdvertiseURL) == "" {
		return fmt.Errorf("ha.advertise_url is required when ha.enabled=true")
	}
	if strings.TrimSpace(cfg.ClusterSecret) == "" {
		return fmt.Errorf("ha.cluster_secret is required when ha.enabled=true")
	}
	seenPeerIDs := map[string]struct{}{}
	seenPeerURLs := map[string]struct{}{}
	enabledPeers := 0
	for i, peer := range cfg.Peers {
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

func normalizeResolvedHAConfig(cfg HAConfig) HAConfig {
	cfg.SelfFQDN = NormalizeHAFQDN(cfg.SelfFQDN)
	cfg.PrivateKeyPath = strings.TrimSpace(cfg.PrivateKeyPath)
	cfg.NodeID = strings.TrimSpace(cfg.NodeID)
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
	cfg.AdvertiseURL = NormalizeHAURL(cfg.AdvertiseURL)
	cfg.ClusterSecret = strings.TrimSpace(cfg.ClusterSecret)

	peers := make([]HAPeerConfig, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		normalized := HAPeerConfig{
			NodeID:       strings.TrimSpace(peer.NodeID),
			Name:         strings.TrimSpace(peer.Name),
			BaseURL:      NormalizeHAURL(peer.BaseURL),
			PublicKeyPEM: strings.TrimSpace(peer.PublicKeyPEM),
			Enabled:      peer.Enabled,
		}
		if !normalized.Enabled && normalized.NodeID == "" && normalized.Name == "" && normalized.BaseURL == "" && normalized.PublicKeyPEM == "" {
			continue
		}
		peers = append(peers, normalized)
	}
	cfg.Peers = peers

	nodes := make([]HANodeConfig, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		normalized := HANodeConfig{
			FQDN:         NormalizeHAFQDN(node.FQDN),
			NodeID:       strings.TrimSpace(node.NodeID),
			NodeName:     strings.TrimSpace(node.NodeName),
			AdvertiseURL: NormalizeHAURL(node.AdvertiseURL),
			PublicKeyPEM: strings.TrimSpace(node.PublicKeyPEM),
			Enabled:      node.Enabled || strings.TrimSpace(node.FQDN) != "" || strings.TrimSpace(node.NodeID) != "" || strings.TrimSpace(node.NodeName) != "" || strings.TrimSpace(node.AdvertiseURL) != "" || strings.TrimSpace(node.PublicKeyPEM) != "",
		}
		if !normalized.Enabled && normalized.FQDN == "" && normalized.NodeID == "" && normalized.NodeName == "" && normalized.AdvertiseURL == "" && normalized.PublicKeyPEM == "" {
			continue
		}
		nodes = append(nodes, normalized)
	}
	cfg.Nodes = nodes
	return cfg
}

func NormalizeHAURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/")
	}
	return strings.TrimRight(value, "/")
}

func NormalizeHAFQDN(value string) string {
	value = strings.TrimSpace(strings.TrimRight(value, "/"))
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		host = strings.TrimSpace(parsed.Host)
	}
	if host == "" {
		host = strings.TrimSpace(parsed.Path)
	}
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		host = hostOnly
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}
