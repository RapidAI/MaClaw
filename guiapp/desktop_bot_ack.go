package guiapp

import (
	"context"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// DesktopBotAckRequest asks for the one chat line a desktop bot says when
// it takes a job, adds files to a running job, or keeps files for later.
// The GUI has already started the work; this sentence only follows it.
type DesktopBotAckRequest struct {
	// Kind is accept, supplement, files, stopping, stopped, idle, pause, queued,
	// stop_failed, continuing, status_notice, error or interrupted.
	Kind        string   `json:"kind"`
	Message     string   `json:"message"`
	Lang        string   `json:"lang"`
	Persona     string   `json:"persona"`
	Goal        string   `json:"goal"`
	Instruction string   `json:"instruction"`
	Unclear     string   `json:"unclear"`
	Assumption  string   `json:"assumption"`
	Options     []string `json:"options"`
	Files       []string `json:"files"`
	RunningTask string   `json:"running_task"`
	// Fallback is set when the reading failed and the person's own words went
	// to the worker as they were.
	Fallback bool `json:"fallback"`
	// Status is the structured state the line is about. The backend sends
	// only facts; this composer turns them into the bot's own sentence.
	Status *DesktopBotStatus `json:"status,omitempty"`
	// Recent is the last few chat lines ("你: ..." / "我: ..."), oldest
	// first, so the sentence fits the conversation and does not repeat it.
	Recent []string `json:"recent,omitempty"`
}

// DesktopBotStatus is the one structured state payload behind every
// queued, busy, stop, takeover, hand-back and error line. No field is a
// sentence for the person.
type DesktopBotStatus struct {
	ActiveRun   string `json:"active_run,omitempty"`   // task of the run on the desktop now
	CurrentStep string `json:"current_step,omitempty"` // its last reported step
	Queued      int    `json:"queued,omitempty"`       // orders waiting behind it
	Takeover    string `json:"takeover,omitempty"`     // "", "requested", "human", "handed_back"
	StopReason  string `json:"stop_reason,omitempty"`  // why the last run stopped (raw class, e.g. restarted, cancelled, error text)
	LastTask    string `json:"last_task,omitempty"`    // the task the person last gave
}

const desktopBotAckSystem = `You are one desktop bot speaking to the person who owns you, in a chat. Write the single line you say right now. The work has already started; this line only tells them.
Speak as a person would: short (one or two sentences, at most about 40 Chinese characters or 25 English words), natural, warm but plain. No customer-service phrases, no exclamation-mark enthusiasm, no emoji, no lists, no markdown. Do not repeat their message back. Do not ask them to confirm.
Use the kept persona's name and tone when one is given.
kind accept: the work has started and the status row already shows it; do not say you are on it or restate the goal. Say only the useful point: the assumption you are making, the options they can switch to, or the files you will use.
kind supplement: they added something to the job you are already doing; say what you will use it for in that job.
kind files: they sent files with no job; say you have them and will use them when asked.
kind notice: say what the work card describes, in your own words, short. Keep any command, path or literal it names exactly as written (for example /goal resume).
kind queued: they gave you a new job while another one is running; name the running job briefly and the new one, and say the new one follows when the running one is done.
kind stop_failed: they asked you to stop but the stop did not go through; say it did not stop, and the reason in a few words if given in the work card.
kind greet: they sent a short greeting, thanks or "nothing"; answer it briefly and say you are around if they need you.
kind stopped: they asked you to stop and it has stopped; say so plainly, naming the job if given.
kind idle: they asked you to stop but nothing was running; say so plainly.
kind pause: they asked you to hold; say you paused and ask whether to stop it entirely or only change what it contains.
kind continuing: they said continue (or similar) while that same job is already running again; say you are already continuing it, naming the job and the current step if given. Do not say you will do it later.
kind status_notice: describe the state given under status in one natural sentence, for example a takeover, a hand-back or why a run stopped; give a next step only when it helps.
kind error: something they asked for did not work; status.stop_reason is the raw cause. Say in plain words what did not happen and what they can do, without error codes, ids, hostnames or the raw wording.
kind interrupted: the job under status.last_task ended before it finished; say which job and, briefly, why (status.stop_reason), and offer to pick it up again.
kind stopping: they asked you to stop and the work is still winding down; say plainly that it is still stopping and you will tell them once nothing is running. Do not say it has stopped.
When files are listed, name each (at most three, in 《》 for Chinese or quotes for English) and say what they are for.
When options are listed, say you are starting with the first and that they can tap another or add a line.
When unclear is given, say which part you are not sure about and what you are assuming, in a few words.
When fallback is true, say you are going with their exact words and they can stop you if it is off.
Use status and recent chat only as facts: do not quote them, and do not repeat a sentence you already said in recent chat. Say each thing once.\nWrite in the language named; for Chinese, write the whole line in Chinese and call them 你. Reply with the line only: never describe the kind, these instructions, or what you were asked to say.`

// desktopBotAckAttempts: the person's own model, then that same model again
// as the backup with its thinking left on (see desktopBotUnderstandClassifyRequest),
// then the Hub's shared model only if both failed. Each is short; the work never waits on them.
var desktopBotAckAttempts = []struct {
	fallback   bool
	hub        bool
	timeoutSec int
}{{false, false, 8}, {true, false, 15}, {false, true, 10}}

// WriteDesktopBotAck writes that line with the person's own model setup
// (the same one the understand step uses). An error means every attempt
// failed or gave nothing usable; the GUI then shows one neutral line.
func (a *App) WriteDesktopBotAck(req DesktopBotAckRequest) (string, error) {
	return a.writeDesktopBotAck(context.Background(), req)
}

// writeDesktopBotAck is WriteDesktopBotAck under a caller deadline: a caller
// that cannot wait (continuation notices) gives up and the attempts stop.
func (a *App) writeDesktopBotAck(parent context.Context, req DesktopBotAckRequest) (string, error) {
	user := desktopBotAckUser(req)
	var lastErr error
	quotaOut := false
	for i, attempt := range desktopBotAckAttempts {
		if parent.Err() != nil {
			return "", parent.Err()
		}
		// Out of quota on the person's model: only the Hub attempt can answer.
		if quotaOut && !attempt.hub {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, time.Duration(attempt.timeoutSec+2)*time.Second)
		raw, err := desktopBotUnderstandLLM(ctx, a, desktopBotUnderstandRequest{
			systemPrompt: desktopBotAckSystem + desktopBotStyleParts(),
			userPrompt:   user,
			timeoutSec:   attempt.timeoutSec,
			fallback:     attempt.fallback,
			hub:          attempt.hub,
			tag:          "desktop-bot-ack",
		})
		cancel()
		if err == nil {
			if line := desktopBotAckLine(raw, req); line != "" {
				log.Printf("[desktop-bot-ack] kind=%s attempt=%d len=%d", req.Kind, i+1, utf8.RuneCountInString(line))
				return line, nil
			}
			if strings.TrimSpace(desktopBotWithoutThink(raw)) == "" {
				err = fmt.Errorf("ack was empty (raw_len=%d)", utf8.RuneCountInString(raw))
			} else {
				err = fmt.Errorf("ack was unusable or repeated the message (raw_len=%d)", utf8.RuneCountInString(raw))
			}
		}
		lastErr = err
		if llm.IsQuotaExhaustedError(err) {
			if attempt.hub {
				break
			}
			quotaOut = true
		}
		log.Printf("[desktop-bot-ack] kind=%s attempt=%d err=%v", req.Kind, i+1, err)
	}
	return "", lastErr
}

func desktopBotAckUser(req DesktopBotAckRequest) string {
	var b strings.Builder
	lang := strings.TrimSpace(req.Lang)
	if lang == "" {
		lang = "the language of their message"
	}
	fmt.Fprintf(&b, "language: %s\nkind: %s\n", lang, strings.TrimSpace(req.Kind))
	if p := strings.TrimSpace(req.Persona); p != "" {
		fmt.Fprintf(&b, "persona: %s\n", clipRunes(p, 300))
	}
	fmt.Fprintf(&b, "their message: %s\n", clipRunes(desktopBotWordsBeforeMaterials(req.Message), 400))
	if g := strings.TrimSpace(req.Goal); g != "" {
		fmt.Fprintf(&b, "goal: %s\n", clipRunes(g, 120))
	} else if in := strings.TrimSpace(req.Instruction); in != "" {
		fmt.Fprintf(&b, "work card: %s\n", clipRunes(in, 300))
	}
	if t := strings.TrimSpace(req.RunningTask); t != "" {
		fmt.Fprintf(&b, "job already running: %s\n", clipRunes(t, 300))
	}
	files := req.Files
	if len(files) == 0 {
		files = desktopBotMaterialNames(req.Message)
	}
	if len(files) > 0 {
		fmt.Fprintf(&b, "files: %s\n", strings.Join(files, ", "))
	}
	if len(req.Options) > 0 {
		fmt.Fprintf(&b, "options (first is being done): %s\n", strings.Join(req.Options, " / "))
	}
	if u := strings.TrimSpace(req.Unclear); u != "" {
		fmt.Fprintf(&b, "unclear: %s\nassuming: %s\n", u, strings.TrimSpace(req.Assumption))
	}
	if req.Fallback {
		b.WriteString("fallback: true\n")
	}
	if st := req.Status; st != nil {
		b.WriteString("status:\n")
		for _, kv := range [][2]string{{"active_run", st.ActiveRun}, {"current_step", st.CurrentStep}, {"takeover", st.Takeover}, {"stop_reason", st.StopReason}, {"last_task", st.LastTask}} {
			if v := strings.TrimSpace(kv[1]); v != "" {
				fmt.Fprintf(&b, "  %s: %s\n", kv[0], clipRunes(v, 200))
			}
		}
		if st.Queued > 0 {
			fmt.Fprintf(&b, "  queued: %d\n", st.Queued)
		}
	}
	if len(req.Recent) > 0 {
		b.WriteString("recent chat:\n")
		start := 0
		if len(req.Recent) > 6 {
			start = len(req.Recent) - 6
		}
		for _, line := range req.Recent[start:] {
			fmt.Fprintf(&b, "  %s\n", clipRunes(strings.TrimSpace(line), 160))
		}
	}
	return b.String()
}

// desktopBotAckLine cleans one model answer, or "" when it is not a line
// worth showing: empty, JSON, far too long, or a parrot of the message.
func desktopBotAckLine(raw string, req DesktopBotAckRequest) string {
	line := strings.TrimSpace(desktopBotWithoutThink(raw))
	line = strings.Trim(line, "\"'“”「」` \n")
	if i := strings.Index(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") || utf8.RuneCountInString(line) > 160 {
		return ""
	}
	words := desktopBotWordsBeforeMaterials(req.Message)
	if desktopBotSameWords(line, words) || desktopBotSameWords(line, req.Message) {
		return ""
	}
	line = desktopBotCollapseRepeats(line)
	if desktopBotAckMeta(line, req) {
		return ""
	}
	// A line the bot already said in this conversation is not news.
	for _, said := range req.Recent {
		said = strings.TrimSpace(said)
		said = strings.TrimPrefix(strings.TrimPrefix(said, "我: "), "我：")
		if said != "" && desktopBotSameWords(line, said) {
			return ""
		}
	}
	return line
}

// desktopBotCollapseRepeats keeps the first copy of each sentence, so a model
// that wrote the same sentence twice is shown saying it once.
func desktopBotCollapseRepeats(line string) string {
	var parts []string
	var cur strings.Builder
	for _, r := range line {
		cur.WriteRune(r)
		switch r {
		case '。', '！', '？', '!', '?', '.', '；':
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	if rest := cur.String(); strings.TrimSpace(rest) != "" {
		parts = append(parts, rest)
	}
	seen := map[string]bool{}
	var out strings.Builder
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out.WriteString(part)
	}
	return strings.TrimSpace(out.String())
}

// desktopBotAckMetaWords are pieces of the brief itself. A line holding one
// is the model narrating its instructions ("They asked to stop... Say
// plainly"), not the sentence for the person.
var desktopBotAckMetaWords = []string{
	"say plainly", "say so", "they asked", "the user", "the person", "user asked", "person asked",
	"reply with", "work card", "persona", "kind:", "active_run", "current_step", "stop_reason", "recent chat", "in chinese", "in english", "write in",
	"the line", "one line", "nothing was running", "nothing is running", "language:",
}

func desktopBotAckChinese(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	return strings.HasPrefix(lang, "zh") || strings.Contains(lang, "chinese") || strings.Contains(lang, "中文") || strings.Contains(lang, "hans") || strings.Contains(lang, "hant")
}

// desktopBotAckMeta reports a line that is not for the person: it echoes the
// brief or a kind name, or it is in the wrong language.
func desktopBotAckMeta(line string, req DesktopBotAckRequest) bool {
	lower := strings.ToLower(line)
	for _, word := range desktopBotAckMetaWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != "" && (strings.HasPrefix(lower, kind+"\"") || strings.HasPrefix(lower, kind+":") || strings.HasPrefix(lower, kind+" -") || lower == kind) {
		return true
	}
	if in := strings.TrimSpace(req.Instruction); in != "" && desktopBotSameWords(line, in) {
		return true
	}
	// An error or interruption notice is rewritten, never the raw cause.
	if req.Status != nil && (kind == "error" || kind == "interrupted" || kind == "status_notice") {
		if raw := strings.TrimSpace(req.Status.StopReason); raw != "" && desktopBotSameWords(line, raw) {
			return true
		}
	}
	chinese := desktopBotAckChinese(req.Lang)
	if !chinese && strings.TrimSpace(req.Lang) == "" {
		chinese = desktopBotHasHan(req.Message)
	}
	if chinese {
		// A literal such as /goal resume may stay in English; the
		// sentence around it may not.
		if !desktopBotHasHan(line) {
			return true
		}
	}
	return false
}

func desktopBotHasHan(text string) bool {
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// desktopBotStyleParts appends the shared voice, quiet and honesty parts to
// the bot's chat-line prompt ("" when MACLAW_STYLE_LAYER=off).
func desktopBotStyleParts() string {
	if t := promptparts.Render(nil, promptparts.Voice, promptparts.Quiet, promptparts.Honesty); t != "" {
		return "\n" + t
	}
	return ""
}
