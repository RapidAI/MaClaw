package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The name walk reports trouble by putting the reason in Text and marking
// Outcome. Reading only Text returned that reason as though it were the list
// of matching files, so a cancelled or misdirected walk arrived looking like
// a completed search that found nothing -- and the model concludes the file
// does not exist.
func TestTrustedFileReadDoesNotPassOffAFailedWalkAsMatches(t *testing.T) {
	_, err := trustedFileReadLocated(agent.SearchToolResult{
		Text:    "Glob cancelled",
		Outcome: agent.SearchToolOutcomeError,
	})
	if err == nil {
		t.Fatal("a walk that failed reported itself as a result set")
	}
	if !strings.Contains(err.Error(), "trusted_file_read_locate_failed") {
		t.Fatalf("err = %v, want the locate failure", err)
	}
}

// The opposite over-correction is just as wrong: finding nothing is a
// complete, truthful answer, and turning it into a failure would make every
// genuinely absent file look like a broken search.
func TestTrustedFileReadKeepsAnEmptyResultASuccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome agent.SearchToolOutcome
		text    string
	}{
		{"no match", agent.SearchToolOutcomeNoMatch, "No files matched"},
		{"matched", agent.SearchToolOutcomeMatched, "main.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := trustedFileReadLocated(agent.SearchToolResult{Text: tc.text, Outcome: tc.outcome})
			if err != nil {
				t.Fatalf("%s reported a failure: %v", tc.name, err)
			}
			if got != tc.text {
				t.Fatalf("got %q, want the walk's own text %q", got, tc.text)
			}
		})
	}
}
