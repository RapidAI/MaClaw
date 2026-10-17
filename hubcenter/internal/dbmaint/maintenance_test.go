package dbmaint

import (
	"errors"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := Normalize(Config{Enabled: true})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	return cfg
}

func openObservation(now time.Time, nodeID string) Observation {
	return Observation{
		Now:               now,
		NodeID:            nodeID,
		CurrentAutoVacuum: AutoVacuumNone,
		DesiredAutoVacuum: AutoVacuumIncremental,
		DBBytes:           1000,
		FreeBytes:         1000 + DefaultFreeHeadroomBytes,
		Replication:       Replication{PeerCount: 0, PendingOps: 10},
	}
}

func TestNormalizeDefaultsAndRejectsBadWindow(t *testing.T) {
	cfg, err := Normalize(Config{Enabled: true})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.WindowStart != DefaultWindowStart || cfg.WindowEnd != DefaultWindowEnd {
		t.Fatalf("window = %s-%s", cfg.WindowStart, cfg.WindowEnd)
	}
	if cfg.IncrementalVacuumPages != DefaultIncrementalVacuumPages {
		t.Fatalf("pages = %d", cfg.IncrementalVacuumPages)
	}
	if cfg.FreeHeadroomBytes != DefaultFreeHeadroomBytes {
		t.Fatalf("headroom = %d", cfg.FreeHeadroomBytes)
	}
	if cfg.MetricsInterval != DefaultMetricsInterval || cfg.WarnPendingOps != DefaultWarnPendingOps || cfg.WarnDBBytes != DefaultWarnDBBytes {
		t.Fatalf("metrics defaults = %+v", cfg)
	}
	if _, err := Normalize(Config{WindowStart: "25:00"}); err == nil {
		t.Fatal("expected invalid window start")
	}
	if _, err := Normalize(Config{WindowStart: "03:00", WindowEnd: "03:00"}); err == nil {
		t.Fatal("expected empty window to be rejected")
	}
	clamped, err := Normalize(Config{IncrementalVacuumPages: 100000, WarnPendingOps: -1, WarnDBBytes: -1})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if clamped.IncrementalVacuumPages != MaxIncrementalVacuumPages {
		t.Fatalf("pages = %d, want cap %d", clamped.IncrementalVacuumPages, MaxIncrementalVacuumPages)
	}
	if clamped.WarnPendingOps != -1 || clamped.WarnDBBytes != -1 {
		t.Fatalf("negative warn thresholds were overwritten: %+v", clamped)
	}
}

func TestPruneVacuumPagesDisabledIsZero(t *testing.T) {
	if got := PruneVacuumPages(false, 64); got != 0 {
		t.Fatalf("disabled pages = %d, want 0", got)
	}
	if got := PruneVacuumPages(true, 0); got != DefaultIncrementalVacuumPages {
		t.Fatalf("default pages = %d", got)
	}
	if got := PruneVacuumPages(true, 100000); got != MaxIncrementalVacuumPages {
		t.Fatalf("capped pages = %d", got)
	}
}

func TestStaggerIsStableAndSeparatesNodes(t *testing.T) {
	window := 2 * time.Hour
	a := Stagger("hc-1", window)
	b := Stagger("hc-2", window)
	c := Stagger("hc-3", window)
	if a != Stagger("hc-1", window) || b != Stagger("hc-2", window) {
		t.Fatal("stagger changed for the same node id")
	}
	if a == b || b == c || a == c {
		t.Fatalf("stagger collided: hc-1=%s hc-2=%s hc-3=%s", a, b, c)
	}
	span := window - window/4
	for _, offset := range []time.Duration{a, b, c} {
		if offset < 0 || offset >= span {
			t.Fatalf("offset %s outside %s", offset, span)
		}
	}
	if Stagger("", window) != Stagger("single", window) {
		t.Fatal("empty node id should stagger like single")
	}
}

func TestInConvertSlotRespectsWindowAndStagger(t *testing.T) {
	w, err := ParseWindow("03:00", "05:00")
	if err != nil {
		t.Fatalf("ParseWindow: %v", err)
	}
	loc := time.FixedZone("L", 8*3600)
	start := time.Date(2026, 10, 17, 3, 0, 0, 0, loc)
	stagger := 20 * time.Minute
	if InConvertSlot(start.Add(-time.Second), w, stagger) {
		t.Fatal("before the window was eligible")
	}
	if InConvertSlot(start.Add(stagger-time.Second), w, stagger) {
		t.Fatal("before stagger was eligible")
	}
	if !InConvertSlot(start.Add(stagger), w, stagger) {
		t.Fatal("at stagger was not eligible")
	}
	if !InConvertSlot(start.Add(90*time.Minute), w, stagger) {
		t.Fatal("inside the window was not eligible")
	}
	if InConvertSlot(time.Date(2026, 10, 17, 5, 0, 0, 0, loc), w, stagger) {
		t.Fatal("window end was eligible")
	}

	wrap, err := ParseWindow("23:00", "01:00")
	if err != nil {
		t.Fatalf("ParseWindow wrap: %v", err)
	}
	late := time.Date(2026, 10, 17, 23, 10, 0, 0, loc)
	afterMidnight := time.Date(2026, 10, 18, 0, 30, 0, 0, loc)
	if InConvertSlot(late, wrap, 30*time.Minute) {
		t.Fatal("23:10 was eligible before a 30m stagger")
	}
	if !InConvertSlot(time.Date(2026, 10, 17, 23, 30, 0, 0, loc), wrap, 30*time.Minute) {
		t.Fatal("23:30 was not eligible")
	}
	if !InConvertSlot(afterMidnight, wrap, 30*time.Minute) {
		t.Fatal("00:30 was not inside the wrapping window")
	}
	if InConvertSlot(time.Date(2026, 10, 18, 1, 0, 0, 0, loc), wrap, 0) {
		t.Fatal("01:00 was still inside the wrapping window")
	}
}

func TestConversionGuard(t *testing.T) {
	cfg := testConfig(t)
	w, err := ParseWindow(cfg.WindowStart, cfg.WindowEnd)
	if err != nil {
		t.Fatalf("ParseWindow: %v", err)
	}
	loc := time.FixedZone("L", 8*3600)
	start := time.Date(2026, 10, 17, 3, 0, 0, 0, loc)
	earlyID, lateID := "hc-1", "hc-2"
	early := Stagger(earlyID, w.Duration())
	late := Stagger(lateID, w.Duration())
	if late < early {
		earlyID, lateID = lateID, earlyID
		early, late = late, early
	}
	if late <= early {
		t.Fatalf("expected distinct staggers, got %s and %s", early, late)
	}
	eligible := start.Add(late)
	beforeLate := start.Add(early)

	t.Run("allows caught up node after its stagger", func(t *testing.T) {
		state, action := Step(cfg, State{}, openObservation(eligible, lateID))
		if !action.Vacuum {
			t.Fatalf("Vacuum=false reason=%s", action.Reason)
		}
		if state.LastConvertAttempt != eligible {
			t.Fatalf("attempt = %s", state.LastConvertAttempt)
		}
	})

	t.Run("peer with a later stagger waits", func(t *testing.T) {
		_, action := Step(cfg, State{}, openObservation(beforeLate, lateID))
		if action.Vacuum {
			t.Fatal("later node converted before its stagger")
		}
		if action.Reason == "" || !action.LogSkip {
			t.Fatalf("reason=%q logSkip=%v", action.Reason, action.LogSkip)
		}
		_, again := Step(cfg, State{LastSkip: action.Reason}, openObservation(beforeLate, lateID))
		if again.LogSkip {
			t.Fatal("repeated the same skip reason")
		}
	})

	t.Run("earlier node can run while the later one waits", func(t *testing.T) {
		_, earlyAction := Step(cfg, State{}, openObservation(beforeLate, earlyID))
		_, lateAction := Step(cfg, State{}, openObservation(beforeLate, lateID))
		if !earlyAction.Vacuum {
			t.Fatalf("early node reason=%s", earlyAction.Reason)
		}
		if lateAction.Vacuum {
			t.Fatal("late node converted at the early node's offset")
		}
	})

	t.Run("outside the window", func(t *testing.T) {
		_, action := Step(cfg, State{}, openObservation(start.Add(-time.Minute), earlyID))
		if action.Vacuum || action.Reason != "outside the low-traffic conversion window" {
			t.Fatalf("action=%+v", action)
		}
	})

	t.Run("disk one byte short", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.FreeBytes = obs.DBBytes + cfg.FreeHeadroomBytes - 1
		_, action := Step(cfg, State{}, obs)
		if action.Vacuum {
			t.Fatal("converted without enough free disk")
		}
		obs.FreeBytes++
		_, action = Step(cfg, State{}, obs)
		if !action.Vacuum {
			t.Fatalf("exact headroom was refused: %s", action.Reason)
		}
	})

	t.Run("free disk check failure", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.FreeErr = errors.New("statfs failed")
		_, action := Step(cfg, State{}, obs)
		if action.Vacuum {
			t.Fatal("converted without a free-disk reading")
		}
	})

	t.Run("replication not caught up", func(t *testing.T) {
		cases := []Replication{
			{PeerCount: 2, AckKnown: false, MaxSeq: 10, PinAck: 10, RetainedOps: 4000},
			{PeerCount: 2, AckKnown: true, InboundPending: true, MaxSeq: 10, PinAck: 10, RetainedOps: 4000},
			{PeerCount: 2, AckKnown: true, MaxSeq: 5000, PinAck: 0, RetainedOps: 4000},
		}
		for _, repl := range cases {
			obs := openObservation(eligible, lateID)
			obs.Replication = repl
			_, action := Step(cfg, State{}, obs)
			if action.Vacuum {
				t.Fatalf("converted with %+v", repl)
			}
		}
		obs := openObservation(eligible, lateID)
		obs.Replication = Replication{PeerCount: 2, AckKnown: true, MaxSeq: 4500, PinAck: 1000, RetainedOps: 4000}
		_, action := Step(cfg, State{}, obs)
		if !action.Vacuum {
			t.Fatalf("caught-up peers were refused: %s", action.Reason)
		}
	})

	t.Run("replication check failure", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.ReplicationErr = errors.New("db")
		_, action := Step(cfg, State{}, obs)
		if action.Vacuum {
			t.Fatal("converted when the replication check failed")
		}
	})

	t.Run("already incremental records once and does not vacuum", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.CurrentAutoVacuum = AutoVacuumIncremental
		state, action := Step(cfg, State{}, obs)
		if action.Vacuum || !action.RecordWithoutVacuum {
			t.Fatalf("action=%+v", action)
		}
		obs.CompletionRecorded = true
		state, action = Step(cfg, state, obs)
		if action.Vacuum || action.RecordWithoutVacuum {
			t.Fatalf("repeated record: %+v", action)
		}
		if !action.LogSkip {
			t.Fatal("did not log the transition to the completed state")
		}
		_, action = Step(cfg, state, obs)
		if action.LogSkip {
			t.Fatal("logged the completed state twice")
		}
	})

	t.Run("stale completion record does not block a NONE database", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.CompletionRecorded = true
		_, action := Step(cfg, State{}, obs)
		if !action.Vacuum {
			t.Fatalf("stale record blocked conversion: %s", action.Reason)
		}
	})

	t.Run("does not convert away from an explicit non-incremental mode", func(t *testing.T) {
		obs := openObservation(eligible, lateID)
		obs.DesiredAutoVacuum = AutoVacuumNone
		_, action := Step(cfg, State{}, obs)
		if action.Vacuum {
			t.Fatal("converted while desired mode is NONE")
		}
		obs.DesiredAutoVacuum = AutoVacuumIncremental
		obs.CurrentAutoVacuum = AutoVacuumFull
		_, action = Step(cfg, State{}, obs)
		if action.Vacuum {
			t.Fatal("converted a FULL database")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		off := cfg
		off.Enabled = false
		_, action := Step(off, State{}, openObservation(eligible, lateID))
		if action.Vacuum || action.RecordWithoutVacuum {
			t.Fatalf("disabled maintenance acted: %+v", action)
		}
	})

	t.Run("retry gap", func(t *testing.T) {
		state, action := Step(cfg, State{}, openObservation(eligible, lateID))
		if !action.Vacuum {
			t.Fatalf("first attempt reason=%s", action.Reason)
		}
		_, action = Step(cfg, state, openObservation(eligible.Add(time.Minute), lateID))
		if action.Vacuum || action.Reason != "conversion was attempted recently" {
			t.Fatalf("retry was not held: %+v", action)
		}
		_, action = Step(cfg, state, openObservation(eligible.Add(cfg.ConvertRetry), lateID))
		if !action.Vacuum {
			t.Fatalf("retry after the gap was refused: %s", action.Reason)
		}
	})
}

func TestMetricsAndWarnThresholds(t *testing.T) {
	cfg := testConfig(t)
	cfg.MetricsInterval = 30 * time.Minute
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	obs := openObservation(now, "hc-1")
	obs.CurrentAutoVacuum = AutoVacuumIncremental
	obs.CompletionRecorded = true
	obs.Replication.PendingOps = cfg.WarnPendingOps + 1
	obs.DBBytes = cfg.WarnDBBytes + 1

	state, action := Step(cfg, State{}, obs)
	if !action.LogMetrics || !action.WarnPending || !action.WarnSize {
		t.Fatalf("first metrics action=%+v", action)
	}
	_, action = Step(cfg, state, obs)
	if action.LogMetrics || action.WarnPending || action.WarnSize {
		t.Fatalf("metrics repeated inside the interval: %+v", action)
	}
	obs.Now = now.Add(cfg.MetricsInterval)
	_, action = Step(cfg, state, obs)
	if !action.LogMetrics || !action.WarnPending || !action.WarnSize {
		t.Fatalf("interval did not warn again: %+v", action)
	}

	cfg.WarnPendingOps = -1
	cfg.WarnDBBytes = -1
	obs.Now = now
	_, action = Step(cfg, State{}, obs)
	if !action.LogMetrics || action.WarnPending || action.WarnSize {
		t.Fatalf("negative thresholds still warned: %+v", action)
	}
}
