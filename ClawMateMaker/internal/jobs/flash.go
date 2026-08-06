package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/flash"
	"clawmatemaker/internal/logging"
	"clawmatemaker/internal/partition"
	"clawmatemaker/internal/verify"
)

type FlashRequest struct {
	Port               string              `json:"port"`
	PackagePath        string              `json:"packagePath"`
	Trust              firmware.TrustStore `json:"-"`
	ExpectedChip       string              `json:"expectedChip"`
	ExpectedFlashBytes int64               `json:"expectedFlashBytes"`
	BoardID            string              `json:"boardId"`
	// Recovery is set only by the explicit journal-recovery flow. A damaged
	// bootloader or partition table must not make a complete ROM recovery
	// impossible merely because the old layout can no longer be parsed.
	Recovery bool `json:"-"`
}
type FlashResult struct {
	JobID         string    `json:"jobId"`
	AttemptID     string    `json:"attemptId"`
	Status        string    `json:"status"`
	ErrorCode     string    `json:"errorCode,omitempty"`
	ErrorMessage  string    `json:"errorMessage,omitempty"`
	PackageID     string    `json:"packageId,omitempty"`
	ImagesWritten int       `json:"imagesWritten"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
}
type FlashJob struct {
	request          FlashRequest
	jobID, attemptID string
	log              *logging.Writer
	logRoot          string
}

// flashLease serializes irreversible writes inside one desktop process. A
// port can be reused by the operating system after an unplug, so allowing a
// second write job to race the first would invalidate the pre-write binding
// check even when the jobs name different COM ports. Cross-process protection
// is supplied by the OS serial-port open itself; this lease covers the Wails
// API boundary before a child process owns the port.
var flashLease sync.Mutex

func acquireFlashLease() (func(), error) {
	if !flashLease.TryLock() {
		return nil, errors.New("another firmware write job is already active")
	}
	return flashLease.Unlock, nil
}

func NewFlashJob(root string, request FlashRequest, emit func(logging.Event)) (*FlashJob, error) {
	if request.Port == "" || request.PackagePath == "" {
		return nil, errors.New("port and package path are required")
	}
	jobID, err := id("job")
	if err != nil {
		return nil, err
	}
	attemptID, err := id("attempt")
	if err != nil {
		return nil, err
	}
	w, err := logging.New(root, jobID, attemptID, emit)
	if err != nil {
		return nil, err
	}
	return &FlashJob{request: request, jobID: jobID, attemptID: attemptID, log: w, logRoot: root}, nil
}

// Run implements the first controlled write flow. All mutable steps occur only
// after signed package verification, ROM identity recheck, capacity check, and
// a fail-closed security-state query.
func (j *FlashJob) Run(ctx context.Context) (result FlashResult, retErr error) {
	result = FlashResult{JobID: j.jobID, AttemptID: j.attemptID, Status: "running", StartedAt: time.Now().UTC()}
	j.log.Event(logging.Info, "prepare", "job", "JOB_CREATED", "job.created", "", map[string]any{"port": j.request.Port})
	releaseLease, err := acquireFlashLease()
	if err != nil {
		return j.fail(&result, "WRITE_JOB_ACTIVE", err)
	}
	defer releaseLease()
	writingStarted := false
	var writingBootCritical bool
	var packageSHA256 string
	defer func() {
		if retErr != nil && writingStarted {
			// A completed esptool process is not enough to prove that every image
			// was written and booted. Persist the recovery state before closing the
			// job so a later app start cannot mistake an interrupted single-slot
			// factory update for an ordinary retry.
			if err := WriteJournal(j.logRoot, Journal{JobID: j.jobID, PackageID: result.PackageID, PackageSHA256: packageSHA256, State: JournalRecoveryRequired, BootCriticalModified: writingBootCritical}); err != nil {
				j.log.Event(logging.Error, "flash", "journal", "JOURNAL_RECOVERY_WRITE_FAILED", "journal.recovery.write.failed", err.Error(), nil)
			} else {
				j.log.Event(logging.Warn, "flash", "journal", "RECOVERY_REQUIRED", "flash.recovery_required", "The write did not complete a verified boot. Keep the device connected and use a complete recovery package through ROM download mode.", nil)
			}
		}
		result.FinishedAt = time.Now().UTC()
		if retErr != nil {
			result.Status = "failed"
			j.log.Event(logging.Error, "flash", "job", result.ErrorCode, "job.failed", result.ErrorMessage, nil)
		} else {
			result.Status = "succeeded"
			j.log.Event(logging.Info, "complete", "job", "JOB_SUCCEEDED", "job.succeeded", "", map[string]any{"durationMs": time.Since(result.StartedAt).Milliseconds()})
		}
		_ = j.log.WriteSummary(result)
		_ = j.log.Close()
	}()
	j.log.Event(logging.Info, "prepare", "firmware", "STAGE_STARTED", "stage.started", "Verifying signed firmware package.", nil)
	verified, err := firmware.VerifyRelease(j.request.PackagePath, j.request.Trust)
	if err != nil {
		return j.fail(&result, "PACKAGE_SIGNATURE_INVALID", err)
	}
	result.PackageID = verified.Manifest.PackageID
	packageSHA256 = verified.ArchiveSHA256
	if err := WriteJournal(j.logRoot, Journal{JobID: j.jobID, PackageID: verified.Manifest.PackageID, PackageSHA256: verified.ArchiveSHA256, State: JournalPrepared}); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	if verified.Manifest.Board.ID == "" || verified.Manifest.Chip.Family == "" || verified.Manifest.Chip.FlashBytes <= 0 {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare board and chip capacity"))
	}
	if verified.Manifest.Layout.ID == "" || verified.Manifest.Layout.Fingerprint == "" || verified.Manifest.Layout.PartitionTablePath == "" || (verified.Manifest.Mode != "full" && verified.Manifest.Mode != "app-only") {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare a known layout and mode"))
	}
	if j.request.Recovery && verified.Manifest.Mode != firmware.ModeFull {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", errors.New("ROM recovery requires a complete firmware package"))
	}
	if verified.Manifest.AppIdentity.ProjectName == "" || verified.Manifest.AppIdentity.AppVersion == "" || verified.Manifest.AppIdentity.ELFSHA256 == "" || verified.Manifest.BootVerification.Baud <= 0 || verified.Manifest.BootVerification.TimeoutSeconds <= 0 {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare app identity and boot verification policy"))
	}
	if j.request.BoardID == "" {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", errors.New("a selected firmware board target is required"))
	}
	profile, err := catalog.ProfileForFirmwareBoardID(j.request.BoardID)
	if err != nil {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", err)
	}
	if err := catalog.ValidateManifestBinding(profile, verified.Manifest.Board.ID, verified.Manifest.Board.ProfileHash, verified.Manifest.Chip.Family, verified.Manifest.Chip.FlashBytes); err != nil {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("firmware catalog binding: %w", err))
	}
	j.log.Event(logging.Info, "prepare", "firmware", "PACKAGE_VERIFIED", "package.verified", "", map[string]any{"bytes": len(verified.Manifest.Files)})
	tool, err := flash.FindTool()
	if err != nil {
		return j.fail(&result, "TOOL_UNAVAILABLE", err)
	}
	// Preserve stable OS enumeration evidence before esptool's hard reset. A
	// native USB Serial/JTAG device can come back under a different COM/tty
	// path after the flash, so its USB serial number (when supplied by the OS)
	// is the only signal that may associate a changed endpoint automatically.
	postFlashCandidate := device.Candidate{Port: j.request.Port}
	if candidates, listErr := device.ListCandidates(); listErr != nil {
		j.log.Event(logging.Warn, "prepare", "device", "PORT_BASELINE_UNAVAILABLE", "port.baseline.unavailable", "Could not read USB enumeration metadata; boot verification will accept only the original serial port.", map[string]any{"port": j.request.Port})
	} else {
		for _, candidate := range candidates {
			if candidate.Port == j.request.Port {
				postFlashCandidate = candidate
				break
			}
		}
		j.log.Event(logging.Info, "prepare", "device", "PORT_BASELINE_CAPTURED", "port.baseline.captured", "Captured serial endpoint metadata for post-reset association.", map[string]any{"port": j.request.Port})
	}
	chipRun, err := tool.RunReadOnly(ctx, j.request.Port, "chip_id")
	j.logSidecar("chip_id", chipRun, err)
	if err != nil {
		return j.fail(&result, "BOOTLOADER_SYNC_FAILED", err)
	}
	chip := flash.ParseChipID(chipRun.Output)
	if j.request.ExpectedChip != "" && !strings.Contains(strings.ToLower(chip.Chip+" "+chip.Revision), strings.ToLower(j.request.ExpectedChip)) {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("expected %s, observed %s", j.request.ExpectedChip, chip.Chip))
	}
	if chip.MAC == "" {
		return j.fail(&result, "DEVICE_BINDING_UNAVAILABLE", errors.New("ROM probe did not return a device MAC address"))
	}
	flashRun, err := tool.RunReadOnly(ctx, j.request.Port, "flash_id")
	j.logSidecar("flash_id", flashRun, err)
	if err != nil {
		return j.fail(&result, "FLASH_PROBE_FAILED", err)
	}
	observed := flash.ParseFlashID(flashRun.Output)
	required := verified.Manifest.Chip.FlashBytes
	if j.request.ExpectedFlashBytes > required {
		required = j.request.ExpectedFlashBytes
	}
	if observed.SizeBytes <= 0 || observed.SizeBytes < required {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("flash capacity %d is below required %d", observed.SizeBytes, required))
	}
	var currentLayout partition.Table
	var currentApp flash.ESPAppDescription
	if j.request.Recovery {
		j.log.Event(logging.Warn, "prepare", "partition", "RECOVERY_LAYOUT_BYPASS", "recovery.layout.bypass", "Explicit full ROM recovery does not depend on the existing partition table; the signed package layout will be verified before writing.", nil)
	} else {
		partitionPath := filepath.Join(os.TempDir(), j.jobID+"-partition-table.bin")
		defer os.Remove(partitionPath)
		partitionRun, err := tool.ReadFlash(ctx, j.request.Port, 0x8000, 4096, partitionPath)
		j.logSidecar("read_flash_partition_table", partitionRun, err)
		if err != nil {
			return j.fail(&result, "PARTITION_READ_FAILED", err)
		}
		partitionBytes, err := os.ReadFile(partitionPath)
		if err != nil {
			return j.fail(&result, "PARTITION_READ_FAILED", err)
		}
		currentLayout, err = partition.Parse(partitionBytes, uint64(observed.SizeBytes))
		if err != nil {
			return j.fail(&result, "PARTITION_LAYOUT_INVALID", err)
		}
		layoutReplacing, layoutErr := validateCurrentLayoutForMode(verified.Manifest.Mode, currentLayout.Fingerprint, verified.Manifest.Layout.Fingerprint)
		if layoutErr != nil {
			return j.fail(&result, "FIRMWARE_INCOMPATIBLE", layoutErr)
		}
		if layoutReplacing {
			j.log.Event(logging.Warn, "prepare", "partition", "FULL_LAYOUT_REPLACEMENT", "full.layout.replacement", "The signed complete image replaces a different existing partition layout; user data may be overwritten.", map[string]any{"bytes": len(currentLayout.Entries)})
		} else {
			j.log.Event(logging.Info, "prepare", "partition", "LAYOUT_VERIFIED", "layout.verified", "", map[string]any{"bytes": len(currentLayout.Entries)})
		}
		factory, found := partition.Find(currentLayout.Entries, "factory")
		if !found || factory.Size < 4096 {
			return j.fail(&result, "PARTITION_LAYOUT_INVALID", errors.New("current layout has no readable factory application partition"))
		}
		appDescriptorPath := filepath.Join(os.TempDir(), j.jobID+"-app-descriptor.bin")
		defer os.Remove(appDescriptorPath)
		appDescriptorRun, appDescriptorErr := tool.ReadFlash(ctx, j.request.Port, uint64(factory.Offset), 4096, appDescriptorPath)
		j.logSidecar("read_flash_app_descriptor", appDescriptorRun, appDescriptorErr)
		if appDescriptorErr == nil {
			var appDescriptorBytes []byte
			appDescriptorBytes, appDescriptorErr = os.ReadFile(appDescriptorPath)
			if appDescriptorErr == nil {
				currentApp, appDescriptorErr = flash.ParseESPAppDescription(appDescriptorBytes)
			}
		}
		if appDescriptorErr != nil {
			if verified.Manifest.Mode == firmware.ModeAppOnly {
				return j.fail(&result, "APP_DESCRIPTOR_INVALID", fmt.Errorf("read current application descriptor: %w", appDescriptorErr))
			}
			j.log.Event(logging.Warn, "prepare", "firmware", "APP_DESCRIPTOR_UNAVAILABLE", "app_descriptor.unavailable", "The installed application descriptor is unavailable; continuing only because this signed complete image replaces the whole layout.", nil)
		} else {
			j.log.Event(logging.Info, "prepare", "firmware", "APP_DESCRIPTOR_OBSERVED", "app_descriptor.observed", "Read the current ESP-IDF application descriptor before writing.", map[string]any{"project": currentApp.ProjectName, "version": currentApp.Version})
			if err := flash.ValidateCurrentAppDescription(currentApp, verified.Manifest.AppIdentity.ProjectName); err != nil {
				if verified.Manifest.Mode == firmware.ModeAppOnly {
					return j.fail(&result, "FIRMWARE_INCOMPATIBLE", err)
				}
				j.log.Event(logging.Warn, "prepare", "firmware", "APP_DESCRIPTOR_PROJECT_MISMATCH", "app_descriptor.project_mismatch", "The installed application project differs from the complete replacement package; user data may not be compatible.", nil)
			}
		}
	}
	securityRun, err := tool.RunReadOnly(ctx, j.request.Port, "get-security-info")
	j.logSidecar("get-security-info", securityRun, err)
	if err != nil {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", err)
	}
	security := flash.ParseSecurityInfo(securityRun.Output)
	if security.SecureBoot == nil || security.FlashEncryption == nil || security.SecureVersion == nil || *security.SecureBoot || *security.FlashEncryption || verified.Manifest.SecurityBaseline.SecureVersion != 0 || *security.SecureVersion != 0 {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", errors.New("security state is non-baseline or could not be safely determined"))
	}
	for _, f := range verified.Manifest.Files {
		if f.Region == "metadata" {
			continue
		}
		if f.Offset == nil || f.Region == "" {
			return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", fmt.Errorf("image %s has no write offset or region", f.Path))
		}
	}
	tmp, err := os.MkdirTemp("", "clawmatemaker-verified-")
	if err != nil {
		return j.fail(&result, "LOCAL_STORAGE_FAILED", err)
	}
	defer os.RemoveAll(tmp)
	if _, err := firmware.ExtractVerifiedImages(j.request.PackagePath, tmp, verified); err != nil {
		return j.fail(&result, "PACKAGE_EXTRACTION_FAILED", err)
	}
	packageTablePath := filepath.Join(tmp, filepath.FromSlash(verified.Manifest.Layout.PartitionTablePath))
	packageTableRaw, err := os.ReadFile(packageTablePath)
	if err != nil {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", fmt.Errorf("package partition table: %w", err))
	}
	packageLayout, err := partition.Parse(packageTableRaw, uint64(observed.SizeBytes))
	if err != nil {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", err)
	}
	if packageLayout.Fingerprint != verified.Manifest.Layout.Fingerprint {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("package partition table does not match declared layout fingerprint"))
	}
	images := make([]flash.WriteImage, 0, len(verified.Manifest.Files))
	for _, spec := range verified.Manifest.Files {
		if spec.Region == "metadata" {
			continue
		}
		if verified.Manifest.Mode == "app-only" && spec.Region != "app" {
			return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("app-only package contains non-app region %s", spec.Region))
		}
		if verified.Manifest.Mode == firmware.ModeAppOnly {
			factory, ok := partition.Find(currentLayout.Entries, "factory")
			if !ok || uint64(*spec.Offset) < uint64(factory.Offset) || uint64(*spec.Offset)+uint64(spec.Size) > uint64(factory.Offset)+uint64(factory.Size) {
				return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("app image %s is outside factory partition", spec.Path))
			}
		}
		images = append(images, flash.WriteImage{Offset: *spec.Offset, Path: filepath.Join(tmp, filepath.FromSlash(spec.Path)), SHA256: spec.SHA256, Size: spec.Size, Region: spec.Region})
	}
	// A serial port name can be reused after a hot-unplug. Re-query ROM
	// identity immediately before the irreversible operation and compare the
	// in-memory MAC (never emitted to public logs) with the one seen during the
	// compatibility checks above.
	bindingRun, err := tool.RunReadOnly(ctx, j.request.Port, "chip_id")
	j.logSidecar("chip_id_prewrite_binding", bindingRun, err)
	if err != nil {
		return j.fail(&result, "DEVICE_BINDING_UNAVAILABLE", err)
	}
	currentChip := flash.ParseChipID(bindingRun.Output)
	if currentChip.MAC == "" || !strings.EqualFold(currentChip.MAC, chip.MAC) {
		return j.fail(&result, "DEVICE_CHANGED", errors.New("device identity changed after compatibility checks; refusing to write"))
	}
	j.log.Event(logging.Info, "prepare", "device", "DEVICE_BINDING_VERIFIED", "device.binding.verified", "ROM device identity still matches the preflight probe.", nil)
	j.log.Event(logging.Warn, "flash", "job", "RISK_WINDOW_STARTED", "flash.risk_window", "Flash writing is starting. Do not disconnect the device.", map[string]any{"bytes": len(images)})
	bootCritical := false
	for _, image := range images {
		if image.Offset == 0 || image.Offset == 0x8000 {
			bootCritical = true
			break
		}
	}
	if err := WriteJournal(j.logRoot, Journal{JobID: j.jobID, PackageID: verified.Manifest.PackageID, PackageSHA256: verified.ArchiveSHA256, State: JournalWriting, BootCriticalModified: bootCritical}); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	writingStarted = true
	writingBootCritical = bootCritical
	writeRun, writeBaud, err := j.writeWithFallback(ctx, tool, images)
	if err != nil {
		j.log.Event(logging.Warn, "flash", "job", "RECOVERY_REQUIRED", "flash.recovery_required", "Writing was interrupted; enter ROM download mode and retry a complete recovery package.", nil)
		return j.fail(&result, "FLASH_WRITE_FAILED", err)
	}
	result.ImagesWritten = len(images)
	j.log.Event(logging.Info, "flash", "engine", "STAGE_COMPLETED", "stage.completed", "", map[string]any{"durationMs": writeRun.Duration.Milliseconds(), "bytes": len(images), "baud": writeBaud})
	verifyRun, err := tool.VerifyImages(ctx, j.request.Port, writeBaud, images)
	j.logSidecar("verify_flash", verifyRun, err)
	if err != nil {
		return j.fail(&result, "FLASH_VERIFY_FAILED", err)
	}
	j.log.Event(logging.Info, "verify", "engine", "FLASH_VERIFIED", "flash.verified", "All written images were verified by readback hash.", map[string]any{"bytes": len(images), "durationMs": verifyRun.Duration.Milliseconds()})
	bootTimeout := time.Duration(verified.Manifest.BootVerification.TimeoutSeconds) * time.Second
	expectedBoot := verify.Expectation{BoardID: verified.Manifest.Board.ID, LayoutID: verified.Manifest.Layout.ID, ReleaseSequence: verified.Manifest.AppIdentity.ReleaseSequence, ProjectName: verified.Manifest.AppIdentity.ProjectName, AppVersion: verified.Manifest.AppIdentity.AppVersion, AppELFSHA256: verified.Manifest.AppIdentity.ELFSHA256, Chip: verified.Manifest.Chip.Family, FlashBytes: verified.Manifest.Chip.FlashBytes, PSRAMBytes: verified.Manifest.AppIdentity.PSRAMBytes, RequiredSelfTests: verified.Manifest.BootVerification.RequiredSelfTests}
	j.log.Event(logging.Info, "boot", "device", "PORT_REENUMERATION_STARTED", "port.reenumeration.started", "Waiting for the same device serial endpoint after hard reset.", map[string]any{"port": j.request.Port})
	bootCandidate, err := device.WaitForReenumeratedPort(ctx, postFlashCandidate, device.ListCandidates, device.DefaultReenumerationPolicy())
	if err != nil {
		j.log.Event(logging.Warn, "boot", "device", "PORT_REENUMERATION_FAILED", "port.reenumeration.failed", err.Error(), map[string]any{"port": j.request.Port})
		return j.fail(&result, "BOOT_NOT_VERIFIED", err)
	}
	if bootCandidate.Port != j.request.Port {
		j.log.Event(logging.Info, "boot", "device", "PORT_REENUMERATED", "port.reenumerated", "Matched the post-reset serial endpoint using stable USB metadata.", map[string]any{"port": bootCandidate.Port})
	} else {
		j.log.Event(logging.Info, "boot", "device", "PORT_REENUMERATED", "port.reenumerated", "The original serial endpoint returned after reset.", map[string]any{"port": bootCandidate.Port})
	}
	j.log.Event(logging.Info, "boot", "verify", "STAGE_STARTED", "stage.started", "Waiting for a fresh protocol-v2 BOOT_STATUS response.", map[string]any{"port": bootCandidate.Port})
	bootResult, err := verify.Wait(ctx, bootCandidate.Port, verified.Manifest.BootVerification.Baud, bootTimeout, expectedBoot, func(line string) error { return j.log.AppendRaw("serial.log", line) })
	if err != nil {
		j.log.Event(logging.Warn, "boot", "verify", "BOOT_NOT_VERIFIED", "boot.not_verified", err.Error(), nil)
		return j.fail(&result, "BOOT_NOT_VERIFIED", err)
	}
	j.log.Event(logging.Info, "boot", "verify", "BOOT_VERIFIED", "boot.verified", "Received matching protocol-v2 BOOT_STATUS.", map[string]any{"attempt": bootResult.Attempts})
	if err := WriteJournal(j.logRoot, Journal{JobID: j.jobID, PackageID: verified.Manifest.PackageID, PackageSHA256: verified.ArchiveSHA256, State: JournalVerified, BootCriticalModified: bootCritical}); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	return result, nil
}

// writeWithFallback retries the initial flash transfer from high speed to the
// conservative ROM rate. Every failed attempt is preserved in diagnostics.
// Once any write process succeeds, no further baud retry is allowed: later
// verification failure must enter the recovery flow rather than risk a second
// unplanned write to a potentially changed device.
func (j *FlashJob) writeWithFallback(ctx context.Context, tool flash.Tool, images []flash.WriteImage) (flash.Result, int, error) {
	var lastResult flash.Result
	var lastErr error
	for attempt, baud := range flash.SupportedWriteBauds {
		j.log.Event(logging.Info, "flash", "engine", "FLASH_WRITE_ATTEMPT", "flash.write.attempt", "Starting controlled flash transfer.", map[string]any{"attempt": attempt + 1, "baud": baud})
		result, err := tool.WriteImages(ctx, j.request.Port, baud, images)
		j.logSidecar(fmt.Sprintf("write_flash_%d", baud), result, err)
		if err == nil {
			if attempt > 0 {
				j.log.Event(logging.Warn, "flash", "engine", "FLASH_BAUD_FALLBACK_SUCCEEDED", "flash.baud.fallback.succeeded", "Firmware transfer succeeded after lowering the serial speed.", map[string]any{"attempt": attempt + 1, "baud": baud})
			}
			return result, baud, nil
		}
		lastResult, lastErr = result, err
		if !flash.CanRetryWriteAtLowerBaud(result, err) {
			j.log.Event(logging.Warn, "flash", "engine", "FLASH_BAUD_FALLBACK_BLOCKED", "flash.baud.fallback.blocked", "The transfer may have started writing, so automatic retry is blocked and recovery is required.", map[string]any{"attempt": attempt + 1, "baud": baud})
			break
		}
		if attempt+1 < len(flash.SupportedWriteBauds) {
			j.log.Event(logging.Warn, "flash", "engine", "FLASH_BAUD_FALLBACK", "flash.baud.fallback", "Flash transfer failed before a successful write; retrying at a lower serial speed.", map[string]any{"attempt": attempt + 1, "fromBaud": baud, "toBaud": flash.SupportedWriteBauds[attempt+1]})
		}
	}
	return lastResult, 0, fmt.Errorf("flash transfer failed at all supported baud rates: %w", lastErr)
}
func (j *FlashJob) fail(r *FlashResult, code string, err error) (FlashResult, error) {
	r.ErrorCode = code
	r.ErrorMessage = err.Error()
	return *r, err
}

// validateCurrentLayoutForMode keeps the preservation boundary explicit. An
// app-only package can only be safe when it targets exactly the installed
// layout. A signed complete image writes from offset zero and is allowed to
// replace an older layout; the UI has already shown the destructive impact and
// the job records the replacement in the diagnostic journal.
func validateCurrentLayoutForMode(mode, currentFingerprint, expectedFingerprint string) (replacing bool, err error) {
	if currentFingerprint == expectedFingerprint {
		return false, nil
	}
	if mode == firmware.ModeFull {
		return true, nil
	}
	return false, fmt.Errorf("layout fingerprint mismatch: device %s, package %s", currentFingerprint, expectedFingerprint)
}
func (j *FlashJob) logSidecar(action string, r flash.Result, err error) {
	s := logging.Info
	c := "SIDECAR_COMPLETED"
	d := r.Output
	if err != nil {
		s = logging.Error
		c = "SIDECAR_FAILED"
		d = fmt.Sprintf("%v\n%s", err, r.Output)
	}
	_ = j.log.AppendRaw("sidecar.log", fmt.Sprintf("[%s]\n%s\n", action, d))
	j.log.Event(s, "flash", "sidecar", c, "sidecar."+strings.ToLower(c), d, map[string]any{"command": action, "exitCode": r.ExitCode, "durationMs": r.Duration.Milliseconds()})
}
