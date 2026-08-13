package digitalasset

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ExtractZipSafely extracts zipPath into destDir with Zip Slip protection and size limits.
func ExtractZipSafely(zipPath, destDir string, maxFiles int, maxTotalBytes, maxSingleBytes int64, denyExts []string) (fileCount int, totalBytes int64, err error) {
	if maxFiles <= 0 {
		maxFiles = 5000
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = 1024 * 1024 * 1024
	}
	if maxSingleBytes <= 0 {
		maxSingleBytes = 50 * 1024 * 1024
	}
	deny := make(map[string]struct{}, len(denyExts))
	for _, ext := range denyExts {
		deny[strings.ToLower(strings.TrimSpace(ext))] = struct{}{}
	}
	result := archiveutil.ExtractToDirectoryWithPolicy(zipPath, destDir, archiveutil.Limits{
		MaxFiles:            maxFiles,
		MaxTotalBytes:       maxTotalBytes,
		MaxFileBytes:        maxSingleBytes,
		MaxCompressionRatio: 0,
	}, archiveutil.ExtractionPolicy{Filter: func(entry archiveutil.Entry) (bool, error) {
		if entry.Dir || strings.Contains(entry.Path, "__MACOSX/") || strings.HasSuffix(entry.Path, ".DS_Store") {
			return !strings.Contains(entry.Path, "__MACOSX/") && !strings.HasSuffix(entry.Path, ".DS_Store"), nil
		}
		_, denied := deny[strings.ToLower(filepath.Ext(entry.Path))]
		return !denied, nil
	}})
	if !result.OK {
		return 0, 0, fmt.Errorf("extract zip: %s: %s", result.Code, result.Message)
	}
	return result.Files, result.WrittenBytes, nil
}

// ImportArchiveZip starts an async job: extract zip then import the tree (with progress polling).
// The zip at zipPath is copied into a managed temp dir immediately so the HTTP handler can
// delete its upload temp file without racing the background extract.
func (s *Service) ImportArchiveZip(ctx context.Context, tenantID, libraryID, actor, zipPath string) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Host == nil {
		return nil, fmt.Errorf("knowledge host is nil")
	}
	zipPath = strings.TrimSpace(zipPath)
	if zipPath == "" {
		return nil, fmt.Errorf("zip path required")
	}
	label := filepath.Base(zipPath)
	if label == "" || label == "." || strings.HasPrefix(label, "dal-archive-") {
		// CreateTemp names are opaque; prefer a stable display label.
		label = "archive.zip"
	}
	// Job is created before extract so the UI can show progress during large unzips.
	job, err := s.createImportJob(ctx, tenantID, libraryID, actor, "archive", label, []string{label})
	if err != nil {
		return nil, err
	}

	tmp := s.Host.TmpDir(tenantID, "archive_"+job.ID)
	managedZip := filepath.Join(tmp, "source.zip")
	extract := filepath.Join(tmp, "extract")
	if err := os.MkdirAll(extract, 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		s.failJob(job, err)
		return job, err
	}
	if err := copyFileContents(zipPath, managedZip); err != nil {
		_ = os.RemoveAll(tmp)
		s.failJob(job, fmt.Errorf("stage archive: %w", err))
		return job, err
	}

	job.ProgressJSON = mustJSONString(map[string]any{
		"phase": "extracting", "percent": 5,
		"root_label": label, "file_names": []string{label},
		"message": "archive staged; extracting",
	})
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)

	go func() {
		defer os.RemoveAll(tmp)
		job.ProgressJSON = mustJSONString(map[string]any{
			"phase": "extracting", "percent": 8,
			"root_label": label, "file_names": []string{label},
			"message": "extracting archive",
		})
		job.UpdatedAt = time.Now().UTC()
		_ = s.Repo.UpdateJob(context.Background(), job)

		fileCount, totalBytes, err := ExtractZipSafely(managedZip, extract,
			s.Settings.MaxArchiveFileCount,
			s.Settings.MaxArchiveExtractedBytes,
			50*1024*1024,
			s.Settings.ArchiveDenyExtensions,
		)
		if err != nil {
			s.failJob(job, err)
			return
		}
		job.ProgressJSON = mustJSONString(map[string]any{
			"phase": "importing", "percent": 12,
			"root_label": label, "file_names": []string{label},
			"extracted_files": fileCount, "extracted_bytes": totalBytes,
			"message": "archive extracted; importing",
		})
		job.UpdatedAt = time.Now().UTC()
		_ = s.Repo.UpdateJob(context.Background(), job)

		s.runImportDirectory(context.Background(), job, extract, actor, "archive", []string{label}, "")
	}()
	return job, nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
