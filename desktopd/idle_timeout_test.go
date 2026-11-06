package desktopd

import (
	"testing"
	"time"
)

func TestDesktopIdleDefaultIsTwentyMinutes(t *testing.T) {
	if DesktopIdleDefault != 20*time.Minute {
		t.Fatalf("DesktopIdleDefault=%v", DesktopIdleDefault)
	}
	if WorkspaceIdleDefault != 24*time.Hour {
		t.Fatalf("WorkspaceIdleDefault=%v", WorkspaceIdleDefault)
	}
}

func TestReaperIntervalShortensForShortIdle(t *testing.T) {
	if got := ReaperInterval(20 * time.Minute); got != time.Minute {
		t.Fatalf("20m -> %v", got)
	}
	if got := ReaperInterval(5 * time.Minute); got != time.Minute {
		t.Fatalf("5m -> %v", got)
	}
	if got := ReaperInterval(24 * time.Hour); got != 10*time.Minute {
		t.Fatalf("24h -> %v", got)
	}
	if got := ReaperInterval(0); got != 10*time.Minute {
		t.Fatalf("0 -> %v", got)
	}
}

func TestParseMaxIdle(t *testing.T) {
	d, err := ParseMaxIdle("")
	if err != nil || d != 0 {
		t.Fatalf("empty: %v %v", d, err)
	}
	d, err = ParseMaxIdle("20m")
	if err != nil || d != 20*time.Minute {
		t.Fatalf("20m: %v %v", d, err)
	}
	if _, err = ParseMaxIdle("nope"); err == nil {
		t.Fatal("bad duration accepted")
	}
}

func TestParseWorkspaceMaxIdleDefaultsLong(t *testing.T) {
	d, err := ParseWorkspaceMaxIdle("")
	if err != nil || d != WorkspaceIdleDefault {
		t.Fatalf("empty: %v %v", d, err)
	}
	d, err = ParseWorkspaceMaxIdle("2h")
	if err != nil || d != 2*time.Hour {
		t.Fatalf("2h: %v %v", d, err)
	}
}

func TestWorkspaceIdleIndependentOfDesktop(t *testing.T) {
	svc := &Service{}
	if got := svc.workspaceIdle(); got != WorkspaceIdleDefault {
		t.Fatalf("default %v", got)
	}
	svc.WorkspaceMaxIdle = 2 * time.Hour
	if got := svc.workspaceIdle(); got != 2*time.Hour {
		t.Fatalf("override %v", got)
	}
}
