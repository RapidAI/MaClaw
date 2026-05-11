package main

import "testing"

func TestSessionOutputMarkerPredicates(t *testing.T) {
	if !sessionOutputMarkerAPIRetry.IsAPIRetry() {
		t.Fatal("API retry marker should match retry predicate")
	}
	if sessionOutputMarkerAPIError.IsAPIRetry() {
		t.Fatal("API error marker should not match retry-only predicate")
	}
	if !sessionOutputMarkerAPIRetry.IsTransientAPIIssue() || !sessionOutputMarkerAPIError.IsTransientAPIIssue() {
		t.Fatal("API retry and API error markers should both be transient API issues")
	}
	if sessionOutputMarkerNone.IsTransientAPIIssue() {
		t.Fatal("none marker should not be a transient API issue")
	}
}

func TestRecentSessionOutputHasMarkerUsesPredicates(t *testing.T) {
	lines := []string{"working", "API retry scheduled"}
	if !recentSessionOutputHasMarker(lines, 2, sessionOutputMarkerKind.IsAPIRetry) {
		t.Fatal("expected recent API retry marker")
	}
	if recentSessionOutputHasMarker(lines, 0, sessionOutputMarkerKind.IsAPIRetry) {
		t.Fatal("maxLines=0 should not match")
	}
}
