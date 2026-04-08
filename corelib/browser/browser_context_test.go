package browser

import "testing"

func TestSessionStoresRecentNetworkAndErrorLines(t *testing.T) {
	s := &Session{}
	s.recentNetwork = []string{"GET https://example.com/api"}
	s.recentErrors = []string{"console error"}
	if got := s.lastNetworkLines(); len(got) != 1 || got[0] != "GET https://example.com/api" {
		t.Fatalf("lastNetworkLines = %#v", got)
	}
	if got := s.lastErrorLines(); len(got) != 1 || got[0] != "console error" {
		t.Fatalf("lastErrorLines = %#v", got)
	}
}
