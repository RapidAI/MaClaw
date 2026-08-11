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

	"github.com/RapidAI/CodeClaw/hub/internal/digitalasset"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func newDigitalAssetLibraryForImport(t *testing.T, svc *digitalasset.Service) digitalasset.LibraryView {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": "Import target", "acl_mode": "all_members", "sync_enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create library status=%d body=%s", rec.Code, rec.Body.String())
	}
	var lib digitalasset.LibraryView
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	return lib
}

func multipartDigitalAssetRequest(t *testing.T, method, url string, fields []struct {
	name, path string
	data       []byte
}) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, field := range fields {
		part, err := w.CreateFormFile(field.name, field.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(field.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, url, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func digitalAssetErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Code
}

func TestDigitalAssetUploadHandlerRejectsOversizedFile(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	lib := newDigitalAssetLibraryForImport(t, svc)
	payload := make([]byte, maxDigitalAssetUploadFileBytes+1)
	req := multipartDigitalAssetRequest(t, http.MethodPost, "/api/admin/digital-assets/libraries/"+lib.ID+"/import/upload", []struct {
		name, path string
		data       []byte
	}{{"files", "too-large.txt", payload}})
	req.SetPathValue("id", lib.ID)
	rec := httptest.NewRecorder()
	ImportDigitalAssetUploadAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if code := digitalAssetErrorCode(t, rec); code != "FILE_TOO_LARGE" {
		t.Fatalf("code=%q body=%s", code, rec.Body.String())
	}
}

func TestDigitalAssetBrowserDirHandlerRejectsDuplicatePath(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	lib := newDigitalAssetLibraryForImport(t, svc)
	req := multipartDigitalAssetRequest(t, http.MethodPost, "/api/admin/digital-assets/libraries/"+lib.ID+"/import/browser-dir", []struct {
		name, path string
		data       []byte
	}{
		{"files", "docs/duplicate.md", []byte("first")},
		{"files", "docs/duplicate.md", []byte("second")},
	})
	req.SetPathValue("id", lib.ID)
	rec := httptest.NewRecorder()
	ImportDigitalAssetBrowserDirAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if code := digitalAssetErrorCode(t, rec); code != "DUPLICATE_FILE_PATH" {
		t.Fatalf("code=%q body=%s", code, rec.Body.String())
	}
}

func TestDigitalAssetUploadHandlerCreatesImportJob(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	lib := newDigitalAssetLibraryForImport(t, svc)
	req := multipartDigitalAssetRequest(t, http.MethodPost, "/api/admin/digital-assets/libraries/"+lib.ID+"/import/upload", []struct {
		name, path string
		data       []byte
	}{{"files", "docs/hello.md", []byte("# Hello\n\nenterprise asset")}})
	req.SetPathValue("id", lib.ID)
	rec := httptest.NewRecorder()
	ImportDigitalAssetUploadAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job digitalasset.JobView
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || (job.Status != "queued" && job.Status != "running" && job.Status != "succeeded") {
		t.Fatalf("job=%+v", job)
	}
}

func newDigitalAssetTestService(t *testing.T, enabled bool) *digitalasset.Service {
	t.Helper()
	provider, err := storesqlite.NewProvider(storesqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hub.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	settings := digitalasset.DefaultTenantSettings()
	settings.Enabled = enabled
	host := digitalasset.NewKnowledgeHost(t.TempDir(), 4)
	t.Cleanup(host.CloseAll)
	return &digitalasset.Service{
		Repo:     st.DigitalAssets,
		Host:     host,
		ACL:      &digitalasset.Evaluator{AncestorMatch: true},
		Limiter:  digitalasset.NewSyncLimiter(60, 8),
		Settings: settings,
		Enabled:  enabled,
	}
}

func TestDigitalAssetAdminHandlers_FeatureFlagAndCRUD(t *testing.T) {
	// disabled => 404
	svcOff := newDigitalAssetTestService(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/libraries", nil)
	ListDigitalAssetLibrariesAdminHandler(svcOff)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	// settings GET always works even when feature off
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/settings", nil)
	GetDigitalAssetSettingsAdminHandler(svcOff)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings get when disabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	svc := newDigitalAssetTestService(t, true)
	// create
	body, _ := json.Marshal(map[string]any{
		"name": "Policy", "acl_mode": "all_members", "sync_enabled": true,
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	// inject admin context tenant default via direct service create path is covered by service tests;
	// here ensure handler returns 201 with enabled service (AdminTenantID falls back to default).
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var lib digitalasset.LibraryView
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	if lib.ID == "" || lib.Name != "Policy" {
		t.Fatalf("lib=%+v", lib)
	}

	// list
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/libraries", nil)
	ListDigitalAssetLibrariesAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var list struct {
		Items []digitalasset.LibraryView `json:"items"`
		Total int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list=%+v", list)
	}

	// ensure store repo actually holds library (not fake response)
	got, err := svc.Repo.GetLibrary(req.Context(), store.DefaultTenantID, lib.ID)
	if err != nil || got == nil || got.Name != "Policy" {
		t.Fatalf("repo get=%+v err=%v", got, err)
	}
}

func TestDigitalAssetDeleteSourceHandlers(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	// create library
	body, _ := json.Marshal(map[string]any{"name": "Docs", "acl_mode": "all_members", "sync_enabled": true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var lib digitalasset.LibraryView
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}

	// import via service (handler multipart is heavier)
	docDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(docDir, "x.md"), []byte("# X\n\nhello delete source handler\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := svc.ImportDirectoryIntoLibrary(req.Context(), store.DefaultTenantID, lib.ID, docDir, "admin", "local_dir")
	if err != nil || job.Status != "succeeded" {
		t.Fatalf("import: %+v err=%v", job, err)
	}
	srcPage, err := svc.ListLibrarySources(req.Context(), store.DefaultTenantID, lib.ID, "", 20, 0)
	if err != nil || len(srcPage.Items) < 1 {
		t.Fatalf("sources=%v err=%v", srcPage.Items, err)
	}
	sources := srcPage.Items

	// list sources via handler
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/libraries/"+lib.ID+"/sources?limit=50", nil)
	req.SetPathValue("id", lib.ID)
	ListDigitalAssetLibrarySourcesAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sources status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody struct {
		Items   []digitalasset.SourceView `json:"items"`
		Total   int                       `json:"total"`
		Offset  int                       `json:"offset"`
		Limit   int                       `json:"limit"`
		HasMore bool                      `json:"has_more"`
		Count   int                       `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if listBody.Count < 1 || listBody.Offset != 0 {
		t.Fatalf("list body=%+v", listBody)
	}
	// offset page request (may be empty when only one source)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/libraries/"+lib.ID+"/sources?limit=1&offset=1", nil)
	req.SetPathValue("id", lib.ID)
	ListDigitalAssetLibrarySourcesAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list offset status=%d body=%s", rec.Code, rec.Body.String())
	}

	// delete single
	srcID := sources[0].ID
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/digital-assets/libraries/"+lib.ID+"/sources/"+srcID, nil)
	req.SetPathValue("id", lib.ID)
	req.SetPathValue("source_id", srcID)
	DeleteDigitalAssetLibrarySourceAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delRes digitalasset.DeleteSourcesResult
	if err := json.Unmarshal(rec.Body.Bytes(), &delRes); err != nil {
		t.Fatal(err)
	}
	if delRes.Deleted != 1 {
		t.Fatalf("deleted=%d", delRes.Deleted)
	}

	// not found
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/digital-assets/libraries/"+lib.ID+"/sources/missing", nil)
	req.SetPathValue("id", lib.ID)
	req.SetPathValue("source_id", "missing")
	DeleteDigitalAssetLibrarySourceAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status=%d", rec.Code)
	}
}

func TestDigitalAssetPatchLibraryACL(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	body, _ := json.Marshal(map[string]any{
		"name": "ACL Lib", "acl_mode": "all_members", "sync_enabled": true,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var lib digitalasset.LibraryView
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}

	// restricted + selected departments + sync off
	patch, _ := json.Marshal(map[string]any{
		"set_acl":      true,
		"acl_mode":     "restricted",
		"departments":  []string{"dept_a", " dept_a ", "dept_b"},
		"sync_enabled": false,
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(patch))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated digitalasset.LibraryView
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ACLMode != digitalasset.ACLModeRestricted {
		t.Fatalf("acl_mode=%q", updated.ACLMode)
	}
	if updated.SyncEnabled {
		t.Fatalf("expected sync_enabled=false")
	}
	if len(updated.Departments) != 2 {
		t.Fatalf("departments dedupe got %v", updated.Departments)
	}

	// A restricted ACL without departments would deny every regular member, so
	// reject it at the API boundary instead of persisting a broken policy.
	emptyRestricted, _ := json.Marshal(map[string]any{
		"set_acl": true, "acl_mode": "restricted", "departments": []string{},
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(emptyRestricted))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty restricted status=%d body=%s", rec.Code, rec.Body.String())
	}

	// ACL updates are atomic: partial payloads must not silently reset the mode
	// or drop existing department grants.
	partialMode, _ := json.Marshal(map[string]any{"acl_mode": "all_members"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(partialMode))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("partial acl mode status=%d body=%s", rec.Code, rec.Body.String())
	}

	partialDepartments, _ := json.Marshal(map[string]any{"departments": []string{"dept_c"}})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(partialDepartments))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("partial departments status=%d body=%s", rec.Code, rec.Body.String())
	}

	unknownField := []byte(`{"acl_mode":"restricted","departments":["dept_c"],"user_emails":["not-allowed@example.com"]}`)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(unknownField))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown ACL field status=%d body=%s", rec.Code, rec.Body.String())
	}

	// invalid mode
	bad, _ := json.Marshal(map[string]any{
		"set_acl": true, "acl_mode": "everyone", "departments": []string{"dept_a"},
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(bad))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status=%d body=%s", rec.Code, rec.Body.String())
	}

	// back to all_members clears grants
	clear, _ := json.Marshal(map[string]any{
		"set_acl": true, "acl_mode": "all_members", "departments": []string{},
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/digital-assets/libraries/"+lib.ID, bytes.NewReader(clear))
	req.SetPathValue("id", lib.ID)
	PatchDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear acl status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ACLMode != digitalasset.ACLModeAllMembers {
		t.Fatalf("cleared mode=%q", updated.ACLMode)
	}
	if len(updated.Departments) != 0 {
		t.Fatalf("expected empty departments, got %v", updated.Departments)
	}
}

func TestDecodeDigitalAssetLibraryPatchRejectsNonObjectAndMultipleValues(t *testing.T) {
	for _, body := range []string{
		`[]`,
		`null`,
		`{"acl_mode":"all_members","departments":[]} {}`,
	} {
		if _, err := decodeDigitalAssetLibraryPatch(strings.NewReader(body)); err == nil {
			t.Fatalf("expected invalid patch body to fail: %s", body)
		}
	}
}

func TestDigitalAssetCreateRejectsUnknownAndMultipleJSONValues(t *testing.T) {
	svc := newDigitalAssetTestService(t, true)
	for _, body := range []string{
		`{"name":"Docs","acl_mode":"all_members","departments":[],"user_emails":["not-allowed@example.com"]}`,
		`{"name":"Docs","acl_mode":"all_members","departments":[]} {}`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", strings.NewReader(body))
		CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid create payload status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestDecodeDigitalAssetJSONRejectsOversizedPayload(t *testing.T) {
	var req digitalAssetLibraryCreateRequest
	payload := `{"name":"` + strings.Repeat("a", int(maxDigitalAssetLibraryJSONBytes)) + `"}`
	if err := decodeDigitalAssetJSON(strings.NewReader(payload), &req); err == nil {
		t.Fatal("expected oversized digital asset JSON payload to be rejected")
	}
}

func TestDigitalAssetACLDepartmentsLimit(t *testing.T) {
	tooMany := make([]string, digitalasset.MaxACLDepartments+1)
	for i := range tooMany {
		tooMany[i] = "dept_" + strconv.Itoa(i)
	}
	if err := validateDigitalAssetACLDepartments(tooMany); err == nil {
		t.Fatal("expected oversized departments array to be rejected")
	}

	svc := newDigitalAssetTestService(t, true)
	body, err := json.Marshal(map[string]any{"name": "Docs", "acl_mode": "restricted", "departments": tooMany})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized create departments status=%d body=%s", rec.Code, rec.Body.String())
	}

	duplicates := make([]string, digitalasset.MaxACLDepartments+1)
	for i := range duplicates {
		duplicates[i] = " dept_one "
	}
	if err := validateDigitalAssetACLDepartments(duplicates); err != nil {
		t.Fatalf("duplicate departments should count once, got %v", err)
	}
}

func TestDigitalAssetSettingsToggleEnablesFeature(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hub.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	settings := digitalasset.DefaultTenantSettings()
	settings.Enabled = false
	svc := &digitalasset.Service{
		Repo:     st.DigitalAssets,
		Host:     digitalasset.NewKnowledgeHost(t.TempDir(), 4),
		ACL:      &digitalasset.Evaluator{AncestorMatch: true},
		Limiter:  digitalasset.NewSyncLimiter(60, 8),
		System:   st.System,
		Settings: settings,
		Enabled:  false,
	}

	// off => list 404
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/digital-assets/libraries", nil)
	ListDigitalAssetLibrariesAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when off, got %d", rec.Code)
	}

	// enable via PUT
	body, _ := json.Marshal(map[string]any{"enabled": true, "sync_enabled": true})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/admin/digital-assets/settings", bytes.NewReader(body))
	PutDigitalAssetSettingsAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings status=%d body=%s", rec.Code, rec.Body.String())
	}

	// create works after enable
	body, _ = json.Marshal(map[string]any{"name": "Docs", "acl_mode": "all_members"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/digital-assets/libraries", bytes.NewReader(body))
	CreateDigitalAssetLibraryAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create after enable status=%d body=%s", rec.Code, rec.Body.String())
	}
}
