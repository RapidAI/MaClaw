package desktopd

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// reaper stops desktops idle for longer than maxIdle. It runs on the Docker
// host itself, so it also catches containers Hub can no longer name: a user
// assignment moved to another service, a Hub restart that lost bookkeeping,
// or a MaClawSrv crash before its stop call. The supervisor's stop flushes
// the website login first, so a reaped desktop logs back in with its profile.
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
		s.withUser(tenantID, userID, func() error {
			_, err := s.docker(context.WithoutCancel(ctx), "exec", container, "python3", "/desktop_supervisor.py", "stop", key)
			return err
		})
	}
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
