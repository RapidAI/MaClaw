package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentViewSchemaVersionStableAndChangesWithContract(t *testing.T) {
	base := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"amount": map[string]interface{}{"type": "number"},
		},
	}
	same := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"amount": map[string]interface{}{"type": "number"},
		},
	}
	changed := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"amount": map[string]interface{}{"type": "number"},
			"reason": map[string]interface{}{"type": "string"},
		},
	}

	v1 := agentViewSchemaVersion("tool.adapter", "expense", base)
	v2 := agentViewSchemaVersion("tool.adapter", "expense", same)
	v3 := agentViewSchemaVersion("tool.adapter", "expense", changed)

	if v1 == "" {
		t.Fatal("expected non-empty schema version")
	}
	if v1 != v2 {
		t.Fatalf("expected equivalent contracts to reuse schema version, got %q and %q", v1, v2)
	}
	if v1 == v3 {
		t.Fatalf("expected changed contract to produce a new schema version, got %q", v3)
	}
}

func TestAttachAgentViewSchemaVersionAddsMetadataAndHiddenField(t *testing.T) {
	view := map[string]interface{}{
		"type": "form",
		"id":   "tool:run:expense",
		"meta": map[string]interface{}{"source": "tool.adapter"},
		"fields": []map[string]interface{}{
			{"name": "amount", "type": "number"},
		},
	}

	attachAgentViewSchemaVersion(view, "tool.adapter", "expense", map[string]interface{}{"amount": "number"})

	meta, _ := view["meta"].(map[string]interface{})
	version, _ := meta["schemaVersion"].(string)
	if version == "" || meta["schemaSource"] != "tool.adapter" || meta["schemaID"] != "expense" {
		t.Fatalf("expected schema metadata, got %#v", meta)
	}
	fields := view["fields"].([]map[string]interface{})
	for _, field := range fields {
		if field["name"] == agentViewSchemaVersionField {
			if field["type"] != "hidden" || field["value"] != version {
				t.Fatalf("unexpected schema version hidden field: %#v", field)
			}
			return
		}
	}
	t.Fatalf("expected schema version hidden field, got %#v", fields)
}

func TestRecordAgentViewSchemaPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_view_schemas.json")
	view := map[string]interface{}{
		"type":  "form",
		"id":    "tool:run:expense",
		"title": "Run expense",
		"meta": map[string]interface{}{
			"schemaVersion": "abc123",
			"schemaSource":  "tool.adapter",
			"schemaID":      "expense",
		},
	}

	resetAgentViewSchemaPersistentStoreForTest()
	if err := recordAgentViewSchema(path, view); err != nil {
		t.Fatalf("recordAgentViewSchema first call error = %v", err)
	}
	if err := recordAgentViewSchema(path, view); err != nil {
		t.Fatalf("recordAgentViewSchema second call error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var snapshot agentViewSchemaSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(snapshot.Records) != 1 {
		t.Fatalf("expected one schema record, got %#v", snapshot.Records)
	}
	record := snapshot.Records[0]
	if record.Version != "abc123" || record.Source != "tool.adapter" || record.ID != "expense" {
		t.Fatalf("unexpected schema record identity: %#v", record)
	}
	if record.ViewID != "tool:run:expense" || record.ViewType != "form" || record.Title != "Run expense" {
		t.Fatalf("unexpected schema record view fields: %#v", record)
	}
	if record.UseCount != 2 {
		t.Fatalf("expected use_count 2, got %#v", record)
	}
	if record.FirstSeen.IsZero() || record.LastSeen.IsZero() || record.LastSeen.Before(record.FirstSeen) {
		t.Fatalf("unexpected timestamps: %#v", record)
	}

	resetAgentViewSchemaPersistentStoreForTest()
	if err := recordAgentViewSchema(path, view); err != nil {
		t.Fatalf("recordAgentViewSchema reload call error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after reload error = %v", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("Unmarshal after reload error = %v", err)
	}
	if len(snapshot.Records) != 1 || snapshot.Records[0].UseCount != 3 {
		t.Fatalf("expected persisted use_count to increment to 3, got %#v", snapshot.Records)
	}
}

func resetAgentViewSchemaPersistentStoreForTest() {
	agentViewSchemaPersistentStore.Lock()
	defer agentViewSchemaPersistentStore.Unlock()
	agentViewSchemaPersistentStore.loadedPath = ""
	agentViewSchemaPersistentStore.items = map[string]agentViewSchemaRecord{}
}
