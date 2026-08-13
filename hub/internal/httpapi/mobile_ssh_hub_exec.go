package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"golang.org/x/crypto/ssh"
)

// Hub-side SSH execution for exec_mode=hub_exec (Phase C/D).
// Does not require desktop GUI online when vault credentials are present.

const mobileHubSSHDialTimeout = 12 * time.Second
const mobileHubSSHRunTimeout = 90 * time.Second
const mobileHubSSHLiveIdle = 10 * time.Minute
const mobileHubSSHInputTimeout = 45 * time.Second

// Soft single-shot base64 size before switching to chunked pull (internal).
const mobileHubFileSingleShotBytes = 2 * 1024 * 1024
const mobileHubFileChunkRawBytes = 512 * 1024 // 512KiB per dd chunk
const mobileHubFileTTL = 15 * time.Minute

// mobileHubFileMaxBytes is the absolute hub_exec download cap (chunked allowed).
func mobileHubFileMaxBytes() int {
	n := mobileCapHubFileDownloadBytes()
	if n <= 0 {
		return int(mobileCapHubFileDownloadDefault)
	}
	if n > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(n)
}

// Live hub_exec resources keyed by mobile session id.
// - client: reused TCP/SSH connection for one-shot Run
// - shell: optional interactive shell for sequential inputs (cwd/env retained)
var mobileHubLive = struct {
	sync.Mutex
	conns  map[string]*mobileHubLiveConn
	shells map[string]*mobileHubLiveShell
}{
	conns:  make(map[string]*mobileHubLiveConn),
	shells: make(map[string]*mobileHubLiveShell),
}

type mobileHubLiveConn struct {
	client   *ssh.Client
	lastUsed time.Time
}

type mobileHubLiveShell struct {
	client   *ssh.Client
	session  *ssh.Session
	stdin    io.WriteCloser
	stdout   io.Reader
	stderr   io.Reader
	lastUsed time.Time
	mu       sync.Mutex
}

func init() {
	go mobileHubLiveGCLoop()
	go mobileHubFilesGCLoop()
}

func mobileHubLiveGCLoop() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for range t.C {
		mobileHubLiveGC()
	}
}

func mobileHubLiveGC() {
	now := time.Now()
	mobileHubLive.Lock()
	defer mobileHubLive.Unlock()
	for id, c := range mobileHubLive.conns {
		if c == nil || now.Sub(c.lastUsed) > mobileHubSSHLiveIdle {
			if c != nil && c.client != nil {
				_ = c.client.Close()
			}
			delete(mobileHubLive.conns, id)
		}
	}
	for id, s := range mobileHubLive.shells {
		if s == nil || now.Sub(s.lastUsed) > mobileHubSSHLiveIdle {
			mobileHubLiveShellCloseLocked(id, s)
		}
	}
}

func mobileHubLiveShellCloseLocked(id string, s *mobileHubLiveShell) {
	if s == nil {
		delete(mobileHubLive.shells, id)
		return
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.session != nil {
		_ = s.session.Close()
	}
	// client may be shared with conns entry; close only if not tracked separately
	if c, ok := mobileHubLive.conns[id]; !ok || c == nil || c.client != s.client {
		if s.client != nil {
			_ = s.client.Close()
		}
	}
	delete(mobileHubLive.shells, id)
}

func mobileHubLiveCloseSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	mobileHubLive.Lock()
	defer mobileHubLive.Unlock()
	if s, ok := mobileHubLive.shells[sessionID]; ok {
		mobileHubLiveShellCloseLocked(sessionID, s)
	}
	if c, ok := mobileHubLive.conns[sessionID]; ok {
		if c != nil && c.client != nil {
			_ = c.client.Close()
		}
		delete(mobileHubLive.conns, sessionID)
	}
}

// In-flight hub_exec background tasks keyed by task id (cancel closes remote session.Run).
var mobileHubTaskRuns = struct {
	sync.Mutex
	cancels map[string]context.CancelFunc
}{
	cancels: make(map[string]context.CancelFunc),
}

func mobileHubTaskRegister(taskID string, cancel context.CancelFunc) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || cancel == nil {
		return
	}
	mobileHubTaskRuns.Lock()
	if prev, ok := mobileHubTaskRuns.cancels[taskID]; ok && prev != nil {
		prev()
	}
	mobileHubTaskRuns.cancels[taskID] = cancel
	mobileHubTaskRuns.Unlock()
}

func mobileHubTaskUnregister(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	mobileHubTaskRuns.Lock()
	delete(mobileHubTaskRuns.cancels, taskID)
	mobileHubTaskRuns.Unlock()
}

// mobileHubTaskCancel signals an in-flight hub_exec task to stop (if running).
func mobileHubTaskCancel(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	mobileHubTaskRuns.Lock()
	cancel, ok := mobileHubTaskRuns.cancels[taskID]
	mobileHubTaskRuns.Unlock()
	if !ok || cancel == nil {
		return false
	}
	cancel()
	return true
}

func mobileShellSingleQuote(s string) string {
	// POSIX-safe single-quote wrapping for remote paths/commands.
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Short-lived Hub-side file blobs for hub_exec download (token → bytes).
type mobileHubFileBlob struct {
	Token     string
	TenantID  string
	OwnerID   string
	Filename  string
	Content   []byte
	ExpiresAt time.Time
}

var mobileHubFiles = struct {
	sync.Mutex
	blobs map[string]mobileHubFileBlob
}{
	blobs: make(map[string]mobileHubFileBlob),
}

func mobileHubFilesGCLoop() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for range t.C {
		mobileHubFilesGC()
	}
}

func mobileHubFilesGC() {
	now := time.Now()
	mobileHubFiles.Lock()
	defer mobileHubFiles.Unlock()
	for id, b := range mobileHubFiles.blobs {
		if now.After(b.ExpiresAt) {
			delete(mobileHubFiles.blobs, id)
		}
	}
}

func mobileHubFileStore(tenantID, ownerID, filename string, content []byte) (token string, err error) {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if !mobileOwnerWriteAllowedLocked(tenantID, ownerID) {
		return "", fmt.Errorf("file owner is no longer available")
	}
	mobileHubFiles.Lock()
	defer mobileHubFiles.Unlock()
	return mobileHubFileStoreLocked(tenantID, ownerID, filename, content)
}

// mobileHubFileStoreForOperation serializes final download publication with
// user cleanup. Holding the operation lock until the blob is registered means
// a purge either sees and removes the blob, or removes the operation first and
// prevents publication altogether.
func mobileHubFileStoreForOperation(op *mobileBackendSSHFileOperationRecord, filename string, content []byte) (token string, err error) {
	if op == nil {
		return "", fmt.Errorf("file operation missing")
	}
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if !mobileOwnerWriteAllowedLocked(op.TenantID, op.OwnerID) {
		return "", fmt.Errorf("file operation owner is no longer available")
	}
	mobileBackendSSHFileOperations.Lock()
	defer mobileBackendSSHFileOperations.Unlock()
	existing, ok := mobileBackendSSHFileOperations.operations[op.OperationID]
	if !ok || existing.OwnerID != op.OwnerID || existing.TenantID != op.TenantID {
		return "", fmt.Errorf("file operation no longer exists")
	}
	mobileHubFiles.Lock()
	defer mobileHubFiles.Unlock()
	return mobileHubFileStoreLocked(op.TenantID, op.OwnerID, filename, content)
}

// mobileHubFileStoreLocked stores a download blob while mobileHubFiles is
// locked. Callers that need account-deletion serialization may hold an
// operation lock around it.
func mobileHubFileStoreLocked(tenantID, ownerID, filename string, content []byte) (token string, err error) {
	if len(content) == 0 {
		return "", fmt.Errorf("empty file")
	}
	if len(content) > mobileHubFileMaxBytes() {
		return "", fmt.Errorf("file exceeds hub_exec download limit (%d bytes)", mobileHubFileMaxBytes())
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token = hex.EncodeToString(raw[:])
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "download.bin"
	}
	mobileHubFiles.blobs[token] = mobileHubFileBlob{
		Token:     token,
		TenantID:  tenantID,
		OwnerID:   ownerID,
		Filename:  path.Base(name),
		Content:   append([]byte(nil), content...),
		ExpiresAt: time.Now().Add(mobileHubFileTTL),
	}
	return token, nil
}

func mobileHubFileLookup(token, tenantID, ownerID string) (mobileHubFileBlob, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return mobileHubFileBlob{}, false
	}
	mobileHubFiles.Lock()
	defer mobileHubFiles.Unlock()
	b, ok := mobileHubFiles.blobs[token]
	if !ok {
		return mobileHubFileBlob{}, false
	}
	if time.Now().After(b.ExpiresAt) {
		delete(mobileHubFiles.blobs, token)
		return mobileHubFileBlob{}, false
	}
	if b.OwnerID != ownerID || b.TenantID != tenantID {
		return mobileHubFileBlob{}, false
	}
	return b, true
}

// MobileHubSSHFileDownloadHandler serves short-lived hub_exec download blobs.
//
//	GET /api/mobile/ssh/files/download/{token}
func MobileHubSSHFileDownloadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if token == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "token is required")
			return
		}
		blob, ok := mobileHubFileLookup(token, principal.TenantID, principal.UserID)
		if !ok {
			writeError(w, http.StatusNotFound, "FILE_NOT_FOUND", "download expired or not found")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(blob.Filename, `"`, "")+`"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(blob.Content)))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(blob.Content)
	}
}

func mobileHubSSHDial(profile mobileServerProfileRecord, vault mobileSSHVaultRecord) (*ssh.Client, error) {
	secret := mobileSSHVaultDecrypt(vault.EncryptedSecret)
	if secret == "" {
		return nil, fmt.Errorf("vault secret unavailable")
	}
	passphrase := mobileSSHVaultDecrypt(vault.EncryptedPassphrase)
	port := profile.Port
	if port <= 0 {
		port = 22
	}
	var auths []ssh.AuthMethod
	switch vault.AuthMode {
	case "private_key":
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	default:
		auths = append(auths, ssh.Password(secret))
		auths = append(auths, ssh.KeyboardInteractive(
			func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = secret
				}
				return answers, nil
			},
		))
	}
	cfg := &ssh.ClientConfig{
		User:            profile.Username,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // vault profiles are user-managed emergency hosts
		Timeout:         mobileHubSSHDialTimeout,
	}
	addr := net.JoinHostPort(profile.Host, fmt.Sprintf("%d", port))
	return ssh.Dial("tcp", addr, cfg)
}

func mobileHubLiveGetOrDial(sessionID string, profile mobileServerProfileRecord, vault mobileSSHVaultRecord) (*ssh.Client, error) {
	sessionID = strings.TrimSpace(sessionID)
	mobileHubLive.Lock()
	if c, ok := mobileHubLive.conns[sessionID]; ok && c != nil && c.client != nil {
		c.lastUsed = time.Now()
		client := c.client
		mobileHubLive.Unlock()
		return client, nil
	}
	mobileHubLive.Unlock()

	client, err := mobileHubSSHDial(profile, vault)
	if err != nil {
		return nil, err
	}
	mobileHubLive.Lock()
	// Race: another goroutine may have dialed; prefer existing.
	if c, ok := mobileHubLive.conns[sessionID]; ok && c != nil && c.client != nil {
		_ = client.Close()
		c.lastUsed = time.Now()
		existing := c.client
		mobileHubLive.Unlock()
		return existing, nil
	}
	mobileHubLive.conns[sessionID] = &mobileHubLiveConn{client: client, lastUsed: time.Now()}
	mobileHubLive.Unlock()
	return client, nil
}

func mobileHubSSHRunCommandOnClient(client *ssh.Client, command string, timeout time.Duration) (string, int, error) {
	return mobileHubSSHRunCommandOnClientCtx(context.Background(), client, command, timeout)
}

// mobileHubPartialWriter buffers remote stdout/stderr and emits size/time-based chunks
// for progressive hub_exec feedback (realtime session output).
type mobileHubPartialWriter struct {
	mu          sync.Mutex
	buf         bytes.Buffer
	lastFlush   time.Time
	minInterval time.Duration
	minBytes    int
	onPartial   func(chunk string)
	// full keeps the complete stream for the final return value.
	full bytes.Buffer
}

func newMobileHubPartialWriter(onPartial func(string)) *mobileHubPartialWriter {
	return &mobileHubPartialWriter{
		// Slightly coarser defaults reduce WS flood on chatty tools (journalctl, find).
		minInterval: 750 * time.Millisecond,
		minBytes:    256,
		onPartial:   onPartial,
		lastFlush:   time.Now(),
	}
}

func (w *mobileHubPartialWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	w.mu.Lock()
	_, _ = w.full.Write(p)
	_, _ = w.buf.Write(p)
	var chunk string
	if w.buf.Len() >= w.minBytes && time.Since(w.lastFlush) >= w.minInterval {
		chunk = w.buf.String()
		w.buf.Reset()
		w.lastFlush = time.Now()
	}
	w.mu.Unlock()
	if chunk != "" && w.onPartial != nil {
		w.onPartial(chunk)
	}
	return len(p), nil
}

func (w *mobileHubPartialWriter) FlushPartial() {
	if w == nil {
		return
	}
	w.mu.Lock()
	chunk := w.buf.String()
	w.buf.Reset()
	w.lastFlush = time.Now()
	w.mu.Unlock()
	if chunk != "" && w.onPartial != nil {
		w.onPartial(chunk)
	}
}

func (w *mobileHubPartialWriter) String() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.full.String()
}

func mobileHubSSHRunCommandOnClientCtx(ctx context.Context, client *ssh.Client, command string, timeout time.Duration) (string, int, error) {
	return mobileHubSSHRunCommandOnClientCtxPartial(ctx, client, command, timeout, nil)
}

func mobileHubSSHRunCommandOnClientCtxPartial(
	ctx context.Context,
	client *ssh.Client,
	command string,
	timeout time.Duration,
	onPartial func(chunk string),
) (string, int, error) {
	if client == nil {
		return "", -1, fmt.Errorf("nil ssh client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", -1, fmt.Errorf("command is required")
	}
	if timeout <= 0 {
		timeout = mobileHubSSHRunTimeout
	}
	session, err := client.NewSession()
	if err != nil {
		return "", -1, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	stdout := newMobileHubPartialWriter(onPartial)
	stderr := newMobileHubPartialWriter(onPartial)
	session.Stdout = stdout
	session.Stderr = stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	combine := func() string {
		stdout.FlushPartial()
		stderr.FlushPartial()
		out := strings.TrimSpace(stdout.String())
		errOut := strings.TrimSpace(stderr.String())
		if errOut != "" {
			if out != "" {
				return out + "\n" + errOut
			}
			return errOut
		}
		return out
	}

	select {
	case err := <-done:
		out := combine()
		code := 0
		if err != nil {
			if ee, ok := err.(*ssh.ExitError); ok {
				code = ee.ExitStatus()
			} else {
				return mobileClipRunes(out, 12000), -1, err
			}
		}
		return mobileClipRunes(out, 12000), code, nil
	case <-timer.C:
		_ = session.Close()
		_ = combine()
		return "", -1, fmt.Errorf("command timed out after %s", timeout)
	case <-ctx.Done():
		_ = session.Close()
		out := combine()
		return mobileClipRunes(out, 12000), -1, fmt.Errorf("command cancelled")
	}
}

func mobileHubSSHRunCommand(profile mobileServerProfileRecord, vault mobileSSHVaultRecord, command string, timeout time.Duration) (string, int, error) {
	client, err := mobileHubSSHDial(profile, vault)
	if err != nil {
		return "", -1, err
	}
	defer client.Close()
	return mobileHubSSHRunCommandOnClient(client, command, timeout)
}

func mobileHubSSHRunCommandForSession(sessionID string, profile mobileServerProfileRecord, vault mobileSSHVaultRecord, command string, timeout time.Duration) (string, int, error) {
	return mobileHubSSHRunCommandForSessionCtx(context.Background(), sessionID, profile, vault, command, timeout)
}

func mobileHubSSHRunCommandForSessionCtx(ctx context.Context, sessionID string, profile mobileServerProfileRecord, vault mobileSSHVaultRecord, command string, timeout time.Duration) (string, int, error) {
	return mobileHubSSHRunCommandForSessionCtxPartial(ctx, sessionID, profile, vault, command, timeout, nil)
}

func mobileHubSSHRunCommandForSessionCtxPartial(
	ctx context.Context,
	sessionID string,
	profile mobileServerProfileRecord,
	vault mobileSSHVaultRecord,
	command string,
	timeout time.Duration,
	onPartial func(chunk string),
) (string, int, error) {
	client, err := mobileHubLiveGetOrDial(sessionID, profile, vault)
	if err != nil {
		return "", -1, err
	}
	out, code, runErr := mobileHubSSHRunCommandOnClientCtxPartial(ctx, client, command, timeout, onPartial)
	if runErr != nil && strings.Contains(runErr.Error(), "use of closed network connection") {
		// Drop dead connection and retry once (unless cancelled).
		if ctx.Err() != nil {
			return out, code, runErr
		}
		mobileHubLiveCloseSession(sessionID)
		client, err = mobileHubLiveGetOrDial(sessionID, profile, vault)
		if err != nil {
			return "", -1, err
		}
		return mobileHubSSHRunCommandOnClientCtxPartial(ctx, client, command, timeout, onPartial)
	}
	return out, code, runErr
}

// mobileHubSSHAppendSessionOutputChunk pushes progressive output onto the mobile
// session transcript and broadcasts a realtime chunk (hub_exec long tasks).
func mobileHubSSHAppendSessionOutputChunk(sessionID, chunk string) {
	chunk = strings.TrimRight(chunk, "\x00")
	if strings.TrimSpace(chunk) == "" {
		return
	}
	// Avoid flooding: clip each chunk for the wire payload.
	wire := mobileClipRunes(chunk, 4000)
	mobileKnowledgePurgeState.RLock()
	mobileBackendSSHSessions.Lock()
	sess, ok := mobileBackendSSHSessions.sessions[sessionID]
	if !ok || !mobileOwnerWriteAllowedLocked(sess.TenantID, sess.OwnerID) {
		mobileBackendSSHSessions.Unlock()
		mobileKnowledgePurgeState.RUnlock()
		return
	}
	sess.RecentOutput = mobileClipRunes(sess.RecentOutput+wire, 8000)
	sess.OutputChunk = wire
	sess.OutputSeq++
	sess.UpdatedAt = time.Now().UTC()
	if sess.Status == "ready" || sess.Status == "" {
		sess.Status = "running"
		sess.State = "hub_streaming"
		sess.Message = "hub_exec streaming output"
	}
	mobileBackendSSHSessions.sessions[sessionID] = sess
	payload := mobileBackendSSHSessionPayload(sess)
	tenantID, ownerID := sess.TenantID, sess.OwnerID
	mobileBackendSSHSessions.Unlock()
	mobileKnowledgePurgeState.RUnlock()
	mobileRealtimeBroadcast(tenantID, ownerID, mobileRealtimeBackendSSHSessionEvent(payload))
}

// mobileHubLiveEnsureShell opens (or reuses) an interactive shell for sequential inputs.
func mobileHubLiveEnsureShell(sessionID string, profile mobileServerProfileRecord, vault mobileSSHVaultRecord) (*mobileHubLiveShell, error) {
	sessionID = strings.TrimSpace(sessionID)
	mobileHubLive.Lock()
	if s, ok := mobileHubLive.shells[sessionID]; ok && s != nil {
		s.lastUsed = time.Now()
		mobileHubLive.Unlock()
		return s, nil
	}
	mobileHubLive.Unlock()

	client, err := mobileHubLiveGetOrDial(sessionID, profile, vault)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new shell session: %w", err)
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm", 24, 80, modes); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	live := &mobileHubLiveShell{
		client:   client,
		session:  sess,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		lastUsed: time.Now(),
	}
	// Drain initial banner/prompt briefly so first command output is cleaner.
	_, _ = mobileHubLiveShellRead(live, 800*time.Millisecond)

	mobileHubLive.Lock()
	if existing, ok := mobileHubLive.shells[sessionID]; ok && existing != nil {
		// Another goroutine won; discard ours.
		mobileHubLive.Unlock()
		_ = stdin.Close()
		_ = sess.Close()
		return existing, nil
	}
	mobileHubLive.shells[sessionID] = live
	mobileHubLive.Unlock()
	return live, nil
}

func mobileHubLiveShellRead(shell *mobileHubLiveShell, timeout time.Duration) (string, error) {
	return mobileHubLiveShellReadPartial(shell, timeout, nil)
}

// mobileHubLiveShellReadPartial drains shell stdout/stderr until quiet or timeout.
// onPartial is invoked with each non-empty read for progressive realtime.
func mobileHubLiveShellReadPartial(shell *mobileHubLiveShell, timeout time.Duration, onPartial func(chunk string)) (string, error) {
	if shell == nil {
		return "", fmt.Errorf("nil shell")
	}
	deadline := time.Now().Add(timeout)
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = setReadDeadlineIfPossible(shell.stdout, 200*time.Millisecond)
		n, err := shell.stdout.Read(tmp)
		if n > 0 {
			piece := string(tmp[:n])
			buf.Write(tmp[:n])
			if onPartial != nil {
				onPartial(piece)
			}
			// Keep reading while data arrives quickly.
			deadline = time.Now().Add(350 * time.Millisecond)
			continue
		}
		if err == io.EOF {
			break
		}
		// timeout / temporary: try stderr too then continue until outer deadline
		_ = setReadDeadlineIfPossible(shell.stderr, 50*time.Millisecond)
		if n2, err2 := shell.stderr.Read(tmp); n2 > 0 {
			piece := string(tmp[:n2])
			buf.Write(tmp[:n2])
			if onPartial != nil {
				onPartial(piece)
			}
			deadline = time.Now().Add(350 * time.Millisecond)
			_ = err2
			continue
		}
	}
	return strings.TrimSpace(buf.String()), nil
}

// setReadDeadlineIfPossible is a no-op for plain io.Reader pipes from ssh
// (they don't support deadlines). Kept for future net.Conn-backed readers.
func setReadDeadlineIfPossible(_ io.Reader, _ time.Duration) error { return nil }

func mobileHubLiveShellExec(sessionID string, profile mobileServerProfileRecord, vault mobileSSHVaultRecord, input string, raw bool) (string, error) {
	shell, err := mobileHubLiveEnsureShell(sessionID, profile, vault)
	if err != nil {
		return "", err
	}
	shell.mu.Lock()
	defer shell.mu.Unlock()
	shell.lastUsed = time.Now()
	// Drain any pending output before sending (discard banner noise).
	_, _ = mobileHubLiveShellRead(shell, 150*time.Millisecond)
	if !raw && !strings.HasSuffix(input, "\n") {
		input += "\n"
	}
	if _, err := io.WriteString(shell.stdin, input); err != nil {
		// Shell died; drop and surface error so caller can fall back.
		mobileHubLiveCloseSession(sessionID)
		return "", fmt.Errorf("write shell: %w", err)
	}
	// Raw control (e.g. Ctrl-C) often produces a short burst; line mode may wait longer.
	readWait := mobileHubSSHInputTimeout
	if raw {
		readWait = 1500 * time.Millisecond
	}
	// Progressive push so mobile terminal updates before the HTTP response returns.
	out, _ := mobileHubLiveShellReadPartial(shell, readWait, func(chunk string) {
		mobileHubSSHAppendSessionOutputChunk(sessionID, chunk)
	})
	return mobileClipRunes(out, 12000), nil
}

// mobileHubSSHInterruptSession best-effort interrupts a live interactive shell
// (Ctrl-C). Keeps the TCP connection when possible so subsequent inputs work.
func mobileHubSSHInterruptSession(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	mobileHubLive.Lock()
	shell, ok := mobileHubLive.shells[sessionID]
	mobileHubLive.Unlock()
	if !ok || shell == nil {
		// No interactive shell — drop any stale live resources so next input reopens cleanly.
		return "no interactive shell; live state cleared for next command", nil
	}
	shell.mu.Lock()
	defer shell.mu.Unlock()
	shell.lastUsed = time.Now()
	// SIGINT to remote process group via Ctrl-C.
	if _, err := io.WriteString(shell.stdin, "\x03"); err != nil {
		mobileHubLiveCloseSession(sessionID)
		return "", fmt.Errorf("interrupt shell: %w", err)
	}
	out, _ := mobileHubLiveShellRead(shell, 1500*time.Millisecond)
	if out == "" {
		out = "^C"
	}
	return mobileClipRunes(out, 4000), nil
}

// mobileHubSSHReconnectSession tears down live resources and re-probes + shell.
func mobileHubSSHReconnectSession(record *mobileBackendSSHSessionRecord, principalTenant, principalUser string) error {
	if record == nil {
		return fmt.Errorf("nil session")
	}
	mobileHubLiveCloseSession(record.SessionID)
	return mobileStartHubSSHSession(record, principalTenant, principalUser)
}

// mobileStartHubSSHSession validates vault + profile and marks the session
// connected under Hub execution (no desktop worker required).
func mobileStartHubSSHSession(record *mobileBackendSSHSessionRecord, principalTenant, principalUser string) error {
	profile, ok := mobileFindServerProfile(principalTenant, principalUser, record.ServerProfileID)
	if !ok {
		return fmt.Errorf("server profile not found; sync profiles from desktop or create metadata first")
	}
	vault, ok := mobileSSHVaultLookup(principalTenant, principalUser, record.ServerProfileID)
	if !ok {
		return fmt.Errorf("no Hub vault secret for profile; PUT /api/mobile/ssh/vault/{profileId} first")
	}
	// Connectivity probe + warm live connection / shell.
	out, code, err := mobileHubSSHRunCommandForSession(record.SessionID, profile, vault, "echo maclaw_hub_exec_ok", 15*time.Second)
	if err != nil {
		mobileHubLiveCloseSession(record.SessionID)
		return fmt.Errorf("hub ssh connect failed: %w", err)
	}
	if code != 0 && !strings.Contains(out, "maclaw_hub_exec_ok") {
		mobileHubLiveCloseSession(record.SessionID)
		return fmt.Errorf("hub ssh probe failed (exit %d): %s", code, out)
	}
	// Best-effort open interactive shell for subsequent inputs (cwd/env retained).
	if _, shellErr := mobileHubLiveEnsureShell(record.SessionID, profile, vault); shellErr != nil {
		// Connection still usable for one-shot Run; shell is optional.
		out = out + "\n[interactive shell unavailable: " + shellErr.Error() + "]"
	}
	now := time.Now().UTC()
	record.Status = "ready"
	record.State = "hub_connected"
	record.Message = "Hub is executing SSH directly (exec_mode=hub_exec; live connection + interactive shell when available)."
	record.BackendSessionID = "hub-exec:" + record.SessionID
	record.ClaimedBy = "hub"
	record.RecentOutput = "hub_exec ready\n" + out + "\n"
	record.UpdatedAt = now
	return nil
}

// mobileHubSSHTaskShouldAsync runs long-looking commands off the request path.
// Short interactive probes stay synchronous for snappy mobile UX.
func mobileHubSSHTaskShouldAsync(command string, forceAsync bool) bool {
	if forceAsync {
		return true
	}
	cmd := strings.TrimSpace(command)
	if len(cmd) > 240 {
		return true
	}
	lower := strings.ToLower(cmd)
	// Heuristics: pipes/loops/long tools often exceed interactive wait budgets.
	for _, token := range []string{"|", "&&", ";", "for ", "while ", "sleep ", "tar ", "rsync ", "find ", "docker ", "journalctl "} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// mobileUpdateHubSSHTaskIfPresent keeps a background task from recreating its
// row after user cleanup has removed it from the in-memory queue.
func mobileUpdateHubSSHTaskIfPresent(task *mobileBackendSSHTaskRecord) bool {
	if task == nil {
		return false
	}
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	mobileBackendSSHTasks.Lock()
	defer mobileBackendSSHTasks.Unlock()
	if !mobileOwnerWriteAllowedLocked(task.TenantID, task.OwnerID) {
		return false
	}
	existing, ok := mobileBackendSSHTasks.tasks[task.TaskID]
	if !ok || existing.OwnerID != task.OwnerID || existing.TenantID != task.TenantID {
		return false
	}
	mobileBackendSSHTasks.tasks[task.TaskID] = *task
	return true
}

// mobileUpdateHubSSHFileOperationIfPresent has the same deletion-race guard
// for potentially long running hub_exec file transfers.
func mobileUpdateHubSSHFileOperationIfPresent(op *mobileBackendSSHFileOperationRecord) bool {
	if op == nil {
		return false
	}
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	mobileBackendSSHFileOperations.Lock()
	defer mobileBackendSSHFileOperations.Unlock()
	if !mobileOwnerWriteAllowedLocked(op.TenantID, op.OwnerID) {
		return false
	}
	existing, ok := mobileBackendSSHFileOperations.operations[op.OperationID]
	if !ok || existing.OwnerID != op.OwnerID || existing.TenantID != op.TenantID {
		return false
	}
	mobileBackendSSHFileOperations.operations[op.OperationID] = *op
	return true
}

func mobileRunHubSSHTask(task *mobileBackendSSHTaskRecord, session mobileBackendSSHSessionRecord) {
	now := time.Now().UTC()
	task.Status = "running"
	task.Message = "running on Hub"
	task.UpdatedAt = now
	mobileKnowledgePurgeState.RLock()
	mobileBackendSSHTasks.Lock()
	// Respect kill requested before start.
	existing, ok := mobileBackendSSHTasks.tasks[task.TaskID]
	if !ok || existing.OwnerID != task.OwnerID || existing.TenantID != task.TenantID ||
		strings.EqualFold(existing.Status, "kill_requested") || strings.EqualFold(existing.Status, "cancelled") {
		mobileBackendSSHTasks.Unlock()
		mobileKnowledgePurgeState.RUnlock()
		return
	}
	if !mobileOwnerWriteAllowedLocked(task.TenantID, task.OwnerID) {
		mobileBackendSSHTasks.Unlock()
		mobileKnowledgePurgeState.RUnlock()
		return
	}
	mobileBackendSSHTasks.tasks[task.TaskID] = *task
	mobileBackendSSHTasks.Unlock()
	mobileKnowledgePurgeState.RUnlock()

	ctx, cancel := context.WithCancel(context.Background())
	mobileHubTaskRegister(task.TaskID, cancel)
	defer func() {
		cancel()
		mobileHubTaskUnregister(task.TaskID)
	}()

	profile, ok := mobileFindServerProfile(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		task.Status = "failed"
		task.Message = "server profile not found"
		task.UpdatedAt = time.Now().UTC()
		mobileUpdateHubSSHTaskIfPresent(task)
		return
	}
	vault, ok := mobileSSHVaultLookup(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		task.Status = "failed"
		task.Message = "vault secret missing"
		task.UpdatedAt = time.Now().UTC()
		mobileUpdateHubSSHTaskIfPresent(task)
		return
	}
	// Progressive stream: push chunks to session transcript + task log tail.
	// Session chunks: every partial writer flush. Task RT: min 2s (avoid double flood).
	var streamMu sync.Mutex
	var lastTaskRT time.Time
	const taskRTMinInterval = 2 * time.Second
	onPartial := func(chunk string) {
		if strings.TrimSpace(chunk) == "" {
			return
		}
		mobileHubSSHAppendSessionOutputChunk(session.SessionID, chunk)
		streamMu.Lock()
		task.LogTail = mobileClipRunes(task.LogTail+chunk, 12000)
		task.Message = "streaming on Hub"
		task.UpdatedAt = time.Now().UTC()
		taskCopy := *task
		emitTaskRT := time.Since(lastTaskRT) >= taskRTMinInterval
		if emitTaskRT {
			lastTaskRT = time.Now()
		}
		streamMu.Unlock()
		mobileUpdateHubSSHTaskIfPresent(&taskCopy)
		// Throttled task-level realtime (session already streamed the chunk).
		if emitTaskRT {
			mobileRealtimeBroadcast(session.TenantID, session.OwnerID, mobileRealtimeBackendSSHTaskEvent(mobileBackendSSHTaskPayload(taskCopy)))
		}
	}

	// Header line once so clients see which command started streaming.
	mobileHubSSHAppendSessionOutputChunk(session.SessionID, "\n$ "+task.Command+"\n")

	out, code, err := mobileHubSSHRunCommandForSessionCtxPartial(
		ctx, session.SessionID, profile, vault, task.Command, mobileHubSSHRunTimeout, onPartial,
	)
	task.UpdatedAt = time.Now().UTC()
	if err != nil {
		if ctx.Err() != nil || strings.Contains(err.Error(), "cancelled") {
			task.Status = "cancelled"
			task.Message = "cancelled on Hub"
		} else {
			task.Status = "failed"
			task.Message = err.Error()
		}
		task.LogTail = out
		if code >= 0 {
			task.ExitCode = &code
		}
	} else {
		task.Status = "ready"
		task.Message = "completed on Hub"
		task.LogTail = out
		task.ExitCode = &code
	}
	mobileBackendSSHTasks.Lock()
	// A user purge removes the task from this map. Never reinsert it from an
	// in-flight worker after its account has been unbound.
	if existing, ok := mobileBackendSSHTasks.tasks[task.TaskID]; ok && existing.OwnerID == task.OwnerID && existing.TenantID == task.TenantID {
		mobileBackendSSHTasks.tasks[task.TaskID] = *task
	}
	mobileBackendSSHTasks.Unlock()

	// Final note: if progressive stream already pushed body, only append trailer.
	mobileBackendSSHSessions.Lock()
	if sess, ok := mobileBackendSSHSessions.sessions[session.SessionID]; ok {
		trailer := ""
		if task.Status == "cancelled" {
			trailer = "[cancelled]\n"
		} else if task.Status == "failed" {
			trailer = "[failed] " + task.Message + "\n"
		} else {
			trailer = "[done]\n"
		}
		// If stream failed early with little progressive output, include full body once.
		if len(out) > 0 && !strings.Contains(sess.RecentOutput, out[:min(40, len(out))]) {
			trailer = out + "\n" + trailer
		}
		sess.RecentOutput = mobileClipRunes(sess.RecentOutput+trailer, 8000)
		sess.OutputChunk = trailer
		sess.OutputSeq++
		sess.Status = "ready"
		sess.State = "hub_connected"
		sess.Message = "Hub is executing SSH directly (exec_mode=hub_exec)."
		sess.UpdatedAt = time.Now().UTC()
		mobileBackendSSHSessions.sessions[session.SessionID] = sess
		// Realtime nudge when async finishes.
		payload := mobileBackendSSHSessionPayload(sess)
		taskPayload := mobileBackendSSHTaskPayload(*task)
		tenantID, ownerID := sess.TenantID, sess.OwnerID
		mobileBackendSSHSessions.Unlock()
		mobileRealtimeBroadcast(tenantID, ownerID, mobileRealtimeBackendSSHSessionEvent(payload))
		mobileRealtimeBroadcast(tenantID, ownerID, mobileRealtimeBackendSSHTaskEvent(taskPayload))
		return
	}
	mobileBackendSSHSessions.Unlock()
}

// mobileHubSSHRunFileOp executes stat/list on the remote host for hub_exec sessions.
// upload/download stay desktop_exec-only (phone local paths are not on Hub).
func mobileHubSSHRunFileOp(session mobileBackendSSHSessionRecord, op *mobileBackendSSHFileOperationRecord) {
	if op == nil {
		return
	}
	now := time.Now().UTC()
	op.Status = "running"
	op.Message = "running on Hub"
	op.ClaimedBy = "hub"
	op.UpdatedAt = now
	if !mobileUpdateHubSSHFileOperationIfPresent(op) {
		return
	}

	profile, ok := mobileFindServerProfile(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		op.Status = "failed"
		op.Message = "server profile not found"
		op.UpdatedAt = time.Now().UTC()
		mobileUpdateHubSSHFileOperationIfPresent(op)
		return
	}
	vault, ok := mobileSSHVaultLookup(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		op.Status = "failed"
		op.Message = "vault secret missing"
		op.UpdatedAt = time.Now().UTC()
		mobileUpdateHubSSHFileOperationIfPresent(op)
		return
	}

	remote := strings.TrimSpace(op.RemotePath)
	q := mobileShellSingleQuote(remote)
	action := strings.ToLower(strings.TrimSpace(op.Action))
	var cmd string
	switch action {
	case "stat":
		// Portable-ish: try GNU/BSD stat then ls -ld.
		cmd = "stat " + q + " 2>/dev/null || ls -ld " + q
	case "list":
		cmd = "ls -la " + q + " 2>&1 | head -n 200"
	case "read", "preview", "cat":
		// Small text preview only (64 KiB); not a full download path.
		cmd = "head -c 65536 " + q + " 2>&1"
	case "download":
		// Handled below via single-shot or chunked pull.
	default:
		op.Status = "failed"
		op.Message = "hub_exec supports only stat/list/read/download; use desktop_exec for upload"
		op.UpdatedAt = time.Now().UTC()
		mobileUpdateHubSSHFileOperationIfPresent(op)
		return
	}

	var transcript string
	if action == "download" {
		onProgress := func(done, total, chunkIdx, chunks int64) {
			op.Status = "running"
			op.BytesTransferred = done
			op.Message = mobileHubFileDownloadProgressMessage(done, total, chunkIdx, chunks)
			op.UpdatedAt = time.Now().UTC()
			mobileUpdateHubSSHFileOperationIfPresent(op)
			// Throttle session spam: first, every 4th chunk, and last.
			if chunkIdx == 0 || (chunkIdx+1)%4 == 0 || chunkIdx+1 == chunks {
				mobileHubSSHAppendSessionOutputChunk(session.SessionID,
					fmt.Sprintf("[download %s %d/%d]\n", path.Base(remote), chunkIdx+1, chunks))
			}
			mobileRealtimeBroadcast(session.TenantID, session.OwnerID,
				mobileRealtimeBackendSSHFileOperationEvent(mobileBackendSSHFileOperationPayload(*op)))
		}
		data, dlErr := mobileHubSSHDownloadRemoteFile(session.SessionID, profile, vault, remote, onProgress)
		op.UpdatedAt = time.Now().UTC()
		if dlErr != nil {
			op.Status = "failed"
			op.Message = dlErr.Error()
			transcript = dlErr.Error()
		} else {
			token, storeErr := mobileHubFileStoreForOperation(op, path.Base(remote), data)
			if storeErr != nil {
				op.Status = "failed"
				op.Message = storeErr.Error()
				transcript = storeErr.Error()
			} else {
				op.Status = "ready"
				op.BytesTransferred = int64(len(data))
				op.DownloadURL = "/api/mobile/ssh/files/download/" + token
				op.Message = fmt.Sprintf("ready · %d bytes · expires in %s · GET download_url with auth",
					len(data), mobileHubFileTTL)
				transcript = op.Message
			}
		}
	} else {
		timeout := 30 * time.Second
		out, code, err := mobileHubSSHRunCommandForSession(session.SessionID, profile, vault, cmd, timeout)
		op.UpdatedAt = time.Now().UTC()
		if err != nil {
			op.Status = "failed"
			op.Message = err.Error()
			if out != "" {
				op.Message = err.Error() + ": " + mobileClipRunes(out, 500)
			}
			transcript = op.Message
		} else if code != 0 {
			op.Status = "failed"
			op.Message = fmt.Sprintf("remote exit %d: %s", code, mobileClipRunes(out, 800))
			transcript = op.Message
		} else {
			op.Status = "ready"
			op.Message = mobileClipRunes(out, 4000)
			op.BytesTransferred = int64(len(out))
			transcript = op.Message
		}
	}
	mobileUpdateHubSSHFileOperationIfPresent(op)

	// Mirror a short note on the SSH session transcript.
	mobileBackendSSHSessions.Lock()
	if sess, ok := mobileBackendSSHSessions.sessions[session.SessionID]; ok {
		note := fmt.Sprintf("\n[file %s %s]\n%s\n", op.Action, remote, transcript)
		sess.RecentOutput = mobileClipRunes(sess.RecentOutput+note, 8000)
		sess.OutputChunk = note
		sess.OutputSeq++
		sess.UpdatedAt = time.Now().UTC()
		mobileBackendSSHSessions.sessions[session.SessionID] = sess
		sessPayload := mobileBackendSSHSessionPayload(sess)
		opPayload := mobileBackendSSHFileOperationPayload(*op)
		tenantID, ownerID := sess.TenantID, sess.OwnerID
		mobileBackendSSHSessions.Unlock()
		mobileRealtimeBroadcast(tenantID, ownerID, mobileRealtimeBackendSSHSessionEvent(sessPayload))
		mobileRealtimeBroadcast(tenantID, ownerID, mobileRealtimeBackendSSHFileOperationEvent(opPayload))
		return
	}
	mobileBackendSSHSessions.Unlock()
	mobileRealtimeBroadcast(session.TenantID, session.OwnerID, mobileRealtimeBackendSSHFileOperationEvent(mobileBackendSSHFileOperationPayload(*op)))
}

// mobileHubFileDownloadPlan decides single-shot vs chunked pull for a remote size.
// mode is "single" or "chunked"; chunks is the planned progress denominator.
func mobileHubFileDownloadPlan(size int64) (mode string, chunks int64, err error) {
	if size <= 0 {
		return "", 0, fmt.Errorf("invalid remote size")
	}
	maxAbs := int64(mobileHubFileMaxBytes())
	if size > maxAbs {
		return "", 0, fmt.Errorf("remote file exceeds hub_exec download limit (%d bytes; size=%d)", maxAbs, size)
	}
	if size <= mobileHubFileSingleShotBytes {
		return "single", 1, nil
	}
	chunk := int64(mobileHubFileChunkRawBytes)
	if chunk <= 0 {
		chunk = 512 * 1024
	}
	chunks = (size + chunk - 1) / chunk
	if chunks < 1 {
		chunks = 1
	}
	return "chunked", chunks, nil
}

// mobileHubFileDownloadProgressMessage is the running file-op message
// (parseable `a/b bytes` for Mobile determinate progress).
func mobileHubFileDownloadProgressMessage(done, total, chunkIdx, chunks int64) string {
	if chunks < 1 {
		chunks = 1
	}
	if chunkIdx < 0 {
		chunkIdx = 0
	}
	return fmt.Sprintf("downloading · chunk %d/%d · %d/%d bytes",
		chunkIdx+1, chunks, done, total)
}

// mobileHubSSHDownloadRemoteFile pulls a remote file into memory.
// Small files use a single base64; larger files use 512KiB dd chunks up to the absolute cap.
// onProgress is optional; chunkIdx is 0-based.
func mobileHubSSHDownloadRemoteFile(
	sessionID string,
	profile mobileServerProfileRecord,
	vault mobileSSHVaultRecord,
	remote string,
	onProgress func(done, total, chunkIdx, chunks int64),
) ([]byte, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return nil, fmt.Errorf("remote path is required")
	}
	q := mobileShellSingleQuote(remote)
	maxAbs := mobileHubFileMaxBytes()
	sizeOut, code, err := mobileHubSSHRunCommandForSession(sessionID, profile, vault,
		"if [ ! -f "+q+" ]; then echo NOT_FILE; exit 1; fi; wc -c < "+q+" | tr -d ' '", 20*time.Second)
	if err != nil {
		return nil, fmt.Errorf("stat remote size: %w", err)
	}
	sizeOut = strings.TrimSpace(sizeOut)
	if code != 0 || sizeOut == "NOT_FILE" || strings.Contains(sizeOut, "NOT_FILE") {
		return nil, fmt.Errorf("remote path is not a regular file")
	}
	size, perr := strconv.ParseInt(sizeOut, 10, 64)
	if perr != nil || size <= 0 {
		return nil, fmt.Errorf("invalid remote size %q", sizeOut)
	}
	mode, chunks, planErr := mobileHubFileDownloadPlan(size)
	if planErr != nil {
		return nil, planErr
	}

	// Single-shot for small files (faster path).
	if mode == "single" {
		if onProgress != nil {
			onProgress(0, size, 0, 1)
		}
		out, code, err := mobileHubSSHRunCommandForSession(sessionID, profile, vault, "base64 "+q, 90*time.Second)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, fmt.Errorf("remote base64 exit %d: %s", code, mobileClipRunes(out, 400))
		}
		data, decErr := mobileHubDecodeBase64Payload(out)
		if decErr != nil {
			return nil, decErr
		}
		if onProgress != nil {
			onProgress(int64(len(data)), size, 0, 1)
		}
		return data, nil
	}

	// Chunked pull.
	chunk := int64(mobileHubFileChunkRawBytes)
	var assembled bytes.Buffer
	assembled.Grow(int(size))
	for i := int64(0); i < chunks; i++ {
		// dd skip/count in blocks of 512KiB; last partial chunk is fine.
		cmd := fmt.Sprintf("dd if=%s bs=%d skip=%d count=1 2>/dev/null | base64", q, chunk, i)
		out, code, err := mobileHubSSHRunCommandForSession(sessionID, profile, vault, cmd, 60*time.Second)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", i, err)
		}
		if code != 0 && strings.TrimSpace(out) == "" {
			return nil, fmt.Errorf("chunk %d empty/failed", i)
		}
		part, decErr := mobileHubDecodeBase64Payload(out)
		if decErr != nil {
			return nil, fmt.Errorf("chunk %d decode: %w", i, decErr)
		}
		if _, werr := assembled.Write(part); werr != nil {
			return nil, werr
		}
		if onProgress != nil {
			onProgress(int64(assembled.Len()), size, i, chunks)
		}
	}
	if int64(assembled.Len()) > int64(maxAbs) {
		return nil, fmt.Errorf("assembled file exceeds hub_exec download limit")
	}
	return assembled.Bytes(), nil
}

func mobileHubDecodeBase64Payload(out string) ([]byte, error) {
	raw := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(out))
	if raw == "" {
		return nil, fmt.Errorf("empty base64 payload")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	return data, nil
}

// mobileHubSSHRunInput executes input on a hub_exec session.
// Prefer interactive shell (retains cwd/env); fall back to one-shot Run on live connection.
// raw=true writes to PTY without forcing a trailing newline (interactive / control keys).
// Interactive shell output is streamed via realtime chunks during the read wait.
func mobileHubSSHRunInput(session *mobileBackendSSHSessionRecord, input string, raw bool) (string, error) {
	if !raw {
		input = strings.TrimSpace(input)
		// Strip trailing newline/control so shell one-shots stay clean.
		input = strings.TrimRight(input, "\r\n")
	}
	if input == "" {
		return "", fmt.Errorf("input is required")
	}
	profile, ok := mobileFindServerProfile(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		return "", fmt.Errorf("server profile not found")
	}
	vault, ok := mobileSSHVaultLookup(session.TenantID, session.OwnerID, session.ServerProfileID)
	if !ok {
		return "", fmt.Errorf("vault secret missing")
	}

	displayIn := input
	if raw {
		displayIn = mobileHubRawInputDisplay(input)
	}
	// Header before exec so progressive chunks attach under the prompt line.
	mobileHubSSHAppendSessionOutputChunk(session.SessionID, "\n$ "+displayIn+"\n")

	var (
		out  string
		code int
		err  error
		via  = "shell"
	)
	// Interactive shell path first (sequential context); streams chunks live.
	out, err = mobileHubLiveShellExec(session.SessionID, profile, vault, input, raw)
	if err != nil {
		if raw {
			mobileHubSSHAppendSessionOutputChunk(session.SessionID, fmt.Sprintf("[hub_exec raw error: %v]\n", err))
			mobileHubSSHFinalizeSessionAfterInput(session, "hub_exec raw input failed: "+err.Error())
			return out, err
		}
		// Fall back to one-shot Run on reused connection.
		via = "oneshot"
		out, code, err = mobileHubSSHRunCommandForSession(session.SessionID, profile, vault, input, mobileHubSSHInputTimeout)
		if out != "" {
			mobileHubSSHAppendSessionOutputChunk(session.SessionID, out+"\n")
		}
	}
	if err != nil {
		mobileHubSSHAppendSessionOutputChunk(session.SessionID, fmt.Sprintf("[hub_exec error (%s): %v]\n", via, err))
	} else if via == "oneshot" && code != 0 {
		mobileHubSSHAppendSessionOutputChunk(session.SessionID, fmt.Sprintf("[exit %d]\n", code))
	}
	msg := "hub_exec input applied via " + via
	if raw {
		msg = "hub_exec raw PTY input applied via " + via
	}
	mobileHubSSHFinalizeSessionAfterInput(session, msg)
	if err != nil {
		return out, err
	}
	return out, nil
}

// mobileHubSSHFinalizeSessionAfterInput merges progressive session state back into
// the caller's record (output already streamed via append chunks).
func mobileHubSSHFinalizeSessionAfterInput(session *mobileBackendSSHSessionRecord, message string) {
	if session == nil {
		return
	}
	now := time.Now().UTC()
	mobileBackendSSHSessions.Lock()
	defer mobileBackendSSHSessions.Unlock()
	if latest, ok := mobileBackendSSHSessions.sessions[session.SessionID]; ok {
		latest.Status = "ready"
		latest.State = "hub_connected"
		latest.Message = message
		latest.UpdatedAt = now
		mobileBackendSSHSessions.sessions[session.SessionID] = latest
		*session = latest
		return
	}
	session.Status = "ready"
	session.State = "hub_connected"
	session.Message = message
	session.UpdatedAt = now
}

func mobileHubRawInputDisplay(input string) string {
	switch input {
	case "\x03":
		return "^C"
	case "\x04":
		return "^D"
	case "\t":
		return "<Tab>"
	case "\r", "\n", "\r\n":
		return "<Enter>"
	case "\x1b":
		return "<Esc>"
	case "\x1b[A":
		return "↑"
	case "\x1b[B":
		return "↓"
	case "\x1b[C":
		return "→"
	case "\x1b[D":
		return "←"
	default:
		if len(input) == 1 && input[0] < 32 {
			return fmt.Sprintf("^%c", input[0]+64)
		}
		// Truncate long raw paste.
		runes := []rune(input)
		if len(runes) > 40 {
			return string(runes[:40]) + "…"
		}
		return input
	}
}
