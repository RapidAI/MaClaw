package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/dbmaint"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

const convertBusyTimeoutMS = 60_000

type maintenanceCompletion struct {
	At         string `json:"at"`
	NodeID     string `json:"node_id"`
	ModeBefore int    `json:"mode_before"`
	ModeAfter  int    `json:"mode_after"`
	SizeBefore int64  `json:"size_before_bytes"`
	SizeAfter  int64  `json:"size_after_bytes"`
	Vacuumed   bool   `json:"vacuumed"`
}

// runDBMaintenance is the in-process replacement for an external VACUUM timer.
// It logs file and history size, releases no pages itself (prune batches do
// that), and once converts a legacy auto_vacuum=NONE file during the
// configured local window.
func runDBMaintenance(ctx context.Context, cfg *config.Config, provider *sqlite.Provider, haSvc *ha.Service) {
	if cfg == nil || provider == nil || provider.Write == nil {
		return
	}
	policy, err := dbmaint.Normalize(maintenancePolicy(cfg.Database.Maintenance))
	if err != nil {
		log.Printf("[hubcenter][db] maintenance config: %v", err)
		return
	}
	if !policy.Enabled {
		log.Printf("[hubcenter][db] automatic maintenance disabled")
		return
	}
	nodeID := maintenanceNodeID(cfg)
	log.Printf("[hubcenter][db] automatic maintenance enabled: incremental_vacuum_pages=%d window=%s-%s local headroom_bytes=%d metrics_every=%s warn_pending_ops=%d warn_db_bytes=%d node=%s",
		dbmaint.PruneVacuumPages(true, policy.IncrementalVacuumPages),
		policy.WindowStart, policy.WindowEnd, policy.FreeHeadroomBytes, policy.MetricsInterval, policy.WarnPendingOps, policy.WarnDBBytes, nodeID)

	state := dbmaint.State{}
	tick := func() {
		state = maintenanceTick(ctx, cfg, policy, provider, haSvc, nodeID, state)
	}
	tick()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func maintenancePolicy(cfg config.DatabaseMaintenanceConfig) dbmaint.Config {
	return dbmaint.Config{
		Enabled:                cfg.Enabled,
		IncrementalVacuumPages: cfg.IncrementalVacuumPages,
		WindowStart:            cfg.WindowStart,
		WindowEnd:              cfg.WindowEnd,
		FreeHeadroomBytes:      cfg.FreeHeadroomBytes,
		MetricsInterval:        time.Duration(cfg.MetricsIntervalMinutes) * time.Minute,
		WarnPendingOps:         cfg.WarnPendingOps,
		WarnDBBytes:            cfg.WarnDBBytes,
	}
}

func maintenanceNodeID(cfg *config.Config) string {
	if cfg == nil {
		return "single"
	}
	if id := cfg.HA.NodeID; id != "" {
		return id
	}
	return "single"
}

func maintenanceTick(ctx context.Context, cfg *config.Config, policy dbmaint.Config, provider *sqlite.Provider, haSvc *ha.Service, nodeID string, state dbmaint.State) dbmaint.State {
	obs := dbmaint.Observation{
		Now:               time.Now(),
		NodeID:            nodeID,
		DesiredAutoVacuum: desiredAutoVacuum(cfg.Database.AutoVacuum),
		DBBytes:           dbmaint.FileSize(cfg.Database.DSN),
	}
	mode, err := sqlite.AutoVacuumMode(ctx, provider.Write)
	if err != nil {
		log.Printf("[hubcenter][db] read auto_vacuum: %v", err)
		return state
	}
	obs.CurrentAutoVacuum = mode
	freelist, err := sqlite.FreelistCount(ctx, provider.Write)
	if err != nil {
		log.Printf("[hubcenter][db] read freelist_count: %v", err)
	} else {
		obs.FreelistPages = freelist
	}
	if _, recorded, err := sqlite.ReadMaintenanceState(ctx, provider.Write, dbmaint.CompletionKey); err != nil {
		log.Printf("[hubcenter][db] read maintenance state: %v", err)
	} else {
		obs.CompletionRecorded = recorded
	}
	obs.FreeBytes, obs.FreeErr = dbmaint.FreeBytes(cfg.Database.DSN)
	obs.Replication, obs.ReplicationErr = maintenanceReplication(ctx, cfg, haSvc, provider)
	next, action := dbmaint.Step(policy, state, obs)
	if action.LogMetrics {
		log.Printf("[hubcenter][db] maintenance file_bytes=%d wal_bytes=%d freelist_pages=%d auto_vacuum=%s pending_ops=%d ack_known=%t ack_seq=%d free_bytes=%d conversion=%s",
			obs.DBBytes, dbmaint.FileSize(cfg.Database.DSN+"-wal"), obs.FreelistPages, dbmaint.AutoVacuumName(obs.CurrentAutoVacuum),
			obs.Replication.PendingOps, obs.Replication.AckKnown, obs.Replication.PinAck, obs.FreeBytes, action.Reason)
	}
	if action.WarnPending {
		log.Printf("[hubcenter][db] WARN pending history ops %d exceed threshold %d", obs.Replication.PendingOps, policy.WarnPendingOps)
	}
	if action.WarnSize {
		log.Printf("[hubcenter][db] WARN database file %d bytes exceeds threshold %d", obs.DBBytes, policy.WarnDBBytes)
	}
	if action.LogSkip && !action.Vacuum && !action.RecordWithoutVacuum {
		log.Printf("[hubcenter][db] auto_vacuum conversion skipped: %s", action.Reason)
	}
	if action.RecordWithoutVacuum {
		if err := writeMaintenanceCompletion(ctx, provider, nodeID, obs.CurrentAutoVacuum, obs.CurrentAutoVacuum, obs.DBBytes, obs.DBBytes, false); err != nil {
			log.Printf("[hubcenter][db] record auto_vacuum completion: %v", err)
		} else {
			log.Printf("[hubcenter][db] auto_vacuum is already %s; recorded completion, no VACUUM", dbmaint.AutoVacuumName(obs.CurrentAutoVacuum))
		}
	}
	if action.Vacuum {
		if err := convertAutoVacuum(ctx, cfg, provider, nodeID, obs, action.Stagger); err != nil {
			log.Printf("[hubcenter][db] auto_vacuum conversion failed: %v", err)
		}
	}
	return next
}

func maintenanceReplication(ctx context.Context, cfg *config.Config, haSvc *ha.Service, provider *sqlite.Provider) (dbmaint.Replication, error) {
	repl := dbmaint.Replication{RetainedOps: int64(cfg.HA.HistoryMaxRetainedOps)}
	if haSvc == nil {
		n, err := sqlite.CountHASyncOps(ctx, provider.Write)
		repl.PendingOps = n
		return repl, err
	}
	snap, err := haSvc.MaintenanceSnapshot(ctx)
	if err != nil {
		return repl, err
	}
	repl.PeerCount = snap.PeerCount
	repl.AckKnown = snap.AckKnown
	repl.MaxSeq = snap.MaxSeq
	repl.PinAck = snap.PinAck
	repl.InboundPending = snap.InboundPending
	repl.PendingOps = snap.PendingOps
	return repl, nil
}

func desiredAutoVacuum(mode string) int {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "full":
		return dbmaint.AutoVacuumFull
	case "none", "off":
		return dbmaint.AutoVacuumNone
	default:
		return dbmaint.AutoVacuumIncremental
	}
}

func convertAutoVacuum(ctx context.Context, cfg *config.Config, provider *sqlite.Provider, nodeID string, obs dbmaint.Observation, stagger time.Duration) error {
	log.Printf("[hubcenter][db] auto_vacuum conversion starting: node=%s file_bytes=%d free_bytes=%d stagger=%s window=%s-%s. VACUUM takes an exclusive lock until it finishes",
		nodeID, obs.DBBytes, obs.FreeBytes, stagger, cfg.Database.Maintenance.WindowStart, cfg.Database.Maintenance.WindowEnd)
	busy := cfg.Database.BusyTimeoutMS
	if busy <= 0 {
		busy = 10000
	}
	if _, err := provider.Write.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", convertBusyTimeoutMS)); err != nil {
		return err
	}
	defer func() {
		_, _ = provider.Write.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout = %d", busy))
	}()
	started := time.Now()
	if err := sqlite.Vacuum(ctx, provider.Write); err != nil {
		return err
	}
	mode, err := sqlite.AutoVacuumMode(ctx, provider.Write)
	if err != nil {
		return err
	}
	if mode != dbmaint.AutoVacuumIncremental {
		return fmt.Errorf("auto_vacuum is %s after VACUUM", dbmaint.AutoVacuumName(mode))
	}
	sizeAfter := dbmaint.FileSize(cfg.Database.DSN)
	if err := writeMaintenanceCompletion(ctx, provider, nodeID, obs.CurrentAutoVacuum, mode, obs.DBBytes, sizeAfter, true); err != nil {
		return err
	}
	log.Printf("[hubcenter][db] auto_vacuum conversion finished: node=%s %d -> %d bytes in %s; auto_vacuum=INCREMENTAL recorded, it will not run again",
		nodeID, obs.DBBytes, sizeAfter, time.Since(started).Round(time.Millisecond))
	return nil
}

func writeMaintenanceCompletion(ctx context.Context, provider *sqlite.Provider, nodeID string, before, after int, sizeBefore, sizeAfter int64, vacuumed bool) error {
	payload, err := json.Marshal(maintenanceCompletion{
		At:         time.Now().UTC().Format(time.RFC3339),
		NodeID:     nodeID,
		ModeBefore: before,
		ModeAfter:  after,
		SizeBefore: sizeBefore,
		SizeAfter:  sizeAfter,
		Vacuumed:   vacuumed,
	})
	if err != nil {
		return err
	}
	return sqlite.WriteMaintenanceState(ctx, provider.Write, dbmaint.CompletionKey, string(payload))
}
