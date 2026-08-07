package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/logging"
	"clawmatemaker/internal/verify"
)

// BootVerificationResult is the browser-safe result of a read-only retry of
// the formal application BOOT_STATUS protocol. It never contains a nonce or
// raw device identifiers.
type BootVerificationResult struct {
	JobID        string    `json:"jobId"`
	AttemptID    string    `json:"attemptId"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	Attempts     int       `json:"attempts"`
	StartedAt    time.Time `json:"startedAt" ts_type:"string"`
	FinishedAt   time.Time `json:"finishedAt" ts_type:"string"`
}

// VerifyBootRetry only opens the running application serial endpoint and
// sends nonce-bound BOOT_STATUS queries. It is available solely after the
// original job persisted successful ROM readback evidence, so it can never
// turn a partly written image into an unsafe ordinary retry path.
func VerifyBootRetry(ctx context.Context, logRoot, originalJobID, port, packagePath string, trust firmware.TrustStore, emit func(logging.Event)) (result BootVerificationResult, retErr error) {
	if !logging.SafeJobID(originalJobID) {
		return result, errors.New("invalid original job ID")
	}
	journal, err := ReadJournal(logRoot, originalJobID)
	if err != nil {
		return result, fmt.Errorf("read recovery journal: %w", err)
	}
	if journal.State != JournalRecoveryRequired || !HasCompleteReadbackEvidence(journal) {
		return result, errors.New("boot verification retry is unavailable until every image has passed ROM readback verification")
	}
	verified, err := firmware.VerifyRelease(packagePath, trust)
	if err != nil {
		return result, fmt.Errorf("verify firmware package: %w", err)
	}
	if verified.Manifest.PackageID != journal.PackageID || verified.ArchiveSHA256 != journal.PackageSHA256 {
		return result, errors.New("verified firmware package does not match the interrupted write journal")
	}
	jobID, err := id("job")
	if err != nil {
		return result, err
	}
	attemptID, err := id("attempt")
	if err != nil {
		return result, err
	}
	log, err := logging.New(logRoot, jobID, attemptID, emit)
	if err != nil {
		return result, err
	}
	result = BootVerificationResult{JobID: jobID, AttemptID: attemptID, Status: "running", StartedAt: time.Now().UTC()}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if retErr != nil {
			result.Status = "failed"
			log.Event(logging.Warn, "boot", "job", result.ErrorCode, "job.failed", result.ErrorMessage, nil)
		} else {
			result.Status = "succeeded"
			log.Event(logging.Info, "boot", "job", "JOB_SUCCEEDED", "job.succeeded", "The interrupted job is now confirmed as booted without rewriting Flash.", nil)
		}
		_ = log.WriteSummary(result)
		_ = log.Close()
	}()
	log.Event(logging.Info, "boot", "job", "JOB_CREATED", "job.created", "Starting a read-only retry of formal boot verification.", map[string]any{"originalJobId": originalJobID})
	if port == "" {
		return bootRetryFail(&result, "BOOT_VERIFY_PORT_REQUIRED", errors.New("application serial port is required"))
	}
	policy := verified.Manifest.BootVerification
	if policy.Baud <= 0 || policy.TimeoutSeconds <= 0 {
		return bootRetryFail(&result, "PACKAGE_MANIFEST_INCOMPLETE", errors.New("release package boot verification policy is incomplete"))
	}
	expected := verify.Expectation{BoardID: verified.Manifest.Board.ID, LayoutID: verified.Manifest.Layout.ID, ReleaseSequence: verified.Manifest.AppIdentity.ReleaseSequence, ProjectName: verified.Manifest.AppIdentity.ProjectName, AppVersion: verified.Manifest.AppIdentity.AppVersion, AppELFSHA256: verified.Manifest.AppIdentity.ELFSHA256, Chip: verified.Manifest.Chip.Family, FlashBytes: verified.Manifest.Chip.FlashBytes, PSRAMBytes: verified.Manifest.AppIdentity.PSRAMBytes, RequiredSelfTests: policy.RequiredSelfTests}
	log.Event(logging.Info, "boot", "verify", "BOOT_REVERIFY_STARTED", "boot.reverify.started", "Sending a fresh nonce-bound BOOT_STATUS query; no Flash command will be issued.", map[string]any{"port": port})
	boot, err := verify.Wait(ctx, port, policy.Baud, time.Duration(policy.TimeoutSeconds)*time.Second, expected, func(line string) error { return log.AppendRaw("serial.log", line) })
	if err != nil {
		return bootRetryFail(&result, "BOOT_NOT_VERIFIED", err)
	}
	result.Attempts = boot.Attempts
	if err := MarkRecoveryResolved(logRoot, originalJobID); err != nil {
		return bootRetryFail(&result, "JOURNAL_INVALID", fmt.Errorf("mark original recovery as verified: %w", err))
	}
	log.Event(logging.Info, "boot", "verify", "BOOT_REVERIFIED", "boot.reverified", "Received matching protocol-v2 BOOT_STATUS and cleared the recovery block without rewriting Flash.", map[string]any{"attempt": boot.Attempts})
	return result, nil
}

func bootRetryFail(result *BootVerificationResult, code string, err error) (BootVerificationResult, error) {
	result.ErrorCode = code
	result.ErrorMessage = err.Error()
	return *result, err
}
