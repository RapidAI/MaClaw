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
	"sort"
	"strings"
	"time"
)

func NewID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

func NormalizeDirectoryImportRequest(req DirectoryImportRequest) DirectoryImportRequest {
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
		if info.Size() > req.MaxFileBytes {
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedTooLarge, "file exceeds max_file_bytes")
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			return nil
		}

		hash, err := fileSHA256(path)
		if err != nil {
			result.FailedFiles++
			item := newFailedImportItem(root, path, now, err)
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			return nil
		}
		if _, dup := seen[hash]; dup {
			result.DuplicateFiles++
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedDuplicate, "duplicate content hash")
			item.FileHash = hash
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			return nil
		}
		seen[hash] = struct{}{}

		item := ImportItem{
			ID:           NewID("kii"),
			FilePath:     path,
			RelativePath: relativePath(root, path),
			FileHash:     hash,
			FileSize:     info.Size(),
			Kind:         kind,
			Status:       ItemStatusQueued,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		result.QueuedFiles++
		result.EstimatedBytes += info.Size()
		items = append(items, item)
		return nil
	})
	if walkErr != nil {
		return result, items, walkErr
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].RelativePath < items[j].RelativePath
	})
	result.Items = items
	return result, items, nil
}

func ScanFiles(ctx context.Context, req DirectoryImportRequest, filePaths []string, existingHashes map[string]struct{}) (DirectoryImportResult, []ImportItem, error) {
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
		if info.Size() > req.MaxFileBytes {
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedTooLarge, "file exceeds max_file_bytes")
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			continue
		}

		hash, err := fileSHA256(path)
		if err != nil {
			result.FailedFiles++
			item := newFailedImportItem(root, path, now, err)
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			continue
		}
		if _, dup := seen[hash]; dup {
			result.DuplicateFiles++
			result.SkippedFiles++
			item := newSkippedImportItem(root, path, now, ItemStatusSkippedDuplicate, "duplicate content hash")
			item.FileHash = hash
			item.FileSize = info.Size()
			item.Kind = kind
			items = append(items, item)
			continue
		}
		seen[hash] = struct{}{}

		item := ImportItem{
			ID:           NewID("kii"),
			FilePath:     path,
			RelativePath: relativePath(root, path),
			FileHash:     hash,
			FileSize:     info.Size(),
			Kind:         kind,
			Status:       ItemStatusQueued,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		result.QueuedFiles++
		result.EstimatedBytes += info.Size()
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].RelativePath < items[j].RelativePath
	})
	result.Items = items
	return result, items, nil
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
	default:
		return ""
	}
}

func isImmediatelyParsedKind(kind string) bool {
	switch kind {
	case SourceKindMarkdown, SourceKindText, SourceKindDOCX, SourceKindPDF, SourceKindPPTX, SourceKindXLSX, SourceKindCSV:
		return true
	default:
		return false
	}
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
