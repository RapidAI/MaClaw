package jobs

import (
	"context"
	"fmt"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/logging"
)

// DownloadJob records official firmware discovery, transfer and signature
// verification as one diagnostic job. A package is never returned as ready
// until its release signature validates against the desktop application's
// compiled-in trust store.
type DownloadJob struct {
	boardID, jobID, attemptID string
	cacheDir                  string
	trust                     firmware.TrustStore
	log                       *logging.Writer
}

func NewDownloadJob(logRoot, cacheDir, boardID string, trust firmware.TrustStore, emit func(logging.Event)) (*DownloadJob, error) {
	if _, err := catalog.Profile(boardID); err != nil {
		return nil, err
	}
	jobID, err := id("job")
	if err != nil {
		return nil, err
	}
	attemptID, err := id("attempt")
	if err != nil {
		return nil, err
	}
	w, err := logging.New(logRoot, jobID, attemptID, emit)
	if err != nil {
		return nil, err
	}
	return &DownloadJob{boardID: boardID, jobID: jobID, attemptID: attemptID, cacheDir: cacheDir, trust: trust, log: w}, nil
}

func (j *DownloadJob) Run(ctx context.Context) (result catalog.DownloadedRelease, retErr error) {
	started := time.Now()
	result.JobID = j.jobID
	j.log.Event(logging.Info, "download", "job", "JOB_CREATED", "job.created", "Starting official firmware download job.", map[string]any{"boardId": j.boardID})
	defer func() {
		if retErr != nil {
			j.log.Event(logging.Error, "download", "job", "JOB_FAILED", "job.failed", retErr.Error(), map[string]any{"boardId": j.boardID})
		} else {
			j.log.Event(logging.Info, "download", "job", "JOB_SUCCEEDED", "job.succeeded", "Official firmware package is verified and ready for installation.", map[string]any{"boardId": j.boardID, "durationMs": time.Since(started).Milliseconds()})
		}
		_ = j.log.WriteSummary(result)
		_ = j.log.Close()
	}()

	emit := func(event logging.Event) {
		j.log.Event(event.Severity, event.Stage, event.Component, event.Code, event.MessageKey, event.Detail, event.Fields)
	}
	result, retErr = catalog.NewClient(j.cacheDir).DownloadLatest(ctx, j.boardID, emit)
	result.JobID = j.jobID
	if retErr != nil {
		return result, retErr
	}
	j.log.Event(logging.Info, "download", "firmware", "RELEASE_SIGNATURE_VERIFY_STARTED", "release.signature.verify.started", "Verifying downloaded firmware manifest signature against the embedded official trust store.", map[string]any{"boardId": j.boardID, "asset": result.AssetName})
	verified, err := firmware.VerifyRelease(result.Path, j.trust)
	if err != nil {
		retErr = fmt.Errorf("official firmware signature verification: %w", err)
		return result, retErr
	}
	profile, err := catalog.Profile(j.boardID)
	if err != nil {
		retErr = err
		return result, retErr
	}
	if err := catalog.ValidateManifestBinding(profile, verified.Manifest.Board.ID, verified.Manifest.Board.ProfileHash, verified.Manifest.Chip.Family, verified.Manifest.Chip.FlashBytes); err != nil {
		retErr = fmt.Errorf("official firmware catalog binding: %w", err)
		return result, retErr
	}
	plan, err := firmware.InstallPlanFor(verified.Manifest)
	if err != nil {
		retErr = fmt.Errorf("official firmware install plan: %w", err)
		return result, retErr
	}
	result.InstallStatus = "verified_ready"
	result.InstallPlan = plan.Mode
	result.PreservesUserData = plan.PreservesUserData
	result.RequiresRecovery = plan.RequiresRecovery
	result.SafetyNote = plan.Summary + " " + plan.Warning
	j.log.Event(logging.Info, "download", "firmware", "RELEASE_SIGNATURE_VERIFIED", "release.signature.verified", "Firmware archive integrity, official signature, and install impact were verified.", map[string]any{"boardId": j.boardID, "asset": result.AssetName, "sha256": result.SHA256})
	return result, nil
}
