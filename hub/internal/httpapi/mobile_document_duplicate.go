package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileContentHashCandidate is a same-size original whose content hash has
// not been stored yet. The blob itself is immutable, so the path can be hashed
// after the documents lock is released.
type mobileContentHashCandidate struct {
	ID       string
	Path     string
	Encoding string
	Mem      []byte
}

func mobileNormalizeContentSHA256(sum string) string {
	sum = strings.ToLower(strings.TrimSpace(sum))
	if len(sum) != sha256.Size*2 {
		return ""
	}
	for _, c := range sum {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return sum
}

// mobileHashStoredOriginal hashes the original bytes, decoding a gzip envelope
// when the blob was stored compressed. mem is the stored form, not the original.
func mobileHashStoredOriginal(relPath, encoding string, mem []byte) (string, error) {
	var src io.Reader
	var closeFn func()
	if len(mem) > 0 {
		src = bytes.NewReader(mem)
	} else {
		f, _, err := mobileOpenDocumentBlob(relPath)
		if err != nil {
			return "", err
		}
		closeFn = func() { _ = f.Close() }
		src = f
	}
	if closeFn != nil {
		defer closeFn()
	}
	if strings.EqualFold(strings.TrimSpace(encoding), "gzip") {
		zr, err := gzip.NewReader(src)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		src = zr
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(src, int64(mobileDocumentOriginalMaxBytes)+1))
	if err != nil {
		return "", err
	}
	if n > int64(mobileDocumentOriginalMaxBytes) {
		return "", errMobileDocumentOriginalTooLarge
	}
	if n == 0 {
		return "", nil
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func mobileDraftOwnedBy(d mobileDocumentDraftRecord, ownerID, tenantID string) bool {
	return d.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, d.TenantID)
}

// mobileHashUploadOriginal hashes the caller's original bytes. The reader is
// consumed; the caller seeks back to the start before storing.
func mobileHashUploadOriginal(r io.Reader) (string, int, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, int64(mobileDocumentOriginalMaxBytes)+1))
	if err != nil {
		return "", 0, err
	}
	if n > int64(mobileDocumentOriginalMaxBytes) {
		return "", 0, errMobileDocumentOriginalTooLarge
	}
	if n == 0 {
		return "", 0, nil
	}
	return hex.EncodeToString(h.Sum(nil)), int(n), nil
}

// mobilePreferDuplicateDraft picks the copy to name in the skip notice.
// The oldest original wins so two existing copies do not swap titles between uploads.
func mobilePreferDuplicateDraft(cur, next mobileDocumentDraftRecord) mobileDocumentDraftRecord {
	if strings.TrimSpace(cur.ID) == "" {
		return next
	}
	switch {
	case next.UpdatedAt.IsZero() && !cur.UpdatedAt.IsZero():
		return cur
	case !next.UpdatedAt.IsZero() && cur.UpdatedAt.IsZero():
		return next
	case next.UpdatedAt.Before(cur.UpdatedAt):
		return next
	case next.UpdatedAt.Equal(cur.UpdatedAt) && next.ID < cur.ID:
		return next
	default:
		return cur
	}
}

// mobileListContentHashWork returns a draft that already has this content hash,
// or the same-size originals that still need hashing.
func mobileListContentHashWork(ownerID, tenantID, wantSHA string, originalSize int) (mobileDocumentDraftRecord, bool, []mobileContentHashCandidate) {
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	work := make([]mobileContentHashCandidate, 0)
	var found mobileDocumentDraftRecord
	matched := false
	for _, d := range mobileDocuments.drafts {
		if !mobileDraftOwnedBy(d, ownerID, tenantID) {
			continue
		}
		have := mobileNormalizeContentSHA256(d.SourceContentSHA256)
		if have != "" {
			if have == wantSHA {
				found = mobilePreferDuplicateDraft(found, d)
				matched = true
			}
			continue
		}
		if originalSize <= 0 || mobileDraftOriginalSize(d) != originalSize || !mobileDraftHasOriginal(d) {
			continue
		}
		c := mobileContentHashCandidate{ID: d.ID, Path: d.SourcePath, Encoding: d.SourceEncoding}
		if len(d.SourceBytes) > 0 {
			c.Mem = append([]byte(nil), d.SourceBytes...)
		}
		work = append(work, c)
	}
	if matched {
		return found, true, nil
	}
	return mobileDocumentDraftRecord{}, false, work
}

func mobileStoreContentHashes(ownerID, tenantID string, sums map[string]string) {
	if len(sums) == 0 {
		return
	}
	mobileDocuments.Lock()
	changed := false
	for id, sum := range sums {
		sum = mobileNormalizeContentSHA256(sum)
		d, ok := mobileDocuments.drafts[id]
		if !ok || sum == "" || !mobileDraftOwnedBy(d, ownerID, tenantID) {
			continue
		}
		if mobileNormalizeContentSHA256(d.SourceContentSHA256) != "" {
			continue
		}
		d.SourceContentSHA256 = sum
		mobileDocuments.drafts[id] = d
		changed = true
	}
	mobileDocuments.Unlock()
	if changed {
		mobilePersistState()
	}
}

func mobileMatchStoredContentHash(ownerID, tenantID, wantSHA string) (mobileDocumentDraftRecord, bool) {
	wantSHA = mobileNormalizeContentSHA256(wantSHA)
	if wantSHA == "" {
		return mobileDocumentDraftRecord{}, false
	}
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	var found mobileDocumentDraftRecord
	matched := false
	for _, d := range mobileDocuments.drafts {
		if !mobileDraftOwnedBy(d, ownerID, tenantID) {
			continue
		}
		if mobileNormalizeContentSHA256(d.SourceContentSHA256) == wantSHA {
			found = mobilePreferDuplicateDraft(found, d)
			matched = true
		}
	}
	if matched {
		return found, true
	}
	return mobileDocumentDraftRecord{}, false
}

// mobileResolveDocumentContentDuplicate reports an existing original with the
// same bytes. Drafts saved before hashes existed are hashed when their original
// size matches, then remembered so the next upload does not read them again.
func mobileResolveDocumentContentDuplicate(ownerID, tenantID, wantSHA string, originalSize int) (mobileDocumentDraftRecord, bool) {
	wantSHA = mobileNormalizeContentSHA256(wantSHA)
	if wantSHA == "" {
		return mobileDocumentDraftRecord{}, false
	}
	match, ok, work := mobileListContentHashWork(ownerID, tenantID, wantSHA, originalSize)
	if ok {
		return match, true
	}
	sums := make(map[string]string, len(work))
	for _, c := range work {
		sum, err := mobileHashStoredOriginal(c.Path, c.Encoding, c.Mem)
		if err != nil {
			continue
		}
		sum = mobileNormalizeContentSHA256(sum)
		if sum == "" {
			continue
		}
		sums[c.ID] = sum
	}
	mobileStoreContentHashes(ownerID, tenantID, sums)
	return mobileMatchStoredContentHash(ownerID, tenantID, wantSHA)
}

func mobileWriteDuplicateDocumentUpload(w http.ResponseWriter, existing mobileDocumentDraftRecord) {
	title := strings.TrimSpace(existing.Title)
	filename := strings.TrimSpace(existing.SourceFilename)
	if title == "" {
		title = filename
	}
	if title == "" {
		title = existing.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                "duplicate",
		"duplicate":             true,
		"message":               "云盘中已有相同内容，未再次上传。",
		"draft_id":              existing.ID,
		"duplicate_of_title":    title,
		"duplicate_of_filename": filename,
		"draft":                 mobileDuplicateDraftPayload(existing, title, filename),
	})
}

// mobileDuplicateDraftPayload is the skip notice, not a document download.
// The full draft payload re-reads the original when its preview looks unreadable.
func mobileDuplicateDraftPayload(existing mobileDocumentDraftRecord, title, filename string) map[string]any {
	payload := map[string]any{
		"id":         existing.ID,
		"title":      title,
		"updated_at": existing.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if filename != "" {
		payload["source_filename"] = filename
	}
	if mobileDraftHasOriginal(existing) {
		payload["has_original"] = true
		payload["source_size"] = mobileDraftOriginalSize(existing)
	}
	return payload
}

// MobileDocumentUploadDuplicateHandler answers whether this account already
// holds the original bytes. Callers skip the file body when it does.
func MobileDocumentUploadDuplicateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var body struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		sum := mobileNormalizeContentSHA256(body.SHA256)
		if sum == "" || body.Size <= 0 || body.Size > int64(mobileDocumentOriginalMaxBytes) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "sha256 and size are required")
			return
		}
		mobileEnsureStateLoaded()
		if dup, ok := mobileResolveDocumentContentDuplicate(principal.UserID, principal.TenantID, sum, int(body.Size)); ok {
			mobileWriteDuplicateDocumentUpload(w, dup)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"duplicate": false})
	}
}
