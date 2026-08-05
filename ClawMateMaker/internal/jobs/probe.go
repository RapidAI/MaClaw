// Package jobs coordinates bounded, diagnosable device operations.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/flash"
	"clawmatemaker/internal/logging"
)

type ProbeJob struct {
	port, jobID, attemptID string
	log                    *logging.Writer
}

type ProbeResult struct {
	JobID            string              `json:"jobId"`
	AttemptID        string              `json:"attemptId"`
	Port             string              `json:"port"`
	Status           string              `json:"status"`
	Tool             string              `json:"tool,omitempty"`
	Chip             flash.ChipInfo      `json:"chip"`
	Flash            flash.FlashInfo     `json:"flash"`
	BoardRecognition catalog.Recognition `json:"boardRecognition"`
	Warnings         []string            `json:"warnings,omitempty"`
	ErrorCode        string              `json:"errorCode,omitempty"`
	ErrorMessage     string              `json:"errorMessage,omitempty"`
	StartedAt        time.Time           `json:"startedAt"`
	FinishedAt       time.Time           `json:"finishedAt"`
}

func NewProbeJob(logRoot, port string, emit func(logging.Event)) (*ProbeJob, error) {
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
	return &ProbeJob{port: port, jobID: jobID, attemptID: attemptID, log: w}, nil
}

func (j *ProbeJob) Run(ctx context.Context) (result ProbeResult, retErr error) {
	result = ProbeResult{JobID: j.jobID, AttemptID: j.attemptID, Port: j.port, Status: "running", StartedAt: time.Now().UTC()}
	j.log.Event(logging.Info, "probe", "job", "JOB_CREATED", "job.created", "", map[string]any{"port": j.port})
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if retErr != nil {
			result.Status = "failed"
			j.log.Event(logging.Error, "probe", "job", result.ErrorCode, "job.failed", result.ErrorMessage, nil)
		} else {
			result.Status = "succeeded"
			j.log.Event(logging.Info, "probe", "job", "JOB_SUCCEEDED", "job.succeeded", "", map[string]any{"durationMs": time.Since(result.StartedAt).Milliseconds()})
		}
		_ = j.log.WriteSummary(result)
		_ = j.log.Close()
	}()

	j.log.Event(logging.Info, "probe", "engine", "STAGE_STARTED", "stage.started", "Read-only ROM probing; no flash write or erase command is permitted.", map[string]any{"port": j.port})
	tool, err := flash.FindTool()
	if err != nil {
		return j.fail(&result, "TOOL_UNAVAILABLE", err)
	}
	result.Tool = "esptool sidecar"
	j.log.Event(logging.Info, "probe", "sidecar", "TOOL_FOUND", "tool.found", "", map[string]any{"tool": "esptool"})

	chipRun, err := tool.RunReadOnly(ctx, j.port, "chip_id")
	j.recordSidecar("chip_id", chipRun, err)
	if err != nil {
		return j.fail(&result, "BOOTLOADER_SYNC_FAILED", err)
	}
	result.Chip = flash.ParseChipID(chipRun.Output)
	if result.Chip.Chip == "" {
		result.Warnings = append(result.Warnings, "Chip text could not be fully parsed; a production write must reconfirm it using the formal ROM protocol.")
	}
	j.log.Event(logging.Info, "probe", "engine", "CHIP_OBSERVED", "chip.observed", "", map[string]any{"chip": result.Chip.Chip, "revision": result.Chip.Revision})

	flashRun, err := tool.RunReadOnly(ctx, j.port, "flash_id")
	j.recordSidecar("flash_id", flashRun, err)
	if err != nil {
		return j.fail(&result, "FLASH_PROBE_FAILED", err)
	}
	result.Flash = flash.ParseFlashID(flashRun.Output)
	j.log.Event(logging.Info, "probe", "engine", "FLASH_OBSERVED", "flash.observed", "", map[string]any{"flashBytes": result.Flash.SizeBytes})
	result.Warnings = append(result.Warnings, "Security eFuse / anti-rollback state is not decided by this development probe; production installation must fail closed.")
	j.log.Event(logging.Info, "probe", "engine", "STAGE_COMPLETED", "stage.completed", "", map[string]any{"durationMs": time.Since(result.StartedAt).Milliseconds()})
	return result, nil
}

func (j *ProbeJob) recordSidecar(action string, r flash.Result, err error) {
	severity := logging.Info
	code := "SIDECAR_COMPLETED"
	detail := r.Output
	if err != nil {
		severity = logging.Error
		code = "SIDECAR_FAILED"
		detail = fmt.Sprintf("%v\n%s", err, r.Output)
	}
	_ = j.log.AppendRaw("sidecar.log", fmt.Sprintf("[%s] %s\n", action, detail))
	j.log.Event(severity, "probe", "sidecar", code, "sidecar."+strings.ToLower(code), detail, map[string]any{"command": action, "exitCode": r.ExitCode, "durationMs": r.Duration.Milliseconds()})
}
func (j *ProbeJob) fail(r *ProbeResult, code string, err error) (ProbeResult, error) {
	r.ErrorCode = code
	r.ErrorMessage = err.Error()
	return *r, err
}
func id(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}
