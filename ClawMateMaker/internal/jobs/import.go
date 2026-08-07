package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/logging"
)

// ImportJob is the offline counterpart to DownloadJob. The selected archive
// remains in its user-owned location; it is only referenced after signature
// and board/install-plan validation succeeds.
type ImportJob struct {
	boardID, packagePath, jobID, attemptID string
	channel                                catalog.ReleaseChannel
	trust                                  firmware.TrustStore
	log                                    *logging.Writer
}

func NewImportJob(logRoot, boardID, packagePath string, trust firmware.TrustStore, emit func(logging.Event)) (*ImportJob, error) {
	return NewImportJobForChannel(logRoot, boardID, packagePath, catalog.StableChannel, trust, emit)
}

func NewImportJobForChannel(logRoot, boardID, packagePath string, channel catalog.ReleaseChannel, trust firmware.TrustStore, emit func(logging.Event)) (*ImportJob, error) {
	if _, err := catalog.Profile(boardID); err != nil {
		return nil, err
	}
	canonicalChannel, err := catalog.NormalizeReleaseChannel(channel)
	if err != nil {
		return nil, err
	}
	if filepath.Ext(packagePath) != ".clawfw" {
		return nil, fmt.Errorf("offline firmware package must use the .clawfw extension")
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
	return &ImportJob{boardID: boardID, packagePath: packagePath, jobID: jobID, attemptID: attemptID, channel: canonicalChannel, trust: trust, log: w}, nil
}

func (j *ImportJob) Run(ctx context.Context) (result catalog.DownloadedRelease, retErr error) {
	started := time.Now()
	result.JobID = j.jobID
	j.log.Event(logging.Info, "import", "job", "JOB_CREATED", "job.created", "Starting offline signed firmware package import.", map[string]any{"boardId": j.boardID, "channel": j.channel})
	defer func() {
		if retErr != nil {
			j.log.Event(logging.Error, "import", "job", "JOB_FAILED", "job.failed", retErr.Error(), map[string]any{"boardId": j.boardID})
		} else {
			j.log.Event(logging.Info, "import", "job", "JOB_SUCCEEDED", "job.succeeded", "Offline firmware package is verified and ready for installation.", map[string]any{"boardId": j.boardID, "durationMs": time.Since(started).Milliseconds()})
		}
		_ = j.log.WriteSummary(result)
		_ = j.log.Close()
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	info, err := os.Stat(j.packagePath)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return result, fmt.Errorf("offline firmware package is unavailable")
	}
	profile, err := catalog.Profile(j.boardID)
	if err != nil {
		return result, err
	}
	j.log.Event(logging.Info, "import", "firmware", "OFFLINE_SIGNATURE_VERIFY_STARTED", "offline.signature.verify.started", "Verifying selected firmware package against the embedded official trust store.", map[string]any{"boardId": j.boardID})
	verified, err := firmware.VerifyRelease(j.packagePath, j.trust)
	if err != nil {
		return result, fmt.Errorf("offline firmware signature verification: %w", err)
	}
	if err := catalog.ValidateManifestBinding(profile, verified.Manifest.Board.ID, verified.Manifest.Board.ProfileHash, verified.Manifest.Chip.Family, verified.Manifest.Chip.FlashBytes); err != nil {
		return result, fmt.Errorf("offline firmware catalog binding: %w", err)
	}
	if verified.Manifest.Channel != string(j.channel) {
		return result, fmt.Errorf("offline firmware channel mismatch: selected %s but signed package declares %s", j.channel, verified.Manifest.Channel)
	}
	plan, err := firmware.InstallPlanFor(verified.Manifest)
	if err != nil {
		return result, fmt.Errorf("offline firmware install plan: %w", err)
	}
	result = catalog.DownloadedRelease{JobID: j.jobID, BoardID: profile.ID, BoardName: profile.Name, Channel: string(j.channel), ReleaseTag: "offline", FirmwareVersion: verified.Manifest.AppIdentity.ReleaseSequence, AssetName: filepath.Base(j.packagePath), Path: j.packagePath, Size: info.Size(), SHA256: verified.ArchiveSHA256, InstallStatus: "verified_ready", InstallPlan: plan.Mode, PreservesUserData: plan.PreservesUserData, RequiresRecovery: plan.RequiresRecovery, SafetyNote: plan.Summary + " " + plan.Warning}
	j.log.Event(logging.Info, "import", "firmware", "OFFLINE_SIGNATURE_VERIFIED", "offline.signature.verified", "Offline firmware archive, signature, board target, and install impact were verified.", map[string]any{"boardId": j.boardID, "sha256": result.SHA256, "firmwareVersion": result.FirmwareVersion})
	return result, nil
}
