package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cloud-workspace cache operations are security-relevant even though the
// cache itself is local.  Keep a small append-only, machine-local audit log so
// a download, purge or remote delete remains diagnosable after the cache is
// removed.  Payloads intentionally contain identifiers and counters only;
// file contents, paths and credentials never enter the log.
type cloudWorkspaceAuditEvent struct {
	At          string `json:"at"`
	EventID     string `json:"event_id,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	Operation   string `json:"operation"`
	Outcome     string `json:"outcome"`
	Revision    string `json:"revision,omitempty"`
	Files       int    `json:"files,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// Remote audit delivery is deliberately best-effort and bounded.  The local
// JSONL file remains the immediate durable record; a small queue avoids
// blocking a sync/release path on a slow or partitioned Hub while preventing
// one noisy workspace from creating an unbounded number of goroutines.
type cloudWorkspaceRemoteAuditJob struct {
	app   *App
	event cloudWorkspaceAuditEvent
}

var (
	cloudWorkspaceRemoteAuditOnce  sync.Once
	cloudWorkspaceRemoteAuditQueue = make(chan cloudWorkspaceRemoteAuditJob, 256)
)

func enqueueCloudWorkspaceRemoteAudit(app *App, event cloudWorkspaceAuditEvent) {
	if app == nil || strings.TrimSpace(event.WorkspaceID) == "" {
		return
	}
	cloudWorkspaceRemoteAuditOnce.Do(func() {
		go func() {
			for job := range cloudWorkspaceRemoteAuditQueue {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = job.app.sendCloudWorkspaceAudit(ctx, job.event)
				cancel()
			}
		}()
	})
	select {
	case cloudWorkspaceRemoteAuditQueue <- cloudWorkspaceRemoteAuditJob{app: app, event: event}:
	default:
		// Keep the operation path healthy under a Hub outage. The local audit
		// record is still available for later support collection.
	}
}

func (a *App) sendCloudWorkspaceAudit(ctx context.Context, event cloudWorkspaceAuditEvent) error {
	if a == nil || strings.TrimSpace(event.WorkspaceID) == "" {
		return nil
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceAuditAPIPath(event.WorkspaceID), cloudWorkspaceHTTPOptions{
		timeout: 5 * time.Second, maxRead: cloudWorkspaceResponseMaxSize, accept: "application/json",
		contentType: "application/json", rawBody: raw,
		headers: map[string]string{"Idempotency-Key": "audit-" + event.EventID},
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("cloud workspace audit returned status %d", status)
	}
	return nil
}

// Audit details are deliberately reduced to a stable, path-free reason code.
// Raw Go/HTTP errors frequently include absolute cache paths, request URLs or
// other caller-controlled text; persisting them would violate the audit log's
// no-content/no-path contract and can leak secrets into support bundles.
func cloudWorkspaceAuditReason(detail string) string {
	s := strings.ToLower(strings.TrimSpace(detail))
	if s == "" {
		return ""
	}
	switch {
	case strings.Contains(s, "revoked"), strings.Contains(s, "撤权"), strings.Contains(s, "撤銷"):
		return "revoked"
	case strings.Contains(s, "context canceled"), strings.Contains(s, "canceled"):
		return "canceled"
	case strings.Contains(s, "deadline exceeded"), strings.Contains(s, "timeout"):
		return "timeout"
	case strings.Contains(s, "permission denied"), strings.Contains(s, "access denied"):
		return "permission_denied"
	case strings.Contains(s, "fenced"), strings.Contains(s, "lease") && strings.Contains(s, "invalid"):
		return "fenced"
	case strings.Contains(s, "hash mismatch"):
		return "integrity_mismatch"
	case strings.Contains(s, "revision conflict"), strings.Contains(s, "conflict"):
		return "conflict"
	case strings.Contains(s, "quota"), strings.Contains(s, "storage space"):
		return "quota_exceeded"
	default:
		return "operation_failed"
	}
}

var cloudWorkspaceAuditMu sync.Mutex

func cloudWorkspaceAuditPath(a *App) string {
	if a == nil {
		return ""
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return ""
	}
	return filepath.Join(root, "cloud-workspace-audit.jsonl")
}

func (a *App) recordCloudWorkspaceAudit(event cloudWorkspaceAuditEvent) {
	if a == nil || strings.TrimSpace(event.WorkspaceID) == "" || strings.TrimSpace(event.Operation) == "" {
		return
	}
	// Workspace IDs are normally server-generated cws_* values, but audit
	// records must remain safe even if a compromised/buggy Hub returns an
	// unexpected identifier.  Hash anything that is not a bounded cache ID so
	// paths and control characters cannot enter the append-only log.
	if !validCloudWorkspaceCacheID(event.WorkspaceID) || len(event.WorkspaceID) > 128 {
		sum := sha256.Sum256([]byte(strings.TrimSpace(event.WorkspaceID)))
		event.WorkspaceID = "sha256:" + hex.EncodeToString(sum[:8])
	} else {
		event.WorkspaceID = strings.TrimSpace(event.WorkspaceID)
	}
	if strings.TrimSpace(event.At) == "" {
		event.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(event.Outcome) == "" {
		event.Outcome = "ok"
	}
	if event.Outcome != "ok" {
		event.Detail = cloudWorkspaceAuditReason(event.Detail)
	} else {
		// Successful audit records never need free-form detail.  Keep this guard
		// in case a future caller accidentally supplies one.
		event.Detail = ""
	}
	if strings.TrimSpace(event.EventID) == "" {
		sum := sha256.Sum256([]byte(event.At + "\x00" + event.WorkspaceID + "\x00" + event.Operation + "\x00" + event.Outcome + "\x00" + event.Revision + "\x00" + fmt.Sprint(event.Files) + "\x00" + fmt.Sprint(event.Bytes) + "\x00" + event.Detail))
		event.EventID = "cwa_" + hex.EncodeToString(sum[:16])
	}
	path := cloudWorkspaceAuditPath(a)
	if path == "" {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	cloudWorkspaceAuditMu.Lock()
	defer cloudWorkspaceAuditMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	// Bound the local audit file.  Rotation is recoverable and never removes
	// the current log; the previous generation remains available for support
	// collection until the next rotation.
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= 4<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\n", raw); err != nil {
		return
	}
	_ = f.Sync()
	enqueueCloudWorkspaceRemoteAudit(a, event)
}
