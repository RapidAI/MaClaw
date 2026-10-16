package desktopd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// reaper stops desktops idle for longer than maxIdle. It runs on the Docker
// host itself, so it also catches containers Hub can no longer name: a user
// assignment moved to another service, a Hub restart that lost bookkeeping,
// or a MaClawSrv crash before its stop call. The supervisor's stop flushes
// the website login and signals the session inside the container; it does not
// stop the container. After that call the reaper checks the container and,
// when it is still running, falls back to docker stop (the same grace period
// as Service.Stop). A container left up because an isolated bot display is
// busy is not a failure.
type reaper struct {
	interval time.Duration
	maxIdle  time.Duration
}

// StartReaper launches the background idle reconciler. Call with maxIdle <= 0
// to keep the previous behaviour of never reaping.
func (s *Service) StartReaper(ctx context.Context, interval, maxIdle time.Duration) {
	if s == nil || maxIdle <= 0 {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	r := &reaper{interval: interval, maxIdle: maxIdle}
	go r.loop(ctx, s)
}

func (r *reaper) loop(ctx context.Context, s *Service) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.once(ctx, s)
		}
	}
}

type idleStatus struct {
	Running     bool `json:"running"`
	IdleSeconds int  `json:"idle_seconds"`
	Known       bool `json:"known"`
}

// once stops every running desktop whose supervisor reports it idle beyond
// the limit. Unknown or unreadable states are left alone.
func (r *reaper) once(ctx context.Context, s *Service) {
	out, err := s.docker(ctx, "ps", "--filter", "label=maclaw.tenant", "--format", "{{.Names}}\\t{{.Label \"maclaw.tenant\"}}\\t{{.Label \"maclaw.user\"}}")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 {
			continue
		}
		container, tenantID, userID := fields[0], fields[1], fields[2]
		if container == "" || tenantID == "" || userID == "" {
			continue
		}
		key, err := desktop.UserKey(tenantID, userID)
		if err != nil {
			continue
		}
		statusOut, err := s.docker(ctx, "exec", container, "python3", "/desktop_supervisor.py", "idle", key)
		if err != nil {
			continue
		}
		var status idleStatus
		if line := statusLine(statusOut); line != "" {
			err = json.Unmarshal([]byte(line), &status)
		}
		if err != nil || !status.Known || !status.Running {
			continue
		}
		if time.Duration(status.IdleSeconds)*time.Second < r.maxIdle {
			continue
		}
		log.Printf("[desktopd] reaping idle desktop container=%s tenant=%s user=%s idle=%ds", container, tenantID, userID, status.IdleSeconds)
		if err := s.withUser(tenantID, userID, func() error {
			return r.stopOne(ctx, s, container, tenantID, userID, key)
		}); err != nil {
			log.Printf("[desktopd] reaper stop failed container=%s tenant=%s user=%s: %v", container, tenantID, userID, err)
		}
	}
}

// stopOne stops one idle desktop and checks that it actually went down.
// The supervisor stop error is logged here and does not by itself abort the
// docker stop fallback: a failed exec used to be discarded, and the container
// stayed up with only the "reaping" line in the log.
func (r *reaper) stopOne(ctx context.Context, s *Service, container, tenantID, userID, key string) error {
	stopCtx := context.WithoutCancel(ctx)
	_, stopErr := s.docker(stopCtx, "exec", container, "python3", "/desktop_supervisor.py", "stop", key)
	if stopErr != nil {
		log.Printf("[desktopd] reaper supervisor stop failed container=%s tenant=%s user=%s: %v", container, tenantID, userID, stopErr)
	}
	running, err := s.containerRunning(stopCtx, container)
	if err != nil {
		log.Printf("[desktopd] reaper could not verify container=%s tenant=%s user=%s: %v", container, tenantID, userID, err)
		running = true
	}
	if !running {
		log.Printf("[desktopd] reaped idle desktop container=%s tenant=%s user=%s via=container-already-stopped", container, tenantID, userID)
		return nil
	}
	if s.containerHasBusyBot(stopCtx, container) {
		sessionUp, sessionErr := s.desktopSessionRunning(stopCtx, container, key)
		if sessionErr != nil {
			return fmt.Errorf("bot display is busy and desktop state is unknown: %w", sessionErr)
		}
		if sessionUp {
			return fmt.Errorf("desktop still running after supervisor stop; container left up because a bot display is busy")
		}
		log.Printf("[desktopd] idle desktop container=%s tenant=%s user=%s session stopped; container left running because a bot display is busy", container, tenantID, userID)
		return nil
	}
	log.Printf("[desktopd] reaper desktop still up after supervisor stop container=%s tenant=%s user=%s; docker stop", container, tenantID, userID)
	if err := s.stopContainer(stopCtx, container); err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	running, err = s.containerRunning(stopCtx, container)
	if err != nil {
		return fmt.Errorf("verify after docker stop: %w", err)
	}
	if running {
		return fmt.Errorf("container %s still running after docker stop", container)
	}
	log.Printf("[desktopd] reaped idle desktop container=%s tenant=%s user=%s via=docker-stop", container, tenantID, userID)
	return nil
}

// containerRunning reports whether the named container is running. A missing
// container counts as down.
func (s *Service) containerRunning(ctx context.Context, name string) (bool, error) {
	out, err := s.docker(ctx, "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		if missingObject(err) {
			return false, nil
		}
		return false, err
	}
	switch strings.TrimSpace(out) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected docker state %q", strings.TrimSpace(out))
	}
}

// containerHasBusyBot reports whether an isolated bot display in this
// container is in use. docker stop would end that bot's task, so the reaper
// leaves the container up. A supervisor that has no bots-busy command, or an
// empty answer, is treated as not busy: that is the historical single-desktop
// container.
func (s *Service) containerHasBusyBot(ctx context.Context, name string) bool {
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "bots-busy")
	if err != nil {
		return false
	}
	line := statusLine(out)
	if line == "" {
		return false
	}
	var status struct {
		Busy []string `json:"busy"`
	}
	if json.Unmarshal([]byte(line), &status) != nil {
		return false
	}
	return len(status.Busy) > 0
}

// desktopSessionRunning re-reads the supervisor idle status. An unreadable
// answer is reported as an error so the caller does not treat silence as down.
func (s *Service) desktopSessionRunning(ctx context.Context, container, key string) (bool, error) {
	out, err := s.docker(ctx, "exec", container, "python3", "/desktop_supervisor.py", "idle", key)
	if err != nil {
		return true, err
	}
	line := statusLine(out)
	if line == "" {
		return true, fmt.Errorf("idle status unreadable")
	}
	var status idleStatus
	if err := json.Unmarshal([]byte(line), &status); err != nil {
		return true, err
	}
	return status.Running, nil
}

// statusLine picks the JSON line out of combined docker exec output.
func statusLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
