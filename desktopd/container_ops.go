package desktopd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func (s *Service) runCommand(ctx context.Context, stdin io.Reader, args ...string) (string, int, error) {
	if s != nil && s.RunCommand != nil {
		return s.RunCommand(ctx, stdin, args...)
	}
	return defaultRunCommand(ctx, stdin, args...)
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
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, desktop.FileReadArgs(containerName(spec.TenantID, spec.UserID), filePath, desktop.FileReadMax)...)
		return runErr
	})
	if err != nil {
		return "", err
	}
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
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, desktop.FileBytesArgs(containerName(spec.TenantID, spec.UserID), filePath, desktop.FileBytesMax)...)
		return runErr
	})
	if err != nil {
		return "", err
	}
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
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, strings.NewReader(content), desktop.FileWriteArgs(containerName(spec.TenantID, spec.UserID), filePath)...)
		return runErr
	})
	if err != nil {
		return "", err
	}
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
		output, exitCode, runErr = s.runCommand(ctx, nil, desktop.FileReadArgs(container, filePath, desktop.FileContentMax)...)
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
		output, exitCode, runErr = s.runCommand(ctx, strings.NewReader(next), desktop.FileWriteArgs(container, filePath)...)
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
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, desktop.FileListArgs(containerName(spec.TenantID, spec.UserID), filePath)...)
		return runErr
	})
	if err != nil {
		return "", err
	}
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
	args, err := desktop.BashArgs(containerName(spec.TenantID, spec.UserID), command)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, desktop.BashBudget)
	defer cancel()
	var output string
	var exitCode int
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, args...)
		return runErr
	})
	if err != nil {
		if ctx.Err() != nil {
			return desktop.ProgramOutput(output, exitCode, true), nil
		}
		return "", err
	}
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
