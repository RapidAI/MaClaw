package database

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
)

const accessPrewriteBackupSuffix = ".maclaw-prewrite.bak"

// accessDiskAvailable is the volume free-space probe used before copying the
// pre-write backup. Tests replace it to simulate a full disk.
var accessDiskAvailable = archiveutil.AvailableBytes

func accessConnectionDSN(p Profile, secret string) string {
	dsn := strings.TrimSpace(p.DSN)
	if dsn == "" {
		dsn = "Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=" + p.FilePath + ";"
	}
	write := p.WriteEnabled && !p.ReadOnly
	if !write && !strings.Contains(strings.ToLower(dsn), "readonly=") {
		dsn += "ReadOnly=1;"
	}
	if secret != "" && !dsnEmbedsCredentials(dsn) {
		dsn += "PWD=" + secret + ";"
	}
	return dsn
}

func accessLockSidecar(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	switch strings.ToLower(ext) {
	case ".accdb":
		return base + ".laccdb"
	case ".mdb":
		return base + ".ldb"
	default:
		return ""
	}
}

// prepareAccessWrite enforces the design's write preflight: shared-lock
// detection via the Access sidecar, a writable handle, and a sibling backup
// copy so a failed mutation can be recovered. Compact/repair is not performed.
func prepareAccessWrite(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("path_denied: file_path required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("connection: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path_denied: file_path is a directory")
	}
	if err := ensureAccessDiskSpace(path, info.Size()); err != nil {
		return err
	}
	// ACE creates .ldb/.laccdb for the process that already holds the
	// connection, including this writer. Sidecar presence is not proof of a
	// foreign exclusive lock; a failed RDWR open is.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if sidecar := accessLockSidecar(path); sidecar != "" {
			if _, statErr := os.Stat(sidecar); statErr == nil {
				return fmt.Errorf("permission: Access database is locked by another process")
			}
		}
		return fmt.Errorf("permission: Access database is not writable: %w", err)
	}
	_ = f.Close()
	if err := copyAccessBackup(path, path+accessPrewriteBackupSuffix); err != nil {
		return fmt.Errorf("quota_exceeded: cannot create pre-write backup: %w", err)
	}
	return nil
}

func ensureAccessDiskSpace(path string, fileSize int64) error {
	need := fileSize + 1<<20
	if need < 1<<20 {
		need = 1 << 20
	}
	avail, err := accessDiskAvailable(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("quota_exceeded: cannot measure disk space: %w", err)
	}
	if avail < need {
		return fmt.Errorf("quota_exceeded: not enough disk space for Access pre-write backup")
	}
	return nil
}

func copyAccessBackup(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}
