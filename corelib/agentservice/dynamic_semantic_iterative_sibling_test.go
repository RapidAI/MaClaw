package agentservice

import (
	"strings"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestWithSpentBudgetNoteListsSettledIterativeSiblings(t *testing.T) {
	remoteBase := "need:shell.execute.remote_host:abc123def456"
	remoteID := "selection:" + remoteBase
	surface := &coreDynamicSemanticSurface{
		plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
			ID: remoteID, NeedID: remoteBase, Continuation: true,
			FitProof: coretool.FitProof{MatchedCapability: coretool.CapabilityShellExecuteRemoteHost},
		}}},
		issued:    map[string]bool{remoteID: true},
		completed: map[string]bool{},
		grants:    map[string]coretool.InvocationGrant{},
	}
	failed := surface.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "trusted_ssh_timeout"}, remoteID)
	if failed.Result != "trusted_ssh_timeout" {
		t.Fatalf("settled ssh narrated a limit: %q", failed.Result)
	}
	if len(surface.plan.Selections) != 2 || surface.plan.Selections[1].Continuation {
		t.Fatalf("remote selections=%d continuation=%v", len(surface.plan.Selections), len(surface.plan.Selections) == 2 && surface.plan.Selections[1].Continuation)
	}
	again := surface.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "trusted_ssh_timeout"}, remoteID)
	if again.Result != "trusted_ssh_timeout" || len(surface.plan.Selections) != 2 {
		t.Fatalf("unissued sibling grew again: %q selections=%d", again.Result, len(surface.plan.Selections))
	}

	unknown := &coreDynamicSemanticSurface{
		plan:      surface.plan,
		issued:    map[string]bool{remoteID: true},
		completed: map[string]bool{},
		grants:    map[string]coretool.InvocationGrant{},
	}
	// Copy only the spent node so this case starts from one command.
	unknown.plan.Selections = []coretool.PlannedSelection{surface.plan.Selections[0]}
	lost := unknown.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "lost", Unknown: true}, remoteID)
	if lost.Result != "lost" || len(unknown.plan.Selections) != 1 {
		t.Fatalf("unknown outcome listed another command: %q selections=%d", lost.Result, len(unknown.plan.Selections))
	}
	waiting := unknown.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "pending", AwaitingReceipt: true}, remoteID)
	if waiting.Result != "pending" || len(unknown.plan.Selections) != 1 {
		t.Fatalf("awaiting receipt listed another command: %q selections=%d", waiting.Result, len(unknown.plan.Selections))
	}

	downloadBase := "need:artifact.acquire.remote:abc"
	firstID := "selection:" + downloadBase
	secondNeed := coretool.RepeatSiblingNeedID(downloadBase, 1)
	secondID := "selection:" + secondNeed
	download := &coreDynamicSemanticSurface{
		plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{
			{ID: firstID, NeedID: downloadBase, FitProof: coretool.FitProof{MatchedCapability: coretool.CapabilityArtifactAcquireRemote}},
			{ID: secondID, NeedID: secondNeed, FitProof: coretool.FitProof{MatchedCapability: coretool.CapabilityArtifactAcquireRemote}},
		}},
		issued:    map[string]bool{firstID: true, secondID: true},
		completed: map[string]bool{firstID: true, secondID: true},
		grants:    map[string]coretool.InvocationGrant{},
	}
	noted := download.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "saved", Succeeded: true}, secondID)
	if len(download.plan.Selections) != 2 || !strings.Contains(noted.Result, coretool.RepeatWaveListedMarker) {
		t.Fatalf("download selections=%d result=%q", len(download.plan.Selections), noted.Result)
	}

	writeBase := "need:fs.write.local:abc123def456"
	writeID := "selection:" + writeBase
	write := &coreDynamicSemanticSurface{
		plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
			ID: writeID, NeedID: writeBase,
			FitProof: coretool.FitProof{MatchedCapability: coretool.CapabilityFSWriteLocal},
		}}},
		issued:    map[string]bool{writeID: true},
		completed: map[string]bool{writeID: true},
		grants:    map[string]coretool.InvocationGrant{},
	}
	wrote := write.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "wrote", Succeeded: true}, writeID)
	if wrote.Result != "wrote" || len(write.plan.Selections) != 2 || write.plan.Selections[1].Continuation {
		t.Fatalf("file write result=%q selections=%d", wrote.Result, len(write.plan.Selections))
	}

	companionBase := "need:zz-baseline:shell.execute.remote_host:abc123def456"
	companionID := "selection:" + companionBase
	companion := &coreDynamicSemanticSurface{
		plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
			ID: companionID, NeedID: companionBase,
			EvidenceIDs: []string{"intent:baseline_workspace"},
			FitProof:    coretool.FitProof{MatchedCapability: coretool.CapabilityShellExecuteRemoteHost},
		}}},
		issued:    map[string]bool{companionID: true},
		completed: map[string]bool{companionID: true},
		grants:    map[string]coretool.InvocationGrant{},
	}
	kept := companion.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "ok", Succeeded: true}, companionID)
	if kept.Result != "ok" || len(companion.plan.Selections) != 1 {
		t.Fatalf("companion remote grew: %q selections=%d", kept.Result, len(companion.plan.Selections))
	}

	stale := &coreDynamicSemanticSurface{
		plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
			ID: remoteID, NeedID: remoteBase,
			FitProof: coretool.FitProof{MatchedCapability: coretool.CapabilityShellExecuteRemoteHost},
		}}},
		issued: map[string]bool{remoteID: true},
		grants: map[string]coretool.InvocationGrant{},
	}
	if got := stale.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}, remoteID); len(stale.plan.Selections) != 1 || strings.Contains(got.Result, coretool.RepeatWaveListedMarker) {
		t.Fatalf("binding recovery grew a copy of the dead binding: %q selections=%d", got.Result, len(stale.plan.Selections))
	}
	if got := stale.withSpentBudgetNote(coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_execution_cancelled", ReasonCode: "dynamic_execution_cancelled"}, remoteID); len(stale.plan.Selections) != 1 {
		t.Fatalf("cancellation listed another command: %q selections=%d", got.Result, len(stale.plan.Selections))
	}
}
