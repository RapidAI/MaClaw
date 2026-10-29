package agentservice

import (
	"log"
	"os"
	"strings"
	"time"
)

// Orphaned runs. A run's goroutine and cancel func live only in the process
// that admitted it. When MaClawSrv restarts (deploy, crash) every run still
// stored as "running" has no executor any more, yet it keeps blocking the
// session queue ("等手上的做完") and CancelRun refuses it (no cancel func ->
// ErrRunNotRunning). Seen live 10-22: run_181f2018 stayed running after a
// restart and the person's resend was queued behind it.
//
// At startup every stored running run is marked failed with
// runInterruptedError and a run.failed event. MACLAW_RECONCILE_ORPHAN_RUNS=off
// disables this (for a deployment where several processes share one store).

// orphanCancelGrace: CancelRun treats a running run without a cancel func as
// orphaned only after this age.
const orphanCancelGrace = 30 * time.Second

const runInterruptedError = "interrupted: service restarted while the run was in progress"

func orphanRunReconcileEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_RECONCILE_ORPHAN_RUNS"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// reconcileOrphanRuns marks runs left running by a previous process as
// failed. Returns how many were changed. Never fails startup.
func (s *Service) reconcileOrphanRuns() int {
	if s == nil || s.store == nil || !orphanRunReconcileEnabled() {
		return 0
	}
	store, err := snapshotStore(s.store)
	if err != nil {
		log.Printf("[agentservice] orphan run reconcile: snapshot: %v", err)
		return 0
	}
	tenants, err := store.ListTenants()
	if err != nil {
		log.Printf("[agentservice] orphan run reconcile: list tenants: %v", err)
		return 0
	}
	n := 0
	for _, t := range tenants {
		users, err := store.ListUsers(t.ID)
		if err != nil {
			continue
		}
		for _, u := range users {
			instances, err := store.ListInstances(t.ID, u.ID)
			if err != nil {
				continue
			}
			for _, inst := range instances {
				runs, err := store.ListRuns(t.ID, u.ID, inst.ID)
				if err != nil {
					continue
				}
				for _, run := range runs {
					if run.Status != RunStatusRunning {
						continue
					}
					if _, live := s.peekRunCancel(run.ID); live {
						continue
					}
					if s.markRunInterrupted(run) {
						n++
						log.Printf("[agentservice] orphan run marked interrupted run=%s session=%s instance=%s started=%s", run.ID, run.SessionID, run.InstanceID, run.StartedAt.Format("2006-01-02T15:04:05Z07:00"))
					}
				}
			}
		}
	}
	return n
}

// markRunInterrupted stores run as failed (interrupted) with its event.
func (s *Service) markRunInterrupted(run Run) bool {
	s.reapRunSupplements(run.ID)
	completed := s.now()
	run.Status = RunStatusFailed
	run.Error = runInterruptedError
	run.CompletedAt = &completed
	if !run.StartedAt.IsZero() {
		run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	}
	ev := runTerminalEventFor(run, "run.failed", map[string]any{"error": run.Error, "reason": "interrupted"})
	committed, err := s.saveRunTerminalWithEvents(run, []RunEvent{ev})
	if err != nil {
		if saveErr := s.store.SaveRun(run); saveErr != nil {
			log.Printf("[agentservice] orphan run %s: save failed: %v", run.ID, saveErr)
			return false
		}
		committed = false
	}
	if !committed {
		s.emitRunEvent(run, ev.Type, ev.Payload)
	}
	return true
}
