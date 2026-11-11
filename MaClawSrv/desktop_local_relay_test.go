package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

// relayRig wires the real broker, the real GUI agent and the real Python
// helper: only Hub's JSON surface is emulated and bash runs on this host.
type relayRig struct {
	files  *fakeFileStore
	broker *localrelay.Broker
	agent  *localrelay.Agent
	key    string
	home   string // the person's home
	cloud  string // the cloud desktop's home
	asks   int
}

func newRelayRig(t *testing.T) *relayRig {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the helper")
	}
	r := &relayRig{broker: &localrelay.Broker{}, key: localrelay.UserKey("t1", "u1"), home: t.TempDir(), cloud: t.TempDir()}
	settings := localrelay.DefaultSettings(r.home)
	for _, d := range []string{"Documents", "Downloads"} {
		os.MkdirAll(filepath.Join(r.home, d), 0o755)
	}
	r.agent = &localrelay.Agent{
		Home: r.home,
		Load: func() localrelay.Settings { return settings },
		Ask:  func(context.Context, localrelay.Prompt) localrelay.Answer { r.asks++; return localrelay.AnswerOnce },
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, b := net.Pipe()
	go r.agent.ServeGUI(ctx, a)
	go r.broker.Attach(ctx, r.key, "m", b)
	for i := 0; i < 200; i++ {
		if ok, _, _ := r.broker.Online(r.key); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+localrelay.ConnectPath, r.broker.ServeConnect)
	r.files = &fakeFileStore{data: map[string][]byte{}, size: map[string]int64{}}
	r.files.routes(mux)
	srv := httptest.NewServer(mux)
	t.Setenv("MACLAW_RELAY_HUB_URL", srv.URL)
	r.agent.Fetch = localrelay.HTTPFetcher(srv.Client(), srv.URL, nil)

	oldHome, oldPost, oldBash, oldFileHub := localRelayHome, localRelayPost, localRelayBash, botFileHub
	botFileHub = func(ctx context.Context, method, apiPath string, headers map[string]string, body io.Reader, size int64) ([]byte, int, error) {
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+apiPath, body)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return raw, resp.StatusCode, nil
	}
	localRelayHome = r.cloud
	localRelayPost = r.hub
	localRelayBash = func(ctx context.Context, _ agentruntime.Scope, command string) (string, error) {
		out, err := exec.CommandContext(ctx, "bash", "-c", command).CombinedOutput()
		return string(out), err
	}
	t.Cleanup(func() {
		localRelayHome, localRelayPost, localRelayBash, botFileHub = oldHome, oldPost, oldBash, oldFileHub
		// Stop helpers left listening by a failed test.
		exec.Command("pkill", "-f", r.cloud).Run()
		srv.Close()
		cancel()
	})
	return r
}

// hub emulates PostLocalRelayHandler's JSON surface over the real broker.
func (r *relayRig) hub(ctx context.Context, body map[string]any) (localRelayReply, error) {
	str := func(k string) string { s, _ := body[k].(string); return s }
	base := localrelay.Request{BotID: "b4", BotName: "Bot 4", RunID: str("run_id"), Remote: str("remote")}
	fail := func(err error) (localRelayReply, error) {
		return localRelayReply{Code: localrelay.CodeOf(err), Error: err.Error()}, nil
	}
	switch str("op") {
	case "grant_to_cloud":
		req := base
		req.Op, req.Path = localrelay.OpCheckRead, str("path")
		resp, err := r.broker.Call(ctx, r.key, req)
		if err != nil {
			return fail(err)
		}
		g := r.broker.Mint(localrelay.Grant{Key: r.key, BotID: "b4", Kind: localrelay.KindToCloud, LocalPath: resp.Path})
		return localRelayReply{OK: true, ID: g.ID, Token: g.Token, LocalPath: resp.Path, Name: resp.Name, Size: resp.Size}, nil
	case "grant_to_local":
		req := base
		req.Op, req.Path, req.Name = localrelay.OpCheckWrite, str("path"), str("name")
		resp, err := r.broker.Call(ctx, r.key, req)
		if err != nil {
			return fail(err)
		}
		g := r.broker.Mint(localrelay.Grant{Key: r.key, BotID: "b4", BotName: "Bot 4", Kind: localrelay.KindToLocal, LocalPath: resp.Path, Name: resp.Name, MaxSize: 200 << 20})
		return localRelayReply{OK: true, ID: g.ID, Token: g.Token, Folder: resp.Path, Name: resp.Name, MaxBytes: 200 << 20}, nil
	case "grant_forward", "grant_socks":
		g := localrelay.Grant{Key: r.key, BotID: "b4", Kind: localrelay.KindSocks}
		if str("op") == "grant_forward" {
			port, _ := body["port"].(int)
			req := base
			req.Op, req.Host, req.Port = localrelay.OpCheckDial, str("host"), port
			if _, err := r.broker.Call(ctx, r.key, req); err != nil {
				return fail(err)
			}
			g.Kind, g.Host, g.Port = localrelay.KindForward, req.Host, port
		}
		g = r.broker.Mint(g)
		return localRelayReply{OK: true, ID: g.ID, Token: g.Token, IdleMin: g.IdleMin}, nil
	case "fetch_to_local":
		ref := str("ref")
		r.files.mu.Lock()
		size := r.files.size[ref]
		r.files.mu.Unlock()
		req := base
		req.Path, req.Name, req.Size, req.URL, req.Ref = str("path"), str("name"), size, "/dl/"+ref, ref
		resp, err := r.broker.Fetch(ctx, r.key, req, time.Minute)
		if err != nil {
			return fail(err)
		}
		return localRelayReply{OK: true, State: resp.State, Path: resp.Path, Name: resp.Name, Size: size}, nil
	case "close":
		return localRelayReply{OK: true, Closed: r.broker.Revoke(r.key, str("id"))}, nil
	case "grants":
		return localRelayReply{OK: true, Grants: r.broker.List(r.key, "b4")}, nil
	case "status":
		on, _, _ := r.broker.Online(r.key)
		return localRelayReply{OK: true, Online: on}, nil
	}
	return localRelayReply{Code: "invalid", Error: "op"}, nil
}

func TestLocalRelayUploadAndDownloadThroughHelper(t *testing.T) {
	r := newRelayRig(t)
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", RunID: "run_abc"}
	src := filepath.Join(r.home, "Documents", "data.csv")
	payload := strings.Repeat("a,b,c\n", 200000)
	os.WriteFile(src, []byte(payload), 0o644)
	dest := filepath.Join(r.cloud, "work") + "/"
	out, err := localRelayRun(context.Background(), scope, "upload", map[string]any{"local_path": src, "remote_path": dest})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(r.cloud, "work", "data.csv"))
	if string(got) != payload || !strings.Contains(out, "Copied") || strings.Contains(out, "token") {
		t.Fatalf("upload result %q, %d bytes", out, len(got))
	}
	// The one-use token file is gone after the transfer.
	if matches, _ := filepath.Glob(filepath.Join(r.cloud, ".maclaw", "relay", "state", "*.token")); len(matches) != 0 {
		t.Fatalf("token files left: %v", matches)
	}

	report := filepath.Join(r.cloud, "work", "report.pdf")
	os.WriteFile(report, []byte("%PDF-1.4 weather"), 0o644)
	botFolder := localrelay.DefaultSettings(r.home).BotFolder("Bot 4", "b4")
	os.MkdirAll(botFolder, 0o755)
	os.WriteFile(filepath.Join(botFolder, "report.pdf"), []byte("older"), 0o644)
	out, err = localRelayRun(context.Background(), scope, "download", map[string]any{"remote_path": report})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	saved := filepath.Join(botFolder, "report (1).pdf")
	if b, _ := os.ReadFile(saved); string(b) != "%PDF-1.4 weather" || !strings.Contains(out, saved) {
		t.Fatalf("download result %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(botFolder, "report.pdf")); string(b) != "older" {
		t.Fatal("download overwrote an existing file")
	}
}

func TestLocalRelayTruthfulFailures(t *testing.T) {
	r := newRelayRig(t)
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", RunID: "run_abc"}
	r.agent.Ask = func(context.Context, localrelay.Prompt) localrelay.Answer { return localrelay.AnswerDeny }
	outside := filepath.Join(r.home, "secret.txt")
	os.WriteFile(outside, []byte("x"), 0o600)
	_, err := localRelayRun(context.Background(), scope, "upload", map[string]any{"local_path": outside, "remote_path": r.cloud + "/"})
	if err == nil || localrelay.CodeOf(err) != localrelay.CodeDenied {
		t.Fatalf("declined upload must fail with denied, got %v", err)
	}
	// Offline: the reply says the app is not connected.
	localRelayPost = func(context.Context, map[string]any) (localRelayReply, error) {
		return localRelayReply{Code: localrelay.CodeOffline, Error: localrelay.ErrOffline.Message}, nil
	}
	_, err = localRelayRun(context.Background(), scope, "list", map[string]any{"path": "~"})
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("offline: %v", err)
	}
}

func TestLocalRelayForwardTunnel(t *testing.T) {
	r := newRelayRig(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", RunID: "run_abc"}
	out, err := localRelayRun(context.Background(), scope, "tunnel_open", map[string]any{"host": "127.0.0.1", "port": port})
	if err != nil {
		t.Fatalf("tunnel_open: %v", err)
	}
	if r.asks != 1 {
		t.Fatalf("a new LAN target should ask once, asked %d", r.asks)
	}
	i := strings.Index(out, "127.0.0.1:")
	local, _ := strconv.Atoi(strings.Fields(out[i+len("127.0.0.1:"):])[0])
	id := strings.Fields(out)[1]
	for round := 0; round < 2; round++ {
		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)))
		if err != nil {
			t.Fatalf("dial tunnel: %v (%s)", err, out)
		}
		io.WriteString(c, "nvidia-smi\n")
		line, _ := bufio.NewReader(c).ReadString('\n')
		c.Close()
		if line != "nvidia-smi\n" {
			t.Fatalf("round %d echo %q", round, line)
		}
	}
	if r.asks != 1 {
		t.Fatalf("a reconnect asked again (%d)", r.asks)
	}
	listing, _ := localRelayRun(context.Background(), scope, "tunnel_list", nil)
	if !strings.Contains(listing, id) {
		t.Fatalf("tunnel_list %q lacks %s", listing, id)
	}
	closed, err := localRelayRun(context.Background(), scope, "tunnel_close", map[string]any{"id": id})
	if err != nil || !strings.Contains(closed, "stopped") {
		t.Fatalf("close: %q %v", closed, err)
	}
	time.Sleep(300 * time.Millisecond)
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)), time.Second); err == nil {
		c.Close()
		t.Fatal("listener still up after tunnel_close")
	}
}

func TestLocalRelaySocks(t *testing.T) {
	newRelayRig(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			io.Copy(c, c)
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", RunID: "run_abc"}
	out, err := localRelayRun(context.Background(), scope, "socks_open", nil)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out, "127.0.0.1:")
	local, _ := strconv.Atoi(strings.TrimSuffix(strings.Fields(out[i+len("127.0.0.1:"):])[0], ";"))
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	io.ReadFull(c, reply)
	req := []byte{5, 1, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}
	c.Write(req)
	head := make([]byte, 10)
	io.ReadFull(c, head)
	if head[1] != 0 {
		t.Fatalf("socks reply %v", head)
	}
	io.WriteString(c, "hi\n")
	line, _ := bufio.NewReader(c).ReadString('\n')
	if line != "hi\n" {
		t.Fatalf("socks echo %q", line)
	}
	id := strings.Fields(out)[2]
	localRelayRun(context.Background(), scope, "tunnel_close", map[string]any{"id": id})
}

func TestLocalRelayHelperFitsOneBashCommand(t *testing.T) {
	script := localRelayScript("lr_0123456789", strings.Repeat("f", 64), "python3 x\n")
	if len(script) > 15<<10 {
		t.Fatalf("install script is %d bytes; the container bash limit is 16 KB", len(script))
	}
}

func TestLocalRelayHubURLFromContainer(t *testing.T) {
	t.Setenv("MACLAW_RELAY_HUB_URL", "")
	t.Setenv(desktopHubURLEnv, "http://127.0.0.1:9399")
	if got := localRelayHubURL(); got != "http://172.17.0.1:9399" {
		t.Fatalf("got %s", got)
	}
	t.Setenv(desktopHubURLEnv, "https://hub.example.com")
	if got := localRelayHubURL(); got != "https://hub.example.com" {
		t.Fatalf("got %s", got)
	}
}

// fakeFileStore emulates Hub's bot file store routes the helper and
// MaClawSrv use (create, resumable PUT, status) plus a plain download.
type fakeFileStore struct {
	mu   sync.Mutex
	data map[string][]byte
	size map[string]int64
	n    int
}

func (f *fakeFileStore) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/desktop-services/files", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Size int64 `json:"size"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.n++
		id := fmt.Sprintf("bf_%023x", f.n)
		f.data[id], f.size[id] = nil, in.Size
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id, "upload_token": "tok-" + id})
	})
	status := func(w http.ResponseWriter, id string, code int) {
		f.mu.Lock()
		got, size := int64(len(f.data[id])), f.size[id]
		f.mu.Unlock()
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"ok": code == 200, "received": got, "size": size, "complete": got == size})
	}
	mux.HandleFunc("GET /api/v1/desktop-services/files/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		status(w, r.PathValue("id"), 200)
	})
	mux.HandleFunc("PUT /api/v1/desktop-services/files/{id}/data", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if r.Header.Get("X-Upload-Token") != "tok-"+id {
			w.WriteHeader(403)
			return
		}
		off, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		if off != int64(len(f.data[id])) {
			f.mu.Unlock()
			status(w, id, 409)
			return
		}
		f.data[id] = append(f.data[id], body...)
		f.mu.Unlock()
		status(w, id, 200)
	})
	mux.HandleFunc("GET /dl/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		b := f.data[r.PathValue("id")]
		f.mu.Unlock()
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
	})
}
