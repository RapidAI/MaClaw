package guiapp

import (
	"encoding/json"
	"testing"
)

func TestDesktopBotInfoKeepsHubCreatedAt(t *testing.T) {
	const created = "2024-01-15T00:30:00Z"
	listBody := []byte(`{"items":[{"id":"bot_1","name":"值班","description":"晚上","instance_id":"inst_1","created_at":"` + created + `"}]}`)
	var listed struct {
		Items []hubBot `json:"items"`
	}
	if err := json.Unmarshal(listBody, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	fromList := desktopBotInfo(listed.Items[0])
	if fromList.CreatedAt != created || fromList.Title != "值班" || fromList.ID != "bot_1" {
		t.Fatalf("list info = %+v", fromList)
	}

	var createdBot hubBot
	if err := json.Unmarshal([]byte(`{"id":"bot_new","name":"Bot 2","description":"desk","instance_id":"inst_new","created_at":"2023-11-04T16:45:00Z"}`), &createdBot); err != nil {
		t.Fatal(err)
	}
	fromCreate := desktopBotInfo(createdBot)
	if fromCreate.CreatedAt != "2023-11-04T16:45:00Z" || fromCreate.ID != "bot_new" {
		t.Fatalf("create info = %+v", fromCreate)
	}

	raw, err := json.Marshal(fromList)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["created_at"] != created {
		t.Fatalf("wire created_at = %#v", wire["created_at"])
	}
}

func TestDesktopBotInfoKeepsTheComputerNode(t *testing.T) {
	var listed struct {
		Items []hubBot `json:"items"`
	}
	raw := []byte(`{"items":[{"id":"bot_local","name":"实验室","instance_id":"inst_l","node_id":"dn_lab"},{"id":"bot_cloud","name":"云端","instance_id":"inst_c"}]}`)
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	local := desktopBotInfo(listed.Items[0])
	cloud := desktopBotInfo(listed.Items[1])
	if local.NodeID != "dn_lab" {
		t.Fatalf("local node = %q", local.NodeID)
	}
	if cloud.NodeID != "" {
		t.Fatalf("cloud node = %q", cloud.NodeID)
	}
	wire, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if got["node_id"] != "dn_lab" {
		t.Fatalf("wire node_id = %#v", got["node_id"])
	}
}
