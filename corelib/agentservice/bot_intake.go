package agentservice

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Intake decides what one new message does. The GUI only forwards the
// words and shows the result. A timeout or a bad answer does not start
// work: the message stays a discussion. Nothing is folded into a run the
// model did not name.

// BotIntakeRun is one task this bot is already doing.
type BotIntakeRun struct {
	RunID string `json:"run_id"`
	Task  string `json:"task"`
}

// BotIntakeInput is one message to judge.
type BotIntakeInput struct {
	BotID   string         `json:"bot_id"`
	Content string         `json:"content"`
	Lang    string         `json:"lang,omitempty"`
	Persona string         `json:"persona,omitempty"`
	Phase   string         `json:"phase,omitempty"`
	Earlier []string       `json:"earlier,omitempty"`
	Running []BotIntakeRun `json:"running,omitempty"`
}

// BotIntakeSchedule is a timer. The decision is made here. MaClawSrv writes
// the row onto its own clock and fires it. The GUI may show a copy; it does
// not start the turn.
type BotIntakeSchedule struct {
	Name            string `json:"name,omitempty"`
	Mode            string `json:"mode,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Hour            *int   `json:"hour,omitempty"`
	Minute          *int   `json:"minute,omitempty"`
	DayOfWeek       *int   `json:"day_of_week,omitempty"`
}

// BotIntakePlan is the current plan for this conversation. Empty fields
// are left as they were. A present empty list clears that list.
type BotIntakePlan struct {
	Goal        string    `json:"goal,omitempty"`
	Constraints *[]string `json:"constraints,omitempty"`
	Success     *[]string `json:"success,omitempty"`
	Open        *[]string `json:"open_questions,omitempty"`
}

// BotIntake is the decision.
//
// Kind is discuss, plan_amend, new, supplement, change, stop, pause,
// continue, reply, ask, schedule, or status. discuss and plan_amend never
// start a run. new with phase plan is a destructive order and still starts
// a plan-phase run. RunID names the live run when one applies. Instruction
// is the work order. Ack is the first sentence the person sees; empty
// means the caller shows its own neutral line.
type BotIntake struct {
	Kind        string             `json:"kind"`
	RunID       string             `json:"run_id,omitempty"`
	Instruction string             `json:"instruction,omitempty"`
	Ack         string             `json:"ack,omitempty"`
	Pace        string             `json:"pace,omitempty"`
	Told        string             `json:"told,omitempty"`
	Persona     string             `json:"persona,omitempty"`
	Schedule    *BotIntakeSchedule `json:"schedule,omitempty"`
	Plan        *BotIntakePlan     `json:"plan,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	// Phase is execute or plan. execute is the default. plan is only when
	// this message itself orders something destructive or irreversible.
	Phase string `json:"phase,omitempty"`
}

// BotIntakeClassifier answers the intake prompt with the model's raw text.
type BotIntakeClassifier func(ctx context.Context, system, user string) (string, error)

// BotIntakeBudget bounds the whole decision, including the first sentence.
// On timeout the message stays a discussion and Ack is empty.
var BotIntakeBudget = 2 * time.Second

const botIntakeChangeMark = "【改变做法】"

const botIntakeSystem = `You decide what one new message does for a desktop assistant. You do not touch the desktop and you have no tools. Return one JSON object and nothing else.

The object has these fields:
- kind: one of discuss, plan_amend, new, supplement, change, stop, pause, continue, reply, ask, schedule, status.
- run: the number of a running task this message acts on, or 0 when it acts on none.
- instruction: the work order for the desktop helper when there is desktop work already running, otherwise an empty string. For supplement or change it is the whole updated order, not only the new sentence. For discuss and plan_amend it is empty.
- ack: one short sentence the person sees at once, in the language named below, in your own words. It must not claim the work is already done. Empty only when kind is status.
- pace: discuss, direct, reply, ask, schedule, or status.
- persona: the whole identity to keep after this message, or an empty string when it did not change, or a single hyphen when they withdraw it.
- plan: an object for discuss and plan_amend. goal is one sentence. constraints and success are lists of short lines. open_questions lists what is still missing. Omit a list to leave it unchanged. An empty list clears it.
- schedule: an object only when kind is schedule. mode is create or cancel. name is a short stable name. For create, either interval_minutes is a positive number of minutes, or hour (0-23) and minute (0-59) name a clock time. day_of_week is 0 for Sunday through 6 for Saturday; omit it to run every day. For cancel, name is the timer to stop, or empty to stop every timer of this bot.

kind:
- discuss: the person is shaping work that has not started. This includes a first description and a sentence that only says to begin.
- plan_amend: the person changes a plan that is already being discussed and has not started.
- supplement: the message adds a constraint, detail, or material to a running task and does not change that task's goal.
- change: the message redirects a running task, so work already under way must take a new direction.
- stop: the message asks to stop or abandon a running task.
- pause: the message asks to hold a running task and resume it later, not to abandon it.
- continue: the message only asks to keep going on a running task and adds no new instruction.
- reply: small talk, thanks, or an identity, with no desktop work.
- ask: a fact only this person knows is missing, so the work cannot start.
- schedule: they want work at a later time or on a repeat, or they want that timer stopped.
- status: they ask how work is going or what it already produced. Do not invent the answer.
- new: only when this message itself orders something destructive or irreversible. Otherwise task talk is discuss or plan_amend.

When no task is running, do not use supplement, change, stop, pause, or continue.
Saying to start or confirm does not by itself make kind new. It stays discuss or plan_amend.
If you are unsure which running task a message belongs to, kind is discuss and run is 0.
Do not match the message against a list of phrases.

phase is execute or plan. execute is the default. Use plan only when this message itself orders something destructive or irreversible: deleting the person's own data, paying, wiping a machine, or sending a message to many other people. Fetching, writing, publishing, and continuing work already arranged are execute. A follow-up that continues arranged work or answers a previous question is kind continue or reply and phase execute. If you are unsure, phase is execute.`

// IntakeBotMessage decides what a message does. A classifier that fails,
// times out, or answers something unusable leaves the message as a
// discussion. It does not become a task.
func (s *Service) IntakeBotMessage(ctx context.Context, p Principal, instanceID string, in BotIntakeInput, classify BotIntakeClassifier) (BotIntake, error) {
	bot := strings.TrimSpace(in.BotID)
	content := strings.TrimSpace(in.Content)
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return BotIntake{}, err
	}
	if bot == "" || botOfClientSessionKey(bot) != bot {
		return BotIntake{}, ErrBotThreadsBadBot
	}
	if content == "" {
		return discussIntake("empty"), nil
	}
	running := liveIntakeRuns(in.Running)
	if classify == nil {
		return discussIntake("no classifier"), nil
	}
	user := intakeUserPromptClean(content, in, running)
	cctx, cancel := context.WithTimeout(ctx, BotIntakeBudget)
	defer cancel()
	type answer struct {
		raw string
		err error
	}
	done := make(chan answer, 1)
	go func() {
		raw, err := classify(cctx, botIntakeSystem, user)
		done <- answer{raw, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-cctx.Done():
		return discussIntake("timeout"), nil
	}
	if got.err != nil {
		return discussIntake("classifier unavailable"), nil
	}
	parsed, ok := parseBotIntake(got.raw)
	if !ok {
		return discussIntake("unreadable"), nil
	}
	return normalizeBotIntake(parsed, content, running), nil
}

func discussIntake(reason string) BotIntake {
	return BotIntake{Kind: "discuss", Pace: "discuss", Reason: reason}
}

func liveIntakeRuns(in []BotIntakeRun) []BotIntakeRun {
	var out []BotIntakeRun
	seen := map[string]bool{}
	for _, r := range in {
		id := strings.TrimSpace(r.RunID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, BotIntakeRun{RunID: id, Task: oneLineRunes(r.Task, 400)})
		if len(out) == 8 {
			break
		}
	}
	return out
}

func intakeUserPromptClean(content string, in BotIntakeInput, running []BotIntakeRun) string {
	var b strings.Builder
	lang := strings.TrimSpace(in.Lang)
	if lang == "" {
		lang = "unspecified"
	}
	phase := strings.TrimSpace(in.Phase)
	if phase == "" {
		phase = "unspecified"
	}
	b.WriteString("language: ")
	b.WriteString(oneLineRunes(lang, 40))
	b.WriteString("\nphase: ")
	b.WriteString(oneLineRunes(phase, 20))
	if persona := strings.TrimSpace(in.Persona); persona != "" {
		b.WriteString("\npersona already kept:\n")
		b.WriteString(oneLineRunes(persona, 400))
	}
	if len(in.Earlier) > 0 {
		b.WriteString("\nearlier messages:")
		n := 0
		for _, line := range in.Earlier {
			line = oneLineRunes(line, 200)
			if line == "" {
				continue
			}
			n++
			if n > 8 {
				break
			}
			b.WriteString("\n- ")
			b.WriteString(line)
		}
	}
	if len(running) == 0 {
		b.WriteString("\nrunning tasks: none")
	} else {
		b.WriteString("\nrunning tasks:")
		for i, r := range running {
			b.WriteString("\n")
			b.WriteString(itoa(i + 1))
			b.WriteString(". ")
			task := strings.TrimSpace(r.Task)
			if task == "" {
				task = "(no description)"
			}
			b.WriteString(oneLineRunes(task, 300))
		}
	}
	b.WriteString("\nnew message:\n")
	b.WriteString(oneLineRunes(content, 800))
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [8]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}

func parseBotIntake(raw string) (BotIntake, bool) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return BotIntake{}, false
	}
	var v struct {
		Kind        string             `json:"kind"`
		Run         *int               `json:"run"`
		Instruction string             `json:"instruction"`
		Ack         string             `json:"ack"`
		Pace        string             `json:"pace"`
		Told        string             `json:"told"`
		Persona     string             `json:"persona"`
		Schedule    *BotIntakeSchedule `json:"schedule"`
		Plan        *BotIntakePlan     `json:"plan"`
		Reason      string             `json:"reason"`
		Phase       string             `json:"phase"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &v) != nil {
		return BotIntake{}, false
	}
	run := 0
	if v.Run != nil {
		run = *v.Run
	}
	out := BotIntake{
		Kind:        strings.ToLower(strings.TrimSpace(v.Kind)),
		Instruction: strings.TrimSpace(v.Instruction),
		Ack:         oneLineRunes(v.Ack, 200),
		Pace:        strings.ToLower(strings.TrimSpace(v.Pace)),
		Told:        oneLineRunes(v.Told, 400),
		Persona:     strings.TrimSpace(v.Persona),
		Schedule:    v.Schedule,
		Plan:        clipIntakePlan(v.Plan),
		Reason:      oneLineRunes(v.Reason, 120),
		Phase:       strings.ToLower(strings.TrimSpace(v.Phase)),
	}
	if run < 0 {
		run = 0
	}
	out.RunID = itoa(run)
	return out, true
}

func normalizeBotIntake(in BotIntake, content string, running []BotIntakeRun) BotIntake {
	kind := canonicalIntakeKind(in.Kind, in.Pace)
	idx := 0
	if n, ok := atoi(in.RunID); ok {
		idx = n
	}
	var runID string
	if idx >= 1 && idx <= len(running) {
		runID = running[idx-1].RunID
	}
	needsRun := kind == "supplement" || kind == "change" || kind == "stop" || kind == "pause" || kind == "continue"
	if needsRun && runID == "" && len(running) == 1 {
		runID = running[0].RunID
	}
	if needsRun && runID == "" {
		return discussIntake("no run")
	}
	if kind == "continue" && len(running) == 0 {
		held := discussIntake("nothing to continue")
		held.Ack = in.Ack
		held.Plan = in.Plan
		return held
	}
	instruction := strings.TrimSpace(in.Instruction)
	phase := intakePhase(kind, in.Phase)
	// Ordinary task talk is a discussion. A destructive order keeps the
	// plan-phase run it already had.
	if kind == "new" && phase != "plan" {
		kind = "discuss"
		if in.Plan == nil {
			in.Plan = &BotIntakePlan{}
		}
		if strings.TrimSpace(in.Plan.Goal) == "" && instruction != "" {
			in.Plan.Goal = oneLineRunes(instruction, 300)
		}
		instruction = ""
		phase = ""
	}
	if instruction == "" && (kind == "new" || kind == "supplement" || kind == "change") {
		instruction = content
	}
	if kind == "change" && instruction != "" && !strings.HasPrefix(instruction, botIntakeChangeMark) {
		instruction = botIntakeChangeMark + instruction
	}
	if r := []rune(instruction); len(r) > 4000 {
		instruction = string(r[:4000])
	}
	out := BotIntake{
		Kind:        kind,
		RunID:       runID,
		Instruction: instruction,
		Ack:         in.Ack,
		Pace:        in.Pace,
		Told:        in.Told,
		Persona:     clipPersona(in.Persona),
		Reason:      in.Reason,
	}
	if kind == "schedule" {
		sched, ok := validIntakeSchedule(in.Schedule)
		if !ok {
			held := discussIntake("bad schedule")
			held.Ack = in.Ack
			return held
		}
		out.Schedule = sched
		out.Pace = "schedule"
	}
	if out.Pace == "" {
		switch kind {
		case "reply":
			out.Pace = "reply"
		case "ask":
			out.Pace = "ask"
		case "status":
			out.Pace = "status"
		case "schedule":
			out.Pace = "schedule"
		default:
			out.Pace = "direct"
		}
	}
	if kind == "reply" || kind == "ask" {
		if strings.TrimSpace(out.Told) == "" {
			out.Told = out.Ack
		}
		if strings.TrimSpace(out.Told) == "" && strings.TrimSpace(out.Ack) == "" {
			return discussIntake("empty reply")
		}
		out.Instruction = ""
	}
	if kind == "status" {
		out.Instruction = oneLineRunes(instruction, 200)
		out.Ack = ""
	}
	if kind == "continue" {
		out.Instruction = ""
	}
	if kind == "discuss" || kind == "plan_amend" {
		out.Instruction = ""
		out.Plan = in.Plan
		if out.Pace == "" || out.Pace == "direct" {
			out.Pace = "discuss"
		}
		phase = ""
	}
	out.Phase = phase
	return out
}

func clipIntakePlan(in *BotIntakePlan) *BotIntakePlan {
	if in == nil {
		return nil
	}
	out := &BotIntakePlan{Goal: oneLineRunes(in.Goal, 300)}
	out.Constraints = clipPlanLines(in.Constraints)
	out.Success = clipPlanLines(in.Success)
	out.Open = clipPlanLines(in.Open)
	return out
}

func clipPlanLines(in *[]string) *[]string {
	if in == nil {
		return nil
	}
	lines := make([]string, 0, len(*in))
	for _, line := range *in {
		line = oneLineRunes(line, 200)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == 8 {
			break
		}
	}
	return &lines
}

// intakePhase is the model's phase, with execute as the only default.
// continue and reply are follow-ups and always run. Anything other than
// the word plan is execute. There is no keyword list over the message.
func intakePhase(kind, raw string) string {
	if kind == "discuss" || kind == "plan_amend" {
		return ""
	}
	if kind == "continue" || kind == "reply" {
		return "execute"
	}
	if strings.EqualFold(strings.TrimSpace(raw), "plan") {
		return "plan"
	}
	return "execute"
}

func canonicalIntakeKind(kind, pace string) string {
	switch kind {
	case "discuss", "plan_amend", "new", "supplement", "change", "stop", "pause", "continue", "reply", "ask", "schedule", "status":
		return kind
	case "plan":
		return "plan_amend"
	case "chat", "small_talk":
		return "reply"
	case "new_task", "parallel", "separate":
		return "new"
	case "modify", "replace", "redirect":
		return "change"
	}
	switch pace {
	case "reply":
		return "reply"
	case "ask":
		return "ask"
	case "schedule":
		return "schedule"
	case "status":
		return "status"
	}
	return "new"
}

func validIntakeSchedule(s *BotIntakeSchedule) (*BotIntakeSchedule, bool) {
	if s == nil {
		return nil, false
	}
	mode := strings.ToLower(strings.TrimSpace(s.Mode))
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "cancel" {
		return nil, false
	}
	out := &BotIntakeSchedule{Name: oneLineRunes(s.Name, 80), Mode: mode}
	if mode == "cancel" {
		return out, true
	}
	if s.IntervalMinutes > 0 {
		if s.IntervalMinutes > 7*24*60 {
			return nil, false
		}
		out.IntervalMinutes = s.IntervalMinutes
		return out, true
	}
	if s.Hour == nil || s.Minute == nil {
		return nil, false
	}
	h, m := *s.Hour, *s.Minute
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return nil, false
	}
	out.Hour, out.Minute = &h, &m
	if s.DayOfWeek != nil {
		d := *s.DayOfWeek
		if d < 0 || d > 6 {
			return nil, false
		}
		out.DayOfWeek = &d
	}
	return out, true
}

func clipPersona(s string) string {
	s = strings.TrimSpace(s)
	if s == "-" {
		return s
	}
	return oneLineRunes(s, 500)
}

func atoi(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
