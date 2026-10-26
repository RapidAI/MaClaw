package desktopd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A slow exec of one user (a screenshot is the usual one) must not queue
// that user's status read behind it: reads take the gate's shared side.
// Without the read/write split this blocks until the screenshot finishes.
func TestUserGateReadSideDoesNotWaitForExec(t *testing.T) {
	spec := Spec{TenantID: "tenant", UserID: "alice"}
	shotStarted := make(chan struct{})
	releaseShot := make(chan struct{})
	var shotOnce sync.Once
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "{{.State.Running}}") {
			return "true", nil
		}
		if strings.Contains(joined, "supervisor.py command ") {
			shotOnce.Do(func() { close(shotStarted) })
			<-releaseShot
			return "not-a-png", nil
		}
		t.Fatalf("unexpected docker call: %s", joined)
		return "", nil
	}}

	shotDone := make(chan error, 1)
	go func() {
		_, _, err := svc.CaptureShot(context.Background(), "tenant", "alice", ":20", "", desktop.ShotOptions{})
		shotDone <- err
	}()
	select {
	case <-shotStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("screenshot did not reach docker")
	}

	statusDone := make(chan error, 1)
	go func() {
		status, err := svc.MachineStatus(context.Background(), spec)
		if err == nil && status.State != "running" {
			err = fmt.Errorf("state = %q", status.State)
		}
		statusDone <- err
	}()
	select {
	case err := <-statusDone:
		if err != nil {
			t.Fatalf("status behind a slow screenshot: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("status read queued behind a slow screenshot")
	}

	close(releaseShot)
	<-shotDone
}
