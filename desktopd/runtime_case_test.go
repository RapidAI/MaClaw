package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Stage 1. DockerRuntime wraps today's create, start, stop, and exec.
// A running container is not rebuilt. This is not Firecracker.
func TestDesktopBotRuntimeCases(t *testing.T) {
	t.Run("docker_runtime_wraps_today", func(t *testing.T) {
		ownerKey, err := desktop.UserKey("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		name := "maclaw-desktop-" + ownerKey
		spec := Spec{TenantID: "tenant", UserID: "alice"}

		running := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "inspect ") && strings.Contains(joined, "{{.State.Running}}") {
				return "true", nil
			}
			t.Fatalf("ensure of a running desktop called %s", joined)
			return "", nil
		}}
		if err := running.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}

		var missingCalls []string
		missing := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			missingCalls = append(missingCalls, strings.Join(args, " "))
			return "", errors.New("Error: No such container")
		}}
		if err := missing.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		if len(missingCalls) != 1 || strings.Contains(strings.Join(missingCalls, "\n"), " run ") || strings.Contains(missingCalls[0], "start ") {
			t.Fatalf("missing ensure created a container: %v", missingCalls)
		}

		refused := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			t.Fatalf("refused exec reached docker: %s", strings.Join(args, " "))
			return "", nil
		}}
		if _, err := (DockerRuntime{s: refused}).Exec(context.Background(), spec, ExecRequest{Kind: ExecShell, Args: []string{"rm", "-f", name}}); err == nil {
			t.Fatal("rm was accepted as exec")
		}
		if _, err := (DockerRuntime{s: refused}).Exec(context.Background(), spec, ExecRequest{Kind: ExecShell, Args: []string{"run", name}}); err == nil {
			t.Fatal("run was accepted as exec")
		}
		if _, err := (DockerRuntime{s: refused}).Exec(context.Background(), spec, ExecRequest{Kind: "firecracker", Args: []string{"exec", name}}); err == nil {
			t.Fatal("an unknown exec kind was accepted")
		}

		var fileCalls []string
		files := &Service{RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
			fileCalls = append(fileCalls, strings.Join(args, " "))
			return "hello", 0, nil
		}}
		got, err := files.ReadFile(context.Background(), "tenant", "alice", "/home/desktop/notes.txt")
		if err != nil || got != "hello" {
			t.Fatalf("read = %q %v", got, err)
		}
		if len(fileCalls) != 1 || !strings.HasPrefix(fileCalls[0], "exec "+name+" ") {
			t.Fatalf("file exec = %v", fileCalls)
		}
		bashOut, err := files.Bash(context.Background(), "tenant", "alice", "echo hi")
		if err != nil || !strings.Contains(bashOut, "hello") {
			t.Fatalf("bash = %q %v", bashOut, err)
		}
		if len(fileCalls) != 2 || !strings.HasPrefix(fileCalls[1], "exec "+name+" ") || !strings.Contains(fileCalls[1], " command ") {
			t.Fatalf("bash exec = %v", fileCalls)
		}
		if shell := commandPayload(t, strings.Fields(fileCalls[1])); shell.Op != "shell" || shell.Command != "echo hi" || shell.Display != "" {
			t.Fatalf("bash request = %+v", shell)
		}

		var snapCalls []string
		synced := false
		snap := &Service{
			Run: func(_ context.Context, args ...string) (string, error) {
				joined := strings.Join(args, " ")
				snapCalls = append(snapCalls, joined)
				if strings.Contains(joined, " flush ") {
					return "", nil
				}
				t.Fatalf("snapshot docker %s", joined)
				return "", nil
			},
			VolumeSync: func(_ context.Context, meta SleepMeta, _ []desktop.Mount) (VolumeSyncResult, error) {
				synced = true
				if meta.Runtime != "docker" || strings.Contains(strings.ToLower(meta.Generation+meta.UserKey), "token") {
					t.Fatalf("sync meta = %+v", meta)
				}
				return VolumeSyncResult{Generation: "g1", Volumes: append([]string(nil), sleepVolumeNames...)}, nil
			},
		}
		if err := (DockerRuntime{s: snap}).Snapshot(context.Background(), spec); err != nil || !synced {
			t.Fatalf("snapshot err=%v synced=%v calls=%v", err, synced, snapCalls)
		}
		forbidRebuild(t, snapCalls)
		for _, call := range snapCalls {
			if strings.HasPrefix(call, "stop ") {
				t.Fatalf("snapshot stopped the container: %s", call)
			}
		}
		noSync := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			return "", nil
		}}
		if err := (DockerRuntime{s: noSync}).Snapshot(context.Background(), spec); err == nil {
			t.Fatal("snapshot without a syncer stopped or succeeded")
		}

		var stopCalls []string
		stopped := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			stopCalls = append(stopCalls, strings.Join(args, " "))
			return "", nil
		}}
		out, err := stopped.Stop(context.Background(), "tenant", "alice")
		if err != nil || out.Status != "stopped" {
			t.Fatalf("stop = %+v %v calls=%v", out, err, stopCalls)
		}
		if len(stopCalls) != 2 || !strings.Contains(stopCalls[0], " flush ") || !strings.Contains(stopCalls[1], "stop --time "+desktopStopGrace) {
			t.Fatalf("stop calls = %v", stopCalls)
		}
		forbidRebuild(t, stopCalls)
		bot := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			t.Fatalf("bot stop reached docker: %s", strings.Join(args, " "))
			return "", nil
		}}
		botOut, err := bot.Stop(context.Background(), "tenant", "alice::desk::bot_1")
		if err != nil || botOut.Status != "idle" {
			t.Fatalf("bot stop = %+v %v", botOut, err)
		}

		mounts, err := desktop.PrivateMounts("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		mountText := make([]string, 0, len(mounts))
		for _, mount := range mounts {
			mountText = append(mountText, mount.Volume+"="+mount.Target)
		}
		inspectLine := "true|1g|" + strings.Join(mountText, " ") + "|maclaw-gui:2|maclaw-gui:2|"
		var restoreCalls []string
		restore := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			restoreCalls = append(restoreCalls, joined)
			switch args[0] {
			case "inspect":
				return inspectLine, nil
			case "image":
				if strings.Contains(joined, ":state") {
					return "", errors.New("Error: No such image")
				}
				return "sha256:img", nil
			case "update":
				return "", nil
			case "exec":
				if strings.Contains(joined, " ensure ") {
					return `{"display":":20","cdp_port":19020,"token":"` + strings.Repeat("ab", 16) + `"}`, nil
				}
				t.Fatalf("restore exec %s", joined)
			default:
				t.Fatalf("restore docker %s", joined)
			}
			return "", nil
		}}
		restored, err := (DockerRuntime{s: restore}).Restore(context.Background(), spec)
		if err != nil || restored.Container != name || !strings.Contains(restored.EnsureOut, `"display":":20"`) {
			t.Fatalf("restore = %+v %v", restored, err)
		}
		forbidRebuild(t, restoreCalls)
		joinedRestore := strings.Join(restoreCalls, "\n")
		if strings.Contains(joinedRestore, " commit ") || !strings.Contains(joinedRestore, " ensure ") {
			t.Fatalf("restore calls =\n%s", joinedRestore)
		}

		root := t.TempDir()
		metaDir := filepath.Join(root, "computers", "tenant", ownerKey)
		if err := os.MkdirAll(metaDir, 0o700); err != nil {
			t.Fatal(err)
		}
		metaBody := []byte(`{"state":"running","runtime":"docker","snapshot_id":"","generation":0,"last_activity":"2026-10-18T04:00:00Z","screens":[{"screen":1,"display":":20"}]}`)
		if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), metaBody, 0o600); err != nil {
			t.Fatal(err)
		}
		statusSvc := &Service{
			ComputerDir: root,
			Run: func(_ context.Context, args ...string) (string, error) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "{{.State.Running}}") {
					return "true", nil
				}
				t.Fatalf("status docker %s", joined)
				return "", nil
			},
		}
		status, err := statusSvc.MachineStatus(context.Background(), spec)
		if err != nil || status.State != "running" || status.Runtime != "docker" || len(status.Screens) != 1 || status.Screens[0].Display != ":20" {
			t.Fatalf("status = %+v %v", status, err)
		}
		encoded, _ := json.Marshal(status)
		if strings.Contains(string(encoded), "sleeping") || strings.Contains(string(encoded), "waking") || strings.Contains(string(encoded), "token") {
			t.Fatalf("status json = %s", encoded)
		}
		after, err := os.ReadFile(filepath.Join(metaDir, "meta.json"))
		if err != nil || string(after) != string(metaBody) {
			t.Fatal("status rewrote meta.json")
		}
		absent := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			return "", errors.New("Error: No such container")
		}}
		absentStatus, err := (DockerRuntime{s: absent}).Status(context.Background(), spec)
		if err != nil || absentStatus.State != "absent" || absentStatus.State == "sleeping" {
			t.Fatalf("absent = %+v %v", absentStatus, err)
		}
		down := &Service{Run: func(_ context.Context, _ ...string) (string, error) {
			return "false", nil
		}}
		downStatus, err := (DockerRuntime{s: down}).Status(context.Background(), spec)
		if err != nil || downStatus.State != "absent" {
			t.Fatalf("stopped = %+v %v", downStatus, err)
		}

		tape := &runtimeTape{}
		wired := &Service{AdvertiseHost: "docker.example", Machines: tape, Stage2Sleep: true}
		wired.Run = func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			switch args[0] {
			case "ps":
				return name + "\ttenant\talice", nil
			case "exec":
				if strings.Contains(joined, " idle ") {
					return `{"running":true,"idle_seconds":600,"known":true}`, nil
				}
				return "", errors.New("no bots")
			default:
				t.Fatalf("reaper docker %s", joined)
			}
			return "", nil
		}
		if err := wired.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		if _, err := wired.Stop(context.Background(), "tenant", "alice"); err != nil {
			t.Fatal(err)
		}
		if _, err := wired.App(context.Background(), "tenant", "alice", ":20", []string{"key", "Return"}); err != nil {
			t.Fatal(err)
		}
		if _, err := wired.ReadFile(context.Background(), "tenant", "alice", "/home/desktop/notes.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := wired.Bash(context.Background(), "tenant", "alice", "echo hi"); err != nil {
			t.Fatal(err)
		}
		if _, err := wired.Ctl(context.Background(), "tenant", "alice", ":20", map[string]any{"op": "screen"}); err != nil {
			t.Fatal(err)
		}
		_, _, shotErr := wired.CaptureShot(context.Background(), "tenant", "alice", ":20", "", desktop.ShotOptions{})
		if !strings.Contains(strings.Join(tape.hits, " "), "exec:"+ExecShot) {
			t.Fatalf("shot err=%v hits=%v", shotErr, tape.hits)
		}
		if _, err := wired.OpenSession(context.Background(), spec); err == nil || !strings.Contains(strings.Join(tape.hits, " "), "restore") {
			t.Fatalf("open hits=%v err=%v", tape.hits, err)
		}
		if _, err := wired.MachineStatus(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		wired.ReapStage2(context.Background())
		want := []string{"ensure", "stop", "exec:" + ExecClick, "exec:" + ExecFile, "exec:" + ExecShell, "exec:" + ExecCtl, "exec:" + ExecShot, "restore", "status", "snapshot"}
		if strings.Join(tape.hits, ",") != strings.Join(want, ",") {
			t.Fatalf("seam = %v", tape.hits)
		}
	})
}

func forbidRebuild(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if strings.HasPrefix(call, "rm ") || strings.HasPrefix(call, "run ") || strings.Contains(call, " commit ") {
			t.Fatalf("runtime rebuilt the container: %s", call)
		}
	}
}

type runtimeTape struct {
	hits []string
}

func (t *runtimeTape) Ensure(context.Context, Spec) error {
	t.hits = append(t.hits, "ensure")
	return nil
}

func (t *runtimeTape) Exec(_ context.Context, _ Spec, req ExecRequest) (ExecResult, error) {
	t.hits = append(t.hits, "exec:"+req.Kind)
	if len(req.Args) == 0 || req.Args[0] != "exec" {
		return ExecResult{}, errors.New("not exec")
	}
	if req.Kind == ExecCtl {
		return ExecResult{Out: `{"ok":true}`}, nil
	}
	return ExecResult{Out: "hello"}, nil
}

func (t *runtimeTape) Snapshot(context.Context, Spec) error {
	t.hits = append(t.hits, "snapshot")
	return errors.New("sync left the container up")
}

func (t *runtimeTape) Restore(context.Context, Spec) (RestoreResult, error) {
	t.hits = append(t.hits, "restore")
	return RestoreResult{}, errors.New("restore seam")
}

func (t *runtimeTape) Stop(context.Context, Spec) (Desktop, error) {
	t.hits = append(t.hits, "stop")
	return Desktop{Status: "stopped"}, nil
}

func (t *runtimeTape) Status(context.Context, Spec) (RuntimeStatus, error) {
	t.hits = append(t.hits, "status")
	return RuntimeStatus{State: "running", Runtime: "docker"}, nil
}
