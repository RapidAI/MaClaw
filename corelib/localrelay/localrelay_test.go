package localrelay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckPathTraversalAndReadOnly(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	ro := filepath.Join(base, "ro")
	os.MkdirAll(allowed, 0o755)
	os.MkdirAll(ro, 0o755)
	os.WriteFile(filepath.Join(base, "secret.txt"), []byte("s"), 0o600)
	os.WriteFile(filepath.Join(allowed, "ok.txt"), []byte("ok"), 0o600)
	s := Settings{FilesEnabled: true, Roots: []Root{{Path: allowed, Write: true}, {Path: ro}}}.Normalize()

	d, err := s.CheckPath(filepath.Join(allowed, "ok.txt"), base, AccessRead)
	if err != nil || d.Outside {
		t.Fatalf("inside read: %+v %v", d, err)
	}
	d, err = s.CheckPath(allowed+string(filepath.Separator)+".."+string(filepath.Separator)+"secret.txt", base, AccessRead)
	if err != nil || !d.Outside {
		t.Fatalf("../ must leave the root: %+v %v", d, err)
	}
	d, err = s.CheckPath(ro, base, AccessWrite)
	if err != nil || !d.Outside {
		t.Fatalf("a read-only root is outside for writes: %+v %v", d, err)
	}
	if _, err := s.CheckPath("relative/x", base, AccessRead); CodeOf(err) != CodeInvalid {
		t.Fatalf("relative path: %v", err)
	}
	if _, err := s.CheckPath(filepath.Join(allowed, "missing"), base, AccessRead); CodeOf(err) != CodeNotFound {
		t.Fatalf("missing path: %v", err)
	}
	// Prefix siblings are not inside: /x/allowed2 is not under /x/allowed.
	sib := allowed + "2"
	os.MkdirAll(sib, 0o755)
	if d, _ := s.CheckPath(sib, base, AccessRead); !d.Outside {
		t.Fatalf("sibling with the same prefix counted as inside")
	}
	if d, _ := s.CheckPath("~/allowed/ok.txt", base, AccessRead); d.Outside {
		t.Fatalf("~ expansion: %+v", d)
	}
}

func TestCheckPathSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	os.MkdirAll(allowed, 0o755)
	secret := filepath.Join(base, "secret.txt")
	os.WriteFile(secret, []byte("s"), 0o600)
	if err := os.Symlink(secret, filepath.Join(allowed, "link.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	os.Symlink(base, filepath.Join(allowed, "up"))
	s := Settings{Roots: []Root{{Path: allowed, Write: true}}}
	for _, p := range []string{filepath.Join(allowed, "link.txt"), filepath.Join(allowed, "up", "secret.txt"), filepath.Join(allowed, "up")} {
		d, err := s.CheckPath(p, base, AccessRead)
		if err != nil || !d.Outside {
			t.Fatalf("%s escapes through a symlink but counted inside: %+v %v", p, d, err)
		}
	}
	d, _ := s.CheckPath(filepath.Join(allowed, "up"), base, AccessWrite)
	if !d.Outside {
		t.Fatalf("write folder through symlink counted inside")
	}
}

func TestCreateUniqueNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "a (1).pdf"), []byte("old1"), 0o644)
	f, full, err := CreateUnique(dir, "../../a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if filepath.Base(full) != "a (2).pdf" || filepath.Dir(full) != dir {
		t.Fatalf("got %s", full)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.pdf")); string(b) != "old" {
		t.Fatalf("existing file changed")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":       "report.pdf",
		"../../etc/passwd": "passwd",
		`C:\Windows\x.dll`: "x.dll",
		"a<b>c?.txt":       "a_b_c_.txt",
		"..":               "",
		"  .hidden  ":      "hidden",
		"CON.txt":          "_CON.txt",
		"北京天气.pdf":         "北京天气.pdf",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMatchNetRule(t *testing.T) {
	ip := net.ParseIP("192.168.1.20")
	cases := []struct {
		rule string
		host string
		port int
		want bool
	}{
		{"192.168.1.0/24", "192.168.1.20", 22, true},
		{"192.168.1.0/24:22", "192.168.1.20", 80, false},
		{"192.168.1.0/24:22,8000-8100", "192.168.1.20", 8080, true},
		{"192.168.1.20", "gpu", 22, true},
		{"gpu", "gpu", 22, true},
		{"gpu:22", "GPU", 22, true},
		{"*.lab", "gpu.lab", 22, true},
		{"*.lab", "lab", 22, false},
		{"10.0.0.0/8", "192.168.1.20", 22, false},
	}
	for _, c := range cases {
		if got := MatchNetRule(c.rule, c.host, ip, c.port); got != c.want {
			t.Errorf("%s vs %s:%d = %v", c.rule, c.host, c.port, got)
		}
	}
	s := Settings{NetAllow: []string{"10.1.0.0/16:22"}}
	d, err := s.CheckNet("gpu.internal", 22, func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.1.2.3")}, nil })
	if err != nil || !d.Allowed || !d.Private || d.Addr != "10.1.2.3:22" {
		t.Fatalf("CheckNet: %+v %v", d, err)
	}
	d, _ = s.CheckNet("10.1.2.3", 80, nil)
	if d.Allowed {
		t.Fatalf("port outside rule allowed")
	}
	if _, err := s.CheckNet("x", 70000, nil); CodeOf(err) != CodeInvalid {
		t.Fatalf("bad port: %v", err)
	}
}

// rig is one Broker and one Agent joined by an in-memory link, plus an
// httptest server mounting the connect endpoint.
type rig struct {
	broker  *Broker
	agent   *Agent
	srv     *httptest.Server
	key     string
	home    string
	prompts atomic.Int32
	cancel  context.CancelFunc
}

func newRig(t *testing.T, s Settings, ask func(context.Context, Prompt) Answer) *rig {
	t.Helper()
	r := &rig{broker: &Broker{}, key: UserKey("t1", "u1"), home: t.TempDir()}
	settings := s
	r.agent = &Agent{
		Home:     r.home,
		Load:     func() Settings { return settings },
		Remember: func(k string) error { settings.Approved = append(settings.Approved, k); return nil },
	}
	r.agent.Ask = func(ctx context.Context, p Prompt) Answer {
		r.prompts.Add(1)
		if ask == nil {
			return AnswerOnce
		}
		return ask(ctx, p)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	a, b := net.Pipe()
	go r.agent.ServeGUI(ctx, a)
	go r.broker.Attach(ctx, r.key, "m1", b)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ok, _, _ := r.broker.Online(r.key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("link never came up")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+ConnectPath, r.broker.ServeConnect)
	r.srv = httptest.NewServer(mux)
	t.Cleanup(func() { r.srv.Close(); cancel() })
	return r
}

// connect is what the container helper does: an HTTP/1.1 upgrade, then raw bytes.
func (r *rig) connect(t *testing.T, headers map[string]string) (net.Conn, *bufio.Reader, int, Response) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(r.srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("GET " + ConnectPath + " HTTP/1.1\r\nHost: hub\r\nConnection: Upgrade\r\nUpgrade: maclaw-relay\r\n")
	for k, v := range headers {
		b.WriteString(k + ": " + v + "\r\n")
	}
	b.WriteString("\r\n")
	io.WriteString(conn, b.String())
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	var info Response
	if resp.StatusCode == http.StatusSwitchingProtocols {
		raw, _ := base64.StdEncoding.DecodeString(resp.Header.Get("X-Relay-Info"))
		json.Unmarshal(raw, &info)
	} else {
		var e connectErr
		json.NewDecoder(resp.Body).Decode(&e)
		info = Response{Code: e.Code, Error: e.Error}
	}
	return conn, br, resp.StatusCode, info
}

func TestRelayUploadLargeFileStreams(t *testing.T) {
	r := newRig(t, Settings{}, nil)
	docs := filepath.Join(r.home, "Documents")
	os.MkdirAll(docs, 0o755)
	r.agent.Load = func() Settings {
		return Settings{FilesEnabled: true, Roots: []Root{{Path: docs}}, MaxTransferMB: 50}
	}
	data := make([]byte, 6<<20+123) // several yamux windows
	rand.Read(data)
	src := filepath.Join(docs, "big.bin")
	os.WriteFile(src, data, 0o644)

	resp, err := r.broker.Call(context.Background(), r.key, Request{Op: OpCheckRead, BotID: "b1", Path: src})
	if err != nil || resp.Size != int64(len(data)) {
		t.Fatalf("check_read: %+v %v", resp, err)
	}
	g := r.broker.Mint(Grant{Key: r.key, BotID: "b1", Kind: KindToCloud, LocalPath: src})
	conn, br, status, info := r.connect(t, map[string]string{"X-Relay-Token": g.Token})
	defer conn.Close()
	if status != 101 || info.Size != int64(len(data)) {
		t.Fatalf("status %d info %+v", status, info)
	}
	got, _ := io.ReadAll(br)
	if sha256.Sum256(got) != sha256.Sum256(data) {
		t.Fatalf("bytes differ: got %d want %d", len(got), len(data))
	}
	if n := r.prompts.Load(); n != 1 {
		t.Fatalf("first access to a folder class should ask once, asked %d", n)
	}
	// Single use: the same token is refused afterwards.
	c2, _, status, info := r.connect(t, map[string]string{"X-Relay-Token": g.Token})
	c2.Close()
	if status != http.StatusGone || info.Code != CodeExpired {
		t.Fatalf("reused transfer token: %d %+v", status, info)
	}
}

func TestRelayDownloadNeverOverwrites(t *testing.T) {
	r := newRig(t, Settings{}, nil)
	r.agent.Load = func() Settings { return DefaultSettings(r.home) }
	folder := DefaultSettings(r.home).BotFolder("Bot 4", "b4")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "weather.pdf"), []byte("old"), 0o644)

	payload := bytes.Repeat([]byte("pdf-bytes "), 300000)
	g := r.broker.Mint(Grant{Key: r.key, BotID: "b4", BotName: "Bot 4", Kind: KindToLocal, Name: "weather.pdf", MaxSize: 1 << 30})
	conn, br, status, info := r.connect(t, map[string]string{"X-Relay-Token": g.Token, "X-Relay-Size": strconv.Itoa(len(payload))})
	defer conn.Close()
	if status != 101 {
		t.Fatalf("status %d %+v", status, info)
	}
	conn.Write(payload)
	var final Response
	if err := ReadLine(br, &final); err != nil || !final.OK {
		t.Fatalf("final %+v %v", final, err)
	}
	if filepath.Base(final.Path) != "weather (1).pdf" {
		t.Fatalf("saved as %s", final.Path)
	}
	if b, _ := os.ReadFile(filepath.Join(folder, "weather.pdf")); string(b) != "old" {
		t.Fatal("existing file overwritten")
	}
	if b, _ := os.ReadFile(final.Path); !bytes.Equal(b, payload) {
		t.Fatal("saved bytes differ")
	}
}

func TestRelayShortDownloadRemovesPartial(t *testing.T) {
	r := newRig(t, Settings{}, nil)
	r.agent.Load = func() Settings { return DefaultSettings(r.home) }
	g := r.broker.Mint(Grant{Key: r.key, BotID: "b4", BotName: "Bot 4", Kind: KindToLocal, Name: "cut.bin"})
	conn, _, status, _ := r.connect(t, map[string]string{"X-Relay-Token": g.Token, "X-Relay-Size": "1000"})
	if status != 101 {
		t.Fatalf("status %d", status)
	}
	conn.Write([]byte("only a few"))
	conn.(*net.TCPConn).CloseWrite()
	time.Sleep(300 * time.Millisecond)
	conn.Close()
	folder := DefaultSettings(r.home).BotFolder("Bot 4", "b4")
	deadline := time.Now().Add(3 * time.Second)
	for {
		items, _ := os.ReadDir(folder)
		if len(items) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial file left: %v", items[0].Name())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRelayApprovalTimeoutAndDeny(t *testing.T) {
	r := newRig(t, Settings{}, func(ctx context.Context, p Prompt) Answer {
		<-ctx.Done()
		return AnswerDeny
	})
	outside := filepath.Join(r.home, "elsewhere.txt")
	os.WriteFile(outside, []byte("x"), 0o644)
	r.agent.Load = func() Settings {
		return Settings{FilesEnabled: true, ConfirmSeconds: 1, Roots: []Root{{Path: filepath.Join(r.home, "MaClaw"), Write: true}}}
	}
	_, err := r.broker.Call(context.Background(), r.key, Request{Op: OpCheckRead, BotID: "b1", Path: outside})
	if CodeOf(err) != CodeTimeout {
		t.Fatalf("expected timeout, got %v", err)
	}
	r.agent.Ask = func(context.Context, Prompt) Answer { return AnswerDeny }
	_, err = r.broker.Call(context.Background(), r.key, Request{Op: OpCheckRead, BotID: "b1", Path: outside})
	if CodeOf(err) != CodeDenied {
		t.Fatalf("expected denied, got %v", err)
	}
}

func TestRelayAlwaysIsRemembered(t *testing.T) {
	r := newRig(t, Settings{}, func(context.Context, Prompt) Answer { return AnswerAlways })
	docs := filepath.Join(r.home, "Docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "a.txt"), []byte("a"), 0o644)
	s := Settings{FilesEnabled: true, Roots: []Root{{Path: docs}}}
	r.agent.Load = func() Settings { return s }
	r.agent.Remember = func(k string) error { s.Approved = append(s.Approved, k); return nil }
	for i := 0; i < 3; i++ {
		if _, err := r.broker.Call(context.Background(), r.key, Request{Op: OpList, BotID: "b1", Path: docs}); err != nil {
			t.Fatal(err)
		}
	}
	if r.prompts.Load() != 1 || len(s.Approved) != 1 || !strings.HasPrefix(s.Approved[0], "b1|root:read:") {
		t.Fatalf("prompts=%d approved=%v", r.prompts.Load(), s.Approved)
	}
	// Another bot asks again: approvals are per bot.
	r.broker.Call(context.Background(), r.key, Request{Op: OpList, BotID: "b2", Path: docs})
	if r.prompts.Load() != 2 {
		t.Fatalf("a second bot reused the first bot's approval")
	}
}

func TestRelayOfflineAndDisabled(t *testing.T) {
	b := &Broker{}
	if _, err := b.Call(context.Background(), "nobody", Request{Op: OpPing}); CodeOf(err) != CodeOffline {
		t.Fatalf("offline: %v", err)
	}
	r := newRig(t, Settings{}, nil)
	r.agent.Load = func() Settings { return Settings{} }
	if _, err := r.broker.Call(context.Background(), r.key, Request{Op: OpList, Path: r.home}); CodeOf(err) != CodeDisabled {
		t.Fatalf("files off: %v", err)
	}
	if _, err := r.broker.Call(context.Background(), r.key, Request{Op: OpCheckDial, Host: "127.0.0.1", Port: 22}); CodeOf(err) != CodeDisabled {
		t.Fatalf("network off: %v", err)
	}
}

func echoServer(t *testing.T) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func TestRelayForwardAndSocksTunnel(t *testing.T) {
	host, port := echoServer(t)
	r := newRig(t, Settings{}, nil)
	r.agent.Load = func() Settings {
		return Settings{NetworkEnabled: true, NetAllow: []string{"127.0.0.1:" + strconv.Itoa(port)}}
	}
	g := r.broker.Mint(Grant{Key: r.key, BotID: "b1", Kind: KindForward, Host: host, Port: port})
	conn, br, status, info := r.connect(t, map[string]string{"X-Relay-Token": g.Token})
	if status != 101 {
		t.Fatalf("forward: %d %+v", status, info)
	}
	io.WriteString(conn, "hello gpu\n")
	line, _ := br.ReadString('\n')
	if line != "hello gpu\n" {
		t.Fatalf("echo got %q", line)
	}
	conn.Close()
	if r.prompts.Load() != 0 {
		t.Fatalf("a pre-approved target asked")
	}

	// SOCKS: a target outside the rules asks; deny is a 403 to the container.
	r.agent.Ask = func(context.Context, Prompt) Answer { return AnswerDeny }
	sg := r.broker.Mint(Grant{Key: r.key, BotID: "b1", Kind: KindSocks})
	c2, _, status, info := r.connect(t, map[string]string{"X-Relay-Token": sg.Token, "X-Relay-Target": "127.0.0.1:1"})
	c2.Close()
	if status != http.StatusForbidden || info.Code != CodeDenied {
		t.Fatalf("socks deny: %d %+v", status, info)
	}
	c3, br3, status, _ := r.connect(t, map[string]string{"X-Relay-Token": sg.Token, "X-Relay-Target": net.JoinHostPort(host, strconv.Itoa(port))})
	if status != 101 {
		t.Fatalf("socks allowed target: %d", status)
	}
	io.WriteString(c3, "x\n")
	if l, _ := br3.ReadString('\n'); l != "x\n" {
		t.Fatalf("socks echo %q", l)
	}
	c3.Close()
	time.Sleep(50 * time.Millisecond)
	list := r.broker.List(r.key, "b1")
	if len(list) != 2 {
		t.Fatalf("grants: %+v", list)
	}
	if !r.broker.Revoke(r.key, g.ID) {
		t.Fatal("revoke failed")
	}
	c4, _, status, _ := r.connect(t, map[string]string{"X-Relay-Token": g.Token})
	c4.Close()
	if status != http.StatusGone {
		t.Fatalf("revoked tunnel still connects: %d", status)
	}
}

func TestGrantIdleExpiry(t *testing.T) {
	now := time.Now()
	b := &Broker{Now: func() time.Time { return now }}
	g := b.Mint(Grant{Key: "k", Kind: KindForward, Host: "h", Port: 22, Idle: time.Minute})
	if _, err := b.use(g.Token); err != nil {
		t.Fatal(err)
	}
	b.release(g.Token, 0, 0, "")
	now = now.Add(2 * time.Minute)
	if _, err := b.use(g.Token); CodeOf(err) != CodeExpired {
		t.Fatalf("idle tunnel still usable: %v", err)
	}
	if _, err := b.use("wrong"); CodeOf(err) != CodeExpired {
		t.Fatalf("unknown token: %v", err)
	}
}

func TestOpenTimeoutWhenGUIStalls(t *testing.T) {
	r := newRig(t, Settings{}, func(ctx context.Context, p Prompt) Answer { time.Sleep(2 * time.Second); return AnswerOnce })
	r.agent.Load = func() Settings { return Settings{FilesEnabled: true, ConfirmSeconds: 30} }
	r.broker.OpenTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := r.broker.Call(context.Background(), r.key, Request{Op: OpList, Path: r.home})
	if CodeOf(err) != CodeTimeout || time.Since(start) > time.Second {
		t.Fatalf("stalled GUI: %v after %v", err, time.Since(start))
	}
}

func TestReadLineLeavesBody(t *testing.T) {
	var buf bytes.Buffer
	WriteLine(&buf, Response{OK: true, Size: 3})
	buf.WriteString("abc")
	var r Response
	if err := ReadLine(&buf, &r); err != nil || r.Size != 3 {
		t.Fatal(err)
	}
	if buf.String() != "abc" {
		t.Fatalf("body consumed: %q", buf.String())
	}
}
