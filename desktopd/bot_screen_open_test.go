package desktopd

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestBotBashDoesNotOpenAMissingScreen(t *testing.T) {
	bot := desktop.BotDesktopUser("alice", "bot_a")
	var mu sync.Mutex
	ensured := 0
	svc := botScreenService(&mu, &ensured)
	text, err := svc.Bash(context.Background(), "tenant", bot, "echo hi")
	if err != nil || text != "hi" {
		t.Fatalf("bash=%q err=%v", text, err)
	}
	if ensured != 0 {
		t.Fatalf("ensured=%d, bash opened a screen", ensured)
	}
	text, err = svc.Bash(context.Background(), "tenant", bot, "echo hi")
	if err != nil || text != "hi" || ensured != 0 {
		t.Fatalf("second bash=%q err=%v ensured=%d", text, err, ensured)
	}
}

func TestClosedScreenDisplayIsNotADisplay(t *testing.T) {
	bot := desktop.BotDesktopUser("alice", "bot_a")
	svc := &Service{
		Run: func(_ context.Context, args ...string) (string, error) {
			if strings.Contains(strings.Join(args, " "), " display ") {
				return `{"display":":21","running":false}`, nil
			}
			return "", nil
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			return "cannot open display\n", 1, nil
		},
	}
	text, err := svc.Bash(context.Background(), "tenant", bot, "firefox")
	var closed *desktopbot.ScreenClosedError
	if !errors.As(err, &closed) || !strings.Contains(text, "cannot open display") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestThreeBotsBashWithoutOpeningScreens(t *testing.T) {
	var mu sync.Mutex
	ensured := map[string]int{}
	svc := &Service{
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			bot := botArg(joined)
			if strings.Contains(joined, " display ") {
				mu.Lock()
				opened := ensured[bot] > 0
				mu.Unlock()
				if !opened {
					return "", errors.New("closed")
				}
				return `{"display":":21"}`, nil
			}
			if strings.Contains(joined, " ensure ") {
				mu.Lock()
				ensured[bot]++
				mu.Unlock()
				return `{"display":":21","cdp_port":19020,"token":"abcdabcdabcdabcd"}`, nil
			}
			return "", nil
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			return "ok\n", 0, nil
		},
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	for _, id := range []string{"bot_a", "bot_b", "bot_c"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			text, err := svc.Bash(context.Background(), "tenant", desktop.BotDesktopUser("alice", id), "echo hi")
			if err != nil || text != "ok" {
				errCh <- errors.New(id + " bash")
				return
			}
			errCh <- nil
		}(id)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ensured) != 0 {
		t.Fatalf("bash opened screens: %v", ensured)
	}
}

func TestEnsureStartOpensTheNamedBotScreen(t *testing.T) {
	prevWait := botScreenOpenWait
	botScreenOpenWait = 0
	t.Cleanup(func() { botScreenOpenWait = prevWait })
	user := desktop.BotDesktopUser("alice", "bot_a")
	spec, err := normalize(Spec{TenantID: "tenant", UserID: user, WakeReason: WakeReasonBotDesktop})
	if err != nil {
		t.Fatal(err)
	}
	down := "false|" + desktop.DefaultShmSize + "|" + inspectMounts(t, spec) + "|" + desktop.DefaultImage + "|" + desktop.DefaultImage + "|"
	up := "true|" + desktop.DefaultShmSize + "|" + inspectMounts(t, spec) + "|" + desktop.DefaultImage + "|" + desktop.DefaultImage + "|"
	running := false
	var ensured, started int
	svc := &Service{
		EnforceWakeReason: true,
		Run: func(_ context.Context, args ...string) (string, error) {
			if len(args) == 0 {
				return "", nil
			}
			switch args[0] {
			case "inspect":
				if strings.Contains(strings.Join(args, " "), "{{.State.Running}}") && !strings.Contains(strings.Join(args, " "), "maclaw.shm") {
					if running {
						return "true", nil
					}
					return "false", nil
				}
				if running {
					return up, nil
				}
				return down, nil
			case "start":
				started++
				running = true
				return "", nil
			case "exec":
				if strings.Contains(strings.Join(args, " "), " ensure ") {
					ensured++
					return `{"display":":21","cdp_port":19020,"token":"abcdabcdabcdabcd"}`, nil
				}
			}
			return "", nil
		},
	}
	if err := svc.Ensure(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if started != 1 || ensured != 1 {
		t.Fatalf("started=%d ensured=%d", started, ensured)
	}
	// A second bot of the same owner opens its own screen. The shared
	// container start does not do that for it.
	other := spec
	other.BotID = "bot_b"
	other.UserID = "alice"
	if err := svc.Ensure(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if started != 1 || ensured != 2 {
		t.Fatalf("second bot started=%d ensured=%d", started, ensured)
	}
	owner, err := normalize(Spec{TenantID: "tenant", UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	ownerInspect := "true|" + desktop.DefaultShmSize + "|" + inspectMounts(t, owner) + "|" + desktop.DefaultImage + "|" + desktop.DefaultImage + "|"
	before := ensured
	svc.Run = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			return ownerInspect, nil
		}
		if len(args) > 0 && args[0] == "exec" {
			t.Fatal("owner ensure opened a bot screen")
		}
		return "", nil
	}
	if err := svc.Ensure(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if ensured != before {
		t.Fatalf("owner ensure opened a screen: %d -> %d", before, ensured)
	}
}

func botScreenService(mu *sync.Mutex, ensured *int) *Service {
	return &Service{
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			switch {
			case strings.Contains(joined, " display "):
				mu.Lock()
				opened := *ensured > 0
				mu.Unlock()
				if !opened {
					return "", errors.New("closed")
				}
				return `{"display":":21"}`, nil
			case strings.Contains(joined, " ensure "):
				mu.Lock()
				*ensured++
				mu.Unlock()
				return `{"display":":21","cdp_port":19020,"token":"abcdabcdabcdabcd"}`, nil
			default:
				return "", nil
			}
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			return "hi\n", 0, nil
		},
	}
}

func botArg(joined string) string {
	fields := strings.Fields(joined)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func inspectMounts(t *testing.T, spec Spec) string {
	t.Helper()
	priv, err := desktop.PrivateMounts(spec.TenantID, spec.UserID)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := desktop.PackageMounts(spec.TenantID, spec.UserID)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(priv)+len(pkgs))
	for _, mount := range append(priv, pkgs...) {
		parts = append(parts, mount.Volume+"="+mount.Target)
	}
	return strings.Join(parts, " ")
}
