package botmgmt

import (
	"context"
	"sort"
	"strings"
	"time"
)

// The display feed is what the person's GUI shows, apart from what each
// bot's agent consumes: messages between bots (any kind, files included),
// delegates and the runs Hub started for them, and server-side wake runs.
// Acking (delivery to an agent) never hides a note from the feed; only the
// GUI's own "shown" mark does, so a server-side wake while the GUI was
// closed still leaves the whole exchange for the GUI to backfill.

const botFeedMax = 200

// markWakeRun records the server-side turn that took these messages.
func (s *Service) markWakeRun(tenantID, userID string, noteIDs []string, runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return
	}
	changed := false
	for _, id := range noteIDs {
		if n := findCrossNote(rec.CrossNotes, id); n != nil && n.Owner == userID && n.WakeRunID == "" {
			n.WakeRunID = runID
			changed = true
		}
	}
	if changed {
		_ = s.save(ctx, tenantID, rec)
	}
}

// BotFeed marks shown as displayed, then returns this person's notes the
// GUI has not shown yet (all=true: every kept note, for a history view),
// oldest first.
//
// The read runs without the service lock: like Enabled, the store read, the
// JSON decode, and the grant/directory walk stay off it, so one GUI poll
// cannot queue behind another tenant's writes. The shown marks are a write
// and keep the lock's mutual exclusion; the feed is built from the snapshot
// read here, with the just-shown ids already excluded.
func (s *Service) BotFeed(ctx context.Context, tenantID, userID string, shown []string, all bool) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range shown {
		want[strings.TrimSpace(id)] = true
	}
	out := []BotCrossNote{}
	for _, n := range pruneCrossNotes(rec.CrossNotes, s.now()) {
		if n.Owner != userID || (n.Kind != "delegate" && n.Kind != "report") {
			continue
		}
		// A note named in shown is stamped by markCrossNotesShown below; it
		// must not come back in this response.
		if !all && (n.ShownAt != "" || want[n.ID]) {
			continue
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	if len(out) > botFeedMax {
		out = out[len(out)-botFeedMax:]
	}
	if len(want) > 0 {
		if err := s.markCrossNotesShown(ctx, tenantID, userID, want); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// markCrossNotesShown stamps the shown mark on this user's notes. It reloads
// the record under the write lock: the feed read an unlocked snapshot, and
// the mark must serialize with every other writer of this record.
func (s *Service) markCrossNotesShown(ctx context.Context, tenantID, userID string, want map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	stamp := s.now().UTC().Format(time.RFC3339)
	changed := false
	for i := range rec.CrossNotes {
		n := &rec.CrossNotes[i]
		if want[n.ID] && n.Owner == userID && n.ShownAt == "" {
			n.ShownAt = stamp
			changed = true
		}
	}
	if changed {
		return s.save(ctx, tenantID, rec)
	}
	return nil
}
