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
	"time"
)

var ErrToolMissing = errors.New("esptool sidecar not found")

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

func FindTool() (Tool, error) {
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
	if action == "chip_id" { verb = "chip-id" }
	if action == "flash_id" { verb = "flash-id" }
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

// WriteImages is deliberately narrow: the job planner supplies verified files and
// fixed offsets. This adapter never accepts user-provided argument strings.
func (t Tool) WriteImages(ctx context.Context, port string, baud int, images []WriteImage) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if baud != 115200 && baud != 460800 && baud != 921600 {
		return Result{}, fmt.Errorf("unsupported baud rate: %d", baud)
	}
	if len(images) == 0 || len(images) > 16 {
		return Result{}, errors.New("invalid image count")
	}
	args := []string{"--port", port, "--baud", strconv.Itoa(baud), "--after", "hard_reset", "write_flash", "--flash_mode", "keep", "--flash_freq", "keep", "--flash_size", "keep"}
	lastEnd := uint64(0)
	for i, img := range images {
		if img.Offset%0x1000 != 0 || img.Path == "" || img.Size <= 0 {
			return Result{}, fmt.Errorf("invalid image %d", i)
		}
		if uint64(img.Size) > ^uint64(0)-img.Offset {
			return Result{}, fmt.Errorf("image %d range overflows", i)
		}
		if i > 0 && img.Offset < lastEnd {
			return Result{}, errors.New("overlapping images")
		}
		if _, err := os.Stat(img.Path); err != nil {
			return Result{}, fmt.Errorf("image not accessible: %w", err)
		}
		args = append(args, fmt.Sprintf("0x%x", img.Offset), img.Path)
		lastEnd = img.Offset + uint64(img.Size)
	}
	return t.run(ctx, args)
}

// VerifyImages asks esptool to verify each already-written image. It is invoked
// before a write job can become successful; a process exit code is not enough.
func (t Tool) VerifyImages(ctx context.Context, port string, baud int, images []WriteImage) (Result, error) {
	if !validPort(port) {
		return Result{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	if baud != 115200 && baud != 460800 && baud != 921600 {
		return Result{}, fmt.Errorf("unsupported baud rate: %d", baud)
	}
	if len(images) == 0 || len(images) > 16 {
		return Result{}, errors.New("invalid image count")
	}
	args := []string{"--port", port, "--baud", strconv.Itoa(baud), "verify_flash"}
	lastEnd := uint64(0)
	for i, img := range images {
		if img.Offset%0x1000 != 0 || img.Path == "" || img.Size <= 0 {
			return Result{}, fmt.Errorf("invalid image %d", i)
		}
		if uint64(img.Size) > ^uint64(0)-img.Offset {
			return Result{}, fmt.Errorf("image %d range overflows", i)
		}
		if i > 0 && img.Offset < lastEnd {
			return Result{}, errors.New("overlapping images")
		}
		if _, err := os.Stat(img.Path); err != nil {
			return Result{}, fmt.Errorf("image not accessible: %w", err)
		}
		args = append(args, fmt.Sprintf("0x%x", img.Offset), img.Path)
		lastEnd = img.Offset + uint64(img.Size)
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
	return t.run(ctx, []string{"--port", port, "--baud", "115200", "read_flash", fmt.Sprintf("0x%x", offset), fmt.Sprintf("0x%x", length), output})
}

func (t Tool) run(ctx context.Context, args []string) (Result, error) {
	started := time.Now()
	cmd := exec.CommandContext(ctx, t.Path, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	result := Result{Command: append([]string{filepath.Base(t.Path)}, args...), Output: out.String(), Duration: time.Since(started)}
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
		if strings.Contains(lower, "detecting chip type") || strings.Contains(lower, "chip is") {
			ci.Chip = line
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
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "secure boot") {
			v := strings.Contains(lower, "enabled") && !strings.Contains(lower, "not enabled") && !strings.Contains(lower, "disabled")
			if strings.Contains(lower, "enabled") || strings.Contains(lower, "disabled") || strings.Contains(lower, "not enabled") {
				s.SecureBoot = &v
			}
		}
		if strings.Contains(lower, "flash encryption") {
			v := strings.Contains(lower, "enabled") && !strings.Contains(lower, "disabled")
			if strings.Contains(lower, "enabled") || strings.Contains(lower, "disabled") {
				s.FlashEncryption = &v
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
