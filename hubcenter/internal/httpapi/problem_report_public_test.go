package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func TestPublicProblemReportDiagnosticsDownload(t *testing.T) {
	const origin = "https://center.example"
	h, store := newProblemReportPublicHandler(t, origin)
	now := time.Now().UTC()
	token, err := skillmarket.NewProblemReportDownloadToken()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("diagnostics-zip-bytes")
	report := &skillmarket.ProblemReport{
		ID: "BR-public", ReporterUserID: "user-1", ReporterContact: "user@example.com",
		OSVersion: "Windows", Description: "fails", Status: "pending",
		DiagnosticsPath: "diagnostics.zip", OriginURL: origin, PublicDownloadToken: token,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	writeProblemReportDiagnostics(t, h.dataDir, report.ID, body)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/problem-reports/d/{token}/diagnostics.zip", h.PublicDiagnosticsDownload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/problem-reports/d/"+strings.ToUpper(token)+"/diagnostics.zip", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(body) {
		t.Fatalf("public download status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "diagnostics.zip") {
		t.Fatalf("disposition = %q", rec.Header().Get("Content-Disposition"))
	}

	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/v1/problem-reports/d/"+strings.Repeat("ab", 32)+"/diagnostics.zip", nil))
	if bad.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d", bad.Code)
	}
	short := httptest.NewRecorder()
	mux.ServeHTTP(short, httptest.NewRequest(http.MethodGet, "/api/v1/problem-reports/d/short/diagnostics.zip", nil))
	if short.Code != http.StatusNotFound {
		t.Fatalf("short token status = %d", short.Code)
	}
}

func TestAdminProblemReportListPublishesDiagnosticsURL(t *testing.T) {
	const origin = "https://center.example"
	h, store := newProblemReportPublicHandler(t, origin)
	now := time.Now().UTC()
	local := &skillmarket.ProblemReport{
		ID: "BR-local", ReporterUserID: "user-1", OSVersion: "Windows", Description: "local",
		Status: "pending", DiagnosticsPath: "diagnostics.zip", OriginURL: origin,
		CreatedAt: now, UpdatedAt: now,
	}
	remote := &skillmarket.ProblemReport{
		ID: "BR-remote", ReporterUserID: "user-1", OSVersion: "Windows", Description: "remote",
		Status: "pending", DiagnosticsPath: "diagnostics.zip", OriginURL: "https://origin.example",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	for _, report := range []*skillmarket.ProblemReport{local, remote} {
		if err := store.CreateProblemReport(context.Background(), report); err != nil {
			t.Fatal(err)
		}
	}
	writeProblemReportDiagnostics(t, h.dataDir, local.ID, []byte("local-zip"))

	payload := listAdminProblemReports(t, h)
	if _, ok := payload[local.ID]["public_download_token"]; ok {
		t.Fatal("admin list returned the raw download token")
	}
	localURL, _ := payload[local.ID]["diagnostics_url"].(string)
	if !strings.Contains(localURL, "/api/v1/problem-reports/d/") || !strings.HasSuffix(localURL, "/diagnostics.zip") || !strings.HasPrefix(localURL, origin) {
		t.Fatalf("local diagnostics url = %q", localURL)
	}
	if remoteURL, _ := payload[remote.ID]["diagnostics_url"].(string); remoteURL != "" {
		t.Fatalf("peer minted a download url %q", remoteURL)
	}
	storedRemote, err := store.GetProblemReport(context.Background(), remote.ID)
	if err != nil || storedRemote.PublicDownloadToken != "" {
		t.Fatalf("peer token = %+v err=%v", storedRemote, err)
	}

	again := listAdminProblemReports(t, h)
	if again[local.ID]["diagnostics_url"] != localURL {
		t.Fatalf("backfill changed the url: %q", again[local.ID]["diagnostics_url"])
	}
	token := strings.TrimPrefix(strings.TrimSuffix(localURL, "/diagnostics.zip"), origin+"/api/v1/problem-reports/d/")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/problem-reports/d/"+token+"/diagnostics.zip", nil)
	req.SetPathValue("token", token)
	h.PublicDiagnosticsDownload(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "local-zip" {
		t.Fatalf("backfilled download status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAdminProblemReportListKeepsReportsWhenOneTokenBackfillFails(t *testing.T) {
	const origin = "https://center.example"
	h, store := newProblemReportPublicHandler(t, origin)
	now := time.Now().UTC()
	for _, id := range []string{"BR-ok", "BR-fail"} {
		if err := store.CreateProblemReport(context.Background(), &skillmarket.ProblemReport{
			ID: id, ReporterUserID: "user-1", OSVersion: "Windows", Description: id,
			Status: "pending", DiagnosticsPath: "diagnostics.zip", OriginURL: origin,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_one_problem_report_token
		BEFORE UPDATE OF public_download_token ON sm_problem_reports
		WHEN NEW.id = 'BR-fail'
		BEGIN
			SELECT RAISE(ABORT, 'token backfill failed');
		END`); err != nil {
		t.Fatal(err)
	}

	payload := listAdminProblemReports(t, h)
	okURL, _ := payload["BR-ok"]["diagnostics_url"].(string)
	if !strings.Contains(okURL, "/api/v1/problem-reports/d/") || !strings.HasSuffix(okURL, "/diagnostics.zip") {
		t.Fatalf("ok diagnostics url = %q", okURL)
	}
	if failURL, _ := payload["BR-fail"]["diagnostics_url"].(string); failURL != "" {
		t.Fatalf("failed row published %q", failURL)
	}
	failed, err := store.GetProblemReport(context.Background(), "BR-fail")
	if err != nil || failed.PublicDownloadToken != "" {
		t.Fatalf("failed token = %+v err=%v", failed, err)
	}
	kept, err := store.GetProblemReport(context.Background(), "BR-ok")
	if err != nil || len(kept.PublicDownloadToken) != 64 {
		t.Fatalf("ok token = %+v err=%v", kept, err)
	}
}

func TestPublicProblemReportDiagnosticsRedirectsToOrigin(t *testing.T) {
	h, store := newProblemReportPublicHandler(t, "https://peer.example")
	now := time.Now().UTC()
	token := strings.Repeat("cd", 32)
	if err := store.CreateProblemReport(context.Background(), &skillmarket.ProblemReport{
		ID: "BR-peer", ReporterUserID: "user-1", OSVersion: "Linux", Description: "peer",
		Status: "pending", OriginURL: "https://origin.example", PublicDownloadToken: token,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/problem-reports/d/"+token+"/diagnostics.zip", nil)
	req.SetPathValue("token", token)
	h.PublicDiagnosticsDownload(rec, req)
	want := "https://origin.example/api/v1/problem-reports/d/" + token + "/diagnostics.zip"
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func newProblemReportPublicHandler(t *testing.T, origin string) (*ProblemReportHandlers, *skillmarket.Store) {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewProblemReportHandlers(store, nil, t.TempDir())
	h.SetOriginURLProvider(func(context.Context) (string, error) { return origin, nil })
	return h, store
}

func writeProblemReportDiagnostics(t *testing.T, dataDir, id string, body []byte) {
	t.Helper()
	root, ok := problemReportStorageRoot(dataDir, id)
	if !ok {
		t.Fatalf("unsafe report id %s", id)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "diagnostics.zip"), body, 0o640); err != nil {
		t.Fatal(err)
	}
}

func listAdminProblemReports(t *testing.T, h *ProblemReportHandlers) map[string]map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.AdminList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/problem-reports", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, item := range payload.Items {
		id, _ := item["id"].(string)
		out[id] = item
	}
	return out
}
