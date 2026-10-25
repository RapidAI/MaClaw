package desktopd

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestSupervisorBootRestoresOwnerAndRecentBots(t *testing.T) {
	owner := strings.Repeat("ab", 8)
	out := runSupervisorPython(t, `
import contextlib, time
owner = "`+owner+`"
recent, stale, never, other = owner + "1"*16, owner + "2"*16, owner + "3"*16, "cd"*8 + "4"*16
calls = []
sup.desktop_lock = contextlib.nullcontext
sup.log_event = lambda *a, **k: None
sup.ensure = lambda key: calls.append(("ensure", key)) or {"token": "secret"}
def eb(key):
    calls.append(("bot", key))
    if key == recent:
        return {"token": "secret"}
sup.ensure_bot = eb
sup.load_registry = lambda: {"users": {owner: {}, recent: {"bot": True}, stale: {"bot": True}, never: {"bot": True}, other: {"bot": True}}}
for key in (recent, stale):
    sup.touch_bot_file(key, "activity", force=True)
old = time.time() - 3600
os.utime(sup.ROOT / stale / "activity", (old, old))
res = sup.boot_restore()
print(json.dumps(calls))
print("secret" in json.dumps(res), res["restored"] == [owner, recent])
`, "MACLAW_DESKTOP_KEY="+owner)
	lines := strings.Split(out, "\n")
	want := `[["ensure", "` + owner + `"], ["bot", "` + owner + strings.Repeat("1", 16) + `"]]`
	if len(lines) != 2 || lines[0] != want || lines[1] != "False True" {
		t.Fatalf("boot:\n%s\nwant first line %s", out, want)
	}
}

type bootDocker struct {
	mu      sync.Mutex
	started map[string]string
	calls   []string
}

func (d *bootDocker) run(_ context.Context, args ...string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, strings.Join(args, " "))
	switch args[0] {
	case "ps":
		var names []string
		for name := range d.started {
			names = append(names, name)
		}
		return strings.Join(append(names, "maclaw-gui"), "\n"), nil
	case "inspect":
		var lines []string
		for _, name := range args[3:] {
			lines = append(lines, "/"+name+" "+d.started[name])
		}
		return strings.Join(lines, "\n"), nil
	case "exec":
		return `{"restored": ["k"]}`, nil
	}
	return "", nil
}

func (d *bootDocker) execs() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if strings.HasPrefix(c, "exec ") && strings.HasSuffix(c, "/desktop_supervisor.py boot") {
			n++
		}
	}
	return n
}

func TestBootWatcherBootsEachContainerStartOnce(t *testing.T) {
	d := &bootDocker{started: map[string]string{"maclaw-desktop-a": "t1", "maclaw-desktop-b": "t1"}}
	svc := &Service{Run: d.run}
	w := &bootWatcher{seen: map[string]string{}}
	ctx := context.Background()
	if got := w.once(ctx, svc); len(got) != 2 {
		t.Fatalf("first pass booted %v", got)
	}
	if got := w.once(ctx, svc); len(got) != 0 || d.execs() != 2 {
		t.Fatalf("second pass booted %v (execs %d)", got, d.execs())
	}
	d.mu.Lock()
	d.started["maclaw-desktop-a"] = "t2" // docker restart
	d.mu.Unlock()
	if got := w.once(ctx, svc); len(got) != 1 || got[0] != "maclaw-desktop-a" {
		t.Fatalf("restart pass booted %v", got)
	}
	for _, c := range d.calls {
		if strings.Contains(c, "maclaw-gui") && strings.HasPrefix(c, "exec") {
			t.Fatalf("booted a non-desktop container: %s", c)
		}
	}
}
