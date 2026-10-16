package desktopd

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

type reaperDocker struct {
	mu sync.Mutex

	calls []string
	// running is the inspect result. docker stop sets it false unless stopKeepsRunning.
	running          map[string]bool
	stopKeepsRunning bool
	supervisorErr    error
	stopErr          error
	busy             string
	// sessionAfterStop is the idle "running" value after the first idle query.
	sessionAfterStop bool
	idleCalls        int
}

func (d *reaperDocker) run(_ context.Context, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, joined)
	switch {
	case strings.HasPrefix(joined, "ps "):
		return "maclaw-desktop-k1\ttenant-a\tk1\nmaclaw-desktop-k2\ttenant-a\tk2", nil
	case strings.Contains(joined, " bots-busy"):
		if d.busy == "" {
			return `{"busy":[]}`, nil
		}
		return `{"busy":["` + d.busy + `"]}`, nil
	case strings.HasPrefix(joined, "inspect "):
		name := args[len(args)-1]
		if d.running[name] {
			return "true", nil
		}
		return "false", nil
	case strings.HasPrefix(joined, "stop "):
		if d.stopErr != nil {
			return "", d.stopErr
		}
		if !d.stopKeepsRunning {
			name := args[len(args)-1]
			d.running[name] = false
		}
		return "", nil
	case strings.Contains(joined, " idle "):
		d.idleCalls++
		key := args[len(args)-1]
		if strings.HasSuffix(key, "k2") || strings.Contains(joined, "maclaw-desktop-k2") {
			return `{"running":true,"idle_seconds":60,"known":true}`, nil
		}
		if d.idleCalls > 1 {
			return `{"running":` + boolJSON(d.sessionAfterStop) + `,"idle_seconds":7200,"known":true}`, nil
		}
		return `{"running":true,"idle_seconds":7200,"known":true}`, nil
	case strings.Contains(joined, " stop "):
		if d.supervisorErr != nil {
			return "", d.supervisorErr
		}
		return "", nil
	default:
		return "", errors.New("unexpected docker args: " + joined)
	}
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (d *reaperDocker) count(substr string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, call := range d.calls {
		if strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

func newReaperService(d *reaperDocker) *Service {
	if d.running == nil {
		d.running = map[string]bool{"maclaw-desktop-k1": true, "maclaw-desktop-k2": true}
	}
	return &Service{Run: d.run, AdvertiseHost: "dockerd.example"}
}

func TestReaperStopsOnlyIdleDesktops(t *testing.T) {
	key1, err := desktop.UserKey("tenant-a", "k1")
	if err != nil {
		t.Fatal(err)
	}
	docker := &reaperDocker{}
	svc := newReaperService(docker)
	r := &reaper{interval: time.Minute, maxIdle: time.Hour}
	r.once(context.Background(), svc)

	if docker.count(" stop "+key1) != 1 {
		t.Fatalf("supervisor stop count = %d, calls %#v", docker.count(" stop "+key1), docker.calls)
	}
	stops := 0
	docker.mu.Lock()
	for _, call := range docker.calls {
		if strings.HasPrefix(call, "stop ") && strings.Contains(call, "maclaw-desktop-k1") {
			stops++
			if !strings.Contains(call, "--time "+desktopStopGrace) {
				t.Fatalf("docker stop missing grace: %s", call)
			}
		}
		if strings.Contains(call, "maclaw-desktop-k2") && (strings.Contains(call, " stop ") || strings.HasPrefix(call, "stop ")) {
			t.Fatalf("active desktop was reaped: %s", call)
		}
	}
	docker.mu.Unlock()
	if stops != 1 {
		t.Fatalf("idle container was not docker-stopped exactly once: %d", stops)
	}
	if docker.running["maclaw-desktop-k1"] {
		t.Fatal("idle container still marked running")
	}
}

func TestReaperLogsSupervisorStopErrorAndFallsBack(t *testing.T) {
	docker := &reaperDocker{supervisorErr: errors.New("exec failed")}
	logs := captureLog(t)
	svc := newReaperService(docker)
	(&reaper{maxIdle: time.Hour}).once(context.Background(), svc)

	text := logs.String()
	if !strings.Contains(text, "supervisor stop failed") || !strings.Contains(text, "exec failed") {
		t.Fatalf("supervisor failure was not logged:\n%s", text)
	}
	if !strings.Contains(text, "via=docker-stop") {
		t.Fatalf("successful docker stop was not confirmed:\n%s", text)
	}
	if strings.Contains(text, "reaper stop failed") {
		t.Fatalf("docker stop fallback was reported as a failure:\n%s", text)
	}
	if docker.count("stop --time") != 1 {
		t.Fatalf("docker stop count = %d", docker.count("stop --time"))
	}
}

func TestReaperSkipsDockerStopWhenContainerAlreadyDown(t *testing.T) {
	docker := &reaperDocker{running: map[string]bool{"maclaw-desktop-k1": false, "maclaw-desktop-k2": true}}
	logs := captureLog(t)
	svc := newReaperService(docker)
	(&reaper{maxIdle: time.Hour}).once(context.Background(), svc)

	if docker.count("stop --time") != 0 {
		t.Fatalf("docker stop ran against an already-stopped container: %#v", docker.calls)
	}
	if !strings.Contains(logs.String(), "via=container-already-stopped") {
		t.Fatalf("missing confirmation:\n%s", logs.String())
	}
}

func TestReaperReportsContainerStillRunningAfterDockerStop(t *testing.T) {
	docker := &reaperDocker{stopKeepsRunning: true}
	logs := captureLog(t)
	svc := newReaperService(docker)
	(&reaper{maxIdle: time.Hour}).once(context.Background(), svc)

	text := logs.String()
	if !strings.Contains(text, "reaper stop failed") || !strings.Contains(text, "still running after docker stop") {
		t.Fatalf("still-running container was not reported:\n%s", text)
	}
	if strings.Contains(text, "via=docker-stop") {
		t.Fatalf("claimed the container was reaped:\n%s", text)
	}
}

func TestReaperLeavesContainerWhenBotIsBusy(t *testing.T) {
	docker := &reaperDocker{busy: "bot-1", sessionAfterStop: false}
	logs := captureLog(t)
	svc := newReaperService(docker)
	(&reaper{maxIdle: time.Hour}).once(context.Background(), svc)

	if docker.count("stop --time") != 0 {
		t.Fatalf("docker stop killed a container with a busy bot: %#v", docker.calls)
	}
	if !strings.Contains(logs.String(), "bot display is busy") {
		t.Fatalf("missing bot explanation:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "reaper stop failed") {
		t.Fatalf("busy bot was reported as a stop failure:\n%s", logs.String())
	}
}

func TestReaperErrorsWhenBusyBotSessionStaysUp(t *testing.T) {
	docker := &reaperDocker{busy: "bot-1", sessionAfterStop: true}
	logs := captureLog(t)
	svc := newReaperService(docker)
	(&reaper{maxIdle: time.Hour}).once(context.Background(), svc)

	if docker.count("stop --time") != 0 {
		t.Fatal("docker stop ran while a bot was busy")
	}
	if !strings.Contains(logs.String(), "reaper stop failed") || !strings.Contains(logs.String(), "still running after supervisor stop") {
		t.Fatalf("session that stayed up was not reported:\n%s", logs.String())
	}
}
