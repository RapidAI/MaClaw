package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The Hub carried the same defect as the GUI, one line apart in a slice both
// hosts got at once: the walk's Outcome was dropped and only Text returned, so
// a failed walk arrived as a completed search that matched nothing.
func TestReviewedHostFileReadDoesNotPassOffAFailedWalkAsMatches(t *testing.T) {
	_, err := reviewedHostFileReadLocated(agent.SearchToolResult{
		Text:    "Glob cancelled",
		Outcome: agent.SearchToolOutcomeError,
	})
	if err == nil {
		t.Fatal("a walk that failed reported itself as a result set")
	}
	if !strings.Contains(err.Error(), "host_file_read_locate_failed") {
		t.Fatalf("err = %v, want the locate failure", err)
	}
	// The name must not collide with the vocabulary that means "nobody could
	// see the outcome"; a walk that failed definitely read nothing.
	if strings.Contains(err.Error(), "unobserved") || strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("err = %v would be classified as an unknown outcome", err)
	}
}

func TestReviewedHostFileReadKeepsAnEmptyResultASuccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome agent.SearchToolOutcome
		text    string
	}{
		{"no match", agent.SearchToolOutcomeNoMatch, "No files matched"},
		{"matched", agent.SearchToolOutcomeMatched, "main.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reviewedHostFileReadLocated(agent.SearchToolResult{Text: tc.text, Outcome: tc.outcome})
			if err != nil {
				t.Fatalf("%s reported a failure: %v", tc.name, err)
			}
			if got != tc.text {
				t.Fatalf("got %q, want the walk's own text %q", got, tc.text)
			}
		})
	}
}
