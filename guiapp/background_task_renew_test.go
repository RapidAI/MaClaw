package guiapp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestBackgroundTaskRenewMarkFollowsChatTaskID(t *testing.T) {
	if backgroundTaskRenews(context.Background()) {
		t.Fatal("plain context renews")
	}
	if markChatTaskToolContext(nil, "") != nil {
		t.Fatal("empty chat task id marked a nil context")
	}
	if backgroundTaskRenews(markChatTaskToolContext(context.Background(), "  ")) {
		t.Fatal("blank chat task id renews")
	}
	ctx := markChatTaskToolContext(context.Background(), "ct-1")
	if !backgroundTaskRenews(ctx) {
		t.Fatal("chat task context does not renew")
	}
	child, cancel := context.WithCancel(ctx)
	detached := context.WithoutCancel(child)
	cancel()
	if !backgroundTaskRenews(detached) {
		t.Fatal("renewal mark was dropped")
	}
	if detached.Err() != nil {
		t.Fatal("detached context stayed cancelled")
	}
}

func TestBackgroundTaskRenewalSurvivesRouteDeadlineLift(t *testing.T) {
	parent := withBackgroundTaskRenewal(context.Background())
	route, cancel := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancel()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("missing deadline")
	}
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}
	scriptCtx, release := llm.ContinueAfterOwnedRouteDeadline(route)
	defer release()
	if !backgroundTaskRenews(scriptCtx) {
		t.Fatal("renewal mark did not survive the route deadline lift")
	}
	if scriptCtx.Err() != nil {
		t.Fatal("lifted context is already done")
	}
}

func TestBackgroundTaskRenewProgressLine(t *testing.T) {
	got := formatCraftRenewingExecutionProgress("powershell", 1, 2, 300)
	if !strings.HasPrefix(got, "正在执行") || !strings.Contains(got, "有状态则续期") || !strings.Contains(got, "连续 300s") {
		t.Fatal(got)
	}
	if strings.Contains(got, "最长等待") {
		t.Fatal(got)
	}
	plain := formatCraftExecutionProgress("powershell", 1, 2, 300)
	if !strings.Contains(plain, "最长等待 300s") {
		t.Fatal(plain)
	}
	if craftRenewQuietSeconds(0) != craftToolMaxTimeout {
		t.Fatalf("quiet = %d", craftRenewQuietSeconds(0))
	}
}

func TestBackgroundTaskRenewalNoticeStaysOnTheCard(t *testing.T) {
	line := backgroundTaskRenewalNotice(time.Minute)
	if !strings.HasPrefix(line, "正在执行") || !strings.Contains(line, "后台任务已续期") {
		t.Fatal(line)
	}
	for _, toolName := range []string{"bash", "craft_tool"} {
		got := filterUserFacingToolProgress("zh", toolName, line)
		if !strings.Contains(got, "后台任务已续期") {
			t.Fatalf("%s filtered to %q", toolName, got)
		}
		milestone, ok := chatTaskProgressMilestone(got)
		if !ok || !strings.Contains(milestone, "后台任务已续期") {
			t.Fatalf("%s milestone %q ok=%v", toolName, milestone, ok)
		}
	}
	if _, ok := chatTaskProgressMilestone("命令仍在执行中（已 30s）: ping"); ok {
		t.Fatal("command heartbeat was kept as a milestone")
	}
}

func TestBackgroundTaskRenewsPastTheStallWindowWhenItReports(t *testing.T) {
	powershell := renewTestPowerShell(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "report.ps1")
	script := "$ErrorActionPreference='Stop'\r\n" +
		"for ($i = 0; $i -lt 16; $i++) {\r\n" +
		"  [Console]::Out.WriteLine(\"tick $i\")\r\n" +
		"  [Console]::Out.Flush()\r\n" +
		"  Start-Sleep -Milliseconds 200\r\n" +
		"}\r\n"
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	var notes []string
	ctx := withScriptProgressCallback(withBackgroundTaskRenewal(context.Background()), func(msg string) {
		notes = append(notes, msg)
	})
	started := time.Now()
	out, err := executeScriptWithContext(ctx, path, "powershell", dir, 1, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("err = %v after %s output=%q", err, elapsed, out)
	}
	if !strings.Contains(out, "tick") {
		t.Fatalf("output = %q", out)
	}
	if elapsed < 2*time.Second {
		t.Fatalf("script ended in %s, inside the old stall window", elapsed)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "后台任务已续期") {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestBackgroundTaskRenewStopsWhenAScriptGoesQuiet(t *testing.T) {
	powershell := renewTestPowerShell(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sleep.ps1")
	if err := os.WriteFile(path, []byte("Start-Sleep -Seconds 30\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := executeScriptWithContext(withBackgroundTaskRenewal(context.Background()), path, "powershell", dir, 1, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	elapsed := time.Since(started)
	stall, ok := err.(*craftScriptStallError)
	if !ok {
		t.Fatalf("err = %T %v after %s", err, err, elapsed)
	}
	if _, isTimeout := err.(*craftScriptTimeoutError); isTimeout {
		t.Fatal("stall was reported as the script timeout")
	}
	if !strings.Contains(stall.Error(), "续期") {
		t.Fatal(stall.Error())
	}
	if elapsed >= 15*time.Second {
		t.Fatalf("quiet script ran for %s", elapsed)
	}
}

func TestBackgroundTaskRenewStopsWhenTheParentStops(t *testing.T) {
	powershell := renewTestPowerShell(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sleep.ps1")
	if err := os.WriteFile(path, []byte("Start-Sleep -Seconds 30\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, stop := context.WithCancel(withBackgroundTaskRenewal(context.Background()))
	go func() {
		time.Sleep(400 * time.Millisecond)
		stop()
	}()
	started := time.Now()
	_, err := executeScriptWithContext(parent, path, "powershell", dir, 30, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("script finished after the caller stop")
	}
	if _, ok := err.(*craftScriptStallError); ok {
		t.Fatal("caller stop was reported as a stall")
	}
	if _, ok := err.(*craftScriptTimeoutError); ok {
		t.Fatal("caller stop was reported as the script timeout")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("script kept running for %s after the stop", elapsed)
	}
}

func TestBuildCraftExecCommandNilContextHasNoWatcher(t *testing.T) {
	runtimes := craftRuntimeAvailability{PowerShell: "powershell"}
	renewing, err := buildCraftExecCommand(nil, "cleanup.ps1", "powershell", runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if renewing.Cancel != nil {
		t.Fatal("nil context still installed a CommandContext watcher")
	}
	foreground, err := buildCraftExecCommand(context.Background(), "cleanup.ps1", "powershell", runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if foreground.Cancel == nil {
		t.Fatal("foreground command has no context cancel")
	}
}

func renewTestPowerShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("powershell script")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip(err.Error())
	}
	return powershell
}
