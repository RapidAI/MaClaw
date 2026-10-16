package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

// LoadDatasetFiles reads every JSON document under dir.
func LoadDatasetFiles(dir string) ([]DatasetFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]DatasetFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		var file DatasetFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(file.Category) == "" {
			return nil, fmt.Errorf("%s: category is required", entry.Name())
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Category < files[j].Category
	})
	return files, nil
}

// SampleResult captures the observable outcome of one scripted run.
type SampleResult struct {
	FinalStatus  string
	Completed    bool
	ManagerNexts []string
	Rounds       int
}

// scriptQueue is the stub LLM reply source for one role. An exhausted queue
// repeats its last entry so round-limit samples do not need padding.
type scriptQueue struct {
	replies []string
	next    int
}

func (q *scriptQueue) take() string {
	if len(q.replies) == 0 {
		return ""
	}
	if q.next >= len(q.replies) {
		return q.replies[len(q.replies)-1]
	}
	reply := q.replies[q.next]
	q.next++
	return reply
}

// evalRolesForNext mirrors guiapp.horizonRolesForNext; corelib cannot import
// the GUI supervisor, so the mapping is duplicated here.
func evalRolesForNext(next longhorizon.NextStep) (execRole, auditRole string) {
	switch next {
	case longhorizon.NextCLI:
		return longhorizon.RoleCLIExecutor, longhorizon.RoleCLIAuditor
	case longhorizon.NextGUI:
		return longhorizon.RoleGUIExecutor, longhorizon.RoleGUIAuditor
	case longhorizon.NextBrowser:
		return longhorizon.RoleBrowserExecutor, longhorizon.RoleBrowserAuditor
	default:
		return "", ""
	}
}

// evalAuditEvidence mirrors guiapp.horizonAuditEvidence: probe digest first,
// executor claim second.
func evalAuditEvidence(claim, probeDigest string) string {
	probe := strings.TrimSpace(probeDigest)
	claim = strings.TrimSpace(claim)
	if probe == "" {
		return claim
	}
	if claim == "" {
		return "Probe:\n" + probe
	}
	return "Probe:\n" + probe + "\nClaim:\n" + claim
}

// RunSample evaluates one sample and asserts its expected outcome.
func RunSample(sample Sample) error {
	_, err := EvaluateSample(sample)
	return err
}

// EvaluateSample runs one sample through the scripted supervisor loop and
// returns the observed result. A mismatch against sample.Expected is an
// error, mirroring routingeval.EvaluateSample.
func EvaluateSample(sample Sample) (*SampleResult, error) {
	if strings.TrimSpace(sample.ID) == "" {
		return nil, fmt.Errorf("sample id is required")
	}
	if len(sample.ManagerScript) == 0 {
		return nil, fmt.Errorf("sample %s: manager_script is required", sample.ID)
	}
	maxRounds := longhorizon.ClampMaxRounds(sample.MaxRounds)
	state := &longhorizon.TaskState{
		TaskID:    "eval-" + sample.ID,
		UserGoal:  sample.UserGoal,
		Status:    longhorizon.StatusManaging,
		MaxRounds: maxRounds,
		Policy: longhorizon.PolicySnapshot{
			OwnerID:       "user-eval",
			HorizonTaskID: "eval-" + sample.ID,
		},
	}
	manager := &scriptQueue{replies: sample.ManagerScript}
	executor := &scriptQueue{replies: sample.ExecutorScript}
	auditor := &scriptQueue{replies: sample.AuditorScript}
	probes := &scriptQueue{replies: sample.ProbeScript}

	result := &SampleResult{}
	idleAsks := 0
	round := 0
	// The loop must always terminate; guard against a diverging sample.
	safety := 4*maxRounds + 16
	for steps := 0; ; steps++ {
		if steps > safety {
			return nil, fmt.Errorf("sample %s: supervisor loop did not terminate", sample.ID)
		}
		managerEp, ok := longhorizon.AssembleManagerContext(longhorizon.ManagerPlan{Goal: state.UserGoal}, state, "", state.Policy)
		if !ok {
			return nil, fmt.Errorf("sample %s: manager context polluted", sample.ID)
		}
		_ = managerEp
		plan := longhorizon.ParseManagerPlan(manager.take())
		state.ManagerNext = plan.Next
		result.ManagerNexts = append(result.ManagerNexts, string(plan.Next))

		switch plan.Next {
		case longhorizon.NextAsk:
			idleAsks++
			if idleAsks >= maxRounds {
				state.Status = longhorizon.StatusBlocked
				return finishSample(state, result, sample)
			}
			continue
		case longhorizon.NextBlocked:
			state.Status = longhorizon.StatusBlocked
			return finishSample(state, result, sample)
		case longhorizon.NextDone:
			if longhorizon.MarkCompleted(state) {
				return finishSample(state, result, sample)
			}
			state.Carryover = append(state.Carryover, "Manager said done, but the latest real audit is not complete+clean+aligned.")
			state.Carryover = longhorizon.ClipCarryover(state.Carryover)
			idleAsks++
			if idleAsks >= maxRounds {
				state.Status = longhorizon.StatusBlocked
				return finishSample(state, result, sample)
			}
			continue
		}

		execRole, auditRole := evalRolesForNext(plan.Next)
		if execRole == "" {
			return nil, fmt.Errorf("sample %s: unroutable manager next %q", sample.ID, plan.Next)
		}
		idleAsks = 0
		if round >= maxRounds {
			state.Status = longhorizon.StatusBlocked
			return finishSample(state, result, sample)
		}
		round++
		state.RoundIndex = round
		state.Policy.RoundIndex = round

		execEp := longhorizon.AssembleEpisodeContext(execRole, plan, state, "", state.Policy)
		if !longhorizon.AssembleIsClean(execEp) {
			return nil, fmt.Errorf("sample %s: executor context polluted (role %s)", sample.ID, execRole)
		}
		claim := executor.take()
		probeDigest := probes.take()
		probe := longhorizon.ProbeResult{Digest: probeDigest, OK: strings.TrimSpace(probeDigest) != ""}

		auditEvidence := evalAuditEvidence(claim, probe.Digest)
		auditEp := longhorizon.AssembleEpisodeContext(auditRole, plan, state, auditEvidence, state.Policy)
		auditRaw := "Status: incomplete\nIntegrity: suspect\nAlignment: drifted\nSummary: auditor context was polluted."
		if longhorizon.AssembleIsClean(auditEp) {
			auditRaw = auditor.take()
		}
		report := longhorizon.ParseAuditReport(auditRaw, probe)
		report.RoundIndex = round
		state.Rounds = append(state.Rounds, longhorizon.ManagedRound{
			RoundIndex: round,
			Next:       plan.Next,
			Goal:       plan.Goal,
			Acceptance: plan.Acceptance,
			Audit:      &report,
		})
	}
}

// finishSample records the terminal state and asserts the expected outcome.
func finishSample(state *longhorizon.TaskState, result *SampleResult, sample Sample) (*SampleResult, error) {
	result.FinalStatus = state.Status
	result.Completed = state.Completed
	result.Rounds = len(state.Rounds)
	if err := assertExpected(result, sample.Expected); err != nil {
		return result, fmt.Errorf("sample %s: %w", sample.ID, err)
	}
	return result, nil
}

func assertExpected(result *SampleResult, expected ExpectedSpec) error {
	if strings.TrimSpace(expected.FinalStatus) == "" {
		return fmt.Errorf("expected.final_status is required")
	}
	if result.FinalStatus != expected.FinalStatus {
		return fmt.Errorf("final status=%s, want %s", result.FinalStatus, expected.FinalStatus)
	}
	if result.Completed != expected.Completed {
		return fmt.Errorf("completed=%v, want %v", result.Completed, expected.Completed)
	}
	if expected.ManagerNexts != nil {
		if !sameStringSlice(result.ManagerNexts, expected.ManagerNexts) {
			return fmt.Errorf("manager nexts=%v, want %v", result.ManagerNexts, expected.ManagerNexts)
		}
	}
	if result.Rounds != expected.Rounds {
		return fmt.Errorf("rounds=%d, want %d", result.Rounds, expected.Rounds)
	}
	return nil
}

func sameStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// ReportEntry is the per-sample line of an evaluation Report.
type ReportEntry struct {
	Category string
	SampleID string
	Result   *SampleResult
	Err      error
}

// Report aggregates one run over every dataset file.
type Report struct {
	Entries []ReportEntry
}

// Failures counts the samples whose run or assertions failed.
func (r Report) Failures() int {
	n := 0
	for _, entry := range r.Entries {
		if entry.Err != nil {
			n++
		}
	}
	return n
}

// String renders a stable, human-readable summary, one line per sample plus
// a totals footer.
func (r Report) String() string {
	var b strings.Builder
	for _, entry := range r.Entries {
		name := entry.Category + "/" + entry.SampleID
		if entry.Err != nil {
			fmt.Fprintf(&b, "FAIL %s: %v\n", name, entry.Err)
			continue
		}
		fmt.Fprintf(&b, "PASS %s status=%s completed=%v rounds=%d nexts=%s\n",
			name, entry.Result.FinalStatus, entry.Result.Completed, entry.Result.Rounds,
			strings.Join(entry.Result.ManagerNexts, ","))
	}
	fmt.Fprintf(&b, "total=%d failed=%d", len(r.Entries), r.Failures())
	return b.String()
}

// RunDatasets evaluates every sample of every dataset file and collects the
// outcomes into a Report. It never aborts early so one broken sample does not
// hide the rest.
func RunDatasets(files []DatasetFile) Report {
	report := Report{}
	for _, dataset := range files {
		for _, sample := range dataset.Samples {
			result, err := EvaluateSample(sample)
			report.Entries = append(report.Entries, ReportEntry{
				Category: dataset.Category,
				SampleID: sample.ID,
				Result:   result,
				Err:      err,
			})
		}
	}
	return report
}
