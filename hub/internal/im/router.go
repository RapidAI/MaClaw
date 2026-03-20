package im

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
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
}

// defaultAgentTimeout is the maximum time to wait for an Agent response.
// 多轮 Agent 循环（最多 12 轮 LLM 调用）可能需要较长时间
const defaultAgentTimeout = 180 * time.Second

// cleanupInterval controls how often expired pending requests are reaped.
const cleanupInterval = 30 * time.Second

// progressHeartbeat is the sentinel value sent by the client to keep the
// response timer alive without delivering a visible message to the user.
const progressHeartbeat = "__heartbeat__"

// ---------------------------------------------------------------------------
// MessageRouter — routes IM messages to MaClaw Agent via WebSocket
// ---------------------------------------------------------------------------

// ProgressDeliveryFunc is called to deliver progress text to a user via IM.
type ProgressDeliveryFunc func(ctx context.Context, userID, platformName, platformUID, text string)

// MessageRouter replaces the old NL_Router + BridgeExecutor pipeline.
// It transparently relays IM messages to the user's MaClaw client Agent
// and waits for the Agent's response.
type MessageRouter struct {
	devices          DeviceFinder
	progressDelivery ProgressDeliveryFunc

	mu          sync.Mutex
	pendingReqs map[string]*PendingIMRequest // requestID → pending

	// selectedMachine tracks the user's chosen machine for IM routing.
	// Key: userID, Value: machineID. Protected by mu.
	selectedMachine map[string]string

	// discussions tracks active /discuss sessions per user. Protected by mu.
	discussions map[string]*DiscussionState

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewMessageRouter creates a MessageRouter with the given device finder.
func NewMessageRouter(devices DeviceFinder) *MessageRouter {
	r := &MessageRouter{
		devices:         devices,
		pendingReqs:     make(map[string]*PendingIMRequest),
		selectedMachine: make(map[string]string),
		discussions:     make(map[string]*DiscussionState),
		stopCh:          make(chan struct{}),
	}
	go r.cleanupLoop()
	return r
}

// MachineSelectResult is returned by SelectMachine / TrySelectByName.
type MachineSelectResult struct {
	OK       bool   // selection succeeded
	Message  string // human-readable status message
}

// broadcastMachineID is the sentinel value stored in selectedMachine to
// indicate that the user is in broadcast mode (/call all).
const broadcastMachineID = "__all__"

// SelectMachine explicitly sets the target machine for a user (via /call).
func (r *MessageRouter) SelectMachine(ctx context.Context, userID, name string) MachineSelectResult {
	machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)
	if len(machines) == 0 {
		return MachineSelectResult{OK: false, Message: "📴 当前没有在线设备。"}
	}
	if len(machines) == 1 {
		return MachineSelectResult{OK: true, Message: fmt.Sprintf("✅ 当前只有一台在线设备 %s，无需切换。", machines[0].Name)}
	}

	// "/call all" — enter broadcast mode.
	if strings.EqualFold(name, "all") {
		r.mu.Lock()
		r.selectedMachine[userID] = broadcastMachineID
		r.mu.Unlock()
		var names []string
		for _, m := range machines {
			names = append(names, m.Name)
		}
		return MachineSelectResult{
			OK:      true,
			Message: fmt.Sprintf("📢 已进入群聊模式，后续消息将发送给所有在线设备：%s\n\n使用 @昵称 消息 可指定某台设备。使用 /call <昵称> 退出群聊。", strings.Join(names, "、")),
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
			Message: fmt.Sprintf("⚠️ 有 %d 台设备同名 %q，请先在客户端修改昵称使其唯一，然后重试。\n\n%s", len(matched), name, list),
		}
	}

	r.mu.Lock()
	r.selectedMachine[userID] = matched[0].MachineID
	r.mu.Unlock()

	return MachineSelectResult{
		OK:      true,
		Message: fmt.Sprintf("✅ 已切换设备，你当前正在与 %s 交流。", matched[0].Name),
	}
}

// TrySelectByName attempts to match the text against an online machine name.
// Returns (true, response) if the text matched a machine name (switch or error).
// Returns (false, nil) if no match — caller should route as normal message.
func (r *MessageRouter) TrySelectByName(ctx context.Context, userID, text string) (handled bool, resp *GenericResponse) {
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
			StatusIcon: "⚠️",
			Title:      "设备重名",
			Body:       fmt.Sprintf("有 %d 台设备同名 %q，请先在客户端修改昵称使其唯一。\n\n%s\n\n修改后使用 /call <昵称> 切换。", len(matched), text, list),
		}
	}

	// If the matched machine is already the current selection, don't intercept —
	// let the text pass through to the Agent as a normal message.
	r.mu.Lock()
	current := r.selectedMachine[userID]
	r.mu.Unlock()
	if current == matched[0].MachineID {
		return false, nil
	}

	// Switch to the new machine.
	r.mu.Lock()
	r.selectedMachine[userID] = matched[0].MachineID
	r.mu.Unlock()

	return true, &GenericResponse{
		StatusCode: 200,
		StatusIcon: "✅",
		Title:      "已切换设备",
		Body:       fmt.Sprintf("已切换设备，你当前正在与 %s 交流。", matched[0].Name),
	}
}

// GetSelectedMachine returns the currently selected machine for a user.
func (r *MessageRouter) GetSelectedMachine(userID string) (machineID string, ok bool) {
	r.mu.Lock()
	mid, ok := r.selectedMachine[userID]
	r.mu.Unlock()
	return mid, ok
}

// ClearSelectedMachine removes the machine selection for a user.
func (r *MessageRouter) ClearSelectedMachine(userID string) {
	r.mu.Lock()
	delete(r.selectedMachine, userID)
	r.mu.Unlock()
}

// formatMachineList builds a human-readable list of online machines.
func (r *MessageRouter) formatMachineList(machines []OnlineMachineInfo) string {
	var b strings.Builder
	b.WriteString("📋 在线设备列表：\n")
	for i, m := range machines {
		llm := "❌"
		if m.LLMConfigured {
			llm = "✅"
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
			StatusIcon: "📴",
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
						StatusIcon: "⚠️",
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
			StatusIcon: "❓",
			Title:      "设备未找到",
			Body:       fmt.Sprintf("未找到名为 %q 的在线设备。", targetName),
		}, nil
	}

	if len(machines) == 1 {
		// Single machine — auto-select.
		m := machines[0]
		r.mu.Lock()
		r.selectedMachine[userID] = m.MachineID
		r.mu.Unlock()
		if !m.LLMConfigured {
			return &GenericResponse{
				StatusCode: 503,
				StatusIcon: "⚠️",
				Title:      "Agent 未就绪",
				Body:       "设备已在线，但 MaClaw LLM 未配置。Agent 无法运行。\n\n请在 MaClaw 客户端的设置中配置 LLM（URL、Key、Model），然后重试。",
			}, nil
		}
		return r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, "")
	}

	// Multiple machines.
	r.mu.Lock()
	selected := r.selectedMachine[userID]
	r.mu.Unlock()

	if selected == "" {
		list := r.formatMachineList(machines)
		return &GenericResponse{
			StatusCode: 300,
			StatusIcon: "🖥️",
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
					StatusIcon: "⚠️",
					Title:      "Agent 未就绪",
					Body:       "设备已在线，但 MaClaw LLM 未配置。Agent 无法运行。\n\n请在 MaClaw 客户端的设置中配置 LLM（URL、Key、Model），然后重试。",
				}, nil
			}
			return r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, "")
		}
	}

	// Selected machine went offline.
	r.mu.Lock()
	delete(r.selectedMachine, userID)
	r.mu.Unlock()
	list := r.formatMachineList(machines)
	return &GenericResponse{
		StatusCode: 503,
		StatusIcon: "📴",
		Title:      "设备已离线",
		Body:       fmt.Sprintf("之前选择的设备已离线，请重新选择：\n\n%s", list),
	}, nil
}

// routeToSingleMachine sends a message to one machine and waits for the reply.
// If namePrefix is non-empty, progress and response text are prefixed with [namePrefix].
func (r *MessageRouter) routeToSingleMachine(ctx context.Context, userID, platformName, platformUID, text, machineID, namePrefix string) (*GenericResponse, error) {
	requestID := fmt.Sprintf("im_%s_%d", userID, time.Now().UnixNano())
	now := time.Now()
	pending := &PendingIMRequest{
		RequestID:    requestID,
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
	wsMsg := map[string]interface{}{
		"type":       "im.user_message",
		"request_id": requestID,
		"ts":         time.Now().Unix(),
		"payload": map[string]interface{}{
			"user_id":  userID,
			"platform": platformName,
			"text":     text,
		},
	}
	if err := r.devices.SendToMachine(machineID, wsMsg); err != nil {
		log.Printf("[MessageRouter] SendToMachine failed for machine=%s: %v", machineID, err)
		body := "无法将消息发送到您的设备，请检查连接状态。"
		if namePrefix != "" {
			body = fmt.Sprintf("[%s] %s", namePrefix, body)
		}
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "📴",
			Title:      "发送失败",
			Body:       body,
		}, nil
	}

	// 5. Wait for Agent response with resettable timeout.
	timer := time.NewTimer(pending.Timeout)
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
					StatusIcon: "❌",
					Title:      "Agent 返回空响应",
					Body:       body,
				}, nil
			}
			gr := resp.ToGenericResponse()
			if namePrefix != "" {
				gr.Body = fmt.Sprintf("[%s] %s", namePrefix, gr.Body)
			}
			return gr, nil

		case progressText := <-pending.ProgressCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(pending.Timeout)

			// Silent heartbeat — only resets the timer, never delivered to user.
			if progressText == progressHeartbeat {
				continue
			}

			progressTexts = append(progressTexts, progressText)

			isDup := progressText == lastProgressText
			lastProgressText = progressText

			if time.Since(lastDelivered) >= progressMinInterval && !isDup {
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
				StatusIcon: "⏰",
				Title:      "Agent 响应超时",
				Body:       body,
			}, nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// routeBroadcast sends the message to all online machines concurrently and
// collects responses. Each machine's response/progress is prefixed with
// [machineName]. Responses are delivered first-come-first-served.
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
			StatusIcon: "⚠️",
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

	for _, m := range targets {
		go func(m OnlineMachineInfo) {
			resp, err := r.routeToSingleMachine(ctx, userID, platformName, platformUID, text, m.MachineID, m.Name)
			ch <- result{name: m.Name, resp: resp, err: err}
		}(m)
	}

	// Collect all responses.
	var parts []string
	for range targets {
		res := <-ch
		if res.err != nil {
			parts = append(parts, fmt.Sprintf("[%s] ❌ 错误: %v", res.name, res.err))
		} else if res.resp != nil {
			// Body is already prefixed by routeToSingleMachine.
			parts = append(parts, res.resp.Body)
		}
	}

	if len(skipped) > 0 {
		parts = append(parts, fmt.Sprintf("⚠️ 以下设备 LLM 未配置，已跳过: %s", strings.Join(skipped, "、")))
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "📢",
		Title:      "群聊回复",
		Body:       strings.Join(parts, "\n\n"),
	}, nil
}

// HandleAgentResponse is called when the Hub receives an "im.agent_response"
// message from a MaClaw client. It matches the response to the pending
// request by requestID and delivers it.
func (r *MessageRouter) HandleAgentResponse(requestID string, resp *AgentResponse) {
	r.mu.Lock()
	pending, ok := r.pendingReqs[requestID]
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
