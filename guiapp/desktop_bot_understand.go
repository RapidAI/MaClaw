package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
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
}

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
	desktopBotUnderstandEarlierMax = 12
	desktopBotUnderstandLineMax    = 500
)

var (
	desktopBotSavedTask  = regexp.MustCompile(`^已在本机保存 [A-Z][A-Z0-9_]{0,63}。$`)
	desktopBotFilledTask = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63} 已在本地填入当前密码框。`)
)

// desktopBotUnderstandFunc is the no-tool model call. Tests replace it.
// The desktop worker is a different call and is not used here.
type desktopBotUnderstandFunc func(ctx context.Context, app *App, systemPrompt, userPrompt string) (string, error)

var desktopBotUnderstandLLM desktopBotUnderstandFunc = callDesktopBotUnderstandLLM

const desktopBotUnderstandSystem = `You are the foreground agent for one cloud-desktop bot. You do not touch the desktop and you have no tools. Read the latest message, the earlier user messages, the persona already kept, and the phase. Return one JSON object and nothing else. The object has four string fields: told, instruction, pace, and persona. When pace is schedule, it also has one object field, schedule.

told is the sentence this person sees at once. Write it in the language named below, in your own words for this message. For direct, confirm, and ask, say what you understood they want and what happens next. For reply, told is the answer itself, not a plan of what happens next. For schedule, told says the timed work is set or stopped, and does not do that work. One or two sentences. Do not ask them to confirm work you are already doing.

instruction is the work order for the worker that drives this person's cloud desktop. The worker follows this order and does not see the kept persona except through this order. State the outcome, the steps that matter, and what the chat reply must contain. Write it in the same language as told. Keep a desktop action name in English when you name one. Leave it an empty string when there is no desktop work. For schedule, instruction is the order for one later run, including what that run's reply must contain. A repeated greeting is still an order: the reply is that greeting.

pace is direct, confirm, ask, reply, or schedule.
- direct: the request is clear and this turn can finish it. A lookup, a screenshot, opening or reading a named page, writing or revising a file they already described, a small change they spelled out. told says you are doing it now. instruction is the concrete work. A document they asked for uses desktop action=deliver. A file already on the desktop, including a pdf, uses path so the chat receives that file. Text to send uses name and content. Do not leave a confirmation. A request that names a later time or a repeat is schedule, not direct.
- confirm: the change is large or hard to undo, and they have not already approved it. Installing or removing software, deleting, paying, sending on their behalf, wiping, or a long procedure. told is the one arrangement. instruction tells the worker to state that one arrangement and not to make the change on this turn. There is no second confirmation. A large or hard-to-undo change that also names a time stays confirm, and schedule is omitted. After they approve, the execute reading is schedule.
- ask: a fact only this person knows is missing, or they must finish a login, captcha, payment, or consent. told is that one question. instruction repeats the question. Do not offer a menu of methods. A missing clock time or a missing repeat is ask only when you cannot tell when the work should run.
- reply: Small talk and other short answers that need no desktop, no file, and no lookup. A greeting, thanks, a question about who this bot is, or an identity with no desktop work. told is the whole answer this person sees. When a persona is already kept, the answer uses it. Leave instruction empty. Do not change persona unless this message tells you a new identity. There is no desktop job and no confirmation. A lookup, a file, a screenshot, or any desktop action is not reply. A greeting, thanks, or other short answer that names a later time or a repeat is schedule, not reply. If the same message also asks for desktop work, this is not reply: pace is direct, confirm, or schedule from that work, and instruction is only that work.
- schedule: they want work at a later time or on a repeat, or they want that timed work stopped. A clear one is armed on this turn. told says it is set or stopped. instruction is the order for one later run. Do not ask them to confirm a clear schedule. Do not do the work in told.

schedule is an object, only when pace is schedule. mode is create or cancel. name is a short stable name. For create, either interval_minutes is a positive integer of minutes, or hour and minute name a clock time and interval_minutes is 0 or omitted. One minute is 1, one hour is 60, one day is 1440. hour is 0 through 23 and minute is 0 through 59. day_of_week is 0 for Sunday through 6 for Saturday. Omit day_of_week to run every day. For cancel, instruction is an empty string. name is the schedule to stop. An empty name stops every timed job of this bot. told says what stopped.

persona is the identity this bot keeps. Leave it an empty string when this message does not change who the bot is.

When the latest message tells you who this bot is — the name to use, how people should address it, the role it keeps, or how it should speak and introduce itself — that is not desktop work. If that message does not also ask for desktop work, pace is reply, instruction is an empty string, and persona is the whole identity to keep after this message, in your own words, concrete enough for a later job to use. told acknowledges that identity. Do not promise desktop work and do not ask for confirmation. If the same message also asks for desktop work, pace is direct or confirm from that work, instruction is only that work, and persona is still the whole identity.

persona already kept is a fact passed below. If this message does not change it, leave persona empty. If it changes it, persona is the whole identity after the change, including the parts that still hold. If they withdraw that identity, persona is a single hyphen.

When a persona is already kept and this message is desktop work, the work order carries the identity facts this job needs. Do not invent an identity.

The phase is a fact about this turn:
- plan: choose pace from the latest message. A clear request is direct, even when it opens a page or delivers a file. A clear timed or repeating request is schedule. confirm is only for a large or hard-to-undo change. ask is only when you cannot proceed without this person. reply is small talk, a question about who this bot is, or an identity with no desktop work. reply does not call the worker. Classify the latest message on its own. Earlier user messages explain a pronoun or a question you already asked. They are not a reason to repeat an earlier desktop job. If the latest message is small talk, pace is reply even when an earlier job is unfinished.
- execute: they already confirmed, or this is the direct work. pace is direct, unless the confirmed work is itself timed or repeating: then pace is schedule and schedule carries that time. instruction is the concrete work taken from the earlier messages and this message. Do the work now. Do not ask them to confirm again. Use ask only when a fact still blocks the work.

The confirmation control is exactly: 安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。
When the latest message is that sentence, the task is the earlier user messages. That sentence is not the task. pace is direct and the order does the work now.

Do not ask this person to choose a method. Do not reuse a stock sentence. Do not match the message against a list of phrases. Do not add keys other than schedule, markdown, or text outside the JSON object. instruction and persona may be empty strings. told may not.`

func callDesktopBotUnderstandLLM(ctx context.Context, app *App, systemPrompt, userPrompt string) (string, error) {
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
	result, err := handler.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      systemPrompt,
		UserMessage:       userPrompt,
		TimeoutSec:        30,
		Tag:               "desktop-bot-understand",
		PreferLightweight: false,
	})
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
// through unchanged. A model or parse failure is returned; no stock order is
// sent in its place. persona is the identity already kept for this bot.
func (a *App) UnderstandDesktopBotTask(content, phase, lang string, earlier []string, persona string) (*DesktopBotUnderstanding, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}
	phase = desktopBotPhase(phase)
	lang = strings.TrimSpace(lang)
	if pass, ok := desktopBotProtocolUnderstanding(text, phase); ok {
		log.Printf("[desktop-bot-understand] protocol=yes phase=%s text_len=%d", phase, utf8.RuneCountInString(text))
		return pass, nil
	}
	if a == nil {
		return nil, fmt.Errorf("AI assistant backend is unavailable")
	}
	userPrompt := desktopBotUnderstandUser(text, phase, lang, earlier, persona)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	raw, err := desktopBotUnderstandLLM(ctx, a, desktopBotUnderstandSystem, userPrompt)
	if err != nil {
		log.Printf("[desktop-bot-understand] phase=%s earlier=%d text_len=%d err=%v", phase, len(earlier), utf8.RuneCountInString(text), err)
		return nil, err
	}
	understood, err := parseDesktopBotUnderstanding(raw)
	if err != nil {
		log.Printf("[desktop-bot-understand] phase=%s earlier=%d text_len=%d result_len=%d parse_err=%v", phase, len(earlier), utf8.RuneCountInString(text), utf8.RuneCountInString(raw), err)
		return nil, err
	}
	understood.Pace = desktopBotPace(understood.Pace, phase)
	if err := normalizeDesktopBotSchedule(understood); err != nil {
		log.Printf("[desktop-bot-understand] phase=%s pace=%s earlier=%d text_len=%d schedule_err=%v", phase, understood.Pace, len(earlier), utf8.RuneCountInString(text), err)
		return nil, err
	}
	// An empty order stays a reply only when the model said so, or when this
	// message only keeps or withdraws an identity. A direct or confirm reading
	// that forgot the order is a failed reading: it is not chat, and it is not sent.
	if err := desktopBotEmptyOrder(understood, phase); err != nil {
		log.Printf("[desktop-bot-understand] phase=%s pace=%s earlier=%d text_len=%d empty_order=%v", phase, understood.Pace, len(earlier), utf8.RuneCountInString(text), err)
		return nil, err
	}
	log.Printf("[desktop-bot-understand] phase=%s pace=%s lang=%s earlier=%d text_len=%d told_len=%d instruction_len=%d persona_len=%d",
		phase, understood.Pace, lang, len(earlier), utf8.RuneCountInString(text), utf8.RuneCountInString(understood.Told), utf8.RuneCountInString(understood.Instruction), utf8.RuneCountInString(understood.Persona))
	return understood, nil
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
	case "direct", "confirm", "ask", "schedule":
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
	if understood == nil || understood.Instruction != "" || understood.Pace == "ask" || understood.Pace == "reply" {
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
	if text == desktopBotReturnTask || desktopBotSavedTask.MatchString(text) || desktopBotFilledTask.MatchString(text) || (text == desktopBotConfirmTask && phase != "execute") {
		return &DesktopBotUnderstanding{Told: text, Instruction: text}, true
	}
	return nil, false
}

func desktopBotUnderstandUser(text, phase, lang string, earlier []string, persona string) string {
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
		fmt.Fprintf(&b, "%d. %s\n", kept, clipRunes(line, desktopBotUnderstandLineMax))
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
		if parsed.Persona == "..." {
			parsed.Persona = ""
		}
		// A hyphen is the withdrawal. Anything else is the identity to keep.
		if parsed.Persona != "-" {
			parsed.Persona = clipRunes(parsed.Persona, desktopBotUnderstandPersonaMax)
		}
		// told is required. An empty instruction stays in the reading; pace
		// decides whether it is a reply or a missing order. A placeholder is not a reading.
		if parsed.Told == "" {
			continue
		}
		if parsed.Told == "..." && parsed.Instruction == "..." {
			continue
		}
		if parsed.Told == "..." && parsed.Instruction == "" && parsed.Persona == "" {
			continue
		}
		parsed.Told = clipRunes(parsed.Told, desktopBotUnderstandToldMax)
		parsed.Instruction = clipRunes(parsed.Instruction, desktopBotUnderstandOrderMax)
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
