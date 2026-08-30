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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
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
	// IncludeHandleFooter appends the model-facing read-back handle to Preview.
	// Nil defaults to true. Set false for preview-only compatibility helpers;
	// the full content is still spilled and Projection.Handle remains available.
	IncludeHandleFooter *bool
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
	includeFooter := opts.IncludeHandleFooter == nil || *opts.IncludeHandleFooter
	modelPreview := preview
	if includeFooter {
		preview = fitPreviewWithHandleFooter(preview, handle, limit)
		handle.PreviewBytes = len(preview)
		modelPreview = appendHandleFooter(preview, handle)
	}
	out := Projection{
		Preview: modelPreview,
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

// SessionDirectoryName returns the opaque on-disk namespace for a session key.
// Hosts may use it for diagnostics; models should only see handle IDs.
func SessionDirectoryName(sessionKey string) string {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return ""
	}
	segment := sanitizePathSegment(raw)
	// Preserve the historical directory for common portable owner IDs. Other
	// values get an additional stable digest so case-insensitive filesystems,
	// Windows device names, and Unicode normalization cannot merge owners.
	if sessionKey == raw && segment == raw && isPortableSessionSegment(raw) && !isWindowsDeviceName(raw) && len(raw) <= maxSessionSegmentBytes {
		return raw
	}
	return hashedSessionSegment(segment, sessionKey)
}

// Spill writes full content to disk and returns a handle.
func Spill(opts SpillOptions) (*Handle, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = maclawpath.ToolResultsDir()
	}
	session := SessionDirectoryName(opts.SessionKey)
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
	if err := fileutil.AtomicWriteFile(path, []byte(opts.Content), 0o600); err != nil {
		return nil, fmt.Errorf("toolresult: write: %w", err)
	}
	invalidateStoreStats(root)

	return &Handle{
		ID:            id,
		Path:          path,
		ToolName:      strings.TrimSpace(opts.ToolName),
		SessionKey:    opts.SessionKey,
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
		return utf8Prefix(s, limit)
	}
	headLen := budget * 2 / 3
	tailLen := budget - headLen
	return utf8Prefix(s, headLen) + sep + utf8Suffix(s, tailLen)
}

// HandleFooterMarker introduces the model-facing read-back footer appended to
// a spilled preview. Anything that truncates tool-result content afterwards
// must preserve everything from this marker to the end of the string —
// cutting the footer (in particular the id line) orphans the spilled full
// result, because the model can no longer read it back with read_tool_result.
const HandleFooterMarker = "[tool_result_handle]"

func appendHandleFooter(preview string, h *Handle) string {
	if h == nil {
		return preview
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(preview, "\n"))
	b.WriteString("\n\n")
	b.WriteString(HandleFooterMarker + "\n")
	fmt.Fprintf(&b, "id: %s\n", h.ID)
	fmt.Fprintf(&b, "tool: %s\n", modelVisibleToolName(h.ToolName))
	fmt.Fprintf(&b, "original_bytes: %d\n", h.OriginalBytes)
	fmt.Fprintf(&b, "preview_bytes: %d\n", h.PreviewBytes)
	b.WriteString("hint: 完整结果已落盘。需要细节时用 read_tool_result(id=..., offset, limit) 分段读取；勿要求模型复述全文。\n")
	return b.String()
}

func modelVisibleToolName(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return "tool"
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	const maxToolNameBytes = 96
	if len(name) > maxToolNameBytes {
		name = utf8Prefix(name, maxToolNameBytes)
	}
	return name
}

// fitPreviewWithHandleFooter keeps the complete model projection within the
// caller's preview budget. Historically callers applied a second blind byte
// truncation after Project, which could cut the handle footer itself. Reserve
// footer space up front instead, then compact only the lossy preview portion.
func fitPreviewWithHandleFooter(preview string, h *Handle, limit int) string {
	if h == nil || limit <= 0 {
		return preview
	}
	// PreviewBytes only changes decimal width by a few bytes. Iterate to a fixed
	// point so the final footer length is accounted for exactly.
	for i := 0; i < 8; i++ {
		h.PreviewBytes = len(preview)
		footerOnly := appendHandleFooter("", h)
		budget := limit - len(footerOnly)
		if budget < 0 {
			budget = 0
		}
		if len(preview) <= budget {
			return preview
		}
		if budget == 0 {
			preview = ""
			continue
		}
		preview = DefaultPreview(preview, budget)
	}
	return preview
}

func newHandleID(toolName string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("toolresult: rand: %w", err)
	}
	tool := sanitizePathSegment(toolName)
	if tool == "" {
		tool = "tool"
	}
	if len(tool) > 24 {
		tool = utf8Prefix(tool, 24)
	}
	return fmt.Sprintf("%s_%s_%s",
		time.Now().UTC().Format("20060102T150405"),
		tool,
		hex.EncodeToString(buf[:]),
	), nil
}

func sanitizePathSegment(s string) string {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	changed := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			changed = true
		}
	}
	built := b.String()
	out := strings.Trim(built, "._")
	if out != built {
		changed = true
	}
	if out == "" {
		out = "x"
		changed = true
	}
	// Escaping alone is not injective ("a:b" and "a/b" both became "a_b"),
	// which could merge security namespaces. Keep readable safe IDs unchanged;
	// append a stable digest whenever normalization altered the raw owner/tool.
	if changed {
		sum := sha256.Sum256([]byte(raw))
		out += "_" + hex.EncodeToString(sum[:8])
	}
	return out
}

const maxSessionSegmentBytes = 120

const derivedSessionPrefix = "_tr_"

func isPortableSessionSegment(s string) bool {
	if s == "" || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") ||
		strings.EqualFold(s, "default") || strings.HasPrefix(strings.ToLower(s), derivedSessionPrefix) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func isWindowsDeviceName(s string) bool {
	base := strings.ToLower(strings.SplitN(s, ".", 2)[0])
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(base) == 4 && base[3] >= '1' && base[3] <= '9' {
		return strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")
	}
	return false
}

func hashedSessionSegment(readable, raw string) string {
	sum := sha256.Sum256([]byte(raw))
	suffix := "_" + hex.EncodeToString(sum[:8])
	budget := maxSessionSegmentBytes - len(derivedSessionPrefix) - len(suffix)
	if budget < 1 {
		return derivedSessionPrefix + hex.EncodeToString(sum[:8])
	}
	prefix := strings.TrimRight(utf8Prefix(readable, budget), "._")
	if prefix == "" {
		prefix = "x"
	}
	return derivedSessionPrefix + prefix + suffix
}
