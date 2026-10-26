package remote

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHHostConfig_Defaults(t *testing.T) {
	cfg := SSHHostConfig{Host: "10.0.0.1", User: "root"}
	cfg.Defaults()

	if cfg.Port != 22 {
		t.Errorf("expected port 22, got %d", cfg.Port)
	}
	if cfg.ConnectTimeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", cfg.ConnectTimeout)
	}
	if cfg.KeepaliveInterval != 15*time.Second {
		t.Errorf("expected 15s keepalive, got %v", cfg.KeepaliveInterval)
	}
	if cfg.AuthMethod != "key" {
		t.Errorf("expected auth_method=key, got %s", cfg.AuthMethod)
	}
}

func TestSSHHostConfigDefaultsCanonicalizesEndpoint(t *testing.T) {
	cfg := SSHHostConfig{Host: "[2001:db8::1]", User: " root ", Label: " gpu "}
	cfg.Defaults()
	if cfg.Host != "2001:db8::1" || cfg.User != "root" || cfg.Label != "gpu" || cfg.Port != 22 {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.SSHHostID() != "root@2001:db8::1:22" {
		t.Fatalf("id = %s", cfg.SSHHostID())
	}
}

func TestSSHHostConfig_SSHHostID(t *testing.T) {
	tests := []struct {
		cfg    SSHHostConfig
		expect string
	}{
		{SSHHostConfig{Host: "10.0.0.1", User: "root", Port: 22}, "root@10.0.0.1:22"},
		{SSHHostConfig{Host: "web.example.com", User: "deploy", Port: 2222}, "deploy@web.example.com:2222"},
		{SSHHostConfig{Host: "10.0.0.1", User: "root"}, "root@10.0.0.1:22"}, // Port=0 → default 22
		{SSHHostConfig{Host: " 10.0.0.1 ", User: " root ", Port: 22}, "root@10.0.0.1:22"},
		{SSHHostConfig{Host: "[2001:db8::1]", User: "root", Port: 22}, "root@2001:db8::1:22"},
		{SSHHostConfig{Host: "2001:0db8:0000:0000:0000:0000:0000:0001", User: "root", Port: 22}, "root@2001:db8::1:22"},
		{SSHHostConfig{Host: " Web.Example ", User: "Deploy", Port: 2222}, "Deploy@Web.Example:2222"},
	}
	for _, tt := range tests {
		got := tt.cfg.SSHHostID()
		if got != tt.expect {
			t.Errorf("SSHHostID() = %q, want %q", got, tt.expect)
		}
	}
}

func TestSSHPoolKeySeparatesHostKeyPolicies(t *testing.T) {
	base := SSHHostConfig{Host: "example.com", User: "deploy", Port: 22}
	legacy := sshPoolKey(base)
	pinnedA := base
	pinnedA.HostKeyFingerprint = "SHA256:aaa"
	pinnedB := base
	pinnedB.HostKeyFingerprint = "SHA256:bbb"
	if legacy == sshPoolKey(pinnedA) || sshPoolKey(pinnedA) == sshPoolKey(pinnedB) {
		t.Fatalf("pool keys must isolate legacy and pinned policies: %q %q %q", legacy, sshPoolKey(pinnedA), sshPoolKey(pinnedB))
	}
}

func TestSSHPoolAcquireResolvedPinsCapturedHandshakeAndReusesIt(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestPasswordSSHServer(t, signer, "deploy", "correct-password")

	pool := NewSSHPool()
	var observed string
	host, _ := testSSHHostPort(t, address)
	requested := SSHHostConfig{
		Host:                      host,
		User:                      "deploy",
		Port:                      testSSHPort(t, address),
		Password:                  "correct-password",
		AuthMethod:                "password",
		ConnectTimeout:            2 * time.Second,
		HostKeyFingerprintCapture: func(fingerprint string) { observed = fingerprint },
	}
	first, resolved, err := pool.AcquireResolved(requested)
	if err != nil {
		t.Fatalf("AcquireResolved() error = %v", err)
	}
	if first == nil {
		t.Fatal("AcquireResolved() returned nil client")
	}
	wantFingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	if observed != wantFingerprint || resolved.HostKeyFingerprint != wantFingerprint {
		t.Fatalf("capture = %q, resolved pin = %q, want %q", observed, resolved.HostKeyFingerprint, wantFingerprint)
	}
	if resolved.HostKeyFingerprintCapture != nil {
		t.Fatal("resolved config retained runtime capture callback")
	}
	if _, legacyPresent := pool.conns[sshPoolKey(requested)]; legacyPresent {
		t.Fatal("captured connection was retained under legacy-unverified pool identity")
	}
	if _, pinnedPresent := pool.conns[sshPoolKey(resolved)]; !pinnedPresent {
		t.Fatal("captured connection was not retained under its pinned pool identity")
	}

	second, err := pool.Acquire(resolved)
	if err != nil {
		t.Fatalf("Acquire(resolved) error = %v", err)
	}
	if second != first {
		t.Fatal("pinned acquire did not reuse the authenticated captured connection")
	}
	pool.Release(resolved, first)
	pool.Release(resolved, first)
	if got := pool.Stats(); len(got) != 0 {
		t.Fatalf("pool still has entries after both releases: %#v", got)
	}
}

func TestAcquireReusesReferencedClientWhenProbeWouldFail(t *testing.T) {
	signer := testSSHSigner(t)
	address := startTestPasswordSSHServer(t, signer, "deploy", "correct-password")
	host, port := testSSHHostPort(t, address)
	cfg := SSHHostConfig{
		Host:           host,
		User:           "deploy",
		Port:           port,
		Password:       "correct-password",
		AuthMethod:     "password",
		ConnectTimeout: 2 * time.Second,
	}
	pool := NewSSHPool()
	t.Cleanup(pool.CloseAll)

	first, err := pool.acquire(cfg)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	key := sshPoolKey(cfg)
	pool.mu.Lock()
	held := pool.conns[key]
	pool.mu.Unlock()
	if held == nil || held.client != first || held.refCount != 1 {
		t.Fatalf("session ref = %#v", held)
	}

	// The TCP session is gone. A keepalive probe now fails, and closing the
	// pooled client here would drop the session that still holds a reference.
	if err := first.Close(); err != nil {
		t.Fatalf("close live client: %v", err)
	}

	second, err := pool.acquire(cfg)
	if err != nil {
		t.Fatalf("acquire() while referenced error = %v", err)
	}
	if second != first {
		t.Fatal("acquire replaced a connection that a session still references")
	}
	pool.mu.Lock()
	held = pool.conns[key]
	pool.mu.Unlock()
	if held == nil || held.client != first || held.refCount != 2 {
		t.Fatalf("after second acquire, entry = %#v", held)
	}

	pool.Release(cfg, first)
	pool.mu.Lock()
	held = pool.conns[key]
	pool.mu.Unlock()
	if held == nil || held.client != first || held.refCount != 1 {
		t.Fatal("releasing the extra reference closed the session connection")
	}
}

func TestReleaseOnlyDropsTheAcquiredClient(t *testing.T) {
	pool := NewSSHPool()
	cfg := SSHHostConfig{Host: "10.0.0.8", User: "deploy", Port: 22}
	cfg.Defaults()
	key := sshPoolKey(cfg)
	session := &ssh.Client{}
	replacement := &ssh.Client{}
	pool.mu.Lock()
	pool.conns[key] = &poolEntry{client: session, hostID: cfg.SSHHostID(), refCount: 2}
	pool.mu.Unlock()

	pool.Release(cfg, session)
	pool.mu.Lock()
	entry := pool.conns[key]
	pool.mu.Unlock()
	if entry == nil || entry.client != session || entry.refCount != 1 {
		t.Fatalf("matching release entry = %#v", entry)
	}

	pool.mu.Lock()
	entry.client = replacement
	entry.refCount = 1
	pool.mu.Unlock()

	pool.Release(cfg, session)
	pool.mu.Lock()
	entry = pool.conns[key]
	pool.mu.Unlock()
	if entry == nil || entry.client != replacement || entry.refCount != 1 {
		t.Fatalf("replacement entry = %#v", entry)
	}
}

func TestSSHPoolAcquireResolvedCapturesOnlyKnownHostsVerifiedKey(t *testing.T) {
	trusted := testSSHSigner(t)
	address := startTestPasswordSSHServer(t, trusted, "deploy", "correct-password")
	host, port := testSSHHostPort(t, address)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	knownHostLine := fmt.Sprintf("[%s]:%d %s", host, port, ssh.MarshalAuthorizedKey(trusted.PublicKey()))
	if err := os.WriteFile(knownHostsPath, []byte(knownHostLine), 0o600); err != nil {
		t.Fatal(err)
	}

	var observed string
	_, resolved, err := NewSSHPool().AcquireResolved(SSHHostConfig{
		Host:                      host,
		User:                      "deploy",
		Port:                      port,
		Password:                  "correct-password",
		AuthMethod:                "password",
		KnownHostsPath:            knownHostsPath,
		ConnectTimeout:            2 * time.Second,
		HostKeyFingerprintCapture: func(fingerprint string) { observed = fingerprint },
	})
	if err != nil {
		t.Fatalf("AcquireResolved() error = %v", err)
	}
	want := ssh.FingerprintSHA256(trusted.PublicKey())
	if observed != want || resolved.HostKeyFingerprint != want {
		t.Fatalf("capture = %q, resolved pin = %q, want %q", observed, resolved.HostKeyFingerprint, want)
	}
}

func startTestPasswordSSHServer(t *testing.T, signer ssh.Signer, user, password string) string {
	t.Helper()
	return startTestPasswordSSHServerCounted(t, signer, user, password, nil)
}

func startTestPasswordSSHServerCounted(t *testing.T, signer ssh.Signer, user, password string, dials *int32) string {
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
			if dials != nil {
				atomic.AddInt32(dials, 1)
			}
			go func() {
				serverConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
				if handshakeErr != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					_ = channel.Reject(ssh.UnknownChannelType, "test server accepts no channels")
				}
				_ = serverConn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

// startTestShellSSHServer accepts a shell so a replacement PTY can start
// on a connection the pool already holds.
func startTestShellSSHServer(t *testing.T, signer ssh.Signer, user, password string, dials *int32) string {
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
			if dials != nil {
				atomic.AddInt32(dials, 1)
			}
			go serveTestShell(conn, serverConfig)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func serveTestShell(conn net.Conn, cfg *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, reqs, acceptErr := newChannel.Accept()
		if acceptErr != nil {
			continue
		}
		go func() {
			for req := range reqs {
				switch req.Type {
				case "pty-req", "shell":
					req.Reply(true, nil)
				default:
					if req.WantReply {
						req.Reply(false, nil)
					}
				}
			}
		}()
		go func() { _, _ = io.Copy(io.Discard, channel) }()
	}
	_ = serverConn.Close()
}

func testSSHPort(t *testing.T, address string) int {
	t.Helper()
	_, port := testSSHHostPort(t, address)
	return port
}

func testSSHHostPort(t *testing.T, address string) (string, int) {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	var parsed int
	if _, err := fmt.Sscanf(port, "%d", &parsed); err != nil || parsed <= 0 {
		t.Fatalf("invalid test server port %q: %v", port, err)
	}
	return host, parsed
}

func TestSSHPoolStatsDoNotExposeHostKeyPolicy(t *testing.T) {
	publicHostID := "developer@example.com:22"
	pool := &SSHPool{conns: map[string]*poolEntry{
		publicHostID + "|fingerprint=SHA256:secret-pin": {
			hostID:   publicHostID,
			refCount: 2,
		},
		publicHostID + "|known-hosts=C:\\private\\known_hosts": {
			hostID:   publicHostID,
			refCount: 1,
		},
	}}

	stats := pool.Stats()
	if len(stats) != 1 || stats[publicHostID] != 3 {
		t.Fatalf("Stats() = %#v, want one aggregated public host entry", stats)
	}
}

func TestSSHHostKeyCallbackRejectsMismatchedPin(t *testing.T) {
	keyA, keyB := testSSHSigner(t), testSSHSigner(t)
	callback, err := sshHostKeyCallback(SSHHostConfig{HostKeyFingerprint: ssh.FingerprintSHA256(keyA.PublicKey())})
	if err != nil {
		t.Fatal(err)
	}
	if err := callback("example.com:22", nil, keyA.PublicKey()); err != nil {
		t.Fatalf("matching pin rejected: %v", err)
	}
	if err := callback("example.com:22", nil, keyB.PublicKey()); err == nil {
		t.Fatal("mismatched host key should be rejected")
	}
}

func testSSHSigner(t *testing.T) ssh.Signer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestSSHPool_NewAndStats(t *testing.T) {
	pool := NewSSHPool()
	stats := pool.Stats()
	if len(stats) != 0 {
		t.Errorf("new pool should have 0 connections, got %d", len(stats))
	}
}

func TestSSHSessionManager_GetNotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent session")
	}
}

func TestSSHSessionManager_GetSessionStatus_NotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	_, ok := mgr.GetSessionStatus("nonexistent")
	if ok {
		t.Error("expected GetSessionStatus to return false for nonexistent session")
	}
}

func TestSSHSessionManager_ListEmpty(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	list := mgr.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestSSHSessionManager_WriteInput_NotFound(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	err := mgr.WriteInput("nonexistent", "ls")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSSHSessionManager_WriteInputNeverReconnects(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	// This deliberately unusable handle is enough to exercise the direct
	// WriteInput path. In contrast to WriteInputChecked, it must return the
	// write failure without swapping the session handle or consulting the pool.
	handle := &SSHPTYSession{started: true}
	session := &SSHManagedSession{
		ID:     "bound",
		Status: SessionRunning,
		Spec: SSHSessionSpec{HostConfig: SSHHostConfig{
			Host: "build.example.test", User: "deploy", HostKeyFingerprint: "SHA256:pin",
		}},
		Handle: handle,
	}
	mgr.mu.Lock()
	mgr.sessions[session.ID] = session
	mgr.mu.Unlock()

	if err := mgr.WriteInput(session.ID, "git status"); err == nil {
		t.Fatal("expected direct write failure from unusable handle")
	}
	if session.Handle != handle {
		t.Fatal("WriteInput replaced the bound SSH session handle")
	}
}

func TestSSHReconnectReusesThePooledConnection(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestShellSSHServer(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	pool := NewSSHPool()
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&dials) != 1 {
		t.Fatalf("setup dials = %d", dials)
	}

	mgr := NewSSHSessionManager(pool)
	session := &SSHManagedSession{
		ID:     "ssh_shell_exited",
		Status: SessionExited,
		Spec:   SSHSessionSpec{HostConfig: cfg},
		Summary: SSHSessionSummary{
			HostID: cfg.SSHHostID(),
			Status: string(SessionExited),
		},
		Handle: &SSHPTYSession{
			client:   client,
			started:  true,
			outputCh: make(chan []byte, 1),
			exitCh:   make(chan PTYExit, 1),
			stopKA:   make(chan struct{}),
		},
	}
	mgr.mu.Lock()
	mgr.sessions[session.ID] = session
	mgr.mu.Unlock()
	exitedShell := session.Handle

	got, reconnected, retired := mgr.AttachOpenSession(cfg.SSHHostID(), "", "", "", false)
	if retired != "" || got != session || !reconnected {
		t.Fatalf("attach = %v reconnected=%v retired=%s", got, reconnected, retired)
	}
	if got.PTY() == exitedShell || SessionStatus(got.GetSummary().Status).IsOpen() == false {
		t.Fatal("exited shell was reused")
	}
	if atomic.LoadInt32(&dials) != 1 {
		t.Fatalf("replacement dials = %d, want the pooled connection", dials)
	}
	key := sshPoolKey(cfg)
	pool.mu.Lock()
	entry := pool.conns[key]
	pool.mu.Unlock()
	if entry == nil || entry.client != client || entry.refCount != 1 {
		t.Fatalf("pool entry = %#v, want the original client at ref 1", entry)
	}

	handle := got.PTY()
	again, againReconnected, againRetired := mgr.AttachOpenSession(cfg.SSHHostID(), "", "", "", false)
	if againRetired != "" || again != session || againReconnected || again.PTY() != handle {
		t.Fatalf("live shell attach = reconnected=%v retired=%s handle=%v", againReconnected, againRetired, again.PTY())
	}
	if atomic.LoadInt32(&dials) != 1 {
		t.Fatalf("live attach dials = %d", dials)
	}
}

func TestSSHReconnectDialsWhenThePooledClientCannotStartAShell(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestPasswordSSHServerCounted(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	pool := NewSSHPool()
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewSSHSessionManager(pool)
	session := &SSHManagedSession{
		ID:     "ssh_dead_client",
		Status: SessionExited,
		Spec:   SSHSessionSpec{HostConfig: cfg},
		Summary: SSHSessionSummary{
			HostID: cfg.SSHHostID(),
			Status: string(SessionExited),
		},
		Handle: &SSHPTYSession{
			client:   client,
			started:  true,
			outputCh: make(chan []byte, 1),
			exitCh:   make(chan PTYExit, 1),
			stopKA:   make(chan struct{}),
		},
	}
	if err := mgr.reconnectSession(session); err == nil {
		t.Fatal("a client that rejects shells was treated as the replacement PTY")
	}
	if got := atomic.LoadInt32(&dials); got != 2 {
		t.Fatalf("dials = %d, want the retained client plus one new connection", got)
	}
	if session.ID != "ssh_dead_client" {
		t.Fatalf("session id = %s", session.ID)
	}
	pool.mu.Lock()
	left := len(pool.conns)
	pool.mu.Unlock()
	if left != 0 {
		t.Fatalf("pool kept %d connections after the failed replacement", left)
	}
}

// The pool entry is already gone, which is what keepalive does when it drops
// a dead connection. Acquire dials the replacement. That dial's inability to
// start a shell is the result of this replacement, not another corpse to dial.
func TestSSHReconnectDialsOnceWhenThePooledClientIsAlreadyGone(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestPasswordSSHServerCounted(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	pool := NewSSHPool()
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool.Release(cfg, client)
	if atomic.LoadInt32(&dials) != 1 {
		t.Fatalf("setup dials = %d", dials)
	}

	mgr := NewSSHSessionManager(pool)
	session := &SSHManagedSession{
		ID:     "ssh_client_already_gone",
		Status: SessionExited,
		Spec:   SSHSessionSpec{HostConfig: cfg},
		Summary: SSHSessionSummary{
			HostID: cfg.SSHHostID(),
			Status: string(SessionExited),
		},
		Handle: &SSHPTYSession{
			client:   client,
			started:  true,
			outputCh: make(chan []byte, 1),
			exitCh:   make(chan PTYExit, 1),
			stopKA:   make(chan struct{}),
		},
	}
	if err := mgr.reconnectSession(session); err == nil {
		t.Fatal("a client that rejects shells was treated as the replacement PTY")
	}
	if got := atomic.LoadInt32(&dials); got != 2 {
		t.Fatalf("dials = %d, want the dropped connection plus one replacement", got)
	}
	if session.ID != "ssh_client_already_gone" {
		t.Fatalf("session id = %s", session.ID)
	}
	pool.mu.Lock()
	left := len(pool.conns)
	pool.mu.Unlock()
	if left != 0 {
		t.Fatalf("pool kept %d connections after the failed replacement", left)
	}
}

func TestCreateDialsOnceWhenTheRetainedClientCannotStartAShell(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestPasswordSSHServerCounted(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	pool := NewSSHPool()
	if _, err := pool.Acquire(cfg); err != nil {
		t.Fatal(err)
	}
	mgr := NewSSHSessionManager(pool)
	if _, err := mgr.Create(SSHSessionSpec{HostConfig: cfg}); err == nil {
		t.Fatal("a retained client that rejects shells was started")
	}
	if got := atomic.LoadInt32(&dials); got != 2 {
		t.Fatalf("dials = %d, want the retained client plus one replacement", got)
	}
	pool.mu.Lock()
	left := len(pool.conns)
	pool.mu.Unlock()
	if left != 0 {
		t.Fatalf("pool kept %d connections after the failed create", left)
	}
}

func TestCreateDoesNotReplaceAClientItJustDialed(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestPasswordSSHServerCounted(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	mgr := NewSSHSessionManager(NewSSHPool())
	if _, err := mgr.Create(SSHSessionSpec{HostConfig: cfg}); err == nil {
		t.Fatal("a new client that rejects shells was started")
	}
	if got := atomic.LoadInt32(&dials); got != 1 {
		t.Fatalf("dials = %d, want the one connection this create dialed", got)
	}
}

func TestCreateReusesARetainedLiveConnection(t *testing.T) {
	signer := testSSHSigner(t)
	var dials int32
	addr := startTestShellSSHServer(t, signer, "root", "secret", &dials)
	host, port := testSSHHostPort(t, addr)
	cfg := SSHHostConfig{
		Host:               host,
		User:               "root",
		Port:               port,
		AuthMethod:         "password",
		Password:           "secret",
		ConnectTimeout:     2 * time.Second,
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	}
	cfg.Defaults()
	pool := NewSSHPool()
	client, err := pool.Acquire(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewSSHSessionManager(pool)
	session, err := mgr.Create(SSHSessionSpec{HostConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if session.PTY() == nil || !SessionStatus(session.GetSummary().Status).IsOpen() {
		t.Fatal("create did not publish an open shell")
	}
	if atomic.LoadInt32(&dials) != 1 {
		t.Fatalf("dials = %d, want the retained connection", dials)
	}
	pool.mu.Lock()
	entry := pool.conns[sshPoolKey(cfg)]
	pool.mu.Unlock()
	if entry == nil || entry.client != client || entry.refCount != 2 {
		t.Fatalf("pool entry = %#v, want the original client at ref 2", entry)
	}
}

func TestNormalizeSSHPTYSize(t *testing.T) {
	cols, rows := normalizeSSHPTYSize(0, 0)
	if cols != 120 || rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", cols, rows)
	}
	cols, rows = normalizeSSHPTYSize(80, 24)
	if cols != 80 || rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", cols, rows)
	}
}

func TestSplitSSHOutputLines(t *testing.T) {
	lines := splitSSHOutputLines([]byte("hello\nworld\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("unexpected lines: %v", lines)
	}

	lines = splitSSHOutputLines([]byte("no newline"))
	if len(lines) != 1 || lines[0] != "no newline" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestSplitCompleteSSHLines(t *testing.T) {
	complete, rem := splitCompleteSSHLines("hello\nworld\nEXIT: 0")
	if len(complete) != 2 || complete[0] != "hello" || complete[1] != "world" {
		t.Fatalf("complete=%v", complete)
	}
	if rem != "EXIT: 0" {
		t.Fatalf("remainder=%q", rem)
	}

	// CRLF
	complete, rem = splitCompleteSSHLines("a\r\nb\r\n")
	if len(complete) != 2 || complete[0] != "a" || complete[1] != "b" || rem != "" {
		t.Fatalf("crlf complete=%v rem=%q", complete, rem)
	}

	// half-line only
	complete, rem = splitCompleteSSHLines("partial")
	if len(complete) != 0 || rem != "partial" {
		t.Fatalf("half complete=%v rem=%q", complete, rem)
	}
}

func TestHasCommandExitMarker(t *testing.T) {
	if !hasCommandExitMarker([]string{"output", "EXIT: 0"}) {
		t.Fatal("expected EXIT: 0 detected")
	}
	if !hasCommandExitMarker([]string{"EXIT:127"}) {
		t.Fatal("expected EXIT:127 detected")
	}
	if hasCommandExitMarker([]string{"no exit here", "price EXIT: never"}) {
		t.Fatal("should not match non-numeric EXIT")
	}
	if hasCommandExitMarker([]string{"just output"}) {
		t.Fatal("should not match without EXIT")
	}
	if hasCommandExitMarker([]string{"EXIT: 0xdead"}) {
		t.Fatal("should reject non-decimal EXIT codes")
	}
	// only inspect tail
	old := make([]string, 20)
	for i := range old {
		old[i] = "noise"
	}
	old = append(old, "EXIT: 0")
	if !hasCommandExitMarker(old) {
		t.Fatal("expected tail EXIT detected")
	}
}
