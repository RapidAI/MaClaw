package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Snapshots file content before ACP write tools so we can emit a diff card
// after the write (Cursor-like review in chat; full inline diff UI still depends
// on the VS Code ACP extension).
type acpWriteSnapshotStore struct {
	mu sync.Mutex
	// key: requestID + "\x00" + absPath
	before map[string]string
}

var globalACPWriteSnaps acpWriteSnapshotStore

func (s *acpWriteSnapshotStore) key(requestID, absPath string) string {
	// Windows paths are case-insensitive; normalize so capture/take don't miss
	// when tool args and disk resolution disagree on drive/letter case.
	p := filepath.Clean(strings.TrimSpace(absPath))
	if filepath.Separator == '\\' {
		p = strings.ToLower(p)
	}
	return strings.TrimSpace(requestID) + "\x00" + p
}

func (s *acpWriteSnapshotStore) capture(requestID, name, argsJSON, cwd string) {
	if !isACPProgrammingRequestID(requestID) || !nameIsWriteTool(name) {
		return
	}
	paths := acpPathsFromToolArgs(name, argsJSON)
	if len(paths) == 0 {
		return
	}
	abs := resolveACPPath(cwd, paths[0])
	if abs == "" {
		return
	}
	// Cap snapshot size so huge files don't blow memory on ACP turns.
	const maxSnap = 512 * 1024
	data, err := os.ReadFile(abs)
	content := ""
	if err == nil {
		if len(data) > maxSnap {
			// Keep a prefix marker; diff will fall back to summary mode.
			content = string(data[:maxSnap]) + "\n/* …truncated for snapshot… */\n"
		} else {
			content = string(data)
		}
	}
	s.mu.Lock()
	if s.before == nil {
		s.before = make(map[string]string)
	}
	// Bound map growth: prefer dropping other requests' snaps so the current
	// turn's capture/take still matches (full wipe caused lost diffs under load).
	if len(s.before) > 64 {
		prefix := strings.TrimSpace(requestID) + "\x00"
		for k := range s.before {
			if !strings.HasPrefix(k, prefix) {
				delete(s.before, k)
			}
		}
		// Still over budget (huge single turn): hard reset.
		if len(s.before) > 64 {
			s.before = make(map[string]string)
		}
	}
	s.before[s.key(requestID, abs)] = content
	s.mu.Unlock()
}

func (s *acpWriteSnapshotStore) take(requestID, absPath string) (before string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.before == nil {
		return "", false
	}
	k := s.key(requestID, absPath)
	before, ok = s.before[k]
	delete(s.before, k)
	return before, ok
}

func (s *acpWriteSnapshotStore) clearRequest(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	prefix := requestID + "\x00"
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.before {
		if strings.HasPrefix(k, prefix) {
			delete(s.before, k)
		}
	}
}

func resolveACPPath(cwd, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

func acpContentFromArgs(argsJSON string) string {
	var m map[string]any
	if json.Unmarshal([]byte(argsJSON), &m) != nil {
		return ""
	}
	for _, k := range []string{"content", "text", "new_str", "new_string", "replacement"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// buildACPWriteDiffSummary returns a markdown diff card for VS Code chat.
func buildACPWriteDiffSummary(requestID, name, argsJSON, cwd string) string {
	if !nameIsWriteTool(name) {
		return ""
	}
	paths := acpPathsFromToolArgs(name, argsJSON)
	if len(paths) == 0 {
		return ""
	}
	rel := paths[0]
	abs := resolveACPPath(cwd, rel)
	afterBytes, err := os.ReadFile(abs)
	after := ""
	if err == nil {
		after = string(afterBytes)
	} else {
		// Fall back to args content if disk read fails (rare race).
		after = acpContentFromArgs(argsJSON)
	}
	before, hadSnap := globalACPWriteSnaps.take(requestID, abs)
	if !hadSnap {
		// No snapshot: treat as create if empty before.
		before = ""
	}
	displayPath := rel
	if cwd != "" {
		if r, err := filepath.Rel(filepath.Clean(cwd), abs); err == nil && !strings.HasPrefix(r, "..") {
			displayPath = filepath.ToSlash(r)
		}
	}
	diffBody := unifiedDiffText(before, after, displayPath)
	if strings.TrimSpace(diffBody) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("### File change: `")
	b.WriteString(displayPath)
	b.WriteString("`\n\n")
	if before == "" && after != "" {
		b.WriteString("_new file_\n\n")
	} else if before != "" && after == "" {
		b.WriteString("_cleared_\n\n")
	}
	b.WriteString("```diff\n")
	b.WriteString(diffBody)
	if !strings.HasSuffix(diffBody, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n")
	return b.String()
}

// unifiedDiffText is a compact line-oriented diff (not full Myers) for chat cards.
func unifiedDiffText(before, after, path string) string {
	const maxLines = 80
	bl := splitLinesKeep(before)
	al := splitLinesKeep(after)
	if len(bl) == 0 && len(al) == 0 {
		return ""
	}
	// Identical
	if before == after {
		return fmt.Sprintf(" %s (unchanged)\n", path)
	}
	var out strings.Builder
	out.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path))
	// Simple LCS-free hunk: show removals then additions with context limit.
	// For short files show full; for long files show head/tail samples.
	if len(bl) <= maxLines && len(al) <= maxLines {
		i, j := 0, 0
		for i < len(bl) || j < len(al) {
			if i < len(bl) && j < len(al) && bl[i] == al[j] {
				out.WriteString(" ")
				out.WriteString(bl[i])
				out.WriteByte('\n')
				i++
				j++
				continue
			}
			// Prefer consume deletes then inserts when mismatch.
			if i < len(bl) && (j >= len(al) || bl[i] != al[j]) {
				// Look ahead if this line appears later in after (skip as delete).
				out.WriteString("-")
				out.WriteString(bl[i])
				out.WriteByte('\n')
				i++
				continue
			}
			if j < len(al) {
				out.WriteString("+")
				out.WriteString(al[j])
				out.WriteByte('\n')
				j++
			}
		}
		return out.String()
	}
	// Long file: summary + first/last chunks.
	out.WriteString(fmt.Sprintf("@@ summary: %d → %d lines @@\n", len(bl), len(al)))
	show := 12
	for i := 0; i < len(bl) && i < show; i++ {
		out.WriteString("-")
		out.WriteString(bl[i])
		out.WriteByte('\n')
	}
	if len(bl) > show {
		out.WriteString(fmt.Sprintf("-… (%d more old lines)\n", len(bl)-show))
	}
	for i := 0; i < len(al) && i < show; i++ {
		out.WriteString("+")
		out.WriteString(al[i])
		out.WriteByte('\n')
	}
	if len(al) > show {
		out.WriteString(fmt.Sprintf("+… (%d more new lines)\n", len(al)-show))
	}
	return out.String()
}

func splitLinesKeep(s string) []string {
	if s == "" {
		return nil
	}
	// Normalize to \n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	// Drop trailing empty from final newline
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func truncateRunesACP(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
