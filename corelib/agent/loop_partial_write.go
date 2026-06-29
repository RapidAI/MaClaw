package agent

// loop_partial_write.go implements best-effort partial write recovery for
// truncated write_file tool calls in RunLoop. When the LLM's output token
// limit cuts off write_file JSON arguments, this code extracts whatever path
// and content were received and writes them to disk — converting a failed
// tool call into a partially successful one.
//
// This is the corelib equivalent of gui/im_agent_loop_truncation.go's
// attemptPartialWriteFile, made available to all RunLoop consumers (GUI
// CodingSubAgent, TUI, etc.).

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// partialWriteResult holds the outcome of a best-effort partial write.
type partialWriteResult struct {
	Path         string
	BytesWritten int
	RuneCount    int
	Tail         string // last N chars of written content (so LLM knows where to continue)
}

// attemptLoopPartialWriteFile tries to extract path and partial content from
// truncated write_file JSON arguments and writes whatever was received to disk.
//
// Returns nil if extraction fails (missing path, empty content, etc.).
func attemptLoopPartialWriteFile(rawArgs string) *partialWriteResult {
	if rawArgs == "" {
		return nil
	}

	path := extractJSONStringFieldFromRaw(rawArgs, "path")
	if path == "" {
		return nil
	}

	// Resolve path: if relative, resolve against cwd.
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}

	content := extractJSONStringFieldFromRaw(rawArgs, "content")
	if content == "" {
		return nil
	}

	// Require minimum content length to avoid writing useless tiny fragments.
	const minPartialWriteBytes = 50
	if len(content) < minPartialWriteBytes {
		log.Printf("[agent-loop] partial write: content too short (%d bytes), skipping for %q", len(content), path)
		return nil
	}

	// Determine write mode from args (default: overwrite).
	mode := extractJSONStringFieldFromRaw(rawArgs, "mode")

	if mode != "append" {
		// Don't overwrite an existing file with truncated content — UNLESS
		// the existing file was itself a previous partial write (smaller than
		// the new truncated content). In that case, the new truncation contains
		// more data and should replace the old partial.
		if info, statErr := os.Stat(path); statErr == nil {
			existingSize := info.Size()
			newSize := int64(len(content))
			if newSize <= existingSize {
				// New content is not larger — don't regress.
				log.Printf("[agent-loop] partial write: refusing to overwrite %q (%d bytes) with shorter truncated content (%d bytes)", path, existingSize, newSize)
				return nil
			}
			// New content is larger than existing → this is likely a fresh
			// truncation attempt with more output. Allow overwrite.
			log.Printf("[agent-loop] partial write: overwriting previous partial %q (%d bytes) with longer content (%d bytes)", path, existingSize, newSize)
		} else if !os.IsNotExist(statErr) {
			return nil
		}
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[agent-loop] partial write: failed to create directory %q: %v", dir, err)
		return nil
	}

	var err error
	contentBytes := []byte(content)
	if mode == "append" {
		f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			log.Printf("[agent-loop] partial write: failed to open %q for append: %v", path, openErr)
			return nil
		}
		_, err = f.Write(contentBytes)
		f.Close()
	} else {
		err = os.WriteFile(path, contentBytes, 0o644)
	}
	if err != nil {
		log.Printf("[agent-loop] partial write: failed to write %q: %v", path, err)
		return nil
	}

	runeCount := utf8.RuneCount(contentBytes)
	log.Printf("[agent-loop] partial write: wrote %d bytes (%d runes) to %q (mode=%s, truncated args=%d bytes)",
		len(contentBytes), runeCount, path, mode, len(rawArgs))

	// Capture tail so LLM knows where to continue.
	tail := content
	runes := []rune(tail)
	const tailMaxRunes = 80
	if len(runes) > tailMaxRunes {
		tail = string(runes[len(runes)-tailMaxRunes:])
	}

	return &partialWriteResult{
		Path:         path,
		BytesWritten: len(contentBytes),
		RuneCount:    runeCount,
		Tail:         tail,
	}
}

// buildLoopPartialWriteRecovery generates the recovery prompt after a
// successful partial write, telling the LLM exactly what happened and
// instructing it to continue with mode=append.
func buildLoopPartialWriteRecovery(pw *partialWriteResult) string {
	return fmt.Sprintf(
		"[system] write_file arguments were truncated by the model output limit. "+
			"The system saved the received partial content to disk.\n"+
			"  File: %s\n"+
			"  Bytes written: %d (%d runes)\n"+
			"  Last content: ...%s\n\n"+
			"Continue writing the remaining content using:\n"+
			"  write_file(path=%q, mode=\"append\", content=\"...remaining content from where it was cut off...\")\n"+
			"Keep each chunk under 3000 characters to avoid further truncation.",
		pw.Path, pw.BytesWritten, pw.RuneCount, pw.Tail, pw.Path,
	)
}

// buildLoopPartialWriteAppendHint generates a hint when partial write refused
// to overwrite an existing file — instructs LLM to use mode=append.
func buildLoopPartialWriteAppendHint(path string, fileSize int64) string {
	return fmt.Sprintf(
		"[system] write_file was truncated again. The file %q already exists (%d bytes from a previous partial write). "+
			"Do NOT use mode=overwrite — use write_file(path=%q, mode=\"append\", content=\"...remaining...\") to continue from where you left off. "+
			"Keep each chunk under 3000 characters.",
		path, fileSize, path,
	)
}

// ---------------------------------------------------------------------------
// truncatedToolArgsLookup looks up raw args for a tool name in the
// TruncatedToolArgs map, handling possible whitespace in map keys.
// Returns empty string if not found.
// ---------------------------------------------------------------------------

func truncatedToolArgsLookup(args map[string]string, name string) string {
	if len(args) == 0 {
		return ""
	}
	// Direct lookup first (common case).
	if v, ok := args[name]; ok && v != "" {
		return v
	}
	// Fallback: check trimmed keys (some models emit tool names with spaces).
	for k, v := range args {
		if strings.TrimSpace(k) == name && v != "" {
			return v
		}
	}
	return ""
}

// resolvePartialWritePath extracts the "path" field from truncated JSON args
// and resolves it to an absolute path. Returns empty string on failure.
func resolvePartialWritePath(rawArgs string) string {
	p := extractJSONStringFieldFromRaw(rawArgs, "path")
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		if wd, err := os.Getwd(); err == nil {
			p = filepath.Join(wd, p)
		} else {
			return ""
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// extractJSONStringFieldFromRaw extracts a JSON string field value from a
// possibly truncated/invalid JSON string using best-effort parsing.
// Handles JSON escape sequences. If the string value is truncated (no closing
// quote), returns whatever content was found.
// ---------------------------------------------------------------------------

func extractJSONStringFieldFromRaw(rawJSON string, fieldName string) string {
	fieldMarker := `"` + fieldName + `"`
	startIdx := strings.Index(rawJSON, fieldMarker)
	if startIdx < 0 {
		return ""
	}
	i := startIdx + len(fieldMarker)
	// Skip whitespace after field name.
	for i < len(rawJSON) && (rawJSON[i] == ' ' || rawJSON[i] == '\t' || rawJSON[i] == '\r' || rawJSON[i] == '\n') {
		i++
	}
	if i >= len(rawJSON) || rawJSON[i] != ':' {
		return ""
	}
	i++
	// Skip whitespace after colon.
	for i < len(rawJSON) && (rawJSON[i] == ' ' || rawJSON[i] == '\t' || rawJSON[i] == '\r' || rawJSON[i] == '\n') {
		i++
	}
	if i >= len(rawJSON) || rawJSON[i] != '"' {
		return ""
	}
	i++ // skip opening quote

	// Parse JSON string value, handling escapes.
	var buf strings.Builder
	for i < len(rawJSON) {
		ch := rawJSON[i]
		if ch == '"' {
			break // end of string
		}
		if ch == '\\' {
			if i+1 >= len(rawJSON) {
				break // truncated escape at end
			}
			next := rawJSON[i+1]
			switch next {
			case '"', '\\', '/':
				buf.WriteByte(next)
				i += 2
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'b':
				buf.WriteByte('\b')
				i += 2
			case 'f':
				buf.WriteByte('\f')
				i += 2
			case 'u':
				if i+5 >= len(rawJSON) {
					return buf.String() // truncated unicode escape
				}
				hexStr := rawJSON[i+2 : i+6]
				var codepoint uint32
				if _, err := fmt.Sscanf(hexStr, "%04x", &codepoint); err == nil {
					if codepoint >= 0xD800 && codepoint <= 0xDBFF {
						// High surrogate — look for low surrogate.
						if i+11 < len(rawJSON) && rawJSON[i+6] == '\\' && rawJSON[i+7] == 'u' {
							lowHex := rawJSON[i+8 : i+12]
							var low uint32
							if _, err2 := fmt.Sscanf(lowHex, "%04x", &low); err2 == nil && low >= 0xDC00 && low <= 0xDFFF {
								combined := 0x10000 + (codepoint-0xD800)*0x400 + (low - 0xDC00)
								buf.WriteRune(rune(combined))
								i += 12
							} else {
								buf.WriteRune('\uFFFD')
								i += 6
							}
						} else {
							buf.WriteRune('\uFFFD')
							i += 6
						}
					} else if codepoint >= 0xDC00 && codepoint <= 0xDFFF {
						buf.WriteRune('\uFFFD')
						i += 6
					} else {
						buf.WriteRune(rune(codepoint))
						i += 6
					}
				} else {
					buf.WriteByte('\\')
					buf.WriteByte('u')
					i += 2
				}
			default:
				buf.WriteByte('\\')
				buf.WriteByte(next)
				i += 2
			}
		} else {
			buf.WriteByte(ch)
			i++
		}
	}

	return buf.String()
}
