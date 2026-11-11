package main

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// A request to post on several sites that is sent again (the person repeats
// it after a run did only some of them) is the same task in the recovery
// ledger: same conversation, same words, same lineage. The sites that task
// already published (goal verified done in an earlier run) start this run
// as settled records, so the run goes straight to the sites still open, its
// reply lists the earlier posts with their links, and nothing is posted
// twice (11-11 22:25: zhihu and csdn were live, xiaohongshu was not).

var desktopPublishCarried sync.Map // run id -> true once seeded

// desktopPublishCarriedSites: run id -> []string site ids an earlier run of
// the same request published (they count as done for this run's outcome).
var desktopPublishCarriedSites sync.Map

func desktopCarriedSites(runID string) []string {
	if v, ok := desktopPublishCarriedSites.Load(runID); ok {
		return v.([]string)
	}
	return nil
}

// desktopSeedEarlierPosts seeds the run's records from the lineage's
// verified publish goals of earlier runs. Once per run.
func desktopSeedEarlierPosts(request agentruntime.TurnRequest, scope agentruntime.Scope) {
	runID := strings.TrimSpace(scope.RunID)
	if runID == "" {
		return
	}
	base := desktopTaskSpecBase(runID)
	if len(base.Sites) < 2 {
		return
	}
	if _, done := desktopPublishCarried.LoadOrStore(runID, true); done {
		return
	}
	lineage, err := desktopRecLineage(request, scope)
	if err != nil || lineage == "" {
		return
	}
	sk := desktopRecoveryScope(scope)
	eng := desktopRecovery()
	entries := desktopLedgerEntries(desktopLedgerKey(scope))
	for _, p := range base.Sites {
		if p == nil {
			continue
		}
		var earlier []string
		for _, g := range eng.Goals(sk, "publish|"+lineage+"|"+p.ID) {
			if g == nil || g.State != recovery.StateDone {
				continue
			}
			for _, r := range g.Runs {
				if r != runID {
					earlier = append(earlier, r)
				}
			}
		}
		if len(earlier) == 0 {
			continue
		}
		rec := desktopPublishRecord{Site: p.ID, State: browser.PublishLive, Evidence: "earlier_run", Earlier: true}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if e.Kind != "posted" || !desktopProfileHasHost(p, e.Host) || !desktopStrIn(earlier, e.Run) {
				continue
			}
			rec.Title, rec.URL = e.Title, e.URL
			break
		}
		desktopPublishRecordsMu.Lock()
		desktopPublishDone[runID] = append(desktopPublishDone[runID], rec)
		desktopPublishRecordsMu.Unlock()
		desktopPublishCarriedSites.Store(runID, append(desktopCarriedSites(runID), p.ID))
	}
}
