package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotSleepCases(t *testing.T) {
	t.Run("idle_does_not_stop_when_sync_fails", func(t *testing.T) {
		logs := captureLog(t)
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		var metas []SleepMeta
		svc.VolumeSync = func(_ context.Context, meta SleepMeta, mounts []desktop.Mount) (VolumeSyncResult, error) {
			metas = append(metas, meta)
			if len(mounts) != 4 {
				t.Fatalf("volumes = %d", len(mounts))
			}
			return VolumeSyncResult{}, errors.New("sync failed")
		}
		svc.ReapStage2(context.Background())
		if docker.count("stop --time") != 0 {
			t.Fatalf("docker stop ran after a failed sync: %#v", docker.calls)
		}
		if !docker.running["maclaw-desktop-k1"] {
			t.Fatal("container was marked stopped")
		}
		if docker.count(" flush ") == 0 {
			t.Fatal("sleep did not flush before the sync")
		}
		if len(metas) != 1 {
			t.Fatalf("syncs = %d", len(metas))
		}
		raw, err := json.Marshal(metas[0])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(raw)), "token") {
			t.Fatalf("meta contains a token: %s", raw)
		}
		text := logs.String()
		if strings.Contains(text, "via=docker-stop") || strings.Contains(strings.ToLower(text), "token=") {
			t.Fatalf("log:\n%s", text)
		}
		if !strings.Contains(text, "left container running") {
			t.Fatalf("sync failure was not recorded:\n%s", text)
		}
	})

	t.Run("nil_store_does_not_stop", func(t *testing.T) {
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.ReapStage2(context.Background())
		if docker.count("stop --time") != 0 || !docker.running["maclaw-desktop-k1"] {
			t.Fatalf("unconfigured store stopped the container: %#v", docker.calls)
		}
	})

	t.Run("flush_failure_does_not_stop", func(t *testing.T) {
		docker := &reaperDocker{flushErr: errors.New("flush failed")}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.VolumeSync = func(context.Context, SleepMeta, []desktop.Mount) (VolumeSyncResult, error) {
			t.Fatal("sync ran after flush failed")
			return VolumeSyncResult{}, nil
		}
		svc.ReapStage2(context.Background())
		if docker.count("stop --time") != 0 || !docker.running["maclaw-desktop-k1"] {
			t.Fatal("flush failure stopped the container")
		}
	})

	t.Run("switch_off_keeps_running", func(t *testing.T) {
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = false
		svc.VolumeSync = func(context.Context, SleepMeta, []desktop.Mount) (VolumeSyncResult, error) {
			t.Fatal("switch off synced")
			return VolumeSyncResult{}, nil
		}
		svc.ReapStage2(context.Background())
		if len(docker.calls) != 0 {
			t.Fatalf("switch off touched docker: %#v", docker.calls)
		}
		off, err := ParseStage2Sleep("")
		if err != nil || off {
			t.Fatalf("empty switch = %v %v", off, err)
		}
		if off, err = ParseStage2Sleep("off"); err != nil || off {
			t.Fatalf("off = %v %v", off, err)
		}
		on, err := ParseStage2Sleep("on")
		if err != nil || !on {
			t.Fatalf("on = %v %v", on, err)
		}
	})

	t.Run("sync_then_stop_keeps_volumes", func(t *testing.T) {
		logs := captureLog(t)
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.VolumeSync = func(_ context.Context, meta SleepMeta, mounts []desktop.Mount) (VolumeSyncResult, error) {
			raw, _ := json.Marshal(meta)
			if strings.Contains(strings.ToLower(string(raw)), "token") {
				t.Fatalf("meta contains a token: %s", raw)
			}
			got := map[string]bool{}
			for _, mount := range mounts {
				got[mount.Target] = true
			}
			for _, target := range []string{"/desktops", "/home/desktop", "/opt", "/usr/local"} {
				if !got[target] {
					t.Fatalf("missing %s in %#v", target, mounts)
				}
			}
			return VolumeSyncResult{Generation: "g1", Volumes: []string{"desktops", "home", "opt", "usr-local"}}, nil
		}
		svc.ReapStage2(context.Background())
		if docker.count("stop --time "+desktopStopGrace) != 1 {
			t.Fatalf("docker stop count, calls %#v", docker.calls)
		}
		if docker.running["maclaw-desktop-k1"] {
			t.Fatal("idle container still running")
		}
		docker.mu.Lock()
		defer docker.mu.Unlock()
		for _, call := range docker.calls {
			if strings.Contains(call, "commit") || strings.HasPrefix(call, "rm ") || strings.Contains(call, "volume rm") || strings.Contains(call, "always") {
				t.Fatalf("sleep changed the container: %s", call)
			}
			if strings.Contains(call, "maclaw-desktop-k2") && strings.HasPrefix(call, "stop ") {
				t.Fatalf("active desktop was stopped: %s", call)
			}
		}
		if strings.Contains(strings.ToLower(logs.String()), "token=") {
			t.Fatalf("log:\n%s", logs.String())
		}
	})

	t.Run("ensure_after_flush_stays_running", func(t *testing.T) {
		logs := captureLog(t)
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.ComputerDir = t.TempDir()
		// The reaper's ps list names the fixture containers. Ensure addresses
		// the same user by the real container name.
		name := containerName("tenant-a", "k1")
		docker.running[name] = true
		ensured := make(chan error, 1)
		docker.onFlush = func() {
			go func() {
				ensured <- svc.Ensure(context.Background(), Spec{TenantID: "tenant-a", UserID: "k1"})
			}()
			deadline := time.Now().Add(2 * time.Second)
			for !svc.wakeWanted("tenant-a", "k1") {
				if time.Now().After(deadline) {
					t.Fatal("ensure did not record a wake before the user lock")
				}
				runtime.Gosched()
			}
		}
		svc.VolumeSync = func(context.Context, SleepMeta, []desktop.Mount) (VolumeSyncResult, error) {
			t.Fatal("sync ran after ensure")
			return VolumeSyncResult{}, nil
		}
		svc.ReapStage2(context.Background())
		if err := <-ensured; err != nil {
			t.Fatal(err)
		}
		if docker.count("stop --time") != 0 || docker.count(" stop ") != 0 || docker.count("start ") != 0 || !docker.running["maclaw-desktop-k1"] || !docker.running[name] {
			t.Fatalf("ensure after flush stopped the machine: %#v", docker.calls)
		}
		meta, err := svc.ReadComputerMeta("tenant-a", "k1")
		if err != nil || meta.State != "running" || meta.Generation != 0 || meta.SnapshotID != "" {
			t.Fatalf("meta=%+v err=%v", meta, err)
		}
		raw, err := os.ReadFile(filepath.Join(svc.ComputerDir, "computers", "tenant-a", mustKey(t, "tenant-a", "k1"), "meta.json"))
		if err != nil || strings.Contains(string(raw), "token") {
			t.Fatalf("meta file=%q err=%v", raw, err)
		}
		if !strings.Contains(logs.String(), "ensure arrived after flush") || strings.Contains(logs.String(), "via=docker-stop") {
			t.Fatalf("log:\n%s", logs.String())
		}
	})

	t.Run("restart_always_rejected", func(t *testing.T) {
		if err := RejectRestartAlways("always"); err == nil {
			t.Fatal("restart always was accepted")
		}
		if err := RejectRestartAlways(" ALWAYS "); err == nil {
			t.Fatal("restart ALWAYS was accepted")
		}
		if err := RejectRestartAlways(RestartPolicy()); err != nil {
			t.Fatal(err)
		}
		if RestartPolicy() != "unless-stopped" {
			t.Fatalf("policy = %s", RestartPolicy())
		}
		var got string
		svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			got = strings.Join(args, " ")
			return "", nil
		}}
		spec := Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "2g", CPUs: "1", ShmSize: "1g"}
		if err := svc.runContainer(context.Background(), "maclaw-desktop-x", spec, "maclaw-gui:2"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "--restart unless-stopped") || strings.Contains(got, "always") {
			t.Fatalf("run args = %s", got)
		}
	})

	t.Run("warm_start_over_25s", func(t *testing.T) {
		t0 := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
		svc := stoppedDesktop()
		var n int
		svc.Now = func() time.Time {
			n++
			if n%2 == 1 {
				return t0
			}
			return t0.Add(WarmStartBudget)
		}
		if err := svc.Ensure(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}); err != nil {
			t.Fatal(err)
		}
		n = 0
		svc.Now = func() time.Time {
			n++
			if n%2 == 1 {
				return t0
			}
			return t0.Add(WarmStartBudget + time.Nanosecond)
		}
		err := svc.Ensure(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
		wake, ok := err.(WarmStartError)
		if !ok || wake.UserBubble() || !wake.Retryable() || !strings.Contains(err.Error(), "再试一次") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("file_wakes_stopped_container", func(t *testing.T) {
		var calls []string
		running := false
		svc := &Service{
			Run: func(_ context.Context, args ...string) (string, error) {
				joined := strings.Join(args, " ")
				calls = append(calls, joined)
				switch {
				case strings.HasPrefix(joined, "inspect "):
					if running {
						return "true", nil
					}
					return "false", nil
				case strings.HasPrefix(joined, "start "):
					running = true
					return "", nil
				default:
					return "", errors.New("unexpected docker args: " + joined)
				}
			},
			RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
				joined := strings.Join(args, " ")
				calls = append(calls, "cmd "+joined)
				if strings.Contains(joined, " /desktop_supervisor.py command ") {
					return "hi\n", 0, nil
				}
				return "hello", 0, nil
			},
		}
		srv := httptest.NewServer(Handler(svc, "tok", "", nil, nil))
		defer srv.Close()
		post := func(path, body string) *http.Response {
			t.Helper()
			req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer tok")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			return resp
		}
		file := post("/v1/desktops/file", `{"tenant_id":"tenant","user_id":"alice","action":"read","path":"/home/desktop/a.txt"}`)
		fileBody, _ := io.ReadAll(file.Body)
		file.Body.Close()
		if file.StatusCode != http.StatusOK || !strings.Contains(string(fileBody), "hello") {
			t.Fatalf("file status=%d body=%s", file.StatusCode, fileBody)
		}
		bash := post("/v1/desktops/bash", `{"tenant_id":"tenant","user_id":"alice","command":"echo hi"}`)
		bashBody, _ := io.ReadAll(bash.Body)
		bash.Body.Close()
		if bash.StatusCode != http.StatusOK || !strings.Contains(string(bashBody), "hi") {
			t.Fatalf("bash status=%d body=%s", bash.StatusCode, bashBody)
		}
		starts := 0
		for _, call := range calls {
			if strings.HasPrefix(call, "start ") {
				starts++
			}
			if strings.Contains(call, "commit") || strings.HasPrefix(call, "rm ") || strings.Contains(call, "always") {
				t.Fatalf("wake changed the container: %s", call)
			}
		}
		if starts != 1 || !running {
			t.Fatalf("starts=%d running=%v calls=%v", starts, running, calls)
		}
	})

	t.Run("apt_or_bash_stays_up", func(t *testing.T) {
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.VolumeSync = readySync()
		release := svc.holdWork("tenant-a", "k1")
		defer release()
		svc.ReapStage2(context.Background())
		if docker.count("stop --time") != 0 || docker.count(" flush ") != 0 {
			t.Fatalf("busy user was slept: %#v", docker.calls)
		}
	})

	t.Run("idle_under_five_minutes_stays", func(t *testing.T) {
		docker := &reaperDocker{idleOverride: 299}
		svc := newReaperService(docker)
		svc.Stage2Sleep = true
		svc.VolumeSync = readySync()
		svc.ReapStage2(context.Background())
		if docker.count("stop --time") != 0 || docker.count(" flush ") != 0 {
			t.Fatalf("four minutes fifty-nine seconds was slept: %#v", docker.calls)
		}
	})
}

func readySync() func(context.Context, SleepMeta, []desktop.Mount) (VolumeSyncResult, error) {
	return func(context.Context, SleepMeta, []desktop.Mount) (VolumeSyncResult, error) {
		return VolumeSyncResult{Generation: "g1", Volumes: []string{"desktops", "home", "opt", "usr-local"}}, nil
	}
}

func stoppedDesktop() *Service {
	running := false
	return &Service{Run: func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "inspect "):
			if running {
				return "true", nil
			}
			return "false", nil
		case strings.HasPrefix(joined, "start "):
			running = true
			return "", nil
		default:
			return "", errors.New("unexpected docker args: " + joined)
		}
	}}
}
