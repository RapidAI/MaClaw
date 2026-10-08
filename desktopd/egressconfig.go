package desktopd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// egressconfig.go backs the CONSUMER side of the egress proxy: which upstream
// proxy (a desktopd abroad) to chain through, and which URL desktop containers
// receive. The admin panel edits it; every consumer resolves it per use —
// the chain listener per CONNECT, the desktop env per desktop creation — so a
// panel change takes effect without a restart. The file lives next to the
// other state files and follows the same tmp+rename discipline; the env
// values (DESKTOPD_UPSTREAM_PROXY / DESKTOPD_DESKTOP_PROXY_URL) stay as the
// bootstrap fallback while no panel file exists.

const egressConfigFileName = "egress_proxy.json"

// EgressConfig is one consumer's egress settings. Both fields may be empty
// (= no chaining / containers go direct).
type EgressConfig struct {
	Upstream        string `json:"upstream"`
	DesktopProxyURL string `json:"desktop_proxy_url"`
	UpdatedAt       string `json:"updated_at"`
}

// EgressSource resolves the consumer egress config for one desktopd process.
type EgressSource struct {
	// path is the panel file; "" means panel management is disabled and the
	// fallback is the only configuration.
	path     string
	fallback EgressConfig

	cacheMu   sync.Mutex
	cacheSize int64
	cacheMod  time.Time
	cacheCfg  EgressConfig
	cacheOK   bool
}

// NewEgressSource builds the config source. stateDir may be empty for
// deployments without the admin panel.
func NewEgressSource(stateDir string, fallback EgressConfig) *EgressSource {
	s := &EgressSource{fallback: fallback}
	if stateDir != "" {
		s.path = filepath.Join(stateDir, egressConfigFileName)
	}
	return s
}

// Get returns the effective config and whether it came from the panel file.
// An unreadable or malformed file is an error, not a silent fallback —
// egress failing loudly beats a proxy that silently changed underneath.
func (s *EgressSource) Get() (cfg EgressConfig, fromPanel bool, err error) {
	if s.path == "" {
		return s.fallback, false, nil
	}
	info, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.fallback, false, nil
		}
		return EgressConfig{}, false, err
	}
	s.cacheMu.Lock()
	if s.cacheOK && s.cacheSize == info.Size() && s.cacheMod.Equal(info.ModTime()) {
		cfg := s.cacheCfg
		s.cacheMu.Unlock()
		return cfg, true, nil
	}
	s.cacheMu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return EgressConfig{}, false, err
	}
	var file EgressConfig
	if err := json.Unmarshal(raw, &file); err != nil {
		return EgressConfig{}, false, fmt.Errorf("%s is malformed: %w", egressConfigFileName, err)
	}
	cfg, err = normalizeEgressConfig(file)
	if err != nil {
		return EgressConfig{}, false, fmt.Errorf("%s: %w", egressConfigFileName, err)
	}
	s.cacheMu.Lock()
	s.cacheCfg = cfg
	s.cacheSize = info.Size()
	s.cacheMod = info.ModTime()
	s.cacheOK = true
	s.cacheMu.Unlock()
	return cfg, true, nil
}

// Set validates and stores the panel-chosen config. Takes effect on the next
// use of each consumer.
func (s *EgressSource) Set(cfg EgressConfig) error {
	if s.path == "" {
		return fmt.Errorf("egress management is disabled (no state directory)")
	}
	cfg, err := normalizeEgressConfig(cfg)
	if err != nil {
		return err
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := writeStateFile(dir, egressConfigFileName, raw); err != nil {
		return err
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	s.cacheMu.Lock()
	s.cacheCfg = cfg
	s.cacheSize = info.Size()
	s.cacheMod = info.ModTime()
	s.cacheOK = true
	s.cacheMu.Unlock()
	return nil
}

// Clear removes the panel-set config, restoring the env fallback.
func (s *EgressSource) Clear() error {
	if s.path == "" {
		return fmt.Errorf("egress management is disabled (no state directory)")
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.cacheMu.Lock()
	s.cacheCfg = EgressConfig{}
	s.cacheSize = 0
	s.cacheMod = time.Time{}
	s.cacheOK = false
	s.cacheMu.Unlock()
	return nil
}

// normalizeEgressConfig trims and validates both fields. The upstream must
// parse as an http(s) proxy URL (it may embed the key as userinfo); the
// desktop URL must pass DesktopProxyBind's private-IP rules. Empty means off.
func normalizeEgressConfig(cfg EgressConfig) (EgressConfig, error) {
	cfg.Upstream = strings.TrimSpace(cfg.Upstream)
	cfg.DesktopProxyURL = strings.TrimSpace(cfg.DesktopProxyURL)
	if cfg.Upstream != "" {
		u, err := url.Parse(cfg.Upstream)
		if err != nil || u == nil || u.Host == "" {
			return EgressConfig{}, fmt.Errorf("upstream %q does not parse as a URL", cfg.Upstream)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return EgressConfig{}, fmt.Errorf("upstream scheme must be http or https, not %q", u.Scheme)
		}
	}
	if cfg.DesktopProxyURL != "" {
		if _, _, err := DesktopProxyBind(cfg.DesktopProxyURL); err != nil {
			return EgressConfig{}, err
		}
	}
	return cfg, nil
}
