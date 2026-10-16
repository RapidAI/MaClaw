package guiapp

import (
	"fmt"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// reasoningStutterMaxRunes is the longest trimmed line that can count as an
// identical punctuation stutter. A repeated "OK" or "是" stays on that path.
const reasoningStutterMaxRunes = 2

// reasoningStutterHaltCount is how many identical punctuation lines in a row
// end the reasoning stream. Production 2026-09-26: deepseek-v4.1-flash emitted
// 72484 standalone "." lines (154870 newlines, 583KB) into 思考过程. The
// content path has a repetition halt; reasoning was forwarded verbatim, so
// the panel was a tall blank field of dots and the turn burned 85k output
// tokens over six minutes.
const reasoningStutterHaltCount = 12

// reasoningChantMaxRunes is the longest trimmed line that can join a rotating
// short-line chant. Production 2026-09-27: the same model cycled
// "Go / Final / OK / Writing / Done / Output / Alright / Let me write"
// about 6900 times (70289 output tokens, 2m40s) because each line differed,
// so the identical-punctuation run never reached the halt. The content
// repetition filter ignores sentences under 15 runes and paragraphs under
// 30, so it does not see this either.
const reasoningChantMaxRunes = 48

// reasoningChantWindow is how many recent short lines are compared. A single
// repeated word never fills a diverse window, so "OK" repeated stays.
const reasoningChantWindow = 32

// reasoningChantMaxDistinct is the largest vocabulary that still counts as a
// consecutive chant. One distinct line is a repeated word and is allowed.
const reasoningChantMaxDistinct = 10

// reasoningChantRepeatCount and reasoningChantMinVoices catch the same chant
// when longer lines keep breaking the consecutive window. Production
// 2026-09-27 inserted "Hmm, I'm stuck in a loop" between the short lines, so
// the longest pure short run was 16 and the 32-line window never filled.
// Four different short lines each said this many times is the collapse.
const reasoningChantRepeatCount = 6

const reasoningChantMinVoices = 4

// reasoningChantMaxTracked bounds the per-stream short-line counts.
const reasoningChantMaxTracked = 256

// reasoningStutterFilter stops a reasoning stream that has collapsed into the
// same punctuation line, or into a small rotating set of short lines. Blank
// lines do not reset either run. A longer line resets the chant window, so
// ordinary wrapped thoughts still pass.
type reasoningStutterFilter struct {
	downstream    llm.TokenCallback
	stop          func()
	pending       strings.Builder
	last          string
	run           int
	chants        []string
	chantDistinct int
	chantCounts   map[string]int
	chantVoices   int
	halted        bool
	suppressed    int
	logged        bool
}

func newReasoningStutterFilter(downstream llm.TokenCallback) *reasoningStutterFilter {
	if downstream == nil {
		downstream = func(string) {}
	}
	return &reasoningStutterFilter{downstream: downstream}
}

func (f *reasoningStutterFilter) Write(delta string) {
	if f == nil || delta == "" {
		return
	}
	if f.halted {
		f.suppressed += utf8.RuneCountInString(delta)
		return
	}
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *reasoningStutterFilter) Flush() {
	if f == nil || f.halted {
		return
	}
	f.drain(true)
}

func (f *reasoningStutterFilter) Halted() bool {
	return f != nil && f.halted
}

func (f *reasoningStutterFilter) SuppressedRunes() int {
	if f == nil {
		return 0
	}
	return f.suppressed
}

func (f *reasoningStutterFilter) drain(force bool) {
	for {
		text := f.pending.String()
		if text == "" {
			return
		}
		idx := strings.IndexByte(text, '\n')
		if idx < 0 {
			if !force {
				return
			}
			f.pending.Reset()
			f.observe(text)
			return
		}
		line := text[:idx+1]
		f.pending.Reset()
		f.pending.WriteString(text[idx+1:])
		f.observe(line)
		if f.halted {
			f.suppressed += utf8.RuneCountInString(f.pending.String())
			f.pending.Reset()
			return
		}
	}
}

func (f *reasoningStutterFilter) observe(line string) {
	unit := strings.TrimSpace(line)
	if unit == "" {
		f.downstream(line)
		return
	}
	if reasoningStutterPunctuation(unit) && unit == f.last {
		f.run++
	} else {
		f.last = unit
		f.run = 1
	}
	if f.run >= reasoningStutterHaltCount {
		f.haltStutter(line, fmt.Sprintf("unit=%q run=%d", unit, f.run))
		return
	}
	if kind, detail := f.chantCollapsed(unit); kind {
		f.haltStutter(line, detail)
		return
	}
	f.downstream(line)
}

func (f *reasoningStutterFilter) haltStutter(line, detail string) {
	f.halted = true
	f.suppressed += utf8.RuneCountInString(line)
	if !f.logged {
		f.logged = true
		log.Printf("[LLM Stream] reasoning stutter halted: %s", detail)
	}
	if f.stop != nil {
		f.stop()
	}
}

// chantCollapsed reports a rotating short-line chant. A longer line clears
// only the consecutive window. Repeat counts survive that break, because the
// production trace kept inserting a longer "I'm stuck in a loop" line between
// the same few words. Punctuation is ignored, like a blank line. One repeated
// word does not collapse.
func (f *reasoningStutterFilter) chantCollapsed(unit string) (bool, string) {
	if reasoningStutterPunctuation(unit) {
		return false, ""
	}
	if utf8.RuneCountInString(unit) > reasoningChantMaxRunes {
		f.chants = f.chants[:0]
		return false, ""
	}
	if len(f.chants) < reasoningChantWindow {
		f.chants = append(f.chants, unit)
	} else {
		copy(f.chants, f.chants[1:])
		f.chants[reasoningChantWindow-1] = unit
	}
	if len(f.chants) == reasoningChantWindow {
		distinct := make(map[string]struct{}, len(f.chants))
		for _, line := range f.chants {
			distinct[line] = struct{}{}
		}
		n := len(distinct)
		f.chantDistinct = n
		if n >= 2 && n <= reasoningChantMaxDistinct {
			return true, fmt.Sprintf("chant window=%d distinct=%d", reasoningChantWindow, n)
		}
	}
	if f.chantCounts == nil {
		f.chantCounts = make(map[string]int)
	}
	if c, ok := f.chantCounts[unit]; ok {
		c++
		f.chantCounts[unit] = c
		if c == reasoningChantRepeatCount {
			f.chantVoices++
		}
	} else if len(f.chantCounts) < reasoningChantMaxTracked {
		f.chantCounts[unit] = 1
	}
	if f.chantVoices >= reasoningChantMinVoices {
		return true, fmt.Sprintf("chant voices=%d repeat=%d", f.chantVoices, reasoningChantRepeatCount)
	}
	return false, ""
}

// recoverReasoningStutter keeps a completion that already parsed a tool
// call, and otherwise turns the cancelled stream into an empty stop. The
// agent loop recovers one empty stop and asks again; it does not treat the
// caller's own cancel as a provider failure. A user cancel (reqErr set) is
// left untouched.
func recoverReasoningStutter(resp *llm.Response, reqErr error, halted bool) (*llm.Response, bool) {
	if !halted || reqErr != nil {
		return resp, false
	}
	if resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		return resp, true
	}
	if resp == nil || len(resp.Choices) == 0 {
		resp = &llm.Response{Choices: []llm.Choice{{FinishReason: llmFinishReasonStop.String()}}}
	}
	return resp, true
}

func reasoningStutterPunctuation(unit string) bool {
	if unit == "" || utf8.RuneCountInString(unit) > reasoningStutterMaxRunes {
		return false
	}
	for _, r := range unit {
		if !unicode.IsPunct(r) {
			return false
		}
	}
	return true
}

// reasoningDisplayStream is the reasoning half of a token stream: role-prefix
// lines are stripped, Hy3 tool grammar is held out of the panel, then a
// stutter halt drops a degenerate short-line run.
type reasoningDisplayStream struct {
	role    *rolePrefixStreamFilter
	hy3     *hy3MarkupHold
	stutter *reasoningStutterFilter
}

func newReasoningDisplayStream(onToken llm.TokenCallback, stop func()) *reasoningDisplayStream {
	stutter := newReasoningStutterFilter(func(delta string) {
		if onToken != nil && delta != "" {
			onToken("\x01" + delta)
		}
	})
	stutter.stop = stop
	hy3 := &hy3MarkupHold{downstream: stutter.Write}
	return &reasoningDisplayStream{
		stutter: stutter,
		hy3:     hy3,
		role:    newRolePrefixStreamFilter(hy3.Write),
	}
}

// hy3MarkupHold drops <tool_call:SUFFIX> grammar from the thinking panel.
// The raw reasoning buffer is parsed separately and becomes a real tool call.
type hy3MarkupHold struct {
	downstream llm.TokenCallback
	pending    strings.Builder
	suppressed bool
}

func (f *hy3MarkupHold) Write(delta string) {
	if f == nil || f.downstream == nil || delta == "" || f.suppressed {
		return
	}
	f.pending.WriteString(delta)
	visible, hold, suppress := llm.HoldHy3ToolMarkup(f.pending.String(), false)
	f.pending.Reset()
	if visible != "" {
		f.downstream(visible)
	}
	if suppress {
		f.suppressed = true
		return
	}
	if hold != "" {
		f.pending.WriteString(hold)
	}
}

func (f *hy3MarkupHold) Flush() {
	if f == nil || f.suppressed {
		if f != nil {
			f.pending.Reset()
		}
		return
	}
	if f.pending.Len() == 0 {
		return
	}
	visible, _, _ := llm.HoldHy3ToolMarkup(f.pending.String(), true)
	f.pending.Reset()
	if visible != "" && f.downstream != nil {
		f.downstream(visible)
	}
}

func (s *reasoningDisplayStream) Write(delta string) {
	if s == nil || s.role == nil {
		return
	}
	s.role.Write(delta)
}

func (s *reasoningDisplayStream) Flush() {
	if s == nil {
		return
	}
	if s.role != nil {
		s.role.Flush()
	}
	if s.hy3 != nil {
		s.hy3.Flush()
	}
	if s.stutter != nil {
		s.stutter.Flush()
	}
}

func (s *reasoningDisplayStream) Halted() bool {
	return s != nil && s.stutter.Halted()
}

// ThinkCallback receives <think> bodies. They skip the role-prefix line
// buffer, drop Hy3 tool grammar, and still pass through the stutter halt.
func (s *reasoningDisplayStream) ThinkCallback() llm.TokenCallback {
	if s == nil || s.hy3 == nil {
		return func(string) {}
	}
	return s.hy3.Write
}
