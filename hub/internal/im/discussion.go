package im

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Discussion — multi-round AI-to-AI discussion orchestrated by Hub
// ---------------------------------------------------------------------------

const defaultDiscussionRounds = 3

// DiscussionState tracks an active /discuss session for a user.
type DiscussionState struct {
	Topic       string
	PrevSummary string // summary carried over for follow-up topics
	Round       int    // current round (1-based), 0 = not started
	MaxRounds   int
	Running     bool
	StopCh      chan struct{}
	stopOnce    sync.Once
	StartedAt   time.Time

	// UserInputCh receives human interjections during a running discussion.
	// Messages are drained between rounds and injected into the next prompt.
	UserInputCh chan string
}

// requestStop signals the discussion loop to stop after the current round.
func (d *DiscussionState) requestStop() {
	d.stopOnce.Do(func() { close(d.StopCh) })
}

// stopped returns true if stop has been requested.
func (d *DiscussionState) stopped() bool {
	select {
	case <-d.StopCh:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// StartDiscussion — entry point called from HandleMessage
// ---------------------------------------------------------------------------

// StartDiscussion begins a multi-round AI discussion. It runs in a goroutine
// and delivers each round's results to the user via IM progress delivery.
// Returns immediately with a status message.
func (r *MessageRouter) StartDiscussion(ctx context.Context, userID, platformName, platformUID, topic string) *GenericResponse {
	machines := r.devices.FindAllOnlineMachinesForUser(ctx, userID)

	// Filter LLM-configured machines.
	var targets []OnlineMachineInfo
	for _, m := range machines {
		if m.LLMConfigured {
			targets = append(targets, m)
		}
	}
	if len(targets) < 2 {
		return &GenericResponse{
			StatusCode: 400,
			StatusIcon: "⚠️",
			Title:      "无法开始讨论",
			Body:       "至少需要 2 台 LLM 已配置的在线设备才能进行讨论。",
		}
	}

	r.mu.Lock()
	existing := r.discussions[userID]
	if existing != nil && existing.Running {
		r.mu.Unlock()
		return &GenericResponse{
			StatusCode: 409,
			StatusIcon: "⚠️",
			Title:      "讨论进行中",
			Body:       "已有讨论正在进行，请先 /stop 终止当前讨论。",
		}
	}

	// Build initial prompt: if there's a previous summary, prepend it.
	var prevSummary string
	if existing != nil && existing.PrevSummary != "" {
		prevSummary = existing.PrevSummary
	}

	// If LLM conductor is available, delegate to it for smart orchestration.
	if r.conductor != nil && r.conductor.breaker.Allow() {
		ds := &DiscussionState{
			Topic:       topic,
			PrevSummary: prevSummary,
			Running:     true,
			StopCh:      make(chan struct{}),
			UserInputCh: make(chan string, 8),
			StartedAt:   time.Now(),
		}
		r.discussions[userID] = ds
		r.mu.Unlock()
		// Pass the DiscussionState's StopCh so /stop can terminate the conductor.
		return r.conductor.StartConductedDiscussion(ctx, userID, platformName, platformUID, topic, targets, prevSummary, ds.StopCh)
	}

	ds := &DiscussionState{
		Topic:       topic,
		PrevSummary: prevSummary,
		MaxRounds:   defaultDiscussionRounds,
		Running:     true,
		StopCh:      make(chan struct{}),
		UserInputCh: make(chan string, 8),
		StartedAt:   time.Now(),
	}
	r.discussions[userID] = ds
	r.mu.Unlock()

	var names []string
	for _, m := range targets {
		names = append(names, m.Name)
	}

	// Launch the discussion loop in background.
	go r.runDiscussion(userID, platformName, platformUID, ds, targets)

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🗣️",
		Title:      "讨论开始",
		Body: fmt.Sprintf("话题: %s\n参与者: %s\n轮数: %d 轮 + 总结\n\n讨论进行中，每轮结果会实时推送。发送 /stop 可提前终止。",
			topic, strings.Join(names, "、"), ds.MaxRounds),
	}
}

// discussionTimeout is the maximum wall-clock time for an entire discussion
// (all rounds + summary). Prevents runaway goroutines if machines are stuck.
const discussionTimeout = 20 * time.Minute

// runDiscussion executes the multi-round discussion loop.
func (r *MessageRouter) runDiscussion(userID, platformName, platformUID string, ds *DiscussionState, targets []OnlineMachineInfo) {
	defer func() {
		if rv := recover(); rv != nil {
			r.deliverProgress(context.Background(), userID, platformName, platformUID,
				fmt.Sprintf("❌ 讨论异常终止: %v", rv))
		}
		r.mu.Lock()
		ds.Running = false
		r.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), discussionTimeout)
	defer cancel()

	// Build the first-round prompt.
	prompt := ds.Topic
	if ds.PrevSummary != "" {
		prompt = fmt.Sprintf("上次讨论总结:\n%s\n\n新话题: %s", ds.PrevSummary, ds.Topic)
	}

	var allRoundTexts []string // accumulates [name] text for summary

	for round := 1; round <= ds.MaxRounds; round++ {
		if ds.stopped() {
			r.deliverProgress(ctx, userID, platformName, platformUID, "⏹ 讨论已被用户终止。")
			break
		}
		if ctx.Err() != nil {
			r.deliverProgress(context.Background(), userID, platformName, platformUID, "⏰ 讨论总时间超限，自动终止。")
			break
		}

		ds.Round = round
		r.deliverProgress(ctx, userID, platformName, platformUID,
			fmt.Sprintf("── 第 %d/%d 轮 ──", round, ds.MaxRounds))

		results := r.discussionRound(ctx, userID, platformName, platformUID, prompt, targets)

		// Build round summary text and deliver each reply.
		var roundParts []string
		for _, res := range results {
			if res.Err != nil {
				line := fmt.Sprintf("[%s] ❌ 错误: %v", res.Name, res.Err)
				roundParts = append(roundParts, line)
				r.deliverProgress(ctx, userID, platformName, platformUID, line)
			} else if res.Text == "" {
				line := fmt.Sprintf("[%s] ⏰ 超时未回复", res.Name)
				roundParts = append(roundParts, line)
				r.deliverProgress(ctx, userID, platformName, platformUID, line)
			} else {
				line := fmt.Sprintf("[%s] %s", res.Name, res.Text)
				roundParts = append(roundParts, line)
				r.deliverProgress(ctx, userID, platformName, platformUID, line)
			}
		}

		allRoundTexts = append(allRoundTexts, roundParts...)

		// Deliver round summary.
		roundSummary := FormatRoundSummary(round, results)
		r.deliverProgress(ctx, userID, platformName, platformUID, roundSummary)

		// Drain any human interjections received during this round.
		var userInputs []string
		drainLoop:
		for {
			select {
			case msg := <-ds.UserInputCh:
				userInputs = append(userInputs, msg)
			default:
				break drainLoop
			}
		}

		// Next round prompt = all replies from this round + human input if any.
		if len(userInputs) > 0 {
			humanText := strings.Join(userInputs, "\n")
			prompt = fmt.Sprintf("以下是第 %d 轮各参与者的观点：\n\n%s\n\n💬 主持人补充：\n%s\n\n请基于以上观点和主持人的补充继续讨论。",
				round, strings.Join(roundParts, "\n\n"), humanText)
			r.deliverProgress(ctx, userID, platformName, platformUID,
				fmt.Sprintf("💬 主持人发言已加入第 %d 轮讨论", round+1))
		} else {
			prompt = fmt.Sprintf("以下是第 %d 轮各参与者的观点：\n\n%s\n\n请基于以上观点继续讨论，补充、反驳或深化。",
				round, strings.Join(roundParts, "\n\n"))
		}
	}

	if ds.stopped() {
		// Still generate summary from what we have.
		if len(allRoundTexts) == 0 {
			return
		}
	}

	// Summary round: pick a random machine to summarize.
	r.deliverProgress(ctx, userID, platformName, platformUID, "── 生成总结 ──")

	summaryPrompt := FormatDiscussionSummary(ds.Topic, allRoundTexts)

	summarizer := targets[rand.Intn(len(targets))]
	summaryResp, err := r.routeToSingleMachine(ctx, userID, platformName, platformUID,
		summaryPrompt, summarizer.MachineID, "")

	var summaryText string
	if err != nil {
		summaryText = fmt.Sprintf("总结生成失败: %v", err)
	} else if summaryResp != nil {
		summaryText = summaryResp.Body
	} else {
		summaryText = "总结生成失败: 空响应"
	}

	// Store summary for follow-up.
	r.mu.Lock()
	ds.PrevSummary = summaryText
	r.mu.Unlock()

	r.deliverProgress(ctx, userID, platformName, platformUID,
		fmt.Sprintf("📋 总结 (by %s):\n%s\n\n讨论结束。直接发消息可追加话题继续讨论，/stop 退出讨论模式。",
			summarizer.Name, summaryText))
}

// discussionRound sends the prompt to all target machines concurrently
// and collects their replies.
func (r *MessageRouter) discussionRound(ctx context.Context, userID, platformName, platformUID, prompt string, targets []OnlineMachineInfo) []discussionRoundResult {
	type result struct {
		idx  int
		name string
		text string
		err  error
	}
	ch := make(chan result, len(targets))

	for i, m := range targets {
		go func(i int, m OnlineMachineInfo) {
			resp, err := r.routeToSingleMachine(ctx, userID, platformName, platformUID,
				prompt, m.MachineID, "")
			var text string
			if err == nil && resp != nil {
				text = resp.Body
			}
			ch <- result{idx: i, name: m.Name, text: text, err: err}
		}(i, m)
	}

	results := make([]discussionRoundResult, len(targets))
	for range targets {
		res := <-ch
		results[res.idx] = discussionRoundResult{
			Name: res.name,
			Text: res.text,
			Err:  res.err,
		}
	}
	return results
}

// discussionRoundResult holds one machine's reply in a discussion round.
type discussionRoundResult struct {
	Name string
	Text string
	Err  error
}

// StopDiscussion stops the active discussion for a user.
func (r *MessageRouter) StopDiscussion(userID string) *GenericResponse {
	r.mu.Lock()
	ds := r.discussions[userID]
	r.mu.Unlock()

	if ds == nil {
		return &GenericResponse{
			StatusCode: 404,
			StatusIcon: "❓",
			Title:      "无讨论",
			Body:       "当前没有进行中的讨论。",
		}
	}

	if ds.Running {
		ds.requestStop()
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "⏹",
			Title:      "终止讨论",
			Body:       "已请求终止讨论，当前轮次完成后将停止并生成总结。",
		}
	}

	// Not running — clear the discussion state entirely.
	r.mu.Lock()
	delete(r.discussions, userID)
	r.mu.Unlock()

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "✅",
		Title:      "退出讨论",
		Body:       "已退出讨论模式。",
	}
}

// InjectUserInput sends a human message into the running discussion.
// It will be picked up between rounds and added to the next prompt.
// Returns true if the message was accepted.
func (r *MessageRouter) InjectUserInput(userID, text string) bool {
	r.mu.Lock()
	ds := r.discussions[userID]
	r.mu.Unlock()

	if ds == nil || !ds.Running {
		return false
	}

	// Non-blocking send — drop if buffer is full (unlikely with size 8).
	select {
	case ds.UserInputCh <- text:
		return true
	default:
		return false
	}
}

// IsInDiscussion returns true if the user has an active (or completed but
// not cleared) discussion session. Used by HandleMessage to route follow-up
// messages as new discussion topics.
func (r *MessageRouter) IsInDiscussion(userID string) bool {
	r.mu.Lock()
	ds := r.discussions[userID]
	r.mu.Unlock()
	return ds != nil
}

// IsDiscussionRunning returns true if a discussion loop is actively executing.
func (r *MessageRouter) IsDiscussionRunning(userID string) bool {
	r.mu.Lock()
	ds := r.discussions[userID]
	r.mu.Unlock()
	return ds != nil && ds.Running
}

// SetDiscussionRounds adjusts the remaining rounds for an active discussion.
func (r *MessageRouter) SetDiscussionRounds(userID string, rounds int) {
	r.mu.Lock()
	ds := r.discussions[userID]
	r.mu.Unlock()
	if ds != nil {
		ds.MaxRounds = ds.Round + rounds
	}
}
