package clientsecurity

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func EnforceConfig(cfg corelib.AppConfig, name string, args map[string]interface{}) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if IsDeveloperMode(cfg) && isSkillInstallTool(name, args) {
		return true, ""
	}
	if ok, reason := enforceSkillSourcePolicy(cfg.SkillSourcesAllowed, name, args); !ok {
		return false, reason
	}
	if !cfg.HubSecurityCentralized {
		return true, ""
	}
	if (name == "send_file" || name == "send_to_im") && !cfg.FileOutboundEnabled {
		return false, "file outbound is disabled by Hub security policy"
	}
	// Proactive IM text send is outbound messaging; reuse file-outbound gate.
	// Use intent inference so omitting action=send cannot bypass the policy.
	if name == "im_message" && scheduler.IsIMMessageSendIntent(args) && !cfg.FileOutboundEnabled {
		return false, "IM message outbound is disabled by Hub security policy"
	}
	if isImageOutboundTool(name) && !cfg.ImageOutboundEnabled {
		return false, "image outbound is disabled by Hub security policy"
	}
	if sandbox := strings.ToLower(strings.TrimSpace(cfg.SandboxMode)); name == "bash" && sandbox != "" && sandbox != "none" {
		return false, fmt.Sprintf("bash requires %s sandbox, but this client path cannot guarantee sandbox enforcement", sandbox)
	}
	if ok, reason := enforceNetworkLevel(strings.ToLower(strings.TrimSpace(cfg.NetworkLevel)), cfg.NetworkAllowlist, name, argsWithInferredSkillSourceEndpoint(name, args)); !ok {
		return false, reason
	}
	return true, ""
}

func IsDeveloperMode(cfg corelib.AppConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.SecurityPolicyMode), "developer")
}

func IsHubManagedSecurityConfigKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "hub_security_centralized",
		"security_profile",
		"security_policy_mode",
		"sandbox_mode",
		"network_level",
		"network_allowlist",
		"yolo_mode_allowed",
		"smart_route_enabled",
		"gossip_enabled",
		"file_outbound_enabled",
		"image_outbound_enabled",
		"skill_sources_allowed":
		return true
	default:
		return false
	}
}

func RejectHubManagedSecurityConfigChange(current corelib.AppConfig, key string) (bool, string) {
	if !current.HubSecurityCentralized || !IsHubManagedSecurityConfigKey(key) {
		return false, ""
	}
	return true, "security setting is managed by Hub policy"
}

func PreserveHubManagedSecurityConfig(current corelib.AppConfig, next *corelib.AppConfig) {
	if next == nil || !current.HubSecurityCentralized {
		return
	}
	next.HubSecurityCentralized = current.HubSecurityCentralized
	next.SecurityPolicyMode = current.SecurityPolicyMode
	next.SandboxMode = current.SandboxMode
	next.NetworkLevel = current.NetworkLevel
	next.NetworkAllowlist = append([]string(nil), current.NetworkAllowlist...)
	next.YoloModeAllowed = current.YoloModeAllowed
	next.SmartRouteEnabled = current.SmartRouteEnabled
	next.GossipEnabled = current.GossipEnabled
	next.FileOutboundEnabled = current.FileOutboundEnabled
	next.ImageOutboundEnabled = current.ImageOutboundEnabled
	next.SkillSourcesAllowed = append([]string(nil), current.SkillSourcesAllowed...)
}

func isImageOutboundTool(name string) bool {
	return name == "screenshot" || strings.Contains(name, "screenshot")
}

func enforceNetworkLevel(level string, allowlist []string, name string, args map[string]interface{}) (bool, string) {
	if level == "" || level == "full" {
		return true, ""
	}
	if !isNetworkTool(name) && !skillInstallNeedsNetwork(name, args) && !toolArgsContainURL(args) && !toolArgsContainHost(args) && !bashCommandLooksNetworked(name, args) {
		return true, ""
	}
	if level == "none" {
		return false, "network access is disabled by Hub security policy"
	}
	if level == "intranet" || level == "allowlist" {
		if toolArgsAreAllowedByNetworkPolicy(level, allowlist, args) && !bashCommandLooksNetworked(name, args) {
			return true, ""
		}
		return false, fmt.Sprintf("%s network access is restricted by Hub security policy", level)
	}
	return true, ""
}

func isNetworkTool(name string) bool {
	switch name {
	case "web_search", "web_fetch", "knowledge_save_url", "open", "search_and_install_skill", "ssh":
		return true
	case "manage_skill":
		return false
	default:
		return strings.HasPrefix(name, "browser") || strings.Contains(name, "web_")
	}
}

func enforceSkillSourcePolicy(allowedSources []string, name string, args map[string]interface{}) (bool, string) {
	if len(allowedSources) == 0 || !isSkillInstallTool(name, args) {
		return true, ""
	}
	if skillSourcesBlockAll(allowedSources) {
		return false, skill.FormatSourcePolicyDenied(effectiveSkillSource(args), nil)
	}
	if skillSearchSourceIsDeferred(name, args) {
		return true, ""
	}
	source := normalizeSkillSource(effectiveSkillSource(args))
	if source == "" {
		source = "skillhub"
	}
	for _, allowed := range allowedSources {
		if strings.EqualFold(normalizeSkillSource(allowed), source) {
			return true, ""
		}
	}
	return false, skill.FormatSourcePolicyDenied(source, allowedSources)
}

func skillSourcesBlockAll(allowedSources []string) bool {
	return len(allowedSources) == 1 && strings.EqualFold(strings.TrimSpace(allowedSources[0]), "__none__")
}

func skillSearchSourceIsDeferred(name string, args map[string]interface{}) bool {
	if explicitSkillSource(args) {
		return false
	}
	if name == "search_and_install_skill" {
		return true
	}
	if name == "manage_skill" {
		action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
		return action == "search"
	}
	return false
}

func argsWithInferredSkillSourceEndpoint(name string, args map[string]interface{}) map[string]interface{} {
	if !isSkillInstallTool(name, args) || len(urlsFromAny(args)) > 0 || len(hostsFromAny(args)) > 0 {
		return args
	}
	endpoint := ""
	switch normalizeSkillSource(effectiveSkillSource(args)) {
	case "github":
		endpoint = "https://github.com"
	case "clawhub":
		endpoint = "https://cn.clawhub-mirror.com"
	}
	if endpoint == "" {
		return args
	}
	copyArgs := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		copyArgs[k] = v
	}
	copyArgs["url"] = endpoint
	return copyArgs
}

func explicitSkillSource(args map[string]interface{}) bool {
	if normalizeSkillSource(stringArg(args, "source")) != "" {
		return true
	}
	return strings.TrimSpace(firstStringArg(args, "hub_url", "install_ref", "url", "repo_url", "raw_url", "repo_full_name")) != "" || strings.TrimSpace(stringArg(args, "zip_base64")) != ""
}

func isSkillInstallTool(name string, args map[string]interface{}) bool {
	switch name {
	case "search_and_install_skill", "install_skill_hub":
		return true
	case "manage_skill":
		action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
		return action == "install" || action == "update" || action == "search"
	default:
		return false
	}
}

func skillInstallNeedsNetwork(name string, args map[string]interface{}) bool {
	if !isSkillInstallTool(name, args) {
		return false
	}
	return normalizeSkillSource(effectiveSkillSource(args)) != "local"
}

func effectiveSkillSource(args map[string]interface{}) string {
	source := normalizeSkillSource(stringArg(args, "source"))
	if source != "" {
		return source
	}
	if strings.TrimSpace(stringArg(args, "zip_base64")) != "" {
		return "local"
	}
	if strings.TrimSpace(firstStringArg(args, "repo_url", "raw_url", "repo_full_name")) != "" {
		return "github"
	}
	hubURL := strings.ToLower(strings.TrimSpace(firstStringArg(args, "hub_url", "install_ref", "url")))
	switch {
	case hubURL == "github" || strings.Contains(hubURL, "github.com"):
		return "github"
	case strings.Contains(hubURL, "clawhub"):
		return "clawhub"
	case hubURL != "":
		return "skillhub"
	default:
		return "skillhub"
	}
}

func normalizeSkillSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "enterprise", "hub", "enterprisehub":
		return "enterprise_hub"
	case "claw_hub":
		return "clawhub"
	case "git_hub":
		return "github"
	case "zip", "local_upload":
		return "local"
	default:
		return source
	}
}

func bashCommandLooksNetworked(name string, args map[string]interface{}) bool {
	if strings.ToLower(strings.TrimSpace(name)) != "bash" {
		return false
	}
	cmd := normalizeCommandForNetworkScan(stringArg(args, "command"))
	if strings.Contains(cmd, "http://") || strings.Contains(cmd, "https://") {
		return true
	}
	for _, marker := range []string{" curl ", " curl.exe ", " wget ", " wget.exe ", " invoke-webrequest ", " iwr ", " irm ", " invoke-restmethod ", " ssh ", " scp ", " sftp ", " rsync ", " ftp ", " telnet ", " nc ", " ncat ", " netcat ", " ping ", " nslookup ", " dig ", " git clone ", " gh repo clone ", " npm install ", " npm view ", " pip install ", " go get "} {
		if strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

func normalizeCommandForNetworkScan(command string) string {
	cmd := strings.ToLower(strings.TrimSpace(command))
	for _, sep := range []string{"\r", "\n", "\t", ";", "&&", "||", "|", "(", ")", "<", ">"} {
		cmd = strings.ReplaceAll(cmd, sep, " ")
	}
	return " " + strings.Join(strings.Fields(cmd), " ") + " "
}

func firstStringArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := stringArg(args, key); value != "" {
			return value
		}
	}
	return ""
}

func toolArgsContainURL(args map[string]interface{}) bool {
	return len(urlsFromAny(args)) > 0
}

func toolArgsContainHost(args map[string]interface{}) bool {
	return len(hostsFromAny(args)) > 0
}

func toolArgsAreAllowedByNetworkPolicy(level string, allowlist []string, args map[string]interface{}) bool {
	urls := urlsFromAny(args)
	hosts := hostsFromAny(args)
	if len(urls) == 0 && len(hosts) == 0 {
		return false
	}
	for _, s := range urls {
		if level == "allowlist" {
			if !isAllowlistedURL(s, allowlist) {
				return false
			}
			continue
		}
		if !isIntranetURL(s) {
			return false
		}
	}
	for _, host := range hosts {
		if level == "allowlist" {
			if !isAllowlistedHost(host, allowlist) {
				return false
			}
			continue
		}
		if !isIntranetHost(host) {
			return false
		}
	}
	return true
}

func urlsFromAny(value interface{}) []string {
	switch v := value.(type) {
	case map[string]interface{}:
		var urls []string
		for _, item := range v {
			urls = append(urls, urlsFromAny(item)...)
		}
		return urls
	case []interface{}:
		var urls []string
		for _, item := range v {
			urls = append(urls, urlsFromAny(item)...)
		}
		return urls
	case []string:
		var urls []string
		for _, item := range v {
			urls = append(urls, urlsFromString(item)...)
		}
		return urls
	case string:
		return urlsFromString(v)
	}
	return nil
}

var embeddedHTTPURLRe = regexp.MustCompile(`https?://[^\s"'<>\\]+`)

func urlsFromString(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if looksHTTPURL(value) {
		return []string{value}
	}
	return embeddedHTTPURLRe.FindAllString(value, -1)
}

func looksHTTPURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://")
}

func hostsFromAny(value interface{}) []string {
	switch v := value.(type) {
	case map[string]interface{}:
		var hosts []string
		for key, item := range v {
			if isHostArgKey(key) {
				switch host := item.(type) {
				case string:
					if strings.TrimSpace(host) != "" {
						hosts = append(hosts, host)
					}
				case []string:
					for _, h := range host {
						if strings.TrimSpace(h) != "" {
							hosts = append(hosts, h)
						}
					}
				}
			}
			hosts = append(hosts, hostsFromAny(item)...)
		}
		return hosts
	case []interface{}:
		var hosts []string
		for _, item := range v {
			hosts = append(hosts, hostsFromAny(item)...)
		}
		return hosts
	}
	return nil
}

func isHostArgKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "host", "hostname", "server", "address", "endpoint_host", "remote_host":
		return true
	default:
		return false
	}
}

func isIntranetURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return false
	}
	return isIntranetHost(u.Hostname())
}

func isIntranetHost(raw string) bool {
	host := normalizeHost(raw)
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func isAllowlistedURL(raw string, allowlist []string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return false
	}
	return isAllowlistedHost(u.Hostname(), allowlist)
}

func isAllowlistedHost(raw string, allowlist []string) bool {
	host := normalizeHost(raw)
	if host == "" {
		return false
	}
	for _, item := range allowlist {
		pattern := normalizeHost(item)
		if pattern == "" {
			continue
		}
		if pattern == host || (strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:])) {
			return true
		}
	}
	return false
}

func normalizeHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Hostname()
		}
	}
	if strings.Contains(host, "@") {
		host = host[strings.LastIndex(host, "@")+1:]
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	} else if strings.Count(host, ":") == 1 {
		host = strings.Split(host, ":")[0]
	}
	return strings.Trim(host, "[]")
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}
