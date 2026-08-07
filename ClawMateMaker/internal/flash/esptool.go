// Package flash owns the only external-tool boundary. It is deliberately read-only in this phase.
package flash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrToolMissing = errors.New("esptool sidecar not found")

// SupportedWriteBauds are ordered from the normal high-speed path down to
// the recovery fallback. Jobs may retry at a lower value only when no write
// was accepted by the bootloader, or when a later verification/boot phase has
// already marked the device as requiring explicit ROM recovery.
var SupportedWriteBauds = []int{921600, 460800, 115200}

type Tool struct {
	Path    string
	Version string
}
type Result struct {
	Command  []string
	Output   string
	ExitCode int
	Duration time.Duration
}

// WriteProgress is derived from esptool's live transfer telemetry.  The
// adapter intentionally reports bytes from the signed images currently being
// transferred, rather than inventing time-based percentages in the UI.
type WriteProgress struct {
	TransferredBytes int64
	TotalBytes       int64
}

func (p WriteProgress) Percent() float64 {
	if p.TotalBytes <= 0 {
		return 0
	}
	return float64(p.TransferredBytes) * 100 / float64(p.TotalBytes)
}

func FindTool() (Tool, error) {
	config := currentSidecarConfig()
	if config.production {
		return managedToolForConfig(config)
	}
	if env := os.Getenv("CLAWMATE_ESPTOOL"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return Tool{Path: env}, nil
		}
		return Tool{}, fmt.Errorf("%w: CLAWMATE_ESPTOOL path is unavailable", ErrToolMissing)
	}
	names := []string{"esptool", "esptool.exe"}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return Tool{Path: p}, nil
		}
	}
	if os.Getenv("ProgramFiles") != "" {
		for _, p := range []string{`C:\Espressif\tools\python\v6.0.2\venv\Scripts\esptool.exe`, filepath.Join(os.Getenv("USERPROFILE"), `.espressif\python_env\idf6.0_py3.14_env\Scripts\esptool.exe`)} {
			if _, err := os.Stat(p); err == nil {
				return Tool{Path: p}, nil
			}
		}
	}
	return Tool{}, ErrToolMissing
}

func (t Tool) RunReadOnly(ctx context.Context, port string, action string) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if action != "chip_id" && action != "flash_id" && action != "get-security-info" {
		return Result{}, fmt.Errorf("unsupported read-only action: %s", action)
	}
	// esptool 5 renamed these verbs. Keep the application-level operation names
	// stable so an older signed sidecar can still be used while new sidecars do
	// not emit deprecation noise into the diagnostic stream.
	verb := action
	if action == "chip_id" {
		verb = "chip-id"
	}
	if action == "flash_id" {
		verb = "flash-id"
	}
	args := []string{"--port", port, "--baud", "115200", verb}
	return t.run(ctx, args)
}

type WriteImage struct {
	Offset uint64
	Path   string
	SHA256 string
	Size   int64
	Region string
}

func validWriteBaud(baud int) bool {
	for _, supported := range SupportedWriteBauds {
		if baud == supported {
			return true
		}
	}
	return false
}

// CanRetryWriteAtLowerBaud is deliberately conservative. An esptool process
// error does not prove that Flash stayed unchanged, so a job may fall back
// only when diagnostics demonstrate that it never established a ROM session.
// All other errors must be treated as potentially partial writes and require
// the explicit recovery path.
func CanRetryWriteAtLowerBaud(result Result, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error() + "\n" + result.Output)
	for _, marker := range []string{
		"failed to connect",
		"no serial data received",
		"could not open port",
		"cannot configure port",
		"serial port is unavailable",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// WriteImages is deliberately narrow: the job planner supplies verified files and
// fixed offsets. This adapter never accepts user-provided argument strings.
func (t Tool) WriteImages(ctx context.Context, port string, baud int, images []WriteImage) (Result, error) {
	return t.writeImages(ctx, port, baud, images, nil)
}

// WriteImagesWithProgress is the controlled-write variant used by FlashJob.
// The callback sees esptool's live percentage converted against the immutable
// signed image lengths.  It is deliberately not exposed to the frontend.
func (t Tool) WriteImagesWithProgress(ctx context.Context, port string, baud int, images []WriteImage, progress func(WriteProgress)) (Result, error) {
	return t.writeImages(ctx, port, baud, images, progress)
}

func (t Tool) writeImages(ctx context.Context, port string, baud int, images []WriteImage, progress func(WriteProgress)) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if !validWriteBaud(baud) {
		return Result{}, fmt.Errorf("unsupported baud rate: %d", baud)
	}
	if len(images) == 0 || len(images) > 16 {
		return Result{}, errors.New("invalid image count")
	}
	// Keep the target in the ROM bootloader after writing.  verify_flash is a
	// second ROM operation and must run before the App is allowed to boot;
	// otherwise a successful write can be followed by a misleading verify
	// failure simply because the device has already left download mode.
	args := []string{"--port", port, "--baud", strconv.Itoa(baud), "--after", "no_reset", "write_flash", "--flash_mode", "keep", "--flash_freq", "keep", "--flash_size", "keep"}
	for i, img := range images {
		if img.Offset%0x1000 != 0 || img.Path == "" || img.Size <= 0 {
			return Result{}, fmt.Errorf("invalid image %d", i)
		}
		if uint64(img.Size) > ^uint64(0)-img.Offset {
			return Result{}, fmt.Errorf("image %d range overflows", i)
		}
		if _, err := os.Stat(img.Path); err != nil {
			return Result{}, fmt.Errorf("image not accessible: %w", err)
		}
		args = append(args, fmt.Sprintf("0x%x", img.Offset), img.Path)
	}
	var total int64
	for _, image := range images {
		total += image.Size
	}
	if progress != nil {
		progress(WriteProgress{TotalBytes: total})
	}
	var last int64
	onLine := func(line string) {
		percent, ok := parseWritePercent(line)
		if !ok || total <= 0 {
			return
		}
		transferred := int64(float64(total) * percent / 100)
		if transferred < last {
			// A multi-image esptool invocation may render a new line from a
			// lower offset. Never report a backwards progress jump as fact.
			return
		}
		if transferred > total {
			transferred = total
		}
		last = transferred
		progress(WriteProgress{TransferredBytes: transferred, TotalBytes: total})
	}
	result, err := t.runStreaming(ctx, args, onLine)
	if err == nil && progress != nil && last < total {
		progress(WriteProgress{TransferredBytes: total, TotalBytes: total})
	}
	return result, err
}

// VerifyImages asks esptool to verify each already-written image. It is invoked
// before a write job can become successful; a process exit code is not enough.
func (t Tool) VerifyImages(ctx context.Context, port string, baud int, images []WriteImage) (Result, error) {
	return t.verifyImages(ctx, port, baud, images, "hard_reset")
}

// VerifyImagesNoReset keeps the target in ROM download mode after a per-image
// readback check. FlashJob uses it between independently committed images so
// the signed write order is real device I/O order, not merely a manifest hint.
func (t Tool) VerifyImagesNoReset(ctx context.Context, port string, baud int, images []WriteImage) (Result, error) {
	return t.verifyImages(ctx, port, baud, images, "no_reset")
}

func (t Tool) verifyImages(ctx context.Context, port string, baud int, images []WriteImage, after string) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if !validWriteBaud(baud) {
		return Result{}, fmt.Errorf("unsupported baud rate: %d", baud)
	}
	if len(images) == 0 || len(images) > 16 {
		return Result{}, errors.New("invalid image count")
	}
	// This is the single intentional transition back to application mode: the
	// range hash has succeeded while the ROM session is still active, then
	// esptool performs a hard reset so the job can bind the re-enumerated
	// application serial endpoint and obtain nonce-bound BOOT_STATUS proof.
	if after != "no_reset" && after != "hard_reset" {
		return Result{}, errors.New("invalid verification reset strategy")
	}
	args := []string{"--port", port, "--baud", strconv.Itoa(baud), "--after", after, "verify_flash"}
	for i, img := range images {
		if img.Offset%0x1000 != 0 || img.Path == "" || img.Size <= 0 {
			return Result{}, fmt.Errorf("invalid image %d", i)
		}
		if uint64(img.Size) > ^uint64(0)-img.Offset {
			return Result{}, fmt.Errorf("image %d range overflows", i)
		}
		if _, err := os.Stat(img.Path); err != nil {
			return Result{}, fmt.Errorf("image not accessible: %w", err)
		}
		args = append(args, fmt.Sprintf("0x%x", img.Offset), img.Path)
	}
	return t.run(ctx, args)
}

// ReadFlash reads a planner-bounded region into a caller-owned file. The public
// UI never gets an offset/length-to-file primitive; this is used only for the
// partition table and app descriptor checks.
func (t Tool) ReadFlash(ctx context.Context, port string, offset uint64, length uint32, output string) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if output == "" || offset%0x1000 != 0 || length == 0 || length > 64*1024 {
		return Result{}, errors.New("invalid bounded flash read")
	}
	if _, err := os.Stat(filepath.Dir(output)); err != nil {
		return Result{}, fmt.Errorf("read destination directory: %w", err)
	}
	// esptool 5 uses hyphenated verbs. Keep the adapter's Go method name
	// stable while avoiding deprecated-command output in user diagnostics.
	return t.run(ctx, []string{"--port", port, "--baud", "115200", "read-flash", fmt.Sprintf("0x%x", offset), fmt.Sprintf("0x%x", length), output})
}

func (t Tool) run(ctx context.Context, args []string) (Result, error) {
	return t.runStreaming(ctx, args, nil)
}

func (t Tool) runStreaming(ctx context.Context, args []string, onLine func(string)) (Result, error) {
	started := time.Now()
	command := append([]string{filepath.Base(t.Path)}, args...)
	out := newOutputCapture(onLine)
	cmd := exec.Command(t.Path, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := prepareProcessTree(cmd); err != nil {
		return Result{Command: command, Duration: time.Since(started)}, fmt.Errorf("prepare esptool process tree: %w", err)
	}
	err := runWithCancellation(ctx, cmd)
	out.flush()
	result := Result{Command: command, Output: out.String(), Duration: time.Since(started)}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	if err != nil {
		return result, fmt.Errorf("esptool %s: %w", action(args), err)
	}
	return result, nil
}

var writePercentPattern = regexp.MustCompile(`(?i)writing\s+at\s+0x[0-9a-f]+.*?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`)

func parseWritePercent(line string) (float64, bool) {
	match := writePercentPattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value < 0 || value > 100 {
		return 0, false
	}
	return value, true
}

// outputCapture retains the complete diagnostic output while also splitting
// carriage-return based progress redraws into safe live lines. os/exec may
// write stdout and stderr concurrently, so both buffers are protected.
type outputCapture struct {
	mu      sync.Mutex
	out     bytes.Buffer
	pending string
	onLine  func(string)
}

func newOutputCapture(onLine func(string)) *outputCapture { return &outputCapture{onLine: onLine} }

func (c *outputCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	_, _ = c.out.Write(p)
	c.pending += string(p)
	parts := strings.FieldsFunc(c.pending, func(r rune) bool { return r == '\n' || r == '\r' })
	endsLine := strings.HasSuffix(c.pending, "\n") || strings.HasSuffix(c.pending, "\r")
	lines := append([]string(nil), parts...)
	if !endsLine && len(lines) != 0 {
		c.pending = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	} else {
		c.pending = ""
	}
	onLine := c.onLine
	c.mu.Unlock()
	if onLine != nil {
		for _, line := range lines {
			if line != "" {
				onLine(line)
			}
		}
	}
	return len(p), nil
}

func (c *outputCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.String()
}

func (c *outputCapture) flush() {
	c.mu.Lock()
	line, onLine := c.pending, c.onLine
	c.pending = ""
	c.mu.Unlock()
	if line != "" && onLine != nil {
		onLine(line)
	}
}

// runWithCancellation owns the sidecar lifetime rather than relying on
// exec.CommandContext's parent-only kill. A sidecar can start Python helpers
// which retain the serial handle after its parent exits, so cancellation must
// terminate the whole dedicated process tree before returning to the job.
func runWithCancellation(ctx context.Context, cmd *exec.Cmd) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		return err
	case <-ctx.Done():
		killErr := terminateProcessTree(cmd)
		waitErr := <-waited
		if killErr != nil {
			return fmt.Errorf("cancelled (%w); terminate sidecar process tree: %v", ctx.Err(), killErr)
		}
		if waitErr != nil {
			return fmt.Errorf("cancelled (%w): %v", ctx.Err(), waitErr)
		}
		return ctx.Err()
	}
}
func action(args []string) string {
	if len(args) > 0 {
		return args[len(args)-1]
	}
	return "command"
}

var portPattern = regexp.MustCompile(`^(?i)(COM[1-9][0-9]*|/dev/(tty|cu\.)[A-Za-z0-9._-]+)$`)

func validPort(port string) bool { return portPattern.MatchString(port) }

type ChipInfo struct {
	Chip     string   `json:"chip"`
	Revision string   `json:"revision"`
	Features []string `json:"features"`
	MAC      string   `json:"-"`
}

func ParseChipID(output string) ChipInfo {
	ci := ChipInfo{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "chip type:") {
			ci.Chip = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		} else if strings.Contains(lower, "detecting chip type") || strings.Contains(lower, "chip is") {
			if ci.Chip == "" {
				ci.Chip = line
			}
		}
		if strings.Contains(lower, "revision") {
			ci.Revision = line
		}
		if strings.Contains(lower, "features:") {
			ci.Features = append(ci.Features, line)
		}
		if strings.Contains(lower, "mac:") {
			ci.MAC = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}
	return ci
}

type FlashInfo struct {
	SizeBytes int64  `json:"sizeBytes"`
	Vendor    string `json:"vendor"`
	Device    string `json:"device"`
}

type SecurityInfo struct {
	SecureBoot      *bool  `json:"secureBoot"`
	FlashEncryption *bool  `json:"flashEncryption"`
	SecureVersion   *int   `json:"secureVersion"`
	Raw             string `json:"-"`
}

// ParseSecurityInfo intentionally yields unknown rather than guessing when an
// esptool version changes its wording. Callers must fail closed on nil fields.
func ParseSecurityInfo(output string) SecurityInfo {
	s := SecurityInfo{Raw: output}
	// esptool v5's ESP32-S3 `get-security-info` reports a combined eFuse
	// flags word rather than a separate SECURE_VERSION line. A zero word is a
	// provable baseline; a non-zero word remains unknown here and is rejected by
	// callers unless a future parser can safely decode it for that chip family.
	zeroFlags := regexp.MustCompile(`(?im)^\s*flags:\s*0x0+\b`)
	if zeroFlags.MatchString(output) {
		v := 0
		s.SecureVersion = &v
	}
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "secure boot") {
			if known, enabled := securitySwitchState(lower); known {
				s.SecureBoot = &enabled
			}
		}
		if strings.Contains(lower, "flash encryption") {
			if known, enabled := securitySwitchState(lower); known {
				s.FlashEncryption = &enabled
			}
		}
		if strings.Contains(lower, "secure version") || strings.Contains(lower, "anti-rollback") {
			re := regexp.MustCompile(`\b([0-9]+)\b`)
			if m := re.FindStringSubmatch(lower); len(m) == 2 {
				n, err := strconv.Atoi(m[1])
				if err == nil {
					s.SecureVersion = &n
				}
			}
		}
	}
	return s
}

// securitySwitchState parses only unambiguous affirmative or negative
// wording.  In particular, "not enabled" must not be classified as enabled
// merely because it contains the word "enabled".  Unknown wording is kept
// nil so the pre-write gate remains fail-closed.
func securitySwitchState(line string) (known bool, enabled bool) {
	line = strings.ToLower(strings.TrimSpace(line))
	for _, negative := range []string{"not enabled", "disabled", "not set", "false", "no"} {
		if strings.Contains(line, negative) {
			return true, false
		}
	}
	for _, positive := range []string{"enabled", "active", "set", "true", "yes"} {
		if strings.Contains(line, positive) {
			return true, true
		}
	}
	return false, false
}

func ParseFlashID(output string) FlashInfo {
	fi := FlashInfo{}
	re := regexp.MustCompile(`(?i)detected flash size:\s*([0-9]+)MB`)
	if m := re.FindStringSubmatch(output); len(m) == 2 {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		fi.SizeBytes = n * 1024 * 1024
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), "manufacturer:") {
			fi.Vendor = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
		if strings.Contains(strings.ToLower(line), "device:") {
			fi.Device = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}
	return fi
}
