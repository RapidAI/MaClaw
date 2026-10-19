package desktopd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	// Stage2Idle is the stage-2 sleep. The historical DESKTOPD_MAX_IDLE reaper
	// is a different switch and is not shortened to this.
	Stage2Idle = 5 * time.Minute
	// WarmStartBudget is how long a tool waits for a stopped container to
	// accept docker start. Past this the caller gets a retryable error.
	WarmStartBudget = 25 * time.Second
	// desktopRestartPolicy stays unless-stopped. docker stop is compatible
	// with it; restart always would bring the container back and break sleep.
	desktopRestartPolicy = "unless-stopped"
)

// WarmStartError is the retryable tool error for a warm start that is still
// unfinished after WarmStartBudget. It is not a sentence for the person.
type WarmStartError struct{}

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
	return s.flightErr(ensureFlightKey(spec), ctx, func(ctx context.Context) error {
		return s.withUser(spec.TenantID, spec.UserID, func() error {
			return s.runtime().Ensure(ctx, spec)
		})
	})
}

func (s *Service) ensureUnlocked(ctx context.Context, spec Spec) error {
	started := s.now()
	name := containerName(spec.TenantID, spec.UserID)
	out, err := s.docker(ctx, "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		if missingObject(err) || missingObject(errors.New(out)) {
			return s.warmBudget(started)
		}
		return err
	}
	if strings.TrimSpace(out) != "true" {
		if _, startErr := s.docker(ctx, "start", name); startErr != nil {
			return startErr
		}
	}
	s.markComputerRunning(spec)
	return s.warmBudget(started)
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
