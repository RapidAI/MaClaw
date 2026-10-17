// Package dbmaint decides when Hub Center may maintain its SQLite file.
//
// History deletes free pages but do not shrink a database that was created
// with auto_vacuum=NONE. The scheduler asks for a small incremental_vacuum
// after each prune batch once the file is INCREMENTAL, and it allows one
// full VACUUM to convert a legacy file. That conversion takes an exclusive
// lock and rewrites the file, so it runs only inside a low-traffic window,
// only after this node's stagger offset, only when free disk covers the
// file plus headroom, and only when replication is caught up.
package dbmaint

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

const (
	// AutoVacuumNone is PRAGMA auto_vacuum = 0.
	AutoVacuumNone = 0
	// AutoVacuumFull is PRAGMA auto_vacuum = 1.
	AutoVacuumFull = 1
	// AutoVacuumIncremental is PRAGMA auto_vacuum = 2.
	AutoVacuumIncremental = 2

	// DefaultWindowStart and DefaultWindowEnd are local clock times.
	// The end is exclusive.
	DefaultWindowStart = "03:00"
	DefaultWindowEnd   = "05:00"
	// DefaultFreeHeadroomBytes is the extra free space required on top of
	// the current database file size. VACUUM rewrites the file beside itself.
	DefaultFreeHeadroomBytes int64 = 512 << 20
	// DefaultIncrementalVacuumPages is how many freelist pages one prune
	// batch may hand back to the OS. 32 pages is 128KiB at the default
	// 4KiB page size, short enough that the writer lock is not held long.
	DefaultIncrementalVacuumPages = 32
	// MaxIncrementalVacuumPages is a hard cap. A larger config value is
	// clamped so a typo cannot stall writers for a long incremental_vacuum.
	MaxIncrementalVacuumPages = 128
	// DefaultMetricsInterval is how often the process logs file and history size.
	DefaultMetricsInterval = 30 * time.Minute
	// DefaultWarnPendingOps warns when ha_sync_ops grows well past the
	// retained window (default 4000). Production froze a host around 57000.
	DefaultWarnPendingOps int64 = 20000
	// DefaultWarnDBBytes warns at 1GiB, before the file reaches the
	// multi-gigabyte size that pinned the service cgroup.
	DefaultWarnDBBytes int64 = 1 << 30
	// DefaultConvertRetry is the quiet period after a conversion attempt
	// so a busy database is not VACUUMed again in a tight loop.
	DefaultConvertRetry = 15 * time.Minute
	// CompletionKey is the local_maintenance_state row that records a
	// finished conversion. It is not an HA system setting.
	CompletionKey = "auto_vacuum_incremental"
)

// Config is the maintenance policy. Zero values are filled by Normalize.
type Config struct {
	Enabled                bool
	IncrementalVacuumPages int
	WindowStart            string
	WindowEnd              string
	FreeHeadroomBytes      int64
	MetricsInterval        time.Duration
	WarnPendingOps         int64
	WarnDBBytes            int64
	ConvertRetry           time.Duration
}

// Normalize fills defaults, clamps the incremental page count, and rejects
// a window that is not HH:MM. A negative warn threshold disables that warning.
// Enabled is left as the caller set it.
func Normalize(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.WindowStart) == "" {
		cfg.WindowStart = DefaultWindowStart
	}
	if strings.TrimSpace(cfg.WindowEnd) == "" {
		cfg.WindowEnd = DefaultWindowEnd
	}
	if _, err := ParseWindow(cfg.WindowStart, cfg.WindowEnd); err != nil {
		return Config{}, err
	}
	cfg.IncrementalVacuumPages = ClampPages(cfg.IncrementalVacuumPages)
	if cfg.FreeHeadroomBytes == 0 {
		cfg.FreeHeadroomBytes = DefaultFreeHeadroomBytes
	}
	if cfg.FreeHeadroomBytes < 0 {
		return Config{}, fmt.Errorf("free headroom must be >= 0")
	}
	if cfg.MetricsInterval == 0 {
		cfg.MetricsInterval = DefaultMetricsInterval
	}
	if cfg.MetricsInterval < 0 {
		return Config{}, fmt.Errorf("metrics interval must be >= 0")
	}
	if cfg.WarnPendingOps == 0 {
		cfg.WarnPendingOps = DefaultWarnPendingOps
	}
	if cfg.WarnDBBytes == 0 {
		cfg.WarnDBBytes = DefaultWarnDBBytes
	}
	if cfg.ConvertRetry <= 0 {
		cfg.ConvertRetry = DefaultConvertRetry
	}
	return cfg, nil
}

// ClampPages bounds a configured page count to the default or the hard cap.
// Zero and negative values use the default. The prune path passes 0 itself
// when maintenance is disabled; it does not call ClampPages in that case.
func ClampPages(n int) int {
	if n <= 0 {
		return DefaultIncrementalVacuumPages
	}
	if n > MaxIncrementalVacuumPages {
		return MaxIncrementalVacuumPages
	}
	return n
}

// PruneVacuumPages is the page count to pass to PRAGMA incremental_vacuum
// after a history delete batch. Disabled maintenance returns 0 so the prune
// does not touch the freelist.
func PruneVacuumPages(enabled bool, pages int) int {
	if !enabled {
		return 0
	}
	return ClampPages(pages)
}

// Window is a local-time range. EndMin is exclusive. A range that wraps
// midnight has StartMin > EndMin.
type Window struct {
	StartMin int
	EndMin   int
}

// Duration is the length of the window, including a midnight wrap.
func (w Window) Duration() time.Duration {
	mins := w.EndMin - w.StartMin
	if mins <= 0 {
		mins += 24 * 60
	}
	return time.Duration(mins) * time.Minute
}

// ParseWindow parses HH:MM bounds. The end is exclusive. Equal bounds are
// rejected so a typo cannot silently disable conversion.
func ParseWindow(start, end string) (Window, error) {
	sh, sm, err := parseClock(start)
	if err != nil {
		return Window{}, fmt.Errorf("window start: %w", err)
	}
	eh, em, err := parseClock(end)
	if err != nil {
		return Window{}, fmt.Errorf("window end: %w", err)
	}
	startMin := sh*60 + sm
	endMin := eh*60 + em
	if startMin == endMin {
		return Window{}, fmt.Errorf("window %s-%s is empty", strings.TrimSpace(start), strings.TrimSpace(end))
	}
	return Window{StartMin: startMin, EndMin: endMin}, nil
}

func parseClock(value string) (hour, minute int, err error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("invalid clock %q (want HH:MM)", value)
	}
	if _, err = fmt.Sscanf(parts[0], "%d", &hour); err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid clock %q (want HH:MM)", value)
	}
	if _, err = fmt.Sscanf(parts[1], "%d", &minute); err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid clock %q (want HH:MM)", value)
	}
	// Reject trailing junk such as "03:00:00" or "3am".
	if strings.TrimLeft(parts[0], "0123456789") != "" || strings.TrimLeft(parts[1], "0123456789") != "" {
		return 0, 0, fmt.Errorf("invalid clock %q (want HH:MM)", value)
	}
	if len(parts[0]) > 2 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("invalid clock %q (want HH:MM)", value)
	}
	return hour, minute, nil
}

// Stagger is a stable offset inside the window derived from nodeID.
// Peers with different ids start at different times. The offset lands in
// the first three quarters of the window so the node still has time to run
// after it becomes eligible. An empty id is hashed as "single".
func Stagger(nodeID string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	span := window - window/4
	if span <= 0 {
		span = window
	}
	seconds := int64(span / time.Second)
	if seconds <= 0 {
		return 0
	}
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "single"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return time.Duration(h.Sum64()%uint64(seconds)) * time.Second
}

// InConvertSlot reports whether now is inside the window and at or after
// this node's stagger offset from the window start. now is interpreted in
// its own location, which is the process local time when the runner passes
// time.Now().
func InConvertSlot(now time.Time, w Window, stagger time.Duration) bool {
	if !inWindow(now, w) {
		return false
	}
	start := windowStart(now, w)
	return !now.Before(start.Add(stagger))
}

func inWindow(now time.Time, w Window) bool {
	mins := now.Hour()*60 + now.Minute()
	if w.StartMin < w.EndMin {
		return mins >= w.StartMin && mins < w.EndMin
	}
	return mins >= w.StartMin || mins < w.EndMin
}

func windowStart(now time.Time, w Window) time.Time {
	y, m, d := now.Date()
	start := time.Date(y, m, d, w.StartMin/60, w.StartMin%60, 0, 0, now.Location())
	if w.StartMin < w.EndMin {
		return start
	}
	mins := now.Hour()*60 + now.Minute()
	if mins < w.EndMin {
		return start.Add(-24 * time.Hour)
	}
	return start
}

// Replication is the catch-up view the conversion guard needs.
type Replication struct {
	PeerCount int
	AckKnown  bool
	MaxSeq    int64
	PinAck    int64
	// RetainedOps is the configured rewind window. Peers are caught up when
	// the unacked gap does not exceed it.
	RetainedOps int64
	// InboundPending is true when this node has not successfully pulled
	// from every configured peer, or the last pull recorded an error.
	InboundPending bool
	PendingOps     int64
}

// CaughtUp reports whether a full VACUUM may block this node. No configured
// peers means there is nothing to wait for.
func (r Replication) CaughtUp() (bool, string) {
	if r.PeerCount <= 0 {
		return true, "no peers configured"
	}
	if !r.AckKnown {
		return false, "peer acks are incomplete"
	}
	if r.InboundPending {
		return false, "inbound replication is still pending"
	}
	retained := r.RetainedOps
	if retained < 0 {
		retained = 0
	}
	lag := r.MaxSeq - r.PinAck
	if lag < 0 {
		lag = 0
	}
	if lag > retained {
		return false, fmt.Sprintf("peers lag by %d ops, above retained window %d", lag, retained)
	}
	return true, "replication is caught up"
}

// State is what the scheduler remembers between ticks.
type State struct {
	LastMetrics        time.Time
	LastConvertAttempt time.Time
	LastSkip           string
}

// Observation is one look at the database and the cluster.
type Observation struct {
	Now                time.Time
	NodeID             string
	CurrentAutoVacuum  int
	DesiredAutoVacuum  int
	CompletionRecorded bool
	DBBytes            int64
	FreeBytes          int64
	FreelistPages      int64
	Replication        Replication
	FreeErr            error
	ReplicationErr     error
}

// Action is what the runner should do for this tick. Reason is the
// conversion status included in the metrics log.
type Action struct {
	Vacuum              bool
	RecordWithoutVacuum bool
	LogMetrics          bool
	WarnPending         bool
	WarnSize            bool
	LogSkip             bool
	Reason              string
	Stagger             time.Duration
}

// Step decides maintenance work for one wake-up. It does not touch the
// database. The runner performs Vacuum or RecordWithoutVacuum and logs.
func Step(cfg Config, state State, obs Observation) (State, Action) {
	var action Action
	if cfg.MetricsInterval > 0 && (state.LastMetrics.IsZero() || !obs.Now.Before(state.LastMetrics.Add(cfg.MetricsInterval))) {
		state.LastMetrics = obs.Now
		action.LogMetrics = true
		if cfg.WarnPendingOps > 0 && obs.Replication.PendingOps > cfg.WarnPendingOps {
			action.WarnPending = true
		}
		if cfg.WarnDBBytes > 0 && obs.DBBytes > cfg.WarnDBBytes {
			action.WarnSize = true
		}
	}
	if !cfg.Enabled {
		action.Reason = "maintenance disabled"
		return state, action
	}
	if obs.CurrentAutoVacuum == AutoVacuumIncremental {
		if !obs.CompletionRecorded {
			action.RecordWithoutVacuum = true
			return skip(state, action, "auto_vacuum is already INCREMENTAL; recording completion without VACUUM")
		}
		return skip(state, action, "auto_vacuum conversion already completed")
	}
	if obs.DesiredAutoVacuum != AutoVacuumIncremental {
		return skip(state, action, "database.auto_vacuum is not incremental; leaving the existing database unchanged")
	}
	if obs.CurrentAutoVacuum != AutoVacuumNone {
		return skip(state, action, fmt.Sprintf("auto_vacuum mode %d is not NONE; online conversion only rewrites NONE databases", obs.CurrentAutoVacuum))
	}
	window, err := ParseWindow(cfg.WindowStart, cfg.WindowEnd)
	if err != nil {
		return skip(state, action, err.Error())
	}
	action.Stagger = Stagger(obs.NodeID, window.Duration())
	if !inWindow(obs.Now, window) {
		return skip(state, action, "outside the low-traffic conversion window")
	}
	if !InConvertSlot(obs.Now, window, action.Stagger) {
		return skip(state, action, fmt.Sprintf("waiting %s after window start for node stagger", action.Stagger))
	}
	if !state.LastConvertAttempt.IsZero() && obs.Now.Sub(state.LastConvertAttempt) < cfg.ConvertRetry {
		return skip(state, action, "conversion was attempted recently")
	}
	if obs.FreeErr != nil {
		return skip(state, action, fmt.Sprintf("free disk check failed: %v", obs.FreeErr))
	}
	need := obs.DBBytes + cfg.FreeHeadroomBytes
	if obs.FreeBytes < need {
		return skip(state, action, fmt.Sprintf("need %d free bytes to VACUUM, have %d", need, obs.FreeBytes))
	}
	if obs.ReplicationErr != nil {
		return skip(state, action, fmt.Sprintf("replication check failed: %v", obs.ReplicationErr))
	}
	ok, why := obs.Replication.CaughtUp()
	if !ok {
		return skip(state, action, why)
	}
	state.LastConvertAttempt = obs.Now
	state.LastSkip = ""
	action.Vacuum = true
	action.Reason = why
	action.Stagger = Stagger(obs.NodeID, window.Duration())
	return state, action
}

func skip(state State, action Action, reason string) (State, Action) {
	action.Reason = reason
	action.LogSkip = reason != state.LastSkip
	state.LastSkip = reason
	return state, action
}

// AutoVacuumName is the log label for a PRAGMA auto_vacuum integer.
func AutoVacuumName(mode int) string {
	switch mode {
	case AutoVacuumNone:
		return "NONE"
	case AutoVacuumFull:
		return "FULL"
	case AutoVacuumIncremental:
		return "INCREMENTAL"
	default:
		return fmt.Sprintf("mode-%d", mode)
	}
}
