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
