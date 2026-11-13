package desktopd

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestParseMemUsage(t *testing.T) {
	got, ok := ParseMemUsage("2.85GiB / 3GiB")
	if !ok || got.LimitBytes != 3*1024*1024*1024 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if got.Percent < 0.94 || got.Percent > 0.96 {
		t.Fatalf("percent %v", got.Percent)
	}
	if _, ok := ParseMemUsage("nope"); ok {
		t.Fatal("accepted a non-stat")
	}
}

func TestMemoryStatsTheOwnerContainer(t *testing.T) {
	owner := containerName("tenant-a", "alice")
	var got string
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		got = strings.Join(args, " ")
		return "1.2GiB / 3GiB", nil
	}}
	rep, err := svc.Memory(context.Background(), "tenant-a", desktop.BotDesktopUser("alice", "bot1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, " "+owner) {
		t.Fatalf("stats %q want %s", got, owner)
	}
	botName := containerName("tenant-a", desktop.BotDesktopUser("alice", "bot1"))
	if botName == owner || strings.Contains(got, botName) {
		t.Fatalf("stats used the bot container %q in %q", botName, got)
	}
	if rep.LimitBytes != 3*1024*1024*1024 || rep.UsedBytes == 0 {
		t.Fatalf("%+v", rep)
	}
}
