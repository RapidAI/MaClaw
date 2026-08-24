package main

import (
	"strings"
	"testing"
)

// Across the Wails boundary an omitted field arrives as its zero value. For a
// boolean verdict that would mean a caller who said nothing is recorded as
// having said "it failed" -- turning silence into a judgement about exactly the
// thing nobody knew. The outcome is therefore a word, and an unspoken one is
// refused rather than interpreted.
func TestUnknownEffectResolutionRefusesAnUnspokenOutcome(t *testing.T) {
	for _, outcome := range []string{"", "   ", "unknown", "true", "yes", "ok", "maybe"} {
		if _, err := semanticEffectResolutionOutcome(outcome); err == nil {
			t.Fatalf("outcome %q was accepted as a verdict", outcome)
		}
	}
	for _, tc := range []struct {
		outcome string
		want    bool
	}{
		{"succeeded", true},
		{"failed", false},
		{"  Succeeded  ", true},
		{"FAILED", false},
	} {
		got, err := semanticEffectResolutionOutcome(tc.outcome)
		if err != nil || got != tc.want {
			t.Fatalf("outcome %q -> %v err=%v", tc.outcome, got, err)
		}
	}
}

// Each guard has to turn the request away before the ledger is reached, and an
// app with no durable routing behind it must not be the thing that stops it --
// otherwise the guards would only appear to work on a configured host.
func TestUnknownEffectResolutionChecksTheRequestBeforeTheLedger(t *testing.T) {
	app := &App{}
	valid := SemanticEffectResolutionRequest{
		OperationID: "op-1", Confirm: true, Outcome: "succeeded", Evidence: "checked the channel console",
	}
	for _, tc := range []struct {
		name    string
		mutate  func(SemanticEffectResolutionRequest) SemanticEffectResolutionRequest
		wantMsg string
	}{
		{"no confirmation", func(r SemanticEffectResolutionRequest) SemanticEffectResolutionRequest {
			r.Confirm = false
			return r
		}, "confirm"},
		{"unspoken outcome", func(r SemanticEffectResolutionRequest) SemanticEffectResolutionRequest {
			r.Outcome = ""
			return r
		}, "outcome"},
		{"no evidence", func(r SemanticEffectResolutionRequest) SemanticEffectResolutionRequest {
			r.Evidence = ""
			return r
		}, "evidence"},
		{"blank evidence", func(r SemanticEffectResolutionRequest) SemanticEffectResolutionRequest {
			r.Evidence = "   "
			return r
		}, "evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := app.ResolveUnknownSemanticEffect(tc.mutate(valid))
			if err == nil {
				t.Fatal("an underspecified verdict was accepted")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err=%v, expected it to name the missing %s", err, tc.wantMsg)
			}
		})
	}
}

// The exit is worth having only because the name on it means something, so the
// operator must be a fact someone could go and check rather than a placeholder.
func TestDesktopResolutionOperatorNamesSomethingCheckable(t *testing.T) {
	operator := desktopResolutionOperator()
	if strings.TrimSpace(operator) == "" {
		t.Fatal("no operator identity at all; a verdict would be anonymous")
	}
	for _, placeholder := range []string{"desktop-user", "local-desktop-operator", "unknown", "anonymous"} {
		if strings.EqualFold(operator, placeholder) {
			t.Fatalf("operator %q is a placeholder, not an identity", operator)
		}
	}
}
