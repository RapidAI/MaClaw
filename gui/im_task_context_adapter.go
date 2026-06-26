package main

// im_task_context_adapter.go — Bridges the GUI's LLMClassify to the
// corelib TaskLLMClassifier interface, and provides initialization and
// integration helpers for the TaskContextManager.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func guiTaskContextConfig() agent.TaskContextConfig {
	return agent.DefaultTaskContextConfig()
}

// taskContextLLMAdapter adapts IMMessageHandler.LLMClassify to the
// agent.TaskLLMClassifier interface.
type taskContextLLMAdapter struct {
	handler *IMMessageHandler
}

func (a *taskContextLLMAdapter) Classify(systemPrompt, userMessage string, timeoutSec int) (string, error) {
	return a.ClassifyWithContext(context.Background(), systemPrompt, userMessage, timeoutSec)
}

func (a *taskContextLLMAdapter) ClassifyWithContext(ctx context.Context, systemPrompt, userMessage string, timeoutSec int) (string, error) {
	if a.handler == nil {
		return "", fmt.Errorf("handler not initialized")
	}
	ctx = llm.WithRequestTraceIfMissing(ctx, "task-context")
	result, err := a.handler.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      systemPrompt,
		UserMessage:       userMessage,
		TimeoutSec:        timeoutSec,
		Tag:               "task-context",
		PreferLightweight: true,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// initTaskContextManager creates and wires the TaskContextManager and
// TaskArchive into the IMMessageHandler. Called during handler setup.
func (h *IMMessageHandler) initTaskContextManager() {
	config := guiTaskContextConfig()

	// Determine archive storage path.
	archivePath := ""
	if h.app != nil {
		dataDir := h.app.GetDataDir()
		if dataDir != "" {
			archivePath = filepath.Join(dataDir, "task_archive.json")
		}
	}

	h.taskArchive = agent.NewTaskArchive(archivePath, config.MaxArchivedTasks)

	// Always create the LLM adapter — it checks LLM availability at call
	// time, not at init time. This handles the case where the user
	// configures LLM after the handler is created.
	llm := &taskContextLLMAdapter{handler: h}

	h.taskContextManager = agent.NewTaskContextManager(config, llm)
}

// ensureTaskContextManager lazily initializes the TaskContextManager if
// it hasn't been set up yet. Safe to call multiple times.
func (h *IMMessageHandler) ensureTaskContextManager() {
	if h.taskContextManager == nil {
		h.initTaskContextManager()
	}
}

// resolveTaskContext uses the TaskContextManager to determine the action
// for a new user message. Returns the decision and applies side effects
// (archiving current task, restoring recalled task, clearing history).
func (h *IMMessageHandler) resolveTaskContext(
	ctx context.Context,
	userID, trimmedMsg string,
	history []agent.ConversationEntry,
	hasPendingAskUser bool,
	isConfirmedResume bool,
	explicitNewTask bool,
) agent.TaskContextDecision {
	h.ensureTaskContextManager()

	var archived []agent.ArchivedTask
	if h.taskArchive != nil {
		archived = h.taskArchive.List(userID)
	}

	hasActiveUnderstanding := false

	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-context", OwnerID: userID})
	lastAccess := time.Time{}
	if h.memory != nil {
		lastAccess = h.memory.LastAccessTime(userID)
	}
	input := agent.ResolveInput{
		Context:                       ctx,
		OwnerID:                       userID,
		UserMessage:                   trimmedMsg,
		History:                       history,
		LastAccess:                    lastAccess,
		ArchivedTasks:                 archived,
		HasPendingAskUser:             hasPendingAskUser,
		IsConfirmedResume:             isConfirmedResume,
		HasActiveUnderstandingSession: hasActiveUnderstanding,
		HasIncompleteTaskMarker:       hasIncompleteTaskMarker(history),
		HasActiveBackgroundTask:       h.hasActiveCommandBackgroundTaskForOwner(userID),
		ExplicitNewTask:               explicitNewTask,
	}

	decision := h.taskContextManager.Resolve(input)

	log.Printf("[TaskContext] user=%s action=%s reason=%q source=%s historyLen=%d archivedLen=%d",
		userID, decision.Action, decision.Reason, decision.Source, len(history), len(archived))

	return decision
}

// archiveCurrentTask saves the current conversation as an archived task
// before clearing history for a new task.
func (h *IMMessageHandler) archiveCurrentTask(userID string, history []agent.ConversationEntry, status agent.ArchivedTaskStatus) {
	if h.taskArchive == nil || len(history) < 2 {
		return // too short to be worth archiving
	}

	projectPath := ""
	if h.app != nil {
		projectPath = strings.TrimSpace(h.getCurrentProjectPath())
	}

	task := agent.BuildArchivedTask(userID, history, status, projectPath)
	h.taskArchive.Archive(task)

	// Also save to long-term memory store if available.
	if h.memoryStore != nil {
		if summary := buildQuickSummary(history); summary != "" {
			tags := []string{"archived_task", task.ID, string(status)}
			if projectPath != "" {
				tags = append(tags, projectPath)
			}
			_, err := h.memoryStore.UpsertConversationSummary(memory.ConversationSummaryUpsertOptions{
				Title:            task.Summary,
				Content:          summary,
				Tags:             tags,
				IdentityTagCount: 2,
				OwnerID:          userID,
				SourceType:       "archived_task",
			})
			if err == nil && h.app != nil {
				h.app.triggerMemoryPipelineSoon(45 * time.Second)
			}
		}
	}

	log.Printf("[TaskContext] archived task %s for user %s: %q", task.ID, userID, truncateRunes(task.Summary, 60))
}

// restoreRecalledTask loads an archived task's compressed history into
// the conversation memory, giving the agent context about the recalled task.
func (h *IMMessageHandler) restoreRecalledTask(userID, taskID string) bool {
	if h.taskArchive == nil {
		return false
	}

	task, ok := h.taskArchive.Get(userID, taskID)
	if !ok {
		log.Printf("[TaskContext] recall failed: task %s not found for user %s", taskID, userID)
		return false
	}

	// Clear current conversation and inject the recalled task's context.
	h.memory.Clear(userID)
	h.clearPerUserSessionState(userID)

	if len(task.CompressedHistory) > 0 {
		// Inject a context marker followed by the compressed history.
		entries := []agent.ConversationEntry{
			{
				Role: "user",
				Content: fmt.Sprintf("[系统：用户要求恢复之前的任务]\n原始任务：%s\n状态：%s",
					truncateRunes(task.LastRequest, 200), task.Status),
			},
			{
				Role:    "assistant",
				Content: "好的，我已恢复之前任务的上下文，将基于此继续工作。",
			},
		}
		entries = append(entries, task.CompressedHistory...)
		h.memory.Save(userID, entries)
	}

	log.Printf("[TaskContext] restored task %s for user %s: %q", taskID, userID, truncateRunes(task.Summary, 60))
	return true
}
