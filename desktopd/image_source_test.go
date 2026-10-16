package desktopd

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSource = "ghcr.io/rapidai/maclaw-gui:2"

// pullDocker fakes the docker commands an image source pull runs.
type pullDocker struct {
	mu        sync.Mutex
	commands  []string
	local     map[string]bool // local image names that exist
	pullErr   error
	contract  error
	pullGate  chan struct{} // when set, pull blocks until it is closed
	onPull    func()        // runs after a successful pull
	pullCount int
}

func newPullDocker(local ...string) *pullDocker {
	f := &pullDocker{local: map[string]bool{}}
	for _, image := range local {
		f.local[image] = true
	}
	return f
}

func (f *pullDocker) run(ctx context.Context, args ...string) (string, error) {
	line := strings.Join(args, " ")
	f.mu.Lock()
	f.commands = append(f.commands, line)
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(line, "image inspect --format {{.Id}} "):
		image := args[len(args)-1]
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.local[image] {
			return "sha256:" + strings.Repeat("a", 12), nil
		}
		return "", errString("Error: No such image: " + image)
	case args[0] == "pull":
		f.mu.Lock()
		f.pullCount++
		gate := f.pullGate
		f.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		if f.pullErr != nil {
			return "", f.pullErr
		}
		f.mu.Lock()
		f.local[args[1]] = true
		f.mu.Unlock()
		if f.onPull != nil {
			f.onPull()
		}
		return "", nil
	case args[0] == "run":
		return "missing startxfce4", f.contract
	case args[0] == "tag":
		f.mu.Lock()
		f.local[args[2]] = true
		f.mu.Unlock()
		return "", nil
	}
	return "", nil
}

func (f *pullDocker) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, line := range f.commands {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func (f *pullDocker) pulls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullCount
}

func sourceService(f *pullDocker) *Service {
	return &Service{Run: f.run, ImageSources: map[string]string{DefaultImage: testSource}, ImagePullTimeout: time.Minute}
}

func TestParseImageSources(t *testing.T) {
	cases := []struct {
		raw  string
		want map[string]string
		bad  bool
	}{
		{"", map[string]string{"maclaw-gui:2": DefaultImageSource}, false},
		{"off", nil, false},
		{"OFF", nil, false},
		{"none", nil, false},
		{"registry.example.com/maclaw-gui:2", map[string]string{"maclaw-gui:2": "registry.example.com/maclaw-gui:2"}, false},
		{"ghcr.io/rapidai/maclaw-gui@sha256:4078a47c", map[string]string{"maclaw-gui:2": "ghcr.io/rapidai/maclaw-gui@sha256:4078a47c"}, false},
		{"maclaw-gui:2=ghcr.io/rapidai/maclaw-gui:2-cc24156, maclaw-gui:3=ghcr.io/rapidai/maclaw-gui:3",
			map[string]string{"maclaw-gui:2": "ghcr.io/rapidai/maclaw-gui:2-cc24156", "maclaw-gui:3": "ghcr.io/rapidai/maclaw-gui:3"}, false},
		{"--all-tags", nil, true},
		{"maclaw-gui:2=-x", nil, true},
		{"maclaw-gui:2=a b", nil, true},
		{"x@sha256:1=ghcr.io/a:1", nil, true},
		{"maclaw-gui:2=", nil, true},
	}
	for _, tc := range cases {
		got, err := ParseImageSources(tc.raw)
		if (err != nil) != tc.bad {
			t.Errorf("%q: err=%v want bad=%v", tc.raw, err, tc.bad)
			continue
		}
		if tc.bad {
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %v want %v", tc.raw, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%q: got %v want %v", tc.raw, got, tc.want)
			}
		}
	}
}

func TestPresentImageIsNeverPulledOrReplaced(t *testing.T) {
	f := newPullDocker(DefaultImage)
	if err := sourceService(f).installImage(context.Background(), DefaultImage); err != nil {
		t.Fatal(err)
	}
	if f.has("pull") || f.has("tag") || f.has("run") {
		t.Fatalf("present image was touched: %v", f.commands)
	}
}

func TestMissingImageIsPulledCheckedAndTagged(t *testing.T) {
	f := newPullDocker()
	if err := sourceService(f).installImage(context.Background(), DefaultImage); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"image inspect --format {{.Id}} maclaw-gui:2",
		"pull " + testSource,
		"image inspect --format {{.Id}} " + testSource,
		"run --rm --entrypoint sh sha256:aaaaaaaaaaaa -c ",
		"image inspect --format {{.Id}} maclaw-gui:2",
		"tag sha256:aaaaaaaaaaaa maclaw-gui:2",
	}
	if len(f.commands) != len(want) {
		t.Fatalf("commands = %q", f.commands)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(f.commands[i], prefix) {
			t.Fatalf("command %d = %q, want prefix %q", i, f.commands[i], prefix)
		}
	}
	if !strings.Contains(f.commands[3], "startxfce4") || !strings.Contains(f.commands[3], "/desktop_supervisor.py") {
		t.Fatalf("contract check is incomplete: %q", f.commands[3])
	}
}

func TestFailedSourcePullFailsWithoutTaggingOrDockerHub(t *testing.T) {
	f := newPullDocker()
	f.pullErr = errString("dial tcp: i/o timeout")
	err := sourceService(f).installImage(context.Background(), DefaultImage)
	if err == nil || !strings.Contains(err.Error(), testSource) {
		t.Fatalf("err = %v", err)
	}
	if f.has("tag") || f.has("pull maclaw-gui:2") {
		t.Fatalf("failed pull still tagged or fell back to docker.io: %v", f.commands)
	}
}

func TestImageFailingTheContractIsNotTagged(t *testing.T) {
	f := newPullDocker()
	f.contract = errString("exit status 1")
	err := sourceService(f).installImage(context.Background(), DefaultImage)
	if err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("err = %v", err)
	}
	if f.has("tag") {
		t.Fatalf("image failing the contract was tagged: %v", f.commands)
	}
}

func TestLocalImageBuiltDuringThePullWins(t *testing.T) {
	f := newPullDocker()
	f.onPull = func() {
		f.mu.Lock()
		f.local[DefaultImage] = true // built on the host meanwhile
		f.mu.Unlock()
	}
	if err := sourceService(f).installImage(context.Background(), DefaultImage); err != nil {
		t.Fatal(err)
	}
	if f.has("tag") {
		t.Fatalf("local image was replaced: %v", f.commands)
	}
}

func TestConcurrentRequestsShareOnePull(t *testing.T) {
	f := newPullDocker()
	f.pullGate = make(chan struct{})
	svc := sourceService(f)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.installImage(context.Background(), DefaultImage)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.pulls() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(f.pullGate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := f.pulls(); n != 1 {
		t.Fatalf("pulls = %d, want 1", n)
	}
}

func TestFailedPullIsNotRetriedOnEveryRequest(t *testing.T) {
	f := newPullDocker()
	f.pullErr = errString("connection refused")
	svc := sourceService(f)
	for i := 0; i < 3; i++ {
		if err := svc.installImage(context.Background(), DefaultImage); err == nil {
			t.Fatal("missing image was accepted")
		}
	}
	if n := f.pulls(); n != 1 {
		t.Fatalf("pulls = %d, want 1 within the retry window", n)
	}
	statuses := svc.ImagePullStatuses()
	if len(statuses) != 1 || statuses[0].State != "failed" || statuses[0].Source != testSource {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestCallerTimeoutLeavesThePullRunning(t *testing.T) {
	f := newPullDocker()
	f.pullGate = make(chan struct{})
	svc := sourceService(f)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := svc.installImage(ctx, DefaultImage)
	if err == nil || !strings.Contains(err.Error(), "still being pulled") {
		t.Fatalf("err = %v", err)
	}
	if s := svc.ImagePullStatuses(); s[0].State != "pulling" {
		t.Fatalf("status = %+v", s)
	}
	close(f.pullGate)
	if err := svc.installImage(context.Background(), DefaultImage); err != nil {
		t.Fatal(err)
	}
	if n := f.pulls(); n != 1 {
		t.Fatalf("pulls = %d, want the background pull to be reused", n)
	}
	if !f.has("tag sha256:aaaaaaaaaaaa maclaw-gui:2") {
		t.Fatalf("background pull did not tag: %v", f.commands)
	}
}

func TestImageWithoutSourceKeepsThePlainPull(t *testing.T) {
	f := newPullDocker()
	svc := &Service{Run: f.run} // DESKTOPD_IMAGE_SOURCE=off
	if err := svc.installImage(context.Background(), "registry.example.com/custom:1"); err != nil {
		t.Fatal(err)
	}
	if !f.has("pull registry.example.com/custom:1") || f.has("tag") || f.has("run") {
		t.Fatalf("commands = %v", f.commands)
	}
}

func TestPrefetchPullsOnlyMissingImages(t *testing.T) {
	f := newPullDocker("maclaw-gui:3")
	svc := &Service{Run: f.run, ImageSources: map[string]string{
		"maclaw-gui:2": testSource,
		"maclaw-gui:3": "ghcr.io/rapidai/maclaw-gui:3",
	}}
	svc.PrefetchImages(context.Background())
	if !f.has("pull "+testSource) || f.has("pull ghcr.io/rapidai/maclaw-gui:3") {
		t.Fatalf("commands = %v", f.commands)
	}
}

func TestSourcePullFailureLeavesTheOldDesktopRunning(t *testing.T) {
	f := &fakeDocker{t: t, inspect: "true|1g|" + readyMounts(t, "tenant", "alice") + "||maclaw-gui:1", stateLabel: "missing"}
	var commands []string
	var mu sync.Mutex
	svc := &Service{AdvertiseHost: "docker.example", ImageSources: map[string]string{DefaultImage: testSource},
		Run: func(ctx context.Context, args ...string) (string, error) {
			line := strings.Join(args, " ")
			mu.Lock()
			commands = append(commands, line)
			mu.Unlock()
			if strings.HasPrefix(line, "image inspect") && strings.HasSuffix(line, " maclaw-gui:2") {
				return "", errString("Error: No such image")
			}
			if args[0] == "pull" {
				return "", errString("net/http: TLS handshake timeout")
			}
			return f.run(ctx, args...)
		}}
	if _, err := svc.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"}); err == nil {
		t.Fatal("missing image was accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, line := range commands {
		for _, bad := range []string{"stop ", "rm ", "commit ", "exec ", "tag "} {
			if strings.HasPrefix(line, bad) {
				t.Fatalf("old desktop was touched after a failed source pull: %s", line)
			}
		}
	}
}
