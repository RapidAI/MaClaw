package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	record := loadAgentViewSchemaRecordForTest(t, "abc123")
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
	record = loadAgentViewSchemaRecordForTest(t, "abc123")
	if record.UseCount != 3 {
		t.Fatalf("expected persisted use_count to increment to 3, got %#v", record)
	}
}

func resetAgentViewSchemaPersistentStoreForTest() {
	agentViewSchemaPersistentStore.Lock()
	defer agentViewSchemaPersistentStore.Unlock()
	closeAgentViewSchemaStoreLocked()
}

func loadAgentViewSchemaRecordForTest(t *testing.T, version string) agentViewSchemaRecord {
	t.Helper()
	agentViewSchemaPersistentStore.Lock()
	defer agentViewSchemaPersistentStore.Unlock()
	if agentViewSchemaPersistentStore.database == nil {
		t.Fatal("schema database is not open")
	}
	var record agentViewSchemaRecord
	var firstSeen, lastSeen int64
	if err := agentViewSchemaPersistentStore.database.db.QueryRow(`SELECT version, source, schema_id, view_id, view_type, title,
		first_seen_unix_nano, last_seen_unix_nano, use_count FROM agent_view_schemas WHERE version=?`, version).Scan(
		&record.Version, &record.Source, &record.ID, &record.ViewID, &record.ViewType, &record.Title,
		&firstSeen, &lastSeen, &record.UseCount); err != nil {
		t.Fatal(err)
	}
	record.FirstSeen = agentViewSchemaTimeFromUnixNano(firstSeen)
	record.LastSeen = agentViewSchemaTimeFromUnixNano(lastSeen)
	return record
}

func TestRecordAgentViewSchemaMigratesLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_view_schemas.json")
	now := time.Now().Add(-time.Hour)
	data, err := json.Marshal(agentViewSchemaSnapshot{Records: []agentViewSchemaRecord{{
		Version: "legacy", Source: "tool.adapter", ID: "expense", FirstSeen: now, LastSeen: now, UseCount: 4,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	resetAgentViewSchemaPersistentStoreForTest()
	view := map[string]interface{}{
		"type": "form", "id": "tool:run:expense",
		"meta": map[string]interface{}{"schemaVersion": "legacy", "schemaSource": "tool.adapter", "schemaID": "expense"},
	}
	if err := recordAgentViewSchema(path, view); err != nil {
		t.Fatal(err)
	}
	record := loadAgentViewSchemaRecordForTest(t, "legacy")
	if record.UseCount != 5 || !record.FirstSeen.Equal(now) {
		t.Fatalf("legacy record = %#v", record)
	}
}

func TestRecordAgentViewSchemaPreservesLegacyZeroFirstSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_view_schemas.json")
	data, err := json.Marshal(agentViewSchemaSnapshot{Records: []agentViewSchemaRecord{{
		Version: "zero-time", Source: "tool.adapter", ID: "expense", UseCount: 2,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	resetAgentViewSchemaPersistentStoreForTest()
	view := map[string]interface{}{
		"type": "form", "id": "tool:run:expense",
		"meta": map[string]interface{}{"schemaVersion": "zero-time", "schemaSource": "tool.adapter", "schemaID": "expense"},
	}
	if err := recordAgentViewSchema(path, view); err != nil {
		t.Fatal(err)
	}
	record := loadAgentViewSchemaRecordForTest(t, "zero-time")
	if !record.FirstSeen.IsZero() {
		t.Fatalf("zero first_seen changed to %s", record.FirstSeen)
	}
	if record.LastSeen.IsZero() || record.UseCount != 3 {
		t.Fatalf("last_seen/use_count = %s/%d", record.LastSeen, record.UseCount)
	}
}
