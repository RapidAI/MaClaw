package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// ExpectedDeviceBinding comes only from an immediately preceding successful
	// read-only probe and is never serialized or logged. It prevents a port
	// reuse between physical-board confirmation and the irreversible write.
	ExpectedDeviceBinding string `json:"-"`
	BoardID               string `json:"boardId"`
	// RetryOfJobID records the prior non-writing failure in the new attempt's
	// durable audit log. It is accepted only from App.RetryJob after it has
	// ruled out recovery-required journals; it is never a resume cursor.
	RetryOfJobID string `json:"-"`
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
	StartedAt     time.Time `json:"startedAt" ts_type:"string"`
	FinishedAt    time.Time `json:"finishedAt" ts_type:"string"`
}
type FlashJob struct {
	request          FlashRequest
	jobID, attemptID string
	log              *logging.Writer
	logRoot          string
	progressMu       sync.Mutex
	progressTotal    int64
	progressCurrent  int64
	progressStarted  time.Time
	progressLastLog  time.Time
}

// JobID is safe to expose to the application coordinator. It is an opaque
// identifier used only for log lookup and cancellation routing.
func (j *FlashJob) JobID() string { return j.jobID }

// RequestCancel records a durable audit event before the App cancels the job
// context and terminates the sidecar process tree. This method does not itself
// decide whether recovery is needed; Run knows whether a write has started.
func (j *FlashJob) RequestCancel() {
	j.log.Event(logging.Warn, "job", "control", "JOB_CANCEL_REQUESTED", "job.cancel.requested", "Cancellation was requested by the user. The current safe operation will stop.", nil)
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
	createdFields := map[string]any{"port": j.request.Port}
	if j.request.RetryOfJobID != "" {
		createdFields["retryOfJobId"] = j.request.RetryOfJobID
		j.log.Event(logging.Info, "prepare", "job", "JOB_RETRY_CREATED", "job.retry.created", "A fresh preflight retry was created for a prior failure that did not begin writing.", createdFields)
	} else {
		j.log.Event(logging.Info, "prepare", "job", "JOB_CREATED", "job.created", "", createdFields)
	}
	releaseLease, err := acquireFlashLease()
	if err != nil {
		return j.fail(&result, "WRITE_JOB_ACTIVE", err)
	}
	defer releaseLease()
	writingStarted := false
	var writingBootCritical bool
	var writingFlashVerified bool
	var packageSHA256 string
	var writingJournal Journal
	defer func() {
		recoveryRequired := false
		if retErr != nil && writingStarted {
			if errors.Is(retErr, context.Canceled) {
				result.ErrorCode = "JOB_CANCELLED_RECOVERY_REQUIRED"
				result.ErrorMessage = "cancellation interrupted an irreversible write; complete ROM recovery is required before another flash"
			}
			// A completed esptool process is not enough to prove that every image
			// was written and booted. Persist the recovery state before closing the
			// job so a later app start cannot mistake an interrupted single-slot
			// factory update for an ordinary retry.
			journal := writingJournal
			if journal.JobID == "" {
				journal = Journal{JobID: j.jobID, PackageID: result.PackageID, PackageSHA256: packageSHA256, BootCriticalModified: writingBootCritical}
			}
			journal.State = JournalRecoveryRequired
			journal.FlashVerified = writingFlashVerified && HasCompleteReadbackEvidence(journal)
			if err := WriteJournal(j.logRoot, journal); err != nil {
				j.log.Event(logging.Error, "flash", "journal", "JOURNAL_RECOVERY_WRITE_FAILED", "journal.recovery.write.failed", err.Error(), nil)
			} else {
				recoveryRequired = true
				j.log.Event(logging.Warn, "flash", "journal", "RECOVERY_REQUIRED", "flash.recovery_required", "The write did not complete a verified boot. Keep the device connected and use a complete recovery package through ROM download mode.", nil)
			}
		}
		result.FinishedAt = time.Now().UTC()
		if retErr != nil {
			if recoveryRequired {
				result.Status = "recovery_required"
				j.log.Event(logging.Error, "flash", "job", result.ErrorCode, "job.recovery_required", result.ErrorMessage, nil)
			} else {
				result.Status = "failed"
				j.log.Event(logging.Error, "flash", "job", result.ErrorCode, "job.failed", result.ErrorMessage, nil)
			}
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
	// The board selection is a deliberate physical-label confirmation in the
	// UI, but it cannot be the only board binding.  Before a write, require the
	// currently running application to return a fresh nonce-bound identity that
	// agrees with both the selected catalog profile and the signed package. ROM
	// data alone cannot distinguish these ESP32-S3 products.  Recovery is the
	// sole exception: a damaged application may be unable to answer, and the
	// explicit full-package recovery flow remains protected by its ROM/layout
	// and selected-board gates below.
	if !j.request.Recovery {
		identity, identityErr := device.ProbeApplicationIdentity(ctx, j.request.Port)
		if identityErr != nil {
			return j.fail(&result, "DEVICE_IDENTITY_UNAVAILABLE", fmt.Errorf("nonce-bound application identity: %w", identityErr))
		}
		if identity.Protocol != device.ProtocolVersion ||
			identity.FirmwareTargetBoardID != verified.Manifest.Board.ID ||
			identity.FirmwareTargetBoardID != profile.FirmwareBoardID ||
			!strings.EqualFold(identity.Chip, verified.Manifest.Chip.Family) ||
			identity.FlashBytes != verified.Manifest.Chip.FlashBytes ||
			identity.PSRAMBytes < verified.Manifest.AppIdentity.PSRAMBytes {
			return j.fail(&result, "DEVICE_IDENTITY_MISMATCH", errors.New("nonce-bound application identity does not match the selected signed firmware profile"))
		}
		j.log.Event(logging.Info, "prepare", "device", "DEVICE_IDENTITY_VERIFIED", "device.identity.verified", "Fresh protocol-v2 application identity matches the selected signed firmware profile.", nil)
	}
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
	if j.request.ExpectedDeviceBinding == "" || deviceBinding(chip.MAC) != j.request.ExpectedDeviceBinding {
		return j.fail(&result, "DEVICE_CHANGED", errors.New("ROM device identity no longer matches the confirmed physical board"))
	}
	j.log.Event(logging.Info, "prepare", "device", "CONFIRMED_DEVICE_BINDING_VERIFIED", "device.confirmed_binding.verified", "ROM device identity matches the board-confirmation probe.", nil)
	flashRun, err := tool.RunReadOnly(ctx, j.request.Port, "flash_id")
	j.logSidecar("flash_id", flashRun, err)
	if err != nil {
		return j.fail(&result, "FLASH_PROBE_FAILED", err)
	}
	observed := flash.ParseFlashID(flashRun.Output)
	required := verified.Manifest.Chip.FlashBytes
	// Capacity is an identity boundary for the official catalog, not merely a
	// minimum storage budget. In particular, a 32 MiB Waveshare package must
	// never be written to a larger, differently provisioned ESP32-S3 device.
	// The selected profile and manifest have already been bound above; keep the
	// independently observed ROM value exact here at the write boundary.
	if j.request.ExpectedFlashBytes > 0 && j.request.ExpectedFlashBytes != required {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("selected profile flash capacity %d does not match signed package capacity %d", j.request.ExpectedFlashBytes, required))
	}
	if observed.SizeBytes <= 0 || observed.SizeBytes != required {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("flash capacity %d does not exactly match required %d", observed.SizeBytes, required))
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
	if err := firmware.ValidateSecurityBaseline(verified.Manifest.SecurityBaseline); err != nil {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", err)
	}
	if security.SecureBoot == nil || security.FlashEncryption == nil || security.SecureVersion == nil || *security.SecureBoot || *security.FlashEncryption || *security.SecureVersion != verified.Manifest.SecurityBaseline.SecureVersion {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", errors.New("security state is non-baseline or could not be safely determined"))
	}
	j.log.Event(logging.Info, "prepare", "security", "SECURITY_BASELINE_VERIFIED", "security.baseline.verified", "Signed package security baseline matches the read-only eFuse state.", nil)
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
	imagesByName := make(map[string]flash.WriteImage, len(verified.Manifest.Files))
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
		image := flash.WriteImage{Offset: *spec.Offset, Path: filepath.Join(tmp, filepath.FromSlash(spec.Path)), SHA256: spec.SHA256, Size: spec.Size, Region: spec.Region}
		images = append(images, image)
		if spec.Name != "" {
			imagesByName[spec.Name] = image
		}
	}
	if len(verified.Manifest.WriteOrder) != 0 {
		ordered := make([]flash.WriteImage, 0, len(images))
		for _, name := range verified.Manifest.WriteOrder {
			image, ok := imagesByName[name]
			if !ok {
				return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", fmt.Errorf("signed write order image %q is unavailable", name))
			}
			ordered = append(ordered, image)
		}
		images = ordered
		j.log.Event(logging.Info, "prepare", "flash", "WRITE_ORDER_VERIFIED", "write_order.verified", "Applied the signed full-install image order; archive entry order is ignored.", map[string]any{"images": len(images)})
	}
	if err := validateWritePlan(images, uint64(observed.SizeBytes)); err != nil {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", err)
	}
	j.beginWriteProgress(images)
	j.log.Event(logging.Info, "prepare", "flash", "WRITE_PLAN_VERIFIED", "write_plan.verified", "Verified every signed image range is non-overlapping and inside the observed Flash capacity.", map[string]any{"images": len(images), "flashBytes": observed.SizeBytes})
	for _, image := range images {
		j.logImage(logging.Info, "FLASH_IMAGE_PLANNED", "flash.image.planned", "Signed image is included in the immutable write plan.", image, 0)
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
	journalImages := journalImagesFromWritePlan(images)
	planSHA256, err := JournalPlanSHA256(journalImages)
	if err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	writingJournal = Journal{JobID: j.jobID, PackageID: verified.Manifest.PackageID, PackageSHA256: verified.ArchiveSHA256, PlanSHA256: planSHA256, Images: journalImages, State: JournalWriting, BootCriticalModified: bootCritical}
	if err := WriteJournal(j.logRoot, writingJournal); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	writingStarted = true
	writingBootCritical = bootCritical
	writeRun, writeBaud, err := j.writeWithFallback(ctx, tool, images, len(verified.Manifest.WriteOrder) != 0, &writingJournal)
	if err != nil {
		j.log.Event(logging.Warn, "flash", "job", "RECOVERY_REQUIRED", "flash.recovery_required", "Writing was interrupted; enter ROM download mode and retry a complete recovery package.", nil)
		return j.fail(&result, "FLASH_WRITE_FAILED", err)
	}
	result.ImagesWritten = len(images)
	j.log.Event(logging.Info, "flash", "engine", "STAGE_COMPLETED", "stage.completed", "", map[string]any{"durationMs": writeRun.Duration.Milliseconds(), "bytes": len(images), "baud": writeBaud})
	if len(verified.Manifest.WriteOrder) == 0 {
		for _, image := range images {
			// Legacy packages transfer the frozen image set in one controlled
			// invocation. This acknowledgement is intentionally not called
			// "verified": the next ROM verify_flash is the evidence that permits
			// boot verification.
			j.logImage(logging.Info, "FLASH_IMAGE_WRITE_ACKNOWLEDGED", "flash.image.write.acknowledged", "The write engine accepted this planned image; ROM readback verification is pending.", image, writeBaud)
		}
		if err := j.updateJournalImageStates(&writingJournal, images, JournalImageWritten); err != nil {
			return j.fail(&result, "JOURNAL_INVALID", err)
		}
		verifyRun, verifyErr := tool.VerifyImages(ctx, j.request.Port, writeBaud, images)
		j.logSidecar("verify_flash", verifyRun, verifyErr)
		if verifyErr != nil {
			return j.fail(&result, "FLASH_VERIFY_FAILED", verifyErr)
		}
		j.log.Event(logging.Info, "verify", "engine", "FLASH_VERIFIED", "flash.verified", "All written images were verified by readback hash.", map[string]any{"bytes": len(images), "durationMs": verifyRun.Duration.Milliseconds()})
		for _, image := range images {
			j.logImage(logging.Info, "FLASH_IMAGE_VERIFIED", "flash.image.verified", "ROM readback verification completed for this image range.", image, writeBaud)
		}
		if err := j.updateJournalImageStates(&writingJournal, images, JournalImageReadbackVerified); err != nil {
			return j.fail(&result, "JOURNAL_INVALID", err)
		}
	} else {
		// writeSplitImages has already acknowledged and read back each image
		// before moving to the next signed-order boundary.
		j.log.Event(logging.Info, "verify", "engine", "FLASH_VERIFIED", "flash.verified", "Every signed-order image was individually written and verified by ROM readback.", map[string]any{"images": len(images), "baud": writeBaud})
	}
	// Keep the readback boundary in the durable journal. A later BOOT_STATUS
	// timeout can safely offer a non-writing verification retry; an interrupted
	// transfer or failed readback must instead remain a full ROM recovery case.
	if !allJournalImagesAtState(writingJournal.Images, JournalImageReadbackVerified) {
		return j.fail(&result, "JOURNAL_INVALID", errors.New("not every planned image has durable ROM readback evidence"))
	}
	writingJournal.FlashVerified = true
	if err := WriteJournal(j.logRoot, writingJournal); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	writingFlashVerified = true
	j.log.Event(logging.Info, "verify", "journal", "FLASH_READBACK_PERSISTED", "flash.readback.persisted", "ROM readback evidence was persisted before application boot verification.", nil)
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
	writingJournal.State = JournalVerified
	if err := WriteJournal(j.logRoot, writingJournal); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	return result, nil
}

// writeWithFallback retries the initial flash transfer from high speed to the
// conservative ROM rate. Every failed attempt is preserved in diagnostics.
// Once any write process succeeds, no further baud retry is allowed: later
// verification failure must enter the recovery flow rather than risk a second
// unplanned write to a potentially changed device.
func (j *FlashJob) writeWithFallback(ctx context.Context, tool flash.Tool, images []flash.WriteImage, split bool, journal *Journal) (flash.Result, int, error) {
	var lastResult flash.Result
	var lastErr error
	for attempt, baud := range flash.SupportedWriteBauds {
		j.log.Event(logging.Info, "flash", "engine", "FLASH_WRITE_ATTEMPT", "flash.write.attempt", "Starting controlled flash transfer.", map[string]any{"attempt": attempt + 1, "baud": baud})
		if split {
			result, err := j.writeSplitImages(ctx, tool, baud, images, journal)
			j.logSidecar(fmt.Sprintf("write_flash_%d", baud), result, err)
			if err == nil {
				return result, baud, nil
			}
			return result, 0, fmt.Errorf("signed-order image transfer failed: %w", err)
		}
		result, err := tool.WriteImagesWithProgress(ctx, j.request.Port, baud, images, j.reportWriteProgress)
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

func (j *FlashJob) writeSplitImages(ctx context.Context, tool flash.Tool, baud int, images []flash.WriteImage, journal *Journal) (flash.Result, error) {
	var total flash.Result
	var completedBytes int64
	for index, image := range images {
		j.log.Event(logging.Info, "flash", "engine", "FLASH_IMAGE_WRITE_STARTED", "flash.image.write.started", "Writing the next signed-order image while the device remains in ROM download mode.", map[string]any{"index": index + 1, "images": len(images), "offset": image.Offset, "region": image.Region, "baud": baud})
		result, err := tool.WriteImagesWithProgress(ctx, j.request.Port, baud, []flash.WriteImage{image}, func(progress flash.WriteProgress) {
			progress.TransferredBytes += completedBytes
			progress.TotalBytes = j.progressTotalBytes()
			j.reportWriteProgress(progress)
		})
		j.logSidecar(fmt.Sprintf("write_flash_%d_%d", baud, index+1), result, err)
		total.Duration += result.Duration
		total.ExitCode = result.ExitCode
		if err != nil {
			return total, err
		}
		j.logImage(logging.Info, "FLASH_IMAGE_WRITE_ACKNOWLEDGED", "flash.image.write.acknowledged", "The write engine accepted this signed-order image; ROM readback verification is pending.", image, baud)
		if err := j.updateJournalImageStates(journal, []flash.WriteImage{image}, JournalImageWritten); err != nil {
			return total, err
		}
		last := index == len(images)-1
		var verifyResult flash.Result
		if last {
			verifyResult, err = tool.VerifyImages(ctx, j.request.Port, baud, []flash.WriteImage{image})
		} else {
			verifyResult, err = tool.VerifyImagesNoReset(ctx, j.request.Port, baud, []flash.WriteImage{image})
		}
		j.logSidecar(fmt.Sprintf("verify_flash_%d", index+1), verifyResult, err)
		total.Duration += verifyResult.Duration
		total.ExitCode = verifyResult.ExitCode
		if err != nil {
			return total, err
		}
		j.logImage(logging.Info, "FLASH_IMAGE_VERIFIED", "flash.image.verified", "ROM readback verification completed for this signed-order image range.", image, baud)
		if err := j.updateJournalImageStates(journal, []flash.WriteImage{image}, JournalImageReadbackVerified); err != nil {
			return total, err
		}
		completedBytes += image.Size
	}
	return total, nil
}

func journalImagesFromWritePlan(images []flash.WriteImage) []JournalImage {
	result := make([]JournalImage, 0, len(images))
	for index, image := range images {
		result = append(result, JournalImage{Name: fmt.Sprintf("image-%03d", index+1), Region: image.Region, Offset: image.Offset, Size: image.Size, SHA256: image.SHA256, State: JournalImagePlanned})
	}
	return result
}

func allJournalImagesAtState(images []JournalImage, expected JournalImageState) bool {
	if len(images) == 0 {
		return false
	}
	for _, image := range images {
		if image.State != expected {
			return false
		}
	}
	return true
}

// updateJournalImageStates changes the canonical in-memory plan and persists
// it atomically after every evidence boundary. Keeping that same object means
// the recovery defer always retains the latest completed-image evidence.
func (j *FlashJob) updateJournalImageStates(stateOwner *Journal, images []flash.WriteImage, state JournalImageState) error {
	if stateOwner == nil {
		return errors.New("journal state owner is required")
	}
	journal := *stateOwner
	for _, image := range images {
		found := false
		for index := range journal.Images {
			entry := &journal.Images[index]
			if entry.Offset == image.Offset && entry.Size == image.Size && entry.Region == image.Region && entry.SHA256 == image.SHA256 {
				entry.State = state
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("planned journal image at 0x%x is missing", image.Offset)
		}
	}
	if err := WriteJournal(j.logRoot, journal); err != nil {
		return err
	}
	*stateOwner = journal
	return nil
}

// beginWriteProgress establishes the denominator from the exact signed image
// lengths. It is never derived from wall-clock time or archive length.
func (j *FlashJob) beginWriteProgress(images []flash.WriteImage) {
	var total int64
	for _, image := range images {
		total += image.Size
	}
	j.progressMu.Lock()
	j.progressTotal = total
	j.progressCurrent = 0
	j.progressStarted = time.Now()
	j.progressLastLog = time.Time{}
	j.progressMu.Unlock()
}

func (j *FlashJob) progressTotalBytes() int64 {
	j.progressMu.Lock()
	defer j.progressMu.Unlock()
	return j.progressTotal
}

// reportWriteProgress records real engine telemetry at no more than 5 Hz.
// The UI can coalesce it further, while the log remains an evidence trail.
func (j *FlashJob) reportWriteProgress(progress flash.WriteProgress) {
	j.progressMu.Lock()
	defer j.progressMu.Unlock()
	if j.progressTotal <= 0 || progress.TransferredBytes < j.progressCurrent {
		return
	}
	if progress.TransferredBytes > j.progressTotal {
		progress.TransferredBytes = j.progressTotal
	}
	now := time.Now()
	if progress.TransferredBytes == j.progressCurrent && !j.progressLastLog.IsZero() {
		return
	}
	if !j.progressLastLog.IsZero() && progress.TransferredBytes < j.progressTotal && now.Sub(j.progressLastLog) < 200*time.Millisecond {
		j.progressCurrent = progress.TransferredBytes
		return
	}
	j.progressCurrent = progress.TransferredBytes
	j.progressLastLog = now
	elapsed := now.Sub(j.progressStarted).Seconds()
	fields := map[string]any{"transferredBytes": progress.TransferredBytes, "totalBytes": j.progressTotal, "percent": float64(progress.TransferredBytes) * 100 / float64(j.progressTotal)}
	if elapsed > 0 {
		fields["bytesPerSecond"] = int64(float64(progress.TransferredBytes) / elapsed)
	}
	j.log.Event(logging.Info, "flash", "engine", "FLASH_TRANSFER_PROGRESS", "flash.transfer.progress", "Measured transfer progress from the flash engine.", fields)
}
func (j *FlashJob) fail(r *FlashResult, code string, err error) (FlashResult, error) {
	if errors.Is(err, context.Canceled) {
		code = "JOB_CANCELLED"
	}
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

// validateWritePlan is the last pure-data boundary before a device write. It
// repeats the per-file checks done by archive verification after extraction,
// this time against the *observed* device capacity. In particular, a validly
// signed full image must not be handed to esptool when a smaller or otherwise
// incompatible flash chip is connected.
func validateWritePlan(images []flash.WriteImage, flashBytes uint64) error {
	if flashBytes == 0 || len(images) == 0 || len(images) > 16 {
		return errors.New("invalid write plan or observed flash capacity")
	}
	ordered := append([]flash.WriteImage(nil), images...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Offset < ordered[j].Offset })
	var previousEnd uint64
	for index, image := range ordered {
		if image.Offset%0x1000 != 0 || image.Size <= 0 {
			return fmt.Errorf("write image %d has an invalid offset or size", index)
		}
		size := uint64(image.Size)
		if size > ^uint64(0)-image.Offset || image.Offset+size > flashBytes {
			return fmt.Errorf("write image %d exceeds observed flash capacity", index)
		}
		if index > 0 && image.Offset < previousEnd {
			return fmt.Errorf("write images overlap at offset %#x", image.Offset)
		}
		previousEnd = image.Offset + size
	}
	return nil
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

// logImage records per-image evidence without exposing a host file path or
// firmware bytes. Large complete images still travel in one esptool command,
// but diagnostics can identify the exact signed range that was planned,
// accepted by the writer, and later verified from ROM.
func (j *FlashJob) logImage(severity logging.Severity, code, messageKey, detail string, image flash.WriteImage, baud int) {
	fields := map[string]any{
		"image":  filepath.Base(image.Path),
		"region": image.Region,
		"offset": image.Offset,
		"size":   image.Size,
		"sha256": image.SHA256,
	}
	if baud > 0 {
		fields["baud"] = baud
	}
	j.log.Event(severity, "flash", "engine", code, messageKey, detail, fields)
}
