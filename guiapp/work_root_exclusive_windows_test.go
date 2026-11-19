//go:build windows

package guiapp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestExclusiveGitCancelAfterExitKeepsTheStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "cmd", "/c", "exit", "0")
	configureExclusiveGitCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !exclusiveGitProcessExited(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("process did not exit")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Cancel after exit = %v, want ErrProcessDone", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait = %v, want the original success", err)
	}
}

func TestExclusiveGitCancelStopsLiveProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "cmd", "/c", "ping -n 30 127.0.0.1")
	configureExclusiveGitCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Cancel(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("Cancel live = %v, want nil", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("live process survived cancel")
	}
}
