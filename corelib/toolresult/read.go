package toolresult

import (
	"fmt"
	"io"
	"math"
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
	// Limit is the max number of bytes to return. A non-positive limit uses
	// DefaultReadLimit. Values above MaxReadLimit are capped.
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
// One page matches a spilled terminal capture that did not fit the 16KB inline
// preview, so a status dump can be read back without several 6KB slices.
const DefaultReadLimit = 16 * 1024

// MaxReadLimit caps a single read_tool_result call.
const MaxReadLimit = 32768

// ModelReadLimit is the page size the model-facing reader applies.
// Read itself still honors an exact limit for byte-level callers.
//
// A missing limit and a limit below one page both mean the default page.
// The tool schema used to say "prefer small limit windows". Models answered
// with limit=1 and offset=0. A one-byte slice is truncated, the result says
// to call read_tool_result again, and the same arguments never advance
// through the spilled payload.
func ModelReadLimit(limit int) int {
	if limit < DefaultReadLimit {
		return DefaultReadLimit
	}
	if limit > MaxReadLimit {
		return MaxReadLimit
	}
	return limit
}

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
	id = NormalizeHandleID(id)
	if id == "" || id == "x" {
		return "", fmt.Errorf("toolresult: invalid id")
	}

	session := SessionDirectoryName(sessionKey)
	candidates := make([]string, 0, 6)
	if session != "" {
		candidates = append(candidates, filepath.Join(rootAbs, session, id+".txt"))
		candidates = append(candidates, filepath.Join(rootAbs, session, id+encryptedSuffix))
	} else {
		candidates = append(candidates, filepath.Join(rootAbs, "default", id+".txt"))
		candidates = append(candidates, filepath.Join(rootAbs, "default", id+encryptedSuffix))
		// Direct join when session was empty in spill but id path known.
		candidates = append(candidates, filepath.Join(rootAbs, id+".txt"))
		candidates = append(candidates, filepath.Join(rootAbs, id+encryptedSuffix))
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

	// Fallback: walk one level of session dirs for id handles (bounded).
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		return "", fmt.Errorf("toolresult: handle %q not found", id)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, suffix := range []string{".txt", encryptedSuffix} {
			c := filepath.Join(rootAbs, e.Name(), id+suffix)
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				if err := validateResolvedStorePath(rootAbs, c, ""); err != nil {
					continue
				}
				return c, nil
			}
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
	// From here on, ONLY the already-validated descriptor is read. Closing it
	// and reopening by path would invalidate the SameFile check above and
	// reopen the TOCTOU window it was meant to close.
	if strings.HasSuffix(path, encryptedSuffix) {
		// Encrypted handles are bounded (spills cap payload size), so a
		// whole-file decrypt is safe and keeps the read path simple.
		return readEncryptedSlice(rootAbs, path, file, info.Size(), opts)
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

// readEncryptedSlice decrypts an encrypted handle in full and returns the
// requested bounded page. Encrypted payloads are spill-bounded (database
// results are capped at the tool's byte budget), so whole-file decryption
// here does not create an unbounded memory path. r must be the descriptor
// already validated by Read (SameFile-checked), and size its stat size.
func readEncryptedSlice(rootAbs, path string, r io.Reader, size int64, opts ReadOptions) (ReadResult, error) {
	plain, err := readStorePayload(rootAbs, path, r, size)
	if err != nil {
		return ReadResult{}, err
	}
	total := len(plain)
	offset := min(max(opts.Offset, 0), total)
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
	start, alignedEnd := alignUTF8ReadWindow(plain, offset, end)
	chunk := plain[start:alignedEnd]
	out := ReadResult{
		Content:       string(chunk),
		Path:          path,
		ID:            strings.TrimSpace(opts.ID),
		Offset:        start,
		ReturnedBytes: len(chunk),
		TotalBytes:    total,
		Truncated:     alignedEnd < total,
	}
	if base := filepath.Base(path); strings.HasSuffix(base, encryptedSuffix) {
		out.ID = strings.TrimSuffix(base, encryptedSuffix)
	}
	if out.Truncated {
		out.NextOffset = alignedEnd
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

// NormalizeHandleID is the id Resolve opens. A model may append .txt or .enc,
// or include characters that cannot appear in the stored name. Those spellings
// are one spilled page.
func NormalizeHandleID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimSuffix(id, ".txt")
	id = strings.TrimSuffix(id, encryptedSuffix)
	return sanitizePathSegment(id)
}

// FormatReadResult renders a model-facing text response for read_tool_result.
// The page bytes sit between the two fences. The action hint is after the
// closing fence: a hint above a 16KB body was followed by the same offset the
// model had just sent, and a hint glued onto the body was copied into the file.
func FormatReadResult(r ReadResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[tool_result_read]\nid: %s\noffset: %d\nreturned_bytes: %d\ntotal_bytes: %d\n",
		r.ID, r.Offset, r.ReturnedBytes, r.TotalBytes)
	if r.Truncated {
		fmt.Fprintf(&b, "truncated: true\nnext_offset: %d\n", r.NextOffset)
	} else {
		b.WriteString("truncated: false\n")
	}
	b.WriteString("\n---\n")
	b.WriteString(r.Content)
	b.WriteString("\n--- end ---\n")
	if r.Truncated {
		fmt.Fprintf(&b, "hint: truncated is true. Call read_tool_result with this id and offset=%d. Omit limit. Do not send offset=%d.\n",
			r.NextOffset, r.Offset)
	} else {
		fmt.Fprintf(&b, "hint: truncated is false. This page is complete. Do not call read_tool_result for this id and offset %d.\n",
			r.Offset)
	}
	return b.String()
}

// ParseArgsInt extracts an int from tool args (int/float64/string).
// A missing key uses def. A present value that is not a finite integer also
// uses def; callers that must not treat that as the first page use
// InvalidArgsInt.
func ParseArgsInt(args map[string]interface{}, key string, def int) int {
	n, ok := parseArgsInt(args, key, def)
	if !ok {
		return def
	}
	return n
}

// InvalidArgsInt reports that key is present and is not a finite integer.
// A missing key is valid: the caller default applies. The words "next_offset"
// are not a byte index; treating them as 0 re-reads the first page.
func InvalidArgsInt(args map[string]interface{}, key string) bool {
	_, ok := parseArgsInt(args, key, 0)
	return !ok
}

func parseArgsInt(args map[string]interface{}, key string, def int) (int, bool) {
	if args == nil {
		return def, true
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def, true
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		if int64(int(n)) != n {
			return 0, false
		}
		return int(n), true
	case float64:
		return wholeInt(n)
	case string:
		s := strings.TrimSpace(n)
		if i, err := strconv.Atoi(s); err == nil {
			return i, true
		}
		// "16384.0" is the same byte offset. Atoi rejects it and the page
		// identity would collapse onto offset 0, which then looks like a repeat
		// of the first page. A fraction is not that page: truncating 16384.9
		// would land on 16384 and stop the real continuation.
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return wholeInt(f)
	default:
		return 0, false
	}
}

// wholeInt accepts only values that are already integers. Truncating a
// fraction would alias a different byte index.
func wholeInt(f float64) (int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < float64(math.MinInt) || f > float64(math.MaxInt) {
		return 0, false
	}
	n := int(f)
	if float64(n) != f {
		return 0, false
	}
	return n, true
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
