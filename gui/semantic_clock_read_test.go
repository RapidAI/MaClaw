package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func currentTimeClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelCurrentTime,
		Confidence: .98,
		ToolNames:  []string{"current_datetime", "web_search"},
	}
}

func TestIMSemanticClockUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	h.semanticTrustedClock = func(userID string) (string, error) {
		t.Fatalf("planning must not execute clock user=%q", userID)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "现在几点了", "lansenger", "root-clock", "turn-clock", currentTimeClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedClockAdapter || selection.FitProof.MatchedCapability != semanticTrustedClockCapability {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only clock must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	if name != "current_datetime" || definition["name"] != "current_datetime" {
		t.Fatalf("managed clock name=%q, want current_datetime", name)
	}
	if selection.AdapterName == "current_datetime" || selection.AdapterName == "web_search" {
		t.Fatalf("managed clock leaked registry adapter %q", selection.AdapterName)
	}
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if len(properties) != 0 {
		t.Fatalf("clock schema=%#v", properties)
	}
	for _, forbidden := range []string{"timezone", "format", "locale", "channel", "destination", "group_name", "query"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing clock schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedClockAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"timezone":"UTC","channel":"lansenger"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged clock fields=%q", got)
	}
}

func TestIMSemanticClockExecutesEmptyObjectWithoutLookup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	var seenUser string
	h.semanticTrustedClock = func(userID string) (string, error) {
		seenUser = userID
		return "2026-08-17 Monday ISO week 2026-W34 07:49:00 (timezone: Local)", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "what time is it now?", "lansenger", "root-clock-exec", "turn-clock-exec", currentTimeClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{}`)
	if !strings.Contains(got, "ISO week") || strings.Contains(got, "current_datetime") || strings.Contains(got, "web_search") {
		t.Fatalf("bound clock=%q", got)
	}
	if seenUser != "user-1" {
		t.Fatalf("principal=%q", seenUser)
	}
	if replay := cb.ExecuteTool(name, `{}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticClockRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	h.semanticTrustedClock = func(string) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "现在几点了", "lansenger", "root-clock-token", "turn-clock-token", currentTimeClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"query":"now"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "trusted_clock_arguments_rejected") {
		t.Fatalf("extra field=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "现在几点了", "lansenger", "root-clock-token-2", "turn-clock-token-2", currentTimeClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_clock_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.readTrustedClock(""); err == nil || !strings.Contains(err.Error(), "trusted_clock_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticClockReadsHostLocalTime(t *testing.T) {
	h := &IMMessageHandler{}
	before := time.Now()
	out, err := h.readTrustedClock("user-1")
	after := time.Now()
	if err != nil || !strings.Contains(out, "ISO week") || !strings.Contains(out, "timezone:") {
		t.Fatalf("clock=%q err=%v", out, err)
	}
	if strings.Contains(out, "current_datetime") || strings.Contains(out, "[file_base64") {
		t.Fatalf("clock leaked legacy/delivery names: %q", out)
	}
	parsed, err := time.ParseInLocation("2006-01-02 Monday ISO week 2006-W15 15:04:05", strings.Split(out, " (timezone:")[0], before.Location())
	if err != nil {
		// Format includes weekday name and ISO week; just check the date prefix.
		if !strings.HasPrefix(out, before.Format("2006-01-02")) && !strings.HasPrefix(out, after.Format("2006-01-02")) {
			t.Fatalf("clock date prefix=%q", out)
		}
		return
	}
	if parsed.Before(before.Add(-2*time.Second)) || parsed.After(after.Add(2*time.Second)) {
		t.Fatalf("clock=%q before=%s after=%s", out, before, after)
	}
}
