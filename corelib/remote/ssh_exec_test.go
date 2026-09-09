package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startTestExecSSHServer starts an in-process SSH server that accepts session
// channels and answers exec requests with deterministic canned output. Each
// exec writes "OUT:<cmd>" on stdout and "ERR:<cmd>" on stderr in small chunks
// so concurrent commands would visibly interleave on any shared stream. A
// command containing "__EXIT_7__" reports exit status 7; "__SLEEP__" makes the
// server hold the channel open for 30s to exercise timeout/cancel paths.
// pty-req/shell are accepted so a full managed PTY session can coexist with
// dedicated exec channels on the same connection.
func startTestExecSSHServer(t *testing.T, signer ssh.Signer, user, password string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, gotPassword []byte) (*ssh.Permissions, error) {
			if conn.User() == user && string(gotPassword) == password {
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
					go handleTestExecSessionChannel(ch, chRequests)
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func handleTestExecSessionChannel(ch ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "pty-req":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			go func() {
				_, _ = io.Copy(io.Discard, ch)
				_ = ch.Close()
			}()
			return
		case "exec":
			_ = req.Reply(true, nil)
			go runTestExecCommand(ch, req.Payload)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
	_ = ch.Close()
}

func runTestExecCommand(ch ssh.Channel, payload []byte) {
	var command string
	if len(payload) >= 4 {
		n := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
		if n >= 0 && len(payload) >= 4+n {
			command = string(payload[4 : 4+n])
		}
	}
	exitCode := uint32(0)
	if strings.Contains(command, "__EXIT_7__") {
		exitCode = 7
	}
	if strings.Contains(command, "__SLEEP__") {
		time.Sleep(30 * time.Second)
	}
	// Chunked writes with a short pause encourage interleaving when a caller
	// wrongly shares one stream between concurrent commands.
	for _, chunk := range []string{"OUT:", command, "\n"} {
		_, _ = io.WriteString(ch, chunk)
		time.Sleep(5 * time.Millisecond)
	}
	_, _ = io.WriteString(ch.Stderr(), "ERR:"+command)
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{exitCode}))
	_ = ch.Close()
}

func testExecSSHConfig(t *testing.T, address string) SSHHostConfig {
	t.Helper()
	host, port := testSSHHostPort(t, address)
	return SSHHostConfig{
		Host:           host,
		User:           "deploy",
		Port:           port,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 2 * time.Second,
	}
}

func TestRunSSHCommandCapturesOwnStreamAndExitCode(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestExecSSHServer(t, signer, "deploy", "correct-password")
	pool := NewSSHPool()
	cfg := testExecSSHConfig(t, address)
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer pool.CloseAll()

	result, err := RunSSHCommand(context.Background(), client, "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("RunSSHCommand() error = %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "OUT:echo hello\n" || result.Stderr != "ERR:echo hello" {
		t.Fatalf("unexpected result: %+v", result)
	}

	failed, err := RunSSHCommand(context.Background(), client, "false __EXIT_7__", 5*time.Second)
	if err != nil {
		t.Fatalf("non-zero remote exit must not be an API error: %v", err)
	}
	if failed.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", failed.ExitCode)
	}
}

func TestRunSSHCommandTimeoutAndCancel(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestExecSSHServer(t, signer, "deploy", "correct-password")
	pool := NewSSHPool()
	cfg := testExecSSHConfig(t, address)
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer pool.CloseAll()

	if _, err := RunSSHCommand(context.Background(), client, "__SLEEP__", 100*time.Millisecond); err == nil ||
		!strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunSSHCommand(ctx, client, "echo hi", 5*time.Second); err == nil {
		t.Fatal("cancelled context must fail the command")
	}
	if _, err := RunSSHCommand(context.Background(), nil, "echo hi", time.Second); err == nil {
		t.Fatal("nil client must fail closed")
	}
	if _, err := RunSSHCommand(context.Background(), client, "   ", time.Second); err == nil {
		t.Fatal("empty command must fail closed")
	}
}

// TestRunSSHCommandConcurrentChannelsDoNotInterleave is the layer-1 blocking
// guarantee: many commands over one pooled connection each get an isolated
// non-PTY channel, so parallel writers cannot pollute each other's output.
func TestRunSSHCommandConcurrentChannelsDoNotInterleave(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestExecSSHServer(t, signer, "deploy", "correct-password")
	pool := NewSSHPool()
	cfg := testExecSSHConfig(t, address)
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer pool.CloseAll()

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("token-%d", i)
			result, runErr := RunSSHCommand(context.Background(), client, "echo "+token, 10*time.Second)
			if runErr != nil {
				errs[i] = runErr
				return
			}
			if result.Stdout != "OUT:echo "+token+"\n" || result.Stderr != "ERR:echo "+token || result.ExitCode != 0 {
				errs[i] = fmt.Errorf("worker %d received foreign output: %+v", i, result)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
}

// TestExecCommandChannelAlongsideManagedPTY proves a managed interactive PTY
// session and dedicated per-command channels coexist on one connection.
func TestExecCommandChannelAlongsideManagedPTY(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestExecSSHServer(t, signer, "deploy", "correct-password")
	mgr := NewSSHSessionManager(NewSSHPool())
	defer mgr.pool.CloseAll()

	session, err := mgr.Create(SSHSessionSpec{HostConfig: testExecSSHConfig(t, address)})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	result, err := mgr.ExecCommandChannel(context.Background(), session.ID, "echo dedicated", 5*time.Second)
	if err != nil {
		t.Fatalf("ExecCommandChannel() error = %v", err)
	}
	if result.Stdout != "OUT:echo dedicated\n" || result.ExitCode != 0 {
		t.Fatalf("unexpected channel result: %+v", result)
	}

	if _, err := mgr.ExecCommandChannel(context.Background(), "missing", "echo hi", time.Second); err == nil {
		t.Fatal("unknown session must fail closed")
	}
	if _, err := mgr.ExecCommandChannel(context.Background(), session.ID, "  ", time.Second); err == nil {
		t.Fatal("empty command must fail closed")
	}
}
