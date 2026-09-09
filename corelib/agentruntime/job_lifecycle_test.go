package agentruntime

import (
	"errors"
	"testing"
	"time"
)

func TestBeginJobAttemptAndScheduleRetry(t *testing.T) {
	now := time.Now().UTC()
	job := Job{ID: "job-1", Status: JobStatusPending, RetryPolicy: JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 10, MaximumBackoffMillis: 100}}
	started, ok := BeginJobAttempt(job, now)
	if !ok || started.Status != JobStatusRunning || started.Attempt != 1 || started.StartedAt == nil {
		t.Fatalf("begin=%#v ok=%t", started, ok)
	}
	parked := ScheduleJobRetry(started, time.Second, "temp", "boom", now)
	if parked.Status != JobStatusPending || parked.NextAttemptAt == nil || parked.LastAttemptErrorCode != "temp" {
		t.Fatalf("retry=%#v", parked)
	}
	exhausted := Job{ID: "job-1", Status: JobStatusRunning, Attempt: 1, RetryPolicy: JobRetryPolicy{MaxAttempts: 1}}
	if _, ok := BeginJobAttempt(exhausted, now); ok {
		t.Fatal("exhausted retry budget must not start")
	}
}

func TestApplyJobWorkerOutcomeAndPersistFailure(t *testing.T) {
	now := time.Now().UTC()
	base := Job{ID: "job-1", Status: JobStatusRunning, LeaseOwnerID: "owner"}
	succeeded := ApplyJobWorkerOutcome(base, now, JobWorkerOutcome{Result: []byte(`{"ok":true}`)})
	if succeeded.Status != JobStatusSucceeded || string(succeeded.Result) != `{"ok":true}` || succeeded.CompletedAt == nil {
		t.Fatalf("success=%#v", succeeded)
	}
	canceled := ApplyJobWorkerOutcome(base, now, JobWorkerOutcome{Canceled: true})
	if canceled.Status != JobStatusCanceled || canceled.ErrorCode != JobErrorCodeCanceled || canceled.Error != "job canceled" {
		t.Fatalf("canceled=%#v", canceled)
	}
	shutdown := StampJobCanceled(base, now, "job canceled during service shutdown")
	if shutdown.Status != JobStatusCanceled || shutdown.Error != "job canceled during service shutdown" {
		t.Fatalf("shutdown cancel=%#v", shutdown)
	}
	uncertain := ApplyJobWorkerOutcome(base, now, JobWorkerOutcome{Uncertain: true, Err: errors.New("maybe")})
	if uncertain.Status != JobStatusUnknown || uncertain.ErrorCode != JobErrorCodeReconcileRequired || uncertain.CompletedAt != nil {
		t.Fatalf("uncertain=%#v", uncertain)
	}
	failed := ApplyJobWorkerOutcome(base, now, JobWorkerOutcome{Err: errors.New("boom"), ErrorCode: "x", ErrorText: "boom"})
	if failed.Status != JobStatusFailed || failed.ErrorCode != "x" {
		t.Fatalf("failed=%#v", failed)
	}
	persist := MarkJobPersistenceFailed(base, now)
	if persist.Status != JobStatusFailed || persist.ErrorCode != JobErrorCodePersistenceFailed || persist.LeaseExpiresAt != nil {
		t.Fatalf("persist=%#v", persist)
	}
	afterEffect := MarkJobCompletionPersistFailed(base, now, true)
	if afterEffect.Status != JobStatusUnknown || afterEffect.ErrorCode != JobErrorCodeReconcileRequired {
		t.Fatalf("after effect=%#v", afterEffect)
	}
	running := StampHostJobStatus(Job{ID: "host-1"}, JobStatusRunning, now, JobWorkerOutcome{})
	if running.Status != JobStatusRunning || running.Attempt != 1 || running.StartedAt == nil {
		t.Fatalf("host running=%#v", running)
	}
	again := StampHostJobStatus(running, JobStatusRunning, now, JobWorkerOutcome{})
	if again.Attempt != 1 {
		t.Fatalf("repeat running must not consume retry budget: %#v", again)
	}
	done := StampHostJobStatus(running, JobStatusSucceeded, now, JobWorkerOutcome{Result: []byte(`{"ok":true}`)})
	if done.Status != JobStatusSucceeded || string(done.Result) != `{"ok":true}` {
		t.Fatalf("host success=%#v", done)
	}
	hostFailed := StampHostJobStatus(running, JobStatusFailed, now, JobWorkerOutcome{ErrorCode: "skill_run_failed", ErrorText: "boom"})
	if hostFailed.Status != JobStatusFailed || hostFailed.ErrorCode != "skill_run_failed" || hostFailed.Error != "boom" {
		t.Fatalf("host fail=%#v", hostFailed)
	}
}
