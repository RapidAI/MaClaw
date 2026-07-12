// Package toolresult provides dual-view tool outputs:
//
//   - Runtime view: full raw result persisted on disk (handle).
//   - Provider view: compact preview sent back to the model.
//
// This is the MaClaw counterpart to OpenSquilla-style tool compression:
// large logs/pages stay available without flooding the context window.
package toolresult

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Handle identifies a spilled full tool result on disk.
type Handle struct {
	ID            string    `json:"id"`
	Path          string    `json:"path"`
	ToolName      string    `json:"tool_name,omitempty"`
	SessionKey    string    `json:"session_key,omitempty"`
	OriginalBytes int       `json:"original_bytes"`
	PreviewBytes  int       `json:"preview_bytes"`
	CreatedAt     time.Time `json:"created_at"`
}

// Projection is the dual-view result after Project.
type Projection struct {
	// Preview is the model-visible text (may include a handle footer).
	Preview string `json:"preview"`
	// Handle is non-nil when the full raw content was spilled to disk.
	Handle *Handle `json:"handle,omitempty"`
	// Spilled is true when Preview is a compact view of a stored full result.
	Spilled bool `json:"spilled"`
}

// ProjectOptions configures dual-view projection.
type ProjectOptions struct {
	// ToolName is the tool that produced Content (for metadata / footer).
	ToolName string
	// SessionKey isolates spilled files per user/session (optional).
	SessionKey string
	// Content is the full runtime tool result.
	Content string
	// Preview is the model-bound compact text. When empty, a default head/tail
	// truncation of Content is used with Limit.
	Preview string
	// Limit is the maximum preview size used when Preview is empty. Default 4096.
	Limit int
	// MinSpillBytes only spills when Content is at least this large.
	// Default: same as Limit (spill only when truncated).
	MinSpillBytes int
	// Root overrides the storage directory. Empty uses maclawpath.ToolResultsDir().
	Root string
	// ForceSpill stores Content even when Preview equals Content.
	ForceSpill bool
}

// Project builds a provider preview and optionally spills the full content.
//
// Spill happens when Content is longer than Preview (caller already truncated),
// or when ForceSpill is set, or when Preview is empty and Content exceeds Limit.
func Project(opts ProjectOptions) (Projection, error) {
	content := opts.Content
	if content == "" {
		return Projection{Preview: opts.Preview}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 4096
	}
	minSpill := opts.MinSpillBytes
	if minSpill <= 0 {
		minSpill = limit
	}

	preview := opts.Preview
	if preview == "" {
		// Tool-aware structured projection (Phase 4); falls back to DefaultPreview.
		preview = StructuredPreview(opts.ToolName, content, limit)
	}

	shouldSpill := opts.ForceSpill ||
		(len(content) >= minSpill && (len(preview) < len(content) || preview != content))
	if !shouldSpill {
		RecordProjection(opts.ToolName, len(content), len(preview), false)
		return Projection{Preview: preview}, nil
	}

	handle, err := Spill(SpillOptions{
		ToolName:   opts.ToolName,
		SessionKey: opts.SessionKey,
		Content:    content,
		Root:       opts.Root,
	})
	if err != nil {
		// Spill failure must not break the agent turn — return preview only.
		RecordProjection(opts.ToolName, len(content), len(preview), false)
		return Projection{Preview: preview}, err
	}
	handle.PreviewBytes = len(preview)
	out := Projection{
		Preview: appendHandleFooter(preview, handle),
		Handle:  handle,
		Spilled: true,
	}
	RecordProjection(opts.ToolName, len(content), len(out.Preview), true)
	return out, nil
}

// SpillOptions configures raw content persistence.
type SpillOptions struct {
	ToolName   string
	SessionKey string
	Content    string
	Root       string
}

// Spill writes full content to disk and returns a handle.
func Spill(opts SpillOptions) (*Handle, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = maclawpath.ToolResultsDir()
	}
	session := sanitizePathSegment(opts.SessionKey)
	if session == "" {
		session = "default"
	}
	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("toolresult: mkdir: %w", err)
	}

	id, err := newHandleID(opts.ToolName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".txt")
	if err := os.WriteFile(path, []byte(opts.Content), 0o600); err != nil {
		return nil, fmt.Errorf("toolresult: write: %w", err)
	}

	return &Handle{
		ID:            id,
		Path:          path,
		ToolName:      strings.TrimSpace(opts.ToolName),
		SessionKey:    session,
		OriginalBytes: len(opts.Content),
		CreatedAt:     time.Now().UTC(),
	}, nil
}

// ReadFile returns the full spilled content at path.
// Path must resolve under the tool_results store (unless Root is overridden via
// Resolve/Read). Prefer Read() for model-facing partial re-reads.
func ReadFile(path string) (string, error) {
	abs, err := Resolve("", path, "", "")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DefaultPreview builds a head/tail preview within limit bytes.
func DefaultPreview(s string, limit int) string {
	if limit <= 0 {
		limit = 4096
	}
	if len(s) <= limit {
		return s
	}
	sep := fmt.Sprintf("\n\n... (已截断，共 %d 字节) ...\n\n", len(s))
	budget := limit - len(sep)
	if budget < 64 {
		return s[:limit]
	}
	headLen := budget * 2 / 3
	tailLen := budget - headLen
	return s[:headLen] + sep + s[len(s)-tailLen:]
}

func appendHandleFooter(preview string, h *Handle) string {
	if h == nil {
		return preview
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(preview, "\n"))
	b.WriteString("\n\n")
	b.WriteString("[tool_result_handle]\n")
	fmt.Fprintf(&b, "id: %s\n", h.ID)
	fmt.Fprintf(&b, "path: %s\n", h.Path)
	fmt.Fprintf(&b, "tool: %s\n", h.ToolName)
	fmt.Fprintf(&b, "original_bytes: %d\n", h.OriginalBytes)
	fmt.Fprintf(&b, "preview_bytes: %d\n", h.PreviewBytes)
	b.WriteString("hint: 完整结果已落盘。需要细节时用 read_tool_result(id=... 或 path=..., offset, limit) 分段读取；勿要求模型复述全文。\n")
	return b.String()
}

func newHandleID(toolName string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("toolresult: rand: %w", err)
	}
	tool := sanitizePathSegment(toolName)
	if tool == "" {
		tool = "tool"
	}
	if len(tool) > 24 {
		tool = tool[:24]
	}
	return fmt.Sprintf("%s_%s_%s",
		time.Now().UTC().Format("20060102T150405"),
		tool,
		hex.EncodeToString(buf[:]),
	), nil
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "._")
	if out == "" {
		return "x"
	}
	return out
}
