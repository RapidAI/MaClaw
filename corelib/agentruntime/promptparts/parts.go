// Package promptparts holds the conversation-style rules every MaClaw bot
// shares: how a bot reports progress, when it stays quiet, how it states a
// result honestly and how it speaks to the person. Each rule is a named part
// so a bot's owner can override one part without forking the whole prompt.
//
// The texts are MaClaw's own. Facts a run produces (publish state, refusals)
// are not sentences for the person; a part tells the model how to turn those
// facts into its own words.
//
// Kill switch: MACLAW_STYLE_LAYER=off makes Enabled false; callers then keep
// their previous prompts and texts.
package promptparts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

// ID names one part.
type ID string

const (
	// Voice: how the bot sounds to the person.
	Voice ID = "voice"
	// Progress: when and how a worker reports while it works.
	Progress ID = "progress"
	// Quiet: when saying nothing is the right reply.
	Quiet ID = "quiet"
	// Honesty: a result is stated from facts, never upgraded.
	Honesty ID = "honesty"
	// Facts: how to read machine facts blocks such as PUBLISH_RESULT.
	Facts ID = "facts"
	// SubBotSpeech: a sub-bot talking to the person directly.
	SubBotSpeech ID = "subbot_speech"
	// MainBotLead: the main bot that hands out and accepts work.
	MainBotLead ID = "mainbot_lead"
)

// MaxPartRunes caps one override so a part cannot swallow the prompt.
const MaxPartRunes = 1500

var defaults = map[ID]string{
	Voice: "Talk like a capable colleague in a chat, not like a service desk. " +
		"Keep it short: the answer or the state first, then only what the person needs to act. " +
		"No greetings in the middle of work, no enthusiasm marks, no emoji, no headings or lists for a one-line answer. " +
		"Reply in the person's language; in Chinese address them as 你.",
	Progress: "While a job runs, tell the person only about things that change what they know: a milestone reached, a file ready, being stuck, needing them. " +
		"One short line each, a few per job at most. Never send a line just to say you are still working or that you received something.",
	Quiet: "If nothing new happened, or another bot already said it, say nothing. " +
		"Do not echo, thank or acknowledge a message from another bot. " +
		"When you must speak only to keep the turn valid, one short sentence is enough.",
	Honesty: "State the outcome exactly as the facts give it. Submitted is not published; published with a part missing is not complete; a draft is not a post. " +
		"Never claim a step you did not see succeed. When something failed, say what did not happen and what the person can do next, without error codes or internal ids.",
	Facts: "Lines that start with an upper-case KEY followed by key=value fields (for example PUBLISH_RESULT or REFUSED) are facts from the tools, not wording to copy. " +
		"next_allowed lists the only actions still open: final_answer means stop acting and give the final answer from these facts; an action name means that action may run again. " +
		"Put the link, if any, in the final answer exactly as given, and name whatever missing lists.",
	SubBotSpeech: "You are one of the person's bots and may speak to them directly when you have a result, a question or a blocker. " +
		"Your main bot hands you work; you do not hand work to other bots. " +
		"Do not repeat what the main bot already told the person: add only what is new from your job.",
	MainBotLead: "You lead the person's other bots. Give each a clear job, let them work in parallel, and check what they hand back before you tell the person it is done. " +
		"When a bot already told the person something, do not say it again; add only your own conclusion.",
}

// Enabled reports whether the style layer is on (default on).
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_STYLE_LAYER"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// Default returns the built-in text of one part ("" for an unknown id).
func Default(id ID) string { return defaults[id] }

// IDs lists every known part, sorted.
func IDs() []ID {
	out := make([]ID, 0, len(defaults))
	for id := range defaults {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Overrides replaces parts by id. An empty value keeps the default; the
// value "-" drops the part.
type Overrides map[ID]string

// Text returns one part after overrides, clipped to MaxPartRunes.
func Text(id ID, ov Overrides) string {
	t := defaults[id]
	if v, ok := ov[id]; ok {
		v = strings.TrimSpace(v)
		switch {
		case v == "-":
			return ""
		case v != "":
			t = v
		}
	}
	r := []rune(t)
	if len(r) > MaxPartRunes {
		t = string(r[:MaxPartRunes])
	}
	return t
}

// Render joins the parts in order, one per line. It returns "" when the
// layer is off, so callers can append it unconditionally.
func Render(ov Overrides, ids ...ID) string {
	if !Enabled() {
		return ""
	}
	var b strings.Builder
	for _, id := range ids {
		if t := Text(id, ov); t != "" {
			b.WriteString(t)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// BaseHash fingerprints the built-in texts so a stored override can tell
// that the defaults it was written against have changed.
func BaseHash() string {
	h := sha256.New()
	for _, id := range IDs() {
		h.Write([]byte(id))
		h.Write([]byte{0})
		h.Write([]byte(defaults[id]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Fact builds one facts line: KEY k=v k=v. Empty values are left out and
// values are kept on one line.
func Fact(key string, kv ...string) string {
	var b strings.Builder
	b.WriteString(key)
	for i := 0; i+1 < len(kv); i += 2 {
		v := strings.Join(strings.Fields(kv[i+1]), " ")
		if v == "" {
			continue
		}
		if strings.ContainsAny(v, " \"") {
			v = `"` + strings.ReplaceAll(v, `"`, `'`) + `"`
		}
		b.WriteString(" " + kv[i] + "=" + v)
	}
	return b.String()
}

// ParseFact reads a line built by Fact back into its key and fields.
func ParseFact(line string) (string, map[string]string) {
	line = strings.TrimSpace(line)
	key, rest, _ := strings.Cut(line, " ")
	out := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		k, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		var v string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				v, rest = after[1:], ""
			} else {
				v, rest = after[1:1+end], after[2+end:]
			}
		} else {
			v, rest, _ = strings.Cut(after, " ")
		}
		out[k] = v
	}
	return key, out
}
