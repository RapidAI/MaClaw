package workflow

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

// Payload size and attachment limits.
const (
	MaxTitleLength   = 200
	MaxSummaryLength = 2000
	MaxRationaleLen  = 2000

	MaxPayloadBytes      = 100 * 1024 // 100 KB
	MaxAttachments       = 10
	MaxAttachmentsTotalB = 50 * 1024 * 1024 // 50 MB
)

// ApprovalRequest is the structured payload sent to VE approvers.
type ApprovalRequest struct {
	ID            string                 `json:"id"`
	InstanceID    string                 `json:"instance_id"`
	NodeID        string                 `json:"node_id"`
	WorkflowName  string                 `json:"workflow_name"`
	Title         string                 `json:"title"`
	Summary       string                 `json:"summary"`
	Details       map[string]interface{} `json:"details"`
	Attachments   []AttachmentRef        `json:"attachments"`
	HintRules     []string               `json:"hint_rules"`
	RequesterID   string                 `json:"requester_id"`
	RequesterName string                 `json:"requester_name,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
}

// AttachmentRef is a reference to a file stored on Hub.
type AttachmentRef struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size_bytes"`
}

// ApprovalResponse is the decision returned by a VE approver.
type ApprovalResponse struct {
	RequestID   string    `json:"request_id"`
	Decision    string    `json:"decision"` // "approve", "reject", "escalate"
	Rationale   string    `json:"rationale,omitempty"`
	MatchedRule string    `json:"matched_rule,omitempty"`
	DecidedAt   time.Time `json:"decided_at"`
	ApproverID  string    `json:"approver_id"`
}

// Validation errors.
var (
	ErrTitleTooLong      = errors.New("title exceeds maximum length of 200 characters")
	ErrSummaryTooLong    = errors.New("summary exceeds maximum length of 2000 characters")
	ErrRationaleTooLong  = errors.New("rationale exceeds maximum length of 2000 characters")
	ErrTooManyAttach     = errors.New("attachments exceed maximum count of 10")
	ErrAttachTotalTooBig = errors.New("total attachment size exceeds 50 MB")
)

// ValidateApprovalRequest validates the approval request payload.
// It checks field length constraints, attachment limits, and payload size.
// If the serialized payload exceeds MaxPayloadBytes, the Details field is
// truncated (set to nil) to bring the size within limits.
func ValidateApprovalRequest(req *ApprovalRequest) error {
	if utf8.RuneCountInString(req.Title) > MaxTitleLength {
		return ErrTitleTooLong
	}
	if utf8.RuneCountInString(req.Summary) > MaxSummaryLength {
		return ErrSummaryTooLong
	}
	if err := validateAttachments(req.Attachments); err != nil {
		return err
	}

	// Check serialized payload size; truncate details if exceeded.
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if len(data) > MaxPayloadBytes {
		req.Details = nil
		// Re-check after truncation — title, summary, and hint_rules are preserved.
		data, err = json.Marshal(req)
		if err != nil {
			return err
		}
		if len(data) > MaxPayloadBytes {
			// Even without details the payload is too large; this is unlikely
			// given the field length constraints, but guard against it.
			return errors.New("payload exceeds 100 KB even after truncating details")
		}
	}
	return nil
}

// ValidateApprovalResponse validates the approval response payload.
func ValidateApprovalResponse(resp *ApprovalResponse) error {
	if utf8.RuneCountInString(resp.Rationale) > MaxRationaleLen {
		return ErrRationaleTooLong
	}
	return nil
}

// validateAttachments checks attachment count and total size constraints.
func validateAttachments(attachments []AttachmentRef) error {
	if len(attachments) > MaxAttachments {
		return ErrTooManyAttach
	}
	var total int64
	for i := range attachments {
		total += attachments[i].Size
	}
	if total > MaxAttachmentsTotalB {
		return ErrAttachTotalTooBig
	}
	return nil
}
