package main

import "testing"

func TestShouldApplyWorkflowFilter_ReviewOverridesSkip(t *testing.T) {
	cases := []struct {
		name                 string
		skipNeedsConfirmGate bool
		awaitingReview       bool
		workflowAgentLoop    bool
		phaseBlocked         bool
		activeWorkflow       bool
		want                 bool
	}{
		{name: "normal workflow filtering", skipNeedsConfirmGate: false, awaitingReview: false, workflowAgentLoop: false, want: true},
		{name: "non-review skip", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, want: false},
		{name: "active workflow overrides skip", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, activeWorkflow: true, want: true},
		{name: "review overrides skip", skipNeedsConfirmGate: true, awaitingReview: true, workflowAgentLoop: false, want: true},
		{name: "workflow agent loop overrides skip", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: true, want: true},
		{name: "blocked phase overrides skip", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, phaseBlocked: true, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldApplyWorkflowFilter(tc.skipNeedsConfirmGate, tc.awaitingReview, tc.workflowAgentLoop, tc.phaseBlocked, tc.activeWorkflow)
			if got != tc.want {
				t.Fatalf("shouldApplyWorkflowFilter(%v, %v, %v, %v, %v)=%v, want %v",
					tc.skipNeedsConfirmGate, tc.awaitingReview, tc.workflowAgentLoop, tc.phaseBlocked, tc.activeWorkflow, got, tc.want)
			}
		})
	}
}

func TestShouldSkipWorkflowToolExecutionGate_WorkflowAgentLoopOverridesSkip(t *testing.T) {
	cases := []struct {
		name                 string
		skipNeedsConfirmGate bool
		awaitingReview       bool
		workflowAgentLoop    bool
		phaseBlocked         bool
		activeWorkflow       bool
		want                 bool
	}{
		{name: "no skip", skipNeedsConfirmGate: false, awaitingReview: false, workflowAgentLoop: false, want: false},
		{name: "plain confirmed continuation skips without workflow", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, want: true},
		{name: "active workflow keeps guard", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, activeWorkflow: true, want: false},
		{name: "workflow agent loop keeps guard", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: true, want: false},
		{name: "review keeps guard", skipNeedsConfirmGate: true, awaitingReview: true, workflowAgentLoop: false, want: false},
		{name: "blocked phase keeps guard", skipNeedsConfirmGate: true, awaitingReview: false, workflowAgentLoop: false, phaseBlocked: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSkipWorkflowToolExecutionGate(tc.skipNeedsConfirmGate, tc.awaitingReview, tc.workflowAgentLoop, tc.phaseBlocked, tc.activeWorkflow)
			if got != tc.want {
				t.Fatalf("shouldSkipWorkflowToolExecutionGate(%v, %v, %v, %v, %v)=%v, want %v",
					tc.skipNeedsConfirmGate, tc.awaitingReview, tc.workflowAgentLoop, tc.phaseBlocked, tc.activeWorkflow, got, tc.want)
			}
		})
	}
}
