package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrJobReporterUnavailable = errors.New("job reporter is unavailable")
	ErrJobNotActive           = errors.New("job is not active")
	ErrInvalidJobUpdate       = errors.New("invalid job update")
	ErrStaleJobCheckpoint     = errors.New("stale job checkpoint")
)

// JobCheckpoint is deliberately metadata-only. Domain-specific resume data,
// credentials and filesystem paths stay in their owning repository; the
// shared/API job envelope records only an ordered, non-secret phase marker.
type JobCheckpoint struct {
	Sequence  uint64    `json:"sequence"`
	Phase     string    `json:"phase"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JobUpdate allows progress and its checkpoint to be committed in one
// JobStore replacement. ProgressText is applied only when Progress is set.
type JobUpdate struct {
	Progress     *float64
	ProgressText string
	Checkpoint   *JobCheckpoint
}

// JobReporter is injected into a worker context by a host. Runtime/domain
// code can therefore publish durable progress without importing GUI, HTTP or
// a concrete job manager.
type JobReporter interface {
	ReportJobUpdate(context.Context, JobUpdate) error
}

type jobReporterContextKey struct{}

// WithJobReporter scopes a reporter to one job execution. A nil reporter is
// intentionally ignored so callers cannot install a context value that later
// panics on use.
func WithJobReporter(ctx context.Context, reporter JobReporter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, jobReporterContextKey{}, reporter)
}

// ReportJobUpdate validates the transport-neutral update before handing it to
// the host. Checkpoint time is host-owned and must be zero in caller input.
func ReportJobUpdate(ctx context.Context, update JobUpdate) error {
	if err := ValidateJobUpdate(update); err != nil {
		return err
	}
	if ctx == nil {
		return ErrJobReporterUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reporter, _ := ctx.Value(jobReporterContextKey{}).(JobReporter)
	if reporter == nil {
		return ErrJobReporterUnavailable
	}
	return reporter.ReportJobUpdate(ctx, cloneJobUpdate(update))
}

// ReportJobProgress is the common progress-only convenience path.
func ReportJobProgress(ctx context.Context, progress float64, text string) error {
	return ReportJobUpdate(ctx, JobUpdate{Progress: &progress, ProgressText: text})
}

// SaveJobCheckpoint records an ordered phase marker without exposing a
// domain-specific replay payload through the shared Job API.
func SaveJobCheckpoint(ctx context.Context, sequence uint64, phase string) error {
	return ReportJobUpdate(ctx, JobUpdate{Checkpoint: &JobCheckpoint{Sequence: sequence, Phase: phase}})
}

func ValidateJobUpdate(update JobUpdate) error {
	if update.Progress == nil && update.Checkpoint == nil {
		return fmt.Errorf("%w: progress or checkpoint is required", ErrInvalidJobUpdate)
	}
	if update.Progress != nil {
		if math.IsNaN(*update.Progress) || math.IsInf(*update.Progress, 0) || *update.Progress < 0 || *update.Progress > 1 {
			return fmt.Errorf("%w: progress must be between 0 and 1", ErrInvalidJobUpdate)
		}
	}
	if update.Checkpoint != nil {
		if !update.Checkpoint.UpdatedAt.IsZero() {
			return fmt.Errorf("%w: checkpoint time is host-owned", ErrInvalidJobUpdate)
		}
		if err := validateJobCheckpointIdentity(*update.Checkpoint); err != nil {
			return err
		}
	}
	return nil
}

// ValidateJobCheckpoint validates a checkpoint read from durable storage.
func ValidateJobCheckpoint(checkpoint JobCheckpoint) error {
	if err := validateJobCheckpointIdentity(checkpoint); err != nil {
		return err
	}
	if checkpoint.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: checkpoint updated_at is required", ErrInvalidJobUpdate)
	}
	return nil
}

func validateJobCheckpointIdentity(checkpoint JobCheckpoint) error {
	if checkpoint.Sequence == 0 {
		return fmt.Errorf("%w: checkpoint sequence must be positive", ErrInvalidJobUpdate)
	}
	phase := strings.TrimSpace(checkpoint.Phase)
	if phase == "" || len(phase) > 64 {
		return fmt.Errorf("%w: checkpoint phase must contain 1-64 safe characters", ErrInvalidJobUpdate)
	}
	for i := 0; i < len(phase); i++ {
		c := phase[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("%w: checkpoint phase must contain 1-64 safe characters", ErrInvalidJobUpdate)
	}
	return nil
}

func cloneJobUpdate(update JobUpdate) JobUpdate {
	copy := update
	if update.Progress != nil {
		progress := *update.Progress
		copy.Progress = &progress
	}
	if update.Checkpoint != nil {
		checkpoint := *update.Checkpoint
		copy.Checkpoint = &checkpoint
	}
	return copy
}
