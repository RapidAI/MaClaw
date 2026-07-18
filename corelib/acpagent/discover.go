package acpagent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	DefaultGatewayPort    = 18777
	DefaultGatewayBaseURL = "http://127.0.0.1:18777/api/im-gateway/v1"
	DefaultClientID       = "maclaw-acp-bridge"
)

// GatewayEndpoint is a discovered IM Gateway attachment target.
type GatewayEndpoint struct {
	BaseURL    string
	Token      string
	ConfigPath string
	OK         bool // true when enabled+token found in config
}

// DiscoverGateway loads GUI AppConfig and builds the gateway base URL + token.
// configPath empty → <MaclawBaseDir>/config.json (or MACLAW_DATA_DIR).
// Env overrides: MACLAW_GATEWAY_URL, MACLAW_GATEWAY_TOKEN, MACLAW_CONFIG.
func DiscoverGateway(configPath string) GatewayEndpoint {
	if p := strings.TrimSpace(os.Getenv("MACLAW_CONFIG")); p != "" && strings.TrimSpace(configPath) == "" {
		configPath = p
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(dataDir(), "config.json")
	}
	out := GatewayEndpoint{ConfigPath: configPath, BaseURL: DefaultGatewayBaseURL}

	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		var cfg corelib.AppConfig
		if json.Unmarshal(data, &cfg) == nil {
			host := gatewayClientHost(cfg.ThirdPartyGatewayHost)
			port := cfg.ThirdPartyGatewayPort
			if port <= 0 {
				port = DefaultGatewayPort
			}
			out.BaseURL = fmt.Sprintf("http://%s/api/im-gateway/v1", netJoinHostPort(host, port))
			out.Token = strings.TrimSpace(cfg.ThirdPartyGatewayToken)
			out.OK = cfg.ThirdPartyGatewayEnabled && out.Token != ""
		}
	}

	if u := strings.TrimSpace(os.Getenv("MACLAW_GATEWAY_URL")); u != "" {
		out.BaseURL = strings.TrimRight(u, "/")
	}
	if t := strings.TrimSpace(os.Getenv("MACLAW_GATEWAY_TOKEN")); t != "" {
		out.Token = t
		out.OK = true
	}
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	return out
}

func dataDir() string {
	if d := strings.TrimSpace(os.Getenv("MACLAW_DATA_DIR")); d != "" {
		return d
	}
	return corelib.MaclawBaseDir()
}

func gatewayClientHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "127.0.0.1"
	}
	return host
}

func netJoinHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		// IPv6 bare address
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
