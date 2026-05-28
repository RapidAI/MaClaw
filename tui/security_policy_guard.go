package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func tuiSecurityGuard(cfg func() corelib.AppConfig) func(string, map[string]interface{}) (bool, string) {
	return func(name string, args map[string]interface{}) (bool, string) {
		if cfg == nil {
			return true, ""
		}
		return enforceClientSecurityPolicy(cfg(), name, args)
	}
}

func enforceClientSecurityPolicy(cfg corelib.AppConfig, name string, args map[string]interface{}) (bool, string) {
	return clientsecurity.EnforceConfig(cfg, name, args)
}

func rejectHubManagedSecurityConfigChange(cfg corelib.AppConfig, key string) (bool, string) {
	return clientsecurity.RejectHubManagedSecurityConfigChange(cfg, key)
}

func preserveHubManagedSecurityConfig(current corelib.AppConfig, next *corelib.AppConfig) {
	clientsecurity.PreserveHubManagedSecurityConfig(current, next)
}

func enforceScriptedCommandSecurity(command string, args []string) error {
	cfg, err := commands.NewFileConfigStore(commands.ResolveDataDir()).LoadConfig()
	if err != nil {
		return nil
	}
	toolName, toolArgs, ok := scriptedCommandSecurityTool(command, args)
	if !ok {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(command), "skillhub") {
		action := ""
		if len(args) > 0 {
			action = strings.ToLower(strings.TrimSpace(args[0]))
		}
		if action == "search" || action == "check-updates" {
			toolArgs = tuiSkillSearchPolicyArgs(cfg, strings.Join(args[1:], " "))
		}
	}
	if allowed, reason := enforceClientSecurityPolicy(cfg, toolName, toolArgs); !allowed {
		if reason == "" {
			reason = "blocked by Hub security policy"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func scriptedCommandSecurityTool(command string, args []string) (string, map[string]interface{}, bool) {
	cmd := strings.ToLower(strings.TrimSpace(command))
	action := ""
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch cmd {
	case "skillhub":
		switch action {
		case "search", "check-updates":
			return "search_and_install_skill", map[string]interface{}{"query": strings.Join(args[1:], " ")}, true
		case "install", "install-github", "update", "rate":
			toolArgs := map[string]interface{}{"action": "install", "source": "skillhub"}
			if action == "install-github" {
				toolArgs["source"] = "github"
			}
			if len(args) > 1 {
				toolArgs["install_ref"] = args[1]
			}
			return "manage_skill", toolArgs, true
		}
	case "skillmarket", "capabilitymarket":
		return "web_search", map[string]interface{}{"query": strings.Join(args, " ")}, true
	case "mcp":
		if action == "add" {
			if command := firstFlagValue(args, "--command"); command != "" {
				return "bash", map[string]interface{}{"command": strings.Join(append([]string{command}, splitCommaFlagValue(args, "--args")...), " ")}, true
			}
			return "web_fetch", map[string]interface{}{"url": firstHTTPArg(args)}, true
		}
		if action == "call-tool" || action == "tools" || action == "health-check" {
			return "web_fetch", map[string]interface{}{"url": firstHTTPArg(args)}, true
		}
	case "remote":
		if action != "" && action != "status" {
			return "web_fetch", map[string]interface{}{"url": firstHTTPArg(args)}, true
		}
	}
	return "", nil, false
}

func firstFlagValue(args []string, flagName string) string {
	for i, arg := range args {
		if arg == flagName && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(arg, flagName+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, flagName+"="))
		}
	}
	return ""
}

func splitCommaFlagValue(args []string, flagName string) []string {
	value := firstFlagValue(args, flagName)
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstHTTPArg(args []string) string {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		lower := strings.ToLower(arg)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return arg
		}
		if strings.Contains(arg, "=") {
			value := strings.TrimSpace(arg[strings.Index(arg, "=")+1:])
			lowerValue := strings.ToLower(value)
			if strings.HasPrefix(lowerValue, "http://") || strings.HasPrefix(lowerValue, "https://") {
				return value
			}
		}
	}
	return ""
}
