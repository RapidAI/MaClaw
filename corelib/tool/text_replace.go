package tool

import (
	"errors"
	"strings"
)

var (
	// ErrTextPassageEmpty means oldString carried no passage to find.
	ErrTextPassageEmpty = errors.New("text_passage_old_empty")
	// ErrTextPassageNotFound means the passage is absent even after CR, LF,
	// and CRLF are treated as the same line break.
	ErrTextPassageNotFound = errors.New("text_passage_not_found")
)

// TextReplaceResult is one passage replacement.
//
// Matches is how many times the passage occurs. Applied is how many of those
// were written. replaceAll false still reports every match and writes only
// the first, which is the same choice strings.Replace makes.
type TextReplaceResult struct {
	Text    string
	Matches int
	Applied int
}

// ReplaceTextPassage replaces oldString inside text.
//
// A source file is a sequence of lines plus a newline convention. The
// convention (CR, LF, or CRLF) is how the file is stored, not part of the
// passage a model can quote: tool arguments arrive with LF because that is
// what a JSON string contains. Matching on raw bytes therefore rejects every
// multi-line edit of a CRLF file and leaves single-line edits working, which
// is why agents then invent line-by-line scripts.
//
// The match is on logical lines. Bytes outside the match are not rewritten.
// Newlines inside the inserted text adopt the convention of the matched span,
// or the file's majority convention when the span itself has no line break.
func ReplaceTextPassage(text, oldString, newString string, replaceAll bool) (TextReplaceResult, error) {
	if oldString == "" {
		return TextReplaceResult{}, ErrTextPassageEmpty
	}
	// A passage with no line break cannot depend on CR versus LF. The match is
	// a byte search, and only inserted line breaks take the file's convention.
	if !strings.ContainsAny(oldString, "\r\n") {
		return replaceBytePassage(text, oldString, encodeInserted(text, newString), replaceAll)
	}
	// A file with one convention is already a byte string of that convention.
	// Quoting another convention still matches, and the replacement is stored
	// the same way. The check stops at the first foreign break. A file with
	// no break cannot contain this passage. Mixed files need the origin map
	// so each span keeps the break it actually contains.
	if ending, uniform, hasBreak := uniformEnding(text); uniform {
		if !hasBreak {
			return TextReplaceResult{}, ErrTextPassageNotFound
		}
		return replaceBytePassage(text, encodeNewlines(oldString, ending), encodeNewlines(newString, ending), replaceAll)
	}
	logicalFile, origin, fallback := logicalTextMap(text)
	logicalOld := normalizeNewlines(oldString)
	ats := indexAll(logicalFile, logicalOld)
	if len(ats) == 0 {
		return TextReplaceResult{}, ErrTextPassageNotFound
	}
	matches := len(ats)
	if !replaceAll {
		ats = ats[:1]
	}
	updated := splicePassages(text, origin, ats, len(logicalOld), newString, fallback)
	return TextReplaceResult{Text: updated, Matches: matches, Applied: len(ats)}, nil
}

func replaceBytePassage(text, oldString, newString string, replaceAll bool) (TextReplaceResult, error) {
	if oldString == "" {
		return TextReplaceResult{}, ErrTextPassageNotFound
	}
	if replaceAll {
		matches := strings.Count(text, oldString)
		if matches == 0 {
			return TextReplaceResult{}, ErrTextPassageNotFound
		}
		return TextReplaceResult{
			Text:    strings.Replace(text, oldString, newString, -1),
			Matches: matches,
			Applied: matches,
		}, nil
	}
	rel := strings.Index(text, oldString)
	if rel < 0 {
		return TextReplaceResult{}, ErrTextPassageNotFound
	}
	// The common edit writes the first match and leaves the tail untouched.
	// The tail is still scanned so Matches stays the full count.
	end := rel + len(oldString)
	matches := 1 + strings.Count(text[end:], oldString)
	var b strings.Builder
	b.Grow(len(text) - len(oldString) + len(newString))
	b.WriteString(text[:rel])
	b.WriteString(newString)
	b.WriteString(text[end:])
	return TextReplaceResult{Text: b.String(), Matches: matches, Applied: 1}, nil
}

// encodeInserted rewrites line breaks in newString to the file convention.
// A replacement with no line break is returned as itself.
func encodeInserted(text, newString string) string {
	if !strings.ContainsAny(newString, "\r\n") {
		return newString
	}
	return encodeNewlines(newString, majorityEnding(text))
}

// splicePassages writes each match once, left to right. Rebuilding the whole
// string per match copies every preceding byte again.
func splicePassages(text string, origin, ats []int, width int, newString, fallback string) string {
	var b strings.Builder
	b.Grow(len(text) + len(newString)*len(ats))
	encoded := map[string]string{}
	cursor := 0
	for _, at := range ats {
		start := origin[at]
		end := origin[at+width]
		b.WriteString(text[cursor:start])
		ending, ok := firstNewline(text[start:end])
		if !ok {
			ending = fallback
		}
		piece, ok := encoded[ending]
		if !ok {
			piece = encodeNewlines(newString, ending)
			encoded[ending] = piece
		}
		b.WriteString(piece)
		cursor = end
	}
	b.WriteString(text[cursor:])
	return b.String()
}

func logicalTextMap(s string) (string, []int, string) {
	var b strings.Builder
	b.Grow(len(s))
	origin := make([]int, 0, len(s)+1)
	crlf, lf, cr := 0, 0, 0
	for i := 0; i < len(s); {
		origin = append(origin, i)
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			b.WriteByte('\n')
			crlf++
			i += 2
			continue
		}
		if s[i] == '\r' {
			b.WriteByte('\n')
			cr++
			i++
			continue
		}
		if s[i] == '\n' {
			b.WriteByte('\n')
			lf++
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	origin = append(origin, len(s))
	return b.String(), origin, majorityFrom(crlf, lf, cr)
}

func indexAll(hay, needle string) []int {
	if needle == "" {
		return nil
	}
	var at []int
	from := 0
	for from < len(hay) {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			break
		}
		at = append(at, from+i)
		from += i + len(needle)
	}
	return at
}

func firstNewline(span string) (string, bool) {
	for i := 0; i < len(span); i++ {
		switch span[i] {
		case '\r':
			if i+1 < len(span) && span[i+1] == '\n' {
				return "\r\n", true
			}
			return "\r", true
		case '\n':
			return "\n", true
		}
	}
	return "", false
}

// uniformEnding reports the only line break in s.
// uniform is false once a second convention appears, so the caller can switch
// to the origin map without counting the rest of the file. hasBreak is false
// when s has no line break at all; ending is then LF.
func uniformEnding(s string) (ending string, uniform, hasBreak bool) {
	var kind string
	for i := 0; i < len(s); {
		var br string
		switch s[i] {
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				br = "\r\n"
				i += 2
			} else {
				br = "\r"
				i++
			}
		case '\n':
			br = "\n"
			i++
		default:
			i++
			continue
		}
		if kind == "" {
			kind = br
			continue
		}
		if br != kind {
			return "", false, true
		}
	}
	if kind == "" {
		return "\n", true, false
	}
	return kind, true, true
}

func majorityEnding(s string) string {
	return majorityFrom(newlineCensus(s))
}

func newlineCensus(s string) (crlf, lf, cr int) {
	for i := 0; i < len(s); {
		switch s[i] {
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				crlf++
				i += 2
				continue
			}
			cr++
			i++
		case '\n':
			lf++
			i++
		default:
			i++
		}
	}
	return crlf, lf, cr
}

func majorityFrom(crlf, lf, cr int) string {
	switch {
	case crlf > 0 && crlf >= lf && crlf >= cr:
		return "\r\n"
	case lf > 0 && lf >= cr:
		return "\n"
	case cr > 0:
		return "\r"
	default:
		return "\n"
	}
}

func normalizeNewlines(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			b.WriteByte('\n')
			i += 2
			continue
		}
		if s[i] == '\r' {
			b.WriteByte('\n')
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func encodeNewlines(s, ending string) string {
	logical := normalizeNewlines(s)
	if ending == "\n" {
		return logical
	}
	return strings.ReplaceAll(logical, "\n", ending)
}
