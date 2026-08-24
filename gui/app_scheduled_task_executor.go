package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

const scheduledTaskExecutorOwnerID = "system:scheduler:background:scheduled-task-executor"

// scheduledTaskConversationOwner keeps assistant bindings for autonomous work
// distinct from one another. Assistant bindings are process-local and keyed by
// IMUserMessage.UserID while a turn is executing; using the historic constant
// "scheduled_task" for every profile would let two concurrent tasks overwrite
// each other's expert and filesystem boundary. Legacy desktop tasks retain
// their existing owner for compatibility, while a profile task is scoped by
// its immutable bot and scheduler task IDs.
func scheduledTaskConversationOwner(task *scheduler.ScheduledTask) string {
	if task == nil {
		return "scheduled_task"
	}
	profileID := task.BotProfileID
	if profileID == "" && task.Delivery != nil {
		profileID = task.Delivery.BotProfileID
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "scheduled_task"
	}
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" {
		// A persisted scheduler task always has an ID. Keep even malformed
		// in-memory tasks isolated from other profiles rather than allowing a
		// fallback to the shared legacy owner.
		taskID = "unsaved"
	}
	return fmt.Sprintf("lansenger-scheduled:%d:%s:%d:%s", len(profileID), profileID, len(taskID), taskID)
}

// scheduledTaskHandler selects the agent instance that owns a task. Profile
// deliveries are created only by that profile's private handler, so executing
// the task through a desktop/Hub handler would silently lose its expert and
// filesystem boundary. Missing profile runtime is fail-closed.
func (a *App) scheduledTaskHandler(task *scheduler.ScheduledTask, fallback *IMMessageHandler) (*IMMessageHandler, *agent.AssistantBinding, error) {
	profileID := ""
	if task != nil {
		profileID = task.BotProfileID
		// Delivery-level ownership lets tasks created by the first multi-bot
		// release retain their agent boundary after this task-level field was
		// introduced.
		if profileID == "" && task.Delivery != nil {
			profileID = task.Delivery.BotProfileID
		}
	}
	if profileID == "" {
		if fallback == nil {
			return nil, nil, fmt.Errorf("agent handler not initialized — LLM may not be configured")
		}
		return fallback, nil, nil
	}
	manager, err := a.lansengerGatewayManagerForSend(profileID)
	if err != nil || manager == nil || manager.profile == nil {
		if err == nil {
			err = fmt.Errorf("lansenger bot profile %q is unavailable", profileID)
		}
		return nil, nil, err
	}
	runtime, err := manager.ensureProfileRuntime()
	if err != nil || runtime == nil || runtime.handler == nil {
		if err == nil {
			err = fmt.Errorf("lansenger bot profile %q runtime is unavailable", profileID)
		}
		return nil, nil, err
	}
	return runtime.handler, lansengerAssistantBinding(manager.configuredProfile(nil)), nil
}

// buildLocalScheduledTaskExecutor creates a TaskExecutor that runs scheduled
// tasks through the local IMMessageHandler without requiring Hub connectivity.
// This ensures scheduled tasks fire for desktop-only users.
func (a *App) buildLocalScheduledTaskExecutor() scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(scheduledTaskExecutorOwnerID, "delegate_task", map[string]interface{}{"agent": "scheduled_task", "request": task.Action, "task_id": task.ID, "task_name": task.Name}); err != nil {
			return "", err
		}
		// Show a quiet notification when the task starts executing.
		if ShowNotification != nil {
			ShowNotification(
				"定时任务开始",
				fmt.Sprintf("%s: %s", task.Name, scheduler.TruncateStr(task.Action, 100)),
				1, // info icon
			)
		}

		handler, binding, err := a.scheduledTaskHandler(task, a.ensureLocalIMHandler())
		if err != nil {
			return "", err
		}

		// Prepend a hint so the agent knows this is an autonomous task.
		actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)

		onProgress := func(text string) {
			log.Printf("[ScheduledTask] %s progress: %s", task.Name, text)
		}

		resp := handler.HandleIMMessageWithProgressAndStream(IMUserMessage{
			UserID:           scheduledTaskConversationOwner(task),
			Platform:         "scheduler",
			Text:             actionText,
			MinIterations:    50,
			IsBackground:     true,
			CancelCtx:        ctx,
			AssistantBinding: binding,
		}, onProgress, nil, nil, nil)

		if resp == nil {
			return "", fmt.Errorf("nil response from agent")
		}

		resultText := resp.Text
		hasError := resp.Error != ""
		var runErr error
		if hasError {
			runErr = fmt.Errorf("%s", resp.Error)
		}
		// Fold timeout/cancel into runErr so partial text can still be delivered.
		runErr = scheduler.AnnotateRunErrWithContext(ctx, runErr)

		// Structured delivery even on timeout when partial text exists (process-type reports).
		if err := a.deliverScheduledTaskResult(task, resultText, runErr); err != nil {
			log.Printf("[ScheduledTask] %s delivery: %v", task.Name, err)
			resultText, runErr = scheduler.MergeDeliveryOutcome(task.Delivery, resultText, runErr, err)
		}

		showScheduledTaskCompletionNotification(task.Name, resultText, resp.Error, hasError, runErr)

		if runErr != nil {
			return resultText, runErr
		}
		return resultText, nil
	}
}

// buildHubScheduledTaskExecutor creates a TaskExecutor that runs scheduled
// tasks through the IMMessageHandler AND pushes results to IM channels via Hub.
// This upgrades the local executor when Hub connectivity is available.
func (a *App) buildHubScheduledTaskExecutor(hubClient *RemoteHubClient) scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(scheduledTaskExecutorOwnerID, "delegate_task", map[string]interface{}{"agent": "scheduled_task", "request": task.Action, "task_id": task.ID, "task_name": task.Name}); err != nil {
			return "", err
		}
		// Show a quiet notification when the task starts executing.
		if ShowNotification != nil {
			ShowNotification(
				"定时任务开始",
				fmt.Sprintf("%s: %s", task.Name, scheduler.TruncateStr(task.Action, 100)),
				1,
			)
		}

		onProgress := func(text string) {
			log.Printf("[ScheduledTask] %s progress: %s", task.Name, text)
		}

		actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)

		handler, binding, err := a.scheduledTaskHandler(task, hubClient.ensureIMHandler())
		if err != nil {
			return "", err
		}
		resp := handler.HandleIMMessageWithProgressAndStream(IMUserMessage{
			UserID:           scheduledTaskConversationOwner(task),
			Platform:         "scheduler",
			Text:             actionText,
			MinIterations:    50,
			IsBackground:     true,
			CancelCtx:        ctx,
			AssistantBinding: binding,
		}, onProgress, nil, nil, nil)

		if resp == nil {
			return "", fmt.Errorf("nil response from agent")
		}

		resultText := resp.Text
		hasError := resp.Error != ""
		var runErr error
		if hasError {
			runErr = fmt.Errorf("%s", resp.Error)
		}
		runErr = scheduler.AnnotateRunErrWithContext(ctx, runErr)

		// Prefer explicit delivery targets (channel / group / user).
		// Fall back to owner Hub proactive only when no structured delivery is set.
		// Deliver even on timeout when partial text exists.
		if task.Delivery != nil && task.Delivery.Active() {
			if err := a.deliverScheduledTaskResult(task, resultText, runErr); err != nil {
				a.log(fmt.Sprintf("[scheduled-task] delivery failed: %v", err))
				resultText, runErr = scheduler.MergeDeliveryOutcome(task.Delivery, resultText, runErr, err)
			}
		} else if store := a.scheduleDispatchBindingStore(); store != nil {
			if _, ok := store.Get(task.ID); ok {
				if err := a.deliverScheduledTaskResult(task, resultText, runErr); err != nil {
					a.log(fmt.Sprintf("[scheduled-task] managed dispatch failed: %v", err))
				}
			} else {
				var proactiveMsg string
				if hasError {
					if resultText != "" {
						proactiveMsg = fmt.Sprintf("Task %s completed with an error.\n\nResult:\n%s\n\nError: %s", task.Name, resultText, resp.Error)
					} else {
						proactiveMsg = fmt.Sprintf("Task %s completed with an error.\n\nError: %s", task.Name, resp.Error)
					}
				} else if resultText != "" {
					proactiveMsg = fmt.Sprintf("Task %s completed successfully.\n\nResult:\n%s", task.Name, resultText)
				}
				if proactiveMsg != "" {
					if err := hubClient.SendIMProactiveMessage(proactiveMsg); err != nil {
						a.log(fmt.Sprintf("[scheduled-task] proactive message send failed: %v", err))
					}
				}
			}
		}

		showScheduledTaskCompletionNotification(task.Name, resultText, resp.Error, hasError, runErr)

		if runErr != nil {
			return resultText, runErr
		}
		return resultText, nil
	}
}

func showScheduledTaskCompletionNotification(taskName, resultText, respError string, hasError bool, runErr error) {
	notifSummary := resultText
	if hasError && notifSummary == "" {
		notifSummary = respError
	}
	if notifSummary == "" {
		return
	}
	if FlashAndBeep != nil {
		FlashAndBeep()
	}
	notifTitle := "定时任务完成"
	// Soft delivery warnings keep task success; prefer that title over "失败".
	if scheduler.HasDeliveryWarning(resultText) && runErr == nil {
		notifTitle = "定时任务完成(投递有警告)"
	} else if hasError || runErr != nil {
		notifTitle = "定时任务失败"
	}
	if ShowNotification != nil {
		// Keep soft delivery warning visible inside the short toast body.
		body := scheduler.TruncatePreservingDeliveryWarning(notifSummary, 200)
		ShowNotification(
			notifTitle,
			fmt.Sprintf("%s: %s", taskName, body),
			1,
		)
	}
}

// ensureLocalIMHandler returns the IMMessageHandler, creating it if needed.
// This is a lazy accessor — the handler is created on first use, not at
// scheduler startup time. This handles the case where a catch-up task fires
// before Hub infrastructure is fully initialized.
func (a *App) ensureLocalIMHandler() *IMMessageHandler {
	// Ensure the local/degraded Hub client exists so scheduled tasks can run
	// even when remote machine credentials are missing.
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil
	}
	return hubClient.ensureIMHandler()
}
