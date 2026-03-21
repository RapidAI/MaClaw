package im

import "testing"

func machines(names ...string) []OnlineMachineInfo {
	var out []OnlineMachineInfo
	for _, n := range names {
		out = append(out, OnlineMachineInfo{MachineID: "id_" + n, Name: n, LLMConfigured: true})
	}
	return out
}

func TestRuleEngine_AtNamePrefix(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("@MacBook 你好", ms, "", true, false)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target, got %s", d.Action)
	}
	if d.TargetID != "id_MacBook" {
		t.Fatalf("expected target id_MacBook, got %s", d.TargetID)
	}
}

func TestRuleEngine_AtNameNotFound(t *testing.T) {
	e := &RuleEngine{}
	// Need multiple machines so rule 2 (single device) doesn't kick in.
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("@Unknown hello", ms, "", true, false)
	// @name not matched → falls through to need_classification (LLM enabled)
	if d.Action != ActionNeedClassification {
		t.Fatalf("expected need_classification for unmatched @name, got %s", d.Action)
	}
}

func TestRuleEngine_SingleDeviceDirectForward(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook")
	d := e.Evaluate("hello", ms, "", true, false)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target for single device, got %s", d.Action)
	}
	if d.TargetID != "id_MacBook" {
		t.Fatalf("expected id_MacBook, got %s", d.TargetID)
	}
}

func TestRuleEngine_SingleDeviceSmartRouteEnabled(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook")
	// smart_route_single_device=true → skip rule 2, fall to need_classification
	d := e.Evaluate("hello", ms, "", true, true)
	if d.Action != ActionNeedClassification {
		t.Fatalf("expected need_classification with smart_route enabled, got %s", d.Action)
	}
}

func TestRuleEngine_SelectedDevice(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("hello", ms, "id_iMac", true, false)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target for selected device, got %s", d.Action)
	}
	if d.TargetID != "id_iMac" {
		t.Fatalf("expected id_iMac, got %s", d.TargetID)
	}
}

func TestRuleEngine_SelectedDeviceOffline(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook") // iMac is offline
	d := e.Evaluate("hello", ms, "id_iMac", true, false)
	// Selected device offline → single device rule kicks in (rule 2)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target (single device fallback), got %s", d.Action)
	}
}

func TestRuleEngine_BroadcastMode(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("hello", ms, broadcastMachineID, true, false)
	if d.Action != ActionBroadcast {
		t.Fatalf("expected broadcast, got %s", d.Action)
	}
}

func TestRuleEngine_NoRuleMatchedWithLLM(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("hello", ms, "", true, false)
	if d.Action != ActionNeedClassification {
		t.Fatalf("expected need_classification, got %s", d.Action)
	}
}

func TestRuleEngine_NoRuleMatchedNoLLM(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	d := e.Evaluate("hello", ms, "", false, false)
	if d.Action != ActionPassthrough {
		t.Fatalf("expected passthrough without LLM, got %s", d.Action)
	}
}

func TestRuleEngine_PriorityAtNameOverBroadcast(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook", "iMac")
	// Even in broadcast mode, @name should take priority
	d := e.Evaluate("@MacBook check this", ms, broadcastMachineID, true, false)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target (@name priority), got %s", d.Action)
	}
	if d.TargetID != "id_MacBook" {
		t.Fatalf("expected id_MacBook, got %s", d.TargetID)
	}
}

func TestRuleEngine_AtNameCaseInsensitive(t *testing.T) {
	e := &RuleEngine{}
	ms := machines("MacBook")
	d := e.Evaluate("@macbook hi", ms, broadcastMachineID, true, false)
	if d.Action != ActionRouteToTarget {
		t.Fatalf("expected route_to_target, got %s", d.Action)
	}
}
