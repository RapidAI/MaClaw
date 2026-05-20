package memory

import (
	"context"
	"fmt"
	"time"
)

// DeleteEntryForHost removes a host-visible memory entry by ID through the
// shared corelib delete path.
func (s *Store) DeleteEntryForHost(id string) error {
	if s == nil {
		return nil
	}
	return s.Delete(id)
}

// MemoryCandidatesForHost returns consolidation candidates for host inspection
// commands while keeping candidate selection in corelib/memory.
func (s *Store) MemoryCandidatesForHost(ctx context.Context, keyword string, limit int, apply bool) ToolCandidatesResult {
	if s == nil {
		return ToolCandidatesResult{}
	}
	return s.MemoryCandidatesForTool(ctx, keyword, limit, apply)
}

// MemoryThemesForHost returns theme diagnostics for host inspection commands.
func (s *Store) MemoryThemesForHost(opts ToolThemesOptions) ToolThemesResult {
	if s == nil {
		return ToolThemesResult{}
	}
	return s.MemoryThemesForTool(opts)
}

// EvaluateRecallStrategiesForHost runs recall evaluation for host diagnostics.
func (s *Store) EvaluateRecallStrategiesForHost(cases []RecallEvalCase, limit int) RecallEvalReport {
	if s == nil {
		return RecallEvalReport{}
	}
	return s.EvaluateRecallStrategies(cases, limit)
}

// EvaluateRecallStrategiesWithMaintenanceForHost runs recall evaluation with
// safe maintenance actions through the shared corelib evaluator.
func (s *Store) EvaluateRecallStrategiesWithMaintenanceForHost(cases []RecallEvalCase, limit int, issueLimit int, actionLimit int) RecallMaintenanceEvalReport {
	if s == nil {
		return RecallMaintenanceEvalReport{}
	}
	return s.EvaluateRecallStrategiesWithMaintenance(cases, limit, issueLimit, actionLimit)
}

func (s *Store) EmbedStatusForHost() EmbedStatus {
	if s == nil {
		return EmbedStatus{}
	}
	return s.EmbedStatusForTool()
}

func (s *Store) GraphNeighborsForHost(id string) []GraphNeighborSnapshot {
	if s == nil {
		return nil
	}
	return s.GraphNeighborsForTool(id)
}

func (s *Store) StrengthForHost(now time.Time) []StrengthSnapshot {
	if s == nil {
		return nil
	}
	return s.StrengthForTool(now)
}

func (s *Store) InferForHost(query string, opts InferenceOptions) InferenceResult {
	if s == nil {
		return InferenceResult{}
	}
	return s.InferForTool(query, opts)
}

// CompressForHost runs the shared maintenance compressor for host commands that
// do not keep a long-lived Maintenance instance.
func (s *Store) CompressForHost(ctx context.Context) (*CompressResult, error) {
	if s == nil {
		return nil, nil
	}
	return NewMaintenance(s, nil, nil).Compress(ctx)
}

func (s *Store) ListBackupsForHost() ([]BackupInfo, error) {
	if s == nil {
		return nil, nil
	}
	return NewMaintenance(s, nil, nil).ListBackups()
}

func (s *Store) RestoreBackupForHost(name string) error {
	if s == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return NewMaintenance(s, nil, nil).RestoreBackup(name)
}

func (s *Store) DeleteBackupForHost(name string) error {
	if s == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return NewMaintenance(s, nil, nil).DeleteBackup(name)
}

// HandleToolForHost runs the shared memory tool behavior for host agent tools
// without exposing GUI/TUI code to the raw package-level tool dispatcher.
func (s *Store) HandleToolForHost(args map[string]interface{}, opts ToolOptions) string {
	return HandleTool(s, args, opts)
}
