package main

import (
	"strings"
	"testing"
)

func TestRememberAgentViewOpenAssignsMonotonicRevision(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{
		"type":  "form",
		"id":    "skill:run:demo",
		"title": "Demo",
		"meta": map[string]interface{}{
			"schemaVersion": "abc123",
			"schemaSource":  "skill.adapter",
			"schemaID":      "demo",
		},
		"fields": []map[string]interface{}{
			{"name": "input", "type": "text"},
		},
	}
	rev1 := app.rememberAgentViewOpen(view, 10)
	if rev1 < 1 {
		t.Fatalf("rev1 = %d", rev1)
	}
	meta := view["meta"].(map[string]interface{})
	if meta["viewRevision"] != rev1 {
		t.Fatalf("meta.viewRevision = %v, want %d", meta["viewRevision"], rev1)
	}
	// Hidden field should be present.
	found := false
	for _, f := range view["fields"].([]map[string]interface{}) {
		if f["name"] == agentViewRevisionField {
			found = true
			if parseAgentViewInt64(f["value"]) != rev1 {
				t.Fatalf("hidden revision = %v", f["value"])
			}
		}
	}
	if !found {
		t.Fatal("missing hidden revision field")
	}

	rev2 := app.rememberAgentViewOpen(view, 11)
	if rev2 <= rev1 {
		t.Fatalf("expected monotonic revision, rev1=%d rev2=%d", rev1, rev2)
	}
}

func TestValidateAgentViewSubmitRevisionRejectsStale(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{
		"type": "form",
		"id":   "tool:run:x",
		"title": "X",
		"meta": map[string]interface{}{
			"schemaVersion": "v1",
		},
	}
	current := app.rememberAgentViewOpen(view, 100)
	// Open again to supersede.
	_ = app.rememberAgentViewOpen(view, 101)

	stale := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:       "tool:run:x",
		ViewRevision: current, // older than latest
		Data:         map[string]interface{}{"input": "1"},
	})
	if stale == nil || stale.Error != agentViewStaleRevisionError {
		t.Fatalf("expected stale rejection, got %#v", stale)
	}
	if !strings.Contains(stale.Text, "过期") && !strings.Contains(strings.ToLower(stale.Text), "out of date") {
		t.Fatalf("user message = %q", stale.Text)
	}

	// Matching latest revision is accepted.
	rec, ok := app.agentViewOpenRecord("tool:run:x")
	if !ok {
		t.Fatal("missing open record")
	}
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:        "tool:run:x",
		ViewRevision:  rec.Revision,
		SchemaVersion: "v1",
		Data:          map[string]interface{}{},
	}); got != nil {
		t.Fatalf("current revision should pass: %#v", got)
	}
}

func TestValidateAgentViewSubmitRevisionSchemaMismatch(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{
		"id":    "mcp:call:y",
		"type":  "form",
		"title": "Y",
		"meta":  map[string]interface{}{"schemaVersion": "schema-a"},
	}
	rev := app.rememberAgentViewOpen(view, 5)
	got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:        "mcp:call:y",
		ViewRevision:  rev,
		SchemaVersion: "schema-b",
	})
	if got == nil || got.Error != agentViewSchemaMismatchError {
		t.Fatalf("expected schema mismatch, got %#v", got)
	}
}

func TestValidateAgentViewSubmitRevisionLegacyOmitPasses(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{
		"id":    "legacy",
		"type":  "form",
		"title": "L",
		"meta":  map[string]interface{}{"schemaVersion": "s"},
	}
	_ = app.rememberAgentViewOpen(view, 3)
	// Old clients omit view_revision — still accepted for compatibility.
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID: "legacy",
		Data:   map[string]interface{}{"x": 1},
	}); got != nil {
		t.Fatalf("omit revision should pass: %#v", got)
	}
}

func TestHandleAgentViewSubmitPayloadStaleDoesNotForget(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{
		"id":    "skill:run:z",
		"type":  "form",
		"title": "Z",
		"meta":  map[string]interface{}{"schemaVersion": "s1"},
	}
	old := app.rememberAgentViewOpen(view, 1)
	_ = app.rememberAgentViewOpen(view, 2)
	resp := app.handleAgentViewSubmitPayload(AgentViewSubmitPayload{
		ViewID:       "skill:run:z",
		ViewRevision: old,
		Data:         map[string]interface{}{},
	})
	if resp == nil || resp.Error != agentViewStaleRevisionError {
		t.Fatalf("resp = %#v", resp)
	}
	if _, ok := app.agentViewOpenRecord("skill:run:z"); !ok {
		t.Fatal("stale submit must not clear open record")
	}
}

func TestForgetAgentViewOpenOnClear(t *testing.T) {
	app := &App{}
	view := map[string]interface{}{"id": "v1", "type": "form", "title": "t", "meta": map[string]interface{}{}}
	_ = app.rememberAgentViewOpen(view, 1)
	app.forgetAgentViewOpen("v1")
	if _, ok := app.agentViewOpenRecord("v1"); ok {
		t.Fatal("expected forget")
	}
}
