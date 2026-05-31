package workflow

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

func TestClassifySubCategory(t *testing.T) {
	tests := []struct {
		input    string
		expected WorkflowSubCategory
	}{
		{"审批类", SubCategoryApproval},
		{" approval_workflow ", SubCategoryApproval},
		{"APPROVAL", SubCategoryApproval},
		{"自动化类", SubCategoryAutomation},
		{"AUTOMATION", SubCategoryAutomation},
		{"协作类", SubCategoryCollaboration},
		{"COLLABORATION", SubCategoryCollaboration},
		{"approval", SubCategoryApproval},
		{"automation", SubCategoryAutomation},
		{"collaboration", SubCategoryCollaboration},
		{"unknown", SubCategoryApproval},
		{"", SubCategoryApproval},
	}
	for _, tt := range tests {
		got := classifySubCategory(tt.input)
		if got != tt.expected {
			t.Errorf("classifySubCategory(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestMatchesFilter_NoFilter(t *testing.T) {
	listing := WorkflowMarketListing{
		Name:        "采购审批流程",
		Description: "适用于企业采购场景",
		Author:      "user_001",
		SubCategory: SubCategoryApproval,
	}
	filter := MarketListingFilter{}
	if !matchesFilter(listing, filter) {
		t.Error("expected empty filter to match all listings")
	}
}

func TestMatchesFilter_SubCategory(t *testing.T) {
	listing := WorkflowMarketListing{
		Name:        "自动化部署",
		SubCategory: SubCategoryAutomation,
	}

	// Matching sub-category.
	filter := MarketListingFilter{SubCategory: SubCategoryAutomation}
	if !matchesFilter(listing, filter) {
		t.Error("expected matching sub-category to pass")
	}

	// Non-matching sub-category.
	filter = MarketListingFilter{SubCategory: SubCategoryApproval}
	if matchesFilter(listing, filter) {
		t.Error("expected non-matching sub-category to fail")
	}
}

func TestMatchesFilter_Author(t *testing.T) {
	listing := WorkflowMarketListing{
		Name:   "流程A",
		Author: "user_admin",
	}

	// Matching author (case-insensitive).
	filter := MarketListingFilter{Author: "User_Admin"}
	if !matchesFilter(listing, filter) {
		t.Error("expected case-insensitive author match to pass")
	}

	// Non-matching author.
	filter = MarketListingFilter{Author: "other_user"}
	if matchesFilter(listing, filter) {
		t.Error("expected non-matching author to fail")
	}
}

func TestMatchesFilter_Keyword(t *testing.T) {
	listing := WorkflowMarketListing{
		Name:        "采购审批流程",
		Description: "适用于企业采购场景的三级审批",
	}

	// Keyword in name.
	filter := MarketListingFilter{Keyword: "采购"}
	if !matchesFilter(listing, filter) {
		t.Error("expected keyword in name to match")
	}

	// Keyword in description.
	filter = MarketListingFilter{Keyword: "三级审批"}
	if !matchesFilter(listing, filter) {
		t.Error("expected keyword in description to match")
	}

	// Keyword case-insensitive (English).
	listing.Name = "Purchase Approval"
	filter = MarketListingFilter{Keyword: "purchase"}
	if !matchesFilter(listing, filter) {
		t.Error("expected case-insensitive keyword match")
	}

	// Non-matching keyword.
	filter = MarketListingFilter{Keyword: "不存在的关键词"}
	if matchesFilter(listing, filter) {
		t.Error("expected non-matching keyword to fail")
	}
}

func TestMatchesFilter_Combined(t *testing.T) {
	listing := WorkflowMarketListing{
		Name:        "采购审批流程",
		Description: "适用于企业采购场景",
		Author:      "user_001",
		SubCategory: SubCategoryApproval,
	}

	// All filters match.
	filter := MarketListingFilter{
		SubCategory: SubCategoryApproval,
		Author:      "user_001",
		Keyword:     "采购",
	}
	if !matchesFilter(listing, filter) {
		t.Error("expected all matching filters to pass")
	}

	// One filter doesn't match.
	filter = MarketListingFilter{
		SubCategory: SubCategoryApproval,
		Author:      "user_002", // wrong author
		Keyword:     "采购",
	}
	if matchesFilter(listing, filter) {
		t.Error("expected one non-matching filter to fail")
	}
}

func TestBuildWorkflowMarketListing_BasicFields(t *testing.T) {
	cap := capability.CapabilitySummary{
		ID:                "cap_wf_123",
		CapabilityType:    "approval_workflow",
		Publisher:         "user_admin",
		CapabilityID:      "wf_123",
		DisplayName:       "采购审批流程",
		Description:       "适用于企业采购场景",
		Status:            "active",
		CurrentVersionKey: "1.0.0",
		MetadataJSON:      "",
	}

	listing := buildWorkflowMarketListing(cap)

	if listing.ID != "cap_wf_123" {
		t.Errorf("ID = %q, want %q", listing.ID, "cap_wf_123")
	}
	if listing.Name != "采购审批流程" {
		t.Errorf("Name = %q, want %q", listing.Name, "采购审批流程")
	}
	if listing.Description != "适用于企业采购场景" {
		t.Errorf("Description = %q, want %q", listing.Description, "适用于企业采购场景")
	}
	if listing.Author != "user_admin" {
		t.Errorf("Author = %q, want %q", listing.Author, "user_admin")
	}
	if listing.Category != MarketCategoryWorkflow {
		t.Errorf("Category = %q, want %q", listing.Category, MarketCategoryWorkflow)
	}
	if listing.SubCategory != SubCategoryApproval {
		t.Errorf("SubCategory = %q, want %q", listing.SubCategory, SubCategoryApproval)
	}
	if listing.CapabilityID != "wf_123" {
		t.Errorf("CapabilityID = %q, want %q", listing.CapabilityID, "wf_123")
	}
}

func TestBuildWorkflowMarketListing_WithMetadata(t *testing.T) {
	cap := capability.CapabilitySummary{
		ID:                "cap_wf_456",
		CapabilityType:    "approval_workflow",
		Publisher:         "user_dev",
		CapabilityID:      "wf_456",
		DisplayName:       "自动化部署流程",
		Description:       "CI/CD 自动化部署",
		Status:            "active",
		CurrentVersionKey: "v2",
		MetadataJSON:      `{"category":"自动化类","node_count":5,"approval_modes":["single","countersign"],"thumbnail_url":"/api/v1/workflow/456/thumbnail","version_number":"2.1.0","published_at":"2026-01-15T10:00:00Z","usage_count":42}`,
	}

	listing := buildWorkflowMarketListing(cap)

	if listing.SubCategory != SubCategoryAutomation {
		t.Errorf("SubCategory = %q, want %q", listing.SubCategory, SubCategoryAutomation)
	}
	if listing.NodeCount != 5 {
		t.Errorf("NodeCount = %d, want %d", listing.NodeCount, 5)
	}
	if len(listing.ApprovalModes) != 2 {
		t.Errorf("ApprovalModes len = %d, want 2", len(listing.ApprovalModes))
	}
	// The listing must NOT advertise a thumbnail/preview URL even when a stale
	// thumbnail_url key is present in an older capability metadata row: the Hub
	// serves no thumbnail route, so advertising one would make the advertised
	// state diverge from the served state (Requirement 2.12). The market layer
	// no longer parses or surfaces thumbnail_url.
	if listing.Version != "2.1.0" {
		t.Errorf("Version = %q, want %q", listing.Version, "2.1.0")
	}
	if listing.PublishedAt != "2026-01-15T10:00:00Z" {
		t.Errorf("PublishedAt = %q, want %q", listing.PublishedAt, "2026-01-15T10:00:00Z")
	}
	if listing.UsageCount != 42 {
		t.Errorf("UsageCount = %d, want %d", listing.UsageCount, 42)
	}
}

func TestBuildWorkflowMarketListing_InvalidMetadataJSON(t *testing.T) {
	cap := capability.CapabilitySummary{
		ID:             "cap_wf_789",
		CapabilityType: "approval_workflow",
		Publisher:      "user_x",
		DisplayName:    "Test Workflow",
		Status:         "active",
		MetadataJSON:   `{invalid json`,
	}

	// Should not panic, falls back to defaults.
	listing := buildWorkflowMarketListing(cap)
	if listing.SubCategory != SubCategoryApproval {
		t.Errorf("SubCategory = %q, want default %q", listing.SubCategory, SubCategoryApproval)
	}
	if listing.NodeCount != 0 {
		t.Errorf("NodeCount = %d, want 0 (default)", listing.NodeCount)
	}
}

func TestMarketService_ListWorkflows_NilService(t *testing.T) {
	svc := NewMarketService(nil)
	page, err := svc.ListWorkflows(context.Background(), MarketListingFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page == nil {
		t.Fatal("expected non-nil page")
	}
	if len(page.Listings) != 0 {
		t.Errorf("expected empty listings, got %d", len(page.Listings))
	}
}

func TestMarketService_ListWorkflows_NormalSkillFilter(t *testing.T) {
	// When filtering for normal skills, workflow listing should return empty.
	svc := NewMarketService(nil)
	page, err := svc.ListWorkflows(context.Background(), MarketListingFilter{
		Category: MarketCategoryNormalSkill,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Listings) != 0 {
		t.Errorf("expected empty listings for normal_skill category, got %d", len(page.Listings))
	}
}
