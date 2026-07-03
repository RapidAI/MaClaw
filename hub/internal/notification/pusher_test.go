package notification

import (
	"encoding/json"
	"testing"
)

type recordingMachineSender struct {
	sent map[string][]map[string]any
}

func (s *recordingMachineSender) SendToMachine(machineID string, msg any) error {
	if s.sent == nil {
		s.sent = make(map[string][]map[string]any)
	}
	asMap, ok := msg.(map[string]any)
	if !ok {
		s.sent[machineID] = append(s.sent[machineID], map[string]any{"unexpected": msg})
		return nil
	}
	s.sent[machineID] = append(s.sent[machineID], asMap)
	return nil
}

type staticOnlineMachines struct {
	ids []string
}

func (l staticOnlineMachines) ListOnlineMachineIDs() []string {
	return append([]string{}, l.ids...)
}

func TestPusherBroadcastToAllPreservesNotificationEnvelope(t *testing.T) {
	sender := &recordingMachineSender{}
	pusher := NewPusher(sender, staticOnlineMachines{ids: []string{"machine-a", "machine-b"}})
	envelope, err := BuildNotificationEnvelope(NotificationPushPayload{
		Action: "new",
		Notification: &Notification{
			ID:       "notif-1",
			Title:    "Hub broadcast",
			Content:  "hello",
			Category: CategorySystemAnnouncement,
			Priority: PriorityNormal,
			Status:   StatusPublished,
		},
	})
	if err != nil {
		t.Fatalf("BuildNotificationEnvelope: %v", err)
	}

	if err := pusher.BroadcastToAll(envelope); err != nil {
		t.Fatalf("BroadcastToAll: %v", err)
	}

	for _, machineID := range []string{"machine-a", "machine-b"} {
		items := sender.sent[machineID]
		if len(items) != 1 {
			t.Fatalf("%s sent count = %d, want 1", machineID, len(items))
		}
		raw, err := json.Marshal(items[0])
		if err != nil {
			t.Fatalf("marshal sent envelope: %v", err)
		}
		var got struct {
			Type    string `json:"type"`
			Payload struct {
				Action       string `json:"action"`
				Notification struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"notification"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal sent envelope: %v", err)
		}
		if got.Type != NotificationEnvelopeType {
			t.Fatalf("%s envelope type = %q, want %q", machineID, got.Type, NotificationEnvelopeType)
		}
		if got.Payload.Action != "new" || got.Payload.Notification.ID != "notif-1" || got.Payload.Notification.Title != "Hub broadcast" {
			t.Fatalf("%s payload = %+v", machineID, got.Payload)
		}
	}
}
