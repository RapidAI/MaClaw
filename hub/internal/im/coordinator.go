package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const hubDirectAnswerTimeout = 15 * time.Second

// SmartRouteChecker checks whether a user has smart route (LLM) permission.
type SmartRouteChecker interface {
	// IsSmartRouteEnabled returns true if the user identified by userID
	// is allowed to use LLM-powered smart routing.
	IsSmartRouteEnabled(ctx context.Context, userID string) bool
}

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
	smartRouteCheck  SmartRouteChecker

	// Seamless chat subsystems
	convContext *conversationContextStore
	spaceState  *spaceStateStore

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
		intentClassifier: NewIntentClassifier(configProvider, breaker, router.LLMSemaphore()),
		breaker:          breaker,
		configProvider:   configProvider,
		profileCache:     NewDeviceProfileCache(),
		convContext:      newConversationContextStore(),
		spaceState:       newSpaceStateStore(),
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

// ConvContext returns the conversation context store for external access (e.g. /context command).
func (c *Coordinator) ConvContext() *conversationContextStore {
	return c.convContext
}

// SpaceState returns the space state store for external access (e.g. /call validation).
func (c *Coordinator) SpaceStateStore() *spaceStateStore {
	return c.spaceState
}

// ConvContextStats returns active context count and total rounds for the admin API.
func (c *Coordinator) ConvContextStats() (int, int) {
	return c.convContext.Stats()
}

// SetSmartRouteChecker wires the smart route permission checker.
func (c *Coordinator) SetSmartRouteChecker(checker SmartRouteChecker) {
	c.smartRouteCheck = checker
}

// IsUserSmartRouteEnabled checks if a specific user has smart route permission.
func (c *Coordinator) IsUserSmartRouteEnabled(ctx context.Context, userID string) bool {
	if c.smartRouteCheck == nil {
		return true // no checker wired → allow all (backward compat)
	}
	return c.smartRouteCheck.IsSmartRouteEnabled(ctx, userID)
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
// messages. It dispatches by space state (lobby/private/meeting), then
// applies rule engine + LLM classification within each space.
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

	// Smart route permission check: if user doesn't have permission,
	// bypass LLM entirely and use basic routing.
	if !c.IsUserSmartRouteEnabled(ctx, userID) {
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)
	}

	// Single device online → no space model, pure passthrough to lobby logic.
	state := c.spaceState.GetOrCreate(userID)

	switch state.State {
	case SpacePrivate:
		return c.handlePrivateMessage(ctx, userID, platformName, platformUID, text, state, machines)
	case SpaceMeeting:
		return c.handleMeetingMessage(ctx, userID, platformName, platformUID, text, state)
	default: // lobby
		return c.handleLobbyMessage(ctx, userID, platformName, platformUID, text, machines)
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

	// Inject conversation context into classification.
	cc := c.convContext.GetOrCreate(userID)
	convRounds := cc.GetRecentSummaries(3)

	history := c.getRecentHistory(userID)
	result, err := c.intentClassifier.Classify(ctx, userID, text, profiles, history, convRounds)
	if err != nil {
		log.Printf("[Coordinator] classify error: %v, falling back to RouteToAgent", err)
		return c.router.RouteToAgent(ctx, userID, platformName, platformUID, text)
	}

	// Record routing decision in history.
	c.recordHistory(userID, text, result)

	switch result.Type {
	case IntentDirectAnswer:
		return c.hubDirectAnswer(ctx, userID, platformName, platformUID, text, machines)

	case IntentRouteSingle:
		// Notify user about the routing decision.
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		return c.routeToTargetWithContext(ctx, userID, platformName, platformUID, text, result.TargetID, machines, result.Reason)

	case IntentBroadcast:
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)

	case IntentDiscuss:
		c.notifyRouting(ctx, userID, platformName, platformUID, result)
		topic := result.Topic
		if topic == "" {
			topic = text
		}
		// Enter meeting state.
		var participantIDs []string
		for _, m := range machines {
			participantIDs = append(participantIDs, m.MachineID)
		}
		_ = c.spaceState.EnterMeeting(userID, topic, participantIDs)
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

// routeToTargetWithContext sends a message to a specific machine, with handoff
// detection and async ConversationContext recording.
func (c *Coordinator) routeToTargetWithContext(
	ctx context.Context,
	userID, platformName, platformUID, text, targetID string,
	machines []OnlineMachineInfo,
	reason string,
) (*GenericResponse, error) {
	// Verify target is online and LLM-configured.
	var targetMachine *OnlineMachineInfo
	for i := range machines {
		if machines[i].MachineID == targetID {
			targetMachine = &machines[i]
			break
		}
	}
	if targetMachine == nil {
		return &GenericResponse{
			StatusCode: 404,
			StatusIcon: "📴",
			Title:      "设备不在线",
			Body:       "目标设备已离线。",
		}, nil
	}
	if !targetMachine.LLMConfigured {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "⚠️",
			Title:      "Agent 未就绪",
			Body:       fmt.Sprintf("设备 %s 的 LLM 未配置，Agent 无法运行。", targetMachine.Name),
		}, nil
	}

	// Check for device handoff — if last conversation was on a different device.
	cc := c.convContext.GetOrCreate(userID)
	lastRounds := cc.GetRecentSummaries(1)
	handoffCtx := ""
	if len(lastRounds) > 0 && lastRounds[0].TargetDevice != targetID && lastRounds[0].TargetDevice != "" {
		handoffCtx = cc.BuildHandoffContext(lastRounds[0].DeviceName, reason)
	}

	// Inject handoff context into the message text if switching devices.
	routeText := text
	if handoffCtx != "" {
		routeText = fmt.Sprintf("[上下文切换]\n%s\n---\n%s", handoffCtx, text)
		log.Printf("[Coordinator] session handoff for user=%s: from=%s to=%s", userID, lastRounds[0].DeviceName, targetMachine.Name)
	}

	// Route to the target machine.
	resp, err := c.router.routeToSingleMachine(ctx, userID, platformName, platformUID, routeText, targetID, "")
	if err != nil {
		return resp, err
	}

	// Async record to ConversationContext (record original text, not the handoff-prefixed one).
	if resp != nil {
		cfg := c.configProvider()
		cc.RecordRoundAsync(text, resp.Body, targetID, targetMachine.Name, cfg, c.breaker, c.router.LLMSemaphore())
	}

	return resp, err
}

// routeToTarget sends a message to a specific machine, checking LLM readiness.
// Legacy method kept for backward compatibility.
func (c *Coordinator) routeToTarget(
	ctx context.Context,
	userID, platformName, platformUID, text, targetID string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	return c.routeToTargetWithContext(ctx, userID, platformName, platformUID, text, targetID, machines, "")
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

// --- space state dispatch handlers ---

// handlePrivateMessage handles messages when user is in private chat mode.
// - Messages go directly to the private target (no IntentClassifier).
// - @ has no special meaning, sent as plain text.
// - Not recorded to public ConversationContext.
// - Every 5 messages, a status reminder is sent.
// - If target is offline, auto-return to lobby.
func (c *Coordinator) handlePrivateMessage(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	state *SpaceState,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	// Verify private target is still online.
	online := false
	for _, m := range machines {
		if m.MachineID == state.PrivateTarget {
			online = true
			break
		}
	}
	if !online {
		c.spaceState.Reset(userID)
		go c.router.deliverProgress(ctx, userID, platformName, platformUID,
			fmt.Sprintf("📴 私聊目标 %s 已离线，已返回大厅。", state.PrivateName))
		return c.handleLobbyMessage(ctx, userID, platformName, platformUID, text, machines)
	}

	// Increment message count and send periodic reminder.
	count := c.spaceState.IncrementMessageCount(userID)
	if count > 0 && count%5 == 0 {
		reminder := fmt.Sprintf("💡 当前私聊模式 → %s（第 %d 条消息）。发送 /call all 返回大厅。", state.PrivateName, count)
		go c.router.deliverProgress(ctx, userID, platformName, platformUID, reminder)
	}

	return c.router.routeToSingleMachine(ctx, userID, platformName, platformUID, text, state.PrivateTarget, "")
}

// handleMeetingMessage handles messages when user is in meeting mode.
// - ParseMentions: if @ present, validate participants and do side chat.
// - No @: inject into active discussion via InjectUserInput.
// - If no active discussion, reset to lobby.
func (c *Coordinator) handleMeetingMessage(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	state *SpaceState,
) (*GenericResponse, error) {
	if !c.router.IsInDiscussion(userID) {
		// No active discussion — defensive reset to lobby.
		c.spaceState.Reset(userID)
		go c.router.deliverProgress(ctx, userID, platformName, platformUID,
			"📋 会议已结束，已返回大厅。")
		machines := c.devices.FindAllOnlineMachinesForUser(ctx, userID)
		return c.handleLobbyMessage(ctx, userID, platformName, platformUID, text, machines)
	}

	// Parse @ mentions for side chat.
	names, body := ParseMentions(text)
	if len(names) == 0 {
		// No @: inject into discussion.
		if c.router.InjectUserInput(userID, text) {
			return &GenericResponse{
				StatusCode: 200,
				StatusIcon: "🗣️",
				Title:      "已注入会议",
				Body:       "消息已注入当前讨论。",
			}, nil
		}
		return &GenericResponse{
			StatusCode: 429,
			StatusIcon: "⏳",
			Title:      "缓冲已满",
			Body:       "发言过多，请稍后再试。",
		}, nil
	}

	// Resolve @ names to participant machineIDs.
	machines := c.devices.FindAllOnlineMachinesForUser(ctx, userID)
	machineByName := make(map[string]OnlineMachineInfo)
	for _, m := range machines {
		machineByName[strings.ToLower(m.Name)] = m
	}
	participantSet := make(map[string]bool)
	for _, p := range state.Participants {
		participantSet[p] = true
	}

	var targets []OnlineMachineInfo
	var notFound []string
	for _, name := range names {
		m, ok := machineByName[strings.ToLower(name)]
		if !ok {
			notFound = append(notFound, name)
			continue
		}
		if !participantSet[m.MachineID] {
			notFound = append(notFound, name+"(非参与者)")
			continue
		}
		targets = append(targets, m)
	}
	if len(notFound) > 0 && len(targets) == 0 {
		return &GenericResponse{
			StatusCode: 400,
			StatusIcon: "⚠️",
			Title:      "参与者未找到",
			Body:       fmt.Sprintf("未找到会议参与者: %s", strings.Join(notFound, "、")),
		}, nil
	}

	// Side chat: send to each @'d device sequentially, collect replies.
	if body == "" {
		body = text
	}
	var replies []string
	for _, t := range targets {
		resp, err := c.router.routeToSingleMachine(ctx, userID, platformName, platformUID, body, t.MachineID, "")
		if err != nil {
			replies = append(replies, fmt.Sprintf("[%s] ❌ %v", t.Name, err))
		} else if resp != nil {
			replies = append(replies, fmt.Sprintf("[%s] %s", t.Name, resp.Body))
		}
	}

	// Format side chat result and inject into discussion context.
	var targetNames []string
	for _, t := range targets {
		targetNames = append(targetNames, t.Name)
	}
	sideChatMsg := fmt.Sprintf("💬 小会（%s）：\n用户: %s\n%s",
		strings.Join(targetNames, "、"), truncate(body, 80), strings.Join(replies, "\n"))
	c.router.InjectUserInput(userID, sideChatMsg)

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "💬",
		Title:      "小会回复",
		Body:       sideChatMsg,
	}, nil
}

// handleLobbyMessage handles messages in lobby mode (default).
// - ParseMentions: @ at start → directed send to @'d devices.
// - No @: RuleEngine → IntentClassifier → dispatch.
// - direct_answer → hubDirectAnswer.
// - discuss → enter meeting state.
// - route_single → routeToTarget with handoff detection.
// - Async RecordRound to ConversationContext.
func (c *Coordinator) handleLobbyMessage(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	cfg := c.configProvider()
	llmEnabled := cfg != nil && cfg.Enabled && c.breaker.Allow()
	smartRouteSingle := cfg != nil && cfg.SmartRouteSingleDevice

	// Send welcome message on first interaction in smart mode.
	if llmEnabled && len(machines) > 1 {
		c.maybeWelcome(ctx, userID, platformName, platformUID)
	}

	// Parse @ mentions for directed send in lobby.
	names, body := ParseMentions(text)
	if len(names) > 0 {
		return c.handleLobbyMention(ctx, userID, platformName, platformUID, names, body, text, machines)
	}

	selected, _ := c.router.GetSelectedMachine(userID)
	decision := c.ruleEngine.Evaluate(text, machines, selected, llmEnabled, smartRouteSingle)

	switch decision.Action {
	case ActionRouteToTarget:
		return c.routeToTargetWithContext(ctx, userID, platformName, platformUID, text, decision.TargetID, machines, "")
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

// handleLobbyMention handles @ mentions in lobby mode.
// @ in lobby = directed send to the @'d devices, transparent to others.
func (c *Coordinator) handleLobbyMention(
	ctx context.Context,
	userID, platformName, platformUID string,
	names []string, body, originalText string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	machineByName := make(map[string]OnlineMachineInfo)
	for _, m := range machines {
		machineByName[strings.ToLower(m.Name)] = m
	}

	var targets []OnlineMachineInfo
	var notFound []string
	for _, name := range names {
		m, ok := machineByName[strings.ToLower(name)]
		if !ok {
			notFound = append(notFound, name)
			continue
		}
		targets = append(targets, m)
	}

	if len(targets) == 0 {
		// No valid targets — fall back to normal routing.
		return c.classifyAndRoute(ctx, userID, platformName, platformUID, originalText, machines)
	}

	if body == "" {
		body = originalText
	}

	// Single target: route directly.
	if len(targets) == 1 {
		return c.routeToTargetWithContext(ctx, userID, platformName, platformUID, body, targets[0].MachineID, machines, "")
	}

	// Multiple targets: parallel send via routeToMultiple.
	var targetIDs []string
	for _, t := range targets {
		targetIDs = append(targetIDs, t.MachineID)
	}
	return c.router.routeToMultiple(ctx, userID, platformName, platformUID, body, targetIDs)
}

// hubDirectAnswer uses the Hub LLM to directly answer a user question
// without routing to any device. 15s timeout → degrade to broadcast.
func (c *Coordinator) hubDirectAnswer(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	cfg := c.configProvider()
	if cfg == nil || !cfg.Enabled {
		return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)
	}

	// Build prompt with recent conversation context.
	cc := c.convContext.GetOrCreate(userID)
	recent := cc.GetRecentSummaries(5)

	var contextBlock string
	if len(recent) > 0 {
		var b strings.Builder
		b.WriteString("\n\n最近对话上下文：\n")
		for _, r := range recent {
			fmt.Fprintf(&b, "- [%s] 用户: %s → 摘要: %s\n", r.DeviceName, truncate(r.UserText, 50), truncate(r.Summary, 60))
		}
		contextBlock = b.String()
	}

	systemPrompt := `你是一个编程助手，直接回答用户的技术问题。回答要简洁准确。
如果问题需要访问用户的项目文件、代码或运行命令，请回复 JSON: {"need_device": true}
否则直接回答问题。` + contextBlock

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": text},
	}

	llmCfg := cfg.ToMaclawLLMConfig()
	client := &http.Client{Timeout: hubDirectAnswerTimeout}

	answerCtx, cancel := context.WithTimeout(ctx, hubDirectAnswerTimeout)
	defer cancel()

	type llmResult struct {
		resp *agent.LLMSimpleResponse
		err  error
	}
	ch := make(chan llmResult, 1)
	sem := c.router.LLMSemaphore()
	go func() {
		// Acquire LLM semaphore inside the goroutine so Release happens
		// only after the LLM call completes, not when the caller times out.
		if !sem.Acquire(answerCtx) {
			ch <- llmResult{nil, fmt.Errorf("LLM semaphore timeout")}
			return
		}
		defer sem.Release()
		r, e := agent.DoSimpleLLMRequest(llmCfg, messages, client, hubDirectAnswerTimeout)
		ch <- llmResult{r, e}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			log.Printf("[Coordinator] hubDirectAnswer LLM error: %v, degrading to broadcast", res.err)
			c.breaker.RecordFailure()
			return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)
		}
		c.breaker.RecordSuccess()

		content := strings.TrimSpace(res.resp.Content)

		// Check for need_device degradation.
		if strings.Contains(content, `"need_device"`) && strings.Contains(content, "true") {
			log.Printf("[Coordinator] hubDirectAnswer returned need_device, degrading to normal routing")
			return c.classifyAndRouteNonDirect(ctx, userID, platformName, platformUID, text, machines)
		}

		// Record to conversation context.
		cc.RecordRoundAsync(text, content, "hub", "Hub AI", cfg, c.breaker, c.router.LLMSemaphore())

		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "🤖",
			Title:      "Hub AI",
			Body:       content + "\n\n — Hub AI",
		}, nil

	case <-answerCtx.Done():
		log.Printf("[Coordinator] hubDirectAnswer timeout for user=%s, degrading to broadcast", userID)
		return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)
	}
}

// classifyAndRouteNonDirect re-classifies excluding direct_answer, falling back to broadcast.
func (c *Coordinator) classifyAndRouteNonDirect(
	ctx context.Context,
	userID, platformName, platformUID, text string,
	machines []OnlineMachineInfo,
) (*GenericResponse, error) {
	// Simple fallback: broadcast to all devices.
	return c.router.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)
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
