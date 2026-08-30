package knowledge

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// ValidatePublicHTTPURL normalizes a user-provided URL and rejects obvious
// local/private targets before any fetch is attempted. DNS rebinding-safe
// resolution is handled by the future fetcher; this guard covers direct hosts.
func ValidatePublicHTTPURL(raw string) (*url.URL, error) {
	value := websearch.CanonicalFetchURL(raw)
	if value == "" {
		return nil, fmt.Errorf("empty URL")
	}
	u, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	if IsBlockedHost(u.Hostname()) {
		return nil, fmt.Errorf("URL host %q is not public", u.Hostname())
	}
	u.Fragment = ""
	return u, nil
}

func IsBlockedHost(host string) bool {
	return websearch.IsBlockedPublicHost(host)
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return isPrivateIPv4(ip4)
	}
	return isPrivateIPv6(ip)
}

func isPrivateIPv4(ip net.IP) bool {
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168) ||
		(ip[0] == 169 && ip[1] == 254) ||
		(ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127) ||
		(ip[0] == 127) ||
		(ip[0] == 0) ||
		(ip[0] >= 224) ||
		(ip[0] == 198 && (ip[1] == 18 || ip[1] == 19))
}

func isPrivateIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	return ip.IsPrivate() ||
		ip.IsLoopback() ||
		strings.HasPrefix(strings.ToLower(ip.String()), "fc") ||
		strings.HasPrefix(strings.ToLower(ip.String()), "fd")
}
