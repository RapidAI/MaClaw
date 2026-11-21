package cua

import (
	"context"
	"testing"
	"time"
)

// TestOpenDefaultAgainstLiveDriver requires a reachable driver (set
// MACLAW_CUA_SOCKET to a `cua-driver serve` socket). CI without one must
// skip; a failure here is a real protocol break, not an environment quirk.
func TestOpenDefaultAgainstLiveDriver(t *testing.T) {
	d, err := OpenDefault(context.Background())
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	defer d.Close()
	health, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got, _ := health["overall"].(string); got != "ok" {
		t.Fatalf("overall = %q, want ok: %v", got, health)
	}
}

// TestOpenDefaultLiveDriverAgainstServe is a stronger live test: it needs a
// real `cua-driver serve` running (MACLAW_CUA_SOCKET set to its socket).
// The Open() liveness check must pass, and a real tool call must return
// structured data. Skipped when the socket is not a live serve daemon.
func TestOpenDefaultLiveDriverAgainstServe(t *testing.T) {
	d, err := OpenDefault(context.Background())
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	defer d.Close()
	wins, err := d.Call(context.Background(), "list_windows", map[string]any{})
	if err != nil {
		t.Fatalf("list_windows: %v", err)
	}
	if got, _ := wins["windows"].([]any); len(got) == 0 {
		t.Fatalf("list_windows returned no windows: %v", wins)
	}
}

// TestErrNoDriverWithoutSocket guards the unset-env path.
func TestErrNoDriverWithoutSocket(t *testing.T) {
	t.Setenv(EnvSocket, "")
	if _, err := OpenDefault(context.Background()); err != ErrNoDriver {
		t.Fatalf("got %v, want ErrNoDriver", err)
	}
}

// TestPipePathNormalization checks the address forms hosts pass.
func TestPipePathNormalization(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`\\.\pipe\openbot-cua-1`, `\\.\pipe\openbot-cua-1`},
		{`\\.\pipe\openbot-cua-2`, `\\.\pipe\openbot-cua-2`},
		{`openbot-cua-3`, `\\.\pipe\openbot-cua-3`},
	}
	for _, c := range cases {
		if got := pipePath(c.in); got != c.want {
			t.Errorf("pipePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAwaitContextDeadline ensures a wedged read does not outlive ctx.
func TestAwaitContextDeadline(t *testing.T) {
	rt := &runtime{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := rt.await(1, ctx)
	if err == nil {
		t.Fatalf("expected an error from a closed/unconnected await")
	}
}
