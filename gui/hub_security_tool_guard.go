package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
)

func (a *App) enforceHubSecurityAppPolicy(name string, args map[string]interface{}) (bool, string) {
	return enforceClientSecurityPolicy(a.effectiveHubSecurityConfig(), name, args)
}

func (a *App) effectiveHubSecurityConfig() corelib.AppConfig {
	if a == nil {
		return corelib.AppConfig{}
	}
	if p := a.hubSecurityCache.get(); p != nil && p.CentralizedSecurity && p.Policy != nil {
		ep := p.Policy
		return corelib.AppConfig{
			HubSecurityCentralized: true,
			SecurityPolicyMode:     ep.GuardrailMode,
			SandboxMode:            ep.SandboxMode,
			NetworkLevel:           ep.NetworkLevel,
			NetworkAllowlist:       append([]string(nil), ep.NetworkAllowlist...),
			YoloModeAllowed:        ep.YoloModeAllowed,
			GossipEnabled:          ep.GossipEnabled,
			FileOutboundEnabled:    ep.FileOutboundEnabled,
			ImageOutboundEnabled:   ep.ImageOutboundEnabled,
			SkillSourcesAllowed:    skillSourcesForAppConfig(ep.SkillSourcesAllowed, ep.SkillSourcesRestricted),
		}
	}
	if p := a.hubSecurityCache.get(); p != nil && (p.SkillSourcesRestricted || len(p.SkillSourcesAllowed) > 0) {
		if cfg, err := a.LoadConfig(); err == nil && clientsecurity.IsDeveloperMode(cfg) {
			return cfg
		}
		return corelib.AppConfig{
			HubSecurityCentralized: false,
			SkillSourcesAllowed:    skillSourcesForAppConfig(p.SkillSourcesAllowed, p.SkillSourcesRestricted),
		}
	}
	if cfg, err := a.LoadConfig(); err == nil {
		return cfg
	}
	return corelib.AppConfig{}
}

func (h *IMMessageHandler) enforceHubSecurityToolPolicy(name string, args map[string]interface{}) (bool, string) {
	if h == nil || h.app == nil {
		return true, ""
	}
	return h.app.enforceHubSecurityAppPolicy(name, args)
}

func enforceClientSecurityPolicy(cfg corelib.AppConfig, name string, args map[string]interface{}) (bool, string) {
	return clientsecurity.EnforceConfig(cfg, name, args)
}

func skillSourcesForAppConfig(sources []string, restricted bool) []string {
	if restricted && len(sources) == 0 {
		return []string{"__none__"}
	}
	return append([]string(nil), sources...)
}
