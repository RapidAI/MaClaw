package guiapp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// codingStaticRemoteExecConformanceVersion is the hermetic conformance suite
// version for the remote-exec cancellation fence. It must change exactly when
// codingStaticRemoteExecFenceVersion does; the equality test below is the
// machine evidence pinning suite to implementation, in the E1 discipline
// (remediation doc §9.20 gap retirement).
const codingStaticRemoteExecConformanceVersion = "coding-remote-exec-v1"

func TestCodingStaticRemoteExecFenceVersionMatchesConformanceSuite(t *testing.T) {
	if codingStaticRemoteExecFenceVersion != codingStaticRemoteExecConformanceVersion {
		t.Fatalf("exec fence implementation version %q does not match conformance suite version %q", codingStaticRemoteExecFenceVersion, codingStaticRemoteExecConformanceVersion)
	}
}

// codingStaticRemoteExecConformance records which exec-fence properties the
// hermetic suite proves. Evidence metadata, not a production gate.
type codingStaticRemoteExecConformance struct {
	HasCancelFencesPartialOutput  bool
	HasLateOutputNotDelivered     bool
	HasConcurrentCancelIdempotent bool
}

// codingStaticRemoteExecConformanceCoverage maps every Has* field to the
// conformance tests that prove it. Function references make a missing or
// renamed test a compile error; the meta-test makes an unmapped field a
// failure.
var codingStaticRemoteExecConformanceCoverage = map[string][]func(*testing.T){
	"HasCancelFencesPartialOutput":  {TestCodingStaticRemoteExecCancelFencesPartialOutput},
	"HasLateOutputNotDelivered":     {TestCodingStaticRemoteExecLateOutputNotDelivered},
	"HasConcurrentCancelIdempotent": {TestCodingStaticRemoteExecConcurrentCancelIdempotent},
}

func TestCodingStaticRemoteExecConformanceFieldsHaveCoverage(t *testing.T) {
	conformanceType := reflect.TypeOf(codingStaticRemoteExecConformance{})
	hasFields := 0
	for i := 0; i < conformanceType.NumField(); i++ {
		field := conformanceType.Field(i)
		if !strings.HasPrefix(field.Name, "Has") || field.Type.Kind() != reflect.Bool {
			continue
		}
		hasFields++
		if len(codingStaticRemoteExecConformanceCoverage[field.Name]) == 0 {
			t.Errorf("exec-fence conformance field %s has no hermetic coverage", field.Name)
		}
	}
	if hasFields != 3 {
		t.Fatalf("exec-fence conformance field count changed without updating the coverage map: %d", hasFields)
	}
}

// startRemoteExecConformanceServer starts an in-process SSH server (loopback
// TCP, no external network) that answers exec requests deterministically: a
// command containing "__PARTIAL_SLEEP__" writes one stdout chunk immediately
// and then holds the channel open; any other command echoes "OUT:<cmd>". The
// interactive shell echoes PTY input so the scrollback path also has
// observable partial output. This is the same hermetic-seam pattern as
// corelib/remote/ssh_exec_test.go, replicated here because that helper is
// test-package-private.
func startRemoteExecConformanceServer(t *testing.T) (string, ssh.Signer) {
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
					go handleRemoteExecConformanceChannel(ch, chRequests)
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String(), signer
}

func handleRemoteExecConformanceChannel(ch ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "pty-req":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			go func() { _, _ = io.Copy(ch, ch) }()
			return
		case "exec":
			_ = req.Reply(true, nil)
			go runRemoteExecConformanceCommand(ch, req.Payload)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
	_ = ch.Close()
}

func runRemoteExecConformanceCommand(ch ssh.Channel, payload []byte) {
	var command string
	if len(payload) >= 4 {
		n := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
		if n >= 0 && len(payload) >= 4+n {
			command = string(payload[4 : 4+n])
		}
	}
	if strings.Contains(command, "__PARTIAL_SLEEP__") {
		_, _ = io.WriteString(ch, "PARTIAL-OUTPUT\n")
		time.Sleep(30 * time.Second)
		_ = ch.Close()
		return
	}
	_, _ = io.WriteString(ch, "OUT:"+command+"\n")
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	_ = ch.Close()
}

// newRemoteExecConformanceHandler builds an IMMessageHandler whose SSH
// manager owns one live managed session on the fake server, plus the frozen
// target identity the runtime-bound caller requires.
func newRemoteExecConformanceHandler(t *testing.T) (*IMMessageHandler, string, string) {
	t.Helper()
	address, signer := startRemoteExecConformanceServer(t)
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
	session, err := mgr.Create(remote.SSHSessionSpec{HostConfig: cfg})
	if err != nil {
		t.Fatalf("create managed session: %v", err)
	}
	h := &IMMessageHandler{sshMgr: mgr}
	workDir := "/srv/project"
	expected := guiRemoteCodingTargetIdentityForConfig(cfg, workDir)
	if expected == "" {
		t.Fatal("frozen target identity must derive from the fake session config")
	}
	return h, session.ID, expected
}

// TestCodingStaticRemoteExecCancelFencesPartialOutput: over the exec-channel
// caller path (sshExecChannelContext → ExecCommandChannel → RunSSHCommand), a
// command that already produced observable stdout and is then cancelled
// yields an error and NO output — partial stdout/stderr never surfaces as a
// complete remote coding result.
func TestCodingStaticRemoteExecCancelFencesPartialOutput(t *testing.T) {
	h, sessionID, expected := newRemoteExecConformanceHandler(t)
	workDir := "/srv/project"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	out, err := h.sshExecChannelContext(ctx, sessionID, "cargo test __PARTIAL_SLEEP__", 5, expected, workDir)
	if err == nil {
		t.Fatal("cancelled remote exec must fail")
	}
	if strings.Contains(out, "PARTIAL") || strings.TrimSpace(out) != "" {
		t.Fatalf("cancelled remote exec leaked partial output: %q", out)
	}
}

// TestCodingStaticRemoteExecLateOutputNotDelivered: after a fenced cancel, the
// verified session stays usable and a follow-up exec sees only its own output
// — the interrupted command's late partial bytes are never delivered into a
// later result.
func TestCodingStaticRemoteExecLateOutputNotDelivered(t *testing.T) {
	h, sessionID, expected := newRemoteExecConformanceHandler(t)
	workDir := "/srv/project"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	_, _ = h.sshExecChannelContext(ctx, sessionID, "cargo test __PARTIAL_SLEEP__", 5, expected, workDir)

	out, err := h.sshExecChannelContext(context.Background(), sessionID, "echo after", 5, expected, workDir)
	if err != nil {
		t.Fatalf("follow-up exec after fenced cancel failed: %v", err)
	}
	if !strings.Contains(out, "OUT:echo after") || strings.Contains(out, "PARTIAL") {
		t.Fatalf("late partial output delivered to follow-up exec: %q", out)
	}
}

// TestCodingStaticRemoteExecConcurrentCancelIdempotent: concurrent cancellers
// of one in-flight exec produce exactly one fenced outcome — no panic, no
// second result, no partial output (safe under -race).
func TestCodingStaticRemoteExecConcurrentCancelIdempotent(t *testing.T) {
	h, sessionID, expected := newRemoteExecConformanceHandler(t)
	workDir := "/srv/project"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(200 * time.Millisecond)
			cancel()
		}()
	}
	out, err := h.sshExecChannelContext(ctx, sessionID, "cargo test __PARTIAL_SLEEP__", 5, expected, workDir)
	wg.Wait()
	if err == nil {
		t.Fatal("cancelled remote exec must fail")
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("concurrent-cancel exec leaked output: %q", out)
	}
}
