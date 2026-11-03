package botmgmt

import (
	"strings"
	"time"
)

// Independent task watcher (alongside srv desktop_watchdog / ensureReceipt):
// the watcher role patrols each bot's worker runs and reports a finding
// (stuck, needs the person, finished with nothing to show, failed) to that
// bot's router (bot_roles.go). It never speaks to the person itself. One
// finding per run and reason; never spam.

const (
	patrolEvery      = time.Minute
	patrolStallAfter = 15 * time.Minute
	patrolQuietAge   = 2 * time.Minute // brand-new runs stay quiet
)

type patrolSight struct {
	fingerprint string
	changed     time.Time
	notified    map[string]bool // reason -> sent
}

func (s *Service) ensureTaskPatrol() {
	if s == nil {
		return
	}
	s.patrolOnce.Do(func() {
		s.patrolSight = map[string]*patrolSight{}
		go s.desktopTaskPatrolLoop()
	})
}

func (s *Service) desktopTaskPatrolLoop() {
	t := time.NewTicker(patrolEvery)
	defer t.Stop()
	for range t.C {
		s.patrolDesktopTasksOnce()
	}
}

func (s *Service) patrolDesktopTasksOnce() {
	if s == nil {
		return
	}
	type snap struct {
		tenant, user, bot, run, status, activity, where, next, open, text, attention string
		handoff                                                                      bool
		started, finished                                                            time.Time
		done, delivered                                                              bool
		err                                                                          string
	}
	var list []snap
	now := s.now()
	s.mu.Lock()
	for key, st := range s.desktopAdmits {
		if st == nil {
			continue
		}
		st.mu.Lock()
		runID := strings.TrimSpace(st.runID)
		if runID == "" {
			st.mu.Unlock()
			continue
		}
		tenant, user := desktopAdmitTenantUser(key)
		list = append(list, snap{
			tenant: tenant, user: user, bot: st.botID, run: runID,
			status: st.status, activity: st.activity,
			where: st.progressWhere, next: st.progressNext, open: st.progressOpen,
			text: strings.TrimSpace(st.reply.Text), attention: strings.TrimSpace(st.reply.AttentionReason),
			handoff: st.reply.Handoff, started: st.started, finished: st.finished,
			done: st.done, delivered: st.delivered,
			err: errString(st.err),
		})
		st.mu.Unlock()
	}
	s.mu.Unlock()

	s.patrolMu.Lock()
	defer s.patrolMu.Unlock()
	if s.patrolSight == nil {
		s.patrolSight = map[string]*patrolSight{}
	}
	live := map[string]bool{}
	for _, it := range list {
		id := it.tenant + "\x00" + it.user + "\x00" + it.run
		live[id] = true
		fp := it.status + "|" + it.activity + "|" + it.where + "|" + it.next + "|" + it.open + "|" + it.text + "|" + it.attention
		sight := s.patrolSight[id]
		if sight == nil {
			sight = &patrolSight{fingerprint: fp, changed: now, notified: map[string]bool{}}
			s.patrolSight[id] = sight
		} else if sight.fingerprint != fp {
			sight.fingerprint = fp
			sight.changed = now
		}
		if it.started.IsZero() || now.Sub(it.started) < patrolQuietAge {
			continue
		}
		reason := ""
		switch {
		case !it.done && now.Sub(sight.changed) >= patrolStallAfter:
			reason = "stalled"
		case it.done && !it.delivered && strings.TrimSpace(it.text) == "" && it.err == "" && !it.handoff && it.attention == "":
			reason = "no_receipt"
		case it.done && (it.handoff || it.attention != ""):
			reason = "attention"
		case it.done && it.err != "" && strings.TrimSpace(it.text) == "":
			reason = "failed"
		}
		if reason == "" || sight.notified[reason] {
			continue
		}
		sight.notified[reason] = true
		// Watcher -> this bot's router; the router alone talks to the person.
		go s.watcherReport(watchFinding{TenantID: it.tenant, UserID: it.user, BotID: it.bot, RunID: it.run, Reason: reason, Detail: it.attention})
	}
	for id := range s.patrolSight {
		if !live[id] {
			delete(s.patrolSight, id)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// desktopAdmitTenantUser pulls tenant and user out of an admit key.
func desktopAdmitTenantUser(key string) (tenant, user string) {
	// key = tenant\x00user\x00bot\x00run (tenant may be empty for default)
	parts := strings.Split(key, "\x00")
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", parts[0]
	case 2:
		return parts[0], parts[1]
	default:
		return parts[0], parts[1]
	}
}
