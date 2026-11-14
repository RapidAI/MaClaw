package desktopd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// fakeHost models docker for the workspace: the desktop and the workspace
// containers, each missing, stopped, or running.
type fakeHost struct {
	desktop   string // "", "stopped", "running"
	work      string
	workImage string
	calls     []string
	execs     []string
}

func (h *fakeHost) packageMountText() string {
	mounts, _ := desktop.UserMounts("tenant", "alice")
	var b strings.Builder
	for _, m := range mounts {
		b.WriteString(m.Volume + "=" + m.Target + " ")
	}
	return b.String()
}

func (h *fakeHost) run(_ context.Context, a ...string) (string, error) {
	line := strings.Join(a, " ")
	h.calls = append(h.calls, line)
	desk := containerName("tenant", "alice")
	work := workspaceName("tenant", "alice")
	missing := errors.New("Error: No such object")
	switch {
	case a[0] == "volume" && a[1] == "create":
		return a[2], nil
	case a[0] == "run" && strings.Contains(line, "/seed") && !strings.Contains(line, "--name "+work):
		// seed emptiness probe or copy
		if strings.Contains(line, "echo EMPTY") {
			return "EMPTY", nil
		}
		return "", nil
	case a[0] == "inspect" && a[len(a)-1] == desk:
		if h.desktop == "" {
			return "", missing
		}
		if strings.Contains(line, "{{.Image}}") {
			return "sha256:state1|maclaw-desktop-user-x:state", nil
		}
		return map[bool]string{true: "true", false: "false"}[h.desktop == "running"], nil
	case a[0] == "inspect" && a[len(a)-1] == work:
		if h.work == "" {
			return "", missing
		}
		if strings.Contains(line, "maclaw.work.image") {
			running := map[bool]string{true: "true", false: "false"}[h.work == "running"]
			return running + "|" + h.workImage + "|" + h.packageMountText(), nil
		}
		return map[bool]string{true: "true", false: "false"}[h.work == "running"], nil
	case a[0] == "run" && strings.Contains(line, "--name "+work):
		h.work, h.workImage = "running", "sha256:state1"
		return "id", nil
	case a[0] == "start" && a[1] == work:
		h.work = "running"
		return "", nil
	case a[0] == "start":
		return "", errors.New("unexpected desktop start: " + line)
	case a[0] == "rm" && a[len(a)-1] == work:
		h.work = ""
		return "", nil
	case a[0] == "stop" && a[len(a)-1] == work:
		h.work = "stopped"
		return "", nil
	}
	return "", nil
}

func (h *fakeHost) service(t *testing.T) *Service {
	return &Service{
		ComputerDir:   t.TempDir(),
		ToolWorkspace: true,
		Run:           h.run,
		RunCommand: func(_ context.Context, in io.Reader, a ...string) (string, int, error) {
			if in != nil {
				_, _ = io.ReadAll(in)
			}
			h.execs = append(h.execs, strings.Join(a, " "))
			return "ok", 0, nil
		},
	}
}

func TestHTTPWhileTheDesktopSleepsRunsInTheWorkspace(t *testing.T) {
	h := &fakeHost{desktop: "stopped"}
	svc := h.service(t)
	svc.RunCommand = func(_ context.Context, _ io.Reader, a ...string) (string, int, error) {
		h.execs = append(h.execs, strings.Join(a, " "))
		return "hello\n__MACLAW_HTTP__200 text/html", 0, nil
	}
	body, status, kind, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err != nil || body != "hello" || status != 200 || kind != "text/html" {
		t.Fatalf("body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	work := workspaceName("tenant", "alice")
	desk := containerName("tenant", "alice")
	for _, c := range h.calls {
		if strings.HasPrefix(c, "start "+desk) {
			t.Fatalf("desktop started: %s", c)
		}
	}
	if len(h.execs) != 1 || !strings.Contains(h.execs[0], "exec "+work+" ") || strings.Contains(h.execs[0], "exec "+desk+" ") {
		t.Fatalf("execs=%v", h.execs)
	}
}

func TestBotBashWhileTheDesktopSleepsStaysInTheWorkspace(t *testing.T) {
	h := &fakeHost{desktop: "stopped"}
	svc := h.service(t)
	bot := desktop.BotDesktopUser("alice", "bot_a")
	if _, err := svc.Bash(context.Background(), "tenant", bot, "python train.py"); err != nil {
		t.Fatal(err)
	}
	work := workspaceName("tenant", "alice")
	for _, c := range h.calls {
		if strings.Contains(c, " ensure ") || strings.HasPrefix(c, "start "+containerName("tenant", "alice")) {
			t.Fatalf("bash opened a desktop: %s", c)
		}
	}
	if len(h.execs) != 1 || !strings.HasPrefix(h.execs[0], "exec "+work+" ") {
		t.Fatalf("execs=%v", h.execs)
	}
}

func TestBashWhileTheDesktopSleepsRunsInTheWorkspace(t *testing.T) {
	h := &fakeHost{desktop: "stopped"}
	svc := h.service(t)
	if err := svc.EnsureTools(context.Background(), Spec{TenantID: "tenant", UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Bash(context.Background(), "tenant", "alice", "echo hi > ~/a.txt"); err != nil {
		t.Fatal(err)
	}
	work := workspaceName("tenant", "alice")
	var run string
	for _, c := range h.calls {
		if strings.HasPrefix(c, "start "+containerName("tenant", "alice")) {
			t.Fatalf("desktop started: %s", c)
		}
		if strings.HasPrefix(c, "run ") {
			run = c
		}
	}
	for _, want := range []string{
		"--name " + work, "--restart no", "--entrypoint sh",
		"--volume maclaw-home-", ":/home/desktop", ":/desktops", ":/opt", ":/usr/local",
		"--volume maclaw-usr-", ":/usr", "maclaw-dpkg-", "maclaw-etc-", "maclaw-apt-",
		"--label maclaw.work.user=alice", "sha256:state1 -c " + workspaceHold,
	} {
		if !strings.Contains(run, want) {
			t.Fatalf("run lacks %q: %s", want, run)
		}
	}
	for _, bad := range []string{"--publish", "--privileged", "--cap-add", "maclaw.tenant=", "desktop_supervisor.py boot"} {
		if strings.Contains(run, bad) {
			t.Fatalf("run has %q: %s", bad, run)
		}
	}
	if len(h.execs) != 1 || !strings.HasPrefix(h.execs[0], "exec "+work+" ") {
		t.Fatalf("execs = %v", h.execs)
	}

	// A second call reuses the running workspace; a stopped one is started.
	h.calls = nil
	if _, err := svc.ReadFile(context.Background(), "tenant", "alice", "~/a.txt"); err != nil {
		t.Fatal(err)
	}
	h.work = "stopped"
	if _, err := svc.WriteFile(context.Background(), "tenant", "alice", "~/b.txt", "x"); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.calls {
		if strings.HasPrefix(c, "run ") {
			t.Fatalf("workspace recreated: %s", c)
		}
	}
	if !strings.Contains(strings.Join(h.calls, "\n"), "start "+work) {
		t.Fatalf("stopped workspace not started: %v", h.calls)
	}
	if len(h.execs) != 3 || !strings.HasPrefix(h.execs[2], "exec -i "+work+" ") {
		t.Fatalf("execs = %v", h.execs)
	}
}

func TestBashWhileTheDesktopRunsStaysOnTheDesktop(t *testing.T) {
	h := &fakeHost{desktop: "running"}
	svc := h.service(t)
	if _, err := svc.Bash(context.Background(), "tenant", "alice", "ls"); err != nil {
		t.Fatal(err)
	}
	if h.work != "" || len(h.execs) != 1 || !strings.HasPrefix(h.execs[0], "exec "+containerName("tenant", "alice")+" ") {
		t.Fatalf("work=%q execs=%v", h.work, h.execs)
	}
}

func TestWorkspaceFollowsTheDesktopImage(t *testing.T) {
	h := &fakeHost{desktop: "stopped", work: "stopped", workImage: "sha256:old"}
	svc := h.service(t)
	if _, err := svc.ListFile(context.Background(), "tenant", "alice", ""); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(h.calls, "\n")
	if !strings.Contains(all, "rm -f "+workspaceName("tenant", "alice")) || h.workImage != "sha256:state1" {
		t.Fatalf("stale workspace kept: %v", h.calls)
	}
}

func TestIdleWorkspaceIsStopped(t *testing.T) {
	h := &fakeHost{desktop: "stopped", work: "running", workImage: "sha256:state1"}
	svc := h.service(t)
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	work := workspaceName("tenant", "alice")
	psLine := work + "\ttenant\talice"
	base := h.run
	svc.Run = func(ctx context.Context, a ...string) (string, error) {
		if a[0] == "ps" {
			return psLine, nil
		}
		return base(ctx, a...)
	}
	// First sight after a restart: one more idle period.
	svc.reapWorkspaces(context.Background(), time.Hour)
	if h.work != "running" {
		t.Fatal("unseen workspace stopped")
	}
	now = now.Add(30 * time.Minute)
	svc.reapWorkspaces(context.Background(), time.Hour)
	if h.work != "running" {
		t.Fatal("recent workspace stopped")
	}
	now = now.Add(31 * time.Minute)
	release := svc.holdWork("tenant", "alice")
	svc.reapWorkspaces(context.Background(), time.Hour)
	if h.work != "running" {
		t.Fatal("busy workspace stopped")
	}
	release()
	svc.reapWorkspaces(context.Background(), time.Hour)
	if h.work != "stopped" {
		t.Fatal("idle workspace left running")
	}
}

func TestExecAllowsTheWorkspaceOnlyForBashAndFiles(t *testing.T) {
	spec := Spec{TenantID: "tenant", UserID: "alice"}
	work := workspaceName("tenant", "alice")
	for _, kind := range []string{ExecFile, ExecShell} {
		if err := execAllowed(spec, ExecRequest{Kind: kind, Args: []string{"exec", work, "true"}}); err != nil {
			t.Fatalf("%v: %v", kind, err)
		}
	}
	if err := execAllowed(spec, ExecRequest{Kind: ExecShot, Args: []string{"exec", work, "true"}}); err == nil {
		t.Fatal("screenshot in the workspace allowed")
	}
	if err := execAllowed(spec, ExecRequest{Kind: ExecShell, Args: []string{"exec", workspaceName("tenant", "bob"), "true"}}); err == nil {
		t.Fatal("another user's workspace allowed")
	}
}

func TestToolWorkspaceOffKeepsTheDesktopWake(t *testing.T) {
	h := &fakeHost{desktop: "stopped"}
	svc := h.service(t)
	svc.ToolWorkspace = false
	if _, err := svc.Bash(context.Background(), "tenant", "alice", "ls"); err != nil {
		t.Fatal(err)
	}
	if h.work != "" || !strings.HasPrefix(h.execs[0], "exec "+containerName("tenant", "alice")+" ") {
		t.Fatalf("execs = %v", h.execs)
	}
}

func TestPackageVolumesAreSharedAcrossDesktopAndWorkspace(t *testing.T) {
	pkg, err := desktop.PackageMounts("tenant", "alice")
	if err != nil || len(pkg) != 4 {
		t.Fatalf("pkg=%v err=%v", pkg, err)
	}
	all, err := desktop.UserMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	// Package roots come first so /usr/local can cover /usr.
	if all[0].Target != "/usr" || all[len(all)-1].Target != "/usr/local" {
		t.Fatalf("order = %v", all)
	}
	spec := Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "1g", CPUs: "1", ShmSize: "64m"}
	if packageMountsReady("", spec) {
		t.Fatal("empty mounts ready")
	}
	text := ""
	for _, m := range pkg {
		text += m.Volume + "=" + m.Target + " "
	}
	if !packageMountsReady(text, spec) {
		t.Fatal("package mounts not ready")
	}
}
