package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type latexSnapshotSink struct {
	mu   sync.Mutex
	n    int
	last *latexTemplateSnapshot
	ch   chan struct{}
}

func (s *latexSnapshotSink) AppendLatexTemplateSnapshot(_ context.Context, snap any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if typed, ok := snap.(*latexTemplateSnapshot); ok {
		s.last = typed
	}
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

func TestLatexTemplateSnapshotReplicatesPackageBytes(t *testing.T) {
	source := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}synced\\end{document}",
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{
		"name":        "Synced thesis",
		"category_id": "thesis",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	source.adminUploadTemplate(rec, req)
	var uploaded LatexTemplate
	latexTemplateJSON(t, rec, &uploaded)

	snap, err := source.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatalf("DumpSnapshot: %v", err)
	}
	if len(snap.Templates) != 1 || snap.Templates[0].ID != uploaded.ID || snap.Templates[0].ZipBase64 == "" {
		t.Fatalf("snapshot templates = %+v", snap.Templates)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	peer := newLatexTemplateTestHandler(t)
	if err := peer.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	rec = httptest.NewRecorder()
	peer.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	var catalogue struct {
		Templates []LatexTemplate `json:"templates"`
	}
	latexTemplateJSON(t, rec, &catalogue)
	if len(catalogue.Templates) != 1 || catalogue.Templates[0].Name != "Synced thesis" {
		t.Fatalf("peer catalogue = %+v", catalogue.Templates)
	}
	download := httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/"+uploaded.ID+"/download", nil)
	download.SetPathValue("id", uploaded.ID)
	rec = httptest.NewRecorder()
	peer.downloadTemplate(rec, download)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("peer download status=%d body=%s", rec.Code, rec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/latex-templates/"+uploaded.ID, nil)
	deleteReq.SetPathValue("id", uploaded.ID)
	rec = httptest.NewRecorder()
	source.adminDeleteTemplate(rec, deleteReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	snap, err = source.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatalf("DumpSnapshot after delete: %v", err)
	}
	raw, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatalf("ApplySnapshot after delete: %v", err)
	}
	rec = httptest.NewRecorder()
	peer.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	latexTemplateJSON(t, rec, &catalogue)
	if len(catalogue.Templates) != 0 {
		t.Fatalf("peer kept deleted templates: %+v", catalogue.Templates)
	}
}

func TestApplySnapshotKeepsTheInstalledZipWhenThePayloadIsRejected(t *testing.T) {
	source := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}kept\\end{document}",
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{
		"name":        "Kept",
		"category_id": "thesis",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	source.adminUploadTemplate(rec, req)
	var uploaded LatexTemplate
	latexTemplateJSON(t, rec, &uploaded)
	snap, err := source.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	peer := newLatexTemplateTestHandler(t)
	if err := peer.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(peer.packageDir(), uploaded.ID+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}replaced\\end{document}",
	})
	sum := sha256.Sum256(replacement)
	snap.Templates[0].ZipBase64 = base64.StdEncoding.EncodeToString(replacement)
	snap.Templates[0].SHA256 = hex.EncodeToString(sum[:])
	snap.Templates = append(snap.Templates, latexTemplateSnapshotTemplate{
		ID: "second", Name: "Broken", CategoryID: "thesis", Version: "1", MainFile: "main.tex",
		Status: latexTemplateStatusApproved, Source: "admin", SHA256: "deadbeef",
		ZipBase64: base64.StdEncoding.EncodeToString([]byte("not-the-bytes-we-hashed")),
	})
	raw, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplySnapshot(context.Background(), raw); err == nil {
		t.Fatal("expected the snapshot to be rejected")
	}
	got, err := os.ReadFile(filepath.Join(peer.packageDir(), uploaded.ID+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, installed) {
		t.Fatal("rejected snapshot replaced the installed package")
	}
	list := httptest.NewRecorder()
	peer.listApprovedTemplates(list, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	var catalogue struct {
		Templates []LatexTemplate `json:"templates"`
	}
	latexTemplateJSON(t, list, &catalogue)
	if len(catalogue.Templates) != 1 || catalogue.Templates[0].ID != uploaded.ID {
		t.Fatalf("catalogue = %+v", catalogue.Templates)
	}
}

func TestApplySnapshotKeepsPackagesThePeerHasNotSeen(t *testing.T) {
	left := uploadLatexTemplate(t, newLatexTemplateTestHandler(t), "Left paper", "thesis")
	rightHandler := newLatexTemplateTestHandler(t)
	right := uploadLatexTemplate(t, rightHandler, "Right paper", "journal")
	snap, err := left.handler.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := rightHandler.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rightHandler.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	var catalogue struct {
		Templates []LatexTemplate `json:"templates"`
	}
	latexTemplateJSON(t, rec, &catalogue)
	got := map[string]string{}
	for _, item := range catalogue.Templates {
		got[item.ID] = item.Name
	}
	if got[left.item.ID] != "Left paper" || got[right.item.ID] != "Right paper" {
		t.Fatalf("catalogue = %+v", catalogue.Templates)
	}

	older, err := rightHandler.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	olderRaw, err := json.Marshal(older)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rightHandler.db.Exec(`UPDATE latex_templates SET name=?, updated_at=? WHERE id=?`, "Renamed locally", "2099-01-01T00:00:00Z", right.item.ID); err != nil {
		t.Fatal(err)
	}
	if err := rightHandler.ApplySnapshot(context.Background(), olderRaw); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	rightHandler.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	latexTemplateJSON(t, rec, &catalogue)
	for _, item := range catalogue.Templates {
		if item.ID == right.item.ID && item.Name != "Renamed locally" {
			t.Fatalf("older snapshot renamed %s to %s", right.item.ID, item.Name)
		}
	}
}

type uploadedLatexTemplate struct {
	handler *LatexTemplateHandlers
	item    LatexTemplate
}

func uploadLatexTemplate(t *testing.T, handler *LatexTemplateHandlers, name, category string) uploadedLatexTemplate {
	t.Helper()
	archive := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}" + name + "\\end{document}",
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{
		"name":        name,
		"category_id": category,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.adminUploadTemplate(rec, req)
	var item LatexTemplate
	latexTemplateJSON(t, rec, &item)
	return uploadedLatexTemplate{handler: handler, item: item}
}

func TestBuiltinCategoryStartupDoesNotLookLikeANewerEdit(t *testing.T) {
	handler := newLatexTemplateTestHandler(t)
	if err := handler.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	const renamed = "会议论文"
	const updated = "2020-01-01T00:00:00.000000000Z"
	if _, err := handler.db.Exec(`UPDATE latex_template_categories SET name=?, updated_at=? WHERE id=?`, renamed, updated, "conference"); err != nil {
		t.Fatal(err)
	}
	latexTemplateSchemaByDB.Delete(handler.db)
	if err := handler.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	var name, gotUpdated string
	if err := handler.db.QueryRow(`SELECT name, updated_at FROM latex_template_categories WHERE id=?`, "conference").Scan(&name, &gotUpdated); err != nil {
		t.Fatal(err)
	}
	if name != renamed || gotUpdated != updated {
		t.Fatalf("category = %s @ %s, want the administrator's edit", name, gotUpdated)
	}
}

func TestApplySnapshotIgnoresAnUntimedTemplate(t *testing.T) {
	handler := newLatexTemplateTestHandler(t)
	uploaded := uploadLatexTemplate(t, handler, "Kept name", "thesis")
	snap, err := handler.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snap.Templates[0].UpdatedAt = ""
	snap.Templates[0].Name = "Untimed replacement"
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := handler.db.QueryRow(`SELECT name FROM latex_templates WHERE id=?`, uploaded.item.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Kept name" {
		t.Fatalf("name = %q, want the stored template", name)
	}
}

func TestLatexTemplateTimeOrdersAFractionAfterTheWholeSecond(t *testing.T) {
	if !latexTemplateTimeAfter("2026-09-29T10:00:00.5Z", "2026-09-29T10:00:00Z") {
		t.Fatal("a fraction inside the second is later than the whole second")
	}
	if latexTemplateTimeAfter("2026-09-29T10:00:00Z", "2026-09-29T10:00:00.5Z") {
		t.Fatal("the whole second is earlier than a fraction inside it")
	}
}

func TestApplySnapshotKeepsAFractionalUpdate(t *testing.T) {
	handler := newLatexTemplateTestHandler(t)
	uploaded := uploadLatexTemplate(t, handler, "Whole second", "thesis")
	snap, err := handler.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snap.Templates[0].UpdatedAt = "2026-09-29T10:00:00Z"
	snap.Templates[0].Name = "Stale name"
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	peer := newLatexTemplateTestHandler(t)
	if err := peer.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.db.Exec(`UPDATE latex_templates SET name=?, updated_at=? WHERE id=?`, "Edited later", "2026-09-29T10:00:00.5Z", uploaded.item.ID); err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := peer.db.QueryRow(`SELECT name FROM latex_templates WHERE id=?`, uploaded.item.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Edited later" {
		t.Fatalf("name = %q, want the later edit", name)
	}
}

func TestLatexTemplateUploadEmitsSnapshot(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	sink := &latexSnapshotSink{ch: make(chan struct{}, 1)}
	h.SetSyncRecorder(sink)
	archive := latexTemplateZip(t, map[string]string{
		"paper.tex": "\\documentclass{article}\\begin{document}emit\\end{document}",
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{
		"name":        "Emitted",
		"category_id": "journal",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.adminUploadTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-sink.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not emit a latex template snapshot")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.n == 0 || sink.last == nil || len(sink.last.Templates) != 1 || sink.last.Templates[0].ZipBase64 == "" {
		t.Fatalf("emitted snapshot n=%d last=%+v", sink.n, sink.last)
	}
}

func TestApplySnapshotIgnoresAnUntimedDelete(t *testing.T) {
	handler := newLatexTemplateTestHandler(t)
	uploaded := uploadLatexTemplate(t, handler, "Kept name", "thesis")
	snap, err := handler.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snap.TemplateDeletes = []latexTemplateSnapshotTombstone{{ID: uploaded.item.ID, DeletedAt: "not-a-time"}}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.ApplySnapshot(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := handler.db.QueryRow(`SELECT name FROM latex_templates WHERE id=?`, uploaded.item.ID).Scan(&name); err != nil {
		t.Fatalf("template removed by an untimed delete: %v", err)
	}
	if name != "Kept name" {
		t.Fatalf("name = %q", name)
	}
}

func TestLatexTemplateSeedPublishesARenamedCategory(t *testing.T) {
	handler := newLatexTemplateTestHandler(t)
	if err := handler.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.db.Exec(`UPDATE latex_template_categories SET name=?, updated_at=? WHERE id=?`, "会议论文", "2026-09-29T10:00:00.000000000Z", "conference"); err != nil {
		t.Fatal(err)
	}
	sink := &latexSnapshotSink{ch: make(chan struct{}, 1)}
	handler.SetSyncRecorder(sink)
	handler.seedHASnapshot(context.Background(), &latexSeedProbe{has: false, sink: sink})
	select {
	case <-sink.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the renamed category snapshot")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.last == nil {
		t.Fatal("missing snapshot")
	}
	for _, category := range sink.last.Categories {
		if category.ID == "conference" && category.Name == "会议论文" {
			return
		}
	}
	t.Fatalf("snapshot categories = %+v", sink.last.Categories)
}

func TestLatexTemplateSeedSkipsEmptyCatalogue(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	sink := &latexSnapshotSink{ch: make(chan struct{}, 1)}
	h.seedHASnapshot(context.Background(), &latexSeedProbe{has: false, sink: sink})
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.n != 0 {
		t.Fatalf("empty catalogue seeded %d snapshots", sink.n)
	}
}

type latexSeedProbe struct {
	has  bool
	sink *latexSnapshotSink
}

func (p *latexSeedProbe) HasEntityTypeOps(context.Context, ...string) (bool, error) {
	return p.has, nil
}

func (p *latexSeedProbe) AppendLatexTemplateSnapshot(ctx context.Context, snap any) {
	p.sink.AppendLatexTemplateSnapshot(ctx, snap)
}
