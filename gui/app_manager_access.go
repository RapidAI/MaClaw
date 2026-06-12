package main

import "github.com/RapidAI/CodeClaw/corelib/tool"

// app_manager_access.go provides access methods for App components through managers.

func (a *App) GetRemoteSessions() *RemoteSessionManager {
	if a.managers != nil && a.managers.sessions != nil {
		if sessions := a.managers.sessions.GetRemoteSessions(); sessions != nil {
			return sessions.(*RemoteSessionManager)
		}
	}
	return a.remoteSessions
}

func (a *App) GetBrowserSessions() *BrowserAgentManager {
	if a.managers != nil && a.managers.sessions != nil {
		if sessions := a.managers.sessions.GetBrowserSessions(); sessions != nil {
			return sessions.(*BrowserAgentManager)
		}
	}
	return a.browserSessions
}

func (a *App) GetMCPRegistry() *MCPRegistry {
	if a.managers != nil && a.managers.core != nil {
		if registry := a.managers.core.GetMCPRegistry(); registry != nil {
			return registry.(*MCPRegistry)
		}
	}
	return a.mcpRegistry
}

func (a *App) GetLocalMCPManager() *LocalMCPManager {
	if a.managers != nil && a.managers.core != nil {
		if manager := a.managers.core.GetLocalMCPManager(); manager != nil {
			return manager.(*LocalMCPManager)
		}
	}
	return a.localMCPManager
}

func (a *App) GetSkillExecutor() *SkillExecutor {
	return a.skillExecutor
}

func (a *App) GetSkillRunner() *SkillRunner {
	return a.skillRunner
}

func (a *App) GetSessionStarter() *CodingSessionStarter {
	return a.sessionStarter
}

func (a *App) GetSkillMarketClient() *SkillMarketClient {
	return a.skillMarketClient
}

func (a *App) GetGossipClient() *GossipClient {
	return a.gossipClient
}

func (a *App) GetAutoUploadTrigger() *AutoUploadTrigger {
	return a.autoUploadTrigger
}

func (a *App) GetGossipAutoPublish() *AutoPublishTrigger {
	return a.gossipAutoPublish
}

func (a *App) GetRiskAssessor() *RiskAssessor {
	if a.managers != nil && a.managers.security != nil {
		if assessor := a.managers.security.GetRiskAssessor(); assessor != nil {
			return assessor.(*RiskAssessor)
		}
	}
	return a.riskAssessor
}

func (a *App) GetPolicyEngine() *PolicyEngine {
	if a.managers != nil && a.managers.security != nil {
		if engine := a.managers.security.GetPolicyEngine(); engine != nil {
			return engine.(*PolicyEngine)
		}
	}
	return a.policyEngine
}

func (a *App) GetAuditLog() *AuditLog {
	if a.managers != nil && a.managers.security != nil {
		if logValue := a.managers.security.GetAuditLog(); logValue != nil {
			return logValue.(*AuditLog)
		}
	}
	return a.auditLog
}

func (a *App) GetLLMSecurityReview() *LLMSecurityReview {
	if a.managers != nil && a.managers.security != nil {
		if review := a.managers.security.GetLLMSecurityReview(); review != nil {
			return review.(*LLMSecurityReview)
		}
	}
	return a.llmSecurityReview
}

func (a *App) GetMDNSScanner() *MDNSScanner {
	if a.managers != nil && a.managers.networking != nil {
		if scanner := a.managers.networking.GetMDNSScanner(); scanner != nil {
			return scanner.(*MDNSScanner)
		}
	}
	return a.mdnsScanner
}

func (a *App) GetProjectScanner() *ProjectScanner {
	if a.managers != nil && a.managers.networking != nil {
		if scanner := a.managers.networking.GetProjectScanner(); scanner != nil {
			return scanner.(*ProjectScanner)
		}
	}
	return a.projectScanner
}

func (a *App) GetToolDefGenerator() *ToolDefinitionGenerator {
	if a.managers != nil && a.managers.core != nil {
		if generator := a.managers.core.GetToolDefGenerator(); generator != nil {
			return generator.(*ToolDefinitionGenerator)
		}
	}
	return a.toolDefGenerator
}

func (a *App) GetToolRouter() *ToolRouter {
	if a.managers != nil && a.managers.core != nil {
		if router := a.managers.core.GetToolRouter(); router != nil {
			return router.(*ToolRouter)
		}
	}
	return a.toolRouter
}

func (a *App) GetUsageTracker() *tool.UsageTracker {
	if a.managers != nil && a.managers.core != nil {
		if tracker := a.managers.core.GetUsageTracker(); tracker != nil {
			return tracker.(*tool.UsageTracker)
		}
	}
	return a.usageTracker
}

func (a *App) GetToolSelector() *ToolSelector {
	if a.managers != nil && a.managers.core != nil {
		if selector := a.managers.core.GetToolSelector(); selector != nil {
			return selector.(*ToolSelector)
		}
	}
	return a.toolSelector
}

func (a *App) GetSkillHubClient() *SkillHubClient {
	return a.skillHubClient
}

func (a *App) GetCapabilityGapDetector() *CapabilityGapDetector {
	return a.capabilityGapDetector
}

func (a *App) GetExperienceExtractor() *ExperienceExtractor {
	return a.experienceExtractor
}

func (a *App) GetOrchestrator() *Orchestrator {
	return a.orchestrator
}

func (a *App) GetSharedContext() *SharedContextStore {
	return a.sharedContext
}
