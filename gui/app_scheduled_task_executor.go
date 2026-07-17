package main

import (
	"context"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

const scheduledTaskExecutorOwnerID = "system:scheduler:background:scheduled-task-executor"

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

		handler := a.ensureLocalIMHandler()
		if handler == nil {
			return "", fmt.Errorf("agent handler not initialized — LLM may not be configured")
		}

		// Prepend a hint so the agent knows this is an autonomous task.
		actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)

		onProgress := func(text string) {
			log.Printf("[ScheduledTask] %s progress: %s", task.Name, text)
		}

		resp := handler.HandleIMMessageWithProgressAndStream(IMUserMessage{
			UserID:        "scheduled_task",
			Platform:      "scheduler",
			Text:          actionText,
			MinIterations: 50,
			IsBackground:  true,
			CancelCtx:     ctx,
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

		handler := hubClient.ensureIMHandler()
		resp := handler.HandleIMMessageWithProgressAndStream(IMUserMessage{
			UserID:        "scheduled_task",
			Platform:      "scheduler",
			Text:          actionText,
			MinIterations: 50,
			IsBackground:  true,
			CancelCtx:     ctx,
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
