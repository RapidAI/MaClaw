package llmpool

import (
	"reflect"
	"testing"
)

func TestNormalizeAllowedNodeIDs(t *testing.T) {
	t.Parallel()
	got := NormalizeAllowedNodeIDs([]string{" hc-1 ", "", "hc-2", "HC-1", "hc-2"})
	want := []string{"hc-1", "hc-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAllowedNodeIDs() = %#v, want %#v", got, want)
	}
	if NormalizeAllowedNodeIDs(nil) != nil {
		t.Fatal("nil allowlist should stay nil")
	}
	if NormalizeAllowedNodeIDs([]string{"", "  "}) != nil {
		t.Fatal("blank allowlist should become nil (all nodes)")
	}
}

func TestProviderAllowedOnNode(t *testing.T) {
	t.Parallel()
	all := ProviderConfig{ID: "openai"}
	if !ProviderAllowedOnNode(all, "hc-cn") || !ProviderAllowedOnNode(all, "") {
		t.Fatal("empty allowlist must allow every node")
	}
	restricted := ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-us", "hc-sg"}}
	if !ProviderAllowedOnNode(restricted, "hc-us") || !ProviderAllowedOnNode(restricted, "HC-SG") {
		t.Fatal("listed nodes must be allowed")
	}
	if ProviderAllowedOnNode(restricted, "hc-cn") {
		t.Fatal("unlisted node must be denied")
	}
	if ProviderAllowedOnNode(restricted, "") {
		t.Fatal("restricted provider must not egress from an unknown node")
	}
}
