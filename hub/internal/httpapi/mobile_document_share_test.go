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
	setMobileStatePathForTest(t, filepath.Join(root, "state.json"))
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

func TestCompanionDocumentShareUpdateRevokesOldPrivateAccess(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "rotate-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "rotate-reader@example.com")
	created := postCompanionShareAs(t, identity, ownerToken, "public", "", "0", "", "rotate.pdf", []byte("%PDF-rotate"))
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)
	importShare := func(identityToken, password, grant string) *httptest.ResponseRecorder {
		body := `{}`
		if password != "" {
			body = `{"password":` + strconv.Quote(password) + `}`
		}
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(body))
		req.SetPathValue("token", token)
		if identityToken != "" {
			req.Header.Set("Authorization", "Bearer "+identityToken)
		}
		if grant != "" {
			req.Header.Set(companionReaderGrantHeader, grant)
		}
		rec := httptest.NewRecorder()
		CompanionDocumentShareImportHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	patch := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/shares/"+token, strings.NewReader(`{"visibility":"private","password":`+strconv.Quote(password)+`}`))
		req.SetPathValue("token", token)
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		CompanionDocumentShareUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	if rec := importShare(readerToken, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("account import: %d %s", rec.Code, rec.Body.String())
	}
	guest := importShare("", "", "")
	guestGrant := guest.Header().Get(companionReaderGrantHeader)
	if guest.Code != http.StatusOK || !companionReaderGrantPattern.MatchString(guestGrant) {
		t.Fatalf("guest import: %d grant=%q %s", guest.Code, guestGrant, guest.Body.String())
	}

	if rec := patch("first-secret"); rec.Code != http.StatusOK {
		t.Fatalf("make private: %d %s", rec.Code, rec.Body.String())
	}
	share, _ := companionShareByToken(token)
	if len(share.Importers) != 0 || len(share.Guests) != 0 {
		t.Fatalf("old credentials survived public to private: %#v", share)
	}
	if rec := importShare(readerToken, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old account import bypassed new password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := importShare("", "", guestGrant); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old guest grant bypassed new password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := importShare(readerToken, "first-secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("account reimport: %d %s", rec.Code, rec.Body.String())
	}
	guest = importShare("", "first-secret", guestGrant)
	newGuestGrant := guest.Header().Get(companionReaderGrantHeader)
	if guest.Code != http.StatusOK || !companionReaderGrantPattern.MatchString(newGuestGrant) {
		t.Fatalf("guest reimport: %d grant=%q %s", guest.Code, newGuestGrant, guest.Body.String())
	}

	if rec := patch("replacement-secret"); rec.Code != http.StatusOK {
		t.Fatalf("rotate password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := importShare(readerToken, "first-secret", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("old account password still worked: %d %s", rec.Code, rec.Body.String())
	}
	if rec := importShare("", "first-secret", newGuestGrant); rec.Code != http.StatusForbidden {
		t.Fatalf("old guest password still worked: %d %s", rec.Code, rec.Body.String())
	}
	if rec := importShare(readerToken, "replacement-secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("replacement password: %d %s", rec.Code, rec.Body.String())
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

func TestCompanionRemoteChangeRows(t *testing.T) {
	rows := companionRemoteChangeRows([]map[string]any{{
		"token":          "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"hub_origin":     "https://other.example",
		"revision":       float64(2),
		"notes_revision": float64(3),
		"marks_revision": float64(4),
		"chat_revision":  float64(5),
		"reader_grant":   "secret",
	}, {
		"token":      "not-a-token",
		"hub_origin": "https://other.example",
	}, {
		"token": "cd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}})
	if len(rows) != 1 || rows[0].ChatRevision != 5 || rows[0].NotesRevision != 3 || rows[0].HubOrigin != "https://other.example" {
		t.Fatalf("remote counters: %+v", rows)
	}
}

func TestCompanionDocumentShareChangesEndpointAndListETag(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "share-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "share-reader@example.com")

	publicRec := postCompanionShareWithFields(t, identity, ownerToken, "public", "", "", "0", "公开.pdf", "", []byte("%PDF public"))
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public create: %d %s", publicRec.Code, publicRec.Body.String())
	}
	publicToken := companionShareTokenFrom(t, publicRec)
	personRec := postCompanionShareWithFields(t, identity, ownerToken, "person", "", "share-reader@example.com", "0", "指定.pdf", "", []byte("%PDF person"))
	if personRec.Code != http.StatusOK {
		t.Fatalf("person create: %d %s", personRec.Code, personRec.Body.String())
	}
	personToken := companionShareTokenFrom(t, personRec)

	getChanges := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/changes", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		CompanionDocumentShareChangesHandler(identity).ServeHTTP(rec, req)
		return rec
	}

	ownerChanges := getChanges(ownerToken)
	if ownerChanges.Code != http.StatusOK {
		t.Fatalf("owner changes: %d %s", ownerChanges.Code, ownerChanges.Body.String())
	}
	var ownerPayload struct {
		Seq    uint64 `json:"seq"`
		Shares []struct {
			Token         string `json:"token"`
			Revision      int    `json:"revision"`
			NotesRevision int    `json:"notes_revision"`
			Visibility    string `json:"visibility"`
			Host          bool   `json:"host"`
			Target        string `json:"target"`
			PendingHost   bool   `json:"pending_host"`
		} `json:"shares"`
	}
	if err := json.Unmarshal(ownerChanges.Body.Bytes(), &ownerPayload); err != nil {
		t.Fatal(err)
	}
	if ownerPayload.Seq == 0 || len(ownerPayload.Shares) != 2 {
		t.Fatalf("owner changes payload: %s", ownerChanges.Body.String())
	}
	if !bytes.Contains(ownerChanges.Body.Bytes(), []byte(`"remote_shares":`)) {
		t.Fatal("changes feed omitted the cross-hub counters")
	}
	seen := map[string]struct {
		revision   int
		visibility string
		host       bool
		target     string
	}{}
	for _, row := range ownerPayload.Shares {
		if row.PendingHost {
			t.Fatalf("new share has a pending host: %+v", row)
		}
		seen[row.Token] = struct {
			revision   int
			visibility string
			host       bool
			target     string
		}{row.Revision, row.Visibility, row.Host, row.Target}
	}
	if seen[publicToken].revision != 1 || seen[publicToken].visibility != "public" || !seen[publicToken].host || seen[publicToken].target != "" {
		t.Fatalf("public change row: %+v", seen[publicToken])
	}
	if seen[personToken].revision != 1 || seen[personToken].visibility != "person" || !seen[personToken].host || seen[personToken].target != "share-reader@example.com" {
		t.Fatalf("person change row: %+v", seen[personToken])
	}

	otherChanges := getChanges(otherToken)
	if otherChanges.Code != http.StatusOK {
		t.Fatalf("other changes: %d %s", otherChanges.Code, otherChanges.Body.String())
	}
	var otherPayload struct {
		Seq    uint64 `json:"seq"`
		Shares []struct {
			Token  string `json:"token"`
			Target string `json:"target"`
		} `json:"shares"`
	}
	if err := json.Unmarshal(otherChanges.Body.Bytes(), &otherPayload); err != nil {
		t.Fatal(err)
	}
	// The person share names this reader; the public one does not until they
	// import it, so only one row is visible now. The invite target stays on
	// the admin row, the same way the full list hides it from a reader.
	if len(otherPayload.Shares) != 1 || otherPayload.Shares[0].Token != personToken || otherPayload.Shares[0].Target != "" {
		t.Fatalf("other change rows: %s", otherChanges.Body.String())
	}

	// The full list carries the same sequence as an ETag, and a matching
	// If-None-Match gets a 304 instead of a rebuilt body.
	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	listReq.Header.Set("Authorization", "Bearer "+ownerToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", listRec.Code, listRec.Body.String())
	}
	etag := listRec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("list response missing ETag")
	}
	cachedReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	cachedReq.Header.Set("Authorization", "Bearer "+ownerToken)
	cachedReq.Header.Set("If-None-Match", etag)
	cachedRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(cachedRec, cachedReq)
	if cachedRec.Code != http.StatusNotModified {
		t.Fatalf("conditional list: %d %s", cachedRec.Code, cachedRec.Body.String())
	}

	// A mutation that moves the sequence: the other reader imports the public
	// link and becomes an importer, which is durable state.
	importRec := postCompanionShareImport(t, identity, otherToken, publicToken, `{}`)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", importRec.Code, importRec.Body.String())
	}
	companionSharePersistWG.Wait()
	afterChanges := getChanges(ownerToken)
	var afterPayload struct {
		Seq uint64 `json:"seq"`
	}
	if err := json.Unmarshal(afterChanges.Body.Bytes(), &afterPayload); err != nil {
		t.Fatal(err)
	}
	if afterPayload.Seq != ownerPayload.Seq+1 {
		t.Fatalf("seq did not bump once after import: before=%d after=%d", ownerPayload.Seq, afterPayload.Seq)
	}
	// The stale ETag no longer matches.
	staleReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	staleReq.Header.Set("Authorization", "Bearer "+ownerToken)
	staleReq.Header.Set("If-None-Match", etag)
	staleRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusOK {
		t.Fatalf("stale conditional list: %d", staleRec.Code)
	}
}

func TestCompanionDocumentShareMarksSyncAndFetch(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "share-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "share-reader@example.com")

	created := postCompanionShareWithFields(t, identity, ownerToken, "person", "", "share-reader@example.com", "0", "标注.pdf", "", []byte("%PDF marks"))
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	marksBody := `{"highlights":[{"id":"m1","page":1}],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[]}`
	postMarks := func(bearer, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/marks", strings.NewReader(body))
		req.SetPathValue("token", token)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		CompanionDocumentShareMarksUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}

	// Only the host seat writes marks.
	stranger := postMarks(otherToken, marksBody)
	if stranger.Code != http.StatusForbidden {
		t.Fatalf("reader marks post: %d %s", stranger.Code, stranger.Body.String())
	}
	host := postMarks(ownerToken, marksBody)
	if host.Code != http.StatusOK {
		t.Fatalf("host marks post: %d %s", host.Code, host.Body.String())
	}
	var posted struct {
		MarksRevision int `json:"marks_revision"`
	}
	if err := json.Unmarshal(host.Body.Bytes(), &posted); err != nil || posted.MarksRevision != 1 {
		t.Fatalf("marks post view: %s", host.Body.String())
	}
	companionSharePersistWG.Wait()

	// A second push bumps the marks revision without touching the project
	// revision.
	again := postMarks(ownerToken, marksBody)
	var reposted struct {
		MarksRevision int `json:"marks_revision"`
		Revision      int `json:"revision"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &reposted); err != nil || reposted.MarksRevision != 2 || reposted.Revision != 1 {
		t.Fatalf("marks repost view: %s", again.Body.String())
	}
	companionSharePersistWG.Wait()

	getMarks := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token+"/marks", nil)
		req.SetPathValue("token", token)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		CompanionDocumentShareMarksHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	got := getMarks(otherToken)
	if got.Code != http.StatusOK || got.Body.String() != marksBody {
		t.Fatalf("reader marks get: %d %s", got.Code, got.Body.String())
	}
	if got.Header().Get("X-Companion-Marks-Revision") != "2" {
		t.Fatalf("marks revision header: %q", got.Header().Get("X-Companion-Marks-Revision"))
	}
	// A signed-in user with no access is refused.
	thirdToken, _ := issueViewerToken(t, identity, "share-outsider@example.com")
	if denied := getMarks(thirdToken); denied.Code != http.StatusForbidden {
		t.Fatalf("outsider marks get: %d", denied.Code)
	}

	// The changes endpoint carries the marks revision for the recipient.
	changesReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/changes", nil)
	changesReq.Header.Set("Authorization", "Bearer "+otherToken)
	changesRec := httptest.NewRecorder()
	CompanionDocumentShareChangesHandler(identity).ServeHTTP(changesRec, changesReq)
	if changesRec.Code != http.StatusOK || !strings.Contains(changesRec.Body.String(), `"marks_revision":2`) {
		t.Fatalf("changes rows: %d %s", changesRec.Code, changesRec.Body.String())
	}

	// Deleting the share cleans the marks blob up too.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/shares/"+token, nil)
	delReq.SetPathValue("token", token)
	delReq.Header.Set("Authorization", "Bearer "+ownerToken)
	delRec := httptest.NewRecorder()
	CompanionDocumentShareDeleteHandler(identity).ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", delRec.Code, delRec.Body.String())
	}
	companionSharePersistWG.Wait()
	if gone := getMarks(otherToken); gone.Code != http.StatusNotFound {
		t.Fatalf("marks after delete: %d", gone.Code)
	}
}

func TestCompanionDocumentShareChatSyncAndFetch(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "share-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "share-reader@example.com")

	created := postCompanionShareWithFields(t, identity, ownerToken, "person", "", "share-reader@example.com", "0", "聊天.pdf", "", []byte("%PDF chat"))
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	chatBody := `[{"role":"user","text":"主持人的新问题"},{"role":"assistant","text":"主持人的新回答"}]`
	postChat := func(bearer, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/chat", strings.NewReader(body))
		req.SetPathValue("token", token)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		CompanionDocumentShareChatUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	getChat := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token+"/chat", nil)
		req.SetPathValue("token", token)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		CompanionDocumentShareChatHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	getChanges := func(bearer string) (uint64, string) {
		req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/changes", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		CompanionDocumentShareChangesHandler(identity).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("changes: %d %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Seq, rec.Body.String()
	}

	if missing := getChat(otherToken); missing.Code != http.StatusNotFound {
		t.Fatalf("chat before a push: %d %s", missing.Code, missing.Body.String())
	}
	// Only the host seat writes the transcript.
	if reader := postChat(otherToken, chatBody); reader.Code != http.StatusForbidden {
		t.Fatalf("reader chat post: %d %s", reader.Code, reader.Body.String())
	}
	beforeSeq, _ := getChanges(otherToken)
	host := postChat(ownerToken, chatBody)
	if host.Code != http.StatusOK {
		t.Fatalf("host chat post: %d %s", host.Code, host.Body.String())
	}
	var posted struct {
		ChatRevision int `json:"chat_revision"`
	}
	if err := json.Unmarshal(host.Body.Bytes(), &posted); err != nil || posted.ChatRevision != 1 {
		t.Fatalf("chat post view: %s", host.Body.String())
	}
	companionSharePersistWG.Wait()
	// A transcript-only change still bumps the change sequence, so a polling
	// participant learns about it from the cheap endpoint.
	afterSeq, changesBody := getChanges(otherToken)
	if afterSeq != beforeSeq+1 || !strings.Contains(changesBody, `"chat_revision":1`) {
		t.Fatalf("changes after chat: seq before=%d after=%d body=%s", beforeSeq, afterSeq, changesBody)
	}

	// The reader with access fetches the blob and its revision.
	fetched := getChat(otherToken)
	if fetched.Code != http.StatusOK || fetched.Body.String() != chatBody {
		t.Fatalf("reader chat get: %d %s", fetched.Code, fetched.Body.String())
	}
	if got := fetched.Header().Get("X-Companion-Chat-Revision"); got != "1" {
		t.Fatalf("chat revision header = %q", got)
	}
	if got := fetched.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("chat cache-control = %q", got)
	}
	// Pushing the same transcript again is not a new version: the revision and
	// the change sequence stay put, so participants do not refetch the blob.
	seqBeforeRepeat, _ := getChanges(otherToken)
	repeat := postChat(ownerToken, chatBody)
	var repeated struct {
		ChatRevision int `json:"chat_revision"`
	}
	if err := json.Unmarshal(repeat.Body.Bytes(), &repeated); err != nil || repeated.ChatRevision != 1 {
		t.Fatalf("duplicate chat push: %s", repeat.Body.String())
	}
	seqAfterRepeat, _ := getChanges(otherToken)
	if seqAfterRepeat != seqBeforeRepeat {
		t.Fatalf("duplicate chat push bumped seq: before=%d after=%d", seqBeforeRepeat, seqAfterRepeat)
	}
	// A changed push moves the revision and replaces the blob.
	next := `[{"role":"user","text":"第二个问题"}]`
	again := postChat(ownerToken, next)
	var reposted struct {
		ChatRevision int `json:"chat_revision"`
		Revision     int `json:"revision"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &reposted); err != nil || reposted.ChatRevision != 2 || reposted.Revision != 1 {
		t.Fatalf("chat repost view: %s", again.Body.String())
	}
	fetched = getChat(otherToken)
	if fetched.Body.String() != next || fetched.Header().Get("X-Companion-Chat-Revision") != "2" {
		t.Fatalf("chat after repost: %d %s", fetched.Code, fetched.Body.String())
	}

	// The full list carries the same revision to a participant's library.
	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	listReq.Header.Set("Authorization", "Bearer "+otherToken)
	listRec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"chat_revision":2`) {
		t.Fatalf("list chat revision: %d %s", listRec.Code, listRec.Body.String())
	}

	// A question posted from the revision it read must not replace an answer
	// that landed first. The same body with the current revision still writes.
	staleBody := `[{"role":"user","text":"晚到的旧问题"}]`
	staleReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/chat", strings.NewReader(staleBody))
	staleReq.SetPathValue("token", token)
	staleReq.Header.Set("Authorization", "Bearer "+ownerToken)
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.Header.Set("X-Companion-Chat-Base", "1")
	staleRec := httptest.NewRecorder()
	CompanionDocumentShareChatUpdateHandler(identity).ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale question: %d %s", staleRec.Code, staleRec.Body.String())
	}
	if fetched = getChat(otherToken); fetched.Body.String() != next {
		t.Fatalf("stale question replaced the answer: %s", fetched.Body.String())
	}
	freshBody := `[{"role":"user","text":"第二个问题"},{"role":"assistant","text":"第二个回答"}]`
	freshReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/chat", strings.NewReader(freshBody))
	freshReq.SetPathValue("token", token)
	freshReq.Header.Set("Authorization", "Bearer "+ownerToken)
	freshReq.Header.Set("Content-Type", "application/json")
	freshReq.Header.Set("X-Companion-Chat-Base", "2")
	freshRec := httptest.NewRecorder()
	CompanionDocumentShareChatUpdateHandler(identity).ServeHTTP(freshRec, freshReq)
	if freshRec.Code != http.StatusOK {
		t.Fatalf("current question: %d %s", freshRec.Code, freshRec.Body.String())
	}
	if fetched = getChat(otherToken); fetched.Body.String() != freshBody {
		t.Fatalf("question at the current revision: %s", fetched.Body.String())
	}

	// A signed-in user with no access is refused.
	thirdToken, _ := issueViewerToken(t, identity, "share-outsider@example.com")
	if denied := getChat(thirdToken); denied.Code != http.StatusForbidden {
		t.Fatalf("outsider chat get: %d", denied.Code)
	}

	// Deleting the share cleans the transcript blob up too.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/shares/"+token, nil)
	delReq.SetPathValue("token", token)
	delReq.Header.Set("Authorization", "Bearer "+ownerToken)
	delRec := httptest.NewRecorder()
	CompanionDocumentShareDeleteHandler(identity).ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", delRec.Code, delRec.Body.String())
	}
	companionSharePersistWG.Wait()
	if gone := getChat(otherToken); gone.Code != http.StatusNotFound {
		t.Fatalf("chat after delete: %d", gone.Code)
	}
}

// Transcript images have their own endpoint. Only the host seat writes one,
// the write does not move the change sequence, and anyone who can read the
// share reads the image.
func TestCompanionShareFigureUploadAndFetch(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, owner := issueViewerToken(t, identity, "figure-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "figure-reader@example.com")
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	companionDocShares.Lock()
	companionDocShares.items[token] = companionDocShare{
		Token: token, OwnerID: owner.UserID, OwnerLabel: "figure-owner@example.com",
		Visibility: "public", CreatedAt: time.Now().UTC(), Revision: 1,
	}
	seqBefore := companionDocShares.seq
	companionDocShares.Unlock()

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 64)...)
	postFigure := func(bearer, name string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/figures/"+name, bytes.NewReader(body))
		req.SetPathValue("token", token)
		req.SetPathValue("name", name)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		CompanionDocumentShareFigureUpdateHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	if rec := postFigure(readerToken, "fig-1.png", png); rec.Code != http.StatusForbidden {
		t.Fatalf("reader figure post: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postFigure(ownerToken, "a..b.png", png); rec.Code != http.StatusBadRequest {
		t.Fatalf("dotted name post: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postFigure(ownerToken, "fig-1.png", png); rec.Code != http.StatusOK {
		t.Fatalf("host figure post: %d %s", rec.Code, rec.Body.String())
	}
	companionDocShares.Lock()
	if companionDocShares.seq != seqBefore {
		companionDocShares.Unlock()
		t.Fatal("figure upload moved the change sequence")
	}
	if companionDocShares.items[token].Figures["fig-1.png"] == "" {
		companionDocShares.Unlock()
		t.Fatal("figure blob path not recorded")
	}
	companionDocShares.Unlock()

	getFigure := func(bearer, name string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token+"/figures/"+name, nil)
		req.SetPathValue("token", token)
		req.SetPathValue("name", name)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		CompanionDocumentShareFigureHandler(identity).ServeHTTP(rec, req)
		return rec
	}
	got := getFigure(readerToken, "fig-1.png")
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), png) {
		t.Fatalf("reader figure get: %d len=%d", got.Code, got.Body.Len())
	}
	if mime := got.Header().Get("Content-Type"); mime != "image/png" {
		t.Fatalf("figure mime: %q", mime)
	}
	if rec := getFigure("", "fig-1.png"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous figure get: %d", rec.Code)
	}
	if rec := getFigure(readerToken, "missing.png"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown figure get: %d", rec.Code)
	}

	// Deleting the share cleans the figure blob up too.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/shares/"+token, nil)
	delReq.SetPathValue("token", token)
	delReq.Header.Set("Authorization", "Bearer "+ownerToken)
	delRec := httptest.NewRecorder()
	CompanionDocumentShareDeleteHandler(identity).ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", delRec.Code, delRec.Body.String())
	}
	if rec := getFigure(readerToken, "fig-1.png"); rec.Code != http.StatusNotFound {
		t.Fatalf("figure after delete: %d", rec.Code)
	}
}
