package main

import (
	"context"
	"sort"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/condeval"
)

// ApprovalRuleEngine evaluates three-way routing rules against a request payload.
type ApprovalRuleEngine struct{}

// RoutingDecision represents the outcome of rule evaluation.
type RoutingDecision string

const (
	DecisionAutoApprove  RoutingDecision = "auto_approve"
	DecisionAutoReject   RoutingDecision = "auto_reject"
	DecisionRequireHuman RoutingDecision = "require_human"
)

// ApprovalRules contains the three categories of routing rules.
type ApprovalRules struct {
	AutoReject   []ApprovalRule `json:"auto_reject"`
	AutoApprove  []ApprovalRule `json:"auto_approve"`
	RequireHuman []ApprovalRule `json:"require_human"`
}

// ApprovalRule is a single condition-based routing rule.
type ApprovalRule struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Position   int             `json:"position"` // ordering within category
	Conditions []RuleCondition `json:"conditions"`
	Reason     string          `json:"reason,omitempty"` // for rejection messages
}

// RuleCondition is a single field comparison.
type RuleCondition struct {
	Field    string      `json:"field"` // dot-notation path, max depth 3
	Operator Operator    `json:"operator"`
	Value    interface{} `json:"value"`
}

// Operator defines the comparison operator for a rule condition.
type Operator string

const (
	OpEquals     Operator = "equals"
	OpNotEquals  Operator = "not_equals"
	OpGT         Operator = "greater_than"
	OpLT         Operator = "less_than"
	OpContains   Operator = "contains"
	OpInList     Operator = "in_list"
	OpNotInList  Operator = "not_in_list"
	OpIsEmpty    Operator = "is_empty"
	OpIsNotEmpty Operator = "is_not_empty"
)

// ApprovalRequestPayload is the data against which rules are evaluated.
type ApprovalRequestPayload struct {
	Data map[string]interface{} `json:"data"`
}

// resolveField extracts a value from the payload using dot-notation path.
// Thin wrapper over condeval.ResolveField for backward compatibility.
func resolveField(data map[string]interface{}, fieldPath string) (interface{}, bool) {
	return condeval.ResolveField(data, fieldPath)
}

// evaluateCondition checks whether a single condition matches against the payload.
// Delegates to the shared condeval package.
func evaluateCondition(condition RuleCondition, data map[string]interface{}) bool {
	return condeval.EvaluateCondition(condition.Field, string(condition.Operator), condition.Value, data)
}

// evaluateRule checks whether all conditions in a rule match (AND logic).
func evaluateRule(rule ApprovalRule, data map[string]interface{}) bool {
	if len(rule.Conditions) == 0 {
		return false // a rule with no conditions never matches
	}
	for _, cond := range rule.Conditions {
		if !evaluateCondition(cond, data) {
			return false
		}
	}
	return true
}

// isEmpty checks if a value is considered empty.
// Thin wrapper over condeval.IsEmpty for backward compatibility.
func isEmpty(val interface{}) bool {
	return condeval.IsEmpty(val)
}

// compareEquals checks equality between field value and condition value.
func compareEquals(fieldVal, condVal interface{}) bool {
	return condeval.Equals(fieldVal, condVal)
}

// compareNumeric compares two values numerically.
func compareNumeric(fieldVal, condVal interface{}) int {
	return condeval.CompareNumeric(fieldVal, condVal)
}

// toFloat64 converts a value to float64 if possible.
func toFloat64(val interface{}) (float64, bool) {
	return condeval.ToFloat64(val)
}

// compareContains checks if the field value contains the condition value.
func compareContains(fieldVal, condVal interface{}) bool {
	return condeval.Contains(fieldVal, condVal)
}

// compareInList checks if the field value is in the condition value list.
func compareInList(fieldVal, condVal interface{}) bool {
	return condeval.InList(fieldVal, condVal)
}

// EvaluateResult holds the outcome of rule evaluation.
type EvaluateResult struct {
	Decision    RoutingDecision
	MatchedRule *ApprovalRule
}

// Evaluate processes the request payload against configured rules.
// Priority order: auto-reject → auto-approve → require-human.
// Rules within each category are evaluated in position order (ascending).
// Returns the routing decision and the matched rule (nil if default).
// Evaluation is bounded by a 5-second timeout via context.
func (e *ApprovalRuleEngine) Evaluate(ctx context.Context, rules *ApprovalRules, payload *ApprovalRequestPayload) (RoutingDecision, *ApprovalRule, error) {
	if rules == nil || payload == nil {
		return DecisionRequireHuman, nil, nil
	}

	// Create a 5-second timeout context if the parent context has no earlier deadline.
	evalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// evaluateRules is pure CPU-bound (in-memory map lookups). No goroutine needed.
	// The context timeout is checked between rules in evaluateCategory.
	decision, matched := e.evaluateRules(evalCtx, rules, payload)
	if evalCtx.Err() != nil {
		return DecisionRequireHuman, nil, evalCtx.Err()
	}
	return decision, matched, nil
}

// evaluateRules performs the actual rule evaluation logic.
// Priority: auto-reject → auto-approve → require-human.
// Within each category, rules are sorted by Position (ascending) and the first match wins.
func (e *ApprovalRuleEngine) evaluateRules(ctx context.Context, rules *ApprovalRules, payload *ApprovalRequestPayload) (RoutingDecision, *ApprovalRule) {
	data := payload.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Phase 1: Evaluate auto-reject rules (highest priority)
	if matched := e.evaluateCategory(ctx, rules.AutoReject, data); matched != nil {
		return DecisionAutoReject, matched
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return DecisionRequireHuman, nil
	}

	// Phase 2: Evaluate auto-approve rules
	if matched := e.evaluateCategory(ctx, rules.AutoApprove, data); matched != nil {
		return DecisionAutoApprove, matched
	}

	// Check context cancellation between phases
	if ctx.Err() != nil {
		return DecisionRequireHuman, nil
	}

	// Phase 3: Evaluate require-human rules
	if matched := e.evaluateCategory(ctx, rules.RequireHuman, data); matched != nil {
		return DecisionRequireHuman, matched
	}

	// Default: require human review (no rule matched)
	return DecisionRequireHuman, nil
}

// evaluateCategory sorts rules by position and returns the first matching rule.
// Returns nil if no rule matches or context is cancelled.
func (e *ApprovalRuleEngine) evaluateCategory(ctx context.Context, categoryRules []ApprovalRule, data map[string]interface{}) *ApprovalRule {
	if len(categoryRules) == 0 {
		return nil
	}

	// Sort rules by Position (ascending) — first match by position wins
	sorted := make([]ApprovalRule, len(categoryRules))
	copy(sorted, categoryRules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Position < sorted[j].Position
	})

	for i := range sorted {
		// Check context cancellation between rule evaluations
		if ctx.Err() != nil {
			return nil
		}
		if evaluateRule(sorted[i], data) {
			return &sorted[i]
		}
	}
	return nil
}
