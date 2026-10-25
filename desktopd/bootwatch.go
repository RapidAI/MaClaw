package desktopd

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// Desktop containers come back from a docker restart, a dockerd restart or a
// host reboot through their restart policy, which runs only pid 1. Containers
// created before pid 1 ran boot (and any whose boot failed) would then sit
// with no screen until somebody opened them. The boot watcher closes that gap:
// it lists the running desktop containers every interval and runs the
// supervisor's boot once for each container start it has not seen yet. The
// first pass after desktopd starts covers every running desktop. boot is
// idempotent and takes the desktop lock, so racing pid 1's own boot or a
// restart-computer ensure is harmless.

type bootWatcher struct {
	mu   sync.Mutex
	seen map[string]string // container name -> StartedAt already booted
}

// StartBootWatcher runs the watcher until ctx ends.
func (s *Service) StartBootWatcher(ctx context.Context, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = 20 * time.Second
	}
	w := &bootWatcher{seen: map[string]string{}}
	go func() {
		for {
			w.once(ctx, s)
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
}

// BootRestoreOnce runs one watcher pass with fresh state (every running
// desktop counts as new). Tests and manual reconciles use it.
func (s *Service) BootRestoreOnce(ctx context.Context) []string {
	w := &bootWatcher{seen: map[string]string{}}
	return w.once(ctx, s)
}

func (w *bootWatcher) once(ctx context.Context, s *Service) []string {
	out, err := s.docker(ctx, "ps", "--filter", "name=^maclaw-desktop-", "--format", "{{.Names}}")
	if err != nil {
		log.Printf("[desktopd] boot watch: docker ps: %v", err)
		return nil
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); strings.HasPrefix(name, "maclaw-desktop-") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"inspect", "-f", "{{.Name}} {{.State.StartedAt}}"}, names...)
	out, err = s.docker(ctx, args...)
	if err != nil {
		// A container that vanished between ps and inspect fails the batch;
		// the next pass sees the rest.
		log.Printf("[desktopd] boot watch: docker inspect: %v", err)
		return nil
	}
	var booted []string
	alive := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name, started := strings.TrimPrefix(fields[0], "/"), fields[1]
		alive[name] = true
		w.mu.Lock()
		done := w.seen[name] == started
		w.mu.Unlock()
		if done {
			continue
		}
		bctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		res, err := s.docker(bctx, "exec", name, "python3", "/desktop_supervisor.py", "boot")
		cancel()
		// Once per start either way: an old image without boot would fail
		// every pass, and opening the desktop still runs ensure.
		w.mu.Lock()
		w.seen[name] = started
		w.mu.Unlock()
		if err != nil {
			log.Printf("[desktopd] boot watch: %s boot failed: %v", name, err)
			continue
		}
		booted = append(booted, name)
		log.Printf("[desktopd] boot watch: %s started %s, boot: %s", name, started, lastLine(res))
	}
	w.mu.Lock()
	for name := range w.seen {
		if !alive[name] {
			delete(w.seen, name)
		}
	}
	w.mu.Unlock()
	return booted
}

func lastLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = text[i+1:]
	}
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}
