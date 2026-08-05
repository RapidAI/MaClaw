package main

import (
	"strings"
	"testing"
)

func TestTurnMetaResponseField_Compact(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "aux", Model: "m-flash"},
		1200, 340, 50, 0.0123, "light", 3800, false, false, false,
	)
	if len(fields) != 1 || fields[0].Label != "Turn" {
		t.Fatalf("fields=%+v", fields)
	}
	if !fields[0].Internal {
		t.Fatalf("Turn field must be marked internal: %+v", fields[0])
	}
	v := fields[0].Value
	for _, part := range []string{"fast", "aux", "m-flash", "in=1.2k", "out=340", "cache=50", "~¥0.0123", "prompt=light(-3.8k)"} {
		if !strings.Contains(v, part) {
			t.Fatalf("missing %q in %q", part, v)
		}
	}
}

func TestTurnMetaResponseField_Upgraded(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "reasoning", Source: "primary", Model: "m1"},
		2000, 400, 0, 0.02, "full", 0, true, false, false,
	)
	if len(fields) != 1 {
		t.Fatalf("fields=%+v", fields)
	}
	if !strings.Contains(fields[0].Value, "prompt=full(upgraded)") {
		t.Fatalf("value=%q", fields[0].Value)
	}
	if strings.Contains(fields[0].Value, "prompt=light") {
		t.Fatalf("upgraded should not show light: %q", fields[0].Value)
	}
}

func TestTurnMetaResponseField_ABSample(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "aux", Model: "m-flash"},
		100, 20, 0, 0, "full", 0, false, true, false,
	)
	if len(fields) != 1 || !strings.Contains(fields[0].Value, "prompt=full(ab)") {
		t.Fatalf("fields=%+v", fields)
	}
}

func TestTurnMetaResponseField_SoftFull(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "aux", Model: "m1"},
		10, 5, 0, 0, "full", 0, false, false, true,
	)
	if len(fields) != 1 || !strings.Contains(fields[0].Value, "prompt=full(soft)") {
		t.Fatalf("fields=%+v", fields)
	}
}

func TestTurnMetaResponseField_CostTierShadow(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{
			Task: "fast", Source: "aux", Model: "m-flash",
			CostTier: "c0", CostRouteMode: "shadow",
		},
		100, 20, 0, 0, "", 0, false, false, false,
	)
	if len(fields) != 1 || !strings.Contains(fields[0].Value, "tier=c0(shadow)") {
		t.Fatalf("fields=%+v", fields)
	}
	routeFields := modelRouteResponseFields(modelRouteDecision{
		Task: "fast", Model: "m1", CostTier: "c0", CostRouteMode: "shadow",
	})
	found := false
	for _, f := range routeFields {
		if f.Label == "Cost tier" && strings.Contains(f.Value, "c0") {
			if !f.Internal {
				t.Fatalf("cost tier field must be marked internal: %+v", f)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("route fields missing cost tier: %+v", routeFields)
	}
}

func TestTurnMetaResponseField_Empty(t *testing.T) {
	if fields := turnMetaResponseField(modelRouteDecision{}, 0, 0, 0, 0, "", 0, false, false, false); len(fields) != 0 {
		t.Fatalf("expected empty, got %+v", fields)
	}
}
