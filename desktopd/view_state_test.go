package desktopd

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestH264PlayerPostsStreamDisconnect(t *testing.T) {
	raw, err := os.ReadFile("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `source:"maclaw-desktop"`) || !strings.Contains(text, `noteStream("disconnect")`) {
		t.Fatal("h264 player does not post a stream disconnect")
	}
}

func TestClassifyDesktopView(t *testing.T) {
	if got := classifyDesktopView(false, false, "http://stale"); got != DesktopAsleep {
		t.Fatalf("stopped container %s", got)
	}
	if got := classifyDesktopView(true, false, "http://stale"); got != DesktopDisplayStopped {
		t.Fatalf("stopped display %s", got)
	}
	if got := classifyDesktopView(true, true, ""); got != DesktopDisplayStopped {
		t.Fatalf("no page %s", got)
	}
	if got := classifyDesktopView(true, true, "http://live"); got != DesktopRunning {
		t.Fatalf("running %s", got)
	}
}

func TestViewStateNamesAStoppedContainerAndAStoppedDisplay(t *testing.T) {
	ownerKey, err := desktop.UserKey("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("d", 32)
	screens := map[string]struct {
		display, token, profile string
		running                 bool
	}{
		ownerKey: {display: ":23", token: token, profile: "/desktops/" + ownerKey + "/screens/23/profile", running: false},
	}
	var calls []string
	containerUp := true
	svc := &Service{AdvertiseHost: "docker.example", Run: func(ctx context.Context, args ...string) (string, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if args[0] == "inspect" {
			if containerUp {
				return "true", nil
			}
			return "false", nil
		}
		return peekDocker(t, &calls, screens)(ctx, args...)
	}}
	stopped, err := svc.ViewState(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
	if err != nil || stopped.State != DesktopDisplayStopped || stopped.Novnc != "" {
		t.Fatalf("display %#v err=%v", stopped, err)
	}
	containerUp = false
	asleep, err := svc.ViewState(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
	if err != nil || asleep.State != DesktopAsleep || asleep.Novnc != "" {
		t.Fatalf("container %#v err=%v", asleep, err)
	}
	joined := strings.Join(calls, "\n")
	for _, bad := range []string{" run ", " rm ", " start ", " ensure ", "restore"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("view woke the desktop via %q", bad)
		}
	}
	screens[ownerKey] = struct {
		display, token, profile string
		running                 bool
	}{display: ":23", token: token, profile: screens[ownerKey].profile, running: true}
	containerUp = true
	live, err := svc.ViewState(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
	if err != nil || live.State != DesktopRunning || !strings.Contains(live.Novnc, token) {
		t.Fatalf("live %#v err=%v", live, err)
	}
	svc.Run = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "inspect" {
			return "", errDocker("inspect failed")
		}
		return "true", nil
	}
	unread, err := svc.ViewState(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
	if err != nil || unread.State != DesktopUnavailable || unread.Novnc != "" {
		t.Fatalf("inspect %#v err=%v", unread, err)
	}
}
