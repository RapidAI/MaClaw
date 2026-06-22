package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// --- Mock store for VersionManager tests ---

type mockVersionStore struct {
	workflows map[string]*WorkflowDefinition
	versions  map[string]*WorkflowVersion
}

func newMockVersionStore() *mockVersionStore {
	return &mockVersionStore{
		workflows: make(map[string]*WorkflowDefinition),
		versions:  make(map[string]*WorkflowVersion),
	}
}

func (m *mockVersionStore) CreateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	m.workflows[def.ID] = def
	return nil
}

func (m *mockVersionStore) GetWorkflow(_ context.Context, id string) (*WorkflowDefinition, error) {
	return m.workflows[id], nil
}

func (m *mockVersionStore) ListWorkflows(_ context.Context, ownerID string) ([]WorkflowDefinition, error) {
	var result []WorkflowDefinition
	for _, w := range m.workflows {
		if w.OwnerID == ownerID {
			result = append(result, *w)
		}
	}
	return result, nil
}

func (m *mockVersionStore) CreateVersion(_ context.Context, ver *WorkflowVersion) error {
	m.versions[ver.ID] = ver
	return nil
}

func (m *mockVersionStore) UpdateVersion(_ context.Context, ver *WorkflowVersion) error {
	existing, ok := m.versions[ver.ID]
	if !ok {
		return nil
	}
	existing.Graph = ver.Graph
	existing.VersionNumber = ver.VersionNumber
	existing.UpdatedAt = ver.UpdatedAt
	return nil
}

func (m *mockVersionStore) GetVersion(_ context.Context, id string) (*WorkflowVersion, error) {
	return m.versions[id], nil
}

func (m *mockVersionStore) GetPublishedVersion(_ context.Context, workflowID string) (*WorkflowVersion, error) {
	for _, v := range m.versions {
		if v.WorkflowID == workflowID && v.Status == VersionPublished {
			return v, nil
		}
	}
	return nil, nil
}

func (m *mockVersionStore) UpdateVersionStatus(_ context.Context, id string, status VersionStatus, reason string) error {
	v, ok := m.versions[id]
	if !ok {
		return nil
	}
	v.Status = status
	if reason != "" {
		v.RejectionReason = reason
	}
	return nil
}

func (m *mockVersionStore) ListVersions(_ context.Context, workflowID string) ([]WorkflowVersion, error) {
	var result []WorkflowVersion
	for _, v := range m.versions {
		if v.WorkflowID == workflowID {
			result = append(result, *v)
		}
	}
	return result, nil
}

func (m *mockVersionStore) ListPendingReviews(_ context.Context, _, _ int) ([]WorkflowVersion, int, error) {
	var result []WorkflowVersion
	for _, v := range m.versions {
		if v.Status == VersionPendingReview {
			result = append(result, *v)
		}
	}
	return result, len(result), nil
}

// --- Helper to build a valid workflow graph ---

func validGraph() WorkflowGraph {
	return WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger_1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval_1", Type: NodeApproval, Label: "Approve"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger_1", TargetID: "approval_1"},
		},
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return body
}

// --- Tests ---

func TestSaveDraft_FirstVersion(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("SaveDraft failed: %v", err)
	}
	if ver.VersionNumber != "0.1.0" {
		t.Errorf("first version = %q, want %q", ver.VersionNumber, "0.1.0")
	}
	if ver.Status != VersionDraft {
		t.Errorf("status = %q, want %q", ver.Status, VersionDraft)
	}
	if ver.WorkflowID != "wf_1" {
		t.Errorf("workflow_id = %q, want %q", ver.WorkflowID, "wf_1")
	}
}

func TestSaveDraft_IncrementsPatchOnExistingDraft(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Create first draft
	ver1, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("first SaveDraft failed: %v", err)
	}
	if ver1.VersionNumber != "0.1.0" {
		t.Fatalf("first version = %q, want %q", ver1.VersionNumber, "0.1.0")
	}

	// Save again — should increment patch
	ver2, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("second SaveDraft failed: %v", err)
	}
	if ver2.VersionNumber != "0.1.1" {
		t.Errorf("second version = %q, want %q", ver2.VersionNumber, "0.1.1")
	}
}

// TestSaveDraft_UpdateBranchDoesNotCreateNewRow asserts that re-saving an
// existing draft updates it in place (via WorkflowStore.UpdateVersion) rather
// than creating a new version row. This is the fix for the SaveDraft
// "update existing draft" branch (2.9): the version-row count must not grow,
// and the same row's graph + version number must be updated.
func TestSaveDraft_UpdateBranchDoesNotCreateNewRow(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// First SaveDraft creates the initial draft (0.1.0).
	ver1, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("first SaveDraft failed: %v", err)
	}
	if got := len(store.versions); got != 1 {
		t.Fatalf("after first SaveDraft: version-row count = %d, want 1", got)
	}

	// Second SaveDraft hits the "update existing draft" branch with a changed
	// graph. It must update in place — NOT create a new row.
	updatedGraph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger_1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval_1", Type: NodeApproval, Label: "Review (updated)"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger_1", TargetID: "approval_1"},
		},
	}
	ver2, err := vm.SaveDraft(ctx, "wf_1", updatedGraph)
	if err != nil {
		t.Fatalf("second SaveDraft failed: %v", err)
	}

	if got := len(store.versions); got != 1 {
		t.Fatalf("after update branch: version-row count = %d, want 1 (update branch must not create a new row)", got)
	}

	// The updated row must carry the bumped version number and the new graph.
	if ver2.VersionNumber != "0.1.1" {
		t.Errorf("updated version = %q, want %q", ver2.VersionNumber, "0.1.1")
	}
	stored := store.versions[ver1.ID]
	if stored == nil {
		t.Fatalf("original draft row %q should still exist after update", ver1.ID)
	}
	if stored.VersionNumber != "0.1.1" {
		t.Errorf("stored version number = %q, want %q (updated in place)", stored.VersionNumber, "0.1.1")
	}
	if stored.Status != VersionDraft {
		t.Errorf("stored status = %q, want %q (stays draft)", stored.Status, VersionDraft)
	}
	if len(stored.Graph.Nodes) != 2 || stored.Graph.Nodes[1].Label != "Review (updated)" {
		t.Errorf("stored graph not updated in place: %+v", stored.Graph.Nodes)
	}
}

func TestSaveDraft_IncrementsMinorAfterNonDraft(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Create and publish a version
	ver1, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, ver1.ID)
	_ = vm.Approve(ctx, ver1.ID)

	// Save new draft — should increment minor from published version
	ver2, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("SaveDraft after publish failed: %v", err)
	}
	if ver2.VersionNumber != "0.2.0" {
		t.Errorf("version after publish = %q, want %q", ver2.VersionNumber, "0.2.0")
	}
}

func TestSubmitForReview_ValidGraph(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	err := vm.SubmitForReview(ctx, ver.ID)
	if err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	updated := store.versions[ver.ID]
	if updated.Status != VersionPendingReview {
		t.Errorf("status = %q, want %q", updated.Status, VersionPendingReview)
	}
}

func TestSubmitForReview_RejectsNonDraft(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID) // now pending_review

	err := vm.SubmitForReview(ctx, ver.ID)
	if err == nil {
		t.Fatal("expected error for non-draft submission")
	}
	if err != ErrVersionNotDraft {
		t.Errorf("error = %v, want ErrVersionNotDraft", err)
	}
}

func TestSubmitForReview_RejectsInvalidGraph_NoNodes(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	emptyGraph := WorkflowGraph{Nodes: nil, Edges: nil}
	ver, _ := vm.SaveDraft(ctx, "wf_1", emptyGraph)

	err := vm.SubmitForReview(ctx, ver.ID)
	if err == nil {
		t.Fatal("expected error for empty graph")
	}
}

func TestSubmitForReview_RejectsInvalidGraph_NoTrigger(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	noTriggerGraph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "a1", Type: NodeApproval, Label: "Approve"},
		},
	}
	ver, _ := vm.SaveDraft(ctx, "wf_1", noTriggerGraph)

	err := vm.SubmitForReview(ctx, ver.ID)
	if err == nil {
		t.Fatal("expected error for graph without trigger")
	}
}

func TestSubmitForReview_RejectsDisconnectedNodes(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	disconnectedGraph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger_1", Type: NodeTrigger, Label: "Start"},
			{ID: "approval_1", Type: NodeApproval, Label: "Approve"},
			{ID: "orphan_1", Type: NodeAction, Label: "Orphan"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "trigger_1", TargetID: "approval_1"},
			// orphan_1 has no incoming edge from trigger path
		},
	}
	ver, _ := vm.SaveDraft(ctx, "wf_1", disconnectedGraph)

	err := vm.SubmitForReview(ctx, ver.ID)
	if err == nil {
		t.Fatal("expected error for disconnected nodes")
	}
}

func TestApprove_SupersedesPreviousPublished(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Create and publish v1
	v1, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, v1.ID)
	_ = vm.Approve(ctx, v1.ID)

	if store.versions[v1.ID].Status != VersionPublished {
		t.Fatalf("v1 should be published")
	}

	// Create and publish v2
	v2, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, v2.ID)
	_ = vm.Approve(ctx, v2.ID)

	// v1 should be superseded
	if store.versions[v1.ID].Status != VersionSuperseded {
		t.Errorf("v1 status = %q, want %q", store.versions[v1.ID].Status, VersionSuperseded)
	}
	if store.versions[v2.ID].Status != VersionPublished {
		t.Errorf("v2 status = %q, want %q", store.versions[v2.ID].Status, VersionPublished)
	}
}

func TestReject_SetsReasonAndStatus(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	reason := "Missing approval node configuration"
	err := vm.Reject(ctx, ver.ID, reason)
	if err != nil {
		t.Fatalf("Reject failed: %v", err)
	}

	updated := store.versions[ver.ID]
	if updated.Status != VersionRejected {
		t.Errorf("status = %q, want %q", updated.Status, VersionRejected)
	}
	if updated.RejectionReason != reason {
		t.Errorf("reason = %q, want %q", updated.RejectionReason, reason)
	}
}

func TestUnpublish(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)
	_ = vm.Approve(ctx, ver.ID)

	err := vm.Unpublish(ctx, ver.ID)
	if err != nil {
		t.Fatalf("Unpublish failed: %v", err)
	}

	if store.versions[ver.ID].Status != VersionUnpublished {
		t.Errorf("status = %q, want %q", store.versions[ver.ID].Status, VersionUnpublished)
	}
}

func TestUnpublish_RejectsNonPublished(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())

	err := vm.Unpublish(ctx, ver.ID)
	if err != ErrVersionNotPublished {
		t.Errorf("error = %v, want ErrVersionNotPublished", err)
	}
}

func TestCreateDraftFromPublished(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Publish a version
	v1, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, v1.ID)
	_ = vm.Approve(ctx, v1.ID)

	// Create draft from published
	draft, err := vm.CreateDraftFromPublished(ctx, "wf_1")
	if err != nil {
		t.Fatalf("CreateDraftFromPublished failed: %v", err)
	}
	if draft.Status != VersionDraft {
		t.Errorf("status = %q, want %q", draft.Status, VersionDraft)
	}
	if draft.VersionNumber != "0.2.0" {
		t.Errorf("version = %q, want %q", draft.VersionNumber, "0.2.0")
	}
	// Graph should be copied from published
	if len(draft.Graph.Nodes) != len(validGraph().Nodes) {
		t.Errorf("graph nodes = %d, want %d", len(draft.Graph.Nodes), len(validGraph().Nodes))
	}
}

func TestCreateDraftFromPublished_NoPublishedVersion(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	_, err := vm.CreateDraftFromPublished(ctx, "wf_1")
	if err == nil {
		t.Fatal("expected error when no published version exists")
	}
}

func TestWithdrawReview(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	ver, _ := vm.SaveDraft(ctx, "wf_1", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	err := vm.WithdrawReview(ctx, ver.ID)
	if err != nil {
		t.Fatalf("WithdrawReview failed: %v", err)
	}

	if store.versions[ver.ID].Status != VersionDraft {
		t.Errorf("status = %q, want %q", store.versions[ver.ID].Status, VersionDraft)
	}
}

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		from    VersionStatus
		to      VersionStatus
		allowed bool
	}{
		{VersionDraft, VersionPendingReview, true},
		{VersionDraft, VersionPublished, false},
		{VersionPendingReview, VersionPublished, true},
		{VersionPendingReview, VersionRejected, true},
		{VersionPendingReview, VersionDraft, true}, // withdrawal
		{VersionPublished, VersionSuperseded, true},
		{VersionPublished, VersionUnpublished, true},
		{VersionPublished, VersionDraft, false},
		{VersionRejected, VersionDraft, false},    // terminal
		{VersionSuperseded, VersionDraft, false},  // terminal
		{VersionUnpublished, VersionDraft, false}, // terminal
	}

	for _, tt := range tests {
		name := string(tt.from) + " → " + string(tt.to)
		t.Run(name, func(t *testing.T) {
			got := IsValidTransition(tt.from, tt.to)
			if got != tt.allowed {
				t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.allowed)
			}
		})
	}
}

func TestVersionNumberHelpers(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(string) string
		input  string
		expect string
	}{
		{"incrementPatch 0.1.0", incrementPatch, "0.1.0", "0.1.1"},
		{"incrementPatch 1.2.3", incrementPatch, "1.2.3", "1.2.4"},
		{"incrementPatch 0.0.9", incrementPatch, "0.0.9", "0.0.10"},
		{"incrementMinor 0.1.0", incrementMinor, "0.1.0", "0.2.0"},
		{"incrementMinor 1.2.3", incrementMinor, "1.2.3", "1.3.0"},
		{"incrementMinor 0.9.5", incrementMinor, "0.9.5", "0.10.0"},
		{"incrementPatch invalid", incrementPatch, "bad", "0.1.0"},
		{"incrementMinor invalid", incrementMinor, "bad", "0.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.input)
			if got != tt.expect {
				t.Errorf("%s(%q) = %q, want %q", tt.name, tt.input, got, tt.expect)
			}
		})
	}
}

func TestValidateGraphStructure_ValidGraph(t *testing.T) {
	err := ValidateGraphStructure(validGraph())
	if err != nil {
		t.Errorf("ValidateGraphStructure(validGraph) = %v, want nil", err)
	}
}

func TestValidateGraphStructure_EmptyGraph(t *testing.T) {
	err := ValidateGraphStructure(WorkflowGraph{})
	if err != ErrNoNodes {
		t.Errorf("error = %v, want ErrNoNodes", err)
	}
}

func TestValidateGraphStructure_MultipleTriggers(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start 1"},
			{ID: "t2", Type: NodeTrigger, Label: "Start 2"},
		},
	}
	err := ValidateGraphStructure(graph)
	if err != ErrMultipleTriggers {
		t.Errorf("error = %v, want ErrMultipleTriggers", err)
	}
}

func TestValidateGraphStructure_SingleNodeTrigger(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
		},
	}
	err := ValidateGraphStructure(graph)
	if err != nil {
		t.Errorf("single trigger node should be valid, got: %v", err)
	}
}

func TestValidateGraphStructure_RejectsIncomingEdgeToTrigger(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "a1", Type: NodeApproval, Label: "Approve"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "a1"},
			{ID: "e2", SourceID: "a1", TargetID: "t1"},
		},
	}
	err := ValidateGraphStructure(graph)
	if err != ErrTriggerHasIncoming {
		t.Errorf("error = %v, want ErrTriggerHasIncoming", err)
	}
}

func TestValidateGraphStructure_RejectsOutgoingEdgeFromTerminal(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "done", Type: NodeTypeTerminal, Label: "Done"},
			{ID: "a1", Type: NodeApproval, Label: "Approve"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "done"},
			{ID: "e2", SourceID: "done", TargetID: "a1"},
		},
	}
	err := ValidateGraphStructure(graph)
	if err != ErrTerminalHasOutgoing {
		t.Errorf("error = %v, want ErrTerminalHasOutgoing", err)
	}
}

func TestValidateGraphStructure_AcceptsConditionBranchRoutes(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "cond", Type: NodeConditionBranch, Label: "Check", Config: mustRawJSON(t, ConditionBranchConfig{
				Branches: []BranchCondition{{
					TargetNodeID: "approve",
					Expression:   ConditionExpr{Field: "amount", Operator: "greater_than", Value: 1000},
					Priority:     0,
				}},
				DefaultBranch: "done",
			})},
			{ID: "approve", Type: NodeApproval, Label: "Approve"},
			{ID: "done", Type: NodeTypeTerminal, Label: "Done"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "cond"},
			{ID: "e2", SourceID: "cond", TargetID: "approve"},
			{ID: "e3", SourceID: "cond", TargetID: "done"},
		},
	}
	if err := ValidateGraphStructure(graph); err != nil {
		t.Errorf("ValidateGraphStructure(condition graph) = %v, want nil", err)
	}
}

func TestValidateGraphStructure_RejectsConditionBranchWithoutRoute(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "cond", Type: NodeConditionBranch, Label: "Check", Config: mustRawJSON(t, ConditionBranchConfig{})},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "t1", TargetID: "cond"}},
	}
	err := ValidateGraphStructure(graph)
	if !errors.Is(err, ErrConditionBranchInvalid) {
		t.Errorf("error = %v, want ErrConditionBranchInvalid", err)
	}
}

func TestValidateGraphStructure_RejectsConditionBranchMissingTarget(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "cond", Type: NodeConditionBranch, Label: "Check", Config: mustRawJSON(t, ConditionBranchConfig{
				Branches: []BranchCondition{{
					TargetNodeID: "missing",
					Expression:   ConditionExpr{Field: "amount", Operator: "greater_than", Value: 1000},
				}},
			})},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "t1", TargetID: "cond"}},
	}
	err := ValidateGraphStructure(graph)
	if !errors.Is(err, ErrConditionBranchInvalid) {
		t.Errorf("error = %v, want ErrConditionBranchInvalid", err)
	}
}

func TestValidateGraphStructure_RejectsConditionBranchInvalidExpression(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start"},
			{ID: "cond", Type: NodeConditionBranch, Label: "Check", Config: mustRawJSON(t, ConditionBranchConfig{
				Branches: []BranchCondition{{
					TargetNodeID: "done",
					Expression:   ConditionExpr{Field: "", Operator: "unknown", Value: true},
				}},
			})},
			{ID: "done", Type: NodeTypeTerminal, Label: "Done"},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "cond"},
			{ID: "e2", SourceID: "cond", TargetID: "done"},
		},
	}
	err := ValidateGraphStructure(graph)
	if !errors.Is(err, ErrConditionBranchInvalid) {
		t.Errorf("error = %v, want ErrConditionBranchInvalid", err)
	}
}

func TestFullLifecycle_DraftToPublishToNewDraft(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// 1. Create first draft
	v1, err := vm.SaveDraft(ctx, "wf_1", validGraph())
	if err != nil {
		t.Fatalf("step 1 failed: %v", err)
	}
	if v1.VersionNumber != "0.1.0" {
		t.Fatalf("step 1: version = %q, want 0.1.0", v1.VersionNumber)
	}

	// 2. Submit for review
	if err := vm.SubmitForReview(ctx, v1.ID); err != nil {
		t.Fatalf("step 2 failed: %v", err)
	}

	// 3. Approve (publish)
	if err := vm.Approve(ctx, v1.ID); err != nil {
		t.Fatalf("step 3 failed: %v", err)
	}

	// 4. Create new draft from published (simulates modifying published workflow)
	v2, err := vm.CreateDraftFromPublished(ctx, "wf_1")
	if err != nil {
		t.Fatalf("step 4 failed: %v", err)
	}
	if v2.VersionNumber != "0.2.0" {
		t.Errorf("step 4: version = %q, want 0.2.0", v2.VersionNumber)
	}
	if v2.Status != VersionDraft {
		t.Errorf("step 4: status = %q, want draft", v2.Status)
	}

	// 5. v1 is still published (not affected by new draft)
	if store.versions[v1.ID].Status != VersionPublished {
		t.Errorf("step 5: v1 status = %q, want published", store.versions[v1.ID].Status)
	}

	// 6. Submit and approve v2
	if err := vm.SubmitForReview(ctx, v2.ID); err != nil {
		t.Fatalf("step 6a failed: %v", err)
	}
	if err := vm.Approve(ctx, v2.ID); err != nil {
		t.Fatalf("step 6b failed: %v", err)
	}

	// 7. v1 should now be superseded, v2 published
	if store.versions[v1.ID].Status != VersionSuperseded {
		t.Errorf("step 7: v1 status = %q, want superseded", store.versions[v1.ID].Status)
	}
	if store.versions[v2.ID].Status != VersionPublished {
		t.Errorf("step 7: v2 status = %q, want published", store.versions[v2.ID].Status)
	}
}
