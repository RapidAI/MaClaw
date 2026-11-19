package botmgmt

import (
	"context"
	"errors"
	"testing"
)

// With no grants stored at all, bot access is on for everyone.
func TestLoadForUserDefaultsOpenWithoutGrants(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	ctx := context.Background()
	if _, err := svc.ListBotsForUser(ctx, "tenant-a", "nobody-special"); err != nil {
		t.Fatalf("default-open access: %v", err)
	}
}

func TestFilterGrantedDefaultsOpenWithoutGrants(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	ctx := context.Background()
	subjects := []GrantSubject{{ID: "alice"}, {ID: "bob"}}
	got := svc.FilterGranted(ctx, "tenant-a", subjects)
	if len(got) != 2 {
		t.Fatalf("empty grants should keep everyone, got %v", got)
	}
}

// A cloud-computer request is refused when no cloud desktop is assigned;
// the no-computer path stays open.
func TestCreateBotCloudRequiresAssignment(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	svc.CloudDesktopAssigned = func(context.Context, string, string, []string) error {
		return ErrCloudDesktopUnassigned
	}
	ctx := context.Background()
	yes := true
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "cloud-bot", "", &yes); !errors.Is(err, ErrCloudDesktopUnassigned) {
		t.Fatalf("cloud request without assignment: %v", err)
	}
	no := false
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "plain-bot", "", &no); err != nil {
		t.Fatalf("no-computer bot must not need an assignment: %v", err)
	}
}

func TestCreateBotCloudSucceedsWhenAssigned(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	svc.CloudDesktopAssigned = func(context.Context, string, string, []string) error { return nil }
	ctx := context.Background()
	yes := true
	bot, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "cloud-bot", "", &yes)
	if err != nil {
		t.Fatal(err)
	}
	if bot.ID == "" {
		t.Fatal("empty bot id")
	}
}

func TestDesktopQuotaReportsAvailability(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	svc.CloudDesktopAssigned = func(context.Context, string, string, []string) error {
		return ErrCloudDesktopUnassigned
	}
	ctx := context.Background()
	quota, err := svc.DesktopQuotaForUser(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if quota.CloudDesktop {
		t.Fatal("cloud desktop should read unavailable")
	}
	svc.CloudDesktopAssigned = func(context.Context, string, string, []string) error { return nil }
	quota, err = svc.DesktopQuotaForUser(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !quota.CloudDesktop {
		t.Fatal("cloud desktop should read available")
	}
}
