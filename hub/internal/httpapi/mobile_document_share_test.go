package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func resetCompanionDocShares(t *testing.T) {
	t.Helper()
	companionSharePersistWG.Wait()
	root := t.TempDir()
	t.Setenv(companionDocSharePathEnv, filepath.Join(root, "shares.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(root, "blobs"))
	t.Setenv(mobileStatePathEnv, filepath.Join(root, "state.json"))
	companionDocShares.Lock()
	companionDocShares.loaded = false
	companionDocShares.items = map[string]companionDocShare{}
	companionDocShares.Unlock()
	resetCompanionDocGroups()
	t.Cleanup(func() {
		companionSharePersistWG.Wait()
		companionDocShares.Lock()
		companionDocShares.loaded = false
		companionDocShares.items = map[string]companionDocShare{}
		companionDocShares.Unlock()
		resetCompanionDocGroups()
	})
}

func TestCompanionDocumentSharePublicImportAndPrivatePassword(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, owner := issueViewerToken(t, identity, "share-owner@example.com")
	otherToken, other := issueViewerToken(t, identity, "share-reader@example.com")
	payload := []byte("%PDF-1.4 shared")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", "public")
	_ = writer.WriteField("password", "ignored")
	_ = writer.WriteField("days", "0")
	part, err := writer.CreateFormFile("file", "论文.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &buf)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["permanent"] != true || created["visibility"] != "public" || created["needs_password"] != false {
		t.Fatalf("created: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "password_salt") {
		t.Fatalf("response leaked a password field: %s", rec.Body.String())
	}
	token, _ := created["token"].(string)
	if !companionShareTokenPattern.MatchString(token) {
		t.Fatalf("token: %q", token)
	}
	url, _ := created["url"].(string)
	if !strings.Contains(url, "/hub/documents/shares/"+token) {
		t.Fatalf("url: %q", url)
	}

	metaReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token, nil)
	metaReq.SetPathValue("token", token)
	metaRec := httptest.NewRecorder()
	CompanionDocumentShareHandler(identity).ServeHTTP(metaRec, metaReq)
	if metaRec.Code != http.StatusOK || !strings.Contains(metaRec.Body.String(), "论文.pdf") {
		t.Fatalf("meta: %d %s", metaRec.Code, metaRec.Body.String())
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/hub/documents/shares/"+token, nil)
	pageReq.SetPathValue("token", token)
	pageRec := httptest.NewRecorder()
	CompanionDocumentSharePageHandler().ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK || !strings.Contains(pageRec.Body.String(), "文档管理") {
		t.Fatalf("page: %d %s", pageRec.Code, pageRec.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(`{}`))
	importReq.SetPathValue("token", token)
	importReq.Header.Set("Authorization", "Bearer "+otherToken)
	importRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusOK || !bytes.Equal(importRec.Body.Bytes(), payload) {
		t.Fatalf("import: %d %s", importRec.Code, importRec.Body.String())
	}
	if importRec.Header().Get("X-Companion-Document-Name") != "论文.pdf" {
		t.Fatalf("name header: %q", importRec.Header().Get("X-Companion-Document-Name"))
	}
	if importRec.Header().Get("Content-Length") != strconv.Itoa(len(payload)) {
		t.Fatalf("content length: %q", importRec.Header().Get("Content-Length"))
	}
	shareIndex := os.Getenv(companionDocSharePathEnv)
	companionSharePersistWG.Wait()
	indexBefore, err := os.Stat(shareIndex)
	if err != nil {
		t.Fatal(err)
	}
	againReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(`{}`))
	againReq.SetPathValue("token", token)
	againReq.Header.Set("Authorization", "Bearer "+otherToken)
	againRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(againRec, againReq)
	if againRec.Code != http.StatusOK || !bytes.Equal(againRec.Body.Bytes(), payload) {
		t.Fatalf("repeat import: %d %s", againRec.Code, againRec.Body.String())
	}
	indexAfter, err := os.Stat(shareIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !indexBefore.ModTime().Equal(indexAfter.ModTime()) {
		t.Fatal("repeat import rewrote the share index")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	listReq.Header.Set("Authorization", "Bearer "+otherToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), token) {
		t.Fatalf("received: %d %s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Mine     []map[string]any `json:"mine"`
		Received []map[string]any `json:"received"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Received) != 1 || len(listed.Mine) != 0 {
		t.Fatalf("reader lists: %s", listRec.Body.String())
	}
	ownerList := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	ownerList.Header.Set("Authorization", "Bearer "+ownerToken)
	ownerRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(ownerRec, ownerList)
	if err := json.Unmarshal(ownerRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Mine) != 1 || listed.Mine[0]["token"] != token {
		t.Fatalf("owner list: %s", ownerRec.Body.String())
	}
	if owner.UserID == other.UserID {
		t.Fatal("fixture users collapsed")
	}

	priv := bytes.Buffer{}
	privWriter := multipart.NewWriter(&priv)
	_ = privWriter.WriteField("visibility", "private")
	_ = privWriter.WriteField("password", "secret")
	_ = privWriter.WriteField("days", "3")
	privPart, err := privWriter.CreateFormFile("file", "notes.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := privPart.Write([]byte("private-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := privWriter.Close(); err != nil {
		t.Fatal(err)
	}
	privReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &priv)
	privReq.Header.Set("Authorization", "Bearer "+ownerToken)
	privReq.Header.Set("Content-Type", privWriter.FormDataContentType())
	privRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(privRec, privReq)
	if privRec.Code != http.StatusOK {
		t.Fatalf("private create: %d %s", privRec.Code, privRec.Body.String())
	}
	var privCreated map[string]any
	if err := json.Unmarshal(privRec.Body.Bytes(), &privCreated); err != nil {
		t.Fatal(err)
	}
	privToken, _ := privCreated["token"].(string)
	if privCreated["needs_password"] != true || privCreated["permanent"] != false {
		t.Fatalf("private view: %s", privRec.Body.String())
	}
	denied := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+privToken+"/import", strings.NewReader(`{"password":"nope"}`))
	denied.SetPathValue("token", privToken)
	denied.Header.Set("Authorization", "Bearer "+otherToken)
	deniedRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusForbidden || !strings.Contains(deniedRec.Body.String(), "分享密码不正确") {
		t.Fatalf("wrong password: %d %s", deniedRec.Code, deniedRec.Body.String())
	}
	missing := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+privToken+"/import", strings.NewReader(`{}`))
	missing.SetPathValue("token", privToken)
	missing.Header.Set("Authorization", "Bearer "+otherToken)
	missingRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(missingRec, missing)
	if missingRec.Code != http.StatusUnauthorized || !strings.Contains(missingRec.Body.String(), "需要分享密码") {
		t.Fatalf("missing password: %d %s", missingRec.Code, missingRec.Body.String())
	}
	okReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+privToken+"/import", strings.NewReader(`{"password":"secret"}`))
	okReq.SetPathValue("token", privToken)
	okReq.Header.Set("Authorization", "Bearer "+otherToken)
	okRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK || string(okRec.Body.Bytes()) != "private-bytes" {
		t.Fatalf("right password: %d %s", okRec.Code, okRec.Body.String())
	}

	companionDocShares.Lock()
	share := companionDocShares.items[token]
	share.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	companionDocShares.items[token] = share
	companionDocShares.Unlock()
	late := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(`{}`))
	late.SetPathValue("token", token)
	late.Header.Set("Authorization", "Bearer "+otherToken)
	lateRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(lateRec, late)
	if lateRec.Code != http.StatusGone || !strings.Contains(lateRec.Body.String(), "分享链接已过期") {
		t.Fatalf("expired: %d %s", lateRec.Code, lateRec.Body.String())
	}
	plain := httptest.NewRequest(http.MethodGet, "/hub/documents/shares/not-a-share", nil)
	plain.SetPathValue("token", "not-a-share")
	plainRec := httptest.NewRecorder()
	CompanionDocumentSharePageHandler().ServeHTTP(plainRec, plain)
	if plainRec.Code != http.StatusNotFound || !strings.Contains(plainRec.Body.String(), "不是伴读文档分享") {
		t.Fatalf("plain: %d %s", plainRec.Code, plainRec.Body.String())
	}
	short := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", mustShortShareBody(t))
	short.Header.Set("Authorization", "Bearer "+ownerToken)
	short.Header.Set("Content-Type", shortShareContentType)
	shortRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(shortRec, short)
	if shortRec.Code != http.StatusBadRequest || !strings.Contains(shortRec.Body.String(), "分享密码需为 4–64 个字符") {
		t.Fatalf("short password: %d %s", shortRec.Code, shortRec.Body.String())
	}
}

var shortShareContentType string

func mustShortShareBody(t *testing.T) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", "private")
	_ = writer.WriteField("password", "abc")
	_ = writer.WriteField("days", "1")
	part, err := writer.CreateFormFile("file", "a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("abc"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	shortShareContentType = writer.FormDataContentType()
	return &buf
}

func TestCompanionDocumentShareStateReloads(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "share-reload@example.com")
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", "public")
	_ = writer.WriteField("days", "0")
	part, err := writer.CreateFormFile("file", "keep.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("kept"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	companionDocShares.Lock()
	companionDocShares.loaded = false
	companionDocShares.items = map[string]companionDocShare{}
	companionDocShares.Unlock()
	list := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	list.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "keep.pdf") {
		t.Fatalf("reload: %d %s", listRec.Code, listRec.Body.String())
	}
	if _, err := os.Stat(companionDocShareStatePath()); err != nil {
		t.Fatal(err)
	}
}

func TestCompanionDocumentShareUpdateChangesAudienceAndExpiry(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "share-editor@example.com")
	otherToken, _ := issueViewerToken(t, identity, "share-outsider@example.com")
	// The helper builds its own service, which would mint a token this one
	// cannot read, so the create request is built here.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", "public")
	_ = writer.WriteField("days", "0")
	part, err := writer.CreateFormFile("file", "paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("%PDF-update")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &buf)
	createReq.Header.Set("Authorization", "Bearer "+ownerToken)
	createReq.Header.Set("Content-Type", writer.FormDataContentType())
	created := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(created, createReq)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	token, _ := view["token"].(string)
	if !companionShareTokenPattern.MatchString(token) {
		t.Fatalf("token: %q", token)
	}

	patch := func(identityToken, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/shares/"+token, strings.NewReader(body))
		req.SetPathValue("token", token)
		req.Header.Set("Authorization", "Bearer "+identityToken)
		rec := httptest.NewRecorder()
		CompanionDocumentShareUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}

	// Someone else's link is not theirs to change.
	if rec := patch(otherToken, `{"visibility":"private","password":"secret","days":7}`); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider: %d %s", rec.Code, rec.Body.String())
	}
	if rec := patch(ownerToken, `{"visibility":"nope","days":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad audience: %d %s", rec.Code, rec.Body.String())
	}
	if rec := patch(ownerToken, `{"visibility":"private","days":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("private without a password: %d %s", rec.Code, rec.Body.String())
	}

	// Public → private with a password, and a fresh expiry.
	if rec := patch(ownerToken, `{"visibility":"private","password":"secret","days":7}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	share, ok := companionShareByToken(token)
	if !ok {
		t.Fatal("share disappeared")
	}
	if share.Visibility != "private" || !companionSharePasswordOK(share, "secret") {
		t.Fatalf("share = %#v", share)
	}
	if share.ExpiresAt.IsZero() || time.Until(share.ExpiresAt) < 6*24*time.Hour {
		t.Fatalf("expiry = %v", share.ExpiresAt)
	}
	// Changing only the audience leaves the expiry alone: a request without a
	// days field must not silently turn a dated link permanent.
	before := share.PasswordHash
	if rec := patch(ownerToken, `{"visibility":"private"}`); rec.Code != http.StatusOK {
		t.Fatalf("keep expiry: %d %s", rec.Code, rec.Body.String())
	}
	share, _ = companionShareByToken(token)
	if share.PasswordHash != before || !companionSharePasswordOK(share, "secret") {
		t.Fatalf("password changed: %#v", share)
	}
	if share.ExpiresAt.IsZero() {
		t.Fatalf("expiry was dropped by a request that did not mention it")
	}
	// Changing only the expiry keeps the password already stored.
	before = share.PasswordHash
	if rec := patch(ownerToken, `{"visibility":"private","days":0}`); rec.Code != http.StatusOK {
		t.Fatalf("keep password: %d %s", rec.Code, rec.Body.String())
	}
	share, _ = companionShareByToken(token)
	if share.PasswordHash != before || !companionSharePasswordOK(share, "secret") {
		t.Fatalf("password changed: %#v", share)
	}
	if !share.ExpiresAt.IsZero() {
		t.Fatalf("permanent expected, got %v", share.ExpiresAt)
	}
	// Back to public drops the password entirely.
	if rec := patch(ownerToken, `{"visibility":"public","days":0}`); rec.Code != http.StatusOK {
		t.Fatalf("public: %d %s", rec.Code, rec.Body.String())
	}
	share, _ = companionShareByToken(token)
	if share.PasswordHash != "" || share.PasswordSalt != "" || share.Visibility != "public" {
		t.Fatalf("public share kept a password: %#v", share)
	}
	// The reader who imported the public link is no longer in 收到的分享:
	// the list is what the desktop draws, so the change has to show there.
	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	listReq.Header.Set("Authorization", "Bearer "+ownerToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"permanent":true`) {
		t.Fatalf("list after update: %d %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "password_hash") {
		t.Fatalf("list leaked a password: %s", listRec.Body.String())
	}
}

func postCompanionShareWithFields(t *testing.T, identity *auth.IdentityService, identityToken, visibility, password, target, days, filename, contentHash string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", visibility)
	_ = writer.WriteField("password", password)
	_ = writer.WriteField("target", target)
	_ = writer.WriteField("days", days)
	_ = writer.WriteField("content_hash", contentHash)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &buf)
	req.Header.Set("Authorization", "Bearer "+identityToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	return rec
}

func companionShareTokenFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("create view: %v (%s)", err, rec.Body.String())
	}
	token, _ := view["token"].(string)
	if !companionShareTokenPattern.MatchString(token) {
		t.Fatalf("token: %q (%s)", token, rec.Body.String())
	}
	return token
}

func deleteCompanionShareAs(t *testing.T, identity *auth.IdentityService, identityToken, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/shares/"+token, nil)
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+identityToken)
	rec := httptest.NewRecorder()
	CompanionDocumentShareDeleteHandler(identity).ServeHTTP(rec, req)
	return rec
}

func TestCompanionDocumentShareRejectsDuplicatePaperAndScope(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "dup-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "dup-other@example.com")
	paper := []byte("%PDF-duplicate")
	hash := companionShareContentHash(paper)

	create := func(token, visibility, password, target, contentHash string, body []byte) *httptest.ResponseRecorder {
		return postCompanionShareWithFields(t, identity, token, visibility, password, target, "0", "论文.pdf", contentHash, body)
	}

	first := create(ownerToken, "public", "", "", hash, paper)
	if first.Code != http.StatusOK {
		t.Fatalf("first share: %d %s", first.Code, first.Body.String())
	}
	firstToken := companionShareTokenFrom(t, first)

	duplicate := create(ownerToken, "public", "", "", hash, paper)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}
	if !strings.Contains(duplicate.Body.String(), "已经以相同的范围分享过") {
		t.Fatalf("duplicate message: %s", duplicate.Body.String())
	}
	var duplicatePayload map[string]any
	if err := json.Unmarshal(duplicate.Body.Bytes(), &duplicatePayload); err != nil {
		t.Fatalf("duplicate payload: %v", err)
	}
	if duplicatePayload["token"] != firstToken {
		t.Fatalf("duplicate token = %v, want %s", duplicatePayload["token"], firstToken)
	}
	if shared, ok := duplicatePayload["share"].(map[string]any); !ok || shared["token"] != firstToken {
		t.Fatalf("duplicate share = %v", duplicatePayload["share"])
	}

	// A different audience is a different link.
	if rec := create(ownerToken, "person", "", "reader@example.com", hash, paper); rec.Code != http.StatusOK {
		t.Fatalf("person share: %d %s", rec.Code, rec.Body.String())
	}
	// A private share is its own scope, and the password is not part of it:
	// the same paper with another password is still a duplicate.
	if rec := create(ownerToken, "private", "secret", "", hash, paper); rec.Code != http.StatusOK {
		t.Fatalf("private share: %d %s", rec.Code, rec.Body.String())
	}
	if rec := create(ownerToken, "private", "different", "", hash, paper); rec.Code != http.StatusConflict {
		t.Fatalf("private duplicate: %d %s", rec.Code, rec.Body.String())
	}
	// Another owner may publish the same paper to the same audience.
	if rec := create(otherToken, "public", "", "", hash, paper); rec.Code != http.StatusOK {
		t.Fatalf("other owner: %d %s", rec.Code, rec.Body.String())
	}
	// A different paper with the same name is not a duplicate.
	otherPaper := []byte("%PDF-different")
	if rec := create(ownerToken, "public", "", "", companionShareContentHash(otherPaper), otherPaper); rec.Code != http.StatusOK {
		t.Fatalf("different paper: %d %s", rec.Code, rec.Body.String())
	}

	// Once the earlier link expires, the same paper and audience can go out
	// again. The expired record stays behind; it no longer blocks anything.
	companionDocShares.Lock()
	share := companionDocShares.items[firstToken]
	share.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	companionDocShares.items[firstToken] = share
	companionDocShares.Unlock()
	if rec := create(ownerToken, "public", "", "", hash, paper); rec.Code != http.StatusOK {
		t.Fatalf("expired re-share: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCompanionDocumentShareUpdateRejectsDuplicateScope(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "dup-editor@example.com")
	paper := []byte("%PDF-update-dup")
	hash := companionShareContentHash(paper)

	create := func(visibility, target string) string {
		t.Helper()
		rec := postCompanionShareWithFields(t, identity, ownerToken, visibility, "", target, "0", "paper.pdf", hash, paper)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s: %d %s", visibility, rec.Code, rec.Body.String())
		}
		return companionShareTokenFrom(t, rec)
	}
	patched := func(token, visibility, password, target string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"visibility": visibility,
			"password":   password,
			"target":     target,
			"days":       0,
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/shares/"+token, bytes.NewReader(body))
		req.SetPathValue("token", token)
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		CompanionDocumentShareUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}

	publicToken := create("public", "")
	personToken := create("person", "reader@example.com")

	// Saving the link's own scope is not a conflict with itself.
	if rec := patched(publicToken, "public", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("self scope: %d %s", rec.Code, rec.Body.String())
	}
	// Moving the person link onto the public scope would leave two public
	// links for one paper.
	dup := patched(personToken, "public", "", "")
	if dup.Code != http.StatusConflict || !strings.Contains(dup.Body.String(), "请先删除另一份分享") {
		t.Fatalf("update duplicate: %d %s", dup.Code, dup.Body.String())
	}
	if rec := patched(publicToken, "private", "secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("move to private: %d %s", rec.Code, rec.Body.String())
	}
	if rec := patched(personToken, "private", "secret", ""); rec.Code != http.StatusConflict {
		t.Fatalf("second private: %d %s", rec.Code, rec.Body.String())
	}
	if rec := patched(personToken, "person", "", "reader@example.com"); rec.Code != http.StatusOK {
		t.Fatalf("own person scope: %d %s", rec.Code, rec.Body.String())
	}

	// Once the other private link expires, the same scope is free again.
	companionDocShares.Lock()
	share := companionDocShares.items[publicToken]
	share.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	companionDocShares.items[publicToken] = share
	companionDocShares.Unlock()
	if rec := patched(personToken, "private", "secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("expired other link: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCompanionDocumentShareDeleteRemovesLinkAndBlob(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "delete-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "delete-outsider@example.com")
	body := []byte("%PDF-delete")

	created := postCompanionShareWithFields(t, identity, ownerToken, "public", "", "", "0", "gone.pdf", "", body)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)
	share, ok := companionShareByToken(token)
	if !ok {
		t.Fatal("share disappeared after create")
	}
	blobPath, err := mobileBlobAbsPath(share.BlobPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("blob missing before delete: %v", err)
	}

	if rec := deleteCompanionShareAs(t, identity, otherToken, token); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := companionShareByToken(token); !ok {
		t.Fatal("outsider delete removed the share")
	}

	if rec := deleteCompanionShareAs(t, identity, ownerToken, token); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := companionShareByToken(token); ok {
		t.Fatal("share still in the store after delete")
	}
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("blob after delete: %v", err)
	}

	metaReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token, nil)
	metaReq.SetPathValue("token", token)
	metaRec := httptest.NewRecorder()
	CompanionDocumentShareHandler(identity).ServeHTTP(metaRec, metaReq)
	if metaRec.Code != http.StatusNotFound {
		t.Fatalf("metadata after delete: %d %s", metaRec.Code, metaRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	listReq.Header.Set("Authorization", "Bearer "+ownerToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, listReq)
	if strings.Contains(listRec.Body.String(), token) {
		t.Fatalf("list still carries the deleted token: %s", listRec.Body.String())
	}

	if rec := deleteCompanionShareAs(t, identity, ownerToken, token); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d %s", rec.Code, rec.Body.String())
	}
	// The same paper and audience can go out again after the link is gone.
	again := postCompanionShareWithFields(t, identity, ownerToken, "public", "", "", "0", "gone.pdf", "", body)
	if again.Code != http.StatusOK {
		t.Fatalf("re-share after delete: %d %s", again.Code, again.Body.String())
	}
	if next := companionShareTokenFrom(t, again); next == token {
		t.Fatalf("re-share reused the deleted token %s", token)
	}
}

func TestCompanionDocumentShareCrossTenantPublicAndPrivate(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken := issueViewerTokenForTenant(t, identity, "tenant-a", "owner-cross@example.com")
	readerToken := issueViewerTokenForTenant(t, identity, "tenant-b", "reader-cross@example.com")
	created := postCompanionShareWithFields(t, identity, ownerToken, "public", "", "", "0", "公开.pdf", "", []byte("%PDF-public"))
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	imported := postCompanionShareImport(t, identity, readerToken, token, `{}`)
	if imported.Code != http.StatusOK || string(imported.Body.Bytes()) != "%PDF-public" {
		t.Fatalf("cross-tenant import: %d %s", imported.Code, imported.Body.String())
	}
	if imported.Header().Get(companionReaderGrantHeader) != "" {
		t.Fatal("an account on this Hub should not be issued a guest grant")
	}
	list := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	list.Header.Set("Authorization", "Bearer "+readerToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), token) {
		t.Fatalf("received: %d %s", listRec.Code, listRec.Body.String())
	}

	secret := postCompanionShareWithFields(t, identity, ownerToken, "private", "secret", "", "0", "私密.pdf", "", []byte("%PDF-private"))
	if secret.Code != http.StatusOK {
		t.Fatalf("private: %d %s", secret.Code, secret.Body.String())
	}
	privateToken := companionShareTokenFrom(t, secret)
	denied := postCompanionShareImport(t, identity, readerToken, privateToken, `{}`)
	if denied.Code != http.StatusUnauthorized || !strings.Contains(denied.Body.String(), "需要分享密码") {
		t.Fatalf("private without password: %d %s", denied.Code, denied.Body.String())
	}
	opened := postCompanionShareImport(t, identity, readerToken, privateToken, `{"password":"secret"}`)
	if opened.Code != http.StatusOK || string(opened.Body.Bytes()) != "%PDF-private" {
		t.Fatalf("private import: %d %s", opened.Code, opened.Body.String())
	}
}

func TestCompanionDocumentShareGuestFromAnotherHub(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "owner-guest@example.com")
	created := postCompanionShareWithFields(t, identity, ownerToken, "public", "", "", "0", "跨hub.pdf", "", []byte("%PDF-guest"))
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	noAuth := postCompanionShareImport(t, identity, "", token, `{"reader_label":"reader@other.hub"}`)
	if noAuth.Code != http.StatusOK || string(noAuth.Body.Bytes()) != "%PDF-guest" {
		t.Fatalf("guest import: %d %s", noAuth.Code, noAuth.Body.String())
	}
	grant := noAuth.Header().Get(companionReaderGrantHeader)
	if !companionReaderGrantPattern.MatchString(grant) {
		t.Fatalf("grant: %q", grant)
	}
	if strings.Contains(noAuth.Body.String(), grant) {
		t.Fatal("grant leaked into the file body")
	}

	meta := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token, nil)
	meta.SetPathValue("token", token)
	metaRec := httptest.NewRecorder()
	CompanionDocumentShareHandler(identity).ServeHTTP(metaRec, meta)
	if metaRec.Code != http.StatusOK || strings.Contains(metaRec.Body.String(), `"notes"`) {
		t.Fatalf("metadata leaked notes: %d %s", metaRec.Code, metaRec.Body.String())
	}

	note := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/notes", strings.NewReader(`{"text":"其他 Hub 的问题"}`))
	note.SetPathValue("token", token)
	note.Header.Set(companionReaderGrantHeader, grant)
	noteRec := httptest.NewRecorder()
	CompanionDocumentShareNotesHandler(identity).ServeHTTP(noteRec, note)
	if noteRec.Code != http.StatusOK || !strings.Contains(noteRec.Body.String(), "reader@other.hub") || !strings.Contains(noteRec.Body.String(), "其他 Hub 的问题") {
		t.Fatalf("guest note: %d %s", noteRec.Code, noteRec.Body.String())
	}

	withGrant := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token, nil)
	withGrant.SetPathValue("token", token)
	withGrant.Header.Set(companionReaderGrantHeader, grant)
	withGrantRec := httptest.NewRecorder()
	CompanionDocumentShareHandler(identity).ServeHTTP(withGrantRec, withGrant)
	if withGrantRec.Code != http.StatusOK || !strings.Contains(withGrantRec.Body.String(), "其他 Hub 的问题") {
		t.Fatalf("grant read: %d %s", withGrantRec.Code, withGrantRec.Body.String())
	}

	ownerList := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	ownerList.Header.Set("Authorization", "Bearer "+ownerToken)
	ownerRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(ownerRec, ownerList)
	if !strings.Contains(ownerRec.Body.String(), "其他 Hub 的问题") {
		t.Fatalf("owner did not see the guest note: %s", ownerRec.Body.String())
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/sync", nil)
	syncReq.SetPathValue("token", token)
	syncReq.Header.Set(companionReaderGrantHeader, grant)
	syncRec := httptest.NewRecorder()
	CompanionDocumentShareSyncHandler(identity).ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusUnauthorized {
		t.Fatalf("guest sync: %d %s", syncRec.Code, syncRec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(`{}`))
	bad.SetPathValue("token", token)
	bad.Header.Set("Authorization", "Bearer not-a-session")
	badRec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad session should not become a guest: %d %s", badRec.Code, badRec.Body.String())
	}

	secret := postCompanionShareWithFields(t, identity, ownerToken, "private", "secret", "", "0", "私密.pdf", "", []byte("%PDF-secret"))
	if secret.Code != http.StatusOK {
		t.Fatalf("private: %d %s", secret.Code, secret.Body.String())
	}
	privateToken := companionShareTokenFrom(t, secret)
	needs := postCompanionShareImport(t, identity, "", privateToken, `{}`)
	if needs.Code != http.StatusUnauthorized || !strings.Contains(needs.Body.String(), "需要分享密码") {
		t.Fatalf("private guest: %d %s", needs.Code, needs.Body.String())
	}
	opened := postCompanionShareImport(t, identity, "", privateToken, `{"password":"secret","reader_label":"reader@other.hub"}`)
	if opened.Code != http.StatusOK {
		t.Fatalf("private open: %d %s", opened.Code, opened.Body.String())
	}
	privateGrant := opened.Header().Get(companionReaderGrantHeader)
	shareIndex := os.Getenv(companionDocSharePathEnv)
	companionSharePersistWG.Wait()
	indexBefore, err := os.Stat(shareIndex)
	if err != nil {
		t.Fatal(err)
	}
	again := postCompanionShareImport(t, identity, "", privateToken, `{"reader_grant":"`+privateGrant+`"}`)
	if again.Code != http.StatusOK || string(again.Body.Bytes()) != "%PDF-secret" {
		t.Fatalf("grant reimport: %d %s", again.Code, again.Body.String())
	}
	indexAfter, err := os.Stat(shareIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !indexBefore.ModTime().Equal(indexAfter.ModTime()) {
		t.Fatal("known guest import rewrote the share index")
	}

	person := postCompanionShareWithFields(t, identity, ownerToken, "person", "", "person@example.com", "0", "指定.pdf", "", []byte("%PDF-person"))
	if person.Code != http.StatusOK {
		t.Fatalf("person: %d %s", person.Code, person.Body.String())
	}
	personToken := companionShareTokenFrom(t, person)
	blocked := postCompanionShareImport(t, identity, "", personToken, `{}`)
	if blocked.Code != http.StatusUnauthorized || !strings.Contains(blocked.Body.String(), "只能在文档所在的 Hub 打开") {
		t.Fatalf("person guest: %d %s", blocked.Code, blocked.Body.String())
	}
}

func TestCompanionShareOlderSnapshotDoesNotReplaceANewerIndex(t *testing.T) {
	resetCompanionDocShares(t)
	const token = "cd_0123456789abcdef0123456789abcdef"
	companionDocShares.Lock()
	companionDocShares.loaded = true
	companionDocShares.items = map[string]companionDocShare{
		token: {Token: token, Name: "first"},
	}
	path, body, gen, err := takeCompanionShareSnapshotLocked()
	if err != nil || path == "" || gen == 0 || len(body) == 0 {
		companionDocShares.Unlock()
		t.Fatalf("snapshot: path=%q gen=%d err=%v", path, gen, err)
	}
	current := companionDocShares.items[token]
	current.Name = "second"
	companionDocShares.items[token] = current
	if err := persistCompanionDocSharesLocked(); err != nil {
		companionDocShares.Unlock()
		t.Fatal(err)
	}
	companionDocShares.Unlock()
	if err := writeCompanionShareSnapshot(path, body, gen); err != nil {
		t.Fatal(err)
	}
	companionDocShares.Lock()
	companionDocShares.loaded = false
	companionDocShares.items = map[string]companionDocShare{}
	companionDocShares.Unlock()
	loadCompanionDocShares()
	companionDocShares.Lock()
	got := companionDocShares.items[token].Name
	companionDocShares.Unlock()
	if got != "second" {
		t.Fatalf("older snapshot replaced the index: %q", got)
	}
}

func postCompanionShareImport(t *testing.T, identity *auth.IdentityService, bearer, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(body))
	req.SetPathValue("token", token)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(rec, req)
	return rec
}
