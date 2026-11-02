package service

import (
	"bytes"
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
	opts.ReadyTimeout = 50 * time.Millisecond
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

	next := sampleSnapshot()
	next.Current[0].Title = "写周报"
	runner.SetSnapshot(next)
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

func TestUnchangedCensusDoesNotResendState(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, testOptions())
	runner.SetSnapshot(sampleSnapshot())
	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 8, Location: "鄂尔多斯"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	var first uint64
	readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		if m.Type == protocol.TypeState && first == 0 {
			first = m.Seq
		}
		return first != 0 && m.Type == protocol.TypeAmbient
	})
	if first == 0 {
		t.Fatal("no state frame")
	}

	again := sampleSnapshot()
	again.UpdatedAt = time.Now().Add(time.Minute).UnixMilli()
	runner.SetSnapshot(again)
	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 8, Location: "鄂尔多斯"})
	quiet := readFrames(t, host, 200*time.Millisecond, nil)
	for _, m := range quiet {
		if m.Type == protocol.TypeState && m.Seq > first {
			t.Fatalf("unchanged census resent state seq=%d", m.Seq)
		}
		if m.Type == protocol.TypeAmbient {
			t.Fatal("unchanged weather resent ambient")
		}
	}

	runner.SetWeather(protocol.Weather{Summary: "多云", TemperatureC: 8, Location: "鄂尔多斯"})
	var ambient, state bool
	readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		if m.Type == protocol.TypeAmbient && m.Weather != nil && m.Weather.Summary == "多云" {
			ambient = true
		}
		if m.Type == protocol.TypeState && m.Seq > first {
			state = true
		}
		return ambient
	})
	if !ambient {
		t.Fatal("changed weather was not sent")
	}
	if state {
		t.Fatal("weather refresh also sent state")
	}
}

func TestRepeatSnapshotKeepsCensusFresh(t *testing.T) {
	runner := NewRunner(nil, testOptions())
	aged := sampleSnapshot()
	aged.UpdatedAt = time.Now().Add(-2 * time.Minute).UnixMilli()
	runner.SetSnapshot(aged)

	var first memConn
	if err := runner.sendState(&first); err != nil {
		t.Fatal(err)
	}
	if !mustOneFrame(t, first.Bytes()).Stale {
		t.Fatal("aged census was sent as fresh")
	}

	again := sampleSnapshot()
	again.UpdatedAt = time.Now().UnixMilli()
	runner.SetSnapshot(again)
	var second memConn
	if err := runner.sendState(&second); err != nil {
		t.Fatal(err)
	}
	if mustOneFrame(t, second.Bytes()).Stale {
		t.Fatal("reconfirmed census was marked stale")
	}

	held := NewRunner(nil, testOptions())
	held.SetSnapshot(sampleSnapshot())
	held.mu.Lock()
	held.snap.UpdatedAt = time.Now().Add(-2 * time.Minute).UnixMilli()
	held.mu.Unlock()
	var heldFrame memConn
	if err := held.sendState(&heldFrame); err != nil {
		t.Fatal(err)
	}
	if mustOneFrame(t, heldFrame.Bytes()).Stale {
		t.Fatal("census that only aged in memory was marked stale")
	}
}

func mustOneFrame(t *testing.T, raw []byte) *protocol.Message {
	t.Helper()
	msgs := readFrames(t, bytes.NewReader(raw), time.Second, func(*protocol.Message) bool { return true })
	if len(msgs) != 1 {
		t.Fatalf("frames = %d", len(msgs))
	}
	return msgs[0]
}

type memConn struct{ bytes.Buffer }

func (memConn) Read([]byte) (int, error) { return 0, io.EOF }
func (memConn) Close() error             { return nil }

func TestAmbientRefreshOnlyWhenStaleBitFlips(t *testing.T) {
	runner := NewRunner(nil, testOptions())
	if runner.ambientStaleDue() {
		t.Fatal("refresh without weather")
	}
	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 8, Location: "鄂尔多斯"})
	if !runner.ambientStaleDue() {
		t.Fatal("unsent weather was skipped")
	}

	if err := runner.sendAmbient(discardConn{}); err != nil {
		t.Fatal(err)
	}
	if runner.ambientStaleDue() {
		t.Fatal("fresh weather would be resent")
	}

	runner.mu.Lock()
	runner.weatherAt = time.Now().Add(-3 * time.Hour)
	runner.mu.Unlock()
	if !runner.ambientStaleDue() {
		t.Fatal("stale bit flip was skipped")
	}
	if err := runner.sendAmbient(discardConn{}); err != nil {
		t.Fatal(err)
	}
	if runner.ambientStaleDue() {
		t.Fatal("already-stale weather would be resent")
	}

	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 8, Location: "鄂尔多斯"})
	if !runner.ambientStaleDue() {
		t.Fatal("reconfirmed weather left the stale mark")
	}
	if err := runner.sendAmbient(discardConn{}); err != nil {
		t.Fatal(err)
	}
	if runner.ambientStaleDue() {
		t.Fatal("reconfirmed weather would be sent again")
	}
}

type discardConn struct{}

func (discardConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (discardConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardConn) Close() error                { return nil }

func TestDeviceSoundReportDoesNotEcho(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	var mu sync.Mutex
	var calls int
	var hostHellos int
	var reported protocol.SoundConfig
	opts := testOptions()
	opts.OnDeviceSound = func(cfg protocol.SoundConfig) {
		mu.Lock()
		calls++
		reported = cfg
		mu.Unlock()
	}
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, opts)
	runner.SetSoundConfig(protocol.DefaultSoundConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	var soundFrames int
	framesDone := make(chan struct{})
	go func() {
		defer close(framesDone)
		reader := protocol.NewLineReader(host, protocol.MaxLineBytes)
		for {
			line, err := reader.Next()
			if err != nil {
				return
			}
			msg, err := protocol.DecodeLine(line)
			if err != nil {
				continue
			}
			mu.Lock()
			switch msg.Type {
			case protocol.TypeHello:
				hostHellos++
			case protocol.TypeSound:
				soundFrames++
			}
			mu.Unlock()
		}
	}()

	helloDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(helloDeadline) {
		mu.Lock()
		n := hostHellos
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	gotHello := hostHellos
	mu.Unlock()
	if gotHello < 1 {
		t.Fatal("host hello was not sent")
	}

	hello := `{"type":"hello","proto":2,"fw":"buddy-0.1.0","board":"echoear-2st","touch":true,"sound_available":true,"disp":{"w":360,"h":360,"round":true}}` + "\n"
	if _, err := host.Write([]byte(hello)); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := soundFrames
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	baseline := soundFrames
	mu.Unlock()
	if baseline < 1 {
		t.Fatal("host never sent the initial sound config")
	}

	frame := `{"type":"sound","proto":2,"sound":{"master":false,"running":{"enabled":true,"effect":0},"complete":{"enabled":true,"effect":0},"standby":{"enabled":true,"effect":0}}}` + "\n"
	if _, err := host.Write([]byte(frame)); err != nil {
		t.Fatalf("write device sound: %v", err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n >= 1 && !runner.SoundConfig().Master && runner.Status().DeviceWidth == 360 && runner.Status().DeviceRound {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if runner.SoundConfig().Master || runner.Status().SoundMaster {
		t.Fatal("device mute did not update the runner")
	}
	if runner.Status().DeviceWidth != 360 || runner.Status().DeviceHeight != 360 || !runner.Status().DeviceRound {
		t.Fatalf("panel status = %+v", runner.Status())
	}
	mu.Lock()
	if calls != 1 || reported.Master {
		t.Fatalf("callback calls=%d master=%v", calls, reported.Master)
	}
	mu.Unlock()

	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	extra := soundFrames - baseline
	mu.Unlock()
	if extra != 0 {
		t.Fatalf("device sound was echoed %d times", extra)
	}
	host.Close()
	<-framesDone
}

func TestClockFormat(t *testing.T) {
	clock := clockOf(time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local))
	if clock.Date != "10月27日" || clock.Weekday != "周二" {
		t.Fatalf("clock=%+v", clock)
	}
}

func TestDeviceTapReachesHost(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()

	var mu sync.Mutex
	var calls int
	var got protocol.Input
	var hostHellos int
	opts := testOptions()
	opts.OnDeviceInput = func(in protocol.Input) {
		mu.Lock()
		calls++
		got = in
		mu.Unlock()
	}
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)

	go func() {
		reader := protocol.NewLineReader(host, protocol.MaxLineBytes)
		for {
			line, err := reader.Next()
			if err != nil {
				return
			}
			msg, err := protocol.DecodeLine(line)
			if err != nil {
				continue
			}
			if msg.Type == protocol.TypeHello {
				mu.Lock()
				hostHellos++
				mu.Unlock()
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := hostHellos
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	idx := 0
	writeTestFrame(t, host, &protocol.Message{
		Type: protocol.TypeHello, Proto: protocol.Version, FW: "buddy-test", Board: "echoear",
		Display: &protocol.Display{Width: 240, Height: 240, Panel: "nv3023-240", Font: "cjk24-ext1"},
		Key:     true,
	})
	writeTestFrame(t, host, &protocol.Message{
		Type: protocol.TypeInput, Proto: protocol.Version,
		Input: &protocol.Input{Gesture: "tap", Page: "standby", Index: &idx},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || got.Gesture != "tap" || got.Index == nil || *got.Index != 0 {
		t.Fatalf("calls=%d input=%+v", calls, got)
	}
	st := runner.Status()
	if st.DeviceWidth != 240 || st.DeviceRound || st.DevicePanel != "nv3023-240" || st.DeviceFont != "cjk24-ext1" || !st.DeviceKey {
		t.Fatalf("hello glass = %+v", st)
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

// gatePort blocks Read and Write until Close. Cancel must close it even
// while the session is stuck in the first hello write.
type gatePort struct {
	once   sync.Once
	closed chan struct{}
}

func newGatePort() *gatePort {
	return &gatePort{closed: make(chan struct{})}
}

func (p *gatePort) Read([]byte) (int, error) {
	<-p.closed
	return 0, io.EOF
}

func (p *gatePort) Write([]byte) (int, error) {
	<-p.closed
	return 0, io.ErrClosedPipe
}

func (p *gatePort) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func TestRunStopsWhenLinkDisabled(t *testing.T) {
	calls := 0
	runner := NewRunner(func() (io.ReadWriteCloser, error) {
		calls++
		return nil, ErrLinkDisabled
	}, testOptions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a disabled link was retried")
	}
	if calls != 1 {
		t.Fatalf("dial calls = %d", calls)
	}
}

func TestRunClosesPortWhenWriteBlocks(t *testing.T) {
	port := newGatePort()
	opened := make(chan struct{})
	runner := NewRunner(func() (io.ReadWriteCloser, error) {
		close(opened)
		return port, nil
	}, testOptions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("dial did not run")
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-port.closed:
	case <-time.After(time.Second):
		t.Fatal("cancel left the port open")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after the port closed")
	}
}

func TestRunClosesPortIfDialReturnsAfterCancel(t *testing.T) {
	port := newGatePort()
	started := make(chan struct{})
	release := make(chan struct{})
	runner := NewRunner(func() (io.ReadWriteCloser, error) {
		close(started)
		<-release
		return port, nil
	}, testOptions())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dial did not start")
	}
	cancel()
	close(release)
	select {
	case <-port.closed:
	case <-time.After(time.Second):
		t.Fatal("port opened after cancel was kept")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}
}
