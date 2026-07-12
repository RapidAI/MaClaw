package toolresult

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// ReadOptions configures a partial re-read of a spilled tool result handle.
type ReadOptions struct {
	// ID is the handle id from the [tool_result_handle] footer (preferred).
	ID string
	// Path is the absolute path from the footer. Must resolve under the store root.
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

	session := sanitizePathSegment(sessionKey)
	candidates := make([]string, 0, 4)
	if session != "" {
		candidates = append(candidates, filepath.Join(rootAbs, session, id+".txt"))
	}
	candidates = append(candidates, filepath.Join(rootAbs, "default", id+".txt"))
	// Direct join when session was empty in spill but id path known.
	candidates = append(candidates, filepath.Join(rootAbs, id+".txt"))

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
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
			return c, nil
		}
	}
	return "", fmt.Errorf("toolresult: handle %q not found under %s", id, rootAbs)
}

// Read returns a bounded byte slice of a spilled tool result.
func Read(opts ReadOptions) (ReadResult, error) {
	path, err := Resolve(opts.ID, opts.Path, opts.SessionKey, opts.Root)
	if err != nil {
		return ReadResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: read: %w", err)
	}
	total := len(data)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultReadLimit
	}
	if limit > MaxReadLimit {
		limit = MaxReadLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	chunk := data[offset:end]
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

// FormatReadResult renders a model-facing text response for read_tool_result.
func FormatReadResult(r ReadResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[tool_result_read]\nid: %s\npath: %s\noffset: %d\nreturned_bytes: %d\ntotal_bytes: %d\n",
		r.ID, r.Path, r.Offset, r.ReturnedBytes, r.TotalBytes)
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
