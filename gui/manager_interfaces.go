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

// MockCoreManager is a mock implementation for testing
type MockCoreManager struct {
	templateManager  interface{}
	mcpRegistry      interface{}
	localMCPManager  interface{}
	toolDefGenerator interface{}
	toolRouter       interface{}
	usageTracker     interface{}
	toolSelector     interface{}
	configManager    interface{}
	contextResolver  interface{}
	ready            bool
	warmupDone       bool
}

func NewMockCoreManager() *MockCoreManager {
	return &MockCoreManager{
		ready:      true,
		warmupDone: true,
	}
}

func (m *MockCoreManager) Initialize() error                         { return nil }
func (m *MockCoreManager) GetTemplateManager() interface{}           { return m.templateManager }
func (m *MockCoreManager) SetTemplateManager(tm interface{})         { m.templateManager = tm }
func (m *MockCoreManager) GetMCPRegistry() interface{}               { return m.mcpRegistry }
func (m *MockCoreManager) SetMCPRegistry(registry interface{})       { m.mcpRegistry = registry }
func (m *MockCoreManager) GetLocalMCPManager() interface{}           { return m.localMCPManager }
func (m *MockCoreManager) SetLocalMCPManager(manager interface{})    { m.localMCPManager = manager }
func (m *MockCoreManager) GetToolDefGenerator() interface{}          { return m.toolDefGenerator }
func (m *MockCoreManager) SetToolDefGenerator(generator interface{}) { m.toolDefGenerator = generator }
func (m *MockCoreManager) GetToolRouter() interface{}                { return m.toolRouter }
func (m *MockCoreManager) SetToolRouter(router interface{})          { m.toolRouter = router }
func (m *MockCoreManager) GetUsageTracker() interface{}              { return m.usageTracker }
func (m *MockCoreManager) SetUsageTracker(tracker interface{})       { m.usageTracker = tracker }
func (m *MockCoreManager) GetToolSelector() interface{}              { return m.toolSelector }
func (m *MockCoreManager) SetToolSelector(selector interface{})      { m.toolSelector = selector }
func (m *MockCoreManager) GetConfigManager() interface{}             { return m.configManager }
func (m *MockCoreManager) SetConfigManager(manager interface{})      { m.configManager = manager }
func (m *MockCoreManager) GetContextResolver() interface{}           { return m.contextResolver }
func (m *MockCoreManager) SetContextResolver(resolver interface{})   { m.contextResolver = resolver }
func (m *MockCoreManager) IsReady() bool                             { return m.ready }
func (m *MockCoreManager) IsWarmupDone() bool                        { return m.warmupDone }
func (m *MockCoreManager) MarkWarmupDone()                           { m.warmupDone = true }

// MockSessionManager is a mock implementation for testing
type MockSessionManager struct {
	remoteSessions       interface{}
	browserSessions      interface{}
	sessionStarter       interface{}
	sessionPrecheck      interface{}
	sessionCheckpointer  interface{}
	startupFeedback      interface{}
	ioRelay              interface{}
	conversationArchiver interface{}
}

func NewMockSessionManager() *MockSessionManager {
	return &MockSessionManager{}
}

func (m *MockSessionManager) Initialize() error                       { return nil }
func (m *MockSessionManager) GetRemoteSessions() interface{}          { return m.remoteSessions }
func (m *MockSessionManager) SetRemoteSessions(sessions interface{})  { m.remoteSessions = sessions }
func (m *MockSessionManager) GetBrowserSessions() interface{}         { return m.browserSessions }
func (m *MockSessionManager) SetBrowserSessions(sessions interface{}) { m.browserSessions = sessions }
func (m *MockSessionManager) GetSessionStarter() interface{}          { return m.sessionStarter }
func (m *MockSessionManager) SetSessionStarter(starter interface{})   { m.sessionStarter = starter }
func (m *MockSessionManager) GetSessionPrecheck() interface{}         { return m.sessionPrecheck }
func (m *MockSessionManager) SetSessionPrecheck(precheck interface{}) { m.sessionPrecheck = precheck }
func (m *MockSessionManager) GetSessionCheckpointer() interface{}     { return m.sessionCheckpointer }
func (m *MockSessionManager) SetSessionCheckpointer(checkpointer interface{}) {
	m.sessionCheckpointer = checkpointer
}
func (m *MockSessionManager) GetStartupFeedback() interface{}         { return m.startupFeedback }
func (m *MockSessionManager) SetStartupFeedback(feedback interface{}) { m.startupFeedback = feedback }
func (m *MockSessionManager) GetIORelay() interface{}                 { return m.ioRelay }
func (m *MockSessionManager) SetIORelay(relay interface{})            { m.ioRelay = relay }
func (m *MockSessionManager) GetConversationArchiver() interface{}    { return m.conversationArchiver }
func (m *MockSessionManager) SetConversationArchiver(archiver interface{}) {
	m.conversationArchiver = archiver
}

// MockSecurityManager is a mock implementation for testing
type MockSecurityManager struct {
	riskAssessor      interface{}
	policyEngine      interface{}
	auditLog          interface{}
	llmSecurityReview interface{}
	securityFirewall  interface{}
}

func NewMockSecurityManager() *MockSecurityManager {
	return &MockSecurityManager{}
}

func (m *MockSecurityManager) Initialize() error                       { return nil }
func (m *MockSecurityManager) GetRiskAssessor() interface{}            { return m.riskAssessor }
func (m *MockSecurityManager) SetRiskAssessor(assessor interface{})    { m.riskAssessor = assessor }
func (m *MockSecurityManager) GetPolicyEngine() interface{}            { return m.policyEngine }
func (m *MockSecurityManager) SetPolicyEngine(engine interface{})      { m.policyEngine = engine }
func (m *MockSecurityManager) GetAuditLog() interface{}                { return m.auditLog }
func (m *MockSecurityManager) SetAuditLog(log interface{})             { m.auditLog = log }
func (m *MockSecurityManager) GetLLMSecurityReview() interface{}       { return m.llmSecurityReview }
func (m *MockSecurityManager) SetLLMSecurityReview(review interface{}) { m.llmSecurityReview = review }
func (m *MockSecurityManager) GetSecurityFirewall() interface{}        { return m.securityFirewall }
func (m *MockSecurityManager) SetSecurityFirewall(firewall interface{}) {
	m.securityFirewall = firewall
}

// MockNetworkManager is a mock implementation for testing
type MockNetworkManager struct {
	mdnsScanner       interface{}
	projectScanner    interface{}
	gossipClient      interface{}
	skillHubClient    interface{}
	gossipAutoPublish interface{}
}

func NewMockNetworkManager() *MockNetworkManager {
	return &MockNetworkManager{}
}

func (m *MockNetworkManager) Initialize() error                        { return nil }
func (m *MockNetworkManager) GetMDNSScanner() interface{}              { return m.mdnsScanner }
func (m *MockNetworkManager) SetMDNSScanner(scanner interface{})       { m.mdnsScanner = scanner }
func (m *MockNetworkManager) GetProjectScanner() interface{}           { return m.projectScanner }
func (m *MockNetworkManager) SetProjectScanner(scanner interface{})    { m.projectScanner = scanner }
func (m *MockNetworkManager) GetGossipClient() interface{}             { return m.gossipClient }
func (m *MockNetworkManager) SetGossipClient(client interface{})       { m.gossipClient = client }
func (m *MockNetworkManager) GetSkillHubClient() interface{}           { return m.skillHubClient }
func (m *MockNetworkManager) SetSkillHubClient(client interface{})     { m.skillHubClient = client }
func (m *MockNetworkManager) GetGossipAutoPublish() interface{}        { return m.gossipAutoPublish }
func (m *MockNetworkManager) SetGossipAutoPublish(publish interface{}) { m.gossipAutoPublish = publish }

// MockUIManager is a mock implementation for testing
type MockUIManager struct {
	floatingAssistant interface{}
	powerStateProcess interface{}
	screenDimCancel   interface{}
	workstationCancel interface{}
}

func NewMockUIManager() *MockUIManager {
	return &MockUIManager{}
}

func (m *MockUIManager) Initialize() error                          { return nil }
func (m *MockUIManager) GetFloatingAssistant() interface{}          { return m.floatingAssistant }
func (m *MockUIManager) SetFloatingAssistant(assistant interface{}) { m.floatingAssistant = assistant }
func (m *MockUIManager) GetPowerStateProcess() interface{}          { return m.powerStateProcess }
func (m *MockUIManager) SetPowerStateProcess(process interface{})   { m.powerStateProcess = process }
func (m *MockUIManager) GetScreenDimCancel() interface{}            { return m.screenDimCancel }
func (m *MockUIManager) SetScreenDimCancel(cancel interface{})      { m.screenDimCancel = cancel }
func (m *MockUIManager) GetWorkstationCancel() interface{}          { return m.workstationCancel }
func (m *MockUIManager) SetWorkstationCancel(cancel interface{})    { m.workstationCancel = cancel }

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

// MockAppManagers is a mock implementation for testing
type MockAppManagers struct {
	core       CoreManagerInterface
	sessions   SessionManagerInterface
	security   SecurityManagerInterface
	networking NetworkManagerInterface
	ui         UIManagerInterface
}

func NewMockAppManagers() *MockAppManagers {
	return &MockAppManagers{
		core:       NewMockCoreManager(),
		sessions:   NewMockSessionManager(),
		security:   NewMockSecurityManager(),
		networking: NewMockNetworkManager(),
		ui:         NewMockUIManager(),
	}
}

func (m *MockAppManagers) InitializeAll() error {
	if err := m.core.Initialize(); err != nil {
		return err
	}
	if err := m.sessions.Initialize(); err != nil {
		return err
	}
	if err := m.security.Initialize(); err != nil {
		return err
	}
	if err := m.networking.Initialize(); err != nil {
		return err
	}
	return m.ui.Initialize()
}

func (m *MockAppManagers) GetCoreManager() CoreManagerInterface          { return m.core }
func (m *MockAppManagers) GetSessionManager() SessionManagerInterface    { return m.sessions }
func (m *MockAppManagers) GetSecurityManager() SecurityManagerInterface  { return m.security }
func (m *MockAppManagers) GetNetworkingManager() NetworkManagerInterface { return m.networking }
func (m *MockAppManagers) GetUIManager() UIManagerInterface              { return m.ui }

func (m *MockAppManagers) Shutdown() error { return nil }
func (m *MockAppManagers) HealthCheck() map[string]bool {
	return map[string]bool{
		"core":       true,
		"sessions":   true,
		"security":   true,
		"networking": true,
		"ui":         true,
	}
}
