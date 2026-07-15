package main

import (
	"testing"
)

func TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID:   "layout-governance-only",
		Name: "Governance Layout Only",
		Kind: "enterprise_normal_app",
		App: map[string]any{
			"id":   "layout-governance-only",
			"name": "Governance Layout Only",
			"kind": "enterprise_normal_app",
			"governance": map[string]any{
				"workspaceLayout": map[string]any{
					"schema":        "maclaw.app.ui.v1",
					"entry":         "business_workspace",
					"template":      "dashboard",
					"density":       "spacious",
					"primaryRegion": "right",
					"outputRegion":  "modal",
					"regions": []any{
						map[string]any{"id": "operation_form", "role": "input", "placement": "right"},
						map[string]any{"id": "record_list", "role": "record_list", "placement": "center"},
						map[string]any{"id": "result_panel", "role": "output", "placement": "modal"},
					},
				},
			},
		},
	}

	layout := maclawAppWorkspaceLayoutMetadataForEntry(entry)
	if layout == nil {
		t.Fatal("expected governance workspace layout fallback")
	}
	if layout["entry"] != "business_workspace" || layout["template"] != "dashboard" || layout["density"] != "spacious" {
		t.Fatalf("unexpected governance workspace layout identity: %#v", layout)
	}
	if layout["primaryRegion"] != "right" || layout["primary_region"] != "right" || layout["outputRegion"] != "modal" || layout["output_region"] != "modal" {
		t.Fatalf("workspace layout should expose camel and snake region aliases: %#v", layout)
	}
	if layout["regionCount"] != 3 || layout["region_count"] != 3 {
		t.Fatalf("workspace layout should expose camel and snake region counts: %#v", layout)
	}
	regions, ok := layout["regions"].([]any)
	if !ok || len(regions) != 3 {
		t.Fatalf("workspace layout should preserve regions: %#v", layout["regions"])
	}
}
