package guiapp

import (
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// reasoningStutterMaxRunes is the longest trimmed line that can count as a
// stutter. Only punctuation qualifies, so a repeated "OK" or "是" stays.
const reasoningStutterMaxRunes = 2

// reasoningStutterHaltCount is how many identical short lines in a row end
// the reasoning stream. Production 2026-09-26: deepseek-v4.1-flash emitted
// 72484 standalone "." lines (154870 newlines, 583KB) into 思考过程. The
// content path has a repetition halt; reasoning was forwarded verbatim, so
// the panel was a tall blank field of dots and the turn burned 85k output
// tokens over six minutes.
const reasoningStutterHaltCount = 12

// reasoningStutterFilter stops a reasoning stream that has collapsed into the
// same short line. Blank lines do not reset the run: the observed trace was
// "." separated by empty lines. A longer line resets it, so ordinary wrapped
// thoughts still pass.
type reasoningStutterFilter struct {
	downstream llm.TokenCallback
	stop       func()
	pending    strings.Builder
	last       string
	run        int
	halted     bool
	suppressed int
	logged     bool
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
		f.halted = true
		f.suppressed += utf8.RuneCountInString(line)
		if !f.logged {
			f.logged = true
			log.Printf("[LLM Stream] reasoning stutter halted: unit=%q run=%d", unit, f.run)
		}
		if f.stop != nil {
			f.stop()
		}
		return
	}
	f.downstream(line)
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
// lines are stripped, then a stutter halt drops a degenerate short-line run
// before it reaches the thinking panel.
type reasoningDisplayStream struct {
	role    *rolePrefixStreamFilter
	stutter *reasoningStutterFilter
}

func newReasoningDisplayStream(onToken llm.TokenCallback, stop func()) *reasoningDisplayStream {
	stutter := newReasoningStutterFilter(func(delta string) {
		if onToken != nil && delta != "" {
			onToken("\x01" + delta)
		}
	})
	stutter.stop = stop
	return &reasoningDisplayStream{
		stutter: stutter,
		role:    newRolePrefixStreamFilter(stutter.Write),
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
	if s.stutter != nil {
		s.stutter.Flush()
	}
}

func (s *reasoningDisplayStream) Halted() bool {
	return s != nil && s.stutter.Halted()
}

// ThinkCallback receives <think> bodies. They skip the role-prefix line
// buffer and still pass through the stutter halt.
func (s *reasoningDisplayStream) ThinkCallback() llm.TokenCallback {
	if s == nil || s.stutter == nil {
		return func(string) {}
	}
	return s.stutter.Write
}
