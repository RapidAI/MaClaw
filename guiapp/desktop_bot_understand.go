package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// DesktopBotUnderstanding is the foreground agent's reading of one message.
// Told is the sentence the person sees. Instruction is the work order sent
// to the desktop worker. Persona is the identity to keep. None of them is
// chosen from a stock phrase.
type DesktopBotUnderstanding struct {
	Told        string `json:"told"`
	Instruction string `json:"instruction"`
	// Pace is direct, confirm, ask, or reply. direct does the work now.
	// confirm is the one approval for a large change. ask is the one question
	// when a fact only this person knows is missing. reply is a short answer
	// given here: small talk, a greeting, or an identity with no desktop work.
	// Its instruction is empty, and it is not sent to the desktop.
	Pace string `json:"pace"`
	// Schedule is set only when Pace is schedule. The foreground arms it.
	// This turn does not call the desktop worker.
	Schedule *DesktopBotSchedule `json:"schedule,omitempty"`
	// Persona is the whole identity to keep after this message, in the
	// model's own words. Empty means this message did not change it. A
	// single hyphen withdraws it. It is not a desktop order.
	Persona string `json:"persona"`
	// SupplementOf is set when this message was read as supplementary
	// information for work already being planned.
	//
	// It is a signal, not the decision. Models may omit it indefinitely, so an
	// empty value never means "not a supplement": the reading is still judged
	// from the earlier messages and this one. Treating absence as a new task
	// would silently undo the fusion this prompt asks for.
	SupplementOf string `json:"supplement_of,omitempty"`
	// Reading is empty when this message was read. Otherwise it names, in one
	// word, why there is no reading: unreachable, unread, or unscheduled. The
	// other fields are empty in that case, so no order can reach the worker
	// from a reading that never happened. It is decided here, where the
	// failure is observed, and no layer has to guess it back out of an error
	// string; the model cannot set it either.
	Reading string `json:"reading,omitempty"`
	// Cross is set only when this bot is the primary and the message tells
	// another bot something or hands it work. It is not a pace. A non-primary
	// reading may still carry one; the client drops it and does not post.
	Cross *DesktopBotCross `json:"cross,omitempty"`
	// Fallback is set when no reading could be made and the person's own
	// words were passed on as the order, the same as "send my own words".
	// It names the failure that caused it: unreachable or unread.
	Fallback string `json:"fallback,omitempty"`
	// Goal is a short verb phrase naming the job ("去小红书打个招呼"), used to
	// phrase the acceptance line. Unclear names what is ambiguous and
	// Assumption how the work proceeds anyway; neither ever blocks the job.
	Goal       string `json:"goal,omitempty"`
	Unclear    string `json:"unclear,omitempty"`
	Assumption string `json:"assumption,omitempty"`
	// Options are 2 to 4 short readings of an ambiguous request; the first is
	// the one the work already proceeds with. They are offered as one-tap
	// corrections and never hold the job.
	Options []string `json:"options,omitempty"`
}

// The reading of a message is a helper for the turn, not the turn itself, so a
// helper that fails is reported instead of being allowed to cancel the turn.
// These are the reports. Each one keeps the person's own words available, and
// none of them invents an order.
const (
	// desktopBotReadingUnreachable means no reading came back at all: the model
	// did not answer inside the budget, could not be reached, or stood behind
	// a rate or endpoint gate. It says nothing about the message.
	desktopBotReadingUnreachable = "unreachable"
	// desktopBotReadingUnread means an answer came back and it was not a
	// usable reading: not the object asked for, or an order that was promised
	// but left empty. Asking the same model again for the same sentence is not
	// how this one is fixed.
	desktopBotReadingUnread = "unread"
	// desktopBotReadingUnscheduled means it promised timed work and did not
	// say when it should run. No timer is armed from a time nobody named.
	desktopBotReadingUnscheduled = "unscheduled"
)

// DesktopBotSchedule is the time the foreground arms. Mode is create or
// cancel. IntervalMinutes above zero repeats. Otherwise Hour and Minute are
// a clock time. DayOfWeek nil runs every day; 0 is Sunday through 6 Saturday.
type DesktopBotSchedule struct {
	Name            string `json:"name,omitempty"`
	Mode            string `json:"mode,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Hour            *int   `json:"hour,omitempty"`
	Minute          *int   `json:"minute,omitempty"`
	DayOfWeek       *int   `json:"day_of_week,omitempty"`
}

const (
	desktopBotReturnTask  = "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。"
	desktopBotConfirmTask = "安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。"

	desktopBotUnderstandToldMax    = 500
	desktopBotUnderstandOrderMax   = 4000
	desktopBotUnderstandPersonaMax = 2000
	// Twenty, not twelve: supplements arrive as several short messages building
	// on one request, and a window too small drops the request they modify.
	desktopBotUnderstandEarlierMax = 20
	desktopBotUnderstandLineMax    = 500
	// The parser clips a finished object. The model is not told those sizes,
	// and dense CJK is about one token per rune, so the cap is twice the
	// fields the parser will keep. A shorter cap cuts the JSON mid-string
	// and the reading is reported as if no arrangement came back.
	desktopBotUnderstandMaxOutputTokens = 2 * (desktopBotUnderstandToldMax + desktopBotUnderstandOrderMax + desktopBotUnderstandPersonaMax)

	// desktopBotReadBudget is everything one reading may spend — first attempt,
	// second attempt, parse and all. The body arrives when generation finishes,
	// not at the first byte. Turning thinking off for this call does not shrink
	// the budget: models that reject thinking.type=disabled still spend the
	// trace here. A completed reading of the same sentence took 43s. A 45s cap
	// sat inside that cost: the context then cancelled the stream with an empty
	// body, and the turn was reported as if no answer had come. One number
	// still covers the reading. It has to be long enough for that generation
	// to finish. A fast failure in the first half still gets the second try.
	desktopBotReadBudget = 90 * time.Second
	// desktopBotReadAttempts is how many calls one reading gets. A second try
	// is spent only on a failure that passing time is likely to clear.
	desktopBotReadAttempts = 3
)

var (
	desktopBotSavedTask  = regexp.MustCompile(`^已在本机保存 [A-Z][A-Z0-9_]{0,63}。$`)
	desktopBotFilledTask = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63} 已在本地填入当前密码框。`)
)

// desktopBotUnderstandRequest is one attempt at reading a message. timeoutSec
// is that attempt's own client deadline, cut from the reading's total budget,
// so an attempt can never outlive the reading it belongs to.
type desktopBotUnderstandRequest struct {
	systemPrompt string
	userPrompt   string
	timeoutSec   int
	// fallback asks for the other model: the lightweight profile, then the
	// background role, instead of the assistant profile that just failed.
	fallback bool
	// tag names the call in LLM logs; empty is the understand step.
	tag string
	// hub is the last resort: the Hub's shared background model. It is used
	// only after the person's own model (assistant profile, then its
	// lightweight sibling) has failed. An unset own model already resolves
	// to the Hub service through the assistant profile.
	hub bool
}

// desktopBotUnderstandFunc is the no-tool model call. Tests replace it.
// The desktop worker is a different call and is not used here.
type desktopBotUnderstandFunc func(ctx context.Context, app *App, req desktopBotUnderstandRequest) (string, error)

var desktopBotUnderstandLLM desktopBotUnderstandFunc = callDesktopBotUnderstandLLM

const desktopBotUnderstandSystem = `You are the foreground agent for one cloud-desktop bot. You do not touch the desktop and you have no tools. Read the latest message, the earlier user messages, the persona already kept, and the phase. Return one JSON object and nothing else. The object has four string fields: told, instruction, pace, and persona. When pace is schedule, it also has one object field, schedule.

told is the sentence this person sees at once. Write it in the language named below, in your own words for this message. For direct, confirm, and ask, say what you understood they want and what happens next. For reply, told is the answer itself, not a plan of what happens next. For schedule, told says the timed work is set or stopped, and does not do that work. One or two sentences. Do not ask them to confirm work you are already doing.

instruction is a card for the computer helper, not a driving script. The helper follows this card and does not see the kept persona except through this card. The card has three parts in this order: goal, constraints, and success criteria. goal is the outcome in one sentence. constraints are what must not be done. success criteria say how the three-line report is checked. Do not name click, type, or computer. Do not say what the chat reply must contain. The foreground agent sends the chat reply. Write the card in the same language as told. Leave it an empty string when there is no desktop work. For schedule, the card is the order for one later run. A repeated greeting is still a card: the goal is that greeting.
When the message ends with a [素材] list, those are files the person uploaded; they are already on the desktop at the listed ~/素材/ paths. The card names every listed path exactly and says to use them as the materials (in a file picker, type or paste the full path). Do not ask for files that are listed. told names the files in 《》 and says in a few words what you will use them for.

goal is a short verb phrase (under 20 characters) naming the job in the language named below, such as 去小红书打个招呼. Empty when there is no desktop work.
unclear and assumption: when pace is direct but part of the request is ambiguous, unclear names that part briefly and assumption says how you will proceed anyway. Still do the work; do not switch to ask or confirm for this. Leave both empty when the request is clear.
options: when the request has several plausible readings, list 2 to 4 short phrases (under 15 characters each) naming them, most likely first. The work proceeds with the first one now; the others are offered as one-tap corrections. told then says, in your own words, that you are starting with the first reading and that they can tap another or add a line. Omit options when the request is clear.
pace is direct, confirm, ask, reply, schedule, or status.
- direct: the request is clear and this turn can finish it. A lookup, a screenshot, opening or reading a named page, writing or revising a file they already described, a small change they spelled out. told says you are doing it now. instruction is the concrete work. A document they asked for uses desktop action=deliver. A file already on the desktop, including a pdf, uses path so the chat receives that file. Text to send uses name and content. Do not leave a confirmation. A request that names a later time or a repeat is schedule, not direct.
- confirm: the change is large or hard to undo, and they have not already approved it. Installing or removing software, deleting, paying, sending on their behalf, wiping, or a long procedure. told is the one arrangement, in one or two sentences. instruction is the concrete work the worker will do after they approve: the goal, the constraints, and the success criteria. Do not tell the helper what the chat reply must contain. Do not tell the worker to only describe the arrangement, to skip the change, or to wait for another confirmation. There is no second confirmation. A large or hard-to-undo change that also names a time stays confirm, and schedule is omitted. After they approve, the execute reading is direct and instruction is that concrete work. It is schedule only when the confirmed work itself names a later time or a repeat.
- ask: a fact only this person knows is missing, or they must finish a login, captcha, payment, or consent. told is that one question. instruction repeats the question. Do not offer a menu of methods. A missing clock time or a missing repeat is ask only when you cannot tell when the work should run.
- reply: Small talk and other short answers that need no desktop, no file, and no lookup. A greeting, thanks, a question about who this bot is, or an identity with no desktop work. told is the whole answer this person sees. When a persona is already kept, the answer uses it. Leave instruction empty. Do not change persona unless this message tells you a new identity. There is no desktop job and no confirmation. A lookup, a file, a screenshot, or any desktop action is not reply. A greeting, thanks, or other short answer that names a later time or a repeat is schedule, not reply. If the same message also asks for desktop work, this is not reply: pace is direct, confirm, or schedule from that work, and instruction is only that work.
- schedule: they want work at a later time or on a repeat, or they want that timed work stopped. A clear one is armed on this turn. told says it is set or stopped. instruction is the order for one later run. Do not ask them to confirm a clear schedule. Do not do the work in told.
- status: they are asking how some task is going, or what a task already produced. This includes a question about one named task and a question that only asks for the recent tasks. instruction is a short hint naming which task, copied from their words, or an empty string when they did not name one. Leave told empty. Do not invent a status, a result, or a percent. Do not start, change, or stop work. A request to do the work is direct, confirm, or schedule, not status.

schedule is an object, only when pace is schedule. mode is create or cancel. name is a short stable name. For create, either interval_minutes is a positive integer of minutes, or hour and minute name a clock time and interval_minutes is 0 or omitted. One minute is 1, one hour is 60, one day is 1440. hour is 0 through 23 and minute is 0 through 59. day_of_week is 0 for Sunday through 6 for Saturday. Omit day_of_week to run every day. For cancel, instruction is an empty string. name is the schedule to stop. An empty name stops every timed job of this bot. told says what stopped.

persona is the identity and persistent user preferences this bot keeps. Leave it an empty string when this message does not change who the bot is and does not express new user preferences.

When the latest message tells you who this bot is — the name to use, how people should address it, the role it keeps, or how it should speak and introduce itself — or expresses user interaction and execution preferences (such as preferring multiple screenshots/images showing key milestones, preferring structured tables, visual summaries, or specific formats), that is not desktop work. If that message does not also ask for desktop work, pace is reply, instruction is an empty string, and persona is the whole identity and accumulated user preferences to keep after this message, in your own words, concrete enough for a later job to use. told acknowledges that identity or preference. Do not promise desktop work and do not ask for confirmation. If the same message also asks for desktop work, pace is direct or confirm from that work, instruction is only that work, and persona is still the whole identity and preferences.

persona already kept is a fact passed below. If this message does not change it, leave persona empty. If it changes it, persona is the whole identity and accumulated preferences after the change, including the parts that still hold. If they withdraw that identity or preferences, persona is a single hyphen.

When a persona is already kept and this message is desktop work:
- The work order carries the identity facts this job needs. Do not invent an identity.
- Proactively adapt instruction to the kept user preferences: if the person prefers multiple pictures/screenshots, explicitly require the worker to take multiple screenshots at key milestones and attach them in the final presentation; if the person prefers tables or structured summaries, require the worker to structure the final answer accordingly. Make the bot smarter and more adaptive across tasks without requiring the person to repeat their preferences every turn.

The phase is a fact about this turn:
- plan: choose pace from the latest message. A clear request is direct, even when it opens a page or delivers a file. A clear timed or repeating request is schedule. confirm is only for a large or hard-to-undo change. ask is only when you cannot proceed without this person. reply is small talk, a question about who this bot is, or an identity with no desktop work. reply does not call the worker. status is a question about work already started or finished. status does not call the worker and does not invent the answer.
When earlier messages contain a task being planned or arranged, and the latest message provides supplementary information, additional requirements, constraints, filters, or modifications (e.g. adding filtering criteria, changing output format, adding steps), do not treat it as an isolated request. Seamlessly fuse and synthesize the earlier task and the supplementary requirements into a complete, updated work order in instruction, and present the updated arrangement in told. If the task was awaiting confirmation or is a multi-step arrangement, keep pace as confirm (or direct if clear and safe) so the person can review the updated plan. When an earlier job is already underway and the latest message is a different job, do not fold it into that job and do not stop that job. instruction is only the new job. told says this one waits until the job already underway finishes. Do not say the new job has started. A constraint, filter, or correction of the job already underway stays on that job. told for that change is exactly 在办 when the language is Chinese and exactly On it when the language is English. Do not restate the arrangement. instruction is only the change. Otherwise classify the latest message on its own. Earlier user messages explain a pronoun or a question you already asked. They are not a reason to repeat an earlier desktop job. If the latest message is small talk, pace is reply even when an earlier job is unfinished.
- execute: they already confirmed, or this is the direct work. pace is direct, unless the confirmed work is itself timed or repeating: then pace is schedule and schedule carries that time. instruction is the concrete work taken from the earlier messages and this message. Do the work now. Do not ask them to confirm again. Use ask only when a fact still blocks the work.

If the latest message wants work that is already running stopped or held, in any language or indirect phrasing, pace is reply, instruction is an empty string, and told is exactly STOP_WORK. Do not describe the work as stopped in your own words. A request to change what the work contains, such as stopping bold text or stopping music inside a video being edited, is not this case: classify that work as usual.

Do not ask this person to choose a method. Do not reuse a stock sentence except 在办 or On it for a change of direction. Do not match the message against a list of phrases. Do not add keys other than schedule, markdown, or text outside the JSON object. instruction and persona may be empty strings. told may be empty only when pace is status. Otherwise told may not.

` + desktopbot.WorthSayingPrompt + `
`

func desktopBotUnderstandClassifyRequest(req desktopBotUnderstandRequest) LLMClassifyRequest {
	out := LLMClassifyRequest{
		SystemPrompt:      req.systemPrompt,
		UserMessage:       req.userPrompt,
		TimeoutSec:        req.timeoutSec,
		Tag:               "desktop-bot-understand",
		PreferLightweight: false,
		NoThinking:        true,
		MaxOutputTokens:   desktopBotUnderstandMaxOutputTokens,
	}
	if req.tag != "" {
		out.Tag = req.tag
	}
	// The fallback stays on the person's own provider (its lightweight
	// model). Role background resolves to the Hub's shared model whenever
	// the role inherits the Hub, which put a custom-model user on the Hub
	// auto model for 78.6s on 2026-10-21; that is now the hub attempt only.
	if req.fallback {
		out.Tag = out.Tag + "-fallback"
		out.PreferLightweight = true
	}
	if req.hub {
		out.Tag = out.Tag + "-hub"
		out.PreferLightweight = true
		out.Role = "background"
	}
	return out
}

func callDesktopBotUnderstandLLM(ctx context.Context, app *App, req desktopBotUnderstandRequest) (string, error) {
	if app == nil {
		return "", fmt.Errorf("AI assistant backend is unavailable")
	}
	hub := app.hubClient()
	if hub == nil {
		return "", fmt.Errorf("LLM not configured")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return "", fmt.Errorf("LLM not configured")
	}
	result, err := handler.LLMClassify(ctx, desktopBotUnderstandClassifyRequest(req))
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("desktop bot understanding returned nothing")
	}
	// Text is the message body. A reasoning model often leaves that empty
	// and puts the object in ReasoningContent, sometimes inside a think block.
	// Other classifiers keep using Text only.
	return desktopBotUnderstandText(result.Text, result.Reasoning), nil
}

// UnderstandDesktopBotTask reads one message and returns the person's sentence,
// the desktop worker's order, and any identity to keep. Protocol controls pass
// through unchanged. persona is the identity already kept for this bot.
//
// A reading that cannot be made is reported, never replaced: no stock order is
// sent in its place, and the missing reading does not take the turn down with
// it. The result is empty except for Reading, which names what happened so the
// caller can decide what to do with the person's own words.
//
// catalog is one JSON object naming the account's bots. Wails unmarshals each
// argument into that parameter's own type. A variadic parameter is a slice,
// and the page's string is then rejected before the reading starts.
func (a *App) UnderstandDesktopBotTask(content, phase, lang string, earlier []string, persona, catalog string) (*DesktopBotUnderstanding, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}
	phase = desktopBotPhase(phase)
	lang = strings.TrimSpace(lang)
	// The old approval sentence is not a task and not an approval. Typing it
	// does not call the model and does not rebuild an order from earlier lines.
	if text == desktopBotConfirmTask {
		log.Printf("[desktop-bot-understand] confirm sentence ignored phase=%s text_len=%d", phase, utf8.RuneCountInString(text))
		return &DesktopBotUnderstanding{Reading: desktopBotReadingUnread}, nil
	}
	if pass, ok := desktopBotProtocolUnderstanding(text, phase); ok {
		log.Printf("[desktop-bot-understand] protocol=yes phase=%s text_len=%d", phase, utf8.RuneCountInString(text))
		return pass, nil
	}
	if a == nil {
		return nil, fmt.Errorf("AI assistant backend is unavailable")
	}
	var userPref string
	a.memoryStoreMu.Lock()
	ms := a.memoryStore
	a.memoryStoreMu.Unlock()
	if ms != nil {
		userPref = ms.UserPreferenceSummaryForPrompt(memory.UserPreferenceSummaryPromptOptions{MaxRunes: 300})
	}
	roster := parseDesktopBotRoster(catalog)
	userPrompt := desktopBotUnderstandUser(text, phase, lang, earlier, persona, userPref)
	systemPrompt := desktopBotUnderstandSystem
	if rosterPrimary(roster) {
		systemPrompt += "\n\n" + desktopBotCrossSystem
		userPrompt += desktopBotRosterPrompt(roster)
	}
	started := time.Now()
	deadline := started.Add(desktopBotReadBudget)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// useFallback switches the next attempt to the other model. It is set by
	// an answer that came back but did not parse (the 2026-10-20 field case:
	// deepseek-v4.1-flash returned 200 with an empty body), and by a second
	// network failure on the primary model.
	useFallback, useHub := false, false
	lastClass, lastOutcome := desktopBotReadingUnreachable, "budget spent"
	for attempt := 1; attempt <= desktopBotReadAttempts; attempt++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		// Every attempt answers to the reading's own budget, not to a number
		// of its own, so a later attempt cannot spend time already given away.
		// Truncation keeps the client timeout strictly under the ctx deadline:
		// when the budget fires, reqCtx.Err() is non-nil at the observe point
		// and the failure gate records no endpoint ban for it.
		timeoutSec := int(remaining / time.Second)
		if timeoutSec < 1 {
			timeoutSec = 1
		}
		raw, err := desktopBotUnderstandLLM(ctx, a, desktopBotUnderstandRequest{
			systemPrompt: systemPrompt,
			userPrompt:   userPrompt,
			timeoutSec:   timeoutSec,
			fallback:     useFallback && !useHub,
			hub:          useHub,
		})
		if err != nil {
			log.Printf("[desktop-bot-understand] phase=%s attempt=%d elapsed=%s earlier=%d text_len=%d err=%v",
				phase, attempt, time.Since(started).Round(time.Millisecond), len(earlier), utf8.RuneCountInString(text), err)
			// Retry only what waiting is likely to fix, and only while most of
			// the budget is unspent: a fault that shows up after tens of
			// seconds of generation is not going to be answered in the
			// remainder either. An upstream that named a wait is given that
			// wait first, capped so the next attempt still has room.
			lastClass, lastOutcome = desktopBotReadingUnreachable, "no answer"
			if attempt < desktopBotReadAttempts && desktopBotReadFaultClears(err) && time.Until(deadline)*2 > desktopBotReadBudget {
				if wait := desktopBotReadRetryWait(err, deadline); wait > 0 {
					log.Printf("[desktop-bot-understand] phase=%s attempt=%d waiting=%s for upstream retry hint", phase, attempt, wait.Round(time.Millisecond))
					desktopBotReadSleep(ctx, wait)
				}
				if attempt >= 2 {
					useHub = useFallback
					useFallback = true
				}
				continue
			}
			break
		}
		understood, parseErr := parseDesktopBotUnderstanding(raw)
		if parseErr != nil {
			log.Printf("[desktop-bot-understand] phase=%s attempt=%d elapsed=%s earlier=%d text_len=%d result_len=%d parse_err=%v",
				phase, attempt, time.Since(started).Round(time.Millisecond), len(earlier), utf8.RuneCountInString(text), utf8.RuneCountInString(raw), parseErr)
			lastClass, lastOutcome = desktopBotReadingUnread, "answer was not a reading"
			// An empty or cut answer is often a one-off on a fast model. Ask
			// once more, on the other model, while the budget allows.
			if attempt < desktopBotReadAttempts && !useHub && time.Until(deadline) > 5*time.Second {
				useHub = useFallback
				useFallback = true
				log.Printf("[desktop-bot-understand] phase=%s attempt=%d retrying on fallback model after parse failure hub=%v", phase, attempt, useHub)
				continue
			}
			break
		}
		rawPace := strings.ToLower(strings.TrimSpace(understood.Pace))
		understood.Pace = desktopBotPace(understood.Pace, phase)
		// pace stop, or the exact token, is the cancel signal for a reading
		// that stood in when the fast classifier did not return. It is not a
		// desktop order and it is not a sentence the person sees.
		if rawPace == "stop" || strings.TrimSpace(understood.Told) == "STOP_WORK" {
			understood.Pace = "reply"
			understood.Instruction = ""
			understood.Told = "STOP_WORK"
			understood.Schedule = nil
			understood.Cross = nil
		}
		finishDesktopBotCross(understood, roster)
		if err := normalizeDesktopBotSchedule(understood); err != nil {
			log.Printf("[desktop-bot-understand] phase=%s pace=%s attempt=%d elapsed=%s earlier=%d text_len=%d schedule_err=%v",
				phase, understood.Pace, attempt, time.Since(started).Round(time.Millisecond), len(earlier), utf8.RuneCountInString(text), err)
			return desktopBotConfirmOrLost(desktopBotReadingUnscheduled, "timed work without a time", text, phase, earlier), nil
		}
		// An empty order stays a reply only when the model said so, or when this
		// message only keeps or withdraws an identity. A direct or confirm reading
		// that forgot the order is a failed reading: it is not chat, and it is not sent.
		if err := desktopBotEmptyOrder(understood, phase); err != nil {
			log.Printf("[desktop-bot-understand] phase=%s pace=%s attempt=%d elapsed=%s earlier=%d text_len=%d empty_order=%v",
				phase, understood.Pace, attempt, time.Since(started).Round(time.Millisecond), len(earlier), utf8.RuneCountInString(text), err)
			lastClass, lastOutcome = desktopBotReadingUnread, "order was empty"
			if attempt < desktopBotReadAttempts && !useFallback && time.Until(deadline) > 5*time.Second {
				useFallback = true
				log.Printf("[desktop-bot-understand] phase=%s attempt=%d retrying on fallback model after empty order", phase, attempt)
				continue
			}
			break
		}
		if understood.Pace == "direct" {
			understood.Told = desktopBotAcceptTold(understood, text, lang)
		}
		log.Printf("[desktop-bot-understand] phase=%s pace=%s lang=%s attempt=%d elapsed=%s earlier=%d text_len=%d told_len=%d instruction_len=%d persona_len=%d",
			phase, understood.Pace, lang, attempt, time.Since(started).Round(time.Millisecond), len(earlier),
			utf8.RuneCountInString(text), utf8.RuneCountInString(understood.Told), utf8.RuneCountInString(understood.Instruction), utf8.RuneCountInString(understood.Persona))
		return understood, nil
	}
	fb := desktopBotVerbatimFallback(lastClass, lastOutcome, text, phase, earlier)
	fb.Told = "" // the GUI asks WriteDesktopBotAck for the sentence
	return fb, nil
}

// desktopBotVerbatimFallback is what happens when every attempt failed on a
// real message: the person's own words go to the bot as the order, exactly
// as the "send my own words" button would, instead of parking the turn
// behind a red line and two buttons. Told stays empty so the client shows
// the person's words, not an invented sentence. The client still holds a
// destructive order for one confirmation (desktopBotDestructive).
func desktopBotVerbatimFallback(class, outcome, text, phase string, earlier []string) *DesktopBotUnderstanding {
	log.Printf("[desktop-bot-understand] verbatim fallback class=%s outcome=%s phase=%s earlier=%d text_len=%d", class, outcome, phase, len(earlier), utf8.RuneCountInString(text))
	return &DesktopBotUnderstanding{
		Instruction: clipDesktopBotOrder(text, desktopBotUnderstandOrderMax),
		Pace:        "direct",
		Fallback:    class,
	}
}

// desktopBotConfirmOrLost reports a reading that was not made. It does not
// rebuild an order from the stock confirmation sentence or from earlier lines.
func desktopBotConfirmOrLost(class, outcome, text, phase string, earlier []string) *DesktopBotUnderstanding {
	return desktopBotReadingLost(class, outcome, text, phase, len(earlier))
}

// desktopBotReadingLost reports a reading that was not made. No order, no
// sentence, and no schedule: an empty result cannot reach the worker, so the
// only thing a caller can do with it is tell the truth and keep the person's
// own words.
func desktopBotReadingLost(class, outcome, text, phase string, earlier int) *DesktopBotUnderstanding {
	log.Printf("[desktop-bot-understand] no reading class=%s outcome=%s phase=%s earlier=%d text_len=%d", class, outcome, phase, earlier, utf8.RuneCountInString(text))
	return &DesktopBotUnderstanding{Reading: class}
}

// desktopBotReadFaultClears says whether waiting is likely to fix this failure:
// the route, the server, or a credit window blinked, things that cost almost
// nothing and pass on their own. A timeout deliberately does not qualify. When
// the budget itself fired, the attempt already spent the whole allowance one
// reading gets, and another slow generation is not bought with the remainder.
// A malformed answer does not qualify either — that is a matter for the parse.
func desktopBotReadFaultClears(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	if desktopBotReadTimeout(err.Error()) {
		return false
	}
	return isNetworkLLMRetryError(err) || isRetryableLLMError(err)
}

// desktopBotReadSleep pauses between attempts. It is a var so a test does not
// have to sit out a server's retry hint.
var desktopBotReadSleep = func(ctx context.Context, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// desktopBotReadRetryWait is the pause an upstream asked for before the next
// attempt — Qoder's queue reply names retryAfterSeconds. The hint is capped
// at half of the budget that remains: waiting longer leaves the retry no
// room to finish, which is the same failure reported later.
func desktopBotReadRetryWait(err error, deadline time.Time) time.Duration {
	hint := llmRetryAfterHint(err, time.Now())
	if hint <= 0 {
		return 0
	}
	wait := hint
	if max := time.Until(deadline) / 2; wait > max {
		wait = max
		log.Printf("[desktop-bot-understand] upstream retry hint %s capped to %s by the reading's remaining budget",
			hint.Round(time.Millisecond), wait.Round(time.Millisecond))
	}
	return wait
}

// desktopBotReadTimeout names the timeout markers the shared classifier counts
// as network errors, which they are — just not the retryable kind here. The
// strings are deliberate: net.Error.Timeout() also fires for fast connect and
// handshake timeouts, which leave most of the budget unspent and are worth one
// more try. Only the attempt's own spent allowance must not be retried.
func desktopBotReadTimeout(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "client.timeout") || strings.Contains(lower, "context deadline exceeded")
}

// desktopBotPace keeps a missing or unknown pace on the safe side. A plan
// turn that forgot the field still waits for one confirmation. An execute
// turn that forgot it starts the work.
func desktopBotPace(pace, phase string) string {
	switch strings.ToLower(strings.TrimSpace(pace)) {
	case "reply":
		if phase == "execute" {
			return "direct"
		}
		return "reply"
	case "direct", "confirm", "ask", "schedule", "status":
		return strings.ToLower(strings.TrimSpace(pace))
	}
	if phase == "execute" {
		return "direct"
	}
	return "confirm"
}

// desktopBotEmptyOrder refuses a direct or confirm reading that has nothing
// for the worker. Reply and ask already name their outcome. A persona,
// including a single hyphen that withdraws one, keeps a plan turn a reply.
// An execute turn is not turned into chat: a confirmed job with no order fails.
func desktopBotEmptyOrder(understood *DesktopBotUnderstanding, phase string) error {
	if understood == nil || understood.Instruction != "" || understood.Pace == "ask" || understood.Pace == "reply" || understood.Pace == "status" {
		return nil
	}
	if understood.Pace == "schedule" && understood.Schedule != nil && understood.Schedule.Mode == "cancel" {
		return nil
	}
	if phase != "execute" && strings.TrimSpace(understood.Persona) != "" {
		understood.Pace = "reply"
		return nil
	}
	return fmt.Errorf("desktop bot understanding omitted the work order")
}

// normalizeDesktopBotSchedule keeps a schedule only on a schedule reading.
// A repeat uses minutes. A clock time uses hour and minute. Anything else
// is a failed reading, not a one-off job.
func normalizeDesktopBotSchedule(understood *DesktopBotUnderstanding) error {
	if understood == nil {
		return nil
	}
	if understood.Pace != "schedule" {
		understood.Schedule = nil
		return nil
	}
	spec := understood.Schedule
	if spec == nil {
		return fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	mode := strings.ToLower(strings.TrimSpace(spec.Mode))
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "cancel" {
		return fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	spec.Mode = mode
	spec.Name = clipRunes(strings.TrimSpace(spec.Name), 80)
	if spec.DayOfWeek != nil {
		day := *spec.DayOfWeek
		if day < 0 || day > 6 {
			return fmt.Errorf("desktop bot understanding omitted the schedule")
		}
	}
	if mode == "cancel" {
		spec.IntervalMinutes = 0
		spec.Hour = nil
		spec.Minute = nil
		return nil
	}
	if understood.Instruction == "" {
		spec.Name = ""
	} else if spec.Name == "" {
		spec.Name = clipRunes(understood.Instruction, 40)
	}
	if spec.IntervalMinutes > 0 {
		if spec.IntervalMinutes > 7*24*60 {
			return fmt.Errorf("desktop bot understanding omitted the schedule")
		}
		spec.Hour = nil
		spec.Minute = nil
		return nil
	}
	if spec.Hour == nil || spec.Minute == nil || *spec.Hour < 0 || *spec.Hour > 23 || *spec.Minute < 0 || *spec.Minute > 59 {
		return fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	spec.IntervalMinutes = 0
	return nil
}

func desktopBotPhase(phase string) string {
	if strings.TrimSpace(phase) == "execute" {
		return "execute"
	}
	return "plan"
}

func desktopBotProtocolUnderstanding(text, phase string) (*DesktopBotUnderstanding, bool) {
	if text == desktopBotReturnTask || desktopBotSavedTask.MatchString(text) || desktopBotFilledTask.MatchString(text) {
		return &DesktopBotUnderstanding{Told: text, Instruction: text}, true
	}
	return nil, false
}

func desktopBotUnderstandUser(text, phase, lang string, earlier []string, persona string, userPref ...string) string {
	if lang == "" {
		lang = "the language of the latest message"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "language: %s\nphase: %s\npersona already kept:\n", lang, phase)
	persona = strings.TrimSpace(persona)
	if persona == "" {
		b.WriteString("(none)\n")
	} else {
		fmt.Fprintf(&b, "%s\n", clipRunes(persona, desktopBotUnderstandPersonaMax))
	}
	if len(userPref) > 0 && strings.TrimSpace(userPref[0]) != "" {
		fmt.Fprintf(&b, "user preferences:\n%s\n", clipRunes(strings.TrimSpace(userPref[0]), 600))
	}
	b.WriteString("earlier user messages:\n")
	kept := 0
	start := 0
	if len(earlier) > desktopBotUnderstandEarlierMax {
		start = len(earlier) - desktopBotUnderstandEarlierMax
	}
	for _, line := range earlier[start:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept++
		fmt.Fprintf(&b, "%d. %s\n", kept, clipDesktopBotOrder(line, desktopBotUnderstandLineMax))
	}
	if kept == 0 {
		b.WriteString("(none)\n")
	}
	fmt.Fprintf(&b, "latest message:\n%s\n", text)
	return b.String()
}

// desktopBotUnderstandText prefers whichever side actually parses. Content wins
// when it does. Reasoning is the fallback, including JSON that only exists
// inside a think block. When neither parses, the non-empty body is returned
// so the caller still reports the parse error.
func desktopBotUnderstandText(content, reasoning string) string {
	content = strings.TrimSpace(content)
	reasoning = strings.TrimSpace(reasoning)
	if content != "" {
		if _, err := parseDesktopBotUnderstanding(content); err == nil {
			return content
		}
	}
	if reasoning != "" && reasoning != content {
		if _, err := parseDesktopBotUnderstanding(reasoning); err == nil {
			return reasoning
		}
	}
	if content != "" {
		return content
	}
	return reasoning
}

func parseDesktopBotUnderstanding(raw string) (*DesktopBotUnderstanding, error) {
	if parsed, ok := lastDesktopBotUnderstanding(raw); ok {
		return parsed, nil
	}
	// An unclosed quote inside a think block hides a later object from the
	// brace scan. The block is not the answer when the object sits after it.
	// stripThinkTags leaves this block in place, so drop it locally.
	stripped := desktopBotWithoutThink(raw)
	if stripped != strings.TrimSpace(raw) {
		if parsed, ok := lastDesktopBotUnderstanding(stripped); ok {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("desktop bot understanding was not told and instruction")
}

// lastDesktopBotUnderstanding keeps the last balanced object that has both
// fields. A model may mention a brace, or repeat an empty placeholder, before
// the object it means. The placeholder with both fields set to "..." is skipped.
func lastDesktopBotUnderstanding(raw string) (*DesktopBotUnderstanding, bool) {
	var found *DesktopBotUnderstanding
	for _, body := range desktopBotJSONObjects(raw) {
		var parsed DesktopBotUnderstanding
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			continue
		}
		parsed.Told = strings.TrimSpace(parsed.Told)
		parsed.Instruction = strings.TrimSpace(parsed.Instruction)
		parsed.Persona = strings.TrimSpace(parsed.Persona)
		parsed.SupplementOf = strings.TrimSpace(parsed.SupplementOf)
		// A model does not get to say what its own reading is worth: that is
		// decided by whoever watched the call.
		parsed.Reading = ""
		if parsed.Persona == "..." {
			parsed.Persona = ""
		}
		// A hyphen is the withdrawal. Anything else is the identity to keep.
		if parsed.Persona != "-" {
			parsed.Persona = clipRunes(parsed.Persona, desktopBotUnderstandPersonaMax)
		}
		// Claiming to supplement earlier work but producing no merged order is
		// a reading that did not fuse. It fails here rather than degrading into
		// an incomplete order: half-merged work is worse than asking again.
		if parsed.SupplementOf != "" && parsed.Instruction == "" {
			continue
		}
		// told is required, except a status question: the sentence the person
		// sees is the query result, and a made-up told would be a status nobody
		// checked. An empty instruction stays in the reading; pace decides
		// whether it is a reply, a status question, or a missing order.
		if parsed.Told == "" && parsed.Instruction == "" && !strings.EqualFold(strings.TrimSpace(parsed.Pace), "status") {
			continue
		}
		if parsed.Told == "..." && parsed.Instruction == "..." {
			continue
		}
		if parsed.Told == "..." && parsed.Instruction == "" && parsed.Persona == "" {
			continue
		}
		parsed.Told = clipRunes(parsed.Told, desktopBotUnderstandToldMax)
		parsed.Instruction = clipDesktopBotOrder(parsed.Instruction, desktopBotUnderstandOrderMax)
		found = &parsed
	}
	if found == nil {
		return nil, false
	}
	return found, true
}

func desktopBotWithoutThink(raw string) string {
	var b strings.Builder
	rest := raw
	for {
		start := strings.Index(rest, "<think>")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		rest = rest[start+len("<think>"):]
		end := strings.Index(rest, "</think>")
		if end < 0 {
			b.WriteString(rest)
			break
		}
		rest = rest[end+len("</think>"):]
	}
	return strings.TrimSpace(stripThinkTags(b.String()))
}

func desktopBotJSONObjects(raw string) []string {
	var out []string
	var starts []int
	inStr := false
	esc := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			starts = append(starts, i)
		case '}':
			if len(starts) == 0 {
				continue
			}
			start := starts[len(starts)-1]
			starts = starts[:len(starts)-1]
			out = append(out, raw[start:i+1])
		}
	}
	return out
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// clipDesktopBotOrder is clipRunes for a work order. A trailing [素材] list
// is kept whole, and the prose in front of it is what gets shortened. Cutting
// the list off the end would drop the paths the worker has to open.
func clipDesktopBotOrder(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	suffix := desktopBotMaterialsSuffix(s)
	if suffix == "" {
		return clipRunes(s, max)
	}
	if utf8.RuneCountInString(suffix) >= max {
		if kept := clipMaterialLines(suffix, max); desktopBotHasMaterials(kept) && utf8.RuneCountInString(kept) <= max {
			return kept
		}
		return clipRunes(s, max)
	}
	prose := strings.TrimRight(strings.TrimSuffix(s, suffix), "\n")
	head := clipRunes(prose, max-utf8.RuneCountInString(suffix))
	if strings.TrimSpace(head) == "" {
		return suffix
	}
	return head + suffix
}

// desktopBotMaterialsSuffix is the trailing [素材] list, including the newline
// before the marker when the list is not the whole message.
func desktopBotMaterialsSuffix(message string) string {
	if !desktopBotHasMaterials(message) {
		return ""
	}
	at := strings.LastIndex(message, "[素材]\n")
	if at > 0 {
		at--
	}
	return message[at:]
}

// clipMaterialLines keeps the marker and as many whole path lines as fit.
// A path cut in the middle is not a path.
func clipMaterialLines(suffix string, max int) string {
	body := strings.TrimLeft(suffix, "\n")
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		next := line
		if b.Len() > 0 {
			next = "\n" + line
		}
		if utf8.RuneCountInString(b.String()+next) > max {
			break
		}
		b.WriteString(next)
	}
	return b.String()
}

// DesktopTaskSupplementEval is the verdict on whether a new user message is
// supplementary info for the in-flight background task or a new task.
type DesktopTaskSupplementEval struct {
	IsSupplement bool   `json:"is_supplement"`
	Reason       string `json:"reason,omitempty"`
	Instruction  string `json:"instruction,omitempty"`
}

// desktopSupplementDisabled reports the kill switch from design doc §3.1f:
// MACLAW_SUPPLEMENT=off turns the whole supplement feature off, and every
// message goes out as an ordinary new task instead. Reading the variable at
// call time (it is a crash-only toggle, not a hot path) keeps the switch
// effective for a process that is restarted with it set, and lets a dev
// process flip it without a rebuild.
func desktopSupplementDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_SUPPLEMENT")), "off")
}

const desktopTaskSupplementSystem = `You are an evaluator for a cloud desktop AI assistant. An active task is currently executing in the background on the cloud desktop. The user has just sent a new message.
Determine whether the new message is:
1. A supplement, refinement, additional constraint, filter, format requirement, or modification to the currently running active task (e.g. "只要前5条", "按日期倒序", "也带上截图", "保存为markdown", "格式改成表格", "顺便也把作者名字记下来").
2. A completely new, independent task (e.g. "打开另一个网站", "查询天气", "还需要在另一台机器上做另一件事", an unrelated request). A different job is a new task even when the active one is still running. Words like 还, 另外, 同时, or also do not make it a supplement unless they change the active task itself.
3. Small talk or a general query (e.g. "你好", "你是谁").

Return JSON with:
- "is_supplement": true only if it changes the active task; false if it is a new task or small talk. A new task must not be injected into the active one.
- "reason": brief explanation.
- "instruction": if is_supplement is true, a concise directive summarizing the supplemental requirement to inject into the ongoing execution; otherwise empty string.`

// EvaluateDesktopTaskSupplement decides whether a message sent while a task is running
// is supplementary information for the running task or an independent new task.
func (a *App) EvaluateDesktopTaskSupplement(activeTask, newMessage, lang string) (*DesktopTaskSupplementEval, error) {
	activeTask = strings.TrimSpace(activeTask)
	newMessage = strings.TrimSpace(newMessage)
	if activeTask == "" || newMessage == "" {
		return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "empty input"}, nil
	}
	if newMessage == desktopBotConfirmTask || newMessage == desktopBotReturnTask || desktopBotFilledTask.MatchString(newMessage) || desktopBotSavedTask.MatchString(newMessage) {
		return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "protocol command"}, nil
	}
	// Kill switch (§3.1f): with the feature off the classifier short-circuits
	// to "new task", so the caller sends the words through the ordinary
	// SendDesktopBotTask path and nothing is ever offered to a running turn.
	if desktopSupplementDisabled() {
		return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "supplement disabled"}, nil
	}
	// Files attached while a job runs are materials for that job. The words
	// go in whole so the ~/素材/ paths reach the worker unchanged.
	if desktopBotHasMaterials(newMessage) {
		return &DesktopTaskSupplementEval{IsSupplement: true, Reason: "materials attached", Instruction: newMessage}, nil
	}

	if a != nil {
		if hub := a.hubClient(); hub != nil {
			if handler := hub.ensureIMHandler(); handler != nil {
				userPrompt := fmt.Sprintf("active running task:\n%s\n\nnew message:\n%s\n\nlanguage:\n%s",
					clipRunes(activeTask, 500), clipRunes(newMessage, 500), lang)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				res, err := handler.LLMClassify(ctx, LLMClassifyRequest{
					SystemPrompt:      desktopTaskSupplementSystem,
					UserMessage:       userPrompt,
					TimeoutSec:        12,
					Tag:               "desktop-bot-eval-supplement",
					PreferLightweight: true,
				})
				if err == nil && res != nil {
					content := desktopBotUnderstandText(res.Text, res.Reasoning)
					if content != "" {
						var eval DesktopTaskSupplementEval
						for _, block := range desktopBotJSONObjects(content) {
							if err := json.Unmarshal([]byte(block), &eval); err == nil {
								if eval.IsSupplement && eval.Instruction == "" {
									eval.Instruction = newMessage
								}
								return &eval, nil
							}
						}
					}
				}
			}
		}
	}

	return heuristicDesktopTaskSupplement(activeTask, newMessage), nil
}

func heuristicDesktopTaskSupplement(activeTask, newMessage string) *DesktopTaskSupplementEval {
	lower := strings.ToLower(newMessage)
	if strings.HasPrefix(lower, "新任务") || strings.HasPrefix(lower, "新的任务") || strings.HasPrefix(lower, "另外做") || strings.HasPrefix(lower, "做完这个后") || strings.HasPrefix(lower, "new task") {
		return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "explicit new task"}
	}
	if lower == "你好" || lower == "您好" || lower == "hello" || lower == "hi" || lower == "谢谢" || lower == "多谢" || lower == "thanks" || lower == "thank you" || lower == "你是谁" || lower == "你是谁呀" || lower == "你是谁？" {
		return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "small talk"}
	}
	// Short words such as 也, 还, 另外, 同时, 按, and 截图 show up in a
	// different job just as often as in a correction. Matching them here
	// would inject that job into the run that is already going. The model
	// still makes that distinction; this list is only the fallback, and a
	// miss starts a separate job instead of rewriting the live one.
	supplementKeywords := []string{
		"补充", "再加", "记得", "改成", "只要", "不要", "加上", "格式改成", "额外",
		"过滤", "仅限", "导出为", "保存为",
		"filter by", "save as", "export as", "remember to", "change to",
	}
	for _, kw := range supplementKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return &DesktopTaskSupplementEval{
				IsSupplement: true,
				Reason:       fmt.Sprintf("matched supplement keyword %q", kw),
				Instruction:  newMessage,
			}
		}
	}
	return &DesktopTaskSupplementEval{IsSupplement: false, Reason: "not classified as supplement"}
}

// desktopBotEnglish reports whether the acceptance line should be English.
func desktopBotEnglish(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en")
}

// desktopBotSameWords compares two lines ignoring spaces and punctuation, so
// a told that only parrots the person's message is caught.
func desktopBotSameWords(a, b string) bool {
	norm := func(s string) string {
		var out []rune
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				out = append(out, unicode.ToLower(r))
			}
		}
		return string(out)
	}
	na, nb := norm(a), norm(b)
	return na == "" || na == nb || (len(nb) > 0 && strings.Contains(nb, na) && utf8.RuneCountInString(na)*10 >= utf8.RuneCountInString(nb)*8)
}

// desktopBotAcceptTold keeps the model's own sentence for a job it took,
// and clears it when there is nothing worth showing: empty, a parrot of the
// message or the card, a verbatim fallback, or files the sentence does not
// name. An empty told tells the GUI to start the job now and ask
// WriteDesktopBotAck for the sentence. There is no stock sentence here;
// options stay structured for the chips.
func desktopBotAcceptTold(u *DesktopBotUnderstanding, text, lang string) string {
	u.Options = desktopBotOptions(u.Options)
	told := strings.TrimSpace(u.Told)
	if told == "STOP_WORK" {
		return told
	}
	if u.Fallback != "" || told == "" || desktopBotSameWords(told, text) || desktopBotSameWords(told, u.Instruction) || desktopBotSameWords(told, desktopBotWordsBeforeMaterials(text)) {
		return ""
	}
	if names := desktopBotMaterialNames(text); len(names) > 0 && !desktopBotMentionsAny(told, names) {
		return ""
	}
	return told
}

// desktopBotOptions keeps 2 to 4 distinct, short, non-empty choices, or none.
func desktopBotOptions(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, o := range in {
		o = strings.TrimSpace(strings.Trim(strings.TrimSpace(o), "「」\"'。.，,"))
		if o == "" || seen[o] || utf8.RuneCountInString(o) > 30 {
			continue
		}
		seen[o] = true
		out = append(out, o)
		if len(out) == 4 {
			break
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// desktopBotHasMaterials reports a trailing [素材] list of ~/素材/ paths, as
// the GUI appends when files are attached to a message.
func desktopBotHasMaterials(message string) bool {
	at := strings.LastIndex(message, "[素材]\n")
	if at < 0 || (at > 0 && message[at-1] != '\n') {
		return false
	}
	lines := strings.Split(strings.TrimSpace(message[at+len("[素材]\n"):]), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "- ~/素材/") {
			return false
		}
	}
	return len(lines) > 0
}
