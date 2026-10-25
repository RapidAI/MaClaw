package docgen

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// mermaidRasterMaxRunes matches the on-screen export budget. A longer fence is
// left as source rather than sent to the drawer.
const mermaidRasterMaxRunes = 50_000

// MermaidRasterizer draws one mermaid diagram as PNG bytes.
//
// The PDF renderer has no JavaScript. A fence stays source text when this is
// nil, returns an error, or returns an empty slice. The desktop window
// registers the drawer that already paints diagrams in chat.
type MermaidRasterizer func(source string) ([]byte, error)

var (
	mermaidRasterMu sync.RWMutex
	mermaidRasterFn MermaidRasterizer
)

// SetMermaidRasterizer installs the process drawer used by every PDF render.
// Nil restores the previous behaviour: mermaid fences are printed as code.
func SetMermaidRasterizer(fn MermaidRasterizer) {
	mermaidRasterMu.Lock()
	mermaidRasterFn = fn
	mermaidRasterMu.Unlock()
}

func mermaidRasterizer() MermaidRasterizer {
	mermaidRasterMu.RLock()
	fn := mermaidRasterFn
	mermaidRasterMu.RUnlock()
	return fn
}

// expandMermaidFences replaces each closed mermaid fence with a picture the
// PDF can embed. It runs once per document, before pagination measures the
// markdown: measurement lays the same block out many times, and drawing the
// diagram on each pass would stall the render and could change the picture
// between the measurement and the page.
//
// A fence the drawer cannot paint is left untouched, which is the source the
// reader would otherwise have seen as a code block.
func expandMermaidFences(markdown string, scratch *dataURLScratch) string {
	fn := mermaidRasterizer()
	if fn == nil || scratch == nil {
		return markdown
	}
	if !strings.Contains(markdown, "```") && !strings.Contains(markdown, "~~~") {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	// One document often repeats a diagram. The drawer is a round trip into
	// the window, so a repeated source reuses the picture already drawn.
	drawn := make(map[string]mermaidDraw)
	changed := false
	for i := 0; i < len(lines); i++ {
		marker, ok := markdownFenceMarker(strings.TrimSpace(lines[i]))
		if !ok {
			out = append(out, lines[i])
			continue
		}
		if !isMermaidFenceInfo(markdownFenceInfo(lines[i], marker)) {
			out = append(out, lines[i])
			for i = i + 1; i < len(lines); i++ {
				out = append(out, lines[i])
				if isClosingFence(lines[i], marker) {
					break
				}
			}
			continue
		}
		body := make([]string, 0)
		j := i + 1
		closed := false
		for ; j < len(lines); j++ {
			if isClosingFence(lines[j], marker) {
				closed = true
				break
			}
			body = append(body, lines[j])
		}
		if !closed {
			out = append(out, lines[i:]...)
			i = len(lines)
			continue
		}
		source := strings.TrimSpace(strings.ReplaceAll(strings.Join(body, "\n"), "\r", ""))
		if source == "" || utf8.RuneCountInString(source) > mermaidRasterMaxRunes {
			out = append(out, lines[i:j+1]...)
			i = j
			continue
		}
		if hit, ok := drawn[source]; ok {
			if hit.ok {
				out = append(out, hit.line)
				changed = true
			} else {
				out = append(out, lines[i:j+1]...)
			}
			i = j
			continue
		}
		png, err := fn(source)
		path, wrote := "", false
		if err == nil && len(png) > 0 {
			path, wrote = scratch.writePNG(png)
		}
		if !wrote {
			drawn[source] = mermaidDraw{}
			out = append(out, lines[i:j+1]...)
			i = j
			continue
		}
		line := "![](" + path + ")"
		drawn[source] = mermaidDraw{line: line, ok: true}
		out = append(out, line)
		changed = true
		i = j
	}
	if !changed {
		return markdown
	}
	return strings.Join(out, "\n")
}

// mermaidDraw is one source's picture, or a recorded failure so a repeat
// does not call the drawer again.
type mermaidDraw struct {
	line string
	ok   bool
}

func markdownFenceInfo(line, marker string) string {
	trimmed := strings.TrimSpace(line)
	return strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
}

func isMermaidFenceInfo(info string) bool {
	info = strings.TrimSpace(info)
	if info == "" {
		return false
	}
	field := info
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		field = info[:i]
	}
	return strings.EqualFold(field, "mermaid")
}
