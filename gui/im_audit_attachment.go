package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const imAuditAttachmentDirName = "im_audit_attachments"

// Keep recently created files out of orphan cleanup. An attachment is written
// before its audit row reaches the async SQLite writer, so deleting brand-new
// unreferenced files would race that normal persistence window.
const imAuditOrphanGracePeriod = 10 * time.Minute

func (a *App) imAuditAttachmentRoot() string {
	return filepath.Join(a.GetDataDir(), imAuditAttachmentDirName)
}

func (a *App) saveIMAuditAttachment(platform, groupID, messageID, fileName string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("attachment is empty")
	}
	name := safeIMAuditAttachmentName(fileName)
	if name == "" {
		name = "attachment.bin"
	}
	return a.saveIMAuditAttachmentNamed(platform, groupID, messageID, name, data)
}

func (a *App) saveIMAuditAttachmentNamed(platform, groupID, messageID, name string, data []byte) (string, error) {
	dir := filepath.Join(a.imAuditAttachmentRoot(), safeIMAuditPathPart(platform), safeIMAuditPathPart(groupID), time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	prefix := time.Now().Format("150405.000")
	if id := safeIMAuditPathPart(messageID); id != "unknown" {
		if len(id) > 24 {
			id = id[:24]
		}
		prefix += "_" + id
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := prefix + "_" + name
		if i > 0 {
			candidate = fmt.Sprintf("%s_%s_%d%s", prefix, stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(path)
			if writeErr != nil {
				return "", writeErr
			}
			return "", closeErr
		}
		return path, nil
	}
}

func safeIMAuditAttachmentName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == ".." {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	return strings.TrimRight(name, ". ")
}

func safeIMAuditPathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, value)
	value = strings.Trim(value, ". ")
	if value == "" {
		return "unknown"
	}
	return value
}

func (a *App) validateIMAuditAttachmentPath(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return "", fmt.Errorf("attachment path required")
	}
	root, err := filepath.Abs(a.imAuditAttachmentRoot())
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside IM audit attachments")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("attachment path is a directory")
	}
	return absPath, nil
}

// RevealIMAuditAttachment opens Explorer/Finder with a persisted history file selected.
func (a *App) RevealIMAuditAttachment(path string) error {
	validated, err := a.validateIMAuditAttachmentPath(path)
	if err != nil {
		return err
	}
	return a.ShowItemInFolder(validated)
}

// cleanupIMAuditAttachmentPaths removes only validated files below the audit
// attachment root. Invalid/stale paths are ignored rather than broadening scope.
func (a *App) cleanupIMAuditAttachmentPaths(paths []string) {
	for _, path := range paths {
		validated, err := a.validateIMAuditAttachmentPath(path)
		if err == nil {
			_ = os.Remove(validated)
		}
	}
}

func (a *App) cleanupOrphanIMAuditAttachments(store *IMAuditStore) {
	if store == nil {
		return
	}
	referenced, err := store.AllAttachmentPaths()
	if err != nil {
		return
	}
	known := make(map[string]struct{}, len(referenced))
	for _, path := range referenced {
		if validated, err := a.validateIMAuditAttachmentPath(path); err == nil {
			known[filepath.Clean(validated)] = struct{}{}
		}
	}
	root := a.imAuditAttachmentRoot()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		validated, err := a.validateIMAuditAttachmentPath(path)
		if err != nil {
			// A symlink escaping the managed root is never a valid attachment. Remove
			// only the link itself; validation prevents following it to external data.
			if entry.Type()&os.ModeSymlink != 0 {
				_ = os.Remove(path)
			}
			return nil
		}
		if _, ok := known[filepath.Clean(validated)]; !ok {
			info, statErr := entry.Info()
			if statErr == nil && time.Since(info.ModTime()) >= imAuditOrphanGracePeriod {
				_ = os.Remove(path)
			}
		}
		return nil
	})
}
