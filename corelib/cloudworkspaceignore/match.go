package cloudworkspaceignore

import (
	"regexp"
	"strings"
)

const (
	decNone = iota
	decIgnore
	decInclude
)

type pattern struct {
	negate  bool
	dirOnly bool
	re      *regexp.Regexp
}

func parseIgnore(content string) []pattern {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	out := make([]pattern, 0, len(lines))
	for _, line := range lines {
		if p, ok := parseLine(line); ok {
			out = append(out, p)
		}
	}
	return out
}

func parseLine(line string) (pattern, bool) {
	line = strings.TrimRight(line, "\r")
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return pattern{}, false
	}
	p := pattern{}
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	} else if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
		line = strings.TrimSpace(line)
	}
	if line == "" {
		return pattern{}, false
	}
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	anchored := strings.HasPrefix(line, "/") || strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return pattern{}, false
	}
	re, err := compileGlob(line, !anchored)
	if err != nil {
		return pattern{}, false
	}
	p.re = re
	return p, true
}

func compileGlob(pattern string, unanchored bool) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	if unanchored {
		b.WriteString("(?:.*/)?")
	}
	runes := []rune(pattern)
	for i := 0; i < len(runes); {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				if i+2 < len(runes) && runes[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 3
					continue
				}
				b.WriteString(".*")
				i += 2
				continue
			}
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		case '[':
			j := i + 1
			if j < len(runes) && (runes[j] == '!' || runes[j] == '^') {
				j++
			}
			if j < len(runes) && runes[j] == ']' {
				j++
			}
			for j < len(runes) && runes[j] != ']' {
				j++
			}
			if j >= len(runes) {
				b.WriteString(`\[`)
				i++
				continue
			}
			class := string(runes[i+1 : j])
			b.WriteByte('[')
			if strings.HasPrefix(class, "!") {
				b.WriteByte('^')
				class = class[1:]
			}
			b.WriteString(class)
			b.WriteByte(']')
			i = j + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func (m *Matcher) decision(rel string, isDir bool) int {
	dec := decNone
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if p.re != nil && p.re.MatchString(rel) {
			if p.negate {
				dec = decInclude
			} else {
				dec = decIgnore
			}
		}
	}
	return dec
}
