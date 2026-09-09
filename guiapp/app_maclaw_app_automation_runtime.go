package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// App automation tasks live in the shared scheduler.Manager next to user
// scheduled tasks. The manager assigns random persisted IDs on Add, so the
// stable, deterministic linkage is an Action marker carrying the app ID. The
// logical task ID exposed to the frontend keeps the appauto-<appID> shape.
const (
	maclawAppAutomationTaskActionPrefix = "appauto://"
	maclawAppAutomationTaskIDPrefix     = "appauto-"
)

// maclawAppAutomationActionMarker encodes the owning app ID into the task
// Action. The automation executor routes on this marker and re-reads the
// install record on every fire, so the Action itself is never interpreted as
// natural language.
func maclawAppAutomationActionMarker(appID string) string {
	return maclawAppAutomationTaskActionPrefix + strings.TrimSpace(appID)
}

// maclawAppAutomationAppIDFromTask extracts the owning app ID from a
// scheduler task, or reports false for ordinary user tasks.
func maclawAppAutomationAppIDFromTask(task *scheduler.ScheduledTask) (string, bool) {
	if task == nil {
		return "", false
	}
	action := strings.TrimSpace(task.Action)
	if !strings.HasPrefix(action, maclawAppAutomationTaskActionPrefix) {
		return "", false
	}
	appID := strings.TrimSpace(strings.TrimPrefix(action, maclawAppAutomationTaskActionPrefix))
	return appID, appID != ""
}

// maclawAppAutomationTaskID is the logical task ID surfaced in status
// payloads; it is not the persisted scheduler ID (see package note above).
func maclawAppAutomationTaskID(appID string) string {
	return maclawAppAutomationTaskIDPrefix + strings.TrimSpace(appID)
}

// maclawAppAutomationBindingForApp loads the installed app record and parses
// its binding.automation block. A nil record means the app is not installed;
// a nil binding (with a non-nil record) means the app has no automation
// configuration yet.
func (a *App) maclawAppAutomationBindingForApp(appID string) (*maclawAppInstallRecord, *maclawAppAutomationBinding, error) {
	record, err := a.findMaclawAppInstallRecord(strings.TrimSpace(appID))
	if err != nil || record == nil {
		return record, nil, err
	}
	appMap := anyMap(record.Package["app"])
	binding, err := parseMaclawAppAutomationBinding(appMap, "app")
	if err != nil {
		return record, nil, fmt.Errorf("installed automation app %q has an invalid binding: %w", appID, err)
	}
	return record, binding, nil
}

// findMaclawAppAutomationTask locates the scheduler task owned by an app.
func findMaclawAppAutomationTask(mgr *scheduler.Manager, appID string) *scheduler.ScheduledTask {
	if mgr == nil {
		return nil
	}
	appID = strings.TrimSpace(appID)
	for _, task := range mgr.List() {
		owner, ok := maclawAppAutomationAppIDFromTask(&task)
		if ok && owner == appID {
			matched := task
			return &matched
		}
	}
	return nil
}

// reconcileMaclawAppAutomationTasks aligns scheduler tasks with the install
// registry: install → Add, definition change → Update, uninstall (or lost
// automation kind/binding) → Delete. User tasks are never touched.
func (a *App) reconcileMaclawAppAutomationTasks(mgr *scheduler.Manager) {
	if mgr == nil {
		return
	}
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
		log.Printf("[app-automation] reconcile: read install registry: %v", err)
		return
	}
	type desiredAutomation struct {
		appName string
		binding *maclawAppAutomationBinding
	}
	desired := map[string]desiredAutomation{}
	for i := range registry.Installs {
		record := &registry.Installs[i]
		if normalizeMaclawAppKind(record.Kind) != "automation_app" {
			continue
		}
		appID := strings.TrimSpace(record.AppID)
		if appID == "" {
			continue
		}
		binding, err := parseMaclawAppAutomationBinding(anyMap(record.Package["app"]), "app")
		if err != nil {
			log.Printf("[app-automation] reconcile: skip app %q: %v", appID, err)
			continue
		}
		if binding == nil {
			continue
		}
		desired[appID] = desiredAutomation{appName: record.AppName, binding: binding}
	}
	// Drop tasks whose app is gone or no longer configured for automation.
	for _, task := range mgr.List() {
		owner, ok := maclawAppAutomationAppIDFromTask(&task)
		if !ok {
			continue
		}
		if _, keep := desired[owner]; keep {
			continue
		}
		if err := mgr.Delete(task.ID); err != nil {
			log.Printf("[app-automation] reconcile: delete stale task %s (app %q): %v", task.ID, owner, err)
		}
	}
	// Create or update tasks for configured automation apps.
	for appID, want := range desired {
		existing := findMaclawAppAutomationTask(mgr, appID)
		if existing == nil {
			if _, err := mgr.Add(want.binding.scheduledTaskSpec(appID, want.appName)); err != nil {
				log.Printf("[app-automation] reconcile: add task for app %q: %v", appID, err)
			}
			continue
		}
		if want.binding.matchesTask(existing, appID, want.appName) {
			continue
		}
		if err := mgr.Update(existing.ID, want.binding.updateArgs(appID, want.appName)); err != nil {
			log.Printf("[app-automation] reconcile: update task %s for app %q: %v", existing.ID, appID, err)
		}
	}
}

// syncMaclawAppAutomationTasks lazily initializes the shared scheduler and
// reconciles app automation tasks against the install registry.
func (a *App) syncMaclawAppAutomationTasks() {
	a.ensureScheduledTaskManager()
	a.scheduledTaskManagerMu.Lock()
	mgr := a.scheduledTaskManager
	a.scheduledTaskManagerMu.Unlock()
	a.reconcileMaclawAppAutomationTasks(mgr)
}

// ensureMaclawAppAutomationTask reconciles and returns the scheduler task for
// an automation app, with an actionable error when the app is not installed
// or has no binding.automation configured.
func (a *App) ensureMaclawAppAutomationTask(appID string) (*scheduler.Manager, *scheduler.ScheduledTask, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, nil, fmt.Errorf("app id is required")
	}
	a.syncMaclawAppAutomationTasks()
	a.scheduledTaskManagerMu.Lock()
	mgr := a.scheduledTaskManager
	a.scheduledTaskManagerMu.Unlock()
	if mgr == nil {
		return nil, nil, fmt.Errorf("scheduled task manager not initialized")
	}
	if task := findMaclawAppAutomationTask(mgr, appID); task != nil {
		return mgr, task, nil
	}
	record, binding, err := a.maclawAppAutomationBindingForApp(appID)
	if err != nil {
		return nil, nil, err
	}
	if record == nil {
		return nil, nil, fmt.Errorf("automation app %q is not installed", appID)
	}
	if binding == nil {
		return nil, nil, fmt.Errorf("automation app %q has no binding.automation schedule configured", appID)
	}
	return nil, nil, fmt.Errorf("automation app %q task could not be scheduled (check the schedule dates)", appID)
}

// maclawAppAutomationStatus builds the status payload for the automation
// console panel.
func (a *App) maclawAppAutomationStatus(appID string) (map[string]any, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("app id is required")
	}
	a.syncMaclawAppAutomationTasks()
	status := map[string]any{
		"app_id":     appID,
		"task_id":    maclawAppAutomationTaskID(appID),
		"configured": false,
		"status":     "unconfigured",
	}
	record, binding, err := a.maclawAppAutomationBindingForApp(appID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		status["status"] = "not_installed"
		status["message"] = "app is not installed"
		return status, nil
	}
	if binding == nil {
		status["message"] = "no binding.automation schedule configured"
		return status, nil
	}
	status["configured"] = true
	status["skill"] = binding.Skill
	status["schedule_summary"] = binding.scheduleSummary()
	a.scheduledTaskManagerMu.Lock()
	mgr := a.scheduledTaskManager
	a.scheduledTaskManagerMu.Unlock()
	task := findMaclawAppAutomationTask(mgr, appID)
	if task == nil {
		status["status"] = "error"
		status["message"] = "automation task is not scheduled"
		return status, nil
	}
	status["status"] = task.Status
	status["run_count"] = task.RunCount
	if task.LastResult != "" {
		status["last_result"] = task.LastResult
	}
	if task.LastError != "" {
		status["last_error"] = task.LastError
	}
	if task.NextRunAt != nil {
		status["next_run"] = task.NextRunAt.Format(time.RFC3339)
	}
	if task.LastRunAt != nil {
		status["last_run"] = task.LastRunAt.Format(time.RFC3339)
	}
	return status, nil
}

// executeMaclawAppAutomationTask is the scheduler executor bridge for
// app-owned automation tasks: the app never does work itself; each fire runs
// the bound skill through the async skill runner and waits for the terminal
// status, respecting the scheduler execution context for timeout/cancel.
func (a *App) executeMaclawAppAutomationTask(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
	appID, ok := maclawAppAutomationAppIDFromTask(task)
	if !ok {
		return "", fmt.Errorf("automation task %q carries no app linkage", task.ID)
	}
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(scheduledTaskExecutorOwnerID, "delegate_task", map[string]interface{}{"agent": "scheduled_task", "request": task.Action, "task_id": task.ID, "task_name": task.Name, "app_id": appID}); err != nil {
		return "", err
	}
	if ShowNotification != nil {
		ShowNotification(
			"自动化任务开始",
			fmt.Sprintf("%s", task.Name),
			1, // info icon
		)
	}
	record, binding, err := a.maclawAppAutomationBindingForApp(appID)
	if err != nil {
		return "", err
	}
	if record == nil {
		return "", fmt.Errorf("automation app %q is not installed", appID)
	}
	if binding == nil {
		return "", fmt.Errorf("automation app %q has no binding.automation schedule configured", appID)
	}
	runArgs := map[string]interface{}{}
	for key, value := range binding.Params {
		runArgs[key] = value
	}
	runArgs["_maclaw_app"] = true
	runArgs["app_id"] = appID
	runArgs["app_name"] = record.AppName
	runArgs["app_kind"] = "automation_app"
	runArgs["automation_task_id"] = task.ID
	if binding.OutputDir != "" {
		runArgs["output_dir"] = binding.OutputDir
	}
	runID, err := a.RunNLSkillAsync(binding.Skill, runArgs)
	if err != nil {
		return "", err
	}
	resultText, runErr := a.waitMaclawAppAutomationSkillRun(ctx, runID)
	runErr = scheduler.AnnotateRunErrWithContext(ctx, runErr)
	showScheduledTaskCompletionNotification(task.Name, resultText, "", runErr != nil, runErr)
	if runErr != nil {
		return resultText, runErr
	}
	return resultText, nil
}

// waitMaclawAppAutomationSkillRun polls an async skill run until it reaches a
// terminal state or the scheduler execution context is done (in which case
// the run is cancelled best-effort).
func (a *App) waitMaclawAppAutomationSkillRun(ctx context.Context, runID string) (string, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		a.ensureSkillRunner()
		if a.skillRunner == nil {
			return "", fmt.Errorf("skill runner not initialized")
		}
		status, err := a.skillRunner.GetRunStatus(runID)
		if err != nil {
			return "", err
		}
		switch status.Status.Normalized() {
		case skillRunStatusSuccess:
			return maclawAppAutomationRunResultText(status), nil
		case skillRunStatusFailed:
			message := firstNonEmptyMaclawAppString(status.Error, status.Summary.LastErrorSnippet, "skill run failed")
			return status.Summary.LastOutputSnippet, fmt.Errorf("%s", message)
		case skillRunStatusCancelled:
			return status.Summary.LastOutputSnippet, fmt.Errorf("skill run cancelled")
		}
		select {
		case <-ctx.Done():
			_ = a.skillRunner.CancelRun(runID)
			return "", fmt.Errorf("automation run interrupted: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

// maclawAppAutomationRunResultText renders a compact success summary for the
// task LastResult field.
func maclawAppAutomationRunResultText(status *SkillRunStatus) string {
	if status == nil {
		return ""
	}
	if snippet := strings.TrimSpace(status.Summary.LastOutputSnippet); snippet != "" {
		return snippet
	}
	if names := make([]string, 0, len(status.Artifacts)); len(status.Artifacts) > 0 {
		for _, artifact := range status.Artifacts {
			if name := firstNonEmptyMaclawAppString(artifact.Name, artifact.Path); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			return "artifacts: " + strings.Join(names, ", ")
		}
	}
	return "skill run completed"
}
