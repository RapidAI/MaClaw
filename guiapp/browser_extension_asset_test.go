package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompanionExtensionAssetMatchesSource keeps the embedded copy in step
// with extensions/companion-paper. The binary ships the copy, so a stale one
// would install the previous extension under the current version number.
func TestCompanionExtensionAssetMatchesSource(t *testing.T) {
	embedded, err := companionEmbeddedExtensionFS()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "extensions", "companion-paper")
	seen := map[string]bool{}
	err = fs.WalkDir(embedded, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		seen[path] = true
		want, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("embedded %s is missing from %s: %v", path, source, err)
		}
		got, err := fs.ReadFile(embedded, path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("embedded %s drifted; run scripts/sync-companion-extension-asset.ps1", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// _locales must survive the embed: a plain directory pattern drops names
	// that begin with "_", and the manifest title is a message lookup.
	if !seen["_locales/zh_CN/messages.json"] {
		t.Fatalf("embedded copy has no _locales; files=%v", seen)
	}
	for _, name := range []string{"manifest.json", "background.js"} {
		if !seen[name] {
			t.Fatalf("embedded copy has no %s", name)
		}
	}
	// The source keeps the test-only menu file; the install copy must not.
	for path := range seen {
		if strings.HasSuffix(path, ".mjs") || strings.Contains(path, "_test") {
			t.Fatalf("embedded copy carries a test file: %s", path)
		}
	}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, ".mjs") || strings.Contains(name, "_test") {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			t.Fatalf("source %s is missing from the embedded copy; run scripts/sync-companion-extension-asset.ps1", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBuildCompanionLocalPackageFallsBackToEmbedded covers the installed app:
// no extensions/ next to the exe, no repo tree, no source hook.
func TestBuildCompanionLocalPackageFallsBackToEmbedded(t *testing.T) {
	prevExt := companionExtensionDirHook
	companionExtensionDirHook = func() (string, error) { return "", nil }
	t.Cleanup(func() { companionExtensionDirHook = prevExt })

	dir := t.TempDir()
	local, err := buildCompanionLocalPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !local.Ready || strings.TrimSpace(local.Dir) == "" {
		t.Fatalf("local=%#v", local)
	}
	for _, rel := range []string{
		"manifest.json",
		"background.js",
		filepath.Join("_locales", "zh_CN", "messages.json"),
		filepath.Join("icons", "icon128.png"),
	} {
		path := filepath.Join(dir, companionUnpackedDirName, rel)
		if !companionPathIsFile(path) {
			t.Fatalf("unpacked copy is missing %s", rel)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(dir, companionUnpackedDirName, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte(`"key"`)) {
		t.Fatal("unpacked manifest has no key; the browser would assign a random id")
	}
	if !bytes.Contains(manifest, []byte(browserExtensionVersion)) {
		t.Fatalf("unpacked manifest does not carry %s", browserExtensionVersion)
	}
}

func TestCompanionExtensionStorePackageMatchesSource(t *testing.T) {
	source := filepath.Join("..", "extensions", "companion-paper")
	manifestBody, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var versioned struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifestBody, &versioned); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(filepath.Join("..", "extensions", "companion-paper-store-"+versioned.Version+".zip"))
	if err != nil {
		t.Fatalf("open store package; run scripts/package-companion-extension.ps1: %v", err)
	}
	defer archive.Close()
	packaged := map[string][]byte{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		packaged[file.Name] = body
	}
	expected := map[string][]byte{}
	err = fs.WalkDir(os.DirFS(source), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".mjs") || strings.Contains(name, "_test") {
			return nil
		}
		body, err := fs.ReadFile(os.DirFS(source), path)
		if err != nil {
			return err
		}
		expected[path] = body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packaged) != len(expected) {
		t.Fatalf("store package has %d files, source has %d; run scripts/package-companion-extension.ps1", len(packaged), len(expected))
	}
	for path, want := range expected {
		got, ok := packaged[path]
		if !ok {
			t.Fatalf("store package is missing %s", path)
		}
		if path == "manifest.json" {
			var storeDoc, sourceDoc map[string]any
			if err := json.Unmarshal(got, &storeDoc); err != nil {
				t.Fatal(err)
			}
			if _, exists := storeDoc["key"]; exists {
				t.Fatal("store manifest contains the local development key")
			}
			if err := json.Unmarshal(want, &sourceDoc); err != nil {
				t.Fatal(err)
			}
			delete(sourceDoc, "key")
			gotCanonical, _ := json.Marshal(storeDoc)
			wantCanonical, _ := json.Marshal(sourceDoc)
			if !bytes.Equal(gotCanonical, wantCanonical) {
				t.Fatal("store manifest drifted; run scripts/package-companion-extension.ps1")
			}
			continue
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("store package %s drifted; run scripts/package-companion-extension.ps1", path)
		}
	}
}
