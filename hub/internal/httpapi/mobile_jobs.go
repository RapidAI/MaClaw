package httpapi

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileJobsHandler returns a unified long-running job list for the current
// viewer: document import/export, digital-employee tasks, and backend SSH work.
//
//	GET /api/mobile/jobs
//
// Response shape (design §5):
//
//	{ "jobs": [ { job_id, kind, title, status, progress, updated_at, deep_link, ... } ], "count": N }
func MobileJobsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
			return
		}

		jobs := make([]mobileJobItem, 0, 32)
		jobs = append(jobs, mobileCollectDocumentUploadJobs(ownerID)...)
		jobs = append(jobs, mobileCollectDocumentExportJobs(ownerID)...)
		jobs = append(jobs, mobileCollectDocumentProcessJobs(ownerID)...)
		jobs = append(jobs, mobileCollectDigitalEmployeeJobs(ownerID)...)
		jobs = append(jobs, mobileCollectBackendSSHJobs(ownerID)...)
		jobs = append(jobs, mobileCollectAgentJobs(ownerID)...)
		jobs = append(jobs, mobileCollectMeetingRecordingJobs(ownerID)...)

		sort.SliceStable(jobs, func(i, j int) bool {
			return jobs[i].UpdatedAt.After(jobs[j].UpdatedAt)
		})

		// Cap list size for mobile; oldest fall off after sort.
		const maxJobs = 50
		if len(jobs) > maxJobs {
			jobs = jobs[:maxJobs]
		}

		out := make([]map[string]any, 0, len(jobs))
		active := 0
		for _, j := range jobs {
			if mobileJobIsActive(j.Status) {
				active++
			}
			out = append(out, j.toMap())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"jobs":         out,
			"count":        len(out),
			"active_count": active,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

type mobileJobItem struct {
	JobID     string
	Kind      string
	Title     string
	Status    string
	Progress  float64 // 0..1 when known; -1 unknown
	Message   string
	DeepLink  string
	UpdatedAt time.Time
	// Optional related ids for clients.
	DraftID    string
	EmployeeID string
	SessionID  string
}

func (j mobileJobItem) toMap() map[string]any {
	m := map[string]any{
		"job_id":     j.JobID,
		"kind":       j.Kind,
		"title":      j.Title,
		"status":     j.Status,
		"updated_at": j.UpdatedAt.UTC().Format(time.RFC3339),
		"deep_link":  j.DeepLink,
	}
	if j.Progress >= 0 {
		m["progress"] = j.Progress
	}
	if strings.TrimSpace(j.Message) != "" {
		m["message"] = j.Message
	}
	if j.DraftID != "" {
		m["draft_id"] = j.DraftID
	}
	if j.EmployeeID != "" {
		m["employee_id"] = j.EmployeeID
	}
	if j.SessionID != "" {
		m["session_id"] = j.SessionID
	}
	return m
}

func mobilePrincipalOwnerID(principal *auth.ViewerPrincipal) string {
	if principal == nil {
		return ""
	}
	if id := strings.TrimSpace(principal.UserID); id != "" {
		return id
	}
	return strings.TrimSpace(principal.Email)
}

func mobileJobIsActive(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return false
	}
	// In-flight kill: still active until runner settles to cancelled.
	if s == "kill_requested" {
		return true
	}
	switch {
	case strings.Contains(s, "ready"),
		strings.Contains(s, "done"),
		strings.Contains(s, "complete"),
		strings.Contains(s, "success"),
		strings.Contains(s, "fail"),
		strings.Contains(s, "error"),
		strings.Contains(s, "cancel"):
		return false
	default:
		return true
	}
}

func mobileJobProgressFromStatus(status string) float64 {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(s, "ready"),
		strings.Contains(s, "done"),
		strings.Contains(s, "complete"),
		strings.Contains(s, "success"):
		return 1
	case strings.Contains(s, "fail"),
		strings.Contains(s, "error"),
		strings.Contains(s, "cancel"):
		return 1
	case strings.Contains(s, "queued"), strings.Contains(s, "pending"):
		return 0.05
	case strings.Contains(s, "claim"), strings.Contains(s, "running"), strings.Contains(s, "process"):
		return 0.5
	default:
		return -1
	}
}

func mobileCollectDocumentUploadJobs(ownerID string) []mobileJobItem {
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileDocuments.uploads {
		if rec.OwnerID != ownerID {
			continue
		}
		title := strings.TrimSpace(rec.Filename)
		if title == "" {
			title = "文档导入"
		} else {
			title = "导入 · " + title
		}
		out = append(out, mobileJobItem{
			JobID:     rec.TaskID,
			Kind:      "document_upload",
			Title:     title,
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/documents",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.UploadedAt),
			DraftID:   rec.DraftID,
		})
	}
	return out
}

func mobileCollectDocumentExportJobs(ownerID string) []mobileJobItem {
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileDocuments.exports {
		if rec.OwnerID != ownerID {
			continue
		}
		title := "导出"
		if f := strings.TrimSpace(rec.Format); f != "" {
			title = "导出 · " + f
		}
		out = append(out, mobileJobItem{
			JobID:     rec.JobID,
			Kind:      "document_export",
			Title:     title,
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/documents",
			UpdatedAt: nonZeroTime(rec.CreatedAt, time.Time{}),
			DraftID:   rec.DraftID,
		})
	}
	return out
}

func mobileCollectDigitalEmployeeJobs(ownerID string) []mobileJobItem {
	mobileDigitalEmployeeTasks.Lock()
	defer mobileDigitalEmployeeTasks.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileDigitalEmployeeTasks.tasks {
		if rec.OwnerID != ownerID {
			continue
		}
		title := strings.TrimSpace(rec.Prompt)
		if title == "" {
			title = "数字员工任务"
		} else {
			runes := []rune(title)
			if len(runes) > 48 {
				title = string(runes[:48]) + "…"
			}
			title = "员工 · " + title
		}
		out = append(out, mobileJobItem{
			JobID:      rec.TaskID,
			Kind:       "digital_employee",
			Title:      title,
			Status:     rec.Status,
			Progress:   mobileJobProgressFromStatus(rec.Status),
			Message:    rec.Message,
			DeepLink:   "/employees",
			UpdatedAt:  nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			EmployeeID: rec.EmployeeID,
		})
	}
	return out
}

func mobileCollectBackendSSHJobs(ownerID string) []mobileJobItem {
	out := make([]mobileJobItem, 0)
	mobileBackendSSHTasks.Lock()
	for _, rec := range mobileBackendSSHTasks.tasks {
		if rec.OwnerID != ownerID {
			continue
		}
		title := strings.TrimSpace(rec.Command)
		if title == "" {
			title = "SSH 命令"
		} else {
			runes := []rune(title)
			if len(runes) > 48 {
				title = string(runes[:48]) + "…"
			}
			title = "SSH · " + title
		}
		out = append(out, mobileJobItem{
			JobID:     rec.TaskID,
			Kind:      "ssh_command",
			Title:     title,
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/servers",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			SessionID: rec.SessionID,
		})
	}
	mobileBackendSSHTasks.Unlock()

	mobileBackendSSHFileOperations.Lock()
	for _, rec := range mobileBackendSSHFileOperations.operations {
		if rec.OwnerID != ownerID {
			continue
		}
		action := strings.TrimSpace(rec.Action)
		if action == "" {
			action = "file"
		}
		path := strings.TrimSpace(rec.RemotePath)
		if path == "" {
			path = strings.TrimSpace(rec.LocalPath)
		}
		title := "SSH 文件 · " + action
		if path != "" {
			title += " · " + path
		}
		out = append(out, mobileJobItem{
			JobID:     rec.OperationID,
			Kind:      "ssh_file",
			Title:     title,
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/servers",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			SessionID: rec.SessionID,
		})
	}
	mobileBackendSSHFileOperations.Unlock()

	// Active hub_exec / desktop sessions so the 后台 tab surfaces open control sessions.
	mobileBackendSSHSessions.Lock()
	for _, rec := range mobileBackendSSHSessions.sessions {
		if rec.OwnerID != ownerID {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(rec.Status))
		state := strings.ToLower(strings.TrimSpace(rec.State))
		// Skip fully closed sessions.
		if status == "closed" || state == "closed" || state == "hub_closed" {
			continue
		}
		mode := strings.TrimSpace(rec.ExecMode)
		if mode == "" {
			mode = "desktop_exec"
		}
		title := "SSH 会话 · " + mode
		if pid := strings.TrimSpace(rec.ServerProfileID); pid != "" {
			title += " · " + pid
		}
		msg := strings.TrimSpace(rec.Message)
		if msg == "" {
			msg = status
		}
		out = append(out, mobileJobItem{
			JobID:     rec.SessionID,
			Kind:      "ssh_session",
			Title:     title,
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   msg,
			DeepLink:  "/servers",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			SessionID: rec.SessionID,
		})
	}
	mobileBackendSSHSessions.Unlock()
	return out
}

func nonZeroTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	if !fallback.IsZero() {
		return fallback
	}
	return time.Unix(0, 0).UTC()
}
