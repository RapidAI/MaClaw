package structureddata

import "testing"

func TestBusinessRuleConditionApplies(t *testing.T) {
	data := map[string]any{
		"amount": 125000,
		"status": "pending",
		"customer": map[string]any{
			"tier": "strategic",
		},
	}
	cases := []struct {
		name      string
		condition BusinessRuleCondition
		want      bool
	}{
		{name: "numeric threshold", condition: BusinessRuleCondition{Field: "amount", Op: "gte", Value: 100000}, want: true},
		{name: "nested path", condition: BusinessRuleCondition{Field: "customer.tier", Op: "eq", Value: "strategic"}, want: true},
		{name: "membership", condition: BusinessRuleCondition{Field: "status", Op: "in", Value: []any{"pending", "approved"}}, want: true},
		{name: "missing", condition: BusinessRuleCondition{Field: "contract_ref", Op: "not_exists"}, want: true},
		{name: "not in", condition: BusinessRuleCondition{Field: "status", Op: "not_in", Value: []any{"cancelled", "rejected"}}, want: true},
		{name: "failed threshold", condition: BusinessRuleCondition{Field: "amount", Op: "lt", Value: 100000}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := businessRuleConditionApplies(tc.condition, data); got != tc.want {
				t.Fatalf("businessRuleConditionApplies()=%v want %v", got, tc.want)
			}
		})
	}
}

func TestBusinessRuleConditionsMode(t *testing.T) {
	data := map[string]any{"amount": 12, "status": "pending"}
	rule := BusinessRuleDefinition{
		ConditionsMode: "any",
		Conditions: []BusinessRuleCondition{
			{Field: "amount", Op: "gte", Value: 100000},
			{Field: "status", Op: "eq", Value: "pending"},
		},
	}
	if !businessRuleAppliesToData(rule, data) {
		t.Fatalf("expected any-mode rule to apply")
	}
	rule.ConditionsMode = "all"
	if businessRuleAppliesToData(rule, data) {
		t.Fatalf("expected all-mode rule not to apply")
	}
	explanation := evaluateBusinessRuleConditions(rule, data)
	if explanation.Applies || explanation.ConditionsMode != "all" || len(explanation.ConditionResults) != 2 || explanation.ConditionResults[0].Matched || !explanation.ConditionResults[1].Matched {
		t.Fatalf("unexpected rule explanation: %#v", explanation)
	}
}
