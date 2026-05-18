package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

// AdminReviewService provides admin review operations for workflow version submissions.
type AdminReviewService struct {
	store         WorkflowStore
	capabilitySvc *capability.Service
}

// NewAdminReviewService creates a new AdminReviewService.
func NewAdminReviewService(store WorkflowStore, capabilitySvc *capability.Service) *AdminReviewService {
	return &AdminReviewService{
		store:         store,
		capabilitySvc: capabilitySvc,
	}
}

// PendingSubmission represents a workflow version awaiting admin review,
// enriched with the workflow definition metadata.
type PendingSubmission struct {
	Version      WorkflowVersion    `json:"version"`
	WorkflowName string             `json:"workflow_name"`
	AuthorID     string             `json:"author_id"`
}

// PendingSubmissionsPage is a paginated list of pending submissions.
type PendingSubmissionsPage struct {
	Submissions []PendingSubmission `json:"submissions"`
	Total       int                 `json:"total"`
	Page        int                 `json:"page"`
	PageSize    int                 `json:"page_size"`
}

// SubmissionDetail contains the complete workflow graph and configurations
// for admin inspection during review.
type SubmissionDetail struct {
	Version        WorkflowVersion    `json:"version"`
	WorkflowName   string             `json:"workflow_name"`
	WorkflowDesc   string             `json:"workflow_description"`
	AuthorID       string             `json:"author_id"`
	Graph          WorkflowGraph      `json:"graph"`
	NodeConfigs    []NodeConfigDetail `json:"node_configs"`
}

// NodeConfigDetail provides a parsed view of a node's configuration for review.
type NodeConfigDetail struct {
	NodeID   string          `json:"node_id"`
	NodeType NodeType        `json:"node_type"`
	Label    string          `json:"label"`
	Config   json.RawMessage `json:"config"`
}

const defaultAdminPageSize = 50

// ListPendingSubmissions returns pending workflow submissions sorted by submission
// date (oldest first), paginated at 50 per page.
func (s *AdminReviewService) ListPendingSubmissions(ctx context.Context, page int) (*PendingSubmissionsPage, error) {
	if page < 1 {
		page = 1
	}

	versions, total, err := s.store.ListPendingReviews(ctx, page, defaultAdminPageSize)
	if err != nil {
		return nil, fmt.Errorf("list pending reviews: %w", err)
	}

	submissions := make([]PendingSubmission, 0, len(versions))
	for _, ver := range versions {
		sub := PendingSubmission{
			Version: ver,
		}
		// Enrich with workflow definition metadata.
		def, err := s.store.GetWorkflow(ctx, ver.WorkflowID)
		if err == nil && def != nil {
			sub.WorkflowName = def.Name
			sub.AuthorID = def.OwnerID
		}
		submissions = append(submissions, sub)
	}

	return &PendingSubmissionsPage{
		Submissions: submissions,
		Total:       total,
		Page:        page,
		PageSize:    defaultAdminPageSize,
	}, nil
}

// GetSubmissionForReview returns the complete workflow graph and configurations
// for admin inspection.
func (s *AdminReviewService) GetSubmissionForReview(ctx context.Context, versionID string) (*SubmissionDetail, error) {
	ver, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return nil, errors.New("version not found")
	}
	if ver.Status != VersionPendingReview {
		return nil, fmt.Errorf("version is not pending review (status: %s)", ver.Status)
	}

	def, err := s.store.GetWorkflow(ctx, ver.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow definition: %w", err)
	}

	var workflowName, workflowDesc, authorID string
	if def != nil {
		workflowName = def.Name
		workflowDesc = def.Description
		authorID = def.OwnerID
	}

	// Extract node configurations for review.
	nodeConfigs := make([]NodeConfigDetail, 0, len(ver.Graph.Nodes))
	for _, node := range ver.Graph.Nodes {
		nodeConfigs = append(nodeConfigs, NodeConfigDetail{
			NodeID:   node.ID,
			NodeType: node.Type,
			Label:    node.Label,
			Config:   node.Config,
		})
	}

	return &SubmissionDetail{
		Version:      *ver,
		WorkflowName: workflowName,
		WorkflowDesc: workflowDesc,
		AuthorID:     authorID,
		Graph:        ver.Graph,
		NodeConfigs:  nodeConfigs,
	}, nil
}

// ApproveSubmission transitions a pending_review version to "published",
// supersedes the previous published version, and registers the workflow
// in the Capability Market.
func (s *AdminReviewService) ApproveSubmission(ctx context.Context, versionID string) error {
	ver, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return errors.New("version not found")
	}
	if ver.Status != VersionPendingReview {
		return fmt.Errorf("cannot approve: version is not pending review (status: %s)", ver.Status)
	}

	// Supersede the previous published version (if any).
	prevPublished, err := s.store.GetPublishedVersion(ctx, ver.WorkflowID)
	if err != nil {
		return fmt.Errorf("get previous published version: %w", err)
	}
	if prevPublished != nil {
		if err := s.store.UpdateVersionStatus(ctx, prevPublished.ID, VersionSuperseded, ""); err != nil {
			return fmt.Errorf("supersede previous version: %w", err)
		}
	}

	// Transition to published.
	if err := s.store.UpdateVersionStatus(ctx, versionID, VersionPublished, ""); err != nil {
		return fmt.Errorf("publish version: %w", err)
	}

	// Register in Capability Market.
	if err := s.registerInCapabilityMarket(ctx, ver); err != nil {
		// Log but don't fail the approval — the version is already published.
		// Market registration can be retried.
		_ = err
	}

	return nil
}

// RejectSubmission transitions a pending_review version to "rejected" with a reason.
// The reason must be between 10 and 2000 characters.
func (s *AdminReviewService) RejectSubmission(ctx context.Context, versionID, reason string) error {
	// Validate rejection reason length.
	reasonLen := len([]rune(reason))
	if reasonLen < 10 {
		return errors.New("rejection reason must be at least 10 characters")
	}
	if reasonLen > 2000 {
		return errors.New("rejection reason must not exceed 2000 characters")
	}

	ver, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return errors.New("version not found")
	}
	if ver.Status != VersionPendingReview {
		return fmt.Errorf("cannot reject: version is not pending review (status: %s)", ver.Status)
	}

	if err := s.store.UpdateVersionStatus(ctx, versionID, VersionRejected, reason); err != nil {
		return fmt.Errorf("reject version: %w", err)
	}

	return nil
}

// UnpublishVersion transitions a published version to "unpublished".
// This prevents new workflow instances from being created but does NOT
// terminate running instances — they continue on their bound version.
func (s *AdminReviewService) UnpublishVersion(ctx context.Context, versionID string) error {
	ver, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return errors.New("version not found")
	}
	if ver.Status != VersionPublished {
		return fmt.Errorf("cannot unpublish: version is not published (status: %s)", ver.Status)
	}

	if err := s.store.UpdateVersionStatus(ctx, versionID, VersionUnpublished, ""); err != nil {
		return fmt.Errorf("unpublish version: %w", err)
	}

	// Deactivate in Capability Market (if registered).
	if s.capabilitySvc != nil {
		capID := workflowCapabilityID(ver.WorkflowID)
		_ = s.capabilitySvc.SetCapabilityStatus(ctx, capID, "inactive")
	}

	return nil
}

// registerInCapabilityMarket registers or updates the workflow in the Capability Market.
func (s *AdminReviewService) registerInCapabilityMarket(ctx context.Context, ver *WorkflowVersion) error {
	if s.capabilitySvc == nil {
		return nil
	}

	def, err := s.store.GetWorkflow(ctx, ver.WorkflowID)
	if err != nil {
		return err
	}

	var displayName, description, publisher string
	if def != nil {
		displayName = def.Name
		description = def.Description
		publisher = def.OwnerID
	}

	// Build metadata for market listing.
	metadata := map[string]interface{}{
		"category":       "审批类",
		"node_count":     len(ver.Graph.Nodes),
		"approval_modes": extractApprovalModes(ver.Graph),
		"thumbnail_url":  "/api/v1/workflow/" + ver.WorkflowID + "/thumbnail",
		"version_number": ver.VersionNumber,
		"published_at":   time.Now().UTC().Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadata)

	capID := workflowCapabilityID(ver.WorkflowID)
	globalKey := "approval_workflow:" + ver.WorkflowID

	_, err = s.capabilitySvc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		ID:             capID,
		CapabilityType: "approval_workflow",
		Publisher:      publisher,
		CapabilityID:   ver.WorkflowID,
		DisplayName:    displayName,
		Description:    description,
		Source:         "hub",
		Status:         "active",
		GlobalKey:      globalKey,
		MetadataJSON:   string(metadataJSON),
		Version:        ver.VersionNumber,
		VersionKey:     globalKey + ":" + ver.VersionNumber,
		VersionStatus:  "active",
	})
	return err
}

// workflowCapabilityID generates a deterministic capability ID for a workflow.
func workflowCapabilityID(workflowID string) string {
	return "cap_wf_" + workflowID
}

// extractApprovalModes collects the distinct approval modes used in the workflow graph.
func extractApprovalModes(graph WorkflowGraph) []string {
	modes := map[string]bool{}
	for _, node := range graph.Nodes {
		if node.Type != NodeApproval {
			continue
		}
		var cfg ApprovalNodeConfig
		if err := json.Unmarshal(node.Config, &cfg); err != nil {
			continue
		}
		if cfg.Mode != "" {
			modes[string(cfg.Mode)] = true
		}
	}
	result := make([]string, 0, len(modes))
	for m := range modes {
		result = append(result, m)
	}
	return result
}
