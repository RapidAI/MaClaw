package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
)

func TestToolArchiveUsesRuntimeOwnerForExtractOutput(t *testing.T) {
	projectDir := t.TempDir()
	archivePath := filepath.Join(projectDir, "fixture.zip")
	writeArchiveToolZip(t, archivePath, "dir/item.txt", "ok")
	h := &IMMessageHandler{}
	got := decodeArchiveToolResult(t, h.toolArchive(map[string]interface{}{
		"action":                         "extract",
		"archive_path":                   "fixture.zip",
		"destination":                    "expanded",
		registeredToolPolicyOwnerIDField: desktopUserID + ":" + projectDir,
	}))
	if !got.OK || got.OutputPath != filepath.Join(projectDir, "expanded") {
		t.Fatalf("archive extract = %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(projectDir, "expanded", "dir", "item.txt")); err != nil || string(data) != "ok" {
		t.Fatalf("extracted data = %q, err=%v", data, err)
	}
}

func TestToolArchiveCreateZIPUsesRuntimeOwner(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	got := decodeArchiveToolResult(t, h.toolArchive(map[string]interface{}{
		"action":                         "create_zip",
		"source_paths":                   []interface{}{"source.txt"},
		"output_path":                    "bundle.zip",
		registeredToolPolicyOwnerIDField: desktopUserID + ":" + projectDir,
	}))
	if !got.OK || got.OutputPath != filepath.Join(projectDir, "bundle.zip") {
		t.Fatalf("archive create_zip = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "bundle.zip")); err != nil {
		t.Fatal(err)
	}
}

func TestToolArchiveExternalExtractionRequiresApprovalToken(t *testing.T) {
	projectDir := t.TempDir()
	archivePath := filepath.Join(projectDir, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	got := decodeArchiveToolResult(t, h.toolArchive(map[string]interface{}{
		"action":                         "extract_external",
		"archive_path":                   "fixture.7z",
		"destination":                    "expanded",
		"allow_external":                 true,
		registeredToolPolicyOwnerIDField: desktopUserID + ":" + projectDir,
	}))
	if got.OK || got.Code != archiveutil.CodeExternalApprovalRequired {
		t.Fatalf("unapproved external extraction = %+v", got)
	}
}

func TestRegisterBuiltinToolsIncludesArchive(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})
	tool, ok := r.Get("archive")
	if !ok || tool.Handler == nil {
		t.Fatal("archive tool was not registered")
	}
	for _, key := range []string{"action", "archive_path", "destination", "source_paths", "output_path", "conflict_policy", "root_mode"} {
		if _, ok := tool.InputSchema[key]; !ok {
			t.Fatalf("archive schema missing %q", key)
		}
	}
}

func TestArchiveGroupPolicyAllowsNewOutputUnderAllowedDirectory(t *testing.T) {
	allowed := t.TempDir()
	policy := lansengerGroupPermissionPolicy{AllowedDirectories: []string{allowed}}
	args := map[string]interface{}{
		"action":       "create_zip",
		"source_paths": []interface{}{filepath.Join(allowed, "source.txt")},
		"output_path":  filepath.Join(allowed, "new", "bundle.zip"),
	}
	if err := os.WriteFile(filepath.Join(allowed, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := policy.validateFileToolArgs("archive", args); err != nil {
		t.Fatalf("archive policy rejected a new output below allowed directory: %v", err)
	}
}

func decodeArchiveToolResult(t *testing.T, text string) archiveutil.Result {
	t.Helper()
	var got archiveutil.Result
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("archive response is not JSON: %q (%v)", text, err)
	}
	return got
}

func writeArchiveToolZip(t *testing.T, path, entry, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
