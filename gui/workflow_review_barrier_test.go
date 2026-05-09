package main

import "testing"

func TestShouldApplyWorkflowFilter_ReviewOverridesSkip(t *testing.T) {
	cases := []struct {
		name                 string
		skipNeedsConfirmGate bool
		awaitingReview       bool
		want                 bool
	}{
		{name: "normal workflow filtering", skipNeedsConfirmGate: false, awaitingReview: false, want: true},
		{name: "non-review skip", skipNeedsConfirmGate: true, awaitingReview: false, want: false},
		{name: "review overrides skip", skipNeedsConfirmGate: true, awaitingReview: true, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldApplyWorkflowFilter(tc.skipNeedsConfirmGate, tc.awaitingReview)
			if got != tc.want {
				t.Fatalf("shouldApplyWorkflowFilter(%v, %v)=%v, want %v",
					tc.skipNeedsConfirmGate, tc.awaitingReview, got, tc.want)
			}
		})
	}
}

func TestShouldBypassNeedsConfirmGate_ReviewOverridesSkip(t *testing.T) {
	cases := []struct {
		name                 string
		skipNeedsConfirmGate bool
		codingGateActive     bool
		awaitingReview       bool
		want                 bool
	}{
		{name: "no skip", skipNeedsConfirmGate: false, codingGateActive: false, awaitingReview: false, want: false},
		{name: "non-review continuation may bypass", skipNeedsConfirmGate: true, codingGateActive: false, awaitingReview: false, want: true},
		{name: "coding gate active blocks bypass", skipNeedsConfirmGate: true, codingGateActive: true, awaitingReview: false, want: false},
		{name: "review barrier blocks bypass", skipNeedsConfirmGate: true, codingGateActive: false, awaitingReview: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldBypassNeedsConfirmGate(tc.skipNeedsConfirmGate, tc.codingGateActive, tc.awaitingReview)
			if got != tc.want {
				t.Fatalf("shouldBypassNeedsConfirmGate(%v, %v, %v)=%v, want %v",
					tc.skipNeedsConfirmGate, tc.codingGateActive, tc.awaitingReview, got, tc.want)
			}
		})
	}
}
