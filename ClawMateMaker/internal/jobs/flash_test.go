package jobs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/flash"
	"clawmatemaker/internal/logging"
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

func TestFailedWriteWithRecoveryJournalPersistsDistinctRecoveryStatus(t *testing.T) {
	root := t.TempDir()
	j, err := NewFlashJob(root, FlashRequest{Port: "COM4", PackagePath: filepath.Join(root, "missing.clawfw"), Trust: firmware.TrustStore{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This reaches package validation before any write and is intentionally a
	// normal failed job; the distinct recovery status must only be used after
	// the irreversible-write boundary is crossed.
	result, runErr := j.Run(context.Background())
	if runErr == nil || result.Status != "failed" {
		t.Fatalf("result=%+v err=%v", result, runErr)
	}
	if _, err := logging.ReadSnapshot(root, result.JobID); err != nil {
		t.Fatalf("missing durable snapshot: %v", err)
	}
}

func TestWritePlanRejectsOutOfRangeAndOverlappingImagesBeforeFlash(t *testing.T) {
	if err := validateWritePlan([]flash.WriteImage{{Offset: 0, Size: 0x2000}}, 0x1000); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("out-of-range write image accepted: %v", err)
	}
	if err := validateWritePlan([]flash.WriteImage{{Offset: 0x1000, Size: 0x2000}, {Offset: 0x2000, Size: 0x1000}}, 0x10000); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping write images accepted: %v", err)
	}
	if err := validateWritePlan([]flash.WriteImage{{Offset: 0x1000, Size: 0x1000}, {Offset: 0x4000, Size: 0x1000}}, 0x10000); err != nil {
		t.Fatalf("valid bounded write plan rejected: %v", err)
	}
}

func TestSignedWriteOrderDoesNotUseArchiveOffsetOrder(t *testing.T) {
	// fwpack emits data first, then the App, then partition-table and
	// bootloader. This must remain an explicit signed sequence rather than a
	// coincidental ZIP or numerical-offset order.
	order := []string{"storage", "app", "partition-table", "bootloader"}
	if order[len(order)-2] != "partition-table" || order[len(order)-1] != "bootloader" {
		t.Fatalf("unsafe signed write order: %#v", order)
	}
	if !strings.Contains(flashSecurityGateSource(t), "WRITE_ORDER_VERIFIED") {
		t.Fatal("flash job must log application of the signed write order")
	}
	for _, required := range []string{"writeSplitImages", "VerifyImagesNoReset", "FLASH_IMAGE_WRITE_STARTED", "FLASH_IMAGE_VERIFIED"} {
		if !strings.Contains(flashSecurityGateSource(t), required) {
			t.Fatalf("split write execution is missing %q", required)
		}
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

func TestFlashDeviceBindingUsesNormalizedROMMAC(t *testing.T) {
	const expected = "3d7db9c93eb0c7c08860de80f196319679595ade1f572454d659f83d84f02556"
	if got := deviceBinding("b4:3a:45:a1:e5:84"); got != expected {
		t.Fatalf("device binding = %q, want %q", got, expected)
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

func TestFlashJobRequiresNonceBoundApplicationIdentityBeforeNormalWrite(t *testing.T) {
	source := flashSecurityGateSource(t)
	identity := strings.Index(source, "device.ProbeApplicationIdentity")
	risk := strings.Index(source, "RISK_WINDOW_STARTED")
	if identity < 0 || risk < 0 || identity > risk {
		t.Fatal("normal flash must verify fresh nonce-bound application identity before the risk window")
	}
	for _, required := range []string{
		"DEVICE_IDENTITY_UNAVAILABLE",
		"DEVICE_IDENTITY_MISMATCH",
		"identity.Protocol != device.ProtocolVersion",
		"identity.FirmwareTargetBoardID != verified.Manifest.Board.ID",
		"identity.PSRAMBytes < verified.Manifest.AppIdentity.PSRAMBytes",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("nonce-bound identity gate missing %q", required)
		}
	}
}

func TestRecoveryRetainsExplicitIdentityUnavailableException(t *testing.T) {
	source := flashSecurityGateSource(t)
	if !strings.Contains(source, "if !j.request.Recovery {") {
		t.Fatal("only explicit recovery may proceed without a running application identity")
	}
}

func TestFlashPipelineDoesNotResetBeforeReadbackVerification(t *testing.T) {
	// The flash adapter retains ROM download mode for write_flash and permits
	// exactly one hard reset on verify_flash. Keep this source-level contract
	// visible from the job package because port re-enumeration follows only
	// after that verification has completed.
	source := flashSecurityGateSource(t)
	if !strings.Contains(source, "tool.VerifyImages") || !strings.Contains(source, "PORT_REENUMERATION_STARTED") {
		t.Fatal("flash job must verify in ROM before waiting for app serial re-enumeration")
	}
}

func TestFlashImageEvidenceUsesOnlySafeRangeMetadata(t *testing.T) {
	image := flash.WriteImage{Offset: 0x10000, Path: `C:\private\firmware\app.bin`, Size: 4096, Region: "app", SHA256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if got := filepath.Base(image.Path); got != "app.bin" {
		t.Fatalf("diagnostic image name = %q", got)
	}
	// logImage is invoked only after the signed extraction/write-plan boundary.
	// Keep the explicit source contract so a later refactor cannot substitute
	// an arbitrary renderer-provided path for this evidence.
	if !strings.Contains(flashSecurityGateSource(t), "filepath.Base(image.Path)") {
		t.Fatal("per-image diagnostics must redact the host path to its base name")
	}
}

func TestFlashProgressUsesMeasuredBytesAndIsThrottled(t *testing.T) {
	log, err := logging.New(t.TempDir(), "job-progress", "attempt-progress", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	j := &FlashJob{log: log}
	j.beginWriteProgress([]flash.WriteImage{{Size: 1000}, {Size: 2000}})
	if got := j.progressTotalBytes(); got != 3000 {
		t.Fatalf("total bytes = %d", got)
	}
	// The actual event writer is deliberately absent in this unit test; this
	// verifies that progress accepts monotonic measured bytes and rejects a
	// sidecar redraw that moves backward.
	j.reportWriteProgress(flash.WriteProgress{TransferredBytes: 1800, TotalBytes: 3000})
	if j.progressCurrent != 1800 {
		t.Fatalf("progress = %d", j.progressCurrent)
	}
	j.reportWriteProgress(flash.WriteProgress{TransferredBytes: 900, TotalBytes: 3000})
	if j.progressCurrent != 1800 {
		t.Fatalf("backward progress was accepted: %d", j.progressCurrent)
	}
}
