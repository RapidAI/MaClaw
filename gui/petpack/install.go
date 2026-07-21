package petpack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Security limits (design B.5).
const (
	MaxZipFiles          = 64
	MaxSingleFileBytes   = 512 * 1024
	MaxUncompressedBytes = 2 * 1024 * 1024
	MaxZipArchiveBytes   = 3 * 1024 * 1024 // on-disk zip size before extract
	MaxCompressionRatio  = 12.0
	MaxPathLength        = 180
)

var installMu sync.Mutex

var allowedExt = map[string]bool{
	".webp": true, ".png": true, ".yaml": true, ".yml": true,
	".json": true, ".txt": true, ".md": true,
	// SVG only after scan; still allowed extension but content scanned
	".svg": true,
}

var deniedExt = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true, ".wasm": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".bat": true, ".cmd": true, ".ps1": true, ".html": true, ".htm": true,
}

// InstallZip installs a pet pack zip into userRoot/<id>/.
// Returns installed pack id.
func (r *Registry) InstallZip(zipPath string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("nil registry")
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("pet pack path is a directory, expected .zip file")
	}
	if st.Size() > MaxZipArchiveBytes {
		return "", fmt.Errorf("zip too large (max %d bytes)", MaxZipArchiveBytes)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > MaxZipArchiveBytes {
		return "", fmt.Errorf("zip too large (max %d bytes)", MaxZipArchiveBytes)
	}
	return r.InstallZipBytes(data)
}

// InstallZipBytes installs from raw zip bytes (tests).
func (r *Registry) InstallZipBytes(data []byte) (string, error) {
	if r == nil {
		return "", fmt.Errorf("nil registry")
	}
	if int64(len(data)) > MaxZipArchiveBytes {
		return "", fmt.Errorf("zip too large (max %d bytes)", MaxZipArchiveBytes)
	}
	installMu.Lock()
	defer installMu.Unlock()
	if r.userRoot == "" {
		return "", fmt.Errorf("user packs dir not configured")
	}
	if err := os.MkdirAll(r.userRoot, 0o755); err != nil {
		return "", fmt.Errorf("create user packs dir: %w", err)
	}
	files, err := validateAndExtractZip(data)
	if err != nil {
		return "", err
	}
	// Prefer the shallowest pet-pack.yaml (stable when zip has nested copies).
	manName, manData := pickShallowestManifest(files)
	if manData == nil {
		return "", fmt.Errorf("zip missing pet-pack.yaml")
	}
	m, err := parseManifest(manData)
	if err != nil {
		return "", fmt.Errorf("manifest: %w", err)
	}
	// Strip common root prefix if zip has pack-id/ folder
	prefix := ""
	if dir := filepath.ToSlash(filepath.Dir(manName)); dir != "." && dir != "" {
		prefix = dir + "/"
	}
	dest := filepath.Join(r.userRoot, m.ID)
	// Atomic-ish: write to temp, swap previous dir aside, then promote tmp.
	// Never delete the live pack until the new tree is fully written.
	tmp := dest + ".tmp-install"
	backup := dest + ".bak-install"
	_ = os.RemoveAll(tmp)
	_ = os.RemoveAll(backup)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	written := 0
	for name, content := range files {
		rel := name
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue // skip files outside pack root
			}
			rel = strings.TrimPrefix(name, prefix)
		}
		if rel == "" || rel == "." {
			continue
		}
		if err := writeSecureFile(tmp, rel, content); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
		written++
	}
	if written == 0 {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("zip produced no pack files after root strip")
	}
	// Move existing install out of the way (restore on failure).
	if st, err := os.Lstat(dest); err == nil && st != nil {
		if err := os.Rename(dest, backup); err != nil {
			// Windows busy/locked: fall back to RemoveAll only after tmp is ready.
			if err2 := os.RemoveAll(dest); err2 != nil {
				_ = os.RemoveAll(tmp)
				return "", fmt.Errorf("replace existing pack: %w", err)
			}
			backup = "" // cannot restore
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		// fallback copy on Windows if rename across volumes fails
		if err2 := copyDir(tmp, dest); err2 != nil {
			_ = os.RemoveAll(tmp)
			_ = os.RemoveAll(dest)
			if backup != "" {
				_ = os.Rename(backup, dest)
			}
			return "", fmt.Errorf("promote pack install: rename: %v; copy: %w", err, err2)
		}
		_ = os.RemoveAll(tmp)
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	// Already hold installMu — do not call Scan() (would deadlock).
	_ = r.scanUnlocked()
	return m.ID, nil
}

// Uninstall removes a user-installed pack directory.
// Bundled-only official packs cannot be uninstalled (no user dir).
// If a user override of an official id exists, only the user override is removed.
func (r *Registry) Uninstall(id string) error {
	installMu.Lock()
	defer installMu.Unlock()
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	id = strings.TrimSpace(id)
	if !IsValidPackID(id) {
		return fmt.Errorf("invalid pack id")
	}
	if r.userRoot == "" {
		return fmt.Errorf("user packs dir not configured")
	}
	dir := filepath.Join(r.userRoot, id)
	// Prefer registered Dir so mismatched folder layouts (invalid packs) can still be removed.
	r.mu.RLock()
	if m, ok := r.packs[id]; ok && m != nil && m.Scope == ScopeUser && strings.TrimSpace(m.Dir) != "" {
		dir = m.Dir
	}
	r.mu.RUnlock()
	if err := assertUnderUserRoot(r.userRoot, dir); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if IsOfficialPackID(id) {
				return fmt.Errorf("cannot uninstall official pack %q (bundled only)", id)
			}
			return fmt.Errorf("pack %q is not installed in user packs", id)
		}
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to uninstall symlink pack path")
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Already hold installMu — do not call Scan() (would deadlock).
	return r.scanUnlocked()
}

// pickShallowestManifest chooses the pet-pack.yaml with the fewest path segments
// (stable when a zip accidentally contains more than one).
func pickShallowestManifest(files map[string][]byte) (name string, data []byte) {
	bestDepth := int(^uint(0) >> 1)
	for n, content := range files {
		base := filepath.Base(n)
		if base != "pet-pack.yaml" && base != "pet-pack.yml" {
			continue
		}
		depth := strings.Count(filepath.ToSlash(n), "/")
		if data == nil || depth < bestDepth || (depth == bestDepth && n < name) {
			bestDepth = depth
			name = n
			data = content
		}
	}
	return name, data
}

func assertUnderUserRoot(userRoot, dir string) error {
	if err := pathUnderRoot(userRoot, dir); err != nil {
		return fmt.Errorf("pack directory escapes user packs root")
	}
	return nil
}

func validateAndExtractZip(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %w", err)
	}
	if len(zr.File) > MaxZipFiles {
		return nil, fmt.Errorf("too many files in zip (max %d)", MaxZipFiles)
	}
	out := make(map[string][]byte)
	var totalUncompHeader int64
	var totalComp int64
	var totalUncompActual int64
	for _, f := range zr.File {
		if f == nil {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if len(name) > MaxPathLength {
			return nil, fmt.Errorf("path too long: %s", name)
		}
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
			return nil, fmt.Errorf("zip-slip rejected: %s", name)
		}
		// clean and recheck
		clean := filepath.ToSlash(filepath.Clean(name))
		if strings.HasPrefix(clean, "../") || clean == ".." {
			return nil, fmt.Errorf("zip-slip rejected: %s", name)
		}
		ext := strings.ToLower(filepath.Ext(clean))
		if deniedExt[ext] {
			return nil, fmt.Errorf("forbidden extension %s", ext)
		}
		if !allowedExt[ext] {
			return nil, fmt.Errorf("disallowed extension %s", ext)
		}
		if f.UncompressedSize64 > MaxSingleFileBytes {
			return nil, fmt.Errorf("file too large: %s", clean)
		}
		totalUncompHeader += int64(f.UncompressedSize64)
		totalComp += int64(f.CompressedSize64)
		if totalUncompHeader > MaxUncompressedBytes {
			return nil, fmt.Errorf("uncompressed total exceeds limit")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		limited := io.LimitReader(rc, MaxSingleFileBytes+1)
		content, err := io.ReadAll(limited)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if len(content) > MaxSingleFileBytes {
			return nil, fmt.Errorf("file too large: %s", clean)
		}
		totalUncompActual += int64(len(content))
		if totalUncompActual > MaxUncompressedBytes {
			return nil, fmt.Errorf("uncompressed total exceeds limit")
		}
		if ext == ".svg" {
			if err := scanSVG(content); err != nil {
				return nil, fmt.Errorf("svg rejected (%s): %w", clean, err)
			}
		}
		out[clean] = content
	}
	// Prefer actual bytes for ratio when headers under-report compressed size.
	ratioNum := totalUncompHeader
	if totalUncompActual > ratioNum {
		ratioNum = totalUncompActual
	}
	if totalComp > 0 && float64(ratioNum)/float64(totalComp) > MaxCompressionRatio {
		return nil, fmt.Errorf("zip bomb ratio exceeded")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty zip")
	}
	return out, nil
}

func scanSVG(content []byte) error {
	lower := strings.ToLower(string(content))
	deny := []string{
		"<script", "javascript:", "onload=", "onclick=", "onerror=",
		"foreignobject", "<!entity", "<!doctype", "xlink:href=\"http",
		"href=\"http", "href=\"//", "url(http", "url(//",
	}
	for _, d := range deny {
		if strings.Contains(lower, d) {
			return fmt.Errorf("contains %s", d)
		}
	}
	return nil
}

func writeSecureFile(root, rel string, content []byte) error {
	rel = safeRel(rel)
	if rel == "" {
		return fmt.Errorf("unsafe path")
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := pathUnderRoot(root, full); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
