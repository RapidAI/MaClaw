package im

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Incoming message dedup — prevents duplicate processing when the same
// message arrives from multiple devices (e.g. WeChat mobile + PC online).
// ---------------------------------------------------------------------------

var incomingDedup = struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	lastEvict time.Time
}{seen: make(map[string]time.Time)}

const incomingDedupTTL = 10 * time.Second

// incomingDedupKey builds a dedup key for an incoming message.
// If MessageID is available, use it directly for precise dedup.
// Otherwise, fall back to a content fingerprint (platform:uid:fnv(text+attachInfo)).
func incomingDedupKey(msg IncomingMessage) string {
	if msg.MessageID != "" {
		return msg.PlatformName + ":" + msg.PlatformUID + ":id:" + msg.MessageID
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Attachments) == 0 {
		return "" // skip dedup for empty messages
	}
	h := fnv.New64a()
	h.Write([]byte(text))
	// Include attachment count and first attachment's filename+size to
	// distinguish different media messages with the same (or empty) text.
	for i, att := range msg.Attachments {
		if i >= 2 {
			break // first two attachments are enough for fingerprinting
		}
		h.Write([]byte(att.FileName))
		h.Write([]byte(strconv.FormatInt(att.Size, 10)))
	}
	return fmt.Sprintf("%s:%s:%x:a%d", msg.PlatformName, msg.PlatformUID, h.Sum64(), len(msg.Attachments))
}

// isDuplicateIncoming returns true if the same message was already seen
// within the dedup TTL window.
func isDuplicateIncoming(msg IncomingMessage) bool {
	key := incomingDedupKey(msg)
	if key == "" {
		return false
	}
	now := time.Now()
	incomingDedup.mu.Lock()
	defer incomingDedup.mu.Unlock()

	// Lazy eviction — at most once per 5 seconds.
	if now.Sub(incomingDedup.lastEvict) > 5*time.Second {
		for k, t := range incomingDedup.seen {
			if now.Sub(t) > incomingDedupTTL {
				delete(incomingDedup.seen, k)
			}
		}
		incomingDedup.lastEvict = now
	}

	if _, exists := incomingDedup.seen[key]; exists {
		return true
	}
	incomingDedup.seen[key] = now
	return false
}

// resetIncomingDedup clears the dedup cache. Exported for tests only.
func resetIncomingDedup() {
	incomingDedup.mu.Lock()
	incomingDedup.seen = make(map[string]time.Time)
	incomingDedup.lastEvict = time.Time{}
	incomingDedup.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Abstraction interfaces
// ---------------------------------------------------------------------------

// IdentityResolver abstracts the Identity_Service for user mapping.
type IdentityResolver interface {
	ResolveUser(ctx context.Context, platformName, platformUID string) (string, error)
}

// ---------------------------------------------------------------------------
// Rate limiter (token-bucket, 30 tokens/min per user)
// ---------------------------------------------------------------------------

const (
	rateLimitMaxTokens = 30
	rateLimitRefill    = time.Minute
)

// rateBucket is a simple per-user token bucket.
type rateBucket struct {
	tokens   int
	refillAt time.Time
}

// rateLimiter manages per-user rate limiting.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	stopCh  chan struct{}
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*rateBucket),
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop 定期清理过期的 rate limiter bucket，防止内存无限增长
func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictStale()
		case <-rl.stopCh:
			return
		}
	}
}

// evictStale 移除超过 10 分钟未活跃的 bucket
func (rl *rateLimiter) evictStale() {
	cutoff := time.Now().Add(-10 * time.Minute)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for uid, b := range rl.buckets {
		if b.refillAt.Before(cutoff) {
			delete(rl.buckets, uid)
		}
	}
}

// allow returns true if the user has remaining tokens. It refills the bucket
// if the refill interval has elapsed.
func (rl *rateLimiter) allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[userID]
	now := time.Now()
	if !ok {
		rl.buckets[userID] = &rateBucket{
			tokens:   rateLimitMaxTokens - 1,
			refillAt: now.Add(rateLimitRefill),
		}
		return true
	}

	// Refill if interval elapsed.
	if now.After(b.refillAt) {
		b.tokens = rateLimitMaxTokens
		b.refillAt = now.Add(rateLimitRefill)
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// ---------------------------------------------------------------------------
// Adapter — the IM Adapter Core (Agent Passthrough mode)
// ---------------------------------------------------------------------------

// Adapter is the central IM adapter that manages registered IM plugins,
// routes incoming messages through identity mapping, rate limiting, and
// then transparently relays to the MaClaw Agent via MessageRouter.
type Adapter struct {
	mu      sync.RWMutex
	plugins map[string]IMPlugin

	messageRouter        *MessageRouter
	coordinator          *Coordinator          // optional; nil = passthrough to messageRouter
	deviceNotifier       *DeviceNotifier       // optional; nil = no device notifications
	outboundInterceptor  *OutboundInterceptor  // optional; nil = no outbound interception
	contentAuditor       *ContentAuditor       // optional; nil = no content audit
	identity             IdentityResolver
	limiter              *rateLimiter
	taskDispatcher       *IMTaskDispatcher     // optional; nil = synchronous processing (legacy)
}

// NewAdapter creates a new IM Adapter with the given MessageRouter.
func NewAdapter(router *MessageRouter, identity IdentityResolver) *Adapter {
	return &Adapter{
		plugins:       make(map[string]IMPlugin),
		messageRouter: router,
		identity:      identity,
		limiter:       newRateLimiter(),
	}
}

// SetIdentityResolver replaces the identity resolver after construction.
// This is useful when the resolver depends on the adapter itself (e.g.
// PluginIdentityResolver).
func (a *Adapter) SetIdentityResolver(resolver IdentityResolver) {
	a.identity = resolver
}

// SetCoordinator wires the LLM Coordinator into the adapter. When set,
// non-command messages are routed through the Coordinator instead of
// directly to the MessageRouter.
func (a *Adapter) SetCoordinator(coord *Coordinator) {
	a.coordinator = coord
}

// SetDeviceNotifier wires the device notifier for online/offline notifications.
func (a *Adapter) SetDeviceNotifier(dn *DeviceNotifier) {
	a.deviceNotifier = dn
}

// SetOutboundInterceptor wires the outbound interceptor for file/image
// permission checks on IM channels.
func (a *Adapter) SetOutboundInterceptor(interceptor *OutboundInterceptor) {
	a.outboundInterceptor = interceptor
}

// SetContentAuditor wires the content auditor for outbound content compliance checks.
func (a *Adapter) SetContentAuditor(ca *ContentAuditor) {
	a.contentAuditor = ca
}

// InitTaskDispatcher creates and wires the background task dispatcher.
// Must be called after SetCoordinator (if used). capacity is the per-user
// queue depth (recommended 3-5).
func (a *Adapter) InitTaskDispatcher(capacity int) {
	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		// Re-stash attachments so routeToSingleMachine can pick them up.
		if len(task.Attachments) > 0 {
			a.messageRouter.StashAttachments(task.UserID, task.Attachments)
		}
		if a.coordinator != nil {
			return a.coordinator.Coordinate(ctx, task.UserID, task.PlatformName, task.PlatformUID, task.Text)
		}
		return a.messageRouter.RouteToAgent(ctx, task.UserID, task.PlatformName, task.PlatformUID, task.Text)
	}

	delivery := func(ctx context.Context, userID, platformName, platformUID string, resp *GenericResponse) {
		plugin := a.GetPlugin(platformName)
		if plugin == nil {
			log.Printf("[TaskDispatcher] no plugin for platform %q, cannot deliver result", platformName)
			return
		}
		target := UserTarget{PlatformUID: platformUID, UnifiedUserID: userID}
		a.sendResponse(ctx, plugin, target, resp)
	}

	a.taskDispatcher = NewIMTaskDispatcher(capacity, executor, delivery)
}

// RegisterPlugin registers an IM plugin with the adapter.
// It validates that the plugin implements all required interface methods
// by checking that Name() returns a non-empty string.
func (a *Adapter) RegisterPlugin(plugin IMPlugin) error {
	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("im: plugin Name() returned empty string, refusing to register")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.plugins[name]; exists {
		return fmt.Errorf("im: plugin %q already registered", name)
	}

	// Wire the message handler so the plugin routes messages to us.
	plugin.ReceiveMessage(func(msg IncomingMessage) {
		a.HandleMessage(context.Background(), msg)
	})

	a.plugins[name] = plugin
	log.Printf("[IM Adapter] registered plugin: %s", name)
	return nil
}

// GetPlugin returns the registered plugin by name, or nil.
func (a *Adapter) GetPlugin(name string) IMPlugin {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.plugins[name]
}

// HandleMessage is the main entry point called by IM plugins when they
// receive a message. It orchestrates the Agent Passthrough pipeline:
//
//  1. Identity mapping (platformUID → unifiedUserID)
//  2. Rate limiting (30 req/min per user)
//  3. Route to MaClaw Agent via MessageRouter
//  4. Response formatting & delivery based on CapabilityDeclaration
func (a *Adapter) HandleMessage(ctx context.Context, msg IncomingMessage) {
	plugin := a.GetPlugin(msg.PlatformName)
	if plugin == nil {
		log.Printf("[IM Adapter] no plugin registered for platform %q", msg.PlatformName)
		return
	}

	target := UserTarget{PlatformUID: msg.PlatformUID}

	log.Printf("[IM Adapter] HandleMessage: platform=%s uid=%s text_len=%d", msg.PlatformName, msg.PlatformUID, len(msg.Text))

	// 1. Identity mapping
	unifiedID, err := a.identity.ResolveUser(ctx, msg.PlatformName, msg.PlatformUID)
	if err != nil {
		log.Printf("[IM Adapter] ResolveUser FAILED: platform=%s uid=%s err=%v", msg.PlatformName, msg.PlatformUID, err)
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 403,
			StatusIcon: "👋",
			Title:      "尚未绑定账号",
			Body: "您还没有绑定 Hub 账号，无法使用机器人功能。\n\n" +
				"绑定方法很简单：直接在此对话中发送您的 Hub 注册邮箱地址（例如 you@example.com），" +
				"系统会向该邮箱发送验证码，回复验证码即可完成绑定。",
		})
		return
	}
	msg.UnifiedUserID = unifiedID
	target.UnifiedUserID = unifiedID

	// Mark user as active for device notifications.
	if a.deviceNotifier != nil {
		a.deviceNotifier.MarkUserActive(unifiedID, msg.PlatformName, msg.PlatformUID)
	}

	// 2. Rate limiting
	if !a.limiter.allow(unifiedID) {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 429,
			StatusIcon: "⏳",
			Title:      "请求过于频繁",
			Body:       "您的操作频率已超过限制（每分钟 30 次），请稍后再试。",
		})
		return
	}

	// 2b. Incoming message dedup — drop duplicates from multi-device delivery
	// (e.g. WeChat mobile + PC sending the same event).
	if isDuplicateIncoming(msg) {
		log.Printf("[IM Adapter] DEDUP: dropping duplicate message from platform=%s uid=%s msgID=%s text_len=%d",
			msg.PlatformName, msg.PlatformUID, msg.MessageID, len(msg.Text))
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Attachments) == 0 {
		return
	}

	log.Printf("[IM Adapter] command check: text=%q len=%d bytes=[% x]", text, len(text), []byte(text))

	// 3a. Handle /machines command — list all devices from Hub's perspective.
	if text == "/machines" || text == "/m" {
		log.Printf("[IM Adapter] /machines MATCHED for user=%s platform=%s", msg.UnifiedUserID, msg.PlatformName)
		machines := a.messageRouter.devices.FindAllOnlineMachinesForUser(ctx, unifiedID)
		if len(machines) == 0 {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "📴",
				Title:      "设备列表",
				Body:       "暂无在线设备。请确认 MaClaw 客户端已启动并连接到 Hub。",
			})
			return
		}
		selected, _ := a.messageRouter.GetSelectedMachine(unifiedID)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🖥 在线设备 (%d 台):\n\n", len(machines)))
		for _, m := range machines {
			marker := "  "
			if m.MachineID == selected {
				marker = "▶ "
			}
			llmTag := ""
			if !m.LLMConfigured {
				llmTag = " ⚠️LLM未配置"
			}
			sb.WriteString(fmt.Sprintf("%s%s%s\n", marker, m.Name, llmTag))
		}
		sb.WriteString("\n使用 /call <昵称> 切换设备，/call all 群聊模式。")
		log.Printf("[IM Adapter] /machines response: %d devices, sending to platform=%s uid=%s", len(machines), msg.PlatformName, msg.PlatformUID)
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "🖥️",
			Title:      "设备列表",
			Body:       sb.String(),
		})
		return
	}

	// 3. Handle /call command — always handled by Hub, never sent to Agent.
	if strings.HasPrefix(text, "/call ") || strings.HasPrefix(text, "/call\t") || text == "/call" {
		name := ""
		if len(text) > 5 {
			name = strings.TrimSpace(text[6:])
		}
		if name == "" {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 400,
				StatusIcon: "❓",
				Title:      "缺少参数",
				Body:       "用法: /call <设备昵称>\n\n输入 /machines 查看在线设备列表。",
			})
			return
		}

		// SpaceState validation.
		if a.coordinator != nil {
			ss := a.coordinator.SpaceStateStore()
			state := ss.GetOrCreate(unifiedID)
			if state.State == SpaceMeeting {
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 400,
					StatusIcon: "⚠️",
					Title:      "会议进行中",
					Body:       "会议进行中，无法切换设备。使用 /ask <设备名> <消息> 临时交互，或 /stop 结束会议。",
				})
				return
			}
			if state.State == SpacePrivate && strings.EqualFold(name, "all") {
				// /call all in private mode → exit private, return to lobby.
				ss.ExitPrivate(unifiedID)
				a.messageRouter.ClearSelectedMachine(unifiedID)
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 200,
					StatusIcon: "🏠",
					Title:      "已返回大厅",
					Body:       fmt.Sprintf("已退出与 %s 的私聊，返回大厅模式。", state.PrivateName),
				})
				return
			}
		}

		// If a discussion is running, stop it before switching device.
		if a.messageRouter.IsInDiscussion(unifiedID) {
			a.messageRouter.StopDiscussion(unifiedID)
		}

		result := a.messageRouter.SelectMachine(ctx, unifiedID, name)
		icon := "✅"
		code := 200
		if !result.OK {
			icon = "⚠️"
			code = 400
		}

		// Enter private mode on successful /call <name> (not "all").
		if result.OK && !strings.EqualFold(name, "all") && a.coordinator != nil {
			ss := a.coordinator.SpaceStateStore()
			ss.EnterPrivate(unifiedID, result.MachineID, result.MachineName)
		}
		// /call all → exit private if in private mode.
		if result.OK && strings.EqualFold(name, "all") && a.coordinator != nil {
			ss := a.coordinator.SpaceStateStore()
			state := ss.GetOrCreate(unifiedID)
			if state.State == SpacePrivate {
				ss.ExitPrivate(unifiedID)
			}
		}

		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: code,
			StatusIcon: icon,
			Title:      "设备切换",
			Body:       result.Message,
		})
		return
	}

	// 3b. Handle /discuss command — start AI-to-AI discussion.
	if strings.HasPrefix(text, "/discuss ") || strings.HasPrefix(text, "/discuss\t") {
		topic := strings.TrimSpace(text[9:])
		if topic == "" {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 400,
				StatusIcon: "❓",
				Title:      "缺少参数",
				Body:       "用法: /discuss <话题>",
			})
			return
		}
		// SpaceState validation.
		if a.coordinator != nil {
			ss := a.coordinator.SpaceStateStore()
			state := ss.GetOrCreate(unifiedID)
			if state.State == SpacePrivate {
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 400,
					StatusIcon: "⚠️",
					Title:      "私聊模式中",
					Body:       "私聊模式中无法发起讨论。发送 /call all 返回大厅后再发起。",
				})
				return
			}
			if state.State == SpaceMeeting {
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 400,
					StatusIcon: "⚠️",
					Title:      "已有会议",
					Body:       "已有会议进行中，请先 /stop 结束当前会议。",
				})
				return
			}
			// Enter meeting state with all online devices as participants.
			machines := a.messageRouter.devices.FindAllOnlineMachinesForUser(ctx, unifiedID)
			var participantIDs []string
			for _, m := range machines {
				participantIDs = append(participantIDs, m.MachineID)
			}
			ss.EnterMeeting(unifiedID, topic, participantIDs)
		}
		resp := a.messageRouter.StartDiscussion(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, topic)
		a.sendResponse(ctx, plugin, target, resp)
		return
	}
	if text == "/discuss" {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 400,
			StatusIcon: "❓",
			Title:      "缺少参数",
			Body:       "用法: /discuss <话题>\n\n让多台设备的 AI 围绕话题进行多轮讨论。",
		})
		return
	}

	// 3c-cancel. Handle /cancel command — cancel the currently running agent task.
	if text == "/cancel" || text == "/取消" {
		cancelled, taskText := a.messageRouter.CancelPendingForUser(ctx, unifiedID)
		if cancelled > 0 {
			body := "已发送取消信号，当前任务将尽快停止。"
			if taskText != "" {
				preview := taskText
				runes := []rune(preview)
				if len(runes) > 30 {
					preview = string(runes[:30]) + "…"
				}
				body = fmt.Sprintf("⏹️ 已取消任务「%s」。", preview)
			}
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "⏹️",
				Title:      "已取消",
				Body:       body,
			})
		} else {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "ℹ️",
				Title:      "无活跃任务",
				Body:       "当前没有正在执行的任务。",
			})
		}
		return
	}

	// 3c. Handle /stop command — stop active discussion + exit meeting.
	if text == "/stop" {
		resp := a.messageRouter.StopDiscussion(unifiedID)
		// Also exit meeting state if in meeting.
		if a.coordinator != nil {
			ss := a.coordinator.SpaceStateStore()
			state := ss.GetOrCreate(unifiedID)
			if state.State == SpaceMeeting {
				ss.ExitMeeting(unifiedID)
				if resp.Body != "" {
					resp.Body += "\n🏠 已退出会议，返回大厅。"
				} else {
					resp.Body = "🏠 已退出会议，返回大厅。"
				}
			}
		}
		a.sendResponse(ctx, plugin, target, resp)
		return
	}

	// 3d-ask. Handle /ask <设备名> <消息> — one-shot cross-space interaction.
	if strings.HasPrefix(text, "/ask ") {
		rest := strings.TrimSpace(text[5:])
		spaceIdx := strings.IndexByte(rest, ' ')
		if spaceIdx <= 0 {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 400,
				StatusIcon: "❓",
				Title:      "缺少参数",
				Body:       "用法: /ask <设备名> <消息>\n\n不影响当前空间状态，一次性发送。",
			})
			return
		}
		deviceName := rest[:spaceIdx]
		askText := strings.TrimSpace(rest[spaceIdx+1:])
		if askText == "" {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 400,
				StatusIcon: "❓",
				Title:      "缺少消息",
				Body:       "用法: /ask <设备名> <消息>",
			})
			return
		}
		// Find the device.
		machines := a.messageRouter.devices.FindAllOnlineMachinesForUser(ctx, unifiedID)
		var targetMachine *OnlineMachineInfo
		for i := range machines {
			if strings.EqualFold(machines[i].Name, deviceName) {
				targetMachine = &machines[i]
				break
			}
		}
		if targetMachine == nil {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 404,
				StatusIcon: "📴",
				Title:      "设备未找到",
				Body:       fmt.Sprintf("未找到名为 %q 的在线设备。", deviceName),
			})
			return
		}
		resp, err := a.messageRouter.routeToSingleMachine(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, askText, targetMachine.MachineID, targetMachine.Name)
		if err != nil {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 500,
				StatusIcon: "❌",
				Title:      "发送失败",
				Body:       err.Error(),
			})
			return
		}
		a.sendResponse(ctx, plugin, target, resp)
		return
	}

	// 3d-ctx. Handle /context and /context clear commands.
	if text == "/context" || text == "/context clear" {
		if a.coordinator == nil {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "📭",
				Title:      "对话上下文",
				Body:       "智能路由未启用，无对话上下文。",
			})
			return
		}
		cc := a.coordinator.ConvContext().GetOrCreate(unifiedID)
		if text == "/context clear" {
			cc.Clear()
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "🗑️",
				Title:      "已清除",
				Body:       "对话上下文已清除。",
			})
			return
		}
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "📋",
			Title:      "对话上下文",
			Body:       cc.FormatDisplay(),
		})
		return
	}

	// 3e. Handle /help command.
	if text == "/help" {
		machines := a.messageRouter.devices.FindAllOnlineMachinesForUser(ctx, unifiedID)
		selected, _ := a.messageRouter.GetSelectedMachine(unifiedID)
		llmEnabled := a.coordinator != nil && a.coordinator.IsLLMEnabled()
		helpText := BuildHelpMessage(len(machines), selected, llmEnabled)
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "📋",
			Title:      "帮助",
			Body:       helpText,
		})
		return
	}

	// 3f. Handle /rounds N command — adjust discussion rounds.
	if strings.HasPrefix(text, "/rounds ") {
		nStr := strings.TrimSpace(text[8:])
		n := 0
		for _, c := range nStr {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				n = -1
				break
			}
		}
		if n <= 0 || n > 20 {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 400,
				StatusIcon: "❓",
				Title:      "参数错误",
				Body:       "用法: /rounds <1-20>",
			})
			return
		}
		a.messageRouter.SetDiscussionRounds(unifiedID, n)
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "✅",
			Title:      "已调整",
			Body:       fmt.Sprintf("讨论轮数已调整为 %d 轮。", n),
		})
		return
	}

	// 3f2. Handle /queue command — show task queue status.
	if text == "/queue" {
		if a.taskDispatcher == nil {
			a.sendResponse(ctx, plugin, target, &GenericResponse{
				StatusCode: 200,
				StatusIcon: "📋",
				Title:      "任务队列",
				Body:       "任务队列未启用。",
			})
			return
		}
		stats := a.taskDispatcher.Stats(unifiedID)
		var body string
		if stats.Running {
			body = fmt.Sprintf("🔄 正在执行 1 个任务，队列中还有 %d 个等待。（容量 %d）", stats.Pending, stats.Capacity)
		} else if stats.Pending > 0 {
			body = fmt.Sprintf("📋 队列中有 %d 个任务等待处理。（容量 %d）", stats.Pending, stats.Capacity)
		} else {
			body = "✅ 队列空闲，没有待处理任务。"
		}
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "📋",
			Title:      "任务队列状态",
			Body:       body,
		})
		return
	}

	// 3g. Unknown / command — friendly error.
	if strings.HasPrefix(text, "/") {
		cmd := text
		if idx := strings.IndexByte(text, ' '); idx > 0 {
			cmd = text[:idx]
		}
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 400,
			StatusIcon: "❓",
			Title:      "未知命令",
			Body:       fmt.Sprintf("未识别的命令 %q，发送 /help 查看可用命令。", cmd),
		})
		return
	}

	// 3h. If user is in discussion mode, handle accordingly.
	// When LLM is active, skip this — Coordinator handles meeting messages.
	llmActive := a.coordinator != nil && a.coordinator.IsLLMEnabled()
	if !llmActive && !strings.HasPrefix(text, "/") && a.messageRouter.IsInDiscussion(unifiedID) {
		if a.messageRouter.IsDiscussionRunning(unifiedID) {
			// Discussion is running — inject as human interjection.
			if a.messageRouter.InjectUserInput(unifiedID, text) {
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 200,
					StatusIcon: "💬",
					Title:      "已收到",
					Body:       "你的发言将加入下一轮讨论。",
				})
			} else {
				a.sendResponse(ctx, plugin, target, &GenericResponse{
					StatusCode: 429,
					StatusIcon: "⏳",
					Title:      "缓冲已满",
					Body:       "发言过多，请稍后再试。",
				})
			}
			return
		}
		// Follow-up topic — start a new discussion round with previous summary.
		resp := a.messageRouter.StartDiscussion(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, text)
		a.sendResponse(ctx, plugin, target, resp)
		return
	}

	// 4. Try matching text as a machine nickname (direct name switch).
	// When LLM smart mode is active, skip this — IntentClassifier decides
	// routing, so sending "安妮" means talking TO that device, not switching.
	// Users can still switch explicitly via /call.
	if !llmActive && !strings.HasPrefix(text, "/") {
		if handled, nameResp := a.messageRouter.TrySelectByName(ctx, unifiedID, text); handled {
			a.sendResponse(ctx, plugin, target, nameResp)
			return
		}
	}

	// 5. Route to MaClaw Agent — via Coordinator (if wired) or MessageRouter.
	log.Printf("[IM Adapter] routing: user=%s coordinator=%v dispatcher=%v text_len=%d attachments=%d",
		unifiedID, a.coordinator != nil, a.taskDispatcher != nil, len(text), len(msg.Attachments))

	// --- Fast-path: Hub direct answer (no device needed) ---
	// When the task dispatcher is active and the Coordinator supports it,
	// try to answer simple questions directly without queuing.
	if a.taskDispatcher != nil && a.coordinator != nil {
		if fastResp := a.coordinator.TryFastAnswer(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, text); fastResp != nil {
			a.sendResponse(ctx, plugin, target, fastResp)
			return
		}

		// --- Slow-path: queue for background processing ---
		task := &IMTask{
			UserID:       unifiedID,
			PlatformName: msg.PlatformName,
			PlatformUID:  msg.PlatformUID,
			Text:         text,
			Attachments:  msg.Attachments,
		}
		queueResp := a.taskDispatcher.Enqueue(task)
		a.sendResponse(ctx, plugin, target, queueResp)
		return
	}

	// --- Legacy synchronous path (no task dispatcher) ---
	// Stash attachments so routeToSingleMachine can include them in the
	// WebSocket payload without changing the routing API signatures.
	a.messageRouter.StashAttachments(unifiedID, msg.Attachments)

	var routeResp *GenericResponse
	var routeErr error
	if a.coordinator != nil {
		routeResp, routeErr = a.coordinator.Coordinate(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, text)
	} else {
		routeResp, routeErr = a.messageRouter.RouteToAgent(ctx, unifiedID, msg.PlatformName, msg.PlatformUID, text)
	}
	if routeErr != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 500,
			StatusIcon: "❌",
			Title:      "路由失败",
			Body:       fmt.Sprintf("无法将消息路由到 Agent: %s", routeErr.Error()),
		})
		return
	}

	// Deferred response (media buffered on client, waiting for user intent).
	// Nothing to deliver to the user right now.
	if routeResp == nil {
		return
	}

	// 6. Format and deliver response
	a.sendResponse(ctx, plugin, target, routeResp)
}

// DeliverProgress sends a progress text message to a user via the appropriate
// IM plugin. This is used by the MessageRouter to relay intermediate status
// updates from the Agent during long-running tasks.
func (a *Adapter) DeliverProgress(ctx context.Context, platformName, userID, platformUID, text string) {
	a.mu.RLock()
	plugin, ok := a.plugins[platformName]
	a.mu.RUnlock()
	if !ok {
		log.Printf("[IM Adapter] DeliverProgress: no plugin for platform %q", platformName)
		return
	}

	// Progress messages are always sent as plain text; strip Markdown
	// so users don't see raw formatting marks.
	text = stripMarkdown(text)

	target := UserTarget{PlatformUID: platformUID, UnifiedUserID: userID}
	if err := plugin.SendText(ctx, target, text); err != nil {
		log.Printf("[IM Adapter] DeliverProgress SendText failed for %s: %v", platformName, err)
	}
}

// DeliverResponse sends a full GenericResponse to a user via the appropriate
// IM plugin. This is used by the MessageRouter to deliver image/file
// responses individually in broadcast mode.
func (a *Adapter) DeliverResponse(ctx context.Context, platformName, userID, platformUID string, resp *GenericResponse) {
	a.mu.RLock()
	plugin, ok := a.plugins[platformName]
	a.mu.RUnlock()
	if !ok {
		log.Printf("[IM Adapter] DeliverResponse: no plugin for platform %q", platformName)
		return
	}

	target := UserTarget{PlatformUID: platformUID, UnifiedUserID: userID}
	a.sendResponse(ctx, plugin, target, resp)
}

// ---------------------------------------------------------------------------
// Response formatting & delivery (capability-based format selection)
// ---------------------------------------------------------------------------

// sendResponse delivers a GenericResponse to the user via the appropriate
// plugin method, choosing the best format based on CapabilityDeclaration.
//
// Strategy:
//   - If plugin supports rich cards → SendCard with OutgoingMessage
//   - Otherwise → SendText with FallbackText
func (a *Adapter) sendResponse(ctx context.Context, plugin IMPlugin, target UserTarget, resp *GenericResponse) {
	// Outbound interception: check file/image permissions for IM channels
	intercepted := false
	if a.outboundInterceptor != nil {
		newResp, blocked := a.outboundInterceptor.CheckOutbound(ctx, target.UnifiedUserID, resp, plugin.Name())
		if blocked {
			resp = newResp
			intercepted = true
		}
	}

	// Content audit: check outbound content compliance (skip if already intercepted)
	if !intercepted && a.contentAuditor != nil {
		result := a.contentAuditor.Audit(ctx, target.UnifiedUserID, plugin.Name(), resp)
		switch result.Action {
		case AuditBlock, AuditManualReview:
			resp = result.Response
		case AuditSanitize:
			resp = result.Response
		case AuditDelay:
			// Send placeholder message immediately.
			a.deliverSingleResponse(ctx, plugin, target, result.Response)
			// Start background polling; deliver final result asynchronously.
			a.contentAuditor.StartDelayPolling(ctx, target.UnifiedUserID, plugin.Name(), resp, func(bgCtx context.Context, finalResp *GenericResponse) {
				a.deliverSingleResponse(bgCtx, plugin, target, finalResp)
			})
			return
		case AuditPass:
			// resp unchanged
		}
	}

	caps := plugin.Capabilities()

	// If the response contains an image, send it first via SendImage.
	if resp.ImageKey != "" && caps.SupportsImage {
		caption := resp.ImageCaption
		if caption == "" && resp.Body != "" {
			caption = resp.Body
		}
		if err := plugin.SendImage(ctx, target, resp.ImageKey, caption); err != nil {
			log.Printf("[IM Adapter] SendImage failed for %s: %v, falling back to text", plugin.Name(), err)
			// If image send fails and there's text, fall through to send text.
			if resp.Body == "" {
				_ = plugin.SendText(ctx, target, "截图发送失败: "+err.Error())
				return
			}
		} else {
			// Image sent successfully. If there's no additional text, we're done.
			if resp.Body == "" {
				return
			}
		}
	}

	// If the response contains a file, send it via SendFile.
	if resp.FileData != "" && resp.FileName != "" && caps.SupportsFile {
		if err := plugin.SendFile(ctx, target, resp.FileData, resp.FileName, resp.FileMimeType); err != nil {
			log.Printf("[IM Adapter] SendFile failed for %s: %v, falling back to text", plugin.Name(), err)
			if resp.Body == "" {
				_ = plugin.SendText(ctx, target, "文件发送失败: "+err.Error())
				return
			}
		} else {
			// File sent successfully. If there's no additional text, we're done.
			if resp.Body == "" {
				return
			}
		}
	}

	out := resp.ToOutgoingMessage()

	if caps.SupportsRichCard {
		if err := plugin.SendCard(ctx, target, out); err != nil {
			log.Printf("[IM Adapter] SendCard failed for %s, falling back to text: %v", plugin.Name(), err)
			// Fallback to text on card send failure.
			_ = plugin.SendText(ctx, target, out.FallbackText)
		}
		// If urgent, also send a buzz/urgent notification via the card's fallback text.
		if out.Urgent {
			if urgentPlugin, ok := plugin.(UrgentSender); ok {
				// Send a lightweight urgent text to trigger the buzz notification.
				_ = urgentPlugin.SendUrgentText(ctx, target, "⚡ "+out.Title)
			}
		}
		return
	}

	// No rich card support — send plain text.
	text := out.FallbackText
	if text == "" {
		text = resp.ToFallbackText()
	}

	// Strip Markdown when the platform doesn't render it.
	if !caps.SupportsMarkdown {
		text = stripMarkdown(text)
	}

	// Truncate if platform has a max text length.
	if caps.MaxTextLength > 0 && len(text) > caps.MaxTextLength {
		text = truncateAtLine(text, caps.MaxTextLength)
	}

	// If the message is marked urgent and the plugin supports it, use urgent delivery.
	if out.Urgent {
		if urgentPlugin, ok := plugin.(UrgentSender); ok {
			if err := urgentPlugin.SendUrgentText(ctx, target, text); err != nil {
				log.Printf("[IM Adapter] SendUrgentText failed for %s, falling back to normal: %v", plugin.Name(), err)
				_ = plugin.SendText(ctx, target, text)
			}
			return
		}
	}

	if err := plugin.SendText(ctx, target, text); err != nil {
		log.Printf("[IM Adapter] SendText failed for %s (uid=%s): %v", plugin.Name(), target.PlatformUID, err)
	}
}

// deliverSingleResponse delivers a GenericResponse through the plugin,
// reusing the same delivery logic as sendResponse but without interception/audit.
// Used for async delivery (e.g. delay polling callbacks).
func (a *Adapter) deliverSingleResponse(ctx context.Context, plugin IMPlugin, target UserTarget, resp *GenericResponse) {
	caps := plugin.Capabilities()

	out := resp.ToOutgoingMessage()

	if caps.SupportsRichCard {
		if err := plugin.SendCard(ctx, target, out); err != nil {
			_ = plugin.SendText(ctx, target, out.FallbackText)
		}
		return
	}

	text := out.FallbackText
	if text == "" {
		text = resp.ToFallbackText()
	}
	if !caps.SupportsMarkdown {
		text = stripMarkdown(text)
	}
	if caps.MaxTextLength > 0 && len(text) > caps.MaxTextLength {
		text = truncateAtLine(text, caps.MaxTextLength)
	}
	_ = plugin.SendText(ctx, target, text)
}

// truncateAtLine truncates text to maxLen at a line boundary and appends "…".
func truncateAtLine(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	// Reserve space for the ellipsis suffix.
	cutoff := maxLen - len("…")
	if cutoff < 0 {
		cutoff = 0
	}
	// Find the last newline before cutoff.
	idx := strings.LastIndex(text[:cutoff], "\n")
	if idx < 0 {
		idx = cutoff
	}
	return text[:idx] + "\n…"
}
