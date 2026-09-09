package guiapp

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestGUIAndSrvSemanticBehaviorSnapshot runs the IM catalog (the GUI host
// registry) against the same frozen classifications as the agentservice
// snapshot. Overlapping required identities and Managed flags must match the
// reviewed headless resolver; catalog-specific office/launch qualifiers stay
// host-private.
func TestGUIAndSrvSemanticBehaviorSnapshot(t *testing.T) {
	imRegistry := newIMSemanticCapabilityRegistry()
	reviewedRegistry, err := agentservice.NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatalf("reviewed registry: %v", err)
	}
	for _, tc := range agentservice.SemanticBehaviorSnapshotCases() {
		imNeeds, imManaged, err := semanticNeedsFromClassification(imRegistry, tc.Result)
		if err != nil {
			t.Fatalf("%s IM resolve: %v", tc.Name, err)
		}
		srv := resolveGUIComparedReviewedNeeds(t, reviewedRegistry, tc.Result)
		if !agentservice.SemanticBehaviorRequiredIdentityComparable(tc.Result) {
			continue
		}
		if imManaged != srv.Managed {
			t.Errorf("%s managed drifted: im=%v srv=%v", tc.Name, imManaged, srv.Managed)
		}
		imReq := agentservice.SemanticNeedFamilyRequiredIdentities(agentservice.SnapshotSemanticNeedFamilies(imNeeds))
		srvReq := agentservice.SemanticNeedFamilyRequiredIdentities(agentservice.SnapshotSemanticNeedFamilies(srv.Needs))
		if !reflect.DeepEqual(imReq, srvReq) {
			t.Errorf("%s required identities drifted: im=%v srv=%v", tc.Name, imReq, srvReq)
		}
	}
}

func resolveGUIComparedReviewedNeeds(t *testing.T, registry *tool.CapabilityRegistry, result intent.ClassificationResult) agentservice.DynamicCapabilityNeedResolution {
	t.Helper()
	resolver := &agentservice.IntentLabelCapabilityNeedResolver{
		Classifier:        imSemanticIntentSource{result: result},
		Registry:          registry,
		Rules:             agentservice.ReviewedDynamicIntentCapabilityNeedRules(),
		MinimumConfidence: agentservice.ReviewedIntentMinimumConfidence,
		AmbientRetrieval:  true,
		ArchetypeBundles:  true,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), agentservice.DynamicCapabilityNeedRequest{UserText: "behavior-snapshot"})
	if err != nil {
		t.Fatalf("reviewed resolve %s: %v", result.Primary, err)
	}
	return resolution
}

func TestGUIAndSrvSemanticPlanSurfaceSnapshot(t *testing.T) {
	h := guiPlanSurfaceHandler(t)
	// Resolver Managed (not the plan-path hint floor) keeps this cohort aligned
	// with srv. Need IDs are SchemaDigest-stable, so the pre-plan resolve still
	// names unmet capabilities on the production plan.
	imRegistry := newIMSemanticCapabilityRegistry()
	got := make(map[string]agentservice.SemanticPlanSurfaceSnapshot, len(agentservice.SemanticBehaviorSnapshotCases()))
	managedByCase := make(map[string]bool, len(agentservice.SemanticBehaviorSnapshotCases()))
	for _, tc := range agentservice.SemanticBehaviorSnapshotCases() {
		result := tc.Result
		needs, managed, err := semanticNeedsFromClassification(imRegistry, result)
		if err != nil {
			t.Fatalf("%s IM needs: %v", tc.Name, err)
		}
		managedByCase[tc.Name] = managed
		if !managed || len(needs) == 0 {
			continue
		}
		prepared, handled, planErr := h.semanticPlanForTurnWithClassification("snapshot-user", "behavior-snapshot", "desktop", "snapshot-root", "snapshot-turn", &result)
		if !handled || prepared == nil {
			if planErr != nil {
				got[tc.Name] = agentservice.SemanticPlanSurfaceSnapshot{Unmet: []string{"plan|" + planFailureClass(planErr)}}
				continue
			}
			managedByCase[tc.Name] = false
			continue
		}
		got[tc.Name] = agentservice.SnapshotSemanticPlanSurface(prepared.plan, needs)
	}
	encoded := agentservice.EncodeSemanticPlanSurfaceSnapshot("im", managedByCase, got)
	if err := agentservice.SyncSnapshotFile(filepath.Join("testdata", "semantic_plan_surface_snapshot.txt"), encoded, agentservice.SemanticBehaviorSnapshotUpdateRequested()); err != nil {
		t.Fatal(err)
	}
	if err := agentservice.CheckPlanSurfaceFirstWaveParity("im", encoded, "srv", filepath.Join("..", "corelib", "agentservice", "testdata", "semantic_plan_surface_snapshot.txt")); err != nil {
		t.Error(err)
	}
}

func guiPlanSurfaceHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	pdfB64 := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%fake"))
	if err := h.registry.Register(RegisteredTool{
		Name: "generate_pdf", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"content": map[string]string{"type": "string"}},
		Required:    []string{"content"},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectLocalMutation},
		SemanticProduces: []tool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
		Handler:          func(map[string]interface{}) string { return "[file_base64|report.pdf|application/pdf]" + pdfB64 },
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func planFailureClass(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "unmet"):
		return "unmet"
	case strings.Contains(text, "unmapped"):
		return "unmapped"
	default:
		return "error"
	}
}
