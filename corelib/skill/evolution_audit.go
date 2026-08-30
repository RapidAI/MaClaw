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
	Timestamp string `json:"timestamp"`
	Kind      string `json:"kind"` // repaired | optimized | discovered | failed | queue_full | other
	// RequestID links events belonging to one evolution request. Legacy rows
	// may omit it and are treated as having unknown correlation.
	RequestID string `json:"request_id,omitempty"`
	// Attempt is the one-based attempt number within RequestID.
	Attempt     string `json:"attempt,omitempty"`
	Skill       string `json:"skill,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Source      string `json:"source,omitempty"` // desktop | tui | cli | test
	// Status carries an outcome marker from the event payload (e.g.
	// "rejected" for a reviewed repair draft), so the audit list can tell a
	// pending draft apart from a rejected one.
	Status string `json:"status,omitempty"`
	// Via carries the channel marker from the event payload (e.g.
	// "reviewed_draft" / "reviewed_draft_disable").
	Via            string `json:"via,omitempty"`
	Action         string `json:"action,omitempty"`
	Decision       string `json:"decision,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Risk           string `json:"risk,omitempty"`
	GateStatus     string `json:"gate_status,omitempty"`
	EvidenceDigest string `json:"evidence_digest,omitempty"`
	BackupVersion  string `json:"backup_version,omitempty"`
	Operator       string `json:"operator,omitempty"`
	Trigger        string `json:"trigger,omitempty"`
	SchemaVersion  string `json:"schema_version,omitempty"`
	ConfigRevision string `json:"config_revision,omitempty"`
	EvidenceMode   string `json:"evidence_mode,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	Termination    string `json:"termination,omitempty"`
}

const (
	// EvolutionAuditMaxKeep is the max number of events returned / retained on trim.
	EvolutionAuditMaxKeep = 200
	// evolutionAuditMaxFileBytes triggers a rewrite trim when the log grows too large.
	evolutionAuditMaxFileBytes = 2 << 20 // 2 MiB
)

var evolutionAuditMu sync.Mutex

// EvolutionAuditHealthSnapshot describes whether the local audit sink is
// currently writable. It is intentionally process-local: the JSONL file is
// the durable source of events, while this snapshot lets UI and write gates
// react immediately when the sink becomes unavailable.
type EvolutionAuditHealthSnapshot struct {
	Available     bool   `json:"audit_available"`
	LastError     string `json:"last_audit_error,omitempty"`
	FailureCount  uint64 `json:"audit_failure_count"`
	LastSuccessAt string `json:"last_audit_success_at,omitempty"`
}

var evolutionAuditHealth struct {
	mu            sync.RWMutex
	available     bool
	lastError     string
	failureCount  uint64
	lastSuccessAt string
}

// EvolutionAuditHealth returns a snapshot for GUI diagnostics and policy
// decisions. A sink is considered healthy until the first failed write.
func EvolutionAuditHealth() EvolutionAuditHealthSnapshot {
	evolutionAuditHealth.mu.RLock()
	defer evolutionAuditHealth.mu.RUnlock()
	return EvolutionAuditHealthSnapshot{
		Available:     evolutionAuditHealth.available || evolutionAuditHealth.lastError == "",
		LastError:     evolutionAuditHealth.lastError,
		FailureCount:  evolutionAuditHealth.failureCount,
		LastSuccessAt: evolutionAuditHealth.lastSuccessAt,
	}
}

func markEvolutionAuditFailure(err error) {
	if err == nil {
		return
	}
	evolutionAuditHealth.mu.Lock()
	evolutionAuditHealth.available = false
	evolutionAuditHealth.lastError = err.Error()
	evolutionAuditHealth.failureCount++
	evolutionAuditHealth.mu.Unlock()
}

func markEvolutionAuditSuccess() {
	evolutionAuditHealth.mu.Lock()
	evolutionAuditHealth.available = true
	evolutionAuditHealth.lastError = ""
	evolutionAuditHealth.lastSuccessAt = time.Now().UTC().Format(time.RFC3339)
	evolutionAuditHealth.mu.Unlock()
}

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
	case EventSkillRepairDraftReady:
		return "repair_draft"
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
	_ = RecordEvolutionEventStrict(event, data, source)
}

// RecordEvolutionEventStrict is the checked variant used by critical write
// paths. Unlike the legacy best-effort helper it returns sink failures, while
// still updating the process health snapshot.
func RecordEvolutionEventStrict(event string, data map[string]string, source string) error {
	ev := EvolutionAuditEvent{
		Kind:   KindFromEventName(event),
		Source: strings.TrimSpace(source),
	}
	if data != nil {
		ev.RequestID = strings.TrimSpace(data["request_id"])
		ev.Attempt = strings.TrimSpace(data["attempt"])
		ev.Skill = strings.TrimSpace(data["skill"])
		ev.Explanation = strings.TrimSpace(data["explanation"])
		ev.Status = strings.TrimSpace(data["status"])
		ev.Via = strings.TrimSpace(data["via"])
		ev.Action = strings.TrimSpace(data["action"])
		ev.Decision = strings.TrimSpace(data["decision"])
		ev.Reason = strings.TrimSpace(data["reason"])
		ev.Risk = strings.TrimSpace(data["risk"])
		ev.GateStatus = strings.TrimSpace(data["gate_status"])
		ev.EvidenceDigest = strings.TrimSpace(data["evidence_digest"])
		ev.BackupVersion = strings.TrimSpace(data["backup_version"])
		ev.Operator = strings.TrimSpace(data["operator"])
		ev.Trigger = strings.TrimSpace(data["trigger"])
		ev.SchemaVersion = strings.TrimSpace(data["schema_version"])
		ev.ConfigRevision = strings.TrimSpace(data["config_revision"])
		ev.EvidenceMode = strings.TrimSpace(data["evidence_mode"])
		ev.FailureReason = strings.TrimSpace(data["failure_reason"])
		ev.Termination = strings.TrimSpace(data["termination"])
	}
	if err := AppendEvolutionAudit(DefaultEvolutionAuditPath(), ev); err != nil {
		return err
	}
	return nil
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
		markEvolutionAuditFailure(err)
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		markEvolutionAuditFailure(err)
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		markEvolutionAuditFailure(werr)
		return werr
	}
	if cerr != nil {
		markEvolutionAuditFailure(cerr)
		return cerr
	}
	markEvolutionAuditSuccess()

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
