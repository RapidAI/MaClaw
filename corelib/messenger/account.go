// Package messenger is the desktop Facebook Messenger channel.
// It speaks the public Meta Messenger Platform webhook and Send API,
// the same contract as the OpenClaw facebook-messenger plugin:
// GET hub challenge, HMAC-signed POST events, and Graph Send API replies.
package messenger

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	// ChannelID is the plugin id. Desktop delivery uses the shorter "messenger" key.
	ChannelID = "facebook-messenger"
	// DefaultWebhookPath is the callback path Meta is pointed at.
	DefaultWebhookPath = "/facebook-messenger/webhook"
	// DefaultGraphAPIVersion is used when the user leaves the version blank.
	DefaultGraphAPIVersion = "v25.0"
	// DefaultListenHost keeps the webhook on loopback until the user opens it.
	DefaultListenHost = "127.0.0.1"
	// DefaultListenPort is the local listener Meta's tunnel should forward to.
	DefaultListenPort = 8791
	// MessageTextLimit is Meta's per-message text cap.
	MessageTextLimit = 2000
	// MaxAllowFrom bounds the stored sender list.
	MaxAllowFrom = 500

	DMPolicyAllowlist = "allowlist"
	DMPolicyOpen      = "open"
	DMPolicyDisabled  = "disabled"
)

var graphVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)

// Account is the resolved Messenger configuration for one desktop.
type Account struct {
	Enabled         bool
	PageID          string
	PageAccessToken string
	VerifyToken     string
	AppSecret       string
	GraphAPIVersion string
	WebhookPath     string
	ListenHost      string
	ListenPort      int
	AllowFrom       []string
	DMPolicy        string
	DefaultPSID     string
	PublicBaseURL   string
}

// NormalizeWebhookPath accepts an absolute callback path.
// An empty value selects the default. The stored path has no trailing slash.
func NormalizeWebhookPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultWebhookPath, nil
	}
	if !strings.HasPrefix(raw, "/") || strings.Contains(raw, "..") || strings.ContainsAny(raw, " ?#\\") {
		return "", fmt.Errorf("messenger_webhook_path must be an absolute path")
	}
	if len(raw) > 1 {
		raw = strings.TrimRight(raw, "/")
	}
	if raw == "" {
		return DefaultWebhookPath, nil
	}
	return raw, nil
}

// NormalizeListenHost accepts a bind host. Empty selects loopback.
func NormalizeListenHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, " /\\") {
		return "", fmt.Errorf("messenger_listen_host must be a host name or IP")
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	// A single colon is host:port. IPv6 has more than one and is kept.
	if strings.Count(raw, ":") == 1 {
		return "", fmt.Errorf("messenger_listen_host must not include a port")
	}
	if raw == "" {
		return "", fmt.Errorf("messenger_listen_host must be a host name or IP")
	}
	return raw, nil
}

// NormalizeGraphAPIVersion accepts "v25.0" or "25.0". Empty stays empty.
func NormalizeGraphAPIVersion(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.HasPrefix(strings.ToLower(raw), "v") {
		raw = "v" + raw
	}
	raw = "v" + strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	if !graphVersionPattern.MatchString(raw) {
		return "", fmt.Errorf("messenger_graph_api_version must look like v25.0")
	}
	return raw, nil
}

// NormalizeDMPolicy accepts allowlist, open, or disabled. Empty is allowlist.
func NormalizeDMPolicy(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DMPolicyAllowlist:
		return DMPolicyAllowlist, nil
	case DMPolicyOpen:
		return DMPolicyOpen, nil
	case DMPolicyDisabled:
		return DMPolicyDisabled, nil
	default:
		return "", fmt.Errorf("messenger_dm_policy must be allowlist, open, or disabled")
	}
}

// NormalizePublicBaseURL accepts an http(s) origin without a path query.
// Empty means the desktop has no public callback yet.
func NormalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("messenger_public_base_url must be an http(s) URL")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

// NormalizePSID strips messenger, facebook-messenger, and psid prefixes.
func NormalizePSID(raw string) string {
	raw = strings.TrimSpace(raw)
	for {
		lower := strings.ToLower(raw)
		stripped := false
		for _, prefix := range []string{"facebook-messenger:", "messenger:", "psid:"} {
			if strings.HasPrefix(lower, prefix) {
				raw = strings.TrimSpace(raw[len(prefix):])
				stripped = true
				break
			}
		}
		if !stripped {
			return raw
		}
	}
}

// NormalizeAllowFrom trims, strips prefixes, and drops duplicates.
func NormalizeAllowFrom(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = NormalizePSID(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
		if len(out) >= MaxAllowFrom {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Normalized fills defaults and returns a copy safe to retain.
func (a Account) Normalized() Account {
	a.PageID = strings.TrimSpace(a.PageID)
	a.PageAccessToken = strings.TrimSpace(a.PageAccessToken)
	a.VerifyToken = strings.TrimSpace(a.VerifyToken)
	a.AppSecret = strings.TrimSpace(a.AppSecret)
	a.DefaultPSID = NormalizePSID(a.DefaultPSID)
	if version, err := NormalizeGraphAPIVersion(a.GraphAPIVersion); err == nil {
		a.GraphAPIVersion = version
	}
	if a.GraphAPIVersion == "" {
		a.GraphAPIVersion = DefaultGraphAPIVersion
	}
	if path, err := NormalizeWebhookPath(a.WebhookPath); err == nil {
		a.WebhookPath = path
	} else {
		a.WebhookPath = DefaultWebhookPath
	}
	if host, err := NormalizeListenHost(a.ListenHost); err == nil {
		a.ListenHost = host
	} else {
		a.ListenHost = ""
	}
	if a.ListenHost == "" {
		a.ListenHost = DefaultListenHost
	}
	if a.ListenPort < 1 || a.ListenPort > 65535 {
		a.ListenPort = DefaultListenPort
	}
	if policy, err := NormalizeDMPolicy(a.DMPolicy); err == nil {
		a.DMPolicy = policy
	} else {
		a.DMPolicy = DMPolicyAllowlist
	}
	if base, err := NormalizePublicBaseURL(a.PublicBaseURL); err == nil {
		a.PublicBaseURL = base
	}
	a.AllowFrom = NormalizeAllowFrom(a.AllowFrom)
	return a
}

// CanRun reports whether the webhook can verify and the Send API can reply.
func (a Account) CanRun() bool {
	a = a.Normalized()
	return a.Enabled && a.PageID != "" && a.PageAccessToken != "" && a.VerifyToken != "" && a.AppSecret != ""
}

// ListenKey changes when the TCP listener must be rebound.
// The webhook path is checked on each request and does not rebind the port.
func (a Account) ListenKey() string {
	a = a.Normalized()
	return net.JoinHostPort(a.ListenHost, strconv.Itoa(a.ListenPort))
}

// LocalWebhookURL is the loopback URL a tunnel should forward to.
func (a Account) LocalWebhookURL() string {
	a = a.Normalized()
	host := a.ListenHost
	if host == "0.0.0.0" || host == "::" {
		host = DefaultListenHost
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(a.ListenPort)) + a.WebhookPath
}

// CallbackURL is the URL to paste into Meta. A public base wins over loopback.
// A base that already ends with the webhook path is not appended twice.
func (a Account) CallbackURL() string {
	a = a.Normalized()
	if a.PublicBaseURL != "" {
		if strings.HasSuffix(a.PublicBaseURL, a.WebhookPath) {
			return a.PublicBaseURL
		}
		return a.PublicBaseURL + a.WebhookPath
	}
	return a.LocalWebhookURL()
}

// SenderAllowed applies dmPolicy to one PSID.
func (a Account) SenderAllowed(senderID string) bool {
	a = a.Normalized()
	switch a.DMPolicy {
	case DMPolicyDisabled:
		return false
	case DMPolicyOpen:
		return true
	}
	sender := strings.ToLower(NormalizePSID(senderID))
	if sender == "" {
		return false
	}
	for _, entry := range a.AllowFrom {
		if entry == "*" || strings.ToLower(entry) == sender {
			return true
		}
	}
	return false
}
