package main

// openhuman_wiring.go wires the OpenHuman-inspired modules into the GUI layer.
// Each module has its own initialization and injection point.
//
// Modules wired here:
// ToolMemory below is only a per-tool rule cache; durable memory remains in
// corelib/memory.Store.
// - A3: ModelRouter (corelib/llm) — per-task model routing
// - A4: ToolMemory (corelib/tool) — per-tool persistent rules
// - A5: SituationReport (corelib/agent) — context injection
// - B1: StabilityDetector (corelib/memory) — recall boost/penalty (via store)
// - B2: Heartbeat (corelib/agent) — proactive notifications
// - B5: CostTracker (corelib/llm) — usage cost tracking
// - C5: InjectionGuard (corelib/security) — prompt injection detection
//
// TokenJuice (A1) is already wired in im_conversation_trim.go.
// EventBus (B4) and AgentRegistry (B3) are available but not yet consumed.
// MemoryTree (C1), Subconscious (C2), AutoFetch (C3), ForkContext (C4) are
// infrastructure-level and will be wired in dedicated init files.

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/eventbus"
	cllm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/security"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// --- Module instances on IMMessageHandler ---

// openhumanModules holds lazily-initialized OpenHuman module instances.
type openhumanModules struct {
	modelRouter        *cllm.ModelRouter
	cachedAuxLLM       corelib.AuxiliaryLLMConfig // cached at init, avoids per-call loadConfig
	toolMemory         *coretool.ToolMemoryStore
	costTracker        *cllm.CostTracker
	injectionGuard     *security.InjectionGuard
	heartbeat          *agent.HeartbeatEngine
	subconsciousEngine *memory.SubconsciousEngine
	autoFetchEngine    *agent.AutoFetchEngine
	eventBus           *eventbus.Bus
}

// --- Initialization ---

// initOpenHumanModules initializes all OpenHuman-inspired modules.
// Called once during App startup (after initCoreInfra).
func (a *App) initOpenHumanModules() {
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[openhuman] failed to load config: %v", err)
		cfg = corelib.AppConfig{}
	}

	// B4: Event Bus (initialized first — other modules can publish events)
	a.ohModules.eventBus = eventbus.New()

	// Cache auxiliary LLM config to avoid per-call loadConfig lock contention
	a.ohModules.cachedAuxLLM = cfg.AuxiliaryLLM

	// A3: Model Router
	if len(cfg.ModelRoutes) > 0 {
		routes := make(map[string]cllm.ModelRoute, len(cfg.ModelRoutes))
		for k, v := range cfg.ModelRoutes {
			routes[k] = cllm.ModelRoute{
				Model:    v.Model,
				URL:      v.URL,
				Key:      v.Key,
				Protocol: v.Protocol,
				Provider: v.Provider,
			}
		}
		a.ohModules.modelRouter = cllm.NewModelRouter(routes)
		log.Printf("[openhuman] model router initialized with %d routes", len(routes))
	} else {
		a.ohModules.modelRouter = cllm.NewModelRouter(nil)
	}

	// A4: Tool Memory
	toolMemPath := filepath.Join(corelib.MaclawBaseDir(), "data", "tool_rules.json")
	a.ohModules.toolMemory = coretool.NewToolMemoryStore(toolMemPath)
	log.Printf("[openhuman] tool memory initialized at %s", toolMemPath)

	// B5: Cost Tracker
	budgetLimit := 0.0 // unlimited by default
	if cfg.DailyLLMBudgetUSD > 0 {
		budgetLimit = cfg.DailyLLMBudgetUSD
	}
	a.ohModules.costTracker = cllm.NewCostTracker(budgetLimit)
	log.Printf("[openhuman] cost tracker initialized (budget=$%.2f)", budgetLimit)

	// C5: Injection Guard
	a.ohModules.injectionGuard = security.NewInjectionGuard()
	// Developer mode check deferred to first use (needs config reload)

	// B2: Heartbeat Engine
	a.ohModules.heartbeat = agent.NewHeartbeatEngine(5*time.Minute, func(alerts []agent.HeartbeatAlert) {
		for _, alert := range alerts {
			log.Printf("[heartbeat] %s: %s — %s", alert.Priority, alert.Title, alert.Body)
			// TODO: emit to frontend via Wails event
		}
	})
	// Heartbeat checks will be added by other modules (SSH, background tasks, etc.)

	// C1: Memory Tree seal scheduler (background goroutine)
	go a.startMemoryTreeSealScheduler()

	// C2: Subconscious Engine (background goroutine)
	go a.startSubconsciousEngine()

	// C3: AutoFetch Engine (background goroutine, opt-in via config)
	go a.startAutoFetchEngine()
}

// --- Model Router Integration ---

// routeLLMConfig applies model routing to the primary LLM config based on task type.
// Falls back to auxiliary LLM for lightweight tasks when no explicit route exists.
func (h *IMMessageHandler) routeLLMConfig(task cllm.TaskType) corelib.MaclawLLMConfig {
	primary := h.getMaclawLLMConfig()
	if h.app == nil || h.app.ohModules.modelRouter == nil {
		return primary
	}
	return h.app.ohModules.modelRouter.RouteWithAux(task, primary, h.app.ohModules.cachedAuxLLM)
}

// --- Tool Memory Integration ---

// injectToolMemoryHint returns a hint string to prepend to tool results
// based on learned rules for this tool + context.
func (h *IMMessageHandler) injectToolMemoryHint(toolName, argsJSON string) string {
	if h.app == nil || h.app.ohModules.toolMemory == nil {
		return ""
	}
	// Parse args to extract context keys
	var args map[string]interface{}
	if argsJSON != "" {
		cleaned := coretool.CleanToolArguments(argsJSON)
		_ = json.Unmarshal([]byte(cleaned), &args)
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	contextKeys := coretool.ExtractContextKeys(toolName, args)
	return h.app.ohModules.toolMemory.InjectRules(toolName, contextKeys)
}

// learnToolRule records a learned rule after successful tool execution.
func (h *IMMessageHandler) learnToolRule(toolName, key, context, content string) {
	if h.app == nil || h.app.ohModules.toolMemory == nil {
		return
	}
	h.app.ohModules.toolMemory.LearnRule(toolName, key, context, content)
}

// flushToolMemory persists tool memory to disk.
func (h *IMMessageHandler) flushToolMemory() {
	if h.app == nil || h.app.ohModules.toolMemory == nil {
		return
	}
	h.app.ohModules.toolMemory.Flush()
}

// --- Cost Tracker Integration ---

// recordLLMCost records the cost of an LLM call from the response usage data.
func (h *IMMessageHandler) recordLLMCost(model string, inputTokens, outputTokens int) {
	if h.app == nil || h.app.ohModules.costTracker == nil {
		return
	}
	cost := h.app.ohModules.costTracker.Record(model, inputTokens, outputTokens)
	if cost > 0.01 { // only log significant costs
		log.Printf("[cost] %s: input=%d output=%d cost=$%.4f", model, inputTokens, outputTokens, cost)
	}
}

// isOverDailyBudget returns true if the daily LLM cost budget is exceeded.
func (h *IMMessageHandler) isOverDailyBudget() bool {
	if h.app == nil || h.app.ohModules.costTracker == nil {
		return false
	}
	return h.app.ohModules.costTracker.IsOverBudget()
}

// llmCostSessionSummary returns a human-readable cost summary for logging.
func (h *IMMessageHandler) llmCostSessionSummary() string {
	if h.app == nil || h.app.ohModules.costTracker == nil {
		return ""
	}
	return h.app.ohModules.costTracker.SessionSummary()
}

// --- Injection Guard Integration ---

// checkToolResultInjection scans a tool result for prompt injection attempts.
// Returns a warning prefix if injection is detected, empty string otherwise.
func (h *IMMessageHandler) checkToolResultInjection(toolName, result string) string {
	if h.app == nil || h.app.ohModules.injectionGuard == nil {
		return ""
	}
	alert := h.app.ohModules.injectionGuard.CheckToolResult(toolName, result)
	if alert == nil {
		return ""
	}
	log.Printf("[injection-guard] detected in %s result: pattern=%s category=%s confidence=%.2f",
		toolName, alert.Pattern, alert.Category, alert.Confidence)
	return security.AnnotateWarning(alert)
}

// --- Situation Report Integration ---

// buildSituationReport generates the current situation context for system prompt injection.
// No caching — the report is lightweight (<1ms) and must be per-user accurate.
func (h *IMMessageHandler) buildSituationReport(userID string) string {
	userID = strings.TrimSpace(userID)
	ctx := agent.SituationContext{
		CurrentTime: time.Now(),
	}

	// Active workflow — legacy engine removed, always nil.

	// Active SSH sessions (check if sshMgr is available)
	if h.sshMgr != nil {
		if sessions := h.sshMgr.List(); len(sessions) > 0 {
			for _, s := range sessions {
				if len(ctx.ActiveSSHSessions) >= 3 {
					break
				}
				ctx.ActiveSSHSessions = append(ctx.ActiveSSHSessions, s.ID)
			}
		}
	}

	// Background tasks
	if h.localBgTaskMgr != nil {
		if tasks := h.localBgTaskMgr.List(); len(tasks) > 0 {
			for _, t := range tasks {
				if len(ctx.BackgroundTasks) >= 3 {
					break
				}
				if string(t.Status) == "running" {
					ctx.BackgroundTasks = append(ctx.BackgroundTasks, t.TaskID)
				}
			}
		}
	}

	// Recent artifacts from memory
	if h.memoryStore != nil && userID != "" {
		recent := h.memoryStore.EntriesSince(time.Now().Add(-24 * time.Hour))
		for _, e := range recent {
			if len(ctx.RecentArtifacts) >= 3 {
				break
			}
			if strings.TrimSpace(e.OwnerID) != "" && strings.TrimSpace(e.OwnerID) != userID {
				continue
			}
			if string(e.Category) == "task_artifact" {
				title := e.Title
				if title == "" && len([]rune(e.Content)) > 50 {
					title = string([]rune(e.Content)[:50]) + "..."
				}
				if title != "" {
					ctx.RecentArtifacts = append(ctx.RecentArtifacts, title)
				}
			}
		}
	}

	if !ctx.HasMeaningfulContext() {
		return ""
	}
	return agent.BuildSituationReport(ctx)
}

// --- Event Bus Integration ---
// The event bus is available for new features to publish/subscribe domain events.
// Existing modules emit events through these helper methods.

// emitToolEvent publishes a tool execution event to the event bus.
func (h *IMMessageHandler) emitToolEvent(kind, toolName, result string) {
	if h.app == nil || h.app.ohModules.eventBus == nil {
		return
	}
	h.app.ohModules.eventBus.Publish(eventbus.ToolEvent{
		Kind:     kind,
		ToolName: toolName,
		Result:   truncateForEvent(result, 200),
	})
}

// emitMemoryEvent publishes a memory operation event.
func (h *IMMessageHandler) emitMemoryEvent(kind, entryID string) {
	if h.app == nil || h.app.ohModules.eventBus == nil {
		return
	}
	h.app.ohModules.eventBus.Publish(eventbus.MemoryEvent{
		Kind:    kind,
		EntryID: entryID,
	})
}

// emitAgentEvent publishes an agent loop event.
func (h *IMMessageHandler) emitAgentEvent(kind, userID, detail string) {
	if h.app == nil || h.app.ohModules.eventBus == nil {
		return
	}
	h.app.ohModules.eventBus.Publish(eventbus.AgentEvent{
		Kind:   kind,
		UserID: userID,
		Detail: detail,
	})
}

func truncateForEvent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isExternalSourceTool returns true for tools whose results come from
// external/untrusted sources (web pages, user files, shell output).
// Only these tools need prompt injection scanning.
func isExternalSourceTool(name string) bool {
	switch name {
	case "web_fetch", "web_search", "read_file", "bash", "ssh":
		return true
	}
	return false
}
