package main

import "testing"

func TestRemoteProbeStatusShouldClearActivation(t *testing.T) {
	if normalizeRemoteProbeStatusKind("blocked").ShouldClearActivation() != true {
		t.Fatal("blocked should clear local activation credentials")
	}
	if normalizeRemoteProbeStatusKind("not_found").ShouldClearActivation() {
		t.Fatal("not_found should not clear local activation credentials")
	}
	if normalizeRemoteProbeStatusKind("unknown").ShouldClearActivation() {
		t.Fatal("unknown should not clear local activation credentials")
	}
}
