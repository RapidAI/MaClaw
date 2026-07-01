package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var mobileWebSearch func(context.Context, string, int) ([]websearch.SearchResult, error) = websearch.SearchCtx

var mobileDocuments = struct {
	sync.Mutex
	drafts  map[string]mobileDocumentDraftRecord
	exports map[string]mobileDocumentExportRecord
	uploads map[string]mobileDocumentUploadRecord
}{
	drafts:  make(map[string]mobileDocumentDraftRecord),
	exports: make(map[string]mobileDocumentExportRecord),
	uploads: make(map[string]mobileDocumentUploadRecord),
}

var mobileDigitalEmployeeTasks = struct {
	sync.Mutex
	tasks map[string]mobileDigitalEmployeeTaskRecord
}{
	tasks: make(map[string]mobileDigitalEmployeeTaskRecord),
}

type mobileDocumentDraftRecord struct {
	ID        string
	OwnerID   string
	Title     string
	Template  string
	Markdown  string
	UpdatedAt time.Time
}

type mobileDocumentExportRecord struct {
	JobID     string
	DraftID   string
	OwnerID   string
	Format    string
	Status    string
	CreatedAt time.Time
}

type mobileDocumentUploadRecord struct {
	TaskID     string
	OwnerID    string
	Filename   string
	Status     string
	DraftID    string
	Message    string
	UploadedAt time.Time
	UpdatedAt  time.Time
}

type mobileDigitalEmployeeTaskRecord struct {
	TaskID     string
	EmployeeID string
	OwnerID    string
	Prompt     string
	Status     string
	Result     string
	ClaimedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// MobileBootstrapHandler returns the small, cheap payload the mobile app needs
// immediately after restoring a viewer token. Expensive service details stay on
// their existing dedicated endpoints.
func MobileBootstrapHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"user_id":   principal.UserID,
				"email":     principal.Email,
				"tenant_id": principal.TenantID,
			},
			"features": map[string]any{
				"search":             true,
				"documents":          true,
				"local_ssh":          true,
				"digital_employees":  true,
				"push_notifications": false,
			},
			"services": map[string]any{
				"hub_status":             "online",
				"llm_status_path":        "/api/llm/service/status",
				"models_path":            "/api/llm/v1/models",
				"search_path":            "/api/mobile/search",
				"documents_path":         "/api/mobile/documents",
				"digital_employees_path": "/api/mobile/digital-employees",
			},
			"limits": map[string]any{
				"max_upload_bytes": 25 * 1024 * 1024,
				"max_export_jobs":  3,
			},
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func mobileDocumentDraftPayload(record mobileDocumentDraftRecord) map[string]any {
	return map[string]any{
		"id":         record.ID,
		"title":      record.Title,
		"template":   record.Template,
		"markdown":   record.Markdown,
		"updated_at": record.UpdatedAt.Format(time.RFC3339),
		"owner_id":   record.OwnerID,
	}
}

func mobileDocumentExportPayload(record mobileDocumentExportRecord) map[string]any {
	downloadURL := ""
	if record.Status == "ready" {
		downloadURL = "/api/mobile/documents/export/" + record.JobID + "/download"
	}
	return map[string]any{
		"job_id":       record.JobID,
		"draft_id":     record.DraftID,
		"format":       record.Format,
		"status":       record.Status,
		"download_url": downloadURL,
		"created_at":   record.CreatedAt.Format(time.RFC3339),
	}
}

func mobileDocumentUploadPayload(record mobileDocumentUploadRecord) map[string]any {
	payload := map[string]any{
		"task_id":     record.TaskID,
		"filename":    record.Filename,
		"status":      record.Status,
		"draft_id":    record.DraftID,
		"message":     record.Message,
		"uploaded_at": record.UploadedAt.Format(time.RFC3339),
		"updated_at":  record.UpdatedAt.Format(time.RFC3339),
		"owner_id":    record.OwnerID,
	}
	if record.DraftID != "" {
		if draft, ok := mobileDocuments.drafts[record.DraftID]; ok {
			payload["draft"] = mobileDocumentDraftPayload(draft)
		}
	}
	return payload
}

type mobileSearchRequest struct {
	Query   string   `json:"query"`
	Context []string `json:"context,omitempty"`
}

// MobileSearchHandler validates the mobile search request and returns a stable
// response shape. The actual retrieval/LLM implementation can be swapped in
// behind this contract without changing the mobile client.
func MobileSearchHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req mobileSearchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "query is required")
			return
		}

		results, err := mobileWebSearch(r.Context(), query, 5)
		if err != nil {
			writeError(w, http.StatusBadGateway, "SEARCH_FAILED", "mobile search failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"answer":    mobileSearchAnswer(query, results),
			"citations": mobileSearchCitations(results),
			"query":     query,
			"tenant_id": principal.TenantID,
			"user_id":   principal.UserID,
			"status":    "ready",
		})
	}
}

func mobileSearchAnswer(query string, results []websearch.SearchResult) string {
	query = strings.TrimSpace(query)
	if len(results) == 0 {
		return "未找到可引用的搜索结果。请换一个更具体的问题再试。"
	}
	var b strings.Builder
	b.WriteString("已为你检索：")
	b.WriteString(query)
	b.WriteString("\n\n")
	b.WriteString("可先参考这些来源：")
	for i, result := range results {
		if i >= 3 {
			break
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = strings.TrimSpace(result.URL)
		}
		if title == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%d. %s", i+1, title))
		snippet := strings.TrimSpace(result.Snippet)
		if snippet != "" {
			b.WriteString("：")
			b.WriteString(snippet)
		}
	}
	return b.String()
}
func mobileSearchCitations(results []websearch.SearchResult) []map[string]string {
	citations := make([]map[string]string, 0, len(results))
	for _, result := range results {
		url := strings.TrimSpace(result.URL)
		if url == "" {
			continue
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = url
		}
		citations = append(citations, map[string]string{
			"title":   title,
			"url":     url,
			"snippet": strings.TrimSpace(result.Snippet),
		})
	}
	return citations
}

type mobileDocumentDraftRequest struct {
	Title    string `json:"title"`
	Template string `json:"template"`
	Content  string `json:"content,omitempty"`
}

type mobileDocumentDraftUpdateRequest struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

type mobileDocumentProcessRequest struct {
	Action string `json:"action"`
}

type mobileDocumentExportRequest struct {
	DraftID string `json:"draft_id"`
	Format  string `json:"format"`
}

type mobileSSHAnalyzeRequest struct {
	Output string `json:"output"`
}

type mobileDigitalEmployeeTaskRequest struct {
	Prompt string `json:"prompt"`
}

type mobileDigitalEmployeeTaskUpdateRequest struct {
	Status string `json:"status"`
	Result string `json:"result"`
}

func mobileDigitalEmployeeTaskPayload(record mobileDigitalEmployeeTaskRecord) map[string]any {
	return map[string]any{
		"task_id":     record.TaskID,
		"employee_id": record.EmployeeID,
		"prompt":      record.Prompt,
		"status":      record.Status,
		"result":      record.Result,
		"claimed_by":  record.ClaimedBy,
		"created_at":  record.CreatedAt.Format(time.RFC3339),
		"updated_at":  record.UpdatedAt.Format(time.RFC3339),
	}
}

type mobileDigitalEmployeeWorkerPrincipal struct {
	TenantID  string
	UserID    string
	MachineID string
}

func authenticateMobileDigitalEmployeeWorker(r *http.Request, identity *auth.IdentityService) (mobileDigitalEmployeeWorkerPrincipal, error) {
	if identity == nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, auth.ErrInvalidUserCredentials
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.Header.Get("X-MaClaw-Machine-ID"))
	}
	if machineID != "" {
		if token := extractBearerToken(r); token != "" {
			principal, err := identity.AuthenticateMachine(r.Context(), machineID, token)
			if err == nil && principal != nil {
				return mobileDigitalEmployeeWorkerPrincipal{
					TenantID:  principal.TenantID,
					UserID:    principal.UserID,
					MachineID: principal.MachineID,
				}, nil
			}
		}
	}
	viewer, err := authenticateViewerRequest(r, identity)
	if err != nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, err
	}
	return mobileDigitalEmployeeWorkerPrincipal{
		TenantID: viewer.TenantID,
		UserID:   viewer.UserID,
	}, nil
}

// MobileDocumentDraftHandler creates an emergency document draft contract. It
// returns an ID and normalized markdown so the mobile app can continue editing
// offline while the richer document pipeline is implemented.
func MobileDocumentDraftHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req mobileDocumentDraftRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		template := strings.TrimSpace(req.Template)
		if template == "" {
			template = "report"
		}
		content := strings.TrimSpace(req.Content)
		if content == "" {
			content = "请在这里补充正文。"
		}
		now := time.Now().UTC()
		draftID := fmt.Sprintf("mobdoc_%d", now.UnixNano())
		record := mobileDocumentDraftRecord{
			ID:        draftID,
			OwnerID:   principal.UserID,
			Title:     title,
			Template:  template,
			Markdown:  "# " + title + "\n\n" + content + "\n",
			UpdatedAt: now,
		}
		mobileDocuments.Lock()
		mobileDocuments.drafts[draftID] = record
		mobileDocuments.Unlock()

		writeJSON(w, http.StatusCreated, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_created",
		})
	}
}

// MobileDocumentDraftUpdateHandler persists lightweight title/body edits made
// on the mobile device before export or sharing.
func MobileDocumentDraftUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentDraftUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		markdown := strings.TrimSpace(req.Markdown)
		if markdown == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "markdown is required")
			return
		}
		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == principal.UserID {
			record.Title = title
			record.Markdown = markdown
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_updated",
		})
	}
}

// MobileDocumentProcessHandler applies lightweight emergency document actions.
// It is deterministic today and keeps the same API shape for a richer LLM-backed
// processor later.
func MobileDocumentProcessHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentProcessRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if !mobileDocumentProcessActionAllowed(action) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "action must be one of summarize, translate, rewrite, expand, polish, format")
			return
		}

		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == principal.UserID {
			record.Markdown = mobileProcessDocumentMarkdown(action, record.Markdown)
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "processed",
			"action": action,
		})
	}
}

func mobileDocumentProcessActionAllowed(action string) bool {
	switch action {
	case "summarize", "translate", "rewrite", "expand", "polish", "format":
		return true
	default:
		return false
	}
}

func mobileProcessDocumentMarkdown(action, markdown string) string {
	title, bodyLines := mobileDocumentTitleAndBody(markdown)
	switch action {
	case "summarize":
		return mobileProcessSummarize(title, bodyLines)
	case "translate":
		return mobileProcessTranslate(title, bodyLines)
	case "rewrite":
		return mobileProcessRewrite(title, bodyLines)
	case "expand":
		return mobileProcessExpand(title, bodyLines)
	case "polish":
		return mobileProcessPolish(title, bodyLines)
	case "format":
		return mobileProcessFormat(title, bodyLines)
	default:
		return markdown
	}
}

func mobileDocumentTitleAndBody(markdown string) (string, []string) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	title := "文档"
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && title == "文档" {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if trimmed != "" {
			body = append(body, trimmed)
		}
	}
	return title, body
}

func mobileProcessSummarize(title string, body []string) string {
	points := mobileFirstNonEmptyLines(body, 5)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 摘要\n\n")
	if len(points) == 0 {
		b.WriteString("- 暂无可摘要内容。\n")
		return b.String()
	}
	for _, point := range points {
		b.WriteString("- ")
		b.WriteString(mobileTrimRunes(point, 120))
		b.WriteString("\n")
	}
	return b.String()
}

func mobileProcessTranslate(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 翻译草稿\n\n")
	b.WriteString("> 当前为移动端应急翻译草稿。联网 LLM 可用后会替换为完整翻译。\n\n")
	for _, line := range body {
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessRewrite(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 改写稿\n\n")
	for _, line := range body {
		b.WriteString("- ")
		b.WriteString(strings.Trim(strings.TrimPrefix(line, "-"), " 。."))
		b.WriteString("。\n")
	}
	return b.String()
}

func mobileProcessExpand(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 扩写稿\n\n")
	for _, line := range body {
		b.WriteString("## ")
		b.WriteString(mobileTrimRunes(strings.Trim(strings.TrimPrefix(line, "-"), " 。."), 36))
		b.WriteString("\n\n")
		b.WriteString(line)
		b.WriteString("\n\n待补充：背景、影响、处理建议和下一步行动。\n\n")
	}
	return b.String()
}

func mobileProcessPolish(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 润色稿\n\n")
	for _, line := range body {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "。") && !strings.HasSuffix(line, ".") {
			b.WriteString("。")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessFormat(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, line := range body {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "```") {
			b.WriteString(line)
		} else {
			b.WriteString("- ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func mobileFirstNonEmptyLines(lines []string, limit int) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mobileTrimRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// MobileDocumentExportHandler validates export requests and returns an export
// job contract. Markdown, lightweight PDF, and lightweight DOCX are generated
// in-process for emergency mobile use.
func MobileDocumentExportHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req mobileDocumentExportRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		draftID := strings.TrimSpace(req.DraftID)
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft_id is required")
			return
		}
		mobileDocuments.Lock()
		draft, ok := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !ok || draft.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		format := strings.ToLower(strings.TrimSpace(req.Format))
		switch format {
		case "pdf", "word", "markdown":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "format must be one of pdf, word, markdown")
			return
		}

		now := time.Now().UTC()
		job := mobileDocumentExportRecord{
			JobID:     fmt.Sprintf("mobexp_%d", now.UnixNano()),
			DraftID:   draftID,
			OwnerID:   principal.UserID,
			Format:    format,
			Status:    "queued",
			CreatedAt: now,
		}
		if format == "markdown" || format == "pdf" || format == "word" {
			job.Status = "ready"
		}
		mobileDocuments.Lock()
		mobileDocuments.exports[job.JobID] = job
		mobileDocuments.Unlock()
		writeJSON(w, http.StatusAccepted, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportStatusHandler returns the current export job status.
func MobileDocumentExportStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportDownloadHandler downloads finished lightweight exports.
// Markdown, emergency PDF, and emergency DOCX are generated in-process.
func MobileDocumentExportDownloadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		draft := mobileDocuments.drafts[job.DraftID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		if job.Status != "ready" || (job.Format != "markdown" && job.Format != "pdf" && job.Format != "word") {
			writeError(w, http.StatusConflict, "EXPORT_NOT_READY", "export job is not ready")
			return
		}
		if job.Format == "pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".pdf\"")
			_, _ = w.Write(mobileRenderDraftPDF(draft))
			return
		}
		if job.Format == "word" {
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".docx\"")
			_, _ = w.Write(mobileRenderDraftDOCX(draft))
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".md\"")
		_, _ = w.Write([]byte(draft.Markdown))
	}
}

func mobileRenderDraftPDF(draft mobileDocumentDraftRecord) []byte {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	const linesPerPage = 34
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	if pageCount == 0 {
		pageCount = 1
	}
	fontID := 3 + pageCount*2
	objects := make([]string, fontID+1)
	objects[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageID := 3 + page*2
		contentID := pageID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		contents := mobilePDFPageContent(lines[start:end])
		objects[pageID] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, contentID)
		objects[contentID] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contents), contents)
	}
	objects[2] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
	objects[fontID] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for id := 1; id < len(objects); id++ {
		offsets[id] = out.Len()
		out.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", id, objects[id]))
	}
	xrefOffset := out.Len()
	out.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)))
	out.WriteString("0000000000 65535 f \n")
	for id := 1; id < len(objects); id++ {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[id]))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects), xrefOffset))
	return []byte(out.String())
}

func mobilePDFLines(title, markdown string) []string {
	var out []string
	title = strings.TrimSpace(title)
	if title != "" {
		out = append(out, title, "")
	}
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, raw := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, mobileWrapPDFLine(line, 48)...)
	}
	return out
}

func mobileWrapPDFLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var out []string
	for len(runes) > 0 {
		n := width
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func mobilePDFPageContent(lines []string) string {
	var b strings.Builder
	b.WriteString("BT\n/F1 12 Tf\n50 790 Td\n")
	for idx, line := range lines {
		if idx > 0 {
			b.WriteString("0 -20 Td\n")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString("<")
		b.WriteString(mobilePDFUTF16Hex(line))
		b.WriteString("> Tj\n")
	}
	b.WriteString("ET")
	return b.String()
}

func mobilePDFUTF16Hex(text string) string {
	units := utf16.Encode([]rune(text))
	var b strings.Builder
	b.WriteString("FEFF")
	for _, unit := range units {
		b.WriteString(fmt.Sprintf("%04X", unit))
	}
	return b.String()
}

func mobileRenderDraftDOCX(draft mobileDocumentDraftRecord) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mobileDOCXWriteFile(zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	mobileDOCXWriteFile(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	mobileDOCXWriteFile(zw, "word/document.xml", mobileDOCXDocumentXML(draft))
	_ = zw.Close()
	return buf.Bytes()
}

func mobileDOCXWriteFile(zw *zip.Writer, name, body string) {
	w, err := zw.Create(name)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, body)
}

func mobileDOCXDocumentXML(draft mobileDocumentDraftRecord) string {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			b.WriteString(`<w:p/>`)
			continue
		}
		b.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		b.WriteString(mobileXMLEscape(line))
		b.WriteString(`</w:t></w:r></w:p>`)
	}
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func mobileXMLEscape(text string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}

const mobileDocumentUploadMaxBytes = 25 << 20

func mobileUploadedFileIsImmediateDraft(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".md", ".markdown", ".log", ".csv", ".json", ".docx", ".xlsx", ".pdf":
		return true
	default:
		return false
	}
}

func mobileUploadedFileIsImage(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

func mobileDraftMarkdownFromUpload(filename string, raw []byte) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx":
		return mobileDraftMarkdownFromDOCX(filename, raw)
	case ".xlsx":
		return mobileDraftMarkdownFromXLSX(filename, raw)
	case ".pdf":
		return mobileDraftMarkdownFromPDF(filename, raw)
	}
	if !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = "_Imported file was empty._"
	}
	if ext == ".md" || ext == ".markdown" {
		return text + "\n", true
	}
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		title = "Imported document"
	}
	switch ext {
	case ".log":
		return "# " + title + "\n\n```text\n" + text + "\n```\n", true
	case ".csv":
		return "# " + title + "\n\n```csv\n" + text + "\n```\n", true
	case ".json":
		return "# " + title + "\n\n```json\n" + text + "\n```\n", true
	default:
		return "# " + title + "\n\n" + text + "\n", true
	}
}

func mobileDraftMarkdownFromDOCX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	var documentXML []byte
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", false
		}
		documentXML, err = io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return "", false
		}
		break
	}
	if len(documentXML) == 0 {
		return "", false
	}
	paragraphs := mobileDOCXParagraphs(documentXML)
	if len(paragraphs) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, paragraph := range paragraphs {
		b.WriteString(paragraph)
		b.WriteString("\n\n")
	}
	return b.String(), true
}

func mobileDOCXParagraphs(raw []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var paragraphs []string
	var current strings.Builder
	inParagraph := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "p":
				inParagraph = true
				current.Reset()
			case "t":
				if inParagraph {
					var text string
					if err := decoder.DecodeElement(&text, &item); err == nil {
						current.WriteString(text)
					}
				}
			case "tab":
				if inParagraph {
					current.WriteString("\t")
				}
			case "br":
				if inParagraph {
					current.WriteString("\n")
				}
			}
		case xml.EndElement:
			if item.Name.Local == "p" && inParagraph {
				if text := strings.TrimSpace(current.String()); text != "" {
					paragraphs = append(paragraphs, text)
				}
				inParagraph = false
				current.Reset()
			}
		}
	}
	return paragraphs
}

func mobileDraftMarkdownFromXLSX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	sharedStrings := mobileXLSXSharedStrings(zr)
	sheets := mobileXLSXSheets(zr, sharedStrings)
	if len(sheets) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for index, rows := range sheets {
		if len(rows) == 0 {
			continue
		}
		if index > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Sheet ")
		b.WriteString(fmt.Sprintf("%d", index+1))
		b.WriteString("\n\n")
		for _, row := range rows {
			b.WriteString("- ")
			b.WriteString(strings.Join(row, " | "))
			b.WriteString("\n")
		}
	}
	return b.String(), true
}

func mobileDraftMarkdownFromPDF(filename string, raw []byte) (string, bool) {
	text := strings.TrimSpace(mobilePDFExtractText(raw))
	if text == "" {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(text)
	b.WriteString("\n")
	return b.String(), true
}

func mobileDraftMarkdownFromImage(filename string, raw []byte) string {
	title := mobileUploadTitle(filename)
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if format == "jpg" {
		format = "jpeg"
	}
	width, height := mobileImageDimensions(format, raw)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("图片已导入，等待 OCR/视觉模型识别。\n\n")
	b.WriteString("- 文件名：")
	b.WriteString(filepath.Base(filename))
	b.WriteString("\n")
	b.WriteString("- 格式：")
	b.WriteString(format)
	b.WriteString("\n")
	b.WriteString("- 大小：")
	b.WriteString(fmt.Sprintf("%d bytes", len(raw)))
	b.WriteString("\n")
	if width > 0 && height > 0 {
		b.WriteString("- 分辨率：")
		b.WriteString(fmt.Sprintf("%d x %d", width, height))
		b.WriteString("\n")
	}
	b.WriteString("\n## 待识别内容\n\n")
	b.WriteString("_OCR 完成后会把识别文本更新到这里。_\n")
	return b.String()
}

func mobileImageDimensions(format string, raw []byte) (int, int) {
	switch format {
	case "png":
		if len(raw) >= 24 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			width := int(raw[16])<<24 | int(raw[17])<<16 | int(raw[18])<<8 | int(raw[19])
			height := int(raw[20])<<24 | int(raw[21])<<16 | int(raw[22])<<8 | int(raw[23])
			return width, height
		}
	case "jpg", "jpeg":
		for i := 2; i+9 < len(raw); {
			if raw[i] != 0xFF {
				i++
				continue
			}
			marker := raw[i+1]
			if marker == 0xC0 || marker == 0xC2 {
				height := int(raw[i+5])<<8 | int(raw[i+6])
				width := int(raw[i+7])<<8 | int(raw[i+8])
				return width, height
			}
			if i+3 >= len(raw) {
				break
			}
			size := int(raw[i+2])<<8 | int(raw[i+3])
			if size < 2 {
				break
			}
			i += 2 + size
		}
	}
	return 0, 0
}

func mobilePDFExtractText(raw []byte) string {
	text := string(raw)
	var out []string
	out = append(out, mobilePDFExtractHexStrings(text)...)
	out = append(out, mobilePDFExtractLiteralStrings(text)...)
	return strings.Join(mobileCompactTextLines(out), "\n")
}

func mobilePDFExtractHexStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '<' || (i+1 < len(text) && text[i+1] == '<') {
			continue
		}
		end := strings.IndexByte(text[i+1:], '>')
		if end < 0 {
			break
		}
		body := text[i+1 : i+1+end]
		if decoded := mobileDecodePDFHexString(body); decoded != "" {
			out = append(out, decoded)
		}
		i = i + end + 1
	}
	return out
}

func mobileDecodePDFHexString(value string) string {
	var hexDigits []byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexDigits = append(hexDigits, c)
		}
	}
	if len(hexDigits) < 2 {
		return ""
	}
	if len(hexDigits)%2 == 1 {
		hexDigits = append(hexDigits, '0')
	}
	data := make([]byte, 0, len(hexDigits)/2)
	for i := 0; i+1 < len(hexDigits); i += 2 {
		hi, okHi := mobileHexNibble(hexDigits[i])
		lo, okLo := mobileHexNibble(hexDigits[i+1])
		if !okHi || !okLo {
			return ""
		}
		data = append(data, byte(hi<<4|lo))
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		var runes []rune
		for i := 2; i+1 < len(data); i += 2 {
			runes = append(runes, rune(data[i])<<8|rune(data[i+1]))
		}
		return strings.TrimSpace(string(runes))
	}
	if utf8.Valid(data) {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func mobileHexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	default:
		return 0, false
	}
}

func mobilePDFExtractLiteralStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '(' {
			continue
		}
		decoded, next, ok := mobileReadPDFLiteralString(text, i)
		if ok && strings.TrimSpace(decoded) != "" {
			out = append(out, decoded)
		}
		i = next
	}
	return out
}

func mobileReadPDFLiteralString(text string, start int) (string, int, bool) {
	var b strings.Builder
	escaped := false
	depth := 0
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			default:
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			if depth == 0 {
				return strings.TrimSpace(b.String()), i, true
			}
			depth--
			b.WriteByte(c)
		default:
			if c >= 0x20 || c == '\n' || c == '\r' || c == '\t' {
				b.WriteByte(c)
			}
		}
	}
	return "", start, false
}

func mobileCompactTextLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 1000 || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

func mobileXLSXSharedStrings(zr *zip.Reader) []string {
	raw := mobileZipReadFile(zr, "xl/sharedStrings.xml")
	if len(raw) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var values []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		values = append(values, mobileXLSXInlineText(decoder, start.Name.Local))
	}
	return values
}

func mobileXLSXSheets(zr *zip.Reader, sharedStrings []string) [][][]string {
	var sheets [][][]string
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "xl/worksheets/sheet") || !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		raw := mobileZipReadFile(zr, file.Name)
		rows := mobileXLSXRows(raw, sharedStrings)
		if len(rows) > 0 {
			sheets = append(sheets, rows)
		}
	}
	return sheets
}

func mobileXLSXRows(raw []byte, sharedStrings []string) [][]string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var rows [][]string
	var current []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "row":
				current = nil
			case "c":
				current = append(current, mobileXLSXCellText(decoder, item, sharedStrings))
			}
		case xml.EndElement:
			if item.Name.Local == "row" {
				row := mobileTrimEmptyTrailingCells(current)
				if len(row) > 0 {
					rows = append(rows, row)
				}
				current = nil
			}
		}
	}
	return rows
}

func mobileXLSXCellText(decoder *xml.Decoder, cell xml.StartElement, sharedStrings []string) string {
	cellType := ""
	for _, attr := range cell.Attr {
		if attr.Name.Local == "t" {
			cellType = attr.Value
			break
		}
	}
	var value string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "v":
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					value = strings.TrimSpace(text)
				}
			case "is":
				value = strings.TrimSpace(mobileXLSXInlineText(decoder, item.Name.Local))
			}
		case xml.EndElement:
			if item.Name.Local == "c" {
				if cellType == "s" {
					if index, ok := mobileParseInt(value); ok && index >= 0 && index < len(sharedStrings) {
						return sharedStrings[index]
					}
				}
				return value
			}
		}
	}
	return value
}

func mobileXLSXInlineText(decoder *xml.Decoder, endElement string) string {
	var b strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "t" {
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					b.WriteString(text)
				}
			}
		case xml.EndElement:
			if item.Name.Local == endElement {
				return strings.TrimSpace(b.String())
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func mobileTrimEmptyTrailingCells(cells []string) []string {
	end := len(cells)
	for end > 0 && strings.TrimSpace(cells[end-1]) == "" {
		end--
	}
	return cells[:end]
}

func mobileZipReadFile(zr *zip.Reader, name string) []byte {
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return nil
		}
		return raw
	}
	return nil
}

func mobileUploadTitle(filename string) string {
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		return "Imported document"
	}
	return title
}

func mobileParseInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// MobileDocumentUploadHandler validates emergency document upload metadata and
// returns a parse task contract. Lightweight text-like files are converted into
// drafts immediately; heavier Office/PDF/image parsing remains queued for the
// document pipeline.
func MobileDocumentUploadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if err := r.ParseMultipartForm(mobileDocumentUploadMaxBytes); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "file upload must be multipart/form-data")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "file is required")
			return
		}
		defer file.Close()
		name := strings.TrimSpace(header.Filename)
		if name == "" {
			name = "upload"
		}
		body, err := io.ReadAll(io.LimitReader(file, mobileDocumentUploadMaxBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "UPLOAD_READ_FAILED", "failed to read uploaded file")
			return
		}
		if len(body) > mobileDocumentUploadMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "file exceeds mobile upload limit")
			return
		}
		now := time.Now().UTC()
		record := mobileDocumentUploadRecord{
			TaskID:     fmt.Sprintf("mobparse_%d", now.UnixNano()),
			OwnerID:    principal.UserID,
			Filename:   name,
			Status:     "queued",
			Message:    "Waiting for document parsing pipeline.",
			UploadedAt: now,
			UpdatedAt:  now,
		}
		if mobileUploadedFileIsImmediateDraft(name) {
			if markdown, ok := mobileDraftMarkdownFromUpload(name, body); ok {
				draft := mobileDocumentDraftRecord{
					ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
					OwnerID:   principal.UserID,
					Title:     strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))),
					Template:  "report",
					Markdown:  markdown,
					UpdatedAt: now,
				}
				if draft.Title == "" {
					draft.Title = name
				}
				record.Status = "ready"
				record.DraftID = draft.ID
				record.Message = "File parsed into a mobile draft."
				mobileDocuments.Lock()
				mobileDocuments.drafts[draft.ID] = draft
				mobileDocuments.uploads[record.TaskID] = record
				payload := mobileDocumentUploadPayload(record)
				mobileDocuments.Unlock()
				writeJSON(w, http.StatusAccepted, payload)
				return
			}
			record.Message = "Uploaded file could not be parsed immediately; waiting for document parsing pipeline."
		}
		if mobileUploadedFileIsImage(name) {
			draft := mobileDocumentDraftRecord{
				ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
				OwnerID:   principal.UserID,
				Title:     mobileUploadTitle(name),
				Template:  "report",
				Markdown:  mobileDraftMarkdownFromImage(name, body),
				UpdatedAt: now,
			}
			record.Status = "needs_ocr"
			record.DraftID = draft.ID
			record.Message = "Image imported into a mobile draft; OCR is pending."
			mobileDocuments.Lock()
			mobileDocuments.drafts[draft.ID] = draft
			mobileDocuments.uploads[record.TaskID] = record
			payload := mobileDocumentUploadPayload(record)
			mobileDocuments.Unlock()
			writeJSON(w, http.StatusAccepted, payload)
			return
		}
		mobileDocuments.Lock()
		mobileDocuments.uploads[record.TaskID] = record
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		writeJSON(w, http.StatusAccepted, payload)
	}
}

// MobileDocumentUploadStatusHandler returns the current upload parse task.
func MobileDocumentUploadStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDocuments.Lock()
		record, ok := mobileDocuments.uploads[taskID]
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload task not found")
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func mobileSSHAnalysisPayload(output string) map[string]any {
	lower := strings.ToLower(output)
	summary := "已读取终端输出。"
	recommendation := "先确认当前目录、服务名和影响范围，再手动执行排查命令。高风险命令不要直接复制执行。"
	commandDraft := ""
	switch {
	case strings.Contains(lower, "permission denied"):
		summary = "输出显示权限被拒绝。"
		recommendation = "检查当前用户、目标文件权限、sudo 策略和 SSH 密钥是否匹配。"
		commandDraft = "id && ls -la"
	case strings.Contains(lower, "no space left") || strings.Contains(lower, "disk full"):
		summary = "输出显示磁盘空间不足。"
		recommendation = "先查看磁盘和大目录占用，再决定是否清理日志或扩容。"
		commandDraft = "df -h && du -sh ./* 2>/dev/null | sort -h"
	case strings.Contains(lower, "connection refused"):
		summary = "输出显示连接被拒绝。"
		recommendation = "检查服务是否监听、端口是否正确，以及防火墙或安全组规则。"
		commandDraft = "ss -lntp"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		summary = "输出显示连接超时。"
		recommendation = "优先检查网络连通性、DNS、路由、防火墙和上游服务状态。"
		commandDraft = "ping -c 4 <host> && traceroute <host>"
	case strings.Contains(lower, "nginx"):
		summary = "输出包含 nginx 相关信息。"
		recommendation = "先校验配置，再查看服务状态和最近错误日志。"
		commandDraft = "nginx -t && systemctl status nginx --no-pager"
	case strings.Contains(lower, "failed") || strings.Contains(lower, "error"):
		summary = "输出包含失败或错误信息。"
		recommendation = "先定位具体服务和最近日志，避免直接重启或删除数据。"
		commandDraft = "systemctl --failed && journalctl -xe --no-pager | tail -n 80"
	}
	return map[string]any{
		"summary":        summary,
		"recommendation": recommendation,
		"command_draft":  commandDraft,
		"status":         "ready",
	}
}

// MobileSSHAnalyzeHandler gives the mobile SSH surface a lightweight analysis
// without allowing the server or AI to execute commands.
func MobileSSHAnalyzeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		if _, err := authenticateViewerRequest(r, identity); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req mobileSSHAnalyzeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		output := strings.TrimSpace(req.Output)
		if output == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "output is required")
			return
		}
		writeJSON(w, http.StatusOK, mobileSSHAnalysisPayload(output))
	}
}

// MobileDigitalEmployeeTaskHandler creates a mobile-origin task request for a
// remote digital employee. The first version is intentionally asynchronous and
// permission-aware: mobile submits intent, the remote worker/owner confirms and
// executes according to its own policy.
func MobileDigitalEmployeeTaskHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		employeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if employeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}
		var req mobileDigitalEmployeeTaskRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		prompt := strings.TrimSpace(req.Prompt)
		if prompt == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "prompt is required")
			return
		}
		now := time.Now().UTC()
		record := mobileDigitalEmployeeTaskRecord{
			TaskID:     fmt.Sprintf("mobve_%d", now.UnixNano()),
			EmployeeID: employeeID,
			OwnerID:    principal.UserID,
			Prompt:     prompt,
			Status:     "queued",
			Result:     "任务已提交，等待远程数字员工或授权策略处理。",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks[record.TaskID] = record
		mobileDigitalEmployeeTasks.Unlock()
		writeJSON(w, http.StatusAccepted, mobileDigitalEmployeeTaskPayload(record))
	}
}

// MobileDigitalEmployeeTaskClaimHandler lets an authorized remote worker claim
// one queued mobile-origin task for a digital employee. This closes the mobile
// to remote-capability loop without letting the phone execute commands itself.
func MobileDigitalEmployeeTaskClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		employeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if employeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}

		now := time.Now().UTC()
		claimedBy := principal.MachineID
		if claimedBy == "" {
			claimedBy = principal.UserID
		}
		var claimed mobileDigitalEmployeeTaskRecord
		mobileDigitalEmployeeTasks.Lock()
		for taskID, record := range mobileDigitalEmployeeTasks.tasks {
			if record.EmployeeID != employeeID || record.OwnerID != principal.UserID || record.Status != "queued" {
				continue
			}
			record.Status = "in_progress"
			record.Result = "远程数字员工已领取任务，正在处理。"
			record.ClaimedBy = claimedBy
			record.UpdatedAt = now
			mobileDigitalEmployeeTasks.tasks[taskID] = record
			claimed = record
			break
		}
		mobileDigitalEmployeeTasks.Unlock()
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"task":   nil,
				"status": "empty",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task":   mobileDigitalEmployeeTaskPayload(claimed),
			"status": "claimed",
		})
	}
}

// MobileDigitalEmployeeTaskUpdateHandler lets the remote worker report task
// progress and final results back to the mobile user.
func MobileDigitalEmployeeTaskUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileDigitalEmployeeTaskUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		switch status {
		case "in_progress", "done", "failed":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status must be one of in_progress, done, failed")
			return
		}
		result := strings.TrimSpace(req.Result)
		if (status == "done" || status == "failed") && result == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "result is required for final status")
			return
		}

		now := time.Now().UTC()
		workerID := principal.MachineID
		if workerID == "" {
			workerID = principal.UserID
		}
		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		if ok && record.OwnerID == principal.UserID {
			if record.ClaimedBy != "" && record.ClaimedBy != workerID {
				ok = false
			} else {
				record.Status = status
				if result != "" {
					record.Result = result
				}
				record.ClaimedBy = workerID
				record.UpdatedAt = now
				mobileDigitalEmployeeTasks.tasks[taskID] = record
			}
		}
		mobileDigitalEmployeeTasks.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDigitalEmployeeTaskPayload(record))
	}
}

// MobileDigitalEmployeeTaskStatusHandler returns a mobile-origin digital
// employee task. Full execution is delegated to the remote worker side.
func MobileDigitalEmployeeTaskStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		mobileDigitalEmployeeTasks.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDigitalEmployeeTaskPayload(record))
	}
}

// MobileDigitalEmployeesHandler lists digital employees a mobile viewer may use
// as remote capability entry points. It intentionally uses viewer auth instead
// of the desktop machine token required by /api/ve/discoverable.
func MobileDigitalEmployeesHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), tenantSystem)
		if !veAuthorizationActive(authz) {
			writeJSON(w, http.StatusOK, map[string]any{
				"employees":     []digitalEmployeeEntry{},
				"authorization": authz,
			})
			return
		}

		baseSystem := globalSystemSettings(system)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		registry := loadVERegistry(r.Context(), tenantSystem)
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
		}

		employees := make([]digitalEmployeeEntry, 0, len(registry.Employees))
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive {
				continue
			}
			if !veVisibleToRequester(entry, nil, false) {
				continue
			}
			if !veAccessAllowed(entry, principal.UserID) {
				continue
			}
			entry = applyVEDiscoverablePresence(r.Context(), entry, nil, runtimePresence)
			employees = append(employees, entry)
		}
		sort.SliceStable(employees, func(i, j int) bool {
			if employees[i].OnlineStatus != employees[j].OnlineStatus {
				return employees[i].OnlineStatus == veOnlineStatusOnline
			}
			return employees[i].Name < employees[j].Name
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"employees": employees,
		})
	}
}
