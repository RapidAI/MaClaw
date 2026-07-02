package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func tuiSkillSourceAllowedByPolicy(cfg corelib.AppConfig, source string) bool {
	if clientsecurity.IsDeveloperMode(cfg) {
		return true
	}
	return skill.IsSourceAllowed(source, cfg.SkillSourcesAllowed)
}

func tuiAllowedSkillSearchSources(cfg corelib.AppConfig) []string {
	if clientsecurity.IsDeveloperMode(cfg) {
		return nil
	}
	return cfg.SkillSourcesAllowed
}

func tuiAllowedSkillSearchSourcesForPolicy(cfg corelib.AppConfig, query string) ([]string, error) {
	if clientsecurity.IsDeveloperMode(cfg) {
		return nil, nil
	}
	candidates := []string{"skillhub", "clawhub", "github"}
	allowed := make([]string, 0, len(candidates))
	var blocked []string
	for _, source := range candidates {
		if !skill.IsSourceAllowed(source, cfg.SkillSourcesAllowed) {
			continue
		}
		args := tuiSkillSearchPolicyArgsForSource(cfg, query, source)
		if ok, reason := clientsecurity.EnforceConfig(cfg, "search_and_install_skill", args); !ok {
			blocked = append(blocked, source+": "+reason)
			continue
		}
		allowed = append(allowed, source)
	}
	if len(allowed) == len(candidates) && len(cfg.SkillSourcesAllowed) == 0 {
		return nil, nil
	}
	if len(allowed) == 0 && len(blocked) > 0 {
		return nil, fmt.Errorf("skill search blocked by security policy: %s", strings.Join(blocked, "; "))
	}
	return allowed, nil
}

func tuiSkillSearchPolicySource(cfg corelib.AppConfig) string {
	allowed := tuiAllowedSkillSearchSources(cfg)
	if len(allowed) == 0 {
		return "skillhub"
	}
	for _, source := range allowed {
		if strings.TrimSpace(source) != "" {
			return source
		}
	}
	return "skillhub"
}

func tuiSkillSearchPolicyArgs(cfg corelib.AppConfig, query string) map[string]interface{} {
	return tuiSkillSearchPolicyArgsForSource(cfg, query, tuiSkillSearchPolicySource(cfg))
}

func tuiSkillSearchPolicyArgsForSource(cfg corelib.AppConfig, query, source string) map[string]interface{} {
	args := map[string]interface{}{"query": query, "source": source}
	switch normalizeTUISkillPolicySource(source) {
	case "github":
		args["url"] = "https://github.com"
	case "clawhub":
		args["url"] = skill.ClawHubMirrorURL
	default:
		args["hub_url"] = cfg.ConfiguredHubCenterBaseURL()
	}
	return args
}

func normalizeTUISkillPolicySource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	switch source {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "claw_hub":
		return "clawhub"
	case "git_hub":
		return "github"
	default:
		return source
	}
}

func recordTUIDeveloperSkillRisk(cfg corelib.AppConfig, source, action string, args map[string]interface{}) {
	if !clientsecurity.IsDeveloperMode(cfg) {
		return
	}
	auditAction := security.AuditActionHubSkillInstall
	if strings.EqualFold(action, "update") {
		auditAction = security.AuditActionHubSkillUpdate
	}
	entryArgs := map[string]interface{}{"source": source, "action": action}
	for k, v := range args {
		entryArgs[k] = v
	}
	al, err := security.NewAuditLog(filepath.Join(commands.ResolveDataDir(), "audit_logs"))
	if err != nil {
		return
	}
	defer al.Close()
	_ = al.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		Action:       auditAction,
		ToolName:     "tui_skill_" + action,
		Arguments:    entryArgs,
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyAudit,
		Source:       source,
		Result:       "developer mode recorded skill install risk and allowed operation",
	})
}
