package skill

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchAllFilteredReport_MarksDegradedOnSourceFailure(t *testing.T) {
	// SkillHub returns 500; ClawHub/GitHub may also fail in CI — we only assert
	// skillhub is queried and marked failed when URL is our test server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := DefaultHubClient()
	// Only skillhub — avoid network to clawhub/github in unit test.
	report := client.SearchAllFilteredReport(context.Background(), srv.URL, "pdf", []string{"skillhub"})
	if !report.Degraded {
		t.Fatalf("expected degraded, sources=%+v", report.Sources)
	}
	var found bool
	for _, s := range report.Sources {
		if s.Source == "skillhub" && s.Queried && !s.OK {
			found = true
			if s.Error == "" {
				t.Fatal("expected error string")
			}
		}
	}
	if !found {
		t.Fatalf("skillhub status missing: %+v", report.Sources)
	}
	note := report.FormatDegradedNote()
	if !strings.Contains(note, "skillhub") {
		t.Fatalf("note=%q", note)
	}
	if len(report.Results) != 0 {
		t.Fatalf("expected no results from 500, got %d", len(report.Results))
	}
}

func TestSearchAllFilteredReport_EmptyQueryNotDegraded(t *testing.T) {
	client := DefaultHubClient()
	report := client.SearchAllFilteredReport(context.Background(), "http://example.invalid", "", []string{"skillhub"})
	if report.Degraded {
		t.Fatalf("empty query should not be network-degraded: %+v", report.Sources)
	}
}
