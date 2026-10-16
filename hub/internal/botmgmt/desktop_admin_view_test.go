package botmgmt

import (
	"context"
	"testing"
	"time"
)

type adminViewDesktop struct {
	url  string
	stop int
}

func (d *adminViewDesktop) Open(context.Context, string, string) (string, error) {
	return d.url, nil
}

func (d *adminViewDesktop) Stop(context.Context, string, string) error {
	d.stop++
	return nil
}

type adminViewSettings struct {
	value string
}

func (m *adminViewSettings) Get(context.Context, string) (string, error) {
	if m.value == "" {
		return "", context.Canceled
	}
	return m.value, nil
}

func (m *adminViewSettings) Set(_ context.Context, _, valueJSON string) error {
	m.value = valueJSON
	return nil
}

// An admin check is a hold, not a login: it keeps the desktop up without the
// keyboard, and it has to work after a restart.
func TestAdminDesktopViewHoldsAcrossRestartAndFailure(t *testing.T) {
	settings := &adminViewSettings{}
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

	// One record has to exist before the desktop state can be hydrated and
	// persisted, which is what a real tenant always has.
	svc1 := NewService(settings)
	svc1.Now = func() time.Time { return base }
	if _, err := svc1.SaveConnection(context.Background(), "tenant-a", "http://maclawsrv.example", "tok", true); err != nil {
		t.Fatal(err)
	}
	svc1.Desktop = &adminViewDesktop{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc1.NoteDesktopAdminView("tenant-a", "alice")

	// Hub restarts, then a bot command fails while the admin is still watching.
	svc2 := NewService(settings)
	desk := &adminViewDesktop{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc2.Desktop = desk
	svc2.Now = func() time.Time { return base.Add(time.Minute) }
	svc2.finishDesktopOpen("tenant-a", "alice", "inst_alice", true, true)
	if desk.stop != 0 {
		t.Fatalf("stops=%d, a failed command closed the desktop the admin is checking", desk.stop)
	}

	// The hold is a hold, not a lock: once it expires the same failure stops it.
	svc2.Now = func() time.Time { return base.Add(AdminDesktopViewHold + time.Minute) }
	svc2.finishDesktopOpen("tenant-a", "alice", "inst_alice", true, true)
	if desk.stop != 1 {
		t.Fatalf("stops=%d, want 1 after the admin hold expired", desk.stop)
	}
}

// An admin check must not look like a login handoff: the bot keeps the
// keyboard, and the desktop stays up for the admin without any pin held.
func TestAdminDesktopViewDoesNotHandOverTheKeyboard(t *testing.T) {
	settings := &adminViewSettings{}
	svc := NewService(settings)
	if _, err := svc.SaveConnection(context.Background(), "tenant-a", "http://maclawsrv.example", "tok", true); err != nil {
		t.Fatal(err)
	}
	svc.Desktop = &adminViewDesktop{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.NoteDesktopAdminView("tenant-a", "alice")

	if svc.desktopHeldByPerson("tenant-a", "alice") {
		t.Fatal("an admin check pinned the desktop as if a person were logging in")
	}
	if svc.desktopKeyboardForBot("tenant-a", "alice", "bot_alice") {
		t.Fatal("an admin check handed the keyboard to a bot")
	}
	if svc.ReleaseDesktopIfIdle("tenant-a", "alice") {
		t.Fatal("an admin check released the desktop while the admin is watching")
	}
}
