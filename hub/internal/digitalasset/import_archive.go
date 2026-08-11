package digitalasset

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	deny := map[string]struct{}{}
	for _, e := range denyExts {
		deny[strings.ToLower(e)] = struct{}{}
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, 0, err
	}
	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return 0, 0, err
	}

	for _, f := range r.File {
		name := f.Name
		if strings.Contains(name, "__MACOSX/") || strings.HasSuffix(name, ".DS_Store") {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return 0, 0, fmt.Errorf("symlink not allowed: %s", name)
		}
		cleaned := filepath.Clean(filepath.FromSlash(name))
		if cleaned == "." || cleaned == "" {
			continue
		}
		if strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." || filepath.IsAbs(cleaned) {
			return 0, 0, fmt.Errorf("zip slip rejected: %s", name)
		}
		target := filepath.Join(destAbs, cleaned)
		if !strings.HasPrefix(target, destAbs+string(filepath.Separator)) && target != destAbs {
			return 0, 0, fmt.Errorf("zip slip rejected: %s", name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, 0, err
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(cleaned))
		if _, bad := deny[ext]; bad {
			continue
		}
		if int64(f.UncompressedSize64) > maxSingleBytes {
			return 0, 0, fmt.Errorf("file too large: %s", name)
		}
		if fileCount+1 > maxFiles {
			return 0, 0, fmt.Errorf("too many files in archive")
		}
		if totalBytes+int64(f.UncompressedSize64) > maxTotalBytes {
			return 0, 0, fmt.Errorf("extracted size exceeds limit")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, 0, err
		}
		rc, err := f.Open()
		if err != nil {
			return 0, 0, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return 0, 0, err
		}
		written, copyErr := io.Copy(out, io.LimitReader(rc, maxSingleBytes+1))
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return 0, 0, copyErr
		}
		if written > maxSingleBytes {
			return 0, 0, fmt.Errorf("file too large while extracting: %s", name)
		}
		fileCount++
		totalBytes += written
	}
	return fileCount, totalBytes, nil
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
