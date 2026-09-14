package factclaim

import (
	"encoding/json"
	"net"
	"strings"
)

// BindToolSubject returns the identity a tool observation is about.
//
// Principle: the subject must be named in the invocation and mentioned in
// the evidence. Bind addresses, comments, later pipeline hosts, JSON notes,
// and gateway IPs fall out because they are not in both sets. A
// parenthetical IP after a bound hostname is an alias of that subject
// (name resolution), not a second probe.
func BindToolSubject(toolName, argsJSON, evidence string) (primary string, aliases []string, ok bool) {
	named := invocationSubjects(toolName, argsJSON)
	if len(named) == 0 {
		return "", nil, false
	}
	mentioned := map[string]struct{}{}
	for _, e := range Entities(evidence) {
		mentioned[e] = struct{}{}
	}
	var bound []string
	seen := map[string]struct{}{}
	for _, e := range named {
		if _, ok := mentioned[e]; !ok {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		bound = append(bound, e)
	}
	if len(bound) == 0 {
		return "", nil, false
	}
	primary = preferLastMentioned(bound, evidence)
	for _, e := range bound {
		if e != primary {
			aliases = appendUnique(aliases, e)
		}
	}
	if res := resolvedAddress(primary, evidence); res != "" && res != primary {
		aliases = appendUnique(aliases, res)
	}
	return primary, aliases, true
}

// BindTimeoutSubject names the probe when evidence is empty. A structured
// host field is explicit; otherwise only a single invocation identity is
// unambiguous.
func BindTimeoutSubject(toolName, argsJSON string) (primary string, aliases []string, ok bool) {
	named := invocationSubjects(toolName, argsJSON)
	if len(named) != 1 {
		return "", nil, false
	}
	return named[0], nil, true
}

func invocationSubjects(toolName, argsJSON string) []string {
	cmd := probeCommand(stringArg(argsJSON, "command", "cmd", "script"))
	if LooksLikeProbe(cmd) {
		return Entities(cmd)
	}
	if LooksLikeProbe(toolName) {
		if host := structuredHost(argsJSON); host != "" {
			return []string{host}
		}
	}
	return nil
}

func probeCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if i := strings.Index(cmd, "\n"); i >= 0 {
		cmd = cmd[:i]
	}
	if i := strings.Index(cmd, "#"); i >= 0 {
		cmd = strings.TrimSpace(cmd[:i])
	}
	for _, sep := range []string{"&&", "||"} {
		if i := strings.Index(cmd, sep); i >= 0 {
			cmd = strings.TrimSpace(cmd[:i])
		}
	}
	if i := strings.IndexAny(cmd, ";|"); i >= 0 {
		cmd = strings.TrimSpace(cmd[:i])
	}
	return cmd
}

func isProbe(toolName, argsJSON string) bool {
	return LooksLikeProbe(toolName) || LooksLikeProbe(stringArg(argsJSON, "command", "cmd", "script"))
}

// LooksLikeProbeInvocation reports whether the args name a network probe:
// an explicit host field, or a command that is itself a probe.
func LooksLikeProbeInvocation(toolName, argsJSON string) bool {
	return isProbe(toolName, argsJSON)
}

// LooksLikeProbe reports whether the tool name or command *program* is a
// network probe. Mentions of ping in comments or later pipeline steps do not
// count; only the process being launched does.
func LooksLikeProbe(commandOrTool string) bool {
	name := strings.ToLower(strings.TrimSpace(commandOrTool))
	if name == "" {
		return false
	}
	if strings.ContainsAny(name, " \t") {
		name = commandProgram(name)
	} else {
		name = commandBasename(name)
	}
	switch name {
	case "ping", "ssh", "nmap", "traceroute", "tracepath", "nc", "netcat", "trusted_ssh":
		return true
	default:
		return false
	}
}

func commandProgram(cmd string) string {
	fields := strings.Fields(strings.TrimSpace(cmd))
	for i := 0; i < len(fields); {
		tok := commandBasename(strings.Trim(fields[i], "\"'`"))
		switch tok {
		case "sudo", "command", "time", "nohup", "nice":
			i++
			continue
		case "timeout", "env", "stdbuf":
			i++
			for i < len(fields) {
				f := fields[i]
				if strings.HasPrefix(f, "-") || strings.Contains(f, "=") || isAllASCIIDigits(strings.TrimSuffix(strings.ToLower(f), "s")) {
					i++
					continue
				}
				break
			}
			continue
		default:
			return tok
		}
	}
	return ""
}

func commandBasename(tok string) string {
	tok = strings.TrimSpace(tok)
	if i := strings.LastIndexAny(tok, "/\\"); i >= 0 {
		tok = tok[i+1:]
	}
	return strings.ToLower(tok)
}

func structuredHost(argsJSON string) string {
	host := stringArg(argsJSON, "host", "hostname", "ip", "target", "address", "server")
	if host == "" {
		return ""
	}
	return CanonicalEntity(host)
}

func preferLastMentioned(entities []string, evidence string) string {
	if len(entities) == 1 {
		return entities[0]
	}
	lower := strings.ToLower(evidence)
	bestIdx := -1
	best := ""
	for _, e := range entities {
		raw := strings.ToLower(stripEntityPrefix(e))
		if raw == "" {
			continue
		}
		idx := strings.LastIndex(lower, raw)
		if idx > bestIdx {
			bestIdx = idx
			best = e
		}
	}
	if best != "" {
		return best
	}
	for _, e := range entities {
		if strings.HasPrefix(e, "host:") {
			return e
		}
	}
	return entities[0]
}

func resolvedAddress(primary, evidence string) string {
	host, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(primary)), "host:")
	if !ok || host == "" || evidence == "" {
		return ""
	}
	lower := strings.ToLower(evidence)
	var rest string
	for _, open := range []string{" (", " ["} {
		needle := host + open
		idx := strings.Index(lower, needle)
		if idx < 0 {
			continue
		}
		rest = evidence[idx+len(needle):]
		break
	}
	if rest == "" {
		return ""
	}
	ip := ipv4Pattern.FindString(rest)
	if ip == "" {
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return ""
	}
	return "ip:" + parsed.To4().String()
}

func stringArg(argsJSON string, keys ...string) string {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return ""
	}
	var obj map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &obj) != nil {
		return ""
	}
	for _, key := range keys {
		if s, ok := obj[key].(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				return t
			}
		}
	}
	return ""
}
