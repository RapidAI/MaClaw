package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// DesktopBotAckRequest asks for the one chat line a desktop bot says when
// it takes a job, adds files to a running job, or keeps files for later.
// The GUI has already started the work; this sentence only follows it.
type DesktopBotAckRequest struct {
	// Kind is accept, supplement, files, stopping, stopped, idle, or pause.
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
}

const desktopBotAckSystem = `You are one desktop bot speaking to the person who owns you, in a chat. Write the single line you say right now. The work has already started; this line only tells them.
Speak as a person would: short (one or two sentences, at most about 40 Chinese characters or 25 English words), natural, warm but plain. No customer-service phrases, no exclamation-mark enthusiasm, no emoji, no lists, no markdown. Do not repeat their message back. Do not ask them to confirm.
Use the kept persona's name and tone when one is given.
kind accept: say you are on it, in your own words, naming the goal briefly.
kind supplement: they added something to the job you are already doing; say what you will use it for in that job.
kind files: they sent files with no job; say you have them and will use them when asked.
kind stopped: they asked you to stop and it has stopped; say so plainly, naming the job if given.
kind idle: they asked you to stop but nothing was running; say so plainly.
kind pause: they asked you to hold; say you paused and ask whether to stop it entirely or only change what it contains.
kind stopping: they asked you to stop and the work is still winding down; say plainly that it is still stopping and you will tell them once nothing is running. Do not say it has stopped.
When files are listed, name each (at most three, in 《》 for Chinese or quotes for English) and say what they are for.
When options are listed, say you are starting with the first and that they can tap another or add a line.
When unclear is given, say which part you are not sure about and what you are assuming, in a few words.
When fallback is true, say you are going with their exact words and they can stop you if it is off.
Write in the language named. Reply with the line only.`

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
	user := desktopBotAckUser(req)
	var lastErr error
	for i, attempt := range desktopBotAckAttempts {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(attempt.timeoutSec+2)*time.Second)
		raw, err := desktopBotUnderstandLLM(ctx, a, desktopBotUnderstandRequest{
			systemPrompt: desktopBotAckSystem,
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
	return line
}
