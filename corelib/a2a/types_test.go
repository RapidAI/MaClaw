package a2a

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionMajorityDecision(t *testing.T) {
	now := time.Date(2026, 4, 28, 1, 2, 3, 0, time.UTC)
	s, err := NewSession("a2a-1", "delivery exception", "pick a response plan", []Participant{{ID: "ops"}, {ID: "quality"}, {ID: "sales"}}, PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := s.AddProposal(Proposal{ID: "prop-1", AuthorID: "ops", Title: "fast repair", Content: "repair first, notify customer second", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal returned error: %v", err)
	}
	_ = s.AddReview(Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "ops", Position: ReviewApprove, CreatedAt: now})
	if s.PolicySatisfied("prop-1") {
		t.Fatal("one approval should not satisfy majority of three")
	}
	_ = s.AddReview(Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "quality", Position: ReviewApprove, CreatedAt: now})
	decision, err := s.TryDecide("dec-1", "prop-1", "repair first", now)
	if err != nil {
		t.Fatalf("TryDecide returned error: %v", err)
	}
	if s.Status != SessionDecided || decision.ProposalID != "prop-1" {
		t.Fatalf("decision state = %s %+v", s.Status, decision)
	}
}

func TestConcernBlocksDecision(t *testing.T) {
	now := time.Now()
	s, err := NewSession("a2a-2", "pricing", "choose discount", []Participant{{ID: "sales"}, {ID: "finance"}}, PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	_ = s.AddProposal(Proposal{ID: "prop-1", AuthorID: "sales", Title: "discount", Content: "offer 10 percent", CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "sales", Position: ReviewApprove, CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "finance", Position: ReviewConcern, CreatedAt: now})
	if _, err := s.TryDecide("dec-1", "prop-1", "discount", now); err == nil {
		t.Fatal("expected concern to block decision")
	}
}

func TestReviewSummaryUsesLatestReviewAndSortedReviewers(t *testing.T) {
	now := time.Now()
	s, err := NewSession("a2a-review", "deployment", "pick rollout", []Participant{{ID: "zeta"}, {ID: "alpha"}, {ID: "ops"}}, PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := s.AddProposal(Proposal{ID: "prop-1", AuthorID: "ops", Title: "staged", Content: "ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal returned error: %v", err)
	}
	_ = s.AddReview(Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "zeta", Position: ReviewReject, CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "alpha", Position: ReviewApprove, CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-3", ProposalID: "prop-1", ReviewerID: "zeta", Position: ReviewConcern, CreatedAt: now})
	summary := s.ReviewSummary("prop-1")
	if summary.Approvals != 1 || summary.Rejections != 0 || summary.Concerns != 1 || summary.Abstains != 0 {
		t.Fatalf("summary counts = %+v", summary)
	}
	want := []string{"alpha", "zeta"}
	if len(summary.ReviewedBy) != len(want) || summary.ReviewedBy[0] != want[0] || summary.ReviewedBy[1] != want[1] {
		t.Fatalf("reviewed_by = %v, want %v", summary.ReviewedBy, want)
	}
}

func TestEscalationClosesLocalDecisionPath(t *testing.T) {
	s, err := NewSession("a2a-3", "budget", "approve spend", []Participant{{ID: "ops"}}, PolicyMajority, time.Now())
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := s.Escalate(Escalation{ID: "esc-1", RaisedBy: "ops", Reason: "budget threshold exceeded"}); err != nil {
		t.Fatalf("Escalate returned error: %v", err)
	}
	if s.Status != SessionEscalated || s.Escalation.Target != "human_owner" {
		t.Fatalf("unexpected escalation state: %+v", s.Escalation)
	}
	if err := s.AddMessage(Message{ID: "msg-1", FromID: "ops", Content: "late note"}); err == nil {
		t.Fatal("expected escalated session to reject new messages")
	}
}

func TestSessionAddMessageAllowsStreamEndAndAttachmentOnlyPayloads(t *testing.T) {
	s, err := NewSession("a2a-stream", "stream", "reply", []Participant{{ID: "maclaw-a"}}, PolicyMajority, time.Now())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.AddMessage(Message{ID: "msg-end", FromID: "maclaw-a", Kind: MessageStreamEnd}); err != nil {
		t.Fatalf("stream_end without content should be accepted: %v", err)
	}
	if err := s.AddMessage(Message{ID: "msg-file", FromID: "maclaw-a", Kind: MessageStatement, FileAttachments: []FileAttachment{{FileURL: "https://hub.local/file", Filename: "report.pdf"}}}); err != nil {
		t.Fatalf("attachment-only message should be accepted: %v", err)
	}
	if err := s.AddMessage(Message{ID: "msg-empty", FromID: "maclaw-a", Kind: MessageStatement}); err == nil {
		t.Fatalf("empty non-stream message should be rejected")
	}
}

func TestTextAttachment_JSONRoundTrip(t *testing.T) {
	att := TextAttachment{
		Content:  "SGVsbG8gV29ybGQ=", // base64 "Hello World"
		Filename: "readme.txt",
		MimeType: "text/plain",
	}
	data, err := json.Marshal(att)
	if err != nil {
		t.Fatalf("Marshal TextAttachment: %v", err)
	}
	var got TextAttachment
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal TextAttachment: %v", err)
	}
	if got.Content != att.Content || got.Filename != att.Filename || got.MimeType != att.MimeType {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, att)
	}
}

func TestImageAttachment_JSONRoundTrip(t *testing.T) {
	att := ImageAttachment{
		FileURL:  "https://hub.local/api/ve/files/img-001",
		Filename: "screenshot.png",
		MimeType: "image/png",
		Width:    1920,
		Height:   1080,
	}
	data, err := json.Marshal(att)
	if err != nil {
		t.Fatalf("Marshal ImageAttachment: %v", err)
	}
	var got ImageAttachment
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal ImageAttachment: %v", err)
	}
	if got.FileURL != att.FileURL || got.Filename != att.Filename || got.MimeType != att.MimeType {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, att)
	}
	if got.Width != att.Width || got.Height != att.Height {
		t.Fatalf("dimensions mismatch: got %dx%d, want %dx%d", got.Width, got.Height, att.Width, att.Height)
	}
}

func TestFileAttachment_JSONRoundTrip(t *testing.T) {
	att := FileAttachment{
		FileURL:   "https://hub.local/api/ve/files/doc-001",
		Filename:  "report.pdf",
		MimeType:  "application/pdf",
		SizeBytes: 2048576,
	}
	data, err := json.Marshal(att)
	if err != nil {
		t.Fatalf("Marshal FileAttachment: %v", err)
	}
	var got FileAttachment
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal FileAttachment: %v", err)
	}
	if got.FileURL != att.FileURL || got.Filename != att.Filename || got.MimeType != att.MimeType || got.SizeBytes != att.SizeBytes {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, att)
	}
}

func TestGroupDiscussionMessage_OmitemptyAttachments(t *testing.T) {
	msg := GroupDiscussionMessage{
		ID:        "msg-1",
		SessionID: "session-1",
		FromID:    "user-a",
		Kind:      MessageStatement,
		Content:   "Hello",
		CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "text_attachments") {
		t.Fatalf("empty TextAttachments should be omitted, got: %s", s)
	}
	if strings.Contains(s, "image_attachments") {
		t.Fatalf("empty ImageAttachments should be omitted, got: %s", s)
	}
	if strings.Contains(s, "file_attachments") {
		t.Fatalf("empty FileAttachments should be omitted, got: %s", s)
	}
}

func TestGroupDiscussionMessage_MixedAttachments_JSONRoundTrip(t *testing.T) {
	msg := GroupDiscussionMessage{
		ID:        "msg-2",
		SessionID: "session-2",
		FromID:    "user-b",
		Kind:      MessageStatement,
		Content:   "Here are the files",
		TextAttachments: []TextAttachment{
			{Content: "cHJpbnQoImhlbGxvIik=", Filename: "script.py", MimeType: "text/x-python"},
		},
		ImageAttachments: []ImageAttachment{
			{FileURL: "https://hub.local/api/ve/files/img-1", Filename: "arch.png", MimeType: "image/png", Width: 800, Height: 600},
			{FileURL: "https://hub.local/api/ve/files/img-2", Filename: "flow.jpg", MimeType: "image/jpeg", Width: 1024, Height: 768},
		},
		FileAttachments: []FileAttachment{
			{FileURL: "https://hub.local/api/ve/files/doc-1", Filename: "spec.pdf", MimeType: "application/pdf", SizeBytes: 1048576},
		},
		CreatedAt: time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got GroupDiscussionMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify basic fields
	if got.ID != msg.ID || got.SessionID != msg.SessionID || got.FromID != msg.FromID {
		t.Fatalf("basic fields mismatch: got ID=%s SessionID=%s FromID=%s", got.ID, got.SessionID, got.FromID)
	}
	if got.Kind != msg.Kind || got.Content != msg.Content {
		t.Fatalf("kind/content mismatch: got Kind=%s Content=%s", got.Kind, got.Content)
	}

	// Verify text attachments
	if len(got.TextAttachments) != 1 {
		t.Fatalf("TextAttachments len = %d, want 1", len(got.TextAttachments))
	}
	if got.TextAttachments[0].Filename != "script.py" || got.TextAttachments[0].Content != "cHJpbnQoImhlbGxvIik=" {
		t.Fatalf("TextAttachments[0] mismatch: %+v", got.TextAttachments[0])
	}

	// Verify image attachments
	if len(got.ImageAttachments) != 2 {
		t.Fatalf("ImageAttachments len = %d, want 2", len(got.ImageAttachments))
	}
	if got.ImageAttachments[0].Filename != "arch.png" || got.ImageAttachments[0].Width != 800 {
		t.Fatalf("ImageAttachments[0] mismatch: %+v", got.ImageAttachments[0])
	}
	if got.ImageAttachments[1].Filename != "flow.jpg" || got.ImageAttachments[1].Height != 768 {
		t.Fatalf("ImageAttachments[1] mismatch: %+v", got.ImageAttachments[1])
	}

	// Verify file attachments
	if len(got.FileAttachments) != 1 {
		t.Fatalf("FileAttachments len = %d, want 1", len(got.FileAttachments))
	}
	if got.FileAttachments[0].Filename != "spec.pdf" || got.FileAttachments[0].SizeBytes != 1048576 {
		t.Fatalf("FileAttachments[0] mismatch: %+v", got.FileAttachments[0])
	}
}

func TestGroupDiscussionMessage_AttachmentsDeserializeFromExternalJSON(t *testing.T) {
	// Simulate JSON from an external source (e.g., Hub relay)
	raw := `{
		"id": "msg-ext",
		"session_id": "sess-ext",
		"from_id": "ve-1",
		"kind": "statement",
		"content": "Analysis complete",
		"text_attachments": [
			{"content": "dGVzdA==", "filename": "notes.md", "mime_type": "text/markdown"}
		],
		"image_attachments": [
			{"file_url": "http://hub/files/img-99", "filename": "chart.webp", "mime_type": "image/webp", "width": 640, "height": 480}
		],
		"file_attachments": [
			{"file_url": "http://hub/files/doc-99", "filename": "data.csv", "mime_type": "text/csv", "size_bytes": 4096}
		],
		"created_at": "2026-05-01T12:00:00Z"
	}`
	var msg GroupDiscussionMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("Unmarshal external JSON: %v", err)
	}
	if msg.ID != "msg-ext" || msg.Content != "Analysis complete" {
		t.Fatalf("basic fields: %+v", msg)
	}
	if len(msg.TextAttachments) != 1 || msg.TextAttachments[0].Content != "dGVzdA==" {
		t.Fatalf("TextAttachments: %+v", msg.TextAttachments)
	}
	if len(msg.ImageAttachments) != 1 || msg.ImageAttachments[0].Width != 640 {
		t.Fatalf("ImageAttachments: %+v", msg.ImageAttachments)
	}
	if len(msg.FileAttachments) != 1 || msg.FileAttachments[0].SizeBytes != 4096 {
		t.Fatalf("FileAttachments: %+v", msg.FileAttachments)
	}
}
