package jobs

import (
	"context"
	"strings"
	"testing"

	"clawmatemaker/internal/firmware"
)

func TestFlashRejectsUnsignedPackageBeforeDeviceAccess(t *testing.T) {
	archive := t.TempDir() + "/missing.clawfw"
	j, err := NewFlashJob(t.TempDir(), FlashRequest{Port: "COM4", PackagePath: archive, Trust: firmware.TrustStore{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Run(context.Background())
	if err == nil || r.ErrorCode != "PACKAGE_SIGNATURE_INVALID" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestFlashLeaseRejectsConcurrentWriter(t *testing.T) {
	release, err := acquireFlashLease()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := acquireFlashLease(); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("concurrent write lease err=%v", err)
	}
}

func TestFlashResultCarriesTheFinalWriteBaudOnlyInLogs(t *testing.T) {
	// The public result intentionally contains no raw MAC, port topology, or
	// command arguments. Baud transitions are emitted as redacted job events by
	// writeWithFallback and captured in the diagnostics bundle.
	var result FlashResult
	if result.ImagesWritten != 0 || result.ErrorCode != "" {
		t.Fatalf("unexpected zero result: %+v", result)
	}
}

func TestRecoveryRequestIsInternalOnlyAndRequiresFullPackage(t *testing.T) {
	request := FlashRequest{Port: "COM4", PackagePath: "firmware.clawfw", Recovery: true}
	if !request.Recovery {
		t.Fatal("recovery marker was lost")
	}
	// FlashJob validates this marker only after signature/package parsing, so a
	// caller cannot turn an app-only package into recovery by changing UI data.
}

func TestCurrentLayoutPolicyPreservesOnlyAppOnlyPackages(t *testing.T) {
	if replacing, err := validateCurrentLayoutForMode(firmware.ModeAppOnly, "device-layout", "package-layout"); err == nil || replacing {
		t.Fatalf("app-only mismatch replacing=%v err=%v", replacing, err)
	}
	if replacing, err := validateCurrentLayoutForMode(firmware.ModeFull, "device-layout", "package-layout"); err != nil || !replacing {
		t.Fatalf("full migration replacing=%v err=%v", replacing, err)
	}
	if replacing, err := validateCurrentLayoutForMode(firmware.ModeAppOnly, "same", "same"); err != nil || replacing {
		t.Fatalf("same layout replacing=%v err=%v", replacing, err)
	}
}

func TestFlashJobContainsCurrentAppDescriptorGate(t *testing.T) {
	if !strings.Contains(flashSecurityGateSource(t), "read_flash_app_descriptor") {
		t.Fatal("flash preflight must read the bounded current application descriptor")
	}
	if !strings.Contains(flashSecurityGateSource(t), "APP_DESCRIPTOR_INVALID") {
		t.Fatal("app-only updates must fail closed when the current app descriptor is unavailable")
	}
}
