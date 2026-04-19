package main

// CoreManagerInterface defines the interface for core infrastructure management
type CoreManagerInterface interface {
	// Initialize initializes core components
	Initialize() error

	// Template management
	GetTemplateManager() interface{}
	SetTemplateManager(tm interface{})

	// MCP management
	GetMCPRegistry() interface{}
	SetMCPRegistry(registry interface{})
	GetLocalMCPManager() interface{}
	SetLocalMCPManager(manager interface{})

	// Tool management
	GetToolDefGenerator() interface{}
	SetToolDefGenerator(generator interface{})
	GetToolRouter() interface{}
	SetToolRouter(router interface{})
	GetUsageTracker() interface{}
	SetUsageTracker(tracker interface{})
	GetToolSelector() interface{}
	SetToolSelector(selector interface{})

	// Configuration management
	GetConfigManager() interface{}
	SetConfigManager(manager interface{})
	GetContextResolver() interface{}
	SetContextResolver(resolver interface{})

	// Status checks
	IsReady() bool
	IsWarmupDone() bool
	MarkWarmupDone()
}

// SessionManagerInterface defines the interface for session management
type SessionManagerInterface interface {
	// Initialize initializes session components
	Initialize() error

	// Remote sessions
	GetRemoteSessions() interface{}
	SetRemoteSessions(sessions interface{})

	// Browser sessions
	GetBrowserSessions() interface{}
	SetBrowserSessions(sessions interface{})

	// Session lifecycle
	GetSessionStarter() interface{}
	SetSessionStarter(starter interface{})
	GetSessionPrecheck() interface{}
	SetSessionPrecheck(precheck interface{})
	GetSessionCheckpointer() interface{}
	SetSessionCheckpointer(checkpointer interface{})

	// Session feedback
	GetStartupFeedback() interface{}
	SetStartupFeedback(feedback interface{})

	// Session I/O
	GetIORelay() interface{}
	SetIORelay(relay interface{})

	// Session archiving
	GetConversationArchiver() interface{}
	SetConversationArchiver(archiver interface{})
}

// SecurityManagerInterface defines the interface for security management
type SecurityManagerInterface interface {
	// Initialize initializes security components
	Initialize() error

	// Risk assessment
	GetRiskAssessor() interface{}
	SetRiskAssessor(assessor interface{})

	// Policy management
	GetPolicyEngine() interface{}
	SetPolicyEngine(engine interface{})

	// Audit logging
	GetAuditLog() interface{}
	SetAuditLog(log interface{})

	// LLM security
	GetLLMSecurityReview() interface{}
	SetLLMSecurityReview(review interface{})

	// Security firewall
	GetSecurityFirewall() interface{}
	SetSecurityFirewall(firewall interface{})
}

// NetworkManagerInterface defines the interface for network management
type NetworkManagerInterface interface {
	// Initialize initializes network components
	Initialize() error

	// Discovery services
	GetMDNSScanner() interface{}
	SetMDNSScanner(scanner interface{})
	GetProjectScanner() interface{}
	SetProjectScanner(scanner interface{})

	// Communication services
	GetGossipClient() interface{}
	SetGossipClient(client interface{})
	GetSkillHubClient() interface{}
	SetSkillHubClient(client interface{})

	// Upload services
	GetAutoUploadTrigger() interface{}
	SetAutoUploadTrigger(trigger interface{})
	GetGossipAutoPublish() interface{}
	SetGossipAutoPublish(publish interface{})
}

// UIManagerInterface defines the interface for UI management
type UIManagerInterface interface {
	// Initialize initializes UI components
	Initialize() error

	// UI components
	GetFloatingAssistant() interface{}
	SetFloatingAssistant(assistant interface{})

	// Power management
	GetPowerStateProcess() interface{}
	SetPowerStateProcess(process interface{})
	GetScreenDimCancel() interface{}
	SetScreenDimCancel(cancel interface{})
	GetWorkstationCancel() interface{}
	SetWorkstationCancel(cancel interface{})
}

// AppManagersInterface defines the interface for the overall manager system
type AppManagersInterface interface {
	// Initialize all managers
	InitializeAll() error

	// Get individual managers
	GetCoreManager() CoreManagerInterface
	GetSessionManager() SessionManagerInterface
	GetSecurityManager() SecurityManagerInterface
	GetNetworkingManager() NetworkManagerInterface
	GetUIManager() UIManagerInterface

	// Lifecycle management
	Shutdown() error
	HealthCheck() map[string]bool
}
