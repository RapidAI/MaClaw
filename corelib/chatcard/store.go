package chatcard

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the durable record of chat cards. A process restart reopens it.
type Store struct {
	db  *sql.DB
	mu  sync.Mutex
	now func() time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS chat_cards (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		bot_id TEXT NOT NULL,
		task_id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		body TEXT NOT NULL,
		shot BLOB,
		status TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

func (s *Store) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "card_" + hex.EncodeToString(b[:]), nil
}

// Put stores a new card and supersedes any other pending card for the same task.
func (s *Store) Put(in OpenInput) (Card, error) {
	if s == nil || s.db == nil {
		return Card{}, ErrInvalid
	}
	now := s.clock()
	card, shot, err := Prepare(in, now)
	if err != nil {
		return Card{}, err
	}
	card.ID, err = newID()
	if err != nil {
		return Card{}, err
	}
	if card.Shot != nil {
		card.Shot.ID = card.ID
	}
	body, err := json.Marshal(card)
	if err != nil {
		return Card{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Card{}, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT id, body FROM chat_cards WHERE tenant_id=? AND user_id=? AND bot_id=? AND task_id=? AND status=?`,
		card.TenantID, card.UserID, card.BotID, card.TaskID, StatusPending)
	if err != nil {
		return Card{}, err
	}
	var prior []Card
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return Card{}, err
		}
		var old Card
		if json.Unmarshal([]byte(raw), &old) == nil {
			prior = append(prior, old)
		}
	}
	_ = rows.Close()
	for _, old := range prior {
		old.Status = StatusSuperseded
		old.UpdatedAt = now
		raw, _ := json.Marshal(old)
		if _, err := tx.Exec(`UPDATE chat_cards SET body=?, status=?, updated_at=? WHERE id=?`, string(raw), StatusSuperseded, now.Format(time.RFC3339Nano), old.ID); err != nil {
			return Card{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO chat_cards(id, tenant_id, user_id, bot_id, task_id, run_id, body, shot, status, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		card.ID, card.TenantID, card.UserID, card.BotID, card.TaskID, card.RunID, string(body), shot, card.Status, now.Format(time.RFC3339Nano)); err != nil {
		return Card{}, err
	}
	if err := tx.Commit(); err != nil {
		return Card{}, err
	}
	return card, nil
}

// FindByRun returns the newest card for a run. A missing row is ErrNotFound.
func (s *Store) FindByRun(tenantID, userID, runID string) (Card, error) {
	if s == nil || s.db == nil {
		return Card{}, ErrNotFound
	}
	var raw string
	err := s.db.QueryRow(`SELECT body FROM chat_cards WHERE tenant_id=? AND user_id=? AND run_id=? ORDER BY updated_at DESC LIMIT 1`,
		strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(runID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, err
	}
	var card Card
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return Card{}, err
	}
	return card, nil
}

func (s *Store) Get(id string) (Card, error) {
	card, _, err := s.get(id)
	return card, err
}

func (s *Store) get(id string) (Card, []byte, error) {
	if s == nil || s.db == nil {
		return Card{}, nil, ErrNotFound
	}
	var raw string
	var shot []byte
	err := s.db.QueryRow(`SELECT body, shot FROM chat_cards WHERE id=?`, strings.TrimSpace(id)).Scan(&raw, &shot)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, nil, ErrNotFound
	}
	if err != nil {
		return Card{}, nil, err
	}
	var card Card
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return Card{}, nil, err
	}
	return card, shot, nil
}

// ShotBytes returns the captured image for this card when the caller owns it.
func (s *Store) ShotBytes(actor Actor, id string) ([]byte, Card, error) {
	card, shot, err := s.get(id)
	if err != nil {
		return nil, Card{}, err
	}
	if !userOwns(actor, card) {
		return nil, Card{}, ErrForbidden
	}
	if len(shot) == 0 {
		return nil, card, ErrNotFound
	}
	return append([]byte(nil), shot...), card, nil
}

func userOwns(actor Actor, card Card) bool {
	return actor.Authenticated && actor.Kind == ActorUser && actor.TenantID == card.TenantID && actor.UserID == card.UserID
}

// Submit records one authenticated decision. The same decision a second time
// does not resume the task again.
func (s *Store) Submit(actor Actor, sub Submission) (Outcome, error) {
	if s == nil || s.db == nil {
		return Outcome{}, ErrNotFound
	}
	if !actor.Authenticated || actor.Kind != ActorUser || strings.TrimSpace(actor.UserID) == "" {
		return Outcome{}, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var raw string
	err = tx.QueryRow(`SELECT body FROM chat_cards WHERE id=?`, strings.TrimSpace(sub.RequestID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Outcome{}, ErrNotFound
	}
	if err != nil {
		return Outcome{}, err
	}
	var card Card
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return Outcome{}, err
	}
	out, err := decide(&card, actor, sub, s.clock())
	if err != nil {
		if errors.Is(err, ErrExpired) {
			raw, _ := json.Marshal(card)
			_, _ = tx.Exec(`UPDATE chat_cards SET body=?, status=?, updated_at=? WHERE id=? AND status=?`, string(raw), card.Status, card.UpdatedAt.Format(time.RFC3339Nano), card.ID, StatusPending)
			_ = tx.Commit()
		}
		return Outcome{}, err
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return Outcome{}, err
	}
	res, err := tx.Exec(`UPDATE chat_cards SET body=?, status=?, updated_at=? WHERE id=? AND status=?`,
		string(encoded), card.Status, card.UpdatedAt.Format(time.RFC3339Nano), card.ID, StatusPending)
	if err != nil {
		return Outcome{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		var again string
		if err := tx.QueryRow(`SELECT body FROM chat_cards WHERE id=?`, card.ID).Scan(&again); err != nil {
			return Outcome{}, err
		}
		var current Card
		if json.Unmarshal([]byte(again), &current) != nil {
			return Outcome{}, ErrConflict
		}
		if sameDecision(current, sub) {
			return outcomeOf(current, false, true), nil
		}
		return Outcome{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return Outcome{}, err
	}
	return out, nil
}

func decide(card *Card, actor Actor, sub Submission, now time.Time) (Outcome, error) {
	if actor.TenantID != card.TenantID || actor.UserID != card.UserID {
		return Outcome{}, ErrForbidden
	}
	if strings.TrimSpace(sub.TaskID) != card.TaskID || strings.TrimSpace(sub.BotID) != card.BotID || strings.TrimSpace(sub.RunID) != card.RunID {
		return Outcome{}, ErrForbidden
	}
	if card.Status != StatusPending {
		if sameDecision(*card, sub) {
			return outcomeOf(*card, false, true), nil
		}
		return Outcome{}, ErrConflict
	}
	if !card.ExpiresAt.IsZero() && !now.Before(card.ExpiresAt) {
		card.Status = StatusExpired
		card.UpdatedAt = now
		return Outcome{}, ErrExpired
	}
	if sub.OperationVersion != card.Operation.Version || sub.PayloadHash != card.Operation.Hash {
		return Outcome{}, ErrStale
	}
	if err := checkTarget(*card, sub); err != nil {
		return Outcome{}, err
	}
	for _, value := range sub.Fields {
		if secretField(value) {
			return Outcome{}, fmt.Errorf("%w: a secret cannot be sent on this card", ErrInvalid)
		}
	}
	decline := sub.Decline
	ids := compact(sub.ChoiceIDs)
	if err := checkChoices(*card, ids, &decline); err != nil {
		return Outcome{}, err
	}
	if card.Kind == KindText && !decline {
		if len(sub.Fields) == 0 {
			return Outcome{}, fmt.Errorf("%w: missing information", ErrInvalid)
		}
		allowed := map[string]bool{}
		for _, field := range card.Fields {
			allowed[field.ID] = true
		}
		for id, value := range sub.Fields {
			if !allowed[id] || strings.TrimSpace(value) == "" || secretField(id) {
				return Outcome{}, fmt.Errorf("%w: field", ErrInvalid)
			}
		}
	}
	mode := "continue"
	status := StatusProcessing
	if decline || contains(ids, ChoiceDecline) || contains(ids, ChoiceCancel) {
		mode = "cancel"
		status = StatusCancelled
		if contains(ids, ChoiceDecline) {
			status = StatusDeclined
		}
	} else if card.Kind == KindNativeHandoff && contains(ids, ChoiceCompleted) {
		mode = "verify"
	}
	card.Status = status
	card.InputPaused = false
	card.UpdatedAt = now
	if mode != "cancel" && status != StatusDeclined && status != StatusCancelled {
		card.ResumeCount++
	}
	card.Decision = &Decision{
		ChoiceIDs:        ids,
		Fields:           cloneFields(sub.Fields),
		OperationVersion: sub.OperationVersion,
		PayloadHash:      sub.PayloadHash,
		Decline:          decline || mode == "cancel",
		Mode:             mode,
		At:               now,
	}
	resume := card.ResumeCount > 0 && (mode == "continue" || mode == "verify")
	return outcomeOf(*card, resume, false), nil
}

func outcomeOf(card Card, resume, already bool) Outcome {
	mode := ""
	if card.Decision != nil {
		mode = card.Decision.Mode
	}
	var granted []string
	if mode == "continue" {
		if consequentialOps[card.Operation.Kind] && Grants(card, card.Operation.Kind) {
			granted = []string{card.Operation.Kind}
		}
	}
	return Outcome{
		Status:    card.Status,
		Resume:    resume && !already,
		Already:   already,
		Mode:      mode,
		TaskID:    card.TaskID,
		RunID:     card.RunID,
		RequestID: card.ID,
		BotID:     card.BotID,
		Done:      false,
		Granted:   granted,
	}
}

func checkTarget(card Card, sub Submission) error {
	if card.Kind != KindNativeHandoff {
		return nil
	}
	if card.Shot != nil {
		if strings.TrimSpace(sub.ShotID) != card.Shot.ID {
			return ErrStale
		}
		if !sub.CapturedAt.IsZero() && !sub.CapturedAt.UTC().Equal(card.Shot.CapturedAt.UTC()) {
			return ErrStale
		}
		if strings.TrimSpace(sub.LeaseID) != "" && sub.LeaseID != card.LeaseID {
			return ErrForbidden
		}
	}
	if card.Target == nil {
		return nil
	}
	if sub.TargetOrigin != "" && sub.TargetOrigin != card.Target.Origin {
		return ErrStale
	}
	if sub.TargetURL != "" && sub.TargetURL != card.Target.URL {
		return ErrStale
	}
	if sub.TargetFrame != "" && card.Target.Frame != "" && sub.TargetFrame != card.Target.Frame {
		return ErrStale
	}
	if card.Target.Frame == "" && sub.TargetFrame != "" {
		// The card never identified a frame. A frame id now would highlight
		// a spot the server did not bind.
		return ErrStale
	}
	return nil
}

func checkChoices(card Card, ids []string, decline *bool) error {
	known := map[string]bool{}
	for _, choice := range card.Choices {
		known[choice.ID] = true
	}
	switch card.Kind {
	case KindConfirm, KindChoice, KindNativeHandoff:
		if len(ids) != 1 || !known[ids[0]] {
			return fmt.Errorf("%w: choice", ErrInvalid)
		}
	case KindMulti:
		if len(ids) == 0 {
			return fmt.Errorf("%w: choice", ErrInvalid)
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !known[id] || seen[id] {
				return fmt.Errorf("%w: choice", ErrInvalid)
			}
			seen[id] = true
		}
	case KindText:
		if len(ids) > 0 {
			return fmt.Errorf("%w: text card has no choices", ErrInvalid)
		}
	}
	if contains(ids, ChoiceDecline) || contains(ids, ChoiceCancel) {
		*decline = true
	}
	return nil
}

// Desktop returns the computer a screenshot click may open.
// It never asks the caller to wake a desktop.
func (s *Store) Desktop(actor Actor, id, botID, leaseID string, captured time.Time) (DesktopBinding, error) {
	card, err := s.Get(id)
	if err != nil {
		return DesktopBinding{}, err
	}
	if !userOwns(actor, card) {
		return DesktopBinding{}, ErrForbidden
	}
	if strings.TrimSpace(botID) != card.BotID {
		return DesktopBinding{}, ErrForbidden
	}
	if card.LeaseID == "" || (card.DesktopMode != "isolated" && card.DesktopMode != "shared") {
		return DesktopBinding{}, fmt.Errorf("%w: this card has no desktop lease", ErrInvalid)
	}
	if strings.TrimSpace(leaseID) != card.LeaseID {
		return DesktopBinding{}, ErrForbidden
	}
	if card.Shot != nil && !captured.IsZero() && !captured.UTC().Equal(card.Shot.CapturedAt.UTC()) {
		return DesktopBinding{}, ErrStale
	}
	if card.Status != StatusPending && card.Status != StatusProcessing {
		return DesktopBinding{}, ErrConflict
	}
	return DesktopBinding{
		BotID:       card.BotID,
		TaskID:      card.TaskID,
		RunID:       card.RunID,
		LeaseID:     card.LeaseID,
		DesktopMode: card.DesktopMode,
		CapturedAt:  shotTime(card),
		ShotID:      shotID(card),
		Attach:      AttachWatch,
		Wake:        false,
	}, nil
}

func shotTime(card Card) time.Time {
	if card.Shot == nil {
		return time.Time{}
	}
	return card.Shot.CapturedAt
}

func shotID(card Card) string {
	if card.Shot == nil {
		return ""
	}
	return card.Shot.ID
}

// RecordResult stores a verified outcome. A click cannot call this.
// Done requires a non-empty evidence note from the host.
func (s *Store) RecordResult(id, evidence string, done bool) (Card, error) {
	if s == nil {
		return Card{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	card, _, err := s.get(id)
	if err != nil {
		return Card{}, err
	}
	if card.Status != StatusProcessing {
		return Card{}, ErrConflict
	}
	evidence = strings.TrimSpace(evidence)
	if done && evidence == "" {
		return Card{}, fmt.Errorf("%w: completion needs verified evidence", ErrInvalid)
	}
	now := s.clock()
	status := StatusProcessing
	if done {
		status = StatusDone
	}
	card.Status = status
	card.UpdatedAt = now
	card.Result = &Result{Status: status, Evidence: evidence, At: now}
	raw, err := json.Marshal(card)
	if err != nil {
		return Card{}, err
	}
	if _, err := s.db.Exec(`UPDATE chat_cards SET body=?, status=?, updated_at=? WHERE id=?`, string(raw), status, now.Format(time.RFC3339Nano), card.ID); err != nil {
		return Card{}, err
	}
	return card, nil
}

func compact(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func cloneFields(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
