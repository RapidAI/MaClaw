package codingruntime

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ExperienceProvenance is the minimal durable reference attached to a
// candidate knowledge record. It is intentionally opaque to callers: the
// original prompt, tool payloads, command output, and transcript remain out
// of the knowledge store.
type ExperienceProvenance struct {
	TaskID         string
	AttemptID      string
	EvidenceDigest string
}

// ResolveExperienceProvenance looks up a stable Runtime TaskID and selects
// the newest matching knowledge-eligible attempt that has material execution
// evidence. If a later completed attempt contains only lifecycle facts (for
// example from a legacy adapter), an older matching terminal attempt may still
// provide the durable evidence binding. Cancelled/interrupted tasks are never
// eligible because their side-effect boundary is unresolved.
func ResolveExperienceProvenance(store Store, taskID string) (ExperienceProvenance, error) {
	if store == nil {
		return ExperienceProvenance{}, fmt.Errorf("coding runtime store is unavailable")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ExperienceProvenance{}, fmt.Errorf("coding execution has no durable runtime task ID")
	}
	task, err := store.GetTask(taskID)
	if err != nil {
		return ExperienceProvenance{}, err
	}
	if !KnowledgeEligibleTerminalStatus(task.Status) {
		return ExperienceProvenance{}, fmt.Errorf("runtime task %s is not a knowledge-eligible terminal result", taskID)
	}
	attempts, err := store.ListAttempts(taskID)
	if err != nil {
		return ExperienceProvenance{}, err
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt == nil || attempt.Status != task.Status {
			continue
		}
		digest, digestErr := ExperienceEvidenceDigest(store, task, attempt)
		if digestErr == nil {
			return ExperienceProvenance{TaskID: task.TaskID, AttemptID: attempt.AttemptID, EvidenceDigest: digest}, nil
		}
		if !strings.Contains(digestErr.Error(), "no material execution evidence") {
			return ExperienceProvenance{}, digestErr
		}
	}
	return ExperienceProvenance{}, fmt.Errorf("runtime task %s has no matching terminal attempt with material execution evidence", taskID)
}

// VerifyExperienceProvenance resolves a task's current durable provenance and
// compares it to an expected AttemptID/digest pair. Hosts use this for both
// candidate confirmation and recall-outcome recording so every knowledge
// lifecycle decision shares exactly the same terminal/evidence policy.
func VerifyExperienceProvenance(store Store, taskID, attemptID, evidenceDigest string) error {
	provenance, err := ResolveExperienceProvenance(store, taskID)
	if err != nil {
		return err
	}
	if provenance.AttemptID != strings.TrimSpace(attemptID) || provenance.EvidenceDigest != strings.TrimSpace(evidenceDigest) {
		return fmt.Errorf("runtime provenance no longer matches the expected attempt evidence")
	}
	return nil
}

// ExperienceEvidenceDigest returns an opaque, durable provenance digest for
// knowledge extracted from one completed/failed/blocked coding attempt. It
// hashes only compact Ledger facts and never copies prompts, commands,
// transcripts, credentials, or raw tool output into a knowledge store.
//
// The returned digest is valid only when the attempt contains at least one
// recognized material execution fact. Lifecycle events and unknown event
// types are deliberately insufficient: a host adding a new adapter must
// explicitly extend MaterialExperienceEvidenceEvent with tests before that
// event can authorize automatic knowledge extraction.
func ExperienceEvidenceDigest(store Store, task *Task, attempt *Attempt) (string, error) {
	if store == nil || task == nil || attempt == nil || strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(attempt.AttemptID) == "" {
		return "", fmt.Errorf("coding experience provenance is incomplete")
	}
	if strings.TrimSpace(attempt.TaskID) != "" && attempt.TaskID != task.TaskID {
		return "", fmt.Errorf("coding experience task and attempt do not match")
	}
	events, err := store.ListEvents(attempt.AttemptID)
	if err != nil {
		return "", fmt.Errorf("list runtime evidence events: %w", err)
	}
	var b strings.Builder
	b.WriteString("task=")
	b.WriteString(task.TaskID)
	b.WriteString("\nattempt=")
	b.WriteString(attempt.AttemptID)
	b.WriteString("\npolicy=")
	b.WriteString(task.PolicyDigest)
	materialEvidence := false
	for _, event := range events {
		if strings.TrimSpace(event.PayloadDigest) == "" {
			continue
		}
		b.WriteString("\nevent=")
		b.WriteString(fmt.Sprintf("%d", event.Sequence))
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(event.Type))
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(event.PayloadDigest))
		if MaterialExperienceEvidenceEvent(event.Type) {
			materialEvidence = true
		}
	}
	if !materialEvidence {
		return "", fmt.Errorf("runtime attempt %s has no material execution evidence", attempt.AttemptID)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

// KnowledgeEligibleTerminalStatus identifies Runtime task outcomes that may
// yield candidate-only automatic knowledge. Cancelled and interrupted work is
// excluded because its side-effect boundary is not confirmed; it must remain
// in the recovery flow rather than being distilled into reusable guidance.
func KnowledgeEligibleTerminalStatus(status TaskStatus) bool {
	switch status {
	case TaskCompleted, TaskFailed, TaskBlocked:
		return true
	default:
		return false
	}
}

// MaterialExperienceEvidenceEvent reports whether a compact Ledger event can
// support an automatically extracted coding experience.
func MaterialExperienceEvidenceEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "workspace_before_probed", "workspace_after_probed",
		"final_workspace_gate_passed", "final_workspace_no_change_accepted",
		"result_summary", "file_activity", "verification", "verified_no_change",
		"remote_result_summary", "remote_file_activity":
		return true
	default:
		return false
	}
}
