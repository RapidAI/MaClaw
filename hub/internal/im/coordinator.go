package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// Coordinator is the message processing middleware between the IM Adapter
// and the MessageRouter. It implements the seamless smart mode: rule engine
// first, then LLM intent classification when rules can't decide.
type Coordinator struct {
	router           *MessageRouter
	devices          DeviceFinder
	ruleEngine       *RuleEngine
	intentClassifier *IntentClassifier
	breaker          *CircuitBreaker
	configProvider   func() *HubLLMConfig
	profileCache     *DeviceProfileCache

	mu             sync.Mutex
	routeHistory   map[string][]routeHistoryEntry // userID → recent 3
	welcomedUsers  map[string]bool                // users who got the welcome msg
}

// NewCoordinator creates a Coordinator wired to the given router and config.
func NewCoordinator(
	router *MessageRouter,
	devices DeviceFinder,
	configProvider func() *HubLLMConfig,
) *Coordinator {
	breaker := DefaultCircuitBreaker()
	return &Coordinator{
		router:           router,
		devices:          devices,
		ruleEngine:       &RuleEngine{},
		intentClassifier: NewIntentClassifier(configProvider, breaker),
		breaker:          breaker,
		configProvider:   configProvider,
		profileCache:     NewDeviceProfileCache(),
		routeHistory:     make(map[string][]routeHistoryEntry),
		welcomedUsers:    make(map[string]bool),
	}
}

// IsLLMEnabled returns true if LLM is configured, enabled, and not circuit-broken.
func (c *Coordinator) IsLLMEnabled() bool {
	cfg := c.configProvider()
	if cfg == nil || !cfg.Enabled {
		return false
	}
	return c.breaker.Allow()
}

// GetLLMStatus returns the LLM health status string for the admin API.
func (c *Coordinator) GetLLMStatus() string {
	cfg := c.configProvider()
	if cfg == nil || !cfg.Enabled {
		return "not_configured"
	}
	return c.breaker.Status()
}

// ProfileCache returns the device profile cache for external wiring.
func (c *Coordinator) ProfileCache() *DeviceProfileCache {
	return c.profileCache
}

// Breaker returns the circuit breaker for external wiring (e.g. DiscussionConductor).
func (c *Coordinator) Breaker() *CircuitBreaker {
	return c.breaker
}

// HandleDeviceProfileUpdate is called by the WebSocket gateway when a machine
// sends a device.profile_update message. It parses the profile and updates the cache.
func (c *Coordinator) HandleDeviceProfileUpdate(userID string, payload json.RawMessage) {
	var profile DeviceProfile
	if err := json.Unmarshal(payload, &profile); err != nil {
		log.Printf("[Coordinator] device profile parse error for user=%s: %v", userID, err)
		return
	}
	if profile.MachineID == "" {
		log.Printf("[Coordinator] device profile missing machine_id for user=%s", userID)
		return
	}
	c.profileCache.Update(userID, profile)
	log.Printf("[Coordinator] device profile updated: user=%s machine=%s name=%s", userID, profile.MachineID, profile.Name)
}

// Coordinate is the main entry point called by the Adapter for non-command
// messages. It runs the rule engine, then LLM classification if needed,
// and dispatches to the appropriate MessageRouter method.
func (c *Coordinator) Coordinate(
	ctx context.Context,
	userID, platformName, platformUID, text string,
) (*GenericResponse, error) {
	machines := c.devices.FindAllOnlineMachinesForUser(ctx, userID)
	if len(machines) == 0 {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "📴",
			Title:      "设备不在线",
			Body:       "您的设备当前不在线，无法处理请求。\n\n请确认 MaClaw 客户端已启动并连接到 Hub。",
		}, nil
	}

	cfg := c.configProvider()
	llmEnabled := cfg != nil && cfg.Enabled && c.breaker.Allow()
	smartRouteSingle := cfg != nil && cfg.SmartRouteSingleDevice

	// Send welcome message on first interaction in smart mode.
	if llmEnabled && len(machines) > 1 {
		c.maybeWelcome(ctx, userID, platformName, platformUID)
	}

	// Get the user's current device selection from the router.
	selected, _ := c.router.GetSelectedMachine(userID)

	decision := c.ruleEngine.Evaluate(text, machines, selected, llmEnabled, smartRouteSingle)

	switch decision.Action {
	case ActionRouteToTarget:
		return c.routeToTarget(ctx, userID, platformName, platformUID, text, decision.TargetID, machines)

	case ActionBroadcast:
		return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)

	case ActionNeedClassification:
		return c.classifyAndRoute(ctx, userID, platformName, platformUID, text, machines)

	case ActionPassthrough:
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)

	default:
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)
	}
}

// classifyAndRoute uses the LLM IntentClassifier and dispatches based on result.
func (c *Coordinator) classifyAndRoute(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	profiles := c.profileCache.GetAll(userID)
	if len(profiles) == 0 {
		// No profiles reported yet — build minimal ones from OnlineMachineInfo.
		for _, m := range machines {
			profiles = append(profiles, DeviceProfile{
				MachineID:     m.MachineID,
				Name:          m.Name,
				LLMConfigured: m.LLMConfigured,
			})
		}
	}

	history := c.getRecentHistory(userID)
	result, err := c.intentClassifier.Classify(ctx, userID, text, profiles, history)
	if err != nil {
		log.Printf("[Coordinator] classify error: %v, falling back to RouteToAgent", err)
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)
	}

	// Record routing decision in history.
	c.recordHistory(userID, text, result)

	switch result.Type {
	case IntentRouteSingle:
		// Notify user about the routing decision.
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		return c.routeToTarget(ctx, userID, platformName, platformUID, text, result.TargetID, machines)

	case IntentBroadcast:
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)

	case IntentDiscuss:
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		topic := result.Topic
		if topic == "" {
			topic = text
		}
		return c.router.StartDiscussion(ctx, userID, platformName, platformUID, topic), nil

	case IntentNeedClarification:
		msg := result.Message
		if msg == "" {
			msg = "请补充更多信息，以便我判断应该发给哪台设备。"
		}
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "❓",
			Title:      "需要更多信息",
			Body:       msg,
		}, nil

	default:
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)
	}
}

// routeToTarget sends a message to a specific machine, checking LLM readiness.
func (c *Coordinator) routeToTarget(
	ctx context.Context,
	userID, platformName, platformUID, text, targetID string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	// Verify target is online and LLM-configured.
	for _, m := range machines {
		if m.MachineID == targetID {
			if !m.LLMConfigured {
				return &GenericResponse{
					StatusCode: 503,
					StatusIcon: "⚠️",
					Title:      "Agent 未就绪",
					Body:       fmt.Sprintf("设备 %s 的 LLM 未配置，Agent 无法运行。", m.Name),
				}, nil
			}
			return c.router.routeToSingleMachine(ctx, userID, platformName, platformUID, text, targetID, "")
		}
	}
	// Target not found in online list.
	return &GenericResponse{
		StatusCode: 404,
		StatusIcon: "📴",
		Title:      "设备不在线",
		Body:       "目标设备已离线。",
	}, nil
}

// notifyRouting sends a brief routing notification to the user via IM progress.
func (c *Coordinator) notifyRouting(ctx context.Context, userID, platformName, platformUID string, result *IntentResult) {
	var msg string
	switch result.Type {
	case IntentRouteSingle:
		msg = fmt.Sprintf("📍 %s", result.Reason)
	case IntentBroadcast:
		msg = fmt.Sprintf("📢 %s", result.Reason)
	case IntentDiscuss:
		msg = fmt.Sprintf("🗣️ 检测到讨论意图，已自动发起多设备讨论")
	}
	if msg != "" {
		go c.router.deliverProgress(ctx, userID, platformName, platformUID, msg)
	}
}

// maybeWelcome sends a one-time welcome message when a user first enters
// seamless smart mode (LLM enabled + multiple devices).
func (c *Coordinator) maybeWelcome(ctx context.Context, userID, platformName, platformUID string) {
	c.mu.Lock()
	if c.welcomedUsers[userID] {
		c.mu.Unlock()
		return
	}
	c.welcomedUsers[userID] = true
	c.mu.Unlock()

	machines := c.devices.FindAllOnlineMachinesForUser(ctx, userID)
	var names []string
	for _, m := range machines {
		names = append(names, m.Name)
	}

	msg := fmt.Sprintf("🤖 无感智能模式已启用\n\n"+
		"在线设备：%s\n\n"+
		"直接发消息即可，系统会自动判断发给谁。\n"+
		"也可以使用命令手动控制：/call、/call all、/discuss、/help",
		strings.Join(names, "、"))

	go c.router.deliverProgress(ctx, userID, platformName, platformUID, msg)
}

// --- route history helpers ---

func (c *Coordinator) getRecentHistory(userID string) []routeHistoryEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.routeHistory[userID]
	if len(h) == 0 {
		return nil
	}
	// Return a copy.
	out := make([]routeHistoryEntry, len(h))
	copy(out, h)
	return out
}

func (c *Coordinator) recordHistory(userID, text string, result *IntentResult) {
	target := string(result.Type)
	if result.TargetID != "" {
		target = result.TargetID
	}
	entry := routeHistoryEntry{
		Text:   truncate(text, 80),
		Target: target,
		Reason: result.Reason,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.routeHistory[userID]
	h = append(h, entry)
	if len(h) > 3 {
		h = h[len(h)-3:]
	}
	c.routeHistory[userID] = h
}
