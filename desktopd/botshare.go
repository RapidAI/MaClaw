package desktopd

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One owner's container holds the owner's screen and every isolated bot's
// screen: a browser and a live-view encoder each. Sized for one screen, three
// working bots shared 1.5 CPUs (papers 11-11 21:56: 4052 of 5918 periods
// throttled, screenshots 12-34s, window_list 24s). The container's CPU share
// therefore grows with the bots that opened a screen recently.
//
// DESKTOPD_BOT_CPUS_EACH is the CPU added per bot beyond the first (default
// 0.75, 0 turns it off). The total never passes the host's CPUs minus
// DESKTOPD_HOST_CPU_RESERVE (default 0.5). DESKTOPD_BOT_ACTIVE_MINUTES is how
// long a bot counts after its last screen open (default 30).

type botShare struct {
	mu   sync.Mutex
	seen map[string]map[string]time.Time // owner -> bot -> last open
}

func envFloat(name string, def float64) float64 {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 {
			return v
		}
	}
	return def
}

// note records a bot opening its screen and returns how many of this owner's
// bots count as active now.
func (b *botShare) note(owner, bot string, now time.Time) int {
	window := time.Duration(envFloat("DESKTOPD_BOT_ACTIVE_MINUTES", 30) * float64(time.Minute))
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[string]map[string]time.Time{}
	}
	bots := b.seen[owner]
	if bots == nil {
		bots = map[string]time.Time{}
		b.seen[owner] = bots
	}
	if bot != "" {
		bots[bot] = now
	}
	n := 0
	for id, at := range bots {
		if now.Sub(at) > window {
			delete(bots, id)
			continue
		}
		n++
	}
	return n
}

// sharedCPUs is the container CPU limit for base with n active bots.
func sharedCPUs(base string, n int, hostCPUs int) string {
	each := envFloat("DESKTOPD_BOT_CPUS_EACH", 0.75)
	value, err := strconv.ParseFloat(strings.TrimSpace(base), 64)
	if err != nil || value <= 0 || each <= 0 || n <= 1 {
		return base
	}
	want := value + each*float64(n-1)
	limit := float64(hostCPUs) - envFloat("DESKTOPD_HOST_CPU_RESERVE", 0.5)
	if want > limit {
		want = limit
	}
	if want <= value {
		return base
	}
	return strconv.FormatFloat(want, 'f', 2, 64)
}

// shareSpec sizes spec's CPUs for the owner's active bots.
func (s *Service) shareSpec(spec Spec) Spec {
	n := s.botShare.note(ownerID(spec), spec.BotID, time.Now())
	spec.CPUs = sharedCPUs(spec.CPUs, n, runtime.NumCPU())
	return spec
}
