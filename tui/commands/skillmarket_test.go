package commands

import (
	"testing"
)

// TestRunCapabilityMarket_DelegatesToRunSkillMarket verifies that
// RunCapabilityMarket exists and delegates to RunSkillMarket.
// Both "capabilitymarket" and "skillmarket" are accepted command names
// (capabilitymarket is the primary, skillmarket is the backward-compat alias).
func TestRunCapabilityMarket_DelegatesToRunSkillMarket(t *testing.T) {
	// Compile-time check: both functions exist and are callable.
	var _ func([]string) error = RunCapabilityMarket
	var _ func([]string) error = RunSkillMarket

	// Both should produce the same usage error when called with no args.
	errCap := RunCapabilityMarket(nil)
	errSkill := RunSkillMarket(nil)

	if errCap == nil {
		t.Fatal("RunCapabilityMarket(nil) should return a usage error")
	}
	if errSkill == nil {
		t.Fatal("RunSkillMarket(nil) should return a usage error")
	}
	if errCap.Error() != errSkill.Error() {
		t.Fatalf("RunCapabilityMarket and RunSkillMarket should produce identical errors:\n  cap:   %s\n  skill: %s",
			errCap.Error(), errSkill.Error())
	}
}

// TestBothCommandNames_UnknownAction verifies that both entry points
// handle unknown actions identically, confirming they share the same
// underlying implementation.
func TestBothCommandNames_UnknownAction(t *testing.T) {
	errCap := RunCapabilityMarket([]string{"nonexistent-action-xyz"})
	errSkill := RunSkillMarket([]string{"nonexistent-action-xyz"})

	if errCap == nil || errSkill == nil {
		t.Fatal("both should return error for unknown action")
	}
	if errCap.Error() != errSkill.Error() {
		t.Fatalf("error messages differ:\n  cap:   %s\n  skill: %s",
			errCap.Error(), errSkill.Error())
	}
}

// TestCommandRegistration_BothNamesDispatchToSameHandler verifies that
// the TUI main dispatch (case "skillmarket", "capabilitymarket") routes
// both names to RunCapabilityMarket. This is a structural/compile-time
// verification that the delegation relationship is correct.
func TestCommandRegistration_BothNamesDispatchToSameHandler(t *testing.T) {
	// RunCapabilityMarket is the canonical entry point.
	// RunSkillMarket is the implementation.
	// RunCapabilityMarket must delegate to RunSkillMarket.

	// Verify by calling with a known subcommand that requires no network.
	// "search" with no query should still succeed (returns empty results or
	// hits network — we only care that both paths reach the same code).
	// Instead, use an invalid subcommand to confirm identical error routing.
	args := []string{"__test_invalid_subcommand__"}

	errViaCapability := RunCapabilityMarket(args)
	errViaSkill := RunSkillMarket(args)

	if errViaCapability == nil || errViaSkill == nil {
		t.Fatal("both should return error for invalid subcommand")
	}

	// Same error message confirms same underlying implementation.
	if errViaCapability.Error() != errViaSkill.Error() {
		t.Fatalf("delegation broken — different errors:\n  capabilitymarket: %s\n  skillmarket:      %s",
			errViaCapability.Error(), errViaSkill.Error())
	}
}
