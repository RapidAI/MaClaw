package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// tuiDirectoryTransaction makes an in-place Skill mutation recoverable across
// process restarts. The original directory is moved to a sibling backup,
// copied back as the working tree, and only then mutated. The backup path is
// durable; file contents never need to be embedded in the global queue.
type tuiDirectoryTransaction struct {
	record   skill.EvolutionCompensationRecord
	skillDir string
}

func ensureTUISkillCompensationAdmission(skillName string) error {
	if err := skill.CheckEvolutionCompensationQueue(); err != nil {
		return fmt.Errorf("补偿队列不可读: %w", err)
	}
	if (&skill.EvolutionPipeline{}).HasPendingCompensation(strings.TrimSpace(skillName)) {
		return fmt.Errorf("Skill 存在待恢复补偿")
	}
	return nil
}

func beginTUIDirectoryTransaction(name, skillDir, action, finalAuditKind string) (*tuiDirectoryTransaction, error) {
	action = strings.TrimSpace(action)
	if action == "" || strings.ContainsAny(action, `/\\`) {
		return nil, fmt.Errorf("invalid TUI transaction action")
	}
	if strings.TrimSpace(finalAuditKind) == "" {
		return nil, fmt.Errorf("final audit kind is required")
	}
	skillDir = filepath.Clean(strings.TrimSpace(skillDir))
	if skillDir == "" || skillDir == "." {
		return nil, fmt.Errorf("skill directory is empty")
	}
	info, err := os.Lstat(skillDir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("skill directory must be a real directory")
	}
	rawScope := strings.TrimSpace(commands.ResolveDataDir())
	if rawScope == "" {
		return nil, fmt.Errorf("TUI data directory is empty")
	}
	scope, err := filepath.Abs(rawScope)
	if err != nil {
		return nil, fmt.Errorf("resolve TUI recovery scope: %w", err)
	}
	absSkill, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory: %w", err)
	}
	rel, err := filepath.Rel(filepath.Clean(scope), filepath.Clean(absSkill))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("skill directory is outside TUI data root")
	}
	backupTag := strings.ReplaceAll(action, "_", "-")
	if strings.HasPrefix(backupTag, "tui-validate-") {
		backupTag = "tui-validate"
	}
	backup := filepath.Join(filepath.Dir(absSkill), fmt.Sprintf(".%s-prev-%d", backupTag, time.Now().UnixNano()))
	if _, statErr := os.Lstat(backup); statErr == nil {
		return nil, fmt.Errorf("TUI transaction backup already exists")
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("inspect validation backup: %w", statErr)
	}
	requestID := fmt.Sprintf("evo_%s_%d", action, time.Now().UnixNano())
	record := skill.NewEvolutionCompensationRecord(requestID, name, action, "", nil, false, nil, action+"_pending")
	record.SetRecoveryScope(scope)
	record.SetDirectoryBackupIntent(absSkill, backup)
	record.SetRollbackCleanupPaths([]string{backup})
	record.SetPostCommitCleanupPaths([]string{backup})
	record.FinalAuditKind = strings.TrimSpace(finalAuditKind)
	record.TransactionState = "prepared"
	record.CleanupStatus = "pending"
	if err := skill.PersistEvolutionCompensation(record); err != nil {
		return nil, fmt.Errorf("persist TUI transaction compensation: %w", err)
	}
	tx := &tuiDirectoryTransaction{record: record, skillDir: absSkill}
	if err := os.Rename(absSkill, backup); err != nil {
		return tx, fmt.Errorf("stage TUI transaction directory: %w", err)
	}
	record.SetDirectoryBackup(absSkill, backup, true)
	if err := skill.ReplaceEvolutionCompensation(record); err != nil {
		return tx, fmt.Errorf("persist staged TUI directory: %w", err)
	}
	tx.record = record
	if err := copySkillDirContentsTUI(backup, absSkill); err != nil {
		return tx, fmt.Errorf("recreate TUI working directory: %w", err)
	}
	record.SetDirectoryPublished(true)
	if err := skill.ReplaceEvolutionCompensation(record); err != nil {
		return tx, fmt.Errorf("persist TUI publication: %w", err)
	}
	tx.record = record
	return tx, nil
}

func beginTUIValidationTransaction(name, skillDir string) (*tuiDirectoryTransaction, error) {
	return beginTUIDirectoryTransaction(name, skillDir, "tui_validate_auto_fix", skill.KindFromEventName("skill:tui_skill_validated"))
}

// rollback restores the durable pre-image and removes the queue row only when
// the full rollback completed.  A failure keeps the record for bounded retry
// and needs_review escalation by the shared recovery layer.
func (tx *tuiDirectoryTransaction) rollback(cause error) error {
	if tx == nil {
		return cause
	}
	if err := skill.RestoreEvolutionCompensation(tx.record, nil, nil); err != nil {
		_ = skill.MarkEvolutionCompensationRollbackFailure(&tx.record, err)
		return fmt.Errorf("%v; rollback failed: %w", cause, err)
	}
	if err := skill.ClearEvolutionCompensation(tx.record.RequestID, tx.record.Skill, tx.record.Action); err != nil {
		_ = skill.MarkEvolutionCompensationRollbackFailure(&tx.record, err)
		return fmt.Errorf("%v; clear rollback compensation failed: %w", cause, err)
	}
	return cause
}

// markExternalSubmission records the irreversible SkillMarket acceptance
// before any local receipt/audit write. Startup recovery treats a row carrying
// this marker as externally committed and therefore leaves the local package
// untouched when no external compensator is available.
func (tx *tuiDirectoryTransaction) markExternalSubmission(submissionID string) error {
	if tx == nil {
		return fmt.Errorf("TUI transaction is nil")
	}
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return fmt.Errorf("remote submission id is empty")
	}
	tx.record.SetExternalSnapshot("skillmarket_submission", submissionID)
	tx.record.MarkExternalApplied("skillmarket_submission", true)
	tx.record.FailureReason = "remote_submission_accepted"
	if err := skill.ReplaceEvolutionCompensation(tx.record); err != nil {
		// Queue replacement is the normal path. An append fallback still leaves a
		// latest deduplicated row containing the external marker, which is safer
		// than allowing restart recovery to infer that the remote side is absent.
		if appendErr := skill.PersistEvolutionCompensation(tx.record); appendErr != nil {
			return fmt.Errorf("persist remote submission marker: %v; append fallback: %w", err, appendErr)
		}
	}
	return nil
}

// commit marks the validation as committed before deleting the durable backup.
// Cleanup and queue removal are independent; either failure remains a
// post-commit blocker instead of rolling back an audited result.
func (tx *tuiDirectoryTransaction) commit() error {
	if tx == nil {
		return fmt.Errorf("TUI transaction is nil")
	}
	tx.record.TransactionState = "committed"
	tx.record.CleanupStatus = "pending"
	if err := skill.ReplaceEvolutionCompensation(tx.record); err != nil {
		return fmt.Errorf("persist committed TUI transaction: %w", err)
	}
	if err := skill.CleanupCommittedEvolutionCompensation(tx.record); err != nil {
		_ = skill.MarkEvolutionCompensationCleanupFailure(&tx.record, err)
		return fmt.Errorf("TUI transaction cleanup pending: %w", err)
	}
	if err := skill.ClearEvolutionCompensation(tx.record.RequestID, tx.record.Skill, tx.record.Action); err != nil {
		_ = skill.MarkEvolutionCompensationCleanupFailure(&tx.record, err)
		return fmt.Errorf("TUI transaction queue cleanup pending: %w", err)
	}
	return nil
}
