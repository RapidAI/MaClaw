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

// DiscussionConductor orchestrates multi-device discussions using LLM to
// decide who speaks next, what to ask, and when to conclude. It replaces
// the mechanical round-robin logic when Hub LLM is configured.
type DiscussionConductor struct {
	configProvider func() *HubLLMConfig
	breaker        *CircuitBreaker
	router         *MessageRouter
	client         *http.Client
}

// NewDiscussionConductor creates a new conductor.
func NewDiscussionConductor(configProvider func() *HubLLMConfig, breaker *CircuitBreaker, router *MessageRouter) *DiscussionConductor {
	return &DiscussionConductor{
		configProvider: configProvider,
		breaker:        breaker,
		router:         router,
		client:         &http.Client{Timeout: 15 * time.Second},
	}
}

// ConductedDiscussionState tracks an LLM-orchestrated discussion.
type ConductedDiscussionState struct {
	Topic          string
	Devices        []OnlineMachineInfo
	Rounds         []ConductedRound
	MaxRounds      int
	Running        bool
	StopCh         chan struct{}
	stopOnce       sync.Once
	UserInputCh    chan string
	HistorySummary string // injected from discussion_history
}

func (s *ConductedDiscussionState) requestStop() {
	s.stopOnce.Do(func() { close(s.StopCh) })
}

func (s *ConductedDiscussionState) stopped() bool {
	select {
	case <-s.StopCh:
		return true
	default:
		return false
	}
}

// ConductedRound records one round of an LLM-orchestrated discussion.
type ConductedRound struct {
	Number    int
	Action    string            // ask_all, ask_specific, cross_review, summarize, conclude
	TargetIDs []string
	Prompt    string
	Replies   map[string]string // machineID → reply
	Summary   string
}

// conductorAction is the LLM's decision for the next discussion step.
type conductorAction struct {
	Action    string   `json:"action"`     // ask_all, ask_specific, cross_review, summarize, conclude
	TargetIDs []string `json:"target_ids"` // devices to address (ask_specific, cross_review)
	Prompt    string   `json:"prompt"`     // what to ask
	Summary   string   `json:"summary"`    // for summarize/conclude
}

const conductorSystemPrompt = `你是一个多设备讨论编排助手。你需要根据讨论进展决定下一步动作。

可选动作：
- "ask_all": 向所有设备提问（需提供 prompt）
- "ask_specific": 向指定设备追问（需提供 target_ids 和 prompt）
- "cross_review": 要求某设备评价另一设备的观点（需提供 target_ids 和 prompt）
- "summarize": 生成阶段性总结（需提供 summary）
- "conclude": 结束讨论并生成最终总结（需提供 summary）

以 JSON 格式返回，仅返回 JSON。`

// StartConductedDiscussion launches an LLM-orchestrated discussion.
// Returns immediately with a status message; the discussion runs in background.
// externalStopCh is the DiscussionState.StopCh from the caller so /stop works.
func (dc *DiscussionConductor) StartConductedDiscussion(
	ctx context.Context,
	userID, platformName, platformUID, topic string,
	devices []OnlineMachineInfo,
	historySummary string,
	externalStopCh chan struct{},
) *GenericResponse {
	stopCh := externalStopCh
	if stopCh == nil {
		stopCh = make(chan struct{})
	}
	state := &ConductedDiscussionState{
		Topic:          topic,
		Devices:        devices,
		MaxRounds:      10,
		Running:        true,
		StopCh:         stopCh,
		UserInputCh:    make(chan string, 8),
		HistorySummary: historySummary,
	}

	var names []string
	for _, d := range devices {
		names = append(names, d.Name)
	}

	go dc.runConductedDiscussion(userID, platformName, platformUID, state)

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🗣️",
		Title:      "智能讨论开始",
		Body: fmt.Sprintf("话题: %s\n参与者: %s\n模式: LLM 智能编排（最多 %d 轮）\n\n讨论进行中，发送 /stop 可提前终止。",
			topic, strings.Join(names, "、"), state.MaxRounds),
	}
}

func (dc *DiscussionConductor) runConductedDiscussion(userID, platformName, platformUID string, state *ConductedDiscussionState) {
	defer func() {
		if rv := recover(); rv != nil {
			dc.router.deliverProgress(context.Background(), userID, platformName, platformUID,
				fmt.Sprintf("❌ 智能讨论异常终止: %v", rv))
		}
		state.Running = false
		dc.router.mu.Lock()
		delete(dc.router.discussions, userID)
		dc.router.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// Round 1: ask all devices the initial topic.
	firstPrompt := state.Topic
	if state.HistorySummary != "" {
		firstPrompt = fmt.Sprintf("历史讨论参考:\n%s\n\n新话题: %s", state.HistorySummary, state.Topic)
	}

	dc.router.deliverProgress(ctx, userID, platformName, platformUID, "── 第 1 轮：全员发言 ──")
	replies := dc.askDevices(ctx, userID, platformName, platformUID, firstPrompt, state.Devices)
	state.Rounds = append(state.Rounds, ConductedRound{
		Number:  1,
		Action:  "ask_all",
		Prompt:  firstPrompt,
		Replies: replies,
	})
	dc.deliverRoundReplies(ctx, userID, platformName, platformUID, replies, state.Devices, state.Topic, 1)

	// Subsequent rounds: LLM decides.
	for round := 2; round <= state.MaxRounds; round++ {
		if state.stopped() || ctx.Err() != nil {
			break
		}

		// Drain user interjections.
		var userInputs []string
	drainLoop:
		for {
			select {
			case msg := <-state.UserInputCh:
				userInputs = append(userInputs, msg)
			default:
				break drainLoop
			}
		}

		action := dc.decideNextAction(ctx, state, userInputs)
		if action == nil {
			// LLM decision failed — fall back to conclude.
			dc.router.deliverProgress(ctx, userID, platformName, platformUID, "── LLM 编排失败，自动生成总结 ──")
			break
		}

		dc.router.deliverProgress(ctx, userID, platformName, platformUID,
			fmt.Sprintf("── 第 %d/%d 轮：%s ──", round, state.MaxRounds, action.Action))

		if action.Action == "conclude" || action.Action == "summarize" {
			summary := action.Summary
			if summary == "" {
				summary = "讨论结束。"
			}
			state.Rounds = append(state.Rounds, ConductedRound{
				Number:  round,
				Action:  action.Action,
				Summary: summary,
			})
			dc.router.deliverProgress(ctx, userID, platformName, platformUID,
				fmt.Sprintf("📋 %s", summary))
			if action.Action == "conclude" {
				break
			}
			continue
		}

		// Determine target devices.
		targets := state.Devices
		if len(action.TargetIDs) > 0 && (action.Action == "ask_specific" || action.Action == "cross_review") {
			targets = filterDevices(state.Devices, action.TargetIDs)
			if len(targets) == 0 {
				targets = state.Devices
			}
		}

		prompt := action.Prompt
		if prompt == "" {
			prompt = "请继续讨论。"
		}

		replies = dc.askDevices(ctx, userID, platformName, platformUID, prompt, targets)
		state.Rounds = append(state.Rounds, ConductedRound{
			Number:    round,
			Action:    action.Action,
			TargetIDs: action.TargetIDs,
			Prompt:    prompt,
			Replies:   replies,
		})
		dc.deliverRoundReplies(ctx, userID, platformName, platformUID, replies, targets, state.Topic, round)
	}

	// Final summary if not already concluded.
	lastRound := state.Rounds[len(state.Rounds)-1]
	if lastRound.Action != "conclude" {
		dc.router.deliverProgress(ctx, userID, platformName, platformUID, "── 生成最终总结 ──")
		summary := dc.generateFinalSummary(ctx, state)
		dc.router.deliverProgress(ctx, userID, platformName, platformUID,
			fmt.Sprintf("📋 最终总结:\n%s\n\n讨论结束。", summary))
	}
}

func (dc *DiscussionConductor) askDevices(
	ctx context.Context,
	userID, platformName, platformUID, prompt string,
	devices []OnlineMachineInfo,
) map[string]string {
	// Build participant name list for discussion_context.
	var allNames []string
	for _, d := range devices {
		allNames = append(allNames, d.Name)
	}

	type result struct {
		machineID string
		text      string
	}
	ch := make(chan result, len(devices))
	sem := dc.router.LLMSemaphore()
	for _, d := range devices {
		go func(d OnlineMachineInfo) {
			// Acquire LLM semaphore slot; skip device on timeout.
			if !sem.Acquire(ctx) {
				log.Printf("[DiscussionConductor] semaphore timeout for device=%s, skipping", d.Name)
				ch <- result{d.MachineID, ""}
				return
			}
			defer sem.Release()

			// Build discussion_context instruction for this device.
			var others []string
			for _, n := range allNames {
				if n != d.Name {
					others = append(others, n)
				}
			}
			instruction := fmt.Sprintf("你是讨论参与者「%s」，其他参与者有 %s。请以你的角色身份参与讨论。不要重复自己之前的观点，重点回应其他参与者的新观点。",
				d.Name, strings.Join(others, "、"))
			contextualPrompt := fmt.Sprintf("[讨论角色: %s] %s\n\n%s", d.Name, instruction, prompt)

			resp, err := dc.router.routeToSingleMachine(ctx, userID, platformName, platformUID, contextualPrompt, d.MachineID, "")
			var text string
			if err == nil && resp != nil {
				text = resp.Body
			}
			ch <- result{d.MachineID, text}
		}(d)
	}

	replies := make(map[string]string)
	for range devices {
		r := <-ch
		replies[r.machineID] = r.text
	}
	return replies
}

func (dc *DiscussionConductor) deliverRoundReplies(
	ctx context.Context,
	userID, platformName, platformUID string,
	replies map[string]string,
	devices []OnlineMachineInfo,
	topic string,
	round int,
) {
	prefix := fmt.Sprintf("🗣️ 会议 | %s | 第%d轮", truncate(topic, 20), round)
	for _, d := range devices {
		text := replies[d.MachineID]
		if text == "" {
			text = "⏰ 超时未回复"
		}
		dc.router.deliverProgress(ctx, userID, platformName, platformUID,
			fmt.Sprintf("%s\n[%s] %s", prefix, d.Name, text))
	}
}

func (dc *DiscussionConductor) decideNextAction(ctx context.Context, state *ConductedDiscussionState, userInputs []string) *conductorAction {
	cfg := dc.configProvider()
	if cfg == nil || !dc.breaker.Allow() {
		return nil
	}

	// Build context for LLM.
	var b strings.Builder
	fmt.Fprintf(&b, "讨论话题: %s\n\n", state.Topic)
	for _, r := range state.Rounds {
		fmt.Fprintf(&b, "第 %d 轮 (%s):\n", r.Number, r.Action)
		for id, text := range r.Replies {
			name := id
			for _, d := range state.Devices {
				if d.MachineID == id {
					name = d.Name
					break
				}
			}
			fmt.Fprintf(&b, "  [%s]: %s\n", name, truncate(text, 200))
		}
		if r.Summary != "" {
			fmt.Fprintf(&b, "  总结: %s\n", r.Summary)
		}
	}
	if len(userInputs) > 0 {
		fmt.Fprintf(&b, "\n💬 主持人补充: %s\n", strings.Join(userInputs, "\n"))
	}
	fmt.Fprintf(&b, "\n当前轮次: %d/%d\n请决定下一步动作。", len(state.Rounds)+1, state.MaxRounds)

	messages := []interface{}{
		map[string]string{"role": "system", "content": conductorSystemPrompt},
		map[string]string{"role": "user", "content": b.String()},
	}

	llmCfg := cfg.ToMaclawLLMConfig()

	// Acquire LLM semaphore for the decision call.
	sem := dc.router.LLMSemaphore()
	if !sem.Acquire(ctx) {
		log.Printf("[DiscussionConductor] semaphore timeout for decideNextAction")
		return nil
	}
	defer sem.Release()

	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, dc.client, 10*time.Second)
	if err != nil {
		log.Printf("[DiscussionConductor] LLM error: %v", err)
		dc.breaker.RecordFailure()
		return nil
	}
	dc.breaker.RecordSuccess()

	content := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var action conductorAction
	if err := json.Unmarshal([]byte(content), &action); err != nil {
		log.Printf("[DiscussionConductor] JSON parse failed: %v", err)
		return nil
	}
	return &action
}

func (dc *DiscussionConductor) generateFinalSummary(ctx context.Context, state *ConductedDiscussionState) string {
	cfg := dc.configProvider()
	if cfg == nil || !dc.breaker.Allow() {
		return dc.fallbackSummary(state)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "话题: %s\n\n讨论记录:\n", state.Topic)
	for _, r := range state.Rounds {
		for id, text := range r.Replies {
			name := id
			for _, d := range state.Devices {
				if d.MachineID == id {
					name = d.Name
					break
				}
			}
			fmt.Fprintf(&b, "[%s] %s\n", name, truncate(text, 300))
		}
	}
	b.WriteString("\n请生成结构化总结，分为：共识点、分歧点、待定事项。")

	messages := []interface{}{
		map[string]string{"role": "user", "content": b.String()},
	}

	llmCfg := cfg.ToMaclawLLMConfig()

	// Acquire LLM semaphore for the summary call.
	sem := dc.router.LLMSemaphore()
	if !sem.Acquire(ctx) {
		log.Printf("[DiscussionConductor] semaphore timeout for generateFinalSummary")
		return dc.fallbackSummary(state)
	}
	defer sem.Release()

	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, dc.client, 15*time.Second)
	if err != nil {
		log.Printf("[DiscussionConductor] summary LLM error: %v", err)
		return dc.fallbackSummary(state)
	}
	return resp.Content
}

func (dc *DiscussionConductor) fallbackSummary(state *ConductedDiscussionState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "话题: %s\n共 %d 轮讨论\n", state.Topic, len(state.Rounds))
	for _, r := range state.Rounds {
		for id, text := range r.Replies {
			name := id
			for _, d := range state.Devices {
				if d.MachineID == id {
					name = d.Name
					break
				}
			}
			fmt.Fprintf(&b, "- [%s] %s\n", name, truncate(text, 100))
		}
	}
	return b.String()
}

func filterDevices(all []OnlineMachineInfo, ids []string) []OnlineMachineInfo {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var out []OnlineMachineInfo
	for _, d := range all {
		if idSet[d.MachineID] || idSet[d.Name] {
			out = append(out, d)
		}
	}
	return out
}
