package main

// workflow_integration.go adds workflow engine support to the TUI.
//
// This is the foundation for TUI workflow integration. It initializes the
// workflow engine with the same registry and templates as the GUI, and adds
// the interception point in handleChatSend. The intent understanding LLM
// caller is wired to the same LLM config as the agent loop.
//
// Current limitations vs GUI:
// - No doc preview panel (TUI is text-only — documents shown inline)
// - No SteeringWorkflowDetector (steering rules still work via agent loop)
// - No SubAgent (TUI uses direct mode via agent.RunLoop)
//
// These can be added incrementally without changing the architecture.

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

var tuiQuotedPathPattern = regexp.MustCompile(`"([^"]+)"|'([^']+)'|“([^”]+)”|‘([^’]+)’`)

type tuiWorkflowLLMCaller struct {
	app    *TUIApp
	client *http.Client
}

func (c *tuiWorkflowLLMCaller) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	if c == nil || c.app == nil {
		return "", fmt.Errorf("TUI workflow LLM caller is not initialized")
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: timeout + 5*time.Second}
	}
	resp, err := agent.DoSimpleLLMRequest(c.app.llmConfig, messages, client, timeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}

// TUIWorkflowCallbacks implements workflow.EngineCallbacks for the TUI.
// In TUI mode, events are logged but not emitted to a frontend — the
// workflow state is communicated through inline text in the chat.
type TUIWorkflowCallbacks struct {
	app *TUIApp
}

func (c *TUIWorkflowCallbacks) SendTextToUser(userID, text string) error {
	log.Printf("[TUI-workflow] text: %s", truncateTUI(text, 80))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	log.Printf("[TUI-workflow] phase update: phase=%s index=%d", state.CurrentPhase, state.PhaseIndex)
	return nil
}

func (c *TUIWorkflowCallbacks) EmitDocUpdate(userID, phaseID, content string) error {
	log.Printf("[TUI-workflow] doc update: phase=%s len=%d", phaseID, len(content))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	log.Printf("[TUI-workflow] gate result: phase=%s", phaseID)
	return nil
}

// tuiWorkflowStore implements workflow.PersistenceStore (in-memory no-op).
// TUI sessions are typically short-lived; workflow state doesn't need
// to survive restarts (the conversation history does, via ConversationMemory).
type tuiWorkflowStore struct{}

func (s *tuiWorkflowStore) SaveWorkflowState(state *workflow.WorkflowState) error { return nil }
func (s *tuiWorkflowStore) LoadWorkflowState(userID string) (*workflow.WorkflowState, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteWorkflowState(id string) error                     { return nil }
func (s *tuiWorkflowStore) ListActiveWorkflows() ([]*workflow.WorkflowState, error) { return nil, nil }
func (s *tuiWorkflowStore) SaveUnderstandingSession(session *workflow.UnderstandingSession) error {
	return nil
}
func (s *tuiWorkflowStore) LoadUnderstandingSession(userID string) (*workflow.UnderstandingSession, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteUnderstandingSession(userID string) error { return nil }
func (s *tuiWorkflowStore) CleanupExpired(olderThan time.Duration) error   { return nil }

// initWorkflowEngine creates and wires the workflow engine for the TUI.
// The engine uses the same registry (19 templates) as the GUI. New workflow
// starts go through IntentUnderstandingManager, not template keyword matching.
func (app *TUIApp) initWorkflowEngine() *workflow.WorkflowEngine {
	registry := workflow.NewWorkflowRegistry()
	store := &tuiWorkflowStore{}
	callbacks := &TUIWorkflowCallbacks{app: app}
	llmCaller := &tuiWorkflowLLMCaller{
		app:    app,
		client: &http.Client{},
	}
	understanding := workflow.NewIntentUnderstandingManager(store, llmCaller, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, store, callbacks)

	return engine
}

// getWorkflowEngine returns the workflow engine if available and enabled, or nil.
// This is the TUI's single enforcement point for the "workflow enabled" config
// toggle. All workflow consumers should go through this method.
func (app *TUIApp) getWorkflowEngine() *workflow.WorkflowEngine {
	if !app.appConfig.IsWorkflowEnabled() {
		return nil
	}
	return app.workflowEngine
}

// handleWorkflowInterception checks if the message should be handled by the
// workflow engine. Returns a non-empty string if the message was fully handled
// (the string is the response to show the user). Returns empty string if the
// message should proceed to the normal agent loop.
func (app *TUIApp) handleWorkflowInterception(text string) string {
	engine := app.getWorkflowEngine()
	if engine == nil {
		return ""
	}

	userID := "tui-user"
	filter := engine.GetFilter()
	if filter == nil {
		return ""
	}

	classification := filter.Classify(userID, text)
	log.Printf("[TUI-workflow] classify: %v text=%q", classification, truncateTUI(text, 60))

	switch classification {
	case workflow.FilterActiveWorkflow:
		return app.handleActiveWorkflowTUI(text)

	case workflow.FilterActiveUnderstanding:
		return app.handleActiveUnderstandingTUI(text)

	case workflow.FilterNeedsUnderstanding:
		if shouldBypassTUIWorkflowUnderstanding(text) {
			return ""
		}
		return app.handleNeedsUnderstandingTUI(text)

	case workflow.FilterSimpleDirective:
		return "" // pass through to normal agent loop
	}

	return ""
}

func (app *TUIApp) handleActiveWorkflowTUI(text string) string {
	userID := "tui-user"
	text = app.expandWorkflowAttachmentInput(userID, text)
	resp, err := app.workflowEngine.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] HandleInput error: %v", err)
		return fmt.Sprintf("工作流处理出错: %v", err)
	}
	if resp == nil {
		return ""
	}

	if resp.PendingReview || resp.PendingConfirm {
		return app.handleWorkflowReviewTUI(userID, text)
	}

	if !resp.RunAgentLoop {
		if resp.Complete {
			app.workflowMu.Lock()
			app.workflowAgentLoop = false
			app.pendingPhasePrompt = ""
			app.workflowMu.Unlock()
		}
		return resp.Text
	}

	// RunAgentLoop=true — stash the phase prompt for the agent loop.
	if resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	if resp.Advance && resp.Text != "" {
		// Phase advanced — return the transition text. The next agent loop
		// call will pick up the stashed phase prompt.
		return resp.Text
	}
	return "" // fall through to agent loop with stashed phase prompt
}

func (app *TUIApp) handleWorkflowReviewTUI(userID, text string) string {
	intent := workflow.ReviewIntentOther
	if strings.TrimSpace(app.llmConfig.URL) != "" && strings.TrimSpace(app.llmConfig.Model) != "" {
		raw, err := app.classifyWorkflowReviewIntentTUI(userID, text)
		if err != nil {
			log.Printf("[TUI-workflow] review intent classification failed: %v", err)
		} else {
			intent = workflow.ParseReviewIntent(raw)
		}
	}

	resp, err := app.workflowEngine.ApplyReviewIntent(userID, intent, text)
	if err != nil {
		log.Printf("[TUI-workflow] ApplyReviewIntent error: intent=%s err=%v", intent, err)
		return app.workflowReviewBarrierText(userID)
	}
	if intent == workflow.ReviewIntentSwitchTask {
		return app.handleWorkflowInterception(text)
	}
	return app.handleWorkflowResponseTUI(userID, resp)
}

func (app *TUIApp) classifyWorkflowReviewIntentTUI(userID, text string) (string, error) {
	messages := []interface{}{
		map[string]string{
			"role": "system",
			"content": `You are a user intent classifier for a workflow review step.

The user has just seen a phase deliverable and the workflow must wait for an explicit review decision before continuing.

Classify the user's response into exactly one category. Reply with ONLY the category word:
- "confirm" — approve and continue to the next phase.
- "supplement" — provide additions, corrections, questions, or requested changes for the current phase deliverable.
- "skip" — skip the current phase if the workflow template allows it.
- "cancel" — abandon the current workflow.
- "switch_task" — abandon this workflow and start a clearly different task.
- "other" — unrelated request that should not advance or execute tools while review is pending.`,
		},
		map[string]string{
			"role":    "user",
			"content": text,
		},
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := agent.DoSimpleLLMRequest(app.llmConfig, messages, client, 10*time.Second)
	if err != nil || resp == nil {
		return "", err
	}
	return resp.Content, nil
}

func (app *TUIApp) handleWorkflowResponseTUI(userID string, resp *workflow.WorkflowResponse) string {
	if resp == nil {
		return ""
	}
	if !resp.RunAgentLoop {
		if resp.Complete {
			app.workflowMu.Lock()
			app.workflowAgentLoop = false
			app.pendingPhasePrompt = ""
			app.workflowMu.Unlock()
		}
		return resp.Text
	}
	if resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	return ""
}

func (app *TUIApp) workflowReviewBarrierText(userID string) string {
	ws := app.workflowEngine.GetActiveWorkflow(userID)
	if ws == nil {
		return ""
	}
	phaseName := ws.CurrentPhase
	if tmpl := app.workflowEngine.GetRegistry().Match(ws.Type); tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
		phaseName = tmpl.Phases[ws.PhaseIndex].Name
	}
	return fmt.Sprintf("Current workflow is waiting for review at phase %q. Please confirm, supplement, skip if allowed, cancel, or start a clearly different task.", phaseName)
}

func (app *TUIApp) handleActiveUnderstandingTUI(text string) string {
	userID := "tui-user"
	understanding := app.workflowEngine.GetUnderstanding()
	if understanding == nil {
		return ""
	}

	reply, ready, cancelled, intent, err := understanding.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding HandleInput error: %v", err)
		return "我收到了你的补充，但刚才内部理解步骤临时失败了。请再发一次补充，或者直接说“开工”，我会继续当前任务。"
	}
	if cancelled {
		return "已取消。"
	}
	if ready && intent != nil {
		if intent.Category == workflow.WorkflowNone || intent.Category == "" {
			return ""
		}
		state, err := app.workflowEngine.StartWorkflow(userID, *intent)
		if err != nil {
			log.Printf("[TUI-workflow] StartWorkflow error: %v", err)
			return fmt.Sprintf("启动工作流失败: %v", err)
		}
		return app.buildWorkflowStartOverview(userID, state, reply)
	}
	return reply
}

func (app *TUIApp) handleNeedsUnderstandingTUI(text string) string {
	userID := "tui-user"
	if strings.TrimSpace(app.llmConfig.URL) == "" || strings.TrimSpace(app.llmConfig.Model) == "" {
		return ""
	}
	understanding := app.workflowEngine.GetUnderstanding()
	if understanding == nil {
		return ""
	}

	result, err := understanding.Start(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding Start error: %v", err)
		return ""
	}
	if result == nil || result.Rejected {
		return ""
	}
	return result.Reply
}

func shouldBypassTUIWorkflowUnderstanding(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "" || utf8.RuneCountInString(trimmed) <= 1
}

func (app *TUIApp) buildWorkflowStartOverview(userID string, state *workflow.WorkflowState, prefix string) string {
	overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)
	if req := app.workflowEngine.GetInputRequirement(userID); req != nil {
		overview += "\n\n📎 " + req.Description
		if len(req.FileTypes) > 0 {
			overview += fmt.Sprintf("（支持格式：%s）", strings.Join(req.FileTypes, "、"))
		}
		if req.AcceptText {
			overview += "\n\nTUI 中请直接粘贴/拖入本地文件路径，或把文档正文粘贴进来。路径里有空格时请用引号包起来。"
		}
	}
	if strings.TrimSpace(prefix) != "" {
		overview = strings.TrimSpace(prefix) + "\n\n" + overview
	}

	// The first phase needs the agent loop to generate content.
	// Stash the phase prompt.
	tmpl := app.workflowEngine.GetRegistry().Match(state.Type)
	if tmpl != nil && len(tmpl.Phases) > 0 {
		phasePrompt := workflow.BuildPhaseSystemPrompt(state, &tmpl.Phases[0], app.workflowEngine.GetRegistry())
		if phasePrompt != "" {
			app.workflowMu.Lock()
			app.pendingPhasePrompt = phasePrompt
			app.workflowAgentLoop = true
			app.workflowMu.Unlock()
		}
	}

	return overview
}

// expandWorkflowAttachmentInput turns a pasted/dropped local path into explicit
// workflow context. TUI has no file-picker, so the user's attachment gesture is
// a text path in the chat input.
func (app *TUIApp) expandWorkflowAttachmentInput(userID, text string) string {
	if app == nil || app.workflowEngine == nil || app.workflowEngine.GetInputRequirement(userID) == nil {
		return text
	}
	path := firstExistingWorkflowPath(text)
	if path == "" {
		return text
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return fmt.Sprintf("用户已在 TUI 中提供本地文件路径作为附件：%s\n\n请优先使用 read_file 或可用的文档/办公工具读取该文件内容，再按当前工作流阶段进行分析。用户原始输入：%s", abs, text)
}

func firstExistingWorkflowPath(text string) string {
	for _, candidate := range workflowPathCandidates(text) {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.Trim(candidate, "\"'“”‘’")
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func workflowPathCandidates(text string) []string {
	var out []string
	for _, m := range tuiQuotedPathPattern.FindAllStringSubmatch(text, -1) {
		for i := 1; i < len(m); i++ {
			if strings.TrimSpace(m[i]) != "" {
				out = append(out, m[i])
			}
		}
	}
	fields := strings.Fields(text)
	for i := range fields {
		if looksLikePath(fields[i]) {
			out = append(out, strings.TrimRight(fields[i], ",，。;；"))
		}
	}
	return out
}

func looksLikePath(s string) bool {
	s = strings.Trim(s, "\"'“”‘’")
	return strings.Contains(s, `:\`) || strings.Contains(s, `:/`) || strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

// truncateTUI truncates a string for logging.
func truncateTUI(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
