package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const mobileBlobDirEnv = "MACLAW_MOBILE_BLOB_DIR"

// Hot in-process cache limit for original bytes. Larger files stay disk-only
// (SourcePath + SourceSize) and are loaded on demand without re-caching.
const mobileDocumentSourceHotCacheMax = 512 << 10 // 512 KiB

// mobileDocumentBlobDir is the on-disk store for original mobile documents
// (draft/upload binaries). Defaults to <state.json parent>/blobs.
func mobileDocumentBlobDir() string {
	if p := strings.TrimSpace(os.Getenv(mobileBlobDirEnv)); p != "" {
		return p
	}
	state := mobileStatePath()
	if state == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(state), "blobs")
}

// mobileDocumentBlobStoreReady is true when the blob root is configured and
// currently present as a directory. Repair must not wipe SourcePath metadata
// while the store is offline (misconfigured env, unmounted volume, etc.).
func mobileDocumentBlobStoreReady() bool {
	root := strings.TrimSpace(mobileDocumentBlobDir())
	if root == "" {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

// mobileShouldClearSourceMetaAfterStreamFail is true only when a failed original
// stream can be attributed to a confirmed-missing blob (store online + size 0).
// When the store is offline or the file still exists (e.g. permission error),
// metadata must be preserved.
func mobileShouldClearSourceMetaAfterStreamFail(relPath string) bool {
	p := strings.TrimSpace(relPath)
	if p == "" {
		return true
	}
	if !mobileDocumentBlobStoreReady() {
		return false
	}
	return mobileDocumentBlobSize(p) == 0
}

// mobileWriteDocumentBlob writes raw bytes and returns a path relative to the
// blob root: owner/kind/id.bin
func mobileWriteDocumentBlob(ownerID, kind, id string, raw []byte) (string, error) {
	root := mobileDocumentBlobDir()
	if root == "" {
		return "", fmt.Errorf("mobile blob dir is not configured")
	}
	ownerID = strings.TrimSpace(ownerID)
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	if ownerID == "" || kind == "" || id == "" {
		return "", fmt.Errorf("invalid blob key")
	}
	// Prevent path traversal in id/owner/kind.
	ownerID = filepath.Base(ownerID)
	kind = filepath.Base(kind)
	id = filepath.Base(id)
	if ownerID == "." || ownerID == ".." || kind == "." || kind == ".." || id == "." || id == ".." {
		return "", fmt.Errorf("invalid blob key")
	}
	rel := filepath.ToSlash(filepath.Join(ownerID, kind, id+".bin"))
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return rel, nil
}

// mobileBlobAbsPath resolves a relative blob path under the blob root.
func mobileBlobAbsPath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", fmt.Errorf("empty blob path")
	}
	root := mobileDocumentBlobDir()
	if root == "" {
		return "", fmt.Errorf("mobile blob dir is not configured")
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid blob path")
	}
	abs := filepath.Join(root, clean)
	relCheck, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", fmt.Errorf("invalid blob path")
	}
	return abs, nil
}

func mobileReadDocumentBlob(relPath string) ([]byte, error) {
	abs, err := mobileBlobAbsPath(relPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// mobileOpenDocumentBlob opens a blob for streaming. Caller must Close the file.
func mobileOpenDocumentBlob(relPath string) (*os.File, int64, error) {
	abs, err := mobileBlobAbsPath(relPath)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, 0, fmt.Errorf("blob path is a directory")
	}
	return f, info.Size(), nil
}

func mobileDeleteDocumentBlob(relPath string) {
	abs, err := mobileBlobAbsPath(relPath)
	if err != nil {
		return
	}
	_ = os.Remove(abs)
}

// mobileDocumentBlobSize returns on-disk size for a relative blob path, or 0.
func mobileDocumentBlobSize(relPath string) int {
	abs, err := mobileBlobAbsPath(relPath)
	if err != nil {
		return 0
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return 0
	}
	n := info.Size()
	if n > int64(^uint(0)>>1) {
		return 0
	}
	return int(n)
}

// mobileWriteOriginalHTTP streams original bytes to the client. Prefers disk
// streaming when SourcePath is set to avoid holding multi-MB files in RAM.
func mobileWriteOriginalHTTP(w http.ResponseWriter, contentType, filename string, mem []byte, relPath string) bool {
	return mobileWriteOriginalHTTPDisp(w, contentType, filename, mem, relPath, false)
}

// mobileWriteOriginalHTTPDisp is like mobileWriteOriginalHTTP; inline=true uses
// Content-Disposition: inline (browser image preview / <img>).
func mobileWriteOriginalHTTPDisp(w http.ResponseWriter, contentType, filename string, mem []byte, relPath string, inline bool) bool {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		filename = "download"
	}
	disp := "attachment"
	if inline {
		disp = "inline"
	}
	// Prefer streaming from disk when a path is available.
	if p := strings.TrimSpace(relPath); p != "" {
		f, size, err := mobileOpenDocumentBlob(p)
		if err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition", disp+"; filename="+strconv.Quote(filename))
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			w.Header().Set("Cache-Control", "private, max-age=300")
			w.WriteHeader(http.StatusOK)
			_, _ = io.Copy(w, f)
			return true
		}
	}
	if len(mem) == 0 {
		return false
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disp+"; filename="+strconv.Quote(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(mem)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(mem)
	return true
}

// mobileNormalizeDraftSourceMeta fills SourceSize from memory or disk without loading bytes.
func mobileNormalizeDraftSourceMeta(d *mobileDocumentDraftRecord) {
	if d == nil {
		return
	}
	if d.SourceSize == 0 && len(d.SourceBytes) > 0 {
		d.SourceSize = len(d.SourceBytes)
	}
	if d.SourceSize == 0 && strings.TrimSpace(d.SourcePath) != "" {
		if n := mobileDocumentBlobSize(d.SourcePath); n > 0 {
			d.SourceSize = n
		}
	}
}

func mobileNormalizeUploadSourceMeta(u *mobileDocumentUploadRecord) {
	if u == nil {
		return
	}
	if u.SourceSize == 0 && len(u.SourceBytes) > 0 {
		u.SourceSize = len(u.SourceBytes)
	}
	if u.SourceSize == 0 && strings.TrimSpace(u.SourcePath) != "" {
		if n := mobileDocumentBlobSize(u.SourcePath); n > 0 {
			u.SourceSize = n
		}
	}
}

// mobileDraftRepairSourceMeta clears path/size when the blob is missing and there
// is no in-memory copy. Returns true if the record was mutated.
// When the blob store is offline, path/size are left alone so a transient outage
// cannot mass-delete durable original metadata.
func mobileDraftRepairSourceMeta(d *mobileDocumentDraftRecord) bool {
	if d == nil {
		return false
	}
	if len(d.SourceBytes) > 0 {
		return false
	}
	p := strings.TrimSpace(d.SourcePath)
	if p == "" {
		if d.SourceSize != 0 {
			d.SourceSize = 0
			return true
		}
		return false
	}
	if !mobileDocumentBlobStoreReady() {
		return false
	}
	if mobileDocumentBlobSize(p) > 0 {
		return false
	}
	d.SourcePath = ""
	d.SourceSize = 0
	return true
}

func mobileUploadRepairSourceMeta(u *mobileDocumentUploadRecord) bool {
	if u == nil {
		return false
	}
	if len(u.SourceBytes) > 0 {
		return false
	}
	p := strings.TrimSpace(u.SourcePath)
	if p == "" {
		if u.SourceSize != 0 {
			u.SourceSize = 0
			return true
		}
		return false
	}
	if !mobileDocumentBlobStoreReady() {
		return false
	}
	if mobileDocumentBlobSize(p) > 0 {
		return false
	}
	u.SourcePath = ""
	u.SourceSize = 0
	return true
}

// mobileDraftHasOriginal reports whether a draft still has an original file.
func mobileDraftHasOriginal(d mobileDocumentDraftRecord) bool {
	if len(d.SourceBytes) > 0 {
		return true
	}
	if p := strings.TrimSpace(d.SourcePath); p != "" {
		// Prefer trusting SourceSize for list hot paths; if size is 0, stat disk.
		// Stale paths with non-zero size are cleared by mobileDraftRepairSourceMeta
		// on list/detail/download (avoids N disk stats on every has_original check).
		if d.SourceSize > 0 {
			return true
		}
		return mobileDocumentBlobSize(p) > 0
	}
	return false
}

func mobileDraftSourceSize(d mobileDocumentDraftRecord) int {
	if d.SourceSize > 0 {
		return d.SourceSize
	}
	if n := len(d.SourceBytes); n > 0 {
		return n
	}
	return 0
}

// mobileDraftLoadSourceBytes returns original bytes (memory cache or disk).
// Large files on disk are not re-cached into SourceBytes.
// On hard miss with an online store, clears ghost path/size on the provided
// record (caller must write the record back and persist if desired).
func mobileDraftLoadSourceBytes(d *mobileDocumentDraftRecord) []byte {
	if d == nil {
		return nil
	}
	if len(d.SourceBytes) > 0 {
		return d.SourceBytes
	}
	if strings.TrimSpace(d.SourcePath) == "" {
		return nil
	}
	raw, err := mobileReadDocumentBlob(d.SourcePath)
	if err != nil || len(raw) == 0 {
		if mobileDocumentBlobStoreReady() && mobileDocumentBlobSize(d.SourcePath) == 0 {
			d.SourcePath = ""
			d.SourceSize = 0
		}
		return nil
	}
	if len(raw) <= mobileDocumentSourceHotCacheMax {
		d.SourceBytes = raw
	}
	if d.SourceSize == 0 {
		d.SourceSize = len(raw)
	}
	return raw
}

func mobileUploadHasSource(u mobileDocumentUploadRecord) bool {
	if len(u.SourceBytes) > 0 {
		return true
	}
	if p := strings.TrimSpace(u.SourcePath); p != "" {
		// When the store is online, always stat — SourceSize alone can be ghost.
		// When offline, trust metadata so list/claim does not mass-fail during outages.
		if mobileDocumentBlobStoreReady() {
			return mobileDocumentBlobSize(p) > 0
		}
		return u.SourceSize > 0
	}
	return false
}

func mobileUploadSourceSize(u mobileDocumentUploadRecord) int {
	if u.SourceSize > 0 {
		return u.SourceSize
	}
	return len(u.SourceBytes)
}

func mobileUploadLoadSourceBytes(u *mobileDocumentUploadRecord) []byte {
	if u == nil {
		return nil
	}
	if len(u.SourceBytes) > 0 {
		return u.SourceBytes
	}
	if strings.TrimSpace(u.SourcePath) == "" {
		return nil
	}
	raw, err := mobileReadDocumentBlob(u.SourcePath)
	if err != nil || len(raw) == 0 {
		if mobileDocumentBlobStoreReady() && mobileDocumentBlobSize(u.SourcePath) == 0 {
			u.SourcePath = ""
			u.SourceSize = 0
		}
		return nil
	}
	if len(raw) <= mobileDocumentSourceHotCacheMax {
		u.SourceBytes = raw
	}
	if u.SourceSize == 0 {
		u.SourceSize = len(raw)
	}
	return raw
}

// mobilePersistDocumentOriginal stores raw on disk when possible and updates
// path/size. Falls back to in-memory only when blob dir is unavailable.
// Files larger than mobileDocumentSourceHotCacheMax keep only path/size in RAM.
func mobilePersistDocumentOriginal(ownerID, kind, id string, raw []byte) (path string, size int, mem []byte) {
	size = len(raw)
	if size == 0 {
		return "", 0, nil
	}
	if p, err := mobileWriteDocumentBlob(ownerID, kind, id, raw); err == nil {
		if size <= mobileDocumentSourceHotCacheMax {
			return p, size, append([]byte(nil), raw...)
		}
		return p, size, nil
	}
	// No blob dir: must keep memory copy for the running process.
	return "", size, append([]byte(nil), raw...)
}

// mobileReleaseUploadOriginalWhenDraftOwns drops the upload-side original once
// the draft owns a durable copy (ready or failed terminal states).
// Returns true when draft and/or upload meta were mutated (caller should persist).
// Caller holds mobileDocuments.Lock.
//
// Important: ghost draft SourcePath/SourceSize must be repaired before trusting
// has_original — otherwise a missing draft blob would free the only real upload
// original and lose the file permanently.
func mobileReleaseUploadOriginalWhenDraftOwns(record *mobileDocumentUploadRecord) (dirty bool) {
	if record == nil {
		return false
	}
	switch record.Status {
	case "ready", "failed":
	default:
		return false
	}
	if strings.TrimSpace(record.DraftID) == "" {
		return false
	}
	draft, ok := mobileDocuments.drafts[record.DraftID]
	if !ok || draft.OwnerID != record.OwnerID {
		return false
	}
	if mobileDraftRepairSourceMeta(&draft) {
		mobileDocuments.drafts[record.DraftID] = draft
		dirty = true
	}
	if !mobileDraftHasOriginal(draft) {
		return dirty
	}
	// Draft keeps the source of truth; upload source is only for workers.
	released := false
	if p := strings.TrimSpace(record.SourcePath); p != "" {
		if p != strings.TrimSpace(draft.SourcePath) {
			mobileDeleteDocumentBlob(p)
		}
		record.SourcePath = ""
		released = true
	}
	if record.SourceSize != 0 {
		record.SourceSize = 0
		released = true
	}
	if len(record.SourceBytes) > 0 {
		record.SourceBytes = nil
		released = true
	}
	if released {
		dirty = true
	}
	return dirty
}

// mobileReleaseUploadOriginalAfterReady is kept as a thin alias for call sites.
func mobileReleaseUploadOriginalAfterReady(record *mobileDocumentUploadRecord) bool {
	return mobileReleaseUploadOriginalWhenDraftOwns(record)
}

// mobileStripDraftBlobForPersist returns a copy suitable for JSON state.
// SourceBytes are stripped only when a durable SourcePath exists — never drop
// the only copy of an original.
func mobileStripDraftBlobForPersist(d mobileDocumentDraftRecord) mobileDocumentDraftRecord {
	out := d
	if len(out.SourceBytes) > 0 && out.SourceSize == 0 {
		out.SourceSize = len(out.SourceBytes)
	}
	if out.SourcePath == "" && len(out.SourceBytes) > 0 && out.ID != "" && out.OwnerID != "" {
		if p, err := mobileWriteDocumentBlob(out.OwnerID, "draft", out.ID, out.SourceBytes); err == nil {
			out.SourcePath = p
		}
	}
	if strings.TrimSpace(out.SourcePath) != "" {
		out.SourceBytes = nil
	}
	return out
}

func mobileStripUploadBlobForPersist(u mobileDocumentUploadRecord) mobileDocumentUploadRecord {
	out := u
	if len(out.SourceBytes) > 0 && out.SourceSize == 0 {
		out.SourceSize = len(out.SourceBytes)
	}
	if out.SourcePath == "" && len(out.SourceBytes) > 0 && out.TaskID != "" && out.OwnerID != "" {
		if p, err := mobileWriteDocumentBlob(out.OwnerID, "upload", out.TaskID, out.SourceBytes); err == nil {
			out.SourcePath = p
		}
	}
	if strings.TrimSpace(out.SourcePath) != "" {
		out.SourceBytes = nil
	}
	return out
}
