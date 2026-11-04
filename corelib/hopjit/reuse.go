package hopjit

import (
	"context"
	"errors"
	"fmt"
)

type ReuseCaller interface {
	ExecuteStep(context.Context, StepReady) (map[string]any, error)
	Replan(context.Context, EngineSnapshot) (string, error)
}

type ReuseResult struct {
	RunID    string         `json:"run_id"`
	Status   NextStatus     `json:"status"`
	Outputs  map[string]any `json:"outputs,omitempty"`
	Pause    *PauseState    `json:"pause,omitempty"`
	Failure  *Failure       `json:"failure,omitempty"`
	Snapshot EngineSnapshot `json:"snapshot"`
}

func DriveReuse(ctx context.Context, engine *ExecutionEngine, caller ReuseCaller) (ReuseResult, error) {
	if engine == nil || caller == nil {
		return ReuseResult{}, errors.New("hopjit reuse driver requires engine and caller")
	}
	engine.caller = caller
	for iteration := 0; iteration < 10000; iteration++ {
		if err := ctx.Err(); err != nil {
			return ReuseResult{}, err
		}
		next, err := engine.Next(ctx)
		if err != nil {
			return ReuseResult{}, err
		}
		switch next.Status {
		case NextStepReady, NextRunning:
			if next.Step == nil {
				if next.Status == NextRunning && len(next.Snapshot.Inflight) > 0 {
					if err := engine.waitForInflight(ctx); err != nil {
						return ReuseResult{}, err
					}
					continue
				}
				return ReuseResult{}, errors.New("hopjit step_ready missing step")
			}
			outputs, callErr := caller.ExecuteStep(ctx, *next.Step)
			if callErr == nil {
				callErr = engine.Complete(ctx, next.Step.StepID, outputs)
			}
			if callErr != nil {
				if err := engine.Fail(ctx, next.Step.StepID, Failure{Kind: FailureError, Reason: callErr.Error()}); err != nil {
					return ReuseResult{}, err
				}
			}
		case NextAdaptiveNeeded:
			steps, err := caller.Replan(ctx, next.Snapshot)
			if err != nil {
				return ReuseResult{}, err
			}
			if err := engine.Replan(ctx, next.Snapshot.AdaptiveNeeded, steps); err != nil {
				return ReuseResult{}, err
			}
		case NextPaused, NextCompleted, NextFailed, NextAborted:
			return reuseResultFromNext(next), nil
		default:
			return ReuseResult{}, fmt.Errorf("unknown hopjit status %s", next.Status)
		}
	}
	return ReuseResult{}, errors.New("hopjit reuse iteration limit exceeded")
}

func reuseResultFromNext(next NextResult) ReuseResult {
	return ReuseResult{
		RunID: next.RunID, Status: next.Status, Outputs: next.Outputs,
		Pause: next.Pause, Failure: next.Failure, Snapshot: next.Snapshot,
	}
}
