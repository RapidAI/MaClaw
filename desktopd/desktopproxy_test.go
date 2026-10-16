package desktopd

import (
	"context"
	"strings"
	"testing"
)

// TestRunContainerInjectsDesktopProxyEnv checks that desktop containers carry
// the egress proxy in their environment: the browser gets its --proxy-server
// flag from these variables (desktop_supervisor.browser_proxy_flags), and
// curl/apt read them directly.
func TestRunContainerInjectsDesktopProxyEnv(t *testing.T) {
	var lines []string
	run := func(_ context.Context, args ...string) (string, error) {
		lines = append(lines, strings.Join(args, " "))
		return "", nil
	}
	svc := &Service{AdvertiseHost: "docker.example", DesktopProxyURL: "http://172.17.0.1:18083", Run: run}
	if _, err := svc.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"--env HTTP_PROXY=http://172.17.0.1:18083",
		"--env HTTPS_PROXY=http://172.17.0.1:18083",
		"--env http_proxy=http://172.17.0.1:18083",
		"--env https_proxy=http://172.17.0.1:18083",
		"--env NO_PROXY=localhost,127.0.0.1,::1",
		"--env no_proxy=localhost,127.0.0.1,::1",
		"--label maclaw.desktop-proxy=http://172.17.0.1:18083",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("run is missing %q:\n%s", want, joined)
		}
	}

	// No proxy configured: the env vars are still written, empty, so a
	// committed state image that carried a proxy cannot leak it into a
	// desktop recreated after the proxy was disabled.
	lines = nil
	plain := &Service{AdvertiseHost: "docker.example", Run: run}
	if _, err := plain.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"}); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(lines, "\n")
	// The empty env reads "--env HTTP_PROXY= --env HTTPS_PROXY= ..." once
	// args are joined; a populated value never matches that shape.
	if !strings.Contains(joined, "--env HTTP_PROXY= --env HTTPS_PROXY= --env http_proxy=") {
		t.Fatalf("disabled proxy should still write empty envs:\n%s", joined)
	}
	if !strings.Contains(joined, "--label maclaw.desktop-proxy=") {
		t.Fatalf("disabled proxy should still record the empty label:\n%s", joined)
	}
}
