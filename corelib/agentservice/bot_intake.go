package agentservice

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// Intake decides what one new message does. The GUI only forwards the
// words and shows the result. On a timeout or a bad answer the message is
// a new task: nothing is folded into a run the model did not name.

// BotIntakeRun is one task this bot is already doing.
type BotIntakeRun struct {
	RunID string `json:"run_id"`
	Task  string `json:"task"`
}

// BotIntakeInput is one message to judge.
type BotIntakeInput struct {
	BotID   string           `json:"bot_id"`
	Content string           `json:"content"`
	Lang    string           `json:"lang,omitempty"`
	Persona string           `json:"persona,omitempty"`
	Phase   string           `json:"phase,omitempty"`
	Earlier []string         `json:"earlier,omitempty"`
	Running []BotIntakeRun   `json:"running,omitempty"`
	Timers  []BotIntakeTimer `json:"timers,omitempty"`
	// NoDesktop marks a bot created with desktop_mode=none. The decision
	// prompt then names the bot's real surface, so a rewritten instruction
	// cannot order desktop work the bot has no way to do.
	NoDesktop bool `json:"no_desktop,omitempty"`
}

// BotIntakeTimer is a timer this bot already has. A later message that only
// adds a place or a file form updates this row instead of starting a job.
type BotIntakeTimer struct {
	Name            string `json:"name,omitempty"`
	Action          string `json:"action,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Hour            int    `json:"hour,omitempty"`
	Minute          int    `json:"minute,omitempty"`
	DayOfWeek       int    `json:"day_of_week,omitempty"`
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

// BotIntake is the decision.
//
// Kind is new, supplement, change, stop, pause, continue, reply, ask,
// schedule, or status. RunID names the live run for the first six when
// one applies. Instruction is the work order. Ack is the first sentence
// the person sees; empty means the caller shows its own neutral line.
type BotIntake struct {
	Kind        string             `json:"kind"`
	RunID       string             `json:"run_id,omitempty"`
	Instruction string             `json:"instruction,omitempty"`
	Ack         string             `json:"ack,omitempty"`
	Pace        string             `json:"pace,omitempty"`
	Told        string             `json:"told,omitempty"`
	Persona     string             `json:"persona,omitempty"`
	Schedule    *BotIntakeSchedule `json:"schedule,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	// Phase is execute or plan. execute is the default. plan is a destructive
	// order, or a task still being specified with the person. A specifying
	// plan is not shown as a mode: kind stays ask until the spec is settled.
	Phase string `json:"phase,omitempty"`
	// Spec is true while this message is specifying a task with the person,
	// or is the settled spec that should now run. The instruction is the
	// spec. The person sees ack, not a mode name.
	Spec bool `json:"spec,omitempty"`
}

// BotIntakeClassifier answers the intake prompt with the model's raw text.
type BotIntakeClassifier func(ctx context.Context, system, user string) (string, error)

// BotIntakeBudget bounds the whole decision, including the first sentence.
// Hub cancels this call at 5s. The decision finishes well inside that so an
// open spec is held and that answer reaches Hub, instead of Hub starting the
// raw words. A slow model is better than dropping the first sentence, so the
// budget leaves room for a couple of seconds of LLM latency.
var BotIntakeBudget = 3000 * time.Millisecond

const botIntakeChangeMark = "【改变做法】"

const botIntakeSystem = `You decide what one new message does for a desktop assistant. You do not touch the desktop and you have no tools. Return one JSON object and nothing else.

The object has these fields:
- kind: one of new, supplement, change, stop, pause, continue, reply, ask, schedule, status.
- run: the number of a running task this message acts on, or 0 when it acts on none.
- instruction: the work order for the desktop helper when there is desktop work, otherwise an empty string. For supplement or change it is the whole updated order, not only the new sentence.
- ack: one short sentence the person sees at once, in the language named below, in your own words. It must not claim the work is already done. Empty only when kind is status.
- pace: direct, reply, ask, schedule, or status.
- persona: the whole identity to keep after this message, or an empty string when it did not change, or a single hyphen when they withdraw it.
- schedule: an object only when kind is schedule. mode is create or cancel. name is a short stable name. For create, either interval_minutes is a positive number of minutes, or hour (0-23) and minute (0-59) name a clock time. day_of_week is 0 for Sunday through 6 for Saturday; omit it to run every day. For cancel, name is the timer to stop, or empty to stop every timer of this bot.
- spec: true or false. Leave it false for a clear one-step command: opening a named page, a screenshot, a lookup, a small change already spelled out, small talk, a status question, or a timer. Set spec true when the message is a class of work that still has to be specified with this person before it can be done faithfully — a post, a report, a program, a publication, or another multi-step delivery — and a detail that would change the result is missing and cannot be defaulted: who it is for, where it goes, the form, the source, the length, or what done looks like. A spec already open is listed under "spec already open". A later message that fills that spec in keeps spec true.

kind:
- supplement: the message adds a constraint, detail, or material to a running task and does not change that task's goal.
- change: the message redirects a running task, so work already under way must take a new direction.
- stop: the message asks to stop or abandon a running task.
- pause: the message asks to hold a running task and resume it later, not to abandon it.
- continue: the message only asks to keep going on a running task and adds no new instruction.
- reply: small talk, thanks, or an identity, with no desktop work.
- ask: a fact only this person knows is missing, so the work cannot start.
- schedule: they want work at a later time or on a repeat, the work should keep happening, or they want that timer stopped. When it should keep happening and names no period, still use schedule and set interval_minutes to a period that fits that work. A job that is finished in one run is not schedule.
- status: they ask how work is going or what it already produced. Do not invent the answer.
- new: an independent job, or anything you are not sure about.

A line under "timers already armed" is a timer that already runs. When the new message only adds a fact or a delivery form for that timer — a city, a place, or a file type such as PDF — kind is schedule, mode is create, and name is that timer's name. instruction is the whole order for one later run: what the timer already does, the place it covers, and the file form. A place is the subject of the report, not a field inside a sample file. A file form is how that later run is delivered. Do not make the file on this turn. ack says the timer now uses that place and that form, and does not say a file was already made. This is not kind new, not kind reply, and not kind ask. A different repeating job gets its own name. Leave a clock field out to keep the armed timer's value. Set interval_minutes, or hour and minute, only when the message changes the time. Omit day_of_week to keep the armed weekday.

When no task is running, do not use supplement, change, stop, pause, or continue.
A clear request is direct work. Do not ask only for permission to proceed. While spec is true and the spec is not settled, this sentence does not apply: kind stays ask.
If you are unsure which running task a message belongs to, kind is new and run is 0.
Do not match the message against a list of phrases.

While spec is true and the person has not settled it: kind is ask, phase is plan, and instruction is the whole spec so far (the goal, the constraints already named, and the open point). The person does not see instruction. ack is one short question, or the one detail you still need, in ordinary words. Do not say 规划模式, 規劃模式, plan mode, or that any mode started.
When the person has confirmed that spec, or the spec is complete and they told you to go ahead: spec stays true, kind is new, phase is execute, and instruction is the whole settled spec the desktop helper should carry out. ack says you are starting that work, in ordinary words, and does not name a mode. A phase of plan does not keep the work waiting once the order carries the missing fact and 还缺 is empty. The words of 还缺 themselves are not that fact. A different job sets spec false and is judged on its own.

phase is execute or plan. execute is the default. Use plan when spec is true and not yet settled, and when this message itself orders something destructive or irreversible: deleting the person's own data, paying, wiping a machine, or sending a message to many other people. Destructive work stays spec false and kind new. Fetching, writing, publishing, and continuing work already arranged are execute when spec is false. A follow-up that continues a running task, or answers a question that is not about an armed timer, is kind continue or reply and phase execute. A follow-up that only fills in an armed timer stays kind schedule. If you are unsure and spec is false, phase is execute.`

// botIntakeNoDesktopClause tells the decision what a computerless bot can
// run. In the shared prompt "the desktop helper" and "desktop work" name
// this bot and the work its own tools can do — a rewritten instruction or
// spec that mentions a desktop, a screen, or a screenshot orders a surface
// that does not exist, which is how a no-desktop bot ended up promising
// screenshot proof.
const botIntakeNoDesktopClause = "\n\nThis bot has no computer and no desktop. Wherever this prompt says \"the desktop assistant\", \"the desktop helper\", or \"desktop work\", it means this bot and the work its own tools do — answering, searching the web, fetching pages, ssh, knowledge, mailbox, timers, and making files such as PDFs that arrive attached to the chat. kind, instruction, spec, and ack must not mention a desktop, a screen, a window, a screenshot, or files on a computer, and must not order work that needs one. ack may promise a chat file such as a PDF, and must only promise what those tools can do; it cannot promise messaging other bots, operating computers, or anything else outside that list. When the person asks for desktop work, still answer honestly about the limit in ack rather than writing an instruction the bot cannot do."

// botIntakeSystemFor returns the decision prompt for this bot's surface.
func botIntakeSystemFor(noDesktop bool) string {
	if !noDesktop {
		return botIntakeSystem
	}
	return botIntakeSystem + botIntakeNoDesktopClause
}

// IntakeBotMessage decides what a message does. A classifier that fails,
// times out, or answers something unusable yields a new task whose
// instruction is the person's own words.
func (s *Service) IntakeBotMessage(ctx context.Context, p Principal, instanceID string, in BotIntakeInput, classify BotIntakeClassifier) (BotIntake, error) {
	bot := strings.TrimSpace(in.BotID)
	content := strings.TrimSpace(in.Content)
	fallback := BotIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Reason: "fallback"}
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return BotIntake{}, err
	}
	// The instance record is authoritative: a caller that forgot (or
	// downgraded) the no_desktop flag cannot give this bot a surface it
	// does not have, and a marked bot keeps the restriction even when the
	// request body drops it.
	noDesktop := in.NoDesktop ||
		strings.TrimSpace(inst.Metadata["hub_bot_desktop"]) == desktop.BotDesktopModeNone
	if bot == "" || botOfClientSessionKey(bot) != bot {
		return BotIntake{}, ErrBotThreadsBadBot
	}
	if content == "" {
		fallback.Reason = "empty"
		return fallback, nil
	}
	running := liveIntakeRuns(in.Running)
	draft := desktopbot.SpecOf(p.TenantID, p.UserID, bot)
	if classify == nil {
		if draft != "" {
			return retireOpenSpec(p.TenantID, p.UserID, bot, draft, content, "no classifier"), nil
		}
		fallback.Reason = "no classifier"
		return fallback, nil
	}
	user := intakeUserPromptClean(content, in, running)
	if draft != "" {
		user += "\nspec already open:\n" + clipSpecRunes(draft, 800)
	}
	cctx, cancel := context.WithTimeout(ctx, BotIntakeBudget)
	defer cancel()
	type answer struct {
		raw string
		err error
	}
	done := make(chan answer, 1)
	go func() {
		raw, err := classify(cctx, botIntakeSystemFor(noDesktop), user)
		done <- answer{raw, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-cctx.Done():
		if draft != "" {
			return retireOpenSpec(p.TenantID, p.UserID, bot, draft, content, "timeout"), nil
		}
		fallback.Reason = "timeout"
		return fallback, nil
	}
	if got.err != nil {
		if draft != "" {
			return retireOpenSpec(p.TenantID, p.UserID, bot, draft, content, "classifier unavailable"), nil
		}
		fallback.Reason = "classifier unavailable"
		return fallback, nil
	}
	parsed, ok := parseBotIntake(got.raw)
	if !ok {
		if draft != "" {
			return retireOpenSpec(p.TenantID, p.UserID, bot, draft, content, "unreadable"), nil
		}
		fallback.Reason = "unreadable"
		return fallback, nil
	}
	parsed = completeIntakeSchedule(parsed, in.Timers)
	out := normalizeBotIntake(parsed, content, running)
	out = preferOpenSpecCard(p.TenantID, p.UserID, bot, content, out)
	syncOpenSpec(p.TenantID, p.UserID, bot, content, out)
	return out, nil
}

// retireOpenSpec keeps the open spec and moves its generation. The sentence
// that could not be read stays on 这次补充. A background rewrite from the
// previous generation can no longer land.
func retireOpenSpec(tenant, user, bot, draft, content, reason string) BotIntake {
	draft = strings.TrimSpace(draft)
	if draft != "" {
		draft = clipSpec(foldSpecSupplement(draft, content))
		desktopbot.NoteSpec(tenant, user, bot, draft)
	}
	return holdOpenSpec(draft, reason)
}

// holdOpenSpec keeps an unfinished spec when this message could not be read.
// Starting a run from the unread sentence would skip the details still open.
func holdOpenSpec(draft, reason string) BotIntake {
	ack := "这句没接住，你把要补的那点再说一遍。"
	if gap := specGapOpen(draft); gap != "" {
		ack = specGapQuestion(gap)
	}
	return BotIntake{
		Kind:        "ask",
		Instruction: draft,
		Ack:         ack,
		Told:        ack,
		Pace:        "ask",
		Spec:        true,
		Phase:       "plan",
		Reason:      reason,
	}
}

// syncOpenSpec remembers a spec still being discussed, and drops it once the
// settled spec is sent to run or a different job takes the turn. A short
// follow-up does not replace a card that already has a goal and a done check.
func syncOpenSpec(tenant, user, bot, content string, out BotIntake) {
	if out.Spec && out.Phase == "plan" && strings.TrimSpace(out.Instruction) != "" {
		desktopbot.NoteSpec(tenant, user, bot, specTextToKeep(tenant, user, bot, content, out.Instruction))
		return
	}
	if out.Phase == "execute" && (out.Kind == "new" || out.Kind == "schedule") {
		desktopbot.ClearSpec(tenant, user, bot)
	}
}

// specTextToKeep keeps the background card when this turn only added a short
// line. The person's sentence is folded in so the next rewrite still sees it.
func specTextToKeep(tenant, user, bot, content, next string) string {
	next = strings.TrimSpace(next)
	card := strings.TrimSpace(desktopbot.SpecOf(tenant, user, bot))
	text := next
	if specCardIsWritten(card) && !specCardIsWritten(next) {
		text = card
	}
	content = strings.TrimSpace(content)
	if specCardIsWritten(text) {
		text = foldSpecSupplement(text, content)
	}
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000])
	}
	return text
}

func specCardIsWritten(card string) bool {
	return strings.Contains(card, "目标：") || strings.Contains(card, "目标:") || strings.Contains(card, "完成标准：") || strings.Contains(card, "完成标准:")
}

// specSentenceIsCard reports that this text is the spec itself. The person
// hears a question, and a card is not that question.
func specSentenceIsCard(s string) bool {
	for _, label := range specFieldLabels {
		if strings.Contains(s, label) {
			return true
		}
	}
	return false
}

// specVisibleSentence is the one sentence when this turn has no written card
// yet. A question beats a bare receipt. A turn that is already starting keeps
// its statement: a question in the other field does not replace it. A spec
// card is never that sentence.
func specVisibleSentence(ack, told, instruction string, starting bool) string {
	rawAck, rawTold := ack, told
	if specSentenceIsCard(ack) {
		ack = ""
	}
	if specSentenceIsCard(told) {
		told = ""
	}
	ack = strings.TrimSpace(ack)
	told = strings.TrimSpace(told)
	if starting && ack != "" && !specAckAsks(ack) {
		return ack
	}
	if starting && told != "" && !specAckAsks(told) {
		return told
	}
	if specAckAsks(ack) {
		return ack
	}
	if specAckAsks(told) {
		return told
	}
	if ack != "" {
		return ack
	}
	if told != "" {
		return told
	}
	if gap := specGapOpen(instruction); gap != "" {
		return specGapQuestion(gap)
	}
	if gap := specGapOpen(rawTold); gap != "" {
		return specGapQuestion(gap)
	}
	if gap := specGapOpen(rawAck); gap != "" {
		return specGapQuestion(gap)
	}
	return ""
}

var specFieldLabels = []string{"目标：", "目标:", "约束：", "约束:", "完成标准：", "完成标准:", "还缺：", "还缺:", "这次补充：", "这次补充:"}

const specDoneCheckPrefix = "做完后按这个标准核对，并把结果告诉我："

// specField reads one labeled line. The value stops at the next label.
func specField(card string, labels ...string) (string, string) {
	bestAt := -1
	bestLabel := ""
	best := ""
	for _, label := range labels {
		idx := strings.Index(card, label)
		if idx < 0 || (bestAt >= 0 && idx > bestAt) {
			continue
		}
		rest := card[idx+len(label):]
		cut := len(rest)
		for _, other := range specFieldLabels {
			if j := strings.Index(rest, other); j >= 0 && j < cut {
				cut = j
			}
		}
		bestAt = idx
		bestLabel = label
		best = strings.Trim(strings.TrimSpace(rest[:cut]), " ，,。.")
	}
	return bestLabel, best
}

func specGapOpen(card string) string {
	_, gap := specField(card, "还缺：", "还缺:")
	gap = specTrimWords(gap)
	if specGapIsClosed(gap) {
		return ""
	}
	return gap
}

// specGapIsClosed reports that this open-point text names nothing left to
// ask. A question mark on that word does not keep the work waiting.
func specGapIsClosed(gap string) bool {
	switch specTrimWords(gap) {
	case "", "无", "没有", "暂无", "空", "无缺", "-", "—":
		return true
	}
	return false
}

func specTrimWords(s string) string {
	return strings.Trim(strings.TrimSpace(s), " ，,。．.!！?？;；、")
}

// specGapAnswered reports that this sentence is new. Repeating a whole field,
// or a fact already kept on 这次补充, is not new, so the open point is asked
// again. A shorter fact inside a longer field still is new, so the model's
// own question stays. A period on a sentence already written does not make it new.
func specGapAnswered(card, gap, content string) bool {
	if gap == "" {
		return true
	}
	content = specTrimWords(content)
	if content == "" {
		return false
	}
	if specMessageRepeatsField(card, content) || specSupplementHas(card, content) {
		return false
	}
	return true
}

// specAlreadyOnCard reports that folding this sentence would not add a fact.
// The open point's own words are already on the card. A shorter fact that
// appears only inside that point is not, so it can still be kept.
func specAlreadyOnCard(card, content string) bool {
	raw := strings.TrimSpace(content)
	trimmed := specTrimWords(raw)
	if raw == "" || trimmed == "" {
		return true
	}
	if specMessageRepeatsField(card, trimmed) {
		return true
	}
	body := stripSpecGap(card)
	return strings.Contains(body, raw) || strings.Contains(body, trimmed)
}

// specSupplementHas reports that this sentence is already one fact on 这次补充.
func specSupplementHas(card, content string) bool {
	content = specTrimWords(content)
	if content == "" {
		return false
	}
	_, extra := specField(card, "这次补充：", "这次补充:")
	for _, part := range strings.FieldsFunc(extra, func(r rune) bool { return r == '；' || r == ';' }) {
		if specTrimWords(part) == content {
			return true
		}
	}
	return false
}

// specRunMayCloseGap is the one close check. 还缺 closes when this run's
// order carries the latest sentence and that sentence is not itself a
// written field. The open point repeated back is not an answer. A shorter
// fact inside a longer line, including one kept under 这次补充, can still
// be the answer.
func specRunMayCloseGap(card, content, instruction string) bool {
	raw := strings.TrimSpace(content)
	content = specTrimWords(raw)
	instruction = strings.TrimSpace(instruction)
	if content == "" || (!strings.Contains(instruction, raw) && !strings.Contains(instruction, content)) {
		return false
	}
	return !specMessageRepeatsField(card, content)
}

// specMessageRepeatsField reports that this sentence is just one written
// field again, including the open point. A name that sits inside a longer
// line is a different fact.
func specMessageRepeatsField(card, content string) bool {
	content = specTrimWords(content)
	if content == "" {
		return false
	}
	for _, labels := range [][]string{
		{"目标：", "目标:"},
		{"约束：", "约束:"},
		{"完成标准：", "完成标准:"},
		{"还缺：", "还缺:"},
	} {
		if _, value := specField(card, labels...); specTrimWords(value) == content {
			return true
		}
	}
	return false
}

// specSettledBody is the goal, the constraints, and the done check. The open
// point and 这次补充 are not part of it, so a fact that only sits there is
// not already written into the settled lines.
func specSettledBody(card string) string {
	return specWithoutSupplement(stripSpecGap(card))
}

func specWithoutSupplement(card string) string {
	card, _ = stripSpecSupplements(card)
	return card
}

// stripSpecSupplements lifts every 这次补充 line off the card. Either colon
// is the same line. The values come back in the order they were written.
func stripSpecSupplements(card string) (string, []string) {
	var extras []string
	for _, label := range []string{"这次补充：", "这次补充:"} {
		for strings.Contains(card, label) {
			i := strings.Index(card, label)
			rest := card[i+len(label):]
			cut := len(rest)
			for _, other := range specFieldLabels {
				if other == label {
					continue
				}
				if j := strings.Index(rest, other); j >= 0 && j < cut {
					cut = j
				}
			}
			if v := strings.Trim(strings.TrimSpace(rest[:cut]), "；; "); v != "" {
				extras = append(extras, v)
			}
			card = strings.TrimSpace(card[:i] + rest[cut:])
		}
	}
	return card, extras
}

// foldSpecSupplement keeps one 这次补充 line. A second label is invisible to
// the field reader, so a later fact is joined onto the line already there.
func foldSpecSupplement(card, content string) string {
	content = strings.TrimSpace(content)
	card = strings.TrimSpace(card)
	if specAlreadyOnCard(card, content) {
		return card
	}
	card, extras := stripSpecSupplements(card)
	extras = append(extras, content)
	const label = "这次补充："
	if card == "" {
		return label + strings.Join(extras, "；")
	}
	return card + "\n" + label + strings.Join(extras, "；")
}

func replaceSpecGap(card, gap string) string {
	card = stripSpecGap(card)
	gap = strings.TrimSpace(gap)
	if gap == "" {
		return card
	}
	if card == "" {
		return "还缺：" + gap
	}
	return card + "\n还缺：" + gap
}

func stripSpecGap(card string) string {
	label, _ := specField(card, "还缺：", "还缺:")
	if label == "" {
		return strings.TrimSpace(card)
	}
	idx := strings.Index(card, label)
	rest := card[idx+len(label):]
	cut := len(rest)
	for _, other := range specFieldLabels {
		if j := strings.Index(rest, other); j >= 0 && j < cut {
			cut = j
		}
	}
	return strings.TrimSpace(card[:idx] + rest[cut:])
}

func clipSpec(card string) string {
	return clipSpecRunes(card, 4000)
}

func clipSpecRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if max > 0 && len(r) > max {
		return string(r[:max])
	}
	return s
}

// SpecDraftForRefine is the spec the background rewrite should see: the
// stored card when this turn kept it, otherwise the line just decided.
func SpecDraftForRefine(tenant, user, bot, fallback string) (uint64, string) {
	gen := desktopbot.SpecGen(tenant, user, bot)
	draft := strings.TrimSpace(desktopbot.SpecOf(tenant, user, bot))
	if draft == "" {
		draft = strings.TrimSpace(fallback)
	}
	return gen, draft
}

// preferOpenSpecCard is the one spec transition. A written card with an open
// 还缺 stays a question, including the card this turn just wrote. The same
// fact carried by an order that no longer has 还缺 settles it, and that card
// is what runs. A stored card that is already written wins over this turn.
func preferOpenSpecCard(tenant, user, bot, content string, out BotIntake) BotIntake {
	if !out.Spec {
		return out
	}
	card := strings.TrimSpace(desktopbot.SpecOf(tenant, user, bot))
	if !specCardIsWritten(card) {
		card = strings.TrimSpace(out.Instruction)
	}
	if !specCardIsWritten(card) {
		sentence := specVisibleSentence(out.Ack, out.Told, out.Instruction, out.Phase == "execute")
		if sentence == "" {
			if out.Phase == "execute" {
				sentence = "我按这个做。"
			} else {
				sentence = "还差一处要你定。"
			}
		}
		out.Ack = sentence
		out.Told = sentence
		return out
	}
	return applyOpenSpec(card, content, out)
}

// applyOpenSpec turns one message and the open card into either the question
// still open or the run order. The phase the model named does not grow a
// second decision.
func applyOpenSpec(card, content string, out BotIntake) BotIntake {
	gap := specGapOpen(card)
	instructionGap := specGapOpen(out.Instruction)
	closes := gap != "" && specRunMayCloseGap(card, content, out.Instruction) && instructionGap == ""
	if gap != "" && !closes {
		force := strings.EqualFold(out.Phase, "execute") || !specGapAnswered(card, gap, content)
		return specAsk(card, content, out, force)
	}
	if gap == "" && instructionGap != "" {
		return specAsk(replaceSpecGap(card, instructionGap), content, out, true)
	}
	return specRun(card, content, out)
}

func specAsk(card, content string, out BotIntake, forceGap bool) BotIntake {
	gap := specGapOpen(card)
	card = clipSpec(foldSpecSupplement(card, content))
	ack := specChooseAsk(out.Ack, out.Told, gap, forceGap)
	out.Kind = "ask"
	out.Phase = "plan"
	out.Pace = "ask"
	out.Spec = true
	out.Ack = ack
	out.Told = ack
	out.Instruction = card
	return out
}

func specRun(card, content string, out BotIntake) BotIntake {
	// The open point comes off first. The answer is often only a piece of
	// that line; folding first would treat it as already written, and the
	// strip would drop it from the order.
	card = stripSpecGap(card)
	card = foldSpecSupplement(card, content)
	if _, done := specField(card, "完成标准：", "完成标准:"); done != "" && !strings.Contains(card, specDoneCheckPrefix) {
		card += "\n" + specDoneCheckPrefix + done
	}
	ack := strings.TrimSpace(out.Ack)
	told := strings.TrimSpace(out.Told)
	if specSentenceIsCard(ack) {
		ack = ""
	}
	if specSentenceIsCard(told) {
		told = ""
	}
	if ack == "" || strings.ContainsAny(ack, "?？") {
		if told != "" && !strings.ContainsAny(told, "?？") {
			ack = told
		} else {
			ack = "我按这个做。"
		}
	}
	out.Ack = ack
	out.Told = ack
	out.Kind = "new"
	out.Phase = "execute"
	out.Pace = "direct"
	out.Spec = true
	out.Instruction = clipSpec(card)
	return out
}

// specChooseAsk is the question this turn shows. A sentence that already
// asks the open point stays, whichever field carried it. A card does not.
func specChooseAsk(ack, told, gap string, force bool) string {
	if specSentenceIsCard(ack) {
		ack = ""
	}
	if specSentenceIsCard(told) {
		told = ""
	}
	ack = strings.TrimSpace(ack)
	told = strings.TrimSpace(told)
	switch {
	case specAckCarriesGap(ack, gap):
		return ack
	case !force && specAckAsks(ack):
		return ack
	case specAckCarriesGap(told, gap):
		return told
	default:
		return specGapQuestion(gap)
	}
}

// specAckCarriesGap reports that this question already asks the open point.
// The shorter wording is not substituted for it.
func specAckCarriesGap(ack, gap string) bool {
	if !specAckAsks(ack) {
		return false
	}
	ack = specTrimWords(ack)
	gap = specTrimWords(gap)
	if ack == "" || gap == "" {
		return false
	}
	return strings.Contains(ack, gap)
}

// specAckAsks reports that this sentence is a question the person can answer.
// A bare question mark, or a line that only names the work, does not ask.
func specAckAsks(ack string) bool {
	ack = strings.TrimSpace(ack)
	if ack == "" || !strings.ContainsAny(ack, "?？") {
		return false
	}
	return specTrimWords(ack) != ""
}

func specGapQuestion(gap string) string {
	gap = strings.TrimSpace(gap)
	if gap == "" {
		return "还差一处要你定。"
	}
	if !strings.HasSuffix(gap, "？") && !strings.HasSuffix(gap, "?") {
		gap += "？"
	}
	gap = desktopbot.HidePlanMode(gap)
	if gap == "" {
		return "还差一处要你定。"
	}
	return gap
}

// SpecRefineBudget is how long the background spec may take. It does not
// hold the person's question.
var SpecRefineBudget = 20 * time.Second

const specRefineSystem = `You write the hidden task spec for one desktop bot. The person does not see this. Return one JSON object and nothing else. The object has one string field, instruction.

instruction is the whole spec in the language of the latest message. Use these labels, each on its own line: 目标, 约束, 完成标准, 还缺. 目标 is the outcome. 约束 is what must not be done, from what was already said. 完成标准 is how the finished job is checked. 还缺 is the one fact only this person knows, or empty when nothing is missing. Keep every fact already written under 目标, 约束, and 完成标准. While 还缺 is open, put the latest message on the 这次补充 line only. Leave 目标, 约束, 完成标准, and the open 还缺 as they are. Keep a 这次补充 line that is already written. A line under 这次补充 does not by itself close 还缺. Do not drop 目标 or 完成标准 that are already written. Do not clear an open 还缺. Do not invent sites, files, or steps they did not ask for. Do not say 规划模式, 規劃模式, or plan mode.`

// specRefineNoDesktopClause keeps a computerless bot's card off the surface
// the intake prompt must not name either.
const specRefineNoDesktopClause = "\n\nThis bot has no computer and no desktop. 目标, 约束, 完成标准, 还缺, and 这次补充 must not mention a desktop, a screen, a window, or a screenshot, and must not order work that needs one."

// specRefineSystemFor returns the spec rewrite prompt for this bot's surface.
func specRefineSystemFor(noDesktop bool) string {
	if !noDesktop {
		return specRefineSystem
	}
	return specRefineSystem + specRefineNoDesktopClause
}

// RefineOpenSpec rewrites the open spec in the background. gen is the
// generation NoteSpec returned for this turn. A newer message or a settled
// job makes the write drop.
func RefineOpenSpec(ctx context.Context, tenant, user, bot string, gen uint64, draft, latest string, earlier []string, noDesktop bool, classify BotIntakeClassifier) {
	draft = strings.TrimSpace(draft)
	if classify == nil || gen == 0 || draft == "" || desktopbot.SpecGen(tenant, user, bot) != gen {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, SpecRefineBudget)
	defer cancel()
	var b strings.Builder
	b.WriteString("spec so far:\n")
	b.WriteString(clipSpecRunes(draft, 800))
	if len(earlier) > 0 {
		b.WriteString("\nearlier messages:")
		n := 0
		for _, line := range earlier {
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
	b.WriteString("\nlatest message:\n")
	b.WriteString(oneLineRunes(latest, 800))
	raw, err := classify(cctx, specRefineSystemFor(noDesktop), b.String())
	if err != nil || desktopbot.SpecGen(tenant, user, bot) != gen {
		return
	}
	card, ok := parseSpecCard(raw)
	if !ok {
		return
	}
	card = desktopbot.StripPlanModeLabels(card)
	if card == "" || !specRewriteKeeps(draft, card, latest) {
		return
	}
	if r := []rune(card); len(r) > 4000 {
		card = string(r[:4000])
	}
	desktopbot.ReplaceSpec(tenant, user, bot, gen, card)
}

func parseSpecCard(raw string) (string, bool) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", false
	}
	var v struct {
		Instruction string `json:"instruction"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &v) != nil {
		return "", false
	}
	card := strings.TrimSpace(v.Instruction)
	if card == "" {
		return "", false
	}
	return card, true
}

// specRewriteKeeps refuses a background rewrite that drops or replaces a
// fact already written. The open point can grow, and it cannot be swapped
// for a different question. Either colon is the same label.
func specRewriteKeeps(draft, card, latest string) bool {
	if !specFieldKept(draft, card, "目标：", "目标:") ||
		!specFieldKept(draft, card, "约束：", "约束:") ||
		!specFieldKept(draft, card, "完成标准：", "完成标准:") {
		return false
	}
	if gap := specTrimWords(specGapOpen(draft)); gap != "" {
		next := specTrimWords(specGapOpen(card))
		if next == "" || !strings.Contains(next, gap) {
			return false
		}
	}
	latest = specTrimWords(latest)
	if latest != "" && specGapOpen(draft) != "" &&
		strings.Contains(specTrimWords(specSettledBody(card)), latest) &&
		!strings.Contains(specTrimWords(specSettledBody(draft)), latest) {
		return false
	}
	if _, extra := specField(draft, "这次补充：", "这次补充:"); extra != "" {
		_, kept := specField(card, "这次补充：", "这次补充:")
		if !strings.Contains(kept, extra) && !strings.Contains(specTrimWords(kept), specTrimWords(extra)) {
			return false
		}
	}
	return true
}

// specFieldKept reports that a fact already written under this label is still
// in the rewrite. A longer line may contain the old fact. A different fact
// may not replace it.
func specFieldKept(draft, card string, labels ...string) bool {
	_, old := specField(draft, labels...)
	old = specTrimWords(old)
	if old == "" {
		return true
	}
	_, next := specField(card, labels...)
	return strings.Contains(specTrimWords(next), old)
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
	if len(in.Timers) > 0 {
		b.WriteString("\ntimers already armed:")
		n := 0
		for _, timer := range in.Timers {
			action := oneLineRunes(timer.Action, 200)
			if action == "" {
				continue
			}
			n++
			if n > 4 {
				break
			}
			b.WriteString("\n- ")
			b.WriteString(oneLineRunes(timer.Name, 40))
			if timer.IntervalMinutes > 0 {
				b.WriteString(" every ")
				b.WriteString(itoa(timer.IntervalMinutes))
				b.WriteString(" minutes")
			} else {
				b.WriteString(" at ")
				b.WriteString(itoa(timer.Hour))
				b.WriteString(":")
				if timer.Minute < 10 {
					b.WriteString("0")
				}
				b.WriteString(itoa(timer.Minute))
				if timer.DayOfWeek >= 0 && timer.DayOfWeek <= 6 {
					b.WriteString(" day_of_week ")
					b.WriteString(itoa(timer.DayOfWeek))
				}
			}
			b.WriteString(": ")
			b.WriteString(action)
		}
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
		Reason      string             `json:"reason"`
		Phase       string             `json:"phase"`
		Spec        bool               `json:"spec"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &v) != nil || v.Run == nil {
		return BotIntake{}, false
	}
	out := BotIntake{
		Kind:        strings.ToLower(strings.TrimSpace(v.Kind)),
		Instruction: strings.TrimSpace(v.Instruction),
		Ack:         oneLineRunes(v.Ack, 200),
		Pace:        strings.ToLower(strings.TrimSpace(v.Pace)),
		Told:        oneLineRunes(v.Told, 400),
		Persona:     strings.TrimSpace(v.Persona),
		Schedule:    v.Schedule,
		Reason:      oneLineRunes(v.Reason, 120),
		Phase:       strings.ToLower(strings.TrimSpace(v.Phase)),
		Spec:        v.Spec,
	}
	out.RunID = itoa(*v.Run) // temporary: the index, rewritten by normalize
	if *v.Run < 0 {
		out.RunID = "0"
	}
	// stash the numeric index in Reason's neighbor via RunID prefix
	out.RunID = itoa(*v.Run)
	return out, true
}

func normalizeBotIntake(in BotIntake, content string, running []BotIntakeRun) BotIntake {
	kind := canonicalIntakeKind(in.Kind, in.Pace)
	if spec, ok := normalizeSpecIntake(in, kind, content); ok {
		return spec
	}
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
		return BotIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Ack: in.Ack, Reason: "no run"}
	}
	if kind == "continue" && len(running) == 0 {
		return BotIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Ack: in.Ack, Reason: "nothing to continue"}
	}
	instruction := strings.TrimSpace(in.Instruction)
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
			return BotIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Ack: in.Ack, Reason: "bad schedule"}
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
			return BotIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Reason: "empty reply"}
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
	out.Phase = intakePhase(kind, in.Phase)
	return out
}

// normalizeSpecIntake is the hidden specifying turn. Unsettled work stays a
// question: no desktop run, and the sentence does not name a planning mode.
// A settled spec is an ordinary new task whose instruction is that spec.
// Schedule, status, stop, pause, and reply are not specifying turns.
func normalizeSpecIntake(in BotIntake, kind, content string) (BotIntake, bool) {
	if !in.Spec {
		return BotIntake{}, false
	}
	switch kind {
	case "schedule", "status", "stop", "pause", "reply":
		return BotIntake{}, false
	}
	instruction := strings.TrimSpace(in.Instruction)
	if instruction == "" {
		instruction = strings.TrimSpace(content)
	}
	if r := []rune(instruction); len(r) > 4000 {
		instruction = string(r[:4000])
	}
	ack := desktopbot.HidePlanMode(in.Ack)
	told := desktopbot.HidePlanMode(in.Told)
	settled := strings.EqualFold(strings.TrimSpace(in.Phase), "execute") && kind != "ask"
	if settled {
		return BotIntake{
			Kind:        "new",
			Instruction: instruction,
			Ack:         ack,
			Told:        told,
			Pace:        "direct",
			Spec:        true,
			Phase:       "execute",
			Reason:      in.Reason,
			Persona:     clipPersona(in.Persona),
		}, true
	}
	return BotIntake{
		Kind:        "ask",
		Instruction: instruction,
		Ack:         ack,
		Told:        told,
		Pace:        "ask",
		Spec:        true,
		Phase:       "plan",
		Reason:      in.Reason,
		Persona:     clipPersona(in.Persona),
	}, true
}

// intakePhase is the model's phase, with execute as the only default.
// continue and reply are follow-ups and always run. Anything other than
// the word plan is execute. There is no keyword list over the message.
func intakePhase(kind, raw string) string {
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
	case "new", "supplement", "change", "stop", "pause", "continue", "reply", "ask", "schedule", "status":
		return kind
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

// completeIntakeSchedule fills the clock fields a schedule left out. The
// named timer, or the only timer when the name was left out, supplies each
// missing field. A field the decision set is kept. Without this, a decision
// that names the timer but omits part of the clock is rejected and the
// message starts a new job.
func completeIntakeSchedule(in BotIntake, timers []BotIntakeTimer) BotIntake {
	if canonicalIntakeKind(in.Kind, in.Pace) != "schedule" || in.Schedule == nil || len(timers) == 0 {
		return in
	}
	sched := *in.Schedule
	picked := pickIntakeTimer(timers, sched.Name)
	if picked == nil {
		return in
	}
	if strings.TrimSpace(sched.Name) == "" {
		sched.Name = picked.Name
	}
	// An interval timer stays on its interval unless the decision names a
	// complete clock. One of hour or minute is not a new time.
	explicitClock := sched.Hour != nil && sched.Minute != nil
	if sched.IntervalMinutes == 0 && !explicitClock && picked.IntervalMinutes > 0 {
		sched.IntervalMinutes = picked.IntervalMinutes
		sched.Hour, sched.Minute = nil, nil
	} else if sched.IntervalMinutes == 0 {
		if sched.Hour == nil {
			h := picked.Hour
			sched.Hour = &h
		}
		if sched.Minute == nil {
			m := picked.Minute
			sched.Minute = &m
		}
	}
	if sched.IntervalMinutes == 0 && sched.DayOfWeek == nil && picked.IntervalMinutes == 0 && picked.DayOfWeek >= 0 && picked.DayOfWeek <= 6 {
		d := picked.DayOfWeek
		sched.DayOfWeek = &d
	}
	in.Schedule = &sched
	return in
}

// pickIntakeTimer identifies the armed timer by the name the decision used.
// An empty name means the only armed timer. A different name is a different
// timer and does not borrow this one's clock.
func pickIntakeTimer(timers []BotIntakeTimer, name string) *BotIntakeTimer {
	name = strings.TrimSpace(name)
	if name != "" {
		for i := range timers {
			if timers[i].Name == name {
				return &timers[i]
			}
		}
		return nil
	}
	if len(timers) == 1 {
		return &timers[0]
	}
	return nil
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
		if s.DayOfWeek != nil {
			d := *s.DayOfWeek
			if d < 0 || d > 6 {
				return nil, false
			}
			out.DayOfWeek = &d
		}
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
