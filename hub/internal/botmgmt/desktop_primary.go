package botmgmt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const desktopCrossNoteCap = 50

// BotListItem is one bot on GET /api/v1/bots. Primary is computed for the
// response. It is not a field of Bot, so save cannot write it back onto the row.
type BotListItem struct {
	Bot
	Primary bool `json:"primary,omitempty"`
}

// BotCrossNote is one message between bots of the same account. It is not a run.
type BotCrossNote struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	From      string `json:"from"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Order     string `json:"order,omitempty"`
	CreatedAt string `json:"created_at"`
	// StartedAt is the single claim that a client may admit this note.
	// Empty means no client has started it. RunID is filled after 202.
	StartedAt string `json:"started_at,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	// ReplyTo is the delegate note a result answers. One delegate is answered once.
	ReplyTo string `json:"reply_to,omitempty"`
	// NoteID is the request alias for ReplyTo. It is not stored on the note.
	NoteID string `json:"note_id,omitempty"`
	// Topic is a report's reason: done, blocked, progress, question,
	// artifact, info or reply. Artifacts are desktop paths or artifact ids.
	Topic     string   `json:"topic,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
	// Duplicate is set on a response when an identical report was already
	// stored inside the dedupe window; it is not stored.
	Duplicate bool `json:"duplicate,omitempty"`
	// AckedAt is set once the receiving bot (its GUI, or its MaClawSrv run)
	// has taken the message from its Hub mailbox. Unacked messages are
	// replayed on every inbox read, so a closed GUI loses nothing.
	AckedAt string `json:"acked_at,omitempty"`
}

// ListBotsForUser returns this user's bots. Exactly one of them is primary
// when the user has any bot: the stored id when it still exists, otherwise
// the earliest parseable CreatedAt, then bot id ascending.
func (s *Service) ListBotsForUser(ctx context.Context, tenantID, userID string) ([]BotListItem, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	bots := botsOwnedBy(rec.Bots, userID)
	primary := primaryBotID(rec, userID)
	out := make([]BotListItem, 0, len(bots))
	for _, bot := range bots {
		out = append(out, BotListItem{Bot: bot, Primary: primary != "" && bot.ID == primary})
	}
	return out, nil
}

// SetPrimaryBot stores this bot as the account's only primary. The previous
// id is replaced in the same write. Creating a bot does not call this.
func (s *Service) SetPrimaryBot(ctx context.Context, tenantID, userID, botID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !botOwned(rec.Bots, userID, botID) {
		return ErrNotFound
	}
	if rec.PrimaryBot == nil {
		rec.PrimaryBot = map[string]string{}
	}
	rec.PrimaryBot[userID] = botID
	return s.save(ctx, tenantID, rec)
}

func botsOwnedBy(bots []Bot, userID string) []Bot {
	userID = strings.TrimSpace(userID)
	out := make([]Bot, 0)
	for _, bot := range bots {
		if strings.TrimSpace(bot.OwnerUserID) == userID {
			out = append(out, bot)
		}
	}
	return out
}

func botOwned(bots []Bot, userID, botID string) bool {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return false
	}
	for _, bot := range botsOwnedBy(bots, userID) {
		if bot.ID == botID {
			return true
		}
	}
	return false
}

func primaryBotID(rec record, userID string) string {
	userID = strings.TrimSpace(userID)
	owned := botsOwnedBy(rec.Bots, userID)
	if len(owned) == 0 {
		return ""
	}
	if rec.PrimaryBot != nil {
		marked := strings.TrimSpace(rec.PrimaryBot[userID])
		if marked != "" && botOwned(owned, userID, marked) {
			return marked
		}
	}
	return earliestBotID(owned)
}

func earliestBotID(bots []Bot) string {
	if len(bots) == 0 {
		return ""
	}
	best := 0
	for i := 1; i < len(bots); i++ {
		if botCreatedBefore(bots[i], bots[best]) {
			best = i
		}
	}
	return bots[best].ID
}

func botCreatedBefore(a, b Bot) bool {
	ta, aErr := time.Parse(time.RFC3339, strings.TrimSpace(a.CreatedAt))
	tb, bErr := time.Parse(time.RFC3339, strings.TrimSpace(b.CreatedAt))
	aOK, bOK := aErr == nil, bErr == nil
	if aOK != bOK {
		return aOK
	}
	if aOK && !ta.Equal(tb) {
		return ta.Before(tb)
	}
	return a.ID < b.ID
}

// PostBotCrossTalk appends one note. notify and delegate require the caller
// to be the primary bot and do not admit a run. result is accepted from the
// delegate note's stored sender, including after that bot is no longer primary
// or has been deleted.
func (s *Service) PostBotCrossTalk(ctx context.Context, tenantID, userID, fromBotID string, in BotCrossNote) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	fromBotID = strings.TrimSpace(fromBotID)
	kind := strings.TrimSpace(in.Kind)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	rec.CrossNotes = pruneCrossNotes(rec.CrossNotes, s.now())
	switch kind {
	case "notify", "delegate":
		if !botOwned(rec.Bots, userID, fromBotID) {
			return BotCrossNote{}, ErrNotFound
		}
		if primaryBotID(rec, userID) != fromBotID {
			return BotCrossNote{}, ErrNotPrimary
		}
		target := strings.TrimSpace(in.Target)
		if target == "" || target == fromBotID || !botOwned(rec.Bots, userID, target) {
			return BotCrossNote{}, ErrNoSuchBot
		}
		note := BotCrossNote{
			ID:        newCrossNoteID(),
			Owner:     userID,
			From:      fromBotID,
			Target:    target,
			Kind:      kind,
			Text:      strings.TrimSpace(in.Text),
			Order:     strings.TrimSpace(in.Order),
			CreatedAt: s.now().UTC().Format(time.RFC3339),
		}
		rec.CrossNotes = append(rec.CrossNotes, note)
		rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
		if err := s.save(ctx, tenantID, rec); err != nil {
			return BotCrossNote{}, err
		}
		return note, nil
	case "result":
		parentID := strings.TrimSpace(in.ReplyTo)
		if parentID == "" {
			parentID = strings.TrimSpace(in.NoteID)
		}
		if parentID == "" {
			parentID = strings.TrimSpace(in.ID)
		}
		parent := findCrossNote(rec.CrossNotes, parentID)
		if parent == nil || parent.Kind != "delegate" || parent.Owner != userID || parent.Target != fromBotID {
			return BotCrossNote{}, ErrNotFound
		}
		if existing := resultFor(rec.CrossNotes, parent.ID); existing != nil {
			return *existing, nil
		}
		note := BotCrossNote{
			ID:        newCrossNoteID(),
			Owner:     userID,
			From:      parent.From,
			Target:    parent.Target,
			Kind:      "result",
			Text:      strings.TrimSpace(in.Text),
			CreatedAt: s.now().UTC().Format(time.RFC3339),
			ReplyTo:   parent.ID,
		}
		rec.CrossNotes = append(rec.CrossNotes, note)
		rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
		if err := s.save(ctx, tenantID, rec); err != nil {
			return BotCrossNote{}, err
		}
		return note, nil
	case "report":
		note, changed, err := s.reportLocked(&rec, userID, fromBotID, in)
		if err != nil || !changed {
			return note, err
		}
		if err := s.save(ctx, tenantID, rec); err != nil {
			return BotCrossNote{}, err
		}
		return note, nil
	default:
		return BotCrossNote{}, ErrInvalidInput
	}
}

// Report limits: one bot may send at most reportPerHour reports, and an
// identical report (same from, target, topic, text) inside reportDedupe is
// answered with the stored one instead of a second note.
const (
	reportPerHour = 15
	reportDedupe  = 10 * time.Minute
	reportTextMax = 2000
)

var reportTopics = map[string]bool{"done": true, "blocked": true, "progress": true, "question": true, "artifact": true, "info": true, "reply": true}

// reportLocked appends a bot-originated report. Any bot of the account may
// report to the primary (target empty, "main" or "primary") or to a peer.
// It is a message, not control: it never admits a run.
func (s *Service) reportLocked(rec *record, userID, fromBotID string, in BotCrossNote) (BotCrossNote, bool, error) {
	if !botOwned(rec.Bots, userID, fromBotID) {
		return BotCrossNote{}, false, ErrNotFound
	}
	target := strings.TrimSpace(in.Target)
	switch strings.ToLower(target) {
	case "", "main", "primary":
		target = primaryBotID(*rec, userID)
	}
	if target == "" || target == fromBotID || !botOwned(rec.Bots, userID, target) {
		return BotCrossNote{}, false, ErrNoSuchBot
	}
	topic := strings.ToLower(strings.TrimSpace(in.Topic))
	if !reportTopics[topic] {
		topic = "info"
	}
	text := strings.TrimSpace(in.Text)
	if r := []rune(text); len(r) > reportTextMax {
		text = string(r[:reportTextMax])
	}
	artifacts := make([]string, 0, len(in.Artifacts))
	for _, a := range in.Artifacts {
		if a = strings.TrimSpace(a); a != "" && len(artifacts) < 10 {
			artifacts = append(artifacts, a)
		}
	}
	if text == "" && len(artifacts) == 0 {
		return BotCrossNote{}, false, ErrInvalidInput
	}
	now := s.now()
	recent := 0
	for i := len(rec.CrossNotes) - 1; i >= 0; i-- {
		n := rec.CrossNotes[i]
		if n.Kind != "report" || n.Owner != userID || n.From != fromBotID {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, n.CreatedAt)
		if err != nil {
			continue
		}
		age := now.Sub(stamp)
		if age <= reportDedupe && n.Target == target && n.Topic == topic && n.Text == text && strings.Join(n.Artifacts, ",") == strings.Join(artifacts, ",") {
			n.Duplicate = true
			return n, false, nil
		}
		if age <= time.Hour {
			recent++
		}
	}
	if recent >= reportPerHour {
		return BotCrossNote{}, false, ErrRateLimited
	}
	note := BotCrossNote{
		ID:        newCrossNoteID(),
		Owner:     userID,
		From:      fromBotID,
		Target:    target,
		Kind:      "report",
		Topic:     topic,
		Text:      text,
		Artifacts: artifacts,
		RunID:     strings.TrimSpace(in.RunID),
		ReplyTo:   strings.TrimSpace(in.ReplyTo),
		CreatedAt: now.UTC().Format(time.RFC3339),
	}
	rec.CrossNotes = append(rec.CrossNotes, note)
	rec.CrossNotes = capReportNotes(rec.CrossNotes, userID)
	rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
	return note, true, nil
}

// PostBotReportFromInstance is the MaClawSrv side of a report: a desktop
// run (identified by its instance) tells the account's main bot, or a peer,
// that it finished, is blocked, has a question or has a file.
func (s *Service) PostBotReportFromInstance(ctx context.Context, tenantID, userID, instanceID string, in BotCrossNote) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" || instanceID == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	rec.CrossNotes = pruneCrossNotes(rec.CrossNotes, s.now())
	from := ""
	for _, bot := range botsOwnedBy(rec.Bots, userID) {
		if strings.TrimSpace(bot.InstanceID) == instanceID {
			from = bot.ID
			break
		}
	}
	if from == "" {
		return BotCrossNote{}, ErrNotFound
	}
	// A target given by name resolves to that bot's id.
	if t := strings.TrimSpace(in.Target); t != "" && !botOwned(rec.Bots, userID, t) {
		for _, bot := range botsOwnedBy(rec.Bots, userID) {
			if strings.EqualFold(strings.TrimSpace(bot.Name), t) {
				in.Target = bot.ID
				break
			}
		}
	}
	note, changed, err := s.reportLocked(&rec, userID, from, in)
	if err != nil || !changed {
		return note, err
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, err
	}
	return note, nil
}

// ListBotCrossNotes returns this user's notes that are still inside the
// seven-day window. It does not consume them.
func (s *Service) ListBotCrossNotes(ctx context.Context, tenantID, userID string) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	notes := pruneCrossNotes(rec.CrossNotes, s.now())
	out := make([]BotCrossNote, 0)
	userID = strings.TrimSpace(userID)
	for _, note := range notes {
		if note.Owner == userID {
			out = append(out, note)
		}
	}
	return out, nil
}

// ClaimBotCrossStart lets one client start a delegate. A second claim while
// the first is still fresh, or after a run id is stored, does not win.
// An empty claim older than 30 seconds can be taken again.
func (s *Service) ClaimBotCrossStart(ctx context.Context, tenantID, userID, botID, noteID string) (BotCrossNote, bool, error) {
	if s == nil {
		return BotCrossNote{}, false, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	noteID = strings.TrimSpace(noteID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, false, err
	}
	note := findCrossNote(rec.CrossNotes, noteID)
	if note == nil || note.Owner != userID || note.Kind != "delegate" || note.Target != botID {
		return BotCrossNote{}, false, ErrNotFound
	}
	now := s.now()
	if strings.TrimSpace(note.RunID) != "" || crossClaimFresh(note.StartedAt, now) {
		return *note, false, nil
	}
	note.StartedAt = now.UTC().Format(time.RFC3339)
	note.RunID = ""
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, false, err
	}
	return *note, true, nil
}

// NoteBotCrossRun stores the admitted run on a claim this target already won.
func (s *Service) NoteBotCrossRun(ctx context.Context, tenantID, userID, botID, noteID, runID string) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	note := findCrossNote(rec.CrossNotes, strings.TrimSpace(noteID))
	if note == nil || note.Owner != strings.TrimSpace(userID) || note.Target != strings.TrimSpace(botID) || note.Kind != "delegate" {
		return BotCrossNote{}, ErrNotFound
	}
	if strings.TrimSpace(note.StartedAt) == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	if existing := strings.TrimSpace(note.RunID); existing != "" && existing != runID {
		return *note, nil
	}
	note.RunID = runID
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, err
	}
	return *note, nil
}

func crossClaimFresh(started string, now time.Time) bool {
	started = strings.TrimSpace(started)
	if started == "" {
		return false
	}
	stamp, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return true
	}
	return now.Sub(stamp) <= desktopAdmitAbandonAfter
}

func findCrossNote(notes []BotCrossNote, id string) *BotCrossNote {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for i := range notes {
		if notes[i].ID == id {
			return &notes[i]
		}
	}
	return nil
}

func resultFor(notes []BotCrossNote, delegateID string) *BotCrossNote {
	for i := range notes {
		if notes[i].Kind == "result" && notes[i].ReplyTo == delegateID {
			return &notes[i]
		}
	}
	return nil
}

func pruneCrossNotes(notes []BotCrossNote, now time.Time) []BotCrossNote {
	if len(notes) == 0 {
		return notes
	}
	out := make([]BotCrossNote, 0, len(notes))
	for _, note := range notes {
		stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(note.CreatedAt))
		if err == nil && now.Sub(stamp) > desktopTaskUndeliveredKeep {
			continue
		}
		out = append(out, note)
	}
	return out
}

func capCrossNotes(notes []BotCrossNote, userID string) []BotCrossNote {
	userID = strings.TrimSpace(userID)
	owned := 0
	for _, note := range notes {
		if note.Owner == userID && note.Kind != "report" {
			owned++
		}
	}
	if owned <= desktopCrossNoteCap {
		return notes
	}
	drop := owned - desktopCrossNoteCap
	out := make([]BotCrossNote, 0, len(notes)-drop)
	for _, note := range notes {
		if drop > 0 && note.Owner == userID && note.Kind != "report" {
			drop--
			continue
		}
		out = append(out, note)
	}
	return out
}

// reportNoteCap bounds each account's mailbox. Reports are kept apart from
// the delegate-note cap. Past the cap acked reports go first, oldest first;
// unacked ones are dropped only when the mailbox is all unacked.
const reportNoteCap = 60

func capReportNotes(notes []BotCrossNote, userID string) []BotCrossNote {
	count := 0
	for _, n := range notes {
		if n.Owner == userID && n.Kind == "report" {
			count++
		}
	}
	drop := count - reportNoteCap
	if drop <= 0 {
		return notes
	}
	gone := map[int]bool{}
	for pass := 0; pass < 2 && drop > 0; pass++ {
		for i, n := range notes {
			if drop == 0 {
				break
			}
			if gone[i] || n.Owner != userID || n.Kind != "report" {
				continue
			}
			if pass == 0 && n.AckedAt == "" {
				continue
			}
			gone[i] = true
			drop--
		}
	}
	out := make([]BotCrossNote, 0, len(notes)-len(gone))
	for i, n := range notes {
		if !gone[i] {
			out = append(out, n)
		}
	}
	return out
}

// mailboxKinds are the notes that land in a bot's Hub mailbox.
var mailboxKinds = map[string]bool{"report": true}

func resolveMailboxBot(rec record, userID, botID string) (string, error) {
	botID = strings.TrimSpace(botID)
	switch strings.ToLower(botID) {
	case "", "main", "primary":
		botID = primaryBotID(rec, userID)
	}
	if botID == "" || !botOwned(rec.Bots, userID, botID) {
		return "", ErrNoSuchBot
	}
	return botID, nil
}

// BotMailbox returns the messages addressed to botID ("main" = the
// account primary), oldest first. Unacked only unless all is set. ack ids
// are marked taken first, in the same write, so a reader can ack the last
// batch and pull the next one in one call.
func (s *Service) BotMailbox(ctx context.Context, tenantID, userID, botID string, ack []string, all bool) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return s.mailboxLocked(ctx, tenantID, &rec, userID, botID, ack, all)
}

func (s *Service) mailboxLocked(ctx context.Context, tenantID string, rec *record, userID, botID string, ack []string, all bool) ([]BotCrossNote, error) {
	id, err := resolveMailboxBot(*rec, userID, botID)
	if err != nil {
		return nil, err
	}
	rec.CrossNotes = pruneCrossNotes(rec.CrossNotes, s.now())
	if len(ack) > 0 {
		want := map[string]bool{}
		for _, a := range ack {
			want[strings.TrimSpace(a)] = true
		}
		stamp := s.now().UTC().Format(time.RFC3339)
		changed := false
		for i := range rec.CrossNotes {
			n := &rec.CrossNotes[i]
			if want[n.ID] && n.Owner == userID && n.Target == id && n.AckedAt == "" {
				n.AckedAt = stamp
				changed = true
			}
		}
		if changed {
			if err := s.save(ctx, tenantID, *rec); err != nil {
				return nil, err
			}
		}
	}
	out := []BotCrossNote{}
	for _, n := range rec.CrossNotes {
		if n.Owner == userID && n.Target == id && mailboxKinds[n.Kind] && (all || n.AckedAt == "") {
			out = append(out, n)
		}
	}
	return out, nil
}

// BotMailboxForInstance is BotMailbox for the bot that owns a MaClawSrv
// instance (the remote bot reading its own mail).
func (s *Service) BotMailboxForInstance(ctx context.Context, tenantID, userID, instanceID string, ack []string) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" || instanceID == "" {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	for _, bot := range botsOwnedBy(rec.Bots, userID) {
		if strings.TrimSpace(bot.InstanceID) == instanceID {
			return s.mailboxLocked(ctx, tenantID, &rec, userID, bot.ID, ack, false)
		}
	}
	return nil, ErrNotFound
}

func newCrossNoteID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "note_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return "note_" + hex.EncodeToString(buf[:])
}

// reportDelegateRunEnd is Hub's own "teammate finished" message: when a run
// that the primary delegated (a delegate note carries its run id) ends, Hub
// puts a done/blocked report in the primary's mailbox. It runs on Hub's run
// watcher, so it does not need any GUI to be open.
func (s *Service) reportDelegateRunEnd(tenantID, userID, botID, runID, status string, reply Reply, runErr error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return
	}
	userID = strings.TrimSpace(userID)
	primary := primaryBotID(rec, userID)
	if primary == "" || primary == botID {
		return
	}
	delegate := ""
	for _, n := range rec.CrossNotes {
		if n.Owner == userID && n.Kind == "delegate" && n.Target == botID && n.RunID == runID {
			delegate = n.ID
		}
	}
	if delegate == "" {
		return
	}
	topic, text := "done", strings.TrimSpace(reply.Text)
	switch {
	case runErr != nil || status == "failed" || status == "error":
		topic = "blocked"
		text = "失败 / failed: " + firstNonEmptyString(text, fmt.Sprint(runErr))
	case reply.Handoff || strings.TrimSpace(reply.AttentionReason) != "" || strings.TrimSpace(reply.AskUserQuestion) != "":
		topic = "blocked"
		text = firstNonEmptyString(strings.TrimSpace(reply.AskUserQuestion), strings.TrimSpace(reply.AttentionReason), text, "需要人处理 / needs the person")
	}
	if text == "" {
		text = "任务完成 / run finished"
	}
	arts := []string{}
	for _, f := range reply.Files {
		if name := strings.TrimSpace(f.Name); name != "" && len(arts) < 10 {
			arts = append(arts, name)
		}
	}
	note, changed, err := s.reportLocked(&rec, userID, botID, BotCrossNote{Target: primary, Topic: topic, Text: text, Artifacts: arts, RunID: runID, ReplyTo: delegate})
	if err != nil || !changed || note.ID == "" {
		return
	}
	_ = s.save(ctx, tenantID, rec)
}

func firstNonEmptyString(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" && x != "<nil>" {
			return x
		}
	}
	return ""
}
