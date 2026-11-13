package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// desktopIntakeBudget bounds the srv decision. The ack is part of that
// answer. On timeout or error the message is a new task.
var desktopIntakeBudget = 2 * time.Second

// DesktopSchedule is a timer MaClawSrv decided and armed on its own clock
// during bot-intake. Hub returns it so the GUI can show the ack. Hub does
// not keep a second clock, and the GUI must not start the turn.
type DesktopSchedule struct {
	Name            string `json:"name,omitempty"`
	Mode            string `json:"mode,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Hour            *int   `json:"hour,omitempty"`
	Minute          *int   `json:"minute,omitempty"`
	DayOfWeek       *int   `json:"day_of_week,omitempty"`
}

type desktopIntake struct {
	Kind        string           `json:"kind"`
	RunID       string           `json:"run_id"`
	Instruction string           `json:"instruction"`
	Ack         string           `json:"ack"`
	Pace        string           `json:"pace"`
	Told        string           `json:"told"`
	Persona     string           `json:"persona"`
	Schedule    *DesktopSchedule `json:"schedule"`
	Reason      string           `json:"reason"`
	Phase       string           `json:"phase"`
}

type desktopRunBrief struct {
	RunID    string
	Task     string
	Status   string
	Progress string
}

// desktopResumeWindow is how long a cancelled, failed, or interrupted run
// stays available for "继续".
const desktopResumeWindow = 30 * time.Minute

type desktopSteerRequest struct {
	TenantID        string
	UserID          string
	DesktopUser     string
	BotID           string
	InstanceID      string
	Content         string
	Phase           string
	ClientMessageID string
	ContinueSession string
	Action          string
	Admit           bool
	Rec             *record
	TokenRec        *record
	Token           *string
	HaveToken       bool
}

// steerDesktopMessage applies the server decision. handled means the caller
// returns admission and does not start a run. Otherwise content is what the
// run should be given (the model's order, or the person's words).
func (s *Service) steerDesktopMessage(ctx context.Context, in desktopSteerRequest) (string, BotAdmission, bool) {
	content := strings.TrimSpace(in.Content)
	if s == nil || content == "" || desktopReplay(ctx) || hubStartedTurn(in.ClientMessageID) {
		return content, BotAdmission{}, false
	}
	defer s.rememberDesktopTalk(in.TenantID, in.UserID, in.BotID, content)
	explicitNew := desktopCanonicalContinue(in.BotID, in.ContinueSession) == strings.TrimSpace(in.BotID)
	actionReturn := strings.EqualFold(strings.TrimSpace(in.Action), "return")
	running, earlier := s.desktopIntakeContext(in.TenantID, in.UserID, in.BotID)
	decision := desktopIntake{Kind: "new", Instruction: content, Pace: "direct", Reason: "explicit"}
	if !explicitNew && !actionReturn {
		decision = s.fetchDesktopIntake(ctx, in, running, earlier)
	}
	if supplementOff() && (decision.Kind == "supplement" || decision.Kind == "change") {
		decision.Kind = "new"
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
		decision.RunID = ""
	}
	if decision.RunID != "" && !briefHas(running, decision.RunID) {
		decision.Kind = "new"
		decision.RunID = ""
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
	}
	if strings.TrimSpace(decision.Phase) != "plan" {
		decision.Phase = "execute"
	}
	base := BotAdmission{
		Kind:            decision.Kind,
		Ack:             decision.Ack,
		Phase:           decision.Phase,
		RunID:           decision.RunID,
		ClientMessageID: in.ClientMessageID,
		Schedule:        decision.Schedule,
	}
	if in.Admit && !actionReturn && decision.Kind == "new" && s.personHasBotKeyboard(in.TenantID, in.UserID, in.BotID) {
		pos, err := s.enqueueDesktopMessage(in.TenantID, in.UserID, in.BotID, desktopQueueItem{
			ClientMessageID: in.ClientMessageID,
			Content:         content,
			Phase:           in.Phase,
			ContinueSession: in.ContinueSession,
			Kind:            "new",
			Instruction:     content,
			Ack:             decision.Ack,
			Held:            true,
			Secrets:         desktopSecretBag(ctx),
		})
		if err == nil {
			base.Accepted = true
			base.Queued = true
			base.Held = true
			base.Position = pos
			base.Kind = "new"
			return content, base, true
		}
	}
	switch decision.Kind {
	case "supplement", "change":
		text := firstNonEmptyString(decision.Instruction, content)
		if _, err := s.SupplementDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, text); err == nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			return content, base, true
		}
	case "stop":
		if s.CancelDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID) == nil {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "pause":
		_, err := s.PauseDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, "", 0)
		if err == nil || err == ErrPauseUnsupported {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "continue":
		if decision.RunID != "" {
			_, _ = s.ResumeDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID)
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "reply", "ask":
		text := firstNonEmptyString(decision.Told, decision.Ack)
		if text != "" {
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			return content, base, true
		}
	case "status":
		text, _, err := s.DesktopTaskQuery(ctx, in.TenantID, in.UserID, in.BotID, firstNonEmptyString(decision.Instruction, content), "")
		if err == nil && strings.TrimSpace(text) != "" {
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			return content, base, true
		}
	case "schedule":
		if decision.Schedule != nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			base.RunID = ""
			return content, base, true
		}
	}
	next := strings.TrimSpace(decision.Instruction)
	if next == "" {
		next = content
	}
	next = agentservice.FoldEarlierIntoInstruction(next, earlier)
	if content != "" && !strings.Contains(next, content) {
		next += "\n原话：" + content
	}
	base.Kind = "new"
	base.RunID = ""
	return next, base, false
}

func supplementOff() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_SUPPLEMENT")), "off")
}

func briefHas(running []desktopRunBrief, runID string) bool {
	for _, r := range running {
		if r.RunID == runID {
			return true
		}
	}
	return false
}

func (s *Service) botHasLiveRun(tenantID, userID, botID string) bool {
	return len(s.desktopLiveRunBriefs(tenantID, userID, botID)) > 0
}

func (s *Service) desktopLiveRunBriefs(tenantID, userID, botID string) []desktopRunBrief {
	if s == nil {
		return nil
	}
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []desktopRunBrief
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		done := state.done
		runID := strings.TrimSpace(state.runID)
		task := state.task
		state.mu.Unlock()
		if done || runID == "" {
			continue
		}
		status := "running"
		if s.runPauseStatus(tenantID, userID, botID, runID) == "paused" {
			status = "paused"
		}
		out = append(out, desktopRunBrief{RunID: runID, Task: task, Status: status})
	}
	return out
}

func (s *Service) runPauseStatus(tenantID, userID, botID, runID string) string {
	if s == nil {
		return ""
	}
	if v, ok := s.runPauses.Load(runPauseKey(tenantID, userID, botID, runID)); ok {
		if st, _ := v.(RunPauseState); st.Paused {
			return "paused"
		}
	}
	return ""
}

func resumableDesktopStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled", "failed", "interrupted":
		return true
	}
	return false
}

// desktopIntakeContext is the live runs plus recently stopped ones, and a
// few earlier task lines so intake can tell "继续" from a new chore.
func (s *Service) desktopIntakeContext(tenantID, userID, botID string) ([]desktopRunBrief, []string) {
	live := s.desktopLiveRunBriefs(tenantID, userID, botID)
	if s == nil {
		return live, nil
	}
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	now := s.now()
	s.mu.Lock()
	var stopped []desktopRunBrief
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		done, status, task, runID, progress, finished := state.done, state.status, state.task, state.runID, state.progressWhere, state.finished
		state.mu.Unlock()
		if !done || !resumableDesktopStatus(status) || strings.TrimSpace(runID) == "" {
			continue
		}
		if !finished.IsZero() && now.Sub(finished) > desktopResumeWindow {
			continue
		}
		stopped = append(stopped, desktopRunBrief{RunID: runID, Task: task, Status: status, Progress: progress})
	}
	s.mu.Unlock()
	out := append(live, stopped...)
	var earlier []string
	seen := map[string]bool{}
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] || len(earlier) == 8 {
			return
		}
		seen[line] = true
		earlier = append(earlier, line)
	}
	for _, line := range s.desktopTalkLines(tenantID, userID, botID) {
		add(line)
	}
	for _, r := range out {
		line := strings.TrimSpace(r.Task)
		if line == "" {
			continue
		}
		if r.Status != "" && r.Status != "running" {
			line += " [" + r.Status + "]"
		}
		add(line)
	}
	return out, earlier
}

type desktopTalkLog struct {
	mu    sync.Mutex
	lines []string
}

var desktopTalks sync.Map

func desktopTalkKey(s *Service, tenantID, userID, botID string) string {
	_ = s
	return tenantID + "\x00" + userID + "\x00" + botID
}

func (s *Service) desktopTalkLines(tenantID, userID, botID string) []string {
	if s == nil {
		return nil
	}
	v, ok := desktopTalks.Load(desktopTalkKey(s, tenantID, userID, botID))
	if !ok {
		return nil
	}
	log := v.(*desktopTalkLog)
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.lines...)
}

func (s *Service) rememberDesktopTalk(tenantID, userID, botID, line string) {
	line = strings.TrimSpace(line)
	if s == nil || line == "" {
		return
	}
	key := desktopTalkKey(s, tenantID, userID, botID)
	v, _ := desktopTalks.LoadOrStore(key, &desktopTalkLog{})
	log := v.(*desktopTalkLog)
	log.mu.Lock()
	if n := len(log.lines); n > 0 && log.lines[n-1] == line {
		log.mu.Unlock()
		return
	}
	log.lines = append(log.lines, line)
	if len(log.lines) > 12 {
		log.lines = append([]string(nil), log.lines[len(log.lines)-12:]...)
	}
	log.mu.Unlock()
	pruneDesktopTalks()
}

func pruneDesktopTalks() {
	n := 0
	desktopTalks.Range(func(key, value any) bool {
		n++
		log, _ := value.(*desktopTalkLog)
		if log == nil {
			desktopTalks.Delete(key)
			return true
		}
		log.mu.Lock()
		empty := len(log.lines) == 0
		log.mu.Unlock()
		if empty {
			desktopTalks.Delete(key)
		}
		return true
	})
	if n <= 256 {
		return
	}
	extra := n - 256
	desktopTalks.Range(func(key, _ any) bool {
		if extra <= 0 {
			return false
		}
		desktopTalks.Delete(key)
		extra--
		return true
	})
}

func (s *Service) fetchDesktopIntake(ctx context.Context, in desktopSteerRequest, running []desktopRunBrief, earlier []string) desktopIntake {
	fallback := desktopIntake{Kind: "new", Instruction: strings.TrimSpace(in.Content), Pace: "direct", Phase: "execute", Reason: "fallback"}
	if s == nil || strings.TrimSpace(in.InstanceID) == "" {
		return fallback
	}
	rec := in.TokenRec
	token := in.Token
	if !in.HaveToken || rec == nil || token == nil || strings.TrimSpace(*token) == "" {
		fresh, tk, err := s.ownerToken(ctx, in.TenantID, in.UserID)
		if err != nil {
			fallback.Reason = "no token"
			return fallback
		}
		rec = &fresh
		token = &tk
	}
	budget := desktopIntakeBudget
	if s.messageTimeout > 0 && s.messageTimeout/4 < budget {
		budget = s.messageTimeout / 4
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	body := map[string]any{
		"bot_id":  in.BotID,
		"content": in.Content,
		"phase":   in.Phase,
	}
	if len(running) > 0 {
		rows := make([]map[string]string, 0, len(running))
		for _, r := range running {
			row := map[string]string{"run_id": r.RunID, "task": r.Task}
			if r.Status != "" {
				row["status"] = r.Status
			}
			if r.Progress != "" {
				row["progress"] = r.Progress
			}
			rows = append(rows, row)
		}
		body["running"] = rows
	}
	if len(earlier) > 0 {
		body["earlier"] = earlier
	}
	var out desktopIntake
	path := "/api/v1/instances/" + url.PathEscape(in.InstanceID) + "/bot-intake"
	err := s.authAsOwner(rctx, in.TenantID, in.UserID, rec, token, http.MethodPost, path, body, &out)
	if err != nil || strings.TrimSpace(out.Kind) == "" {
		if rctx.Err() != nil {
			fallback.Reason = "timeout"
		}
		return fallback
	}
	out.Kind = strings.ToLower(strings.TrimSpace(out.Kind))
	out.RunID = strings.TrimSpace(out.RunID)
	if strings.TrimSpace(out.Phase) != "plan" {
		out.Phase = "execute"
	}
	return out
}
