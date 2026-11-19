package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// commandRenewCPUMin is the smallest process-tree CPU increase that counts as
// work. A quiet sleeper still accumulates a few milliseconds of scheduler
// time; that must not renew the stall window.
const commandRenewCPUMin = 20 * time.Millisecond

// commandRenewCPUWindow is how long sub-threshold CPU may add up. The stall
// window itself can be days, so CPU dust from a stuck process must not be
// integrated over that whole window. Work has to land inside this horizon.
const commandRenewCPUWindow = 5 * time.Second

// commandRenewCaptureLimit is how much of a renewing command's stdout or
// stderr is kept. The craft display cap (4096) and the bash display caps
// (8192 / 4096) sit under this, so their truncation markers still apply.
// Bytes past the cap still count as progress.
const commandRenewCaptureLimit = 64 << 10

// commandTreeCPUByPID samples the user+kernel time of each live process in
// pid's tree. ok is false when the tree cannot be inspected. A failed sample
// is not progress: treating it as progress would renew a task forever on a
// platform that cannot see CPU.
var commandTreeCPUByPID = sampleCommandTreeCPUByPID

// errCommandDidNotExit is the bound on waiting for a killed tree to finish.
// It is not the script's own status.
var errCommandDidNotExit = errors.New("command did not exit after the process tree was stopped")

// commandRenewBeforeKill, when set, runs after a stall is noticed and before
// the tree is stopped. Tests use it to let the process exit on its own.
var commandRenewBeforeKill func(*exec.Cmd)

// ProgressClock records the last time a wrapped writer received bytes.
// WaitCommandRenewing reads it from another goroutine.
type ProgressClock struct {
	last atomic.Int64
}

// Mark records activity at the current time.
func (c *ProgressClock) Mark() {
	if c == nil {
		return
	}
	c.last.Store(time.Now().UnixNano())
}

// Last is the time of the most recent mark. The zero time means nothing has
// been marked yet.
func (c *ProgressClock) Last() time.Time {
	if c == nil {
		return time.Time{}
	}
	n := c.last.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// Writer returns a writer that updates the clock whenever a non-empty write
// succeeds. A nil destination still counts the bytes so a caller can watch
// activity without retaining the payload.
func (c *ProgressClock) Writer(dst io.Writer) io.Writer {
	if c == nil {
		return dst
	}
	return &progressClockWriter{dst: dst, clock: c}
}

type progressClockWriter struct {
	dst   io.Writer
	clock *ProgressClock
}

func (w *progressClockWriter) Write(p []byte) (int, error) {
	var n int
	var err error
	if w.dst == nil {
		n = len(p)
	} else {
		n, err = w.dst.Write(p)
	}
	if n > 0 {
		w.clock.Mark()
	}
	return n, err
}

// CaptureLimitWriter stores at most limit bytes of a renewing command's
// output. limit <= 0 uses commandRenewCaptureLimit. Write reports the full
// input length, including bytes that were not stored, so a ProgressClock
// wrapped around it still sees discarded output.
func CaptureLimitWriter(dst *bytes.Buffer, limit int) io.Writer {
	if limit <= 0 {
		limit = commandRenewCaptureLimit
	}
	return &captureLimitWriter{dst: dst, limit: limit}
}

type captureLimitWriter struct {
	dst   *bytes.Buffer
	limit int
}

func (w *captureLimitWriter) Write(p []byte) (int, error) {
	if w.dst != nil && w.dst.Len() < w.limit {
		remain := w.limit - w.dst.Len()
		chunk := p
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
		_, _ = w.dst.Write(chunk)
	}
	return len(p), nil
}

// RenewalOptions controls WaitCommandRenewing.
//
// QuietFor is the stall window. It resets when the clock moves or when
// per-process CPU gains inside commandRenewCPUWindow reach
// commandRenewCPUMin. A child exiting does not hide later work in its parent.
// There is no total cap.
// Poll defaults to one second, so a stall is noticed at most one poll late.
// NoticeEvery defaults to 30s; the first real progress observation notifies
// immediately.
type RenewalOptions struct {
	QuietFor    time.Duration
	Poll        time.Duration
	NoticeEvery time.Duration
	Clock       *ProgressClock
	OnRenew     func(running time.Duration)
}

// CommandStallError means the process produced neither output nor CPU
// progress for Quiet. It is not a context deadline and not a caller cancel.
type CommandStallError struct {
	Quiet time.Duration
}

func (e *CommandStallError) Error() string {
	if e == nil {
		return "command stalled"
	}
	return fmt.Sprintf("command stalled: no output or process activity for %s", e.Quiet)
}

// WaitCommandRenewing waits for a command that the caller has already started
// and not yet waited. It kills the process tree when ctx ends or when the
// stall window expires with no new output and no process-tree CPU growth.
// A caller cancel wins over a stall observed at the same moment.
func WaitCommandRenewing(ctx context.Context, cmd *exec.Cmd, opt RenewalOptions) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	quietFor := opt.QuietFor
	if quietFor <= 0 {
		quietFor = 300 * time.Second
	}
	poll := opt.Poll
	if poll <= 0 {
		poll = time.Second
	}
	noticeEvery := opt.NoticeEvery
	if noticeEvery <= 0 {
		noticeEvery = 30 * time.Second
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	if err := ctx.Err(); err != nil {
		TerminateCommandTree(cmd)
		return finishRenewWait(done, err)
	}

	started := time.Now()
	lastProgress := started
	var cpuTrack cpuProgressTracker
	cpuTrack.observe(sampleRenewCPU(cmd))
	var lastNotice time.Time
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			TerminateCommandTree(cmd)
			return finishRenewWait(done, ctx.Err())
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				TerminateCommandTree(cmd)
				return finishRenewWait(done, err)
			}
			progressed := false
			if opt.Clock != nil {
				if marked := opt.Clock.Last(); !marked.IsZero() && marked.After(lastProgress) {
					lastProgress = marked
					progressed = true
				}
			}
			if cpuTrack.observe(sampleRenewCPU(cmd)) >= commandRenewCPUMin {
				lastProgress = time.Now()
				progressed = true
			}
			if progressed && opt.OnRenew != nil && (lastNotice.IsZero() || time.Since(lastNotice) >= noticeEvery) {
				lastNotice = time.Now()
				opt.OnRenew(time.Since(started))
			}
			if time.Since(lastProgress) >= quietFor {
				if err := ctx.Err(); err != nil {
					TerminateCommandTree(cmd)
					return finishRenewWait(done, err)
				}
				// The process can exit in the same poll that the quiet window
				// ends. Wait may not have posted yet. A status that is already
				// posted, or a process that has already exited, wins over a
				// stall. A clean exit also wins when it lands while the tree
				// is being stopped: the kill itself does not exit 0.
				if err, exited := commandAlreadyExited(done); exited {
					return err
				}
				if commandRenewBeforeKill != nil {
					commandRenewBeforeKill(cmd)
				}
				alreadyExited := !commandStillRunning(cmd)
				TerminateCommandTree(cmd)
				waitErr := finishRenewWait(done, nil)
				if err := ctx.Err(); err != nil {
					return err
				}
				if waitErr == nil || (alreadyExited && !errors.Is(waitErr, errCommandDidNotExit)) {
					return waitErr
				}
				return &CommandStallError{Quiet: quietFor}
			}
		}
	}
}

func sampleRenewCPU(cmd *exec.Cmd) (map[int]time.Duration, bool) {
	if cmd == nil || cmd.Process == nil {
		return nil, false
	}
	return commandTreeCPUByPID(cmd.Process.Pid)
}

// sampleCommandTreeCPU is the summed tree time. Tests that only need "did
// this process get busier" use it. Renewal itself tracks each pid.
func sampleCommandTreeCPU(pid int) (time.Duration, bool) {
	byPID, ok := sampleCommandTreeCPUByPID(pid)
	if !ok {
		return 0, false
	}
	var total time.Duration
	for _, cpu := range byPID {
		total += cpu
	}
	return total, true
}

func finishRenewWait(done <-chan error, prefer error) error {
	select {
	case err := <-done:
		if prefer != nil {
			return prefer
		}
		return err
	case <-time.After(5 * time.Second):
		if prefer != nil {
			return prefer
		}
		return errCommandDidNotExit
	}
}

// commandStillRunning reports whether cmd's process is still executing.
// A pid that is already gone has exited. Any other lookup failure stays
// "running", so a stall kill is not rewritten into the script's exit status.
func commandStillRunning(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return commandPIDRunning(cmd.Process.Pid)
}

// descendantPIDs returns root and every process that descends from it.
// parents maps a pid to its parent pid. Cycles and self-parents are ignored.
func descendantPIDs(root int, parents map[int]int) []int {
	if root <= 0 {
		return nil
	}
	children := make(map[int][]int, len(parents))
	for pid, ppid := range parents {
		if pid <= 0 || pid == ppid {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	out := make([]int, 0, 4)
	stack := []int{root}
	seen := map[int]struct{}{root: {}}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, pid)
		for _, child := range children[pid] {
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			stack = append(stack, child)
		}
	}
	return out
}

// procStatUsage reads ppid and the raw user/system tick counts from one
// /proc/<pid>/stat line. The command name is inside parentheses and may
// itself contain spaces and parentheses, so the numeric fields start after
// the last ')'.
func procStatUsage(line string) (ppid int, utime, stime int64, ok bool) {
	end := strings.LastIndexByte(line, ')')
	if end < 0 || end+1 >= len(line) {
		return 0, 0, 0, false
	}
	fields := strings.Fields(line[end+1:])
	// state, ppid, pgrp, session, tty, tpgid, flags, minflt, cminflt,
	// majflt, cmajflt, utime, stime.
	if len(fields) < 13 {
		return 0, 0, 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid < 0 {
		return 0, 0, 0, false
	}
	utime, err = strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	stime, err = strconv.ParseInt(fields[12], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return ppid, utime, stime, true
}

// cpuProgressTracker turns per-pid samples into a progress delta.
// The first successful sample is a baseline. Later samples sum only positive
// per-pid changes since the previous successful sample. A child that exits
// drops out, so the parent's own growth is still visible. A pid whose CPU
// falls is a new lifetime (the kernel reused the pid); the drop is not
// progress. A new pid contributes the CPU it has already used. Gains below
// commandRenewCPUMin add up only while they land inside
// commandRenewCPUWindow. A failed sample leaves the baseline alone.
type cpuProgressTracker struct {
	seen    map[int]time.Duration
	have    bool
	credits []cpuCredit
	now     func() time.Time
}

type cpuCredit struct {
	at    time.Time
	delta time.Duration
}

func (t *cpuProgressTracker) clock() time.Time {
	if t != nil && t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *cpuProgressTracker) observe(sample map[int]time.Duration, ok bool) time.Duration {
	now := t.clock()
	if !ok {
		t.prune(now)
		return 0
	}
	if !t.have {
		t.seen = copyCPUSample(sample)
		t.have = true
		return 0
	}
	next := make(map[int]time.Duration, len(sample))
	var delta time.Duration
	for pid, cpu := range sample {
		if pid <= 0 || cpu < 0 {
			continue
		}
		prev, existed := t.seen[pid]
		switch {
		case !existed:
			delta += cpu
		case cpu > prev:
			delta += cpu - prev
		}
		next[pid] = cpu
	}
	t.seen = next
	t.prune(now)
	if delta > 0 {
		t.credits = append(t.credits, cpuCredit{at: now, delta: delta})
	}
	sum := t.creditSum()
	if sum < commandRenewCPUMin {
		return 0
	}
	t.credits = nil
	return sum
}

func (t *cpuProgressTracker) prune(now time.Time) {
	if len(t.credits) == 0 {
		return
	}
	cutoff := now.Add(-commandRenewCPUWindow)
	kept := t.credits[:0]
	for _, credit := range t.credits {
		if credit.at.After(cutoff) {
			kept = append(kept, credit)
		}
	}
	t.credits = kept
}

func (t *cpuProgressTracker) creditSum() time.Duration {
	var sum time.Duration
	for _, credit := range t.credits {
		sum += credit.delta
	}
	return sum
}

func copyCPUSample(sample map[int]time.Duration) map[int]time.Duration {
	out := make(map[int]time.Duration, len(sample))
	for pid, cpu := range sample {
		if pid <= 0 || cpu < 0 {
			continue
		}
		out[pid] = cpu
	}
	return out
}

// commandAlreadyExited reports a Wait that has already finished. The stall
// path uses it so a process that exited as the quiet window ended keeps its
// real status.
func commandAlreadyExited(done <-chan error) (error, bool) {
	select {
	case err := <-done:
		return err, true
	default:
		return nil, false
	}
}

// procATCLKTCK is the Linux auxiliary-vector key for the user-space clock
// tick rate. golang.org/x/sys/unix has Sysconf only on Solaris, so the Linux
// sampler reads this key instead of calling Sysconf.
const procATCLKTCK uintptr = 17

// clockTicksFromAuxv returns the user-space ticks per second. A missing or
// zero AT_CLKTCK uses the Linux userspace ABI value of 100.
func clockTicksFromAuxv(vec [][2]uintptr) int64 {
	for _, pair := range vec {
		if pair[0] == procATCLKTCK && pair[1] > 0 {
			return int64(pair[1])
		}
	}
	return 100
}

// parseProcChildren reads a /proc/<pid>/task/<tid>/children file. Entries are
// space-separated pids. Duplicates and non-pids are dropped.
func parseProcChildren(data string) []int {
	fields := strings.Fields(data)
	out := make([]int, 0, len(fields))
	seen := make(map[int]struct{}, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	return out
}
