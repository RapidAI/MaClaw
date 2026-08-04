package toolresult

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// ReadOptions configures a partial re-read of a spilled tool result handle.
type ReadOptions struct {
	// ID is the handle id from the [tool_result_handle] footer (preferred).
	ID string
	// Path is a legacy internal absolute path. Must resolve under the store root;
	// model-facing handle footers and schemas intentionally do not expose it.
	Path string
	// SessionKey narrows ID resolution when Path is empty.
	SessionKey string
	// Root overrides the storage directory. Empty uses maclawpath.ToolResultsDir().
	Root string
	// Offset is a 0-based byte offset into the stored content.
	Offset int
	// Limit is the max number of bytes to return. Default 6000, max 32768.
	Limit int
}

// ReadResult is a bounded slice of a spilled tool result.
type ReadResult struct {
	Content       string `json:"content"`
	Path          string `json:"path"`
	ID            string `json:"id,omitempty"`
	Offset        int    `json:"offset"`
	ReturnedBytes int    `json:"returned_bytes"`
	TotalBytes    int    `json:"total_bytes"`
	Truncated     bool   `json:"truncated"`
	NextOffset    int    `json:"next_offset,omitempty"`
}

// DefaultReadLimit is the model-facing default slice size for read_tool_result.
const DefaultReadLimit = 6000

// MaxReadLimit caps a single read_tool_result call.
const MaxReadLimit = 32768

// Resolve finds the on-disk path for a handle id and/or path under the store root.
func Resolve(id, path, sessionKey, root string) (string, error) {
	rootAbs, err := storeRoot(root)
	if err != nil {
		return "", err
	}

	path = strings.TrimSpace(path)
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("toolresult: abs path: %w", err)
		}
		if !isUnderRoot(rootAbs, abs) {
			return "", fmt.Errorf("toolresult: path outside tool_results store")
		}
		if session := SessionDirectoryName(sessionKey); session != "" {
			sessionRoot := filepath.Join(rootAbs, session)
			if !isUnderRoot(sessionRoot, abs) {
				return "", fmt.Errorf("toolresult: path outside session tool_results store")
			}
		}
		if err := validateResolvedStorePath(rootAbs, abs, sessionKey); err != nil {
			return "", err
		}
		if st, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("toolresult: stat: %w", err)
		} else if st.IsDir() {
			return "", fmt.Errorf("toolresult: path is a directory")
		}
		return abs, nil
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("toolresult: id or path is required")
	}
	// Strip accidental .txt suffix from models.
	id = strings.TrimSuffix(id, ".txt")
	id = sanitizePathSegment(id)
	if id == "" || id == "x" {
		return "", fmt.Errorf("toolresult: invalid id")
	}

	session := SessionDirectoryName(sessionKey)
	candidates := make([]string, 0, 3)
	if session != "" {
		candidates = append(candidates, filepath.Join(rootAbs, session, id+".txt"))
	} else {
		candidates = append(candidates, filepath.Join(rootAbs, "default", id+".txt"))
		// Direct join when session was empty in spill but id path known.
		candidates = append(candidates, filepath.Join(rootAbs, id+".txt"))
	}

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			if err := validateResolvedStorePath(rootAbs, c, sessionKey); err != nil {
				continue
			}
			return c, nil
		}
	}

	// With an explicit session, fail closed instead of discovering another
	// owner's handle by globally unique id. Unscoped legacy callers retain the
	// bounded one-level fallback for backward compatibility. In particular, do
	// not fall back to pre-hash normalized directories: those lack owner metadata
	// and may represent multiple logical owners (for example, a:b and a/b).
	if session != "" {
		return "", fmt.Errorf("toolresult: handle %q not found", id)
	}

	// Fallback: walk one level of session dirs for id.txt (bounded).
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		return "", fmt.Errorf("toolresult: handle %q not found", id)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c := filepath.Join(rootAbs, e.Name(), id+".txt")
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			if err := validateResolvedStorePath(rootAbs, c, ""); err != nil {
				continue
			}
			return c, nil
		}
	}
	return "", fmt.Errorf("toolresult: handle %q not found", id)
}

func validateResolvedStorePath(rootAbs, path, sessionKey string) error {
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("toolresult: resolve store root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("toolresult: resolve path: %w", err)
	}
	if !isUnderRoot(resolvedRoot, resolvedPath) {
		return fmt.Errorf("toolresult: resolved path outside tool_results store")
	}
	if session := SessionDirectoryName(sessionKey); session != "" {
		resolvedSession, err := filepath.EvalSymlinks(filepath.Join(rootAbs, session))
		if err != nil {
			return fmt.Errorf("toolresult: resolve session store: %w", err)
		}
		if !isUnderRoot(resolvedSession, resolvedPath) {
			return fmt.Errorf("toolresult: resolved path outside session tool_results store")
		}
	}
	return nil
}

// Read returns a bounded byte slice of a spilled tool result.
func Read(opts ReadOptions) (ReadResult, error) {
	path, err := Resolve(opts.ID, opts.Path, opts.SessionKey, opts.Root)
	if err != nil {
		return ReadResult{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: open: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: stat open file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ReadResult{}, fmt.Errorf("toolresult: path is not a regular file")
	}
	// Revalidate after opening to close the path-validation/open race. If a
	// symlink or file was swapped between Resolve and Open, reject the already
	// opened descriptor before reading any content.
	rootAbs, err := storeRoot(opts.Root)
	if err != nil {
		return ReadResult{}, err
	}
	if err := validateResolvedStorePath(rootAbs, path, opts.SessionKey); err != nil {
		return ReadResult{}, err
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(info, current) {
		return ReadResult{}, fmt.Errorf("toolresult: file changed during open")
	}
	maxInt := int64(^uint(0) >> 1)
	if info.Size() < 0 || info.Size() > maxInt {
		return ReadResult{}, fmt.Errorf("toolresult: file is too large to address")
	}
	total := int(info.Size())
	requestedOffset := min(max(opts.Offset, 0), total)
	offset := requestedOffset
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultReadLimit
	}
	if limit > MaxReadLimit {
		limit = MaxReadLimit
	}
	end := total
	if limit < total-offset {
		end = offset + limit
	}
	// Read only the requested page plus the maximum UTF-8 boundary context on
	// each side. This keeps memory O(limit) even for multi-gigabyte results.
	probeStart := max(0, offset-(utf8.UTFMax-1))
	probeEnd := min(total, end+(utf8.UTFMax-1))
	data := make([]byte, probeEnd-probeStart)
	if len(data) > 0 {
		n, readErr := file.ReadAt(data, int64(probeStart))
		if readErr != nil && readErr != io.EOF {
			return ReadResult{}, fmt.Errorf("toolresult: read: %w", readErr)
		}
		if n != len(data) {
			return ReadResult{}, fmt.Errorf("toolresult: short read: got %d bytes, want %d", n, len(data))
		}
	}
	afterRead, err := file.Stat()
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: stat after read: %w", err)
	}
	if afterRead.Size() != info.Size() {
		return ReadResult{}, fmt.Errorf("toolresult: file changed during read")
	}
	localStart, localEnd := alignUTF8ReadWindow(data, offset-probeStart, end-probeStart)
	offset = probeStart + localStart
	end = probeStart + localEnd
	chunk := data[localStart:localEnd]
	out := ReadResult{
		Content:       string(chunk),
		Path:          path,
		ID:            strings.TrimSpace(opts.ID),
		Offset:        offset,
		ReturnedBytes: len(chunk),
		TotalBytes:    total,
		Truncated:     end < total,
	}
	if base := filepath.Base(path); strings.HasSuffix(base, ".txt") {
		out.ID = strings.TrimSuffix(base, ".txt")
	}
	if out.Truncated {
		out.NextOffset = end
	}
	return out, nil
}

func alignUTF8ReadWindow(data []byte, start, end int) (int, int) {
	start = nextValidUTF8Boundary(data, start)
	if end < start {
		end = start
	}
	if end > len(data) {
		end = len(data)
	}
	alignedEnd := end
	for alignedEnd > start && alignedEnd < len(data) && !validUTF8Boundary(data, alignedEnd) {
		alignedEnd--
	}
	if alignedEnd > start || end == len(data) {
		return start, alignedEnd
	}
	alignedEnd = end
	alignedEnd = nextValidUTF8Boundary(data, alignedEnd)
	return start, alignedEnd
}

func validUTF8Boundary(data []byte, offset int) bool {
	if offset <= 0 || offset >= len(data) {
		return true
	}
	// A position is not a boundary only when it sits inside a complete valid
	// multi-byte rune. Invalid bytes remain individually addressable, preserving
	// arbitrary binary/non-UTF-8 tool output across pagination.
	for start := max(0, offset-(utf8.UTFMax-1)); start < offset; start++ {
		if !utf8.RuneStart(data[start]) {
			continue
		}
		r, size := utf8.DecodeRune(data[start:])
		if r != utf8.RuneError && size > 1 && start+size > offset {
			return false
		}
	}
	return true
}

func nextValidUTF8Boundary(data []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	for offset < len(data) && !validUTF8Boundary(data, offset) {
		offset++
	}
	return offset
}

// FormatReadResult renders a model-facing text response for read_tool_result.
func FormatReadResult(r ReadResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[tool_result_read]\nid: %s\noffset: %d\nreturned_bytes: %d\ntotal_bytes: %d\n",
		r.ID, r.Offset, r.ReturnedBytes, r.TotalBytes)
	if r.Truncated {
		fmt.Fprintf(&b, "truncated: true\nnext_offset: %d\nhint: call read_tool_result again with offset=%d to continue\n",
			r.NextOffset, r.NextOffset)
	} else {
		b.WriteString("truncated: false\n")
	}
	b.WriteString("\n---\n")
	b.WriteString(r.Content)
	return b.String()
}

// ParseArgsInt extracts a non-negative int from tool args (int/float64/string).
func ParseArgsInt(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return def
		}
		return i
	default:
		return def
	}
}

// ParseArgsString extracts a string tool argument.
func ParseArgsString(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	default:
		return strings.TrimSpace(fmt.Sprint(s))
	}
}

func storeRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = maclawpath.ToolResultsDir()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("toolresult: abs root: %w", err)
	}
	return abs, nil
}

func isUnderRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	sep := string(os.PathSeparator)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+sep)
}
