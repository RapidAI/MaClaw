package desktopd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func (s *Service) runCommand(ctx context.Context, stdin io.Reader, args ...string) (string, int, error) {
	if s != nil && s.RunCommand != nil {
		return s.RunCommand(ctx, stdin, args...)
	}
	return defaultRunCommand(ctx, stdin, args...)
}

// fileServiceArgs asks the in-container file service to read, write, list,
// or return bytes. The host does not build head, ls, or base64. A write
// keeps docker exec -i so the body stays on stdin. limit is how many bytes
// a read may return; zero means this operation has no cap argument.
func fileServiceArgs(container, op, filePath string, limit int) []string {
	args := []string{"exec"}
	if op == "write" {
		args = append(args, "-i")
	}
	args = append(args, container, "python3", "/desktop_supervisor.py", "file", op, filePath)
	if limit > 0 {
		args = append(args, strconv.Itoa(limit))
	}
	return args
}

func defaultRunCommand(ctx context.Context, stdin io.Reader, args ...string) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		return text, 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return text, exitErr.ExitCode(), nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		trimmed = err.Error()
	}
	return trimmed, 0, fmt.Errorf("%w: %s", ErrDocker, trimmed)
}

// ReadFile returns the text of one file in the user's desktop container.
func (s *Service) ReadFile(ctx context.Context, tenantID, userID, filePath string) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(containerName(spec.TenantID, spec.UserID), "read", filePath, desktop.FileReadMax+1),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	if len(output) > desktop.FileReadMax {
		cut, _ := desktop.CapHead(output, desktop.FileReadMax)
		return cut, nil
	}
	if output == "" {
		return "(empty file)", nil
	}
	return output, nil
}

// ReadBytes returns one file as standard base64. The text read above is a Go
// string, and JSON would replace a PDF's bytes, so the container encodes the
// file before it leaves. A file past the chat limit is refused whole.
func (s *Service) ReadBytes(ctx context.Context, tenantID, userID, filePath string) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(containerName(spec.TenantID, spec.UserID), "bytes", filePath, desktop.FileBytesMax+1),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	raw, err := desktop.DecodeFileBytes(output)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// WriteFile writes content to one file in the user's desktop container.
func (s *Service) WriteFile(ctx context.Context, tenantID, userID, filePath, content string) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	if len(content) > desktop.FileContentMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	var exitCode int
	var output string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind:  ExecFile,
			Args:  fileServiceArgs(containerName(spec.TenantID, spec.UserID), "write", filePath, 0),
			Stdin: strings.NewReader(content),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	return fmt.Sprintf("Wrote %s (%d bytes).", filePath, len(content)), nil
}

// EditFile replaces one exact passage in a container file.
func (s *Service) EditFile(ctx context.Context, tenantID, userID, filePath, old, new string) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	if len(new) > desktop.FileContentMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	container := containerName(spec.TenantID, spec.UserID)
	var output string
	var exitCode int
	// Read and write share one user lock. Releasing it between them lets
	// another command from this user replace the file before the edit lands.
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(container, "read", filePath, desktop.FileContentMax+1),
		})
		output, exitCode = result.Out, result.Exit
		if runErr != nil {
			return runErr
		}
		if exitCode != 0 {
			return fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
		}
		if len(output) > desktop.FileContentMax {
			return fmt.Errorf("%w: file is too large to edit", ErrInvalid)
		}
		next, editErr := desktop.ApplyEdit(output, old, new)
		if editErr != nil {
			return editErr
		}
		result, runErr = s.runtime().Exec(ctx, spec, ExecRequest{
			Kind:  ExecFile,
			Args:  fileServiceArgs(container, "write", filePath, 0),
			Stdin: strings.NewReader(next),
		})
		output, exitCode = result.Out, result.Exit
		if runErr != nil {
			return runErr
		}
		if exitCode != 0 {
			return fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	return fmt.Sprintf("Updated %s.", filePath), nil
}

// ListFile lists one path in the user's desktop container.
func (s *Service) ListFile(ctx context.Context, tenantID, userID, filePath string) (string, error) {
	if strings.TrimSpace(filePath) == "" {
		filePath = desktop.ContainerHome
	}
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(containerName(spec.TenantID, spec.UserID), "list", filePath, 0),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	text := strings.TrimSpace(output)
	if text == "" {
		text = "(empty)"
	}
	return filePath + "\n" + text, nil
}

// Bash runs one command in the user's desktop container and returns its output.
func (s *Service) Bash(ctx context.Context, tenantID, userID, command string) (string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	if _, err = desktop.BashArgs(containerName(spec.TenantID, spec.UserID), command); err != nil {
		return "", err
	}
	// A bot's GUI programs belong on its own display. The command service
	// applies that display. The docker line does not name bash.
	display := ""
	if spec.BotID != "" {
		var displayErr error
		display, displayErr = s.specDisplay(ctx, spec, "")
		if displayErr != nil {
			return "", displayErr
		}
	}
	commandArgs, err := commandServiceArgs(containerName(spec.TenantID, spec.UserID), commandRequest{
		Op:      "shell",
		Command: strings.TrimSpace(command),
		Display: display,
	})
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, desktop.BashBudget)
	defer cancel()
	release := s.holdWork(spec.TenantID, spec.UserID)
	defer release()
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{Kind: ExecShell, Args: commandArgs})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		if ctx.Err() != nil {
			s.noteComputerActivity(spec.TenantID, spec.UserID)
			return desktop.ProgramOutput(output, exitCode, true), nil
		}
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	return desktop.ProgramOutput(output, exitCode, false), nil
}

// HTTPGet fetches one URL with curl inside the user's desktop container.
// The container's network and proxy are the ones curl uses.
func (s *Service) HTTPGet(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", 0, "", err
	}
	args, err := desktop.CurlGetArgs(containerName(spec.TenantID, spec.UserID), rawURL, desktop.HTTPBodyMax)
	if err != nil {
		return "", 0, "", err
	}
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, args...)
		return runErr
	})
	if err != nil {
		return "", 0, "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	body, status, kind := desktop.SplitCurl(output)
	if exitCode != 0 && status == 0 {
		return "", 0, "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	if len(body) > desktop.HTTPBodyMax {
		body, _ = desktop.CapHead(body, desktop.HTTPBodyMax)
	}
	return body, status, kind, nil
}

func commandFailure(output string, exitCode int) string {
	text := strings.TrimSpace(output)
	if text == "" {
		return fmt.Sprintf("exit %d", exitCode)
	}
	return desktop.CapTail(text, 4000)
}

// UploadFile streams one material file of size bytes into the user's desktop
// container through the supervisor's file write, which creates the folder.
// Unlike WriteFile it is binary and bounded by desktop.UploadMax.
func (s *Service) UploadFile(ctx context.Context, tenantID, userID, filePath string, body io.Reader, size int64) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(filePath, desktop.MaterialsRoot+"/") {
		return "", fmt.Errorf("%w: uploads go under %s", ErrInvalid, desktop.MaterialsRoot)
	}
	if size < 0 || size > desktop.UploadMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	counted := &countingReader{r: io.LimitReader(body, desktop.UploadMax+1)}
	var exitCode int
	var output string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind:  ExecFile,
			Args:  fileServiceArgs(containerName(spec.TenantID, spec.UserID), "write", filePath, 0),
			Stdin: counted,
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	if counted.n != size {
		return "", fmt.Errorf("%w: upload was cut short (%d of %d bytes)", ErrInvalid, counted.n, size)
	}
	// docker exec runs as root, so the write leaves root-owned folders and
	// files the desktop user (and its bots) cannot rename, replace or delete.
	// Hand ~/素材, every folder down to the file, and the file to that user.
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: materialChownArgs(containerName(spec.TenantID, spec.UserID), filePath),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	return fmt.Sprintf("Wrote %s (%d bytes).", filePath, counted.n), nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// MaterialOwner is the desktop user (uid 1000) that owns uploaded materials.
const MaterialOwner = "desktop:desktop"

// materialChownArgs gives desktop.MaterialsRoot, each folder below it on the
// way to filePath, and filePath itself to MaterialOwner. Nothing above
// MaterialsRoot is touched.
func materialChownArgs(container, filePath string) []string {
	paths := []string{desktop.MaterialsRoot}
	rest := strings.TrimPrefix(filePath, desktop.MaterialsRoot+"/")
	cur := desktop.MaterialsRoot
	for _, part := range strings.Split(rest, "/") {
		if part == "" {
			continue
		}
		cur += "/" + part
		paths = append(paths, cur)
	}
	return append([]string{"exec", container, "chown", MaterialOwner, "--"}, paths...)
}
