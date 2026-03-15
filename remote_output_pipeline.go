package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type OutputPipeline struct {
	buffer            PreviewBuffer
	extract           EventExtractor
	reducer           SummaryReducer
	recentEventTimes  map[string]time.Time
	dedupeWindow      time.Duration
	recentFileBursts  map[string]fileBurst
	recentCommandRuns map[string]time.Time
	burstWindow       time.Duration
}

type fileBurst struct {
	eventType string
	files     []string
	lastSeen  time.Time
}

func NewOutputPipeline() *OutputPipeline {
	return &OutputPipeline{
		buffer:            NewRingPreviewBuffer(80),
		extract:           NewClaudeEventExtractor(),
		reducer:           NewClaudeSummaryReducer(),
		recentEventTimes:  map[string]time.Time{},
		dedupeWindow:      2 * time.Second,
		recentFileBursts:  map[string]fileBurst{},
		recentCommandRuns: map[string]time.Time{},
		burstWindow:       4 * time.Second,
	}
}

func (p *OutputPipeline) Consume(session *RemoteSession, chunk []byte) OutputResult {
	lines := normalizeChunkLines(chunk)
	if len(lines) == 0 {
		return OutputResult{}
	}

	events := p.coalesceAcrossBursts(session, p.coalesceEvents(p.filterDuplicateEvents(p.extract.Consume(session, lines))))
	summary := p.reducer.Apply(session.Summary, events, lines)
	previewDelta := p.buffer.Append(session.ID, lines)

	return OutputResult{
		Summary:      &summary,
		PreviewDelta: previewDelta,
		Events:       events,
	}
}

func (p *OutputPipeline) filterDuplicateEvents(events []ImportantEvent) []ImportantEvent {
	if len(events) == 0 {
		return events
	}
	if p.recentEventTimes == nil {
		p.recentEventTimes = map[string]time.Time{}
	}

	now := time.Now()
	filtered := make([]ImportantEvent, 0, len(events))
	for _, event := range events {
		key := buildEventDedupeKey(event)
		lastSeen, exists := p.recentEventTimes[key]
		if exists && now.Sub(lastSeen) < p.dedupeWindow {
			continue
		}
		p.recentEventTimes[key] = now
		filtered = append(filtered, event)
	}

	for key, seenAt := range p.recentEventTimes {
		if now.Sub(seenAt) > 5*p.dedupeWindow {
			delete(p.recentEventTimes, key)
		}
	}

	return filtered
}

func (p *OutputPipeline) coalesceEvents(events []ImportantEvent) []ImportantEvent {
	if len(events) < 2 {
		return events
	}

	merged := make([]ImportantEvent, 0, len(events))
	for i := 0; i < len(events); {
		event := events[i]
		if event.Type != "file.change" && event.Type != "file.read" {
			merged = append(merged, event)
			i++
			continue
		}

		j := i + 1
		files := []string{}
		seen := map[string]struct{}{}
		if event.RelatedFile != "" {
			files = append(files, event.RelatedFile)
			seen[event.RelatedFile] = struct{}{}
		}

		for j < len(events) && events[j].Type == event.Type {
			if file := events[j].RelatedFile; file != "" {
				if _, ok := seen[file]; !ok {
					files = append(files, file)
					seen[file] = struct{}{}
				}
			}
			j++
		}

		if len(files) <= 1 {
			event.Count = 1
			merged = append(merged, event)
			i++
			continue
		}

		event.Title = buildMergedEventTitle(event.Type, len(files))
		event.Summary = buildMergedEventSummary(event.Type, files)
		event.Count = len(files)
		event.Grouped = true
		event.RelatedFile = files[len(files)-1]
		merged = append(merged, event)
		i = j
	}

	return merged
}

func (p *OutputPipeline) coalesceAcrossBursts(session *RemoteSession, events []ImportantEvent) []ImportantEvent {
	if len(events) == 0 || session == nil {
		return events
	}
	if p.recentFileBursts == nil {
		p.recentFileBursts = map[string]fileBurst{}
	}
	if p.recentCommandRuns == nil {
		p.recentCommandRuns = map[string]time.Time{}
	}

	now := time.Now()
	merged := make([]ImportantEvent, 0, len(events))
	for _, event := range events {
		if event.Type == "command.started" {
			key := session.ID + "|" + event.Type + "|" + event.Command
			lastSeen, ok := p.recentCommandRuns[key]
			if ok && now.Sub(lastSeen) <= p.burstWindow {
				continue
			}
			p.recentCommandRuns[key] = now
			merged = append(merged, event)
			continue
		}
		if event.Type != "file.change" && event.Type != "file.read" {
			merged = append(merged, event)
			continue
		}

		key := session.ID + "|" + event.Type
		burst, ok := p.recentFileBursts[key]
		if !ok || now.Sub(burst.lastSeen) > p.burstWindow {
			files := collectEventFiles(event)
			p.recentFileBursts[key] = fileBurst{
				eventType: event.Type,
				files:     files,
				lastSeen:  now,
			}
			merged = append(merged, event)
			continue
		}

		files, changed := mergeBurstFiles(burst.files, collectEventFiles(event))
		burst.files = files
		burst.lastSeen = now
		p.recentFileBursts[key] = burst
		if !changed {
			continue
		}
		if len(files) > 1 {
			event.Title = buildMergedEventTitle(event.Type, len(files))
			event.Summary = buildMergedEventSummary(event.Type, files)
			event.Count = len(files)
			event.Grouped = true
			event.RelatedFile = files[len(files)-1]
		} else if event.Count == 0 {
			event.Count = 1
		}
		merged = append(merged, event)
	}

	for key, burst := range p.recentFileBursts {
		if now.Sub(burst.lastSeen) > 2*p.burstWindow {
			delete(p.recentFileBursts, key)
		}
	}
	for key, seenAt := range p.recentCommandRuns {
		if now.Sub(seenAt) > 2*p.burstWindow {
			delete(p.recentCommandRuns, key)
		}
	}

	return merged
}

func buildEventDedupeKey(event ImportantEvent) string {
	var b strings.Builder
	b.Grow(len(event.Type) + len(event.RelatedFile) + len(event.Command) + len(event.Summary) + 3)
	b.WriteString(event.Type)
	b.WriteByte('|')
	b.WriteString(event.RelatedFile)
	b.WriteByte('|')
	b.WriteString(event.Command)
	b.WriteByte('|')
	b.WriteString(event.Summary)
	return b.String()
}

func buildMergedEventTitle(eventType string, count int) string {
	switch eventType {
	case "file.read":
		return fmt.Sprintf("Inspected %d files", count)
	case "file.change":
		return fmt.Sprintf("Changed %d files", count)
	default:
		return fmt.Sprintf("%d events", count)
	}
}

func buildMergedEventSummary(eventType string, files []string) string {
	label := "files"
	verb := "Updated"
	if eventType == "file.read" {
		verb = "Inspected"
	}

	preview := files
	if len(preview) > 3 {
		preview = preview[:3]
	}

	summary := fmt.Sprintf("%s %d %s", verb, len(files), label)
	if len(preview) > 0 {
		summary += ": " + strings.Join(preview, ", ")
		if len(files) > len(preview) {
			summary += ", ..."
		}
	}
	return summary
}

func collectEventFiles(event ImportantEvent) []string {
	files := []string{}
	if event.RelatedFile != "" {
		files = append(files, event.RelatedFile)
	}
	if idx := strings.Index(event.Summary, ": "); idx >= 0 {
		for _, part := range strings.Split(event.Summary[idx+2:], ",") {
			file := strings.TrimSpace(strings.TrimSuffix(part, "..."))
			if file == "" {
				continue
			}
			files, _ = mergeBurstFiles(files, []string{file})
		}
	}
	return files
}

func mergeBurstFiles(existing []string, incoming []string) ([]string, bool) {
	if len(incoming) == 0 {
		return existing, false
	}

	seen := make(map[string]struct{}, len(existing))
	out := append([]string(nil), existing...)
	for _, file := range existing {
		if file == "" {
			continue
		}
		seen[file] = struct{}{}
	}

	changed := false
	for _, file := range incoming {
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, file)
		changed = true
	}
	return out, changed
}

func normalizeChunkLines(chunk []byte) []string {
	text := string(chunk)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	rawLines := strings.Split(text, "\n")
	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isNoiseLine(line) {
			continue
		}
		if len(line) > 300 {
			line = line[:300] + "..."
		}
		out = append(out, line)
	}
	return out
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z~^$]|\x1b\].*?(?:\x1b\\|\x07)|\x1b[()#][A-Z0-9]?|\x1b[a-zA-Z]`)
var controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
var multiSpacePattern = regexp.MustCompile(`\s{2,}`)

func stripANSI(s string) string {
	s = ansiPattern.ReplaceAllString(s, "")
	s = controlPattern.ReplaceAllString(s, "")
	return multiSpacePattern.ReplaceAllString(s, " ")
}

// boxDrawingOnly matches lines composed entirely of box-drawing, block-element,
// and common ASCII separator characters (dashes, equals, pipes, etc.).
var boxDrawingOnly = regexp.MustCompile(`^[\s\x{2500}-\x{259F}\x{2550}-\x{256C}\-=_*+|]+$`)

func isNoiseLine(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || trimmed == ".." || trimmed == "..." {
		return true
	}
	return boxDrawingOnly.MatchString(trimmed)
}

// rawChunkResult holds the parsed lines from a PTY chunk along with a
// flag indicating whether the chunk contained a screen-clear sequence
// (e.g. ESC[2J, ESC[H at the start, or ED/EL sequences that wipe the
// display).  When IsScreenRefresh is true the caller should *replace*
// the accumulated RawOutputLines instead of appending.
type rawChunkResult struct {
	Lines          []string
	IsScreenRefresh bool
}

// screenClearPattern matches common ANSI sequences that clear the screen
// or move the cursor to the home position, indicating a full TUI redraw.
//   \x1b[2J  – Erase entire display
//   \x1b[H   – Cursor home (row 1, col 1)
//   \x1b[1;1H – Cursor to (1,1)
//   \x1b[?1049h – Switch to alternate screen buffer
//   \x1b[?1049l – Switch back from alternate screen buffer
var screenClearPattern = regexp.MustCompile(`\x1b\[2J|\x1b\[H|\x1b\[1;1H|\x1b\[\?1049[hl]`)

// rawChunkLines splits a PTY output chunk into lines with only ANSI
// stripping applied.  No noise filtering, no length truncation — this
// is the "terminal-like" raw view used by the desktop console.
//
// Empty lines (after ANSI stripping) are preserved as blank strings so
// that TUI screen redraws are still counted and the frontend can detect
// new output arriving even when the visible content hasn't changed.
//
// If the chunk contains a screen-clear sequence the result is flagged as
// a screen refresh so the caller can replace (not append) the output buffer.
func rawChunkLines(chunk []byte) rawChunkResult {
	raw := string(chunk)
	isRefresh := screenClearPattern.MatchString(raw)

	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	rawLines := strings.Split(text, "\n")
	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		cleaned := strings.TrimRight(stripANSI(line), " \t")
		out = append(out, cleaned)
	}
	// Trim trailing empty lines that are just artifacts of the final
	// newline in the chunk, but keep interior blanks.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return rawChunkResult{Lines: out, IsScreenRefresh: isRefresh}
}
