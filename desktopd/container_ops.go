package desktopd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
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

const defaultDockerOutputMax = 32 << 20

type tailOutput struct {
	mu    sync.Mutex
	limit int
	head  bool
	data  []byte
}

func (b *tailOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.head {
		if remaining := b.limit - len(b.data); remaining > 0 {
			if len(p) > remaining {
				p = p[:remaining]
			}
			b.data = append(b.data, p...)
		}
		return n, nil
	}
	if n >= b.limit {
		b.data = append(b.data[:0], p[n-b.limit:]...)
		return n, nil
	}
	if overflow := len(b.data) + n - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (b *tailOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func dockerOutputLimit(args []string) int {
	for i, arg := range args {
		if arg == "curl" {
			return desktop.HTTPBodyMax + 64<<10
		}
		if arg == "file" && i+1 < len(args) {
			switch args[i+1] {
			case "read":
				return desktop.FileReadMax + 4<<10
			case "bytes", "link":
				return desktop.FileDeliverBase64Max + 4<<10
			default:
				return 64 << 10
			}
		}
		if arg != "command" || i+1 >= len(args) {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(args[i+1])
		if err != nil {
			continue
		}
		var req commandRequest
		if json.Unmarshal(raw, &req) != nil {
			continue
		}
		switch req.Op {
		case "shell":
			return desktop.BashOutputMax + 1
		case "shot":
			return 64 << 20
		case "ctl":
			return 1 << 20
		}
	}
	return defaultDockerOutputMax
}

func newDockerOutput(args []string) *tailOutput {
	out := &tailOutput{limit: dockerOutputLimit(args)}
	for i, arg := range args {
		if arg == "file" && i+1 < len(args) && args[i+1] == "read" {
			out.head = true
			break
		}
	}
	return out
}

func defaultRunCommand(ctx context.Context, stdin io.Reader, args ...string) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out := newDockerOutput(args)
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	text := out.String()
	if ctx.Err() != nil {
		return text, 0, fmt.Errorf("%w: %w", ErrDocker, ctx.Err())
	}
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
	var container string
	var onDesktop bool
	err = s.withUserReadContext(ctx, spec.TenantID, spec.UserID, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(container, "read", filePath, desktop.FileReadMax+1),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteToolUse(spec, container, onDesktop)
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
// This is the shared read: any container path the bot can name, including
// ~/Downloads and /tmp.
func (s *Service) ReadBytes(ctx context.Context, tenantID, userID, filePath string) (string, error) {
	return s.readEncodedFile(ctx, tenantID, userID, filePath, "bytes")
}

// ReadDesktopLink is the Hub /desktop-file fetch. The supervisor resolves
// symlinks and allows only the work tree and Desktop. The shared bytes
// read does not.
func (s *Service) ReadDesktopLink(ctx context.Context, tenantID, userID, filePath string) (string, error) {
	return s.readEncodedFile(ctx, tenantID, userID, filePath, "link")
}

func (s *Service) readEncodedFile(ctx context.Context, tenantID, userID, filePath, op string) (string, error) {
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
	var container string
	var onDesktop bool
	err = s.withUserReadContext(ctx, spec.TenantID, spec.UserID, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(container, op, filePath, desktop.FileBytesMax+1),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteToolUse(spec, container, onDesktop)
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
	var container string
	var onDesktop bool
	err = s.withUserFileContext(ctx, spec.TenantID, spec.UserID, filePath, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind:  ExecFile,
			Args:  fileServiceArgs(container, "write", filePath, 0),
			Stdin: strings.NewReader(content),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteToolUse(spec, container, onDesktop)
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
	var container string
	var onDesktop bool
	var output string
	var exitCode int
	// Read and write share one user lock. Releasing it between them lets
	// another command from this user replace the file before the edit lands.
	err = s.withUserFileContext(ctx, spec.TenantID, spec.UserID, filePath, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
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
	s.noteToolUse(spec, container, onDesktop)
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
	var container string
	var onDesktop bool
	err = s.withUserReadContext(ctx, spec.TenantID, spec.UserID, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{
			Kind: ExecFile,
			Args: fileServiceArgs(container, "list", filePath, 0),
		})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		return "", err
	}
	s.noteToolUse(spec, container, onDesktop)
	if exitCode != 0 {
		return "", fmt.Errorf("%w: %s", ErrDocker, commandFailure(output, exitCode))
	}
	text := strings.TrimSpace(output)
	if text == "" {
		text = "(empty)"
	}
	return filePath + "\n" + text, nil
}

// Bash runs one command and returns its output. A bot's bash that does not
// ask for a screen stays in the workspace for the life of that bot, even
// when the desktop container is awake, so a job's /tmp and pid stay visible.
func (s *Service) Bash(ctx context.Context, tenantID, userID, command string) (string, error) {
	return s.bash(ctx, tenantID, userID, command, false)
}

func (s *Service) bash(ctx context.Context, tenantID, userID, command string, needsScreen bool) (string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	if _, err = desktop.BashArgs(containerName(spec.TenantID, spec.UserID), command); err != nil {
		return "", err
	}
	// needsScreen opens the bot screen and is the only way this bash gets a
	// new DISPLAY. Otherwise inherit a screen open_screen already started,
	// and stay headless when none is up.
	display := ""
	if spec.BotID != "" && needsScreen {
		if err := s.ensureBotScreen(ctx, spec); err != nil {
			return "", err
		}
		got, openErr := s.specDisplay(ctx, spec, "")
		if openErr != nil {
			return "", openErr
		}
		display = got
	} else if spec.BotID != "" {
		if got, err := s.readBotDisplay(ctx, spec); err == nil {
			display = got
		}
	}
	// A real display is the command's DISPLAY. The sentinel is only for a
	// headless shell, so it cannot replace a screen that is already open
	// and a command on that screen is not screen_required.
	sentinel := ""
	if spec.BotID != "" && !needsScreen && display == "" {
		sentinel = newSentinelToken()
	}
	shell := commandRequest{
		Op:             "shell",
		Command:        strings.TrimSpace(command),
		Display:        display,
		Sentinel:       sentinel,
		TimeoutSeconds: int((desktop.BashBudget - 5*time.Second) / time.Second),
	}
	if _, err := commandServiceArgs(containerName(spec.TenantID, spec.UserID), shell); err != nil {
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
	var container string
	var onDesktop bool
	err = s.withUserReadContext(ctx, spec.TenantID, spec.UserID, func() error {
		if spec.BotID != "" && !needsScreen && s.httpUsesWorkspace() {
			name, wErr := s.ensureWorkspace(ctx, spec)
			if wErr != nil {
				return wErr
			}
			container, onDesktop = name, false
		} else {
			c, desk, cErr := s.toolContainer(ctx, spec)
			if cErr != nil {
				return cErr
			}
			container, onDesktop = c, desk
		}
		commandArgs, argsErr := commandServiceArgs(container, shell)
		if argsErr != nil {
			return argsErr
		}
		result, runErr := s.runtime().Exec(ctx, spec, ExecRequest{Kind: ExecShell, Args: commandArgs})
		output, exitCode = result.Out, result.Exit
		return runErr
	})
	if err != nil {
		if ctx.Err() != nil {
			s.noteToolUse(spec, container, onDesktop)
			return desktop.ProgramOutput(output, exitCode, true), nil
		}
		return "", err
	}
	s.noteToolUse(spec, container, onDesktop)
	text := desktop.ProgramOutput(output, exitCode, false)
	// A successful command may probe DISPLAY. That connection is not a
	// request for a screen. screen_required is a failed command that also
	// connected to the sentinel.
	if sentinel != "" && container != "" && exitCode != 0 && s.sentinelConnected(ctx, container, sentinel) {
		return text, &desktopbot.ScreenRequiredError{Output: text}
	}
	return text, nil
}

func newSentinelToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// sentinelConnected reads the supervisor's record of an X client connecting
// to this command's sentinel display. The program's own output is not read.
func (s *Service) sentinelConnected(ctx context.Context, container, token string) bool {
	if s == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(container) == "" {
		return false
	}
	out, err := s.docker(ctx, "exec", container, "python3", "/desktop_supervisor.py", "sentinel", token)
	if err != nil {
		return false
	}
	var rec struct {
		XClient bool `json:"x_client"`
	}
	line := statusLine(out)
	if line == "" || json.Unmarshal([]byte(line), &rec) != nil {
		return false
	}
	return rec.XClient
}

// ErrDesktopAsleep is a fetch that would have docker-exec'd curl into a
// stopped desktop. With the workspace on, the caller leaves the GUI asleep.
// With the workspace off, HTTPGet wakes once with bot-desktop and retries.
var ErrDesktopAsleep = errors.New("desktop_asleep")

// httpUsesWorkspace is the headless path: curl can run while the GUI sleeps.
// A machine runtime has no Docker sibling, so it wakes the desktop instead.
func (s *Service) httpUsesWorkspace() bool {
	return s != nil && s.ToolWorkspace && s.Machines == nil
}

// HTTPGet fetches one URL with curl. A running desktop is used, so the
// proxy and the network are the same ones the browser has. While the
// desktop sleeps and the workspace is on, curl runs in the headless
// workspace and does not start the GUI. With the workspace off, or on a
// machine runtime, a stopped desktop is woken with bot-desktop. A warm
// start still in progress is WarmStartError, which the HTTP route returns
// as 503 DESKTOP_WAKING. A wake that does not come up is that error, not
// an empty success.
func (s *Service) HTTPGet(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", 0, "", err
	}
	if _, err = desktop.CurlGetArgs(containerName(spec.TenantID, spec.UserID), rawURL, desktop.HTTPBodyMax); err != nil {
		return "", 0, "", err
	}
	body, status, kind, err := s.httpGet(ctx, spec, rawURL)
	if err == nil || !errors.Is(err, ErrDesktopAsleep) || s.httpUsesWorkspace() {
		return body, status, kind, err
	}
	spec.WakeReason = WakeReasonBotDesktop
	if werr := s.Ensure(ctx, spec); werr != nil {
		return "", 0, "", werr
	}
	return s.httpGet(ctx, spec, rawURL)
}

func (s *Service) httpGet(ctx context.Context, spec Spec, rawURL string) (string, int, string, error) {
	var output string
	var exitCode int
	var container string
	var onDesktop bool
	err := s.withUserReadContext(ctx, spec.TenantID, spec.UserID, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
		if desk {
			running, runErr := s.containerRunning(ctx, c)
			// A machine runtime is not a Docker inspect. Treat a missing
			// or unreadable container as asleep so the caller can wake it.
			if runErr != nil && !s.httpUsesWorkspace() {
				return ErrDesktopAsleep
			}
			if runErr != nil {
				return runErr
			}
			if !running {
				return ErrDesktopAsleep
			}
		}
		args, argsErr := desktop.CurlGetArgs(c, rawURL, desktop.HTTPBodyMax)
		if argsErr != nil {
			return argsErr
		}
		var runErr error
		output, exitCode, runErr = s.runCommand(ctx, nil, args...)
		return runErr
	})
	if err != nil {
		return "", 0, "", err
	}
	s.noteToolUse(spec, container, onDesktop)
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
// mv's it onto filePath. The .partial suffix is reserved to keep two target
// paths from sharing one temporary file. A short body deletes the partial
// and leaves the previous file in place. mv, rm, and chown stay inside the
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
	if strings.HasSuffix(filePath, ".partial") {
		return "", fmt.Errorf("%w: .partial is reserved for upload temporary files", ErrInvalid)
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
	var container string
	var onDesktop bool
	err = s.withUserFileContext(ctx, spec.TenantID, spec.UserID, filePath, func() error {
		c, desk, cErr := s.toolContainer(ctx, spec)
		if cErr != nil {
			return cErr
		}
		container, onDesktop = c, desk
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
	s.noteToolUse(spec, container, onDesktop)
	return fmt.Sprintf("Wrote %s (%d bytes).", filePath, counted.n), nil
}

// removeMaterialPartial deletes a short or failed upload. It does not take
// withUser: the caller already holds that lock.
func (s *Service) removeMaterialPartial(ctx context.Context, spec Spec, container, partial string) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	_, _ = s.runtime().Exec(cleanupCtx, spec, ExecRequest{
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
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	_, _ = s.runtime().Exec(cleanupCtx, spec, ExecRequest{
		Kind: ExecFile,
		Args: materialChownArgs(container, filePath, false),
	})
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), materialPublishTimeout)
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
