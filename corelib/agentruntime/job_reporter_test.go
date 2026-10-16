package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type captureJobReporter struct {
	update JobUpdate
}

func (r *captureJobReporter) ReportJobUpdate(_ context.Context, update JobUpdate) error {
	r.update = cloneJobUpdate(update)
	return nil
}

func TestReportJobUpdateUsesContextReporter(t *testing.T) {
	reporter := &captureJobReporter{}
	progress := 0.5
	ctx := WithJobReporter(context.Background(), reporter)
	err := ReportJobUpdate(ctx, JobUpdate{
		Progress: &progress, ProgressText: "halfway",
		Checkpoint: &JobCheckpoint{Sequence: 2, Phase: "transfer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reporter.update.Progress == nil || *reporter.update.Progress != progress || reporter.update.ProgressText != "halfway" {
		t.Fatalf("unexpected progress update: %#v", reporter.update)
	}
	if reporter.update.Checkpoint == nil || reporter.update.Checkpoint.Sequence != 2 || reporter.update.Checkpoint.Phase != "transfer" {
		t.Fatalf("unexpected checkpoint update: %#v", reporter.update)
	}
}

func TestReportJobUpdateFailsClosedWithoutReporter(t *testing.T) {
	if err := ReportJobProgress(context.Background(), 0.5, "halfway"); !errors.Is(err, ErrJobReporterUnavailable) {
		t.Fatalf("missing reporter error = %v", err)
	}
}

func TestValidateJobUpdateRejectsUnsafeOrInvalidValues(t *testing.T) {
	invalidProgress := 2.0
	cases := []JobUpdate{
		{},
		{Progress: &invalidProgress},
		{Checkpoint: &JobCheckpoint{Sequence: 0, Phase: "prepare"}},
		{Checkpoint: &JobCheckpoint{Sequence: 1, Phase: "contains secret/path"}},
		{Checkpoint: &JobCheckpoint{Sequence: 1, Phase: "prepare", UpdatedAt: time.Now()}},
	}
	for _, update := range cases {
		if err := ValidateJobUpdate(update); !errors.Is(err, ErrInvalidJobUpdate) {
			t.Fatalf("ValidateJobUpdate(%#v) = %v", update, err)
		}
	}
}

func TestValidatePersistedJobCheckpointRequiresHostTime(t *testing.T) {
	checkpoint := JobCheckpoint{Sequence: 1, Phase: "prepare"}
	if err := ValidateJobCheckpoint(checkpoint); !errors.Is(err, ErrInvalidJobUpdate) {
		t.Fatalf("missing updated_at error = %v", err)
	}
	checkpoint.UpdatedAt = time.Now().UTC()
	if err := ValidateJobCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
}
