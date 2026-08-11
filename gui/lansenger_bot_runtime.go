package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// lansengerBotRuntime owns mutable agent state for one Lansenger bot profile.
// Unlike the historical gateway-local handler, no conversation, confirmation,
// loop, tool registry or HTTP client state is shared with another bot.
type lansengerBotRuntime struct {
	profileID string
	handler   *IMMessageHandler
	memory    *agent.ConversationMemory
	confirm   *aiConfirmationStore
	queue     *lansengerBotTurnQueue
}

func lansengerBotDataDir(app *App, profileID string) string {
	if app == nil {
		return ""
	}
	return filepath.Join(app.GetDataDir(), "lansenger", "bots", safeFileToken(profileID))
}

func newLansengerBotConversationMemory(app *App, profileID string) *agent.ConversationMemory {
	base := lansengerBotDataDir(app, profileID)
	if base == "" {
		return agent.NewConversationMemory()
	}
	return agent.NewPersistentConversationMemory(filepath.Join(base, "conversation.json"))
}

func newLansengerBotConfirmationStore(app *App, profileID string) *aiConfirmationStore {
	base := lansengerBotDataDir(app, profileID)
	if base == "" {
		return newAIConfirmationStore("")
	}
	return newAIConfirmationStore(filepath.Join(base, "confirmations.json"))
}

func newLansengerBotRuntime(app *App, manager *RemoteSessionManager, profileID string) (*lansengerBotRuntime, error) {
	profileID = strings.TrimSpace(profileID)
	if app == nil || profileID == "" {
		return nil, fmt.Errorf("lansenger bot runtime requires app and profile ID")
	}
	memory := newLansengerBotConversationMemory(app, profileID)
	confirm := newLansengerBotConfirmationStore(app, profileID)
	handler := newIMMessageHandler(app, manager, memory, confirm)
	handler.lansengerBotProfileID = profileID
	configureLansengerBotHandler(app, handler)
	return &lansengerBotRuntime{
		profileID: profileID,
		handler:   handler,
		memory:    memory,
		confirm:   confirm,
		queue:     newLansengerBotTurnQueue(),
	}, nil
}

// configureLansengerBotHandler wires host services only. The constructed
// handler remains private to the profile and therefore retains private loop,
// interrupt and tool activation state.
func configureLansengerBotHandler(a *App, h *IMMessageHandler) {
	if a == nil || h == nil {
		return
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.contextResolver == nil {
		a.ensureContextResolver()
	}
	if a.sessionPrecheck == nil {
		a.ensureSessionPrecheck()
	}
	if a.capabilityGapDetector == nil {
		a.ensureCapabilityGapDetector()
	}
	// Capability-gap resolution mutates application-wide skill activation state;
	// do not share it between independent bot runtimes.
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(cloneHardwareToolDefinitionGenerator(a.toolDefGenerator))
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.usageTracker != nil {
		h.SetUsageTracker(a.usageTracker)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
	h.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
	if a.configManager != nil {
		h.SetConfigManager(a.configManager)
	}
	if a.templateManager != nil {
		h.SetTemplateManager(a.templateManager)
	}
	if a.scheduledTaskManager != nil {
		h.SetScheduledTaskManager(a.scheduledTaskManager)
	}
	if a.contextResolver != nil {
		h.SetContextResolver(a.contextResolver)
	}
	if a.sessionPrecheck != nil {
		h.SetSessionPrecheck(a.sessionPrecheck)
	}
	a.ensureStartupFeedback()
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall == nil {
		a.ensureSecurityFirewall()
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	// A profile-owned handler must never use the desktop/default file-forward
	// route. Inject this internal identity after every tool result is
	// materialized; tool payloads themselves cannot select a bot profile.
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		req.BotProfileID = h.lansengerBotProfileID
		return a.forwardDesktopFileToIMRequest(a.hubClient(), req)
	})
	a.ensureConversationArchiver()
	if a.conversationArchiver != nil {
		h.memory.Archiver = a.conversationArchiver
	}
}

func (r *lansengerBotRuntime) stop() {
	if r == nil {
		return
	}
	if r.queue != nil {
		r.queue.stop()
	}
	if r.handler != nil {
		r.handler.cancelAllSessionsForShutdown()
	}
	if r.memory != nil {
		r.memory.Stop()
	}
}

// lansengerBotTurnQueue executes one turn at a time in receive order. This is
// deliberately per profile: different bots can progress independently while
// a busy customer-service bot cannot interleave two users' replies. Its
// bounded input is intentionally non-blocking: an overloaded bot must reject
// a new turn explicitly instead of holding up the gateway receive loop.
type lansengerBotTurnQueue struct {
	ctx     context.Context
	cancel  context.CancelFunc
	turns   chan lansengerBotTurn
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	stopped bool
	// active is set only while the worker is executing a submitted turn. It
	// lets stop distinguish a full queue from a currently running request.
	// That distinction matters because shutdown must cancel the active turn
	// before waiting for the worker, otherwise a long agent call can make a bot
	// profile update or application exit hang indefinitely.
	active context.CancelFunc
}

type lansengerBotTurn struct {
	ctx  context.Context
	turn func(context.Context)
}

func newLansengerBotTurnQueue() *lansengerBotTurnQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &lansengerBotTurnQueue{ctx: ctx, cancel: cancel, turns: make(chan lansengerBotTurn, 128), done: make(chan struct{})}
	go func() {
		defer close(q.done)
		for {
			select {
			case <-q.ctx.Done():
				return
			case queued := <-q.turns:
				// select may choose an already-buffered turn at the same time
				// cancellation becomes ready. Once stopped, discard queued work;
				// only the turn that was already active gets a cancellation signal.
				if q.ctx.Err() != nil {
					return
				}
				if queued.turn != nil {
					turnCtx, cancel := q.turnContext(queued.ctx)
					// stop may have won the race after the worker removed this
					// item from the channel. In that case turnContext canceled the
					// context and the callback must not be entered at all.
					if turnCtx.Err() != nil {
						cancel()
						q.clearActive()
						return
					}
					queued.turn(turnCtx)
					cancel()
					q.clearActive()
				}
			}
		}
	}()
	return q
}

func (q *lansengerBotTurnQueue) turnContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = q.ctx
	}
	turnCtx, cancel := context.WithCancel(parent)
	q.mu.Lock()
	if q.stopped {
		cancel()
	} else {
		q.active = cancel
	}
	q.mu.Unlock()
	return turnCtx, cancel
}

func (q *lansengerBotTurnQueue) clearActive() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.active = nil
	q.mu.Unlock()
}

func (q *lansengerBotTurnQueue) submit(ctx context.Context, turn func(context.Context)) bool {
	if q == nil || turn == nil {
		return false
	}
	if ctx == nil {
		ctx = q.ctx
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return false
	}
	select {
	case <-q.ctx.Done():
		return false
	case q.turns <- lansengerBotTurn{ctx: ctx, turn: turn}:
		return true
	default:
		return false
	}
}

func (q *lansengerBotTurnQueue) stop() {
	if q == nil {
		return
	}
	q.once.Do(func() {
		q.mu.Lock()
		q.stopped = true
		active := q.active
		q.mu.Unlock()
		q.cancel()
		if active != nil {
			active()
		}
		<-q.done
	})
}

func lansengerBotProfileGroupOptions(profile corelib.LansengerBotProfile) corelib.AppConfig {
	return corelib.AppConfig{
		LansengerGroupPolicy: profile.GroupPolicy, LansengerAllowedGroupIDs: profile.AllowedGroupIDs,
		LansengerIgnoredGroupIDs: profile.IgnoredGroupIDs, LansengerRequireMention: profile.RequireMention,
		LansengerRespondToAtAll: profile.RespondToAtAll, LansengerAutoMentionReply: profile.EffectiveAutoMentionReply(),
		LansengerAutoQuoteReply: profile.AutoQuoteReply, LansengerGroupKnowledgeSourceIDs: profile.KnowledgeSourceIDs,
		LansengerGroupAllowWebSearch: profile.AllowWebSearch, LansengerGroupAllowAllDirectories: profile.AllowAllDirectories,
		LansengerGroupAllowedDirectories: profile.AllowedDirectories, LansengerGroupFileMaxBytes: profile.GroupFileMaxBytes,
	}
}

func lansengerAssistantBinding(profile corelib.LansengerBotProfile) *agent.AssistantBinding {
	return &agent.AssistantBinding{
		BotProfileID: profile.ID, Mode: profile.EffectiveAssistantMode(), ExpertID: profile.ExpertID,
		InitialPrompt: profile.InitialPrompt, WorkingDirectory: profile.WorkingDirectory,
		DocumentDirectories: append([]string(nil), profile.DocumentDirectories...),
		AllowedDirectories:  append([]string(nil), profile.AllowedDirectories...),
		AllowAllDirectories: profile.AllowAllDirectories,
	}
}
