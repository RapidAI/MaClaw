package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// initScheduler creates and starts the scheduled task manager if enabled via
// MACLAW_ENABLE_SCHEDULER=true. The executor sends task actions through the
// agentservice.Service.PostMessage path, using a system principal.
//
// Returns nil if scheduler is disabled or initialization fails.
func initScheduler(dataRoot string, svc *agentservice.Service, executor *agentservice.CoreAgentExecutor) *scheduler.Manager {
	enabled, _ := getenvBoolStrict("MACLAW_ENABLE_SCHEDULER", false)
	if !enabled {
		return nil
	}

	schPath := filepath.Join(dataRoot, "scheduled_tasks.json")
	mgr, err := scheduler.NewManager(schPath)
	if err != nil {
		log.Printf("[MaClawSrv] WARNING: scheduler init failed: %v", err)
		return nil
	}

	// Expose manage_schedule to agents (list_targets / create with group_name resolve).
	if executor != nil {
		executor.ScheduleHandler = newSrvManageScheduleHandler(svc, mgr)
	}

	setSrvSchedulerManager(mgr)
	taskExecutor := buildSrvScheduledTaskExecutor(svc, executor)
	mgr.StartWithExecutor(taskExecutor)
	log.Printf("[MaClawSrv] scheduler started (data: %s)", schPath)
	return mgr
}

// buildSrvScheduledTaskExecutor creates a TaskExecutor for MaClawSrv.
// It uses the agentservice's message execution path to run scheduled tasks.
func buildSrvScheduledTaskExecutor(svc *agentservice.Service, executor *agentservice.CoreAgentExecutor) scheduler.TaskExecutor {
	// Cache the scheduler instance ID to avoid ListInstances on every tick.
	// Protected by mu since fireByID runs executors in concurrent goroutines.
	var (
		mu               sync.Mutex
		cachedInstanceID string
	)

	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		log.Printf("[MaClawSrv-Scheduler] firing task %s: %s", task.Name, scheduler.TruncateStr(task.Action, 100))

		actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)

		// Determine the tenant/user for execution.
		tenantID := executor.LocalBashTenantID
		userID := executor.LocalBashUserID
		if tenantID == "" {
			tenantID = "system"
		}
		if userID == "" {
			userID = "scheduler"
		}

		principal := agentservice.Principal{
			TenantID: tenantID,
			UserID:   userID,
		}

		// Resolve the scheduler instance (cached after first lookup).
		mu.Lock()
		instanceID := cachedInstanceID
		mu.Unlock()

		if instanceID == "" {
			instances, _ := svc.ListInstances(ctx, principal)
			for _, inst := range instances {
				if inst.Metadata != nil && inst.Metadata["purpose"] == "scheduler" {
					instanceID = inst.ID
					break
				}
			}
			if instanceID == "" {
				inst, err := svc.CreateInstance(ctx, principal, agentservice.CreateInstanceInput{
					Name: "Scheduled Tasks",
					Metadata: map[string]string{
						"purpose": "scheduler",
					},
				})
				if err != nil {
					return "", fmt.Errorf("create scheduler instance: %w", err)
				}
				instanceID = inst.ID
			}
			mu.Lock()
			cachedInstanceID = instanceID
			mu.Unlock()
		}

		// Post the message to the instance.
		run, msg, err := svc.PostMessage(ctx, principal, instanceID, "", agentservice.PostMessageInput{
			Content: actionText,
		})
		if err != nil {
			// If the cached instance was deleted, clear cache and retry once.
			mu.Lock()
			cachedInstanceID = ""
			mu.Unlock()
			// Still attempt delivery of any partial text when the run aborted mid-flight.
			runErr := scheduler.AnnotateRunErrWithContext(ctx, fmt.Errorf("post scheduled task message: %w", err))
			if delErr := deliverScheduledTaskResult(svc, principal, task, "", runErr); delErr != nil {
				return scheduler.MergeDeliveryOutcome(task.Delivery, "", runErr, delErr)
			}
			return "", runErr
		}

		result := ""
		if msg != nil && msg.Content != "" {
			result = msg.Content
		}

		runID := ""
		if run != nil {
			runID = run.ID
		}
		log.Printf("[MaClawSrv-Scheduler] task %s completed (run=%s)", task.Name, runID)

		// Fold timeout/cancel so partial agent text can still be pushed.
		runErr := scheduler.AnnotateRunErrWithContext(ctx, nil)

		// Structured channel delivery (lansenger / weixin / telegram / qq).
		if delErr := deliverScheduledTaskResult(svc, principal, task, result, runErr); delErr != nil {
			log.Printf("[MaClawSrv-Scheduler] delivery failed: %v", delErr)
			return scheduler.MergeDeliveryOutcome(task.Delivery, result, runErr, delErr)
		}
		if runErr != nil {
			return result, runErr
		}
		return result, nil
	}
}
