package gopdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"regexp"
	"strings"
)

// Pre-compiled regexes for content stream cleaning.
var (
	reMultiSpace = regexp.MustCompile(`\s+`)
	reDictLength = regexp.MustCompile(`/Length\s+\d+`)
)

// CleanContentStreams optimizes all content streams in the given PDF data
// by removing redundant operators, consolidating state changes, and
// normalizing whitespace. Returns the cleaned PDF data.
//
// This is a standalone function that operates on raw PDF bytes, similar
// to RecompressImages.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	cleaned, err := gopdf.CleanContentStreams(data)
//	os.WriteFile("output.pdf", cleaned, 0644)
func CleanContentStreams(pdfData []byte) ([]byte, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	result := make([]byte, len(pdfData))
	copy(result, pdfData)

	modified := false
	for _, page := range parser.pages {
		for _, contentRef := range page.contents {
			obj, ok := parser.objects[contentRef]
			if !ok || obj.stream == nil {
				continue
			}

			cleaned := cleanContentStream(obj.stream)
			if len(cleaned) >= len(obj.stream) {
				continue // no improvement
			}

			// Compress the cleaned stream.
			var compressed bytes.Buffer
			w, err := zlib.NewWriterLevel(&compressed, zlib.DefaultCompression)
			if err != nil {
				continue
			}
			w.Write(cleaned)
			w.Close()

			newDict := buildCleanedDict(obj.dict, compressed.Len())
			result = replaceObjectStream(result, contentRef, newDict, compressed.Bytes())
			modified = true
		}
	}

	if !modified {
		return pdfData, nil
	}

	result = rebuildXref(result)
	return result, nil
}

// cleanContentStream optimizes a single content stream by removing
// redundant operators and normalizing whitespace.
func cleanContentStream(stream []byte) []byte {
	lines := splitContentLines(stream)
	lines = removeRedundantStateChanges(lines)
	lines = removeEmptyQBlocks(lines)
	lines = normalizeWhitespace(lines)
	return joinContentLines(lines)
}

// splitContentLines splits a content stream into logical operator lines.
func splitContentLines(stream []byte) []string {
	text := string(stream)
	// Split on newlines, keeping each operator on its own line.
	raw := strings.Split(text, "\n")
	var lines []string
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// removeRedundantStateChanges removes consecutive duplicate state-setting
// operators where only the last one matters.
func removeRedundantStateChanges(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	// Operators where consecutive duplicates can be collapsed.
	stateOps := map[string]bool{
		"w":  true, // line width
		"J":  true, // line cap
		"j":  true, // line join
		"M":  true, // miter limit
		"d":  true, // dash pattern
		"ri": true, // rendering intent
		"i":  true, // flatness
		"Tc": true, // character spacing
		"Tw": true, // word spacing
		"Tz": true, // horizontal scaling
		"TL": true, // text leading
		"Tr": true, // text rendering mode
		"Ts": true, // text rise
	}

	result := make([]string, 0, len(lines))
	for i, line := range lines {
		op := extractOperator(line)
		if !stateOps[op] {
			result = append(result, line)
			continue
		}
		// Check if the next line has the same operator.
		if i+1 < len(lines) && extractOperator(lines[i+1]) == op {
			continue // skip this one, the next one overrides it
		}
		result = append(result, line)
	}
	return result
}

// removeEmptyQBlocks removes q/Q pairs that contain no drawing operations.
func removeEmptyQBlocks(lines []string) []string {
	changed := true
	for changed {
		changed = false
		result := make([]string, 0, len(lines))
		i := 0
		for i < len(lines) {
			if lines[i] == "q" && i+1 < len(lines) && lines[i+1] == "Q" {
				// Empty save/restore block — skip both.
				i += 2
				changed = true
				continue
			}
			result = append(result, lines[i])
			i++
		}
		lines = result
	}
	return lines
}

// normalizeWhitespace trims excess whitespace from each line.
func normalizeWhitespace(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = reMultiSpace.ReplaceAllString(strings.TrimSpace(line), " ")
	}
	return result
}

// joinContentLines joins cleaned lines back into a content stream.
func joinContentLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// extractOperator extracts the PDF operator from a content stream line.
// PDF operators are the last token on the line (e.g., "1.0 0 0 1 0 0 cm" -> "cm").
func extractOperator(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	// Handle special cases for operators that are single characters.
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// buildCleanedDict updates the /Length in a dictionary and ensures /FlateDecode filter.
func buildCleanedDict(origDict string, newLen int) string {
	// Replace /Length value.
	dict := reDictLength.ReplaceAllString(origDict, fmt.Sprintf("/Length %d", newLen))

	// Ensure /Filter /FlateDecode is present.
	if !strings.Contains(dict, "/FlateDecode") {
		dict = strings.Replace(dict, ">>", "/Filter /FlateDecode\n>>", 1)
	}

	return dict
}
