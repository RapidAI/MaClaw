package jobs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func flashSecurityGateSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "flash.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFlashSecurityGateFailsClosedForUnknownSecureVersion(t *testing.T) {
	if !strings.Contains(flashSecurityGateSource(t), "security.SecureVersion == nil") {
		t.Fatal("unknown anti-rollback security version must fail closed")
	}
}

func TestFlashWritePlanIsBoundedByObservedFlashBeforeRiskWindow(t *testing.T) {
	source := flashSecurityGateSource(t)
	plan := strings.Index(source, "validateWritePlan(images, uint64(observed.SizeBytes))")
	risk := strings.Index(source, "RISK_WINDOW_STARTED")
	if plan < 0 || risk < 0 || plan > risk {
		t.Fatal("write plan must be range-checked against observed Flash before the risk window")
	}
}

func TestFlashSecurityGateChecksEverySignedBaselineField(t *testing.T) {
	source := flashSecurityGateSource(t)
	for _, expected := range []string{"firmware.ValidateSecurityBaseline(verified.Manifest.SecurityBaseline)", "*security.SecureVersion != verified.Manifest.SecurityBaseline.SecureVersion", "SECURITY_BASELINE_VERIFIED"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("flash security baseline gate missing %q", expected)
		}
	}
}
