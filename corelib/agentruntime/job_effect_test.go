package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type recordingJobEffectRecorder struct {
	prepared JobEffectPreparation
	bound    JobEffectBinding
	settled  JobEffectSettlement
}

func (r *recordingJobEffectRecorder) PrepareJobEffect(_ context.Context, update JobEffectPreparation) (JobEffect, error) {
	r.prepared = update
	return JobEffect{Kind: update.Kind, Payload: update.Payload}, nil
}

func (r *recordingJobEffectRecorder) BindJobEffect(_ context.Context, update JobEffectBinding) (JobEffect, error) {
	r.bound = update
	return JobEffect{Kind: update.Kind, ResourceID: update.ResourceID}, nil
}

func (r *recordingJobEffectRecorder) SettleJobEffect(_ context.Context, update JobEffectSettlement) (JobEffect, error) {
	r.settled = update
	return JobEffect{Kind: update.Kind, State: update.State, ReceiptDigest: update.ReceiptDigest, Payload: update.Payload}, nil
}

func TestJobEffectRecorderContextKeepsRawReceiptOutOfRecord(t *testing.T) {
	recorder := &recordingJobEffectRecorder{}
	ctx := WithJobEffectRecorder(context.Background(), recorder)
	if _, err := PrepareJobEffect(ctx, "migration.export", map[string]string{"phase": "create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindJobEffectResource(ctx, "migration.export", "export-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := SettleJobEffect(ctx, "migration.export", JobEffectCommitted, "private-provider-receipt", "remote_ready", map[string]bool{"ready": true}); err != nil {
		t.Fatal(err)
	}
	if recorder.bound.ResourceID != "export-1" || recorder.settled.ReceiptDigest != JobEffectReceiptDigest("private-provider-receipt") || recorder.settled.ReceiptDigest == "private-provider-receipt" {
		t.Fatalf("unexpected recorded effect: %#v %#v", recorder.bound, recorder.settled)
	}
	if string(recorder.prepared.Payload) != `{"phase":"create"}` || string(recorder.settled.Payload) != `{"ready":true}` {
		t.Fatalf("unexpected protected payloads: %s %s", recorder.prepared.Payload, recorder.settled.Payload)
	}
}

func TestValidateJobEffectAndReconcileResultFailClosed(t *testing.T) {
	now := time.Now().UTC()
	effect := JobEffect{Version: 1, JobID: "job", TenantID: "tenant", UserID: "user", JobKind: "migration.export", Kind: "migration.export", State: JobEffectPrepared, CreatedAt: now, UpdatedAt: now}
	if err := ValidateJobEffect(effect); err != nil {
		t.Fatal(err)
	}
	effect.State = JobEffectCommitted
	if err := ValidateJobEffect(effect); !errors.Is(err, ErrInvalidJobEffect) {
		t.Fatalf("committed effect without receipt accepted: %v", err)
	}
	if err := ValidateJobReconcileResult(JobReconcileResult{Resolved: true, Status: JobStatusRunning}); !errors.Is(err, ErrInvalidJobEffect) {
		t.Fatalf("executable reconciliation accepted: %v", err)
	}
	if err := ValidateJobReconcileResult(JobReconcileResult{Resolved: true, Status: JobStatusSucceeded, Result: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestJobEffectHelpersRequireRecorder(t *testing.T) {
	if _, err := PrepareJobEffect(context.Background(), "migration.export", nil); !errors.Is(err, ErrJobEffectUnavailable) {
		t.Fatalf("missing recorder error = %v", err)
	}
	if _, err := BindJobEffectResource(WithJobEffectRecorder(context.Background(), &recordingJobEffectRecorder{}), "migration.export", " "); !errors.Is(err, ErrInvalidJobEffect) {
		t.Fatalf("empty resource binding error = %v", err)
	}
	if _, err := SettleJobEffect(WithJobEffectRecorder(context.Background(), &recordingJobEffectRecorder{}), "migration.export", JobEffectPrepared, "", "", nil); !errors.Is(err, ErrInvalidJobEffect) {
		t.Fatalf("prepared settlement error = %v", err)
	}
}
