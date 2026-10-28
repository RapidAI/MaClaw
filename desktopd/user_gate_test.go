package desktopd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

func TestInstallSerializesAptWithoutBlockingStatus(t *testing.T) {
	firstAtApt := make(chan struct{})
	secondStarted := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstBlocked atomic.Bool
	var firstOnce, secondOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()

	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "{{.State.Running}}") {
			return "true", nil
		}
		if firstBlocked.Load() {
			secondOnce.Do(func() { close(secondEntered) })
		}
		if strings.Contains(joined, "apt-get install -y") && strings.Contains(joined, " vim") {
			firstBlocked.Store(true)
			firstOnce.Do(func() { close(firstAtApt) })
			<-releaseFirst
			firstBlocked.Store(false)
		}
		return "", nil
	}}

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.Install(context.Background(), "tenant", "alice", []string{"vim"})
		firstDone <- err
	}()
	select {
	case <-firstAtApt:
	case <-time.After(5 * time.Second):
		t.Fatal("first install did not reach apt-get")
	}

	go func() {
		close(secondStarted)
		_, err := svc.Install(context.Background(), "tenant", "alice", []string{"emacs"})
		firstDone <- err
	}()
	<-secondStarted
	overlapped := false
	select {
	case <-secondEntered:
		overlapped = true
	case <-time.After(250 * time.Millisecond):
	}

	statusDone := make(chan error, 1)
	go func() {
		status, err := svc.MachineStatus(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
		if err == nil && status.State != "running" {
			err = fmt.Errorf("state = %q", status.State)
		}
		statusDone <- err
	}()
	var statusErr error
	select {
	case statusErr = <-statusDone:
	case <-time.After(2 * time.Second):
		statusErr = fmt.Errorf("status read queued behind install")
	}

	release()
	for i := 0; i < 2; i++ {
		if err := <-firstDone; err != nil {
			t.Fatalf("install failed: %v", err)
		}
	}
	if overlapped {
		t.Fatal("second install entered docker while the first apt sequence was blocked")
	}
	if statusErr != nil {
		t.Fatal(statusErr)
	}
}
