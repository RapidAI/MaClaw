package botmgmt

import (
	"context"
	"strings"
)

// ReportRunIsDirect tells MaClawSrv whether a file report this instance is
// about to send the main bot belongs to the person instead: the run was
// started by the person in this bot's own chat (no delegate note and no
// mailbox wake started it), so the file goes into that chat, and nothing is
// handed to the main bot. A named peer, a run Hub cannot place, or the main
// bot itself is never direct (Bot 3 → Bot 1, 2026-11-10: a résumé the person
// asked Bot 3 for showed up as a bot-to-bot handoff to a busy Bot 1).
func (s *Service) ReportRunIsDirect(ctx context.Context, tenantID, userID, instanceID, runID, target string) (bool, error) {
	if s == nil {
		return false, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	runID = strings.TrimSpace(runID)
	if userID == "" || instanceID == "" {
		return false, ErrInvalidInput
	}
	if runID == "" {
		return false, nil
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return false, err
	}
	from := ""
	for _, bot := range botsOwnedBy(rec.Bots, userID) {
		if strings.TrimSpace(bot.InstanceID) == instanceID {
			from = bot.ID
			break
		}
	}
	if from == "" {
		return false, ErrNotFound
	}
	primary := primaryBotID(rec, userID)
	if from == primary {
		return false, nil
	}
	switch t := strings.ToLower(strings.TrimSpace(target)); t {
	case "", "main", "primary":
	default:
		if resolveParticipant(rec, userID, target) != primary {
			return false, nil
		}
	}
	return reportRunIsDirect(rec.CrossNotes, userID, from, runID), nil
}

// reportRunIsDirect: no note says another bot (delegate) or the mailbox
// (wake) started this run of bot from.
func reportRunIsDirect(notes []BotCrossNote, userID, from, runID string) bool {
	for _, n := range notes {
		if n.Owner != userID || n.Target != from {
			continue
		}
		if n.Kind == "delegate" && strings.TrimSpace(n.RunID) == runID {
			return false
		}
		if strings.TrimSpace(n.WakeRunID) == runID {
			return false
		}
	}
	return true
}
