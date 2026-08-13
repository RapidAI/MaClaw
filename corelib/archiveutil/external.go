package archiveutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const externalArchiveTimeout = 10 * time.Minute
const externalDiagnosticLimit = 64 << 10

type externalToolProfile struct {
	Program string
	Version string
}

// ExternalProgram returns the first available supported external executable
// for a format. The result is a concrete executable path, not shell text.
func ExternalProgram(format Format) (string, error) {
	for _, name := range externalProgramNames(format) {
		if path, err := exec.LookPath(name); err == nil {
			if _, checkErr := inspectExternalProgram(path, format); checkErr == nil {
				return path, nil
			}
		}
	}
	for _, name := range externalProgramNames(format) {
		if _, err := exec.LookPath(name); err == nil {
			return "", errorf(CodeExternalToolUnusable, "found external program for %s but it failed capability self-check", format)
		}
	}
	return "", errorf(CodeExternalToolNotFound, "no supported external program found for %s", format)
}

func externalProgramNames(format Format) []string {
	switch format {
	case Format7Z:
		return []string{"7z", "7zz", "7za"}
	case FormatXZ:
		return []string{"7z", "7zz", "7za", "xz"}
	case FormatZSTD:
		return []string{"7z", "7zz", "7za", "zstd"}
	case FormatRAR:
		return []string{"7z", "7zz", "7za", "unrar"}
	default:
		return nil
	}
}

// ExtractExternal runs a controlled adapter for formats not handled by the
// embedded reader. It intentionally uses exec argument arrays instead of a
// shell string. Hosts must set AllowExternal only after approval.
func ExtractExternal(req Request) Result {
	if req.ArchivePath == "" {
		return failure(ActionExtractExternal, FormatUnknown, errorf(CodeInvalidArgument, "archive_path is required"))
	}
	if !req.AllowExternal {
		return failure(ActionExtractExternal, FormatUnknown, errorf(CodeExternalApprovalRequired, "external archive execution requires explicit approval"))
	}
	limits := req.Limits.normalized()
	before, err := captureSourceState(req.ArchivePath)
	if err != nil {
		return failure(ActionExtractExternal, FormatUnknown, errorf(CodeIO, "stat archive: %v", err))
	}
	if before.size > limits.MaxInputBytes {
		return failure(ActionExtractExternal, FormatUnknown, errorf(CodeLimitExceeded, "archive input exceeds size limit"))
	}
	format, err := Detect(req.ArchivePath)
	if err != nil {
		return failure(ActionExtractExternal, FormatUnknown, err)
	}
	if !externalFormat(format) {
		return failure(ActionExtractExternal, format, errorf(CodeFormatUnsupported, "external extraction is not configured for %s", format))
	}
	destination := req.Destination
	if destination == "" {
		destination = DefaultDestination(req.ArchivePath)
	}
	stage, err := PrepareExternalStage(destination)
	if err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	defer stage.Cleanup()
	program, err := ExternalProgram(format)
	if err != nil {
		result := failure(ActionExtractExternal, format, err)
		result.InputPath = req.ArchivePath
		result.Fallback = &Fallback{RecommendedPrograms: externalProgramNames(format), CraftToolAllowed: true, UserActionRequired: true}
		return result
	}
	profile, err := inspectExternalProgram(program, format)
	if err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	if err := ensureExternalDiskSpace(stage.Path, limits); err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	if err := runExternalExtractor(program, req.ArchivePath, stage.Path, format); err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	if err := ensureSourceUnchanged(req.ArchivePath, before); err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	stage, files, dirs, written, err := stage.Validate(limits)
	if err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	if files == 0 && dirs == 0 {
		return failure(ActionExtractExternal, format, errorf(CodeExternalExecutionFailed, "external program produced no output"))
	}
	if err := stage.Publish(); err != nil {
		return failure(ActionExtractExternal, format, err)
	}
	return Result{OK: true, Action: ActionExtractExternal, Format: format, InputPath: req.ArchivePath, OutputPath: destination, Files: files, Directories: dirs, WrittenBytes: written, Warnings: []string{fmt.Sprintf("extracted with external program: %s (%s)", filepath.Base(profile.Program), profile.Version)}}
}

func inspectExternalProgram(program string, format Format) (externalToolProfile, error) {
	base := strings.ToLower(filepath.Base(program))
	if !is7ZipProgram(base) && !supportsNativeExternalFormat(base, format) {
		return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program %q cannot handle %s", filepath.Base(program), format)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.Command(program, "--version")
	coretool.PrepareCommandForTreeKill(cmd)
	if startErr := cmd.Start(); startErr != nil {
		return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check start failed: %v", startErr)
	}
	output, err := waitExternalCombinedOutput(ctx, cmd)
	if ctx.Err() == context.DeadlineExceeded {
		return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check timed out: %s", filepath.Base(program))
	}
	if err != nil {
		// A small number of 7-Zip builds only support the short form. Keep the
		// probe fixed and side-effect free; never invoke an arbitrary help string.
		if base == "unrar" || base == "unrar.exe" {
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd = exec.Command(program)
			coretool.PrepareCommandForTreeKill(cmd)
			if startErr := cmd.Start(); startErr != nil {
				return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check start failed: %v", startErr)
			}
			output, err = waitExternalCombinedOutput(ctx, cmd)
			if ctx.Err() == context.DeadlineExceeded || len(strings.TrimSpace(string(output))) == 0 {
				return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check failed: %s", truncateExternalDiagnostic(string(output), err))
			}
		} else if !is7ZipProgram(base) {
			return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check failed: %s", truncateExternalDiagnostic(string(output), err))
		} else {
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd = exec.Command(program, "-h")
			coretool.PrepareCommandForTreeKill(cmd)
			if startErr := cmd.Start(); startErr != nil {
				return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check start failed: %v", startErr)
			}
			output, err = waitExternalCombinedOutput(ctx, cmd)
			if ctx.Err() == context.DeadlineExceeded || err != nil {
				return externalToolProfile{}, errorf(CodeExternalToolUnusable, "external program self-check failed: %s", truncateExternalDiagnostic(string(output), err))
			}
		}
	}
	version := firstDiagnosticLine(string(output))
	if version == "" {
		version = "version unavailable"
	}
	return externalToolProfile{Program: program, Version: version}, nil
}

func waitExternalCombinedOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	output := &boundedDiagnostic{}
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.Stdin = strings.NewReader("")
	err := coretool.WaitCommandWithContext(ctx, cmd)
	return []byte(output.String()), err
}

// boundedDiagnostic retains enough process output for a useful error without
// letting a broken or malicious external archiver consume unbounded memory.
type boundedDiagnostic struct {
	mu        sync.Mutex
	b         strings.Builder
	truncated bool
}

func (d *boundedDiagnostic) Write(data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	remaining := externalDiagnosticLimit - d.b.Len()
	if remaining > 0 {
		if len(data) > remaining {
			d.b.Write(data[:remaining])
			d.truncated = true
		} else {
			d.b.Write(data)
		}
	} else {
		d.truncated = true
	}
	return len(data), nil
}

func (d *boundedDiagnostic) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.truncated {
		return d.b.String() + "\n[external diagnostic truncated]"
	}
	return d.b.String()
}

func supportsNativeExternalFormat(base string, format Format) bool {
	switch format {
	case FormatXZ:
		return base == "xz" || base == "xz.exe"
	case FormatZSTD:
		return base == "zstd" || base == "zstd.exe"
	case FormatRAR:
		return base == "unrar" || base == "unrar.exe"
	default:
		return false
	}
}

func firstDiagnosticLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 200 {
				return line[:200]
			}
			return line
		}
	}
	return ""
}

func ensureExternalDiskSpace(stagePath string, limits Limits) error {
	available, err := availableBytes(stagePath)
	if err != nil {
		return errorf(CodeExternalToolUnusable, "cannot check free disk space for external extraction: %v", err)
	}
	// The output limit is a safety ceiling, so reserve enough space for the
	// largest permitted extraction plus a modest filesystem overhead.
	reserve := limits.MaxTotalBytes + minInt64(64<<20, limits.MaxTotalBytes/8)
	if available < reserve {
		return errorf(CodeExternalToolUnusable, "insufficient free disk space for controlled external extraction")
	}
	return nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func externalFormat(format Format) bool {
	return format == Format7Z || format == FormatXZ || format == FormatZSTD || format == FormatRAR
}

func runExternalExtractor(program, archivePath, stagePath string, format Format) error {
	base := strings.ToLower(filepath.Base(program))
	if is7ZipProgram(base) {
		return runExternalCommand(program, []string{"x", "-y", "-o" + stagePath, archivePath})
	}
	switch format {
	case FormatXZ:
		return runExternalSingleStream(program, []string{"-d", "-k", "-c", archivePath}, stagePath, stripArchiveSuffix(archivePath, ".xz"))
	case FormatZSTD:
		return runExternalSingleStream(program, []string{"-d", "-q", "-c", archivePath}, stagePath, stripArchiveSuffix(archivePath, ".zst"))
	case FormatRAR:
		if base == "unrar" || base == "unrar.exe" {
			return runExternalCommand(program, []string{"x", "-o+", archivePath, stagePath + string(os.PathSeparator)})
		}
	}
	return errorf(CodeFormatUnsupported, "unsupported external program %q for %s", filepath.Base(program), format)
}

func is7ZipProgram(base string) bool {
	return base == "7z" || base == "7z.exe" || base == "7zz" || base == "7zz.exe" || base == "7za" || base == "7za.exe"
}

func stripArchiveSuffix(path, suffix string) string {
	name := filepath.Base(path)
	if strings.HasSuffix(strings.ToLower(name), suffix) {
		name = name[:len(name)-len(suffix)]
	}
	if name == "" {
		return "output"
	}
	return name
}

func runExternalSingleStream(program string, args []string, stagePath, outputName string) error {
	outputPath, err := safeJoin(stagePath, outputName)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errorf(CodeIO, "create external output: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), externalArchiveTimeout)
	defer cancel()
	cmd := exec.Command(program, args...)
	cmd.Stdout = out
	stderr := &boundedDiagnostic{}
	cmd.Stderr = stderr
	cmd.Stdin = strings.NewReader("")
	coretool.PrepareCommandForTreeKill(cmd)
	if startErr := cmd.Start(); startErr != nil {
		_ = out.Close()
		return errorf(CodeExternalExecutionFailed, "external extractor start failed: %v", startErr)
	}
	err = coretool.WaitCommandWithContext(ctx, cmd)
	closeErr := out.Close()
	if ctx.Err() == context.DeadlineExceeded {
		return errorf(CodeExternalExecutionFailed, "external extractor timed out")
	}
	if err != nil {
		return errorf(CodeExternalExecutionFailed, "external extractor failed: %s", truncateExternalDiagnostic(stderr.String(), err))
	}
	if closeErr != nil {
		return errorf(CodeIO, "close external output: %v", closeErr)
	}
	return nil
}

func runExternalCommand(program string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), externalArchiveTimeout)
	defer cancel()
	return runExternalCommandWithContext(ctx, program, args)
}

func runExternalCommandWithContext(ctx context.Context, program string, args []string) error {
	cmd := exec.Command(program, args...)
	stderr := &boundedDiagnostic{}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	cmd.Stdin = strings.NewReader("")
	coretool.PrepareCommandForTreeKill(cmd)
	if startErr := cmd.Start(); startErr != nil {
		return errorf(CodeExternalExecutionFailed, "external extractor start failed: %v", startErr)
	}
	err := coretool.WaitCommandWithContext(ctx, cmd)
	if ctx.Err() == context.DeadlineExceeded {
		return errorf(CodeExternalExecutionFailed, "external extractor timed out")
	}
	if err != nil {
		return errorf(CodeExternalExecutionFailed, "external extractor failed: %s", truncateExternalDiagnostic(stderr.String(), err))
	}
	return nil
}

func truncateExternalDiagnostic(stderr string, err error) string {
	text := strings.TrimSpace(stderr)
	if text == "" {
		text = err.Error()
	}
	if len(text) > 1000 {
		text = text[:1000] + "…"
	}
	return text
}
