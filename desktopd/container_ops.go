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
	"time"

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

// materialPublishTimeout is how long mv and chown may take after the bytes
// are in the partial. Those calls ignore the caller's cancel: skipping them
// leaves the finished file owned by root. A stuck docker exec still has to
// return the per-user lock.
const materialPublishTimeout = 60 * time.Second

// UploadFile streams one material file of size bytes into the user's desktop
// container through the supervisor's file write, which creates the folder.
// Unlike WriteFile it is binary and bounded by desktop.UploadMax.
//
// The running image's file write truncates the path it is given before the
// body arrives, and it has no rename operation. A fifth supervisor argument
// is rejected by that image, so this writes a sibling .partial and then
// mv's it onto filePath. A short body deletes the partial and leaves the
// previous file in place. mv, rm, and chown stay inside the write's withUser
// callback: that lock is not re-entrant. Once the byte count matches, mv and
// chown finish even if the caller has canceled.
func (s *Service) UploadFile(ctx context.Context, tenantID, userID, filePath string, body io.Reader, size int64) (string, error) {
	filePath, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(filePath, desktop.MaterialsRoot+"/") {
		return "", fmt.Errorf("%w: uploads go under %s", ErrInvalid, desktop.MaterialsRoot)
	}
	partial, err := desktop.ContainerPath(filePath + ".partial")
	if err != nil || !strings.HasPrefix(partial, desktop.MaterialsRoot+"/") {
		return "", fmt.Errorf("%w: path is too long", ErrInvalid)
	}
	if size < 0 || size > desktop.UploadMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	counted := &countingReader{r: io.LimitReader(body, desktop.UploadMax+1)}
	container := containerName(spec.TenantID, spec.UserID)
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		// A failed write still creates ~/素材 as root. Give those folders
		// back even when the file itself is discarded.
		abandon := func(cause error) error {
			s.removeMaterialPartial(ctx, spec, container, partial)
			s.chownMaterialDirs(ctx, spec, container, filePath)
			return cause
		}
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind:  ExecFile,
			Args:  fileServiceArgs(container, "write", partial, 0),
			Stdin: counted,
		})
		if runErr != nil {
			return abandon(runErr)
		}
		if result.Exit != 0 {
			return abandon(fmt.Errorf("%w: %s", ErrDocker, commandFailure(result.Out, result.Exit)))
		}
		if counted.n != size {
			return abandon(fmt.Errorf("%w: upload was cut short (%d of %d bytes)", ErrInvalid, counted.n, size))
		}
		// The bytes are durable in the partial. Publish them even when the
		// HTTP client has already gone: a canceled mv or chown releases this
		// lock while the file is still root-owned.
		parent := ctx
		if parent == nil {
			parent = context.Background()
		}
		publishCtx, publishCancel := context.WithTimeout(context.WithoutCancel(parent), materialPublishTimeout)
		defer publishCancel()
		moved, moveErr := s.runtime().Exec(publishCtx, spec, ExecRequest{
			Kind: ExecFile,
			Args: []string{"exec", container, "mv", "-f", "--", partial, filePath},
		})
		if moveErr != nil {
			return abandon(moveErr)
		}
		if moved.Exit != 0 {
			return abandon(fmt.Errorf("%w: %s", ErrDocker, commandFailure(moved.Out, moved.Exit)))
		}
		// docker exec runs as root. Hand ~/素材, the bot folder, and the
		// finished file to the desktop user before this lock is released,
		// so another operation cannot observe a root-owned file.
		owned, ownErr := s.runtime().Exec(publishCtx, spec, ExecRequest{
			Kind: ExecFile,
			Args: materialChownArgs(container, filePath, true),
		})
		if ownErr != nil {
			return ownErr
		}
		if owned.Exit != 0 {
			return fmt.Errorf("%w: %s", ErrDocker, commandFailure(owned.Out, owned.Exit))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.noteComputerActivity(spec.TenantID, spec.UserID)
	return fmt.Sprintf("Wrote %s (%d bytes).", filePath, counted.n), nil
}

// removeMaterialPartial deletes a short or failed upload. It does not take
// withUser: the caller already holds that lock.
func (s *Service) removeMaterialPartial(ctx context.Context, spec Spec, container, partial string) {
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = s.runtime().Exec(context.WithoutCancel(ctx), spec, ExecRequest{
		Kind: ExecFile,
		Args: []string{"exec", container, "rm", "-f", "--", partial},
	})
}

// chownMaterialDirs gives the folders a failed write created back to the
// desktop user. The finished file is not included: it was not replaced.
// Missing folders are ignored. It does not take withUser.
func (s *Service) chownMaterialDirs(ctx context.Context, spec Spec, container, filePath string) {
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = s.runtime().Exec(context.WithoutCancel(ctx), spec, ExecRequest{
		Kind: ExecFile,
		Args: materialChownArgs(container, filePath, false),
	})
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

// materialChownArgs gives desktop.MaterialsRoot and each folder below it to
// MaterialOwner. includeFile also names filePath. Nothing above
// MaterialsRoot is touched.
func materialChownArgs(container, filePath string, includeFile bool) []string {
	paths := []string{desktop.MaterialsRoot}
	rest := strings.TrimPrefix(filePath, desktop.MaterialsRoot+"/")
	cur := desktop.MaterialsRoot
	parts := strings.Split(rest, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		cur += "/" + part
		if !includeFile && i == len(parts)-1 {
			continue
		}
		paths = append(paths, cur)
	}
	return append([]string{"exec", container, "chown", MaterialOwner, "--"}, paths...)
}
