// Package diagnostics creates a bounded, re-redacted support bundle for one job.
package diagnostics

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"clawmatemaker/internal/logging"
)

var allowed = []string{"summary.json", "events.jsonl", "serial.log", "sidecar.log", "log-meta.json", "journal.json"}

const (
	maxDiagnosticFileBytes   int64 = 5 * 1024 * 1024
	maxDiagnosticBundleBytes       = 20 * 1024 * 1024
)

type Bundle struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

func ExportJob(logRoot, jobID, destinationDir string) (Bundle, error) {
	if !safeID(jobID) {
		return Bundle{}, fmt.Errorf("invalid job ID")
	}
	if info, err := os.Stat(destinationDir); err != nil || !info.IsDir() {
		return Bundle{}, fmt.Errorf("export directory is unavailable")
	}
	root, err := filepath.Abs(logRoot)
	if err != nil {
		return Bundle{}, fmt.Errorf("resolve log root: %w", err)
	}
	source := filepath.Join(root, jobID)
	if filepath.Dir(source) != root {
		return Bundle{}, fmt.Errorf("invalid job log path")
	}
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return Bundle{}, fmt.Errorf("job log does not exist")
	}
	final := filepath.Join(destinationDir, fmt.Sprintf("clawmatemaker-diagnostics-%s-%s.zip", jobID, time.Now().UTC().Format("20060102T150405Z")))
	temp := final + ".tmp"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Bundle{}, err
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(temp)
		}
	}()
	z := zip.NewWriter(f)
	var written int64
	for _, name := range allowed {
		path := filepath.Join(source, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxDiagnosticFileBytes {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, fmt.Errorf("diagnostic file %s is not an allowed regular file", name)
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, err
		}
		redacted := []byte(logging.Redact(string(data)))
		if written+int64(len(redacted)) > maxDiagnosticBundleBytes {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, fmt.Errorf("diagnostic bundle exceeds size limit")
		}
		entry, err := z.Create(name)
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, err
		}
		if _, err := entry.Write(redacted); err != nil {
			_ = z.Close()
			_ = f.Close()
			return Bundle{}, err
		}
		written += int64(len(redacted))
	}
	if err := z.Close(); err != nil {
		_ = f.Close()
		return Bundle{}, err
	}
	if err := f.Close(); err != nil {
		return Bundle{}, err
	}
	if err := os.Rename(temp, final); err != nil {
		return Bundle{}, err
	}
	completed = true
	info, err := os.Stat(final)
	if err != nil {
		return Bundle{}, err
	}
	archive, err := os.Open(final)
	if err != nil {
		return Bundle{}, err
	}
	defer archive.Close()
	h := sha256.New()
	if _, err := io.Copy(h, archive); err != nil {
		return Bundle{}, err
	}
	return Bundle{Path: final, SHA256: "sha256:" + hex.EncodeToString(h.Sum(nil)), Bytes: info.Size()}, nil
}
func safeID(v string) bool {
	return logging.SafeJobID(v)
}
