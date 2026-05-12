package main

import (
	"context"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// buildLocalScheduledTaskExecutor creates a TaskExecutor that runs scheduled
// tasks through the local IMMessageHandler without requiring Hub connectivity.
// This ensures scheduled tasks fire for desktop-only users.
func (a *App) buildLocalScheduledTaskExecutor() scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
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

		// Check if we were cancelled by the scheduler timeout.
		if ctx.Err() != nil {
			return resp.Text, ctx.Err()
		}

		// Notify completion.
		resultText := resp.Text
		hasError := resp.Error != ""

		notifSummary := resultText
		if hasError && notifSummary == "" {
			notifSummary = resp.Error
		}
		if notifSummary != "" {
			if FlashAndBeep != nil {
				FlashAndBeep()
			}
			notifTitle := "定时任务完成"
			if hasError {
				notifTitle = "定时任务失败"
			}
			if ShowNotification != nil {
				ShowNotification(
					notifTitle,
					fmt.Sprintf("%s: %s", task.Name, scheduler.TruncateStr(notifSummary, 200)),
					1,
				)
			}
		}

		if resp.Error != "" {
			return resp.Text, fmt.Errorf("%s", resp.Error)
		}
		return resp.Text, nil
	}
}

// buildHubScheduledTaskExecutor creates a TaskExecutor that runs scheduled
// tasks through the IMMessageHandler AND pushes results to IM channels via Hub.
// This upgrades the local executor when Hub connectivity is available.
func (a *App) buildHubScheduledTaskExecutor(hubClient *RemoteHubClient) scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
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

		if ctx.Err() != nil {
			return resp.Text, ctx.Err()
		}

		// Push the result to the user's IM channels via Hub.
		resultText := resp.Text
		hasError := resp.Error != ""

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

		// Play sound + flash + notification on completion.
		notifSummary := resultText
		if hasError && notifSummary == "" {
			notifSummary = resp.Error
		}
		if notifSummary != "" {
			if FlashAndBeep != nil {
				FlashAndBeep()
			}
			notifTitle := "定时任务完成"
			if hasError {
				notifTitle = "定时任务失败"
			}
			if ShowNotification != nil {
				ShowNotification(
					notifTitle,
					fmt.Sprintf("%s: %s", task.Name, scheduler.TruncateStr(notifSummary, 200)),
					1,
				)
			}
		}

		if resp.Error != "" {
			return resp.Text, fmt.Errorf("%s", resp.Error)
		}
		return resp.Text, nil
	}
}

// ensureLocalIMHandler returns the IMMessageHandler, creating it if needed.
// This is a lazy accessor — the handler is created on first use, not at
// scheduler startup time. This handles the case where a catch-up task fires
// before Hub infrastructure is fully initialized.
func (a *App) ensureLocalIMHandler() *IMMessageHandler {
	// ensureInteractionInfra guarantees the hubClient and its IMHandler exist.
	a.ensureInteractionInfra()
	hubClient := a.hubClient()
	if hubClient == nil {
		return nil
	}
	return hubClient.ensureIMHandler()
}
