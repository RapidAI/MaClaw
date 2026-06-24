package main

// weixin_gateway.go implements the TUI-side WeChat gateway.
//
// Architecture: The TUI runs the same corelib/weixin.Gateway as the GUI,
// but routes incoming messages through corelib/agent.RunLoop (the shared
// agent loop) instead of the GUI's IMMessageHandler. This gives WeChat
// users the same agent capabilities (tools, memory, steering, workflow)
// as the TUI chat interface.
//
// The gateway runs as a background goroutine alongside the Bubble Tea
// event loop. Incoming WeChat messages are processed in their own
// goroutines (per-user serialization is handled by weixin.Gateway).
// Responses are sent back via the WeChat API (SendText/SendMedia).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	tea "github.com/charmbracelet/bubbletea"
)

// tuiWeixinGateway manages the WeChat gateway lifecycle in the TUI.
type tuiWeixinGateway struct {
	app     *TUIApp
	program *tea.Program

	mu        sync.Mutex
	gateway   *weixin.Gateway
	lastToken string
	running   bool
	stopOnce  sync.Once
	stopped   chan struct{} // closed when Stop() is called
}

// newTUIWeixinGateway creates a new TUI WeChat gateway manager.
func newTUIWeixinGateway(app *TUIApp) *tuiWeixinGateway {
	return &tuiWeixinGateway{
		app:     app,
		stopped: make(chan struct{}),
	}
}

// stopCh returns the channel that is closed when the gateway is stopped.
// Used by agent loop callbacks to detect early termination.
func (g *tuiWeixinGateway) stopCh() <-chan struct{} {
	return g.stopped
}

// SetProgram sets the Bubble Tea program reference for sending UI messages.
func (g *tuiWeixinGateway) SetProgram(p *tea.Program) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.program = p
}

// Start initializes and starts the WeChat gateway if configured.
// Safe to call multiple times — no-op if already running with same token.
func (g *tuiWeixinGateway) Start() {
	cfg := g.app.appConfig
	if !cfg.WeixinEnabled || cfg.WeixinToken == "" {
		g.Stop()
		return
	}

	g.mu.Lock()
	if g.gateway != nil && g.lastToken == cfg.WeixinToken {
		g.mu.Unlock()
		return
	}
	oldGw := g.gateway
	g.mu.Unlock()

	if oldGw != nil {
		_ = oldGw.Stop()
	}

	baseURL := strings.TrimSpace(cfg.WeixinBaseURL)
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}
	cdnURL := strings.TrimSpace(cfg.WeixinCDNURL)
	if cdnURL == "" {
		cdnURL = weixin.DefaultCDNBaseURL
	}

	gw := weixin.NewGateway(weixin.Config{
		Token:     cfg.WeixinToken,
		BaseURL:   baseURL,
		CDNURL:    cdnURL,
		AccountID: cfg.WeixinAccountID,
	}, g.onIncomingMessage)
	gw.SetStatusCallback(g.onStatusChange)

	g.mu.Lock()
	g.gateway = gw
	g.lastToken = cfg.WeixinToken
	g.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[tui-weixin] gateway start failed: %v", err)
		g.mu.Lock()
		g.gateway = nil
		g.lastToken = ""
		g.mu.Unlock()
		return
	}

	g.mu.Lock()
	g.running = true
	g.mu.Unlock()
	log.Printf("[tui-weixin] gateway started (baseURL=%s)", baseURL)
	g.emitStatus("connected")
}

// Stop shuts down the WeChat gateway. Does not close the stopCh — that is
// only closed by Shutdown() when the TUI process is exiting.
func (g *tuiWeixinGateway) Stop() {
	g.mu.Lock()
	gw := g.gateway
	if gw == nil {
		g.mu.Unlock()
		return
	}
	g.gateway = nil
	g.lastToken = ""
	g.running = false
	g.mu.Unlock()

	_ = gw.Stop()
	log.Printf("[tui-weixin] gateway stopped")
	g.emitStatus("disconnected")
}

// Shutdown signals all in-flight agent loops to stop and then stops the gateway.
// Called once when the TUI process is exiting.
func (g *tuiWeixinGateway) Shutdown() {
	g.stopOnce.Do(func() { close(g.stopped) })
	g.mu.Lock()
	g.program = nil
	g.mu.Unlock()
	g.Stop()
}

// IsRunning returns whether the gateway is currently active.
func (g *tuiWeixinGateway) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// Status returns the current gateway connection status.
func (g *tuiWeixinGateway) Status() string {
	g.mu.Lock()
	gw := g.gateway
	g.mu.Unlock()
	if gw == nil {
		return "disconnected"
	}
	if gw.IsRunning() {
		return "connected"
	}
	return "disconnected"
}

// onStatusChange is called by the gateway when connection status changes.
func (g *tuiWeixinGateway) onStatusChange(status string) {
	log.Printf("[tui-weixin] status: %s", status)
	g.emitStatus(status)
}

// emitStatus sends a status update to the TUI status bar.
func (g *tuiWeixinGateway) emitStatus(status string) {
	g.sendProgram(tuiWeixinStatusMsg{Status: status})
}

func (g *tuiWeixinGateway) sendProgram(msg tea.Msg) {
	g.mu.Lock()
	p := g.program
	select {
	case <-g.stopped:
		g.mu.Unlock()
		return
	default:
	}
	g.mu.Unlock()
	if p != nil {
		go p.Send(msg)
	}
}

// tuiWeixinStatusMsg is sent to the Bubble Tea program when WeChat status changes.
type tuiWeixinStatusMsg struct {
	Status string
}

// tuiWeixinIncomingMsg is sent to the Bubble Tea program when a WeChat message
// arrives (for UI notification purposes only — actual processing happens in
// the background goroutine).
type tuiWeixinIncomingMsg struct {
	FromUserID string
	Text       string
}

// onIncomingMessage is the callback invoked by weixin.Gateway for each message.
// It runs in a goroutine managed by the gateway (per-user serialization).
func (g *tuiWeixinGateway) onIncomingMessage(msg weixin.IncomingMessage) {
	log.Printf("[tui-weixin] incoming: user=%s text_len=%d media=%s",
		msg.FromUserID, len(msg.Text), msg.MediaType)

	// Notify the TUI that a message arrived (for status bar display).
	g.sendProgram(tuiWeixinIncomingMsg{FromUserID: msg.FromUserID, Text: msg.Text})

	// Check LLM is configured.
	if !tuiAppLLMReady(g.app) {
		g.sendText(msg.FromUserID, msg.ContextToken,
			i18n.T(i18n.MsgLLMNotConfigured, "zh"))
		return
	}

	// Build user text (prepend media path info if non-image media).
	text := msg.Text
	if msg.MediaType == "image" && len(msg.MediaData) > 0 {
		// Image-only messages: prompt the LLM to describe what it sees.
		if text == "" {
			text = "[用户发送了一张图片]"
		}
	} else if msg.MediaType != "" && len(msg.MediaData) > 0 {
		text = fmt.Sprintf("[收到%s]\n%s", msg.MediaType, text)
	}
	if text == "" {
		return
	}

	// Run the agent loop and send the response.
	g.processMessage(msg.FromUserID, msg.ContextToken, text)
}

// processMessage runs the agent loop for a WeChat message and sends the response.
func (g *tuiWeixinGateway) processMessage(userID, contextToken, text string) {
	// Panic recovery: if the agent loop or a tool panics, send an error
	// message to the WeChat user instead of silently dropping the response.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tui-weixin] PANIC in processMessage (user=%s): %v", userID, r)
			g.sendText(userID, contextToken, "❌ 内部错误，请稍后重试")
		}
	}()

	// Use a per-user conversation history keyed by "wx:<userID>".
	historyKey := "wx:" + userID

	// Send progress updates to the WeChat user (rate-limited).
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if progressText == "" {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		g.sendText(userID, contextToken,
			i18n.T(i18n.MsgProgressPrefix, "zh")+stripped)
	}

	// NOTE: Workflow interception is skipped for WeChat messages.
	// The TUI workflow engine uses a hardcoded "tui-user" userID and shared
	// pendingPhasePrompt/workflowAgentLoop state. Running workflow interception
	// from WeChat would conflict with the TUI chat user's workflow state.
	// WeChat users get the full agent loop (tools, memory, steering) but
	// not the multi-phase workflow experience. This is a known limitation
	// of the TUI's single-user workflow architecture.

	// Load conversation history.
	history := g.app.history.Load(historyKey)

	// Build callbacks for the agent loop. The stopCh is closed when the
	// gateway is stopped, allowing in-flight agent loops to exit early.
	cb := &tuiWeixinCallbacks{
		app:        g.app,
		onProgress: onProgress,
		stopCh:     g.stopCh(),
	}

	// Run the shared agent loop.
	result := agent.RunLoop(cb, text, history, nil)

	// Save conversation history with trim to prevent unbounded growth.
	// WeChat sessions can run 24/7, so we apply the same trim as TrimHistory.
	history = append(history, agent.ConversationEntry{Role: "user", Content: text})
	if result.Text != "" {
		history = append(history, agent.ConversationEntry{Role: "assistant", Content: result.Text})
	}
	history = agent.TrimHistory(history)
	g.app.history.Save(historyKey, history)

	// Trigger online extraction asynchronously.
	if g.app.memoryStore != nil && len(history) >= 4 {
		if oe := g.app.memoryStore.OnlineExtractor(); oe != nil {
			go func() {
				msgs := convertTUIHistoryToMessages(history, 10)
				if len(msgs) < 2 {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				oeResult := oe.ExtractAndIntegrate(ctx, msgs, "", time.Now(), historyKey)
				if oeResult.Added > 0 || oeResult.Updated > 0 || oeResult.Deleted > 0 {
					log.Printf("[tui-weixin] online_extraction: extracted=%d added=%d updated=%d deleted=%d",
						oeResult.ExtractedFacts, oeResult.Added, oeResult.Updated, oeResult.Deleted)
				}
			}()
		}
	}

	// Send the response.
	if result.Error != "" && result.Text == "" {
		g.sendText(userID, contextToken, "❌ "+result.Error)
		return
	}
	responseText := textutil.StripMarkdown(result.Text)
	if responseText == "" && result.Error != "" {
		responseText = "❌ " + result.Error
	}
	if responseText == "" {
		// Agent loop returned nothing (e.g., cancelled or empty LLM response).
		// Send a minimal acknowledgment so the user knows the message was received.
		responseText = "✅"
	}
	g.sendText(userID, contextToken, responseText)
}

// sendText sends a text message to a WeChat user via the gateway.
func (g *tuiWeixinGateway) sendText(toUserID, contextToken, text string) {
	g.mu.Lock()
	gw := g.gateway
	g.mu.Unlock()
	if gw == nil {
		return
	}
	if contextToken == "" {
		contextToken = gw.GetContextToken(toUserID)
	}
	if err := gw.SendText(context.Background(), weixin.OutgoingText{
		ToUserID:     toUserID,
		Text:         text,
		ContextToken: contextToken,
	}); err != nil {
		log.Printf("[tui-weixin] SendText error (to=%s): %v", toUserID, err)
	}
}

// ---------------------------------------------------------------------------
// tuiWeixinCallbacks — agent.LoopCallbacks for WeChat messages
// ---------------------------------------------------------------------------

// tuiWeixinCallbacks implements agent.LoopCallbacks for WeChat message processing.
// It reuses the TUIApp's infrastructure (tools, memory, steering) but runs
// independently of the Bubble Tea UI (no streaming to terminal).
type tuiWeixinCallbacks struct {
	app        *TUIApp
	onProgress func(string)
	stopCh     <-chan struct{} // closed when gateway is stopping
}

func (c *tuiWeixinCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *tuiWeixinCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.app.appConfig.MaclawAgentMaxIterations)
}

func (c *tuiWeixinCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	deps := c.app.buildSystemPromptDeps()
	return agent.BuildSystemPrompt(deps, userText, isFirstTurn)
}

func (c *tuiWeixinCallbacks) BuildTools(userText string) []map[string]interface{} {
	return c.app.toolRegistry.BuildDefinitions()
}

func (c *tuiWeixinCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	ctx, cancel := contextFromCancelPoll(c.ShouldStop)
	defer cancel()
	return c.app.toolRegistry.ExecuteCtx(ctx, name, args)
}

func (c *tuiWeixinCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.app == nil {
		return true
	}
	return c.app.isWorkflowToolAllowedTUI(name)
}

func (c *tuiWeixinCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.app == nil {
		return true, ""
	}
	return c.app.isWorkflowToolCallAllowedTUI(name, argsJSON)
}

func (c *tuiWeixinCallbacks) OnToken(delta string) {
	// No streaming to terminal for WeChat messages — response is sent
	// as a complete message after the agent loop finishes.
}

func (c *tuiWeixinCallbacks) OnProgress(text string) {
	if c.onProgress != nil {
		c.onProgress(text)
	}
}

func (c *tuiWeixinCallbacks) OnToolCall(name string) {
	// No UI update for WeChat — messages are sent as complete responses.
}

func (c *tuiWeixinCallbacks) OnToolResult(name string) {
	// No UI update for WeChat.
}

func (c *tuiWeixinCallbacks) ShouldStop() bool {
	select {
	case <-c.stopCh:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Bubble Tea integration
// ---------------------------------------------------------------------------

// tuiWeixinStatusText returns a short status label for the status bar.
func tuiWeixinStatusText(lang, status string) string {
	switch status {
	case "connected":
		if lang == "zh" {
			return "微信已连接"
		}
		return "WeChat connected"
	case "connecting":
		if lang == "zh" {
			return "微信连接中..."
		}
		return "WeChat connecting..."
	default:
		return ""
	}
}

// handleTUIWeixinStatus processes WeChat status messages in the Bubble Tea Update loop.
func handleTUIWeixinStatus(m *tuiModel, msg tuiWeixinStatusMsg) {
	lang := m.uiLang()
	statusText := tuiWeixinStatusText(lang, msg.Status)
	if statusText != "" {
		m.root.StatusBar.SetMessage(statusText)
	}
}

// handleTUIWeixinIncoming processes WeChat incoming message notifications.
func handleTUIWeixinIncoming(m *tuiModel, msg tuiWeixinIncomingMsg) {
	lang := m.uiLang()
	preview := msg.Text
	if len([]rune(preview)) > 20 {
		preview = string([]rune(preview)[:20]) + "..."
	}
	var statusMsg string
	if lang == "zh" {
		statusMsg = fmt.Sprintf("📱 微信消息: %s", preview)
	} else {
		statusMsg = fmt.Sprintf("📱 WeChat: %s", preview)
	}
	m.root.StatusBar.SetMessage(statusMsg)
}
