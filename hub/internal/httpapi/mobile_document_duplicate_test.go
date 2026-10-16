package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func setupMobileUploadDuplicateTest(t *testing.T, email string) (*auth.IdentityService, string, *auth.EnrollmentResult, string) {
	t.Helper()
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, email)
	clearMobileStateForTest(t)

	blobRoot := t.TempDir()
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "mobile-state.json"))
	t.Setenv(mobileBlobDirEnv, blobRoot)
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
	})
	return identity, token, enroll, blobRoot
}

func postMobileDocumentUpload(identity *auth.IdentityService, token, filename string, raw []byte) (*httptest.ResponseRecorder, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("filename", filename); err != nil {
		return nil, err
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(raw); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	MobileDocumentUploadHandler(identity).ServeHTTP(rec, req)
	return rec, nil
}

func mustPostMobileDocumentUpload(t *testing.T, identity *auth.IdentityService, token, filename string, raw []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec, err := postMobileDocumentUpload(identity, token, filename, raw)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func decodeUploadPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
	}
	return payload
}

func snapshotMobileDrafts() []mobileDocumentDraftRecord {
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	out := make([]mobileDocumentDraftRecord, 0, len(mobileDocuments.drafts))
	for _, d := range mobileDocuments.drafts {
		out = append(out, d)
	}
	return out
}

func countRegularFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func contentSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestMobileDocumentUploadSkipsDuplicateBytes(t *testing.T) {
	identity, token, _, blobRoot := setupMobileUploadDuplicateTest(t, "dup-upload@example.com")
	raw := []byte("same cloud-drive bytes under two names\n")
	first := mustPostMobileDocumentUpload(t, identity, token, "book.txt", raw)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	firstPayload := decodeUploadPayload(t, first)
	drafts := snapshotMobileDrafts()
	if len(drafts) != 1 {
		t.Fatalf("drafts=%d", len(drafts))
	}
	kept := drafts[0]
	if kept.SourceContentSHA256 != contentSHA256(raw) {
		t.Fatalf("hash=%q want %q", kept.SourceContentSHA256, contentSHA256(raw))
	}
	used := mobileDocumentQuotaUsedBytes(kept.OwnerID, kept.TenantID)
	blobs := countRegularFiles(t, blobRoot)
	if blobs != 1 {
		t.Fatalf("blobs after first upload=%d", blobs)
	}

	second := mustPostMobileDocumentUpload(t, identity, token, "other-name.txt", raw)
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	payload := decodeUploadPayload(t, second)
	if payload["duplicate"] != true || payload["status"] != "duplicate" {
		t.Fatalf("payload=%#v", payload)
	}
	if payload["draft_id"] != kept.ID {
		t.Fatalf("draft_id=%v want %s", payload["draft_id"], kept.ID)
	}
	if payload["duplicate_of_title"] != "book" || payload["duplicate_of_filename"] != "book.txt" {
		t.Fatalf("title=%v filename=%v", payload["duplicate_of_title"], payload["duplicate_of_filename"])
	}
	if payload["message"] != "云盘中已有相同内容，未再次上传。" {
		t.Fatalf("message=%v", payload["message"])
	}
	draftBody, _ := payload["draft"].(map[string]any)
	if _, embedded := draftBody["markdown"]; embedded {
		t.Fatal("duplicate response embedded the document body")
	}
	if _, ok := firstPayload["duplicate"]; ok && firstPayload["duplicate"] == true {
		t.Fatal("first upload was marked duplicate")
	}
	if got := snapshotMobileDrafts(); len(got) != 1 {
		t.Fatalf("drafts after duplicate=%d", len(got))
	}
	if again := mobileDocumentQuotaUsedBytes(kept.OwnerID, kept.TenantID); again != used {
		t.Fatalf("quota used %d -> %d", used, again)
	}
	if got := countRegularFiles(t, blobRoot); got != 1 {
		t.Fatalf("blobs after duplicate=%d", got)
	}
}

func TestMobileDocumentDuplicateNamesOldestCopy(t *testing.T) {
	identity, token, _, blobRoot := setupMobileUploadDuplicateTest(t, "oldest-copy@example.com")
	raw := []byte("two stored copies of one original\n")
	first := mustPostMobileDocumentUpload(t, identity, token, "book.txt", raw)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	blobs := countRegularFiles(t, blobRoot)
	mobileDocuments.Lock()
	var orig mobileDocumentDraftRecord
	for _, d := range mobileDocuments.drafts {
		orig = d
	}
	newer := orig
	newer.ID = "newer-copy"
	newer.Title = "较新的副本"
	newer.SourceFilename = "copy.txt"
	newer.UpdatedAt = orig.UpdatedAt.Add(time.Hour)
	mobileDocuments.drafts[newer.ID] = newer
	mobileDocuments.Unlock()

	second := mustPostMobileDocumentUpload(t, identity, token, "again.txt", raw)
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	payload := decodeUploadPayload(t, second)
	if payload["duplicate"] != true || payload["draft_id"] != orig.ID || payload["duplicate_of_title"] != orig.Title {
		t.Fatalf("payload=%#v", payload)
	}
	if got := snapshotMobileDrafts(); len(got) != 2 {
		t.Fatalf("drafts=%d", len(got))
	}
	if got := countRegularFiles(t, blobRoot); got != blobs {
		t.Fatalf("duplicate upload wrote a blob: %d -> %d", blobs, got)
	}
}

func TestMobileDocumentUploadBackfillsLegacyHashAndKeepsDifferentBytes(t *testing.T) {
	identity, token, _, blobRoot := setupMobileUploadDuplicateTest(t, "legacy-hash@example.com")
	raw := bytes.Repeat([]byte("A"), 64)
	other := bytes.Repeat([]byte("B"), len(raw))
	first := mustPostMobileDocumentUpload(t, identity, token, "legacy.txt", raw)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	mobileDocuments.Lock()
	var owner, tenant, legacyID string
	for id, d := range mobileDocuments.drafts {
		legacyID = id
		owner, tenant = d.OwnerID, d.TenantID
		if d.SourceEncoding != "gzip" || d.SourceOriginalSize != len(raw) || d.SourcePath == "" {
			mobileDocuments.Unlock()
			t.Fatalf("legacy blob not on disk: %#v", d)
		}
		d.SourceContentSHA256 = ""
		mobileDocuments.drafts[id] = d
	}
	mobileDocuments.Unlock()
	used := mobileDocumentQuotaUsedBytes(owner, tenant)

	second := mustPostMobileDocumentUpload(t, identity, token, "different.txt", other)
	if second.Code != http.StatusAccepted {
		t.Fatalf("different bytes status=%d body=%s", second.Code, second.Body.String())
	}
	drafts := snapshotMobileDrafts()
	if len(drafts) != 2 {
		t.Fatalf("drafts=%d", len(drafts))
	}
	var legacy, created mobileDocumentDraftRecord
	for _, d := range drafts {
		if d.ID == legacyID {
			legacy = d
		} else {
			created = d
		}
	}
	if legacy.SourceContentSHA256 != contentSHA256(raw) || created.SourceContentSHA256 != contentSHA256(other) {
		t.Fatalf("legacy=%q created=%q", legacy.SourceContentSHA256, created.SourceContentSHA256)
	}
	if mobileDocumentQuotaUsedBytes(owner, tenant) <= used {
		t.Fatal("different bytes did not increase quota")
	}
	used = mobileDocumentQuotaUsedBytes(owner, tenant)
	blobs := countRegularFiles(t, blobRoot)

	third := mustPostMobileDocumentUpload(t, identity, token, "legacy-again.txt", raw)
	if third.Code != http.StatusOK {
		t.Fatalf("repeat status=%d body=%s", third.Code, third.Body.String())
	}
	payload := decodeUploadPayload(t, third)
	if payload["duplicate"] != true || payload["draft_id"] != legacyID {
		t.Fatalf("payload=%#v", payload)
	}
	if got := snapshotMobileDrafts(); len(got) != 2 {
		t.Fatalf("drafts after repeat=%d", len(got))
	}
	if again := mobileDocumentQuotaUsedBytes(owner, tenant); again != used {
		t.Fatalf("quota used %d -> %d", used, again)
	}
	if got := countRegularFiles(t, blobRoot); got != blobs {
		t.Fatalf("blobs %d -> %d", blobs, got)
	}
}

func TestMobileDocumentUploadDuplicateIgnoresOtherOwnerAndTenant(t *testing.T) {
	identity, token, enroll, _ := setupMobileUploadDuplicateTest(t, "owner-a@example.com")
	raw := []byte("tenant isolated cloud drive bytes\n")
	sum := contentSHA256(raw)
	userTenant := mobileMeetingRecordingTenantID(enroll.TenantID)
	foreignTenant := "tenant-other"
	if foreignTenant == userTenant {
		foreignTenant = "tenant-else"
	}
	mobileDocuments.Lock()
	mobileDocuments.drafts["foreign"] = mobileDocumentDraftRecord{
		ID:                  "foreign",
		OwnerID:             enroll.UserID,
		TenantID:            foreignTenant,
		Title:               "其他租户的文件",
		SourceFilename:      "foreign.txt",
		SourceContentSHA256: sum,
		SourceOriginalSize:  len(raw),
		SourceSize:          8,
		SourcePath:          "foreign/blob",
	}
	mobileDocuments.Unlock()

	own := mustPostMobileDocumentUpload(t, identity, token, "mine.txt", raw)
	if own.Code != http.StatusAccepted {
		t.Fatalf("same owner other tenant status=%d body=%s", own.Code, own.Body.String())
	}
	if payload := decodeUploadPayload(t, own); payload["duplicate"] == true {
		t.Fatalf("other tenant matched: %#v", payload)
	}
	drafts := snapshotMobileDrafts()
	if len(drafts) != 2 {
		t.Fatalf("drafts=%d", len(drafts))
	}
	for _, d := range drafts {
		if d.ID == "foreign" {
			continue
		}
		if d.OwnerID != enroll.UserID || mobileMeetingRecordingTenantID(d.TenantID) != userTenant {
			t.Fatalf("created owner=%q tenant=%q enroll=%q tenant=%q", d.OwnerID, d.TenantID, enroll.UserID, userTenant)
		}
	}

	otherToken, _ := issueViewerToken(t, identity, "owner-b@example.com")
	other := mustPostMobileDocumentUpload(t, identity, otherToken, "theirs.txt", raw)
	if other.Code != http.StatusAccepted {
		t.Fatalf("other owner status=%d body=%s", other.Code, other.Body.String())
	}
	if payload := decodeUploadPayload(t, other); payload["duplicate"] == true {
		t.Fatalf("other owner matched: %#v", payload)
	}
	if got := snapshotMobileDrafts(); len(got) != 3 {
		t.Fatalf("drafts=%d", len(got))
	}
}

func TestMobileDocumentUploadDuplicateProbe(t *testing.T) {
	identity, token, _, _ := setupMobileUploadDuplicateTest(t, "probe@example.com")
	raw := []byte("probe these original bytes\n")
	sum := contentSHA256(raw)
	first := mustPostMobileDocumentUpload(t, identity, token, "probe.txt", raw)
	if first.Code != http.StatusAccepted {
		t.Fatalf("upload status=%d body=%s", first.Code, first.Body.String())
	}
	mobileDocuments.Lock()
	for id, d := range mobileDocuments.drafts {
		d.SourceContentSHA256 = ""
		mobileDocuments.drafts[id] = d
	}
	mobileDocuments.Unlock()

	hit := postDuplicateProbe(t, identity, token, sum, len(raw))
	if hit.Code != http.StatusOK {
		t.Fatalf("probe status=%d body=%s", hit.Code, hit.Body.String())
	}
	payload := decodeUploadPayload(t, hit)
	if payload["duplicate"] != true {
		t.Fatalf("probe payload=%#v", payload)
	}
	drafts := snapshotMobileDrafts()
	if len(drafts) != 1 || drafts[0].SourceContentSHA256 != sum {
		t.Fatalf("backfill drafts=%d hash=%q", len(drafts), drafts[0].SourceContentSHA256)
	}

	miss := postDuplicateProbe(t, identity, token, contentSHA256([]byte("different")), len(raw))
	if miss.Code != http.StatusOK {
		t.Fatalf("miss status=%d body=%s", miss.Code, miss.Body.String())
	}
	if payload := decodeUploadPayload(t, miss); payload["duplicate"] != false {
		t.Fatalf("miss payload=%#v", payload)
	}
	if got := snapshotMobileDrafts(); len(got) != 1 {
		t.Fatalf("probe created a draft: %d", len(got))
	}

	bad := postDuplicateProbe(t, identity, token, "not-a-hash", len(raw))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad hash status=%d body=%s", bad.Code, bad.Body.String())
	}
	empty := postDuplicateProbe(t, identity, token, sum, 0)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty size status=%d body=%s", empty.Code, empty.Body.String())
	}
}

func postDuplicateProbe(t *testing.T, identity *auth.IdentityService, token, sum string, size int) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"sha256": sum, "size": size})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/duplicate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileDocumentUploadDuplicateHandler(identity).ServeHTTP(rec, req)
	return rec
}

func TestMobileDocumentUploadDuplicateConcurrentAdmission(t *testing.T) {
	identity, token, _, blobRoot := setupMobileUploadDuplicateTest(t, "concurrent-dup@example.com")
	raw := []byte("two uploads of one original must keep a single copy\n")
	var wg sync.WaitGroup
	type result struct {
		code int
		body string
		err  error
	}
	results := make([]result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rec, err := postMobileDocumentUpload(identity, token, "concurrent.txt", raw)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].code = rec.Code
			results[i].body = rec.Body.String()
		}(i)
	}
	close(start)
	wg.Wait()

	accepted, duplicates := 0, 0
	for _, res := range results {
		if res.err != nil {
			t.Fatal(res.err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(res.body), &payload); err != nil {
			t.Fatalf("status=%d body=%s err=%v", res.code, res.body, err)
		}
		if res.code == http.StatusAccepted && payload["duplicate"] != true {
			accepted++
			continue
		}
		if res.code == http.StatusOK && payload["duplicate"] == true {
			duplicates++
			continue
		}
		t.Fatalf("unexpected status=%d body=%s", res.code, res.body)
	}
	if accepted != 1 || duplicates != 1 {
		t.Fatalf("accepted=%d duplicates=%d", accepted, duplicates)
	}
	drafts := snapshotMobileDrafts()
	if len(drafts) != 1 {
		t.Fatalf("drafts=%d", len(drafts))
	}
	if drafts[0].SourceContentSHA256 != contentSHA256(raw) {
		t.Fatalf("hash=%q", drafts[0].SourceContentSHA256)
	}
	if got := countRegularFiles(t, blobRoot); got != 1 {
		t.Fatalf("blobs=%d", got)
	}
}
