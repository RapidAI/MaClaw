package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Long-running official assistant jobs (design: short→sync SSE; long→后台 job).
// Jobs appear in GET /api/mobile/jobs and have dedicated create/status endpoints.

const (
	mobileAgentJobStatusQueued  = "queued"
	mobileAgentJobStatusRunning = "running"
	mobileAgentJobStatusReady   = "ready"
	mobileAgentJobStatusFailed  = "failed"

	mobileAgentJobMaxActivePerUser = 5
	mobileAgentJobAnswerMaxRunes   = 50000
	mobileAgentJobRunTimeout       = 6 * time.Minute
)

var mobileAgentJobs = struct {
	sync.Mutex
	jobs map[string]mobileAgentJobRecord
}{
	jobs: make(map[string]mobileAgentJobRecord),
}

type mobileAgentJobRecord struct {
	JobID        string
	OwnerID      string
	TenantID     string
	Query        string
	DocumentID   string
	DocumentTitle string
	Status       string
	Answer       string
	Message      string
	RequestID    string
	// Auth material to re-issue Hub LLM calls as the viewer (not returned to clients).
	AuthHeader string
	BaseScheme string
	BaseHost   string
	// Chat history for the agent loop.
	Messages []mobileChatMessage
	Context  []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type mobileAgentJobCreateRequest struct {
	Query      string              `json:"query"`
	Context    []string            `json:"context,omitempty"`
	Messages   []mobileChatMessage `json:"messages,omitempty"`
	DocumentID string              `json:"document_id,omitempty"`
}

// MobileAgentJobsHandler creates and lists long-running assistant jobs.
//
//	POST /api/mobile/agent/jobs  — enqueue (returns 202)
//	GET  /api/mobile/agent/jobs  — list mine
//	GET  /api/mobile/agent/jobs/{jobId} — status + answer when ready
func MobileAgentJobsHandler(identity *auth.IdentityService, llmHandlers ...http.Handler) http.HandlerFunc {
	var officialLLM http.Handler
	if len(llmHandlers) > 0 {
		officialLLM = llmHandlers[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
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

		jobID := strings.TrimSpace(r.PathValue("jobId"))
		switch {
		case r.Method == http.MethodGet && jobID != "":
			mobileAgentJobGet(w, ownerID, jobID)
		case r.Method == http.MethodGet:
			mobileAgentJobList(w, ownerID)
		case r.Method == http.MethodPost && jobID == "":
			mobileAgentJobCreate(w, r, principal, officialLLM)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST /api/mobile/agent/jobs")
		}
	}
}

func mobileAgentJobCreate(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, officialLLM http.Handler) {
	var req mobileAgentJobCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "query is required")
		return
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if n := mobileAgentJobActiveCount(ownerID); n >= mobileAgentJobMaxActivePerUser {
		writeError(w, http.StatusTooManyRequests, "JOB_LIMIT", fmt.Sprintf("at most %d active assistant jobs", mobileAgentJobMaxActivePerUser))
		return
	}

	docID := strings.TrimSpace(req.DocumentID)
	docTitle := ""
	if docID != "" {
		draft, ok := mobileLookupOwnedDraft(principal, docID)
		if !ok {
			writeError(w, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "bound document not found or not owned by viewer")
			return
		}
		docTitle = draft.Title
	}

	now := time.Now().UTC()
	job := mobileAgentJobRecord{
		JobID:         fmt.Sprintf("mobagent_%d", now.UnixNano()),
		OwnerID:       ownerID,
		TenantID:      strings.TrimSpace(principal.TenantID),
		Query:         query,
		DocumentID:    docID,
		DocumentTitle: docTitle,
		Status:        mobileAgentJobStatusQueued,
		Message:       "queued",
		AuthHeader:    strings.TrimSpace(r.Header.Get("Authorization")),
		BaseScheme:    r.URL.Scheme,
		BaseHost:      r.Host,
		Messages:      append([]mobileChatMessage(nil), req.Messages...),
		Context:       append([]string(nil), req.Context...),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if job.BaseScheme == "" {
		if r.TLS != nil {
			job.BaseScheme = "https"
		} else {
			job.BaseScheme = "http"
		}
	}
	if job.TenantID == "" {
		job.TenantID = "default"
	}

	mobileAgentJobs.Lock()
	mobileAgentJobs.jobs[job.JobID] = job
	mobileAgentJobs.Unlock()

	// Detach from request context so client disconnect does not cancel work.
	go mobileRunAgentJob(job.JobID, principal, officialLLM)

	writeJSON(w, http.StatusAccepted, mobileAgentJobPayload(job))
}

func mobileAgentJobGet(w http.ResponseWriter, ownerID, jobID string) {
	mobileAgentJobs.Lock()
	job, ok := mobileAgentJobs.jobs[jobID]
	mobileAgentJobs.Unlock()
	if !ok || job.OwnerID != ownerID {
		writeError(w, http.StatusNotFound, "JOB_NOT_FOUND", "assistant job not found")
		return
	}
	writeJSON(w, http.StatusOK, mobileAgentJobPayload(job))
}

func mobileAgentJobList(w http.ResponseWriter, ownerID string) {
	items := mobileCollectAgentJobs(ownerID)
	out := make([]map[string]any, 0, len(items))
	for _, j := range items {
		// Re-fetch full payload for list endpoint consistency.
		mobileAgentJobs.Lock()
		rec, ok := mobileAgentJobs.jobs[j.JobID]
		mobileAgentJobs.Unlock()
		if !ok {
			continue
		}
		out = append(out, mobileAgentJobPayload(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  out,
		"count": len(out),
	})
}

func mobileAgentJobPayload(job mobileAgentJobRecord) map[string]any {
	m := map[string]any{
		"job_id":     job.JobID,
		"kind":       "assistant",
		"query":      job.Query,
		"title":      mobileAgentJobTitle(job),
		"status":     job.Status,
		"message":    job.Message,
		"progress":   mobileJobProgressFromStatus(job.Status),
		"updated_at": job.UpdatedAt.UTC().Format(time.RFC3339),
		"created_at": job.CreatedAt.UTC().Format(time.RFC3339),
		"deep_link":  "/tasks",
	}
	if job.DocumentID != "" {
		m["document_id"] = job.DocumentID
		if job.DocumentTitle != "" {
			m["document_title"] = job.DocumentTitle
		}
	}
	if job.RequestID != "" {
		m["llm_request_id"] = job.RequestID
	}
	if job.Status == mobileAgentJobStatusReady && job.Answer != "" {
		m["answer"] = job.Answer
	}
	if job.Status == mobileAgentJobStatusFailed && job.Message != "" {
		m["error"] = job.Message
	}
	return m
}

func mobileAgentJobTitle(job mobileAgentJobRecord) string {
	q := strings.TrimSpace(job.Query)
	runes := []rune(q)
	if len(runes) > 40 {
		q = string(runes[:40]) + "…"
	}
	if q == "" {
		return "助手长任务"
	}
	return "助手 · " + q
}

func mobileAgentJobActiveCount(ownerID string) int {
	mobileAgentJobs.Lock()
	defer mobileAgentJobs.Unlock()
	n := 0
	for _, j := range mobileAgentJobs.jobs {
		if j.OwnerID == ownerID && mobileJobIsActive(j.Status) {
			n++
		}
	}
	return n
}

func mobileAgentJobUpdate(jobID string, mutate func(*mobileAgentJobRecord)) {
	mobileAgentJobs.Lock()
	job, ok := mobileAgentJobs.jobs[jobID]
	if !ok {
		mobileAgentJobs.Unlock()
		return
	}
	mutate(&job)
	job.UpdatedAt = time.Now().UTC()
	mobileAgentJobs.jobs[jobID] = job
	mobileAgentJobs.Unlock()
	// Realtime + offline pending push for terminal assistant jobs.
	payload := mobileAgentJobPayload(job)
	mobileRealtimeBroadcast(job.TenantID, job.OwnerID, map[string]any{
		"type":    "assistant_job",
		"job_id":  job.JobID,
		"task_id": job.JobID,
		"status":  job.Status,
		"task":    payload,
	})
}

func mobileRunAgentJob(jobID string, principal *auth.ViewerPrincipal, officialLLM http.Handler) {
	mobileAgentJobUpdate(jobID, func(j *mobileAgentJobRecord) {
		j.Status = mobileAgentJobStatusRunning
		j.Message = "running"
	})

	mobileAgentJobs.Lock()
	job, ok := mobileAgentJobs.jobs[jobID]
	mobileAgentJobs.Unlock()
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), mobileAgentJobRunTimeout)
	defer cancel()

	// Synthetic request carries viewer auth for Hub LLM proxy RoundTripper.
	reqURL := &url.URL{Scheme: job.BaseScheme, Host: job.BaseHost, Path: "/api/mobile/agent/jobs"}
	baseReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), nil)
	if err != nil {
		mobileAgentJobUpdate(jobID, func(j *mobileAgentJobRecord) {
			j.Status = mobileAgentJobStatusFailed
			j.Message = "failed to build job request"
		})
		return
	}
	if job.AuthHeader != "" {
		baseReq.Header.Set("Authorization", job.AuthHeader)
	}
	baseReq.Host = job.BaseHost

	chatMessages := mobileBuildLLMMessages(job.Query, nil, job.Messages, job.Context)
	if job.DocumentID != "" {
		if draft, ok := mobileLookupOwnedDraft(principal, job.DocumentID); ok {
			chatMessages = mobileInjectBoundDocument(chatMessages, draft)
		}
	}

	// Prefer web evidence for parity with interactive search (best-effort).
	if results, err := mobileWebSearch(ctx, job.Query, 5); err == nil && len(results) > 0 {
		citations := mobileSearchCitations(results)
		chatMessages = mobileBuildLLMMessages(job.Query, citations, job.Messages, job.Context)
		if job.DocumentID != "" {
			if draft, ok := mobileLookupOwnedDraft(principal, job.DocumentID); ok {
				chatMessages = mobileInjectBoundDocument(chatMessages, draft)
			}
		}
	}

	delegated, useDelegated := mobileThirdPartyLLMAuthorization(ctx, principal.TenantID, principal.UserID)
	hasLLM := useDelegated || officialLLM != nil
	if !hasLLM {
		mobileAgentJobUpdate(jobID, func(j *mobileAgentJobRecord) {
			j.Status = mobileAgentJobStatusFailed
			j.Message = "no LLM backend available for assistant job"
		})
		return
	}

	answer, requestID, runErr := mobileRunAgentLoop(ctx, baseReq, principal, officialLLM, delegated, useDelegated, chatMessages, nil)
	if runErr != nil {
		msg := strings.TrimSpace(runErr.Error())
		if msg == "" {
			msg = "assistant job failed"
		}
		// Clip error for storage.
		if runes := []rune(msg); len(runes) > 500 {
			msg = string(runes[:500]) + "…"
		}
		mobileAgentJobUpdate(jobID, func(j *mobileAgentJobRecord) {
			j.Status = mobileAgentJobStatusFailed
			j.Message = msg
			j.RequestID = requestID
		})
		return
	}

	answer = mobileClipRunes(answer, mobileAgentJobAnswerMaxRunes)
	mobileAgentJobUpdate(jobID, func(j *mobileAgentJobRecord) {
		j.Status = mobileAgentJobStatusReady
		j.Message = "ready"
		j.Answer = answer
		j.RequestID = requestID
	})
}

func mobileCollectAgentJobs(ownerID string) []mobileJobItem {
	mobileAgentJobs.Lock()
	defer mobileAgentJobs.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileAgentJobs.jobs {
		if rec.OwnerID != ownerID {
			continue
		}
		out = append(out, mobileJobItem{
			JobID:     rec.JobID,
			Kind:      "assistant",
			Title:     mobileAgentJobTitle(rec),
			Status:    rec.Status,
			Progress:  mobileJobProgressFromStatus(rec.Status),
			Message:   rec.Message,
			DeepLink:  "/assistant",
			UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
			DraftID:   rec.DocumentID,
		})
	}
	return out
}

// mobileEnqueueAgentJobFromSearch is used when Mobile search is called with async=true.
func mobileEnqueueAgentJobFromSearch(
	r *http.Request,
	principal *auth.ViewerPrincipal,
	officialLLM http.Handler,
	req mobileSearchRequest,
) (mobileAgentJobRecord, error) {
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		return mobileAgentJobRecord{}, fmt.Errorf("owner required")
	}
	if n := mobileAgentJobActiveCount(ownerID); n >= mobileAgentJobMaxActivePerUser {
		return mobileAgentJobRecord{}, fmt.Errorf("job limit")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return mobileAgentJobRecord{}, fmt.Errorf("query required")
	}
	docID := strings.TrimSpace(req.DocumentID)
	docTitle := ""
	if docID != "" {
		draft, ok := mobileLookupOwnedDraft(principal, docID)
		if !ok {
			return mobileAgentJobRecord{}, fmt.Errorf("document not found")
		}
		docTitle = draft.Title
	}
	now := time.Now().UTC()
	job := mobileAgentJobRecord{
		JobID:         fmt.Sprintf("mobagent_%d", now.UnixNano()),
		OwnerID:       ownerID,
		TenantID:      strings.TrimSpace(principal.TenantID),
		Query:         query,
		DocumentID:    docID,
		DocumentTitle: docTitle,
		Status:        mobileAgentJobStatusQueued,
		Message:       "queued",
		AuthHeader:    strings.TrimSpace(r.Header.Get("Authorization")),
		BaseScheme:    r.URL.Scheme,
		BaseHost:      r.Host,
		Messages:      append([]mobileChatMessage(nil), req.Messages...),
		Context:       append([]string(nil), req.Context...),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if job.BaseScheme == "" {
		if r.TLS != nil {
			job.BaseScheme = "https"
		} else {
			job.BaseScheme = "http"
		}
	}
	if job.TenantID == "" {
		job.TenantID = "default"
	}
	mobileAgentJobs.Lock()
	mobileAgentJobs.jobs[job.JobID] = job
	mobileAgentJobs.Unlock()
	go mobileRunAgentJob(job.JobID, principal, officialLLM)
	return job, nil
}
