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
	"strings"
	"testing"
	"time"
)

func resetCompanionDocShares(t *testing.T) {
	t.Helper()
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
		companionDocShares.Lock()
		companionDocShares.loaded = false
		companionDocShares.items = map[string]companionDocShare{}
		companionDocShares.Unlock()
		resetCompanionDocGroups()
	})
}

func postCompanionShare(t *testing.T, identityToken, visibility, password, days, filename string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", visibility)
	_ = writer.WriteField("password", password)
	_ = writer.WriteField("days", days)
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
	identity, _, _ := newHTTPAPITestServices(t)
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	return rec
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
