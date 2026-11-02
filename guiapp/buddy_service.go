package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
	"github.com/RapidAI/CodeClaw/buddy/serialport"
	"github.com/RapidAI/CodeClaw/buddy/service"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// buddyConfig is persisted as data/buddy.json.
type buddyConfig struct {
	Enabled bool                  `json:"enabled"`
	Port    string                `json:"port,omitempty"`
	Sound   *protocol.SoundConfig `json:"sound,omitempty"`
}

// BuddyStatus is what the settings panel renders. The diagnostic fields are
// also used by the end-to-end verification harness through the Wails bridge.
type BuddyStatus struct {
	Enabled        bool   `json:"enabled"`
	Port           string `json:"port,omitempty"`
	Connected      bool   `json:"connected"`
	DeviceFW       string `json:"device_fw,omitempty"`
	DeviceBoard    string `json:"device_board,omitempty"`
	DeviceTouch    bool   `json:"device_touch"`
	DeviceKey      bool   `json:"device_key"`
	DeviceSound    bool   `json:"device_sound"`
	DeviceWidth    int    `json:"device_width,omitempty"`
	DeviceHeight   int    `json:"device_height,omitempty"`
	DeviceRound    bool   `json:"device_round,omitempty"`
	DevicePanel    string `json:"device_panel,omitempty"`
	DeviceFont     string `json:"device_font,omitempty"`
	SoundMaster    bool   `json:"sound_master"`
	StateLatencyMS int64  `json:"state_latency_ms,omitempty"`
	Page           string `json:"page,omitempty"`
	LastStateAt    string `json:"last_state_at,omitempty"`
	LastAckAt      string `json:"last_ack_at,omitempty"`
	MissingGlyphs  int    `json:"missing_glyphs,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	SnapshotAgeS   int    `json:"snapshot_age_s,omitempty"`

	LastPushAt          string              `json:"last_push_at,omitempty"`
	LastSentAt          string              `json:"last_sent_at,omitempty"`
	LastSentSeq         uint64              `json:"last_sent_seq,omitempty"`
	Counts              *protocol.Counts    `json:"counts,omitempty"`
	Current             []protocol.TaskItem `json:"current,omitempty"`
	DeviceMissingGlyphs int                 `json:"device_missing_glyphs,omitempty"`
	LatencyMS           int64               `json:"latency_ms,omitempty"`
}

func (a *App) buddyConfigPath() string {
	return filepath.Join(a.GetDataDir(), "buddy.json")
}

func (a *App) loadBuddyConfig() buddyConfig {
	sound := protocol.DefaultSoundConfig()
	// A missing file is a first launch: plug and play. A present file that
	// cannot be read must not default to on, or a torn write would occupy the
	// hardware port with the checkbox off.
	fallbackOff := func() buddyConfig {
		sound := protocol.DefaultSoundConfig()
		return buddyConfig{Enabled: false, Sound: &sound}
	}
	raw, err := os.ReadFile(a.buddyConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return buddyConfig{Enabled: true, Sound: &sound}
		}
		log.Printf("[buddy] config read failed: %v", err)
		return fallbackOff()
	}
	cfg := buddyConfig{Enabled: true, Sound: &sound}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("[buddy] config parse failed: %v", err)
		return fallbackOff()
	}
	if cfg.Sound == nil {
		cfg.Sound = &sound
	}
	cfg.Sound.EnsureAttention()
	cfg.Sound.Sanitize()
	return cfg
}

func (a *App) saveBuddyConfig(cfg buddyConfig) error {
	if cfg.Sound == nil {
		sound := protocol.DefaultSoundConfig()
		cfg.Sound = &sound
	}
	cfg.Sound.Sanitize()
	if err := os.MkdirAll(a.GetDataDir(), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(a.buddyConfigPath(), raw)
}

// replaceFile writes the full contents aside, then swaps them into place so a
// crash cannot leave buddy.json half-written.
func replaceFile(path string, raw []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := renameReplacing(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// buddyLogPath is the on-disk trace of the serial link and pet refreshes.
func buddyLogPath() string {
	return filepath.Join(os.TempDir(), "maclaw-buddy.log")
}

// focusFromBuddyInput raises MaClaw and tells the task list about a tap.
// Pending confirmation wins over the task currently drawn on the glass.
func (a *App) focusFromBuddyInput(in protocol.Input) {
	if in.Gesture != "tap" && in.Gesture != "confirm" {
		return
	}
	payload := map[string]any{"gesture": in.Gesture, "page": in.Page}
	if in.Index != nil {
		payload["index"] = *in.Index
	}
	a.emitEvent("buddy:input", payload)
	if !a.hasWailsEventsContext() {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// logBuddyf mirrors a buddy message to the console and the diagnostic file.
func logBuddyf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Printf("[buddy] %s", line)
	if f, err := os.OpenFile(buddyLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), line)
		_ = f.Close()
	}
}

// startBuddyService starts the in-process serial runner if enabled.
// The config file is read under buddyCfgWrite, then that lock is released
// before the runner exists. Stop can persist enabled=false and cancel the
// new runner instead of waiting out the whole startup.
func (a *App) startBuddyService() {
	a.buddyCfgWrite.Lock()
	cfg := a.loadBuddyConfig()
	a.buddyMu.Lock()
	if a.buddyCancel != nil {
		a.buddyMu.Unlock()
		a.buddyCfgWrite.Unlock()
		return
	}
	a.buddyCfg = cfg
	if !cfg.Enabled {
		a.buddyMu.Unlock()
		a.buddyCfgWrite.Unlock()
		return
	}
	a.buddyCfgWrite.Unlock()
	defer a.buddyMu.Unlock()

	opts := service.DefaultOptions()
	opts.HostVersion = "8.1.0"
	// A windowsgui build has no console, so mirror the link log to a file.
	// It is the only way to check an asset transfer without stealing COM3.
	_ = os.Remove(buddyLogPath())
	opts.Logf = func(format string, args ...any) {
		logBuddyf(format, args...)
	}
	opts.OnDeviceSound = func(cfg protocol.SoundConfig) {
		// Detach from the runner goroutine so a report that arrives while
		// startBuddyService still holds buddyMu cannot deadlock.
		go func() {
			if err := a.persistBuddySound(cfg); err != nil {
				logBuddyf("save device sound: %v", err)
			}
		}()
	}
	opts.OnDeviceInput = func(in protocol.Input) {
		go a.focusFromBuddyInput(in)
	}

	var runner *service.Runner
	dial := func() (io.ReadWriteCloser, error) {
		a.buddyMu.Lock()
		enabled := a.buddyCfg.Enabled
		portName := a.buddyCfg.Port
		a.buddyMu.Unlock()
		if !enabled {
			return nil, service.ErrLinkDisabled
		}
		port, name, err := serialport.Dialer(portName)
		if err != nil {
			return nil, err
		}
		if runner != nil {
			runner.SetPort(name())
		}
		return port, nil
	}
	runner = service.NewRunner(dial, opts)
	a.buddyRunner = runner
	if cfg.Sound != nil {
		runner.SetSoundConfig(*cfg.Sound)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.buddyCancel = cancel
	a.buddyDone = done
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()

	// Re-apply the last snapshot the frontend pushed so a restart does not
	// leave the device blank until the next task change. Treat that replay as
	// a fresh push so latency diagnostics do not measure from an old timestamp.
	if strings.TrimSpace(a.buddyLastSnapshot) != "" {
		var snap protocol.Snapshot
		if err := json.Unmarshal([]byte(a.buddyLastSnapshot), &snap); err == nil {
			runner.SetSnapshot(snap)
			a.buddyLastPushAt = time.Now()
		}
	}
	// Resolve weather once in the background; the ambient loop will refresh it
	// later, and a device reconnect must not wait 45 minutes.
	go a.pushBuddyWeatherFromAmbient()
	// Render the GUI pet into device frames in the background; the transfer
	// starts as soon as the link is up.
	go a.pushBuddyPetAsset(runner)
}

func (a *App) stopBuddyService() {
	// Restart reopens the port, so wait until the runner releases it.
	a.stopBuddyServiceWithin(2 * time.Second)
}

// stopBuddyServiceWithin cancels the runner and waits up to limit for the
// port to close. limit <= 0 returns as soon as cancel is signaled.
func (a *App) stopBuddyServiceWithin(limit time.Duration) {
	a.buddyMu.Lock()
	cancel := a.buddyCancel
	done := a.buddyDone
	a.buddyCancel = nil
	a.buddyDone = nil
	a.buddyRunner = nil
	a.buddyMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil || limit <= 0 {
		return
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Printf("[buddy] runner did not stop within %s", limit)
	}
}

// pushBuddyWeatherFromAmbient reuses the GUI weather pipeline when available.
func (a *App) pushBuddyWeatherFromAmbient() {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	weather, err := a.resolveDeviceAmbientWeather(ctx)
	if err != nil {
		log.Printf("[buddy] weather resolve failed: %v", err)
		return
	}
	a.setBuddyWeather(weather.Summary, weather.Location, weather.TemperatureC)
}

func (a *App) setBuddyWeather(summary, location string, temperatureC int) {
	a.buddyMu.Lock()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner == nil {
		return
	}
	runner.SetWeather(protocol.Weather{
		Summary:      summary,
		TemperatureC: temperatureC,
		Location:     location,
	})
}

// BuddyListPorts returns serial ports with buddy candidates flagged.
func (a *App) BuddyListPorts() ([]serialport.Info, error) {
	return serialport.List()
}

// BuddyGetStatus reports the current link state for the settings panel.
func (a *App) BuddyGetStatus() BuddyStatus {
	a.buddyMu.Lock()
	cfg := a.buddyCfg
	runner := a.buddyRunner
	pushAt := a.buddyLastPushAt
	snapRaw := a.buddyLastSnapshot
	a.buddyMu.Unlock()

	status := BuddyStatus{Enabled: cfg.Enabled, Port: cfg.Port}
	if !pushAt.IsZero() {
		status.LastPushAt = pushAt.Format(time.RFC3339Nano)
	}
	if runner == nil {
		return status
	}
	s := runner.Status()
	status.Connected = s.Connected
	status.Port = firstNonEmpty(s.Port, status.Port)
	status.DeviceFW = s.DeviceFW
	status.DeviceBoard = s.DeviceBoard
	status.DeviceTouch = s.DeviceTouch
	status.DeviceKey = s.DeviceKey
	status.DeviceSound = s.DeviceSound
	status.DeviceWidth = s.DeviceWidth
	status.DeviceHeight = s.DeviceHeight
	status.DeviceRound = s.DeviceRound
	status.DevicePanel = s.DevicePanel
	status.DeviceFont = s.DeviceFont
	status.SoundMaster = s.SoundMaster
	status.StateLatencyMS = s.StateLatencyMS
	status.Page = s.Page
	status.MissingGlyphs = s.MissingGlyphs
	if s.DeviceMissingGlyphs > status.MissingGlyphs {
		// The heartbeat carries the cumulative renderer count; the ack may be
		// older. Surface the larger of the two so the settings warning is real.
		status.MissingGlyphs = s.DeviceMissingGlyphs
	}
	status.LastError = s.LastError
	status.DeviceMissingGlyphs = s.DeviceMissingGlyphs
	status.LastSentSeq = s.LastSentSeq
	if s.LastSentCounts != nil {
		counts := *s.LastSentCounts
		status.Counts = &counts
	}
	if len(s.LastSentCurrent) > 0 {
		status.Current = append([]protocol.TaskItem(nil), s.LastSentCurrent...)
	}
	if !s.LastStateAt.IsZero() {
		status.LastStateAt = s.LastStateAt.Format(time.RFC3339)
	}
	if !s.LastSentAt.IsZero() {
		status.LastSentAt = s.LastSentAt.Format(time.RFC3339Nano)
	}
	if !s.LastAckAt.IsZero() {
		status.LastAckAt = s.LastAckAt.Format(time.RFC3339Nano)
	}
	if !pushAt.IsZero() && !s.LastAckAt.IsZero() && !s.LastAckAt.Before(pushAt) {
		status.LatencyMS = s.LastAckAt.Sub(pushAt).Milliseconds()
	}
	if strings.TrimSpace(snapRaw) != "" {
		var snap protocol.Snapshot
		if err := json.Unmarshal([]byte(snapRaw), &snap); err == nil && snap.UpdatedAt > 0 {
			age := time.Since(time.UnixMilli(snap.UpdatedAt))
			if age > 0 {
				status.SnapshotAgeS = int(age.Seconds())
			}
		}
	}
	return status
}

// BuddyStart enables the service (optionally pinned to a port).
func (a *App) BuddyStart(port string) error {
	a.buddyCfgWrite.Lock()
	cfg := a.loadBuddyConfig()
	cfg.Enabled = true
	cfg.Port = strings.TrimSpace(port)
	a.buddyMu.Lock()
	if sound := cloneBuddySound(a.buddyCfg.Sound); sound != nil {
		cfg.Sound = sound
	}
	a.buddyMu.Unlock()
	err := a.publishBuddyConfig(cfg)
	a.buddyCfgWrite.Unlock()
	if err != nil {
		return fmt.Errorf("save buddy config: %w", err)
	}
	a.stopBuddyService()
	a.startBuddyService()
	return nil
}

// BuddyStop disables the service and releases the serial port so esptool can
// flash the device. The config write finishes before the runner is cancelled,
// and it shares buddyCfgWrite with sound saves so a late report cannot turn
// the link back on.
func (a *App) BuddyStop() error {
	a.buddyCfgWrite.Lock()
	cfg := a.loadBuddyConfig()
	cfg.Enabled = false
	a.buddyMu.Lock()
	if sound := cloneBuddySound(a.buddyCfg.Sound); sound != nil {
		cfg.Sound = sound
	}
	a.buddyMu.Unlock()
	err := a.publishBuddyConfig(cfg)
	a.buddyCfgWrite.Unlock()
	if err != nil {
		return fmt.Errorf("save buddy config: %w", err)
	}
	a.stopBuddyService()
	return nil
}

// cloneBuddySound copies the attention event too. A shallow copy would share
// that pointer, so a later sanitize could change the runner's live config.
func cloneBuddySound(src *protocol.SoundConfig) *protocol.SoundConfig {
	if src == nil {
		return nil
	}
	sound := *src
	if src.Attention != nil {
		att := *src.Attention
		sound.Attention = &att
	}
	return &sound
}

// publishBuddyConfig writes buddy.json and only then updates memory. The
// caller holds buddyCfgWrite. A failed write leaves the switch unchanged, so
// the checkbox cannot show off while the runner still holds the port.
func (a *App) publishBuddyConfig(cfg buddyConfig) error {
	if err := a.saveBuddyConfig(cfg); err != nil {
		return err
	}
	a.buddyMu.Lock()
	a.buddyCfg = cfg
	a.buddyMu.Unlock()
	return nil
}

// persistBuddySound stores a device-reported sound config. Enabled is copied
// under buddyCfgWrite, so a report that started while the link was on cannot
// write enabled=true after BuddyStop.
func (a *App) persistBuddySound(sound protocol.SoundConfig) error {
	sound.EnsureAttention()
	sound.Sanitize()
	a.buddyCfgWrite.Lock()
	defer a.buddyCfgWrite.Unlock()
	a.buddyMu.Lock()
	cfg := a.buddyCfg
	cfg.Sound = &sound
	a.buddyMu.Unlock()
	return a.publishBuddyConfig(cfg)
}

// BuddyGetSoundConfig returns the persisted notification sound settings.
func (a *App) BuddyGetSoundConfig() protocol.SoundConfig {
	a.buddyMu.Lock()
	defer a.buddyMu.Unlock()
	if a.buddyCfg.Sound != nil {
		cfg := *a.buddyCfg.Sound
		return cfg
	}
	return protocol.DefaultSoundConfig()
}

// BuddySetSoundConfig persists the sound settings and pushes them to a live
// device. A nil runner (link disabled) only saves them for the next connect.
func (a *App) BuddySetSoundConfig(cfg protocol.SoundConfig) error {
	cfg.EnsureAttention()
	cfg.Sanitize()
	a.buddyCfgWrite.Lock()
	a.buddyMu.Lock()
	full := a.buddyCfg
	full.Sound = &cfg
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	err := a.publishBuddyConfig(full)
	a.buddyCfgWrite.Unlock()
	if err != nil {
		return fmt.Errorf("save buddy config: %w", err)
	}
	if runner != nil {
		runner.SetSoundConfig(cfg)
	}
	return nil
}

// BuddyPushSnapshot accepts the census the frontend computed with the same
// helpers the sidebar uses. Keeping the calculation in the frontend is what
// guarantees zero drift between the sidebar and the device.
// BuddyPushPet sets the glass pet from the assistant turn: busy while a
// turn is running, idle when it ends. Anything else is ignored.
func (a *App) BuddyPushPet(state string) error {
	a.buddyMu.Lock()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner != nil {
		runner.SetPetMood(state)
	}
	return nil
}

func (a *App) BuddyPushSnapshot(payload string) error {
	var snap protocol.Snapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return fmt.Errorf("buddy snapshot: %w", err)
	}
	if snap.UpdatedAt == 0 {
		snap.UpdatedAt = time.Now().UnixMilli()
	}
	a.buddyMu.Lock()
	a.buddyLastSnapshot = payload
	a.buddyLastPushAt = time.Now()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner != nil {
		runner.SetSnapshot(snap)
	}
	return nil
}

// BuddyPushWeather lets the settings panel or a manual refresh update the
// device without waiting for the ambient loop.
func (a *App) BuddyPushWeather(summary, location string, temperatureC int) error {
	a.setBuddyWeather(strings.TrimSpace(summary), strings.TrimSpace(location), temperatureC)
	return nil
}

// BuddyRefreshWeather resolves weather now and pushes it to the device.
func (a *App) BuddyRefreshWeather() (string, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	weather, err := a.resolveDeviceAmbientWeather(ctx)
	if err != nil {
		return "", err
	}
	a.setBuddyWeather(weather.Summary, weather.Location, weather.TemperatureC)
	return weather.Location, nil
}
