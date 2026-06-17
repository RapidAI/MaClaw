package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSkillArtifactRegistryRegistersAndLooksUpArtifacts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	artifactPath := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(artifactPath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := &SkillRunStatus{
		RunID:   "run-registry-1",
		OwnerID: "owner-1",
		Skill:   "demo-skill",
		Artifacts: []SkillRunArtifact{{
			ID:     "artifact-1",
			URI:    "artifact://skill-run/run-registry-1/artifact-1",
			Name:   "report.pdf",
			Path:   artifactPath,
			Status: skillArtifactStatusVerified,
		}},
	}

	app.registerSkillRunArtifacts(status)

	got, err := app.lookupSkillArtifactPath("run-registry-1", "artifact-1")
	if err != nil || got != artifactPath {
		t.Fatalf("lookup by id = %q, %v", got, err)
	}
	got, err = app.lookupSkillArtifactPath("run-registry-1", "artifact://skill-run/run-registry-1/artifact-1")
	if err != nil || got != artifactPath {
		t.Fatalf("lookup by uri = %q, %v", got, err)
	}
	got, err = app.lookupSkillArtifactPathForOwner("owner-1", "run-registry-1", "artifact-1")
	if err != nil || got != artifactPath {
		t.Fatalf("lookup by owner = %q, %v", got, err)
	}
	if _, err := app.lookupSkillArtifactPathForOwner("owner-2", "run-registry-1", "artifact-1"); err == nil {
		t.Fatal("expected owner mismatch")
	}
	entry, err := app.GetSkillRunArtifactForOwner("owner-1", "run-registry-1", "artifact-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.URI != "artifact://skill-run/run-registry-1/artifact-1" || entry.RunID != "run-registry-1" || entry.ArtifactID != "artifact-1" {
		t.Fatalf("entry identity = %#v", entry)
	}
	if entry.Name != "report.pdf" || entry.Skill != "demo-skill" || !entry.Available {
		t.Fatalf("entry metadata = %#v", entry)
	}
	entries, err := app.ListSkillRunArtifactsForOwner("owner-1", "run-registry-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].URI != entry.URI {
		t.Fatalf("entries = %#v", entries)
	}
	if _, err := app.GetSkillRunArtifactForOwner("owner-2", "run-registry-1", "artifact-1"); err == nil {
		t.Fatal("expected owner mismatch from public lookup")
	}
}

func TestSkillArtifactRegistrySkipsIncompleteArtifacts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:     "run-empty",
		Artifacts: []SkillRunArtifact{{ID: "artifact-1"}},
	})
	if _, err := app.lookupSkillArtifactPath("run-empty", "artifact-1"); err == nil {
		t.Fatal("expected missing artifact lookup to fail")
	}
}

func TestSkillArtifactRegistryRejectsMissingPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	missingPath := filepath.Join(t.TempDir(), "missing.pdf")
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID: "run-missing",
		Artifacts: []SkillRunArtifact{{
			ID:   "artifact-1",
			URI:  "artifact://skill-run/run-missing/artifact-1",
			Path: missingPath,
		}},
	})
	if _, err := app.lookupSkillArtifactPath("run-missing", "artifact-1"); err == nil {
		t.Fatal("expected missing path lookup to fail")
	}
}

func TestSkillArtifactRegistryCleanupRemovesMissingAndExpired(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	validPath := filepath.Join(t.TempDir(), "valid.pdf")
	if err := os.WriteFile(validPath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID: "run-clean",
		Artifacts: []SkillRunArtifact{
			{ID: "valid", URI: "artifact://skill-run/run-clean/valid", Path: validPath},
			{ID: "missing", URI: "artifact://skill-run/run-clean/missing", Path: filepath.Join(t.TempDir(), "missing.pdf")},
		},
	})
	db, err := openSkillArtifactRegistryDB(app.skillArtifactRegistryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := db.ExecContext(context.Background(), `UPDATE skill_run_artifacts SET updated_at = ? WHERE artifact_id = ?`, old, "valid"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	result, err := app.CleanupSkillArtifactRegistry(7, true)
	if err != nil {
		t.Fatal(err)
	}
	if result["expired"] != 1 || result["missing"] != 1 {
		t.Fatalf("cleanup result = %#v", result)
	}
}

func TestSkillArtifactRegistryListFiltersOwner(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pathA := filepath.Join(t.TempDir(), "a.pdf")
	pathB := filepath.Join(t.TempDir(), "b.pdf")
	if err := os.WriteFile(pathA, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-owner-a",
		OwnerID: "owner-a",
		Artifacts: []SkillRunArtifact{{
			ID:   "artifact-a",
			URI:  "artifact://skill-run/run-owner-a/artifact-a",
			Path: pathA,
		}},
	})
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-owner-b",
		OwnerID: "owner-b",
		Artifacts: []SkillRunArtifact{{
			ID:   "artifact-b",
			URI:  "artifact://skill-run/run-owner-b/artifact-b",
			Path: pathB,
		}},
	})

	entries, err := app.ListSkillRunArtifactsForOwner("owner-a", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].OwnerID != "owner-a" || entries[0].ArtifactID != "artifact-a" {
		t.Fatalf("owner filtered entries = %#v", entries)
	}
}

func TestSkillArtifactRegistryKeepsRemoteOnlyArtifacts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-remote",
		OwnerID: "owner-remote",
		Skill:   "remote-skill",
		Artifacts: []SkillRunArtifact{{
			ID:            "remote-pdf",
			URI:           "artifact://skill-run/run-remote/remote-pdf",
			Name:          "remote.pdf",
			RemoteURL:     "https://hub.example/artifacts/remote.pdf",
			Checksum:      "sha256:abc",
			DownloadState: "remote",
		}},
	})
	entry, err := app.GetSkillRunArtifactForOwner("owner-remote", "run-remote", "remote-pdf")
	if err != nil {
		t.Fatal(err)
	}
	if entry.RemoteURL != "https://hub.example/artifacts/remote.pdf" || entry.Checksum != "sha256:abc" || entry.DownloadState != "remote" {
		t.Fatalf("remote metadata = %#v", entry)
	}
	if entry.Available {
		t.Fatalf("remote-only entry should not be locally available: %#v", entry)
	}
	result, err := app.CleanupSkillArtifactRegistry(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if result["missing"] != 0 {
		t.Fatalf("remote-only cleanup result = %#v", result)
	}
	entries, err := app.ListSkillRunArtifactsForOwner("owner-remote", "run-remote", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ArtifactID != "remote-pdf" {
		t.Fatalf("remote-only entries = %#v", entries)
	}
}

func TestSkillArtifactRegistryUpdatesRemoteCache(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-cache",
		OwnerID: "owner-cache",
		Artifacts: []SkillRunArtifact{{
			ID:            "remote-doc",
			URI:           "artifact://skill-run/run-cache/remote-doc",
			Name:          "remote.docx",
			RemoteURL:     "https://hub.example/artifacts/remote.docx",
			Checksum:      "sha256:old",
			DownloadState: "remote",
		}},
	})
	localPath := filepath.Join(t.TempDir(), "remote.docx")
	if err := os.WriteFile(localPath, []byte("docx"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := app.UpdateSkillRunArtifactCacheForOwner("owner-cache", "run-cache", "remote-doc", localPath, "sha256:new")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Available || entry.DownloadState != "downloaded" || entry.Checksum != "sha256:new" {
		t.Fatalf("cache entry = %#v", entry)
	}
	got, err := app.lookupSkillArtifactPathForOwner("owner-cache", "run-cache", "remote-doc")
	if err != nil || got != localPath {
		t.Fatalf("cached lookup = %q, %v", got, err)
	}
	if _, err := app.UpdateSkillRunArtifactCacheForOwner("other-owner", "run-cache", "remote-doc", localPath, ""); err == nil {
		t.Fatal("expected owner mismatch")
	}
}

func TestSkillArtifactDownloadRemoteOnlyArtifact(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	body := []byte("downloaded pdf")
	checksum := "sha256:" + testSkillArtifactSHA256Hex(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artifacts/report.pdf" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-download",
		OwnerID: "owner-download",
		Artifacts: []SkillRunArtifact{{
			ID:            "report",
			URI:           "artifact://skill-run/run-download/report",
			Name:          "report.pdf",
			RemoteURL:     server.URL + "/artifacts/report.pdf",
			Checksum:      checksum,
			DownloadState: "remote",
		}},
	})

	entry, err := app.DownloadSkillRunArtifactForOwner("owner-download", "run-download", "report")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Available || entry.DownloadState != "downloaded" || entry.Checksum != checksum {
		t.Fatalf("downloaded entry = %#v", entry)
	}
	path, err := app.lookupSkillArtifactPathForOwner("owner-download", "run-download", "report")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded body = %q", string(got))
	}
}

func TestSkillArtifactDownloadSeparatesSameNamedArtifacts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	bodies := map[string][]byte{
		"/artifacts/a/report.pdf": []byte("report a"),
		"/artifacts/b/report.pdf": []byte("report b"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-same-name",
		OwnerID: "owner-same-name",
		Artifacts: []SkillRunArtifact{
			{
				ID:            "report-a",
				URI:           "artifact://skill-run/run-same-name/report-a",
				Name:          "report.pdf",
				RemoteURL:     server.URL + "/artifacts/a/report.pdf",
				Checksum:      "sha256:" + testSkillArtifactSHA256Hex(bodies["/artifacts/a/report.pdf"]),
				DownloadState: "remote",
			},
			{
				ID:            "report-b",
				URI:           "artifact://skill-run/run-same-name/report-b",
				Name:          "report.pdf",
				RemoteURL:     server.URL + "/artifacts/b/report.pdf",
				Checksum:      "sha256:" + testSkillArtifactSHA256Hex(bodies["/artifacts/b/report.pdf"]),
				DownloadState: "remote",
			},
		},
	})

	first, err := app.DownloadSkillRunArtifactForOwner("owner-same-name", "run-same-name", "report-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.DownloadSkillRunArtifactForOwner("owner-same-name", "run-same-name", "report-b")
	if err != nil {
		t.Fatal(err)
	}
	firstPath, err := app.lookupSkillArtifactPathForOwner("owner-same-name", "run-same-name", "report-a")
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := app.lookupSkillArtifactPathForOwner("owner-same-name", "run-same-name", "report-b")
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("same named artifacts share cache path: %s", firstPath)
	}
	if first.Checksum == second.Checksum {
		t.Fatalf("checksums should differ: %#v %#v", first, second)
	}
	firstBody, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBody) != "report a" || string(secondBody) != "report b" {
		t.Fatalf("cached bodies = %q, %q", string(firstBody), string(secondBody))
	}
}

func TestSkillArtifactDownloadRejectsChecksumMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("wrong body"))
	}))
	defer server.Close()
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-bad-checksum",
		OwnerID: "owner-bad-checksum",
		Artifacts: []SkillRunArtifact{{
			ID:            "doc",
			URI:           "artifact://skill-run/run-bad-checksum/doc",
			Name:          "doc.txt",
			RemoteURL:     server.URL + "/doc.txt",
			Checksum:      "sha256:" + fmt.Sprintf("%064x", 1),
			DownloadState: "remote",
		}},
	})

	if _, err := app.DownloadSkillRunArtifactForOwner("owner-bad-checksum", "run-bad-checksum", "doc"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	entry, err := app.GetSkillRunArtifactForOwner("owner-bad-checksum", "run-bad-checksum", "doc")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Available || entry.DownloadState != "remote" {
		t.Fatalf("entry after mismatch = %#v", entry)
	}
}

func TestSkillArtifactDownloadRejectsInvalidRemoteURLAndOwnerMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-invalid-remote",
		OwnerID: "owner-invalid-remote",
		Artifacts: []SkillRunArtifact{{
			ID:            "doc",
			URI:           "artifact://skill-run/run-invalid-remote/doc",
			Name:          "doc.txt",
			RemoteURL:     "file:///tmp/doc.txt",
			DownloadState: "remote",
		}},
	})
	if _, err := app.DownloadSkillRunArtifactForOwner("other-owner", "run-invalid-remote", "doc"); err == nil {
		t.Fatal("expected owner mismatch")
	}
	if _, err := app.DownloadSkillRunArtifactForOwner("owner-invalid-remote", "run-invalid-remote", "doc"); err == nil {
		t.Fatal("expected invalid scheme")
	}

	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-missing-host",
		OwnerID: "owner-invalid-remote",
		Artifacts: []SkillRunArtifact{{
			ID:            "doc",
			URI:           "artifact://skill-run/run-missing-host/doc",
			Name:          "doc.txt",
			RemoteURL:     "https://",
			DownloadState: "remote",
		}},
	})
	if _, err := app.DownloadSkillRunArtifactForOwner("owner-invalid-remote", "run-missing-host", "doc"); err == nil {
		t.Fatal("expected missing host")
	}

	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-userinfo",
		OwnerID: "owner-invalid-remote",
		Artifacts: []SkillRunArtifact{{
			ID:            "doc",
			URI:           "artifact://skill-run/run-userinfo/doc",
			Name:          "doc.txt",
			RemoteURL:     "https://token@example.test/doc.txt",
			DownloadState: "remote",
		}},
	})
	if _, err := app.DownloadSkillRunArtifactForOwner("owner-invalid-remote", "run-userinfo", "doc"); err == nil {
		t.Fatal("expected userinfo rejection")
	}
}

func TestSkillArtifactDownloadRejectsInvalidRedirect(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///tmp/doc.txt", http.StatusFound)
	}))
	defer server.Close()
	app.registerSkillRunArtifacts(&SkillRunStatus{
		RunID:   "run-redirect",
		OwnerID: "owner-redirect",
		Artifacts: []SkillRunArtifact{{
			ID:            "doc",
			URI:           "artifact://skill-run/run-redirect/doc",
			Name:          "doc.txt",
			RemoteURL:     server.URL + "/doc.txt",
			DownloadState: "remote",
		}},
	})

	if _, err := app.DownloadSkillRunArtifactForOwner("owner-redirect", "run-redirect", "doc"); err == nil {
		t.Fatal("expected invalid redirect")
	}
}

func TestSkillArtifactDownloadRejectsPrivateRemoteHosts(t *testing.T) {
	blocked := []string{
		"http://localhost/doc.txt",
		"http://app.local/doc.txt",
		"http://127.0.0.1/doc.txt",
		"http://10.0.0.1/doc.txt",
		"http://172.16.0.1/doc.txt",
		"http://192.168.1.1/doc.txt",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]/doc.txt",
	}
	for _, rawURL := range blocked {
		if _, err := parseAllowedSkillArtifactRemoteURL(rawURL, false); err == nil {
			t.Fatalf("expected private host rejection for %s", rawURL)
		}
		if _, err := parseAllowedSkillArtifactRemoteURL(rawURL, true); err != nil {
			t.Fatalf("expected test mode to allow %s: %v", rawURL, err)
		}
	}
}

func testSkillArtifactSHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
