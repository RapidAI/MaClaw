package workflow

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

// Sentinel errors for version management.
var (
	ErrInvalidStatusTransition = errors.New("invalid version status transition")
	ErrVersionNotDraft         = errors.New("version is not in draft status")
	ErrVersionNotPendingReview = errors.New("version is not in pending_review status")
	ErrVersionNotPublished     = errors.New("version is not in published status")
	ErrNotWorkflowOwner        = errors.New("user is not the workflow owner")
	ErrNoNodes                 = errors.New("workflow graph has no nodes")
	ErrDisconnectedNodes       = errors.New("workflow graph has disconnected nodes")
	ErrTriggerHasIncoming      = errors.New("trigger node cannot have incoming edges")
	ErrEmptyVersionNumber      = errors.New("version number is empty")
)

// VersionManager orchestrates the version lifecycle for approval workflows.
// It handles version number auto-increment, status transitions, structure
// validation on submit, and draft creation from published versions.
type VersionManager struct {
	store      WorkflowStore
	auditStore AuditStore
	// capabilitySvc, when set, makes Approve converge on the single
	// authoritative publish path (AdminReviewService.ApproveSubmission):
	// publish + supersede + capability-market registration with rollback on
	// failure. When nil (the default for callers that do not opt in), Approve
	// retains its original publish-and-supersede-only behavior.
	capabilitySvc *capability.Service
}

// NewVersionManager creates a new VersionManager with the given store.
// The auditStore is optional — if nil, audit trail recording is skipped.
func NewVersionManager(store WorkflowStore, auditStore ...AuditStore) *VersionManager {
	vm := &VersionManager{store: store}
	if len(auditStore) > 0 && auditStore[0] != nil {
		vm.auditStore = auditStore[0]
	}
	return vm
}

// WithCapabilityService wires a capability.Service into the VersionManager so
// that Approve converges on the single authoritative publish path
// (AdminReviewService.ApproveSubmission): it registers the published workflow
// in the capability market with rollback on failure as part of the same
// publish operation. It returns the receiver for fluent construction.
//
// When no capability service is configured, Approve keeps its original
// behavior (status transition + supersede only), preserving every existing
// caller and test that constructs a VersionManager without a market.
func (vm *VersionManager) WithCapabilityService(capabilitySvc *capability.Service) *VersionManager {
	vm.capabilitySvc = capabilitySvc
	return vm
}

// validTransitions defines the allowed status transitions.
// Key is the current status, value is the set of allowed target statuses.
var validTransitions = map[VersionStatus][]VersionStatus{
	VersionDraft:         {VersionPendingReview},
	VersionPendingReview: {VersionPublished, VersionRejected, VersionDraft}, // Draft for withdrawal
	VersionPublished:     {VersionSuperseded, VersionUnpublished},
	VersionRejected:      {}, // Terminal — user creates a new version instead
	VersionSuperseded:    {}, // Terminal
	VersionUnpublished:   {}, // Terminal
}

// IsValidTransition checks whether transitioning from current to target status is allowed.
func IsValidTransition(current, target VersionStatus) bool {
	allowed, ok := validTransitions[current]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}

// SaveDraft creates or updates a draft version with an auto-incremented version number.
// If the workflow has no versions, it starts at "0.1.0".
// If the latest version is a draft, it increments the patch number.
// Otherwise, it increments the minor number and resets patch to 0.
func (vm *VersionManager) SaveDraft(ctx context.Context, workflowID string, graph WorkflowGraph) (*WorkflowVersion, error) {
	versions, err := vm.store.ListVersions(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}

	var nextVersion string
	var existingDraft *WorkflowVersion

	if len(versions) == 0 {
		nextVersion = "0.1.0"
	} else {
		// Find the latest version by creation time
		latest := findLatestVersion(versions)

		if latest.Status == VersionDraft {
			// Update existing draft — increment patch
			existingDraft = &latest
			nextVersion = incrementPatch(latest.VersionNumber)
		} else {
			// Create new draft — increment minor
			nextVersion = incrementMinor(latest.VersionNumber)
		}
	}

	now := time.Now().UTC()

	if existingDraft != nil {
		// Update the existing draft version in place: bump its version number
		// (patch) and replace its graph, keeping the status as draft. This
		// mirrors the "update existing draft" semantics — re-saving a draft
		// updates the same version row rather than accumulating new rows.
		existingDraft.Graph = graph
		existingDraft.VersionNumber = nextVersion
		existingDraft.Status = VersionDraft
		existingDraft.UpdatedAt = now
		if err := vm.store.UpdateVersion(ctx, existingDraft); err != nil {
			return nil, fmt.Errorf("update version: %w", err)
		}
		return existingDraft, nil
	}

	ver := &WorkflowVersion{
		ID:            generateID("ver"),
		WorkflowID:    workflowID,
		VersionNumber: nextVersion,
		Status:        VersionDraft,
		Graph:         graph,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := vm.store.CreateVersion(ctx, ver); err != nil {
		return nil, fmt.Errorf("create version: %w", err)
	}
	return ver, nil
}

// SubmitForReview validates the workflow graph structure and transitions the
// version from draft to pending_review.
func (vm *VersionManager) SubmitForReview(ctx context.Context, versionID string) error {
	ver, err := vm.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if ver.Status != VersionDraft {
		return ErrVersionNotDraft
	}

	// Validate workflow graph structure
	if err := ValidateGraphStructure(ver.Graph); err != nil {
		return fmt.Errorf("graph validation failed: %w", err)
	}

	// Transition to pending_review
	if err := vm.store.UpdateVersionStatus(ctx, versionID, VersionPendingReview, ""); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// Approve transitions a pending_review version to published status.
// If there is an existing published version for the same workflow, it is
// marked as superseded.
//
// When a capability market is configured (via WithCapabilityService), Approve
// converges on the single authoritative publish path by delegating to
// AdminReviewService.ApproveSubmission, which performs publish + supersede +
// capability-market registration with rollback on failure as one operation.
// This guarantees the two publish paths cannot drift: a workflow published
// through either path always appears in the market. AdminReviewService.
// ApproveSubmission itself remains the unchanged reference path.
//
// When no capability market is configured, Approve retains its original
// behavior (status transition + supersede only), preserving every existing
// caller and test.
func (vm *VersionManager) Approve(ctx context.Context, versionID string) error {
	if vm.capabilitySvc != nil {
		return NewAdminReviewService(vm.store, vm.capabilitySvc).ApproveSubmission(ctx, versionID)
	}

	ver, err := vm.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if ver.Status != VersionPendingReview {
		return ErrVersionNotPendingReview
	}

	// Supersede the current published version (if any).
	published, err := vm.store.GetPublishedVersion(ctx, ver.WorkflowID)
	if err != nil && !errors.Is(err, ErrNoPublishedVersion) {
		return fmt.Errorf("get published version: %w", err)
	}
	if published != nil {
		if err := vm.store.UpdateVersionStatus(ctx, published.ID, VersionSuperseded, ""); err != nil {
			return fmt.Errorf("supersede previous version: %w", err)
		}
	}

	// Publish the new version.
	if err := vm.store.UpdateVersionStatus(ctx, versionID, VersionPublished, ""); err != nil {
		// Rollback: restore the previous published version if we superseded it.
		if published != nil {
			_ = vm.store.UpdateVersionStatus(ctx, published.ID, VersionPublished, "")
		}
		return fmt.Errorf("publish version: %w", err)
	}
	return nil
}

// Reject transitions a pending_review version to rejected status with a reason.
func (vm *VersionManager) Reject(ctx context.Context, versionID, reason string) error {
	ver, err := vm.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if ver.Status != VersionPendingReview {
		return ErrVersionNotPendingReview
	}

	if err := vm.store.UpdateVersionStatus(ctx, versionID, VersionRejected, reason); err != nil {
		return fmt.Errorf("reject version: %w", err)
	}
	return nil
}

// Unpublish takes down a published version. New instances cannot be created,
// but existing running instances continue on their bound version.
func (vm *VersionManager) Unpublish(ctx context.Context, versionID string) error {
	ver, err := vm.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if ver.Status != VersionPublished {
		return ErrVersionNotPublished
	}

	if err := vm.store.UpdateVersionStatus(ctx, versionID, VersionUnpublished, ""); err != nil {
		return fmt.Errorf("unpublish version: %w", err)
	}
	return nil
}

// CreateDraftFromPublished creates a new draft version based on a published
// workflow, incrementing the minor version number. This is used when a user
// wants to modify an already-published workflow without affecting the current
// published version.
func (vm *VersionManager) CreateDraftFromPublished(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	published, err := vm.store.GetPublishedVersion(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get published version: %w", err)
	}
	if published == nil {
		return nil, ErrNoPublishedVersion
	}

	nextVersion := incrementMinor(published.VersionNumber)
	now := time.Now().UTC()

	ver := &WorkflowVersion{
		ID:            generateID("ver"),
		WorkflowID:    workflowID,
		VersionNumber: nextVersion,
		Status:        VersionDraft,
		Graph:         published.Graph,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := vm.store.CreateVersion(ctx, ver); err != nil {
		return nil, fmt.Errorf("create draft from published: %w", err)
	}
	return ver, nil
}

// WithdrawReview transitions a pending_review version back to draft status.
// Only the workflow owner can withdraw their own submission.
// The withdrawal is recorded in the audit trail if an AuditStore is configured.
// If userID is empty, ownership check is skipped (for backward compatibility).
func (vm *VersionManager) WithdrawReview(ctx context.Context, versionID string, userID ...string) error {
	ver, err := vm.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if ver.Status != VersionPendingReview {
		return ErrVersionNotPendingReview
	}

	// Verify ownership if userID is provided.
	actorID := ""
	if len(userID) > 0 && userID[0] != "" {
		actorID = userID[0]
		wf, err := vm.store.GetWorkflow(ctx, ver.WorkflowID)
		if err != nil {
			return fmt.Errorf("get workflow: %w", err)
		}
		if wf == nil {
			return fmt.Errorf("workflow not found: %s", ver.WorkflowID)
		}
		if wf.OwnerID != actorID {
			return ErrNotWorkflowOwner
		}
	}

	// Transition status from pending_review to draft.
	if err := vm.store.UpdateVersionStatus(ctx, versionID, VersionDraft, ""); err != nil {
		return fmt.Errorf("withdraw review: %w", err)
	}

	// Record the withdrawal in the audit trail.
	if vm.auditStore != nil {
		auditEntry := &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: ver.WorkflowID,
			EventType:  "version_withdrawn",
			ActorID:    actorID,
			Details:    fmt.Sprintf(`{"version_id":"%s","version_number":"%s"}`, versionID, ver.VersionNumber),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}
		if err := vm.auditStore.Append(ctx, auditEntry); err != nil {
			// The status transition already succeeded. Log the audit failure
			// but don't roll back the withdrawal.
			return fmt.Errorf("record audit entry: %w", err)
		}
	}

	return nil
}

// ValidateGraphStructure checks that a workflow graph is structurally valid
// for submission. It verifies:
// 1. The graph has at least one node
// 2. There is exactly one trigger node
// 3. Trigger has no incoming edges
// 4. All nodes are reachable (no disconnected nodes)
func ValidateGraphStructure(graph WorkflowGraph) error {
	if len(graph.Nodes) == 0 {
		return ErrNoNodes
	}

	// Check exactly one trigger node
	trigger, err := findSingleTriggerNode(graph)
	if err != nil {
		return err
	}
	if triggerHasIncomingEdge(graph, trigger.ID) {
		return ErrTriggerHasIncoming
	}

	// Check for disconnected nodes — every non-trigger node must have at
	// least one incoming edge, and every non-terminal node must have at
	// least one outgoing edge. We use reachability from the trigger node.
	if hasDisconnectedNodes(graph) {
		return ErrDisconnectedNodes
	}

	return nil
}

func triggerHasIncomingEdge(graph WorkflowGraph, triggerID string) bool {
	for _, e := range graph.Edges {
		if e.TargetID == triggerID {
			return true
		}
	}
	return false
}

// hasDisconnectedNodes checks if any node in the graph is unreachable from
// the trigger node via BFS traversal.
func hasDisconnectedNodes(graph WorkflowGraph) bool {
	if len(graph.Nodes) <= 1 {
		return false
	}

	// Find trigger node
	var triggerID string
	for _, n := range graph.Nodes {
		if n.Type == NodeTrigger {
			triggerID = n.ID
			break
		}
	}
	if triggerID == "" {
		return true // No trigger means all nodes are disconnected
	}

	// Build adjacency list (directed: source → targets)
	adj := make(map[string][]string)
	for _, e := range graph.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	// BFS from trigger
	visited := make(map[string]bool)
	queue := []string{triggerID}
	visited[triggerID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range adj[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	// Check all nodes are visited
	for _, n := range graph.Nodes {
		if !visited[n.ID] {
			return true
		}
	}
	return false
}

// --- Version number helpers ---

// parseVersion parses a "major.minor.patch" string into its components.
func parseVersion(v string) (major, minor, patch int, err error) {
	if v == "" {
		return 0, 0, 0, ErrEmptyVersionNumber
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %s", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %s", parts[0])
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version: %s", parts[2])
	}
	return major, minor, patch, nil
}

// incrementPatch increments the patch component: "1.2.3" → "1.2.4"
func incrementPatch(v string) string {
	major, minor, patch, err := parseVersion(v)
	if err != nil {
		return "0.1.0"
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

// incrementMinor increments the minor component and resets patch: "1.2.3" → "1.3.0"
func incrementMinor(v string) string {
	major, minor, _, err := parseVersion(v)
	if err != nil {
		return "0.1.0"
	}
	return fmt.Sprintf("%d.%d.0", major, minor+1)
}

// findLatestVersion returns the version with the highest version number
// from the given list. Compares by major, then minor, then patch.
func findLatestVersion(versions []WorkflowVersion) WorkflowVersion {
	if len(versions) == 0 {
		return WorkflowVersion{}
	}
	latest := versions[0]
	latestMaj, latestMin, latestPat, _ := parseVersion(latest.VersionNumber)

	for i := 1; i < len(versions); i++ {
		maj, min, pat, err := parseVersion(versions[i].VersionNumber)
		if err != nil {
			continue
		}
		if maj > latestMaj ||
			(maj == latestMaj && min > latestMin) ||
			(maj == latestMaj && min == latestMin && pat > latestPat) {
			latest = versions[i]
			latestMaj, latestMin, latestPat = maj, min, pat
		}
	}
	return latest
}
