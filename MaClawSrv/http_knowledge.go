package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/nwaples/rardecode/v2"
)

const defaultMaxFileSize int64 = 50 << 20 // 50MB
const maxReadableKnowledgeSourcesPerScope = 5000
const maxKnowledgeArchiveFiles = 2000
const maxKnowledgeUploadFiles = 20
const maxKnowledgePackageJSONBodyBytes int64 = 50 << 20

// knowledgeShareClient is a shared HTTP client for Hub knowledge share operations.
// Uses TLS-skip transport because Hub servers commonly use self-signed certificates.
var knowledgeShareClient = remote.NewHubHTTPClient()

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
	return knowledgeMultipartRequestLimitForFiles(fileLimit, maxKnowledgeUploadFiles)
}

func knowledgeMultipartRequestLimitForFiles(fileLimit int64, fileCount int) int64 {
	if fileLimit <= 0 {
		fileLimit = defaultMaxFileSize
	}
	if fileCount <= 0 {
		fileCount = 1
	}
	return fileLimit*int64(fileCount) + int64(fileCount*4096)
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

type knowledgeExportRequest struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	SourceIDs       []string `json:"source_ids"`
	IncludeDisabled bool     `json:"include_disabled"`
}

type knowledgePackageManifest struct {
	Format      string `json:"format"`
	Version     int    `json:"version"`
	PackageID   string `json:"package_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	TenantID    string `json:"tenant_id,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	SourceCount int    `json:"source_count"`
	Editable    bool   `json:"editable"`
	Notes       string `json:"notes,omitempty"`
}

type knowledgePackageSource struct {
	ID               string   `json:"id,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	URI              string   `json:"uri,omitempty"`
	CanonicalURI     string   `json:"canonical_uri,omitempty"`
	Title            string   `json:"title,omitempty"`
	Author           string   `json:"author,omitempty"`
	SiteName         string   `json:"site_name,omitempty"`
	TopicHint        string   `json:"topic_hint,omitempty"`
	Labels           []string `json:"labels,omitempty"`
	Status           string   `json:"status,omitempty"`
	RelativePath     string   `json:"relative_path,omitempty"`
	BatchID          string   `json:"batch_id,omitempty"`
	ContentHash      string   `json:"content_hash,omitempty"`
	NodeCount        int      `json:"node_count,omitempty"`
	CardCount        int      `json:"card_count,omitempty"`
	FactCount        int      `json:"fact_count,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	Content          string   `json:"content,omitempty"`
	ContentTruncated bool     `json:"content_truncated,omitempty"`
}

type knowledgePackage struct {
	Manifest knowledgePackageManifest `json:"manifest"`
	Sources  []knowledgePackageSource `json:"sources"`
}

func (s *HTTPServer) handleKnowledgeExport(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req knowledgeExportRequest
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description is required"})
		return
	}
	// Increased from 15s to 60s: buildKnowledgeExportPackageWithStore now reads document
	// nodes for each source to include inline content. Large knowledge bases may take time.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	opts := knowledge.ListSourcesOptions{
		TenantID:  p.TenantID,
		OwnerID:   p.UserID,
		SourceIDs: compactStrings(req.SourceIDs),
		Limit:     maxReadableKnowledgeSourcesPerScope,
	}
	if !req.IncludeDisabled {
		opts.Status = "active"
	} else {
		opts.IncludeDisabled = true
	}
	sources, err := s.knowledgeMgr.Store().ListSources(ctx, opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("export knowledge failed: %v", err))})
		return
	}
	pkg := buildKnowledgeExportPackageWithStore(ctx, s.knowledgeMgr.Store(), s.svc.DataRoot(), p, strings.TrimSpace(req.Title), description, sources)
	filename := fmt.Sprintf("maclaw-knowledge-%s.json", pkg.Manifest.PackageID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if err := json.NewEncoder(w).Encode(pkg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleKnowledgeImportPackage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var pkg knowledgePackage
	if err := readJSONBodyWithLimit(r, &pkg, maxKnowledgePackageJSONBodyBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported knowledge package format"})
		return
	}
	if len(pkg.Sources) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package has no sources"})
		return
	}
	job := s.startKnowledgePackageImportJob("knowledge_import_package", p, pkg)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job_id": job.ID, "status": string(job.Status), "package_id": pkg.Manifest.PackageID})
}

func (s *HTTPServer) handleKnowledgeImportShare(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req struct {
		KnowledgeID   string `json:"knowledge_id"`
		ShareLink     string `json:"share_link"`
		HubURL        string `json:"hub_url"`
		HubToken      string `json:"hub_token"`
		Authorization string `json:"authorization"`
	}
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if strings.TrimSpace(req.KnowledgeID) == "" && strings.TrimSpace(req.ShareLink) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "knowledge_id or share_link is required"})
		return
	}
	apiURL, knowledgeID, err := resolveKnowledgeShareAPIURL(req.ShareLink, req.KnowledgeID, req.HubURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	authHeader := knowledgeShareAuthorizationHeader(req.HubToken, req.Authorization)
	share, err := fetchKnowledgeShareMetadata(ctx, apiURL, authHeader)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	packageURL := knowledgeSharePackageURL(apiURL, share)
	if packageURL == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":       "resolved_metadata",
			"knowledge_id": knowledgeID,
			"api_url":      apiURL,
			"share":        share,
			"note":         "share metadata resolved; package_url is not available",
		})
		return
	}
	pkg, err := fetchKnowledgePackage(ctx, packageURL, authHeader)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if err := validateKnowledgePackage(pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	job := s.startKnowledgePackageImportJob("knowledge_import_share", p, pkg)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job_id": job.ID, "status": string(job.Status), "knowledge_id": knowledgeID, "api_url": apiURL, "package_url": packageURL, "package_id": pkg.Manifest.PackageID, "share": share})
}

func validateKnowledgePackage(pkg knowledgePackage) error {
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return fmt.Errorf("unsupported knowledge package format")
	}
	if len(pkg.Sources) == 0 {
		return fmt.Errorf("package has no sources")
	}
	return nil
}

func (s *HTTPServer) startKnowledgePackageImportJob(kind string, p agentservice.Principal, pkg knowledgePackage) *asyncJobRecord {
	return s.jobs.createUserJob(kind, p, func(ctx context.Context) (any, error) {
		store := s.knowledgeMgr.Store()
		// Convert package sources to the canonical shared type.
		sources := make([]knowledge.PackageSource, 0, len(pkg.Sources))
		for _, item := range pkg.Sources {
			sources = append(sources, knowledge.PackageSource{
				ID:               item.ID,
				Kind:             item.Kind,
				URI:              item.URI,
				CanonicalURI:     item.CanonicalURI,
				Title:            item.Title,
				TopicHint:        item.TopicHint,
				Labels:           item.Labels,
				Content:          item.Content,
				ContentTruncated: item.ContentTruncated,
			})
		}
		importResult := knowledge.ImportPackageSources(ctx, store, sources, knowledge.PackageImportOptions{
			OwnerID:   p.UserID,
			TenantID:  p.TenantID,
			TopicHint: pkg.Manifest.Title,
			RootPath:  "share://" + pkg.Manifest.PackageID,
		})
		return map[string]interface{}{
			"import_status":       importResult.Status,
			"package_id":          pkg.Manifest.PackageID,
			"title":               pkg.Manifest.Title,
			"imported":            importResult.Imported,
			"skipped":             importResult.Skipped,
			"failed":              importResult.Failed,
			"total":               importResult.Total,
			"imported_source_ids": importResult.ImportedSourceIDs,
			"skipped_source_ids":  importResult.SkippedSourceIDs,
			"failed_source_ids":   importResult.FailedSourceIDs,
			"retry_source_ids":    importResult.RetrySourceIDs,
			"warnings":            importResult.Warnings,
		}, nil
	})
}

func resolveKnowledgeShareAPIURL(shareLink, knowledgeID, hubURL string) (string, string, error) {
	shareLink = strings.TrimSpace(shareLink)
	knowledgeID = strings.TrimSpace(knowledgeID)
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if shareLink != "" {
		parsed, err := url.Parse(shareLink)
		if err != nil {
			return "", "", fmt.Errorf("invalid share_link: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return "", "", fmt.Errorf("share_link must be an absolute URL")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if knowledgeID == "" && len(parts) > 0 {
			knowledgeID = parts[len(parts)-1]
		}
		return parsed.Scheme + "://" + parsed.Host + "/api/knowledge/shares/" + url.PathEscape(knowledgeID) + "?intent=import", knowledgeID, nil
	}
	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required when share_link is not provided")
	}
	if knowledgeID == "" {
		return "", "", fmt.Errorf("knowledge_id is required")
	}
	parsed, err := url.Parse(hubURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	return parsed.Scheme + "://" + parsed.Host + "/api/knowledge/shares/" + url.PathEscape(knowledgeID) + "?intent=import", knowledgeID, nil
}

func knowledgeShareAuthorizationHeader(hubToken, authorization string) string {
	authorization = strings.TrimSpace(authorization)
	if authorization != "" {
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			return authorization
		}
		return "Bearer " + authorization
	}
	hubToken = strings.TrimSpace(hubToken)
	if hubToken == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(hubToken), "bearer ") {
		return hubToken
	}
	return "Bearer " + hubToken
}

func fetchKnowledgeShareMetadata(ctx context.Context, apiURL, authorization string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := knowledgeShareClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge share: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("knowledge share resolver returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode knowledge share metadata: %w", err)
	}
	return out, nil
}

func knowledgeSharePackageURL(apiURL string, share map[string]interface{}) string {
	raw, _ := share["package_url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(apiURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func fetchKnowledgePackage(ctx context.Context, packageURL, authorization string) (knowledgePackage, error) {
	var pkg knowledgePackage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return pkg, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := knowledgeShareClient.Do(req)
	if err != nil {
		return pkg, fmt.Errorf("fetch knowledge package: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKnowledgePackageJSONBodyBytes+1))
	if err != nil {
		return pkg, err
	}
	if int64(len(body)) > maxKnowledgePackageJSONBodyBytes {
		return pkg, fmt.Errorf("knowledge package is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pkg, fmt.Errorf("knowledge package download returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return pkg, fmt.Errorf("decode knowledge package: %w", err)
	}
	return pkg, nil
}

// maxExportPackageContentBytes caps total inline content in an export package to prevent
// excessively large JSON payloads. Individual sources are capped at maxExportSourceContentBytes.
// Keep these aligned with the GUI knowledge package limits so server exports are not much
// less complete than desktop shares.
const maxExportPackageContentBytes = 40 * 1024 * 1024 // 40 MB total
const maxExportSourceContentBytes = 16 * 1024 * 1024  // 16 MB per source
const maxExportSourceNodes = 5000

func buildKnowledgeExportPackageWithStore(ctx context.Context, store *knowledge.SQLiteStore, dataRoot string, p agentservice.Principal, title, description string, sources []knowledge.Source) knowledgePackage {
	now := time.Now().UTC()
	packageID := fmt.Sprintf("kxp_%s_%d", now.Format("20060102T150405Z"), now.UnixNano())
	items := make([]knowledgePackageSource, 0, len(sources))
	remainingContentBytes := maxExportPackageContentBytes
	for _, source := range sanitizeKnowledgeSourcesForAPI(dataRoot, sources) {
		item := knowledgePackageSource{
			ID:           source.ID,
			Kind:         source.Kind,
			URI:          source.URI,
			CanonicalURI: source.CanonicalURI,
			Title:        source.Title,
			Author:       source.Author,
			SiteName:     source.SiteName,
			TopicHint:    source.TopicHint,
			Labels:       append([]string(nil), source.Labels...),
			Status:       source.Status,
			RelativePath: source.RelativePath,
			BatchID:      source.BatchID,
			ContentHash:  source.ContentHash,
			NodeCount:    source.NodeCount,
			CardCount:    source.CardCount,
			FactCount:    source.FactCount,
			CreatedAt:    knowledgePackageTime(source.CreatedAt),
			UpdatedAt:    knowledgePackageTime(source.UpdatedAt),
		}
		// Include inline content from document nodes so the package is self-contained.
		// URL sources that fail to re-fetch on import will fall back to this content.
		if store != nil && strings.TrimSpace(source.ID) != "" && remainingContentBytes > 0 && ctx.Err() == nil {
			content, truncated := exportPackageSourceContent(ctx, store, source, remainingContentBytes)
			if content != "" {
				item.Content = content
				item.ContentTruncated = truncated
				remainingContentBytes -= len([]byte(content))
				if remainingContentBytes < 0 {
					remainingContentBytes = 0
				}
			}
		}
		items = append(items, item)
	}
	pkg := knowledgePackage{
		Manifest: knowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   packageID,
			Title:       title,
			Description: description,
			CreatedAt:   now.Format(time.RFC3339),
			TenantID:    p.TenantID,
			OwnerID:     p.UserID,
			SourceCount: len(items),
			Editable:    true,
			Notes:       "Editable JSON package. Includes inline content for URL sources as fallback when re-fetch fails on import.",
		},
		Sources: items,
	}
	fitKnowledgeExportPackageJSON(&pkg, maxKnowledgePackageJSONBodyBytes)
	return pkg
}

func fitKnowledgeExportPackageJSON(pkg *knowledgePackage, limitBytes int64) {
	if pkg == nil || limitBytes <= 0 {
		return
	}
	for attempts := 0; attempts < len(pkg.Sources)*4+16; attempts++ {
		raw, err := json.Marshal(pkg)
		if err != nil || int64(len(raw)) <= limitBytes {
			return
		}
		idx := largestKnowledgePackageContentSource(pkg.Sources)
		if idx < 0 {
			return
		}
		currentBytes := len([]byte(pkg.Sources[idx].Content))
		if currentBytes == 0 {
			return
		}
		excess := int64(len(raw)) - limitBytes
		cutBytes := int(excess) + (1 << 20)
		minCut := currentBytes / 10
		if minCut < 64<<10 {
			minCut = 64 << 10
		}
		if cutBytes < minCut {
			cutBytes = minCut
		}
		nextBytes := currentBytes - cutBytes
		if nextBytes < 0 {
			nextBytes = 0
		}
		pkg.Sources[idx].Content = truncateUTF8ToBytes(pkg.Sources[idx].Content, nextBytes)
		pkg.Sources[idx].ContentTruncated = true
		if nextBytes == 0 {
			return
		}
	}
}

func largestKnowledgePackageContentSource(sources []knowledgePackageSource) int {
	idx := -1
	maxBytes := 0
	for i, source := range sources {
		contentBytes := len([]byte(source.Content))
		if contentBytes > maxBytes {
			idx = i
			maxBytes = contentBytes
		}
	}
	return idx
}

// exportPackageSourceContent reads document nodes for a source and returns concatenated
// text content. Returns (content, truncated). This ensures export packages are self-contained.
func exportPackageSourceContent(ctx context.Context, store *knowledge.SQLiteStore, source knowledge.Source, remainingBudget int) (string, bool) {
	if store == nil || remainingBudget <= 0 {
		return "", false
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, maxExportSourceNodes)
	if err != nil || len(nodes) == 0 {
		return "", false
	}
	truncated := source.NodeCount > len(nodes) && len(nodes) >= maxExportSourceNodes
	limit := maxExportSourceContentBytes
	if remainingBudget < limit {
		limit = remainingBudget
	}
	var builder strings.Builder
	used := 0
	for _, node := range nodes {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		separatorBytes := 0
		if used > 0 {
			separatorBytes = 2 // "\n\n"
		}
		available := limit - used - separatorBytes
		if available <= 0 {
			truncated = true
			break
		}
		textBytes := len([]byte(text))
		if textBytes > available {
			// Truncate at rune boundary using linear scan (avoids O(n²) string rebuilds).
			text = truncateUTF8ToBytes(text, available)
			truncated = true
		}
		if text == "" {
			break
		}
		if used > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(text)
		used += separatorBytes + len([]byte(text))
		if truncated {
			break
		}
	}
	return builder.String(), truncated
}

// truncateUTF8ToBytes truncates a UTF-8 string to at most maxBytes without breaking
// multi-byte rune boundaries. O(n) single-pass.
func truncateUTF8ToBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// Simple linear walk: advance one rune at a time until budget is exceeded.
	pos := 0
	for pos < maxBytes {
		// Leading byte tells us the rune width without decoding.
		b := s[pos]
		var size int
		switch {
		case b < 0x80:
			size = 1
		case b < 0xE0:
			size = 2
		case b < 0xF0:
			size = 3
		default:
			size = 4
		}
		if pos+size > maxBytes {
			break
		}
		pos += size
	}
	return s[:pos]
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func knowledgePackageTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func knowledgePackageFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

type userKnowledgeImportBatch struct {
	ID            string    `json:"id"`
	DisplayName   string    `json:"display_name"`
	RootName      string    `json:"root_name,omitempty"`
	Status        string    `json:"status"`
	TopicHint     string    `json:"topic_hint,omitempty"`
	Recursive     bool      `json:"recursive"`
	TotalFiles    int       `json:"total_files"`
	QueuedFiles   int       `json:"queued_files"`
	ImportedFiles int       `json:"imported_files"`
	SkippedFiles  int       `json:"skipped_files"`
	FailedFiles   int       `json:"failed_files"`
	SampleFiles   []string  `json:"sample_files,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s *HTTPServer) handleKnowledgeImportBatches(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	limit := parsePositiveIntQuery(r, "limit", 10, 50)
	page := parsePositiveIntQuery(r, "page", 1, 1000000)
	offset := (page - 1) * limit
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := s.knowledgeMgr.Store().ListImportBatchesPage(ctx, knowledge.ListImportBatchesOptions{
		TenantID: p.TenantID,
		OwnerID:  p.UserID,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("list import batches failed: %v", err))})
		return
	}
	items := make([]userKnowledgeImportBatch, 0, len(out.Batches))
	for _, batch := range out.Batches {
		items = append(items, s.userKnowledgeImportBatchSummary(ctx, batch))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  out.Total,
		"page":   page,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *HTTPServer) handleKnowledgeDeleteImportBatch(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	batchID := strings.TrimSpace(r.PathValue("batchId"))
	if batchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "batchId is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out, err := s.knowledgeMgr.Store().DeleteImportBatch(ctx, knowledge.ImportBatchDeleteRequest{
		BatchID:  batchID,
		TenantID: p.TenantID,
		OwnerID:  p.UserID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "batch not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("delete import batch failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "batch_id": out.BatchID, "deleted_sources": out.DeletedSources, "deleted_batches": out.DeletedBatches})
}

func parsePositiveIntQuery(r *http.Request, key string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 1
	}
	value := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get(key)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			value = n
		}
	}
	if max > 0 && value > max {
		value = max
	}
	return value
}

func (s *HTTPServer) userKnowledgeImportBatchSummary(ctx context.Context, batch knowledge.ImportBatch) userKnowledgeImportBatch {
	summary := userKnowledgeImportBatch{
		ID:            batch.ID,
		DisplayName:   knowledgeBatchDisplayName(batch, nil),
		RootName:      filepath.Base(batch.RootPath),
		Status:        batch.Status,
		TopicHint:     batch.TopicHint,
		Recursive:     batch.Recursive,
		TotalFiles:    batch.TotalFiles,
		QueuedFiles:   batch.QueuedFiles,
		ImportedFiles: batch.Imported,
		SkippedFiles:  batch.Skipped,
		FailedFiles:   batch.Failed,
		CreatedAt:     batch.CreatedAt,
		UpdatedAt:     batch.UpdatedAt,
	}
	items, err := s.knowledgeMgr.Store().ListImportItems(ctx, batch.ID, 4)
	if err == nil {
		summary.SampleFiles = knowledgeBatchSampleFiles(items)
		summary.DisplayName = knowledgeBatchDisplayName(batch, items)
	}
	return summary
}

func knowledgeBatchDisplayName(batch knowledge.ImportBatch, items []knowledge.ImportItem) string {
	if batch.TopicHint != "" {
		return batch.TopicHint
	}
	samples := knowledgeBatchSampleFiles(items)
	if len(samples) == 1 {
		return samples[0]
	}
	if len(samples) > 1 {
		return fmt.Sprintf("%s +%d", samples[0], len(samples)-1)
	}
	if base := strings.TrimSpace(filepath.Base(batch.RootPath)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return batch.ID
}

func knowledgeBatchSampleFiles(items []knowledge.ImportItem) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		name := strings.TrimSpace(item.RelativePath)
		if name == "" {
			name = filepath.Base(item.FilePath)
		}
		if name == "" || name == "." || name == string(filepath.Separator) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
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
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge store is not configured"})
		return
	}
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

func (s *HTTPServer) handleKnowledgeSearchStructured(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req knowledge.StructuredSearchOptions
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	req.OwnerID = p.UserID
	req.TenantID = p.TenantID
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	store := s.knowledgeMgr.AgentStore()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge store is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	results, err := store.SearchStructured(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("structured search failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": sanitizeKnowledgeSearchResultsForAPI(s.svc.DataRoot(), results),
		"total":   len(results),
	})
}

func (s *HTTPServer) handleKnowledgeStructuredCatalog(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	var req knowledge.StructuredCatalogOptions
	if err := readJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	req.OwnerID = p.UserID
	req.TenantID = p.TenantID
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	store := s.knowledgeMgr.AgentStore()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge store is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := store.StructuredCatalog(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), fmt.Sprintf("structured catalog failed: %v", err))})
		return
	}
	writeJSON(w, http.StatusOK, result)
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
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge store is not configured"})
		return
	}
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
	result.LastItemPath = redactKnowledgePathForAPI(dataRoot, result.LastItemPath)
	result.LastItemReason = redactSupportBundleText(dataRoot, result.LastItemReason)
	for i := range result.Warnings {
		result.Warnings[i] = redactSupportBundleText(dataRoot, result.Warnings[i])
	}
	for i := range result.Items {
		result.Items[i].FilePath = redactKnowledgePathForAPI(dataRoot, result.Items[i].FilePath)
		result.Items[i].RelativePath = redactKnowledgePathForAPI(dataRoot, result.Items[i].RelativePath)
		result.Items[i].ErrorMessage = redactSupportBundleText(dataRoot, result.Items[i].ErrorMessage)
	}
	for i := range result.FailedItems {
		result.FailedItems[i].FilePath = redactKnowledgePathForAPI(dataRoot, result.FailedItems[i].FilePath)
		result.FailedItems[i].Error = redactSupportBundleText(dataRoot, result.FailedItems[i].Error)
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
	access := s.knowledgeMgr.Access()
	if access == nil {
		return false
	}
	for _, scope := range access.ResolveForUser(ctx, p.TenantID, p.UserID) {
		if source.TenantID == scope.TenantID && source.OwnerID == scope.OwnerID {
			return true
		}
	}
	return false
}

func (s *HTTPServer) listReadableKnowledgeSources(ctx context.Context, p agentservice.Principal) ([]knowledge.Source, error) {
	store := s.knowledgeMgr.Store()
	if store == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	var merged []knowledge.Source
	seen := map[string]struct{}{}
	scopes := []knowledgeScope(nil)
	tenantID := strings.TrimSpace(p.TenantID)
	userID := strings.TrimSpace(p.UserID)
	if tenantID != "" && userID != "" {
		scopes = []knowledgeScope{{TenantID: tenantID, OwnerID: userID, Name: "self"}}
	}
	if access := s.knowledgeMgr.Access(); access != nil {
		scopes = access.ResolveForUser(ctx, p.TenantID, p.UserID)
	}
	for _, scope := range scopes {
		opts := knowledge.ListSourcesOptions{OwnerID: scope.OwnerID, TenantID: scope.TenantID, Limit: maxReadableKnowledgeSourcesPerScope, IncludeDisabled: true}
		if scope.TenantID != tenantID || scope.OwnerID != userID {
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
	sourceTenantID := strings.TrimSpace(source.TenantID)
	sourceOwnerID := strings.TrimSpace(source.OwnerID)
	principalTenantID := strings.TrimSpace(p.TenantID)
	principalUserID := strings.TrimSpace(p.UserID)
	return sourceTenantID != "" && sourceOwnerID != "" && sourceTenantID == principalTenantID && sourceOwnerID == principalUserID
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
	if store := s.knowledgeMgr.Store(); store != nil {
		stats.VectorIndex = store.VectorIndexStats()
	}

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
			return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == ',' || r == ';' || r == '\uFF0C' || r == '\uFF1B' || r == '\u3001'
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
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	var in struct {
		AdminPassword string `json:"admin_password,omitempty"`
		Password      string `json:"password,omitempty"`
		AdminSecret   string `json:"admin_secret,omitempty"`
	}
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	password := in.AdminPassword
	if password == "" {
		password = in.Password
	}
	authType, ok, err := s.adminOwnerSecretOrPasswordAuthorized(in.AdminSecret, password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !ok {
		_ = s.recordAdminAudit(r.Context(), "admin.knowledge_user_clear_failed", "knowledge", p.TenantID+"/"+p.UserID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "reason": "invalid_admin_authorization", "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin credential"})
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
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_user_cleared", "knowledge", p.TenantID+"/"+p.UserID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "deleted": fmt.Sprint(deleted), "auth_type": authType, "remote_ip": requestClientIP(r)})
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
		sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID, Limit: maxReadableKnowledgeSourcesPerScope, IncludeDisabled: true})
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
	sources, err := s.knowledgeMgr.Store().ListSources(r.Context(), knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID, Limit: maxReadableKnowledgeSourcesPerScope, IncludeDisabled: true})
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
	if store == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "thumbnail not found"})
		return
	}
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
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{IncludeDisabled: true})
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
	return readJSONBodyWithLimit(r, v, maxJSONBodyBytes)
}

func readJSONBodyWithLimit(r *http.Request, v interface{}, limit int64) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("request body too large")
	}
	if len(body) == 0 {
		return fmt.Errorf("empty request body")
	}
	return json.Unmarshal(body, v)
}

// --- Image asset endpoints ---

func (s *HTTPServer) handleKnowledgeSourceThumbnail(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	sourceID := r.PathValue("sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceId is required"})
		return
	}
	store := s.knowledgeMgr.Store()
	if store == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if !s.canReadKnowledgeSourceID(ctx, store, sourceID, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "thumbnail not found"})
		return
	}
	assets := store.ImageAssets()
	if assets == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image assets not configured"})
		return
	}
	thumbPath := assets.ThumbPath(sourceID)
	if thumbPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "thumbnail not found"})
		return
	}
	if _, err := os.Stat(thumbPath); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "thumbnail not found"})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, thumbPath)
}

func (s *HTTPServer) handleKnowledgeSourceImage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
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
	if !s.canReadKnowledgeSourceID(ctx, store, sourceID, p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
		return
	}
	assets := store.ImageAssets()
	if assets == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image assets not configured"})
		return
	}
	assetDir := assets.AssetDir(sourceID)
	if assetDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
		return
	}
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
		return
	}
	var originalPath string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "original") {
			originalPath = filepath.Join(assetDir, entry.Name())
			break
		}
	}
	if originalPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "original image not found"})
		return
	}
	ext := strings.ToLower(filepath.Ext(originalPath))
	contentType := "application/octet-stream"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".bmp":
		contentType = "image/bmp"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, originalPath)
}

func (s *HTTPServer) handleKnowledgeImportImage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	maxSize := knowledgeMaxUploadSize
	r.Body = http.MaxBytesReader(w, r.Body, knowledgeMultipartRequestLimitForFiles(maxSize, 1))
	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid multipart form"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image file is required (form field: image)"})
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !knowledge.IsImageExt(ext) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unsupported image format: %s", ext)})
		return
	}
	tmpDir := filepath.Join(s.svc.DataRoot(), "knowledge", "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	tmpFile, err := os.CreateTemp(tmpDir, "img-upload-*"+ext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	written, err := io.Copy(tmpFile, io.LimitReader(file, maxSize+1))
	if err != nil {
		tmpFile.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save uploaded file"})
		return
	}
	if written > maxSize {
		tmpFile.Close()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image file is too large"})
		return
	}
	tmpFile.Close()
	store := s.knowledgeMgr.Store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge store is not configured"})
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, ext)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := store.ImportDirectory(ctx, knowledge.DirectoryImportRequest{
		RootPath:    filepath.Dir(tmpPath),
		OwnerID:     p.UserID,
		TenantID:    p.TenantID,
		TopicHint:   title,
		IncludeExts: []string{ext},
		Recursive:   false,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("import failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"imported": result.ImportedFiles,
		"title":    title,
	})
}

func (s *HTTPServer) canReadKnowledgeSourceID(ctx context.Context, store *knowledge.SQLiteStore, sourceID string, p agentservice.Principal) bool {
	if store == nil {
		return false
	}
	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		return false
	}
	return s.canReadSource(ctx, source, p)
}
