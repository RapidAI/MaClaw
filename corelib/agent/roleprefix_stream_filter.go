package agent

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

var (
	agentRolePrefixLineRe    = regexp.MustCompile(`^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|\x{FF1A})`)
	agentMidLineRolePrefixRe = regexp.MustCompile(`\n[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|\x{FF1A})`)
)

const agentRolePrefixMidLineCheckThreshold = 40

type rolePrefixStreamFilter struct {
	downstream llm.TokenCallback

	lineBuf         strings.Builder
	halted          bool
	suppressedRunes int
	inCodeBlock     bool
	seenContent     bool
}

func newRolePrefixStreamFilter(downstream llm.TokenCallback) *rolePrefixStreamFilter {
	return &rolePrefixStreamFilter{downstream: downstream}
}

func (f *rolePrefixStreamFilter) Write(delta string) {
	if f.downstream == nil || delta == "" {
		return
	}
	if f.halted {
		f.suppressedRunes += utf8.RuneCountInString(delta)
		return
	}

	for len(delta) > 0 {
		nlIdx := strings.IndexByte(delta, '\n')
		if nlIdx < 0 {
			f.lineBuf.WriteString(delta)
			if f.seenContent && f.lineBuf.Len() > agentRolePrefixMidLineCheckThreshold {
				f.checkMidLinePrefix()
			}
			return
		}

		f.lineBuf.WriteString(delta[:nlIdx+1])
		line := f.lineBuf.String()
		f.lineBuf.Reset()
		delta = delta[nlIdx+1:]

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			f.inCodeBlock = !f.inCodeBlock
		}

		if !f.inCodeBlock && agentRolePrefixLineRe.MatchString(line) {
			if f.seenContent {
				f.halted = true
				f.suppressedRunes += utf8.RuneCountInString(line) + utf8.RuneCountInString(delta)
				return
			}
			loc := agentRolePrefixLineRe.FindStringIndex(line)
			if loc != nil {
				stripped := line[loc[1]:]
				if strings.TrimSpace(stripped) != "" {
					f.downstream(stripped)
					f.seenContent = true
				}
			}
			continue
		}

		f.downstream(line)
		if strings.TrimSpace(line) != "" {
			f.seenContent = true
		}
	}
}

func (f *rolePrefixStreamFilter) Flush() {
	if f.downstream == nil || f.halted {
		return
	}
	remaining := f.lineBuf.String()
	if remaining == "" {
		return
	}
	defer f.lineBuf.Reset()

	if !f.inCodeBlock && agentRolePrefixLineRe.MatchString(remaining) {
		if f.seenContent {
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(remaining)
			return
		}
		loc := agentRolePrefixLineRe.FindStringIndex(remaining)
		if loc != nil {
			stripped := remaining[loc[1]:]
			if strings.TrimSpace(stripped) != "" {
				f.downstream(stripped)
			}
		}
		return
	}

	if !f.inCodeBlock && f.seenContent && len(remaining) > agentRolePrefixMidLineCheckThreshold {
		if strings.Contains(remaining, "Browser") || strings.Contains(remaining, "Tool") {
			if loc := agentMidLineRolePrefixRe.FindStringIndex(remaining); loc != nil {
				before := remaining[:loc[0]]
				if strings.TrimSpace(before) != "" {
					f.downstream(before)
				}
				f.halted = true
				f.suppressedRunes += utf8.RuneCountInString(remaining[loc[0]:])
				return
			}
		}
	}

	f.downstream(remaining)
}

func (f *rolePrefixStreamFilter) checkMidLinePrefix() {
	if f.inCodeBlock {
		return
	}
	buf := f.lineBuf.String()
	if !strings.Contains(buf, "Browser") && !strings.Contains(buf, "Tool") {
		return
	}
	loc := agentMidLineRolePrefixRe.FindStringIndex(buf)
	if loc == nil {
		return
	}
	before := buf[:loc[0]]
	if strings.TrimSpace(before) != "" {
		f.downstream(before)
	}
	f.halted = true
	f.suppressedRunes += utf8.RuneCountInString(buf[loc[0]:])
	f.lineBuf.Reset()
}
