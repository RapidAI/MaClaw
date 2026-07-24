package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Document process jobs: long/large draft transforms run off the request path
// and appear in GET /api/mobile/jobs (kind=document_process).

const (
	mobileDocProcessStatusQueued  = "queued"
	mobileDocProcessStatusRunning = "running"
	mobileDocProcessStatusReady   = "ready"
	mobileDocProcessStatusFailed  = "failed"

	// Large drafts auto-upgrade to async even without client async flag.
	mobileDocProcessAsyncRuneThreshold = 6000
	mobileDocProcessJobDelay           = 50 * time.Millisecond // yield for 202 response
)

var mobileDocumentProcessJobs = struct {
	sync.Mutex
	jobs map[string]mobileDocumentProcessJobRecord
}{
	jobs: make(map[string]mobileDocumentProcessJobRecord),
}

type mobileDocumentProcessJobRecord struct {
	JobID     string
	OwnerID   string
	TenantID  string
	DraftID   string
	Action    string
	Status    string
	Message   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// mobileDocumentProcessShouldAsync decides sync vs background for process API.
func mobileDocumentProcessShouldAsync(asyncFlag bool, markdown string) bool {
	if asyncFlag {
		return true
	}
	return utf8.RuneCountInString(markdown) >= mobileDocProcessAsyncRuneThreshold
}

func mobileDocumentProcessJobPayload(job mobileDocumentProcessJobRecord) map[string]any {
	return map[string]any{
		"job_id":     job.JobID,
		"kind":       "document_process",
		"draft_id":   job.DraftID,
		"action":     job.Action,
		"title":      mobileDocumentProcessJobTitle(job),
		"status":     job.Status,
		"message":    job.Message,
		"progress":   mobileJobProgressFromStatus(job.Status),
		"updated_at": job.UpdatedAt.UTC().Format(time.RFC3339),
		"created_at": job.CreatedAt.UTC().Format(time.RFC3339),
		"deep_link":  "/documents",
	}
}

func mobileDocumentProcessJobTitle(job mobileDocumentProcessJobRecord) string {
	action := strings.TrimSpace(job.Action)
	if action == "" {
		action = "process"
	}
	return "文档处理 · " + action
}

func mobileEnqueueDocumentProcessJob(
	principal *auth.ViewerPrincipal,
	draftID, action string,
) mobileDocumentProcessJobRecord {
	now := time.Now().UTC()
	ownerID := mobilePrincipalOwnerID(principal)
	tenantID := ""
	if principal != nil {
		tenantID = strings.TrimSpace(principal.TenantID)
	}
	job := mobileDocumentProcessJobRecord{
		JobID:     fmt.Sprintf("mobdocproc_%d", now.UnixNano()),
		OwnerID:   ownerID,
		TenantID:  tenantID,
		DraftID:   draftID,
		Action:    action,
		Status:    mobileDocProcessStatusQueued,
		Message:   "文档处理任务已排队",
		CreatedAt: now,
		UpdatedAt: now,
	}
	mobileDocumentProcessJobs.Lock()
	mobileDocumentProcessJobs.jobs[job.JobID] = job
	mobileDocumentProcessJobs.Unlock()
	go mobileRunDocumentProcessJob(job.JobID, principal)
	return job
}

func mobileDocumentProcessJobUpdate(jobID string, mutate func(*mobileDocumentProcessJobRecord)) {
	mobileDocumentProcessJobs.Lock()
	defer mobileDocumentProcessJobs.Unlock()
	job, ok := mobileDocumentProcessJobs.jobs[jobID]
	if !ok {
		return
	}
	mutate(&job)
	job.UpdatedAt = time.Now().UTC()
	mobileDocumentProcessJobs.jobs[jobID] = job
}

func mobileRunDocumentProcessJob(jobID string, principal *auth.ViewerPrincipal) {
	// Brief yield so HTTP 202 is flushed before work starts.
	time.Sleep(mobileDocProcessJobDelay)

	mobileDocumentProcessJobUpdate(jobID, func(j *mobileDocumentProcessJobRecord) {
		j.Status = mobileDocProcessStatusRunning
		j.Message = "processing"
	})

	mobileDocumentProcessJobs.Lock()
	job, ok := mobileDocumentProcessJobs.jobs[jobID]
	mobileDocumentProcessJobs.Unlock()
	if !ok {
		return
	}

	now := time.Now().UTC()
	mobileDocuments.Lock()
	record, found := mobileDocuments.drafts[job.DraftID]
	if !found || record.OwnerID != job.OwnerID || !mobileMeetingRecordingTenantMatches(job.TenantID, record.TenantID) {
		mobileDocuments.Unlock()
		mobileDocumentProcessJobUpdate(jobID, func(j *mobileDocumentProcessJobRecord) {
			j.Status = mobileDocProcessStatusFailed
			j.Message = "draft not found"
		})
		return
	}
	record.Markdown = mobileProcessDocumentMarkdown(job.Action, record.Markdown)
	record.UpdatedAt = now
	mobileDocuments.drafts[job.DraftID] = record
	mobileDocuments.Unlock()
	mobilePersistState()
	if principal != nil {
		go mobileIngestDocumentDraft(principal, record)
	}

	mobileDocumentProcessJobUpdate(jobID, func(j *mobileDocumentProcessJobRecord) {
		j.Status = mobileDocProcessStatusReady
		j.Message = "processed"
	})

	// Realtime nudge for mobile clients polling documents / jobs.
	if principal != nil {
		mobileRealtimeBroadcast(
			principal.TenantID,
			principal.UserID,
			map[string]any{
				"type":     "document_process_job",
				"job_id":   job.JobID,
				"draft_id": job.DraftID,
				"action":   job.Action,
				"status":   mobileDocProcessStatusReady,
			},
		)
	}
}

func mobileCollectDocumentProcessJobs(ownerID, tenantID string) []mobileJobItem {
	mobileDocumentProcessJobs.Lock()
	defer mobileDocumentProcessJobs.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileDocumentProcessJobs.jobs {
		if rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
			continue
		}
		out = append(out, mobileJobItem{
			JobID:     rec.JobID,
			Kind:      "document_process",
			Title:     mobileDocumentProcessJobTitle(rec),
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/documents",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			DraftID:   rec.DraftID,
		})
	}
	return out
}

// MobileDocumentProcessJobStatusHandler returns one document-process job.
//
//	GET /api/mobile/documents/process-jobs/{jobId}
func MobileDocumentProcessJobStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
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
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "job id is required")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		mobileDocumentProcessJobs.Lock()
		job, ok := mobileDocumentProcessJobs.jobs[jobID]
		mobileDocumentProcessJobs.Unlock()
		if !ok || job.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, job.TenantID) {
			writeError(w, http.StatusNotFound, "JOB_NOT_FOUND", "document process job not found")
			return
		}
		payload := mobileDocumentProcessJobPayload(job)
		// Include latest draft when ready so client can refresh editor without
		// a second list call.
		if job.Status == mobileDocProcessStatusReady {
			mobileDocuments.Lock()
			if draft, ok := mobileDocuments.drafts[job.DraftID]; ok && draft.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(principal.TenantID, draft.TenantID) {
				payload["draft"] = mobileDocumentDraftPayload(draft)
			}
			mobileDocuments.Unlock()
		}
		writeJSON(w, http.StatusOK, payload)
	}
}
