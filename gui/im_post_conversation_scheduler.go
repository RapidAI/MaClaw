package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

type postConversationTask struct {
	UserID           string
	History          []agent.ConversationEntry
	DeferredMessages []memory.ConversationMessage
	EnqueuedAt       time.Time
}

type postConversationScheduler struct {
	h       *IMMessageHandler
	runTask func(context.Context, postConversationTask)
	owners  sync.Map // map[string]*postConversationOwnerWorker
}

type postConversationOwnerWorker struct {
	scheduler *postConversationScheduler
	userID    string
	queue     chan postConversationTask

	activeMu     sync.Mutex
	activeCancel context.CancelFunc
}

func newPostConversationScheduler(h *IMMessageHandler) *postConversationScheduler {
	s := &postConversationScheduler{h: h}
	s.runTask = func(ctx context.Context, task postConversationTask) {
		if h != nil {
			h.runPostConversationProcessing(ctx, task.UserID, task.History, task.DeferredMessages)
		}
	}
	return s
}

func (s *postConversationScheduler) Enqueue(task postConversationTask) {
	if s == nil || s.h == nil {
		return
	}
	task.UserID = strings.TrimSpace(task.UserID)
	if task.EnqueuedAt.IsZero() {
		task.EnqueuedAt = time.Now()
	}
	worker := s.workerForOwner(task.UserID)
	worker.cancelActive("replace")
	select {
	case worker.queue <- task:
		log.Printf("[post-conversation-scheduler] enqueue user=%s history_len=%d deferred=%d", task.UserID, len(task.History), len(task.DeferredMessages))
	default:
		select {
		case dropped := <-worker.queue:
			log.Printf("[post-conversation-scheduler] replace_pending user=%s dropped_history_len=%d new_history_len=%d", task.UserID, len(dropped.History), len(task.History))
		default:
		}
		worker.queue <- task
	}
}

func (s *postConversationScheduler) CancelOwner(userID, reason string) bool {
	if s == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if raw, ok := s.owners.Load(userID); ok {
		if worker, ok := raw.(*postConversationOwnerWorker); ok && worker != nil {
			activeCancelled := worker.cancelActive(reason)
			pendingDropped := worker.dropPending(reason)
			return activeCancelled || pendingDropped
		}
	}
	return false
}

func (s *postConversationScheduler) CancelAll(reason string) int {
	if s == nil {
		return 0
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancel-all"
	}
	cancelled := 0
	s.owners.Range(func(_, value any) bool {
		worker, ok := value.(*postConversationOwnerWorker)
		if ok && worker != nil {
			if worker.cancelActive(reason) {
				cancelled++
			}
			if worker.dropPending(reason) {
				cancelled++
			}
		}
		return true
	})
	if cancelled > 0 {
		log.Printf("[post-conversation-scheduler] cancel_all reason=%s cancelled=%d", reason, cancelled)
	}
	return cancelled
}

func (s *postConversationScheduler) workerForOwner(userID string) *postConversationOwnerWorker {
	if raw, ok := s.owners.Load(userID); ok {
		return raw.(*postConversationOwnerWorker)
	}
	worker := &postConversationOwnerWorker{
		scheduler: s,
		userID:    userID,
		queue:     make(chan postConversationTask, 1),
	}
	actual, loaded := s.owners.LoadOrStore(userID, worker)
	if loaded {
		return actual.(*postConversationOwnerWorker)
	}
	go worker.run()
	return worker
}

func (w *postConversationOwnerWorker) run() {
	for task := range w.queue {
		ctx, cancel := context.WithCancel(context.Background())
		w.activeMu.Lock()
		w.activeCancel = cancel
		w.activeMu.Unlock()

		waited := time.Since(task.EnqueuedAt)
		log.Printf("[post-conversation-scheduler] start user=%s wait=%s history_len=%d deferred=%d", task.UserID, waited.Round(time.Millisecond), len(task.History), len(task.DeferredMessages))
		if app := w.scheduler.h.app; app != nil && !app.waitForForegroundAgentIdle(ctx, "post-conversation", task.UserID) {
			log.Printf("[post-conversation-scheduler] skip user=%s reason=foreground_wait_cancelled", task.UserID)
		} else if w.scheduler.runTask != nil {
			w.scheduler.runTask(ctx, task)
		}

		w.activeMu.Lock()
		w.activeCancel = nil
		w.activeMu.Unlock()
		wasCancelled := ctx.Err() != nil
		cancel()
		log.Printf("[post-conversation-scheduler] finish user=%s cancelled=%v total=%s", task.UserID, wasCancelled, time.Since(task.EnqueuedAt).Round(time.Millisecond))
	}
}

func (w *postConversationOwnerWorker) cancelActive(reason string) bool {
	w.activeMu.Lock()
	cancel := w.activeCancel
	w.activeMu.Unlock()
	if cancel != nil {
		log.Printf("[post-conversation-scheduler] cancel_active user=%s reason=%s", w.userID, strings.TrimSpace(reason))
		cancel()
		return true
	}
	return false
}

func (w *postConversationOwnerWorker) dropPending(reason string) bool {
	if w == nil {
		return false
	}
	dropped := false
	for {
		select {
		case task := <-w.queue:
			log.Printf("[post-conversation-scheduler] drop_pending user=%s reason=%s history_len=%d", task.UserID, strings.TrimSpace(reason), len(task.History))
			dropped = true
		default:
			return dropped
		}
	}
}
