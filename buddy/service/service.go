// Package service is the host side of the MaClaw Buddy protocol: it keeps one
// USB serial link alive, pushes task/weather/clock frames, and reports device
// acks. It knows nothing about where task data comes from; the GUI pushes
// snapshots and the development CLI pushes mock data.
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

// Dialer opens a fresh connection. It is called again after every failure so
// the runner can survive an unplug.
type Dialer func() (io.ReadWriteCloser, error)

// Options tunes cadence and is overridable in tests.
type Options struct {
	Heartbeat         time.Duration
	FlushInterval     time.Duration
	AmbientRefresh    time.Duration
	ClockRefresh      time.Duration
	StaleAfter        time.Duration
	WeatherStaleAfter time.Duration
	// ReadyTimeout bounds the wait for the device hello and for each pet
	// asset ack. USB CDC buffers a whole burst, so the host paces the asset
	// transfer on these acks instead of on wall-clock delays.
	ReadyTimeout time.Duration
	HostName     string
	HostVersion  string
	Logf         func(format string, args ...any)
}

// DefaultOptions mirrors protocol/spec.json.
func DefaultOptions() Options {
	return Options{
		Heartbeat:         3 * time.Second,
		FlushInterval:     200 * time.Millisecond,
		AmbientRefresh:    30 * time.Minute,
		ClockRefresh:      time.Minute,
		StaleAfter:        60 * time.Second,
		WeatherStaleAfter: 2 * time.Hour,
		ReadyTimeout:      2 * time.Second,
		HostName:          "MaClaw",
		Logf:              func(string, ...any) {},
	}
}

func (o Options) normalize() Options {
	def := DefaultOptions()
	if o.Heartbeat <= 0 {
		o.Heartbeat = def.Heartbeat
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = def.FlushInterval
	}
	if o.AmbientRefresh <= 0 {
		o.AmbientRefresh = def.AmbientRefresh
	}
	if o.ClockRefresh <= 0 {
		o.ClockRefresh = def.ClockRefresh
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = def.StaleAfter
	}
	if o.WeatherStaleAfter <= 0 {
		o.WeatherStaleAfter = def.WeatherStaleAfter
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = def.ReadyTimeout
	}
	if o.HostName == "" {
		o.HostName = def.HostName
	}
	if o.Logf == nil {
		o.Logf = def.Logf
	}
	return o
}

// Status is a snapshot for the settings UI and diagnostics.
type Status struct {
	Connected     bool      `json:"connected"`
	Port          string    `json:"port,omitempty"`
	DeviceFW      string    `json:"device_fw,omitempty"`
	DeviceBoard   string    `json:"device_board,omitempty"`
	DeviceProto   int       `json:"device_proto,omitempty"`
	DeviceSound   bool      `json:"device_sound"`
	Page          string    `json:"page,omitempty"`
	LastAckAt     time.Time `json:"last_ack_at,omitempty"`
	LastStateAt   time.Time `json:"last_state_at,omitempty"`
	UptimeS       uint32    `json:"uptime_s,omitempty"`
	MissingGlyphs int       `json:"missing_glyphs,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	DeviceTouch   bool      `json:"device_touch"`
	// PetReady is true once the device has committed every pet frame.
	PetReady bool `json:"pet_ready"`

	// Diagnostic fields used by the end-to-end verification: what was sent,
	// when it was sent, and when the device acknowledged it.
	LastSentAt          time.Time           `json:"last_sent_at,omitempty"`
	LastSentSeq         uint64              `json:"last_sent_seq,omitempty"`
	LastSentCounts      *protocol.Counts    `json:"last_sent_counts,omitempty"`
	LastSentCurrent     []protocol.TaskItem `json:"last_sent_current,omitempty"`
	DeviceMissingGlyphs int                 `json:"device_missing_glyphs,omitempty"`
}

// Runner owns the link lifecycle and the latest data to send.
type Runner struct {
	dial Dialer
	opts Options

	mu         sync.Mutex
	snap       protocol.Snapshot
	hasSnap    bool
	snapAt     time.Time
	weather    protocol.Weather
	hasWeather bool
	weatherAt  time.Time
	sound      protocol.SoundConfig
	hasSound   bool
	petFrames  [][]byte
	petFrameMS int
	hasPet     bool
	petGen     uint64
	status     Status
	seq        uint64

	dirty     chan struct{}
	soundWake chan struct{}
	petWake   chan struct{}
	ready     chan struct{}
	assetAcks chan protocol.AssetAck
}

// NewRunner builds a runner; call Run to start it.
func NewRunner(dial Dialer, opts Options) *Runner {
	return &Runner{
		dial:      dial,
		opts:      opts.normalize(),
		dirty:     make(chan struct{}, 1),
		soundWake: make(chan struct{}, 1),
		petWake:   make(chan struct{}, 1),
		ready:     make(chan struct{}, 1),
		assetAcks: make(chan protocol.AssetAck, 8),
	}
}

// SetSnapshot stores the newest task census. The GUI calls this after every
// task-list change; the runner coalesces writes on its flush tick.
func (r *Runner) SetSnapshot(s protocol.Snapshot) {
	r.mu.Lock()
	r.snap = s
	r.hasSnap = true
	r.snapAt = time.Now()
	r.mu.Unlock()
	r.wake()
}

// SetPetAsset stores the animated pet frames (128x128 RGB565+A8) and sends
// them to the device on every connect and after a change.
func (r *Runner) SetPetAsset(frames [][]byte, frameMS int) {
	if len(frames) == 0 || len(frames) > protocol.AssetMaxFrames {
		return
	}
	copied := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		if len(frame) != protocol.AssetFrameBytes {
			return
		}
		copied = append(copied, append([]byte(nil), frame...))
	}
	if frameMS <= 0 {
		frameMS = 450
	}
	r.mu.Lock()
	r.petFrames = copied
	r.petFrameMS = frameMS
	r.hasPet = true
	r.petGen++
	r.mu.Unlock()
	select {
	case r.petWake <- struct{}{}:
	default:
	}
}

// SetWeather stores the newest weather snapshot.
func (r *Runner) SetWeather(w protocol.Weather) {
	r.mu.Lock()
	r.weather = w
	r.hasWeather = true
	r.weatherAt = time.Now()
	r.mu.Unlock()
	r.wake()
}

// SetSoundConfig stores the host-selected notification sounds. The runner
// sends them on every connect and immediately after a change.
func (r *Runner) SetSoundConfig(cfg protocol.SoundConfig) {
	cfg.Sanitize()
	r.mu.Lock()
	r.sound = cfg
	r.hasSound = true
	r.mu.Unlock()
	select {
	case r.soundWake <- struct{}{}:
	default:
	}
}

// Status returns a copy of the current link status.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.status
	// Deep-copy the diagnostic payloads so callers cannot mutate the runner's
	// state through a returned slice/pointer.
	if r.status.LastSentCounts != nil {
		counts := *r.status.LastSentCounts
		out.LastSentCounts = &counts
	}
	if len(r.status.LastSentCurrent) > 0 {
		out.LastSentCurrent = append([]protocol.TaskItem(nil), r.status.LastSentCurrent...)
	}
	return out
}

// SetPort records the port name for diagnostics.
func (r *Runner) SetPort(name string) {
	r.mu.Lock()
	r.status.Port = name
	r.mu.Unlock()
}

func (r *Runner) wake() {
	select {
	case r.dirty <- struct{}{}:
	default:
	}
}

func (r *Runner) setError(err error) {
	r.mu.Lock()
	r.status.LastError = err.Error()
	r.status.Connected = false
	r.mu.Unlock()
	r.opts.Logf("[buddy] %v", err)
}

// Run keeps a connection alive until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		conn, err := r.dial()
		if err != nil {
			r.setError(err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		r.mu.Lock()
		r.status.Connected = true
		r.status.LastError = ""
		r.mu.Unlock()

		err = r.session(ctx, conn)
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		r.setError(err)
		if !sleepCtx(ctx, time.Second) {
			return
		}
	}
}

func (r *Runner) session(ctx context.Context, conn io.ReadWriteCloser) error {
	// Only this goroutine writes, so frames never interleave.
	readErr := make(chan error, 1)
	go func() {
		reader := protocol.NewLineReader(conn, protocol.MaxLineBytes)
		for {
			line, err := reader.Next()
			if err != nil {
				readErr <- err
				return
			}
			msg, err := protocol.DecodeLine(line)
			if err != nil {
				r.opts.Logf("[buddy] bad frame: %v", err)
				continue
			}
			r.handleDeviceMessage(msg)
		}
	}()

	r.drainReady()
	r.drainAssetAcks()
	if err := r.write(conn, &protocol.Message{
		Type:       protocol.TypeHello,
		Proto:      protocol.Version,
		FullResend: true,
		Host:       r.opts.HostName,
		Ver:        r.opts.HostVersion,
	}); err != nil {
		return err
	}
	// A device that just rebooted still has the ROM USB console attached for
	// a moment; its hello proves the firmware parser is draining lines before
	// we push sound and asset frames at it.
	if err := r.waitReady(ctx); err != nil {
		return err
	}
	// Full resend after every (re)connect. A SetPetAsset/SetSoundConfig that
	// raced with the connect queued a wake; that data is included below, so
	// drop the redundant wake instead of sending it twice.
	r.wake()
	r.drainSoundWake()
	if r.hasSoundConfig() {
		if err := r.sendSound(conn); err != nil {
			return err
		}
	}

	// Put the task screen on the device first: the pet transfer is paced on
	// per-chunk acks and can take a couple of seconds.
	statePending := true
	ambientPending := true
	if err := r.sendState(conn); err != nil {
		return err
	}
	if err := r.sendAmbient(conn); err != nil {
		return err
	}
	if err := r.sendClock(conn); err != nil {
		return err
	}
	statePending = false
	ambientPending = false

	// The generation guard keeps a SetPetAsset that raced with this connect
	// from sending the same frames twice.
	sentPetGen := uint64(0)
	if r.hasPetAsset() {
		gen, err := r.sendPetAsset(ctx, conn)
		if err != nil {
			return err
		}
		sentPetGen = gen
	}

	heartbeat := time.NewTicker(r.opts.Heartbeat)
	defer heartbeat.Stop()
	flush := time.NewTicker(r.opts.FlushInterval)
	defer flush.Stop()
	refresh := time.NewTicker(r.opts.AmbientRefresh)
	defer refresh.Stop()
	clock := time.NewTicker(r.opts.ClockRefresh)
	defer clock.Stop()

	clockPending := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			if errors.Is(err, io.EOF) {
				return errors.New("device closed the port")
			}
			return err
		case <-r.dirty:
			statePending = true
			ambientPending = true
		case <-r.soundWake:
			if err := r.sendSound(conn); err != nil {
				return err
			}
		case <-r.petWake:
			if r.currentPetGen() == sentPetGen {
				continue
			}
			gen, err := r.sendPetAsset(ctx, conn)
			if err != nil {
				return err
			}
			sentPetGen = gen
		case <-flush.C:
			if !r.anyData() {
				continue
			}
			if statePending {
				if err := r.sendState(conn); err != nil {
					return err
				}
				statePending = false
			}
			if ambientPending {
				if err := r.sendAmbient(conn); err != nil {
					return err
				}
				ambientPending = false
			}
		case <-heartbeat.C:
			if err := r.write(conn, &protocol.Message{Type: protocol.TypeHeartbeat, Ts: time.Now().Unix()}); err != nil {
				return err
			}
			if clockPending {
				if err := r.sendClock(conn); err != nil {
					return err
				}
				clockPending = false
			}
		case <-clock.C:
			clockPending = true
		case <-refresh.C:
			ambientPending = true
		}
	}
}

func (r *Runner) anyData() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasSnap || r.hasWeather
}

func (r *Runner) sendState(conn io.ReadWriteCloser) error {
	r.mu.Lock()
	if !r.hasSnap {
		r.mu.Unlock()
		return nil
	}
	snap := r.snap
	received := r.snapAt
	r.seq++
	seq := r.seq
	hadPrev := r.status.LastSentCounts != nil
	var prevCounts protocol.Counts
	if hadPrev {
		prevCounts = *r.status.LastSentCounts
	}
	prevCurrent := append([]protocol.TaskItem(nil), r.status.LastSentCurrent...)
	r.mu.Unlock()

	msg := &protocol.Message{
		Type:    protocol.TypeState,
		Proto:   protocol.Version,
		Ts:      time.Now().Unix(),
		Seq:     seq,
		Counts:  &snap.Counts,
		Current: append([]protocol.TaskItem(nil), snap.Current...),
	}
	if snap.UpdatedAt > 0 {
		if at := time.UnixMilli(snap.UpdatedAt); time.Since(at) > r.opts.StaleAfter {
			msg.Stale = true
		}
	} else if time.Since(received) > r.opts.StaleAfter {
		msg.Stale = true
	}
	msg.Sanitize()
	if err := msg.Validate(); err != nil {
		r.opts.Logf("[buddy] snapshot rejected: %v", err)
		return nil
	}
	if err := r.write(conn, msg); err != nil {
		return err
	}
	celebrate := completionCelebration(hadPrev, prevCounts, prevCurrent, snap.Counts, msg.Current)
	r.mu.Lock()
	r.status.LastStateAt = time.Now()
	r.status.LastSentAt = r.status.LastStateAt
	r.status.LastSentSeq = seq
	counts := snap.Counts
	r.status.LastSentCounts = &counts
	r.status.LastSentCurrent = append([]protocol.TaskItem(nil), msg.Current...)
	r.mu.Unlock()
	if celebrate != nil {
		// Send after the state frame so the device updates its counters first
		// and returns to the correct page when the celebration ends.
		cm := &protocol.Message{
			Type:      protocol.TypeCelebrate,
			Proto:     protocol.Version,
			Ts:        time.Now().Unix(),
			Celebrate: celebrate,
		}
		if err := r.write(conn, cm); err != nil {
			return err
		}
		r.opts.Logf("[buddy] celebrate %q", celebrate.Title)
	}
	return nil
}

// completionCelebration detects a single task that left the in-progress list
// because it finished. It deliberately ignores deletions (total shrank) and
// bursts where no title was ever sent, so the device never celebrates a task
// the person removed.
func completionCelebration(
	hadPrev bool,
	prev protocol.Counts,
	prevCurrent []protocol.TaskItem,
	next protocol.Counts,
	nextCurrent []protocol.TaskItem,
) *protocol.Celebrate {
	if !hadPrev {
		return nil
	}
	if next.Done <= prev.Done {
		return nil
	}
	if next.Total < prev.Total {
		return nil
	}
	curIDs := make(map[string]struct{}, len(nextCurrent))
	curTitles := make(map[string]struct{}, len(nextCurrent))
	for _, t := range nextCurrent {
		if t.ID != "" {
			curIDs[t.ID] = struct{}{}
		}
		if t.Title != "" {
			curTitles[t.Title] = struct{}{}
		}
	}
	for _, t := range prevCurrent {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		_, byID := curIDs[t.ID]
		_, byTitle := curTitles[t.Title]
		if (t.ID != "" && byID) || byTitle {
			continue
		}
		return &protocol.Celebrate{Title: t.Title, Seconds: 5}
	}
	return nil
}

func (r *Runner) hasSoundConfig() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasSound
}

func (r *Runner) hasPetAsset() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasPet
}

// sendPetAsset streams every frame in 512-byte base64 chunks. Each chunk is
// acknowledged by the device before the next one is written: USB CDC happily
// buffers a whole burst, so wall-clock pacing either overflows the device's RX
// buffer or crawls at the OS timer granularity.
func (r *Runner) sendPetAsset(ctx context.Context, conn io.ReadWriteCloser) (uint64, error) {
	r.mu.Lock()
	if !r.hasPet {
		r.mu.Unlock()
		return 0, nil
	}
	frames := r.petFrames
	frameMS := r.petFrameMS
	gen := r.petGen
	r.status.PetReady = false
	r.mu.Unlock()

	r.drainAssetAcks()

	const chunkBytes = 512
	sent := 0
	finalReady := false
	for fi, frame := range frames {
		crc := crc32.ChecksumIEEE(frame)
		total := len(frame)
		for off := 0; off < total; off += chunkBytes {
			end := off + chunkBytes
			if end > total {
				end = total
			}
			msg := &protocol.Message{
				Type:  protocol.TypeAssetChunk,
				Proto: protocol.Version,
				Asset: &protocol.AssetChunk{
					Frame:   fi,
					Frames:  len(frames),
					Width:   protocol.AssetWidth,
					Height:  protocol.AssetHeight,
					Offset:  off,
					Total:   total,
					CRC32:   crc,
					FrameMS: frameMS,
					Data:    base64.StdEncoding.EncodeToString(frame[off:end]),
				},
			}
			if err := msg.Validate(); err != nil {
				r.opts.Logf("[buddy] pet chunk rejected: %v", err)
				continue
			}
			if err := r.write(conn, msg); err != nil {
				return gen, err
			}
			sent++
			ready, ok := r.waitAssetAck(ctx, fi, end)
			if !ok {
				r.opts.Logf("[buddy] pet asset stalled at frame %d/%d offset %d; device did not ack",
					fi+1, len(frames), end)
				return gen, nil
			}
			if fi == len(frames)-1 && end == total {
				finalReady = ready
			}
		}
	}
	r.mu.Lock()
	r.status.PetReady = finalReady
	r.mu.Unlock()
	if finalReady {
		r.opts.Logf("[buddy] pet asset confirmed: %d frames, %d chunks", len(frames), sent)
	} else {
		r.opts.Logf("[buddy] pet asset incomplete: %d frames, %d chunks without ready", len(frames), sent)
	}
	return gen, nil
}

// currentPetGen lets the session drop a wake that belongs to frames it has
// already sent (SetPetAsset racing with the connect-time transfer).
func (r *Runner) currentPetGen() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.petGen
}

// waitAssetAck waits for the device's commit ack for one chunk. Acks from
// earlier chunks (the device is strictly sequential) are skipped.
func (r *Runner) waitAssetAck(ctx context.Context, frame, bytes int) (ready, ok bool) {
	timer := time.NewTimer(r.opts.ReadyTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, false
		case <-timer.C:
			return false, false
		case ack := <-r.assetAcks:
			// Exact match: a stale ack from an aborted transfer with a later
			// offset must not satisfy this chunk.
			if ack.Frame == frame && ack.Bytes == bytes {
				return ack.Ready, true
			}
		}
	}
}

func (r *Runner) waitReady(ctx context.Context) error {
	timer := time.NewTimer(r.opts.ReadyTimeout)
	defer timer.Stop()
	select {
	case <-r.ready:
		return nil
	case <-timer.C:
		r.opts.Logf("[buddy] device hello not seen within %s; continuing", r.opts.ReadyTimeout)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) drainReady() {
	for {
		select {
		case <-r.ready:
		default:
			return
		}
	}
}

func (r *Runner) drainAssetAcks() {
	for {
		select {
		case <-r.assetAcks:
		default:
			return
		}
	}
}

func (r *Runner) drainSoundWake() {
	for {
		select {
		case <-r.soundWake:
		default:
			return
		}
	}
}

// signalReady is safe to call while holding r.mu: the channel never blocks.
func (r *Runner) signalReady() {
	select {
	case r.ready <- struct{}{}:
	default:
	}
}

func (r *Runner) sendSound(conn io.ReadWriteCloser) error {
	r.mu.Lock()
	if !r.hasSound {
		r.mu.Unlock()
		return nil
	}
	cfg := r.sound
	r.mu.Unlock()

	msg := &protocol.Message{
		Type:  protocol.TypeSound,
		Proto: protocol.Version,
		Ts:    time.Now().Unix(),
		Sound: &cfg,
	}
	msg.Sanitize()
	if err := msg.Validate(); err != nil {
		r.opts.Logf("[buddy] sound config rejected: %v", err)
		return nil
	}
	return r.write(conn, msg)
}

func (r *Runner) sendAmbient(conn io.ReadWriteCloser) error {
	r.mu.Lock()
	if !r.hasWeather {
		r.mu.Unlock()
		return nil
	}
	weather := r.weather
	at := r.weatherAt
	r.mu.Unlock()

	msg := &protocol.Message{
		Type:    protocol.TypeAmbient,
		Proto:   protocol.Version,
		Ts:      time.Now().Unix(),
		Weather: &weather,
		Stale:   time.Since(at) > r.opts.WeatherStaleAfter,
	}
	msg.Sanitize()
	if err := msg.Validate(); err != nil {
		r.opts.Logf("[buddy] weather rejected: %v", err)
		return nil
	}
	return r.write(conn, msg)
}

func (r *Runner) sendClock(conn io.ReadWriteCloser) error {
	clock := clockOf(time.Now())
	return r.write(conn, &protocol.Message{
		Type:  protocol.TypeClock,
		Proto: protocol.Version,
		Clock: &clock,
	})
}

var weekdays = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func clockOf(t time.Time) protocol.Clock {
	return protocol.Clock{
		Date:    fmt.Sprintf("%d月%d日", int(t.Month()), t.Day()),
		Weekday: weekdays[int(t.Weekday())],
	}
}

func (r *Runner) write(conn io.ReadWriteCloser, msg *protocol.Message) error {
	frame, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	// A USB CDC write may be short; loop until the whole frame is accepted.
	for len(frame) > 0 {
		n, err := conn.Write(frame)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func (r *Runner) handleDeviceMessage(msg *protocol.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch msg.Type {
	case protocol.TypeHello:
		r.status.DeviceFW = msg.FW
		r.status.DeviceBoard = msg.Board
		r.status.DeviceProto = msg.Proto
		r.status.DeviceTouch = msg.Touch
		r.status.DeviceSound = msg.SoundAvailable
		r.signalReady()
	case protocol.TypeStateAck:
		r.status.LastAckAt = time.Now()
		r.status.Page = msg.Page
		if msg.MissingGlyphs > 0 {
			r.status.MissingGlyphs = msg.MissingGlyphs
		}
		r.signalReady()
	case protocol.TypeAssetAck:
		if msg.AssetAck != nil {
			select {
			case r.assetAcks <- *msg.AssetAck:
			default:
			}
		}
	case protocol.TypeHeartbeat:
		r.status.UptimeS = msg.UptimeS
		if msg.Page != "" {
			r.status.Page = msg.Page
		}
		if msg.MissingGlyphs > r.status.DeviceMissingGlyphs {
			r.status.DeviceMissingGlyphs = msg.MissingGlyphs
		}
	case protocol.TypeLog:
		line, _ := json.Marshal(struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}{msg.Level, msg.Msg})
		r.opts.Logf("[device] %s", line)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
