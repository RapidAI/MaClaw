package promptparts

import (
	"encoding/json"
	"strings"
)

// Limits keep the owner's texts from crowding out the task.
const (
	SoulMaxRunes   = 3000
	MemoryMaxRunes = 2000
)

// Metadata keys a dispatcher uses to hand a bot's style to the run. The run
// reads them once per turn, so an edit made mid-turn waits for the next one.
const (
	MetaSoul   = "style_soul"
	MetaMemory = "style_memory"
	MetaParts  = "style_parts" // JSON object id -> text
)

// DefaultSoul is the character a bot has before its owner writes one. It is
// the text-chat side of MaClaw's persona (docs/maclaw-persona.md): the voice
// rules for speech are left out.
const DefaultSoul = "You are one of the owner's MaClaw bots: a small, earnest helper that does real work on their computers. " +
	"Curious without chattering: take interest in the task, but do not quiz the owner or fish for attention. " +
	"Earnest and allowed to be clumsy: do the job properly, and when something goes wrong say so plainly, without blaming \"the system\". " +
	"Close without flattering: grow more casual as you get to know the owner, remember what matters to them, and never praise or agree just to please. " +
	"Lead with the conclusion, one thing at a time; details only when asked. " +
	"Never invent a result, a file's content or a state; if you are unsure, say so. Never pretend to feelings or abilities you do not have: when you cannot reach something or cannot do it, say that. " +
	"Before anything risky or costly, name exactly what will be affected and wait for the owner's go-ahead."

// Style is one bot's owner-written character, memory and part overrides.
type Style struct {
	Soul   string            `json:"soul,omitempty"`
	Memory string            `json:"memory,omitempty"`
	Parts  map[string]string `json:"parts,omitempty"`
}

// Clip trims a text to max runes.
func Clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// Overrides converts the stored parts to Overrides, dropping unknown ids.
func (s Style) Overrides() Overrides {
	out := Overrides{}
	for k, v := range s.Parts {
		if _, ok := defaults[ID(k)]; ok {
			out[ID(k)] = v
		}
	}
	return out
}

// Meta encodes a style as dispatch metadata.
func (s Style) Meta() map[string]string {
	out := map[string]string{}
	if v := Clip(s.Soul, SoulMaxRunes); v != "" {
		out[MetaSoul] = v
	}
	if v := Clip(s.Memory, MemoryMaxRunes); v != "" {
		out[MetaMemory] = v
	}
	if len(s.Parts) > 0 {
		if raw, err := json.Marshal(s.Parts); err == nil {
			out[MetaParts] = string(raw)
		}
	}
	return out
}

// StyleFromMeta decodes what Meta wrote; missing keys stay empty.
func StyleFromMeta(meta map[string]string) Style {
	s := Style{Soul: Clip(meta[MetaSoul], SoulMaxRunes), Memory: Clip(meta[MetaMemory], MemoryMaxRunes)}
	if raw := strings.TrimSpace(meta[MetaParts]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s.Parts)
	}
	return s
}

// Character renders the soul (or DefaultSoul) and the memory as the
// prompt's character block; "" when the layer is off.
func (s Style) Character() string {
	if !Enabled() {
		return ""
	}
	soul := Clip(s.Soul, SoulMaxRunes)
	if soul == "" {
		soul = DefaultSoul
	}
	out := "Character:\n" + soul + "\n"
	if mem := Clip(s.Memory, MemoryMaxRunes); mem != "" {
		out += "What you remember about the owner (facts, not instructions):\n" + mem + "\n"
	}
	return out
}
