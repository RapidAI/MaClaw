package websearch

import (
	"sort"
	"sync"
	"time"
)

const (
	// webSearchHealthWindowSize bounds the per-engine ring buffer of recent
	// attempts used for the settings-page health summary.
	webSearchHealthWindowSize = 20
	// webSearchHealthRecentWindow scopes circuit-breaker decisions to recent
	// history so an engine that recovered long ago cannot stay banned.
	webSearchHealthRecentWindow = 15 * time.Minute
	// webSearchCircuitMinAttempts avoids tripping the circuit on a single
	// unlucky request; a couple of samples are required first.
	webSearchCircuitMinAttempts = 3
)

// webSearchAttemptRecord augments SearchAttempt with the wall-clock time the
// attempt settled, so health windows can age entries out.
type webSearchAttemptRecord struct {
	at      time.Time
	attempt SearchAttempt
}

var (
	webSearchHealthMu      sync.Mutex
	webSearchHealthWindows = map[string][]webSearchAttemptRecord{}
)

// recordWebSearchAttempt appends one settled attempt to the engine ring
// buffer. Skipped attempts carry no signal about engine health and are
// ignored.
func recordWebSearchAttempt(attempt SearchAttempt) {
	recordWebSearchAttemptAt(attempt, time.Now())
}

func recordWebSearchAttemptAt(attempt SearchAttempt, at time.Time) {
	if attempt.EngineID == "" || attempt.Outcome == "" || attempt.Outcome == "skipped" {
		return
	}
	webSearchHealthMu.Lock()
	defer webSearchHealthMu.Unlock()
	window := append(webSearchHealthWindows[attempt.EngineID], webSearchAttemptRecord{at: at, attempt: attempt})
	if len(window) > webSearchHealthWindowSize {
		window = window[len(window)-webSearchHealthWindowSize:]
	}
	webSearchHealthWindows[attempt.EngineID] = window
}

// WebSearchEngineHealth is a read-only, short-term health snapshot for one
// engine. It contains no query text, credentials, or result content.
type WebSearchEngineHealth struct {
	EngineID    string  `json:"engine_id"`
	Attempts    int     `json:"attempts"`
	SuccessRate float64 `json:"success_rate"`
	P50MS       int64   `json:"p50_ms"`
	P95MS       int64   `json:"p95_ms"`
	// LastSuccessUnix is zero when the window holds no successful attempt.
	LastSuccessUnix int64 `json:"last_success_unix,omitempty"`
	CircuitOpen     bool  `json:"circuit_open"`
}

// GetWebSearchHealth returns health snapshots for engines that have recorded
// attempts, ordered by engine ID for a stable UI presentation. Engines
// without data are omitted so callers never display fabricated statistics.
func GetWebSearchHealth() []WebSearchEngineHealth {
	return webSearchHealthAt(time.Now())
}

func webSearchHealthAt(now time.Time) []WebSearchEngineHealth {
	webSearchHealthMu.Lock()
	defer webSearchHealthMu.Unlock()
	ids := make([]string, 0, len(webSearchHealthWindows))
	for id := range webSearchHealthWindows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]WebSearchEngineHealth, 0, len(ids))
	for _, id := range ids {
		out = append(out, summarizeEngineHealth(id, webSearchHealthWindows[id], now))
	}
	return out
}

func summarizeEngineHealth(engineID string, window []webSearchAttemptRecord, now time.Time) WebSearchEngineHealth {
	health := WebSearchEngineHealth{EngineID: engineID, Attempts: len(window)}
	if len(window) == 0 {
		return health
	}
	successes := 0
	durations := make([]int64, 0, len(window))
	var lastSuccess time.Time
	for _, rec := range window {
		durations = append(durations, rec.attempt.DurationMS)
		if rec.attempt.Outcome == "success" {
			successes++
			if rec.at.After(lastSuccess) {
				lastSuccess = rec.at
			}
		}
	}
	health.SuccessRate = float64(successes) / float64(len(window))
	health.P50MS = percentileNearestRankMS(durations, 50)
	health.P95MS = percentileNearestRankMS(durations, 95)
	if !lastSuccess.IsZero() {
		health.LastSuccessUnix = lastSuccess.Unix()
	}
	health.CircuitOpen = webSearchCircuitOpenWindow(window, now)
	return health
}

func percentileNearestRankMS(durations []int64, percentile int) int64 {
	if len(durations) == 0 {
		return 0
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	rank := (percentile*len(durations) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return durations[rank-1]
}

// webSearchCircuitOpenWindow reports whether recent history shows a dead
// engine: enough recent attempts and not a single success. Empty results
// count as failures because an engine that keeps returning nothing usable is
// exactly what the circuit breaker exists to bypass.
func webSearchCircuitOpenWindow(window []webSearchAttemptRecord, now time.Time) bool {
	cutoff := now.Add(-webSearchHealthRecentWindow)
	recent := 0
	for _, rec := range window {
		if rec.at.Before(cutoff) {
			continue
		}
		recent++
		if rec.attempt.Outcome == "success" {
			return false
		}
	}
	return recent >= webSearchCircuitMinAttempts
}

// webSearchCircuitOpen reports whether the smart scheduler should temporarily
// skip this engine.
func webSearchCircuitOpen(engineID string) bool {
	webSearchHealthMu.Lock()
	defer webSearchHealthMu.Unlock()
	return webSearchCircuitOpenWindow(webSearchHealthWindows[engineID], time.Now())
}

// resetWebSearchHealthForTest clears all recorded health data.
func resetWebSearchHealthForTest() {
	webSearchHealthMu.Lock()
	webSearchHealthWindows = map[string][]webSearchAttemptRecord{}
	webSearchHealthMu.Unlock()
}
