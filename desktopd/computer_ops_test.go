package desktopd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// fakeComputer is a tiny docker for restart/reset: one container that can be
// running, stopped or removed, and a recorded call log.
type fakeComputer struct {
	mu      sync.Mutex
	exists  bool
	running bool
	busy    bool
	calls   []string
	mounts  string
}

func newFakeComputer(t *testing.T) *fakeComputer {
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, m := range mounts {
		parts = append(parts, m.Volume+"="+m.Target)
	}
	return &fakeComputer{exists: true, running: true, mounts: strings.Join(parts, " ")}
}

func (f *fakeComputer) run(_ context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.Join(args, " ")
	f.calls = append(f.calls, line)
	switch {
	case args[0] == "inspect":
		if !f.exists {
			return "", errors.New("Error: No such object")
		}
		r := "false"
		if f.running {
			r = "true"
		}
		return r + "|" + DefaultShmSize + "|" + f.mounts + "|" + DefaultImage + "|" + DefaultImage + "|", nil
	case strings.Contains(line, "bots-busy"):
		if f.busy {
			return `{"busy":["k"]}`, nil
		}
		return `{"busy":[]}`, nil
	case args[0] == "stop":
		f.running = false
	case args[0] == "start":
		f.running = true
	case args[0] == "rm":
		f.exists, f.running = false, false
	case args[0] == "run":
		f.exists, f.running = true, true
	case args[0] == "rmi":
		return "", errors.New("Error: No such image")
	}
	return "", nil
}

func (f *fakeComputer) has(prefix string) int {
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func TestRestartFlushesStopsAndStartsKeepingTheContainer(t *testing.T) {
	f := newFakeComputer(t)
	svc := &Service{Run: f.run, ComputerDir: t.TempDir()}
	svc.noteComputerScreen(Spec{TenantID: "tenant", UserID: "alice", BotID: "bot1"}, ":21")
	out, err := svc.Restart(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}, false)
	if err != nil {
		t.Fatal(err)
	}
	flush, stop, start := f.has("exec maclaw-desktop-"), f.has("stop "), f.has("start ")
	if out.Action != "restart" || flush < 0 || stop < flush || start < stop {
		t.Fatalf("out=%+v calls=%v", out, f.calls)
	}
	botKey, _ := desktop.BotKey("tenant", "alice", "bot1")
	ownerFlush := "exec maclaw-desktop-" + mustKey(t, "tenant", "alice") + " python3 /desktop_supervisor.py flush " + mustKey(t, "tenant", "alice")
	if bf, of := f.has("exec maclaw-desktop-"+mustKey(t, "tenant", "alice")+" python3 /desktop_supervisor.py flush "+botKey), f.has(ownerFlush); bf < 0 || of < bf || of > stop {
		t.Fatalf("bot screen then owner must be written back before stop: %v", f.calls)
	}
	if f.has("rm ") >= 0 || f.has("commit ") >= 0 || f.has("run ") >= 0 {
		t.Fatalf("restart must not recreate: %v", f.calls)
	}
	if f.has("exec maclaw-desktop-"+mustKey(t, "tenant", "alice")+" python3 /desktop_supervisor.py ensure") < 0 {
		t.Fatalf("supervisor not brought back: %v", f.calls)
	}
}

func TestRestartRefusesWhileBotsBusyUnlessForced(t *testing.T) {
	f := newFakeComputer(t)
	f.busy = true
	svc := &Service{Run: f.run, ComputerDir: t.TempDir()}
	if _, err := svc.Restart(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}, false); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("err=%v", err)
	}
	if f.has("stop ") >= 0 {
		t.Fatal("busy restart stopped the container")
	}
	if _, err := svc.Restart(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}, true); err != nil {
		t.Fatal(err)
	}
}

func TestRestartWithoutDesktopIsNotFound(t *testing.T) {
	f := newFakeComputer(t)
	f.exists, f.running = false, false
	svc := &Service{Run: f.run, ComputerDir: t.TempDir()}
	if _, err := svc.Restart(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}, false); !errors.Is(err, ErrNoDesktop) {
		t.Fatalf("err=%v", err)
	}
}

func TestResetBacksUpLayerDropsStateAndRecreatesWithVolumes(t *testing.T) {
	f := newFakeComputer(t)
	svc := &Service{Run: f.run, ComputerDir: t.TempDir()}
	out, err := svc.Reset(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}, false)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := desktop.StateImage("tenant", "alice")
	prev := strings.TrimSuffix(state, ":state") + ":prev"
	flush, stop, commit, rm, rmi, run := f.has("exec maclaw-desktop-"), f.has("stop "), f.has("commit "), f.has("rm -f"), f.has("rmi "+state), f.has("run ")
	if out.Backup != prev || !(flush < stop && stop < commit && commit < rm && rm < rmi && rmi < run) || flush < 0 {
		t.Fatalf("out=%+v calls=%v", out, f.calls)
	}
	if !strings.HasSuffix(f.calls[commit], prev) {
		t.Fatalf("commit=%s", f.calls[commit])
	}
	runLine := f.calls[run]
	for _, v := range strings.Fields(f.mounts) {
		vol := strings.SplitN(v, "=", 2)
		if !strings.Contains(runLine, "--volume "+vol[0]+":"+vol[1]) {
			t.Fatalf("volume %s missing from %s", v, runLine)
		}
	}
	if !strings.Contains(runLine, " "+DefaultImage+" ") {
		t.Fatalf("reset did not run the clean image: %s", runLine)
	}
}

func TestComputerEndpointsAuthAndStatus(t *testing.T) {
	f := newFakeComputer(t)
	f.busy = true
	svc := &Service{Run: f.run, ComputerDir: t.TempDir()}
	h := Handler(svc, "secret", t.TempDir(), nil, nil)
	do := func(path, token, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := do("/v1/desktops/restart", "", `{"tenant_id":"tenant","user_id":"alice"}`); c != http.StatusUnauthorized {
		t.Fatalf("no token: %d", c)
	}
	if c := do("/v1/desktops/restart", "secret", `{"tenant_id":"tenant","user_id":"alice"}`); c != http.StatusConflict {
		t.Fatalf("busy: %d", c)
	}
	if c := do("/v1/desktops/reset", "secret", `{"tenant_id":"tenant","user_id":"alice","force":true}`); c != http.StatusOK {
		t.Fatalf("reset: %d", c)
	}
}
