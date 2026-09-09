package remote

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshExecMaxOutputBytes bounds the captured output of one exec channel per
// stream. A runaway remote command must not grow host memory without limit.
const sshExecMaxOutputBytes = 1 << 20

// SSHExecResult is the bounded result of one non-PTY exec channel.
type SSHExecResult struct {
	Stdout string
	Stderr string
	// ExitCode is the remote exit status, or -1 when the server closed the
	// channel without reporting one (for example after a forced close).
	ExitCode int
}

// cappedBuffer is an io.Writer that stores at most cap bytes.
type cappedBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := sshExecMaxOutputBytes - len(b.buf); room > 0 {
		if len(p) > room {
			b.buf = append(b.buf, p[:room]...)
		} else {
			b.buf = append(b.buf, p...)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// RunSSHCommand executes command through its own non-PTY session channel on
// client. Unlike the interactive PTY shell, concurrent commands never share a
// scrollback: stdout/stderr and the exit status belong to this command alone.
// The command runs through the remote login shell exactly like OpenSSH
// `ssh host cmd`; no PTY is allocated and no echo/prompt bytes are captured.
//
// A non-zero remote exit status is reported via ExitCode, not as an error;
// error is reserved for transport/protocol failures, ctx cancellation and
// timeout. On timeout or cancellation the channel is closed so the server can
// reap the remote process.
func RunSSHCommand(ctx context.Context, client *ssh.Client, command string, timeout time.Duration) (SSHExecResult, error) {
	if client == nil {
		return SSHExecResult{}, fmt.Errorf("ssh exec channel: nil client")
	}
	if strings.TrimSpace(command) == "" {
		return SSHExecResult{}, fmt.Errorf("ssh exec channel: empty command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	session, err := client.NewSession()
	if err != nil {
		return SSHExecResult{}, fmt.Errorf("ssh exec channel: new session: %w", err)
	}
	stdout, stderr := &cappedBuffer{}, &cappedBuffer{}
	session.Stdout = stdout
	session.Stderr = stderr
	if err := session.Start(command); err != nil {
		_ = session.Close()
		return SSHExecResult{}, fmt.Errorf("ssh exec channel: start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case waitErr := <-done:
		result := SSHExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
		if waitErr != nil {
			if exitErr, ok := waitErr.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
				return result, nil
			}
			result.ExitCode = -1
			return result, fmt.Errorf("ssh exec channel: wait: %w", waitErr)
		}
		return result, nil
	case <-ctx.Done():
		_ = session.Close()
		return SSHExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, ctx.Err()
	case <-timer.C:
		_ = session.Close()
		return SSHExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1},
			fmt.Errorf("ssh exec channel: command timed out after %s", timeout)
	}
}

// ExecCommandChannel runs one command on its own non-PTY channel of the
// session's underlying pooled SSH connection. It never writes to the shared
// interactive PTY, never waits on its scrollback and never reconnects, so
// parallel isolated writers cannot interleave each other's command output.
// The session must exist and its handle must be alive.
func (m *SSHSessionManager) ExecCommandChannel(ctx context.Context, sessionID, command string, timeout time.Duration) (SSHExecResult, error) {
	if m == nil {
		return SSHExecResult{}, fmt.Errorf("ssh exec channel: session manager is unavailable")
	}
	s, ok := m.Get(sessionID)
	if !ok {
		return SSHExecResult{}, fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return SSHExecResult{}, fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	client := s.Handle.Client()
	if client == nil {
		return SSHExecResult{}, fmt.Errorf("ssh session %s is closed", sessionID)
	}
	return RunSSHCommand(ctx, client, command, timeout)
}
