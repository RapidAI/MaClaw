package service

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

func testOptions() Options {
	opts := DefaultOptions()
	opts.Heartbeat = 25 * time.Millisecond
	opts.FlushInterval = 10 * time.Millisecond
	opts.ClockRefresh = 20 * time.Millisecond
	opts.AmbientRefresh = time.Hour
	opts.StaleAfter = 5 * time.Second
	opts.WeatherStaleAfter = time.Hour
	opts.Logf = func(string, ...any) {}
	return opts
}

func sampleSnapshot() protocol.Snapshot {
	return protocol.Snapshot{
		Counts: protocol.Counts{Total: 3, InProgress: 1, Pending: 1, Done: 1},
		Current: []protocol.TaskItem{
			{ID: "t1", Title: "整理会议纪要", Progress: 0.5},
		},
		UpdatedAt: time.Now().UnixMilli(),
	}
}

// readFrames reads protocol frames from r until stop returns true or the
// deadline expires.
func readFrames(t *testing.T, r io.Reader, timeout time.Duration, stop func(*protocol.Message) bool) []*protocol.Message {
	t.Helper()
	var got []*protocol.Message
	deadline := time.Now().Add(timeout)
	reader := protocol.NewLineReader(r, protocol.MaxLineBytes)
	for time.Now().Before(deadline) {
		line, err := reader.Next()
		if err != nil {
			return got
		}
		msg, err := protocol.DecodeLine(line)
		if err != nil {
			continue
		}
		got = append(got, msg)
		if stop != nil && stop(msg) {
			return got
		}
	}
	return got
}

func TestRunnerPushesHelloStateAmbientHeartbeat(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	logf := &logBuffer{}
	opts := testOptions()
	opts.Logf = logf.Logf
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, opts)
	runner.SetPort("COM_TEST")
	runner.SetSnapshot(sampleSnapshot())
	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 23, Location: "北京"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	want := map[protocol.MessageType]bool{
		protocol.TypeHello:     false,
		protocol.TypeState:     false,
		protocol.TypeAmbient:   false,
		protocol.TypeHeartbeat: false,
		protocol.TypeClock:     false,
	}
	frames := readFrames(t, host, 2*time.Second, func(m *protocol.Message) bool {
		if _, ok := want[m.Type]; ok {
			want[m.Type] = true
		}
		for _, seen := range want {
			if !seen {
				return false
			}
		}
		return true
	})
	for typ, seen := range want {
		if !seen {
			t.Fatalf("missing %s frame; got %v", typ, frameTypes(frames))
		}
	}

	var state *protocol.Message
	for _, m := range frames {
		if m.Type == protocol.TypeState {
			state = m
			break
		}
	}
	if state == nil || state.Counts == nil {
		t.Fatal("no state frame")
	}
	if state.Counts.Total != 3 || state.Counts.InProgress != 1 || state.Counts.Done != 1 {
		t.Fatalf("unexpected counts: %+v", state.Counts)
	}
	if len(state.Current) != 1 || state.Current[0].Title != "整理会议纪要" {
		t.Fatalf("unexpected current: %+v", state.Current)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("state failed validation: %v", err)
	}

	// Device ack must land in Status.
	if _, err := host.Write([]byte(`{"type":"state_ack","applied":true,"page":"tasks"}` + "\n")); err != nil {
		t.Fatalf("write ack: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runner.Status().Page == "tasks" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := runner.Status().Page; got != "tasks" {
		t.Fatalf("status page=%q, want tasks", got)
	}

	cancel()
	host.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestRunnerRejectsInconsistentCounts(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	logf := &logBuffer{}
	opts := testOptions()
	opts.Logf = logf.Logf
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, opts)
	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 5, InProgress: 1},
		UpdatedAt: time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	frames := readFrames(t, host, 250*time.Millisecond, nil)
	for _, m := range frames {
		if m.Type == protocol.TypeState {
			t.Fatalf("inconsistent snapshot should not be sent: %+v", m.Counts)
		}
	}
	if !strings.Contains(logf.String(), "snapshot rejected") {
		t.Fatalf("expected rejection log, got %q", logf.String())
	}
}

func TestRunnerReconnectsAfterDialFailure(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	var attempts int32
	var mu sync.Mutex
	runner := NewRunner(func() (io.ReadWriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return nil, errors.New("port busy")
		}
		return device, nil
	}, testOptions())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	// First dial fails, second succeeds (1s backoff in Run).
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if runner.Status().Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !runner.Status().Connected {
		t.Fatal("runner never reconnected")
	}
}

func TestRunnerSeqIsMonotonic(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, testOptions())
	runner.SetSnapshot(sampleSnapshot())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	frames := readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.TypeState
	})
	var first uint64
	for _, m := range frames {
		if m.Type == protocol.TypeState {
			first = m.Seq
			break
		}
	}
	if first == 0 {
		t.Fatal("no state frame")
	}

	runner.SetSnapshot(sampleSnapshot())
	more := readFrames(t, host, 500*time.Millisecond, func(m *protocol.Message) bool {
		return m.Type == protocol.TypeState && m.Seq > first
	})
	var second uint64
	for _, m := range more {
		if m.Type == protocol.TypeState && m.Seq > first {
			second = m.Seq
			break
		}
	}
	if second <= first {
		t.Fatalf("seq did not advance: %d -> %d", first, second)
	}
}

func TestClockFormat(t *testing.T) {
	clock := clockOf(time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local))
	if clock.Date != "10月27日" || clock.Weekday != "周二" {
		t.Fatalf("clock=%+v", clock)
	}
}

func frameTypes(frames []*protocol.Message) []protocol.MessageType {
	out := make([]protocol.MessageType, 0, len(frames))
	for _, m := range frames {
		out = append(out, m.Type)
	}
	return out
}

type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(time.Now().Format("15:04:05.000") + " ")
	l.b.WriteString(format)
	l.b.WriteString("\n")
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
