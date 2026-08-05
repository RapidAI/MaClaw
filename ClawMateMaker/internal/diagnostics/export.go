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
	"strings"
	"time"

	"clawmatemaker/internal/logging"
)

var allowed = []string{"summary.json", "events.jsonl", "serial.log", "sidecar.log", "log-meta.json", "journal.json"}

type Export struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

func ExportJob(logRoot, jobID, destinationDir string) (Export, error) {
	if !safeID(jobID) {
		return Export{}, fmt.Errorf("invalid job ID")
	}
	if info, err := os.Stat(destinationDir); err != nil || !info.IsDir() {
		return Export{}, fmt.Errorf("export directory is unavailable")
	}
	source := filepath.Join(logRoot, jobID)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return Export{}, fmt.Errorf("job log does not exist")
	}
	final := filepath.Join(destinationDir, fmt.Sprintf("clawmatemaker-diagnostics-%s-%s.zip", jobID, time.Now().UTC().Format("20060102T150405Z")))
	temp := final + ".tmp"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Export{}, err
	}
	z := zip.NewWriter(f)
	for _, name := range allowed {
		data, err := os.ReadFile(filepath.Join(source, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return Export{}, err
		}
		entry, err := z.Create(name)
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return Export{}, err
		}
		if _, err := io.WriteString(entry, logging.Redact(string(data))); err != nil {
			_ = z.Close()
			_ = f.Close()
			return Export{}, err
		}
	}
	if err := z.Close(); err != nil {
		_ = f.Close()
		return Export{}, err
	}
	if err := f.Close(); err != nil {
		return Export{}, err
	}
	if err := os.Rename(temp, final); err != nil {
		return Export{}, err
	}
	data, err := os.ReadFile(final)
	if err != nil {
		return Export{}, err
	}
	sum := sha256.Sum256(data)
	return Export{Path: final, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Bytes: int64(len(data))}, nil
}
func safeID(v string) bool {
	return v != "" && !strings.ContainsAny(v, `\\/:`) && v != "." && v != ".."
}
