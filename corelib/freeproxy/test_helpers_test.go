package freeproxy

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

const freeproxyIntegrationEnv = "FREEPROXY_RUN_INTEGRATION"

func requireIntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv(freeproxyIntegrationEnv) != "1" {
		t.Skip("set FREEPROXY_RUN_INTEGRATION=1 to run integration tests")
	}
}

func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
}

func startTestServer(t *testing.T) (*Server, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()
	srv := NewServer(":0", dir)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()

	deadline := time.After(2 * time.Second)
	for {
		if srv.listener != nil {
			break
		}
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited early: %v", err)
		case <-deadline:
			cancel()
			t.Fatal("server did not start in time")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	return srv, cancel
}
