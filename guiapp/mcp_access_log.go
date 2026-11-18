package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	mcpAccessEntryLimit   = 200
	mcpAccessFileMaxBytes = 1 << 20
	mcpAccessFileBase     = "mcp_host_access.jsonl"
	mcpAccessTimeLayout   = "2006-01-02 15:04:05"
)

// mcpAccessEntry is one remote task initiation seen by the inbound MCP HTTP
// listener. The API key is stored masked: full secrets must not land in a
// log file users may share, and the mask is enough to match the key shown
// on the MCP server panel.
type mcpAccessEntry struct {
	Time        time.Time `json:"time"`
	IP          string    `json:"ip"`
	APIKey      string    `json:"api_key"`
	Action      string    `json:"action"`
	Task        string    `json:"task"`
	ProjectPath string    `json:"project_path,omitempty"`
}

// mcpClientInfo rides the request context: ServeHTTP records who called right
// after the Bearer check, and the create/send paths read it at the moment a
// task actually starts. No context info (tests calling the task methods
// directly) means nothing gets logged.
type mcpClientInfo struct {
	IP     string
	APIKey string
}

type mcpClientInfoContextKey struct{}

func mcpClientInfoFromRequest(r *http.Request, token string) (mcpClientInfo, bool) {
	host := strings.TrimSpace(r.RemoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		host = h
	}
	if host == "" {
		return mcpClientInfo{}, false
	}
	return mcpClientInfo{IP: host, APIKey: mcpMaskKey(token)}, true
}

func mcpClientInfoFromContext(ctx context.Context) (mcpClientInfo, bool) {
	if ctx == nil {
		return mcpClientInfo{}, false
	}
	info, ok := ctx.Value(mcpClientInfoContextKey{}).(mcpClientInfo)
	return info, ok
}

// mcpMaskKey keeps enough of the key to match it against the panel display
// while keeping the full secret out of shareable logs.
func mcpMaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	prefix, rest := "", key
	if strings.HasPrefix(key, mcpAPIKeyPrefix) {
		prefix, rest = mcpAPIKeyPrefix, key[len(mcpAPIKeyPrefix):]
	}
	runes := []rune(rest)
	switch {
	case len(runes) == 0:
		return prefix
	case len(runes) <= 6:
		return prefix + string(runes)
	case len(runes) <= 10:
		return prefix + string(runes[:3]) + "…" + string(runes[len(runes)-3:])
	default:
		return prefix + string(runes[:6]) + "…" + string(runes[len(runes)-4:])
	}
}

// mcpAccessStore keeps recent MCP task initiations: an in-memory ring plus a
// JSONL file under the logs dir so history survives app restarts.
type mcpAccessStore struct {
	mu      sync.Mutex
	loaded  bool
	dir     string // logs dir; empty resolves to corelib.MaclawLogsDir() lazily
	entries []mcpAccessEntry
}

func (s *mcpAccessStore) file() string {
	dir := s.dir
	if strings.TrimSpace(dir) == "" {
		dir = corelib.MaclawLogsDir()
	}
	return filepath.Join(dir, mcpAccessFileBase)
}

func (s *mcpAccessStore) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.entries = s.readFileLocked()
}

func (s *mcpAccessStore) readFileLocked() []mcpAccessEntry {
	data, err := os.ReadFile(s.file())
	if err != nil {
		return nil // missing file is an empty log, not an error
	}
	var entries []mcpAccessEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry mcpAccessEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) > mcpAccessEntryLimit {
		entries = entries[len(entries)-mcpAccessEntryLimit:]
	}
	return entries
}

func (s *mcpAccessStore) record(entry mcpAccessEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	s.entries = append(s.entries, entry)
	if len(s.entries) > mcpAccessEntryLimit {
		s.entries = s.entries[len(s.entries)-mcpAccessEntryLimit:]
	}
	path := s.file()
	if info, err := os.Stat(path); err == nil && info.Size() > mcpAccessFileMaxBytes {
		if err := s.rewriteLocked(path); err != nil {
			// A transient failure (e.g. the file is briefly held) must not drop
			// the entry: fall through and try a plain append instead.
			log.Printf("[mcp-host] access log rewrite failed: %v", err)
		} else {
			return
		}
	}
	if err := s.appendLocked(path, entry); err != nil {
		log.Printf("[mcp-host] access log write failed: %v", err)
	}
}

func (s *mcpAccessStore) appendLocked(path string, entry mcpAccessEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (s *mcpAccessStore) rewriteLocked(path string) error {
	var buf strings.Builder
	for _, entry := range s.entries {
		line, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o600)
}

func (s *mcpAccessStore) snapshot() []mcpAccessEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	out := make([]mcpAccessEntry, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- { // newest first
		out = append(out, s.entries[i])
	}
	return out
}

func (s *mcpAccessStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = true
	s.entries = nil
	if err := os.Remove(s.file()); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[mcp-host] access log clear failed: %v", err)
	}
}

// recordMCPAccess notes one remote task initiation. Requests without client
// info in their context are skipped, so direct task-API calls stay unlogged.
func (a *App) recordMCPAccess(ctx context.Context, action, task, projectPath string) {
	if a == nil {
		return
	}
	info, ok := mcpClientInfoFromContext(ctx)
	if !ok {
		return
	}
	entry := mcpAccessEntry{
		Time:        time.Now(),
		IP:          info.IP,
		APIKey:      mcpMaskKey(info.APIKey), // mask again here: full secrets must not land in logs even if the context carries them raw
		Action:      action,
		Task:        strings.TrimSpace(task),
		ProjectPath: strings.TrimSpace(projectPath),
	}
	a.mcpAccess.record(entry)
	log.Printf("[mcp-host] access ip=%s key=%s action=%s task=%q", entry.IP, entry.APIKey, entry.Action, entry.Task)
}

// GetMCPAccessLog returns recent MCP task initiations, newest first.
func (a *App) GetMCPAccessLog() []map[string]any {
	if a == nil {
		return []map[string]any{}
	}
	entries := a.mcpAccess.snapshot()
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"time":        entry.Time.Local().Format(mcpAccessTimeLayout),
			"ip":          entry.IP,
			"apiKey":      entry.APIKey,
			"action":      entry.Action,
			"task":        entry.Task,
			"projectPath": entry.ProjectPath,
		})
	}
	return out
}

// ClearMCPAccessLog drops every recorded entry, in memory and on disk.
func (a *App) ClearMCPAccessLog() error {
	if a == nil {
		return fmt.Errorf("MCP 访问日志还没有准备好")
	}
	a.mcpAccess.clear()
	return nil
}
