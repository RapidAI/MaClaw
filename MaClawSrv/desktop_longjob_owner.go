package main

import (
	"context"
	"log"
	"strings"
	"sync"
)

// A background job is owned by the run that started it (SourceRun) and by
// that run's goal. Once the run has concluded, the job's watcher does not
// speak for itself. A later finish updates the run record, and starts one
// follow-up only when the result changes that conclusion.

var desktopRunConclusion sync.Map // source run id -> "success" or "failure"

func desktopLongJobNoteConclusion(runID, status string) {
	runID = strings.TrimSpace(runID)
	if runID == "" || (status != "success" && status != "failure") {
		return
	}
	desktopRunConclusion.Store(runID, status)
}

func desktopLongJobOwnerConclusion(j *desktopLongJob) (string, bool) {
	if j == nil || strings.TrimSpace(j.SourceRun) == "" {
		return "", false
	}
	v, ok := desktopRunConclusion.Load(j.SourceRun)
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, s == "success" || s == "failure"
}

// desktopLongJobMaterialChange is a finished job disagreeing with the
// conclusion the owner already gave the person. Same outcome is not news.
func desktopLongJobMaterialChange(j *desktopLongJob, conclusion string) bool {
	if j == nil || !desktopLongJobTerminal(j) {
		return false
	}
	succeeded := j.Status == "done" && (j.ExitCode == nil || *j.ExitCode == 0)
	switch conclusion {
	case "success":
		return !succeeded
	case "failure":
		return succeeded
	default:
		return false
	}
}

// desktopLongJobFinishOwned handles a job that just finished. The person
// does not get a notice from the watcher. The owning run's conclusion
// decides between a record update and one follow-up.
func desktopLongJobFinishOwned(ctx context.Context, j *desktopLongJob) error {
	if conclusion, ok := desktopLongJobOwnerConclusion(j); ok {
		desktopLongJobWriteCompletion(j)
		desktopLongJobStopWatch(j)
		if !desktopLongJobMaterialChange(j, conclusion) {
			j.OutcomeState = "record"
			_ = desktopLongJobSave(j)
			log.Printf("[desktop-longjob] job=%s owner %s already concluded %s; record only", j.ID, j.SourceRun, conclusion)
			return nil
		}
		log.Printf("[desktop-longjob] job=%s changes the conclusion %s; one follow-up", j.ID, conclusion)
		j.OutcomeState = ""
		_ = desktopLongJobSave(j)
		return desktopLongJobScheduleOutcome(ctx, j)
	}
	return desktopLongJobScheduleOutcome(ctx, j)
}

// desktopLongJobReconcileRun runs when its owner concludes. Extra open jobs
// are cancelled. Finished ones are folded into the record and stop watching.
// The newest open job stays, but it will not post on its own later.
func desktopLongJobReconcileRun(runID, conclusion string) {
	desktopLongJobNoteConclusion(runID, conclusion)
	if strings.TrimSpace(runID) == "" {
		return
	}
	var latest *desktopLongJob
	for _, j := range desktopLongJobAll() {
		if j.SourceRun != runID || j.KeepRunning || !desktopLongJobOpen(j) {
			continue
		}
		if latest == nil || j.Started.After(latest.Started) {
			latest = j
		}
	}
	for _, j := range desktopLongJobAll() {
		if j.SourceRun != runID || j.KeepRunning {
			continue
		}
		if desktopLongJobOpen(j) && (latest == nil || j.ID != latest.ID) {
			desktopLongJobAbandon(j, "owner run concluded")
			continue
		}
		if desktopLongJobTerminal(j) && j.OutcomeState != "sent" && j.OutcomeState != "folded" {
			desktopLongJobWriteCompletion(j)
			j.OutcomeState = "record"
			desktopLongJobStopWatch(j)
			_ = desktopLongJobSave(j)
		}
	}
}

// desktopLongJobSupersede retires open jobs of the same goal or the same
// owner when a new one starts. A relaunch must not leave the old watches
// posting. A job declared keep_running is left alone.
func desktopLongJobSupersede(j *desktopLongJob) {
	if j == nil || j.KeepRunning || j.ID == "" {
		return
	}
	goal := desktopLongJobGoalKey(j)
	for _, other := range desktopLongJobAll() {
		if other.ID == j.ID || other.InstanceID != j.InstanceID || other.KeepRunning {
			continue
		}
		if !desktopLongJobOpen(other) && !other.Alive {
			continue
		}
		sameGoal := desktopLongJobGoalKey(other) == goal
		sameOwner := other.SourceRun != "" && other.SourceRun == j.SourceRun
		if !sameGoal && !sameOwner {
			continue
		}
		if desktopLongJobShouldStopProcess(other) {
			desktopLongJobKillProcess(context.Background(), other)
		}
		desktopLongJobAbandon(other, "superseded by "+j.ID)
		log.Printf("[desktop-longjob] job=%s superseded by %s", other.ID, j.ID)
	}
}
