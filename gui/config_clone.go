package main

import "github.com/RapidAI/CodeClaw/corelib"

// cloneAppConfigForMutation returns a working copy safe to mutate in place.
// publishedConfig() shares slice backing arrays with the immutable snap; writers
// must clone before editing nested elements (NLSkills[i].Status, Models[i], …)
// or they corrupt the published snapshot visible to concurrent readers.
func cloneAppConfigForMutation(cfg corelib.AppConfig) corelib.AppConfig {
	out := cfg

	out.Claude = cloneToolConfig(cfg.Claude)
	out.Codex = cloneToolConfig(cfg.Codex)
	out.Opencode = cloneToolConfig(cfg.Opencode)
	out.CodeBuddy = cloneToolConfig(cfg.CodeBuddy)
	out.IFlow = cloneToolConfig(cfg.IFlow)
	out.Kilo = cloneToolConfig(cfg.Kilo)

	if n := len(cfg.Projects); n > 0 {
		out.Projects = append([]corelib.ProjectConfig(nil), cfg.Projects...)
	}
	if n := len(cfg.MaclawLLMProviders); n > 0 {
		out.MaclawLLMProviders = append([]corelib.MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
	}
	if n := len(cfg.WebSearchProviders); n > 0 {
		out.WebSearchProviders = append([]corelib.WebSearchProvider(nil), cfg.WebSearchProviders...)
	}
	if n := len(cfg.MCPServers); n > 0 {
		out.MCPServers = append([]corelib.MCPServerEntry(nil), cfg.MCPServers...)
	}
	if n := len(cfg.LocalMCPServers); n > 0 {
		out.LocalMCPServers = append([]corelib.LocalMCPServerEntry(nil), cfg.LocalMCPServers...)
	}
	// Shallow entry copy: each NLSkillEntry is value-copied so Status/Name edits
	// are isolated. Nested Steps/Triggers slices still share backing arrays —
	// step-editing paths must replace Steps wholesale (they already do).
	if n := len(cfg.NLSkills); n > 0 {
		out.NLSkills = append([]corelib.NLSkillEntry(nil), cfg.NLSkills...)
	}
	if n := len(cfg.SkillHubURLs); n > 0 {
		out.SkillHubURLs = append([]corelib.SkillHubEntry(nil), cfg.SkillHubURLs...)
	}
	if n := len(cfg.ExternalSkillDirs); n > 0 {
		out.ExternalSkillDirs = append([]string(nil), cfg.ExternalSkillDirs...)
	}
	if n := len(cfg.NetworkAllowlist); n > 0 {
		out.NetworkAllowlist = append([]string(nil), cfg.NetworkAllowlist...)
	}
	if n := len(cfg.SkillSourcesAllowed); n > 0 {
		out.SkillSourcesAllowed = append([]string(nil), cfg.SkillSourcesAllowed...)
	}
	if n := len(cfg.TrustedSkillPackageKeyFingerprints); n > 0 {
		out.TrustedSkillPackageKeyFingerprints = append([]string(nil), cfg.TrustedSkillPackageKeyFingerprints...)
	}
	if n := len(cfg.RemoteHubCenterURLs); n > 0 {
		out.RemoteHubCenterURLs = append([]string(nil), cfg.RemoteHubCenterURLs...)
	}
	if cfg.LLMTokenUsage != nil {
		out.LLMTokenUsage = make(map[string]*corelib.TokenUsageStat, len(cfg.LLMTokenUsage))
		for k, v := range cfg.LLMTokenUsage {
			if v == nil {
				out.LLMTokenUsage[k] = nil
				continue
			}
			cp := *v
			out.LLMTokenUsage[k] = &cp
		}
	}
	if cfg.ModelRoutes != nil {
		out.ModelRoutes = make(map[string]corelib.ModelRouteConfig, len(cfg.ModelRoutes))
		for k, v := range cfg.ModelRoutes {
			out.ModelRoutes[k] = v
		}
	}
	if n := len(cfg.FavoriteEmployees); n > 0 {
		out.FavoriteEmployees = append([]string(nil), cfg.FavoriteEmployees...)
	}
	if cfg.FavoriteEmployeeNames != nil {
		out.FavoriteEmployeeNames = make(map[string]string, len(cfg.FavoriteEmployeeNames))
		for k, v := range cfg.FavoriteEmployeeNames {
			out.FavoriteEmployeeNames[k] = v
		}
	}
	if n := len(cfg.VEAllowedDirectories); n > 0 {
		out.VEAllowedDirectories = append([]string(nil), cfg.VEAllowedDirectories...)
	}
	if n := len(cfg.SSHHosts); n > 0 {
		out.SSHHosts = append([]corelib.SSHHostEntry(nil), cfg.SSHHosts...)
	}

	return out
}

func cloneToolConfig(in corelib.ToolConfig) corelib.ToolConfig {
	out := in
	if n := len(in.Models); n > 0 {
		out.Models = append([]corelib.ModelConfig(nil), in.Models...)
	}
	return out
}

func cloneNLSkillEntry(in corelib.NLSkillEntry) corelib.NLSkillEntry {
	out := in
	if n := len(in.Triggers); n > 0 {
		out.Triggers = append([]string(nil), in.Triggers...)
	}
	if n := len(in.Steps); n > 0 {
		out.Steps = append([]corelib.NLSkillStep(nil), in.Steps...)
	}
	if n := len(in.Platforms); n > 0 {
		out.Platforms = append([]string(nil), in.Platforms...)
	}
	if n := len(in.Capabilities); n > 0 {
		out.Capabilities = append([]string(nil), in.Capabilities...)
	}
	if n := len(in.RequiresTools); n > 0 {
		out.RequiresTools = append([]string(nil), in.RequiresTools...)
	}
	if n := len(in.FallbackForTools); n > 0 {
		out.FallbackForTools = append([]string(nil), in.FallbackForTools...)
	}
	if n := len(in.RequiresToolsets); n > 0 {
		out.RequiresToolsets = append([]string(nil), in.RequiresToolsets...)
	}
	if n := len(in.FallbackForToolsets); n > 0 {
		out.FallbackForToolsets = append([]string(nil), in.FallbackForToolsets...)
	}
	if n := len(in.RequiredCredentialFiles); n > 0 {
		out.RequiredCredentialFiles = append([]string(nil), in.RequiredCredentialFiles...)
	}
	if n := len(in.RepairHistory); n > 0 {
		out.RepairHistory = append([]corelib.SkillRepairRecord(nil), in.RepairHistory...)
	}
	if n := len(in.RequiredArgs); n > 0 {
		out.RequiredArgs = append([]string(nil), in.RequiredArgs...)
	}
	return out
}
