package agentservice

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// Frozen classifications, not live UIC. The snapshot is the need-family face
// both hosts publish after the production resolver flags (ambient + archetype).
// Grant tokens and host-prefixed need IDs are out of the contract.
func TestGUIAndSrvSemanticBehaviorSnapshot(t *testing.T) {
	imRegistry := snapshotNeedRegistry(t, IMSemanticIntentCapabilityNeedRules())
	reviewedRegistry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatalf("reviewed registry: %v", err)
	}

	got := make(map[string]map[string]semanticHostSnap, len(SemanticBehaviorSnapshotCases()))
	for _, tc := range SemanticBehaviorSnapshotCases() {
		im := resolveSnapshotNeeds(t, imRegistry, IMSemanticIntentCapabilityNeedRules(), tc.Result)
		srv := resolveSnapshotNeeds(t, reviewedRegistry, ReviewedDynamicIntentCapabilityNeedRules(), tc.Result)
		imFamilies := SnapshotSemanticNeedFamilies(im.Needs)
		srvFamilies := SnapshotSemanticNeedFamilies(srv.Needs)
		got[tc.Name] = map[string]semanticHostSnap{
			"im":  {managed: im.Managed, lines: formatSnapshotFamilies(imFamilies)},
			"srv": {managed: srv.Managed, lines: formatSnapshotFamilies(srvFamilies)},
		}
		if SemanticBehaviorRequiredIdentityComparable(tc.Result) {
			imReq := SemanticNeedFamilyRequiredIdentities(imFamilies)
			srvReq := SemanticNeedFamilyRequiredIdentities(srvFamilies)
			if !reflect.DeepEqual(imReq, srvReq) {
				t.Errorf("%s required identities drifted: im=%v srv=%v", tc.Name, imReq, srvReq)
			}
		}
	}
	if err := SyncSnapshotFile(filepath.Join("testdata", "semantic_behavior_snapshot.txt"), encodeSemanticBehaviorSnapshot(got), SemanticBehaviorSnapshotUpdateRequested()); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticNeedFamilySnapshotIgnoresHostIDs(t *testing.T) {
	left := []coretool.CapabilityNeed{
		{ID: "need:information.search.web:aaaa", Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Polarity: coretool.NeedRequire, Required: true},
		{ID: "need:information.search.web:aaaa#02", Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Polarity: coretool.NeedRequire, Required: false},
	}
	right := []coretool.CapabilityNeed{
		{ID: "need:coding:information.search.web:bbbb", Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Required: true},
		{ID: "need:coding:information.search.web:bbbb#02", Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Required: false},
	}
	if got, want := formatSnapshotFamilies(SnapshotSemanticNeedFamilies(left)), formatSnapshotFamilies(SnapshotSemanticNeedFamilies(right)); !reflect.DeepEqual(got, want) {
		t.Fatalf("id-independent snapshot drifted: %v vs %v", got, want)
	}
}

func resolveSnapshotNeeds(t *testing.T, registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult) DynamicCapabilityNeedResolution {
	t.Helper()
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier:        fixedIntentClassificationSource{result: result},
		Registry:          registry,
		Rules:             rules,
		MinimumConfidence: ReviewedIntentMinimumConfidence,
		AmbientRetrieval:  true,
		ArchetypeBundles:  true,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "behavior-snapshot"})
	if err != nil {
		t.Fatalf("resolve %s: %v", result.Primary, err)
	}
	return resolution
}

func snapshotNeedRegistry(t *testing.T, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate) *coretool.CapabilityRegistry {
	t.Helper()
	registry := coretool.NewCapabilityRegistry("behavior-snapshot-im")
	seen := map[coretool.CapabilityID]bool{}
	register := func(id coretool.CapabilityID) {
		if seen[id] {
			return
		}
		seen[id] = true
		if err := registry.Register(coretool.CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}, Owner: "behavior-snapshot",
		}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	for _, templates := range rules {
		for _, template := range templates {
			register(template.Capability)
		}
	}
	register(coretool.CapabilityKnowledgeReadLocal)
	register(coretool.CapabilityMemoryRecallAgent)
	return registry
}

func formatSnapshotFamilies(families []SemanticNeedFamilySnapshot) []string {
	out := make([]string, 0, len(families))
	for _, family := range families {
		out = append(out, FormatSemanticNeedFamilySnapshot(family))
	}
	return out
}

type semanticHostSnap struct {
	managed bool
	lines   []string
}

func TestGUIAndSrvSemanticPlanSurfaceSnapshot(t *testing.T) {
	reviewedRegistry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatalf("reviewed registry: %v", err)
	}
	planner := newReviewedSnapshotPlanner(t, fullyWiredSharedCatalogCallbacks(t), reviewedRegistry)
	got := make(map[string]SemanticPlanSurfaceSnapshot, len(SemanticBehaviorSnapshotCases()))
	managedByCase := make(map[string]bool, len(SemanticBehaviorSnapshotCases()))
	for _, tc := range SemanticBehaviorSnapshotCases() {
		resolution := resolveSnapshotNeeds(t, reviewedRegistry, ReviewedDynamicIntentCapabilityNeedRules(), tc.Result)
		managedByCase[tc.Name] = resolution.Managed
		if !resolution.Managed || len(resolution.Needs) == 0 {
			continue
		}
		got[tc.Name] = SnapshotSemanticPlanSurface(planner.plan(t, resolution.Needs, resolution.Facts), resolution.Needs)
	}
	encoded := EncodeSemanticPlanSurfaceSnapshot("srv", managedByCase, got)
	if err := SyncSnapshotFile(filepath.Join("testdata", "semantic_plan_surface_snapshot.txt"), encoded, SemanticBehaviorSnapshotUpdateRequested()); err != nil {
		t.Fatal(err)
	}
	if err := CheckPlanSurfaceFirstWaveParity("srv", encoded, "im", filepath.Join("..", "..", "guiapp", "testdata", "semantic_plan_surface_snapshot.txt")); err != nil {
		t.Error(err)
	}
}

func TestSnapshotSemanticPlanSurfaceCollapsesRepeatSiblings(t *testing.T) {
	base := "need:information.search.web:aaaa"
	plan := coretool.ToolPlan{
		Selections: []coretool.PlannedSelection{
			{ID: "selection:" + base, NeedID: base, AdapterName: "host_information_search_web", Phase: coretool.PlanPhaseExecution, FitProof: coretool.FitProof{MatchedCapability: CapabilityInformationSearchWeb, QualifierBindings: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}}},
			{ID: "selection:" + coretool.RepeatSiblingNeedID(base, 1), NeedID: coretool.RepeatSiblingNeedID(base, 1), AdapterName: "host_information_search_web", Phase: coretool.PlanPhaseExecution, FitProof: coretool.FitProof{MatchedCapability: CapabilityInformationSearchWeb, QualifierBindings: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}}},
		},
		Unmet: []coretool.UnmetNeed{{NeedID: coretool.RepeatSiblingNeedID("need:artifact.deliver.current_channel:bbbb", 1), ReasonCode: "no_feasible_provider"}},
	}
	needs := []coretool.CapabilityNeed{
		{ID: base, Capability: CapabilityInformationSearchWeb},
		{ID: "need:artifact.deliver.current_channel:bbbb", Capability: CapabilityArtifactDeliverCurrent},
	}
	got := SnapshotSemanticPlanSurface(plan, needs)
	if len(got.Selections) != 1 || got.Selections[0] != "information.search.web|freshness=reference|host_information_search_web|execution|n=2" {
		t.Fatalf("selections=%v", got.Selections)
	}
	if len(got.FirstWave) != 1 || got.FirstWave[0] != "information.search.web|freshness=reference|host_information_search_web" {
		t.Fatalf("first wave must expose one family at a time, got %v", got.FirstWave)
	}
	if len(got.Unmet) != 1 || got.Unmet[0] != "artifact.deliver.current_channel|no_feasible_provider" {
		t.Fatalf("unmet=%v", got.Unmet)
	}
}

func TestSemanticNeedFamilyRequiredIdentitiesKeepsOverlappingOfficeLiveData(t *testing.T) {
	got := SemanticNeedFamilyRequiredIdentities([]SemanticNeedFamilySnapshot{
		{Capability: string(coretool.CapabilityDocumentWriteOffice), Polarity: string(coretool.NeedRequire), Required: 1},
		{Capability: string(CapabilityInformationSearchWeb), Polarity: string(coretool.NeedRequire), Qualifiers: "freshness=current", Required: 1},
	})
	if len(got) != 1 || got[0] != "information.search.web|require|freshness=current" {
		t.Fatalf("shared required identities=%v, want live_data search only", got)
	}
	if !SemanticBehaviorRequiredIdentityComparable(intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: 0.98,
	}) {
		t.Fatal("office+live_data must still compare overlapping required identities")
	}
}

func TestPlanSurfaceFirstWaveIdentitiesStripsAdapterAndCatalogSpecific(t *testing.T) {
	got := PlanSurfaceFirstWaveIdentities([]string{
		"information.search.web|freshness=reference|host_a|n=2",
		"document.write.office|-|office_adapter",
		"information.search.web|freshness=reference|host_b",
		"information.fetch.web|-|host_fetch",
	})
	want := []string{"information.fetch.web|-", "information.search.web|freshness=reference"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first-wave identities=%v want %v", got, want)
	}
}

func TestPlanSurfaceFirstWaveParityErrorsSkipsEmptySelections(t *testing.T) {
	managed := map[string]bool{}
	for _, tc := range SemanticBehaviorSnapshotCases() {
		managed[tc.Name] = false
	}
	managed["search"] = true
	managed["screenshot"] = true
	managed["office_live_data"] = true
	im := EncodeSemanticPlanSurfaceSnapshot("im", managed, map[string]SemanticPlanSurfaceSnapshot{
		"search": {
			Selections: []string{"information.search.web|freshness=reference|im_search|execution"},
			FirstWave:  []string{"information.search.web|freshness=reference|im_search"},
		},
		"office_live_data": {
			Selections: []string{
				"document.write.office|-|im_office|execution",
				"information.search.web|freshness=current|im_search|execution",
			},
			FirstWave: []string{
				"document.write.office|-|im_office",
				"information.search.web|freshness=current|im_search",
			},
		},
		"screenshot": {Unmet: []string{"visual.capture.desktop|catalog_incomplete"}},
	})
	srv := EncodeSemanticPlanSurfaceSnapshot("srv", managed, map[string]SemanticPlanSurfaceSnapshot{
		"search": {
			Selections: []string{"information.search.web|freshness=reference|host_search|execution"},
			FirstWave:  []string{"information.search.web|freshness=reference|host_search"},
		},
		"office_live_data": {
			Selections: []string{
				"document.write.office|format=spreadsheet|host_office|execution",
				"information.search.web|freshness=current|host_search|execution",
			},
			FirstWave: []string{
				"document.write.office|format=spreadsheet|host_office",
				"information.search.web|freshness=current|host_search",
			},
		},
		"screenshot": {Unmet: []string{"visual.capture.desktop|no_feasible_provider"}},
	})
	if errs := PlanSurfaceFirstWaveParityErrors("im", im, "srv", srv); len(errs) != 0 {
		t.Fatalf("matching first-wave identities reported drift: %v", errs)
	}
	srvDrift := EncodeSemanticPlanSurfaceSnapshot("srv", managed, map[string]SemanticPlanSurfaceSnapshot{
		"search": {
			Selections: []string{"information.search.web|freshness=current|host_search|execution"},
			FirstWave:  []string{"information.search.web|freshness=current|host_search"},
		},
		"office_live_data": {
			Selections: []string{"information.search.web|freshness=current|host_search|execution"},
			FirstWave:  []string{"information.search.web|freshness=current|host_search"},
		},
		"screenshot": {Unmet: []string{"visual.capture.desktop|no_feasible_provider"}},
	})
	if errs := PlanSurfaceFirstWaveParityErrors("im", im, "srv", srvDrift); len(errs) != 1 || !strings.Contains(errs[0], "search first-wave identities") {
		t.Fatalf("want search identity drift, got %v", errs)
	}
}

func TestPlanSurfaceFirstWaveParityErrorsRejectsVacuousPeer(t *testing.T) {
	managed := map[string]bool{}
	for _, tc := range SemanticBehaviorSnapshotCases() {
		managed[tc.Name] = false
	}
	managed["search"] = true
	left := EncodeSemanticPlanSurfaceSnapshot("im", managed, map[string]SemanticPlanSurfaceSnapshot{
		"search": {
			Selections: []string{"information.search.web|freshness=reference|im_search|execution"},
			FirstWave:  []string{"information.search.web|freshness=reference|im_search"},
		},
	})
	right := EncodeSemanticPlanSurfaceSnapshot("srv", map[string]bool{}, nil)
	errs := PlanSurfaceFirstWaveParityErrors("im", left, "srv", right)
	if len(errs) != 1 || !strings.Contains(errs[0], "no overlapping first-wave cases compared") {
		t.Fatalf("wrong-host/empty peer must not pass vacuously, got %v", errs)
	}
}

func TestSnapshotSemanticPlanSurfaceMapsMintedNeedIDsWithoutNeeds(t *testing.T) {
	plan := coretool.ToolPlan{
		Unmet: []coretool.UnmetNeed{{NeedID: "need:artifact.deliver.current_channel:0123456789ab#02", ReasonCode: "no_feasible_provider"}},
	}
	got := SnapshotSemanticPlanSurface(plan, nil)
	if len(got.Unmet) != 1 || got.Unmet[0] != "artifact.deliver.current_channel|no_feasible_provider" {
		t.Fatalf("unmet=%v", got.Unmet)
	}
	coding := SnapshotSemanticPlanSurface(coretool.ToolPlan{
		Unmet: []coretool.UnmetNeed{{NeedID: "need:coding:fs.read.local:0123456789ab", ReasonCode: "policy_denied"}},
	}, nil)
	if len(coding.Unmet) != 1 || coding.Unmet[0] != "fs.read.local|policy_denied" {
		t.Fatalf("coding unmet=%v", coding.Unmet)
	}
}

func TestSyncSnapshotFileDiffsAndUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.txt")
	if err := SyncSnapshotFile(path, []byte("alpha\n"), true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := SyncSnapshotFile(path, []byte("alpha\n"), true); err != nil {
		t.Fatalf("identical update: %v", err)
	}
	if err := SyncSnapshotFile(path, []byte("alpha\n"), false); err != nil {
		t.Fatalf("match: %v", err)
	}
	if err := SyncSnapshotFile(path, []byte("beta\n"), false); err == nil {
		t.Fatal("drift must fail closed")
	}
}

type reviewedSnapshotPlanner struct {
	registry *coretool.CapabilityRegistry
	snapshot coretool.ToolCatalogSnapshot
	now      time.Time
}

func newReviewedSnapshotPlanner(t *testing.T, cb *coreAgentCallbacks, registry *coretool.CapabilityRegistry) *reviewedSnapshotPlanner {
	t.Helper()
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	// Host-owned providers only. Observing an empty MCP/Skill inventory would
	// stamp catalog_incomplete onto every missing provider, including ones this
	// host never attaches (current-channel deliver without a destination).
	catalog, _, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, cb.reviewedHostOwnedServices())
	if err != nil {
		t.Fatalf("prepare reviewed catalog: %v", err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, coretool.CatalogCoverage{State: coretool.CatalogCoverageComplete}, now)
	if err != nil {
		t.Fatalf("publish catalog: %v", err)
	}
	return &reviewedSnapshotPlanner{registry: registry, snapshot: snapshot, now: now}
}

func (p *reviewedSnapshotPlanner) plan(t *testing.T, needs []coretool.CapabilityNeed, facts []coretool.RoutingFact) coretool.ToolPlan {
	t.Helper()
	plan, err := coretool.NewToolPlanner(p.registry).Plan(coretool.RouteRequest{
		RootTaskID: "snapshot-root", SessionID: "snapshot-session", TurnID: "snapshot-turn",
		ChannelScope: "core-agent", Snapshot: p.snapshot, Needs: needs, Facts: facts, Now: p.now,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

func encodeSemanticBehaviorSnapshot(got map[string]map[string]semanticHostSnap) []byte {
	var b strings.Builder
	b.WriteString("# GUI IM vs reviewed headless need-family snapshot.\n")
	b.WriteString("# Host-prefixed need IDs and grant tokens are omitted.\n")
	b.WriteString("# Update with " + SemanticBehaviorSnapshotUpdateEnv + "=1 after reviewing a face change.\n")
	hosts := []string{"im", "srv"}
	for _, tc := range SemanticBehaviorSnapshotCases() {
		for _, host := range hosts {
			snap := got[tc.Name][host]
			managed := "unmanaged"
			if snap.managed {
				managed = "managed"
			}
			if len(snap.lines) == 0 {
				fmt.Fprintf(&b, "%s\t%s\t%s\n", host, tc.Name, managed)
				continue
			}
			for _, line := range snap.lines {
				fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", host, tc.Name, managed, line)
			}
		}
	}
	return []byte(b.String())
}
