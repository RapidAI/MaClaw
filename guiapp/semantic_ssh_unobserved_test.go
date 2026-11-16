package guiapp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestTrustedSSHWaitResultKeepsFinishedOutput(t *testing.T) {
	got, err := trustedSSHWaitResult([]string{"ok", "root@host:~#"}, remote.SessionRunning, context.DeadlineExceeded, "")
	if err != nil || got != "ok\nroot@host:~#" {
		t.Fatalf("finished output at the deadline = %q err=%v", got, err)
	}
	_, err = trustedSSHWaitResult([]string{remote.SSHWaitTimeoutShellBusyNotice}, remote.SessionRunning, nil, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") {
		t.Fatalf("busy shell = %v", err)
	}
	_, err = trustedSSHWaitResult([]string{"partial", "[maclaw] 命令执行超时，已发送 Ctrl+C 中断"}, remote.SessionRunning, context.DeadlineExceeded, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_timeout") || strings.Contains(err.Error(), "outcome_unobserved") {
		t.Fatalf("recovered timeout = %v", err)
	}
	_, err = trustedSSHWaitResult([]string{"uptime", "partial", "[maclaw] 命令执行超时，已发送 Ctrl+C 中断"}, remote.SessionRunning, context.DeadlineExceeded, "uptime")
	if err == nil || !strings.Contains(err.Error(), "partial") || strings.Contains(err.Error(), "uptime") {
		t.Fatalf("timeout echo = %v", err)
	}
	_, err = trustedSSHWaitResult(nil, remote.SessionError, context.DeadlineExceeded, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") || strings.Contains(err.Error(), "trusted_ssh_timeout") {
		t.Fatalf("dead session at the deadline = %v", err)
	}
}

func sshAdapterResult(t *testing.T, handlerErr error) string {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedSSH = func(string, string) (string, error) {
		return "", handlerErr
	}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"echo hi"}`),
	})
	if !strings.Contains(got, handlerErr.Error()) {
		t.Fatalf("adapter did not reach the handler failure %q, got %q", handlerErr, got)
	}
	return got
}

// A session that ended after the command was written leaves no way to know
// whether the command ran. Until this fact had its own name it was classified
// correctly only by coincidence -- the adapter's list happens to treat every
// lost session as unknown -- so the name has to survive on its own merits.
func TestSSHDispatchedThenLostStaysUnknown(t *testing.T) {
	got := sshAdapterResult(t, fmt.Errorf("trusted_ssh_outcome_unobserved"))
	if !strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("dispatched-then-lost command = %q, want an unknown outcome", got)
	}
}

// Splitting the vocabulary must not have narrowed anything. These two still
// report unknown, which is over-cautious rather than unsafe, and the point of
// pinning them is that a later tightening should be a deliberate change with
// its own reasoning rather than a side effect of renaming one case.
func TestSSHTimeoutIsSettledFailure(t *testing.T) {
	got := sshAdapterResult(t, fmt.Errorf("trusted_ssh_timeout"))
	if !strings.HasPrefix(got, "[system rejected]") || strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("timeout = %q, want a settled rejection so the next command stays available", got)
	}
}

// No bound session means the command was never written. Classifying that as
// unknown froze the selection as if the command might have run, so a later
// connect could not close it.
func TestSSHMissingSessionBeforeWriteStaysRetractable(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"echo hi"}`),
	})
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || strings.HasPrefix(got, "[system unknown]") || !strings.Contains(got, "trusted_ssh_session_unavailable") {
		t.Fatalf("missing session before write = %q", got)
	}
}

// A raw ssh line is a policy denial. It must stay a recorded rejection, once,
// so the same bypass is not offered again as a fresh call.
func TestSSHRawCommandStaysRecordedRejection(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"ssh user@host uptime"}`),
	})
	want, tripped := tool.RejectRawSSHCommand("ssh user@host uptime")
	if !tripped || got != want || tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) {
		t.Fatalf("raw ssh = %q", got)
	}
}

func TestRecordedSSHRefusalAfterEffectDropsTheUnexecutedMarker(t *testing.T) {
	got := recordedSSHRefusalAfterEffect("[system rejected] arguments_rejected: trusted_ssh_remote_path_required")
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || !strings.Contains(got, "trusted_ssh_remote_path_required") {
		t.Fatalf("post-effect refusal = %q", got)
	}
}

func TestSSHLostSessionNamesAllStillReportUnknown(t *testing.T) {
	for _, name := range []string{
		"trusted_ssh_session_disconnected",
		"trusted_ssh_session_unavailable",
	} {
		t.Run(name, func(t *testing.T) {
			got := sshAdapterResult(t, fmt.Errorf("%s", name))
			if !strings.HasPrefix(got, "[system unknown]") {
				t.Fatalf("%s = %q, want an unknown outcome", name, got)
			}
		})
	}
}

// startTrustedSSHRecoveryServer is the same hermetic in-process SSH server
// pattern as startRemoteExecConformanceServer. Its interactive shell answers
// every input line with "OUT:<line>" and a root prompt so WaitForOutput sees
// a completion signal. A line containing "__DIE__" writes a partial line and
// closes the channel, simulating a remote shell that vanishes mid-command.
func startTrustedSSHRecoveryServer(t *testing.T) (string, ssh.Signer) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, gotPassword []byte) (*ssh.Permissions, error) {
			if conn.User() == "deploy" && string(gotPassword) == "correct-password" {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid test credentials")
		},
	}
	serverConfig.AddHostKey(signer)
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serverConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
				if handshakeErr != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "only session channels")
						continue
					}
					ch, chRequests, chErr := channel.Accept()
					if chErr != nil {
						continue
					}
					go handleTrustedSSHRecoveryChannel(ch, chRequests, serverConn, listener)
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String(), signer
}

func handleTrustedSSHRecoveryChannel(ch ssh.Channel, requests <-chan *ssh.Request, serverConn *ssh.ServerConn, listener net.Listener) {
	for req := range requests {
		switch req.Type {
		case "pty-req":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			go serveTrustedSSHRecoveryShell(ch, serverConn, listener)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
	_ = ch.Close()
}

func serveTrustedSSHRecoveryShell(ch ssh.Channel, serverConn *ssh.ServerConn, listener net.Listener) {
	defer func() { _ = ch.Close() }()
	buf := make([]byte, 4096)
	pending := ""
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			pending += string(buf[:n])
			for {
				i := strings.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimRight(pending[:i], "\r")
				pending = pending[i+1:]
				if strings.Contains(line, "__DROPHOST__") {
					// The whole host goes away: channel, connection, and the
					// listener, so recovery's re-dial is refused outright.
					_, _ = io.WriteString(ch, "PARTIAL-BEFORE-DROP\n")
					_ = ch.Close()
					_ = serverConn.Close()
					_ = listener.Close()
					return
				}
				if strings.Contains(line, "__DIE__") {
					_, _ = io.WriteString(ch, "PARTIAL-BEFORE-DIE\n")
					return
				}
				_, _ = io.WriteString(ch, "OUT:"+line+"\nroot@fake:~# ")
			}
		}
		if err != nil {
			return
		}
	}
}

// newTrustedSSHRecoverySession builds a session manager holding one managed
// session on the fake server, owned by principal-a.
func newTrustedSSHRecoverySession(t *testing.T) (*remote.SSHSessionManager, *remote.SSHManagedSession) {
	t.Helper()
	address, signer := startTrustedSSHRecoveryServer(t)
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	cfg := remote.SSHHostConfig{
		Host:               host,
		User:               "deploy",
		Port:               port,
		Password:           "correct-password",
		AuthMethod:         "password",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	mgr := remote.NewSSHSessionManager(remote.NewSSHPool())
	t.Cleanup(func() { mgr.Pool().CloseAll() })
	session, err := mgr.Create(remote.SSHSessionSpec{HostConfig: cfg, OwnerID: "principal-a"})
	if err != nil {
		t.Fatalf("create managed session: %v", err)
	}
	return mgr, session
}

// A dropped transport is re-dialed before the command is written, so the
// command still runs on the recovered session instead of failing the turn.
func TestTrustedSSHExecReconnectsDeadSessionBeforeWrite(t *testing.T) {
	mgr, session := newTrustedSSHRecoverySession(t)
	client := session.PTY().SSHClient()
	if client == nil {
		t.Fatal("session has no pooled client")
	}
	_ = client.Close()

	out, err := executeTrustedBoundSSH(mgr, session, "echo hi", 15*time.Second)
	if err != nil {
		t.Fatalf("exec on a recovered session = %v", err)
	}
	if !strings.Contains(out, "OUT:echo hi") {
		t.Fatalf("recovered exec output = %q", out)
	}
}

// A shell that vanishes mid-command reports an unobserved outcome, keeps the
// partial output as evidence, and leaves a reconnected session behind so the
// next command works without a connect round-trip.
func TestTrustedSSHUnobservedOutcomeRecoversSession(t *testing.T) {
	mgr, session := newTrustedSSHRecoverySession(t)

	_, err := executeTrustedBoundSSH(mgr, session, "run __DIE__", 15*time.Second)
	if err == nil {
		t.Fatal("a vanishing shell must not look like a finished command")
	}
	if !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") {
		t.Fatalf("vanished shell = %v, want outcome_unobserved", err)
	}
	if !strings.Contains(err.Error(), "PARTIAL-BEFORE-DIE") {
		t.Fatalf("pre-disconnect output tail missing: %v", err)
	}
	if !strings.Contains(err.Error(), "已自动重连") {
		t.Fatalf("recovery note missing: %v", err)
	}

	out, err := executeTrustedBoundSSH(mgr, session, "echo after", 15*time.Second)
	if err != nil {
		t.Fatalf("follow-up exec on recovered session = %v", err)
	}
	if !strings.Contains(out, "OUT:echo after") {
		t.Fatalf("follow-up output = %q", out)
	}
}

// The exec path binds to the session this principal opened even after its
// shell died: the single dead session is re-dialed rather than refused as
// unavailable.
func TestTrustedSSHExecFallsBackToDeadOwnedSession(t *testing.T) {
	mgr, session := newTrustedSSHRecoverySession(t)
	if err := mgr.WriteInput(session.ID, "run __DIE__"); err != nil {
		t.Fatalf("write die command: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := mgr.GetSessionStatus(session.ID)
		if status == remote.SessionExited || status == remote.SessionError {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	status, _ := mgr.GetSessionStatus(session.ID)
	if status != remote.SessionExited && status != remote.SessionError {
		t.Fatalf("session did not die: %s", status)
	}

	h := &IMMessageHandler{registry: NewToolRegistry(), sshMgr: mgr}
	out, err := h.executeTrustedSSH("principal-a", "echo hi", 15*time.Second)
	if err != nil {
		t.Fatalf("exec over a re-dialed dead session = %v", err)
	}
	if !strings.Contains(out, "OUT:echo hi") {
		t.Fatalf("recovered output = %q", out)
	}
}

// A host that disappears mid-command cannot be recovered. The dead binding
// is released so the next call plans a fresh connect instead of blocking
// on the same session again.
func TestTrustedSSHUnobservedRecoveryFailureReleasesSession(t *testing.T) {
	mgr, session := newTrustedSSHRecoverySession(t)

	_, err := executeTrustedBoundSSH(mgr, session, "run __DROPHOST__", 15*time.Second)
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") {
		t.Fatalf("dropped host = %v, want outcome_unobserved", err)
	}
	if !strings.Contains(err.Error(), "PARTIAL-BEFORE-DROP") {
		t.Fatalf("pre-disconnect output tail missing: %v", err)
	}
	if !strings.Contains(err.Error(), "已释放") {
		t.Fatalf("failed-recovery release note missing: %v", err)
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("unrecoverable session still bound: %d sessions", len(got))
	}

	h := &IMMessageHandler{registry: NewToolRegistry(), sshMgr: mgr}
	if _, err := h.executeTrustedSSH("principal-a", "echo hi", 5*time.Second); err == nil || !strings.Contains(err.Error(), "trusted_ssh_session_unavailable") {
		t.Fatalf("exec after release = %v, want session_unavailable", err)
	}
}
