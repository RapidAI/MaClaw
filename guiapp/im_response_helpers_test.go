package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestTurnMetaResponseField_Compact(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "aux", Model: "m-flash"},
		1200, 340, 50, "light", 3800, false, false, false, nil,
	)
	if len(fields) != 1 || fields[0].Label != "Turn" {
		t.Fatalf("fields=%+v", fields)
	}
	if !fields[0].Internal {
		t.Fatalf("Turn field must be marked internal: %+v", fields[0])
	}
	v := fields[0].Value
	for _, part := range []string{"fast", "aux", "m-flash", "in=1.2k", "out=340", "cache=50", "prompt=light(-3.8k)"} {
		if !strings.Contains(v, part) {
			t.Fatalf("missing %q in %q", part, v)
		}
	}
	if strings.Contains(v, "¥") || strings.Contains(v, "$") {
		t.Fatalf("Turn chip must not show cost: %q", v)
	}
}

func TestTurnMetaResponseField_Upgraded(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "reasoning", Source: "primary", Model: "m1"},
		2000, 400, 0, "full", 0, true, false, false, nil,
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
		100, 20, 0, "full", 0, false, true, false, nil,
	)
	if len(fields) != 1 || !strings.Contains(fields[0].Value, "prompt=full(ab)") {
		t.Fatalf("fields=%+v", fields)
	}
}

func TestTurnMetaResponseField_SoftFull(t *testing.T) {
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "aux", Model: "m1"},
		10, 5, 0, "full", 0, false, false, true, nil,
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
		100, 20, 0, "", 0, false, false, false, nil,
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

func TestTurnMetaResponseField_OfficialCreditsLast(t *testing.T) {
	credits := 0.5
	fields := turnMetaResponseField(
		modelRouteDecision{Task: "fast", Source: "primary", Model: "auto"},
		1000, 804, 0, "light", 0, false, false, false, &credits,
	)
	if len(fields) != 1 {
		t.Fatalf("fields=%+v", fields)
	}
	if !strings.HasSuffix(fields[0].Value, "credits=0.5") {
		t.Fatalf("value=%q", fields[0].Value)
	}
	if strings.Contains(fields[0].Value, "¥") {
		t.Fatalf("credits must not bring back a money estimate: %q", fields[0].Value)
	}
}

func TestNoteOfficialTurnCreditsIncludesBonusBeforeChip(t *testing.T) {
	telemetry := &agentLoopTelemetry{CreditsDeducted: 1, CreditsDeductedSet: true}
	noteOfficialTurnCredits(telemetry, true, 0.25)
	noteOfficialTurnCredits(telemetry, false, 9)
	noteOfficialTurnCredits(nil, true, 1)
	resp := &IMAgentResponse{}
	telemetry.Attach(resp)
	var turn string
	for _, field := range resp.Fields {
		if field.Label == "Turn" {
			turn = field.Value
		}
	}
	if !strings.HasSuffix(turn, "credits=1.25") {
		t.Fatalf("turn=%q", turn)
	}
	if resp.CreditsDeducted == nil || *resp.CreditsDeducted != 1.25 {
		t.Fatalf("credits=%v", resp.CreditsDeducted)
	}
}

func TestSharedLoopTurnCreditsReachChip(t *testing.T) {
	telemetry := &agentLoopTelemetry{}
	resp := &IMAgentResponse{}
	attachSharedLoopTurn(telemetry, &sharedAgentLoopCallbacks{
		llmCfg: corelib.MaclawLLMConfig{ProviderName: "MaClaw官方"},
	}, agent.TurnUsage{InputTokens: 10, OutputTokens: 4, CreditsDeducted: 0.5, CreditsReported: true}, resp)
	var turn string
	for _, field := range resp.Fields {
		if field.Label == "Turn" {
			turn = field.Value
		}
	}
	if !strings.HasSuffix(turn, "credits=0.5") {
		t.Fatalf("turn=%q", turn)
	}
	otherTelemetry := &agentLoopTelemetry{}
	other := &IMAgentResponse{}
	attachSharedLoopTurn(otherTelemetry, &sharedAgentLoopCallbacks{
		llmCfg: corelib.MaclawLLMConfig{ProviderName: "other"},
	}, agent.TurnUsage{CreditsDeducted: 9, CreditsReported: true, Provider: "other"}, other)
	for _, field := range other.Fields {
		if field.Label == "Turn" && strings.Contains(field.Value, "credits=") {
			t.Fatalf("non-official turn showed credits: %q", field.Value)
		}
	}
}

func TestTurnMetaResponseField_Empty(t *testing.T) {
	if fields := turnMetaResponseField(modelRouteDecision{}, 0, 0, 0, "", 0, false, false, false, nil); len(fields) != 0 {
		t.Fatalf("expected empty, got %+v", fields)
	}
}
