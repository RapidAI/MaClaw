package agentservice

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestWaitCommandWithContextKillsProcessTree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process-tree regression test")
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", strings.Join([]string{
		"$child = Start-Process powershell -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 30' -PassThru",
		"Write-Output $child.Id",
		"Start-Sleep -Seconds 30",
	}, "; "))
	coretool.PrepareCommandForTreeKill(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	childIDCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := stdout.Read(buf)
		childIDCh <- strings.TrimSpace(string(buf[:n]))
	}()

	var childID string
	select {
	case childID = <-childIDCh:
	case <-time.After(2 * time.Second):
		t.Fatal("child pid was not captured")
	}
	if childID == "" {
		t.Fatal("child pid is empty")
	}

	ctx, cancel := context.WithCancel(context.Background())
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- coretool.WaitCommandWithContext(ctx, cmd)
	}()
	cancel()
	if err := <-waitErrCh; err != context.Canceled {
		t.Fatalf("waitCommandWithContext error = %v, want Canceled", err)
	}

	for i := 0; i < 20; i++ {
		probe := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "if (Get-Process -Id "+childID+" -ErrorAction SilentlyContinue) { exit 1 } else { exit 0 }")
		coretool.PrepareCommandForTreeKill(probe)
		if err := probe.Run(); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("child process %s survived cancel tree kill", childID)
}
