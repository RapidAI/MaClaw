package botmgmt

import (
	"context"
	"strings"
	"time"
)

// desktopResultFetchWait is how long a finished run's result may wait for
// the GUI before Hub tells the person itself. Tests shorten it.
var desktopResultFetchWait = 2 * time.Minute

// sayUnfetchedResultLater: a run finished but no client took its result
// (the GUI was closed, or lost the wait), so the person would see the
// acknowledgement and then nothing (papers 2026-11-11, Bot 3). After
// desktopResultFetchWait, a result nobody acked is said to the person in
// the bot's own conversation. The say key is the run, so it is said once,
// however often this runs.
func (s *Service) sayUnfetchedResultLater(tenantID, userID, botID, runID string, reply Reply) {
	if s == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(botID) == "" {
		return
	}
	text := strings.TrimSpace(reply.SendToUser)
	if text == "" {
		return // nothing for the person: a quiet finish stays quiet
	}
	wait := desktopResultFetchWait
	go func() {
		time.Sleep(wait)
		if !s.desktopResultUnfetched(tenantID, userID, botID, runID) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := s.Say(ctx, tenantID, userID, botID, "", text, "run-result:"+runID)
		traceBot(botID, "result_said", err, "run", runID, "reason", "not_fetched")
		if err == nil {
			// Said once: a GUI that opens later must not show it again.
			_ = s.AckDesktopTask(ctx, tenantID, userID, botID, runID)
		}
	}()
}

// desktopResultUnfetched: the run's admit is still here and not acked.
// AckDesktopTask removes the admit, so an acked result is never said again.
func (s *Service) desktopResultUnfetched(tenantID, userID, botID, runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.done && !state.delivered
}
