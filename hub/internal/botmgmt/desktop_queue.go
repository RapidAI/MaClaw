package botmgmt

import (
	"context"
	"errors"
	"strings"
	"time"
)

// A bot's waiting messages. The GUI used to keep this queue. Hub keeps it
// so a full bot, or a person who still has the keyboard, does not lose the
// next message. At most desktopQueueMax items, dropped after desktopQueueTTL.

const (
	desktopQueueMax = 20
	desktopQueueTTL = 6 * time.Hour
)

// ErrDesktopQueueFull means this bot already has desktopQueueMax messages waiting.
var ErrDesktopQueueFull = errors.New("这个机器人排队的事已经满了。")

type desktopQueueItem struct {
	ClientMessageID string    `json:"client_message_id"`
	Content         string    `json:"content"`
	Phase           string    `json:"phase,omitempty"`
	ContinueSession string    `json:"continue_session,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	RunID           string    `json:"run_id,omitempty"`
	Instruction     string    `json:"instruction,omitempty"`
	Ack             string    `json:"ack,omitempty"`
	EnqueuedAt      time.Time `json:"enqueued_at"`
	Held            bool      `json:"held,omitempty"`
	State           string    `json:"state,omitempty"`
}

// DesktopQueueView is what a relay polls.
type DesktopQueueView struct {
	State    string `json:"state"`
	RunID    string `json:"run_id,omitempty"`
	Held     bool   `json:"held"`
	Position int    `json:"position"`
}

func desktopQueueKey(tenantID, userID, botID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(botID)
}

func (s *Service) ensureQueueDrainLoop() {
	if s == nil || s.queueDrainOff {
		return
	}
	s.queueDrainOnce.Do(func() {
		go s.desktopQueueLoop()
	})
}

func (s *Service) desktopQueueLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		s.drainAllDesktopQueues()
	}
}

func (s *Service) drainAllDesktopQueues() {
	if s == nil {
		return
	}
	s.mu.Lock()
	type pair struct{ tenant, user, bot string }
	var jobs []pair
	for key := range s.desktopQueues {
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 {
			continue
		}
		jobs = append(jobs, pair{parts[0], parts[1], parts[2]})
	}
	s.mu.Unlock()
	for _, j := range jobs {
		s.drainDesktopQueue(j.tenant, j.user, j.bot)
	}
}

// enqueueDesktopMessage parks one message. A repeat of the same client
// message id returns the position it already has. held means the person
// still has the keyboard.
func (s *Service) enqueueDesktopMessage(tenantID, userID, botID string, item desktopQueueItem) (int, error) {
	if s == nil {
		return 0, ErrSettingsUnavailable
	}
	item.ClientMessageID = strings.TrimSpace(item.ClientMessageID)
	item.Content = strings.TrimSpace(item.Content)
	if item.ClientMessageID == "" || item.Content == "" {
		return 0, ErrInvalidInput
	}
	if item.EnqueuedAt.IsZero() {
		item.EnqueuedAt = s.now()
	}
	if item.Held {
		item.State = "held"
	} else if item.State == "" {
		item.State = "queued"
	}
	key := desktopQueueKey(tenantID, userID, botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	s.dropExpiredQueueLocked(key)
	items := s.desktopQueues[key]
	for i, existing := range items {
		if existing != nil && existing.ClientMessageID == item.ClientMessageID && (existing.State == "queued" || existing.State == "held") {
			return i + 1, nil
		}
	}
	waiting := 0
	for _, existing := range items {
		if existing != nil && (existing.State == "queued" || existing.State == "held") {
			waiting++
		}
	}
	if waiting >= desktopQueueMax {
		return 0, ErrDesktopQueueFull
	}
	if s.desktopQueues == nil {
		s.desktopQueues = map[string][]*desktopQueueItem{}
	}
	copied := item
	s.desktopQueues[key] = append(items, &copied)
	s.persistDesktopState(tenantID)
	s.ensureQueueDrainLoop()
	return waiting + 1, nil
}

func (s *Service) dropExpiredQueueLocked(key string) {
	items := s.desktopQueues[key]
	if len(items) == 0 {
		return
	}
	now := s.now()
	kept := items[:0]
	for _, item := range items {
		if item == nil || now.Sub(item.EnqueuedAt) > desktopQueueTTL {
			continue
		}
		kept = append(kept, item)
	}
	if s.desktopQueues == nil {
		s.desktopQueues = map[string][]*desktopQueueItem{}
	}
	s.desktopQueues[key] = kept
}

// DesktopQueueStatus reports one parked or recently started message.
func (s *Service) DesktopQueueStatus(tenantID, userID, botID, clientMessageID string) DesktopQueueView {
	clientMessageID = strings.TrimSpace(clientMessageID)
	if s == nil || clientMessageID == "" {
		return DesktopQueueView{State: "missing"}
	}
	key := desktopQueueKey(tenantID, userID, botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	s.dropExpiredQueueLocked(key)
	pos := 0
	for _, item := range s.desktopQueues[key] {
		if item == nil {
			continue
		}
		if item.State == "queued" || item.State == "held" {
			pos++
		}
		if item.ClientMessageID != clientMessageID {
			continue
		}
		state := item.State
		if state == "" {
			state = "queued"
		}
		return DesktopQueueView{State: state, RunID: item.RunID, Held: item.Held || state == "held", Position: pos}
	}
	if runID, done := s.queueAdmitLocked(tenantID, userID, botID, clientMessageID); runID != "" {
		state := "running"
		if done {
			state = "done"
		}
		return DesktopQueueView{State: state, RunID: runID}
	}
	return DesktopQueueView{State: "missing"}
}

func (s *Service) queueAdmitLocked(tenantID, userID, botID, clientMessageID string) (string, bool) {
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		id := strings.TrimSpace(state.clientMessageID)
		runID := strings.TrimSpace(state.runID)
		done := state.done
		state.mu.Unlock()
		if id == clientMessageID && runID != "" {
			return runID, done
		}
	}
	return "", false
}

// drainDesktopQueue starts parked messages whose bot has a free slot and
// whose person does not still have the keyboard. It is safe to call often.
func (s *Service) drainDesktopQueue(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	key := desktopQueueKey(tenantID, userID, botID)
	s.mu.Lock()
	if s.queueDraining == nil {
		s.queueDraining = map[string]bool{}
	}
	if s.queueDraining[key] {
		s.mu.Unlock()
		return
	}
	s.queueDraining[key] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.queueDraining, key)
		s.mu.Unlock()
	}()
	for {
		item := s.popDesktopQueue(tenantID, userID, botID)
		if item == nil {
			return
		}
		if err := s.dispatchQueuedItem(tenantID, userID, botID, item); err != nil {
			if err == ErrDesktopBusy {
				s.pushFrontDesktopQueue(tenantID, userID, botID, item)
			}
			return
		}
	}
}

func (s *Service) popDesktopQueue(tenantID, userID, botID string) *desktopQueueItem {
	key := desktopQueueKey(tenantID, userID, botID)
	keyboard := s.personHasBotKeyboard(tenantID, userID, botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpiredQueueLocked(key)
	items := s.desktopQueues[key]
	var item *desktopQueueItem
	for len(items) > 0 {
		head := items[0]
		items = items[1:]
		if head == nil || head.State == "running" || head.State == "done" {
			continue
		}
		if runID, _ := s.queueAdmitLocked(tenantID, userID, botID, head.ClientMessageID); runID != "" {
			head.State = "running"
			head.RunID = runID
			items = append(items, head)
			continue
		}
		if (head.Held || head.State == "held") && keyboard {
			items = append([]*desktopQueueItem{head}, items...)
			s.desktopQueues[key] = items
			return nil
		}
		item = head
		break
	}
	if s.desktopQueues == nil {
		return item
	}
	s.desktopQueues[key] = items
	if item != nil {
		s.persistDesktopState(tenantID)
	}
	return item
}

func (s *Service) pushFrontDesktopQueue(tenantID, userID, botID string, item *desktopQueueItem) {
	if s == nil || item == nil {
		return
	}
	key := desktopQueueKey(tenantID, userID, botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopQueues == nil {
		s.desktopQueues = map[string][]*desktopQueueItem{}
	}
	s.desktopQueues[key] = append([]*desktopQueueItem{item}, s.desktopQueues[key]...)
	s.persistDesktopState(tenantID)
}

func (s *Service) dispatchQueuedItem(tenantID, userID, botID string, item *desktopQueueItem) error {
	if item == nil {
		return nil
	}
	kind := strings.TrimSpace(item.Kind)
	if kind == "" {
		kind = "new"
	}
	ctx := contextReplayDesktop(context.Background())
	switch kind {
	case "supplement", "change":
		if s.runStillLive(tenantID, userID, botID, item.RunID) {
			text := firstNonEmptyString(item.Instruction, item.Content)
			if _, err := s.SupplementDesktopRun(ctx, tenantID, userID, botID, item.RunID, text); err == nil {
				s.finishQueued(tenantID, userID, botID, item, "done", item.RunID)
				return nil
			}
		}
		kind = "new"
	case "stop":
		if item.RunID != "" && s.CancelDesktopRun(ctx, tenantID, userID, botID, item.RunID) == nil {
			s.finishQueued(tenantID, userID, botID, item, "done", item.RunID)
			return nil
		}
		kind = "new"
	case "pause":
		if item.RunID != "" {
			if _, err := s.PauseDesktopRun(ctx, tenantID, userID, botID, item.RunID, "", 0); err == nil || err == ErrPauseUnsupported {
				s.finishQueued(tenantID, userID, botID, item, "done", item.RunID)
				return nil
			}
		}
		kind = "new"
	case "continue":
		if item.RunID != "" && s.runStillLive(tenantID, userID, botID, item.RunID) {
			_, _ = s.ResumeDesktopRun(ctx, tenantID, userID, botID, item.RunID)
			s.finishQueued(tenantID, userID, botID, item, "done", item.RunID)
			return nil
		}
		kind = "new"
	case "reply", "ask", "status", "schedule":
		s.finishQueued(tenantID, userID, botID, item, "done", "")
		return nil
	}
	text := firstNonEmptyString(item.Instruction, item.Content)
	admission, err := s.AdmitDesktopMessageAct(ctx, tenantID, userID, botID, text, item.Phase, item.ClientMessageID, item.ContinueSession, "")
	if err != nil {
		return err
	}
	s.finishQueued(tenantID, userID, botID, item, "running", admission.RunID)
	if strings.HasPrefix(item.ClientMessageID, "xdel-") {
		noteID := strings.TrimPrefix(item.ClientMessageID, "xdel-")
		s.markDelegateRun(tenantID, userID, botID, noteID, firstNonEmptyString(admission.RunID, item.ClientMessageID))
	}
	return nil
}

func (s *Service) finishQueued(tenantID, userID, botID string, item *desktopQueueItem, state, runID string) {
	if item == nil {
		return
	}
	item.State = state
	if runID != "" {
		item.RunID = runID
	}
	item.Held = false
	key := desktopQueueKey(tenantID, userID, botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopQueues == nil {
		s.desktopQueues = map[string][]*desktopQueueItem{}
	}
	// Keep a short record so a poll after the pop still sees running or done.
	s.desktopQueues[key] = append(s.desktopQueues[key], item)
	s.persistDesktopState(tenantID)
}

func (s *Service) runStillLive(tenantID, userID, botID, runID string) bool {
	runID = strings.TrimSpace(runID)
	if s == nil || runID == "" {
		return false
	}
	s.mu.Lock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return !state.done && strings.TrimSpace(state.runID) != ""
}

func (s *Service) personHasBotKeyboard(tenantID, userID, botID string) bool {
	if s == nil {
		return false
	}
	dUser := strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	if rec, err := s.load(context.Background(), tenantID); err == nil {
		if i := indexOf(rec.Bots, botID); i >= 0 {
			dUser = desktopUserForBot(userID, rec.Bots[i])
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, dUser)
	return s.desktopAwaiting[key] && s.desktopHeld[key] == botID
}

type desktopNoQueueKey struct{}

func contextWithoutDesktopQueue(ctx context.Context) context.Context {
	return context.WithValue(ctx, desktopNoQueueKey{}, true)
}

func desktopQueueSuppressed(ctx context.Context) bool {
	return ctx != nil && ctx.Value(desktopNoQueueKey{}) != nil
}

type desktopReplayKey struct{}

// contextReplayDesktop admits a message Hub already decided, without asking
// again and without parking it a second time.
func contextReplayDesktop(ctx context.Context) context.Context {
	return context.WithValue(contextWithoutDesktopQueue(ctx), desktopReplayKey{}, true)
}

func desktopReplay(ctx context.Context) bool {
	return ctx != nil && ctx.Value(desktopReplayKey{}) != nil
}
