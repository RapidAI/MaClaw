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
	Enabled       bool   `json:"enabled"`
	Port          string `json:"port,omitempty"`
	Connected     bool   `json:"connected"`
	DeviceFW      string `json:"device_fw,omitempty"`
	DeviceBoard   string `json:"device_board,omitempty"`
	DeviceTouch   bool   `json:"device_touch"`
	DeviceSound   bool   `json:"device_sound"`
	Page          string `json:"page,omitempty"`
	LastStateAt   string `json:"last_state_at,omitempty"`
	LastAckAt     string `json:"last_ack_at,omitempty"`
	MissingGlyphs int    `json:"missing_glyphs,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	SnapshotAgeS  int    `json:"snapshot_age_s,omitempty"`

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
	cfg := buddyConfig{Enabled: true, Sound: &sound} // plug-and-play by default
	raw, err := os.ReadFile(a.buddyConfigPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("[buddy] config parse failed: %v", err)
		sound := protocol.DefaultSoundConfig()
		return buddyConfig{Enabled: true, Sound: &sound}
	}
	if cfg.Sound == nil {
		cfg.Sound = &sound
	}
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
	return os.WriteFile(a.buddyConfigPath(), raw, 0o644)
}

// buddyLogPath is the on-disk trace of the serial link and pet refreshes.
func buddyLogPath() string {
	return filepath.Join(os.TempDir(), "maclaw-buddy.log")
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
func (a *App) startBuddyService() {
	a.buddyMu.Lock()
	defer a.buddyMu.Unlock()
	if a.buddyCancel != nil {
		return
	}
	cfg := a.loadBuddyConfig()
	a.buddyCfg = cfg
	if !cfg.Enabled {
		return
	}

	opts := service.DefaultOptions()
	opts.HostVersion = "8.1.0"
	// A windowsgui build has no console, so mirror the link log to a file.
	// It is the only way to check an asset transfer without stealing COM3.
	_ = os.Remove(buddyLogPath())
	opts.Logf = func(format string, args ...any) {
		logBuddyf(format, args...)
	}

	var runner *service.Runner
	dial := func() (io.ReadWriteCloser, error) {
		port, name, err := serialport.Dialer(cfg.Port)
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
	// Wait for the runner to close the port before a restart reopens it. The
	// cap keeps shutdown bounded if a dial is stuck in the OS.
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("[buddy] runner did not stop within 2s")
		}
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
	status.DeviceSound = s.DeviceSound
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
	cfg := a.loadBuddyConfig()
	cfg.Enabled = true
	cfg.Port = strings.TrimSpace(port)
	a.buddyMu.Lock()
	a.buddyCfg = cfg
	a.buddyMu.Unlock()
	if err := a.saveBuddyConfig(cfg); err != nil {
		return fmt.Errorf("save buddy config: %w", err)
	}
	a.stopBuddyService()
	a.startBuddyService()
	return nil
}

// BuddyStop disables the service and releases the serial port so esptool can
// flash the device.
func (a *App) BuddyStop() error {
	cfg := a.loadBuddyConfig()
	cfg.Enabled = false
	a.buddyMu.Lock()
	a.buddyCfg = cfg
	a.buddyMu.Unlock()
	if err := a.saveBuddyConfig(cfg); err != nil {
		return fmt.Errorf("save buddy config: %w", err)
	}
	a.stopBuddyService()
	return nil
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
	cfg.Sanitize()
	a.buddyMu.Lock()
	a.buddyCfg.Sound = &cfg
	full := a.buddyCfg
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if err := a.saveBuddyConfig(full); err != nil {
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
