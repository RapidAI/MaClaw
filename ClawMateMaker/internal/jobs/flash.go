package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	defer func() {
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
	if err := WriteJournal(j.logRoot, Journal{JobID: j.jobID, PackageID: verified.Manifest.PackageID, PackageSHA256: verified.ArchiveSHA256, State: JournalPrepared}); err != nil {
		return j.fail(&result, "JOURNAL_INVALID", err)
	}
	if verified.Manifest.Board.ID == "" || verified.Manifest.Chip.Family == "" || verified.Manifest.Chip.FlashBytes <= 0 {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare board and chip capacity"))
	}
	if verified.Manifest.Layout.Fingerprint == "" || verified.Manifest.Layout.PartitionTablePath == "" || (verified.Manifest.Mode != "full" && verified.Manifest.Mode != "app-only") {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare a known layout and mode"))
	}
	if verified.Manifest.AppIdentity.ProjectName == "" || verified.Manifest.AppIdentity.AppVersion == "" || verified.Manifest.AppIdentity.ELFSHA256 == "" || verified.Manifest.BootVerification.Baud <= 0 || verified.Manifest.BootVerification.TimeoutSeconds <= 0 {
		return j.fail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package must declare app identity and boot verification policy"))
	}
	if j.request.BoardID != "" && j.request.BoardID != verified.Manifest.Board.ID {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", errors.New("requested board does not match package"))
	}
	j.log.Event(logging.Info, "prepare", "firmware", "PACKAGE_VERIFIED", "package.verified", "", map[string]any{"bytes": len(verified.Manifest.Files)})
	tool, err := flash.FindTool()
	if err != nil {
		return j.fail(&result, "TOOL_UNAVAILABLE", err)
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
	currentLayout, err := partition.Parse(partitionBytes, uint64(observed.SizeBytes))
	if err != nil {
		return j.fail(&result, "PARTITION_LAYOUT_INVALID", err)
	}
	if currentLayout.Fingerprint != verified.Manifest.Layout.Fingerprint {
		return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("layout fingerprint mismatch: device %s, package %s", currentLayout.Fingerprint, verified.Manifest.Layout.Fingerprint))
	}
	j.log.Event(logging.Info, "prepare", "partition", "LAYOUT_VERIFIED", "layout.verified", "", map[string]any{"bytes": len(currentLayout.Entries)})
	securityRun, err := tool.RunReadOnly(ctx, j.request.Port, "get-security-info")
	j.logSidecar("get-security-info", securityRun, err)
	if err != nil {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", err)
	}
	security := flash.ParseSecurityInfo(securityRun.Output)
	if security.SecureBoot == nil || security.FlashEncryption == nil || *security.SecureBoot || *security.FlashEncryption || verified.Manifest.SecurityBaseline.SecureVersion != 0 || (security.SecureVersion != nil && *security.SecureVersion != 0) {
		return j.fail(&result, "SECURITY_STATE_UNSUPPORTED", errors.New("security state is non-baseline or could not be safely determined"))
	}
	for _, f := range verified.Manifest.Files {
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
		if verified.Manifest.Mode == "app-only" && spec.Region != "app" {
			return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("app-only package contains non-app region %s", spec.Region))
		}
		if verified.Manifest.Mode == "app-only" {
			factory, ok := partition.Find(currentLayout.Entries, "factory")
			if !ok || uint64(*spec.Offset) < uint64(factory.Offset) || uint64(*spec.Offset)+uint64(spec.Size) > uint64(factory.Offset)+uint64(factory.Size) {
				return j.fail(&result, "FIRMWARE_INCOMPATIBLE", fmt.Errorf("app image %s is outside factory partition", spec.Path))
			}
		}
		images = append(images, flash.WriteImage{Offset: *spec.Offset, Path: filepath.Join(tmp, filepath.FromSlash(spec.Path)), SHA256: spec.SHA256, Size: spec.Size, Region: spec.Region})
	}
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
	writeRun, err := tool.WriteImages(ctx, j.request.Port, 921600, images)
	j.logSidecar("write_flash", writeRun, err)
	if err != nil {
		j.log.Event(logging.Warn, "flash", "job", "RECOVERY_REQUIRED", "flash.recovery_required", "Writing was interrupted; enter ROM download mode and retry a complete recovery package.", nil)
		return j.fail(&result, "FLASH_WRITE_FAILED", err)
	}
	result.ImagesWritten = len(images)
	j.log.Event(logging.Info, "flash", "engine", "STAGE_COMPLETED", "stage.completed", "", map[string]any{"durationMs": writeRun.Duration.Milliseconds(), "bytes": len(images)})
	verifyRun, err := tool.VerifyImages(ctx, j.request.Port, 921600, images)
	j.logSidecar("verify_flash", verifyRun, err)
	if err != nil {
		return j.fail(&result, "FLASH_VERIFY_FAILED", err)
	}
	j.log.Event(logging.Info, "verify", "engine", "FLASH_VERIFIED", "flash.verified", "All written images were verified by readback hash.", map[string]any{"bytes": len(images), "durationMs": verifyRun.Duration.Milliseconds()})
	bootTimeout := time.Duration(verified.Manifest.BootVerification.TimeoutSeconds) * time.Second
	expectedBoot := verify.Expectation{BoardID: verified.Manifest.Board.ID, LayoutID: verified.Manifest.Layout.Fingerprint, ReleaseSequence: verified.Manifest.AppIdentity.ReleaseSequence, ProjectName: verified.Manifest.AppIdentity.ProjectName, AppVersion: verified.Manifest.AppIdentity.AppVersion, AppELFSHA256: verified.Manifest.AppIdentity.ELFSHA256, Chip: verified.Manifest.Chip.Family, FlashBytes: verified.Manifest.Chip.FlashBytes, PSRAMBytes: verified.Manifest.AppIdentity.PSRAMBytes, RequiredSelfTests: verified.Manifest.BootVerification.RequiredSelfTests}
	j.log.Event(logging.Info, "boot", "verify", "STAGE_STARTED", "stage.started", "Waiting for a fresh protocol-v2 BOOT_STATUS response.", nil)
	bootResult, err := verify.Wait(ctx, j.request.Port, verified.Manifest.BootVerification.Baud, bootTimeout, expectedBoot, func(line string) error { return j.log.AppendRaw("serial.log", line) })
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
func (j *FlashJob) fail(r *FlashResult, code string, err error) (FlashResult, error) {
	r.ErrorCode = code
	r.ErrorMessage = err.Error()
	return *r, err
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
