package desktopd

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)


func TestReaperStopsOnlyIdleDesktops(t *testing.T) {
	key1, err := desktop.UserKey("tenant-a", "k1")
	if err != nil {
		t.Fatal(err)
	}
	key2, err := desktop.UserKey("tenant-a", "k2")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var calls []string
	runner := func(ctx context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, joined)
		switch {
		case strings.HasPrefix(joined, "ps --filter"):
			return "maclaw-desktop-k1\ttenant-a\tk1\nmaclaw-desktop-k2\ttenant-a\tk2", nil
		case strings.HasSuffix(joined, " idle "+key1):
			return `{"running":true,"idle_seconds":7200,"known":true}`, nil
		case strings.HasSuffix(joined, " idle "+key2):
			return `{"running":true,"idle_seconds":60,"known":true}`, nil
		}
		return "", nil
	}
	svc := &Service{Run: runner, AdvertiseHost: "dockerd.example"}
	r := &reaper{interval: time.Minute, maxIdle: time.Hour}
	r.once(context.Background(), svc)

	mu.Lock()
	defer mu.Unlock()
	stops := 0
	for _, call := range calls {
		if strings.HasSuffix(call, " stop "+key1) {
			stops++
		}
		if strings.HasSuffix(call, " stop "+key2) {
			t.Fatal("active desktop was reaped")
		}
	}
	if stops != 1 {
		t.Fatalf("idle desktop was not stopped exactly once: %d", stops)
	}
}
