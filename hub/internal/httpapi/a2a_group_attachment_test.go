package httpapi

import (
	"testing"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestGroupDiscussionMessageAttachmentsPersistInSession(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "attachments", Question: "Review this file"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	_, err = svc.AddDiscussionMessage("tenant-a", created.Discussion.ID, corea2a.GroupDiscussionMessage{
		FromID:  "maclaw-a",
		Kind:    corea2a.MessageStatement,
		Content: "See attached",
		ImageAttachments: []corea2a.ImageAttachment{{
			FileURL:  "https://hub.local/files/img-1",
			Filename: "diagram.png",
			MimeType: "image/png",
		}},
	})
	if err != nil {
		t.Fatalf("AddDiscussionMessage: %v", err)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	found := false
	for _, msg := range detail.Messages {
		if len(msg.ImageAttachments) == 1 && msg.ImageAttachments[0].Filename == "diagram.png" {
			found = true
		}
	}
	if !found {
		t.Fatalf("attachment metadata not found in detail messages: %+v", detail.Messages)
	}
}

func TestGroupDiscussionAllowsStreamEndAndAttachmentOnlyMessages(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "stream", Question: "Reply"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", created.Discussion.ID, corea2a.GroupDiscussionMessage{FromID: "maclaw-a", Kind: corea2a.MessageStreamEnd}); err != nil {
		t.Fatalf("stream_end without content should be accepted: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", created.Discussion.ID, corea2a.GroupDiscussionMessage{FromID: "maclaw-a", Kind: corea2a.MessageStatement, FileAttachments: []corea2a.FileAttachment{{FileURL: "https://hub.local/files/doc", Filename: "doc.pdf"}}}); err != nil {
		t.Fatalf("attachment-only message should be accepted: %v", err)
	}
}
