//go:build windows

package database

import (
	"strings"
	"testing"
)

// The probe must be safe and deterministic on any Windows machine, with or
// without the ACE driver installed. Assertions stay environment-agnostic on
// purpose: CI hosts differ in what they have registered.
func TestDetectAccessODBCIsSafeAndConsistent(t *testing.T) {
	d := DetectAccessODBC()
	if d.ProcessBitness != "64" && d.ProcessBitness != "32" {
		t.Fatalf("unexpected process bitness: %q", d.ProcessBitness)
	}
	if d.DriverInstalled && len(d.DriverNames) == 0 {
		t.Fatal("driver reported installed without names")
	}
	for _, name := range d.DriverNames {
		if !strings.Contains(strings.ToLower(name), accessDriverNameMarker) {
			t.Fatalf("non-ACE driver name leaked into detection: %q", name)
		}
	}
	if !d.DriverInstalled && d.MatchingBitness {
		t.Fatal("matching bitness set without an installed driver")
	}
	// Hint contract: empty when everything is in place, actionable otherwise.
	hint := d.Hint()
	t.Logf("detection=%+v hint=%q", d, hint)
	if d.DriverInstalled && d.MatchingBitness && d.AdapterLinked && hint != "" {
		t.Fatalf("fully-capable detection produced a hint: %q", hint)
	}
	if !d.DriverInstalled && hint == "" && d.ProbeError == "" {
		t.Fatal("missing driver produced no actionable hint")
	}
}

func TestAccessProfileFailsClosedWithActionableError(t *testing.T) {
	// Only meaningful where the adapter is absent (CGO_ENABLED=0 builds);
	// on cgo builds the profile path proceeds to the real driver.
	if ODBCDriverLinked() {
		t.Skip("cgo adapter linked; driver_missing path not active")
	}
	_, err := openProfile(t.Context(), Profile{ID: "a", Type: SourceAccess, FilePath: "x.accdb"}, nil)
	if err == nil || !strings.Contains(err.Error(), "driver_missing") {
		t.Fatalf("expected driver_missing, got %v", err)
	}
}
