package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const defaultMaxFileSize int64 = 50 << 20 // 50MB
const maxReadableKnowledgeSourcesPerScope = 5000

// resolveKnowledgeMaxFileSize parses the file size limit from env at startup.
func resolveKnowledgeMaxFileSize() int64 {
	if v := strings.TrimSpace(os.Getenv("MACLAW_KNOWLEDGE_MAX_FILE_SIZE")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxFileSize
}

// knowledgeMaxUploadSize is resolved once at init time.
var knowledgeMaxUploadSize = resolveKnowledgeMaxFileSize()

func (s *HTTPServer) requireKnowledge(w http.ResponseWriter) bool {
	if s.knowledgeMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge base is not available"})
		return false
	}
	return true
}

// --- Import endpoints ---

func (s *HTTPServer) handleKnowledgeImportFile(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	maxSize := knowledgeMaxUploadSize
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+1024) // +1KB for form overhead

	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid multipart form"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()

	// Save to temp directory
	tmpDir := filepath.Join(s.svc.DataRoot(), "knowledge", "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	tmpFile, err := os.CreateTemp(tmpDir, "import-*-"+header.Filename)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, file); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save uploaded file"})
		return
	}
	_ = tmpFile.Close()

	title := strings.TrimSpace(r.FormValue("title"))
	labels := strings.TrimSpace(r.FormValue("labels"))
	topicHint := strings.TrimSpace(r.FormValue("topic_hint"))

	// Create async job for import
	job := s.jobs.createUserJob("knowledge_import_file", p, func(ctx context.Context) (any, error) {
		defer os.Remove(tmpPath)
		store := s.knowledgeMgr.Store()
		result, err := store.ImportFiles(ctx, knowledge.DirectoryImportRequest{
			RootPath:  filepath.Dir(tmpPath),
			OwnerID:   p.UserID,
			TenantID:  p.TenantID,
			TopicHint: topicHint,
			Labels:    splitLabels(labels),
		}, []string{tmpPath})
		if err != nil {
			return nil, err
		}
		// Apply user-provided title to the imported source if available.
		if title != "" && len(result.Items) > 0 {
			for _, item := range result.Items {
				if item.SourceID != "" {
					_, _ = store.UpdateSourceMetadata(ctx, knowledge.SourceUpdateRequest{
						ID:    item.SourceID,
						Title: title,
					})
					break
				}
			}
		}
		return sanitizeKnowledgeDirectoryImportResultForAPI(s.svc.DataRoot(), result), nil
	})

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id":   job.ID,
		"filename": filepath.Base(header.Filename),
		"status":   string(job.Status),
	})
}

func (s *HTTPServer) handleKnowledgeImportURL(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		URL       string `json:"url"`
		Title     string `json:"title"`
		TopicHint string `json:"topic_hint"`
		Labels    string `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}

	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source, err := store.SaveURL(ctx, knowledge.URLSaveRequest{
		URL:       req.URL,
		OwnerID:   p.UserID,
		TenantID:  p.TenantID,
		TopicHint: req.TopicHint,
		Labels:    splitLabels(req.Labels),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("failed to save URL: %v", err))})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"source_id": source.ID,
		"title":     source.Title,
		"kind":      string(source.Kind),
		"status":    string(source.Status),
	})
}

func (s *HTTPServer) handleKnowledgeImportText(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		Text      string `json:"text"`
		Title     string `json:"title"`
		TopicHint string `json:"topic_hint"`
		Labels    string `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}

	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	source, err := store.SaveText(ctx, knowledge.TextSaveRequest{
		Text:      req.Text,
		Title:     req.Title,
		OwnerID:   p.UserID,
		TenantID:  p.TenantID,
		TopicHint: req.TopicHint,
		Labels:    splitLabels(req.Labels),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("failed to save text: %v", err))})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"source_id": source.ID,
		"title":     source.Title,
		"kind":      string(source.Kind),
	})
}

func (s *HTTPServer) handleKnowledgeImportDirectory(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		Path      string `json:"path"`
		TopicHint string `json:"topic_hint"`
		Labels    string `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if _, err := os.Stat(req.Path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "directory does not exist"})
		return
	}

	job := s.jobs.createUserJob("knowledge_import_directory", p, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		result, err := store.ImportDirectory(ctx, knowledge.DirectoryImportRequest{
			RootPath:  req.Path,
			OwnerID:   p.UserID,
			TenantID:  p.TenantID,
			TopicHint: req.TopicHint,
			Labels:    splitLabels(req.Labels),
			Recursive: true,
		})
		return sanitizeKnowledgeDirectoryImportResultForAPI(s.svc.DataRoot(), result), err
	})

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id": job.ID,
		"path":   redactSupportBundleValue(s.svc.DataRoot(), req.Path),
		"status": string(job.Status),
	})
}

func (s *HTTPServer) handleKnowledgeImportJobStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	jobID := r.PathValue("jobId")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "jobId is required"})
		return
	}
	job, ok := s.jobs.getUserJob(jobID, p)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// --- Query endpoints ---

func (s *HTTPServer) handleKnowledgeSearch(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req knowledge.SearchOptions
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	// Enforce tenant isolation
	req.OwnerID = p.UserID
	req.TenantID = p.TenantID
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.Limit > 50 {
		req.Limit = 50
	}

	store := s.knowledgeMgr.AgentStore()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	results, err := store.Search(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("search failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": sanitizeKnowledgeSearchResultsForAPI(s.svc.DataRoot(), results),
		"total":   len(results),
	})
}

func (s *HTTPServer) handleKnowledgeContextPack(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req knowledge.ContextPackOptions
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	req.OwnerID = p.UserID
	req.TenantID = p.TenantID

	store := s.knowledgeMgr.AgentStore()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := store.ContextPack(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("context pack failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, sanitizeKnowledgeContextPackForAPI(s.svc.DataRoot(), result))
}

// --- Management endpoints ---

func (s *HTTPServer) handleKnowledgeListSources(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	sources, err := s.listReadableKnowledgeSources(ctx, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("list sources failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sources": sanitizeKnowledgeSourcesForAPI(s.svc.DataRoot(), sources),
		"total":   len(sources),
	})
}

func (s *HTTPServer) handleKnowledgeGetSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canReadSource(ctx, source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	writeJSON(w, http.StatusOK, sanitizeKnowledgeSourceForAPI(s.svc.DataRoot(), source))
}

func (s *HTTPServer) handleKnowledgeDeleteSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canAccessSource(source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if err := store.DeleteSource(ctx, sourceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("delete failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleKnowledgeUpdateSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	var req struct {
		Title     *string  `json:"title"`
		TopicHint *string  `json:"topic_hint"`
		Labels    []string `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canAccessSource(source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}

	updateReq := knowledge.SourceUpdateRequest{ID: sourceID}
	if req.Title != nil {
		updateReq.Title = *req.Title
	}
	if req.TopicHint != nil {
		updateReq.TopicHint = *req.TopicHint
	}
	if req.Labels != nil {
		updateReq.Labels = req.Labels
	}
	if _, err := store.UpdateSourceMetadata(ctx, updateReq); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("update failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *HTTPServer) handleKnowledgeDisableSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canAccessSource(source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if _, err := store.DisableSource(ctx, sourceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("disable failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func (s *HTTPServer) handleKnowledgeEnableSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canAccessSource(source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if _, err := store.EnableSource(ctx, sourceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("enable failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (s *HTTPServer) handleKnowledgeRefreshSource(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if !s.canAccessSource(source, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}
	if _, err := store.RefreshSource(ctx, sourceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("refresh failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func sanitizeKnowledgeDirectoryImportResultForAPI(dataRoot string, result knowledge.DirectoryImportResult) knowledge.DirectoryImportResult {
	result.RootPath = redactKnowledgePathForAPI(dataRoot, result.RootPath)
	result.CurrentFile = redactKnowledgePathForAPI(dataRoot, result.CurrentFile)
	for i := range result.Warnings {
		result.Warnings[i] = redactSupportBundleText(dataRoot, result.Warnings[i])
	}
	for i := range result.Items {
		result.Items[i].FilePath = redactKnowledgePathForAPI(dataRoot, result.Items[i].FilePath)
		result.Items[i].RelativePath = redactKnowledgePathForAPI(dataRoot, result.Items[i].RelativePath)
		result.Items[i].ErrorMessage = redactSupportBundleText(dataRoot, result.Items[i].ErrorMessage)
	}
	return result
}

func sanitizeKnowledgeSourceForAPI(dataRoot string, source knowledge.Source) knowledge.Source {
	source.URI = redactKnowledgeURIForAPI(dataRoot, source.URI)
	source.CanonicalURI = redactKnowledgeURIForAPI(dataRoot, source.CanonicalURI)
	source.Title = redactKnowledgeDisplayTextForAPI(dataRoot, source.Title)
	source.ProjectPath = redactKnowledgePathForAPI(dataRoot, source.ProjectPath)
	source.RelativePath = redactKnowledgePathForAPI(dataRoot, source.RelativePath)
	source.ErrorMessage = redactSupportBundleText(dataRoot, source.ErrorMessage)
	return source
}

func sanitizeKnowledgeSourcesForAPI(dataRoot string, sources []knowledge.Source) []knowledge.Source {
	out := make([]knowledge.Source, len(sources))
	for i := range sources {
		out[i] = sanitizeKnowledgeSourceForAPI(dataRoot, sources[i])
	}
	return out
}

func sanitizeKnowledgeSearchResultsForAPI(dataRoot string, results []knowledge.SearchResult) []knowledge.SearchResult {
	out := make([]knowledge.SearchResult, len(results))
	for i := range results {
		out[i] = results[i]
		out[i].Source = sanitizeKnowledgeSourceForAPI(dataRoot, results[i].Source)
		out[i].NodeTitle = redactKnowledgeDisplayTextForAPI(dataRoot, out[i].NodeTitle)
		out[i].CardTitle = redactKnowledgeDisplayTextForAPI(dataRoot, out[i].CardTitle)
		out[i].Citation = redactKnowledgeDisplayTextForAPI(dataRoot, out[i].Citation)
		out[i].Claim = redactSupportBundleText(dataRoot, out[i].Claim)
		out[i].Summary = redactSupportBundleText(dataRoot, out[i].Summary)
		out[i].Snippet = redactSupportBundleText(dataRoot, out[i].Snippet)
	}
	return out
}

func sanitizeKnowledgeContextPackForAPI(dataRoot string, result knowledge.ContextPackResult) knowledge.ContextPackResult {
	for i := range result.Items {
		result.Items[i].Title = redactKnowledgeDisplayTextForAPI(dataRoot, result.Items[i].Title)
		result.Items[i].Citation = redactSupportBundleText(dataRoot, result.Items[i].Citation)
	}
	for i := range result.Citations {
		result.Citations[i] = sanitizeKnowledgeCitationForAPI(dataRoot, result.Citations[i])
	}
	return result
}

func sanitizeKnowledgeCitationForAPI(dataRoot string, citation knowledge.Citation) knowledge.Citation {
	citation.Label = redactSupportBundleText(dataRoot, citation.Label)
	citation.SourceTitle = redactKnowledgeDisplayTextForAPI(dataRoot, citation.SourceTitle)
	citation.URI = redactKnowledgeURIForAPI(dataRoot, citation.URI)
	citation.RelativePath = redactKnowledgePathForAPI(dataRoot, citation.RelativePath)
	citation.Snippet = redactSupportBundleText(dataRoot, citation.Snippet)
	return citation
}

func redactKnowledgeDisplayTextForAPI(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.Contains(value, "://") {
		return redactEndpointForAPI(dataRoot, value)
	}
	if supportBundleLooksAbsolutePath(value) {
		return redactKnowledgePathForAPI(dataRoot, value)
	}
	return redactSupportBundleText(dataRoot, value)
}

func redactKnowledgeURIForAPI(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.Contains(value, "://") {
		return redactEndpointForAPI(dataRoot, value)
	}
	return redactKnowledgePathForAPI(dataRoot, value)
}

func redactKnowledgePathForAPI(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	wasPath := supportBundleLooksAbsolutePath(value)
	value = redactSupportBundleText(dataRoot, value)
	if wasPath || supportBundleLooksAbsolutePath(value) {
		return supportBundlePathBase(value)
	}
	return value
}

func (s *HTTPServer) canReadSource(ctx context.Context, source knowledge.Source, p agentservice.Principal) bool {
	if s.canAccessSource(source, p) {
		return true
	}
	if source.Status == knowledge.StatusDisabled {
		return false
	}
	for _, scope := range s.knowledgeMgr.Access().ResolveForUser(ctx, p.TenantID, p.UserID) {
		if source.TenantID == scope.TenantID && source.OwnerID == scope.OwnerID {
			return true
		}
	}
	return false
}

func (s *HTTPServer) listReadableKnowledgeSources(ctx context.Context, p agentservice.Principal) ([]knowledge.Source, error) {
	store := s.knowledgeMgr.Store()
	var merged []knowledge.Source
	seen := map[string]struct{}{}
	for _, scope := range s.knowledgeMgr.Access().ResolveForUser(ctx, p.TenantID, p.UserID) {
		opts := knowledge.ListSourcesOptions{OwnerID: scope.OwnerID, TenantID: scope.TenantID, Limit: maxReadableKnowledgeSourcesPerScope}
		if scope.TenantID != p.TenantID || scope.OwnerID != p.UserID {
			opts.Status = "active"
		}
		sources, err := store.ListSources(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if _, ok := seen[source.ID]; ok {
				continue
			}
			seen[source.ID] = struct{}{}
			merged = append(merged, source)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
	})
	return merged, nil
}

// canAccessSource checks if the principal owns the given source.
// User-side management endpoints are intentionally stricter than read access:
// cross-user readable scopes can be searched and opened, but only the owner can
// update, disable, refresh, or delete a source.
func (s *HTTPServer) canAccessSource(source knowledge.Source, p agentservice.Principal) bool {
	return source.TenantID == p.TenantID && source.OwnerID == p.UserID
}

func (s *HTTPServer) handleKnowledgeStats(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sources, err := s.listReadableKnowledgeSources(ctx, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("stats failed: %v", err))})
		return
	}
	stats := knowledgeStatsFromSources(sources)

	// Augment stats with embedding/vector search status for observability.
	s.knowledgeMgr.mu.RLock()
	vectorSearchActive := s.knowledgeMgr.embedder != nil && !embedding.IsNoop(s.knowledgeMgr.embedder)
	s.knowledgeMgr.mu.RUnlock()

	result := map[string]interface{}{
		"stats":                stats,
		"vector_search_active": vectorSearchActive,
	}
	writeJSON(w, http.StatusOK, result)
}

func knowledgeStatsFromSources(sources []knowledge.Source) knowledge.Stats {
	stats := knowledge.Stats{
		SourcesByKind:   make(map[string]int),
		SourcesByStatus: make(map[string]int),
		SourcesByDomain: make(map[string]int),
		SourcesByLabel:  make(map[string]int),
	}
	for _, source := range sources {
		stats.Sources++
		stats.DocumentNodes += source.NodeCount
		stats.Cards += source.CardCount
		stats.Facts += source.FactCount
		stats.SourcesByKind[knowledgeStatKey(source.Kind)]++
		stats.SourcesByStatus[knowledgeStatKey(source.Status)]++
		if domain := strings.ToLower(strings.TrimSpace(source.SiteName)); domain != "" {
			stats.SourcesByDomain[domain]++
		}
		seenLabels := make(map[string]struct{}, len(source.Labels))
		for _, label := range source.Labels {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			if _, ok := seenLabels[label]; ok {
				continue
			}
			seenLabels[label] = struct{}{}
			stats.SourcesByLabel[label]++
		}
		if source.Status == knowledge.StatusParsed || source.Status == knowledge.StatusDistilled || source.Status == knowledge.StatusStale {
			if source.NodeCount == 0 {
				stats.SourcesWithoutNodes++
			}
			if source.CardCount == 0 {
				stats.SourcesWithoutCards++
			}
			if source.NodeCount > 0 && source.CardCount == 0 {
				stats.SourcesRebuildCards++
			}
			if source.Status == knowledge.StatusDistilled || source.Status == knowledge.StatusStale {
				if source.FactCount == 0 {
					stats.SourcesWithoutFacts++
				}
				if source.NodeCount > 0 && source.FactCount == 0 {
					stats.SourcesRebuildFacts++
				}
			}
		}
	}
	return stats
}

func knowledgeStatKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func (s *HTTPServer) handleKnowledgeClearAll(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	deleted, err := store.DeleteSourcesByFilter(ctx, knowledge.ListSourcesOptions{
		OwnerID:  p.UserID,
		TenantID: p.TenantID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("clear failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "cleared", "deleted": deleted})
}

// --- Admin endpoints ---

func (s *HTTPServer) handleAdminKnowledgeStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := store.Stats(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("stats failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *HTTPServer) handleAdminKnowledgeListSources(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Admin: no tenant/owner filter
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("list sources failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sources": sanitizeKnowledgeSourcesForAPI(s.svc.DataRoot(), sources),
		"total":   len(sources),
	})
}

func (s *HTTPServer) handleAdminKnowledgeClearTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	tenantID := r.PathValue("tenantId")
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	deleted, err := store.DeleteSourcesByFilter(ctx, knowledge.ListSourcesOptions{
		TenantID: tenantID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("clear tenant failed: %v", err))})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_tenant_cleared", "knowledge", tenantID, map[string]string{"tenant_id": tenantID, "deleted": fmt.Sprint(deleted), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "cleared", "deleted": deleted})
}

// --- Helpers ---

func splitLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

func readJSONBody(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("empty request body")
	}
	return json.Unmarshal(body, v)
}
