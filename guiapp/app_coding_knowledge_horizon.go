package guiapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

const horizonExperienceProvenancePrefix = "horizon:"

// verifyHorizonCodingExperience revalidates a LongHorizon experience against
// the durable round audit. Horizon episodes never enter the coding runtime
// ledger, so the standard verifier would reject every horizon candidate and
// leave it permanently unfixable in the review queue. The source task state is
// the horizon equivalent of the ledger: it records the task, round and audit
// digest the experience was extracted from.
//
// This verifier is only reachable from manual confirmation. Automatic
// promotion never touches horizon records.
func (a *App) verifyHorizonCodingExperience(exp knowledge.CodingExperience) error {
	if a == nil {
		return fmt.Errorf("coding runtime application is unavailable")
	}
	taskID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(exp.SourceRuntimeTaskID), horizonExperienceProvenancePrefix))
	if taskID == "" {
		return fmt.Errorf("horizon provenance has no task id")
	}
	round, ok := parseHorizonExperienceRound(exp.SourceRuntimeAttemptID)
	if !ok {
		return fmt.Errorf("horizon provenance has no round attempt")
	}
	state, err := longhorizon.LoadTaskState(a.getMaclawBaseDir(), taskID)
	if err != nil {
		return fmt.Errorf("horizon task state is unavailable: %w", err)
	}
	if strings.TrimSpace(state.TaskID) != taskID {
		return fmt.Errorf("horizon task state does not match the recorded task")
	}
	var audit *longhorizon.AuditReport
	for index := range state.Rounds {
		if state.Rounds[index].RoundIndex == round {
			audit = state.Rounds[index].Audit
			break
		}
	}
	if audit == nil {
		return fmt.Errorf("horizon round %d has no audit evidence", round)
	}
	if audit.RoundIndex != 0 && audit.RoundIndex != round {
		return fmt.Errorf("horizon round %d audit is for round %d", round, audit.RoundIndex)
	}
	if strings.TrimSpace(audit.Digest) != strings.TrimSpace(exp.EvidenceDigest) {
		return fmt.Errorf("horizon audit digest no longer matches the candidate evidence")
	}
	horizonTaskID := strings.TrimSpace(state.Policy.HorizonTaskID)
	if horizonTaskID == "" {
		horizonTaskID = taskID
	}
	// Re-check the round-level eligibility that made the write possible. A
	// later whole-task cancellation must not invalidate evidence from an
	// earlier completed round.
	if !longhorizon.ExperienceEligible(longhorizon.EligibilityInput{
		HorizonTaskID: horizonTaskID,
		RoundIndex:    round,
		AuditDigest:   audit.Digest,
		Audit:         audit,
		Untrusted:     state.Policy.Untrusted,
	}) {
		return fmt.Errorf("horizon round %d is not experience-eligible", round)
	}
	return nil
}

func parseHorizonExperienceRound(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "round-") {
		return 0, false
	}
	round, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(raw, "round-")))
	if err != nil || round <= 0 {
		return 0, false
	}
	return round, true
}
