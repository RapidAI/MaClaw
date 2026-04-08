package browser

import "testing"

func TestDomainMatchesSupportsSubdomains(t *testing.T) {
	if !domainMatches("sub.example.com", "example.com") {
		t.Fatal("expected subdomain match")
	}
	if domainMatches("example.org", "example.com") {
		t.Fatal("unexpected cross-domain match")
	}
}

func TestAppendCappedTraceKeepsNewest(t *testing.T) {
	items := []BrowserTraceEvent{{Kind: "a"}, {Kind: "b"}}
	items = appendCappedTrace(items, BrowserTraceEvent{Kind: "c"}, 2)
	if len(items) != 2 {
		t.Fatalf("len = %d", len(items))
	}
	if items[0].Kind != "b" || items[1].Kind != "c" {
		t.Fatalf("items = %#v", items)
	}
}
