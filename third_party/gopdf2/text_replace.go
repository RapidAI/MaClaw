package gopdf

import (
	"fmt"
	"strings"
)

// TextReplaceResult holds the result of a text replacement operation.
type TextReplaceResult struct {
	// PageIndex is the 0-based page index where replacement occurred.
	PageIndex int
	// Count is the number of replacements made on this page.
	Count int
	// OriginalText is the text that was searched for.
	OriginalText string
	// ReplacementText is the text that replaced the original.
	ReplacementText string
}

// ReplaceTextOptions configures text replacement behavior.
type ReplaceTextOptions struct {
	// CaseInsensitive enables case-insensitive matching.
	CaseInsensitive bool
	// MaxReplacements limits the number of replacements (0 = unlimited).
	MaxReplacements int
	// Pages limits replacement to specific pages (0-based). Empty = all pages.
	Pages []int
}

// ReplaceText searches for oldText in the PDF content streams and replaces
// it with newText. This operates on the raw content stream level and works
// best with simple text replacements where the new text has similar length.
//
// Returns the total number of replacements made across all pages.
//
// Note: This modifies the raw PDF bytes. For complex replacements involving
// different fonts or sizes, use the Content Element API instead.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	newData, results, err := gopdf.ReplaceText(data, "Draft", "Final", nil)
//	if err == nil {
//	    os.WriteFile("output.pdf", newData, 0644)
//	}
func ReplaceText(pdfData []byte, oldText, newText string, opts *ReplaceTextOptions) ([]byte, []TextReplaceResult, error) {
	if oldText == "" {
		return pdfData, nil, fmt.Errorf("oldText cannot be empty")
	}
	if opts == nil {
		opts = &ReplaceTextOptions{}
	}

	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return pdfData, nil, fmt.Errorf("parse PDF: %w", err)
	}

	var results []TextReplaceResult
	totalReplacements := 0
	result := make([]byte, len(pdfData))
	copy(result, pdfData)

	pageSet := make(map[int]bool, len(opts.Pages))
	for _, p := range opts.Pages {
		pageSet[p] = true
	}

	for pageIdx, page := range parser.pages {
		// Skip pages not in the target set.
		if len(pageSet) > 0 && !pageSet[pageIdx] {
			continue
		}

		// Check max replacements.
		if opts.MaxReplacements > 0 && totalReplacements >= opts.MaxReplacements {
			break
		}

		stream := parser.getPageContentStream(pageIdx)
		if len(stream) == 0 {
			continue
		}

		// Get the content object number for this page.
		if len(page.contents) == 0 {
			continue
		}
		contentRef := page.contents[0]

		contentObj, ok := parser.objects[contentRef]
		if !ok {
			continue
		}

		// Perform replacement in the content stream.
		streamStr := string(stream)
		count := replaceInContentStream(&streamStr, oldText, newText, opts)

		if count > 0 {
			// Update the stream in the PDF data.
			newStream := []byte(streamStr)
			result = replaceObjectStream(result, contentRef, contentObj.dict, newStream)

			results = append(results, TextReplaceResult{
				PageIndex:       pageIdx,
				Count:           count,
				OriginalText:    oldText,
				ReplacementText: newText,
			})
			totalReplacements += count
		}
	}

	if totalReplacements > 0 {
		result = rebuildXref(result)
	}

	return result, results, nil
}

// replaceInContentStream replaces text within PDF content stream operators.
// It handles both Tj and TJ operators.
func replaceInContentStream(stream *string, oldText, newText string, opts *ReplaceTextOptions) int {
	count := 0
	s := *stream

	// Strategy 1: Replace in literal strings (text)
	count += replaceLiteralStrings(&s, oldText, newText, opts)

	// Strategy 2: Replace in hex strings <hex>
	count += replaceHexStrings(&s, oldText, newText, opts)

	*stream = s
	return count
}

// replaceLiteralStrings replaces text within PDF literal strings (...).
func replaceLiteralStrings(s *string, oldText, newText string, opts *ReplaceTextOptions) int {
	count := 0
	str := *s
	i := 0

	// First pass: check if any replacements are needed.
	hasMatch := false
	scanI := 0
	for scanI < len(str) {
		if str[scanI] == '(' {
			depth := 1
			scanI++
			start := scanI
			for scanI < len(str) && depth > 0 {
				if str[scanI] == '\\' {
					scanI += 2
					if scanI > len(str) {
						scanI = len(str)
					}
					continue
				}
				if str[scanI] == '(' {
					depth++
				} else if str[scanI] == ')' {
					depth--
				}
				scanI++
			}
			end := scanI - 1
			if end < start {
				end = start
			}
			if end > len(str) {
				end = len(str)
			}
			inner := str[start:end]
			if opts.CaseInsensitive {
				if countCaseInsensitive(inner, oldText) > 0 {
					hasMatch = true
					break
				}
			} else {
				if strings.Contains(inner, oldText) {
					hasMatch = true
					break
				}
			}
		} else {
			scanI++
		}
	}

	if !hasMatch {
		return 0
	}

	// Second pass: perform replacements.
	result := strings.Builder{}
	result.Grow(len(str))
	i = 0

	for i < len(str) {
		if str[i] == '(' {
			// Find matching closing paren.
			depth := 1
			start := i
			i++
			for i < len(str) && depth > 0 {
				if str[i] == '\\' {
					i += 2
					if i > len(str) {
						i = len(str)
					}
					continue
				}
				if str[i] == '(' {
					depth++
				} else if str[i] == ')' {
					depth--
				}
				i++
			}
			literal := str[start:i]
			if depth != 0 || len(literal) < 2 {
				// Unbalanced parentheses — write as-is.
				result.WriteString(literal)
				continue
			}
			// Extract inner content.
			inner := literal[1 : len(literal)-1]

			var replaced string
			var matchCount int
			if opts.CaseInsensitive {
				matchCount = countCaseInsensitive(inner, oldText)
				replaced = caseInsensitiveReplace(inner, oldText, newText)
			} else {
				matchCount = strings.Count(inner, oldText)
				replaced = strings.ReplaceAll(inner, oldText, newText)
			}

			if matchCount > 0 {
				count += matchCount
				result.WriteByte('(')
				result.WriteString(replaced)
				result.WriteByte(')')
			} else {
				result.WriteString(literal)
			}
		} else {
			result.WriteByte(str[i])
			i++
		}
	}

	*s = result.String()
	return count
}

// replaceHexStrings replaces text within PDF hex strings <...>.
func replaceHexStrings(s *string, oldText, newText string, opts *ReplaceTextOptions) int {
	// Hex string replacement is more complex; for now we handle the common
	// case where hex strings encode ASCII-compatible text.
	count := 0
	str := *s
	var result strings.Builder
	resultStarted := false
	i := 0

	for i < len(str) {
		if str[i] == '<' && i+1 < len(str) && str[i+1] != '<' {
			start := i
			i++
			for i < len(str) && str[i] != '>' {
				i++
			}
			if i >= len(str) {
				// Unterminated hex string — write as-is.
				if resultStarted {
					result.WriteString(str[start:])
				}
				break
			}
			i++ // skip >
			hexStr := str[start:i]
			if len(hexStr) < 2 {
				if resultStarted {
					result.WriteString(hexStr)
				}
				continue
			}
			// Try to decode, replace, and re-encode.
			inner := hexStr[1 : len(hexStr)-1]
			decoded := decodeHexToASCII(inner)
			if decoded != "" && strings.Contains(decoded, oldText) {
				if !resultStarted {
					result.Grow(len(str))
					result.WriteString(str[:start])
					resultStarted = true
				}
				replaced := strings.ReplaceAll(decoded, oldText, newText)
				count += strings.Count(decoded, oldText)
				result.WriteByte('<')
				result.WriteString(encodeASCIIToHex(replaced))
				result.WriteByte('>')
			} else if resultStarted {
				result.WriteString(hexStr)
			}
		} else {
			if resultStarted {
				result.WriteByte(str[i])
			}
			i++
		}
	}

	if count > 0 && resultStarted {
		*s = result.String()
	}
	return count
}

// decodeHexToASCII decodes a hex string to ASCII if all bytes are printable.
func decodeHexToASCII(hex string) string {
	hex = strings.ReplaceAll(hex, " ", "")
	if len(hex)%2 != 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i+1 < len(hex); i += 2 {
		b := parseHex16(hex[i : i+2])
		if b > 126 || b < 32 {
			return "" // not printable ASCII
		}
		sb.WriteByte(byte(b))
	}
	return sb.String()
}

// encodeASCIIToHex encodes an ASCII string to hex.
func encodeASCIIToHex(s string) string {
	const hexDigits = "0123456789ABCDEF"
	buf := make([]byte, len(s)*2)
	for i := 0; i < len(s); i++ {
		buf[i*2] = hexDigits[s[i]>>4]
		buf[i*2+1] = hexDigits[s[i]&0x0F]
	}
	return string(buf)
}

// caseInsensitiveReplace performs case-insensitive string replacement.
func caseInsensitiveReplace(s, old, new string) string {
	lower := strings.ToLower(s)
	lowerOld := strings.ToLower(old)
	var result strings.Builder
	i := 0
	for i < len(lower) {
		pos := strings.Index(lower[i:], lowerOld)
		if pos < 0 {
			result.WriteString(s[i:])
			break
		}
		result.WriteString(s[i : i+pos])
		result.WriteString(new)
		i += pos + len(lowerOld)
	}
	return result.String()
}

// countCaseInsensitive counts case-insensitive occurrences.
func countCaseInsensitive(s, substr string) int {
	count := 0
	lower := strings.ToLower(s)
	lowerSub := strings.ToLower(substr)
	i := 0
	for {
		pos := strings.Index(lower[i:], lowerSub)
		if pos < 0 {
			break
		}
		count++
		i += pos + len(lowerSub)
	}
	return count
}
