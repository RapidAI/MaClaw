package browser

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TabLedger remembers which browser tabs a run opened. Closing a run
// returns only those tabs. A bot that is over its cap loses its own
// least-recently-used tab, never another bot's.
type TabLedger struct {
	mu   sync.Mutex
	tabs map[string]*tabNote
	cap  int
	now  func() time.Time
}

type tabNote struct {
	id   string
	bot  string
	run  string
	used time.Time
}

func NewTabLedger(cap int) *TabLedger {
	if cap < 1 {
		cap = DefaultBotTabCap()
	}
	return &TabLedger{tabs: map[string]*tabNote{}, cap: cap, now: time.Now}
}

// DefaultBotTabCap is how many tabs one bot may keep open. MACLAW_BOT_TAB_CAP
// overrides it. 0 from the environment means the default.
func DefaultBotTabCap() int {
	const fallback = 6
	raw := strings.TrimSpace(os.Getenv("MACLAW_BOT_TAB_CAP"))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

var runTabs = NewTabLedger(0)

// NoteRunTab records a tab this run opened and returns that bot's
// least-recently-used other tabs that are over the cap.
func NoteRunTab(botID, runID, targetID string) []string {
	return runTabs.Opened(botID, runID, targetID)
}

// TouchRunTab marks a tab this run is using again.
func TouchRunTab(targetID string) {
	runTabs.Touch(targetID)
}

// EndRunTabs forgets the tabs one run opened and returns their ids.
// Tabs another run opened are left in the ledger.
func EndRunTabs(runID string) []string {
	return runTabs.EndRun(runID)
}

func (l *TabLedger) Opened(botID, runID, targetID string) []string {
	botID, runID, targetID = strings.TrimSpace(botID), strings.TrimSpace(runID), strings.TrimSpace(targetID)
	if l == nil || targetID == "" || (botID == "" && runID == "") {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tabs == nil {
		l.tabs = map[string]*tabNote{}
	}
	now := l.now()
	if cur := l.tabs[targetID]; cur != nil {
		cur.used = now
		if botID != "" {
			cur.bot = botID
		}
		if runID != "" {
			cur.run = runID
		}
	} else {
		l.tabs[targetID] = &tabNote{id: targetID, bot: botID, run: runID, used: now}
	}
	return l.overCapLocked(botID, targetID)
}

func (l *TabLedger) Touch(targetID string) {
	if l == nil {
		return
	}
	targetID = strings.TrimSpace(targetID)
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur := l.tabs[targetID]; cur != nil {
		cur.used = l.now()
	}
}

func (l *TabLedger) EndRun(runID string) []string {
	runID = strings.TrimSpace(runID)
	if l == nil || runID == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []string
	for id, note := range l.tabs {
		if note != nil && note.run == runID {
			ids = append(ids, id)
			delete(l.tabs, id)
		}
	}
	return ids
}

func (l *TabLedger) overCapLocked(botID, keep string) []string {
	if botID == "" || l.cap < 1 {
		return nil
	}
	var mine []*tabNote
	for _, note := range l.tabs {
		if note != nil && note.bot == botID {
			mine = append(mine, note)
		}
	}
	var drop []string
	for len(mine) > l.cap {
		oldest := 0
		for i := 1; i < len(mine); i++ {
			if mine[i].used.Before(mine[oldest].used) || (mine[i].used.Equal(mine[oldest].used) && mine[i].id < mine[oldest].id) {
				oldest = i
			}
		}
		if mine[oldest].id == keep && len(mine) > 1 {
			// The tab just opened is not the one to drop.
			next := 0
			if oldest == 0 {
				next = 1
			}
			for i := 0; i < len(mine); i++ {
				if i == oldest {
					continue
				}
				if mine[i].used.Before(mine[next].used) || (mine[i].used.Equal(mine[next].used) && mine[i].id < mine[next].id) {
					next = i
				}
			}
			oldest = next
		}
		drop = append(drop, mine[oldest].id)
		delete(l.tabs, mine[oldest].id)
		mine = append(mine[:oldest], mine[oldest+1:]...)
	}
	return drop
}
