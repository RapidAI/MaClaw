package main

import "testing"

func TestTUIBtwCallbacksBuildToolsReturnsRequestLocalDefinitions(t *testing.T) {
	cb := &tuiBtwCallbacks{}
	first := cb.BuildTools("first request")
	if len(first) == 0 {
		t.Fatal("first request returned no /btw tools")
	}
	first[0]["request_local_test_mutation"] = true
	second := cb.BuildTools("successor request")
	if len(second) == 0 {
		t.Fatal("successor request returned no /btw tools")
	}
	if _, leaked := second[0]["request_local_test_mutation"]; leaked {
		t.Fatalf("successor reused predecessor definition: %#v", second[0])
	}
}
