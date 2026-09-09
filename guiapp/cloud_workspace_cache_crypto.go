package guiapp

// Local Cloud Workspace cache protection.
//
// A cache has to be plaintext while an agent is executing in it, but keeping
// the same tree plaintext after ReleaseCloudWorkspace makes a stolen profile,
// backup agent, or another local user a trivial data-exfiltration path.  This
// file implements a deliberately small, fail-closed envelope:
//   * each regular file is encrypted as independently authenticated chunks;
//   * the path map and file hashes are encrypted as one manifest;
//   * the key is held by the OS keyring (never in the cache directory);
//   * sealing is atomic at the directory level and unsealing verifies every
//     file before exposing it to the task runtime.
//
// It is not E2E encryption: Hub still owns the server-side object key and a
// running process necessarily has plaintext access.  The feature is opt-in
// through AppConfig.CloudWorkspaceCacheEncryption or the
// MACLAW_CLOUD_WORKSPACE_CACHE_ENCRYPTION environment variable.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	cloudWorkspaceCacheKeyringService = "MaClaw Cloud Workspace Cache"
	cloudWorkspaceCacheSealMarker     = "cache-sealed.v1"
	cloudWorkspaceCacheSealDir        = "cache-sealed-files"
	cloudWorkspaceCacheSealVersion    = 1
	cloudWorkspaceCacheSealChunkBytes = 1 << 20
)

var (
	// These indirections make keyring failures deterministic in headless tests
	// without weakening the production default.
	cloudWorkspaceCacheKeyringGet    = keyring.Get
	cloudWorkspaceCacheKeyringSet    = keyring.Set
	cloudWorkspaceCacheKeyringDelete = keyring.Delete
)

type cloudWorkspaceCacheSealFile struct {
	Path string `json:"path"`
	ID   string `json:"id"`
	Mode uint32 `json:"mode"`
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}

type cloudWorkspaceCacheSealManifest struct {
	Version int                           `json:"version"`
	Files   []cloudWorkspaceCacheSealFile `json:"files"`
}

type cloudWorkspaceCacheSealEnvelope struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func cloudWorkspaceCacheEncryptionEnabled(a *App) bool {
	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil && cfg.CloudWorkspaceCacheEncryption {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_CLOUD_WORKSPACE_CACHE_ENCRYPTION"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func cloudWorkspaceCacheKeyringID(tenantID, workspaceID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(workspaceID)))
	return "workspace-" + hex.EncodeToString(sum[:16])
}

func cloudWorkspaceCacheKey(tenantID, workspaceID string, create bool) ([]byte, string, error) {
	item := cloudWorkspaceCacheKeyringID(tenantID, workspaceID)
	raw, err := cloudWorkspaceCacheKeyringGet(cloudWorkspaceCacheKeyringService, item)
	if err == nil {
		key, decodeErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(raw))
		if decodeErr != nil || len(key) != 32 {
			return nil, "", fmt.Errorf("cloud workspace cache keyring entry is invalid")
		}
		sum := sha256.Sum256(key)
		return key, hex.EncodeToString(sum[:]), nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return nil, "", fmt.Errorf("read cloud workspace cache key: %w", err)
	}
	if !create {
		return nil, "", err
	}
	var key [32]byte
	if _, randErr := rand.Read(key[:]); randErr != nil {
		return nil, "", fmt.Errorf("generate cloud workspace cache key: %w", randErr)
	}
	encoded := base64.RawStdEncoding.EncodeToString(key[:])
	if setErr := cloudWorkspaceCacheKeyringSet(cloudWorkspaceCacheKeyringService, item, encoded); setErr != nil {
		return nil, "", fmt.Errorf("store cloud workspace cache key in OS keyring: %w", setErr)
	}
	sum := sha256.Sum256(key[:])
	return key[:], hex.EncodeToString(sum[:]), nil
}

func deleteCloudWorkspaceCacheKey(tenantID, workspaceID string) error {
	err := cloudWorkspaceCacheKeyringDelete(cloudWorkspaceCacheKeyringService, cloudWorkspaceCacheKeyringID(tenantID, workspaceID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete cloud workspace cache key from OS keyring: %w", err)
	}
	return nil
}

func cloudWorkspaceCacheSealMarkerPath(root string) string {
	return filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheSealMarker)
}

func cloudWorkspaceCacheSealDirPath(root string) string {
	return filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheSealDir)
}

func cloudWorkspaceCacheSealMarkerExists(root string) bool {
	info, err := os.Lstat(cloudWorkspaceCacheSealMarkerPath(root))
	return err == nil && info.Mode().IsRegular()
}

func cacheWorkspaceSealAAD(tenantID, workspaceID, rel string) []byte {
	return []byte("maclaw-cloud-cache-v1\x00" + strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(workspaceID) + "\x00" + rel)
}

// cloudWorkspaceCacheInternalPath identifies control files that must remain
// available to the lock/seal protocol itself.  Other .maclaw-cloud files
// (state.json, hash-index.json, and future metadata) are ordinary sensitive
// cache content and are therefore encrypted like user files.
func cloudWorkspaceCacheInternalPath(rel string) bool {
	prefix := cloudWorkspaceCacheStateDir + "/"
	if !strings.HasPrefix(rel, prefix) {
		return false
	}
	rest := strings.TrimPrefix(rel, prefix)
	if rest == cloudWorkspaceCacheSealMarker || rest == cloudWorkspaceCacheSealDir || strings.HasPrefix(rest, cloudWorkspaceCacheSealDir+"/") || rest == "writer.lock" || rest == "writer.lock.guard" || strings.HasPrefix(rest, ".sealed-files-") || strings.HasPrefix(rest, ".unseal-files-") {
		return true
	}
	return false
}

func newCloudWorkspaceCacheGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealCloudWorkspaceCache(a *App, root, tenantID, workspaceID string) error {
	root = normalizeProjectSessionPath(root)
	if root == "" || !validCloudWorkspaceCacheID(workspaceID) {
		return fmt.Errorf("invalid cloud workspace cache identity")
	}
	stateDir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := cloudWorkspaceCacheDirectoryExistsAndSafe(root, stateDir); err != nil {
		return err
	}
	if markerInfo, err := os.Lstat(cloudWorkspaceCacheSealMarkerPath(root)); err == nil {
		if markerInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cloud workspace cache seal marker is a symlink")
		}
		return nil // idempotent release
	} else if !os.IsNotExist(err) {
		return err
	}
	key, keyID, err := cloudWorkspaceCacheKey(tenantID, workspaceID, true)
	if err != nil {
		return err
	}
	gcm, err := newCloudWorkspaceCacheGCM(key)
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(stateDir, ".sealed-files-")
	if err != nil {
		return fmt.Errorf("create cache seal staging directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	var files []cloudWorkspaceCacheSealFile
	err = filepath.WalkDir(root, func(pathName string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, pathName)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == cloudWorkspaceCacheStateDir {
			return nil
		}
		if cloudWorkspaceCacheInternalPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cloud workspace cache contains unsupported symlink %q", rel)
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cloud workspace cache contains unsupported file %q", rel)
		}
		clean, ok := cloudWorkspaceSafeRelPath(rel)
		if !ok {
			return fmt.Errorf("invalid cloud workspace cache path %q", rel)
		}
		idBytes := sha256.Sum256([]byte(keyID + "\x00" + clean))
		id := hex.EncodeToString(idBytes[:16])
		sealedPath := filepath.Join(tmpDir, id+".enc")
		sealedSHA, sealedSize, sealErr := sealCloudWorkspaceCacheFile(pathName, sealedPath, gcm, cacheWorkspaceSealAAD(tenantID, workspaceID, clean))
		if sealErr != nil {
			return fmt.Errorf("seal cache file %q: %w", clean, sealErr)
		}
		post, postErr := os.Stat(pathName)
		if postErr != nil || post.Size() != info.Size() || post.ModTime().UnixNano() != info.ModTime().UnixNano() || cloudWorkspaceFileID(post) != cloudWorkspaceFileID(info) {
			if postErr == nil {
				postErr = fmt.Errorf("file changed while sealing")
			}
			return fmt.Errorf("cloud workspace cache file %q changed: %w", clean, postErr)
		}
		files = append(files, cloudWorkspaceCacheSealFile{Path: clean, ID: id, Mode: uint32(info.Mode().Perm()), Size: sealedSize, SHA: sealedSHA})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	manifestRaw, err := json.Marshal(cloudWorkspaceCacheSealManifest{Version: cloudWorkspaceCacheSealVersion, Files: files})
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	envelope := cloudWorkspaceCacheSealEnvelope{
		Version:    cloudWorkspaceCacheSealVersion,
		KeyID:      keyID,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(gcm.Seal(nil, nonce, manifestRaw, cacheWorkspaceSealAAD(tenantID, workspaceID, "manifest"))),
	}
	markerRaw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	finalDir := cloudWorkspaceCacheSealDirPath(root)
	if finalInfo, statErr := os.Lstat(finalDir); statErr == nil {
		if finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.IsDir() {
			return fmt.Errorf("cloud workspace cache sealed directory is unsafe")
		}
		return fmt.Errorf("cloud workspace cache sealed directory already exists without marker")
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return fmt.Errorf("commit cloud workspace cache sealed files: %w", err)
	}
	// Marker is written only after the encrypted files are durable and visible.
	// If the process dies before this point, the next release can discard the
	// orphan staging directory while plaintext remains untouched.
	if err := atomicWriteFile(cloudWorkspaceCacheSealMarkerPath(root), markerRaw); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("commit cloud workspace cache seal marker: %w", err)
	}
	// A marker means the encrypted copy is authoritative.  Remove only files
	// that were hashed and sealed; leave unexpected files in place so unseal can
	// fail closed rather than silently discarding user data.
	for _, file := range files {
		if removeErr := os.Remove(filepath.Join(root, filepath.FromSlash(file.Path))); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("remove plaintext cloud workspace cache file %q: %w", file.Path, removeErr)
		}
	}
	return nil
}

func sealCloudWorkspaceCacheFile(srcPath, dstPath string, gcm cipher.AEAD, aad []byte) (string, int64, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", 0, err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	ok := false
	defer func() {
		_ = dst.Close()
		if !ok {
			_ = os.Remove(dstPath)
		}
	}()
	if _, err := dst.Write([]byte("MCWSCF1\x00")); err != nil {
		return "", 0, err
	}
	buf := make([]byte, cloudWorkspaceCacheSealChunkBytes)
	h := sha256.New()
	var size int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
			size += int64(n)
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return "", 0, err
			}
			sealed := gcm.Seal(nil, nonce, buf[:n], aad)
			frameLen := uint32(len(nonce) + len(sealed))
			if err := binary.Write(dst, binary.BigEndian, frameLen); err != nil {
				return "", 0, err
			}
			if _, err := dst.Write(nonce); err != nil {
				return "", 0, err
			}
			if _, err := dst.Write(sealed); err != nil {
				return "", 0, err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", 0, readErr
		}
	}
	if err := dst.Sync(); err != nil {
		return "", 0, err
	}
	if err := dst.Close(); err != nil {
		return "", 0, err
	}
	ok = true
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func unsealCloudWorkspaceCache(a *App, root, tenantID, workspaceID string) error {
	root = normalizeProjectSessionPath(root)
	stateDir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := cloudWorkspaceCacheDirectoryExistsAndSafe(root, stateDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	markerPath := cloudWorkspaceCacheSealMarkerPath(root)
	markerInfo, markerStatErr := os.Lstat(markerPath)
	if markerStatErr == nil && markerInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cloud workspace cache seal marker is a symlink")
	}
	markerRaw, err := os.ReadFile(markerPath)
	if os.IsNotExist(err) {
		// Best-effort cleanup of a staging directory left before the marker was
		// committed.  It contains no authoritative data.
		if stateDir := filepath.Join(root, cloudWorkspaceCacheStateDir); cloudWorkspaceCacheDirectoryExistsAndSafe(root, stateDir) == nil {
			if entries, readErr := os.ReadDir(stateDir); readErr == nil {
				for _, entry := range entries {
					if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".sealed-files-") || entry.Name() == cloudWorkspaceCacheSealDir) {
						_ = os.RemoveAll(filepath.Join(stateDir, entry.Name()))
					}
				}
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var envelope cloudWorkspaceCacheSealEnvelope
	if err := json.Unmarshal(markerRaw, &envelope); err != nil || envelope.Version != cloudWorkspaceCacheSealVersion {
		return fmt.Errorf("invalid cloud workspace cache seal marker")
	}
	key, keyID, err := cloudWorkspaceCacheKey(tenantID, workspaceID, false)
	if err != nil {
		return err
	}
	if keyID != strings.TrimSpace(envelope.KeyID) {
		return fmt.Errorf("cloud workspace cache key has been rotated or destroyed")
	}
	gcm, err := newCloudWorkspaceCacheGCM(key)
	if err != nil {
		return err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return fmt.Errorf("invalid cloud workspace cache seal nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return fmt.Errorf("invalid cloud workspace cache seal manifest")
	}
	manifestRaw, err := gcm.Open(nil, nonce, ciphertext, cacheWorkspaceSealAAD(tenantID, workspaceID, "manifest"))
	if err != nil {
		return fmt.Errorf("decrypt cloud workspace cache manifest: %w", err)
	}
	var manifest cloudWorkspaceCacheSealManifest
	if json.Unmarshal(manifestRaw, &manifest) != nil || manifest.Version != cloudWorkspaceCacheSealVersion {
		return fmt.Errorf("invalid cloud workspace cache seal manifest")
	}
	sealedDir := cloudWorkspaceCacheSealDirPath(root)
	if err := cloudWorkspaceCacheDirectoryExistsAndSafe(root, sealedDir); err != nil {
		return fmt.Errorf("cloud workspace cache sealed files unavailable: %w", err)
	}
	// Refuse unexpected plaintext files.  A crash can leave a previously
	// restored file; matching files are replaced after authentication, while a
	// different file is preserved and blocks unseal.
	if err := cloudWorkspaceCacheUnexpectedPlaintext(root, manifest); err != nil {
		return err
	}
	// Decrypt into .maclaw-cloud first.  No user-visible plaintext is exposed
	// until every sealed file has passed AEAD, size and SHA-256 verification;
	// this avoids a tampered later file leaving a partially restored tree.
	stageDir, err := os.MkdirTemp(stateDir, ".unseal-files-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	for _, file := range manifest.Files {
		clean, ok := cloudWorkspaceSafeRelPath(file.Path)
		if !ok || clean != file.Path || file.Size < 0 || !validCloudWorkspaceSHA256(file.SHA) || file.ID == "" {
			return fmt.Errorf("invalid cloud workspace cache seal entry")
		}
		srcPath := filepath.Join(sealedDir, filepath.Base(file.ID)+".enc")
		if filepath.Base(file.ID) != file.ID {
			return fmt.Errorf("invalid cloud workspace cache sealed file id")
		}
		srcInfo, srcStatErr := os.Lstat(srcPath)
		if srcStatErr != nil || srcInfo.Mode()&os.ModeSymlink != 0 || !srcInfo.Mode().IsRegular() {
			return fmt.Errorf("cloud workspace cache sealed file is unsafe")
		}
		stagePath := filepath.Join(stageDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(stagePath), 0o700); err != nil {
			return err
		}
		tmp, err := os.OpenFile(stagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if err := unsealCloudWorkspaceCacheFile(srcPath, tmp, gcm, cacheWorkspaceSealAAD(tenantID, workspaceID, file.Path), file.Size, file.SHA); err != nil {
			_ = tmp.Close()
			_ = os.Remove(stagePath)
			return fmt.Errorf("unseal cache file %q: %w", file.Path, err)
		}
		if err := tmp.Chmod(os.FileMode(file.Mode) & os.ModePerm); err != nil {
			_ = tmp.Close()
			_ = os.Remove(stagePath)
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(stagePath)
			return err
		}
	}
	for _, file := range manifest.Files {
		stagePath := filepath.Join(stageDir, filepath.FromSlash(file.Path))
		dstPath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := ensureCloudWorkspaceCacheDirectory(root, filepath.Dir(dstPath)); err != nil {
			return err
		}
		if existing, statErr := os.Stat(dstPath); statErr == nil {
			if !existing.Mode().IsRegular() {
				return fmt.Errorf("cloud workspace cache target %q is not a regular file", file.Path)
			}
			sum, size, hashErr := hashCloudWorkspaceFile(dstPath)
			if hashErr != nil || sum != file.SHA || size != file.Size {
				return fmt.Errorf("cloud workspace cache target %q changed while sealed", file.Path)
			}
			if removeErr := os.Remove(dstPath); removeErr != nil {
				return fmt.Errorf("replace existing cloud workspace cache file %q: %w", file.Path, removeErr)
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := os.Rename(stagePath, dstPath); err != nil {
			return fmt.Errorf("restore plaintext cloud workspace cache file %q: %w", file.Path, err)
		}
	}
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cloud workspace seal marker: %w", err)
	}
	// Marker removal is the commit point for unseal.  If cleanup of the
	// already-unused ciphertext directory is interrupted, a later Prepare sees
	// plaintext as authoritative and removes the orphan directory safely.
	if err := os.RemoveAll(sealedDir); err != nil {
		return fmt.Errorf("remove cloud workspace sealed files: %w", err)
	}
	return nil
}

func unsealCloudWorkspaceCacheFile(srcPath string, dst *os.File, gcm cipher.AEAD, aad []byte, wantSize int64, wantSHA string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	header := make([]byte, 8)
	if _, err := io.ReadFull(src, header); err != nil || string(header) != "MCWSCF1\x00" {
		return fmt.Errorf("invalid cloud workspace sealed file header")
	}
	h := sha256.New()
	var size int64
	for {
		var frameLen uint32
		err := binary.Read(src, binary.BigEndian, &frameLen)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || frameLen < uint32(gcm.NonceSize()+gcm.Overhead()) || frameLen > uint32(cloudWorkspaceCacheSealChunkBytes+gcm.NonceSize()+gcm.Overhead()) {
			return fmt.Errorf("invalid cloud workspace sealed frame")
		}
		frame := make([]byte, frameLen)
		if _, err := io.ReadFull(src, frame); err != nil {
			return err
		}
		plain, err := gcm.Open(nil, frame[:gcm.NonceSize()], frame[gcm.NonceSize():], aad)
		if err != nil {
			return fmt.Errorf("authenticate cloud workspace sealed frame: %w", err)
		}
		if _, err := dst.Write(plain); err != nil {
			return err
		}
		_, _ = h.Write(plain)
		size += int64(len(plain))
	}
	if size != wantSize || hex.EncodeToString(h.Sum(nil)) != wantSHA {
		return fmt.Errorf("cloud workspace cache plaintext hash mismatch")
	}
	return nil
}

func cloudWorkspaceCacheUnexpectedPlaintext(root string, manifest cloudWorkspaceCacheSealManifest) error {
	allowed := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		allowed[file.Path] = struct{}{}
	}
	var unexpected error
	_ = filepath.WalkDir(root, func(pathName string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			unexpected = walkErr
			return walkErr
		}
		rel, _ := filepath.Rel(root, pathName)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == cloudWorkspaceCacheStateDir {
			return nil
		}
		if cloudWorkspaceCacheInternalPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			unexpected = fmt.Errorf("cloud workspace cache contains symlink %q while sealed", rel)
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := allowed[rel]; !ok {
			unexpected = fmt.Errorf("cloud workspace cache contains unexpected plaintext %q while sealed", rel)
			return filepath.SkipAll
		}
		return nil
	})
	return unexpected
}

// cloudWorkspaceSealedCachePresent is used before local purge to decide
// whether deleting the workspace key is part of the requested privacy action.
func (a *App) cloudWorkspaceSealedCachePresent(workspaceID string) bool {
	if a == nil || !validCloudWorkspaceCacheID(workspaceID) {
		return false
	}
	for _, rootName := range []string{"cloud-workspaces", "cloud-workspaces-readonly"} {
		root := normalizeProjectSessionPath(filepath.Join(a.GetDataDir(), rootName))
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, tenant := range entries {
			if tenant.Type()&os.ModeSymlink != 0 || !tenant.IsDir() {
				continue
			}
			candidate := filepath.Join(root, tenant.Name(), workspaceID)
			if _, err := os.Stat(cloudWorkspaceCacheSealMarkerPath(candidate)); err == nil {
				return true
			}
		}
	}
	return false
}
