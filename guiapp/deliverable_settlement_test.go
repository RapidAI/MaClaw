package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestUnassignedFileDeliverableNames(t *testing.T) {
	sibling := tool.RepeatSiblingNeedID("need:gen", 1)
	plan := tool.ToolPlan{Selections: []tool.PlannedSelection{
		{ID: "gen", NeedID: "need:gen", FitProof: tool.FitProof{MatchedCapability: agentservice.CapabilityDocumentGenerate}},
		{ID: "office", NeedID: "need:office", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{ID: "search", NeedID: "need:search", FitProof: tool.FitProof{MatchedCapability: "web.search"}},
		{ID: "gen-sibling", NeedID: sibling, FitProof: tool.FitProof{MatchedCapability: agentservice.CapabilityDocumentGenerate}},
	}}

	got := unassignedFileDeliverableNames(plan, nil, nil)
	if len(got) != 2 || got[0] != "document.generate.file" || got[1] != "document.write.office" {
		t.Fatalf("names=%v", got)
	}

	optional := unassignedFileDeliverableNames(plan, nil, map[string]bool{"need:gen": true})
	if len(optional) != 1 || optional[0] != "document.write.office" {
		t.Fatalf("optional names=%v", optional)
	}

	done := unassignedFileDeliverableNames(plan, map[string]bool{"gen": true, "office": true}, nil)
	if len(done) != 0 {
		t.Fatalf("completed names=%v", done)
	}
	if unassignedFileDeliverableNames(tool.ToolPlan{}, nil, nil) != nil {
		t.Fatal("empty plan invented a deliverable")
	}
}

func TestDeliverableGapNamesFailsClosedWhenOptionalityIsUnknown(t *testing.T) {
	plan := tool.ToolPlan{Selections: []tool.PlannedSelection{
		{ID: "gen", NeedID: "need:gen", FitProof: tool.FitProof{MatchedCapability: agentservice.CapabilityDocumentGenerate}},
	}}
	cb := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{plan: plan}}
	got := cb.deliverableGapNames(false, "")
	if len(got) != 1 || got[0] != "document.generate.file" {
		t.Fatalf("unknown optionality opened the delivery gate: %#v", got)
	}
}
