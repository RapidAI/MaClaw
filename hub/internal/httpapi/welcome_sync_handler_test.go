package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWelcomeSyncDocumentFlow(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	viewerToken, _ := issueViewerToken(t, identity, "welcome-owner@example.com")

	payload := map[string]any{
		"version":    2,
		"kind":       "maclaw-welcome-custom-templates",
		"exportedAt": "2026-07-14T00:00:00Z",
		"templates": []map[string]any{
			{"title": "A", "body": "hello template body"},
			{"title": "B", "body": "other template body"},
		},
		"userRole": "dev",
	}

	uploadRec := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": payload,
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload status = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploaded WelcomeSyncView
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if !uploaded.HasDocument || uploaded.TemplateCount != 2 || uploaded.Revision == "" {
		t.Fatalf("uploaded view = %+v", uploaded)
	}
	if uploaded.Kind != "maclaw-welcome-custom-templates" {
		t.Fatalf("kind = %q", uploaded.Kind)
	}
	rev1 := uploaded.Revision

	statusRec := doKnowledgeShareJSON(t, WelcomeSyncStatusHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status WelcomeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasDocument || status.Revision != rev1 {
		t.Fatalf("status = %+v", status)
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadWelcomeSyncHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync", viewerToken, nil)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(downloadRec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode doc: %v", err)
	}
	if doc["kind"] != "maclaw-welcome-custom-templates" {
		t.Fatalf("downloaded kind = %v", doc["kind"])
	}

	// Optimistic concurrency: wrong if_match_revision → 409
	conflictRec := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload":            payload,
		"if_match_revision":  "not-the-real-revision",
	})
	if conflictRec.Code != http.StatusConflict {
		t.Fatalf("conflict upload = %d body=%s", conflictRec.Code, conflictRec.Body.String())
	}

	// Matching revision succeeds.
	okRec := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"version":    2,
			"kind":       "maclaw-welcome-custom-templates",
			"exportedAt": "2026-07-14T01:00:00Z",
			"templates":  []map[string]any{{"title": "C", "body": "updated body"}},
		},
		"if_match_revision": rev1,
	})
	if okRec.Code != http.StatusOK {
		t.Fatalf("matched upload = %d body=%s", okRec.Code, okRec.Body.String())
	}
	var updated WelcomeSyncView
	if err := json.Unmarshal(okRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.TemplateCount != 1 || updated.Revision == rev1 {
		t.Fatalf("updated view = %+v", updated)
	}

	deleteRec := doKnowledgeShareJSON(t, DeleteWelcomeSyncHandler(identity, welcomeDir), http.MethodDelete, "/api/welcome/sync", viewerToken, nil)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	statusAfter := doKnowledgeShareJSON(t, WelcomeSyncStatusHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync/status", viewerToken, nil)
	var empty WelcomeSyncView
	_ = json.Unmarshal(statusAfter.Body.Bytes(), &empty)
	if empty.HasDocument {
		t.Fatalf("expected no document after delete, got %+v", empty)
	}
}

func TestWelcomeSyncRequiresAuth(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	rec := doKnowledgeShareJSON(t, WelcomeSyncStatusHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync/status", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWelcomeSyncRejectsWrongKind(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	viewerToken, _ := issueViewerToken(t, identity, "welcome-kind@example.com")
	rec := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"kind":      "not-a-welcome-backup",
			"templates": []any{},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong kind upload = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWelcomeSyncRejectsNonArrayTemplates(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	viewerToken, _ := issueViewerToken(t, identity, "welcome-templates-type@example.com")
	rec := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"kind":      "maclaw-welcome-custom-templates",
			"templates": "not-an-array",
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad templates type = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWelcomeSyncOverwriteExistingDocument(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	viewerToken, _ := issueViewerToken(t, identity, "welcome-overwrite@example.com")
	first := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"version":   2,
			"kind":      "maclaw-welcome-custom-templates",
			"templates": []map[string]any{{"title": "A", "body": "first body xx"}},
		},
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first upload = %d %s", first.Code, first.Body.String())
	}
	// Second overwrite without if-match (last-write-wins). Critical on Windows where
	// rename-over-existing historically failed.
	second := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"version":   2,
			"kind":      "maclaw-welcome-custom-templates",
			"templates": []map[string]any{{"title": "B", "body": "second body yy"}, {"title": "C", "body": "third body zz"}},
		},
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second upload = %d %s", second.Code, second.Body.String())
	}
	var view WelcomeSyncView
	if err := json.Unmarshal(second.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.TemplateCount != 2 {
		t.Fatalf("template_count = %d want 2", view.TemplateCount)
	}
	dl := doKnowledgeShareJSON(t, DownloadWelcomeSyncHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync", viewerToken, nil)
	if dl.Code != http.StatusOK {
		t.Fatalf("download = %d %s", dl.Code, dl.Body.String())
	}
	if !strings.Contains(dl.Body.String(), "second body yy") {
		t.Fatalf("download body missing second payload: %s", dl.Body.String())
	}
}

func TestWelcomeSyncOrphanMetaHasNoDocument(t *testing.T) {
	_, identity, _ := newKnowledgeShareHandlerTestDeps(t)
	welcomeDir := t.TempDir()
	viewerToken, _ := issueViewerToken(t, identity, "welcome-orphan@example.com")
	// Seed a full document, then delete only document.json.
	ok := doKnowledgeShareJSON(t, UploadWelcomeSyncHandler(identity, welcomeDir), http.MethodPut, "/api/welcome/sync", viewerToken, map[string]any{
		"payload": map[string]any{
			"version":   2,
			"kind":      "maclaw-welcome-custom-templates",
			"templates": []map[string]any{{"title": "T", "body": "body"}},
		},
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("seed = %d %s", ok.Code, ok.Body.String())
	}
	statusRec := doKnowledgeShareJSON(t, WelcomeSyncStatusHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync/status", viewerToken, nil)
	var status WelcomeSyncView
	_ = json.Unmarshal(statusRec.Body.Bytes(), &status)
	if !status.HasDocument {
		t.Fatalf("expected document after seed, status=%+v", status)
	}
	found := false
	_ = filepath.WalkDir(welcomeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if d.Name() == "document.json" {
			_ = os.Remove(path)
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("document.json not found to orphan")
	}
	statusRec2 := doKnowledgeShareJSON(t, WelcomeSyncStatusHandler(identity, welcomeDir), http.MethodGet, "/api/welcome/sync/status", viewerToken, nil)
	var status2 WelcomeSyncView
	_ = json.Unmarshal(statusRec2.Body.Bytes(), &status2)
	if status2.HasDocument {
		t.Fatalf("orphan meta should not report has_document, got %+v", status2)
	}
}
