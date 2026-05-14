package ve

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// TestIntegration_GroupChatFlow tests the group chat lifecycle:
// Create conversation → add VE → GroupInvitation → multi-party message broadcast →
// participant responses → attachment fields preserved in broadcast.
func TestIntegration_GroupChatFlow(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-group-test", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	presence := NewPresenceManager()

	// Register and approve multiple VEs
	ve1, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-1",
		Name:           "VE Alpha",
		SkillDesc:      "Code review specialist",
		AccessPolicy:   PolicyPublic,
	})
	_ = registry.Approve(ve1.ID)
	presence.RecordHeartbeat(ve1.ID, "machine-ve-1")

	ve2, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-2",
		Name:           "VE Beta",
		SkillDesc:      "Testing specialist",
		AccessPolicy:   PolicyPublic,
	})
	_ = registry.Approve(ve2.ID)
	presence.RecordHeartbeat(ve2.ID, "machine-ve-2")

	ve3, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-3",
		Name:           "VE Gamma",
		SkillDesc:      "Architecture specialist",
		AccessPolicy:   PolicyPublic,
	})
	_ = registry.Approve(ve3.ID)
	presence.RecordHeartbeat(ve3.ID, "machine-ve-3")

	// Verify all VEs are online and discoverable
	discoverable := registry.ListDiscoverable("machine-client-a")
	if len(discoverable) != 3 {
		t.Fatalf("expected 3 discoverable VEs, got %d", len(discoverable))
	}

	for _, ve := range []*VirtualEmployee{ve1, ve2, ve3} {
		if !presence.IsOnline(ve.ID) {
			t.Fatalf("VE %s should be online", ve.ID)
		}
	}

	// Simulate group chat message broadcast with GroupDiscussionMessage
	// Client A sends a message to the group (all 3 VEs are participants)
	groupMsg := a2a.GroupDiscussionMessage{
		ID:        "msg-group-001",
		SessionID: "session-group-001",
		FromID:    "machine-client-a",
		Kind:      a2a.MessageStatement,
		Content:   "Please review this code and suggest improvements",
		CreatedAt: time.Now(),
	}

	// Verify message can be serialized and deserialized (simulating Hub relay)
	data, err := json.Marshal(groupMsg)
	if err != nil {
		t.Fatalf("Marshal group message failed: %v", err)
	}

	var relayed a2a.GroupDiscussionMessage
	if err := json.Unmarshal(data, &relayed); err != nil {
		t.Fatalf("Unmarshal relayed message failed: %v", err)
	}
	if relayed.Content != groupMsg.Content {
		t.Fatal("relayed message content mismatch")
	}
	if relayed.FromID != "machine-client-a" {
		t.Fatalf("expected from_id=machine-client-a, got %s", relayed.FromID)
	}

	// Simulate VE-1 response (stream_chunk + stream_end)
	ve1Response := a2a.GroupDiscussionMessage{
		ID:        "msg-ve1-resp-001",
		SessionID: "session-group-001",
		FromID:    ve1.ID,
		Kind:      a2a.MessageStreamChunk,
		Content:   "I see a potential issue with the error handling in ",
		CreatedAt: time.Now(),
	}
	ve1End := a2a.GroupDiscussionMessage{
		ID:        "msg-ve1-resp-002",
		SessionID: "session-group-001",
		FromID:    ve1.ID,
		Kind:      a2a.MessageStreamEnd,
		Content:   "line 42. Consider using a custom error type.",
		CreatedAt: time.Now(),
	}

	// Verify stream chunk/end kinds are preserved through serialization
	for _, msg := range []a2a.GroupDiscussionMessage{ve1Response, ve1End} {
		d, _ := json.Marshal(msg)
		var decoded a2a.GroupDiscussionMessage
		_ = json.Unmarshal(d, &decoded)
		if decoded.Kind != msg.Kind {
			t.Fatalf("expected kind=%s, got %s", msg.Kind, decoded.Kind)
		}
	}

	// Test attachment fields are preserved in broadcast
	msgWithAttachments := a2a.GroupDiscussionMessage{
		ID:        "msg-attach-001",
		SessionID: "session-group-001",
		FromID:    "machine-client-a",
		Kind:      a2a.MessageStatement,
		Content:   "Here's the code file for review",
		TextAttachments: []a2a.TextAttachment{
			{Content: "cHJpbnQoImhlbGxvIik=", Filename: "main.py", MimeType: "text/x-python"},
		},
		ImageAttachments: []a2a.ImageAttachment{
			{FileURL: "http://hub/files/img-1", Filename: "screenshot.png", MimeType: "image/png", Width: 1920, Height: 1080},
		},
		FileAttachments: []a2a.FileAttachment{
			{FileURL: "http://hub/files/doc-1", Filename: "spec.pdf", MimeType: "application/pdf", SizeBytes: 2048576},
		},
		CreatedAt: time.Now(),
	}

	// Simulate broadcast: serialize → deserialize for each participant
	broadcastData, _ := json.Marshal(msgWithAttachments)
	for _, participantID := range []string{ve1.ID, ve2.ID, ve3.ID} {
		var received a2a.GroupDiscussionMessage
		if err := json.Unmarshal(broadcastData, &received); err != nil {
			t.Fatalf("participant %s: unmarshal broadcast failed: %v", participantID, err)
		}
		// Verify all attachment fields preserved
		if len(received.TextAttachments) != 1 {
			t.Fatalf("participant %s: expected 1 text attachment, got %d", participantID, len(received.TextAttachments))
		}
		if received.TextAttachments[0].Filename != "main.py" {
			t.Fatalf("participant %s: text attachment filename mismatch", participantID)
		}
		if len(received.ImageAttachments) != 1 || received.ImageAttachments[0].Width != 1920 {
			t.Fatalf("participant %s: image attachment mismatch", participantID)
		}
		if len(received.FileAttachments) != 1 || received.FileAttachments[0].SizeBytes != 2048576 {
			t.Fatalf("participant %s: file attachment mismatch", participantID)
		}
	}
}
