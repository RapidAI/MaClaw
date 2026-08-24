package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

type horizonSession struct {
	mu               sync.Mutex
	ownerID          string
	requestID        string
	lang             string
	state            *longhorizon.TaskState
	cancel           context.CancelFunc
	loopCtx          *LoopContext
	inbox            []string
	notify           chan struct{}
	status           string
	asking           bool
	resumeAsk        bool
	started          bool
	cancelled        bool
	cancelNotified   bool
	finalized        bool
	experienceWrites int
	eventSeq         int
	storeRoot        string
	handler          *IMMessageHandler
	eventScopeID      string
	computerUseOwner  string
	browserSessionIDs []string
	createdBrowserIDs map[string]bool
}

func (s *horizonSession) activeUserGoal() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled || s.finalized || s.state == nil {
		return ""
	}
	return strings.TrimSpace(s.state.UserGoal)
}

func (h *IMMessageHandler) activeHorizonWorkingStateGoal(userID string) string {
	if h == nil {
		return ""
	}
	return h.loadHorizonSessionOrRunning(userID).activeUserGoal()
}

func (s *horizonSession) computerUseOwnerOr(fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if s == nil {
		return fallback
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := strings.TrimSpace(s.computerUseOwner); o != "" {
		return o
	}
	return fallback
}

func (s *horizonSession) rememberBrowserSession(id string, created bool) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	for _, existing := range s.browserSessionIDs {
		if existing == id {
			if created {
				if s.createdBrowserIDs == nil {
					s.createdBrowserIDs = map[string]bool{}
				}
				s.createdBrowserIDs[id] = true
			}
			task, owner, status, round := s.horizonLogIDsLocked()
			s.mu.Unlock()
			horizonLogIDs(task, owner, status, round, "browser_session", horizonLogKV(horizonLogField("id", id), fmt.Sprintf("created=%v reused=true", created)))
			return
		}
	}
	s.browserSessionIDs = append(s.browserSessionIDs, id)
	if created {
		if s.createdBrowserIDs == nil {
			s.createdBrowserIDs = map[string]bool{}
		}
		s.createdBrowserIDs[id] = true
	}
	task, owner, status, round := s.horizonLogIDsLocked()
	s.mu.Unlock()
	horizonLogIDs(task, owner, status, round, "browser_session", horizonLogKV(horizonLogField("id", id), fmt.Sprintf("created=%v", created)))
}

func (s *horizonSession) latestBrowserSessionID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.browserSessionIDs) == 0 {
		return ""
	}
	return s.browserSessionIDs[len(s.browserSessionIDs)-1]
}

func (s *horizonSession) takeCreatedBrowserSessionIDs() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.createdBrowserIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.createdBrowserIDs))
	for _, id := range s.browserSessionIDs {
		if s.createdBrowserIDs[id] {
			out = append(out, id)
		}
	}
	s.createdBrowserIDs = nil
	return out
}

func (h *IMMessageHandler) horizonActive(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if _, ok := h.horizonSessions.Load(userID); ok {
		return true
	}
	return h.horizonSupervisorRunning(userID)
}

func (h *IMMessageHandler) horizonSupervisorRunning(userID string) bool {
	if h == nil {
		return false
	}
	_, ok := h.horizonRunning.Load(strings.TrimSpace(userID))
	return ok
}

func (h *IMMessageHandler) markHorizonRunning(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	h.horizonRunning.Store(strings.TrimSpace(sess.ownerID), sess)
}

func (h *IMMessageHandler) clearHorizonRunning(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	userID := strings.TrimSpace(sess.ownerID)
	if cur, ok := h.horizonRunning.Load(userID); ok && cur == sess {
		h.horizonRunning.Delete(userID)
	}
}

func (h *IMMessageHandler) loadHorizonSession(userID string) *horizonSession {
	if h == nil {
		return nil
	}
	v, ok := h.horizonSessions.Load(strings.TrimSpace(userID))
	if !ok {
		return nil
	}
	sess, _ := v.(*horizonSession)
	return sess
}

func (h *IMMessageHandler) loadHorizonSessionOrRunning(userID string) *horizonSession {
	if sess := h.loadHorizonSession(userID); sess != nil {
		return sess
	}
	if h == nil {
		return nil
	}
	v, ok := h.horizonRunning.Load(strings.TrimSpace(userID))
	if !ok {
		return nil
	}
	sess, _ := v.(*horizonSession)
	return sess
}

func (h *IMMessageHandler) storeHorizonSession(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	sess.ownerID = strings.TrimSpace(sess.ownerID)
	h.horizonSessions.Store(sess.ownerID, sess)
}

func (h *IMMessageHandler) storeHorizonSessionIfAbsent(sess *horizonSession) bool {
	if h == nil || sess == nil {
		return false
	}
	sess.ownerID = strings.TrimSpace(sess.ownerID)
	if sess.ownerID == "" {
		return false
	}
	_, loaded := h.horizonSessions.LoadOrStore(sess.ownerID, sess)
	return !loaded
}

func (h *IMMessageHandler) dropHorizonSession(userID string) {
	if h == nil {
		return
	}
	h.horizonSessions.Delete(strings.TrimSpace(userID))
}

func (h *IMMessageHandler) dropHorizonSessionIf(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	userID := strings.TrimSpace(sess.ownerID)
	if cur := h.loadHorizonSession(userID); cur == sess {
		h.horizonSessions.Delete(userID)
	}
}

func (h *IMMessageHandler) cancelHorizonSession(userID string) bool {
	return h.cancelHorizonSessionWithReason(userID, "")
}

func (h *IMMessageHandler) cancelHorizonSessionWithReason(userID, reason string) bool {
	sess := h.loadHorizonSessionOrRunning(userID)
	if sess == nil {
		return false
	}
	reason = strings.TrimSpace(reason)
	held := true
	sess.mu.Lock()
	defer func() {
		if held {
			sess.mu.Unlock()
		}
	}()
	sess.cancelled = true
	if reason == "session" || reason == "shutdown" {
		sess.cancelNotified = true
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	if sess.loopCtx != nil {
		sess.loopCtx.Cancel()
	}
	if sess.state != nil {
		sess.state.Status = longhorizon.StatusCancelled
		sess.state.Completed = false
	}
	cuOwner := strings.TrimSpace(sess.computerUseOwner)
	if cuOwner == "" {
		cuOwner = strings.TrimSpace(sess.ownerID)
	}
	notify := sess.notify
	held = false
	sess.mu.Unlock()
	h.dropHorizonSession(userID)
	if cuOwner != "" {
		setHorizonComputerUseClaimOnly(cuOwner, false)
	}
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	sess.persist()
	extra := ""
	if reason != "" {
		extra = "reason=" + reason
	}
	horizonLog(sess, "cancel", extra)
	return true
}

func (h *IMMessageHandler) cancelAllHorizonSessions(reason string) {
	if h == nil {
		return
	}
	seen := map[string]bool{}
	collect := func(key, _ any) bool {
		id, ok := key.(string)
		if !ok {
			return true
		}
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return true
		}
		seen[id] = true
		h.cancelHorizonSessionWithReason(id, reason)
		return true
	}
	h.horizonSessions.Range(collect)
	h.horizonRunning.Range(collect)
}

func (sess *horizonSession) enqueue(text string) {
	if sess == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	sess.mu.Lock()
	held := true
	defer func() {
		if held {
			sess.mu.Unlock()
		}
	}()
	if sess.cancelled {
		return
	}
	sess.inbox = append(sess.inbox, text)
	if sess.state != nil {
		sess.state.Carryover = append(sess.state.Carryover, text)
		sess.state.Carryover = longhorizon.ClipCarryover(sess.state.Carryover)
	}
	sess.persistLocked()
	notify := sess.notify
	task, owner, status, round := sess.horizonLogIDsLocked()
	held = false
	sess.mu.Unlock()
	horizonLogIDs(task, owner, status, round, "inbox", horizonLogField("text", text))
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
}

func (sess *horizonSession) drainInbox() []string {
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	out := append([]string(nil), sess.inbox...)
	sess.inbox = nil
	sess.mu.Unlock()
	return out
}

func (sess *horizonSession) discardPendingInbox() {
	sess.drainInbox()
	if sess == nil || sess.notify == nil {
		return
	}
	select {
	case <-sess.notify:
	default:
	}
}

func joinHorizonInbox(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return strings.Join(out, "\n")
}

func (sess *horizonSession) persist() {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.persistLocked()
}

func (sess *horizonSession) persistLocked() {
	if sess == nil || sess.state == nil || strings.TrimSpace(sess.storeRoot) == "" {
		return
	}
	if sess.cancelled {
		sess.state.Status = longhorizon.StatusCancelled
		sess.state.Completed = false
	}
	if err := longhorizon.SaveTaskState(sess.storeRoot, sess.state); err != nil {
		task, owner, status, round := sess.horizonLogIDsLocked()
		horizonLogIDs(task, owner, status, round, "persist_fail", horizonLogField("err", err.Error()))
	}
	if sess.handler != nil {
		sess.handler.emitHorizonProjectionLocked(sess)
	}
}

func (sess *horizonSession) isCancelled() bool {
	if sess == nil {
		return true
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.cancelled
}

func (sess *horizonSession) markCancelNotified() {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	sess.cancelNotified = true
	sess.mu.Unlock()
}

func (h *IMMessageHandler) horizonStoreRoot() string {
	if h != nil && h.app != nil {
		return h.app.getMaclawBaseDir()
	}
	return ""
}

func (h *IMMessageHandler) horizonProjectPath(userID string, msg IMUserMessage) string {
	if h != nil && h.horizonProjectPathFn != nil {
		return strings.TrimSpace(h.horizonProjectPathFn(userID))
	}
	if msg.AssistantBinding != nil {
		if dir := strings.TrimSpace(msg.AssistantBinding.WorkingDirectory); dir != "" {
			return normalizeProjectSessionPath(dir)
		}
	}
	if msg.StartMenu != nil {
		if dir := strings.TrimSpace(msg.StartMenu.WorkingDir); dir != "" {
			return normalizeProjectSessionPath(dir)
		}
	}
	if h != nil && h.app != nil {
		if dir := strings.TrimSpace(h.app.BoundWorkingDirForOwner(userID)); dir != "" {
			return dir
		}
	}
	if dir := projectPathFromSessionOwnerID(userID); dir != "" {
		return dir
	}
	if h != nil && h.app != nil && strings.TrimSpace(userID) == desktopUserID {
		return strings.TrimSpace(h.app.EffectiveDesktopWorkingDir())
	}
	return ""
}

func (h *IMMessageHandler) emitHorizonProjectionLocked(sess *horizonSession) {
	if h == nil || h.app == nil || sess == nil || sess.state == nil {
		return
	}
	proj := longhorizon.ProjectTaskState(sess.state)
	if proj.SessionKey == "" {
		proj.SessionKey = sess.ownerID
	}
	if proj.EventScopeID == "" {
		proj.EventScopeID = sess.eventScopeID
	}
	payload, err := json.Marshal(proj)
	if err != nil {
		return
	}
	h.app.emitEvent("horizon:projection", string(payload))
}

func (h *IMMessageHandler) emitHorizonEvent(name, requestID, userID, text string) {
	if h == nil || h.app == nil {
		return
	}
	scope := ""
	if sess := h.loadHorizonSessionOrRunning(userID); sess != nil {
		scope = sess.eventScopeID
	}
	payload, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:    requestID,
		Text:         text,
		SessionKey:   userID,
		EventScopeID: scope,
	})
	if err != nil {
		return
	}
	h.app.emitEvent(name, string(payload))
}

func (h *IMMessageHandler) emitHorizonAssistantReply(sess *horizonSession, text string) {
	if h == nil || h.app == nil || sess == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	sess.mu.Lock()
	sess.eventSeq++
	seq := sess.eventSeq
	taskID := ""
	if sess.state != nil {
		taskID = sess.state.TaskID
	}
	ownerID := sess.ownerID
	scope := sess.eventScopeID
	sess.mu.Unlock()
	requestID := fmt.Sprintf("horizon-%s-%d", strings.TrimSpace(taskID), seq)
	roundEvt, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:    requestID,
		Text:         text,
		SessionKey:   ownerID,
		DisplayText:  "\u3010Horizon\u3011",
		EventScopeID: scope,
	})
	if err == nil {
		h.app.emitEvent("ai-assistant-foreground-round-started", string(roundEvt))
	}
	h.app.emitAIAssistantResponse(requestID, &IMAgentResponse{
		Text:         text,
		RequestID:    requestID,
		SessionKey:   ownerID,
		EventScopeID: scope,
	})
}

func (h *IMMessageHandler) startHorizonHeartbeat(ctx context.Context, userID string) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.emitHorizonEvent("ai-assistant-progress", "", userID, imHeartbeatMsg)
			}
		}
	}()
}
