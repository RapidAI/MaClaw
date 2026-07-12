package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildAppView_RequiresAppIDAndMain(t *testing.T) {
	if _, err := BuildAppView(AppViewBuildInput{Title: "x"}); err == nil {
		t.Fatal("expected error without appId")
	}
	if _, err := BuildAppView(AppViewBuildInput{AppID: "expense"}); err == nil {
		t.Fatal("expected error without main")
	}
	view, err := BuildAppView(AppViewBuildInput{
		AppID:     "expense",
		SessionID: "u1",
		Title:     "报销",
		Layout:    "workspace",
		Main: map[string]interface{}{
			"type":  "form",
			"id":    "expense:form",
			"title": "填写报销",
			"fields": []map[string]interface{}{
				{"name": "amount", "type": "number", "required": true},
			},
		},
		Nav: []map[string]interface{}{
			{"id": "form", "label": "表单", "targetViewId": "expense:form"},
		},
	})
	if err != nil {
		t.Fatalf("BuildAppView: %v", err)
	}
	if view["type"] != appViewType || view["schema"] != appViewSchemaV1 {
		t.Fatalf("type/schema = %v / %v", view["type"], view["schema"])
	}
	if view["id"] != "app:expense:u1" {
		t.Fatalf("id = %v", view["id"])
	}
	if view["appId"] != "expense" || view["sessionId"] != "u1" {
		t.Fatalf("app/session = %v / %v", view["appId"], view["sessionId"])
	}
	meta, _ := view["meta"].(map[string]interface{})
	if meta == nil || strings.TrimSpace(fmt.Sprint(meta["schemaVersion"])) == "" {
		t.Fatalf("expected schemaVersion, meta=%#v", meta)
	}
	if !IsAppView(view) {
		t.Fatal("IsAppView false")
	}
	if AppViewAppID(view) != "expense" || AppViewSessionID(view) != "u1" {
		t.Fatalf("extractors app=%q session=%q", AppViewAppID(view), AppViewSessionID(view))
	}
}

func TestBuildAppView_RejectsNestedAppView(t *testing.T) {
	_, err := BuildAppView(AppViewBuildInput{
		AppID: "x",
		Main: map[string]interface{}{
			"type":  appViewType,
			"title": "nested",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("expected nest error, got %v", err)
	}
}

func TestStrictAppViewSubmitRequiresRevision(t *testing.T) {
	app := &App{}
	view, err := BuildAppView(AppViewBuildInput{
		AppID: "orders",
		Title: "订单",
		Main: map[string]interface{}{
			"type":  "form",
			"id":    "orders:main",
			"title": "列表",
			"fields": []map[string]interface{}{
				{"name": "q", "type": "text"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rev := app.rememberAgentViewOpen(view, 50)
	if rev <= 0 {
		t.Fatalf("rev=%d", rev)
	}
	rec, ok := app.agentViewOpenRecord(fmt.Sprint(view["id"]))
	if !ok || !rec.Strict {
		t.Fatalf("expected strict open record: %#v ok=%v", rec, ok)
	}

	// Omit revision → reject in strict mode.
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID: fmt.Sprint(view["id"]),
		AppID:  "orders",
		Data:   map[string]interface{}{},
	}); got == nil || got.Error != agentViewMissingRevisionErr {
		t.Fatalf("omit revision: %#v", got)
	}

	// Wrong app id.
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:        fmt.Sprint(view["id"]),
		ViewRevision:  rev,
		SchemaVersion: rec.SchemaVersion,
		AppID:         "other",
	}); got == nil || got.Error != agentViewAppIDMismatchErr {
		t.Fatalf("app mismatch: %#v", got)
	}

	// Happy path.
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:        fmt.Sprint(view["id"]),
		ViewRevision:  rev,
		SchemaVersion: rec.SchemaVersion,
		AppID:         "orders",
		SessionID:     AppViewSessionID(view),
	}); got != nil {
		t.Fatalf("strict ok: %#v", got)
	}
}

func TestStrictAppViewStaleAfterReopen(t *testing.T) {
	app := &App{}
	view, err := BuildAppView(AppViewBuildInput{
		AppID: "crm",
		Title: "CRM",
		Main:  map[string]interface{}{"type": "form", "id": "crm:f", "title": "F", "fields": []map[string]interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	old := app.rememberAgentViewOpen(view, 1)
	newRev := app.rememberAgentViewOpen(view, 2)
	if newRev <= old {
		t.Fatalf("revs %d %d", old, newRev)
	}
	rec, _ := app.agentViewOpenRecord(fmt.Sprint(view["id"]))
	if got := app.validateAgentViewSubmitRevision(AgentViewSubmitPayload{
		ViewID:        fmt.Sprint(view["id"]),
		ViewRevision:  old,
		SchemaVersion: rec.SchemaVersion,
		AppID:         "crm",
	}); got == nil || got.Error != agentViewStaleRevisionError {
		t.Fatalf("stale: %#v", got)
	}
}
