package llm

import (
	"strings"
)

// thinkFilter wraps a TokenCallback to suppress <think>...</think> content
// in real-time during streaming.
type thinkFilter struct {
	downstream TokenCallback
	inside     bool
	trimNext   bool // trim leading whitespace after exiting a think block
	pending    strings.Builder
	emitted    bool // true once any non-empty content has been sent downstream
}

const (
	thinkOpen     = "<think>"
	thinkClose    = "</think>"
	funcCallOpen  = "<|FunctionCallBegin|>"
	funcCallClose = "<|FunctionCallEnd|>"
	toolCallOpen  = "<tool_call>"
	toolCallClose = "</tool_call>"
)

func NewThinkFilter(downstream TokenCallback) TokenCallback {
	f := &thinkFilter{downstream: downstream}
	return f.Write
}

func (f *thinkFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain()
}

func (f *thinkFilter) drain() {
	for {
		s := f.pending.String()
		if s == "" {
			return
		}

		// Trim leading whitespace after exiting a think block or before first content
		if (f.trimNext || !f.emitted) && !f.inside {
			trimmed := strings.TrimLeft(s, " \t\r\n")
			if trimmed == "" {
				// All whitespace — consume and wait for more
				f.pending.Reset()
				return
			}
			if trimmed != s {
				f.pending.Reset()
				f.pending.WriteString(trimmed)
				f.trimNext = false
				continue
			}
			f.trimNext = false
		}

		if !f.inside {
			if strings.Contains(s, thinkOpen) {
				idx := strings.Index(s, thinkOpen)
				if idx > 0 {
					f.downstream(s[:idx])
					f.emitted = true
				}
				f.inside = true
				remaining := s[idx+len(thinkOpen):]
				f.pending.Reset()
				f.pending.WriteString(remaining)
				continue
			}

			// Check for partial match
			matchAny := false
			for i := 1; i < len(thinkOpen); i++ {
				if strings.HasSuffix(s, thinkOpen[:i]) {
					matchAny = true
					if len(s) > i {
						f.downstream(s[:len(s)-i])
						f.emitted = true
						f.pending.Reset()
						f.pending.WriteString(thinkOpen[:i])
					}
					break
				}
			}
			if !matchAny {
				f.downstream(s)
				f.emitted = true
				f.pending.Reset()
			}
			return
		} else {
			if strings.Contains(s, thinkClose) {
				idx := strings.Index(s, thinkClose)
				f.inside = false
				f.trimNext = true
				remaining := s[idx+len(thinkClose):]
				f.pending.Reset()
				f.pending.WriteString(remaining)
				continue
			}

			matchAny := false
			for i := 1; i < len(thinkClose); i++ {
				if strings.HasSuffix(s, thinkClose[:i]) {
					matchAny = true
					break
				}
			}
			if !matchAny {
				f.pending.Reset()
			}
			return
		}
	}
}

// Similar filters for funcCall and toolCall can be implemented here if needed,
// but they follow the same pattern. For brevity, I'll implement a generic one.

type tagFilter struct {
	downstream TokenCallback
	openTag    string
	closeTag   string
	inside     bool
	pending    strings.Builder
}

func NewTagFilter(downstream TokenCallback, open, close string) TokenCallback {
	f := &tagFilter{downstream: downstream, openTag: open, closeTag: close}
	return f.Write
}

func (f *tagFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain()
}

func (f *tagFilter) drain() {
	for {
		s := f.pending.String()
		if s == "" { return }

		if !f.inside {
			if strings.Contains(s, f.openTag) {
				idx := strings.Index(s, f.openTag)
				if idx > 0 { f.downstream(s[:idx]) }
				f.inside = true
				remaining := s[idx+len(f.openTag):]
				f.pending.Reset()
				f.pending.WriteString(remaining)
				continue
			}
			matchAny := false
			for i := 1; i < len(f.openTag); i++ {
				if strings.HasSuffix(s, f.openTag[:i]) {
					matchAny = true
					if len(s) > i {
						f.downstream(s[:len(s)-i])
						f.pending.Reset()
						f.pending.WriteString(f.openTag[:i])
					}
					break
				}
			}
			if !matchAny {
				f.downstream(s)
				f.pending.Reset()
			}
			return
		} else {
			if strings.Contains(s, f.closeTag) {
				idx := strings.Index(s, f.closeTag)
				f.inside = false
				remaining := s[idx+len(f.closeTag):]
				f.pending.Reset()
				f.pending.WriteString(remaining)
				continue
			}
			matchAny := false
			for i := 1; i < len(f.closeTag); i++ {
				if strings.HasSuffix(s, f.closeTag[:i]) {
					matchAny = true
					break
				}
			}
			if !matchAny { f.pending.Reset() }
			return
		}
	}
}
