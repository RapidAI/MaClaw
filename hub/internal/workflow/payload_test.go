package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestValidateApprovalRequest_ValidPayload(t *testing.T) {
	req := &ApprovalRequest{
		ID:           "req-1",
		InstanceID:   "inst-1",
		NodeID:       "node-1",
		WorkflowName: "Test Workflow",
		Title:        "Approve purchase",
		Summary:      "Please approve this purchase order.",
		Details:      map[string]interface{}{"amount": 500},
		Attachments:  []AttachmentRef{{URL: "https://hub/file1.pdf", Filename: "invoice.pdf", Size: 1024}},
		HintRules:    []string{"amount < 1000 → auto-approve"},
		RequesterID:  "user-1",
		CreatedAt:    time.Now(),
	}
	if err := ValidateApprovalRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateApprovalRequest_TitleTooLong(t *testing.T) {
	req := &ApprovalRequest{
		Title: strings.Repeat("a", MaxTitleLength+1),
	}
	err := ValidateApprovalRequest(req)
	if err != ErrTitleTooLong {
		t.Fatalf("expected ErrTitleTooLong, got: %v", err)
	}
}

func TestValidateApprovalRequest_TitleExactMax(t *testing.T) {
	req := &ApprovalRequest{
		Title: strings.Repeat("x", MaxTitleLength),
	}
	if err := ValidateApprovalRequest(req); err != nil {
		t.Fatalf("expected no error for exact max title, got: %v", err)
	}
}

func TestValidateApprovalRequest_SummaryTooLong(t *testing.T) {
	req := &ApprovalRequest{
		Title:   "OK",
		Summary: strings.Repeat("b", MaxSummaryLength+1),
	}
	err := ValidateApprovalRequest(req)
	if err != ErrSummaryTooLong {
		t.Fatalf("expected ErrSummaryTooLong, got: %v", err)
	}
}

func TestValidateApprovalRequest_TooManyAttachments(t *testing.T) {
	attachments := make([]AttachmentRef, MaxAttachments+1)
	for i := range attachments {
		attachments[i] = AttachmentRef{URL: "https://hub/f", Filename: "f", Size: 100}
	}
	req := &ApprovalRequest{
		Title:       "OK",
		Attachments: attachments,
	}
	err := ValidateApprovalRequest(req)
	if err != ErrTooManyAttach {
		t.Fatalf("expected ErrTooManyAttach, got: %v", err)
	}
}

func TestValidateApprovalRequest_AttachmentsTotalTooBig(t *testing.T) {
	attachments := []AttachmentRef{
		{URL: "https://hub/big", Filename: "big.bin", Size: MaxAttachmentsTotalB + 1},
	}
	req := &ApprovalRequest{
		Title:       "OK",
		Attachments: attachments,
	}
	err := ValidateApprovalRequest(req)
	if err != ErrAttachTotalTooBig {
		t.Fatalf("expected ErrAttachTotalTooBig, got: %v", err)
	}
}

func TestValidateApprovalRequest_MaxAttachmentsExactly(t *testing.T) {
	attachments := make([]AttachmentRef, MaxAttachments)
	for i := range attachments {
		attachments[i] = AttachmentRef{URL: "https://hub/f", Filename: "f", Size: 100}
	}
	req := &ApprovalRequest{
		Title:       "OK",
		Attachments: attachments,
	}
	if err := ValidateApprovalRequest(req); err != nil {
		t.Fatalf("expected no error for exactly max attachments, got: %v", err)
	}
}

func TestValidateApprovalRequest_TruncatesDetailsWhenPayloadTooLarge(t *testing.T) {
	// Build a large details map that pushes payload over 100 KB.
	largeValue := strings.Repeat("x", MaxPayloadBytes)
	req := &ApprovalRequest{
		ID:      "req-1",
		Title:   "OK",
		Summary: "Short summary",
		Details: map[string]interface{}{"big": largeValue},
	}
	err := ValidateApprovalRequest(req)
	if err != nil {
		t.Fatalf("expected no error after truncation, got: %v", err)
	}
	if req.Details != nil {
		t.Fatal("expected Details to be nil after truncation")
	}
}

func TestValidateApprovalRequest_PreservesTitleSummaryHintRulesAfterTruncation(t *testing.T) {
	largeValue := strings.Repeat("y", MaxPayloadBytes)
	req := &ApprovalRequest{
		ID:        "req-1",
		Title:     "Important Title",
		Summary:   "Important Summary",
		Details:   map[string]interface{}{"big": largeValue},
		HintRules: []string{"rule1", "rule2"},
	}
	err := ValidateApprovalRequest(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if req.Title != "Important Title" {
		t.Fatal("title was modified")
	}
	if req.Summary != "Important Summary" {
		t.Fatal("summary was modified")
	}
	if len(req.HintRules) != 2 {
		t.Fatal("hint_rules were modified")
	}
}

func TestValidateApprovalResponse_Valid(t *testing.T) {
	resp := &ApprovalResponse{
		RequestID:  "req-1",
		Decision:   "approve",
		Rationale:  "Amount is within budget.",
		ApproverID: "ve-1",
		DecidedAt:  time.Now(),
	}
	if err := ValidateApprovalResponse(resp); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateApprovalResponse_RationaleTooLong(t *testing.T) {
	resp := &ApprovalResponse{
		RequestID: "req-1",
		Decision:  "reject",
		Rationale: strings.Repeat("z", MaxRationaleLen+1),
	}
	err := ValidateApprovalResponse(resp)
	if err != ErrRationaleTooLong {
		t.Fatalf("expected ErrRationaleTooLong, got: %v", err)
	}
}

func TestValidateApprovalRequest_MultibyteTitle(t *testing.T) {
	// 200 Chinese characters should be exactly at the limit (rune count).
	req := &ApprovalRequest{
		Title: strings.Repeat("中", MaxTitleLength),
	}
	if err := ValidateApprovalRequest(req); err != nil {
		t.Fatalf("expected no error for 200 CJK runes, got: %v", err)
	}

	req.Title = strings.Repeat("中", MaxTitleLength+1)
	if err := ValidateApprovalRequest(req); err != ErrTitleTooLong {
		t.Fatalf("expected ErrTitleTooLong for 201 CJK runes, got: %v", err)
	}
}
