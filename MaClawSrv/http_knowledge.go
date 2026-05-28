package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	"github.com/nwaples/rardecode/v2"
)

const defaultMaxFileSize int64 = 50 << 20 // 50MB
const maxReadableKnowledgeSourcesPerScope = 5000
const maxKnowledgeArchiveFiles = 2000
const maxKnowledgeUploadFiles = 20

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
	r.Body = http.MaxBytesReader(w, r.Body, knowledgeMultipartRequestLimit(maxSize))

	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid multipart form"})
		return
	}
	tmpDir := filepath.Join(s.svc.DataRoot(), "knowledge", "tmp")
	uploads, err := saveKnowledgeMultipartFiles(r, tmpDir, "import-*", maxSize, maxKnowledgeUploadFiles)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	labels := strings.TrimSpace(r.FormValue("labels"))
	topicHint := strings.TrimSpace(r.FormValue("topic_hint"))

	// Create async job for import
	job := s.jobs.createUserJob("knowledge_import_file", p, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		importReq := knowledge.DirectoryImportRequest{
			OwnerID:   p.UserID,
			TenantID:  p.TenantID,
			TopicHint: topicHint,
			Labels:    splitLabels(labels),
		}
		result, err := importKnowledgeUploadedFiles(ctx, store, uploads, tmpDir, maxSize, importReq)
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
		"job_id":     job.ID,
		"filename":   uploads[0].Name,
		"filenames":  uploadedKnowledgeFileNames(uploads),
		"file_count": len(uploads),
		"status":     string(job.Status),
	})
}

func isKnowledgeArchivePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".zip" || ext == ".rar"
}

func archiveExtractLimit(uploadLimit int64) int64 {
	limit := uploadLimit * 10
	if limit <= 0 || limit > 500<<20 {
		return 500 << 20
	}
	return limit
}

func knowledgeMultipartRequestLimit(fileLimit int64) int64 {
	if fileLimit <= 0 {
		fileLimit = defaultMaxFileSize
	}
	return fileLimit*maxKnowledgeUploadFiles + int64(maxKnowledgeUploadFiles*4096)
}

func extractKnowledgeArchive(ctx context.Context, archivePath, uploadName, parentDir string, maxBytes int64) (string, []string, error) {
	extractDir, err := os.MkdirTemp(parentDir, "knowledge-archive-*")
	if err != nil {
		return "", nil, err
	}
	var paths []string
	switch strings.ToLower(filepath.Ext(uploadName)) {
	case ".zip":
		paths, err = extractKnowledgeZip(ctx, archivePath, extractDir, maxBytes)
	case ".rar":
		paths, err = extractKnowledgeRAR(ctx, archivePath, extractDir, maxBytes)
	default:
		err = fmt.Errorf("unsupported archive type")
	}
	if err != nil {
		_ = os.RemoveAll(extractDir)
		return "", nil, err
	}
	if len(paths) == 0 {
		_ = os.RemoveAll(extractDir)
		return "", nil, fmt.Errorf("archive has no importable files")
	}
	return extractDir, paths, nil
}

func extractKnowledgeZip(ctx context.Context, archivePath, extractDir string, maxBytes int64) ([]string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	paths := make([]string, 0, len(zr.File))
	var total int64
	for _, file := range zr.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > uint64(maxBytes-total) {
			return nil, fmt.Errorf("archive exceeds extracted size limit")
		}
		outPath, err := safeKnowledgeArchivePath(extractDir, file.Name)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, err
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		written, copyErr := writeKnowledgeArchiveFile(outPath, rc, maxBytes-total)
		closeErr := rc.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += written
		paths = append(paths, outPath)
		if len(paths) > maxKnowledgeArchiveFiles {
			return nil, fmt.Errorf("archive exceeds file count limit")
		}
	}
	return paths, nil
}

func extractKnowledgeRAR(ctx context.Context, archivePath, extractDir string, maxBytes int64) ([]string, error) {
	rr, err := rardecode.OpenReader(archivePath, rardecode.MaxDictionarySize(128<<20))
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var paths []string
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.IsDir {
			continue
		}
		if header.Encrypted || header.HeaderEncrypted {
			return nil, fmt.Errorf("encrypted rar archives are not supported")
		}
		if !header.UnKnownSize && header.UnPackedSize > maxBytes-total {
			return nil, fmt.Errorf("archive exceeds extracted size limit")
		}
		outPath, err := safeKnowledgeArchivePath(extractDir, header.Name)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, err
		}
		written, err := writeKnowledgeArchiveFile(outPath, rr, maxBytes-total)
		if err != nil {
			return nil, err
		}
		total += written
		paths = append(paths, outPath)
		if len(paths) > maxKnowledgeArchiveFiles {
			return nil, fmt.Errorf("archive exceeds file count limit")
		}
	}
	return paths, nil
}

func safeKnowledgeArchivePath(root, name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("archive contains unsafe path")
	}
	outPath := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, outAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive contains unsafe path")
	}
	return outAbs, nil
}

func writeKnowledgeArchiveFile(path string, src io.Reader, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, fmt.Errorf("archive exceeds extracted size limit")
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(src, maxBytes+1))
	if err != nil {
		return written, err
	}
	if written > maxBytes {
		return written, fmt.Errorf("archive exceeds extracted size limit")
	}
	return written, nil
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

func (s *HTTPServer) handleKnowledgeImportURLs(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		URLs           []string `json:"urls"`
		Text           string   `json:"text"`
		MaxDepth       int      `json:"max_depth"`
		SameDomainOnly *bool    `json:"same_domain_only"`
		TopicHint      string   `json:"topic_hint"`
		Labels         string   `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	urls := normalizeKnowledgeImportURLs(append(req.URLs, req.Text))
	if len(urls) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "urls are required"})
		return
	}
	if req.MaxDepth < 0 || req.MaxDepth > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_depth must be between 0 and 5"})
		return
	}
	sameDomainOnly := true
	if req.SameDomainOnly != nil {
		sameDomainOnly = *req.SameDomainOnly
	}

	job := s.jobs.createUserJob("knowledge_import_urls", p, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		labels := splitLabels(req.Labels)
		if req.MaxDepth == 0 {
			result := store.SaveURLs(ctx, knowledge.URLBatchSaveRequest{
				URLs:      urls,
				OwnerID:   p.UserID,
				TenantID:  p.TenantID,
				TopicHint: req.TopicHint,
				Labels:    labels,
			})
			return result, nil
		}
		results := make([]knowledge.DeepCrawlResult, 0, len(urls))
		for _, rawURL := range urls {
			engine := knowledge.NewDeepCrawlEngine(store, nil)
			result, err := engine.StartCrawl(ctx, knowledge.DeepCrawlRequest{
				SeedURL:        rawURL,
				MaxDepth:       req.MaxDepth,
				SameDomainOnly: sameDomainOnly,
				OwnerID:        p.UserID,
				TenantID:       p.TenantID,
				TopicHint:      req.TopicHint,
				Labels:         labels,
			})
			if err != nil {
				if ctx.Err() != nil {
					return nil, err
				}
				results = append(results, knowledge.DeepCrawlResult{
					Status: "failed",
					Failed: 1,
					Items:  []knowledge.DeepCrawlItem{{URL: rawURL, Status: "failed", Error: err.Error()}},
				})
				continue
			}
			results = append(results, result)
		}
		return map[string]any{"results": results}, nil
	})

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id":    job.ID,
		"status":    string(job.Status),
		"url_count": len(urls),
		"max_depth": req.MaxDepth,
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
		"status":    "completed",
		"source_id": source.ID,
		"title":     source.Title,
		"kind":      string(source.Kind),
	})
}

type uploadedKnowledgeFile struct {
	Name string
	Path string
}

func saveKnowledgeMultipartFiles(r *http.Request, tmpDir, patternPrefix string, maxFileSize int64, maxFiles int) ([]uploadedKnowledgeFile, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		return nil, fmt.Errorf("file field is required")
	}
	if maxFiles <= 0 {
		maxFiles = maxKnowledgeUploadFiles
	}
	if len(r.MultipartForm.File["file"]) > maxFiles {
		return nil, fmt.Errorf("too many files: maximum is %d", maxFiles)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory")
	}
	uploads := make([]uploadedKnowledgeFile, 0, len(r.MultipartForm.File["file"]))
	for _, header := range r.MultipartForm.File["file"] {
		upload, err := saveKnowledgeMultipartFile(tmpDir, patternPrefix, header, maxFileSize)
		if err != nil {
			removeUploadedKnowledgeFiles(uploads)
			return nil, err
		}
		uploads = append(uploads, upload)
	}
	return uploads, nil
}

func saveKnowledgeMultipartFile(tmpDir, patternPrefix string, header *multipart.FileHeader, maxFileSize int64) (uploadedKnowledgeFile, error) {
	if header == nil {
		return uploadedKnowledgeFile{}, fmt.Errorf("file field is required")
	}
	if maxFileSize <= 0 {
		maxFileSize = defaultMaxFileSize
	}
	if header.Size > maxFileSize {
		return uploadedKnowledgeFile{}, fmt.Errorf("file too large: maximum is %d bytes", maxFileSize)
	}
	file, err := header.Open()
	if err != nil {
		return uploadedKnowledgeFile{}, fmt.Errorf("file field is required")
	}
	defer file.Close()
	uploadName := filepath.Base(header.Filename)
	if uploadName == "." || uploadName == string(filepath.Separator) || strings.TrimSpace(uploadName) == "" {
		uploadName = "upload"
	}
	tmpFile, err := os.CreateTemp(tmpDir, patternPrefix+"-"+uploadName)
	if err != nil {
		return uploadedKnowledgeFile{}, fmt.Errorf("failed to create temp file")
	}
	tmpPath := tmpFile.Name()
	limited := &io.LimitedReader{R: file, N: maxFileSize + 1}
	written, err := io.Copy(tmpFile, limited)
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return uploadedKnowledgeFile{}, fmt.Errorf("failed to save uploaded file")
	}
	if written > maxFileSize {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return uploadedKnowledgeFile{}, fmt.Errorf("file too large: maximum is %d bytes", maxFileSize)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return uploadedKnowledgeFile{}, fmt.Errorf("failed to save uploaded file")
	}
	return uploadedKnowledgeFile{Name: uploadName, Path: tmpPath}, nil
}

func importKnowledgeUploadedFiles(ctx context.Context, store *knowledge.SQLiteStore, uploads []uploadedKnowledgeFile, tmpDir string, maxSize int64, baseReq knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	merged := knowledge.DirectoryImportResult{Status: "completed"}
	for _, upload := range uploads {
		defer os.Remove(upload.Path)
		importReq := baseReq
		importReq.RootPath = filepath.Dir(upload.Path)
		importReq.Recursive = false
		var result knowledge.DirectoryImportResult
		var err error
		if isKnowledgeArchivePath(upload.Name) {
			extractDir, extracted, err := extractKnowledgeArchive(ctx, upload.Path, upload.Name, tmpDir, archiveExtractLimit(maxSize))
			if err != nil {
				return merged, err
			}
			defer os.RemoveAll(extractDir)
			importReq.RootPath = extractDir
			importReq.Recursive = true
			result, err = store.ImportFiles(ctx, importReq, extracted)
		} else {
			result, err = store.ImportFiles(ctx, importReq, []string{upload.Path})
		}
		if err != nil {
			return merged, err
		}
		merged = mergeKnowledgeDirectoryImportResults(merged, result)
	}
	return merged, nil
}

func mergeKnowledgeDirectoryImportResults(a, b knowledge.DirectoryImportResult) knowledge.DirectoryImportResult {
	if a.RootPath == "" {
		a.RootPath = b.RootPath
	}
	if a.BatchID == "" && len(a.Items) == 0 {
		a.BatchID = b.BatchID
	} else if b.BatchID != "" && a.BatchID != b.BatchID {
		a.BatchID = ""
	}
	if b.Status != "" && b.Status != "completed" {
		a.Status = b.Status
	}
	a.TotalFiles += b.TotalFiles
	a.QueuedFiles += b.QueuedFiles
	a.DuplicateFiles += b.DuplicateFiles
	a.SkippedFiles += b.SkippedFiles
	a.ImportedFiles += b.ImportedFiles
	a.FailedFiles += b.FailedFiles
	a.ProcessedFiles += b.ProcessedFiles
	a.EstimatedBytes += b.EstimatedBytes
	a.Warnings = append(a.Warnings, b.Warnings...)
	a.Items = append(a.Items, b.Items...)
	if b.CurrentFile != "" {
		a.CurrentFile = b.CurrentFile
	}
	if b.CurrentStep != "" {
		a.CurrentStep = b.CurrentStep
		a.StepProgress = b.StepProgress
		a.TotalSteps = b.TotalSteps
		a.CurrentStepNum = b.CurrentStepNum
	}
	return a
}

func uploadedKnowledgeFileNames(uploads []uploadedKnowledgeFile) []string {
	names := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		names = append(names, upload.Name)
	}
	return names
}

func removeUploadedKnowledgeFiles(uploads []uploadedKnowledgeFile) {
	for _, upload := range uploads {
		_ = os.Remove(upload.Path)
	}
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

func normalizeKnowledgeImportURLs(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == ',' || r == ';' || r == '，' || r == '；'
		}) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
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

func (s *HTTPServer) handleAdminPublicKnowledgeLibraries(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	libraries, err := s.knowledgeMgr.Access().ListPublicLibraries(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	views, err := s.publicKnowledgeLibraryViews(r.Context(), libraries)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": views})
}

func (s *HTTPServer) publicKnowledgeLibraryViews(ctx context.Context, libraries []publicKnowledgeLibrary) ([]publicKnowledgeLibraryView, error) {
	views := make([]publicKnowledgeLibraryView, 0, len(libraries))
	store := s.knowledgeMgr.Store()
	for _, library := range libraries {
		sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID, Limit: maxReadableKnowledgeSourcesPerScope})
		if err != nil {
			return nil, err
		}
		view := publicKnowledgeLibraryView{publicKnowledgeLibrary: library, SourceCount: len(sources)}
		for _, source := range sources {
			if source.Status == knowledge.StatusDistilled || source.Status == knowledge.StatusStale {
				view.DistilledSources++
			}
			updated := source.UpdatedAt
			if updated.IsZero() {
				updated = source.CreatedAt
			}
			if !updated.IsZero() && (view.LatestSourceAt == nil || updated.After(*view.LatestSourceAt)) {
				copyTime := updated
				view.LatestSourceAt = &copyTime
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *HTTPServer) handleAdminPublicKnowledgeSources(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	library, ok := s.publicKnowledgeLibraryForRequest(w, r)
	if !ok {
		return
	}
	sources, err := s.knowledgeMgr.Store().ListSources(r.Context(), knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID, Limit: maxReadableKnowledgeSourcesPerScope})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"library": library, "sources": sanitizeKnowledgeSourcesForAPI(s.svc.DataRoot(), sources), "total": len(sources)})
}

func (s *HTTPServer) handleAdminPublicKnowledgeCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		TenantID string `json:"tenant_id"`
		Name     string `json:"name"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !s.requireExistingKnowledgeTenant(w, r, req.TenantID) {
		return
	}
	library, created, err := s.knowledgeMgr.Access().EnsurePublicLibrary(r.Context(), req.TenantID, req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if created {
		s.recordPublicKnowledgeAudit(r, "admin.public_knowledge_library_created", library, map[string]string{})
		writeJSON(w, http.StatusCreated, library)
		return
	}
	writeJSON(w, http.StatusOK, library)
}

func (s *HTTPServer) handleAdminPublicKnowledgeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	library, ok, err := s.knowledgeMgr.Access().GetPublicLibrary(r.Context(), r.PathValue("libraryId"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "library not found"})
		return
	}
	deleted, err := s.knowledgeMgr.Store().DeleteSourcesByFilter(r.Context(), knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	library, ok, removedScopes, err := s.knowledgeMgr.Access().DeletePublicLibrary(r.Context(), library.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "library not found"})
		return
	}
	s.recordPublicKnowledgeAudit(r, "admin.public_knowledge_library_deleted", library, map[string]string{"deleted_sources": strconv.Itoa(deleted), "removed_scopes": strconv.Itoa(removedScopes)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "library": library, "deleted_sources": deleted, "removed_scopes": removedScopes})
}

func (s *HTTPServer) recordPublicKnowledgeAudit(r *http.Request, action string, library publicKnowledgeLibrary, extra map[string]string) {
	metadata := map[string]string{
		"tenant_id":    library.TenantID,
		"owner_id":     library.OwnerID,
		"library_id":   library.ID,
		"library_name": library.Name,
		"remote_ip":    requestClientIP(r),
	}
	for key, value := range extra {
		metadata[key] = value
	}
	_ = s.recordAdminAudit(r.Context(), action, "public_knowledge_library", library.ID, metadata)
}

func (s *HTTPServer) requireExistingKnowledgeTenant(w http.ResponseWriter, r *http.Request, tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id is required"})
		return false
	}
	if _, err := s.svc.GetTenant(r.Context(), tenantID); err != nil {
		if errors.Is(err, agentservice.ErrTenantNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return false
	}
	return true
}

func (s *HTTPServer) publicKnowledgeLibraryForRequest(w http.ResponseWriter, r *http.Request) (publicKnowledgeLibrary, bool) {
	library, ok, err := s.knowledgeMgr.Access().GetPublicLibrary(r.Context(), r.PathValue("libraryId"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return publicKnowledgeLibrary{}, false
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "library not found"})
		return publicKnowledgeLibrary{}, false
	}
	return library, true
}

func (s *HTTPServer) handleAdminPublicKnowledgeImportText(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) || !s.requireKnowledge(w) {
		return
	}
	library, ok := s.publicKnowledgeLibraryForRequest(w, r)
	if !ok {
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
	source, err := s.knowledgeMgr.Store().SaveText(r.Context(), knowledge.TextSaveRequest{Text: req.Text, Title: req.Title, OwnerID: library.OwnerID, TenantID: library.TenantID, TopicHint: req.TopicHint, Labels: splitLabels(req.Labels)})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	s.recordPublicKnowledgeAudit(r, "admin.public_knowledge_import_text", library, map[string]string{"source_id": source.ID, "kind": source.Kind})
	writeJSON(w, http.StatusCreated, map[string]any{"status": "completed", "library": library, "source_id": source.ID, "title": source.Title, "kind": source.Kind})
}

func (s *HTTPServer) handleAdminPublicKnowledgeImportURLs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) || !s.requireKnowledge(w) {
		return
	}
	library, ok := s.publicKnowledgeLibraryForRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		URLs           []string `json:"urls"`
		Text           string   `json:"text"`
		MaxDepth       int      `json:"max_depth"`
		SameDomainOnly *bool    `json:"same_domain_only"`
		TopicHint      string   `json:"topic_hint"`
		Labels         string   `json:"labels"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	urls := normalizeKnowledgeImportURLs(append(req.URLs, req.Text))
	if len(urls) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "urls are required"})
		return
	}
	if req.MaxDepth < 0 || req.MaxDepth > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_depth must be between 0 and 5"})
		return
	}
	sameDomainOnly := true
	if req.SameDomainOnly != nil {
		sameDomainOnly = *req.SameDomainOnly
	}
	job := s.jobs.createUserJob("public_knowledge_import_urls", agentservice.Principal{TenantID: library.TenantID, UserID: library.OwnerID}, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		labels := splitLabels(req.Labels)
		if req.MaxDepth == 0 {
			return store.SaveURLs(ctx, knowledge.URLBatchSaveRequest{URLs: urls, OwnerID: library.OwnerID, TenantID: library.TenantID, TopicHint: req.TopicHint, Labels: labels}), nil
		}
		results := make([]knowledge.DeepCrawlResult, 0, len(urls))
		for _, rawURL := range urls {
			result, err := knowledge.NewDeepCrawlEngine(store, nil).StartCrawl(ctx, knowledge.DeepCrawlRequest{SeedURL: rawURL, MaxDepth: req.MaxDepth, SameDomainOnly: sameDomainOnly, OwnerID: library.OwnerID, TenantID: library.TenantID, TopicHint: req.TopicHint, Labels: labels})
			if err != nil {
				if ctx.Err() != nil {
					return nil, err
				}
				results = append(results, knowledge.DeepCrawlResult{Status: "failed", Failed: 1, Items: []knowledge.DeepCrawlItem{{URL: rawURL, Status: "failed", Error: err.Error()}}})
				continue
			}
			results = append(results, result)
		}
		return map[string]any{"results": results}, nil
	})
	s.recordPublicKnowledgeAudit(r, "admin.public_knowledge_import_urls", library, map[string]string{"job_id": job.ID, "url_count": strconv.Itoa(len(urls)), "max_depth": strconv.Itoa(req.MaxDepth)})
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": string(job.Status), "library": library, "url_count": len(urls), "max_depth": req.MaxDepth})
}

func (s *HTTPServer) handleAdminPublicKnowledgeImportFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) || !s.requireKnowledge(w) {
		return
	}
	library, ok := s.publicKnowledgeLibraryForRequest(w, r)
	if !ok {
		return
	}
	maxSize := knowledgeMaxUploadSize
	r.Body = http.MaxBytesReader(w, r.Body, knowledgeMultipartRequestLimit(maxSize))
	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid multipart form"})
		return
	}
	tmpDir := filepath.Join(s.svc.DataRoot(), "knowledge", "tmp")
	uploads, err := saveKnowledgeMultipartFiles(r, tmpDir, "public-import-*", maxSize, maxKnowledgeUploadFiles)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	topicHint := strings.TrimSpace(r.FormValue("topic_hint"))
	labels := strings.TrimSpace(r.FormValue("labels"))
	job := s.jobs.createUserJob("public_knowledge_import_file", agentservice.Principal{TenantID: library.TenantID, UserID: library.OwnerID}, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		importReq := knowledge.DirectoryImportRequest{OwnerID: library.OwnerID, TenantID: library.TenantID, TopicHint: topicHint, Labels: splitLabels(labels)}
		result, err := importKnowledgeUploadedFiles(ctx, store, uploads, tmpDir, maxSize, importReq)
		return sanitizeKnowledgeDirectoryImportResultForAPI(s.svc.DataRoot(), result), err
	})
	s.recordPublicKnowledgeAudit(r, "admin.public_knowledge_import_file", library, map[string]string{"job_id": job.ID, "filename": uploads[0].Name, "file_count": strconv.Itoa(len(uploads))})
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": string(job.Status), "library": library, "filename": uploads[0].Name, "filenames": uploadedKnowledgeFileNames(uploads), "file_count": len(uploads)})
}

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
