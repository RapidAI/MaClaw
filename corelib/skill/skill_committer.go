package skill

// SkillCommitter is the shared durable transaction boundary for Skill
// definition changes.  It deliberately knows nothing about GUI state or
// routing policy: callers provide the authoritative config/YAML/index/audit
// callbacks and receive one result that can be used for event and upload
// admission.

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SkillCommitter coordinates the durable parts of a Skill write. Every
// mutation is preceded by a persisted compensation snapshot; successful
// transactions clear that snapshot only after final audit succeeds.
type SkillCommitter struct {
	SkillLoader func() []corelib.NLSkillEntry
	SkillSaver  func([]corelib.NLSkillEntry) error
	// RollbackSkillSaver optionally provides an independent authoritative restore
	// channel when the forward saver is deliberately fault-injected.
	RollbackSkillSaver func([]corelib.NLSkillEntry) error
	// EntryMerger optionally controls how the candidate is merged into the
	// latest authoritative entry. The default preserves the historical
	// evolution merge semantics; lifecycle transitions may supply a merger to
	// copy proof metadata (for example staged verification fields) atomically.
	EntryMerger func(dst, src *corelib.NLSkillEntry)
	// EntriesMutator is used by lifecycle actions that atomically update more
	// than one registry entry (for example retiring a duplicate). When set, it
	// replaces the default single-entry merge before the saver is called.
	EntriesMutator   func([]corelib.NLSkillEntry) ([]corelib.NLSkillEntry, error)
	DefinitionWriter func(*corelib.NLSkillEntry) error
	// DefinitionWriterWithCompensation is the record-aware variant used when
	// the writer creates transaction-specific artifacts (for example a version
	// backup whose exact path is only known after allocation). The updated
	// record is durably replaced before index publication, closing the crash
	// window between artifact creation and the next transaction phase.
	DefinitionWriterWithCompensation func(*corelib.NLSkillEntry, *EvolutionCompensationRecord) error
	// ExternalCommit/ExternalRollback cover filesystem operations that cannot
	// be represented by the config entry alone (directory quarantine/rename,
	// package publication, etc.). ExternalCommit runs after the durable
	// compensation snapshot is written and before config is changed; rollback
	// runs first so YAML paths are restored only after their original directory
	// is available again.
	ExternalCommit func() error
	// ExternalCommitWithCompensation is the record-aware variant for directory
	// publishers. It may enrich the already-persisted snapshot with exact paths
	// crossed by the filesystem mutation; the committer persists that enrichment
	// before proceeding to config/index/audit.
	ExternalCommitWithCompensation func(*EvolutionCompensationRecord) error
	ExternalRollback               func() error
	IndexRefresher                 func() error
	// RollbackIndexRefresher rebuilds the derived index from the restored
	// authoritative snapshot. It must not re-upsert the forward candidate.
	// When nil, IndexRefresher is used for backwards compatibility.
	RollbackIndexRefresher func() error
	FinalAuditor           func(event string, data map[string]string) error
	// CompensationMutator enriches the durable snapshot before any mutation.
	// It must only modify metadata and must not perform filesystem I/O.
	CompensationMutator func(*EvolutionCompensationRecord)
	// PostCommitCleanup runs after final audit and committed-state persistence.
	// Failure preserves the committed version but keeps cleanup_status pending.
	PostCommitCleanup func() error
	ConfigRevision    string
	// AllowCreate permits a transaction to append a new definition when the
	// requested identity is absent. It is used by the explicit operator create
	// path; all repair/optimization callers keep the safer update-only default.
	AllowCreate bool
	// SkipIfUnchanged enables the idempotent no-op fast path for callers whose
	// transaction has no external side effects. Directory publishers must leave
	// this disabled because an unchanged registry entry may still accompany a
	// required filesystem publication.
	SkipIfUnchanged bool
	// SkipDefinitionBackup disables YAML pre-image capture and definition
	// writing for transactions that are explicitly config-only. This keeps
	// metadata-only batch maintenance from manufacturing YAML backups.
	SkipDefinitionBackup bool
}

// Commit executes config → YAML → index → final-audit as one compensating
// transaction. A result is executable/uploadable only when State is
// committed and CleanupStatus is clear.
func (c *SkillCommitter) Commit(ctx context.Context, skillName string, after *corelib.NLSkillEntry, event string, auditData map[string]string) EvolutionCommitResult {
	if c == nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: "persistence_not_configured", CleanupStatus: "clear"}
	}
	result := EvolutionCommitResult{State: "rolled_back", CleanupStatus: "clear", ConfigRevision: strings.TrimSpace(c.ConfigRevision)}
	if c.SkillLoader == nil || c.SkillSaver == nil || after == nil {
		result.FailureReason = "persistence_not_configured"
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		result.FailureReason = evolutionFailureReason(err)
		return result
	}
	requestID, _ := EvolutionRequestMetadata(ctx)
	if strings.TrimSpace(requestID) == "" {
		requestID = newEvolutionRequestID()
	}
	result.RequestID = requestID

	originalSkills := c.SkillLoader()
	rollbackSkills := cloneSkillEntries(originalSkills)
	updatedSkills := cloneSkillEntries(originalSkills)
	if c.EntriesMutator != nil {
		mutated, err := c.EntriesMutator(cloneSkillEntries(originalSkills))
		if err != nil {
			result.FailureReason = "candidate_mutation_failed"
			return result
		}
		updatedSkills = cloneSkillEntries(mutated)
	}
	found := false
	var effectiveAfter *corelib.NLSkillEntry
	for i := range updatedSkills {
		if updatedSkills[i].Name == skillName {
			if c.EntriesMutator != nil {
				// The full-list mutator already produced the authoritative entry.
			} else if c.EntryMerger != nil {
				c.EntryMerger(&updatedSkills[i], after)
			} else {
				mergeEvolvedEntry(&updatedSkills[i], after)
			}
			effectiveAfter = CloneNLSkillEntry(&updatedSkills[i])
			found = true
			break
		}
	}
	if !found {
		// A full-list mutator may intentionally remove the requested entry (for
		// example an audited delete). In that case keep the original entry as
		// the backup/definition context while allowing the mutated list to be
		// persisted without treating the operation as "skill not found".
		if c.EntriesMutator != nil {
			for i := range originalSkills {
				if originalSkills[i].Name == skillName || originalSkills[i].MatchesName(skillName) {
					effectiveAfter = CloneNLSkillEntry(&originalSkills[i])
					found = true
					break
				}
			}
		}
	}
	if !found {
		if !c.AllowCreate {
			result.FailureReason = "skill_not_found"
			return result
		}
		candidate := *after
		if strings.TrimSpace(candidate.Name) == "" {
			candidate.Name = strings.TrimSpace(skillName)
		}
		if strings.TrimSpace(candidate.Status) == "" {
			candidate.Status = "active"
		}
		if strings.TrimSpace(candidate.Source) == "" {
			candidate.Source = "manual"
		}
		updatedSkills = append(updatedSkills, candidate)
		effectiveAfter = CloneNLSkillEntry(&candidate)
	}
	if effectiveAfter == nil {
		effectiveAfter = CloneNLSkillEntry(after)
	}
	if c.AllowCreate && strings.TrimSpace(effectiveAfter.SkillDir) == "" {
		// A create without an on-disk definition is a valid config-only Skill;
		// do not synthesize a YAML path or invoke the default writer.
		effectiveAfter = CloneNLSkillEntry(&updatedSkills[len(updatedSkills)-1])
	}
	// An unchanged candidate is a first-class terminal outcome. Do this check
	// before taking a YAML backup or writing the durable compensation record so
	// repeated installs/maintenance plans cannot manufacture versions, queue
	// rows, index writes, or misleading committed audits.
	if c.SkipIfUnchanged && reflect.DeepEqual(originalSkills, updatedSkills) {
		return EvolutionCommitResult{
			State: "skipped", RequestID: requestID, ConfigRevision: c.ConfigRevision,
			FailureReason: "no_change", RollbackComplete: true, CleanupStatus: "clear",
		}
	}

	var yamlPath string
	var yamlBackup []byte
	var yamlExists bool
	if !c.SkipDefinitionBackup {
		var err error
		yamlPath, yamlBackup, yamlExists, err = evolutionYAMLBackup(effectiveAfter)
		if err != nil {
			result.FailureReason = "yaml_backup_failed"
			return result
		}
	}
	action := "evolution"
	if auditData != nil && strings.TrimSpace(auditData["action"]) != "" {
		action = strings.TrimSpace(auditData["action"])
	}
	record := newEvolutionCompensationRecord(requestID, skillName, action, yamlPath, yamlBackup, yamlExists, rollbackSkills, "skill_commit_pending")
	record.TransactionState = "prepared"
	if strings.TrimSpace(event) != "" {
		record.FinalAuditKind = KindFromEventName(event)
	}
	if c.CompensationMutator != nil {
		c.CompensationMutator(&record)
	}
	if err := PersistEvolutionCompensation(record); err != nil {
		result.FailureReason = "compensation_persist_failed"
		result.CleanupStatus = "pending"
		return result
	}
	result.BackupVersion = requestID
	configCommitAttempted := false
	externalCommitAttempted := false

	rollback := func(cause error) EvolutionCommitResult {
		var rollbackErr error
		if externalCommitAttempted && c.ExternalRollback != nil {
			if err := c.ExternalRollback(); err != nil {
				rollbackErr = fmt.Errorf("restore external resource: %w", err)
			}
		}
		if yamlExists {
			if err := restoreEvolutionYAML(yamlPath, yamlBackup); err != nil {
				if rollbackErr != nil {
					rollbackErr = fmt.Errorf("%v; restore YAML: %w", rollbackErr, err)
				} else {
					rollbackErr = fmt.Errorf("restore YAML: %w", err)
				}
			}
		}
		if configCommitAttempted {
			saver := c.RollbackSkillSaver
			if saver == nil {
				saver = c.SkillSaver
			}
			if err := saver(rollbackSkills); err != nil {
				if rollbackErr != nil {
					rollbackErr = fmt.Errorf("%v; restore config: %w", rollbackErr, err)
				} else {
					rollbackErr = fmt.Errorf("restore config: %w", err)
				}
			}
		}
		rollbackIndex := c.RollbackIndexRefresher
		if rollbackIndex == nil {
			rollbackIndex = c.IndexRefresher
		}
		if rollbackIndex != nil {
			if err := rollbackIndex(); err != nil {
				if rollbackErr != nil {
					rollbackErr = fmt.Errorf("%v; refresh index after rollback: %w", rollbackErr, err)
				} else {
					rollbackErr = fmt.Errorf("refresh index after rollback: %w", err)
				}
			}
		}
		if err := cleanupRollbackCompensation(record); err != nil {
			if rollbackErr != nil {
				rollbackErr = fmt.Errorf("%v; cleanup rollback artifacts: %w", rollbackErr, err)
			} else {
				rollbackErr = err
			}
		}
		if rollbackErr != nil {
			record.LastError = rollbackErr.Error()
			record.FailureReason = evolutionFailureReason(cause) + ":rollback_incomplete"
			record.TransactionState = "audit_pending"
			record.CleanupStatus = "pending"
			_ = replaceEvolutionCompensation(record)
			return EvolutionCommitResult{State: "audit_pending", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: record.FailureReason + ":" + rollbackErr.Error(), RollbackComplete: false, CleanupStatus: "pending"}
		}
		if err := ClearEvolutionCompensation(requestID, skillName, record.Action); err != nil {
			record.LastError = err.Error()
			record.FailureReason = evolutionFailureReason(cause)
			record.CleanupStatus = "pending"
			record.TransactionState = "rolled_back"
			_ = replaceEvolutionCompensation(record)
			return EvolutionCommitResult{State: "rolled_back", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: record.FailureReason, RollbackComplete: true, CleanupStatus: "pending"}
		}
		return EvolutionCommitResult{State: "rolled_back", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: evolutionFailureReason(cause), RollbackComplete: true, CleanupStatus: "clear"}
	}

	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if c.ExternalCommitWithCompensation != nil {
		externalCommitAttempted = true
		if err := c.ExternalCommitWithCompensation(&record); err != nil {
			return rollback(fmt.Errorf("external commit: %w", err))
		}
		// A crash after publication but before this replacement must still leave
		// enough information for recovery to remove the new directory or restore
		// the previous one.
		if err := replaceEvolutionCompensation(record); err != nil {
			return rollback(fmt.Errorf("persist external commit compensation: %w", err))
		}
	} else if c.ExternalCommit != nil {
		externalCommitAttempted = true
		if err := c.ExternalCommit(); err != nil {
			return rollback(fmt.Errorf("external commit: %w", err))
		}
	}
	configCommitAttempted = true
	if err := c.SkillSaver(updatedSkills); err != nil {
		return rollback(fmt.Errorf("save evolved skill: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if yamlExists {
		var writeErr error
		if c.DefinitionWriterWithCompensation != nil {
			writeErr = c.DefinitionWriterWithCompensation(effectiveAfter, &record)
			if writeErr == nil {
				if err := replaceEvolutionCompensation(record); err != nil {
					writeErr = fmt.Errorf("persist definition compensation update: %w", err)
				}
			}
		} else {
			writer := c.DefinitionWriter
			if writer == nil {
				writer = WriteBackOptimizedSteps
			}
			writeErr = writer(effectiveAfter)
		}
		if writeErr != nil {
			return rollback(fmt.Errorf("write evolved skill.yaml: %w", writeErr))
		}
	}
	if c.IndexRefresher != nil {
		if err := c.IndexRefresher(); err != nil {
			return rollback(fmt.Errorf("refresh skill index: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if c.FinalAuditor != nil && strings.TrimSpace(event) != "" {
		if err := c.FinalAuditor(event, auditData); err != nil {
			return rollback(fmt.Errorf("final audit: %w", err))
		}
	}
	// Cross the business-commit boundary durably before attempting queue
	// cleanup. A crash after final audit must never replay rollback against the
	// already-audited definition on restart.
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	record.FailureReason = "post_commit_cleanup_pending"
	if err := replaceEvolutionCompensation(record); err != nil {
		return EvolutionCommitResult{State: "committed", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: "post_commit_state_persist_failed", RollbackComplete: true, CleanupStatus: "pending"}
	}
	if c.PostCommitCleanup != nil {
		if err := c.PostCommitCleanup(); err != nil {
			record.LastError = err.Error()
			record.FailureReason = "post_commit_cleanup_failed"
			record.CleanupStatus = "pending"
			record.TransactionState = "committed"
			_ = replaceEvolutionCompensation(record)
			return EvolutionCommitResult{State: "committed", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: record.FailureReason, RollbackComplete: true, CleanupStatus: "pending"}
		}
	}
	if err := ClearEvolutionCompensation(requestID, skillName, record.Action); err != nil {
		// The business commit is already audited. Keep the queue as an admission
		// blocker and never reverse the audited definition merely because cleanup
		// was unavailable.
		record.LastError = err.Error()
		record.FailureReason = "post_commit_cleanup_failed"
		record.CleanupStatus = "pending"
		record.TransactionState = "committed"
		_ = replaceEvolutionCompensation(record)
		return EvolutionCommitResult{State: "committed", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, FailureReason: record.FailureReason, RollbackComplete: true, CleanupStatus: "pending"}
	}
	return EvolutionCommitResult{State: "committed", RequestID: requestID, BackupVersion: requestID, ConfigRevision: c.ConfigRevision, RollbackComplete: true, CleanupStatus: "clear"}
}
