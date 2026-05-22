package main

import "testing"

func TestVEGroupExecutorPlatformMaterializesDesktopArtifacts(t *testing.T) {
	kind := normalizeIMMessagePlatformKind("ve_group_executor")
	if !kind.IsDesktop() {
		t.Fatalf("ve_group_executor should use desktop artifact handling")
	}
	if !kind.IsKnown() {
		t.Fatalf("ve_group_executor should be a known platform")
	}
}
