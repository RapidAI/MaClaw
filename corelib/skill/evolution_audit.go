package skill

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// EvolutionAuditEvent is one durable row in the skill evolution audit log.
// Stored as JSONL under DefaultEvolutionAuditPath().
type EvolutionAuditEvent struct {
	Timestamp   string `json:"timestamp"`
	Kind        string `json:"kind"` // repaired | optimized | discovered | failed | queue_full | other
	Skill       string `json:"skill,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Source      string `json:"source,omitempty"` // desktop | tui | cli | test
}

const (
	// EvolutionAuditMaxKeep is the max number of events returned / retained on trim.
	EvolutionAuditMaxKeep = 200
	// evolutionAuditMaxFileBytes triggers a rewrite trim when the log grows too large.
	evolutionAuditMaxFileBytes = 2 << 20 // 2 MiB
)

var evolutionAuditMu sync.Mutex

// DefaultEvolutionAuditPath returns ~/.maclaw/skill_evolution/audit.jsonl
// (or the process MaclawBaseDir equivalent).
func DefaultEvolutionAuditPath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "skill_evolution", "audit.jsonl")
}

// KindFromEventName maps pipeline/event constants to short audit kinds.
func KindFromEventName(event string) string {
	switch strings.TrimSpace(event) {
	case EventSkillRepaired:
		return "repaired"
	case EventSkillOptimized:
		return "optimized"
	case EventSkillAutoDiscovered:
		return "discovered"
	case EventSkillExecutionFailed:
		return "failed"
	case EventSkillEvolutionQueueFull:
		return "queue_full"
	case "skill:yaml_restore", "yaml_restore":
		return "yaml_restore"
	case "skill:maintenance_apply", "maintenance_apply":
		return "maintenance_apply"
	case "skill:mark_needs_review", "mark_needs_review":
		return "mark_needs_review"
	default:
		if event == "" {
			return "other"
		}
		// Strip optional "skill:" prefix for compact UI kinds.
		if strings.HasPrefix(event, "skill:") {
			return strings.TrimPrefix(event, "skill:")
		}
		return event
	}
}

// RecordEvolutionEvent appends a durable audit row for a pipeline event.
// Failures are silent (best-effort) so audit never blocks the evolution path.
func RecordEvolutionEvent(event string, data map[string]string, source string) {
	ev := EvolutionAuditEvent{
		Kind:   KindFromEventName(event),
		Source: strings.TrimSpace(source),
	}
	if data != nil {
		ev.Skill = strings.TrimSpace(data["skill"])
		ev.Explanation = strings.TrimSpace(data["explanation"])
	}
	_ = AppendEvolutionAudit(DefaultEvolutionAuditPath(), ev)
}

// AppendEvolutionAudit writes one event to path (JSONL). Creates parent dirs.
// Timestamp is set to RFC3339 UTC when empty.
func AppendEvolutionAudit(path string, ev EvolutionAuditEvent) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultEvolutionAuditPath()
	}
	if strings.TrimSpace(ev.Timestamp) == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(ev.Kind) == "" {
		ev.Kind = "other"
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	evolutionAuditMu.Lock()
	defer evolutionAuditMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}

	// Best-effort size trim so the log cannot grow without bound.
	if st, err := os.Stat(path); err == nil && st.Size() > evolutionAuditMaxFileBytes {
		_ = trimEvolutionAuditFileLocked(path, EvolutionAuditMaxKeep)
	}
	return nil
}

// ListEvolutionAudit returns the newest events first, up to limit (capped).
func ListEvolutionAudit(path string, limit int) ([]EvolutionAuditEvent, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultEvolutionAuditPath()
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > EvolutionAuditMaxKeep {
		limit = EvolutionAuditMaxKeep
	}

	evolutionAuditMu.Lock()
	defer evolutionAuditMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	// Keep a rolling window of the last N lines without loading unbounded memory.
	window := make([]EvolutionAuditEvent, 0, limit)
	sc := bufio.NewScanner(f)
	// Allow slightly large explanation lines.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var ev EvolutionAuditEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		window = append(window, ev)
		if len(window) > EvolutionAuditMaxKeep {
			// Drop oldest while scanning a huge file.
			window = window[len(window)-EvolutionAuditMaxKeep:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// Newest first.
	for i, j := 0, len(window)-1; i < j; i, j = i+1, j-1 {
		window[i], window[j] = window[j], window[i]
	}
	if len(window) > limit {
		window = window[:limit]
	}
	return window, nil
}

// EvolutionAuditToolPayload builds the standard manage_skill evolution_audit JSON object.
// limit defaults to 50 (capped at EvolutionAuditMaxKeep). skillFilter matches skill name
// (case-insensitive substring). path empty uses DefaultEvolutionAuditPath().
func EvolutionAuditToolPayload(path string, limit int, skillFilter string) map[string]interface{} {
	if limit <= 0 {
		limit = 50
	}
	if path == "" {
		path = DefaultEvolutionAuditPath()
	}
	// Fetch a bit more when filtering so limit applies after filter.
	fetch := limit
	skillFilter = strings.TrimSpace(skillFilter)
	if skillFilter != "" && fetch < EvolutionAuditMaxKeep {
		fetch = EvolutionAuditMaxKeep
	}
	events, err := ListEvolutionAudit(path, fetch)
	payload := map[string]interface{}{
		"ok":            true,
		"non_executing": true,
		"boundary":      "read-only skill evolution audit log",
		"path":          path,
		"limit":         limit,
	}
	if skillFilter != "" {
		payload["skill_filter"] = skillFilter
	}
	if err != nil {
		payload["ok"] = false
		payload["error"] = err.Error()
		payload["events"] = []EvolutionAuditEvent{}
		payload["count"] = 0
		return payload
	}
	if skillFilter != "" {
		want := strings.ToLower(skillFilter)
		filtered := make([]EvolutionAuditEvent, 0, len(events))
		for _, ev := range events {
			if strings.Contains(strings.ToLower(ev.Skill), want) {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}
	if len(events) > limit {
		events = events[:limit]
	}
	payload["events"] = events
	payload["count"] = len(events)
	return payload
}

func trimEvolutionAuditFileLocked(path string, keep int) error {
	if keep <= 0 {
		keep = EvolutionAuditMaxKeep
	}
	// Re-read without nested lock (caller holds evolutionAuditMu).
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var all [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		all = append(all, []byte(line))
	}
	_ = f.Close()
	if err := sc.Err(); err != nil {
		return err
	}
	if len(all) <= keep {
		return nil
	}
	all = all[len(all)-keep:]
	tmp := path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, line := range all {
		if _, err := out.Write(append(line, '\n')); err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
