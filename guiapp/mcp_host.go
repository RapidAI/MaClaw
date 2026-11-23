package guiapp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	mcpHostDefaultBind   = "127.0.0.1"
	mcpHostWildcardBind  = "0.0.0.0"
	mcpHostPreferredPort = 18790
	mcpAPIKeyPrefix      = "mck_"
	mcpEndpointProtocol  = "mcp-streamable-http"
)

// mcpHost is the streamable-HTTP MCP listener. It binds loopback only unless
// the user opts in to LAN access (config mcp_host_lan_enabled), which widens
// the bind to the wildcard address; the Bearer key stays mandatory either way.
// ACP keeps its own NDJSON socket. This listener only adapts MCP onto the
// same task records and the same assistant runner.
type mcpHost struct {
	app *App

	mu        sync.Mutex
	listener  net.Listener
	server    *http.Server
	token     string
	prevToken string
	port      int
	lan       bool
	lastErr   string
}

type mcpHostEndpoint struct {
	URL       string `json:"url"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Protocol  string `json:"protocol"`
	UpdatedAt string `json:"updatedAt"`
}

func newMCPHost(app *App) *mcpHost {
	return &mcpHost{app: app}
}

func (a *App) ensureMCPHost() {
	a.syncMCPHostFromConfig()
}

func (a *App) syncMCPHostFromConfig() {
	if a == nil {
		return
	}
	// The bot's task calls use the outbound link, which should come up as
	// soon as the service is switched on instead of after the idle sleep.
	defer nudgeLocalRelay()
	a.mcpHostSyncMu.Lock()
	defer a.mcpHostSyncMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.IsMcpHostEnabled() {
		a.stopMCPHostLocked()
		if err != nil {
			log.Printf("[mcp-host] load config: %v", err)
		} else {
			log.Printf("[mcp-host] disabled by config")
		}
		return
	}
	// A listener already on this address keeps its port. Restarting it would
	// drop clients and publish a gap where endpoint.json and the API key disagree.
	if a.alignRunningMCPHost(cfg) {
		return
	}
	a.stopMCPHostLocked()
	host := newMCPHost(a)
	if err := host.Start(cfg.PreferredMcpHostPort(), cfg.IsMcpHostLanEnabled()); err != nil {
		log.Printf("[mcp-host] start: %v", err)
	}
	a.mcpHostMu.Lock()
	a.mcpHost = host
	a.mcpHostMu.Unlock()
}

func (a *App) stopMCPHost() {
	if a == nil {
		return
	}
	a.mcpHostSyncMu.Lock()
	defer a.mcpHostSyncMu.Unlock()
	a.stopMCPHostLocked()
}

func (a *App) stopMCPHostLocked() {
	a.mcpHostMu.Lock()
	host := a.mcpHost
	a.mcpHost = nil
	a.mcpHostMu.Unlock()
	if host != nil {
		host.Stop()
	}
	clearMCPHostEndpoint()
}

func (h *mcpHost) Start(preferredPort int, lan bool) error {
	if h == nil {
		return fmt.Errorf("mcp host is nil")
	}
	h.mu.Lock()
	if h.listener != nil {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	key, err := loadOrCreateMCPAPIKey()
	if err != nil {
		h.setLastErr(err.Error())
		return err
	}
	ln, port, err := listenMCP(preferredPort, lan)
	if err != nil {
		h.setLastErr(err.Error())
		return err
	}
	h.mu.Lock()
	if h.listener != nil {
		h.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	h.token = key
	h.listener = ln
	h.port = port
	h.lan = lan
	h.lastErr = ""
	h.server = &http.Server{
		Handler:           newMCPHTTPTokens(h.tokenValues, h.app, h.lanValue),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv := h.server
	h.mu.Unlock()

	go func() {
		serveErr := srv.Serve(ln)
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Printf("[mcp-host] serve: %v", serveErr)
		}
	}()
	// Align before discovery is published, so a stdio client never learns a
	// port whose listener still has a different key.
	if err := h.alignMCPAPIKeyFile(); err != nil {
		h.setLastErr(err.Error())
		log.Printf("[mcp-host] apikey: %v", err)
	}
	if err := writeMCPHostEndpoint(port); err != nil {
		h.setLastErr(err.Error())
		log.Printf("[mcp-host] discovery: %v", err)
		return err
	}
	if lan {
		log.Printf("[mcp-host] listening on %s:%d (lan access on)", mcpHostWildcardBind, port)
	} else {
		log.Printf("[mcp-host] listening on %s:%d", mcpHostDefaultBind, port)
	}
	return nil
}

func (h *mcpHost) Stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	srv := h.server
	ln := h.listener
	h.server = nil
	h.listener = nil
	h.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
}

// tokenValues returns the keys currently accepted by the MCP listener: the
// live token plus the previous one kept across a rotation so in-flight
// clients holding the old key are not cut off. Order is current, previous.
func (h *mcpHost) tokenValues() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, 2)
	if h.token != "" {
		out = append(out, h.token)
	}
	if h.prevToken != "" && h.prevToken != h.token {
		out = append(out, h.prevToken)
	}
	return out
}

// setToken installs a freshly generated key. The outgoing token moves to
// prevToken so requests signed with it keep working until the next rotation.
func (h *mcpHost) setToken(token string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.prevToken = h.token
	h.token = token
	h.mu.Unlock()
}

// applyMCPAPIKey makes key the only token the listener accepts.
func (h *mcpHost) applyMCPAPIKey(key string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.token = key
	h.prevToken = ""
	h.mu.Unlock()
}

// alignMCPAPIKeyFile makes the apikey file and the live listener token the
// same key. A usable file wins. A missing file is rewritten from the listener
// so stdio sends the key the listener already accepts. Matching keys leave a
// rotation's previous token in place. This read does not generate a new key.
func (h *mcpHost) alignMCPAPIKeyFile() error {
	if h == nil {
		return nil
	}
	_, _, running, _, token, _ := h.snapshot()
	if !running {
		return nil
	}
	fileKey, err := readMCPAPIKey()
	if err == nil && mcpAPIKeyAcceptable(fileKey) {
		if fileKey != token {
			h.applyMCPAPIKey(fileKey)
		}
		return nil
	}
	if mcpAPIKeyAcceptable(token) {
		return writeMCPAPIKey(token)
	}
	key, err := loadOrCreateMCPAPIKey()
	if err != nil {
		return err
	}
	h.applyMCPAPIKey(key)
	return nil
}

// alignRunningMCPHost reports whether the current listener can stay bound.
// Port and LAN changes still restart. A key or discovery mismatch is fixed
// on the live listener.
func (a *App) alignRunningMCPHost(cfg corelib.AppConfig) bool {
	if a == nil {
		return false
	}
	a.mcpHostMu.Lock()
	host := a.mcpHost
	a.mcpHostMu.Unlock()
	if host == nil {
		return false
	}
	_, port, running, _, _, lan := host.snapshot()
	if !running || port <= 0 || lan != cfg.IsMcpHostLanEnabled() {
		return false
	}
	if preferred := cfg.PreferredMcpHostPort(); preferred > 0 && port != preferred {
		return false
	}
	if err := host.alignMCPAPIKeyFile(); err != nil {
		log.Printf("[mcp-host] apikey: %v", err)
		host.setLastErr(err.Error())
	} else {
		host.setLastErr("")
	}
	if !mcpEndpointPublished(port) {
		if err := writeMCPHostEndpoint(port); err != nil {
			log.Printf("[mcp-host] discovery: %v", err)
			host.setLastErr(err.Error())
		}
	}
	return true
}

func (h *mcpHost) setLastErr(message string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.lastErr = message
	h.mu.Unlock()
}

func (h *mcpHost) lanValue() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lan
}

func (h *mcpHost) snapshot() (addr string, port int, running bool, lastErr, token string, lan bool) {
	if h == nil {
		return "", 0, false, "", "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	port = h.port
	lastErr = h.lastErr
	token = h.token
	lan = h.lan
	running = h.listener != nil
	if h.listener != nil {
		addr = h.listener.Addr().String()
	}
	return addr, port, running, lastErr, token, lan
}

func listenMCP(preferredPort int, lan bool) (net.Listener, int, error) {
	bind := mcpHostDefaultBind
	if lan {
		bind = mcpHostWildcardBind
	}
	ports := []int{mcpHostPreferredPort, 0}
	if preferredPort > 0 {
		ports = []int{preferredPort}
	}
	var lastErr error
	for _, port := range ports {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bind, port))
		if err != nil {
			lastErr = err
			log.Printf("[mcp-host] bind %s:%d failed: %v", bind, port, err)
			continue
		}
		tcpAddr, ok := ln.Addr().(*net.TCPAddr)
		if !ok || tcpAddr.Port <= 0 {
			_ = ln.Close()
			lastErr = fmt.Errorf("mcp listener address is invalid")
			continue
		}
		if lan && !tcpAddr.IP.IsUnspecified() {
			_ = ln.Close()
			lastErr = fmt.Errorf("mcp lan listener is not wildcard")
			continue
		}
		if !lan && !isLoopbackAddr(ln.Addr()) {
			_ = ln.Close()
			lastErr = fmt.Errorf("mcp listener is not loopback")
			continue
		}
		return ln, tcpAddr.Port, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("mcp listen failed")
	}
	return nil, 0, lastErr
}

// mcpLANAddressFn indirection keeps the LAN URL in the status payload
// testable without a real network.
var mcpLANAddressFn = mcpLANAddress

// mcpLANAddress returns the primary private LAN IPv4 for display URLs, or ""
// when the machine has none (offline, loopback-only adapters).
func mcpLANAddress() string {
	// An unconnected UDP dial resolves the default-route interface locally
	// without sending a packet, so it stays cheap and side-effect free.
	if conn, err := net.DialTimeout("udp", "8.8.8.8:80", 300*time.Millisecond); err == nil {
		addr, ok := conn.LocalAddr().(*net.UDPAddr)
		_ = conn.Close()
		if ok {
			if ip := addr.IP.To4(); ip != nil && ip.IsPrivate() {
				return ip.String()
			}
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil && ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}

// GetMCPHostStatus reports the inbound MCP server for the tools page.
// The URL, API key, and client configs are included only while it is listening.
func (a *App) GetMCPHostStatus() map[string]any {
	enabled := false
	lanEnabled := false
	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			enabled = cfg.IsMcpHostEnabled()
			lanEnabled = cfg.IsMcpHostLanEnabled()
		}
	}
	if a == nil || !enabled {
		return map[string]any{"enabled": false, "running": false, "lanEnabled": lanEnabled}
	}
	a.mcpHostMu.Lock()
	host := a.mcpHost
	a.mcpHostMu.Unlock()
	addr, port, running, lastErr, key := "", 0, false, "", ""
	if host != nil {
		addr, port, running, lastErr, key, _ = host.snapshot()
	}
	return mcpStatusPayload(true, running, lanEnabled, addr, port, key, mcpExecutablePath(), lastErr)
}

func mcpStatusPayload(enabled, running, lanEnabled bool, addr string, port int, key, command, lastErr string) map[string]any {
	out := map[string]any{
		"enabled":    enabled,
		"running":    false,
		"lanEnabled": lanEnabled,
	}
	if !enabled {
		return out
	}
	if strings.TrimSpace(lastErr) != "" {
		out["error"] = lastErr
	}
	if !running || port <= 0 || strings.TrimSpace(key) == "" {
		return out
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	stdioConfig, httpConfig := mcpClientConfigs(command, endpoint, key)
	out["running"] = true
	out["address"] = addr
	out["url"] = endpoint
	out["port"] = port
	out["apiKey"] = key
	out["command"] = command
	out["stdioConfig"] = stdioConfig
	out["httpConfig"] = httpConfig
	if lanEnabled {
		if lanIP := mcpLANAddressFn(); lanIP != "" {
			lanEndpoint := fmt.Sprintf("http://%s:%d/mcp", lanIP, port)
			out["lanURL"] = lanEndpoint
			out["lanHTTPConfig"] = mcpHTTPClientConfig(command, lanEndpoint, key)
		}
	}
	return out
}

func mcpClientConfigs(command, endpoint, key string) (string, string) {
	stdioRaw, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"maclaw": map[string]any{
				"command": command,
				"args":    []string{"mcp"},
			},
		},
	})
	return string(stdioRaw), mcpHTTPClientConfig(command, endpoint, key)
}

func mcpHTTPClientConfig(command, endpoint, key string) string {
	httpRaw, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"maclaw": map[string]any{
				"url": endpoint,
				"headers": map[string]any{
					"Authorization": "Bearer " + key,
				},
			},
		},
	})
	return string(httpRaw)
}

func mcpExecutablePath() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "MaClaw.exe"
	}
	return exe
}

// GenerateMCPHostAPIKey generates a new local API key. The previous key
// stays valid: requests may carry either the old or the new key until the
// user generates the next one. The server must already be enabled.
func (a *App) GenerateMCPHostAPIKey() (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("请先打开 MCP 服务器")
	}
	a.mcpHostSyncMu.Lock()
	defer a.mcpHostSyncMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.IsMcpHostEnabled() {
		return nil, fmt.Errorf("请先打开 MCP 服务器")
	}
	key, err := rotateMCPAPIKey()
	if err != nil {
		return nil, err
	}
	a.mcpHostMu.Lock()
	host := a.mcpHost
	a.mcpHostMu.Unlock()
	if host != nil {
		host.setToken(key)
	}
	// LAN state comes from config, not the host snapshot: a nil host (listener
	// never started) must not flip the panel's LAN switch back to Off.
	return a.mcpStatusWhileEnabled(host, key, cfg.IsMcpHostLanEnabled()), nil
}

func (a *App) mcpStatusWhileEnabled(host *mcpHost, key string, lanEnabled bool) map[string]any {
	addr, port, running, lastErr, liveKey := "", 0, false, "", ""
	if host != nil {
		addr, port, running, lastErr, liveKey, _ = host.snapshot()
	}
	if strings.TrimSpace(liveKey) != "" {
		key = liveKey
	}
	return mcpStatusPayload(true, running, lanEnabled, addr, port, key, mcpExecutablePath(), lastErr)
}

func mcpDiscoveryDir() string {
	return filepath.Join(corelib.MaclawBaseDir(), "mcp")
}

func loadOrCreateMCPAPIKey() (string, error) {
	if key, err := readMCPAPIKey(); err == nil && mcpAPIKeyAcceptable(key) {
		return key, nil
	}
	return rotateMCPAPIKey()
}

func readMCPAPIKey() (string, error) {
	raw, err := os.ReadFile(filepath.Join(mcpDiscoveryDir(), "apikey"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func rotateMCPAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := mcpAPIKeyPrefix + hex.EncodeToString(buf)
	if err := writeMCPAPIKey(key); err != nil {
		return "", err
	}
	return key, nil
}

func writeMCPAPIKey(key string) error {
	dir := mcpDiscoveryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "apikey"), []byte(key+"\n"), 0o600)
}

func mcpAPIKeyAcceptable(key string) bool {
	key = strings.TrimSpace(key)
	// A manually switched-in key is kept as the user set it; the mck_ prefix
	// only applies to keys this app generates.
	return len(key) >= 8
}

func writeMCPHostEndpoint(port int) error {
	dir := mcpDiscoveryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ep := mcpHostEndpoint{
		URL:       fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		Host:      mcpHostDefaultBind,
		Port:      port,
		PID:       os.Getpid(),
		Protocol:  mcpEndpointProtocol,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, "endpoint.json"), raw, 0o600)
}

func clearMCPHostEndpoint() {
	_ = os.Remove(filepath.Join(mcpDiscoveryDir(), "endpoint.json"))
}

func mcpEndpointPublished(port int) bool {
	if port <= 0 {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(mcpDiscoveryDir(), "endpoint.json"))
	if err != nil {
		return false
	}
	var ep mcpHostEndpoint
	if json.Unmarshal(raw, &ep) != nil || ep.Port != port {
		return false
	}
	_, ok := mcpSafeEndpointURL(ep)
	return ok
}

func readMCPHostDiscovery() (mcpHostEndpoint, string, bool) {
	raw, err := os.ReadFile(filepath.Join(mcpDiscoveryDir(), "endpoint.json"))
	if err != nil {
		return mcpHostEndpoint{}, "", false
	}
	var ep mcpHostEndpoint
	if json.Unmarshal(raw, &ep) != nil || ep.Port <= 0 {
		return mcpHostEndpoint{}, "", false
	}
	safeURL, ok := mcpSafeEndpointURL(ep)
	if !ok {
		return mcpHostEndpoint{}, "", false
	}
	ep.URL = safeURL
	if ep.PID <= 0 || !processAlive(ep.PID) {
		// OpenProcess can fail for a live GUI. Keep the file when the loopback
		// port still accepts connections, and drop it only when nothing is listening.
		if !mcpPortOpen(ep) {
			clearMCPHostEndpoint()
			return mcpHostEndpoint{}, "", false
		}
	}
	key, err := readMCPAPIKey()
	if err != nil || !mcpAPIKeyAcceptable(key) {
		return mcpHostEndpoint{}, "", false
	}
	return ep, key, true
}

func mcpSafeEndpointURL(ep mcpHostEndpoint) (string, bool) {
	if ep.Port <= 0 || ep.Port > 65535 {
		return "", false
	}
	if parsed, err := url.Parse(strings.TrimSpace(ep.URL)); err == nil && mcpHostIsLoopback(parsed.Hostname()) {
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return parsed.String(), true
		}
	}
	host := strings.TrimSpace(ep.Host)
	if host == "" || mcpHostIsLoopback(host) {
		return fmt.Sprintf("http://127.0.0.1:%d/mcp", ep.Port), true
	}
	return "", false
}

func mcpHostIsLoopback(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func mcpPortOpen(ep mcpHostEndpoint) bool {
	if ep.Port <= 0 || ep.Port > 65535 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ep.Port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
