package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
)

// autoSpillResult holds the result of an auto-spill operation.
type autoSpillResult struct {
	Command  string // The rewritten command to execute
	TempFile string // Path to the temp script file (caller should clean up)
}

// autoSpillShellScript writes an oversized shell command to a temp script file
// and returns a short command that executes that script. This handles non-python
// payloads such as heredocs or generated PowerShell/bash scripts that exceed the
// inline command limit but are otherwise valid tool calls.
func autoSpillShellScript(command, workDir string) (*autoSpillResult, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("empty command")
	}

	tmpDir := workDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	ext := ".sh"
	prefix := "maclaw_shell_*.sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
		prefix = "maclaw_shell_*.ps1"
	}

	tmpFile, err := os.CreateTemp(tmpDir, prefix)
	if err != nil {
		tmpFile, err = os.CreateTemp(os.TempDir(), prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create temp shell script: %w", err)
		}
	}
	tmpPath := tmpFile.Name()
	if !strings.HasSuffix(strings.ToLower(tmpPath), ext) {
		nextPath := tmpPath + ext
		tmpFile.Close()
		if err := os.Rename(tmpPath, nextPath); err != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to add script extension: %w", err)
		}
		tmpPath = nextPath
		tmpFile, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to reopen temp shell script: %w", err)
		}
	}

	if runtime.GOOS == "windows" {
		// Windows PowerShell 5.1 reads non-BOM scripts using the active ANSI
		// codepage. A UTF-8 BOM preserves non-ASCII content in spilled scripts.
		if _, err := tmpFile.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to write powershell BOM: %w", err)
		}
	} else {
		if _, err := tmpFile.WriteString("#!/usr/bin/env bash\n"); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to write shell prelude: %w", err)
		}
	}
	if _, err := tmpFile.WriteString(command); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write shell command: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to close temp shell script: %w", err)
	}

	normalizedPath := filepath.ToSlash(tmpPath)
	var execCommand string
	if runtime.GOOS == "windows" {
		execCommand = fmt.Sprintf(`powershell -NoProfile -ExecutionPolicy Bypass -File "%s"`, normalizedPath)
	} else {
		execCommand = fmt.Sprintf(`bash "%s"`, normalizedPath)
	}
	log.Printf("[bash-auto-spill] wrote shell command to %s", tmpPath)
	return &autoSpillResult{Command: execCommand, TempFile: tmpPath}, nil
}

// autoSpillPythonScript extracts the Python script body from a command containing
// "python -c ..." (possibly with prefix/suffix commands joined by && or ;),
// writes it to a temp .py file, and returns a new command that executes the temp file
// with the original prefix and suffix preserved.
//
// This enables the LLM to generate large python-docx/reportlab scripts inline in
// bash(command="python -c \"...\"") without hitting the 4000 rune inline payload limit.
// The transformation is transparent to the LLM.
//
// The caller is responsible for cleaning up TempFile after command execution.
//
// Examples:
//   - python -c "script..." → python "/tmp/maclaw_script_xxx.py"
//   - pip install docx && python -c "script..." && echo done
//     → pip install docx && python "/tmp/maclaw_script_xxx.py" && echo done
func autoSpillPythonScript(command, workDir string) (*autoSpillResult, error) {
	prefix, pythonBin, scriptBody, suffix := splitPythonCCommand(command)
	if scriptBody == "" {
		return nil, fmt.Errorf("could not extract script body from command")
	}

	// Determine temp directory - prefer workDir, fallback to os.TempDir
	tmpDir := workDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	// Create temp file
	tmpFile, err := os.CreateTemp(tmpDir, "maclaw_script_*.py")
	if err != nil {
		// Retry with system temp dir if workDir failed
		tmpFile, err = os.CreateTemp(os.TempDir(), "maclaw_script_*.py")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp script file: %w", err)
		}
	}
	tmpPath := tmpFile.Name()

	// Write script body as UTF-8
	_, err = tmpFile.WriteString(scriptBody)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write script body: %w", err)
	}

	log.Printf("[bash-auto-spill] wrote %d bytes to %s", len(scriptBody), tmpPath)

	// Normalize path for the shell (forward slashes work in PowerShell and bash)
	normalizedPath := filepath.ToSlash(tmpPath)

	// Build the python execution part
	pythonExec := fmt.Sprintf(`%s "%s"`, pythonBin, normalizedPath)

	// Reassemble: prefix && python script.py && suffix
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, pythonExec)
	if suffix != "" {
		parts = append(parts, suffix)
	}

	return &autoSpillResult{
		Command:  strings.Join(parts, " && "),
		TempFile: tmpPath,
	}, nil
}

// pythonCInlineRe matches "python -c" or "python3 -c" (optionally with interpreter
// flags like -X utf8, -u, -B between python and -c) followed by whitespace.
// Supports: python -c, python -X utf8 -c, python -u -B -c, python3 -c
var pythonCInlineRe = regexp.MustCompile(`(?i)(python3?)(?:\s+-(?:[A-Za-z]\s+\S+|[A-Za-z]))*\s+-c\s+`)

// splitPythonCCommand splits a bash command containing "python -c '...'" or "python -c \"...\""
// into (prefix, pythonBin, scriptBody, suffix).
//
// The script body is returned as-is from inside the quotes (no unescaping),
// because JSON parsing has already handled the transport-layer escaping.
// The content between quotes IS the Python source code ready to write to a .py file.
func splitPythonCCommand(command string) (prefix, pythonBin, scriptBody, suffix string) {
	loc := pythonCInlineRe.FindStringIndex(command)
	if loc == nil {
		return "", "", "", ""
	}

	// Validate that "python -c" is at a command boundary, not inside a quoted string.
	// Check if there's an unmatched quote before the match position.
	if isPythonCInsideQuotes(command[:loc[0]]) {
		return "", "", "", ""
	}

	// Everything before the python -c match
	rawPrefix := strings.TrimSpace(command[:loc[0]])
	// Remove trailing && or ; from prefix
	rawPrefix = strings.TrimRight(rawPrefix, " ")
	rawPrefix = strings.TrimSuffix(rawPrefix, "&&")
	rawPrefix = strings.TrimSuffix(rawPrefix, ";")
	rawPrefix = strings.TrimSpace(rawPrefix)

	// Extract python binary name
	submatch := pythonCInlineRe.FindStringSubmatch(command)
	pythonBin = submatch[1]

	// Rest after "python -c "
	rest := command[loc[1]:]

	if len(rest) == 0 {
		return rawPrefix, pythonBin, "", ""
	}

	// The script body is wrapped in quotes: "..." or '...'
	firstRune, _ := utf8.DecodeRuneInString(rest)
	switch firstRune {
	case '"':
		endIdx := findMatchingQuoteRune(rest, '"')
		if endIdx < 0 {
			scriptBody = unescapeShellQuotedPythonBody(rest[1:], '"')
		} else {
			scriptBody = unescapeShellQuotedPythonBody(rest[1:endIdx], '"')
			afterQuote := strings.TrimSpace(rest[endIdx+1:])
			suffix = trimLeadingSeparators(afterQuote)
		}
	case '\'':
		endIdx := findMatchingQuoteRune(rest, '\'')
		if endIdx < 0 {
			scriptBody = unescapeShellQuotedPythonBody(rest[1:], '\'')
		} else {
			scriptBody = unescapeShellQuotedPythonBody(rest[1:endIdx], '\'')
			afterQuote := strings.TrimSpace(rest[endIdx+1:])
			suffix = trimLeadingSeparators(afterQuote)
		}
	default:
		// No quote wrapper — find the next && or ; or end of string
		nextSep := findNextCommandSeparator(rest)
		if nextSep < 0 {
			scriptBody = rest
		} else {
			scriptBody = rest[:nextSep]
			suffix = trimLeadingSeparators(strings.TrimSpace(rest[nextSep:]))
		}
	}

	return rawPrefix, pythonBin, scriptBody, suffix
}

func unescapeShellQuotedPythonBody(body string, quote rune) string {
	// After JSON unmarshal, the command string contains Python source code where the only
	// remaining "escaping" is the shell-level \" used to embed literal " inside double-quoted
	// strings (and \' for single-quoted). The \\ → \ transformation is NOT needed because
	// JSON unmarshal already handled that layer — the LLM's JSON escaping serves as both
	// JSON transport AND shell escaping simultaneously.
	//
	// We only need to convert the quote-escape sequence (\' or \") that shell would interpret.
	if body == "" {
		return body
	}
	escapedQuote := `\` + string(quote)
	return strings.ReplaceAll(body, escapedQuote, string(quote))
}

// isPythonCInsideQuotes checks if the text before a "python -c" match has
// an unmatched quote, indicating the match is inside a string literal.
func isPythonCInsideQuotes(before string) bool {
	singleCount := 0
	doubleCount := 0
	escaped := false
	for _, r := range before {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' {
			singleCount++
		} else if r == '"' {
			doubleCount++
		}
	}
	// Odd count means we're inside that type of quote
	return singleCount%2 != 0 || doubleCount%2 != 0
}

// findMatchingQuoteRune finds the byte index of the closing quote character,
// handling backslash escape sequences. Operates on rune boundaries to correctly
// handle multi-byte UTF-8 characters (e.g., Chinese text in patent scripts).
func findMatchingQuoteRune(s string, quote rune) int {
	escaped := false
	first := true
	for i, r := range s {
		if first {
			// Skip the opening quote
			first = false
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == quote {
			return i
		}
	}
	return -1
}

// findNextCommandSeparator finds the byte index of the next && or ; that's not inside quotes.
func findNextCommandSeparator(s string) int {
	inSingle := false
	inDouble := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '&':
			if !inSingle && !inDouble {
				// Check if next rune is also &
				rest := s[i+utf8.RuneLen(r):]
				if len(rest) > 0 {
					nextR, _ := utf8.DecodeRuneInString(rest)
					if nextR == '&' {
						return i
					}
				}
			}
		case ';':
			if !inSingle && !inDouble {
				return i
			}
		}
	}
	return -1
}

// trimLeadingSeparators removes leading && or ; and whitespace from a suffix string.
func trimLeadingSeparators(s string) string {
	s = strings.TrimLeft(s, " \t")
	s = strings.TrimPrefix(s, "&&")
	s = strings.TrimPrefix(s, ";")
	return strings.TrimSpace(s)
}

// bashCommandIsAutoSpillable returns true if the bash command contains an inline python script
// (python -c / python3 -c pattern) that can be automatically spilled to a temp file by toolBash.
func bashCommandIsAutoSpillable(command string) bool {
	loc := pythonCInlineRe.FindStringIndex(command)
	if loc == nil {
		return false
	}
	// Verify "python -c" is at a command boundary, not inside a quoted string
	return !isPythonCInsideQuotes(command[:loc[0]])
}

// shellRemoveCommand returns a platform-appropriate shell command to delete a file.
func shellRemoveCommand(filePath string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`Remove-Item -Force "%s" -ErrorAction SilentlyContinue`, filepath.ToSlash(filePath))
	}
	return fmt.Sprintf(`rm -f "%s"`, filepath.ToSlash(filePath))
}
