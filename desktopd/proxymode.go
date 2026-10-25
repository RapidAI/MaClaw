package desktopd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Desktop egress proxy modes. The mode is chosen per user desktop container
// and decided inside the bridge forward-proxy listener by the connection's
// source IP, so switching never changes the container's env (no rebuild, no
// browser restart, logins kept).
const (
	// ProxyModeProxy: everything goes through the upstream except the host's
	// DirectSuffixes (the behavior before modes existed).
	ProxyModeProxy = "proxy"
	// ProxyModeDirect: everything is dialed from this host.
	ProxyModeDirect = "direct"
	// ProxyModeSmart: mainland sites (cnrules.go) and DirectSuffixes are
	// dialed directly, everything else through the upstream.
	ProxyModeSmart = "smart"
	// DefaultProxyMode applies to users without a stored mode and to
	// connections whose source IP maps to no known desktop.
	DefaultProxyMode = ProxyModeSmart
)

// NormalizeProxyMode accepts proxy/direct/smart ("always" is an alias of
// proxy). An empty value returns "" (inherit the default).
func NormalizeProxyMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case ProxyModeProxy, "always":
		return ProxyModeProxy, nil
	case ProxyModeDirect:
		return ProxyModeDirect, nil
	case ProxyModeSmart:
		return ProxyModeSmart, nil
	}
	return "", fmt.Errorf("%w: proxy mode must be proxy, direct or smart", ErrInvalid)
}

// ContainerIPResolver lists the bridge IPs of running desktop containers,
// keyed by IP with the container name as value.
type ContainerIPResolver func(ctx context.Context) (map[string]string, error)

// proxyModeRefreshGap throttles docker lookups triggered by unknown IPs.
const proxyModeRefreshGap = 5 * time.Second

// ProxyModeStore keeps each desktop container's mode (persisted to
// stateDir/proxy_modes.json) and the source IP → container map the listener
// uses to find it.
type ProxyModeStore struct {
	path     string
	resolver ContainerIPResolver
	now      func() time.Time

	mu          sync.Mutex
	modes       map[string]string // container name → mode
	ips         map[string]string // source IP → container name
	lastRefresh time.Time
	proxies     []*ForwardProxy
}

type proxyModeFile struct {
	Modes map[string]string `json:"modes"`
}

// NewProxyModeStore loads stateDir/proxy_modes.json. An empty stateDir keeps
// modes in memory only.
func NewProxyModeStore(stateDir string, resolver ContainerIPResolver) *ProxyModeStore {
	s := &ProxyModeStore{resolver: resolver, now: time.Now, modes: map[string]string{}, ips: map[string]string{}}
	if strings.TrimSpace(stateDir) != "" {
		s.path = filepath.Join(stateDir, "proxy_modes.json")
		if raw, err := os.ReadFile(s.path); err == nil {
			var file proxyModeFile
			if err := json.Unmarshal(raw, &file); err != nil {
				log.Printf("[desktopd] proxy_modes.json unreadable, starting empty: %v", err)
			} else {
				for name, mode := range file.Modes {
					if m, err := NormalizeProxyMode(mode); err == nil && m != "" {
						s.modes[name] = m
					}
				}
			}
		}
	}
	return s
}

// attach registers a listener whose tunnels are closed when a mode changes.
func (s *ProxyModeStore) attach(p *ForwardProxy) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	s.proxies = append(s.proxies, p)
	s.mu.Unlock()
}

// Mode reports the stored mode of one user's desktop, DefaultProxyMode when
// none is stored.
func (s *ProxyModeStore) Mode(tenantID, userID string) string {
	name := proxyModeContainer(tenantID, userID)
	if s == nil || name == "" {
		return DefaultProxyMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode := s.modes[name]; mode != "" {
		return mode
	}
	return DefaultProxyMode
}

// Set stores one user's mode and, when it changed, closes that desktop's open
// tunnels so keep-alive connections re-dial on the new route. It reports
// whether the mode changed.
func (s *ProxyModeStore) Set(ctx context.Context, tenantID, userID, mode string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("%w: proxy modes are not enabled on this host", ErrInvalid)
	}
	mode, err := NormalizeProxyMode(mode)
	if err != nil {
		return false, err
	}
	if mode == "" {
		mode = DefaultProxyMode
	}
	name := proxyModeContainer(tenantID, userID)
	if name == "" {
		return false, fmt.Errorf("%w: tenant_id and user_id are required", ErrInvalid)
	}
	s.mu.Lock()
	previous := s.modes[name]
	if previous == "" {
		previous = DefaultProxyMode
	}
	if mode == DefaultProxyMode {
		delete(s.modes, name)
	} else {
		s.modes[name] = mode
	}
	err = s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return false, err
	}
	if previous == mode {
		return false, nil
	}
	s.refresh(ctx, true)
	s.mu.Lock()
	var ips []string
	for ip, container := range s.ips {
		if container == name {
			ips = append(ips, ip)
		}
	}
	proxies := append([]*ForwardProxy{}, s.proxies...)
	s.mu.Unlock()
	for _, p := range proxies {
		p.closeTunnelsFrom(ips)
	}
	return true, nil
}

func (s *ProxyModeStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(proxyModeFile{Modes: s.modes}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// modeForIP resolves the mode for a connection's source IP. Unknown IPs get
// DefaultProxyMode: a stale or missing mapping must never apply another
// user's mode.
func (s *ProxyModeStore) modeForIP(ip string) string {
	if s == nil || ip == "" {
		return DefaultProxyMode
	}
	s.mu.Lock()
	name, ok := s.ips[ip]
	s.mu.Unlock()
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.refresh(ctx, false)
		cancel()
		s.mu.Lock()
		name, ok = s.ips[ip]
		s.mu.Unlock()
	}
	if !ok {
		return DefaultProxyMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode := s.modes[name]; mode != "" {
		return mode
	}
	return DefaultProxyMode
}

// refresh reloads the IP map from docker. Unless forced it runs at most once
// per proxyModeRefreshGap.
func (s *ProxyModeStore) refresh(ctx context.Context, force bool) {
	if s == nil || s.resolver == nil {
		return
	}
	s.mu.Lock()
	if !force && s.now().Sub(s.lastRefresh) < proxyModeRefreshGap {
		s.mu.Unlock()
		return
	}
	s.lastRefresh = s.now()
	s.mu.Unlock()
	ips, err := s.resolver(ctx)
	if err != nil {
		log.Printf("[desktopd] desktop IP lookup failed: %v", err)
		return
	}
	s.mu.Lock()
	s.ips = ips
	s.mu.Unlock()
}

// proxyModeContainer names the container a (tenant, user) mode applies to. An
// isolated bot's desktop user lives in its owner's container, so the mode is
// the owner's.
func proxyModeContainer(tenantID, userID string) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	return containerName(tenantID, owner)
}

// Snapshot lists stored non-default modes by container name (admin view).
func (s *ProxyModeStore) Snapshot() map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.modes {
		out[k] = v
	}
	return out
}

// DesktopContainerIPs is the ContainerIPResolver backed by docker: every
// running maclaw-desktop-* container and its network IPs.
func (s *Service) DesktopContainerIPs(ctx context.Context) (map[string]string, error) {
	ids, err := s.docker(ctx, "ps", "-q", "--filter", "name=maclaw-desktop-")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	fields := strings.Fields(ids)
	if len(fields) == 0 {
		return out, nil
	}
	args := append([]string{"inspect", "--format", "{{.Name}}{{range .NetworkSettings.Networks}} {{.IPAddress}}{{end}}"}, fields...)
	text, err := s.docker(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseContainerIPs(text), nil
}

func parseContainerIPs(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "/")
		if !strings.HasPrefix(name, "maclaw-desktop-") {
			continue
		}
		for _, ip := range parts[1:] {
			if net.ParseIP(ip) != nil {
				out[ip] = name
			}
		}
	}
	return out
}
