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

// ErrLinkDisabled means the companion switch is off. Run stops instead of
// treating it as a dropped cable and opening the port again.
var ErrLinkDisabled = errors.New("buddy link disabled")

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
	// OnDeviceSound is invoked when the glass reports a local change, such as
	// a long-press mute. It must not push the config back down the wire.
	OnDeviceSound func(protocol.SoundConfig)
	// OnDeviceInput is a short press. The host focuses a task; it does not
	// write a frame back.
	OnDeviceInput func(protocol.Input)
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
	Connected      bool      `json:"connected"`
	Port           string    `json:"port,omitempty"`
	DeviceFW       string    `json:"device_fw,omitempty"`
	DeviceBoard    string    `json:"device_board,omitempty"`
	DeviceProto    int       `json:"device_proto,omitempty"`
	DeviceSound    bool      `json:"device_sound"`
	Page           string    `json:"page,omitempty"`
	LastAckAt      time.Time `json:"last_ack_at,omitempty"`
	LastStateAt    time.Time `json:"last_state_at,omitempty"`
	UptimeS        uint32    `json:"uptime_s,omitempty"`
	MissingGlyphs  int       `json:"missing_glyphs,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	DeviceTouch    bool      `json:"device_touch"`
	DeviceKey      bool      `json:"device_key"`
	DeviceWidth    int       `json:"device_width,omitempty"`
	DeviceHeight   int       `json:"device_height,omitempty"`
	DeviceRound    bool      `json:"device_round,omitempty"`
	DevicePanel    string    `json:"device_panel,omitempty"`
	DeviceFont     string    `json:"device_font,omitempty"`
	SoundMaster    bool      `json:"sound_master"`
	StateLatencyMS int64     `json:"state_latency_ms,omitempty"`
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

	mu      sync.Mutex
	snap    protocol.Snapshot
	hasSnap bool
	snapAt  time.Time
	// snapStale is fixed when a census is accepted. A task list that simply
	// sits unchanged must not turn stale on the next reconnect.
	snapStale  bool
	weather    protocol.Weather
	hasWeather bool
	weatherAt  time.Time
	sound      protocol.SoundConfig
	hasSound   bool
	petFrames  [][]byte
	petFrameMS int
	hasPet     bool
	petGen     uint64
	assetBin   bool
	status     Status
	seq        uint64

	dirty        chan struct{}
	stateDirty   bool
	ambientDirty bool
	mood         string
	hasMood      bool
	moodDirty    bool
	// ambientSent remembers the stale bit of the last ambient frame so the
	// half-hour ticker can stay quiet until that bit actually flips.
	ambientSent      bool
	lastAmbientStale bool
	soundWake        chan struct{}
	petWake          chan struct{}
	ready            chan struct{}
	assetAcks        chan protocol.AssetAck
	// live is the port the session is using. closing is set when the context
	// is cancelled, so a dial that returns late cannot keep the handle.
	live    io.Closer
	closing bool
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
// task-list change; the runner coalesces writes on its flush tick. A repeat
// of the same counts and titles does not send a frame, so a clock tick in
// updated_at cannot light the glass. Stale is decided here, when the census
// is accepted: leaving it in memory does not make a later reconnect stale.
func (r *Runner) SetSnapshot(s protocol.Snapshot) {
	r.mu.Lock()
	if r.hasSnap && censusEqual(r.snap, s) {
		if s.UpdatedAt > r.snap.UpdatedAt {
			r.snap.UpdatedAt = s.UpdatedAt
		}
		r.snapAt = time.Now()
		r.snapStale = snapshotAlreadyStale(r.snap.UpdatedAt, r.opts.StaleAfter)
		r.mu.Unlock()
		return
	}
	r.snap = s
	r.hasSnap = true
	r.snapAt = time.Now()
	r.snapStale = snapshotAlreadyStale(s.UpdatedAt, r.opts.StaleAfter)
	r.stateDirty = true
	r.mu.Unlock()
	r.poke()
}

// SetPetMood stores the assistant emotion. "busy" while a turn is running,
// "idle" when it ends. Repeats are not sent. The device does not treat this
// frame as a reason to turn the backlight on.
func (r *Runner) SetPetMood(state string) {
	switch state {
	case "idle", "busy":
	default:
		return
	}
	r.mu.Lock()
	if r.hasMood && r.mood == state {
		r.mu.Unlock()
		return
	}
	r.mood = state
	r.hasMood = true
	r.moodDirty = true
	r.mu.Unlock()
	r.poke()
}

func (r *Runner) sendPetMood(conn io.ReadWriteCloser) error {
	r.mu.Lock()
	if !r.hasMood {
		r.moodDirty = false
		r.mu.Unlock()
		return nil
	}
	state := r.mood
	r.moodDirty = false
	r.mu.Unlock()
	msg := &protocol.Message{
		Type:  protocol.TypePet,
		Proto: protocol.Version,
		Ts:    time.Now().Unix(),
		Pet:   &protocol.Pet{State: state},
	}
	return r.write(conn, msg)
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

// SetWeather stores the newest weather snapshot. A repeat of the same report
// still refreshes the observation time, so a later identical fetch does not
// age into "stale". No frame is sent unless the text changed or the glass is
// still showing a stale mark this confirmation clears. A change sends ambient
// only, not another state frame.
func (r *Runner) SetWeather(w protocol.Weather) {
	r.mu.Lock()
	contentChanged := !r.hasWeather || r.weather != w
	if contentChanged {
		r.weather = w
		r.hasWeather = true
	}
	r.weatherAt = time.Now()
	// The bit owed to the device is fresh. Send when the payload changed, the
	// device has not heard it, or it was previously told the report was stale.
	send := contentChanged || !r.ambientSent || r.lastAmbientStale
	if !send {
		r.mu.Unlock()
		return
	}
	r.ambientDirty = true
	r.mu.Unlock()
	r.poke()
}

// SetSoundConfig stores the host-selected notification sounds. The runner
// sends them on every connect and immediately after a change.
func (r *Runner) SetSoundConfig(cfg protocol.SoundConfig) {
	cfg.EnsureAttention()
	cfg.Sanitize()
	r.mu.Lock()
	r.sound = cfg
	r.hasSound = true
	r.status.SoundMaster = cfg.Master
	r.mu.Unlock()
	select {
	case r.soundWake <- struct{}{}:
	default:
	}
}

// SoundConfig returns the config the runner will send, including a mute
// reported by the device.
func (r *Runner) SoundConfig() protocol.SoundConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sound
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

// snapshotAlreadyStale reports whether the census was old at the moment it
// was accepted. Ageing in memory is not stale: the GUI only pushes when the
// list changes, and a later reconnect must not mark that list 数据陈旧.
func snapshotAlreadyStale(updatedAt int64, after time.Duration) bool {
	if updatedAt <= 0 || after <= 0 {
		return false
	}
	return time.Since(time.UnixMilli(updatedAt)) > after
}

func censusEqual(a, b protocol.Snapshot) bool {
	if a.Counts != b.Counts || len(a.Current) != len(b.Current) {
		return false
	}
	for i := range a.Current {
		if a.Current[i].ID != b.Current[i].ID || a.Current[i].Title != b.Current[i].Title ||
			a.Current[i].Progress != b.Current[i].Progress || a.Current[i].Phase != b.Current[i].Phase ||
			a.Current[i].Elapsed != b.Current[i].Elapsed {
			return false
		}
	}
	return attentionEqual(a.Attention, b.Attention)
}

func attentionEqual(a, b *protocol.Attention) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Kind == b.Kind && a.ID == b.ID && a.Title == b.Title
}

func (r *Runner) poke() {
	select {
	case r.dirty <- struct{}{}:
	default:
	}
}

func (r *Runner) takeDirty() (state, ambient, mood bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ambient, mood = r.stateDirty, r.ambientDirty, r.moodDirty
	r.stateDirty, r.ambientDirty, r.moodDirty = false, false, false
	return state, ambient, mood
}

func (r *Runner) drainDirty() {
	for {
		select {
		case <-r.dirty:
		default:
			r.mu.Lock()
			r.stateDirty = false
			r.ambientDirty = false
			r.moodDirty = false
			r.mu.Unlock()
			return
		}
	}
}

func (r *Runner) setError(err error) {
	r.mu.Lock()
	r.status.LastError = err.Error()
	r.status.Connected = false
	r.mu.Unlock()
	r.opts.Logf("[buddy] %v", err)
}

// setLive records the open port. It returns false once the context has been
// cancelled, and the caller must close the port itself.
func (r *Runner) setLive(c io.Closer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return false
	}
	r.live = c
	return true
}

// releaseLive closes the current port after a session ends. A reconnect can
// still open the next one.
func (r *Runner) releaseLive() {
	r.mu.Lock()
	c := r.live
	r.live = nil
	r.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// abandonLive closes the port because the context is done. A later setLive
// fails, so a dial that is still inside Open cannot keep the handle.
func (r *Runner) abandonLive() {
	r.mu.Lock()
	r.closing = true
	c := r.live
	r.live = nil
	r.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// Run keeps a connection alive until ctx is cancelled. Cancel closes the
// port immediately, including while a write is blocked inside the driver.
func (r *Runner) Run(ctx context.Context) {
	defer r.releaseLive()
	go func() {
		<-ctx.Done()
		r.abandonLive()
	}()
	backoff := time.Second
	for ctx.Err() == nil {
		conn, err := r.dial()
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			if errors.Is(err, ErrLinkDisabled) {
				return
			}
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
		if !r.setLive(conn) {
			_ = conn.Close()
			return
		}
		r.mu.Lock()
		r.status.Connected = true
		r.status.LastError = ""
		r.mu.Unlock()

		err = r.session(ctx, conn)
		r.releaseLive()
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

	r.mu.Lock()
	r.assetBin = false
	r.mu.Unlock()
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
	// Full resend after every (re)connect. A snapshot or weather update that
	// raced with the connect is included in the sends below.
	r.drainDirty()
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
	if err := r.sendPetMood(conn); err != nil {
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
			if state, ambient, mood := r.takeDirty(); state || ambient || mood {
				statePending = statePending || state
				ambientPending = ambientPending || ambient
				if mood {
					if err := r.sendPetMood(conn); err != nil {
						return err
					}
				}
			}
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
			if r.ambientStaleDue() {
				ambientPending = true
			}
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
	if snap.Attention != nil {
		copied := *snap.Attention
		snap.Attention = &copied
	}
	stale := r.snapStale
	r.seq++
	seq := r.seq
	hadPrev := r.status.LastSentCounts != nil
	var prevCounts protocol.Counts
	if hadPrev {
		prevCounts = *r.status.LastSentCounts
	}
	prevCurrent := append([]protocol.TaskItem(nil), r.status.LastSentCurrent...)
	r.mu.Unlock()

	sentAt := time.Now()
	msg := &protocol.Message{
		Type:      protocol.TypeState,
		Proto:     protocol.Version,
		Ts:        sentAt.Unix(),
		TsMS:      sentAt.UnixMilli(),
		Seq:       seq,
		Counts:    &snap.Counts,
		Current:   append([]protocol.TaskItem(nil), snap.Current...),
		Attention: snap.Attention,
		Stale:     stale,
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
		if t.ID != "" {
			if _, stillThere := curIDs[t.ID]; stillThere {
				continue
			}
			// Another in-progress task may share this title. The id is what left.
			return &protocol.Celebrate{Title: t.Title, Seconds: 5}
		}
		if _, stillThere := curTitles[t.Title]; stillThere {
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
	binary := r.assetBin
	r.status.PetReady = false
	r.mu.Unlock()

	r.drainAssetAcks()

	chunkBytes := 512
	if binary {
		chunkBytes = protocol.BPT1MaxData
	}
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
			if binary {
				raw, err := protocol.EncodeBPT1(protocol.BPT1Chunk{
					Frame: fi, Frames: len(frames),
					Width: protocol.AssetWidth, Height: protocol.AssetHeight,
					FrameMS: frameMS, Offset: off, Total: total, CRC32: crc,
					Data: frame[off:end],
				})
				if err != nil {
					r.opts.Logf("[buddy] pet frame rejected: %v", err)
					continue
				}
				if err := r.writeBytes(conn, raw); err != nil {
					return gen, err
				}
			} else {
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
			}
			sent++
			ready, ok := r.waitAssetAck(ctx, fi, end)
			if !ok {
				if ctx.Err() != nil {
					return gen, ctx.Err()
				}
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
	cfg.EnsureAttention()

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
	stale := time.Since(at) > r.opts.WeatherStaleAfter
	// Record the bit before the write. An identical SetWeather that arrives
	// while the frame is still in the kernel buffer must not queue a second
	// copy of the same report.
	r.ambientSent = true
	r.lastAmbientStale = stale
	r.mu.Unlock()

	msg := &protocol.Message{
		Type:    protocol.TypeAmbient,
		Proto:   protocol.Version,
		Ts:      time.Now().Unix(),
		Weather: &weather,
		Stale:   stale,
	}
	msg.Sanitize()
	if err := msg.Validate(); err != nil {
		r.mu.Lock()
		r.ambientSent = false
		r.mu.Unlock()
		r.opts.Logf("[buddy] weather rejected: %v", err)
		return nil
	}
	return r.write(conn, msg)
}

// ambientStaleDue is true when the device has not yet been told the current
// stale bit. Identical weather is not resent every half hour; one frame goes
// out when the report crosses WeatherStaleAfter.
func (r *Runner) ambientStaleDue() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWeather {
		return false
	}
	stale := time.Since(r.weatherAt) > r.opts.WeatherStaleAfter
	return !r.ambientSent || stale != r.lastAmbientStale
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
	return r.writeBytes(conn, frame)
}

func (r *Runner) writeBytes(conn io.ReadWriteCloser, frame []byte) error {
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
	var deviceSound *protocol.SoundConfig
	var deviceInput *protocol.Input
	r.mu.Lock()
	switch msg.Type {
	case protocol.TypeHello:
		r.status.DeviceFW = msg.FW
		r.status.DeviceBoard = msg.Board
		r.status.DeviceProto = msg.Proto
		r.status.DeviceTouch = msg.Touch
		r.status.DeviceKey = msg.Key
		r.status.DeviceSound = msg.SoundAvailable
		if msg.Display != nil {
			r.status.DeviceWidth = msg.Display.Width
			r.status.DeviceHeight = msg.Display.Height
			r.status.DeviceRound = msg.Display.Round
			r.status.DevicePanel = msg.Display.Panel
			r.status.DeviceFont = msg.Display.Font
		}
		r.assetBin = msg.AssetMode == protocol.AssetModeBPT1
		r.signalReady()
	case protocol.TypeStateAck:
		now := time.Now()
		r.status.LastAckAt = now
		if msg.TsMS > 0 {
			r.status.StateLatencyMS = now.UnixMilli() - msg.TsMS
		}
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
	case protocol.TypeSound:
		// The device already applied this. Updating the runner without
		// soundWake keeps the host from echoing it straight back.
		if msg.Sound != nil {
			cfg := *msg.Sound
			cfg.Sanitize()
			// An older firmware echo omits attention. Keep the host's choice
			// instead of treating the missing field as "off".
			if cfg.Attention == nil {
				if r.sound.Attention != nil {
					att := *r.sound.Attention
					cfg.Attention = &att
				} else {
					cfg.EnsureAttention()
				}
			}
			r.sound = cfg
			r.hasSound = true
			r.status.SoundMaster = cfg.Master
			deviceSound = &cfg
		}
	case protocol.TypeInput:
		if msg.Input != nil {
			in := *msg.Input
			deviceInput = &in
		}
	case protocol.TypeLog:
		line, _ := json.Marshal(struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}{msg.Level, msg.Msg})
		r.opts.Logf("[device] %s", line)
	}
	r.mu.Unlock()
	if deviceSound != nil && r.opts.OnDeviceSound != nil {
		r.opts.OnDeviceSound(*deviceSound)
	}
	if deviceInput != nil && r.opts.OnDeviceInput != nil {
		r.opts.OnDeviceInput(*deviceInput)
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
