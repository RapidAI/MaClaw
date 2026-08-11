// Package jobs coordinates bounded, diagnosable device operations.
package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/flash"
	"clawmatemaker/internal/logging"
	"clawmatemaker/internal/partition"
)

type ProbeJob struct {
	port, jobID, attemptID string
	log                    *logging.Writer
}

type ProbeResult struct {
	JobID     string         `json:"jobId"`
	AttemptID string         `json:"attemptId"`
	Port      string         `json:"port"`
	Status    string         `json:"status"`
	Tool      string         `json:"tool,omitempty"`
	Chip      flash.ChipInfo `json:"chip"`
	// DeviceBinding is a one-way digest of the ROM MAC. It lets the desktop
	// bind the later write to this physical device without persisting or
	// exposing the raw hardware address.
	DeviceBinding    string                   `json:"deviceBinding,omitempty"`
	Flash            flash.FlashInfo          `json:"flash"`
	Security         flash.SecurityInfo       `json:"security"`
	Layout           *partition.Table         `json:"layout,omitempty"`
	AppDescription   *flash.ESPAppDescription `json:"appDescription,omitempty"`
	BoardRecognition catalog.Recognition      `json:"boardRecognition"`
	AppIdentity      device.AppIdentity       `json:"appIdentity,omitempty"`
	Warnings         []string                 `json:"warnings,omitempty"`
	ErrorCode        string                   `json:"errorCode,omitempty"`
	ErrorMessage     string                   `json:"errorMessage,omitempty"`
	StartedAt        time.Time                `json:"startedAt" ts_type:"string"`
	FinishedAt       time.Time                `json:"finishedAt" ts_type:"string"`
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
	if err := ctx.Err(); err != nil {
		return j.fail(&result, "PROBE_CANCELLED", err)
	}

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
	result.DeviceBinding = deviceBinding(result.Chip.MAC)
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
	securityRun, securityErr := tool.RunReadOnly(ctx, j.port, "get-security-info")
	j.recordSidecar("get-security-info", securityRun, securityErr)
	if securityErr != nil {
		result.Warnings = append(result.Warnings, "Security eFuse state could not be read; production installation will fail closed.")
		j.log.Event(logging.Warn, "probe", "security", "SECURITY_STATE_UNAVAILABLE", "security.unavailable", securityErr.Error(), nil)
	} else {
		result.Security = flash.ParseSecurityInfo(securityRun.Output)
		if securityBaseline(result.Security) {
			j.log.Event(logging.Info, "probe", "security", "SECURITY_BASELINE_OBSERVED", "security.baseline.observed", "Read-only eFuse probe found the supported security baseline.", nil)
		} else {
			result.Warnings = append(result.Warnings, "Security eFuse state is enabled or could not be safely determined; production installation will fail closed.")
			j.log.Event(logging.Warn, "probe", "security", "SECURITY_STATE_UNSUPPORTED", "security.unsupported", "Security eFuse state is enabled or could not be safely determined.", nil)
		}
	}
	if result.Flash.SizeBytes > 0 {
		if err := j.probeInstalledLayout(ctx, tool, &result); err != nil {
			result.Warnings = append(result.Warnings, "Installed partition/application metadata could not be read; a production write will re-check it.")
			j.log.Event(logging.Warn, "probe", "partition", "LAYOUT_UNAVAILABLE", "layout.unavailable", err.Error(), nil)
		}
	}
	// ROM probe commands reset the target. Give its application a bounded
	// opportunity to return before opening the application serial endpoint for
	// the nonce-bound identity query. This makes automatic matching reliable on
	// real USB Serial/JTAG hardware without changing the read-only contract.
	if err := waitForApplicationIdentity(ctx, 800*time.Millisecond); err != nil {
		return j.fail(&result, "PROBE_CANCELLED", err)
	}
	identity, identityErr := device.ProbeApplicationIdentity(ctx, j.port)
	if identityErr != nil {
		j.log.Event(logging.Warn, "probe", "identity", "IDENTITY_UNAVAILABLE", "identity.unavailable", identityErr.Error(), nil)
	} else {
		result.AppIdentity = identity
		result.BoardRecognition = catalog.RecognizeApplicationIdentityWithROM(identity, result.Chip, result.Flash)
		j.log.Event(logging.Info, "probe", "identity", "IDENTITY_OBSERVED", "identity.observed", "Received nonce-bound application identity.", map[string]any{"chip": identity.Chip, "flashBytes": identity.FlashBytes})
	}
	j.log.Event(logging.Info, "probe", "engine", "STAGE_COMPLETED", "stage.completed", "", map[string]any{"durationMs": time.Since(result.StartedAt).Milliseconds()})
	return result, nil
}

func waitForApplicationIdentity(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func deviceBinding(mac string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if mac == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(mac))
	return hex.EncodeToString(sum[:])
}

func securityBaseline(value flash.SecurityInfo) bool {
	return value.SecureBoot != nil && !*value.SecureBoot &&
		value.FlashEncryption != nil && !*value.FlashEncryption &&
		value.SecureVersion != nil && *value.SecureVersion == 0
}

// probeInstalledLayout reads only the fixed ESP-IDF partition-table location
// and the factory app descriptor. It is diagnostic evidence for matching an
// already running board; FlashJob repeats these checks before a write.
func (j *ProbeJob) probeInstalledLayout(ctx context.Context, tool flash.Tool, result *ProbeResult) error {
	if result == nil || result.Flash.SizeBytes <= 0 {
		return errors.New("flash capacity is unavailable")
	}
	dir, err := os.MkdirTemp("", "clawmate-probe-")
	if err != nil {
		return fmt.Errorf("create probe workspace: %w", err)
	}
	defer os.RemoveAll(dir)
	partitionPath := filepath.Join(dir, "partition-table.bin")
	run, err := tool.ReadFlash(ctx, j.port, 0x8000, 4096, partitionPath)
	j.recordSidecar("read_flash_partition_table", run, err)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(partitionPath)
	if err != nil {
		return fmt.Errorf("read partition table: %w", err)
	}
	table, err := partition.Parse(raw, uint64(result.Flash.SizeBytes))
	if err != nil {
		return err
	}
	result.Layout = &table
	j.log.Event(logging.Info, "probe", "partition", "LAYOUT_OBSERVED", "layout.observed", "Read and validated the installed ESP-IDF partition table.", map[string]any{"bytes": len(table.Entries)})
	factory, ok := partition.Find(table.Entries, "factory")
	if !ok || factory.Size < 4096 {
		return errors.New("installed layout has no readable factory application partition")
	}
	descriptorPath := filepath.Join(dir, "app-descriptor.bin")
	run, err = tool.ReadFlash(ctx, j.port, uint64(factory.Offset), 4096, descriptorPath)
	j.recordSidecar("read_flash_app_descriptor", run, err)
	if err != nil {
		return err
	}
	raw, err = os.ReadFile(descriptorPath)
	if err != nil {
		return fmt.Errorf("read app descriptor: %w", err)
	}
	description, err := flash.ParseESPAppDescription(raw)
	if err != nil {
		return err
	}
	result.AppDescription = &description
	j.log.Event(logging.Info, "probe", "firmware", "APP_DESCRIPTOR_OBSERVED", "app_descriptor.observed", "Read the installed ESP-IDF application descriptor.", map[string]any{"project": description.ProjectName, "version": description.Version})
	return nil
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
