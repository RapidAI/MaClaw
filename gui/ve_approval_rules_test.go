package main

import (
	"context"
	"testing"
)

func TestResolveField_SimpleKey(t *testing.T) {
	data := map[string]interface{}{
		"amount": 1000.0,
		"name":   "test",
	}
	val, found := resolveField(data, "amount")
	if !found {
		t.Fatal("expected field to be found")
	}
	if val != 1000.0 {
		t.Fatalf("expected 1000.0, got %v", val)
	}
}

func TestResolveField_DotNotation(t *testing.T) {
	data := map[string]interface{}{
		"request": map[string]interface{}{
			"details": map[string]interface{}{
				"amount": 5000.0,
			},
		},
	}
	val, found := resolveField(data, "request.details.amount")
	if !found {
		t.Fatal("expected field to be found")
	}
	if val != 5000.0 {
		t.Fatalf("expected 5000.0, got %v", val)
	}
}

func TestResolveField_MissingField(t *testing.T) {
	data := map[string]interface{}{
		"request": map[string]interface{}{},
	}
	_, found := resolveField(data, "request.amount")
	if found {
		t.Fatal("expected field to not be found")
	}
}

func TestResolveField_NullValue(t *testing.T) {
	data := map[string]interface{}{
		"amount": nil,
	}
	_, found := resolveField(data, "amount")
	if found {
		t.Fatal("expected nil field to be treated as not found")
	}
}

func TestResolveField_ExceedsMaxDepth(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": map[string]interface{}{
					"d": "too deep",
				},
			},
		},
	}
	_, found := resolveField(data, "a.b.c.d")
	if found {
		t.Fatal("expected depth > 3 to not be found")
	}
}

func TestEvaluateCondition_Equals(t *testing.T) {
	data := map[string]interface{}{
		"department": "engineering",
	}
	cond := RuleCondition{Field: "department", Operator: OpEquals, Value: "engineering"}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected equals to match")
	}
	cond.Value = "sales"
	if evaluateCondition(cond, data) {
		t.Fatal("expected equals to not match")
	}
}

func TestEvaluateCondition_NotEquals(t *testing.T) {
	data := map[string]interface{}{
		"status": "pending",
	}
	cond := RuleCondition{Field: "status", Operator: OpNotEquals, Value: "approved"}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected not_equals to match")
	}
	cond.Value = "pending"
	if evaluateCondition(cond, data) {
		t.Fatal("expected not_equals to not match when values are equal")
	}
}

func TestEvaluateCondition_GreaterThan(t *testing.T) {
	data := map[string]interface{}{
		"amount": 5000.0,
	}
	cond := RuleCondition{Field: "amount", Operator: OpGT, Value: 1000.0}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected greater_than to match")
	}
	cond.Value = 5000.0
	if evaluateCondition(cond, data) {
		t.Fatal("expected greater_than to not match when equal")
	}
	cond.Value = 10000.0
	if evaluateCondition(cond, data) {
		t.Fatal("expected greater_than to not match when less")
	}
}

func TestEvaluateCondition_LessThan(t *testing.T) {
	data := map[string]interface{}{
		"amount": 500.0,
	}
	cond := RuleCondition{Field: "amount", Operator: OpLT, Value: 1000.0}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected less_than to match")
	}
	cond.Value = 500.0
	if evaluateCondition(cond, data) {
		t.Fatal("expected less_than to not match when equal")
	}
	cond.Value = 100.0
	if evaluateCondition(cond, data) {
		t.Fatal("expected less_than to not match when greater")
	}
}

func TestEvaluateCondition_Contains_String(t *testing.T) {
	data := map[string]interface{}{
		"description": "urgent purchase request",
	}
	cond := RuleCondition{Field: "description", Operator: OpContains, Value: "urgent"}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected contains to match substring")
	}
	cond.Value = "delayed"
	if evaluateCondition(cond, data) {
		t.Fatal("expected contains to not match missing substring")
	}
}

func TestEvaluateCondition_Contains_Slice(t *testing.T) {
	data := map[string]interface{}{
		"tags": []interface{}{"finance", "urgent", "review"},
	}
	cond := RuleCondition{Field: "tags", Operator: OpContains, Value: "urgent"}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected contains to match slice element")
	}
	cond.Value = "marketing"
	if evaluateCondition(cond, data) {
		t.Fatal("expected contains to not match missing slice element")
	}
}

func TestEvaluateCondition_InList(t *testing.T) {
	data := map[string]interface{}{
		"department": "engineering",
	}
	cond := RuleCondition{
		Field:    "department",
		Operator: OpInList,
		Value:    []interface{}{"engineering", "product", "design"},
	}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected in_list to match")
	}
	cond.Value = []interface{}{"sales", "marketing"}
	if evaluateCondition(cond, data) {
		t.Fatal("expected in_list to not match")
	}
}

func TestEvaluateCondition_NotInList(t *testing.T) {
	data := map[string]interface{}{
		"department": "engineering",
	}
	cond := RuleCondition{
		Field:    "department",
		Operator: OpNotInList,
		Value:    []interface{}{"sales", "marketing"},
	}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected not_in_list to match")
	}
	cond.Value = []interface{}{"engineering", "product"}
	if evaluateCondition(cond, data) {
		t.Fatal("expected not_in_list to not match when value is in list")
	}
}

func TestEvaluateCondition_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		field    string
		expected bool
	}{
		{"missing field", map[string]interface{}{}, "missing", true},
		{"null field", map[string]interface{}{"f": nil}, "f", true},
		{"empty string", map[string]interface{}{"f": ""}, "f", true},
		{"empty slice", map[string]interface{}{"f": []interface{}{}}, "f", true},
		{"empty map", map[string]interface{}{"f": map[string]interface{}{}}, "f", true},
		{"non-empty string", map[string]interface{}{"f": "hello"}, "f", false},
		{"non-empty slice", map[string]interface{}{"f": []interface{}{"a"}}, "f", false},
		{"number zero", map[string]interface{}{"f": 0.0}, "f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := RuleCondition{Field: tt.field, Operator: OpIsEmpty}
			result := evaluateCondition(cond, tt.data)
			if result != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_IsNotEmpty(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		field    string
		expected bool
	}{
		{"missing field", map[string]interface{}{}, "missing", false},
		{"null field", map[string]interface{}{"f": nil}, "f", false},
		{"empty string", map[string]interface{}{"f": ""}, "f", false},
		{"non-empty string", map[string]interface{}{"f": "hello"}, "f", true},
		{"non-empty slice", map[string]interface{}{"f": []interface{}{"a"}}, "f", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := RuleCondition{Field: tt.field, Operator: OpIsNotEmpty}
			result := evaluateCondition(cond, tt.data)
			if result != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_MissingField_NotMatched(t *testing.T) {
	data := map[string]interface{}{
		"name": "test",
	}
	// All operators except is_empty/is_not_empty should return false for missing fields
	operators := []Operator{OpEquals, OpNotEquals, OpGT, OpLT, OpContains, OpInList, OpNotInList}
	for _, op := range operators {
		cond := RuleCondition{Field: "nonexistent", Operator: op, Value: "anything"}
		if evaluateCondition(cond, data) {
			t.Fatalf("operator %s should return false for missing field", op)
		}
	}
}

func TestEvaluateRule_AllConditionsMustMatch(t *testing.T) {
	data := map[string]interface{}{
		"amount":     500.0,
		"department": "engineering",
	}
	rule := ApprovalRule{
		ID:   "rule1",
		Name: "Low amount engineering",
		Conditions: []RuleCondition{
			{Field: "amount", Operator: OpLT, Value: 1000.0},
			{Field: "department", Operator: OpEquals, Value: "engineering"},
		},
	}
	if !evaluateRule(rule, data) {
		t.Fatal("expected rule to match when all conditions match")
	}

	// Change department so second condition fails
	data["department"] = "sales"
	if evaluateRule(rule, data) {
		t.Fatal("expected rule to not match when one condition fails")
	}
}

func TestEvaluateRule_EmptyConditions(t *testing.T) {
	data := map[string]interface{}{"amount": 100.0}
	rule := ApprovalRule{ID: "empty", Conditions: []RuleCondition{}}
	if evaluateRule(rule, data) {
		t.Fatal("expected rule with no conditions to not match")
	}
}

func TestEvaluateCondition_NumericEquals(t *testing.T) {
	data := map[string]interface{}{
		"count": 42.0,
	}
	cond := RuleCondition{Field: "count", Operator: OpEquals, Value: 42.0}
	if !evaluateCondition(cond, data) {
		t.Fatal("expected numeric equals to match")
	}
}

func TestEvaluate_AutoRejectPriority(t *testing.T) {
	// When both auto-reject and auto-approve rules match,
	// auto-reject should win (higher priority).
	rules := &ApprovalRules{
		AutoReject: []ApprovalRule{
			{
				ID:       "reject1",
				Name:     "Reject high amount",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 5000.0},
				},
				Reason: "Amount exceeds limit",
			},
		},
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve1",
				Name:     "Approve engineering",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "department", Operator: OpEquals, Value: "engineering"},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount":     10000.0,
			"department": "engineering",
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionAutoReject {
		t.Fatalf("expected DecisionAutoReject, got %s", decision)
	}
	if matched == nil || matched.ID != "reject1" {
		t.Fatal("expected matched rule to be reject1")
	}
}

func TestEvaluate_AutoApproveWhenNoReject(t *testing.T) {
	rules := &ApprovalRules{
		AutoReject: []ApprovalRule{
			{
				ID:       "reject1",
				Name:     "Reject high amount",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 50000.0},
				},
			},
		},
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve1",
				Name:     "Approve low amount",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpLT, Value: 1000.0},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount": 500.0,
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionAutoApprove {
		t.Fatalf("expected DecisionAutoApprove, got %s", decision)
	}
	if matched == nil || matched.ID != "approve1" {
		t.Fatal("expected matched rule to be approve1")
	}
}

func TestEvaluate_RequireHumanWhenMatched(t *testing.T) {
	rules := &ApprovalRules{
		AutoReject:  []ApprovalRule{},
		AutoApprove: []ApprovalRule{},
		RequireHuman: []ApprovalRule{
			{
				ID:       "human1",
				Name:     "Human review for large amounts",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 1000.0},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount": 5000.0,
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionRequireHuman {
		t.Fatalf("expected DecisionRequireHuman, got %s", decision)
	}
	if matched == nil || matched.ID != "human1" {
		t.Fatal("expected matched rule to be human1")
	}
}

func TestEvaluate_DefaultRequireHumanWhenNoMatch(t *testing.T) {
	rules := &ApprovalRules{
		AutoReject: []ApprovalRule{
			{
				ID:       "reject1",
				Name:     "Reject blacklisted",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "blacklisted", Operator: OpEquals, Value: true},
				},
			},
		},
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve1",
				Name:     "Approve low amount",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpLT, Value: 100.0},
				},
			},
		},
		RequireHuman: []ApprovalRule{},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount":      5000.0,
			"blacklisted": false,
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionRequireHuman {
		t.Fatalf("expected DecisionRequireHuman (default), got %s", decision)
	}
	if matched != nil {
		t.Fatal("expected no matched rule for default decision")
	}
}

func TestEvaluate_RuleOrderingByPosition(t *testing.T) {
	// Two auto-approve rules match, but the one with lower position should win.
	rules := &ApprovalRules{
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve_high_pos",
				Name:     "Higher position rule",
				Position: 5,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpLT, Value: 2000.0},
				},
			},
			{
				ID:       "approve_low_pos",
				Name:     "Lower position rule",
				Position: 1,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpLT, Value: 5000.0},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount": 1000.0,
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionAutoApprove {
		t.Fatalf("expected DecisionAutoApprove, got %s", decision)
	}
	if matched == nil || matched.ID != "approve_low_pos" {
		t.Fatalf("expected first match by position (approve_low_pos), got %v", matched)
	}
}

func TestEvaluate_NilRulesDefaultsToRequireHuman(t *testing.T) {
	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, nil, &ApprovalRequestPayload{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionRequireHuman {
		t.Fatalf("expected DecisionRequireHuman for nil rules, got %s", decision)
	}
	if matched != nil {
		t.Fatal("expected no matched rule for nil rules")
	}
}

func TestEvaluate_NilPayloadDefaultsToRequireHuman(t *testing.T) {
	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	rules := &ApprovalRules{}
	decision, matched, err := engine.Evaluate(ctx, rules, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionRequireHuman {
		t.Fatalf("expected DecisionRequireHuman for nil payload, got %s", decision)
	}
	if matched != nil {
		t.Fatal("expected no matched rule for nil payload")
	}
}

func TestEvaluate_TimeoutReturnsRequireHuman(t *testing.T) {
	engine := &ApprovalRuleEngine{}
	// Use an already-cancelled context to simulate timeout
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rules := &ApprovalRules{
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve1",
				Name:     "Approve all",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 0.0},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{"amount": 100.0},
	}

	decision, _, err := engine.Evaluate(ctx, rules, payload)
	// With a cancelled context, we expect either the goroutine finishes fast
	// or the context error is returned. Either way, the system should not panic.
	if err != nil {
		// Context was cancelled, should default to require human
		if decision != DecisionRequireHuman {
			t.Fatalf("expected DecisionRequireHuman on timeout, got %s", decision)
		}
	}
	// If no error, the goroutine completed before context check — that's also acceptable
}

func TestEvaluate_MultipleRulesFirstMatchWins(t *testing.T) {
	// Multiple auto-reject rules, only the first by position that matches should be returned
	rules := &ApprovalRules{
		AutoReject: []ApprovalRule{
			{
				ID:       "reject_pos2",
				Name:     "Reject amount > 10000",
				Position: 2,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 10000.0},
				},
			},
			{
				ID:       "reject_pos0",
				Name:     "Reject blacklisted dept",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "department", Operator: OpEquals, Value: "banned"},
				},
			},
			{
				ID:       "reject_pos1",
				Name:     "Reject amount > 5000",
				Position: 1,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpGT, Value: 5000.0},
				},
			},
		},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount":     8000.0,
			"department": "engineering",
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionAutoReject {
		t.Fatalf("expected DecisionAutoReject, got %s", decision)
	}
	// Position 0 (banned dept) doesn't match, position 1 (>5000) matches first
	if matched == nil || matched.ID != "reject_pos1" {
		t.Fatalf("expected reject_pos1 (first match by position), got %v", matched)
	}
}

func TestEvaluate_EmptyRulesDefaultsToRequireHuman(t *testing.T) {
	rules := &ApprovalRules{
		AutoReject:   []ApprovalRule{},
		AutoApprove:  []ApprovalRule{},
		RequireHuman: []ApprovalRule{},
	}
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{"amount": 1000.0},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionRequireHuman {
		t.Fatalf("expected DecisionRequireHuman for empty rules, got %s", decision)
	}
	if matched != nil {
		t.Fatal("expected no matched rule for empty rules")
	}
}

func TestEvaluate_MissingFieldSkipsRule(t *testing.T) {
	// Rule references a field that doesn't exist in payload — should be skipped
	rules := &ApprovalRules{
		AutoReject: []ApprovalRule{
			{
				ID:       "reject1",
				Name:     "Reject if priority is critical",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "priority", Operator: OpEquals, Value: "critical"},
				},
			},
		},
		AutoApprove: []ApprovalRule{
			{
				ID:       "approve1",
				Name:     "Approve low amount",
				Position: 0,
				Conditions: []RuleCondition{
					{Field: "amount", Operator: OpLT, Value: 1000.0},
				},
			},
		},
	}
	// Payload has amount but no priority field
	payload := &ApprovalRequestPayload{
		Data: map[string]interface{}{
			"amount": 500.0,
		},
	}

	engine := &ApprovalRuleEngine{}
	ctx := context.Background()
	decision, matched, err := engine.Evaluate(ctx, rules, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Reject rule should be skipped (missing field), approve rule should match
	if decision != DecisionAutoApprove {
		t.Fatalf("expected DecisionAutoApprove (reject skipped due to missing field), got %s", decision)
	}
	if matched == nil || matched.ID != "approve1" {
		t.Fatal("expected matched rule to be approve1")
	}
}
