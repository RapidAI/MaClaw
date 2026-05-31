package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

// Sentinel errors for market operations.
var (
	ErrWorkflowNotPublished = errors.New("workflow is not published")
	ErrWorkflowNotFound     = errors.New("workflow not found")
	ErrMissingUserID        = errors.New("user ID is required to trigger a workflow")
)

// TriggerFromMarket creates a new WorkflowInstance when a user triggers a published
// workflow from the Capability Market. It validates that the workflow has a published
// version before delegating to StartInstance.
//
// The triggering user's ID is injected into the trigger data as "requester_id",
// ensuring the instance is associated with the user who triggered it.
//
// User workflow isolation is enforced at the definition level by WorkflowStore
// (ListWorkflows filters by OwnerID), so other users' definitions are not visible
// or editable. However, any user can trigger a published workflow from the market.
func (e *WorkflowExecutor) TriggerFromMarket(ctx context.Context, workflowID, userID, triggerData string) (*WorkflowInstance, error) {
	if userID == "" {
		return nil, ErrMissingUserID
	}

	// Validate that the workflow exists and has a published version.
	ver, err := e.store.GetPublishedVersion(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("check published version: %w", err)
	}
	if ver == nil {
		return nil, ErrWorkflowNotPublished
	}

	// Inject the triggering user's ID into the trigger data so the instance
	// is bound to the user who triggered it.
	enrichedTriggerData := enrichTriggerDataWithUser(triggerData, userID)

	// Delegate to StartInstance which handles instance creation, audit trail
	// recording, and initial execution from the trigger node.
	return e.StartInstance(ctx, workflowID, enrichedTriggerData)
}

// enrichTriggerDataWithUser injects the userID as "requester_id" into the trigger
// data JSON using proper JSON marshaling to prevent injection attacks.
func enrichTriggerDataWithUser(triggerData, userID string) string {
	var data map[string]interface{}
	if triggerData != "" {
		if err := json.Unmarshal([]byte(triggerData), &data); err != nil {
			data = make(map[string]interface{})
		}
	} else {
		data = make(map[string]interface{})
	}
	data["requester_id"] = userID
	result, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf(`{"requester_id":%s}`, strconv.Quote(userID))
	}
	return string(result)
}

// --- Market Listing and Discovery ---

// MarketCategory represents the top-level market categories.
type MarketCategory string

const (
	// MarketCategoryWorkflow is the top-level category for all workflow types.
	MarketCategoryWorkflow MarketCategory = "workflow"
	// MarketCategoryNormalSkill is the top-level category for normal skills.
	MarketCategoryNormalSkill MarketCategory = "normal_skill"
)

// WorkflowSubCategory represents workflow sub-categories.
type WorkflowSubCategory string

const (
	SubCategoryApproval      WorkflowSubCategory = "审批类"
	SubCategoryAutomation    WorkflowSubCategory = "自动化类"
	SubCategoryCollaboration WorkflowSubCategory = "协作类"
)

// WorkflowMarketListing represents a published workflow displayed in the market.
type WorkflowMarketListing struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Author        string              `json:"author"`
	Version       string              `json:"version"`
	UsageCount    int                 `json:"usage_count"`
	Category      MarketCategory      `json:"category"`
	SubCategory   WorkflowSubCategory `json:"sub_category"`
	NodeCount     int                 `json:"node_count"`
	ApprovalModes []string            `json:"approval_modes"`
	PublishedAt   string              `json:"published_at,omitempty"`
	CapabilityID  string              `json:"capability_id"`
}

// MarketListingFilter defines the filtering criteria for market listing queries.
type MarketListingFilter struct {
	// Category filters by top-level category (workflow or normal_skill).
	Category MarketCategory `json:"category,omitempty"`
	// SubCategory filters by workflow sub-category (审批类, 自动化类, 协作类).
	SubCategory WorkflowSubCategory `json:"sub_category,omitempty"`
	// Author filters by publisher/author ID.
	Author string `json:"author,omitempty"`
	// Keyword searches across name and description.
	Keyword string `json:"keyword,omitempty"`
}

// MarketListingPage is a paginated result of market listings.
type MarketListingPage struct {
	Listings []WorkflowMarketListing `json:"listings"`
	Total    int                     `json:"total"`
}

// MarketService provides workflow-specific market listing and discovery.
type MarketService struct {
	capabilitySvc *capability.Service
}

// NewMarketService creates a new MarketService.
func NewMarketService(capabilitySvc *capability.Service) *MarketService {
	return &MarketService{capabilitySvc: capabilitySvc}
}

// ListWorkflows returns published approval workflows from the capability market,
// enriched with workflow-specific metadata and supporting filtering by category,
// sub-category, author, and keyword search.
func (m *MarketService) ListWorkflows(ctx context.Context, filter MarketListingFilter) (*MarketListingPage, error) {
	if m.capabilitySvc == nil {
		return &MarketListingPage{Listings: []WorkflowMarketListing{}}, nil
	}

	// If the filter requests only normal skills, return empty for workflow listing.
	if filter.Category == MarketCategoryNormalSkill {
		return &MarketListingPage{Listings: []WorkflowMarketListing{}}, nil
	}

	// Query capabilities with capability_type = "approval_workflow".
	capabilities, err := m.capabilitySvc.List(ctx, "approval_workflow")
	if err != nil {
		return nil, err
	}

	listings := make([]WorkflowMarketListing, 0, len(capabilities))
	for _, cap := range capabilities {
		// Only include active capabilities.
		if strings.ToLower(cap.Status) != "active" {
			continue
		}

		listing := buildWorkflowMarketListing(cap)

		// Apply filters.
		if !matchesFilter(listing, filter) {
			continue
		}

		listings = append(listings, listing)
	}

	return &MarketListingPage{
		Listings: listings,
		Total:    len(listings),
	}, nil
}

// buildWorkflowMarketListing converts a CapabilitySummary into a WorkflowMarketListing
// by parsing the metadata_json for workflow-specific fields.
func buildWorkflowMarketListing(cap capability.CapabilitySummary) WorkflowMarketListing {
	listing := WorkflowMarketListing{
		ID:           cap.ID,
		Name:         cap.DisplayName,
		Description:  cap.Description,
		Author:       cap.Publisher,
		Version:      cap.CurrentVersionKey,
		Category:     MarketCategoryWorkflow,
		SubCategory:  SubCategoryApproval, // default for approval_workflow
		CapabilityID: cap.CapabilityID,
	}

	// Parse metadata_json for workflow-specific enrichment.
	if cap.MetadataJSON != "" {
		var metadata workflowMarketMetadata
		if err := json.Unmarshal([]byte(cap.MetadataJSON), &metadata); err == nil {
			if metadata.Category != "" {
				listing.SubCategory = classifySubCategory(metadata.Category)
			}
			listing.NodeCount = metadata.NodeCount
			listing.ApprovalModes = metadata.ApprovalModes
			listing.UsageCount = metadata.UsageCount
			listing.PublishedAt = metadata.PublishedAt
			if metadata.VersionNumber != "" {
				listing.Version = metadata.VersionNumber
			}
		}
	}

	return listing
}

// workflowMarketMetadata is the parsed structure of the metadata_json field
// stored in the capability record for approval workflows.
//
// thumbnail_url is intentionally NOT parsed: the Hub serves no thumbnail route
// and the publish path no longer emits one, so the market listing advertises no
// preview URL. Any stale thumbnail_url key in an older capability row is simply
// ignored on unmarshal, keeping the advertised state and the served state in
// agreement (no advertised URL ⇔ no served route).
type workflowMarketMetadata struct {
	Category      string   `json:"category"`
	NodeCount     int      `json:"node_count"`
	ApprovalModes []string `json:"approval_modes"`
	VersionNumber string   `json:"version_number"`
	PublishedAt   string   `json:"published_at"`
	UsageCount    int      `json:"usage_count"`
}

// classifySubCategory maps a category string from metadata to a WorkflowSubCategory.
func classifySubCategory(category string) WorkflowSubCategory {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "审批类", "approval", "approval_workflow":
		return SubCategoryApproval
	case "自动化类", "automation":
		return SubCategoryAutomation
	case "协作类", "collaboration":
		return SubCategoryCollaboration
	default:
		// Default to approval for approval_workflow capability type.
		return SubCategoryApproval
	}
}

// matchesFilter checks whether a listing matches the given filter criteria.
func matchesFilter(listing WorkflowMarketListing, filter MarketListingFilter) bool {
	// Filter by sub-category.
	if filter.SubCategory != "" && listing.SubCategory != filter.SubCategory {
		return false
	}

	// Filter by author.
	if filter.Author != "" {
		if !strings.EqualFold(listing.Author, filter.Author) {
			return false
		}
	}

	// Filter by keyword (search across name and description, case-insensitive).
	if filter.Keyword != "" {
		keyword := strings.ToLower(filter.Keyword)
		nameMatch := strings.Contains(strings.ToLower(listing.Name), keyword)
		descMatch := strings.Contains(strings.ToLower(listing.Description), keyword)
		if !nameMatch && !descMatch {
			return false
		}
	}

	return true
}
