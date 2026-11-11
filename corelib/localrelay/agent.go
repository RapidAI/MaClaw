package localrelay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botlog"
	"github.com/hashicorp/yamux"
)

// Answer is the person's reply to one Prompt.
type Answer string

const (
	AnswerDeny   Answer = "deny"
	AnswerOnce   Answer = "once"
	AnswerAlways Answer = "always"
)

// Prompt is one question shown on the person's computer.
type Prompt struct {
	ID      string `json:"id"`
	BotID   string `json:"bot_id"`
	BotName string `json:"bot_name"`
	// Kind is read, write, list or network.
	Kind string `json:"kind"`
	// Target is the local path or host:port.
	Target string `json:"target"`
	// Remote is the cloud-side path of a transfer.
	Remote string `json:"remote,omitempty"`
	// Outside: the path is outside every allowed folder.
	Outside bool `json:"outside,omitempty"`
	// Private: a LAN, link-local or loopback address. Local: this computer.
	Private bool  `json:"private,omitempty"`
	Local   bool  `json:"local,omitempty"`
	Size    int64 `json:"size,omitempty"`
	// Seconds the question waits before it counts as no.
	Seconds int `json:"seconds"`
}

// TransferEvent reports one transfer or tunnel connection for the bot chat.
type TransferEvent struct {
	ID        string `json:"id"`
	BotID     string `json:"bot_id"`
	BotName   string `json:"bot_name,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Direction string `json:"direction"` // to_cloud, to_local, tunnel
	Name      string `json:"name,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
	Remote    string `json:"remote,omitempty"`
	Target    string `json:"target,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Done      int64  `json:"done"`
	State     string `json:"state"` // running, done, failed, closed
	Error     string `json:"error,omitempty"`
}

// Agent answers Hub's streams on the person's computer.
type Agent struct {
	Home string
	// Load returns the current policy. Required.
	Load func() Settings
	// Remember persists an "always allow" key.
	Remember func(key string) error
	// Ask shows a question and waits; nil answers no to everything new.
	Ask func(ctx context.Context, p Prompt) Answer
	// Notify receives transfer progress; may be nil.
	Notify func(TransferEvent)
	// Lookup resolves host names; nil uses net.LookupIP.
	Lookup func(string) ([]net.IP, error)
	// Dial connects to a checked address; nil uses net.Dialer.
	Dial func(ctx context.Context, addr string) (net.Conn, error)

	mu     sync.Mutex
	grants map[string]time.Time
	locks  map[string]*sync.Mutex
}

// onceTTL is how long a "once" answer covers the same class. A path outside
// the allowed folders is asked again soon; a folder class or a network
// target lasts the session so an ssh reconnect does not ask every time.
func onceTTL(class string) time.Duration {
	if strings.HasPrefix(class, "path:") {
		return 10 * time.Minute
	}
	return 12 * time.Hour
}

func (a *Agent) settings() Settings {
	if a == nil || a.Load == nil {
		return Settings{}
	}
	return a.Load().Normalize()
}

func (a *Agent) keyLock(key string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locks == nil {
		a.locks = map[string]*sync.Mutex{}
	}
	l := a.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		a.locks[key] = l
	}
	return l
}

func (a *Agent) granted(key string, s Settings) bool {
	for _, k := range s.Approved {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.grants[key]
	return ok && time.Now().Before(until)
}

// approve asks once per bot and class. Concurrent requests of the same
// class wait for the first answer instead of stacking questions.
func (a *Agent) approve(ctx context.Context, s Settings, botID, class string, p Prompt) error {
	key := strings.TrimSpace(botID) + "|" + class
	if a.granted(key, s) {
		return nil
	}
	l := a.keyLock(key)
	l.Lock()
	defer l.Unlock()
	if a.granted(key, a.settings()) {
		return nil
	}
	if a.Ask == nil {
		return &Err{Code: CodeDenied, Message: "this computer cannot show the question, so the request was refused"}
	}
	p.ID = newID()
	p.Seconds = s.ConfirmSeconds
	askCtx, cancel := context.WithTimeout(ctx, time.Duration(s.ConfirmSeconds)*time.Second)
	defer cancel()
	answer := a.Ask(askCtx, p)
	switch answer {
	case AnswerOnce, AnswerAlways:
		a.mu.Lock()
		if a.grants == nil {
			a.grants = map[string]time.Time{}
		}
		a.grants[key] = time.Now().Add(onceTTL(class))
		a.mu.Unlock()
		if answer == AnswerAlways && a.Remember != nil {
			if err := a.Remember(key); err != nil {
				botlog.Write(botID, "localrelay.remember", err)
			}
		}
		return nil
	}
	if askCtx.Err() != nil && ctx.Err() == nil {
		return &Err{Code: CodeTimeout, Message: fmt.Sprintf("nobody answered the question on the computer within %d seconds, so it was not allowed", s.ConfirmSeconds)}
	}
	if ctx.Err() != nil {
		return &Err{Code: CodeTimeout, Message: "the request was cancelled before the person answered"}
	}
	return &Err{Code: CodeDenied, Message: "the person declined this on their computer"}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func respondErr(w io.Writer, err error) {
	var e *Err
	if !errors.As(err, &e) {
		e = &Err{Code: CodeFailed, Message: err.Error()}
	}
	_ = WriteLine(w, Response{Code: e.Code, Error: e.Message})
}

// ServeStream handles one stream until it is done and closes it.
func (a *Agent) ServeStream(ctx context.Context, stream net.Conn) {
	defer stream.Close()
	var req Request
	_ = stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	if err := ReadLine(stream, &req); err != nil {
		return
	}
	_ = stream.SetReadDeadline(time.Time{})
	started := time.Now()
	err := a.handle(ctx, stream, req)
	fields := []string{"op", req.Op, "run", req.RunID, "ms", strconv.FormatInt(time.Since(started).Milliseconds(), 10)}
	if req.Path != "" {
		fields = append(fields, "path", req.Path)
	}
	if req.Host != "" {
		fields = append(fields, "target", net.JoinHostPort(req.Host, strconv.Itoa(req.Port)))
	}
	if req.Op != OpPing {
		botlog.Write(firstNonEmpty(req.BotID, "localrelay"), "localrelay."+req.Op, err, fields...)
	}
	if err != nil {
		respondErr(stream, err)
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

var errFilesOff = &Err{Code: CodeDisabled, Message: "bots may not use files on this computer: it is turned off in Bot 管理 → 设置 → 本地访问"}
var errNetOff = &Err{Code: CodeDisabled, Message: "bots may not reach this computer's network: it is turned off in Bot 管理 → 设置 → 本地访问"}

func (a *Agent) handle(ctx context.Context, stream net.Conn, req Request) error {
	s := a.settings()
	switch req.Op {
	case OpPing:
		return WriteLine(stream, Response{OK: true, Summary: a.summary(s, req)})
	case OpList:
		if !s.FilesEnabled {
			return errFilesOff
		}
		return a.list(ctx, stream, s, req)
	case OpCheckRead, OpRead:
		if !s.FilesEnabled {
			return errFilesOff
		}
		return a.read(ctx, stream, s, req)
	case OpCheckWrite, OpWrite:
		if !s.FilesEnabled {
			return errFilesOff
		}
		return a.write(ctx, stream, s, req)
	case OpCheckDial, OpDial:
		if !s.NetworkEnabled {
			return errNetOff
		}
		return a.dial(ctx, stream, s, req)
	}
	return &Err{Code: CodeInvalid, Message: "unknown request " + req.Op}
}

func (a *Agent) summary(s Settings, req Request) *Summary {
	out := &Summary{FilesEnabled: s.FilesEnabled, NetworkEnabled: s.NetworkEnabled, MaxTransferMB: s.MaxTransferMB, OS: runtime.GOOS, TunnelIdleMinutes: s.TunnelIdleMinutes}
	if s.FilesEnabled {
		for _, r := range s.Roots {
			out.Roots = append(out.Roots, Entry{Name: r.Path, Dir: true, Write: r.Write})
		}
		out.BotFolder = s.BotFolder(req.BotName, req.BotID)
	}
	return out
}

func (a *Agent) list(ctx context.Context, stream net.Conn, s Settings, req Request) error {
	if strings.TrimSpace(req.Path) == "" {
		var entries []Entry
		for _, r := range s.Roots {
			entries = append(entries, Entry{Name: r.Path, Dir: true, Write: r.Write})
		}
		return WriteLine(stream, Response{OK: true, Entries: entries, Summary: a.summary(s, req)})
	}
	d, err := s.CheckPath(req.Path, a.Home, AccessRead)
	if err != nil {
		return err
	}
	info, err := os.Stat(d.Resolved)
	if err != nil {
		return &Err{Code: CodeNotFound, Message: err.Error()}
	}
	if !info.IsDir() {
		return &Err{Code: CodeInvalid, Message: d.Resolved + " is a file, not a folder"}
	}
	if err := a.approve(ctx, s, req.BotID, d.Class, Prompt{BotID: req.BotID, BotName: req.BotName, Kind: "list", Target: d.Resolved, Outside: d.Outside}); err != nil {
		return err
	}
	items, err := os.ReadDir(d.Resolved)
	if err != nil {
		return &Err{Code: CodeFailed, Message: err.Error()}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsDir() != items[j].IsDir() {
			return items[i].IsDir()
		}
		return strings.ToLower(items[i].Name()) < strings.ToLower(items[j].Name())
	})
	resp := Response{OK: true, Path: d.Resolved}
	for _, it := range items {
		if len(resp.Entries) >= MaxListEntries {
			resp.Truncated = true
			break
		}
		e := Entry{Name: it.Name(), Dir: it.IsDir()}
		if fi, err := it.Info(); err == nil {
			if !it.IsDir() {
				e.Size = fi.Size()
			}
			e.ModTime = fi.ModTime().Format(time.RFC3339)
		}
		resp.Entries = append(resp.Entries, e)
	}
	return WriteLine(stream, resp)
}

func (a *Agent) read(ctx context.Context, stream net.Conn, s Settings, req Request) error {
	d, err := s.CheckPath(req.Path, a.Home, AccessRead)
	if err != nil {
		return err
	}
	info, err := os.Stat(d.Resolved)
	if err != nil {
		return &Err{Code: CodeNotFound, Message: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return &Err{Code: CodeInvalid, Message: d.Resolved + " is not a regular file (a folder is listed with action=local_list; send its files one by one or zip it first)"}
	}
	if info.Size() > s.MaxBytes() {
		return &Err{Code: CodeTooLarge, Message: fmt.Sprintf("%s is %s, above the %d MB limit set on this computer", d.Resolved, HumanSize(info.Size()), s.MaxTransferMB)}
	}
	if err := a.approve(ctx, s, req.BotID, d.Class, Prompt{BotID: req.BotID, BotName: req.BotName, Kind: "read", Target: d.Resolved, Remote: req.Remote, Outside: d.Outside, Size: info.Size()}); err != nil {
		return err
	}
	if req.Op == OpCheckRead {
		return WriteLine(stream, Response{OK: true, Path: d.Resolved, Name: filepath.Base(d.Resolved), Size: info.Size()})
	}
	f, err := os.Open(d.Resolved)
	if err != nil {
		return &Err{Code: CodeFailed, Message: err.Error()}
	}
	defer f.Close()
	size := info.Size()
	if err := WriteLine(stream, Response{OK: true, Path: d.Resolved, Name: filepath.Base(d.Resolved), Size: size}); err != nil {
		return nil
	}
	ev := TransferEvent{ID: newID(), BotID: req.BotID, BotName: req.BotName, RunID: req.RunID, Direction: "to_cloud", Name: filepath.Base(d.Resolved), LocalPath: d.Resolved, Remote: req.Remote, Size: size, State: "running"}
	a.notify(ev)
	n, cerr := io.Copy(stream, &progressReader{r: io.LimitReader(f, size), ev: &ev, notify: a.notify})
	ev.Done = n
	if cerr != nil || n != size {
		ev.State = "failed"
		ev.Error = fmt.Sprintf("sent %s of %s", HumanSize(n), HumanSize(size))
		if cerr != nil {
			ev.Error += ": " + cerr.Error()
		}
		a.notify(ev)
		return nil // the stream is broken; the other side sees the short count
	}
	ev.State = "done"
	a.notify(ev)
	return nil
}

func (a *Agent) writeFolder(s Settings, req Request) (string, error) {
	folder := strings.TrimSpace(req.Path)
	botFolder := s.BotFolder(req.BotName, req.BotID)
	if folder == "" {
		if botFolder == "" {
			return "", &Err{Code: CodeDenied, Message: "no writable folder is allowed on this computer; add one in Bot 管理 → 设置 → 本地访问"}
		}
		folder = botFolder
	}
	folder = ExpandHome(folder, a.Home)
	if botFolder != "" && filepath.IsAbs(folder) && Within(botFolder, filepath.Clean(folder)) {
		// The bot's own folder (and subfolders) are made on first use.
		if err := os.MkdirAll(folder, 0o755); err != nil {
			return "", &Err{Code: CodeFailed, Message: err.Error()}
		}
	}
	return folder, nil
}

func (a *Agent) write(ctx context.Context, stream net.Conn, s Settings, req Request) error {
	folder, err := a.writeFolder(s, req)
	if err != nil {
		return err
	}
	d, err := s.CheckPath(folder, a.Home, AccessWrite)
	if err != nil {
		return err
	}
	if info, err := os.Stat(d.Resolved); err != nil || !info.IsDir() {
		return &Err{Code: CodeInvalid, Message: d.Resolved + " is not a folder"}
	}
	name := SafeName(req.Name)
	if name == "" {
		return &Err{Code: CodeInvalid, Message: "the file name is empty or invalid"}
	}
	if req.Size < 0 || req.Size > s.MaxBytes() {
		return &Err{Code: CodeTooLarge, Message: fmt.Sprintf("the file is %s, above the %d MB limit set on this computer", HumanSize(req.Size), s.MaxTransferMB)}
	}
	if err := a.approve(ctx, s, req.BotID, d.Class, Prompt{BotID: req.BotID, BotName: req.BotName, Kind: "write", Target: filepath.Join(d.Resolved, name), Remote: req.Remote, Outside: d.Outside, Size: req.Size}); err != nil {
		return err
	}
	if req.Op == OpCheckWrite {
		return WriteLine(stream, Response{OK: true, Path: d.Resolved, Name: name})
	}
	f, full, err := CreateUnique(d.Resolved, name)
	if err != nil {
		return &Err{Code: CodeFailed, Message: err.Error()}
	}
	if err := WriteLine(stream, Response{OK: true, Path: full, Name: filepath.Base(full)}); err != nil {
		f.Close()
		os.Remove(full)
		return nil
	}
	ev := TransferEvent{ID: newID(), BotID: req.BotID, BotName: req.BotName, RunID: req.RunID, Direction: "to_local", Name: filepath.Base(full), LocalPath: full, Remote: req.Remote, Size: req.Size, State: "running"}
	a.notify(ev)
	n, cerr := io.Copy(f, &progressReader{r: io.LimitReader(stream, req.Size), ev: &ev, notify: a.notify})
	if serr := f.Sync(); cerr == nil {
		cerr = serr
	}
	if clerr := f.Close(); cerr == nil {
		cerr = clerr
	}
	ev.Done = n
	if cerr != nil || n != req.Size {
		os.Remove(full)
		msg := fmt.Sprintf("received %s of %s; the partial file was removed", HumanSize(n), HumanSize(req.Size))
		if cerr != nil {
			msg += ": " + cerr.Error()
		}
		ev.State, ev.Error = "failed", msg
		a.notify(ev)
		return &Err{Code: CodeFailed, Message: msg}
	}
	ev.State = "done"
	a.notify(ev)
	return WriteLine(stream, Response{OK: true, Path: full, Name: filepath.Base(full), Size: n})
}

func (a *Agent) dial(ctx context.Context, stream net.Conn, s Settings, req Request) error {
	d, err := s.CheckNet(req.Host, req.Port, a.Lookup)
	if err != nil {
		return err
	}
	target := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	if !d.Allowed {
		if err := a.approve(ctx, s, req.BotID, d.Class, Prompt{BotID: req.BotID, BotName: req.BotName, Kind: "network", Target: target, Private: d.Private, Local: d.Local}); err != nil {
			return err
		}
	}
	if req.Op == OpCheckDial {
		return WriteLine(stream, Response{OK: true, Addr: d.Addr})
	}
	dial := a.Dial
	if dial == nil {
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			var dd net.Dialer
			return dd.DialContext(ctx, "tcp", addr)
		}
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, err := dial(dctx, d.Addr)
	cancel()
	if err != nil {
		return &Err{Code: CodeFailed, Message: fmt.Sprintf("this computer could not connect to %s (%s): %v", target, d.Addr, err)}
	}
	defer conn.Close()
	if err := WriteLine(stream, Response{OK: true, Addr: d.Addr}); err != nil {
		return nil
	}
	ev := TransferEvent{ID: newID(), BotID: req.BotID, BotName: req.BotName, RunID: req.RunID, Direction: "tunnel", Target: target, State: "running"}
	a.notify(ev)
	up, down := Pipe(stream, conn)
	ev.Done = up + down
	ev.State = "closed"
	a.notify(ev)
	return nil
}

func (a *Agent) notify(ev TransferEvent) {
	if a.Notify != nil {
		a.Notify(ev)
	}
}

// Pipe copies both ways until either side ends and returns bytes a→b, b→a.
func Pipe(a, b net.Conn) (int64, int64) {
	var ab, ba int64
	done := make(chan struct{}, 2)
	go func() {
		ab, _ = io.Copy(b, a)
		closeWrite(b)
		done <- struct{}{}
	}()
	go func() {
		ba, _ = io.Copy(a, b)
		closeWrite(a)
		done <- struct{}{}
	}()
	<-done
	// Give the other direction a moment to drain a half-close, then end both.
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	a.Close()
	b.Close()
	return ab, ba
}

func closeWrite(c net.Conn) {
	type halfCloser interface{ CloseWrite() error }
	if hc, ok := c.(halfCloser); ok {
		_ = hc.CloseWrite()
		return
	}
	// A yamux stream half-closes on Close: it sends FIN, reads stay open.
	if s, ok := c.(*yamux.Stream); ok {
		_ = s.Close()
	}
}

type progressReader struct {
	r      io.Reader
	ev     *TransferEvent
	notify func(TransferEvent)
	last   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.ev.Done += int64(n)
	if time.Since(p.last) > 400*time.Millisecond {
		p.last = time.Now()
		p.notify(*p.ev)
	}
	return n, err
}

// HumanSize prints bytes as B, KB, MB or GB.
func HumanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
