package httpapi

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func newLatexTemplateTestHandler(t *testing.T) *LatexTemplateHandlers {
	t.Helper()
	// cache=shared keeps one in-memory database for every connection in this
	// pool. A plain ":memory:" DSN gives each connection an empty database.
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
	return NewLatexTemplateHandlers(NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()}))
}

func latexTemplateZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func latexTemplateUpload(t *testing.T, archive []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := mw.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("zip", "template.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func latexTemplateJSON(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

func TestLatexTemplateSeedsTheFourProductCategories(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	rec := httptest.NewRecorder()
	h.listCategories(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/categories", nil))
	var payload struct {
		Categories []LatexTemplateCategory `json:"categories"`
	}
	latexTemplateJSON(t, rec, &payload)
	want := []string{"conference", "journal", "thesis", "other"}
	if len(payload.Categories) != len(want) {
		t.Fatalf("categories = %+v, want %v", payload.Categories, want)
	}
	// The library promises this exact grouping and order, so it is asserted
	// rather than assumed.
	for i, id := range want {
		if payload.Categories[i].ID != id || !payload.Categories[i].Builtin {
			t.Fatalf("category %d = %+v, want %s (builtin)", i, payload.Categories[i], id)
		}
	}
}

// A restart re-runs the seed. It must reassert that the built-in ids exist and
// stay active WITHOUT clobbering the administrator's customised description or
// sort order.
func TestLatexTemplateSeedReassertKeepsAdminCustomisation(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/latex-template-categories/conference",
		strings.NewReader(`{"description":"academic conferences","sort_order":15}`))
	req.SetPathValue("id", "conference")
	rec := httptest.NewRecorder()
	h.patchCategory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Simulate a fresh process against the same database.
	latexTemplateSchemaByDB.Delete(h.db)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	h.listCategories(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/categories", nil))
	var payload struct {
		Categories []LatexTemplateCategory `json:"categories"`
	}
	latexTemplateJSON(t, rec, &payload)
	for _, item := range payload.Categories {
		if item.ID != "conference" {
			continue
		}
		if item.Description != "academic conferences" || item.SortOrder != 15 {
			t.Fatalf("seed reassert clobbered admin edits: %+v", item)
		}
		if !item.Builtin || item.Status != "active" {
			t.Fatalf("built-in category must stay present and active: %+v", item)
		}
		return
	}
	t.Fatal("conference category missing after reseed")
}

func TestLatexTemplateUserShareWaitsForReview(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{
		"paper/main.tex": "\\documentclass{article}\\begin{document}x\\end{document}",
		"paper/refs.bib": "@article{a, title={A}}",
		"template.json":  `{"name":"IEEE conference","category":"conference","version":"2.0","author":"Ada"}`,
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)

	var shared LatexTemplate
	latexTemplateJSON(t, rec, &shared)
	if shared.Status != latexTemplateStatusPending {
		t.Fatalf("status = %q, want %q", shared.Status, latexTemplateStatusPending)
	}
	if shared.Name != "IEEE conference" || shared.CategoryID != "conference" || shared.Author != "Ada" {
		t.Fatalf("manifest fields not applied: %+v", shared)
	}
	if shared.MainFile != "paper/main.tex" {
		t.Fatalf("main_file = %q, want paper/main.tex", shared.MainFile)
	}
	if shared.FileCount != 1 {
		t.Fatalf("file_count = %d, want 1 .tex source", shared.FileCount)
	}

	// A pending package must be invisible to the public catalogue.
	rec = httptest.NewRecorder()
	h.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	var catalogue struct {
		Templates []LatexTemplate `json:"templates"`
	}
	latexTemplateJSON(t, rec, &catalogue)
	if len(catalogue.Templates) != 0 {
		t.Fatalf("pending template leaked into the catalogue: %+v", catalogue.Templates)
	}

	// ...and must not be downloadable.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/"+shared.ID+"/download", nil)
	req.SetPathValue("id", shared.ID)
	h.downloadTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pending download status = %d, want 404", rec.Code)
	}
}

func TestLatexTemplateAdminUploadIsPublishedImmediately(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{
		"main.tex":  "\\documentclass[11pt]{article}\\begin{document}x\\end{document}",
		"style.cls": "\\NeedsTeXFormat{LaTeX2e}",
	})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{
		"name":        "Campus thesis",
		"category_id": "thesis",
		"description": "graduation thesis",
		"author":      "Registrar",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.adminUploadTemplate(rec, req)

	var uploaded LatexTemplate
	latexTemplateJSON(t, rec, &uploaded)
	if uploaded.Status != latexTemplateStatusApproved || uploaded.Source != "admin" {
		t.Fatalf("uploaded = %+v, want approved admin source", uploaded)
	}
	// The admin form is authoritative even when the pack has no manifest.
	if uploaded.Name != "Campus thesis" || uploaded.CategoryID != "thesis" {
		t.Fatalf("admin form fields not applied: %+v", uploaded)
	}

	rec = httptest.NewRecorder()
	h.listApprovedTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates", nil))
	var catalogue struct {
		Templates  []LatexTemplate         `json:"templates"`
		Categories []LatexTemplateCategory `json:"categories"`
	}
	latexTemplateJSON(t, rec, &catalogue)
	if len(catalogue.Templates) != 1 || catalogue.Templates[0].ID != uploaded.ID {
		t.Fatalf("catalogue = %+v, want the uploaded template", catalogue.Templates)
	}
	if len(catalogue.Categories) != 4 {
		t.Fatalf("catalogue categories = %+v, want the four product categories", catalogue.Categories)
	}

	// An approved package downloads byte-identical to what was uploaded.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/"+uploaded.ID+"/download", nil)
	req.SetPathValue("id", uploaded.ID)
	h.downloadTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), archive) {
		t.Fatal("downloaded package differs from the uploaded bytes")
	}
}

func TestLatexTemplateReviewMovesCategoryAndCanReclassify(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	var shared LatexTemplate
	latexTemplateJSON(t, rec, &shared)

	// Approve into a different category in one step.
	reviewBody, _ := json.Marshal(map[string]string{"action": "approve", "reason": "checked against the official class", "category_id": "journal"})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates/"+shared.ID+"/review", bytes.NewReader(reviewBody))
	req.SetPathValue("id", shared.ID)
	rec = httptest.NewRecorder()
	h.adminReviewTemplate(rec, req)
	var reviewed LatexTemplate
	latexTemplateJSON(t, rec, &reviewed)
	if reviewed.Status != latexTemplateStatusApproved || reviewed.CategoryID != "journal" || reviewed.ReviewNote == "" {
		t.Fatalf("reviewed = %+v, want approved in journal with a note", reviewed)
	}

	// An unknown action is rejected rather than silently ignored.
	req = httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates/"+shared.ID+"/review", strings.NewReader(`{"action":"publish","reason":"x"}`))
	req.SetPathValue("id", shared.ID)
	rec = httptest.NewRecorder()
	h.adminReviewTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action status = %d, want 400", rec.Code)
	}

	// A review without a note is rejected: an unrecorded decision is unauditable.
	req = httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates/"+shared.ID+"/review", strings.NewReader(`{"action":"reject"}`))
	req.SetPathValue("id", shared.ID)
	rec = httptest.NewRecorder()
	h.adminReviewTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status = %d, want 400", rec.Code)
	}
}

func TestLatexTemplateRejectsUnsafeOrEmptyPackages(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	cases := []struct {
		name    string
		archive []byte
	}{
		{"not a zip", []byte("plain text")},
		{"no tex source", latexTemplateZip(t, map[string]string{"readme.txt": "hello"})},
		{"path traversal", latexTemplateZip(t, map[string]string{"../escape.tex": "x"})},
		{"nested traversal", latexTemplateZip(t, map[string]string{"a/../../escape.tex": "x"})},
		{"windows drive path", latexTemplateZip(t, map[string]string{"C:\\evil.tex": "x"})},
		{"absolute path", latexTemplateZip(t, map[string]string{"/etc/passwd.tex": "x"})},
		{"executable", latexTemplateZip(t, map[string]string{"main.tex": "x", "run.sh": "#!/bin/sh"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := latexTemplateUpload(t, tc.archive, map[string]string{"email": "ada@example.com"})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			h.shareTemplate(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// A package whose only entry is written as "./main.tex" — the spelling some
// archivers emit — must still be accepted: the dot-segment does not escape the
// archive root.
func TestLatexTemplateAcceptsDotSlashEntry(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"./main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestLatexTemplateShareQueueIsBoundedPerUploader(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	for i := 0; i < latexTemplateMaxPendingPerOwner; i++ {
		body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		h.shareTemplate(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("share %d status = %d, body = %s", i, rec.Code, rec.Body.String())
		}
	}
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("overflow share status = %d, want 429", rec.Code)
	}
	// Another uploader is unaffected by Ada's full queue.
	body, contentType = latexTemplateUpload(t, archive, map[string]string{"email": "bob@example.com"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec = httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second uploader status = %d, want 201", rec.Code)
	}
}

func TestLatexTemplateCategoryLifecycle(t *testing.T) {
	h := newLatexTemplateTestHandler(t)

	// A purely Chinese label must still be addressable by id.
	payload, _ := json.Marshal(map[string]string{"name": "海报", "description": "poster sessions"})
	rec := httptest.NewRecorder()
	h.createCategory(rec, httptest.NewRequest(http.MethodPost, "/api/admin/latex-template-categories", bytes.NewReader(payload)))
	var created LatexTemplateCategory
	latexTemplateJSON(t, rec, &created)
	if created.ID == "" || created.Builtin {
		t.Fatalf("created category = %+v, want a non-builtin id", created)
	}

	// A built-in category cannot be disabled or deleted.
	rec = httptest.NewRecorder()
	disableReq := httptest.NewRequest(http.MethodPatch, "/api/admin/latex-template-categories/conference", strings.NewReader(`{"status":"disabled"}`))
	disableReq.SetPathValue("id", "conference")
	h.patchCategory(rec, disableReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable built-in status = %d, want 400", rec.Code)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/latex-template-categories/other", nil)
	req.SetPathValue("id", "other")
	rec = httptest.NewRecorder()
	h.deleteCategory(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete built-in status = %d, want 400", rec.Code)
	}

	// A category holding a template cannot be deleted either.
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com", "category_id": created.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec = httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("share status = %d, body = %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/latex-template-categories/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	rec = httptest.NewRecorder()
	h.deleteCategory(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use category status = %d, want 409", rec.Code)
	}
}

func TestLatexTemplateDownloadNameIsHeaderSafe(t *testing.T) {
	// A name full of characters that would otherwise land raw in
	// Content-Disposition must be reduced to a safe token.
	item := LatexTemplate{ID: "tpl_1", Name: `A"B` + "\r\n" + `X/Y`}
	name := latexTemplateDownloadName(item)
	if strings.ContainsAny(name, "\"\\ \r\n\t/") {
		t.Fatalf("download name %q still contains unsafe characters", name)
	}
	if name == "" {
		t.Fatal("download name must not be empty")
	}
}

func TestLatexTemplateSubmissionListFollowsTheReview(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	var shared LatexTemplate
	latexTemplateJSON(t, rec, &shared)

	// The submitter sees their own pending submission and nobody else's.
	rec = httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/submissions?email=ada@example.com", nil)
	h.listMySubmissions(rec, listReq)
	var mine struct {
		Submissions []latexTemplateSubmission `json:"submissions"`
	}
	latexTemplateJSON(t, rec, &mine)
	if len(mine.Submissions) != 1 || mine.Submissions[0].RemoteID != shared.ID || mine.Submissions[0].Status != latexTemplateStatusPending {
		t.Fatalf("submissions = %+v, want the one pending submission", mine.Submissions)
	}

	rec = httptest.NewRecorder()
	h.listMySubmissions(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/submissions?email=bob@example.com", nil))
	latexTemplateJSON(t, rec, &mine)
	if len(mine.Submissions) != 0 {
		t.Fatalf("another user saw %d submissions, want 0", len(mine.Submissions))
	}

	// After a decision the submitter sees the new state and the note, which is
	// what stops the desktop badge from going stale.
	reviewBody, _ := json.Marshal(map[string]string{"action": "approve", "reason": "matches the official class"})
	reviewReq := httptest.NewRequest(http.MethodPost, "/api/admin/latex-templates/"+shared.ID+"/review", bytes.NewReader(reviewBody))
	reviewReq.SetPathValue("id", shared.ID)
	rec = httptest.NewRecorder()
	h.adminReviewTemplate(rec, reviewReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("review status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.listMySubmissions(rec, httptest.NewRequest(http.MethodGet, "/api/v1/latex-templates/submissions?email=ada@example.com", nil))
	latexTemplateJSON(t, rec, &mine)
	if len(mine.Submissions) != 1 {
		t.Fatalf("submissions = %+v, want 1", mine.Submissions)
	}
	if mine.Submissions[0].Status != latexTemplateStatusApproved || mine.Submissions[0].ReviewNote == "" {
		t.Fatalf("submission = %+v, want approved with a note", mine.Submissions[0])
	}
}

func TestLatexTemplateUploadRequiresAnAccount(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous share status = %d, want 401", rec.Code)
	}
}

func TestInspectLatexTemplateZipReadsManifest(t *testing.T) {
	archive := latexTemplateZip(t, map[string]string{
		"template.json": `{"name":"Journal","main_file":"src/paper.tex","version":"9.1"}`,
		"src/paper.tex": "\\documentclass{article}\\begin{document}x\\end{document}",
		"src/extra.tex": "\\section{extra}",
	})
	manifest, texCount, err := inspectLatexTemplateZip(archive, "fallback.zip")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if manifest.Name != "Journal" || manifest.Version != "9.1" || manifest.MainFile != "src/paper.tex" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if texCount != 2 {
		t.Fatalf("texCount = %d, want 2", texCount)
	}

	// A manifest pointing at a file that is not in the package is an error, not
	// a silent fallback.
	bad := latexTemplateZip(t, map[string]string{
		"template.json": `{"name":"Broken","main_file":"missing.tex"}`,
		"paper.tex":     "\\documentclass{article}\\begin{document}x\\end{document}",
	})
	if _, _, err := inspectLatexTemplateZip(bad, "bad.zip"); err == nil {
		t.Fatal("a manifest main_file outside the package must be rejected")
	}
}

func TestInspectLatexTemplateZipAcceptsPublisherSources(t *testing.T) {
	// elsarticle.zip ships documented sources and extensionless doc files
	// alongside the .tex samples. Those are part of the class, not payloads.
	archive := latexTemplateZip(t, map[string]string{
		"elsarticle/elsarticle-template-num.tex": "\\documentclass{elsarticle}\\begin{document}x\\end{document}",
		"elsarticle/elsarticle.dtx":              "% documented source",
		"elsarticle/elsarticle.ins":              "% docstrip installer",
		"elsarticle/elsarticle-num.bst":          "ENTRY {}",
		"elsarticle/README":                      "readme",
		"elsarticle/doc/makefile":                "all:\n",
		"elsarticle/doc/elsdoc.tex":              "\\documentclass{article}",
		"elsarticle/doc/elsdoc.pdf":              "%pdf",
		"elsarticle/doc/pdfwidgets.sty":          "% sty",
	})
	manifest, texCount, err := inspectLatexTemplateZip(archive, "elsarticle.zip")
	if err != nil {
		t.Fatalf("publisher package: %v", err)
	}
	if texCount != 2 || manifest.Name != "elsarticle" {
		t.Fatalf("manifest = %+v, texCount = %d", manifest, texCount)
	}

	blocked := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}",
		"run.exe":  "nope",
	})
	if _, _, err := inspectLatexTemplateZip(blocked, "blocked.zip"); err == nil {
		t.Fatal("an executable inside a template package must be rejected")
	}
	unnamed := latexTemplateZip(t, map[string]string{
		"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}",
		"notes":    "not a README",
	})
	if _, _, err := inspectLatexTemplateZip(unnamed, "unnamed.zip"); err == nil {
		t.Fatal("an extensionless file other than the documentation names must be rejected")
	}
}

func TestInspectLatexTemplateZipPrefersTheSampleOverTheManual(t *testing.T) {
	// The class manual is written first. Zip order must not make doc/elsdoc.tex
	// the paper the catalogue opens.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, body string }{
		{"elsarticle/doc/elsdoc.tex", "\\documentclass{article}\\begin{document}manual\\end{document}"},
		{"elsarticle/README", "readme"},
		{"elsarticle/elsarticle.dtx", "% documented source"},
		{"elsarticle/elsarticle-template-num.tex", "\\documentclass{elsarticle}\\begin{document}paper\\end{document}"},
	}
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := inspectLatexTemplateZip(buf.Bytes(), "elsarticle.zip")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.MainFile != "elsarticle-template-num.tex" {
		t.Fatalf("main_file = %q, want the sample article", manifest.MainFile)
	}
}

func TestInspectLatexTemplateZipReadsAWrappedManifest(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, body string }{
		{"journal/doc/main.tex", "\\documentclass{article}\\begin{document}manual\\end{document}"},
		{"journal/template.json", `{"name":"Journal","main_file":"src/paper.tex","version":"9.1","category":"journal"}`},
		{"journal/src/paper.tex", "\\documentclass{elsarticle}\\begin{document}paper\\end{document}"},
	}
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := inspectLatexTemplateZip(buf.Bytes(), "journal.zip")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "Journal" || manifest.Version != "9.1" || manifest.MainFile != "src/paper.tex" {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestInspectLatexTemplateZipSkipsManualNamedMain(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, body string }{
		{"doc/main.tex", "\\documentclass{article}\\begin{document}manual\\end{document}"},
		{"sample.tex", "\\documentclass{elsarticle}\\begin{document}paper\\end{document}"},
	}
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := inspectLatexTemplateZip(buf.Bytes(), "sample.zip")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.MainFile != "sample.tex" {
		t.Fatalf("main_file = %q, want sample.tex", manifest.MainFile)
	}
}

func TestLatexTemplateAdminListFiltersByStatus(t *testing.T) {
	h := newLatexTemplateTestHandler(t)
	archive := latexTemplateZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	body, contentType := latexTemplateUpload(t, archive, map[string]string{"email": "ada@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/latex-templates", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.shareTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("share status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.adminListTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/admin/latex-templates?status=pending", nil))
	var listed struct {
		Templates []LatexTemplate `json:"templates"`
	}
	latexTemplateJSON(t, rec, &listed)
	if len(listed.Templates) != 1 {
		t.Fatalf("pending filter returned %d templates, want 1", len(listed.Templates))
	}

	rec = httptest.NewRecorder()
	h.adminListTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/admin/latex-templates?status=approved", nil))
	latexTemplateJSON(t, rec, &listed)
	if len(listed.Templates) != 0 {
		t.Fatalf("approved filter returned %d templates, want 0", len(listed.Templates))
	}
}
