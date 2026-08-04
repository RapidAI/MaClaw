package im

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// ---------------------------------------------------------------------------
// Dependency interfaces for MessageRouter
// ---------------------------------------------------------------------------

// DeviceFinder abstracts the device service for looking up user machines.
type DeviceFinder interface {
	// FindOnlineMachineForUser returns the machine ID of an online device
	// belonging to the given user. Returns ("", false) if no device is online.
	FindOnlineMachineForUser(ctx context.Context, userID string) (machineID string, llmConfigured bool, found bool)
	// FindAllOnlineMachinesForUser returns all online machines for the user.
	FindAllOnlineMachinesForUser(ctx context.Context, userID string) []OnlineMachineInfo
	// FindOnlineMachineByName returns the machine ID matching the given name
	// (case-insensitive) for the user. Returns ("", false) if not found.
	FindOnlineMachineByName(ctx context.Context, userID, name string) (machineID string, found bool)
	// SendToMachine sends a JSON-serialisable message to the machine via WebSocket.
	SendToMachine(machineID string, msg any) error
}

// OnlineMachineInfo holds summary info about an online machine for IM display.
type OnlineMachineInfo struct {
	MachineID     string
	Name          string
	LLMConfigured bool
}

// ---------------------------------------------------------------------------
// PendingIMRequest — tracks an in-flight IM → Agent request
// ---------------------------------------------------------------------------

// PendingIMRequest represents a message waiting for the Agent's reply.
type PendingIMRequest struct {
	RequestID   string
	TenantID    string
	UserID      string
	PlatformUID string // original platform-specific user ID for progress delivery
	Text        string
	ResponseCh  chan *AgentResponse
	CreatedAt   time.Time
	Timeout     time.Duration

	// ProgressCh receives progress text updates from the Agent. Each update
	// resets the response timeout so long-running tasks don't expire.
	ProgressCh chan string

	// LastActivity tracks the most recent progress or creation time.
	// Used by cleanupExpired to avoid premature reaping of requests
	// that are being kept alive by progress updates.
	lastActivity time.Time

	// VoiceParts is assembled from bounded im.agent_voice_part frames. The
	// final im.agent_response contains text/metadata only and consumes this
	// stream atomically when every expected part has arrived.
	voiceParts      map[int]VoicePart
	voicePartsTotal int
	voicePartsBytes int
	voicePartsBad   bool
}

// defaultAgentTimeout is the maximum time to wait for an Agent response.
// 多轮 Agent 循环（最多 12 轮 LLM 调用）可能需要较长时间
const defaultAgentTimeout = 180 * time.Second

// cleanupInterval controls how often expired pending requests are reaped.
const cleanupInterval = 30 * time.Second

// requestIDCounter is an atomic counter to ensure unique request IDs
// even when multiple goroutines generate them at the same nanosecond.
var requestIDCounter atomic.Uint64

// progressHeartbeat is the sentinel value sent by the client to keep the
// response timer alive without delivering a visible message to the user.
const progressHeartbeat = "__heartbeat__"

// ---------------------------------------------------------------------------
// broadcastProgressDedup — cross-device progress deduplication for broadcast
// ---------------------------------------------------------------------------

type ctxKeyBroadcastDedup struct{}

// broadcastProgressDedup tracks progress texts already delivered across
// multiple concurrent routeToSingleMachine calls within a single broadcast.
// The raw text (without [deviceName] prefix) is used as the dedup key so
// identical acknowledgments from different devices are suppressed.
type broadcastProgressDedup struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newBroadcastProgressDedup() *broadcastProgressDedup {
	return &broadcastProgressDedup{seen: make(map[string]struct{})}
}

// tryDeliver returns true if the text has not been seen before (first caller wins).
func (d *broadcastProgressDedup) tryDeliver(rawText string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[rawText]; ok {
		return false
	}
	d.seen[rawText] = struct{}{}
	return true
}

func withBroadcastDedup(ctx context.Context, d *broadcastProgressDedup) context.Context {
	return context.WithValue(ctx, ctxKeyBroadcastDedup{}, d)
}

func getBroadcastDedup(ctx context.Context) *broadcastProgressDedup {
	if v, ok := ctx.Value(ctxKeyBroadcastDedup{}).(*broadcastProgressDedup); ok {
		return v
	}
	return nil
}

// ---------------------------------------------------------------------------
// MessageRouter — routes IM messages to MaClaw Agent via WebSocket
// ---------------------------------------------------------------------------

// ProgressDeliveryFunc is called to deliver progress text to a user via IM.
type ProgressDeliveryFunc func(ctx context.Context, userID, platformName, platformUID, text string)

// ResponseDeliveryFunc is called to deliver a full GenericResponse to a user
// via IM. Used by routeBroadcast to deliver image/file responses individually.
type ResponseDeliveryFunc func(ctx context.Context, userID, platformName, platformUID string, resp *GenericResponse)

// MessageRouter replaces the old NL_Router + BridgeExecutor pipeline.
// It transparently relays IM messages to the user's MaClaw client Agent
// and waits for the Agent's response.
type MessageRouter struct {
	devices          DeviceFinder
	progressDelivery ProgressDeliveryFunc
	responseDelivery ResponseDeliveryFunc
	conductor        *DiscussionConductor // optional; nil = mechanical rounds
	llmSem           *LLMSemaphore        // global LLM concurrency limiter (legacy/fallback)
	llmSemGUI        *LLMSemaphore        // GUI-originated LLM concurrency limiter
	llmSemIM         *LLMSemaphore        // IM-originated LLM concurrency limiter

	mu          sync.Mutex
	pendingReqs map[string]*PendingIMRequest // requestID → pending

	// selectedMachine tracks the user's chosen machine for IM routing.
	// Key: userID, Value: machineID. Protected by mu.
	selectedMachine map[string]string

	// pendingAttachments temporarily holds attachments for the current
	// message being routed. Key: userID. Set by StashAttachments before
	// RouteToAgent, consumed by routeToSingleMachine. Protected by mu.
	pendingAttachments map[string][]MessageAttachment

	// pendingMessageTypes temporarily holds the structural input modality for
	// the current message being routed. Key: userID.
	pendingMessageTypes       map[string]string
	pendingClientCapabilities map[string]*agent.ClientCapabilities

	// discussions tracks active /discuss sessions per user. Protected by mu.
	discussions map[string]*DiscussionState

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewMessageRouter creates a MessageRouter with the given device finder.
func NewMessageRouter(devices DeviceFinder) *MessageRouter {
	r := &MessageRouter{
		devices:                   devices,
		pendingReqs:               make(map[string]*PendingIMRequest),
		selectedMachine:           make(map[string]string),
		pendingAttachments:        make(map[string][]MessageAttachment),
		pendingMessageTypes:       make(map[string]string),
		pendingClientCapabilities: make(map[string]*agent.ClientCapabilities),
		discussions:               make(map[string]*DiscussionState),
		llmSem:                    NewLLMSemaphore(DefaultMaxConcurrent),
		llmSemGUI:                 NewLLMSemaphore(DefaultMaxConcurrentGUI),
		llmSemIM:                  NewLLMSemaphore(DefaultMaxConcurrentIM),
		stopCh:                    make(chan struct{}),
	}
	go r.cleanupLoop()
	return r
}

// StashAttachments stores attachments for the current message being routed.
// Called by the IM Adapter before RouteToAgent so that routeToSingleMachine
// can include them in the WebSocket payload without changing the routing API.
func (r *MessageRouter) StashAttachments(userID string, attachments []MessageAttachment) {
	r.StashAttachmentsForTenant("", userID, attachments)
}

func (r *MessageRouter) StashAttachmentsForTenant(tenantID, userID string, attachments []MessageAttachment) {
	if len(attachments) == 0 {
		return
	}
	r.mu.Lock()
	r.pendingAttachments[tenantUserRuntimeKey(tenantID, userID)] = attachments
	r.mu.Unlock()
}

// popAttachments retrieves and removes stashed attachments for a user.
func (r *MessageRouter) popAttachments(ctx context.Context, userID string) []MessageAttachment {
	key := routerRuntimeKey(ctx, userID)
	r.mu.Lock()
	att := r.pendingAttachments[key]
	delete(r.pendingAttachments, key)
	r.mu.Unlock()
	return att
}

// StashMessageType stores the structural input modality for the current routed message.
func (r *MessageRouter) StashMessageType(userID, messageType string) {
	r.StashMessageTypeForTenant("", userID, messageType)
}

func (r *MessageRouter) StashMessageTypeForTenant(tenantID, userID, messageType string) {
	if messageType == "" {
		messageType = "text"
	}
	r.mu.Lock()
	r.pendingMessageTypes[tenantUserRuntimeKey(tenantID, userID)] = messageType
	r.mu.Unlock()
}

func (r *MessageRouter) currentMessageType(ctx context.Context, userID string) string {
	key := routerRuntimeKey(ctx, userID)
	r.mu.Lock()
	messageType := r.pendingMessageTypes[key]
	r.mu.Unlock()
	if messageType == "" {
		return "text"
	}
	return messageType
}

func (r *MessageRouter) StashClientCapabilitiesForTenant(tenantID, userID string, capabilities *agent.ClientCapabilities) {
	key := tenantUserRuntimeKey(tenantID, userID)
	r.mu.Lock()
	if capabilities == nil {
		delete(r.pendingClientCapabilities, key)
	} else {
		normalized := agent.NormalizeClientCapabilities(capabilities)
		r.pendingClientCapabilities[key] = &normalized
	}
	r.mu.Unlock()
}

func (r *MessageRouter) popClientCapabilities(ctx context.Context, userID string) *agent.ClientCapabilities {
	key := routerRuntimeKey(ctx, userID)
	r.mu.Lock()
	capabilities := r.pendingClientCapabilities[key]
	delete(r.pendingClientCapabilities, key)
	r.mu.Unlock()
	return capabilities
}

// LLMSemaphore returns the shared LLM concurrency semaphore so that
// other components (Coordinator, IntentClassifier, etc.) can share it.
func (r *MessageRouter) LLMSemaphore() *LLMSemaphore {
	return r.llmSem
}

// LLMSemaphoreForSource returns the per-source semaphore. Use "gui" for
// desktop AI assistant requests and "im" for IM (Feishu/WeChat/etc.).
// Falls back to the global semaphore for unknown sources.
func (r *MessageRouter) LLMSemaphoreForSource(source string) *LLMSemaphore {
	switch source {
	case "gui":
		return r.llmSemGUI
	case "im":
		return r.llmSemIM
	default:
		return r.llmSem
	}
}

// ResizeSourceSemaphores updates the per-source semaphore capacities.
func (r *MessageRouter) ResizeSourceSemaphores(guiCap, imCap int) {
	r.llmSemGUI.Resize(guiCap)
	r.llmSemIM.Resize(imCap)
}

// MachineSelectResult is returned by SelectMachine / TrySelectByName.
type MachineSelectResult struct {
	OK          bool   // selection succeeded
	Message     string // human-readable status message
	MachineID   string // selected machine ID (empty for broadcast or failure)
	MachineName string // selected machine name
}

// broadcastMachineID is the sentinel value stored in selectedMachine to
// indicate that the user is in broadcast mode (/call all).
const broadcastMachineID = "__all__"

func routerRuntimeKey(ctx context.Context, userID string) string {
	return tenantUserRuntimeKey(tenantIDFromContext(ctx), userID)
}

// SelectMachine explicitly sets the target machine for a user (via /call).
func (r *MessageRouter) SelectMachine(ctx context.Context, userID, name string) MachineSelectResult {
	key := routerRuntimeKey(ctx, userID)
	machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
	if len(machines) == 0 {
		return MachineSelectResult{OK: false, Message: i18n.T(i18n.MsgNoOnlineDevices, "zh")}
	}
	if len(machines) == 1 {
		return MachineSelectResult{OK: true, Message: fmt.Sprintf("当前只有一台在线设备 %s，无需切换。", machines[0].Name)}
	}

	// "/call all" — enter broadcast mode.
	if strings.EqualFold(name, "all") {
		r.mu.Lock()
		r.selectedMachine[key] = broadcastMachineID
		r.mu.Unlock()
		var names []string
		for _, m := range machines {
			names = append(names, m.Name)
		}
		guide := fmt.Sprintf("已进入群聊模式\n在线设备：%s\n\n使用方式：\n• 直接发消息 → 所有设备同时回复\n• @昵称 消息 → 只发给指定设备\n• /call <昵称> → 切回单聊\n• /discuss 话题 → 发起多轮讨论\n• /stop → 停止讨论", strings.Join(names, "、"))
		return MachineSelectResult{
			OK:      true,
			Message: guide,
		}
	}

	// Find all matches (case-insensitive) to detect duplicates.
	var matched []OnlineMachineInfo
	for _, m := range machines {
		if strings.EqualFold(m.Name, name) {
			matched = append(matched, m)
		}
	}

	if len(matched) == 0 {
		list := r.formatMachineList(machines)
		return MachineSelectResult{
			OK:      false,
			Message: fmt.Sprintf("未找到名为 %q 的在线设备。\n\n%s", name, list),
		}
	}

	if len(matched) > 1 {
		list := r.formatMachineList(machines)
		return MachineSelectResult{
			OK:      false,
			Message: fmt.Sprintf("有 %d 台设备同名 %q，请先在客户端修改昵称使其唯一，然后重试。\n\n%s", len(matched), name, list),
		}
	}

	r.mu.Lock()
	r.selectedMachine[key] = matched[0].MachineID
	r.mu.Unlock()

	return MachineSelectResult{
		OK:          true,
		Message:     fmt.Sprintf("已切换设备，你当前正在与 %s 交流。", matched[0].Name),
		MachineID:   matched[0].MachineID,
		MachineName: matched[0].Name,
	}
}

// TrySelectByName attempts to match the text against an online machine name.
// Returns (true, response) if the text matched a machine name (switch or error).
// Returns (false, nil) if no match — caller should route as normal message.
func (r *MessageRouter) TrySelectByName(ctx context.Context, userID, text string) (handled bool, resp *GenericResponse) {
	key := routerRuntimeKey(ctx, userID)
	machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
	// Only attempt name-based switching when multiple machines are online.
	if len(machines) <= 1 {
		return false, nil
	}

	// Count matches.
	var matched []OnlineMachineInfo
	for _, m := range machines {
		if strings.EqualFold(m.Name, text) {
			matched = append(matched, m)
		}
	}

	if len(matched) == 0 {
		return false, nil
	}

	if len(matched) > 1 {
		list := r.formatMachineList(machines)
		return true, &GenericResponse{
			StatusCode: 409,
			StatusIcon: "warning",
			Title:      "设备重名",
			Body:       fmt.Sprintf("有 %d 台设备同名 %q，请先在客户端修改昵称使其唯一。\n\n%s\n\n修改后使用 /call <昵称> 切换。", len(matched), text, list),
		}
	}

	// If the matched machine is already the current selection, don't intercept —
	// let the text pass through to the Agent as a normal message.
	r.mu.Lock()
	current := r.selectedMachine[key]
	r.mu.Unlock()
	if current == matched[0].MachineID {
		return false, nil
	}

	// Switch to the new machine.
	r.mu.Lock()
	r.selectedMachine[key] = matched[0].MachineID
	r.mu.Unlock()

	return true, &GenericResponse{
		StatusCode: 200,
		StatusIcon: "ok",
		Title:      "已切换设备",
		Body:       fmt.Sprintf("已切换设备，你当前正在与 %s 交流。", matched[0].Name),
	}
}

// GetSelectedMachine returns the currently selected machine for a user.
func (r *MessageRouter) GetSelectedMachine(userID string) (machineID string, ok bool) {
	return r.GetSelectedMachineForTenant("", userID)
}

func (r *MessageRouter) GetSelectedMachineForTenant(tenantID, userID string) (machineID string, ok bool) {
	r.mu.Lock()
	mid, ok := r.selectedMachine[tenantUserRuntimeKey(tenantID, userID)]
	r.mu.Unlock()
	return mid, ok
}

// ClearSelectedMachine removes the machine selection for a user.
func (r *MessageRouter) ClearSelectedMachine(userID string) {
	r.ClearSelectedMachineForTenant("", userID)
}

func (r *MessageRouter) ClearSelectedMachineForTenant(tenantID, userID string) {
	r.mu.Lock()
	delete(r.selectedMachine, tenantUserRuntimeKey(tenantID, userID))
	r.mu.Unlock()
}

// formatMachineList builds a human-readable list of online machines.
func (r *MessageRouter) formatMachineList(machines []OnlineMachineInfo) string {
	var b strings.Builder
	b.WriteString("在线设备列表：\n")
	for i, m := range machines {
		llm := ""
		if m.LLMConfigured {
			llm = ""
		}
		fmt.Fprintf(&b, "%d. %s (LLM: %s)\n", i+1, m.Name, llm)
	}
	b.WriteString("\n使用 /call <昵称> 切换设备。")
	return b.String()
}

// SetProgressDelivery configures the function used to deliver progress
// updates to users via IM. Called by the Adapter after construction.
func (r *MessageRouter) SetProgressDelivery(fn ProgressDeliveryFunc) {
	r.progressDelivery = fn
}

// SetResponseDelivery configures the function used to deliver full
// GenericResponse messages to users via IM. Used by routeBroadcast
// to deliver image/file responses individually.
func (r *MessageRouter) SetResponseDelivery(fn ResponseDeliveryFunc) {
	r.responseDelivery = fn
}

// SetConductor wires the LLM DiscussionConductor into the router.
// When set, StartDiscussion delegates to the conductor if LLM is available.
func (r *MessageRouter) SetConductor(dc *DiscussionConductor) {
	r.conductor = dc
}

// Stop terminates the background cleanup goroutine.
func (r *MessageRouter) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// RouteToAgent sends the user's IM message to their bound MaClaw device
// and blocks until the Agent replies or the timeout expires.
//
// Preconditions: identity mapping and rate limiting have already been applied
// by the Adapter before calling this method.
func (r *MessageRouter) RouteToAgent(ctx context.Context, userID, platformName, platformUID, text string) (*GenericResponse, error) {
	tenantID := tenantIDFromContext(ctx)
	key := tenantUserRuntimeKey(tenantID, userID)
	// 0. Parse @name prefix for targeted send in broadcast mode.
	var targetName string
	if strings.HasPrefix(text, "@") {
		if idx := strings.IndexByte(text, ' '); idx > 1 {
			targetName = text[1:idx]
			text = strings.TrimSpace(text[idx+1:])
		}
	}

	// 1. Resolve the target machine(s).
	machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
	if len(machines) == 0 {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "info",
			Title:      "设备不在线",
			Body:       "您的设备当前不在线，无法处理请求。\n\n请确认 MaClaw 客户端已启动并连接到 Hub。",
		}, nil
	}

	// If @name was specified, route to that specific machine regardless of mode.
	if targetName != "" {
		for _, m := range machines {
			if strings.EqualFold(m.Name, targetName) {
				if !m.LLMConfigured {
					return &GenericResponse{
						StatusCode: 503,
						StatusIcon: "warning",
						Title:      "Agent 未就绪",
						Body:       fmt.Sprintf("设备 %s 的 LLM 未配置，Agent 无法运行。", m.Name),
					}, nil
				}
				resp, err := r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, "")
				return resp, err
			}
		}
		return &GenericResponse{
			StatusCode: 404,
			StatusIcon: "info",
			Title:      "设备未找到",
			Body:       fmt.Sprintf("未找到名为 %q 的在线设备。", targetName),
		}, nil
	}

	if len(machines) == 1 {
		// Single machine — auto-select.
		m := machines[0]
		r.mu.Lock()
		r.selectedMachine[key] = m.MachineID
		r.mu.Unlock()
		if !m.LLMConfigured {
			return &GenericResponse{
				StatusCode: 503,
				StatusIcon: "warning",
				Title:      "Agent 未就绪",
				Body:       "设备已在线，但 MaClaw LLM 未配置。Agent 无法运行。\n\n请在 MaClaw 客户端的设置中配置 LLM（URL、Key、Model），然后重试。",
			}, nil
		}
		return r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, "")
	}

	// Multiple machines.
	r.mu.Lock()
	selected := r.selectedMachine[key]
	r.mu.Unlock()

	if selected == "" {
		list := r.formatMachineList(machines)
		return &GenericResponse{
			StatusCode: 300,
			StatusIcon: "info",
			Title:      "请选择设备",
			Body:       fmt.Sprintf("您有 %d 台设备在线，请先选择目标设备：\n\n%s", len(machines), list),
		}, nil
	}

	// Broadcast mode.
	if selected == broadcastMachineID {
		return r.routeBroadcast(ctx, userID, platformName, platformUID, text, machines)
	}

	// Single-machine selection — verify still online.
	for _, m := range machines {
		if m.MachineID == selected {
			if !m.LLMConfigured {
				return &GenericResponse{
					StatusCode: 503,
					StatusIcon: "warning",
					Title:      "Agent 未就绪",
					Body:       "设备已在线，但 MaClaw LLM 未配置。Agent 无法运行。\n\n请在 MaClaw 客户端的设置中配置 LLM（URL、Key、Model），然后重试。",
				}, nil
			}
			return r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, "")
		}
	}

	// Selected machine went offline.
	r.mu.Lock()
	delete(r.selectedMachine, key)
	r.mu.Unlock()
	list := r.formatMachineList(machines)
	return &GenericResponse{
		StatusCode: 503,
		StatusIcon: "info",
		Title:      "设备已离线",
		Body:       fmt.Sprintf("之前选择的设备已离线，请重新选择：\n\n%s", list),
	}, nil
}

// routeToSingleMachine sends a message to one machine and waits for the reply.
// If namePrefix is non-empty, progress and response text are prefixed with [namePrefix].
// extraAttachments, if non-nil, are used instead of popping from the stash (used by broadcast).
func (r *MessageRouter) routeToSingleMachine(ctx context.Context, userID, platformName, platformUID, text, machineID, namePrefix string, extraAttachments ...[]MessageAttachment) (*GenericResponse, error) {
	seq := requestIDCounter.Add(1)
	requestID := fmt.Sprintf("im_%s_%d_%d", userID, time.Now().UnixNano(), seq)
	now := time.Now()
	pending := &PendingIMRequest{
		RequestID:    requestID,
		TenantID:     tenantIDFromContext(ctx),
		UserID:       userID,
		PlatformUID:  platformUID,
		Text:         text,
		ResponseCh:   make(chan *AgentResponse, 1),
		ProgressCh:   make(chan string, 8),
		CreatedAt:    now,
		Timeout:      defaultAgentTimeout,
		lastActivity: now,
	}

	r.mu.Lock()
	r.pendingReqs[requestID] = pending
	r.mu.Unlock()

	// Ensure cleanup on all exit paths.
	defer func() {
		r.mu.Lock()
		delete(r.pendingReqs, requestID)
		r.mu.Unlock()
	}()

	// 4. Send im.user_message to MaClaw client via WebSocket.
	var attachments []MessageAttachment
	if len(extraAttachments) > 0 && len(extraAttachments[0]) > 0 {
		attachments = extraAttachments[0]
	} else {
		attachments = r.popAttachments(ctx, userID)
	}
	clientCapabilities := r.popClientCapabilities(ctx, userID)
	wsMsg := map[string]interface{}{
		"type":       "im.user_message",
		"request_id": requestID,
		"ts":         time.Now().Unix(),
		"payload": map[string]interface{}{
			"user_id":      userID,
			"platform":     platformName,
			"message_type": r.currentMessageType(ctx, userID),
			"text":         text,
			"lang":         "zh",
		},
	}
	if len(attachments) > 0 {
		wsMsg["payload"].(map[string]interface{})["attachments"] = attachments
	}
	if clientCapabilities != nil {
		wsMsg["payload"].(map[string]interface{})["client_capabilities"] = clientCapabilities
	}
	if launch := startMenuTaskFromContext(ctx); launch != nil {
		wsMsg["payload"].(map[string]interface{})["start_menu"] = startMenuTaskPayload(launch)
	}
	if err := r.devices.SendToMachine(machineID, wsMsg); err != nil {
		log.Printf("[MessageRouter] SendToMachine failed for machine=%s: %v", machineID, err)
		body := "无法将消息发送到您的设备，请检查连接状态。"
		if namePrefix != "" {
			body = fmt.Sprintf("[%s] %s", namePrefix, body)
		}
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "info",
			Title:      "发送失败",
			Body:       body,
		}, nil
	}

	// 5. Wait for Agent response with resettable timeout. Read the timeout while
	// holding the pending-request lock so test/runtime configuration changes do
	// not race with timer creation.
	r.mu.Lock()
	timeout := pending.Timeout
	r.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var progressTexts []string
	var lastDelivered time.Time
	var lastProgressText string
	const progressMinInterval = 10 * time.Second

	for {
		select {
		case resp := <-pending.ResponseCh:
			if resp == nil {
				body := "Agent 未返回有效回复，请稍后重试。"
				if namePrefix != "" {
					body = fmt.Sprintf("[%s] %s", namePrefix, body)
				}
				return &GenericResponse{
					StatusCode: 500,
					StatusIcon: "error",
					Title:      "Agent 返回空响应",
					Body:       body,
				}, nil
			}
			// Deferred: media was buffered on the client side, waiting for
			// user intent. Do not deliver anything to the IM user — the
			// client will send a real response (or a timeout prompt via
			// progress) later.
			if resp.Deferred {
				log.Printf("[MessageRouter] request_id=%s deferred (media buffered), suppressing IM reply", pending.RequestID)
				return nil, nil
			}
			gr := resp.ToGenericResponse()
			if namePrefix != "" {
				if gr.Body != "" {
					gr.Body = fmt.Sprintf("[%s] %s", namePrefix, gr.Body)
				} else {
					gr.Body = fmt.Sprintf("[%s]", namePrefix)
				}
			}
			return gr, nil

		case progressText := <-pending.ProgressCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			r.mu.Lock()
			timeout := pending.Timeout
			r.mu.Unlock()
			timer.Reset(timeout)

			// Silent heartbeat — only resets the timer, never delivered to user.
			if progressText == progressHeartbeat {
				continue
			}

			progressTexts = append(progressTexts, progressText)

			// Intermediate status messages (e.g. "正在耐心处理中", "正在执行工具")
			// are never delivered to IM users. They only serve to reset the
			// timeout timer above. Delivering them floods the chat with
			// repetitive noise that adds no value.
			if isIntermediateStatusProgress(progressText) {
				continue
			}

			isDup := progressText == lastProgressText
			lastProgressText = progressText

			if time.Since(lastDelivered) >= progressMinInterval && !isDup {
				// Cross-device dedup: in broadcast mode, suppress identical
				// progress texts already sent by another device.
				if dedup := getBroadcastDedup(ctx); dedup != nil {
					if !dedup.tryDeliver(progressText) {
						continue // already delivered by another device, skip
					}
				}
				lastDelivered = time.Now()
				deliverText := progressText
				if namePrefix != "" {
					deliverText = fmt.Sprintf("[%s] %s", namePrefix, progressText)
				}
				go r.deliverProgress(ctx, userID, platformName, platformUID, deliverText)
			}

		case <-timer.C:
			body := "Agent 在 180 秒内未回复，请稍后重试。\n\n可能原因：LLM 服务响应缓慢或不可用。"
			if len(progressTexts) > 0 {
				body = fmt.Sprintf("Agent 任务执行超时。最后状态：%s\n\n任务可能仍在后台运行，请稍后查询结果。", progressTexts[len(progressTexts)-1])
			}
			if namePrefix != "" {
				body = fmt.Sprintf("[%s] %s", namePrefix, body)
			}
			return &GenericResponse{
				StatusCode: 504,
				StatusIcon: "info",
				Title:      "Agent 响应超时",
				Body:       body,
			}, nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func startMenuTaskPayload(launch *StartMenuTaskLaunch) map[string]interface{} {
	payload := map[string]interface{}{
		"title":      strings.TrimSpace(launch.Title),
		"task_text":  strings.TrimSpace(launch.TaskText),
		"agent_mode": strings.TrimSpace(launch.AgentMode),
		"platform":   strings.TrimSpace(launch.Platform),
		"target_uid": strings.TrimSpace(launch.TargetUID),
	}
	if env := launch.CodingEnv; env != nil {
		if dir := strings.TrimSpace(env.WorkingDir); dir != "" {
			payload["working_dir"] = dir
		}
		if remote := env.Remote; remote != nil {
			port := remote.Port
			if port <= 0 || port > 65535 {
				port = 22
			}
			payload["remote_host"] = strings.TrimSpace(remote.Host)
			payload["remote_port"] = port
			payload["remote_user"] = strings.TrimSpace(remote.User)
			payload["remote_dir"] = strings.TrimSpace(remote.WorkDir)
		}
	}
	return payload
}

// routeBroadcast sends the message to all online machines concurrently and
// collects responses. Each machine's response/progress is prefixed with
// [machineName]. Image and file responses are delivered individually via
// responseDelivery; text-only responses are combined into one message.
func (r *MessageRouter) routeBroadcast(ctx context.Context, userID, platformName, platformUID, text string, machines []OnlineMachineInfo) (*GenericResponse, error) {
	// Filter to LLM-configured machines only.
	var targets []OnlineMachineInfo
	var skipped []string
	for _, m := range machines {
		if m.LLMConfigured {
			targets = append(targets, m)
		} else {
			skipped = append(skipped, m.Name)
		}
	}
	if len(targets) == 0 {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "warning",
			Title:      "无可用设备",
			Body:       "所有在线设备的 LLM 均未配置，Agent 无法运行。",
		}, nil
	}

	// Launch concurrent requests.
	type result struct {
		name string
		resp *GenericResponse
		err  error
	}
	ch := make(chan result, len(targets))

	// Create a shared dedup filter so identical progress messages from
	// different devices (e.g. "需要一点时间处理，请稍候...") are only sent once.
	dedupCtx := withBroadcastDedup(ctx, newBroadcastProgressDedup())

	// Pop attachments once and re-stash for each target device so every
	// device receives the same attachments (popAttachments is destructive).
	broadcastAttachments := r.popAttachments(ctx, userID)

	for _, m := range targets {
		go func(m OnlineMachineInfo, atts []MessageAttachment) {
			// Acquire IM-specific LLM semaphore slot; degrade on timeout.
			if !r.llmSemIM.Acquire(dedupCtx) {
				ch <- result{name: m.Name, resp: nil, err: fmt.Errorf("%s", i18n.T(i18n.MsgLLMConcurrencyFull, "zh"))}
				return
			}
			defer r.llmSemIM.Release()
			resp, err := r.routeToSingleMachine(dedupCtx, userID, platformName, platformUID, text, m.MachineID, m.Name, atts)
			ch <- result{name: m.Name, resp: resp, err: err}
		}(m, broadcastAttachments)
	}

	// Collect all responses, delivering rich (image/file) ones individually.
	var deviceReplies []DeviceReply
	var richDelivered int
	for range targets {
		res := <-ch
		dr := DeviceReply{Name: res.name}
		if res.err != nil {
			dr.Err = res.err
		} else if res.resp != nil {
			hasImage := res.resp.ImageKey != ""
			hasFile := res.resp.FileData != "" && res.resp.FileName != ""
			if (hasImage || hasFile) && r.responseDelivery != nil {
				r.responseDelivery(ctx, userID, platformName, platformUID, res.resp)
				richDelivered++
				continue // delivered individually, skip from merged text
			}
			dr.Response = res.resp
		} else {
			// nil response, nil error — treat as timeout
			dr.Err = fmt.Errorf("空响应")
		}
		deviceReplies = append(deviceReplies, dr)
	}

	if len(skipped) > 0 {
		deviceReplies = append(deviceReplies, DeviceReply{
			Name: "LLM 未配置",
			Err:  fmt.Errorf("已跳过: %s", strings.Join(skipped, "、")),
		})
	}

	if len(deviceReplies) == 0 {
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "info",
			Title:      i18n.T(i18n.MsgGroupChatReply, "zh"),
			Body:       fmt.Sprintf("%d 条回复已分别发送（含图片/文件）", richDelivered),
		}, nil
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "info",
		Title:      i18n.T(i18n.MsgGroupChatReply, "zh"),
		Body:       FormatBroadcastReply(deviceReplies),
	}, nil
}

// routeToMultiple sends a message to specific machines concurrently and
// collects responses. Similar to routeBroadcast but targets specific machineIDs.
func (r *MessageRouter) routeToMultiple(ctx context.Context, userID, platformName, platformUID, text string, machineIDs []string) (*GenericResponse, error) {
	allMachines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
	machineMap := make(map[string]OnlineMachineInfo)
	for _, m := range allMachines {
		machineMap[m.MachineID] = m
	}

	var targets []OnlineMachineInfo
	for _, id := range machineIDs {
		if m, ok := machineMap[id]; ok {
			targets = append(targets, m)
		}
	}
	if len(targets) == 0 {
		return &GenericResponse{
			StatusCode: 404,
			StatusIcon: "info",
			Title:      "设备不在线",
			Body:       "目标设备均已离线。",
		}, nil
	}

	type result struct {
		name string
		resp *GenericResponse
		err  error
	}
	ch := make(chan result, len(targets))
	dedupCtx := withBroadcastDedup(ctx, newBroadcastProgressDedup())
	for _, m := range targets {
		go func(m OnlineMachineInfo) {
			// Acquire IM-specific LLM semaphore slot; degrade on timeout.
			if !r.llmSemIM.Acquire(dedupCtx) {
				ch <- result{name: m.Name, resp: nil, err: fmt.Errorf("%s", i18n.T(i18n.MsgLLMConcurrencyFull, "zh"))}
				return
			}
			defer r.llmSemIM.Release()
			resp, err := r.routeToSingleMachine(dedupCtx, userID, platformName, platformUID, text, m.MachineID, m.Name)
			ch <- result{name: m.Name, resp: resp, err: err}
		}(m)
	}

	var deviceReplies []DeviceReply
	for range targets {
		res := <-ch
		dr := DeviceReply{Name: res.name}
		if res.err != nil {
			dr.Err = res.err
		} else if res.resp != nil {
			dr.Response = res.resp
		} else {
			dr.Err = fmt.Errorf("空响应")
		}
		deviceReplies = append(deviceReplies, dr)
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "info",
		Title:      i18n.T(i18n.MsgMultiDeviceReply, "zh"),
		Body:       FormatBroadcastReply(deviceReplies),
	}, nil
}

// HandleAgentResponse is called when the Hub receives an "im.agent_response"
// message from a MaClaw client. It matches the response to the pending
// request by requestID and delivers it.
func (r *MessageRouter) HandleAgentResponse(requestID string, resp *AgentResponse) {
	r.mu.Lock()
	pending, ok := r.pendingReqs[requestID]
	if ok && resp != nil && len(resp.VoiceParts) == 0 && pending.voicePartsTotal > 0 {
		if pending.voicePartsBad || len(pending.voiceParts) != pending.voicePartsTotal {
			log.Printf("[MessageRouter] incomplete voice stream request_id=%s received=%d expected=%d; delivering text only", requestID, len(pending.voiceParts), pending.voicePartsTotal)
		} else {
			resp.VoiceParts = make([]VoicePart, pending.voicePartsTotal)
			complete := true
			for index := 0; index < pending.voicePartsTotal; index++ {
				part, exists := pending.voiceParts[index]
				if !exists {
					complete = false
					break
				}
				resp.VoiceParts[index] = part
			}
			if !complete {
				resp.VoiceParts = nil
				log.Printf("[MessageRouter] voice stream has an index gap request_id=%s; delivering text only", requestID)
			}
		}
	}
	if ok && resp != nil && pending.voicePartsBad {
		log.Printf("[MessageRouter] discarded poisoned voice stream request_id=%s; delivering text only", requestID)
		resp.VoiceParts = nil
		resp.VoiceData, resp.VoiceFileName, resp.VoiceMimeType = "", "", ""
	}
	r.mu.Unlock()

	if !ok {
		log.Printf("[MessageRouter] received agent response for unknown request_id=%s (expired or already handled)", requestID)
		return
	}

	// Non-blocking send — the channel is buffered with size 1.
	select {
	case pending.ResponseCh <- resp:
	default:
		log.Printf("[MessageRouter] response channel full for request_id=%s, dropping", requestID)
	}
}

// HandleAgentVoicePart stores one ordered hardware voice segment. It never
// completes a request by itself; the following im.agent_response is the
// transaction commit and ensures the device receives either all voice parts
// or the complete text-only result.
func (r *MessageRouter) HandleAgentVoicePart(requestID string, frame AgentVoicePart) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending, ok := r.pendingReqs[requestID]
	if !ok {
		log.Printf("[MessageRouter] received voice part for unknown request_id=%s", requestID)
		return
	}
	pending.lastActivity = time.Now()
	data := strings.TrimSpace(frame.Part.Data)
	decoded, decodeErr := base64.StdEncoding.DecodeString(data)
	if frame.Total <= 0 || frame.Total > 512 || frame.Index < 0 || frame.Index >= frame.Total ||
		data == "" || strings.TrimSpace(frame.Part.FileName) == "" || strings.TrimSpace(frame.Part.MimeType) == "" ||
		decodeErr != nil || len(decoded) == 0 || len(decoded) > 256*1024 {
		pending.voicePartsBad = true
		log.Printf("[MessageRouter] rejected invalid voice part request_id=%s index=%d total=%d", requestID, frame.Index, frame.Total)
		return
	}
	if pending.voicePartsTotal != 0 && pending.voicePartsTotal != frame.Total {
		pending.voicePartsBad = true
		log.Printf("[MessageRouter] rejected inconsistent voice total request_id=%s got=%d want=%d", requestID, frame.Total, pending.voicePartsTotal)
		return
	}
	_, duplicate := pending.voiceParts[frame.Index]
	if !duplicate && pending.voicePartsBytes+len(decoded) > 64*1024*1024 {
		pending.voicePartsBad = true
		log.Printf("[MessageRouter] rejected oversized aggregate voice stream request_id=%s bytes>%d", requestID, 64*1024*1024)
		return
	}
	if pending.voiceParts == nil {
		pending.voiceParts = make(map[int]VoicePart, frame.Total)
		pending.voicePartsTotal = frame.Total
	}
	if previous, duplicate := pending.voiceParts[frame.Index]; duplicate {
		if previous.Data != frame.Part.Data || previous.FileName != frame.Part.FileName || previous.MimeType != frame.Part.MimeType {
			pending.voicePartsBad = true
			log.Printf("[MessageRouter] rejected conflicting duplicate voice part request_id=%s index=%d", requestID, frame.Index)
		}
		return
	}
	pending.voiceParts[frame.Index] = frame.Part
	pending.voicePartsBytes += len(decoded)
	// A long response may start streaming near the normal agent deadline. Wake
	// the existing progress path so its live timer is reset, not just the
	// background cleanup timestamp.
	if pending.ProgressCh != nil {
		select {
		case pending.ProgressCh <- progressHeartbeat:
		default:
		}
	}
	log.Printf("[MessageRouter] buffered voice part request_id=%s part=%d/%d base64_bytes=%d", requestID, frame.Index+1, frame.Total, len(frame.Part.Data))
}

// HandleAgentProgress is called when the Hub receives an "im.agent_progress"
// message from a MaClaw client. It delivers the progress text to the pending
// request's ProgressCh, which resets the response timeout in RouteToAgent.
func (r *MessageRouter) HandleAgentProgress(requestID string, text string) {
	r.mu.Lock()
	pending, ok := r.pendingReqs[requestID]
	if ok {
		pending.lastActivity = time.Now()
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	// Non-blocking send — drop if the channel is full (shouldn't happen
	// with buffer size 8, but be safe).
	select {
	case pending.ProgressCh <- text:
	default:
		log.Printf("[MessageRouter] progress channel full for request_id=%s, dropping", requestID)
	}
}

// deliverProgress sends a progress text message to the user via IM.
// This is a best-effort delivery — errors are logged but not propagated.
func (r *MessageRouter) deliverProgress(ctx context.Context, userID, platformName, platformUID, text string) {
	if r.progressDelivery != nil {
		r.progressDelivery(ctx, userID, platformName, platformUID, text)
	}
}

// intermediateStatusMarkers are substrings that identify intermediate status
// progress messages (e.g. "正在耐心处理中", "正在执行工具"). These messages
// are suppressed in IM delivery — they only reset the timeout timer.
// The initial acknowledgment ("收到，正在处理中") is NOT matched here
// because it provides valuable first-response feedback to the user.
var intermediateStatusMarkers = []string{
	"正在耐心处理中",
	"正在执行工具",
	"正在执行命令",
	"正在生成 PDF",
	"正在启动 Skill",
	"正在整理并发送",
	"正在生成并执行脚本",
}

// isIntermediateStatusProgress returns true if the progress text is an
// intermediate status message that should be suppressed in IM delivery.
func isIntermediateStatusProgress(text string) bool {
	if text == "" {
		return false
	}
	for _, marker := range intermediateStatusMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// CancelPendingForUser cancels all pending IM requests for the given user
// by sending a nil response to their ResponseCh, causing routeToSingleMachine
// to return immediately. Also sends an im.cancel_session message to the
// user's selected machine so the client-side agent loop is aborted.
// Returns the number of cancelled requests and the text of the first one.
func (r *MessageRouter) CancelPendingForUser(ctx context.Context, userID string) (int, string) {
	key := routerRuntimeKey(ctx, userID)
	r.mu.Lock()
	var toCancel []*PendingIMRequest
	for _, req := range r.pendingReqs {
		if tenantUserRuntimeKey(req.TenantID, req.UserID) == key {
			toCancel = append(toCancel, req)
		}
	}
	selectedMachine := r.selectedMachine[key]
	r.mu.Unlock()

	var taskText string
	for _, req := range toCancel {
		if taskText == "" {
			taskText = req.Text
		}
		// Send a deferred (suppressed) response so routeToSingleMachine
		// unblocks without delivering a duplicate message to the user.
		// The /cancel handler already sends the user-facing confirmation.
		select {
		case req.ResponseCh <- &AgentResponse{Deferred: true}:
		default:
		}
	}

	// Send cancel signal to the client machine.
	if selectedMachine != "" && selectedMachine != broadcastMachineID {
		cancelMsg := map[string]interface{}{
			"type":    "im.cancel_session",
			"ts":      time.Now().Unix(),
			"payload": map[string]interface{}{"user_id": userID},
		}
		_ = r.devices.SendToMachine(selectedMachine, cancelMsg)
	} else if selectedMachine == broadcastMachineID {
		// Broadcast mode: send cancel to all online machines.
		machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
		for _, m := range machines {
			cancelMsg := map[string]interface{}{
				"type":    "im.cancel_session",
				"ts":      time.Now().Unix(),
				"payload": map[string]interface{}{"user_id": userID},
			}
			_ = r.devices.SendToMachine(m.MachineID, cancelMsg)
		}
	}

	return len(toCancel), taskText
}

// cleanupLoop periodically removes expired pending requests.
func (r *MessageRouter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanupExpired()
		case <-r.stopCh:
			return
		}
	}
}

// cleanupExpired removes pending requests that have exceeded their timeout
// without any recent activity (creation or progress update).
func (r *MessageRouter) cleanupExpired() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, req := range r.pendingReqs {
		if now.Sub(req.lastActivity) > req.Timeout+10*time.Second {
			delete(r.pendingReqs, id)
		}
	}
}
