package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

var (
	srvSchedulerMu  sync.RWMutex
	srvSchedulerMgr *scheduler.Manager

	deliveryAuditMu sync.Mutex
)

func setSrvSchedulerManager(m *scheduler.Manager) {
	srvSchedulerMu.Lock()
	defer srvSchedulerMu.Unlock()
	srvSchedulerMgr = m
}

func getSrvSchedulerManager() *scheduler.Manager {
	srvSchedulerMu.RLock()
	defer srvSchedulerMu.RUnlock()
	return srvSchedulerMgr
}

// ---------------------------------------------------------------------------
// Delivery audit log (JSONL under data root)
// ---------------------------------------------------------------------------

const deliveryAuditFileName = "scheduled_delivery_audit.jsonl"
const deliveryAuditMaxRead = 200
const deliveryAuditRotateBytes = 2 << 20 // 2 MiB
const deliveryAuditKeepLines = 500

// DeliveryAuditEntry records one push attempt (per target) or setup failure.
type DeliveryAuditEntry struct {
	Time        time.Time `json:"time"`
	TaskID      string    `json:"task_id"`
	TaskName    string    `json:"task_name,omitempty"`
	Channel     string    `json:"channel,omitempty"`
	TargetIndex int       `json:"target_index"`
	TargetKind  string    `json:"target_kind,omitempty"`
	Peer        string    `json:"peer,omitempty"`
	OK          bool      `json:"ok"`
	Error       string    `json:"error,omitempty"`
}

func deliveryAuditPath(dataRoot string) string {
	return filepath.Join(strings.TrimSpace(dataRoot), deliveryAuditFileName)
}

func appendDeliveryAudit(dataRoot string, e DeliveryAuditEntry) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	// Never persist self placeholders as peer (not a concrete platform id).
	if scheduler.IsSelfPeerID(e.Peer) {
		e.Peer = ""
	}
	path := deliveryAuditPath(dataRoot)

	deliveryAuditMu.Lock()
	defer deliveryAuditMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[MaClawSrv-Scheduler] audit mkdir: %v", err)
		return
	}
	// Best-effort rotate oversized logs so list stays cheap.
	if st, err := os.Stat(path); err == nil && st.Size() > deliveryAuditRotateBytes {
		rotateDeliveryAuditLocked(path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[MaClawSrv-Scheduler] audit open: %v", err)
		return
	}
	defer f.Close()
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		log.Printf("[MaClawSrv-Scheduler] audit write: %v", err)
	}
}

func rotateDeliveryAuditLocked(path string) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	lines := strings.Split(string(data), "\n")
	// Drop empty trailing line from final newline.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= deliveryAuditKeepLines {
		return
	}
	keep := lines[len(lines)-deliveryAuditKeepLines:]
	var b strings.Builder
	for _, line := range keep {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// listDeliveryAudit returns newest-first entries (capped).
func listDeliveryAudit(dataRoot string, limit int) []DeliveryAuditEntry {
	if limit <= 0 || limit > deliveryAuditMaxRead {
		limit = deliveryAuditMaxRead
	}
	path := deliveryAuditPath(dataRoot)

	deliveryAuditMu.Lock()
	data, err := os.ReadFile(path)
	deliveryAuditMu.Unlock()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var all []DeliveryAuditEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e DeliveryAuditEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		all = append(all, e)
	}
	// Newest last in file → reverse.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}
