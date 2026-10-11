package clientsecurity

import (
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func EnforceConfig(cfg corelib.AppConfig, name string, args map[string]interface{}) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	// Developer mode exempts a skill install from the *source allowlist* — that
	// is the whole point of the mode, and SkillSourcesAllowed is a local
	// convenience list rather than a network boundary.
	//
	// It deliberately does not return early. This used to `return true, ""`
	// here, which also skipped every rule below it, so a skill install became
	// the one tool that ignored network_level: with the level set to "none" —
	// the org-wide shutdown — `search_and_install_skill` and
	// `manage_skill action=install` still reached clawhub/github, while
	// web_fetch was correctly refused. Restricting where skills come from and
	// whether the process may touch the network are different questions, and
	// the exemption is only ever meant to answer the first one. This matters
	// beyond a local toggle because SecurityPolicyMode is Hub-managed
	// (guardrail_mode accepts "developer"), so relaxing skill sources from the
	// Hub used to silently lift the network policy for those tools too.
	developerSkillInstall := IsDeveloperMode(cfg) && isSkillInstallTool(name, args)
	if !developerSkillInstall {
		if ok, reason := enforceSkillSourcePolicy(cfg.SkillSourcesAllowed, name, args); !ok {
			return false, reason
		}
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

// NameLevelRejection reports whether EnforceConfig rejects the tool for every
// possible argument set under cfg. Only name-only, config-static rules qualify
// (bash under a mandated sandbox, file/image outbound while disabled); rules
// that depend on arguments (network level, skill sources, im_message send
// intent) never report a rejection here. Callers use this to keep guaranteed-
// rejected tools out of the rendered model surface; EnforceConfig remains the
// execution-time defense and is unchanged.
func NameLevelRejection(cfg corelib.AppConfig, name string) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !cfg.HubSecurityCentralized {
		return false, ""
	}
	if sandbox := strings.ToLower(strings.TrimSpace(cfg.SandboxMode)); name == "bash" && sandbox != "" && sandbox != "none" {
		return true, fmt.Sprintf("bash requires %s sandbox, but this client path cannot guarantee sandbox enforcement", sandbox)
	}
	if (name == "send_file" || name == "send_to_im") && !cfg.FileOutboundEnabled {
		return true, "file outbound is disabled by Hub security policy"
	}
	if isImageOutboundTool(name) && !cfg.ImageOutboundEnabled {
		return true, "image outbound is disabled by Hub security policy"
	}
	return false, ""
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

// NormalizeNetworkLevel canonicalizes a network level string and reports
// whether it names a level this client understands. It is the single
// definition of the legal set: the execution gate and the config ingest path
// both resolve levels through it, so the two cannot drift apart.
//
// An empty value means "not configured" and normalizes to "full", which is the
// product default (AppConfig documents it, and the load path backfills it).
func NormalizeNetworkLevel(raw string) (string, bool) {
	level := strings.ToLower(strings.TrimSpace(raw))
	switch level {
	case "":
		// "not configured" is the documented default, not a missing policy.
		return "full", true
	case "full", "none", "intranet", "allowlist":
		return level, true
	default:
		return "", false
	}
}

// KnownNetworkLevels lists the accepted network levels, for validation errors
// and for callers that need to tell a typo from a real policy.
func KnownNetworkLevels() []string {
	return []string{"full", "none", "intranet", "allowlist"}
}

func isImageOutboundTool(name string) bool {
	return name == "screenshot" || strings.Contains(name, "screenshot")
}

func enforceNetworkLevel(level string, allowlist []string, name string, args map[string]interface{}) (bool, string) {
	rawLevel := level
	level, known := NormalizeNetworkLevel(level)
	if level == "full" {
		return true, ""
	}
	// Both walks cover the whole argument tree and the payload can be large —
	// a document body, a script — so they are scanned once and reused below.
	hasEndpoint := toolArgsContainNetworkEndpoint(args) || toolArgsContainHost(args)
	networkedCommand := bashCommandLooksNetworked(name, args)

	// Handoff is settled in one place. Its two halves used to sit on opposite
	// sides of the level checks and read as two unrelated rules, which is how
	// "local file" and "URL the user asked for" ended up with the same answer
	// for different reasons.
	if isUserVisibleHandoffTool(name) {
		// A local path or application is a local action that merely shares a
		// name with a network-capable tool. Scoping it by network policy
		// blocked `open /path/to/report.pdf` under every restricted level,
		// which no network policy ever intended to do.
		if !hasEndpoint {
			return true, ""
		}
		// A URL target is where the user is being sent, not what the agent
		// fetches, so a domain scope does not apply to it. Only a policy that
		// disables networking outright closes it. An unrecognized level is
		// not such a policy, so it falls in with the permissive side here
		// rather than pretending to be an organization-wide shutdown.
		if level != "none" {
			return true, ""
		}
		return false, "network access is disabled by Hub security policy"
	}
	if !isNetworkTool(name) && !skillInstallNeedsNetwork(name, args) && !hasEndpoint && !networkedCommand {
		return true, ""
	}
	// Past this point the call does reach the network, so the level applies.
	//
	// An unrecognized level is refused here rather than at the top of the
	// function: a typo, or a value written by a newer Hub, used to fall
	// through every branch and reach the "no restriction" ending below,
	// silently granting full network access. Refusing it only at the point
	// where a network call is actually being judged keeps a malformed policy
	// from bricking local tools too — the same scoping level "none" itself
	// has, since "none" denies network calls while leaving read_file and
	// friends alone.
	//
	// This has to live at the execution gate, not at config ingest, because
	// effectiveHubSecurityConfig hands the Hub's live cache straight to
	// EnforceConfig without passing through persistHubSecurityPolicy.
	if !known {
		return false, fmt.Sprintf("network access is denied: Hub security policy sets network_level to the unrecognized value %q (valid levels: %s)", strings.TrimSpace(rawLevel), strings.Join(KnownNetworkLevels(), ", "))
	}
	if level == "none" {
		return false, "network access is disabled by Hub security policy"
	}
	if level == "intranet" || level == "allowlist" {
		if toolArgsAreAllowedByNetworkPolicy(level, allowlist, args) && !networkedCommand {
			return true, ""
		}
		if level == "allowlist" && len(allowlist) == 0 {
			// An allowlist with no entry blocks every domain, which is
			// indistinguishable from level "none" at execution time but reads
			// as a domain decision. Say so, or the operator cannot tell
			// "that domain is not allowed" from "no domain was ever allowed".
			return false, "allowlist network access is restricted by Hub security policy: the policy enables allowlist mode but lists no allowed domain"
		}
		return false, fmt.Sprintf("%s network access is restricted by Hub security policy", level)
	}
	return true, ""
}

// isNetworkTool reports whether a tool performs network I/O inside the agent.
// It lists the silent channels only. User-visible handoff tools are absent on
// purpose: `open` reaches a URL through the operating system handler, which is
// the opposite of a silent request, and enforceNetworkLevel settles it in its
// own branch before this list is consulted. Leaving it here would have kept
// saying the opposite of what the code does, for the next reader to trust.
func isNetworkTool(name string) bool {
	switch name {
	case "web_search", "web_fetch", "download_file", "knowledge_save_url", "search_and_install_skill", "ssh":
		return true
	case "manage_skill":
		return false
	default:
		return strings.HasPrefix(name, "browser") || strings.Contains(name, "web_")
	}
}

// isUserVisibleHandoffTool reports tools that hand their target to the user's
// own system handler instead of performing the I/O inside the agent.
//
// The network scope exists to bound *silent* egress: a request the agent makes
// on its own, whose payload flows back into the model context. A handoff tool
// is not that. The destination opens in the user's own browser or editor, the
// user sees it happen, and no response body is fetched back into the agent.
//
// Lower risk, not zero risk, and the difference is worth stating plainly: an
// agent can still encode context into a URL query and have the browser open it
// — ToolOpen starts the handler with no prompt — so a handoff stays a one-way,
// URL-sized outbound channel. It is accepted here because the alternative,
// enumerating every domain a user might legitimately be pointed at, is exactly
// what broke this, and because the exposure is bounded by what fits in a URL
// while web_fetch moves whole response bodies silently in both directions.
// Level "none" still closes it: an organization that says no network means it.
//
// Before 2026-10-11 `open` was gated like web_fetch, so allowlist mode
// rejected it for every target, including local file paths. Meanwhile the
// in-product browser — whose snapshot really does feed page content back to
// the model — kept working because it is dispatched through a host path that
// never reaches this gate. The gate was inversely correlated with the risk it
// exists to bound.
func isUserVisibleHandoffTool(name string) bool {
	return name == "open"
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

// toolArgsContainNetworkEndpoint reports whether args carry a URL where the
// tool would plausibly consume it as a network endpoint: the argument key
// designates an endpoint, the whole value is itself a URL, or the value is a
// command/script containing a URL. URLs merely embedded in prose payloads
// (document content, report text, message bodies) are data, not network
// targets — scanning them false-rejects purely offline tools such as
// generate_pdf under the intranet/allowlist network levels.
func toolArgsContainNetworkEndpoint(args map[string]interface{}) bool {
	return valueContainsNetworkEndpoint(args, "")
}

func valueContainsNetworkEndpoint(value interface{}, key string) bool {
	found := false
	walkContainer(value, func(item interface{}, itemKey string) {
		if found {
			return
		}
		if s, ok := item.(string); ok {
			// The key used for the leaf is whichever enclosing key named it; a
			// bare value reached without one carries the caller's key so a top
			// level string argument keeps its own argument name.
			probeKey := itemKey
			if probeKey == "" {
				probeKey = key
			}
			if stringArgLooksNetworkEndpoint(probeKey, s) {
				found = true
			}
		}
	})
	return found
}

func stringArgLooksNetworkEndpoint(key, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !strings.Contains(strings.ToLower(value), "http") {
		return false
	}
	if looksHTTPURL(value) {
		return true
	}
	if isEndpointArgKey(key) || isExecArgKey(key) {
		return len(embeddedHTTPURLRe.FindAllString(value, -1)) > 0
	}
	return false
}

func isEndpointArgKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "url", "urls", "uri", "endpoint", "link", "links", "href", "src", "target",
		"image_url", "avatar_url", "download_url", "file_url", "base_url",
		"repo_url", "raw_url", "hub_url", "install_ref", "webhook", "webhook_url",
		"callback_url", "api_url", "icon_url", "logo_url", "audio_url", "video_url",
		"media_url", "page_url", "site", "homepage", "feed", "feed_url":
		return true
	default:
		return false
	}
}

func isExecArgKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "command", "cmd", "script":
		return true
	default:
		return false
	}
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

// walkContainer yields every leaf together with the argument key it was found
// under, descending through maps and slices of ANY element type.
//
// The previous callers enumerated static slice types ([]interface{},
// []string). That silently decided security by Go static type: a hostile URL
// nested in []map[string]interface{} was invisible to the scanner while the
// identical payload in []interface{} was caught -- verified by probe, the two
// forms returned opposite verdicts under network_level=none for the same
// intent. Nothing guaranteed which form reaches the gate; it depended on how a
// caller happened to build its arguments rather than on JSON decoding.
// Reflecting over every container removes the distinction, so the verdict
// depends on the payload alone.
//
// key is the nearest enclosing map key. It is propagated into nested slices
// because endpoint meaning attaches to the argument name, and a value reached
// through a slice still belongs to that argument.
func walkContainer(value interface{}, visit func(value interface{}, key string)) {
	walkContainerDepth(value, visit, "", 0)
}

// walkContainerDepth bounds recursion so a self-referential structure cannot
// exhaust the stack inside a security gate. Real tool arguments nest a handful
// of levels, and anything deeper is malformed rather than meaningful.
func walkContainerDepth(value interface{}, visit func(interface{}, string), key string, depth int) {
	const maxDepth = 32
	if value == nil || depth > maxDepth {
		return
	}
	visit(value, key)
	switch v := value.(type) {
	case map[string]interface{}:
		for k, item := range v {
			walkContainerDepth(item, visit, k, depth+1)
		}
	case []interface{}:
		for _, item := range v {
			walkContainerDepth(item, visit, key, depth+1)
		}
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Map:
			if !rv.Type().Key().Comparable() {
				return
			}
			iter := rv.MapRange()
			for iter.Next() {
				nextKey := key
				if k, ok := iter.Key().Interface().(string); ok {
					nextKey = k
				}
				walkContainerDepth(iter.Value().Interface(), visit, nextKey, depth+1)
			}
		case reflect.Slice, reflect.Array:
			// A non-interface element is still readable through Interface();
			// anything genuinely unreadable would panic rather than hide data,
			// and no tool argument path produces such a value.
			for i := 0; i < rv.Len(); i++ {
				walkContainerDepth(rv.Index(i).Interface(), visit, key, depth+1)
			}
		}
	}
}

func urlsFromAny(value interface{}) []string {
	switch v := value.(type) {
	case string:
		return urlsFromString(v)
	}
	// Any container shape is walked reflectively so a nested URL can never
	// escape the scan by arriving in an unlisted slice/map type.
	var urls []string
	walkContainer(value, func(item interface{}, key string) {
		if s, ok := item.(string); ok {
			urls = append(urls, urlsFromString(s)...)
		}
	})
	return urls
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
	// Every container shape is walked reflectively so a host carrying key can
	// never escape the scan by arriving in an unlisted slice/map type. The
	// key-based predicate is preserved: only values parked under a host-like
	// name count, which is what keeps this distinct from the URL scanner.
	var hosts []string
	walkContainer(value, func(item interface{}, key string) {
		switch typed := item.(type) {
		case map[string]interface{}:
			collectHostArgValues(typed, &hosts)
		case map[string]string:
			for k, val := range typed {
				if isHostArgKey(k) && strings.TrimSpace(val) != "" {
					hosts = append(hosts, val)
				}
			}
		}
	})
	return hosts
}

// collectHostArgValues appends values of host-carrying keys found in one map.
func collectHostArgValues(in map[string]interface{}, hosts *[]string) {
	for key, item := range in {
		if !isHostArgKey(key) {
			continue
		}
		switch host := item.(type) {
		case string:
			if strings.TrimSpace(host) != "" {
				*hosts = append(*hosts, host)
			}
		case []string:
			for _, h := range host {
				if strings.TrimSpace(h) != "" {
					*hosts = append(*hosts, h)
				}
			}
		case []interface{}:
			for _, h := range host {
				if s, ok := h.(string); ok && strings.TrimSpace(s) != "" {
					*hosts = append(*hosts, s)
				}
			}
		}
	}
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
