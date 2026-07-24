package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMobileDocumentBlobWriteReadDelete(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	raw := []byte("hello original blob")
	rel, err := mobileWriteDocumentBlob("owner1", "draft", "mobdoc_1", raw)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if rel != "owner1/draft/mobdoc_1.bin" && !strings.HasSuffix(filepath.ToSlash(rel), "owner1/draft/mobdoc_1.bin") {
		t.Fatalf("rel=%q", rel)
	}
	got, err := mobileReadDocumentBlob(rel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("got=%q want=%q", got, raw)
	}
	if n := mobileDocumentBlobSize(rel); n != len(raw) {
		t.Fatalf("size=%d want %d", n, len(raw))
	}
	mobileDeleteDocumentBlob(rel)
	if _, err := mobileReadDocumentBlob(rel); err == nil {
		t.Fatal("expected read error after delete")
	}
}

func TestMobileDocumentBlobRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	if _, err := mobileReadDocumentBlob("../etc/passwd"); err == nil {
		t.Fatal("expected traversal reject")
	}
	if _, err := mobileReadDocumentBlob("/abs/path.bin"); err == nil {
		t.Fatal("expected abs path reject")
	}
	// Write with traversal-like id must be rejected.
	if _, err := mobileWriteDocumentBlob("o", "draft", "..", []byte("x")); err == nil {
		t.Fatal("expected reject for id=..")
	}
	if _, err := mobileWriteDocumentBlob("..", "draft", "id1", []byte("x")); err == nil {
		t.Fatal("expected reject for owner=..")
	}
	// Normal write stays under root.
	rel, err := mobileWriteDocumentBlob("o", "draft", "safe1", []byte("x"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	relCheck, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		t.Fatalf("abs outside root: %s", abs)
	}
}

func TestMobilePersistAndStripDraftBlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	raw := []byte("docx-bytes-here")
	path, size, mem := mobilePersistDocumentOriginal("u1", "draft", "d1", raw)
	if path == "" || size != len(raw) || !bytes.Equal(mem, raw) {
		t.Fatalf("path=%q size=%d mem=%q", path, size, mem)
	}
	draft := mobileDocumentDraftRecord{
		ID: "d1", OwnerID: "u1", Title: "t",
		SourcePath: path, SourceSize: size, SourceBytes: mem,
		SourceFilename: "a.docx", UpdatedAt: time.Now().UTC(),
	}
	if !mobileDraftHasOriginal(draft) {
		t.Fatal("expected has original")
	}
	stripped := mobileStripDraftBlobForPersist(draft)
	if stripped.SourceBytes != nil {
		t.Fatal("SourceBytes must be stripped for state.json")
	}
	if stripped.SourcePath == "" || stripped.SourceSize != len(raw) {
		t.Fatalf("stripped path/size=%q %d", stripped.SourcePath, stripped.SourceSize)
	}

	// Simulate load after restart: path only, no memory.
	loaded := mobileDocumentDraftRecord{
		ID: "d1", OwnerID: "u1",
		SourcePath: stripped.SourcePath, SourceSize: stripped.SourceSize,
		SourceFilename: "a.docx",
	}
	mobileNormalizeDraftSourceMeta(&loaded)
	if loaded.SourceSize != len(raw) {
		t.Fatalf("size after normalize=%d", loaded.SourceSize)
	}
	if len(loaded.SourceBytes) != 0 {
		t.Fatal("should not eager-load bytes")
	}
	got := mobileDraftLoadSourceBytes(&loaded)
	if !bytes.Equal(got, raw) {
		t.Fatalf("lazy load=%q", got)
	}
	if !bytes.Equal(loaded.SourceBytes, raw) {
		t.Fatal("cache after load")
	}
}

func TestMobilePersistStateStripsSourceBytesToDisk(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "mobile-state.json")
	blobRoot := t.TempDir()
	t.Setenv(mobileStatePathEnv, statePath)
	t.Setenv(mobileBlobDirEnv, blobRoot)
	// Reset loaded flag so ensure-load can run cleanly if needed.
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true // skip load; we seed maps directly
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
		mobileDocuments.Lock()
		mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
		mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
		mobileDocuments.Unlock()
	})

	raw := []byte("persist-me-please")
	mobileDocuments.Lock()
	mobileDocuments.drafts["pd1"] = mobileDocumentDraftRecord{
		ID: "pd1", OwnerID: "owner", Title: "T", Markdown: "# hi",
		SourceBytes: raw, SourceFilename: "f.bin", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()

	mobilePersistState()

	// state.json must not embed the raw blob.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, raw) {
		t.Fatalf("state.json still contains raw source bytes")
	}
	var state mobilePersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	rec, ok := state.Drafts["pd1"]
	if !ok {
		t.Fatal("draft missing from state")
	}
	if len(rec.SourceBytes) != 0 {
		t.Fatal("persisted SourceBytes should be empty")
	}
	if rec.SourcePath == "" || rec.SourceSize != len(raw) {
		t.Fatalf("path=%q size=%d", rec.SourcePath, rec.SourceSize)
	}
	// Blob file exists.
	got, err := mobileReadDocumentBlob(rec.SourcePath)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("disk blob err=%v got=%q", err, got)
	}
	// Live in-memory record must be backfilled with SourcePath (not only state.json).
	mobileDocuments.Lock()
	live := mobileDocuments.drafts["pd1"]
	mobileDocuments.Unlock()
	if live.SourcePath != rec.SourcePath {
		t.Fatalf("live SourcePath not backfilled: live=%q state=%q", live.SourcePath, rec.SourcePath)
	}
	if live.SourceSize != len(raw) {
		t.Fatalf("live SourceSize=%d", live.SourceSize)
	}
}

func TestMobileDeleteDraftRemovesBlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	raw := []byte("to-delete")
	path, _, _ := mobilePersistDocumentOriginal("ow", "draft", "del1", raw)
	if path == "" {
		t.Fatal("no path")
	}
	if _, err := mobileReadDocumentBlob(path); err != nil {
		t.Fatal(err)
	}
	mobileDeleteDocumentBlob(path)
	if _, err := mobileReadDocumentBlob(path); err == nil {
		t.Fatal("blob should be gone")
	}
}

func TestMobileDraftHasOriginalWithPathOnly(t *testing.T) {
	// Store offline (root missing): helpers may trust SourceSize metadata.
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "no-blobs-root"))
	d := mobileDocumentDraftRecord{SourcePath: "o/draft/x.bin", SourceSize: 12}
	if !mobileDraftHasOriginal(d) {
		t.Fatal("path+size should count as original")
	}
	if mobileDraftSourceSize(d) != 12 {
		t.Fatal("size")
	}
	u := mobileDocumentUploadRecord{SourcePath: "o/upload/y.bin", SourceSize: 3}
	if !mobileUploadHasSource(u) || mobileUploadSourceSize(u) != 3 {
		t.Fatal("upload helpers (offline trust)")
	}
	// Store online: upload path must exist on disk (no ghost SourceSize trust).
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	if mobileUploadHasSource(u) {
		t.Fatal("online store must not treat ghost SourceSize as available")
	}
}

func TestMobileDraftRepairSkipsWhenBlobRootMissing(t *testing.T) {
	// Transient store outage must not wipe durable SourcePath metadata.
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "not-mounted"))
	d := mobileDocumentDraftRecord{
		SourcePath: "owner/draft/id.bin", SourceSize: 42,
	}
	if mobileDraftRepairSourceMeta(&d) {
		t.Fatal("repair must no-op when blob root is missing")
	}
	if d.SourcePath != "owner/draft/id.bin" || d.SourceSize != 42 {
		t.Fatalf("meta wiped during outage: %#v", d)
	}
	u := mobileDocumentUploadRecord{
		SourcePath: "owner/upload/id.bin", SourceSize: 7,
	}
	if mobileUploadRepairSourceMeta(&u) {
		t.Fatal("upload repair must no-op when blob root is missing")
	}
	if u.SourcePath != "owner/upload/id.bin" || u.SourceSize != 7 {
		t.Fatalf("upload meta wiped: %#v", u)
	}
	// Size without path is still safe to clear (inconsistent meta).
	d2 := mobileDocumentDraftRecord{SourceSize: 9}
	if !mobileDraftRepairSourceMeta(&d2) || d2.SourceSize != 0 {
		t.Fatalf("size-only inconsistency not cleared: %#v", d2)
	}
}

func TestMobileShouldClearSourceMetaAfterStreamFail(t *testing.T) {
	// Empty path → nothing durable to protect.
	if !mobileShouldClearSourceMetaAfterStreamFail("") {
		t.Fatal("empty path should clear")
	}
	// Store offline → never clear a real path.
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "offline-root"))
	if mobileShouldClearSourceMetaAfterStreamFail("owner/draft/x.bin") {
		t.Fatal("must not clear during store outage")
	}
	// Store online + missing file → clear.
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	if !mobileShouldClearSourceMetaAfterStreamFail("owner/draft/missing.bin") {
		t.Fatal("confirmed missing blob should clear")
	}
	// Store online + present file → do not clear (open may have failed for other reasons).
	rel, err := mobileWriteDocumentBlob("owner", "draft", "present", []byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	if mobileShouldClearSourceMetaAfterStreamFail(rel) {
		t.Fatal("present blob must not clear meta after stream fail")
	}
}

func TestMobilePersistLargeFileDiskOnlyNoHotCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	raw := bytes.Repeat([]byte("L"), mobileDocumentSourceHotCacheMax+64)
	path, size, mem := mobilePersistDocumentOriginal("u", "draft", "big1", raw)
	if path == "" || size != len(raw) {
		t.Fatalf("path=%q size=%d", path, size)
	}
	if mem != nil {
		t.Fatalf("large file must not keep hot memory cache, mem len=%d", len(mem))
	}
	draft := mobileDocumentDraftRecord{
		ID: "big1", OwnerID: "u", SourcePath: path, SourceSize: size,
	}
	got := mobileDraftLoadSourceBytes(&draft)
	if !bytes.Equal(got, raw) {
		t.Fatalf("lazy load size=%d", len(got))
	}
	// Large reads must not re-cache into SourceBytes.
	if len(draft.SourceBytes) != 0 {
		t.Fatal("large load should not populate SourceBytes cache")
	}
}

func TestMobileReleaseUploadOriginalAfterReady(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	clearMobileStateForTest(t)

	raw := []byte("shared-original")
	upPath, _, upMem := mobilePersistDocumentOriginal("ow", "upload", "task1", raw)
	drPath, _, drMem := mobilePersistDocumentOriginal("ow", "draft", "d1", raw)

	mobileDocuments.Lock()
	mobileDocuments.drafts["d1"] = mobileDocumentDraftRecord{
		ID: "d1", OwnerID: "ow", SourcePath: drPath, SourceSize: len(raw), SourceBytes: drMem,
	}
	rec := mobileDocumentUploadRecord{
		TaskID: "task1", OwnerID: "ow", DraftID: "d1", Status: "ready",
		SourcePath: upPath, SourceSize: len(raw), SourceBytes: upMem,
	}
	if !mobileReleaseUploadOriginalAfterReady(&rec) {
		t.Fatal("expected dirty=true after release")
	}
	mobileDocuments.Unlock()

	if rec.SourcePath != "" || rec.SourceSize != 0 || rec.SourceBytes != nil {
		t.Fatalf("upload original not released: %#v", rec)
	}
	if _, err := mobileReadDocumentBlob(upPath); err == nil {
		t.Fatal("upload blob should be deleted")
	}
	// Draft blob remains.
	got, err := mobileReadDocumentBlob(drPath)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("draft blob err=%v got=%q", err, got)
	}
}

func TestMobileReleaseUploadOriginalKeepsUploadWhenDraftGhost(t *testing.T) {
	// Ghost draft SourceSize must not free the only real upload original.
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	clearMobileStateForTest(t)

	raw := []byte("only-on-upload")
	upPath, _, upMem := mobilePersistDocumentOriginal("ow", "upload", "keep1", raw)

	mobileDocuments.Lock()
	mobileDocuments.drafts["ghost-d"] = mobileDocumentDraftRecord{
		ID: "ghost-d", OwnerID: "ow",
		SourcePath: "missing/draft-ghost.bin", SourceSize: 9999, // file does not exist
	}
	rec := mobileDocumentUploadRecord{
		TaskID: "keep1", OwnerID: "ow", DraftID: "ghost-d", Status: "ready",
		SourcePath: upPath, SourceSize: len(raw), SourceBytes: upMem,
	}
	dirty := mobileReleaseUploadOriginalWhenDraftOwns(&rec)
	draft := mobileDocuments.drafts["ghost-d"]
	mobileDocuments.Unlock()

	if !dirty {
		t.Fatal("expected dirty=true from ghost draft repair")
	}
	if draft.SourcePath != "" || draft.SourceSize != 0 {
		t.Fatalf("ghost draft not repaired: path=%q size=%d", draft.SourcePath, draft.SourceSize)
	}
	if rec.SourcePath != upPath || rec.SourceSize != len(raw) {
		t.Fatalf("upload original must be kept: path=%q size=%d", rec.SourcePath, rec.SourceSize)
	}
	got, err := mobileReadDocumentBlob(upPath)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("upload blob lost: err=%v got=%q", err, got)
	}
}

func TestMobileStripKeepsBytesWhenBlobWriteFails(t *testing.T) {
	// No blob dir / empty state path → write fails → must keep SourceBytes.
	t.Setenv(mobileBlobDirEnv, "")
	t.Setenv(mobileStatePathEnv, "")
	mobileStatePathOverride = ""

	raw := []byte("only-in-memory")
	out := mobileStripDraftBlobForPersist(mobileDocumentDraftRecord{
		ID: "d", OwnerID: "o", SourceBytes: raw, SourceSize: len(raw),
	})
	if out.SourcePath != "" {
		t.Fatalf("unexpected path %q", out.SourcePath)
	}
	if !bytes.Equal(out.SourceBytes, raw) {
		t.Fatal("must retain SourceBytes when disk write is impossible")
	}
}

func TestMobileDraftRepairClearsMissingBlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	d := mobileDocumentDraftRecord{
		SourcePath: "missing/owner/x.bin", SourceSize: 99,
	}
	if !mobileDraftRepairSourceMeta(&d) {
		t.Fatal("expected repair")
	}
	if d.SourcePath != "" || d.SourceSize != 0 {
		t.Fatalf("not cleared: %#v", d)
	}
	if mobileDraftHasOriginal(d) {
		t.Fatal("should not have original after repair")
	}
}

func TestMobileDocumentUploadPayloadTrackedReportsDraftRepair(t *testing.T) {
	clearMobileStateForTest(t)
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	mobileDocuments.Lock()
	mobileDocuments.drafts["pd"] = mobileDocumentDraftRecord{
		ID: "pd", OwnerID: "ow", Title: "t",
		SourcePath: "missing/x.bin", SourceSize: 9, SourceFilename: "a.png",
	}
	mobileDocuments.uploads["pu"] = mobileDocumentUploadRecord{
		TaskID: "pu", OwnerID: "ow", DraftID: "pd",
		Filename: "a.png", Status: "needs_ocr",
	}
	payload, repaired := mobileDocumentUploadPayloadTracked(mobileDocuments.uploads["pu"])
	draft := mobileDocuments.drafts["pd"]
	mobileDocuments.Unlock()

	if !repaired {
		t.Fatal("expected repaired=true for missing draft blob")
	}
	if draft.SourcePath != "" || draft.SourceSize != 0 {
		t.Fatalf("draft meta not cleared: path=%q size=%d", draft.SourcePath, draft.SourceSize)
	}
	// Nested draft should report no original after repair.
	if nested, ok := payload["draft"].(map[string]any); ok {
		if nested["has_original"] == true {
			t.Fatalf("nested draft still has_original: %#v", nested)
		}
	}
	// Upload has no source and draft no longer has original → no download URL.
	if _, ok := payload["source_download_url"]; ok {
		t.Fatal("expected no source_download_url after repair emptied both sides")
	}
}

func TestMobileUploadSourceAvailableRejectsGhostUpload(t *testing.T) {
	// Ghost upload SourceSize must not make a task claimable.
	clearMobileStateForTest(t)
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	mobileDocuments.Lock()
	mobileDocuments.uploads["ghost-up"] = mobileDocumentUploadRecord{
		TaskID: "ghost-up", OwnerID: "ow", Status: "needs_ocr",
		SourcePath: "missing/up-ghost.bin", SourceSize: 50_000,
	}
	// No draft fallback.
	ok, repaired := mobileUploadSourceAvailable(mobileDocuments.uploads["ghost-up"])
	up := mobileDocuments.uploads["ghost-up"]
	mobileDocuments.Unlock()

	if ok {
		t.Fatal("ghost upload must not be source-available")
	}
	if !repaired {
		t.Fatal("expected repaired=true for ghost upload")
	}
	if up.SourcePath != "" || up.SourceSize != 0 {
		t.Fatalf("ghost upload meta not cleared: path=%q size=%d", up.SourcePath, up.SourceSize)
	}
}

func TestMobileUploadPayloadTrackedClearsGhostUploadSourceURL(t *testing.T) {
	clearMobileStateForTest(t)
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)

	mobileDocuments.Lock()
	mobileDocuments.uploads["pu-ghost"] = mobileDocumentUploadRecord{
		TaskID: "pu-ghost", OwnerID: "ow", Status: "needs_ocr",
		Filename:   "a.png",
		SourcePath: "missing/up.bin", SourceSize: 12,
	}
	payload, repaired := mobileDocumentUploadPayloadTracked(mobileDocuments.uploads["pu-ghost"])
	up := mobileDocuments.uploads["pu-ghost"]
	mobileDocuments.Unlock()

	if !repaired {
		t.Fatal("expected repaired=true")
	}
	if up.SourcePath != "" || up.SourceSize != 0 {
		t.Fatalf("upload ghost not cleared: %#v", up)
	}
	if _, ok := payload["source_download_url"]; ok {
		t.Fatal("must not advertise source_download_url for ghost upload")
	}
}

func TestMobileDraftRepairPersistsOutOfStateJSON(t *testing.T) {
	// After repair, path must not reappear from a subsequent strip/persist cycle.
	statePath := filepath.Join(t.TempDir(), "mobile-state.json")
	blobRoot := t.TempDir()
	t.Setenv(mobileStatePathEnv, statePath)
	t.Setenv(mobileBlobDirEnv, blobRoot)
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
		mobileDocuments.Lock()
		mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
		mobileDocuments.Unlock()
	})

	mobileDocuments.Lock()
	mobileDocuments.drafts["gone1"] = mobileDocumentDraftRecord{
		ID: "gone1", OwnerID: "o1", Title: "t", Markdown: "# x",
		SourcePath: "o1/draft/gone1.bin", SourceSize: 42,
		SourceFilename: "a.bin",
	}
	rec := mobileDocuments.drafts["gone1"]
	if !mobileDraftRepairSourceMeta(&rec) {
		t.Fatal("expected repair for missing blob")
	}
	mobileDocuments.drafts["gone1"] = rec
	mobileDocuments.Unlock()

	mobilePersistState()
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state mobilePersistentState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	got := state.Drafts["gone1"]
	if got.SourcePath != "" || got.SourceSize != 0 {
		t.Fatalf("persisted stale original meta: path=%q size=%d", got.SourcePath, got.SourceSize)
	}
	if mobileDraftHasOriginal(got) {
		t.Fatal("persisted draft still claims original")
	}
}

func TestMobileWriteOriginalHTTPStreamsDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	raw := []byte("stream-me-from-disk")
	rel, err := mobileWriteDocumentBlob("o", "draft", "s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if !mobileWriteOriginalHTTP(rec, "text/plain", "a.txt", nil, rel) {
		t.Fatal("stream failed")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("body=%q", rec.Body.Bytes())
	}
	if rec.Header().Get("Content-Length") != strconv.Itoa(len(raw)) {
		t.Fatalf("cl=%s", rec.Header().Get("Content-Length"))
	}
}

func TestMobileDocumentOriginalDiskHTTPRoundTrip(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "blob-http@example.com")
	clearMobileStateForTest(t)

	statePath := filepath.Join(t.TempDir(), "mobile-state.json")
	blobRoot := t.TempDir()
	t.Setenv(mobileStatePathEnv, statePath)
	t.Setenv(mobileBlobDirEnv, blobRoot)
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
	})

	// Seed a draft with disk-only original (simulates post-restart large file).
	raw := []byte("HTTP disk original content")
	rel, err := mobileWriteDocumentBlob(enroll.UserID, "draft", "httpd1", raw)
	if err != nil {
		t.Fatal(err)
	}
	draftID := "httpd1"
	mobileDocuments.Lock()
	mobileDocuments.drafts[draftID] = mobileDocumentDraftRecord{
		ID: draftID, OwnerID: enroll.UserID, TenantID: enroll.TenantID, Title: "T", Markdown: "# note",
		SourceFilename: "note.txt", SourceContentType: "text/plain",
		SourcePath: rel, SourceSize: len(raw), UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()

	// Download original via authenticated handler.
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts/"+draftID+"/source", nil)
	req.SetPathValue("draftId", draftID)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentDraftSourceHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("body=%q", rec.Body.Bytes())
	}

	// Delete draft removes blob.
	del := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/drafts/"+draftID, nil)
	del.SetPathValue("draftId", draftID)
	del.Header.Set("Authorization", "Bearer "+token)
	delRec := httptest.NewRecorder()
	MobileDocumentDraftUpdateHandler(identity).ServeHTTP(delRec, del)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}
	if _, err := mobileReadDocumentBlob(rel); err == nil {
		t.Fatal("blob should be removed after draft delete")
	}
}

func TestMobileUploadSourceFallsBackToDraftOriginal(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "blob-fallback@example.com")
	clearMobileStateForTest(t)

	blobRoot := t.TempDir()
	t.Setenv(mobileBlobDirEnv, blobRoot)

	raw := []byte("draft-owned-original-for-ocr")
	rel, err := mobileWriteDocumentBlob(enroll.UserID, "draft", "fb-draft", raw)
	if err != nil {
		t.Fatal(err)
	}
	mobileDocuments.Lock()
	mobileDocuments.drafts["fb-draft"] = mobileDocumentDraftRecord{
		ID: "fb-draft", OwnerID: enroll.UserID, TenantID: enroll.TenantID, Title: "img",
		SourceFilename: "shot.png", SourceContentType: "image/png",
		SourcePath: rel, SourceSize: len(raw), UpdatedAt: time.Now().UTC(),
	}
	// Upload has no source (released) but is still claimable for OCR.
	mobileDocuments.uploads["fb-task"] = mobileDocumentUploadRecord{
		TaskID: "fb-task", OwnerID: enroll.UserID, TenantID: enroll.TenantID, DraftID: "fb-draft",
		Filename: "shot.png", ContentType: "image/png",
		Status: "needs_ocr", ClaimedBy: enroll.MachineID,
		UploadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()

	mobileDocuments.Lock()
	uploadRec := mobileDocuments.uploads["fb-task"]
	okAvail, _ := mobileUploadSourceAvailable(uploadRec)
	mobileDocuments.Unlock()
	if !okAvail {
		t.Fatal("expected draft fallback source available")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/fb-task/source", nil)
	req.SetPathValue("taskId", "fb-task")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadSourceHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("body=%q want draft original", rec.Body.Bytes())
	}
}

func TestMobileUploadSource404ClearsGhostDraftFallbackMeta(t *testing.T) {
	// Upload has no source; draft points at a missing blob. Source GET must 404
	// and drop draft SourcePath/Size so workers stop advertising a download URL.
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "blob-stale-fallback@example.com")
	clearMobileStateForTest(t)

	blobRoot := t.TempDir()
	t.Setenv(mobileBlobDirEnv, blobRoot)
	statePath := filepath.Join(t.TempDir(), "mobile-state.json")
	t.Setenv(mobileStatePathEnv, statePath)
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
	})

	mobileDocuments.Lock()
	mobileDocuments.drafts["stale-fb"] = mobileDocumentDraftRecord{
		ID: "stale-fb", OwnerID: enroll.UserID, TenantID: enroll.TenantID, Title: "gone",
		SourceFilename: "gone.bin", SourceContentType: "application/octet-stream",
		SourcePath: "missing/ghost-stale.bin", SourceSize: 99,
		UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.uploads["stale-task"] = mobileDocumentUploadRecord{
		TaskID: "stale-task", OwnerID: enroll.UserID, TenantID: enroll.TenantID, DraftID: "stale-fb",
		Filename: "gone.bin", Status: "needs_ocr", ClaimedBy: enroll.MachineID,
		UploadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/stale-task/source", nil)
	req.SetPathValue("taskId", "stale-task")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadSourceHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", rec.Code, rec.Body.String())
	}
	mobileDocuments.Lock()
	draft := mobileDocuments.drafts["stale-fb"]
	mobileDocuments.Unlock()
	if draft.SourcePath != "" || draft.SourceSize != 0 {
		t.Fatalf("draft meta not cleared: path=%q size=%d", draft.SourcePath, draft.SourceSize)
	}
}

func TestMobileDocumentUploadMultipartPersistsOriginalToDisk(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "blob-upload@example.com")
	clearMobileStateForTest(t)

	statePath := filepath.Join(t.TempDir(), "mobile-state.json")
	blobRoot := t.TempDir()
	t.Setenv(mobileStatePathEnv, statePath)
	t.Setenv(mobileBlobDirEnv, blobRoot)
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = true
	mobileStatePersistence.Unlock()
	t.Cleanup(func() {
		mobileStatePersistence.Lock()
		mobileStatePersistence.loaded = false
		mobileStatePersistence.Unlock()
	})

	raw := []byte("multipart original body for disk store")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("filename", "memo.txt")
	fw, err := mw.CreateFormFile("file", "memo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	MobileDocumentUploadHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	draftID, _ := payload["draft_id"].(string)
	if draftID == "" {
		if d, ok := payload["draft"].(map[string]any); ok {
			draftID, _ = d["id"].(string)
		}
	}
	if draftID == "" {
		t.Fatalf("no draft_id in %#v", payload)
	}

	mobileDocuments.Lock()
	draft, ok := mobileDocuments.drafts[draftID]
	uploadTaskID, _ := payload["task_id"].(string)
	upload := mobileDocuments.uploads[uploadTaskID]
	mobileDocuments.Unlock()
	if !ok {
		t.Fatal("draft missing")
	}
	if !mobileDraftHasOriginal(draft) || draft.SourcePath == "" {
		t.Fatalf("draft original not on disk: path=%q size=%d mem=%d", draft.SourcePath, draft.SourceSize, len(draft.SourceBytes))
	}
	got, err := mobileReadDocumentBlob(draft.SourcePath)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("draft blob err=%v got=%q", err, got)
	}
	// ready text imports should release upload-side original.
	if upload.Status == "ready" && mobileUploadHasSource(upload) {
		t.Fatalf("upload source should be released after ready: path=%q size=%d", upload.SourcePath, upload.SourceSize)
	}

	// Download via source API.
	srcReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts/"+draftID+"/source", nil)
	srcReq.SetPathValue("draftId", draftID)
	srcReq.Header.Set("Authorization", "Bearer "+token)
	srcRec := httptest.NewRecorder()
	MobileDocumentDraftSourceHandler(identity).ServeHTTP(srcRec, srcReq)
	if srcRec.Code != http.StatusOK || !bytes.Equal(srcRec.Body.Bytes(), raw) {
		t.Fatalf("source status=%d body=%q", srcRec.Code, srcRec.Body.Bytes())
	}

	// state.json must not embed SourceBytes (markdown may still hold text extract).
	mobilePersistState()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state mobilePersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	persisted, ok := state.Drafts[draftID]
	if !ok {
		t.Fatal("draft missing from state")
	}
	if len(persisted.SourceBytes) != 0 {
		t.Fatal("state.json still embeds SourceBytes")
	}
	if persisted.SourcePath == "" || persisted.SourceSize != len(raw) {
		t.Fatalf("path=%q size=%d", persisted.SourcePath, persisted.SourceSize)
	}
	_ = enroll
}
