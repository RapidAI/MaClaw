package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// ErrNoDesktop is a restart or reset for a user who has no container yet.
var ErrNoDesktop = errors.New("desktop does not exist")

// ErrDesktopBusy is a restart or reset while a bot of this user is working
// on (or a person has taken over) a screen. Force overrides it.
var ErrDesktopBusy = errors.New("desktop is busy")

// ComputerResult is what Restart and Reset report back to Hub.
type ComputerResult struct {
	Desktop
	Action string `json:"action"`
	// Backup is the image the old container layer was committed to by a
	// reset (operator recovery only). Empty for restart.
	Backup string `json:"backup,omitempty"`
}

// Restart is "docker restart" for the user's whole computer: every screen's
// browser writes its website login back through the supervisor's own flush
// path, the container stops gracefully (pid 1 flushes the owner again),
// starts, and the supervisor and owner browser are brought back. Volumes and
// the container layer are untouched.
func (s *Service) Restart(ctx context.Context, spec Spec, force bool) (ComputerResult, error) {
	return s.computerOp(ctx, spec, force, "restart")
}

// Reset returns the computer to factory state from the requested image.
// Kept: /desktops (browser profiles and website logins) and /home/desktop.
// Not kept: every self-installed piece of software and system setting — the
// container layer (committed to this user's :prev image as a backup, :state
// dropped) and the /opt and /usr/local volumes (copied to "<volume>-prev"
// backups, then recreated empty so docker fills them from the image).
// Backups keep one generation and are only recovered by an operator.
func (s *Service) Reset(ctx context.Context, spec Spec, force bool) (ComputerResult, error) {
	return s.computerOp(ctx, spec, force, "reset")
}

func (s *Service) computerOp(ctx context.Context, spec Spec, force bool, action string) (ComputerResult, error) {
	spec, err := normalize(spec)
	if err != nil {
		return ComputerResult{}, err
	}
	if spec.BotID != "" {
		return ComputerResult{}, fmt.Errorf("%w: %s applies to the owner's computer, not a bot screen", ErrInvalid, action)
	}
	if s.Machines != nil {
		return ComputerResult{}, fmt.Errorf("%w: %s is only supported on the docker runtime", ErrInvalid, action)
	}
	var out ComputerResult
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var opErr error
		if action == "reset" {
			out, opErr = s.resetUnlocked(ctx, spec, force)
		} else {
			out, opErr = s.restartUnlocked(ctx, spec, force)
		}
		return opErr
	})
	if err == nil {
		log.Printf("desktopd: %s %s done (force=%v)", action, out.Container, force)
	}
	return out, err
}

// busyBots asks the supervisor which bot screens are working or taken over.
// A stopped container or an older supervisor reports none.
func (s *Service) busyBots(ctx context.Context, name string) []string {
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "bots-busy")
	if err != nil {
		return nil
	}
	var status struct {
		Busy []string `json:"busy"`
	}
	if line := statusLine(out); line == "" || json.Unmarshal([]byte(line), &status) != nil {
		return nil
	}
	return status.Busy
}

// flushAllScreens writes every screen's website login back using the
// supervisor's own flush (which also does the screen -> master write-back).
// These are the bot screens; quiesce flushes the owner after them.
func (s *Service) flushAllScreens(ctx context.Context, name string, spec Spec) {
	if meta, err := s.ReadComputerMeta(spec.TenantID, spec.UserID); err == nil {
		for _, row := range meta.Screens {
			if row.Agent == "" || !desktop.ValidBotID(row.Agent) {
				continue
			}
			key, err := desktop.BotKey(spec.TenantID, spec.UserID, row.Agent)
			if err != nil {
				continue
			}
			// Each screen gets its own write budget, so many bot screens
			// cannot eat the owner's.
			botCtx, cancel := loginWriteContext(ctx)
			_, _ = s.docker(botCtx, "exec", name, "python3", "/desktop_supervisor.py", "flush", key)
			cancel()
		}
	}
}

// quiesce checks busy bots, writes logins back and stops the container.
func (s *Service) quiesce(ctx context.Context, name string, spec Spec, current containerState, force bool) error {
	if !current.Running {
		return nil
	}
	if busy := s.busyBots(ctx, name); len(busy) > 0 && !force {
		return fmt.Errorf("%w: %d bot screen(s) are working", ErrDesktopBusy, len(busy))
	}
	desktopGates.forget(spec.TenantID, spec.UserID)
	s.flushAllScreens(ctx, name, spec)
	// The owner last, so the master profile ends with the owner's write;
	// then docker stop (pid 1 flushes the owner once more).
	writeCtx, cancel := loginWriteContext(ctx)
	defer cancel()
	s.flushBrowser(writeCtx, name, spec.TenantID, spec.UserID)
	return s.stopContainer(writeCtx, name)
}

func (s *Service) restartUnlocked(ctx context.Context, spec Spec, force bool) (ComputerResult, error) {
	name := containerName(spec.TenantID, spec.UserID)
	current, err := s.inspect(ctx, name)
	if err != nil {
		return ComputerResult{}, err
	}
	if !current.Exists {
		return ComputerResult{}, ErrNoDesktop
	}
	if err := s.quiesce(ctx, name, spec, current, force); err != nil {
		return ComputerResult{}, err
	}
	if key, err := desktop.UserKey(spec.TenantID, spec.UserID); err == nil {
		s.forgetPublished(key)
	}
	if _, err := s.docker(ctx, "start", name); err != nil {
		return ComputerResult{}, err
	}
	// Restore reuses the container (or recreates it for the usual reasons)
	// and starts the supervisor and the owner's browser again.
	if _, err := s.runtime().Restore(ctx, spec); err != nil {
		return ComputerResult{}, err
	}
	running := spec
	running.Image = containerImage(current)
	return ComputerResult{Desktop: desktopOf(running, name, "running"), Action: "restart"}, nil
}

func (s *Service) resetUnlocked(ctx context.Context, spec Spec, force bool) (ComputerResult, error) {
	name := containerName(spec.TenantID, spec.UserID)
	current, err := s.inspect(ctx, name)
	if err != nil {
		return ComputerResult{}, err
	}
	// The image must exist before anything is torn down.
	if err := s.installImage(ctx, spec.Image); err != nil {
		return ComputerResult{}, err
	}
	state, err := desktop.StateImage(spec.TenantID, spec.UserID)
	if err != nil {
		return ComputerResult{}, err
	}
	prev := strings.TrimSuffix(state, ":state") + ":prev"
	backup := ""
	if current.Exists {
		if err := s.quiesce(ctx, name, spec, current, force); err != nil {
			return ComputerResult{}, err
		}
		// Keep the old layer as a one-generation backup an operator can
		// recover packages from. It is never run automatically.
		if _, err := s.docker(ctx, "commit", "--change", "LABEL "+desktop.ImageLabel+"="+strconv.Quote(containerImage(current)), name, prev); err != nil {
			return ComputerResult{}, err
		}
		backup = prev
		if !privateMountsReady(current.Mounts, spec) {
			if err := s.migrateLiveFiles(ctx, name, spec); err != nil {
				return ComputerResult{}, err
			}
		}
		if _, err := s.docker(ctx, "rm", "-f", name); err != nil {
			return ComputerResult{}, err
		}
	}
	// Software volumes go back to factory content: the old ones are copied
	// to one-generation "-prev" backups and removed, so docker fills the
	// new empty volumes from the image on the next run.
	if err := s.resetSoftwareVolumes(ctx, spec); err != nil {
		return ComputerResult{}, err
	}
	// Without :state the new container starts from the clean requested image.
	if _, err := s.docker(ctx, "rmi", state); err != nil && !missingObject(err) {
		log.Printf("desktopd: reset: untag %s: %v", state, err)
	}
	if key, err := desktop.UserKey(spec.TenantID, spec.UserID); err == nil {
		s.forgetPublished(key)
	}
	if _, err := s.runtime().Restore(ctx, spec); err != nil {
		return ComputerResult{}, err
	}
	return ComputerResult{Desktop: desktopOf(spec, name, "running"), Action: "reset", Backup: backup}, nil
}

// resetVolumeTargets are the private volumes a reset returns to factory
// content. /desktops (browser profiles and logins) and /home/desktop are kept.
var resetVolumeTargets = map[string]bool{"/opt": true, "/usr/local": true}

// ResetBackupSuffix names the one-generation backup of a reset volume.
const ResetBackupSuffix = "-prev"

// resetSoftwareVolumes backs up and removes /opt and /usr/local. The backup
// copy runs in a throwaway container of the requested image (it has sh and
// cp); cp -a keeps owners and modes. A missing volume is skipped.
func (s *Service) resetSoftwareVolumes(ctx context.Context, spec Spec) error {
	mounts, err := desktop.PrivateMounts(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if !resetVolumeTargets[mount.Target] {
			continue
		}
		if _, err := s.docker(ctx, "volume", "inspect", mount.Volume); err != nil {
			if missingObject(err) {
				continue
			}
			return err
		}
		backup := mount.Volume + ResetBackupSuffix
		if _, err := s.docker(ctx, "volume", "rm", "-f", backup); err != nil && !missingObject(err) {
			return err
		}
		if _, err := s.docker(ctx, "volume", "create", backup); err != nil {
			return err
		}
		if _, err := s.docker(ctx, "run", "--rm", "--network", "none", "--entrypoint", "sh",
			"--volume", mount.Volume+":/from:ro", "--volume", backup+":/to",
			spec.Image, "-c", "cp -a /from/. /to/"); err != nil {
			return fmt.Errorf("%w: back up %s: %v", ErrDocker, mount.Volume, err)
		}
		if _, err := s.docker(ctx, "volume", "rm", mount.Volume); err != nil {
			return err
		}
		log.Printf("desktopd: reset: %s backed up to %s and removed", mount.Volume, backup)
	}
	return nil
}
