package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Production shape, 2026-08-28 重庆 turn: a live_data+document_generate
// composite planned with same-topic conversation facts, so the parent surface
// carries no search grant; the model petitioned web_search, and the expansion
// re-plan — which deliberately runs without the turn's user text — could not
// re-derive the conversation-reuse drop and resurrected live_data's
// freshness=current leg. The strict-superset validator then rejected the
// whole child with "adds a need outside the petitioned label". The parent
// surface now records the drop in its replan input, and the expansion mirrors
// it for every lookup leg except the petitioned label's own templates.
func TestSemanticPetitionWebSearchOnReuseDroppedComposite(t *testing.T) {
	classification := liveDataGenerateClassification()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, classification.Primary)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h.app = app
	loopCtx := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "user-1", Platform: "desktop", Text: pengzhouWeatherPDFText(),
	}, nil, false, false)
	loopCtx.History = sameTopicPengzhouHistory()
	requestCtx, cancel := semanticRoutingContext(loopCtx)
	t.Cleanup(cancel)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		requestCtx, "user-1", pengzhouWeatherPDFText(), "desktop", "root-repro-cq", "turn-repro-cq", classification, nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("composite surface handled=%v err=%v", handled, err)
	}
	if planHasCapabilities(surface.plan, "information.search.web") {
		t.Fatalf("fixture must start with the lookup leg dropped by conversation reuse: %#v", surface.plan.Selections)
	}
	if !surface.replan.ConversationLookupReused {
		t.Fatal("parent replan input must record the conversation-reuse drop")
	}

	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop", loopCtx: loopCtx}
	granted, message := cb.PetitionToolCall("web_search")
	if !granted || !strings.Contains(message, "web_search") {
		t.Fatalf("web_search petition on a reuse-dropped composite must be granted: granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if child == surface {
		t.Fatal("petition must publish a child revision")
	}
	if !planHasCapabilities(child.plan, "document.generate.file", "artifact.deliver.current_channel", "information.search.web") {
		t.Fatalf("child plan=%#v", child.plan.Selections)
	}
	// The non-petitioned lookup leg (live_data's freshness=current search) must
	// stay dropped exactly as the parent had it: the expansion adds only the
	// petitioned label's template needs.
	for _, selection := range child.plan.Selections {
		if selection.FitProof.MatchedCapability == "information.search.web" &&
			!sameSemanticQualifiers(selection.FitProof.QualifierBindings, map[string]string{"freshness": "reference"}) {
			t.Fatalf("child revived a non-petitioned lookup leg: %#v", selection)
		}
	}
	if name := semanticGrantNameForAdapter(child, semanticTrustedWebSearchAdapter); name != "web_search" {
		t.Fatalf("child search grant name=%q, want web_search", name)
	}
	if !classificationHasLabel(child.replan.Classification, intent.LabelSearch) {
		t.Fatalf("child replan classification=%+v", child.replan.Classification)
	}
	// The child's lookup legs are published parent authority now; the reuse
	// record must not follow, or a later expansion could drop them again.
	if child.replan.ConversationLookupReused {
		t.Fatal("petitioned child must not inherit the reuse-drop record")
	}
	if child.replan.Attempts != surface.replan.Attempts {
		t.Fatalf("child attempts=%d, parent=%d", child.replan.Attempts, surface.replan.Attempts)
	}
}

// The validator's safety invariant is unchanged: a child that adds a need
// outside the petitioned label's templates is still rejected. This pins the
// rejection itself, independent of the reuse-mirroring fix above.
func TestSemanticPetitionExpansionStillRejectsOutsideLabel(t *testing.T) {
	parent := tool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: []tool.PlannedSelection{{
			NeedID: "need:document.generate.file:x", Phase: tool.PlanPhase("execution"),
			ParameterAuthorization: tool.ParameterAuthorization{CanonicalizerVer: "semantic-parameters-v1"},
			FitProof:               tool.FitProof{MatchedCapability: "document.generate.file"},
		}},
	}
	child := tool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: append(append([]tool.PlannedSelection(nil), parent.Selections...), tool.PlannedSelection{
			NeedID: "need:shell.execute.local:y", Phase: tool.PlanPhase("execution"),
			ParameterAuthorization: tool.ParameterAuthorization{CanonicalizerVer: "semantic-parameters-v1"},
			FitProof:               tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
		}),
	}
	if err := validateSemanticPetitionExpansion(parent, child, intent.LabelSearch); err == nil ||
		!strings.Contains(err.Error(), "outside the petitioned label") {
		t.Fatalf("a shell leg added by a search petition must stay rejected, err=%v", err)
	}
}
