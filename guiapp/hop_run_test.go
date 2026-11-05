package guiapp

import (
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var errHopExecutionUnreadable = errors.New("plan execution store is unavailable")

type hopUnreadableExecutionStore struct{}

func (hopUnreadableExecutionStore) Acquire(tool.PlanExecutionRecord) (tool.PlanExecutionRecord, bool, error) {
	return tool.PlanExecutionRecord{}, false, errHopExecutionUnreadable
}

func (hopUnreadableExecutionStore) Complete(tool.InvocationScope, string, tool.PlanExecutionState, string, string, time.Time) (tool.PlanExecutionRecord, error) {
	return tool.PlanExecutionRecord{}, errHopExecutionUnreadable
}

func (hopUnreadableExecutionStore) SettleAwaitingReceipt(tool.InvocationScope, string, tool.PlanExecutionState, string, string, time.Time) (tool.PlanExecutionRecord, error) {
	return tool.PlanExecutionRecord{}, errHopExecutionUnreadable
}

func (hopUnreadableExecutionStore) Execution(tool.InvocationScope, string) (tool.PlanExecutionRecord, error) {
	return tool.PlanExecutionRecord{}, errHopExecutionUnreadable
}

func (hopUnreadableExecutionStore) Succeeded(tool.InvocationScope) (map[string]bool, error) {
	return nil, errHopExecutionUnreadable
}

func (hopUnreadableExecutionStore) ReconcileStaleRunning(time.Time, time.Duration) (int, error) {
	return 0, errHopExecutionUnreadable
}

func newHopRunTestSurface(t *testing.T) (*semanticCallSurface, tool.PlanExecutionStore) {
	t.Helper()
	plan := tool.ToolPlan{
		RootTaskID: "root-macmini",
		ID:         "plan-macmini",
		Selections: []tool.PlannedSelection{
			{ID: "shell", NeedID: "need:shell", FitProof: tool.FitProof{MatchedCapability: "shell.execute.remote_host"}},
			{ID: "report", NeedID: "need:report", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	return &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID:  "root-macmini",
			PlanID:      "plan-macmini",
			SessionID:   "session-macmini",
			TurnID:      "turn-macmini",
			PrincipalID: "user-macmini",
		},
		executor:  executor,
		completed: map[string]bool{},
	}, store
}

func TestHopRunRequiresTrustedSuccessForEveryRequiredSelection(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-macmini", userText: "查询 macmini状态信息"}

	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining != 2 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("unexecuted run remaining=%d unsettled=%v", snapshot.Remaining, cb.RequiredEffectUnsettled())
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	snapshot = cb.hopRunSnapshot()
	if snapshot.Remaining != 2 || snapshot.Steps[0].Status != HopStepFailed {
		t.Fatalf("parameter rejection was accepted: %#v", snapshot.Steps[0])
	}

	surface.completed["shell"] = true
	if got := cb.hopRunSnapshot().Remaining; got != 1 {
		t.Fatalf("successful shell did not close exactly one effect: %d", got)
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "report"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "report", tool.PlanExecutionAwaitingReceipt, "", "waiting", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := cb.hopRunSnapshot().Remaining; got != 1 {
		t.Fatalf("awaiting receipt closed the run: %d", got)
	}

	surface.completed["report"] = true
	snapshot = cb.hopRunSnapshot()
	if snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("trusted successes left run open: %#v", snapshot)
	}
	final := finalizeHopRun(snapshot, agent.LoopResult{})
	if final.Status != "completed" || final.Remaining != 0 {
		t.Fatalf("final=%#v", final)
	}
}

func TestHopRunLeavesUnstartedSelectionsWithoutRunningEvidence(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.Selections = append(surface.plan.Selections, tool.PlannedSelection{
		ID:       "shell-repeat",
		NeedID:   tool.RepeatSiblingNeedID("need:shell", 1),
		FitProof: tool.FitProof{MatchedCapability: "shell.execute.remote_host"},
	})
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-api2", userText: "查询 api2服务器状态"}

	snapshot := cb.hopRunSnapshot()
	if snapshot.Steps[0].Status != HopStepPending || snapshot.Steps[0].Evidence.State != "" {
		t.Fatalf("unstarted required step = %#v", snapshot.Steps[0])
	}
	repeat := snapshot.Steps[len(snapshot.Steps)-1]
	if repeat.NeedID != tool.RepeatSiblingNeedID("need:shell", 1) || repeat.Required || repeat.Status != HopStepOptional || repeat.Evidence.State != "" {
		t.Fatalf("unstarted repeat = %#v", repeat)
	}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	snapshot = cb.hopRunSnapshot()
	if snapshot.Steps[0].Evidence.State != tool.PlanExecutionRunning || snapshot.Steps[0].Status != HopStepRunning || snapshot.Remaining != 2 {
		t.Fatalf("acquired step = %#v remaining=%d", snapshot.Steps[0], snapshot.Remaining)
	}
	repeat = snapshot.Steps[len(snapshot.Steps)-1]
	if repeat.Evidence.State != "" || repeat.Status != HopStepOptional {
		t.Fatalf("unacquired repeat changed = %#v", repeat)
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell-repeat"}); err != nil {
		t.Fatal(err)
	}
	snapshot = cb.hopRunSnapshot()
	repeat = snapshot.Steps[len(snapshot.Steps)-1]
	if repeat.Status != HopStepRunning || repeat.Evidence.State != tool.PlanExecutionRunning || snapshot.Remaining != 2 {
		t.Fatalf("optional acquire counted as required: %#v remaining=%d", repeat, snapshot.Remaining)
	}
}

func TestHopRunRequiredUnmetAndOptionalNeeds(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Unmet = []tool.UnmetNeed{{NeedID: "need:shell", ReasonCode: "provider_unavailable"}}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if got := hopMissingRequiredEffects(cb.hopRunSnapshot()); len(got) != 2 {
		t.Fatalf("required unmet missing from contract: %#v", got)
	}
	optional := tool.PlannedSelection{ID: "optional", NeedID: "need:optional"}
	if hopSelectionRequired(optional, map[string]bool{"need:optional": true}, true, false) {
		t.Fatal("proved optional need blocked the run")
	}
	recorded := tool.PlannedSelection{ID: "git", NeedID: "need:repo.inspect.vcs:base"}
	if !hopSelectionRequired(recorded, map[string]bool{recorded.NeedID: true}, true, true) {
		t.Fatal("classification map released a recorded obligation")
	}
	sibling := tool.PlannedSelection{ID: "sibling", NeedID: tool.RepeatSiblingNeedID("need:shell", 1)}
	if hopSelectionRequired(sibling, nil, false, true) {
		t.Fatal("repeat sibling became a separate completion obligation")
	}
	companion := tool.PlannedSelection{
		ID:          "acquire",
		NeedID:      "need:artifact.acquire.remote:47dfffbc86c8",
		EvidenceIDs: []string{"intent:archetype_bundle"},
		FitProof:    tool.FitProof{MatchedCapability: tool.CapabilityArtifactAcquireRemote},
	}
	if hopSelectionRequired(companion, nil, false, false) {
		t.Fatal("archetype companion became required when optionality could not be recomputed")
	}
	baseline := tool.PlannedSelection{
		ID:          "baseline-shell",
		NeedID:      "need:zz-baseline:shell.execute.local:abc",
		EvidenceIDs: []string{"intent:baseline_workspace"},
		FitProof:    tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
	}
	if hopSelectionRequired(baseline, nil, false, false) {
		t.Fatal("baseline workspace offer became a required effect")
	}
	lookupOffer := tool.PlannedSelection{
		ID:          "search-offer",
		NeedID:      "need:information.search.web:offered",
		EvidenceIDs: []string{tool.OfferedCompanionEvidencePrefix + "live_data"},
		FitProof:    tool.FitProof{MatchedCapability: "information.search.web"},
	}
	if hopSelectionRequired(lookupOffer, nil, false, true) {
		t.Fatal("unconfirmed lookup offer became required when the optional bit was lost")
	}
	confirmedLookup := tool.PlannedSelection{
		ID:          "search-required",
		NeedID:      "need:information.search.web:required",
		EvidenceIDs: []string{"intent:live_data"},
		FitProof:    tool.FitProof{MatchedCapability: "information.search.web"},
	}
	if !hopSelectionRequired(confirmedLookup, nil, false, true) {
		t.Fatal("confirmed lookup was released with the offer")
	}
	offered := tool.PlannedSelection{
		ID:       "selection:need:tool-head:shell.execute.local:63d070da4fef",
		NeedID:   "need:tool-head:shell.execute.local:63d070da4fef",
		Optional: true,
		FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
	}
	if hopSelectionRequired(offered, nil, true, false) || hopSelectionRequired(offered, nil, false, true) {
		t.Fatal("plan-time optional selection became a required effect")
	}
	unrecorded := tool.PlannedSelection{
		ID:       "selection:need:tool-head:shell.execute.local:legacy",
		NeedID:   "need:tool-head:shell.execute.local:legacy",
		FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
	}
	if !hopSelectionRequired(unrecorded, nil, false, false) {
		t.Fatal("a selection without the plan obligation bit must stay fail-closed")
	}
}

func TestHopRunToolHeadShellDoesNotBlockAFinishedInspect(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "git", NeedID: "need:repo.inspect.vcs:ea8af2139251", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityRepoInspectVCS}},
		{
			ID:       "bash",
			NeedID:   "need:tool-head:shell.execute.local:63d070da4fef",
			Optional: true,
			FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
		},
		{
			ID:       "bash-repeat",
			NeedID:   tool.RepeatSiblingNeedID("need:tool-head:shell.execute.local:63d070da4fef", 1),
			Optional: true,
			FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
		},
	}
	surface.completed["git"] = true
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-stash", userText: "git的stash是什么意思？"}
	snapshot := cb.hopRunSnapshot()
	if cb.RequiredEffectUnsettled() || snapshot.Remaining != 0 {
		t.Fatalf("unused tool-head bash kept the answer open: remaining=%d steps=%#v", snapshot.Remaining, snapshot.Steps)
	}
	final := finalizeHopRun(snapshot, agent.LoopResult{})
	if final.Status != "completed" {
		t.Fatalf("status=%s remaining=%d", final.Status, final.Remaining)
	}
}

func TestRecordedPlanReadsObligationWithoutClassification(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ID = "plan-recorded"
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "git", NeedID: "need:repo.inspect.vcs:base", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityRepoInspectVCS}},
		{ID: "bash", NeedID: "need:tool-head:shell.execute.local:x", Optional: true, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal}},
	}
	surface.optionalKnown = true
	surface.optionalCacheKey = semanticOptionalCacheKey(surface)
	surface.optionalByNeed = map[string]bool{"need:repo.inspect.vcs:base": true}
	optional, known := semanticSurfaceOptionalNeeds(surface)
	if !known || !optional["need:tool-head:shell.execute.local:x"] || optional["need:repo.inspect.vcs:base"] {
		t.Fatalf("recorded obligation=%v known=%v", optional, known)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if snapshot := cb.hopRunSnapshot(); snapshot.Remaining == 0 {
		t.Fatal("stale optional map released the recorded inspect")
	}
	if _, knownAgain := semanticSurfaceOptionalNeeds(surface); !knownAgain {
		t.Fatal("recorded obligation was resolved again as unknown")
	}
}

func TestHopRunAdmittedUnmetDoesNotKeepAFinishedPrefixOpen(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "search", NeedID: "need:search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
	}
	surface.completed["search"] = true
	surface.plan.Unmet = []tool.UnmetNeed{
		{NeedID: "need:generate", ReasonCode: "budget_exceeded"},
		{NeedID: "need:~installed-mcp:x:mcp:demo:tool", ReasonCode: "no_feasible_provider"},
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("admitted unmet kept the turn open: remaining=%d steps=%#v", snapshot.Remaining, snapshot.Steps)
	}
	surface.plan.Unmet = append(surface.plan.Unmet, tool.UnmetNeed{NeedID: "need:worklog", ReasonCode: "provider_unavailable"})
	if snapshot = cb.hopRunSnapshot(); snapshot.Remaining == 0 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("uncovered provider gap was admitted: %#v", snapshot)
	}
}

func TestUnrecordedOptionalityFailureStaysCached(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.replan = &semanticReplanInput{}
	if _, known := semanticSurfaceOptionalNeeds(surface); known || surface.optionalCacheKey == "" {
		t.Fatal("missing registry was treated as a resolved optional set")
	}
	cached := surface.optionalCacheKey
	surface.plan.Selections[0].Optional = true
	optional, known := semanticSurfaceOptionalNeeds(surface)
	if surface.optionalCacheKey != cached || !known || !optional[surface.plan.Selections[0].NeedID] {
		t.Fatalf("cached failure did not keep the current optional bit: known=%v cache=%q optional=%v", known, surface.optionalCacheKey, optional)
	}
}

func TestHopRunAuthoringFaultStaysRequiredWhenMarkedOptional(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ID = "plan-authoring"
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "other", NeedID: "need:other", Optional: true, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal}},
	}
	surface.plan.Unmet = []tool.UnmetNeed{{NeedID: "need:typo", ReasonCode: "unknown_capability"}}
	surface.optionalKnown = true
	surface.optionalCacheSet = true
	surface.optionalCacheKey = semanticOptionalCacheKey(surface)
	surface.optionalByNeed = map[string]bool{"need:typo": true, "need:other": true}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining == 0 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("optional map hid an authoring fault: %#v", snapshot)
	}
}

func TestHopRunIsRecordedInTrajectory(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	for _, selection := range surface.plan.Selections {
		surface.completed[selection.ID] = true
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-trajectory", userText: "查询 macmini状态信息"}
	recorder := NewTrajectoryRecorderForBaseDir(t.TempDir())
	recorder.StartSession("run-trajectory", "test", "test", "test", "user", "desktop", nil)
	snapshot := finalizeHopRun(cb.hopRunSnapshot(), agent.LoopResult{})
	recorder.SetHopRun(snapshot)
	if recorder.session.HopRun == nil || recorder.session.HopRun.Status != "completed" || recorder.session.HopRun.Remaining != 0 {
		t.Fatalf("recorded hop run=%#v", recorder.session.HopRun)
	}
	if recorder.session.HopRun.Goal != cb.userText || len(recorder.session.HopRun.Steps) != 2 {
		t.Fatalf("structured spec missing: %#v", recorder.session.HopRun)
	}
}

func TestSemanticTurnDeliveryCompleteRejectsNilSurface(t *testing.T) {
	if semanticTurnDeliveryComplete(nil) {
		t.Fatal("nil surface completed a delivery")
	}
}

func TestSemanticOptionalCacheKeyIncludesPlanRevision(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	first := semanticOptionalCacheKey(surface)
	surface.plan.ID = "plan-next-revision"
	if next := semanticOptionalCacheKey(surface); next == first {
		t.Fatal("plan revision did not invalidate the optional-needs cache")
	}
}

func TestHopRunCachesTerminalEvidence(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "digest-failed", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	first := cb.hopRunSnapshot()
	if first.Steps[0].Evidence.ResultDigest != "digest-failed" {
		t.Fatalf("evidence=%#v", first.Steps[0].Evidence)
	}
	if got := surface.hopEvidence[hopEvidenceKey(surface, "shell")]; got.State != tool.PlanExecutionFailed || got.ReasonCode != "parameter_required" {
		t.Fatalf("terminal evidence not cached: %#v", got)
	}
	if second := cb.hopRunSnapshot(); second.Steps[0].Evidence != first.Steps[0].Evidence {
		t.Fatalf("cached evidence changed: first=%#v second=%#v", first.Steps[0].Evidence, second.Steps[0].Evidence)
	}
}

func TestHopRunUnknownExecutionIsUnsettledNotFailure(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionUnknown, "digest-unknown", "receipt_unclear", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-unknown", userText: "查询 macmini状态信息"}

	snapshot := cb.hopRunSnapshot()
	if snapshot.Steps[0].Status != HopStepUnsettled {
		t.Fatalf("unknown execution should be unsettled, got %#v", snapshot.Steps[0])
	}
	if snapshot.Steps[0].Evidence.State != tool.PlanExecutionUnknown || snapshot.Steps[0].Evidence.ReasonCode != "receipt_unclear" {
		t.Fatalf("unknown evidence lost: %#v", snapshot.Steps[0].Evidence)
	}
	if snapshot.Remaining != 2 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("unsettled step must block the run: remaining=%d unsettled=%v", snapshot.Remaining, cb.RequiredEffectUnsettled())
	}
	final := finalizeHopRun(snapshot, agent.LoopResult{})
	if final.Status != "failed" {
		t.Fatalf("unsettled run should finalize as failed, got %#v", final)
	}
}

func TestHopRunEvidenceCacheIsPlanScoped(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "digest-failed", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	first := cb.hopRunSnapshot()
	if first.Steps[0].Evidence.ResultDigest != "digest-failed" {
		t.Fatalf("evidence=%#v", first.Steps[0].Evidence)
	}

	// Plan revision reuses the same surface; the old digest must not leak in.
	surface.plan.ID = "plan-macmini-r2"
	surface.scope.PlanID = "plan-macmini-r2"
	revised := cb.hopRunSnapshot()
	if revised.Steps[0].Evidence.ResultDigest == "digest-failed" {
		t.Fatalf("stale digest leaked across plan revision: %#v", revised.Steps[0].Evidence)
	}
	if revised.Steps[0].Status != HopStepPending {
		t.Fatalf("revised plan should reopen the step, got %#v", revised.Steps[0])
	}
	if revised.Remaining != 2 {
		t.Fatalf("revised plan remaining=%d", revised.Remaining)
	}

	// The old cache entry stays keyed to the old revision and must remain intact.
	if got := surface.hopEvidence["plan-macmini\x00shell"]; got.State != tool.PlanExecutionFailed || got.ResultDigest != "digest-failed" {
		t.Fatalf("old-revision cache entry corrupted: %#v", got)
	}
}

func TestHopRunCompletedOverrideClearsStaleFailureEvidence(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "digest-failed", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-override", userText: "查询 macmini状态信息"}

	failed := cb.hopRunSnapshot()
	if failed.Steps[0].Status != HopStepFailed {
		t.Fatalf("precondition: step should be failed, got %#v", failed.Steps[0])
	}

	// The host later closes the same effect through a trusted path (e.g. a
	// sibling selection satisfied the need). The override must not keep the
	// stale failure reason/digest next to the succeeded state.
	surface.completed["shell"] = true
	overridden := cb.hopRunSnapshot()
	if overridden.Steps[0].Status != HopStepCompleted {
		t.Fatalf("override did not complete the step: %#v", overridden.Steps[0])
	}
	want := HopStepEvidence{State: tool.PlanExecutionSucceeded}
	if overridden.Steps[0].Evidence != want {
		t.Fatalf("stale failure evidence survived override: %#v", overridden.Steps[0].Evidence)
	}
	if overridden.Remaining != 1 {
		t.Fatalf("override remaining=%d", overridden.Remaining)
	}

	// A digest from a genuinely succeeded record is kept.
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "report"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "report", tool.PlanExecutionSucceeded, "digest-ok", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	surface.completed["report"] = true
	final := cb.hopRunSnapshot()
	if final.Steps[1].Evidence != (HopStepEvidence{State: tool.PlanExecutionSucceeded, ResultDigest: "digest-ok"}) {
		t.Fatalf("succeeded digest lost: %#v", final.Steps[1].Evidence)
	}
	if final.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("fully satisfied run still open: remaining=%d", final.Remaining)
	}
}

func TestFinalizeHopRunStatusMapping(t *testing.T) {
	base := func() *HopRunSnapshot {
		return &HopRunSnapshot{SpecVersion: hopRunSpecVersion, Status: "running", Remaining: 1}
	}
	cases := []struct {
		name      string
		result    agent.LoopResult
		remaining int
		want      string
	}{
		{name: "ask pauses", result: agent.LoopResult{AskUser: &agent.AskUserRequest{}}, remaining: 1, want: "paused"},
		{name: "audio pauses", result: agent.LoopResult{RecordAudio: &agent.RecordAudioRequest{}}, remaining: 1, want: "paused"},
		{name: "cancel wins over remaining", result: agent.LoopResult{Error: "cancelled"}, remaining: 1, want: "cancelled"},
		{name: "hard exit unsettled fails", result: agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}, remaining: 1, want: "failed"},
		{name: "open remaining fails", result: agent.LoopResult{}, remaining: 1, want: "failed"},
		{name: "clean run completes", result: agent.LoopResult{}, remaining: 0, want: "completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := base()
			snapshot.Remaining = tc.remaining
			final := finalizeHopRun(snapshot, tc.result)
			if final.Status != tc.want {
				t.Fatalf("status=%q want %q (result=%+v)", final.Status, tc.want, tc.result)
			}
			if final.Remaining != tc.remaining {
				t.Fatalf("remaining mutated: %d", final.Remaining)
			}
		})
	}
	if finalizeHopRun(nil, agent.LoopResult{Error: "x"}) != nil {
		t.Fatal("nil snapshot should stay nil")
	}
}

func newHopRepeatFamilySurface(t *testing.T, capability string) (*semanticCallSurface, tool.PlanExecutionStore) {
	t.Helper()
	plan := tool.ToolPlan{
		RootTaskID: "root-repeat",
		ID:         "plan-repeat",
		Selections: []tool.PlannedSelection{
			{ID: "shell", NeedID: "need:shell", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityID(capability)}},
			{ID: "shell-2", NeedID: tool.RepeatSiblingNeedID("need:shell", 1), FitProof: tool.FitProof{MatchedCapability: tool.CapabilityID(capability)}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	return &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID:  "root-repeat",
			PlanID:      "plan-repeat",
			SessionID:   "session-repeat",
			TurnID:      "turn-repeat",
			PrincipalID: "user-repeat",
		},
		executor:  executor,
		completed: map[string]bool{},
	}, store
}

func TestHopRunFailedWebSearchSettlesWhenFetchSucceeds(t *testing.T) {
	plan := tool.ToolPlan{
		RootTaskID: "root-weather",
		ID:         "plan-weather",
		Selections: []tool.PlannedSelection{
			{ID: "search", NeedID: "need:information.search.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
			{ID: "shell", NeedID: "need:shell", FitProof: tool.FitProof{MatchedCapability: "shell.execute.remote_host"}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	surface := &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID: "root-weather", PlanID: "plan-weather",
			SessionID: "session-weather", TurnID: "turn-weather", PrincipalID: "user-weather",
		},
		executor:  executor,
		completed: map[string]bool{},
	}
	complete := func(id string, state tool.PlanExecutionState, reason string) {
		t.Helper()
		if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Complete(surface.scope, id, state, "digest", reason, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	complete("search", tool.PlanExecutionFailed, "selection_execution_failed")
	complete("shell", tool.PlanExecutionFailed, "parameter_required")
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-weather", userText: "北京天气"}

	open := cb.hopRunSnapshot()
	if !open.Steps[0].Required || open.Steps[0].Superseded || open.Steps[0].Status != HopStepFailed {
		t.Fatalf("failed search without a fetch must stay open: %#v", open.Steps[0])
	}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("failed search and shell must keep the turn unsettled")
	}

	complete("fetch", tool.PlanExecutionSucceeded, "")
	settled := cb.hopRunSnapshot()
	if !settled.Steps[0].Superseded || settled.Steps[0].Status != HopStepFailed {
		t.Fatalf("fetch success must supersede the failed search without erasing it: %#v", settled.Steps[0])
	}
	if settled.Steps[1].Status != HopStepCompleted {
		t.Fatalf("fetch step: %#v", settled.Steps[1])
	}
	if settled.Steps[2].Superseded || settled.Steps[2].Status != HopStepFailed {
		t.Fatalf("unrelated shell failure must stay open: %#v", settled.Steps[2])
	}
	if !cb.RequiredEffectUnsettled() || settled.Remaining != 1 {
		t.Fatalf("shell must still block, remaining=%d unsettled=%v", settled.Remaining, cb.RequiredEffectUnsettled())
	}
}

func TestHopRunPendingWebSearchSettlesWhenFetchSucceeds(t *testing.T) {
	newSurface := func(t *testing.T) (*semanticCallSurface, *sharedAgentLoopCallbacks, func(id string, state tool.PlanExecutionState)) {
		t.Helper()
		plan := tool.ToolPlan{
			RootTaskID: "root-resume-weather",
			ID:         "plan-resume-weather",
			Selections: []tool.PlannedSelection{
				{ID: "search", NeedID: "need:information.search.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
				{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
			},
		}
		store := tool.NewMemoryPlanExecutionStore()
		issuer, err := tool.NewRandomInvocationIssuer()
		if err != nil {
			t.Fatal(err)
		}
		executor, err := tool.NewPlanExecutor(issuer, store)
		if err != nil {
			t.Fatal(err)
		}
		surface := &semanticCallSurface{
			plan: plan,
			scope: tool.InvocationScope{
				RootTaskID: "root-resume-weather", PlanID: "plan-resume-weather",
				SessionID: "session-resume", TurnID: "turn-resume", PrincipalID: "user-resume",
			},
			executor:  executor,
			completed: map[string]bool{},
		}
		cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-resume-weather", userText: "继续上次未完成任务"}
		complete := func(id string, state tool.PlanExecutionState) {
			t.Helper()
			if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: id}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Complete(surface.scope, id, state, "digest", "", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		return surface, cb, complete
	}
	_, fresh, completeFresh := newSurface(t)
	completeFresh("fetch", tool.PlanExecutionSucceeded)
	settled := fresh.hopRunSnapshot()
	if !settled.Steps[0].Superseded || settled.Steps[0].Status != HopStepPending || settled.Steps[0].Evidence.State != "" {
		t.Fatalf("fetch success must discharge a search this plan never started, without inventing an execution: %#v", settled.Steps[0])
	}
	if settled.Remaining != 0 || fresh.RequiredEffectUnsettled() {
		t.Fatalf("recovered lookup still open: remaining=%d unsettled=%v", settled.Remaining, fresh.RequiredEffectUnsettled())
	}
	if got := hopMissingRequiredEffects(settled); len(got) != 0 {
		t.Fatalf("discharged search still listed as missing: %#v", got)
	}
	if final := finalizeHopRun(settled, agent.LoopResult{}); final.Status != "completed" {
		t.Fatalf("final=%#v", final)
	}

	_, noFetch, _ := newSurface(t)
	if got := noFetch.hopRunSnapshot(); got.Steps[0].Superseded || got.Steps[0].Status != HopStepPending || !noFetch.RequiredEffectUnsettled() {
		t.Fatalf("a search with no fetch must stay owed: %#v remaining=%d", got.Steps[0], got.Remaining)
	}

	_, failedFetch, completeFailed := newSurface(t)
	completeFailed("fetch", tool.PlanExecutionFailed)
	if got := failedFetch.hopRunSnapshot(); got.Steps[0].Superseded || !failedFetch.RequiredEffectUnsettled() {
		t.Fatalf("a failed fetch is not the fact: %#v", got.Steps[0])
	}
}

func TestHopRunFetchDoesNotSettleASecondWebSearch(t *testing.T) {
	plan := tool.ToolPlan{
		RootTaskID: "root-two-searches",
		ID:         "plan-two-searches",
		Selections: []tool.PlannedSelection{
			{ID: "beijing", NeedID: "need:information.search.web:beijing", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "shanghai", NeedID: "need:information.search.web:shanghai", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:page", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	surface := &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID: "root-two-searches", PlanID: "plan-two-searches",
			SessionID: "session-two", TurnID: "turn-two", PrincipalID: "user-two",
		},
		executor:  executor,
		completed: map[string]bool{},
	}
	complete := func(id string, state tool.PlanExecutionState) {
		t.Helper()
		if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Complete(surface.scope, id, state, "digest", "", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	complete("fetch", tool.PlanExecutionSucceeded)
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	got := cb.hopRunSnapshot()
	if got.Steps[0].Superseded || got.Steps[1].Superseded || !cb.RequiredEffectUnsettled() {
		t.Fatalf("one fetch must not discharge two unstarted searches: %#v %#v", got.Steps[0], got.Steps[1])
	}
	complete("beijing", tool.PlanExecutionFailed)
	got = cb.hopRunSnapshot()
	if got.Steps[0].Superseded || got.Steps[0].Status != HopStepFailed || got.Steps[1].Superseded || !cb.RequiredEffectUnsettled() {
		t.Fatalf("a second required search keeps both facts open: %#v %#v", got.Steps[0], got.Steps[1])
	}
	complete("shanghai", tool.PlanExecutionFailed)
	got = cb.hopRunSnapshot()
	if got.Steps[0].Superseded || got.Steps[1].Superseded || got.Remaining != 2 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("one fetch must not discharge two failed searches: %#v %#v remaining=%d", got.Steps[0], got.Steps[1], got.Remaining)
	}
}

func TestHopRunOptionalSearchRepeatDoesNotSplitTheLookup(t *testing.T) {
	base := "need:information.search.web:weather"
	plan := tool.ToolPlan{
		RootTaskID: "root-resume-weather",
		ID:         "plan-resume-repeat",
		Selections: []tool.PlannedSelection{
			{ID: "search", NeedID: base, FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "search-2", NeedID: tool.RepeatSiblingNeedID(base, 1), FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	surface := &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID: "root-resume-weather", PlanID: "plan-resume-repeat",
			SessionID: "session-resume", TurnID: "turn-resume", PrincipalID: "user-resume",
		},
		executor:  executor,
		completed: map[string]bool{"fetch": true},
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	got := cb.hopRunSnapshot()
	if !got.Steps[0].Superseded || got.Steps[0].Status != HopStepPending || got.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("one required search plus an unused repeat still settles on the fetch: %#v remaining=%d", got.Steps[0], got.Remaining)
	}
	if got.Steps[1].Required || got.Steps[1].Superseded {
		t.Fatalf("the repeat slot is not a second fact: %#v", got.Steps[1])
	}
}

func TestHopRunOfferedSearchDoesNotBlockFetchRecovery(t *testing.T) {
	plan := tool.ToolPlan{
		RootTaskID:          "root-offer-fetch",
		ID:                  "plan-offer-fetch",
		ObligationsRecorded: true,
		Selections: []tool.PlannedSelection{
			{ID: "search", NeedID: "need:information.search.web:weather", EvidenceIDs: []string{"intent:live_data"}, FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "offer", NeedID: "need:information.search.web:offer", EvidenceIDs: []string{tool.OfferedCompanionEvidencePrefix + "live_data"}, FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "offer-2", NeedID: tool.RepeatSiblingNeedID("need:information.search.web:offer", 1), EvidenceIDs: []string{"intent:archetype_bundle"}, FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
		},
	}
	surface := &semanticCallSurface{
		plan:      plan,
		completed: map[string]bool{"fetch": true},
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	got := cb.hopRunSnapshot()
	if !got.Steps[0].Superseded || !got.Steps[0].Required {
		t.Fatalf("the confirmed search must still be the one fact a fetch can close: %#v", got.Steps[0])
	}
	if got.Steps[1].Required || got.Steps[2].Required {
		t.Fatalf("the offer family counted as more required searches: %#v %#v", got.Steps[1], got.Steps[2])
	}
	if got.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("offer family blocked fetch recovery: remaining=%d unsettled=%v steps=%#v", got.Remaining, cb.RequiredEffectUnsettled(), got.Steps)
	}
}

func TestHopRunUnreadableSearchStaysOpenWhenFetchSucceeds(t *testing.T) {
	plan := tool.ToolPlan{
		RootTaskID: "root-resume-weather",
		ID:         "plan-resume-unreadable",
		Selections: []tool.PlannedSelection{
			{ID: "search", NeedID: "need:information.search.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
		},
	}
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, hopUnreadableExecutionStore{})
	if err != nil {
		t.Fatal(err)
	}
	surface := &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID: "root-resume-weather", PlanID: "plan-resume-unreadable",
			SessionID: "session-resume", TurnID: "turn-resume", PrincipalID: "user-resume",
		},
		executor:  executor,
		completed: map[string]bool{"fetch": true},
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	got := cb.hopRunSnapshot()
	step := got.Steps[0]
	if step.Superseded || step.Status != HopStepUnsettled || step.Evidence.State != tool.PlanExecutionUnknown || !cb.RequiredEffectUnsettled() {
		t.Fatalf("a search whose record could not be read stays open: %#v", step)
	}
	if _, ok := surface.hopEvidence[hopEvidenceKey(surface, "search")]; ok {
		t.Fatal("an unreadable execution must not be cached")
	}
}

func TestHopRunAcquiredWebSearchStaysOpenWhenFetchSucceeds(t *testing.T) {
	plan := tool.ToolPlan{
		RootTaskID: "root-resume-weather",
		ID:         "plan-resume-running",
		Selections: []tool.PlannedSelection{
			{ID: "search", NeedID: "need:information.search.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationSearchWeb}},
			{ID: "fetch", NeedID: "need:information.fetch.web:weather", FitProof: tool.FitProof{MatchedCapability: hopInformationFetchWeb}},
		},
	}
	store := tool.NewMemoryPlanExecutionStore()
	issuer, err := tool.NewRandomInvocationIssuer()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	surface := &semanticCallSurface{
		plan: plan,
		scope: tool.InvocationScope{
			RootTaskID: "root-resume-weather", PlanID: "plan-resume-running",
			SessionID: "session-resume", TurnID: "turn-resume", PrincipalID: "user-resume",
		},
		executor:  executor,
		completed: map[string]bool{},
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "search"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "fetch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "fetch", tool.PlanExecutionSucceeded, "digest", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	got := cb.hopRunSnapshot()
	if got.Steps[0].Superseded || got.Steps[0].Status != HopStepRunning || !cb.RequiredEffectUnsettled() {
		t.Fatalf("a search this plan already started is not replaced by the fetch: %#v", got.Steps[0])
	}
	if webSearchRecoveredByFetch(tool.PlanExecutionUnknown, true) || webSearchRecoveredByFetch(tool.PlanExecutionAwaitingReceipt, true) || webSearchRecoveredByFetch(tool.PlanExecutionRunning, true) {
		t.Fatal("an acquired search record stays the attempt")
	}
	if webSearchRecoveredByFetch(tool.PlanExecutionFailed, false) || webSearchRecoveredByFetch("", false) || !webSearchRecoveredByFetch(tool.PlanExecutionFailed, true) || !webSearchRecoveredByFetch("", true) {
		t.Fatal("fetch recovers a failed or unstarted search only when it is the plan's single required search")
	}
}

func TestHopRunFailedBaseSettlesWhenSiblingSucceeds(t *testing.T) {
	surface, store := newHopRepeatFamilySurface(t, "shell.execute.remote_host")
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-repeat", userText: "在远程主机执行命令"}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining != 1 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("failed base must keep the run open before any sibling succeeds: remaining=%d", snapshot.Remaining)
	}

	// The model retries through the next repeat sibling and the call lands.
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell-2", tool.PlanExecutionSucceeded, "digest-sibling", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	surface.completed["shell-2"] = true

	snapshot = cb.hopRunSnapshot()
	if snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("satisfied family must settle the run: remaining=%d", snapshot.Remaining)
	}
	base := snapshot.Steps[0]
	if base.Status != HopStepFailed || !base.Superseded {
		t.Fatalf("failed base should stay failed evidence but superseded: %#v", base)
	}
	if base.Evidence.State != tool.PlanExecutionFailed || base.Evidence.ReasonCode != "parameter_required" {
		t.Fatalf("superseded step lost its evidence: %#v", base.Evidence)
	}
	if snapshot.Steps[1].Status != HopStepCompleted || snapshot.Steps[1].Superseded {
		t.Fatalf("sibling step wrong: %#v", snapshot.Steps[1])
	}
	if got := hopMissingRequiredEffects(snapshot); len(got) != 0 {
		t.Fatalf("superseded step still listed as missing: %#v", got)
	}
	final := finalizeHopRun(snapshot, agent.LoopResult{})
	if final.Status != "completed" {
		t.Fatalf("final=%#v", final)
	}
}

func TestHopRunFailedBaseStaysOpenWhileSiblingUnsettled(t *testing.T) {
	surface, store := newHopRepeatFamilySurface(t, "shell.execute.remote_host")
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-repeat-pending", userText: "在远程主机执行命令"}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Sibling acquired but still in flight: the family is not settled.
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell-2"}); err != nil {
		t.Fatal(err)
	}
	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining != 1 || !cb.RequiredEffectUnsettled() {
		t.Fatalf("in-flight sibling must not settle the run: remaining=%d", snapshot.Remaining)
	}
	if snapshot.Steps[0].Superseded {
		t.Fatalf("base superseded before the family settled: %#v", snapshot.Steps[0])
	}
}

func TestDeliverableGapSettlesWhenFileFamilySiblingSucceeds(t *testing.T) {
	surface, store := newHopRepeatFamilySurface(t, "document.generate.file")
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell", tool.PlanExecutionFailed, "", "parameter_required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := cb.deliverableGapNames(false, ""); len(got) != 1 || got[0] != "document.generate.file" {
		t.Fatalf("failed base file producer should be a gap: %#v", got)
	}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "shell-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "shell-2", tool.PlanExecutionSucceeded, "digest-sibling", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	surface.completed["shell-2"] = true
	if got := cb.deliverableGapNames(false, ""); len(got) != 0 {
		t.Fatalf("satisfied family must clear the file gap: %#v", got)
	}
	if surface.completed["shell"] {
		t.Fatal("overlay mutated the surface completed map")
	}
}
