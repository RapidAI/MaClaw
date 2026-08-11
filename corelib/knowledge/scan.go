package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func NewID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

func NormalizeDirectoryImportRequest(req DirectoryImportRequest) DirectoryImportRequest {
	// This request travels through scanning, batching and asynchronous import
	// preparation. Keep its trusted parser policy independent from the host's
	// mutable config object for the entire operation.
	req.OfficeReadConfig = agent.CloneOfficeReadConfigPtr(req.OfficeReadConfig)
	req.RootPath = strings.TrimSpace(req.RootPath)
	req.OwnerID = strings.TrimSpace(req.OwnerID)
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ProjectPath = strings.TrimSpace(req.ProjectPath)
	req.TopicHint = strings.TrimSpace(req.TopicHint)
	req.SaveScope = strings.TrimSpace(req.SaveScope)
	req.DistillMode = NormalizeDistillMode(req.DistillMode)
	if req.SaveScope == "" {
		req.SaveScope = SaveScopeProject
	}
	if len(req.IncludeExts) == 0 {
		req.IncludeExts = append([]string(nil), DefaultIncludeExts...)
	}
	req.IncludeExts = normalizeIncludeExts(req.IncludeExts)
	req.ExcludeGlobs = normalizeImportExcludeGlobs(req.ExcludeGlobs)
	if req.MaxFileBytes <= 0 {
		req.MaxFileBytes = DefaultMaxFileBytes
	}
	return req
}

// ScanDirectory scans a directory and classifies files for import. The caller
// can pass existingHashes to mark already imported files as duplicates.
func ScanDirectory(ctx context.Context, req DirectoryImportRequest, existingHashes map[string]struct{}) (DirectoryImportResult, []ImportItem, error) {
	return ScanDirectoryProgress(ctx, req, existingHashes, nil)
}

// ScanDirectoryProgress is ScanDirectory with optional progress callbacks
// (phase "walk" then "hash").
func ScanDirectoryProgress(ctx context.Context, req DirectoryImportRequest, existingHashes map[string]struct{}, onProgress ScanProgressFunc) (DirectoryImportResult, []ImportItem, error) {
	req = NormalizeDirectoryImportRequest(req)
	if req.RootPath == "" {
		return DirectoryImportResult{}, nil, fmt.Errorf("root path is required")
	}
	root, err := filepath.Abs(req.RootPath)
	if err != nil {
		return DirectoryImportResult{}, nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return DirectoryImportResult{}, nil, err
	}
	if !info.IsDir() {
		return DirectoryImportResult{}, nil, fmt.Errorf("%s is not a directory", root)
	}

	exts := make(map[string]struct{}, len(req.IncludeExts))
	for _, ext := range req.IncludeExts {
		if ext != "" {
			exts[normalizeExt(ext)] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	for h := range existingHashes {
		seen[h] = struct{}{}
	}

	now := time.Now().UTC()
	result := DirectoryImportResult{Status: ImportStatusScanned, RootPath: root}
	items := make([]ImportItem, 0)
	// Collect hashable candidates during walk; hash them in parallel afterwards.
	candidates := make([]scanHashCandidate, 0)

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Warnings = append(result.Warnings, walkErr.Error())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if path != root && shouldExcludeImportPath(root, path, req.ExcludeGlobs) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedExcluded, "excluded by exclude_globs"))
			return nil
		}

		if path != root && shouldSkipName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedHidden, "hidden or temporary file"))
			return nil
		}

		if d.IsDir() {
			if path != root && !req.Recursive {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			result.TotalFiles++
			result.FailedFiles++
			items = append(items, newFailedImportItem(root, path, now, err))
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedSymlink, "symbolic links are skipped"))
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		result.TotalFiles++
		ext := normalizeExt(filepath.Ext(path))
		kind := kindForExt(ext)
		if _, ok := exts[ext]; !ok || kind == "" {
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedType, "unsupported file type"))
			return nil
		}
		if result.ExtCounts == nil {
			result.ExtCounts = make(map[string]int)
		}
		result.ExtCounts[ext]++
		if info.Size() > maxKnowledgeImportFileBytesForKind(req.MaxFileBytes, kind) {
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedTooLarge, "file exceeds max_file_bytes")
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			return nil
		}

		candidates = append(candidates, scanHashCandidate{
			path: path,
			rel:  relativePath(root, path),
			kind: kind,
			size: info.Size(),
		})
		if onProgress != nil && len(candidates)%16 == 0 {
			onProgress("walk", len(candidates), 0, path)
		}
		return nil
	})
	if walkErr != nil {
		return result, items, walkErr
	}
	if onProgress != nil {
		onProgress("walk", len(candidates), len(candidates), "")
	}

	if err := hashScanCandidates(ctx, candidates, onProgress); err != nil {
		return result, items, err
	}
	result, items = appendHashedCandidates(result, items, candidates, seen, now)

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].RelativePath < items[j].RelativePath
	})
	result.Items = items
	return result, items, nil
}

func ScanFiles(ctx context.Context, req DirectoryImportRequest, filePaths []string, existingHashes map[string]struct{}) (DirectoryImportResult, []ImportItem, error) {
	return ScanFilesProgress(ctx, req, filePaths, existingHashes, nil)
}

// ScanFilesProgress is ScanFiles with optional progress callbacks.
func ScanFilesProgress(ctx context.Context, req DirectoryImportRequest, filePaths []string, existingHashes map[string]struct{}, onProgress ScanProgressFunc) (DirectoryImportResult, []ImportItem, error) {
	req = NormalizeDirectoryImportRequest(req)
	filePaths = splitFilePathInputs(filePaths)
	cleaned := make([]string, 0, len(filePaths))
	for _, filePath := range filePaths {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}
		abs, err := filepath.Abs(filePath)
		if err != nil {
			return DirectoryImportResult{}, nil, err
		}
		cleaned = append(cleaned, abs)
	}
	if len(cleaned) == 0 {
		return DirectoryImportResult{}, nil, fmt.Errorf("at least one file path is required")
	}

	root := strings.TrimSpace(req.RootPath)
	if root != "" {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return DirectoryImportResult{}, nil, err
		}
		root = absRoot
	} else {
		root = commonRootForFiles(cleaned)
	}

	exts := make(map[string]struct{}, len(req.IncludeExts))
	for _, ext := range req.IncludeExts {
		if ext != "" {
			exts[normalizeExt(ext)] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	for h := range existingHashes {
		seen[h] = struct{}{}
	}

	now := time.Now().UTC()
	result := DirectoryImportResult{Status: ImportStatusScanned, RootPath: root}
	items := make([]ImportItem, 0, len(cleaned))

	candidates := make([]scanHashCandidate, 0, len(cleaned))
	for _, path := range cleaned {
		select {
		case <-ctx.Done():
			return result, items, ctx.Err()
		default:
		}

		info, err := os.Stat(path)
		if err != nil {
			result.TotalFiles++
			result.FailedFiles++
			items = append(items, newFailedImportItem(root, path, now, err))
			continue
		}
		if shouldExcludeImportPath(root, path, req.ExcludeGlobs) {
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedExcluded, "excluded by exclude_globs"))
			continue
		}
		if shouldSkipName(filepath.Base(path)) {
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedHidden, "hidden or temporary file"))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedSymlink, "symbolic links are skipped"))
			continue
		}
		if !info.Mode().IsRegular() {
			result.TotalFiles++
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedType, "selected path is not a regular file"))
			continue
		}

		result.TotalFiles++
		ext := normalizeExt(filepath.Ext(path))
		kind := kindForExt(ext)
		if _, ok := exts[ext]; !ok || kind == "" {
			result.SkippedFiles++
			items = append(items, newSkippedImportItem(root, path, now, ItemStatusSkippedType, "unsupported file type"))
			continue
		}
		if result.ExtCounts == nil {
			result.ExtCounts = make(map[string]int)
		}
		result.ExtCounts[ext]++
		if info.Size() > maxKnowledgeImportFileBytesForKind(req.MaxFileBytes, kind) {
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedTooLarge, "file exceeds max_file_bytes")
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			continue
		}

		candidates = append(candidates, scanHashCandidate{
			path: path,
			rel:  relativePath(root, path),
			kind: kind,
			size: info.Size(),
		})
	}

	if onProgress != nil {
		onProgress("walk", len(candidates), len(candidates), "")
	}
	if err := hashScanCandidates(ctx, candidates, onProgress); err != nil {
		return result, items, err
	}
	result, items = appendHashedCandidates(result, items, candidates, seen, now)

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].RelativePath < items[j].RelativePath
	})
	result.Items = items
	return result, items, nil
}

// scanHashCandidate is a file that passed metadata filters and still needs content hashing.
type scanHashCandidate struct {
	path string
	rel  string
	kind string
	size int64
	hash string
	err  error
}

// hashScanCandidates SHA-256s candidate files in parallel (I/O + CPU bound).
// Preserves slice order; duplicate detection remains sequential afterward.
func hashScanCandidates(ctx context.Context, candidates []scanHashCandidate, onProgress ScanProgressFunc) error {
	n := len(candidates)
	if n == 0 {
		return nil
	}
	// Hashing is parallel, but a progress callback belongs to one consumer (the
	// GUI event bridge in production).  Calling it from multiple workers makes
	// even a simple callback-owned counter or slice race.  Keep hashing fully
	// concurrent while serializing only the short notification boundary.
	var progressMu sync.Mutex
	report := func(done int, path string) {
		if onProgress == nil {
			return
		}
		if done == n || done == 1 || done%8 == 0 {
			progressMu.Lock()
			defer progressMu.Unlock()
			onProgress("hash", done, n, path)
		}
	}
	if n == 1 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		candidates[0].hash, candidates[0].err = fileSHA256(candidates[0].path)
		report(1, candidates[0].path)
		return nil
	}

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > n {
		workers = n
	}

	jobs := make(chan int, n)
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	var doneCount int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					errOnce.Do(func() { firstErr = ctx.Err() })
					return
				}
				h, err := fileSHA256(candidates[i].path)
				candidates[i].hash = h
				candidates[i].err = err
				d := int(atomic.AddInt64(&doneCount, 1))
				report(d, candidates[i].path)
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// appendHashedCandidates applies walk-order duplicate detection after parallel hashing.
func appendHashedCandidates(
	result DirectoryImportResult,
	items []ImportItem,
	candidates []scanHashCandidate,
	seen map[string]struct{},
	now time.Time,
) (DirectoryImportResult, []ImportItem) {
	if seen == nil {
		seen = make(map[string]struct{})
	}
	for _, c := range candidates {
		if c.err != nil {
			result.FailedFiles++
			item := newFailedImportItem(result.RootPath, c.path, now, c.err)
			item.FileSize = c.size
			item.Kind = c.kind
			items = append(items, item)
			continue
		}
		if _, dup := seen[c.hash]; dup {
			result.DuplicateFiles++
			result.SkippedFiles++
			item := newSkippedImportItem(result.RootPath, c.path, now, ItemStatusSkippedDuplicate, "duplicate content hash")
			item.FileHash = c.hash
			item.FileSize = c.size
			item.Kind = c.kind
			items = append(items, item)
			continue
		}
		seen[c.hash] = struct{}{}
		item := ImportItem{
			ID:           NewID("kii"),
			FilePath:     c.path,
			RelativePath: c.rel,
			FileHash:     c.hash,
			FileSize:     c.size,
			Kind:         c.kind,
			Status:       ItemStatusQueued,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		result.QueuedFiles++
		result.EstimatedBytes += c.size
		items = append(items, item)
	}
	return result, items
}

func splitFilePathInputs(values []string) []string {
	paths := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isFilePathInputSeparator) {
			part = strings.TrimSpace(part)
			if part != "" {
				paths = append(paths, part)
			}
		}
	}
	return paths
}

func isFilePathInputSeparator(r rune) bool {
	return r == '\n' || r == '\r' || r == '\t'
}

func commonRootForFiles(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	root := filepath.Dir(paths[0])
	for _, path := range paths[1:] {
		dir := filepath.Dir(path)
		for {
			rel, err := filepath.Rel(root, dir)
			if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))) {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				return root
			}
			root = parent
		}
	}
	return root
}

func BuildSourceFromImport(req DirectoryImportRequest, batchID string, item ImportItem) Source {
	now := time.Now().UTC()
	title := strings.TrimSuffix(filepath.Base(item.FilePath), filepath.Ext(item.FilePath))
	status := StatusPending
	if isImmediatelyParsedKind(item.Kind) {
		status = StatusParsed
	}
	return Source{
		ID:           NewID("ksrc"),
		Kind:         item.Kind,
		URI:          item.FilePath,
		Title:        title,
		FetchedAt:    now,
		ContentHash:  item.FileHash,
		OwnerID:      req.OwnerID,
		TenantID:     req.TenantID,
		ProjectPath:  req.ProjectPath,
		TopicHint:    req.TopicHint,
		SourceTrust:  0.9,
		BatchID:      batchID,
		RelativePath: item.RelativePath,
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func BuildSourceFromURL(rawURL, ownerID, tenantID, projectPath, topicHint string) (Source, error) {
	u, err := ValidatePublicHTTPURL(rawURL)
	if err != nil {
		return Source{}, err
	}
	now := time.Now().UTC()
	return Source{
		ID:           NewID("ksrc"),
		Kind:         SourceKindURL,
		URI:          u.String(),
		CanonicalURI: u.String(),
		Title:        u.Hostname(),
		FetchedAt:    now,
		ContentHash:  sha256String(u.String()),
		OwnerID:      strings.TrimSpace(ownerID),
		TenantID:     strings.TrimSpace(tenantID),
		ProjectPath:  strings.TrimSpace(projectPath),
		TopicHint:    strings.TrimSpace(topicHint),
		SourceTrust:  0.6,
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func kindForExt(ext string) string {
	switch normalizeExt(ext) {
	case ".docx":
		return SourceKindDOCX
	case ".doc":
		return SourceKindDOC
	case ".pdf":
		return SourceKindPDF
	case ".pptx":
		return SourceKindPPTX
	case ".ppt":
		return SourceKindPPT
	case ".xlsx":
		return SourceKindXLSX
	case ".csv":
		return SourceKindCSV
	case ".xls":
		return SourceKindXLS
	case ".md", ".markdown":
		return SourceKindMarkdown
	case ".txt", ".text":
		return SourceKindText
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return SourceKindImage
	default:
		return ""
	}
}

func isImmediatelyParsedKind(kind string) bool {
	switch kind {
	case SourceKindMarkdown, SourceKindText, SourceKindDOCX, SourceKindDOC, SourceKindPDF, SourceKindPPT, SourceKindPPTX, SourceKindXLSX, SourceKindXLS, SourceKindCSV, SourceKindImage:
		return true
	default:
		return false
	}
}

// maxKnowledgeImportFileBytesForKind preserves a caller's lower import cap,
// while preventing Office containers, CSV grids, and PDFs from entering a
// knowledge workflow above the common 32 MiB extraction boundary. PDF OCR
// reads the source into memory and can rasterize pages, so allowing the wider
// generic import ceiling here would bypass the same resource guard used by the
// other parser-heavy formats.
func maxKnowledgeImportFileBytesForKind(requested int64, kind string) int64 {
	if requested <= 0 {
		requested = DefaultMaxFileBytes
	}
	switch kind {
	case SourceKindDOC, SourceKindDOCX, SourceKindXLS, SourceKindXLSX, SourceKindPPT, SourceKindPPTX, SourceKindCSV, SourceKindPDF:
		if requested > agent.MaxOfficeReadFileBytes {
			return agent.MaxOfficeReadFileBytes
		}
	}
	return requested
}

func normalizeExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func normalizeImportExcludeGlobs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			pattern := strings.Trim(strings.TrimSpace(filepath.ToSlash(part)), "/")
			if pattern == "" {
				continue
			}
			if _, ok := seen[pattern]; ok {
				continue
			}
			seen[pattern] = struct{}{}
			out = append(out, pattern)
		}
	}
	return out
}

func shouldExcludeImportPath(root, candidate string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		rel = filepath.Base(candidate)
	}
	rel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if rel == "" || rel == "." {
		return false
	}
	base := pathpkg.Base(rel)
	for _, pattern := range patterns {
		if importGlobMatches(pattern, rel, base) {
			return true
		}
	}
	return false
}

func importGlobMatches(pattern, rel, base string) bool {
	pattern = strings.Trim(strings.TrimSpace(filepath.ToSlash(pattern)), "/")
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	if ok, err := pathpkg.Match(pattern, rel); err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		if ok, err := pathpkg.Match(pattern, base); err == nil && ok {
			return true
		}
	}
	return false
}

func normalizeIncludeExts(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	exts := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			ext := normalizeExt(part)
			if ext == "" {
				continue
			}
			if _, ok := seen[ext]; ok {
				continue
			}
			seen[ext] = struct{}{}
			exts = append(exts, ext)
		}
	}
	return exts
}

func shouldSkipName(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".temp") || strings.HasSuffix(lower, ".bak")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// boundedDocumentSHA256 is the refresh-time identity check for files that
// subsequently enter the shared document snapshot boundary. Unlike generic
// scan hashing, it must not read an arbitrarily large current pathname merely
// to decide whether a 32 MiB-bounded parser may run.
func boundedDocumentSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, agent.MaxOfficeReadFileBytes+1))
	if err != nil {
		return "", err
	}
	if n > agent.MaxOfficeReadFileBytes {
		return "", agent.ErrOfficeReadInputTooLarge
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256String(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}

func newSkippedImportItem(root, path string, now time.Time, status, reason string) ImportItem {
	return ImportItem{ID: NewID("kii"), FilePath: path, RelativePath: relativePath(root, path), Status: status, ErrorMessage: reason, CreatedAt: now, UpdatedAt: now}
}

func newFailedImportItem(root, path string, now time.Time, err error) ImportItem {
	return ImportItem{ID: NewID("kii"), FilePath: path, RelativePath: relativePath(root, path), Status: ItemStatusFailed, ErrorMessage: err.Error(), CreatedAt: now, UpdatedAt: now}
}

func simpleTextNode(source Source, text string) DocumentNode {
	return DocumentNode{
		ID:         NewID("kdn"),
		SourceID:   source.ID,
		Type:       "document",
		Title:      source.Title,
		Text:       text,
		Metadata:   map[string]string{"relative_path": source.RelativePath},
		TokenCount: estimateTokens(text),
	}
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len([]rune(text))/2 + 1
}
