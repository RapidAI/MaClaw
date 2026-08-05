package jobs

import (
	"context"
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
