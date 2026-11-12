package desktopd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	// Stage2Idle is the stage-2 sleep. The historical DESKTOPD_MAX_IDLE reaper
	// is a different switch and is not shortened to this.
	Stage2Idle = 5 * time.Minute
	// DesktopIdleDefault is the historical reaper timeout when operators want
	// "sleep after use". 24h is too long for that promise; 20m matches the
	// expected UX (in-progress desktop step still blocks sleep; mere viewing does not).
	DesktopIdleDefault = 20 * time.Minute
	// WorkspaceIdleDefault keeps the headless bash/file workspace around
	// longer than the GUI desktop. A short desktop sleep must not cull a
	// workspace that still has packages or files in play.
	WorkspaceIdleDefault = 24 * time.Hour
	// WarmStartBudget is how long a tool waits for a stopped container to
	// accept docker start. Past this the caller gets a retryable error.
	WarmStartBudget = 25 * time.Second
	// desktopRestartPolicy stays unless-stopped. docker stop is compatible
	// with it; restart always would bring the container back and break sleep.
	desktopRestartPolicy = "unless-stopped"
)

// ReaperInterval picks how often the idle reconciler runs. A short desktop
// idle needs a short tick, otherwise a 20m timeout effectively becomes 30m.
func ReaperInterval(maxIdle time.Duration) time.Duration {
	if maxIdle <= 0 {
		return 10 * time.Minute
	}
	if maxIdle <= 30*time.Minute {
		return time.Minute
	}
	return 10 * time.Minute
}

// ParseMaxIdle reads DESKTOPD_MAX_IDLE. Empty disables the historical reaper.
func ParseMaxIdle(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("DESKTOPD_MAX_IDLE is not a duration. Set it to 20m, or leave it empty to disable reaping, then restart desktopd")
	}
	if d < 0 {
		return 0, fmt.Errorf("DESKTOPD_MAX_IDLE must be >= 0")
	}
	return d, nil
}

// ParseWorkspaceMaxIdle reads DESKTOPD_WORKSPACE_MAX_IDLE. Empty keeps the
// long workspace default so shortening the desktop idle does not cull it.
func ParseWorkspaceMaxIdle(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return WorkspaceIdleDefault, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("DESKTOPD_WORKSPACE_MAX_IDLE is not a duration. Set it to 24h, or leave it empty for the default, then restart desktopd")
	}
	if d < 0 {
		return 0, fmt.Errorf("DESKTOPD_WORKSPACE_MAX_IDLE must be >= 0")
	}
	return d, nil
}

// WarmStartError is the retryable tool error for a warm start that is still
// unfinished after WarmStartBudget. It is not a sentence for the person.
type WarmStartError struct{}

// Allowed wake reasons for starting a stopped desktop. Anything else is
// refused: health checks, listings, bash/file, background reconnects, and
// leftover Ensure calls must not boot the GUI container.
const (
	WakeReasonBotDesktop = "bot-desktop"
	WakeReasonHumanWake  = "human-wake"
)

// ErrWakeDenied is a start without bot-desktop or human-wake. The container
// stays stopped. Callers show 桌面在休眠 and do not retry as a warm start.
var ErrWakeDenied = errors.New("桌面在休眠。只有 bot 的桌面步骤，或人点「唤醒」，才会开机。")

// AllowedWakeReason reports a start that Daniel's policy allows.
func AllowedWakeReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case WakeReasonBotDesktop, WakeReasonHumanWake:
		return true
	default:
		return false
	}
}

// requireWakeToStart fails closed when a call would docker start or create a
// desktop without an allowed reason. An already-running desktop does not call
// this. The reason is logged so a leak can be found in the journal.
func (s *Service) requireWakeToStart(spec Spec, action string) error {
	if s == nil || !s.EnforceWakeReason {
		return nil
	}
	if AllowedWakeReason(spec.WakeReason) {
		log.Printf("[desktopd] wake reason=%s action=%s tenant=%s user=%s", strings.TrimSpace(spec.WakeReason), action, spec.TenantID, spec.UserID)
		return nil
	}
	log.Printf("[desktopd] wake denied action=%s reason=%q tenant=%s user=%s", action, strings.TrimSpace(spec.WakeReason), spec.TenantID, spec.UserID)
	return ErrWakeDenied
}

func (WarmStartError) Error() string    { return "桌面正在唤醒，再试一次。" }
func (WarmStartError) Retryable() bool  { return true }
func (WarmStartError) UserBubble() bool { return false }

// SleepMeta is the object-storage record for one sleep. It has no token.
// The bucket, account, and region are chosen by the syncer, not here.
type SleepMeta struct {
	Tenant     string   `json:"tenant"`
	UserKey    string   `json:"user_key"`
	Generation string   `json:"generation,omitempty"`
	Runtime    string   `json:"runtime"`
	Volumes    []string `json:"volumes"`
}

// VolumeSyncResult is one generation that all four volumes reached.
type VolumeSyncResult struct {
	Generation string
	Volumes    []string
}

var (
	errSleepStoreUnconfigured = errors.New("object storage is not configured")
	// errSleepAbandoned is an Ensure that arrived after flush. The machine
	// stays running. This is not a sync failure and it does not stop.
	errSleepAbandoned = errors.New("ensure arrived after flush")
	sleepVolumeNames  = []string{"desktops", "home", "opt", "usr-local"}
)

// RestartPolicy is the policy stage 2 pins on every new container.
func RestartPolicy() string { return desktopRestartPolicy }

// RejectRestartAlways fails closed when a container would restart itself
// after an explicit docker stop.
func RejectRestartAlways(policy string) error {
	if strings.EqualFold(strings.TrimSpace(policy), "always") {
		return errors.New("restart policy always breaks sleep; keep unless-stopped")
	}
	return nil
}

// ParseStage2Sleep reads DESKTOPD_SLEEP. Empty and the off words stay off,
// which leaves containers running. Anything else is a configuration error.
func ParseStage2Sleep(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "off", "no":
		return false, nil
	case "1", "true", "on", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("DESKTOPD_SLEEP=%q is not on or off", strings.TrimSpace(raw))
	}
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// holdWork marks apt or bash in progress. The reaper must not stop that user.
// The returned function drops the mark.
func (s *Service) holdWork(tenantID, userID string) func() {
	if s == nil {
		return func() {}
	}
	key := tenantID + "\x00" + userID
	v, _ := s.activity.LoadOrStore(key, new(int64))
	n := v.(*int64)
	atomic.AddInt64(n, 1)
	return func() { atomic.AddInt64(n, -1) }
}

func (s *Service) userBusy(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	v, ok := s.activity.Load(tenantID + "\x00" + userID)
	if !ok {
		return false
	}
	return atomic.LoadInt64(v.(*int64)) > 0
}

func (s *Service) noteWake(tenantID, userID string) {
	if s == nil {
		return
	}
	v, _ := s.wakes.LoadOrStore(tenantID+"\x00"+userID, new(int64))
	atomic.AddInt64(v.(*int64), 1)
}

func (s *Service) clearWake(tenantID, userID string) {
	if s == nil {
		return
	}
	v, ok := s.wakes.Load(tenantID + "\x00" + userID)
	if !ok {
		return
	}
	atomic.AddInt64(v.(*int64), -1)
}

func (s *Service) wakeWanted(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	v, ok := s.wakes.Load(tenantID + "\x00" + userID)
	if !ok {
		return false
	}
	return atomic.LoadInt64(v.(*int64)) > 0
}

// Ensure starts a stopped desktop so the next file, bash, screenshot, or
// session can use it. A missing container is left for session create.
// docker start does not remove volumes or change the image. A warm start
// still unfinished after 25 seconds returns WarmStartError.
func (s *Service) Ensure(ctx context.Context, spec Spec) error {
	spec, err := normalize(spec)
	if err != nil {
		return err
	}
	// Announce before the user lock. Snapshot and Restore already run under
	// that lock, and sleep holds it for the whole stop. An Ensure that waited
	// to announce would be seen only after the machine had stopped. Waiters
	// announce too, and they do not start the container again.
	s.noteWake(spec.TenantID, spec.UserID)
	defer s.clearWake(spec.TenantID, spec.UserID)
	if done, err := s.ensureWithoutExclusiveGate(ctx, spec); done {
		if err != nil {
			return err
		}
		// Each caller opens its own bot screen. The shared ensure flight
		// only starts the container, and it must not hold the user lock
		// while a screen comes up.
		return s.openNamedBotScreen(ctx, spec)
	}
	if err := s.flightErr(ensureFlightKey(spec), ctx, func(ctx context.Context) error {
		return s.withUser(spec.TenantID, spec.UserID, func() error {
			return s.runtime().Ensure(ctx, spec)
		})
	}); err != nil {
		return err
	}
	return s.openNamedBotScreen(ctx, spec)
}

func (s *Service) ensureUnlocked(ctx context.Context, spec Spec) error {
	started := s.now()
	name := containerName(spec.TenantID, spec.UserID)
	current, err := s.inspect(ctx, name)
	if err != nil {
		return err
	}
	if !current.Exists {
		// Missing stays missing so a file or bash call does not create a desktop.
		return s.warmBudget(started)
	}
	// Package mounts (and other recreateReason checks) must apply on Ensure too:
	// a plain docker start would keep an old container without shared apt roots.
	if reason := s.recreateReason(current, spec); reason != "" {
		if err := s.requireWakeToStart(spec, "ensure-recreate"); err != nil {
			return err
		}
		if _, err := s.createUnlocked(ctx, spec); err != nil {
			return err
		}
		return s.warmBudget(started)
	}
	if !current.Running {
		if err := s.requireWakeToStart(spec, "ensure-start"); err != nil {
			return err
		}
		if _, startErr := s.docker(ctx, "start", name); startErr != nil {
			return startErr
		}
	}
	s.markComputerRunning(spec)
	return s.warmBudget(started)
}

// openNamedBotScreen opens the bot named on spec after the container lock is
// released. An owner ensure has no bot and does not open anyone's screen.
// A missing or still-stopped container is left alone: session create owns that.
func (s *Service) openNamedBotScreen(ctx context.Context, spec Spec) error {
	if strings.TrimSpace(spec.BotID) == "" {
		return nil
	}
	name := containerName(spec.TenantID, spec.UserID)
	running, err := s.containerRunning(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	return s.ensureBotScreen(ctx, spec)
}

func (s *Service) warmBudget(started time.Time) error {
	if s.now().Sub(started) > WarmStartBudget {
		return WarmStartError{}
	}
	return nil
}

// ReapStage2 runs one stage-2 pass. The switch off does nothing, so containers
// keep running. Tests call this instead of sleeping five minutes.
func (s *Service) ReapStage2(ctx context.Context) {
	if s == nil || !s.Stage2Sleep {
		return
	}
	(&reaper{maxIdle: Stage2Idle, stage2: true}).once(ctx, s)
}

// prepareSleep flushes the browser, then syncs the four private volumes to
// one generation. An error means the container stays running.
func (s *Service) prepareSleep(ctx context.Context, name, tenantID, userID string) error {
	if err := s.flushForSleep(ctx, name, tenantID, userID); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	if s.wakeWanted(tenantID, userID) {
		s.markComputerRunning(Spec{TenantID: tenantID, UserID: userID})
		return errSleepAbandoned
	}
	if err := s.syncFourVolumes(ctx, tenantID, userID); err != nil {
		return err
	}
	if s.wakeWanted(tenantID, userID) {
		s.markComputerRunning(Spec{TenantID: tenantID, UserID: userID})
		return errSleepAbandoned
	}
	return nil
}

func (s *Service) flushForSleep(ctx context.Context, name, tenantID, userID string) error {
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return err
	}
	_, err = s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "flush", key)
	return err
}

func (s *Service) syncFourVolumes(ctx context.Context, tenantID, userID string) error {
	if s == nil || s.VolumeSync == nil {
		return errSleepStoreUnconfigured
	}
	mounts, err := desktop.PrivateMounts(tenantID, userID)
	if err != nil {
		return err
	}
	if err := mountsAreTheFour(mounts); err != nil {
		return err
	}
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return err
	}
	meta := SleepMeta{
		Tenant:  tenantID,
		UserKey: key,
		Runtime: "docker",
		Volumes: append([]string(nil), sleepVolumeNames...),
	}
	result, err := s.VolumeSync(ctx, meta, mounts)
	if err != nil {
		return err
	}
	return oneGeneration(result)
}

func mountsAreTheFour(mounts []desktop.Mount) error {
	if len(mounts) != len(sleepVolumeNames) {
		return fmt.Errorf("desktop has %d volumes", len(mounts))
	}
	got := map[string]bool{}
	for _, mount := range mounts {
		name, ok := sleepVolumeName(mount.Target)
		if !ok {
			return fmt.Errorf("volume target %s is not one of the four", mount.Target)
		}
		got[name] = true
	}
	for _, name := range sleepVolumeNames {
		if !got[name] {
			return fmt.Errorf("volume %s is missing", name)
		}
	}
	return nil
}

func oneGeneration(result VolumeSyncResult) error {
	if strings.TrimSpace(result.Generation) == "" {
		return errors.New("volume sync returned an empty generation")
	}
	if len(result.Volumes) != len(sleepVolumeNames) {
		return fmt.Errorf("volume sync covered %d volumes", len(result.Volumes))
	}
	got := map[string]bool{}
	for _, name := range result.Volumes {
		if !knownSleepVolume(name) {
			return fmt.Errorf("volume sync unknown volume %s", name)
		}
		got[name] = true
	}
	if len(got) != len(sleepVolumeNames) {
		return errors.New("volume sync did not put all four volumes on one generation")
	}
	return nil
}

func sleepVolumeName(target string) (string, bool) {
	switch target {
	case "/desktops":
		return "desktops", true
	case "/home/desktop":
		return "home", true
	case "/opt":
		return "opt", true
	case "/usr/local":
		return "usr-local", true
	default:
		return "", false
	}
}

func knownSleepVolume(name string) bool {
	for _, item := range sleepVolumeNames {
		if item == name {
			return true
		}
	}
	return false
}

// ensureWithoutExclusiveGate is Ensure's shared-gate fast path. Hub calls
// Ensure before every screenshot and session open; on a running container
// that needs no recreate (or a missing one, which Ensure never creates)
// there is nothing to do, and taking the exclusive gate for that only queued
// this call (and, behind it, every other bot of the container) behind
// whatever bash or install was running. The check runs under the read side,
// so a concurrent sleep or recreate (which hold the write side) is either
// finished or not started yet when it looks. done=false: take the slow path.
func (s *Service) ensureWithoutExclusiveGate(ctx context.Context, spec Spec) (done bool, err error) {
	if !s.dockerRuntime() {
		return false, nil
	}
	started := s.now()
	_ = s.withUserRead(spec.TenantID, spec.UserID, func() error {
		current, inspectErr := s.inspect(ctx, containerName(spec.TenantID, spec.UserID))
		switch {
		case inspectErr != nil:
			return nil
		case !current.Exists:
			// Missing stays missing so a file or bash call does not create a desktop.
			done = true
		case current.Running && s.recreateReason(current, spec) == "":
			s.markComputerRunning(spec)
			done = true
		}
		return nil
	})
	if done {
		return true, s.warmBudget(started)
	}
	return false, nil
}
