package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("MACLAW_COMMAND_RENEW_HELPER") {
	case "":
		os.Exit(m.Run())
	case "print":
		runRenewPrintHelper()
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "spin":
		var sink atomic.Uint64
		for {
			sink.Add(1)
		}
	case "exit-on-byte":
		var buf [1]byte
		_, err := os.Stdin.Read(buf[:])
		if err != nil || buf[0] != '3' {
			os.Exit(0)
		}
		os.Exit(3)
	default:
		fmt.Fprintln(os.Stderr, "unknown MACLAW_COMMAND_RENEW_HELPER")
		os.Exit(2)
	}
}

func runRenewPrintHelper() {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fmt.Fprintln(os.Stdout, "tick")
		_ = os.Stdout.Sync()
		time.Sleep(200 * time.Millisecond)
	}
}

func TestProgressClockMarksNonEmptyWrites(t *testing.T) {
	var clock ProgressClock
	var buf bytes.Buffer
	w := clock.Writer(&buf)
	if _, err := w.Write(nil); err != nil {
		t.Fatal(err)
	}
	if !clock.Last().IsZero() {
		t.Fatal("empty write marked the clock")
	}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if clock.Last().IsZero() || buf.String() != "hi" {
		t.Fatalf("clock=%s buf=%q", clock.Last(), buf.String())
	}
}

func TestProcStatUsageReadsFieldsAfterTheCommandName(t *testing.T) {
	line := "42 (a) b) S 7 1 1 0 -1 0 1 2 3 4 100 50 0"
	ppid, utime, stime, ok := procStatUsage(line)
	if !ok || ppid != 7 || utime != 100 || stime != 50 {
		t.Fatalf("parsed (%d,%d,%d,%v)", ppid, utime, stime, ok)
	}
	if _, _, _, ok := procStatUsage("no-paren"); ok {
		t.Fatal("parsed a line with no command field")
	}
}

func TestDescendantPIDsWalksTheTree(t *testing.T) {
	parents := map[int]int{2: 1, 3: 2, 4: 1, 5: 9, 6: 6}
	got := descendantPIDs(1, parents)
	want := map[int]bool{1: true, 2: true, 3: true, 4: true}
	if len(got) != len(want) {
		t.Fatalf("pids = %v", got)
	}
	for _, pid := range got {
		if !want[pid] {
			t.Fatalf("pids = %v", got)
		}
	}
}

func TestWaitCommandRenewingExtendsWhileOutputContinues(t *testing.T) {
	cmd, stdout, clock := startRenewHelper(t, "print")
	var notices atomic.Int32
	started := time.Now()
	err := WaitCommandRenewing(context.Background(), cmd, RenewalOptions{
		QuietFor:    400 * time.Millisecond,
		Poll:        30 * time.Millisecond,
		NoticeEvery: 200 * time.Millisecond,
		Clock:       clock,
		OnRenew: func(time.Duration) {
			notices.Add(1)
		},
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("wait: %v elapsed=%s out=%q", err, elapsed, stdout.String())
	}
	if !strings.Contains(stdout.String(), "tick") {
		t.Fatalf("output = %q", stdout.String())
	}
	if elapsed < 800*time.Millisecond {
		t.Fatalf("finished in %s, inside the stall window", elapsed)
	}
	if notices.Load() < 2 {
		t.Fatalf("renewal notices = %d", notices.Load())
	}
}

func TestWaitCommandRenewingKeepsAnExitThatLandedBeforeTheKill(t *testing.T) {
	for _, wantCode := range []int{0, 3} {
		t.Run(fmt.Sprintf("exit-%d", wantCode), func(t *testing.T) {
			withCommandTreeCPU(t, func(int) (map[int]time.Duration, bool) {
				return map[int]time.Duration{1: 0}, true
			})
			cmd := renewHelperCommand(t, "exit-on-byte")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout := &bytes.Buffer{}
			clock := &ProgressClock{}
			cmd.Stdout = clock.Writer(stdout)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState != nil || cmd.Process == nil {
					return
				}
				TerminateCommandTree(cmd)
				_, _ = cmd.Process.Wait()
			})
			prev := commandRenewBeforeKill
			commandRenewBeforeKill = func(*exec.Cmd) {
				_, _ = stdin.Write([]byte{byte('0' + wantCode)})
				_ = stdin.Close()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) && commandStillRunning(cmd) {
					time.Sleep(10 * time.Millisecond)
				}
			}
			t.Cleanup(func() { commandRenewBeforeKill = prev })

			err = WaitCommandRenewing(context.Background(), cmd, RenewalOptions{
				QuietFor: 50 * time.Millisecond,
				Poll:     15 * time.Millisecond,
				Clock:    clock,
			})
			if wantCode == 0 {
				if err != nil {
					t.Fatalf("clean exit became %v", err)
				}
				return
			}
			var stall *CommandStallError
			if errors.As(err, &stall) {
				t.Fatalf("exit %d was reported as a stall", wantCode)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != wantCode {
				t.Fatalf("err = %v, want exit %d", err, wantCode)
			}
		})
	}
}

func TestWaitCommandRenewingStopsAQuietCommand(t *testing.T) {
	cmd, _, clock := startRenewHelper(t, "sleep")
	started := time.Now()
	err := WaitCommandRenewing(context.Background(), cmd, RenewalOptions{
		QuietFor: 400 * time.Millisecond,
		Poll:     30 * time.Millisecond,
		Clock:    clock,
	})
	elapsed := time.Since(started)
	var stall *CommandStallError
	if !errors.As(err, &stall) {
		t.Fatalf("err = %v, want a stall after %s", err, elapsed)
	}
	if elapsed >= 15*time.Second {
		t.Fatalf("quiet command ran for %s", elapsed)
	}
}

func TestWaitCommandRenewingParentCancelIsNotAStall(t *testing.T) {
	cmd, _, clock := startRenewHelper(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	err := WaitCommandRenewing(ctx, cmd, RenewalOptions{
		QuietFor: 5 * time.Second,
		Poll:     30 * time.Millisecond,
		Clock:    clock,
	})
	elapsed := time.Since(started)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	var stall *CommandStallError
	if errors.As(err, &stall) {
		t.Fatal("cancel was reported as a stall")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("cancel took %s", elapsed)
	}
}

func TestWaitCommandRenewingCPUGrowthExtendsASilentCommand(t *testing.T) {
	var calls atomic.Int32
	withCommandTreeCPU(t, func(int) (map[int]time.Duration, bool) {
		n := calls.Add(1)
		return map[int]time.Duration{1: time.Duration(n) * 25 * time.Millisecond}, true
	})
	cmd, _, clock := startRenewHelper(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var notices atomic.Int32
	go func() {
		for notices.Load() < 2 && ctx.Err() == nil {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	err := WaitCommandRenewing(ctx, cmd, RenewalOptions{
		QuietFor:    500 * time.Millisecond,
		Poll:        30 * time.Millisecond,
		NoticeEvery: 30 * time.Millisecond,
		Clock:       clock,
		OnRenew: func(time.Duration) {
			notices.Add(1)
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, notices=%d", err, notices.Load())
	}
	if notices.Load() < 2 {
		t.Fatalf("notices = %d", notices.Load())
	}
}

func TestWaitCommandRenewingFailedCPUSampleDoesNotRenew(t *testing.T) {
	withCommandTreeCPU(t, func(int) (map[int]time.Duration, bool) {
		return map[int]time.Duration{1: time.Hour}, false
	})
	cmd, _, clock := startRenewHelper(t, "sleep")
	started := time.Now()
	err := WaitCommandRenewing(context.Background(), cmd, RenewalOptions{
		QuietFor: 250 * time.Millisecond,
		Poll:     30 * time.Millisecond,
		Clock:    clock,
	})
	elapsed := time.Since(started)
	var stall *CommandStallError
	if !errors.As(err, &stall) {
		t.Fatalf("err = %v after %s", err, elapsed)
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("failed CPU sample kept the command alive for %s", elapsed)
	}
}

func TestSampleCommandTreeCPUSeesABusyProcess(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("process CPU is sampled on windows and linux")
	}
	cmd := renewHelperCommand(t, "spin")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		TerminateCommandTree(cmd)
		_ = cmd.Wait()
	}()
	time.Sleep(50 * time.Millisecond)
	first, ok := sampleCommandTreeCPU(cmd.Process.Pid)
	if !ok {
		t.Fatal("CPU sample failed")
	}
	time.Sleep(200 * time.Millisecond)
	second, ok := sampleCommandTreeCPU(cmd.Process.Pid)
	if !ok {
		t.Fatal("second CPU sample failed")
	}
	if second < first+commandRenewCPUMin {
		t.Fatalf("cpu %s -> %s, want at least %s of growth", first, second, commandRenewCPUMin)
	}
}

func startRenewHelper(t *testing.T, mode string) (*exec.Cmd, *bytes.Buffer, *ProgressClock) {
	t.Helper()
	cmd := renewHelperCommand(t, mode)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	clock := &ProgressClock{}
	cmd.Stdout = clock.Writer(stdout)
	cmd.Stderr = clock.Writer(stderr)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState != nil || cmd.Process == nil {
			return
		}
		TerminateCommandTree(cmd)
		_, _ = cmd.Process.Wait()
	})
	return cmd, stdout, clock
}

func renewHelperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MACLAW_COMMAND_RENEW_HELPER="+mode)
	PrepareCommandForTreeKill(cmd)
	return cmd
}

func TestWaitCommandRenewingChildExitDoesNotHideParentCPU(t *testing.T) {
	var calls atomic.Int32
	withCommandTreeCPU(t, func(int) (map[int]time.Duration, bool) {
		n := calls.Add(1)
		if n <= 2 {
			return map[int]time.Duration{
				1: 100 * time.Millisecond,
				2: 400 * time.Millisecond,
			}, true
		}
		parent := 100*time.Millisecond + time.Duration(n-2)*30*time.Millisecond
		return map[int]time.Duration{1: parent}, true
	})
	cmd, _, clock := startRenewHelper(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var notices atomic.Int32
	go func() {
		for notices.Load() < 2 && ctx.Err() == nil {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	err := WaitCommandRenewing(ctx, cmd, RenewalOptions{
		QuietFor:    500 * time.Millisecond,
		Poll:        30 * time.Millisecond,
		NoticeEvery: 30 * time.Millisecond,
		Clock:       clock,
		OnRenew: func(time.Duration) {
			notices.Add(1)
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, notices=%d", err, notices.Load())
	}
	if notices.Load() < 2 {
		t.Fatalf("notices = %d", notices.Load())
	}
}

func TestCPUProgressTracksEachProcess(t *testing.T) {
	var tr cpuProgressTracker
	if d := tr.observe(map[int]time.Duration{1: 100 * time.Millisecond, 2: 400 * time.Millisecond}, true); d != 0 {
		t.Fatalf("baseline delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 130 * time.Millisecond}, true); d != 30*time.Millisecond {
		t.Fatalf("after child exit delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 130 * time.Millisecond, 3: 25 * time.Millisecond}, true); d != 25*time.Millisecond {
		t.Fatalf("new pid delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: time.Hour}, false); d != 0 {
		t.Fatalf("failed sample delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 150 * time.Millisecond, 3: 25 * time.Millisecond}, true); d != 20*time.Millisecond {
		t.Fatalf("after failed sample delta = %s", d)
	}

	var born cpuProgressTracker
	born.observe(map[int]time.Duration{1: 50 * time.Millisecond}, true)
	if d := born.observe(map[int]time.Duration{1: 50 * time.Millisecond, 2: 10 * time.Millisecond}, true); d != 0 {
		t.Fatalf("small new pid delta = %s", d)
	}
	if d := born.observe(map[int]time.Duration{1: 50 * time.Millisecond, 2: 25 * time.Millisecond}, true); d != 25*time.Millisecond {
		t.Fatalf("new pid accumulated delta = %s", d)
	}

	var both cpuProgressTracker
	both.observe(map[int]time.Duration{1: 0, 2: 0}, true)
	if d := both.observe(map[int]time.Duration{1: 15 * time.Millisecond, 2: 15 * time.Millisecond}, true); d != 30*time.Millisecond {
		t.Fatalf("split growth delta = %s", d)
	}
}

func TestCPUProgressAccumulatesSubThresholdAndResetsADrop(t *testing.T) {
	var tr cpuProgressTracker
	tr.observe(map[int]time.Duration{1: 0}, true)
	if d := tr.observe(map[int]time.Duration{1: 10 * time.Millisecond}, true); d != 0 {
		t.Fatalf("10ms delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 19 * time.Millisecond}, true); d != 0 {
		t.Fatalf("19ms delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 25 * time.Millisecond}, true); d != 25*time.Millisecond {
		t.Fatalf("25ms delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 5 * time.Millisecond}, true); d != 0 {
		t.Fatalf("drop delta = %s", d)
	}
	if d := tr.observe(map[int]time.Duration{1: 30 * time.Millisecond}, true); d != 25*time.Millisecond {
		t.Fatalf("after drop delta = %s", d)
	}
}

func TestCPUProgressDustOutsideTheWindowDoesNotRenew(t *testing.T) {
	var tr cpuProgressTracker
	var now time.Time
	tr.now = func() time.Time { return now }
	start := time.Unix(1_700_000_000, 0)
	now = start
	cpu := time.Duration(0)
	if d := tr.observe(map[int]time.Duration{1: cpu}, true); d != 0 {
		t.Fatalf("baseline delta = %s", d)
	}
	for i := 1; i <= 40; i++ {
		now = start.Add(time.Duration(i) * time.Second)
		cpu += time.Millisecond
		if d := tr.observe(map[int]time.Duration{1: cpu}, true); d != 0 {
			t.Fatalf("dust at %s renewed by %s", now.Sub(start), d)
		}
	}
}

func TestCPUProgressCountsWorkInsideTheWindow(t *testing.T) {
	var tr cpuProgressTracker
	var now time.Time
	tr.now = func() time.Time { return now }
	now = time.Unix(1_700_000_000, 0)
	if d := tr.observe(map[int]time.Duration{1: 0}, true); d != 0 {
		t.Fatalf("baseline delta = %s", d)
	}
	now = now.Add(time.Second)
	if d := tr.observe(map[int]time.Duration{1: 15 * time.Millisecond}, true); d != 0 {
		t.Fatalf("first slice = %s", d)
	}
	now = now.Add(4 * time.Second)
	if d := tr.observe(map[int]time.Duration{1: 30 * time.Millisecond}, true); d != 30*time.Millisecond {
		t.Fatalf("inside window = %s", d)
	}
	now = now.Add(6 * time.Second)
	if d := tr.observe(map[int]time.Duration{1: 45 * time.Millisecond}, true); d != 0 {
		t.Fatalf("credit outside the window renewed: %s", d)
	}
}

func TestCommandAlreadyExitedReadsAFinishedWait(t *testing.T) {
	done := make(chan error, 1)
	if _, ok := commandAlreadyExited(done); ok {
		t.Fatal("empty wait looked finished")
	}
	done <- errors.New("exit status 3")
	err, ok := commandAlreadyExited(done)
	if !ok || err == nil || err.Error() != "exit status 3" {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if _, ok := commandAlreadyExited(done); ok {
		t.Fatal("the finished wait was read twice")
	}
}

func TestCaptureLimitWriterKeepsAPrefixAndReportsTheFullWrite(t *testing.T) {
	var buf bytes.Buffer
	var clock ProgressClock
	w := clock.Writer(CaptureLimitWriter(&buf, 4))
	n, err := w.Write([]byte("abcdef"))
	if err != nil || n != 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if buf.String() != "abcd" {
		t.Fatalf("kept %q", buf.String())
	}
	if clock.Last().IsZero() {
		t.Fatal("discarded bytes did not mark the clock")
	}
	n, err = w.Write([]byte("zz"))
	if n != 2 || err != nil || buf.String() != "abcd" {
		t.Fatalf("n=%d err=%v buf=%q", n, err, buf.String())
	}
}

func TestClockTicksFromAuxv(t *testing.T) {
	if got := clockTicksFromAuxv(nil); got != 100 {
		t.Fatalf("fallback = %d", got)
	}
	if got := clockTicksFromAuxv([][2]uintptr{{procATCLKTCK, 250}}); got != 250 {
		t.Fatalf("ticks = %d", got)
	}
	if got := clockTicksFromAuxv([][2]uintptr{{procATCLKTCK, 0}, {6, 4096}}); got != 100 {
		t.Fatalf("zero clktck = %d", got)
	}
}

func TestParseProcChildren(t *testing.T) {
	got := parseProcChildren("12 34 12 no 0 -1 56\n")
	want := []int{12, 34, 56}
	if len(got) != len(want) {
		t.Fatalf("children = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("children = %v", got)
		}
	}
	if parseProcChildren("") != nil && len(parseProcChildren("")) != 0 {
		t.Fatalf("empty = %v", parseProcChildren(""))
	}
}

func withCommandTreeCPU(t *testing.T, fn func(int) (map[int]time.Duration, bool)) {
	t.Helper()
	prev := commandTreeCPUByPID
	commandTreeCPUByPID = fn
	t.Cleanup(func() { commandTreeCPUByPID = prev })
}
