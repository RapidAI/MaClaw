package main

// im_app_accessors.go provides accessor methods for IMMessageHandler that
// abstract away the h.app dependency. Each accessor checks the direct field
// first (set at construction or by TUI), then falls back to h.app for GUI
// late-init compatibility.
//
// This is Phase 1 of the agent-unification plan (docs/agent-unification-design.md).
// The goal is to eliminate direct h.app.XXX references from the handler code
// so that IMMessageHandler can be constructed without a *App (for TUI).

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/steering"
)

// getWebSearchStrategy resolves the search strategy for both desktop and
// standalone/TUI handlers. Standalone hosts may inject a live strategy
// callback; an absent or invalid callback degrades to the safe mainland
// defaults instead of dereferencing the desktop App.
func (h *IMMessageHandler) getWebSearchStrategy() corelib.WebSearchStrategy {
	if h != nil && h.standaloneConfig != nil && h.standaloneConfig.WebSearchStrategyFunc != nil {
		if strategy, err := websearch.NormalizeWebSearchStrategy(h.standaloneConfig.WebSearchStrategyFunc()); err == nil {
			return strategy
		}
	}
	if h != nil && h.app != nil {
		return h.app.effectiveWebSearchStrategy()
	}
	return websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
}

// --- Steering Store ---

func (h *IMMessageHandler) getSteeringStore() *steering.Store {
	if h.steeringStore != nil {
		return h.steeringStore
	}
	if h.app == nil {
		return nil
	}
	return h.app.steeringStore
}

// --- Skill Executor ---

func (h *IMMessageHandler) getSkillExecutor() *SkillExecutor {
	if h.app == nil {
		return nil
	}
	return h.app.skillExecutor
}

// --- Skill Runner ---

func (h *IMMessageHandler) getSkillRunner() *SkillRunner {
	if h.app == nil {
		return nil
	}
	return h.app.skillRunner
}

// --- Skill Hub Client ---

func (h *IMMessageHandler) getSkillHubClient() *SkillHubClient {
	if h.app == nil {
		return nil
	}
	return h.app.skillHubClient
}

// --- Audit Log ---

func (h *IMMessageHandler) getAuditLog() *AuditLog {
	if h.app == nil {
		return nil
	}
	h.app.ensureAuditLog()
	return h.app.auditLog
}

// --- Risk Assessor ---

func (h *IMMessageHandler) getRiskAssessor() *RiskAssessor {
	if h.app == nil {
		return nil
	}
	return h.app.riskAssessor
}

// isSecurityDeveloperMode returns true when the security policy is in "developer"
// mode, which disables all security guardrails for security research purposes.
func (h *IMMessageHandler) isSecurityDeveloperMode() bool {
	if h.app == nil {
		return false
	}
	return h.app.policyEngine != nil && h.app.policyEngine.IsDeveloperMode()
}

// --- MCP Registry ---

func (h *IMMessageHandler) getMCPRegistry() *MCPRegistry {
	if h.app == nil {
		return nil
	}
	return h.app.mcpRegistry
}

// --- Local MCP Manager ---

func (h *IMMessageHandler) getLocalMCPManager() *LocalMCPManager {
	if h.app == nil {
		return nil
	}
	h.app.ensureLocalMCPManager()
	return h.app.localMCPManager
}

// --- Tool Router (on App, distinct from h.toolRouter on handler) ---

func (h *IMMessageHandler) getAppToolRouter() *ToolRouter {
	if h.app == nil {
		return nil
	}
	return h.app.toolRouter
}

// --- LLM Config ---

func (h *IMMessageHandler) getMaclawLLMConfig() corelib.MaclawLLMConfig {
	if h.standaloneConfig != nil && h.standaloneConfig.LLMConfigFunc != nil {
		return h.standaloneConfig.LLMConfigFunc()
	}
	if h.app == nil {
		return corelib.MaclawLLMConfig{}
	}
	return h.app.GetMaclawLLMConfig()
}

// getCodingLLMConfig resolves the coding profile for CodingSubAgent work. It
// intentionally remains separate from the general assistant accessor: a
// coding profile may follow assistant, but it may also be independently set.
func (h *IMMessageHandler) getCodingLLMConfig() corelib.MaclawLLMConfig {
	if h.standaloneConfig != nil && h.standaloneConfig.LLMConfigFunc != nil {
		return h.standaloneConfig.LLMConfigFunc()
	}
	if h.app == nil {
		return corelib.MaclawLLMConfig{}
	}
	return h.app.GetCodingLLMConfig()
}

// getLightweightLLMConfig returns a non-reasoning model config for classification
// tasks. It uses the same provider URL/key but substitutes a lighter model name.
// Reasoning models (deepseek-reasoner, o1-*, etc.) generate chain-of-thought
// before answering, adding 1-2s latency for simple yes/no classification tasks.
//
// Strategy: if the current model is a known reasoning model, substitute with
// the corresponding chat model from the same provider. Otherwise return the
// main config unchanged (it's already a chat model).
func (h *IMMessageHandler) getLightweightLLMConfig() corelib.MaclawLLMConfig {
	return h.getLightweightLLMConfigFromBase(h.getMaclawLLMConfig())
}

// getCodingLightweightLLMConfig is the lightweight companion of the coding
// profile. Coding intent/planning requests are real coding-workbench calls;
// they must not spend or route through the assistant profile merely because
// their prompts are short.
func (h *IMMessageHandler) getCodingLightweightLLMConfig() corelib.MaclawLLMConfig {
	return h.getLightweightLLMConfigFromBase(h.getCodingLLMConfig())
}

func (h *IMMessageHandler) getLightweightLLMConfigFromBase(cfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	// OpenHuman-inspired: try ModelRouter first for fast tasks.
	if h.app != nil && h.app.ohModules.modelRouter != nil && h.app.ohModules.modelRouter.HasRoute("fast") {
		return h.routeLLMConfigFromBase("fast", cfg)
	}

	if cfg.URL == "" || cfg.Model == "" {
		return cfg
	}

	// Map reasoning models to their lightweight counterparts.
	// Same provider, same API key, just a faster model.
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	switch {
	case strings.Contains(model, "deepseek-reasoner") || strings.Contains(model, "deepseek-r1"):
		cfg.Model = "deepseek-chat"
	case strings.Contains(model, "deepseek-v4"):
		// DeepSeek V4 models have thinking enabled by default — use the
		// non-thinking alias for lightweight classification tasks.
		cfg.Model = "deepseek-chat"
	case strings.HasPrefix(model, "o1-") || strings.HasPrefix(model, "o3-"):
		cfg.Model = "gpt-4o-mini"
	case strings.Contains(model, "qwen3"):
		// Qwen3 reasoning models — use qwen-turbo for classification
		cfg.Model = "qwen-turbo"
	}
	// For non-reasoning models (deepseek-chat, gpt-4o, glm-4, etc.),
	// return unchanged — they're already fast enough for classification.
	return cfg
}

func (h *IMMessageHandler) isMaclawLLMConfigured() bool {
	if h.standaloneConfig != nil && h.standaloneConfig.LLMConfigFunc != nil {
		cfg := h.standaloneConfig.LLMConfigFunc()
		return cfg.URL != "" && cfg.Model != ""
	}
	if h.app == nil {
		return false
	}
	return h.app.isMaclawLLMConfigured()
}

func (h *IMMessageHandler) getMaclawAgentMaxIterations() int {
	if h.standaloneConfig != nil && h.standaloneConfig.MaxIterationsFunc != nil {
		return h.standaloneConfig.MaxIterationsFunc()
	}
	if h.app == nil {
		return 30
	}
	return h.app.GetMaclawAgentMaxIterations()
}

func (h *IMMessageHandler) isProMode() bool {
	if h.standaloneConfig != nil && h.standaloneConfig.IsProMode != nil {
		return *h.standaloneConfig.IsProMode
	}
	if h.app == nil {
		return true
	}
	return h.app.isProMode()
}

// --- Config Persistence ---

func (h *IMMessageHandler) loadConfig() (corelib.AppConfig, error) {
	if h.app == nil {
		return corelib.AppConfig{}, nil
	}
	return h.app.LoadConfig()
}

// --- Temp Dir ---

func (h *IMMessageHandler) getTempDir() string {
	if h.app == nil {
		return os.TempDir()
	}
	return h.app.GetTempDir()
}

// --- Project Path ---

func (h *IMMessageHandler) getCurrentProjectPath() string {
	if h.app == nil {
		return ""
	}
	return h.app.GetCurrentProjectPath()
}

// effectiveWorkingDirForUser returns the working directory shared by ProjectDirBar,
// tools, system prompt, and workflow "项目路径" for the given session owner.
// Prefer this over getCurrentProjectPath() for agent/runtime context.
func (h *IMMessageHandler) effectiveWorkingDirForUser(userID string) string {
	if h != nil && h.app != nil {
		if dir := strings.TrimSpace(h.app.EffectiveWorkingDirForOwner(userID)); dir != "" {
			return dir
		}
	}
	if h != nil {
		if dir := strings.TrimSpace(h.getEffectiveWorkingDir()); dir != "" {
			return normalizeProjectSessionPath(dir)
		}
	}
	return normalizeProjectSessionPath(corelib.EffectiveWorkspaceDir())
}

// --- OAuth ---

func (h *IMMessageHandler) ensureOAuthToken() error {
	if h.app == nil {
		return nil
	}
	return h.app.ensureOAuthToken()
}

// --- Interaction Infrastructure ---

func (h *IMMessageHandler) ensureInteractionInfra() {
	if h.app == nil {
		return
	}
	h.app.ensureInteractionInfra()
}

// --- Session Search DB ---

func (h *IMMessageHandler) getSessionSearchDBPath() string {
	if h.app == nil {
		return ""
	}
	return h.app.sessionSearchDBPath()
}

// --- Test Home Dir ---

func (h *IMMessageHandler) getTestHomeDir() string {
	if h.app == nil {
		return ""
	}
	return h.app.testHomeDir
}

// --- Event Emission ---

func (h *IMMessageHandler) emitAppEvent(name string, data ...interface{}) {
	if h.app == nil {
		return
	}
	h.app.emitEvent(name, data...)
}

// --- Hub Client ---

func (h *IMMessageHandler) getHubClient() *RemoteHubClient {
	if h.app == nil {
		return nil
	}
	return h.app.hubClient()
}

// --- Session Starter ---

func (h *IMMessageHandler) getSessionStarter() *CodingSessionStarter {
	if h.app == nil {
		return nil
	}
	return h.app.sessionStarter
}

// --- Context Resolver ---

func (h *IMMessageHandler) getContextResolver() *SessionContextResolver {
	if h.app == nil {
		return nil
	}
	h.app.ensureContextResolver()
	return h.app.contextResolver
}

// --- Skill ensure methods ---

func (h *IMMessageHandler) ensureSkillHubClient() {
	if h.app != nil {
		h.app.ensureSkillHubClient()
	}
}

func (h *IMMessageHandler) ensureSkillRunner() {
	if h.app != nil {
		h.app.ensureSkillRunner()
	}
}

// --- Tool Selector ---

func (h *IMMessageHandler) getToolSelector() *ToolSelector {
	if h.app == nil {
		return nil
	}
	return h.app.toolSelector
}

// --- LLM Providers ---

type llmProvidersResult struct {
	Providers []corelib.MaclawLLMProvider
	Current   string
}

func (h *IMMessageHandler) getMaclawLLMProviders() llmProvidersResult {
	if h.app == nil {
		return llmProvidersResult{}
	}
	r := h.app.GetMaclawLLMProviders()
	return llmProvidersResult{Providers: r.Providers, Current: r.Current}
}

// --- Token Accounting ---

func (h *IMMessageHandler) accumulateLLMTokenUsage(providerName string, input, output int) {
	h.accumulateLLMTokenUsageWithCache(providerName, input, output, 0, 0)
}

func (h *IMMessageHandler) accumulateLLMTokenUsageWithCache(providerName string, input, output, cacheRead, cacheWrite int) {
	if h.app == nil {
		return
	}
	h.app.AccumulateLLMTokenUsageWithCache(providerName, input, output, cacheRead, cacheWrite)
}

// --- MCP Server Resolution ---

func (h *IMMessageHandler) resolveMCPServerRef(ref string) (string, bool, error) {
	if h.app == nil {
		return "", false, nil
	}
	return h.app.resolveMCPServerRef(ref)
}

// --- Skill Deps ---

func (h *IMMessageHandler) appInstallSkillDepsIfMissing(skillDir, skillName string) {
	if h.app != nil {
		h.app.installSkillDepsIfMissing(skillDir, skillName)
	}
}

// --- Run NL Skill Async ---

func (h *IMMessageHandler) appRunNLSkillAsync(name string, args map[string]interface{}) {
	if h.app != nil {
		h.app.RunNLSkillAsync(name, args)
	}
}

// --- Upload Skill ---

func (h *IMMessageHandler) appUploadNLSkillToMarket(skillName string) (string, error) {
	if h.app == nil {
		return "", nil
	}
	return h.app.UploadNLSkillToMarket(skillName)
}

// --- Orchestrator ---

func (h *IMMessageHandler) appEnsureOrchestrator() {
	if h.app != nil {
		h.app.ensureOrchestrator()
	}
}

func (h *IMMessageHandler) appGetOrchestrator() interface{} {
	if h.app == nil {
		return nil
	}
	return h.app.orchestrator
}

// --- Doc Generator ---

func (h *IMMessageHandler) appEnsureDocGenerator() {
	if h.app != nil {
		h.app.ensureDocGenerator()
	}
}

func (h *IMMessageHandler) appGetDocGenerator() interface{} {
	if h.app == nil {
		return nil
	}
	return h.app.docGenerator
}

// --- Save LLM Providers ---

func (h *IMMessageHandler) saveMaclawLLMProviders(providers []corelib.MaclawLLMProvider, current string) error {
	if h.app == nil {
		return nil
	}
	return h.app.SaveMaclawLLMProviders(providers, current)
}

// --- Web Search ---

func (h *IMMessageHandler) appGetWebSearchProviders() interface{} {
	if h.app == nil {
		return nil
	}
	return h.app.GetWebSearchProviders()
}

// --- App Log ---

func (h *IMMessageHandler) appLog(msg string) {
	if h.app != nil {
		h.app.log(msg)
	}
}
