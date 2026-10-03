package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func newLatexTemplateTestApp(t *testing.T) *App {
	t.Helper()
	// A dedicated home directory rather than t.TempDir(): resolving a task
	// workspace opens the memory database, and Windows refuses to delete an open
	// file, which t.TempDir's cleanup would report as a test failure. Cleanup is
	// best-effort here for the same reason.
	home, err := os.MkdirTemp("", "maclaw-latex-templates-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	app := &App{testHomeDir: home, disableBackgroundEmbeddingForTest: true}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()
	return app
}

// latexTemplateTestZip builds a template package with a single shared root
// directory, which is the shape real template archives have.
func latexTemplateTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create("my-template/" + name)
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
	path := filepath.Join(t.TempDir(), "my-template.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func parseLatexTemplateTestLibrary(t *testing.T, app *App) (LatexTemplate, []LatexTemplateCategory) {
	t.Helper()
	raw, err := app.ListLatexTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Templates  []LatexTemplate         `json:"templates"`
		Categories []LatexTemplateCategory `json:"categories"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode library: %v", err)
	}
	if len(payload.Templates) == 0 {
		t.Fatal("library must at least offer the blank template")
	}
	return payload.Templates[0], payload.Categories
}

// TestLatexTemplateInstallKeepsAMergedHubCategory pins the invariant that
// SyncLatexTemplatesFromHub relies on: installing a pack keeps the category it
// declares, as long as that category is already in the index. A sync that merged
// the categories *after* installing would break this and leave every first-sync
// template filed under 其它.
func TestLatexTemplateInstallKeepsAMergedHubCategory(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	hubCategories := []LatexTemplateCategory{{ID: "poster", Name: "海报", SortOrder: 200}}
	if err := app.mergeLatexTemplateCategories(hubCategories); err != nil {
		t.Fatal(err)
	}
	zipPath := latexTemplateTestZip(t, map[string]string{
		"main.tex":      "\\documentclass{article}\\begin{document}x\\end{document}",
		"template.json": `{"name":"Poster","category":"poster"}`,
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.CategoryID != "poster" {
		t.Fatalf("category = %q, want poster (a hub category known at install time)", imported.CategoryID)
	}
	// The template must be filed under the hub category, not under the fallback.
	library := parseLatexTemplateTestLibrary2(t, app)
	found := false
	for _, section := range library {
		if section.categoryID != "poster" {
			continue
		}
		for _, item := range section.templates {
			if item.ID == imported.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("template not filed under the hub category: %+v", library)
	}
}

// parseLatexTemplateTestLibrary2 groups the installed templates the same way the
// library page does, so a test can assert on the section a template lands in.
func parseLatexTemplateTestLibrary2(t *testing.T, app *App) []struct {
	categoryID string
	templates  []LatexTemplate
} {
	t.Helper()
	raw, err := app.ListLatexTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Categories []LatexTemplateCategory `json:"categories"`
		Templates  []LatexTemplate         `json:"templates"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	out := make([]struct {
		categoryID string
		templates  []LatexTemplate
	}, 0, len(payload.Categories))
	for _, category := range payload.Categories {
		section := struct {
			categoryID string
			templates  []LatexTemplate
		}{categoryID: category.ID}
		for _, item := range payload.Templates {
			if item.CategoryID == category.ID {
				section.templates = append(section.templates, item)
			}
		}
		out = append(out, section)
	}
	return out
}

func TestLatexTemplateLibraryShipsTheBlankOptionAndFourCategories(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	blank, categories := parseLatexTemplateTestLibrary(t, app)
	if blank.ID != latexTemplateSourceBlank {
		t.Fatalf("first entry = %q, want the blank option", blank.ID)
	}
	want := []string{"conference", "journal", "thesis", "other"}
	if len(categories) != len(want) {
		t.Fatalf("categories = %+v, want %v", categories, want)
	}
	for i, id := range want {
		if categories[i].ID != id || !categories[i].Builtin {
			t.Fatalf("category %d = %+v, want %s (builtin)", i, categories[i], id)
		}
	}
}

func TestLatexTemplateImportStripsTheArchiveWrapperDirectory(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZip(t, map[string]string{
		"main.tex":      "\\documentclass[11pt]{article}\n\\begin{document}x\\end{document}\n",
		"refs.bib":      "@article{a,title={A}}",
		"template.json": `{"name":"Journal pack","category":"journal","author":"Ada","version":"2.1"}`,
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Name != "Journal pack" || imported.CategoryID != "journal" || imported.Author != "Ada" {
		t.Fatalf("imported = %+v, want the manifest metadata", imported)
	}
	if imported.MainFile != "main.tex" {
		t.Fatalf("main_file = %q, want main.tex (wrapper stripped)", imported.MainFile)
	}
	pack := app.latexTemplatePackDir(imported.ID)
	if _, err := os.Stat(filepath.Join(pack, "main.tex")); err != nil {
		t.Fatalf("main.tex not extracted to the pack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pack, "my-template")); err == nil {
		t.Fatal("the archive wrapper directory must not survive extraction")
	}
	// Sharing re-sends the original bytes, so they must have been kept.
	if _, err := os.Stat(filepath.Join(app.latexTemplateZipDir(), imported.ID+".zip")); err != nil {
		t.Fatalf("original package not kept for sharing: %v", err)
	}
}

func TestLatexTemplateImportAcceptsPublisherSources(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZip(t, map[string]string{
		"doc/elsdoc.tex":              "\\documentclass{article}\n\\begin{document}manual\\end{document}\n",
		"elsarticle-template-num.tex": "\\documentclass{elsarticle}\n\\begin{document}x\\end{document}\n",
		"elsarticle.dtx":              "% documented source",
		"elsarticle.ins":              "% docstrip installer",
		"README":                      "readme",
		"doc/makefile":                "all:\n",
		"doc/elsdoc.pdf":              "%pdf",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "elsarticle-template-num.tex" {
		t.Fatalf("main_file = %q, want the sample article", imported.MainFile)
	}
	pack := app.latexTemplatePackDir(imported.ID)
	for _, name := range []string{"elsarticle.dtx", "elsarticle.ins", "README", filepath.Join("doc", "makefile")} {
		if _, err := os.Stat(filepath.Join(pack, name)); err != nil {
			t.Fatalf("publisher file %s was not kept: %v", name, err)
		}
	}
}

func TestLatexTemplateInstallMatchesWrappedCatalogueMain(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, body string }{
		{"elsarticle/elsarticle-template-num.tex", "\\documentclass{elsarticle}\n\\begin{document}num\\end{document}\n"},
		{"elsarticle/elsarticle-template-harv.tex", "\\documentclass{elsarticle}\n\\begin{document}harv\\end{document}\n"},
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
	raw, err := app.installLatexTemplatePackage(buf.Bytes(), latexTemplatePackageManifest{
		Name:     "Elsevier",
		Version:  "1.0.0",
		MainFile: "elsarticle/elsarticle-template-harv.tex",
	}, 2, latexTemplateSourceHub, "hub-els")
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "elsarticle-template-harv.tex" {
		t.Fatalf("main_file = %q, want the catalogue sample without the wrapper directory", imported.MainFile)
	}
}

func TestLatexTemplateInstallKeepsAnExplicitManualPath(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, body string }{
		{"elsdoc.tex", "\\documentclass{article}\n\\begin{document}root\\end{document}\n"},
		{"doc/elsdoc.tex", "\\documentclass{article}\n\\begin{document}manual\\end{document}\n"},
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
	raw, err := app.installLatexTemplatePackage(buf.Bytes(), latexTemplatePackageManifest{
		Name:     "Manual",
		Version:  "1.0.0",
		MainFile: "doc/elsdoc.tex",
	}, 2, latexTemplateSourceHub, "hub-manual")
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "doc/elsdoc.tex" {
		t.Fatalf("main_file = %q, want the manual path", imported.MainFile)
	}
}

func TestLatexTemplateImportAllowsWrapperDirectoryEntries(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir, err := zw.Create("elsarticle/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Write(nil); err != nil {
		t.Fatal(err)
	}
	body := "\\documentclass{elsarticle}\n\\begin{document}x\\end{document}\n"
	w, err := zw.Create("elsarticle/elsarticle-template-num.tex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "elsarticle.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := app.ImportLatexTemplateFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "elsarticle-template-num.tex" {
		t.Fatalf("main_file = %q", imported.MainFile)
	}
}

func TestLatexTemplateImportRejectsUnsafePackages(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"no tex source", map[string]string{"readme.txt": "hello"}},
		{"parent traversal", map[string]string{"../escape.tex": "x"}},
		{"executable", map[string]string{"main.tex": "x", "run.sh": "#!/bin/sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Written by hand so a raw ".." entry name survives; the zip helper
			// above would prefix it with the wrapper directory.
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for name, body := range tc.files {
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
			path := filepath.Join(t.TempDir(), "bad.zip")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ImportLatexTemplateFromPath(path); err == nil {
				t.Fatalf("%s must be rejected", tc.name)
			}
		})
	}
	if _, err := app.ImportLatexTemplateFromPath(filepath.Join(t.TempDir(), "missing.zip")); err == nil {
		t.Fatal("a missing package must be rejected")
	}
}

func TestCreateLatexDocumentFromTemplateMaterialisesThePack(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZip(t, map[string]string{
		"main.tex":       "\\documentclass{article}\n\\begin{document}thesis\\end{document}\n",
		"chapters/a.tex": "\\section{Intro}\n",
		"template.json":  `{"name":"Thesis","category":"thesis"}`,
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "main.tex" || result.TemplateName != "Thesis" {
		t.Fatalf("result = %+v", result)
	}
	if !result.Created {
		t.Fatal("a freshly materialised document must be reported as created")
	}
	// The document must land in exactly the directory the LaTeX workbench
	// resolves for this task — that coupling is what makes save and compile work
	// on the file the editor opens.
	root, err := app.latexDocumentRoot(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); err != nil {
		t.Fatalf("main.tex not written to the resolved workbench root %q: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "chapters", "a.tex")); err != nil {
		t.Fatalf("sub-file not written to the resolved workbench root %q: %v", root, err)
	}
}

func TestCreateLatexDocumentNeverOverwritesAnExistingPaper(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := app.latexDocumentRoot(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := app.CreateLatexDocument(taskDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var first CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatal(err)
	}
	// The user writes a page, then re-triggers "start a LaTeX paper" from the
	// library. Their work must survive.
	edited := "\\documentclass{article}\\begin{document}three pages later\\end{document}"
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	secondRaw, err := app.CreateLatexDocument(taskDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var second CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(secondRaw), &second); err != nil {
		t.Fatal(err)
	}
	if second.RelativePath != first.RelativePath {
		t.Fatalf("second call pointed at %q, want the existing %q", second.RelativePath, first.RelativePath)
	}
	if !first.Created || second.Created {
		t.Fatalf("created flags = %v then %v, want true then false", first.Created, second.Created)
	}
	got, err := os.ReadFile(filepath.Join(root, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Fatalf("existing paper was overwritten: %q", string(got))
	}
}

func TestCreateLatexDocumentDropsUntouchedBlankWhenEntryDiffers(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateLatexDocument(taskDir, "", ""); err != nil {
		t.Fatal(err)
	}
	root, err := app.latexDocumentRoot(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		"elsarticle-template-num.tex": "\\documentclass{elsarticle}\n\\begin{document}sample\\end{document}\n",
		"elsarticle-num.bst":          "% bst\n",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "elsarticle-template-num.tex" || !result.Created {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("untouched blank main.tex still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "elsarticle-template-num.tex")); err != nil {
		t.Fatal(err)
	}

	// An edited main.tex next to a different entry must survive a second apply.
	userMain := "\\documentclass{article}\\begin{document}user\\end{document}\n"
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(userMain), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateLatexDocument(taskDir, imported.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != userMain {
		t.Fatalf("edited main.tex was removed or replaced: %q", string(got))
	}
}

func TestCreateLatexDocumentReplacesUntouchedBlankWithTemplate(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateLatexDocument(taskDir, "", ""); err != nil {
		t.Fatal(err)
	}
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		"main.tex": "\\documentclass{elsarticle}\n\\begin{document}from template\\end{document}\n",
		"refs.bib": "@article{k,title={t}}\n",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.RelativePath != "main.tex" {
		t.Fatalf("blank skeleton was treated as an existing paper: %+v", result)
	}
	root := result.WorkspacePath
	got, err := os.ReadFile(filepath.Join(root, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "from template") {
		t.Fatalf("template did not replace the blank skeleton: %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(root, "refs.bib")); err != nil {
		t.Fatal(err)
	}

	edited := "\\documentclass{article}\\begin{document}user edits\\end{document}\n"
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	againRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var again CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(againRaw), &again); err != nil {
		t.Fatal(err)
	}
	if again.Created {
		t.Fatal("an edited paper must be reused, not recreated")
	}
	kept, err := os.ReadFile(filepath.Join(root, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != edited {
		t.Fatalf("edited paper was overwritten: %q", string(kept))
	}
}

func TestCreateLatexDocumentKeepsThePackEntryNameWhenNoNameIsRequested(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"paper.tex": "\\documentclass{article}\\begin{document}x\\end{document}",
	} {
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
	path := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := app.ImportLatexTemplateFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "paper.tex" {
		t.Fatalf("imported main_file = %q, want paper.tex", imported.MainFile)
	}
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No file name requested: the pack's own entry point must be used as-is
	// rather than being silently renamed to main.tex.
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "paper.tex" {
		t.Fatalf("result = %+v, want paper.tex kept", result)
	}
	root, err := app.latexDocumentRoot(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); err == nil {
		t.Fatal("paper.tex must not have been renamed to main.tex")
	}

	// Asking for a name must be honoured on a fresh workspace, which only works
	// if the rename happens after the pack is copied.
	secondTask := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(secondTask, 0o755); err != nil {
		t.Fatal(err)
	}
	namedRaw, err := app.CreateLatexDocument(secondTask, imported.ID, "thesis.tex")
	if err != nil {
		t.Fatal(err)
	}
	var named CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(namedRaw), &named); err != nil {
		t.Fatal(err)
	}
	if named.RelativePath != "thesis.tex" {
		t.Fatalf("requested name ignored: %+v", named)
	}
	secondRoot, err := app.latexDocumentRoot(secondTask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(secondRoot, "thesis.tex")); err != nil {
		t.Fatalf("thesis.tex not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondRoot, "paper.tex")); err == nil {
		t.Fatal("paper.tex should have been renamed, not duplicated")
	}
}

// elsevierNumEntry is the pack main among several \documentclass samples.
// Alphabetical scan would stop on the Harvard sample; the installed main file
// is what makes the numeric sample the paper.
const elsevierNumEntry = "elsarticle/elsarticle-template-num.tex"

func elsevierSampleSources() map[string]string {
	return map[string]string{
		"elsarticle/elsarticle-template-harv.tex": "\\documentclass{elsarticle}\n\\begin{document}harv\\end{document}\n",
		elsevierNumEntry: "\\documentclass{elsarticle}\n\\begin{document}num\\end{document}\n",
		"elsarticle/elsarticle-template-num-names.tex": "\\documentclass{elsarticle}\n\\begin{document}names\\end{document}\n",
		"elsarticle/fig-transformer-block.tex":         "\\documentclass[tikz,border=10pt]{standalone}\n\\begin{document}fig\\end{document}\n",
		"elsarticle/doc/elsdoc.tex":                    "\\documentclass{article}\n\\begin{document}manual\\end{document}\n",
	}
}

func importElsevierNumTemplate(t *testing.T, app *App) LatexTemplate {
	t.Helper()
	files := elsevierSampleSources()
	files["template.json"] = `{"name":"Elsevier","main_file":"` + elsevierNumEntry + `"}`
	raw, err := app.ImportLatexTemplateFromPath(latexTemplateTestZipEntries(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != elsevierNumEntry {
		t.Fatalf("installed main_file = %q, want %s", imported.MainFile, elsevierNumEntry)
	}
	return imported
}

func writeLatexWorkspaceFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func decodeCreateLatexDocumentResult(t *testing.T, raw string) CreateLatexDocumentResult {
	t.Helper()
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// TestCreateLatexDocumentBlankReopensInstalledPackEntry is the reopen that used
// to invent root main.tex. The workspace is not a managed task, so there is no
// latex_entry tag; the installed pack's main file is the fact that selects the
// numeric sample over the Harvard sample, the standalone figure and the manual.
func TestCreateLatexDocumentBlankReopensInstalledPackEntry(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	importElsevierNumTemplate(t, app)

	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	freshRoot, err := app.latexDocumentRoot(fresh)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, freshRoot, elsevierSampleSources())
	opened := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, fresh, "", ""))
	if opened.RelativePath != elsevierNumEntry || opened.Created {
		t.Fatalf("blank reopen = %+v, want existing %s", opened, elsevierNumEntry)
	}
	if _, err := os.Stat(filepath.Join(freshRoot, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("blank reopen created root main.tex: %v", err)
	}
	if app.taskLatexEntry(fresh) != "" {
		t.Fatalf("unmanaged workspace stored an entry tag %q", app.taskLatexEntry(fresh))
	}

	// The untouched skeleton beside a different entry is not a paper.
	withSkeleton := filepath.Join(t.TempDir(), "skeleton")
	if err := os.MkdirAll(withSkeleton, 0o755); err != nil {
		t.Fatal(err)
	}
	skeletonRoot, err := app.latexDocumentRoot(withSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, skeletonRoot, elsevierSampleSources())
	if err := os.WriteFile(filepath.Join(skeletonRoot, "main.tex"), []byte(latexBlankTemplateSource), 0o644); err != nil {
		t.Fatal(err)
	}
	dropped := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, withSkeleton, "", ""))
	if dropped.RelativePath != elsevierNumEntry || dropped.Created {
		t.Fatalf("skeleton reopen = %+v", dropped)
	}
	if _, err := os.Stat(filepath.Join(skeletonRoot, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("untouched skeleton was kept: %v", err)
	}

	// An edited root main.tex is a real file and stays on disk. It is not the
	// pack entry, so the paper remains the installed main.
	editedRoot := filepath.Join(t.TempDir(), "edited")
	if err := os.MkdirAll(editedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	editedDir, err := app.latexDocumentRoot(editedRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, editedDir, elsevierSampleSources())
	userMain := "\\documentclass{article}\\begin{document}user paper\\end{document}\n"
	if err := os.WriteFile(filepath.Join(editedDir, "main.tex"), []byte(userMain), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, editedRoot, "", ""))
	if kept.RelativePath != elsevierNumEntry {
		t.Fatalf("edited neighbour stole the entry: %+v", kept)
	}
	got, err := os.ReadFile(filepath.Join(editedDir, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != userMain {
		t.Fatalf("edited main.tex changed: %q", string(got))
	}
}

// TestCreateLatexDocumentBlankKeepsTheRecordedEntry plants the samples in the
// task's working directory, which is not the task folder. The first blank
// reopen records the pack entry. A second installed main in the same tree
// must not replace that record: a rescan would otherwise pick the new file,
// which sorts ahead of every elsarticle sample.
func TestCreateLatexDocumentBlankKeepsTheRecordedEntry(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	importElsevierNumTemplate(t, app)
	work := filepath.Join(t.TempDir(), "latex-paper")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := app.createExpertTask(builtinLatexExpertID, "LaTeX", work)
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	root, err := app.latexDocumentRoot(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !latexTestSamePath(root, work) {
		t.Fatalf("document root = %q, want working dir %q", root, work)
	}
	writeLatexWorkspaceFiles(t, root, elsevierSampleSources())
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(latexBlankTemplateSource), 0o644); err != nil {
		t.Fatal(err)
	}

	first := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if first.RelativePath != elsevierNumEntry || first.Created {
		t.Fatalf("first reopen = %+v", first)
	}
	if !latexTestSamePath(first.WorkspacePath, work) {
		t.Fatalf("workspace = %q, want %q", first.WorkspacePath, work)
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("untouched skeleton survived reopen: %v", err)
	}
	if _, err := os.Stat(filepath.Join(created.ProjectPath, "main.tex")); err == nil {
		t.Fatal("blank reopen wrote main.tex into the task directory")
	}
	if _, err := os.Stat(filepath.Join(created.ProjectPath, "workspace", "main.tex")); err == nil {
		t.Fatal("blank reopen wrote main.tex into the task workspace")
	}
	if app.taskLatexEntry(created.ProjectPath) != elsevierNumEntry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(created.ProjectPath))
	}

	notes := latexTemplateTestZipEntries(t, map[string]string{
		"template.json": `{"name":"Notes","main_file":"aaa.tex"}`,
		"aaa.tex":       "\\documentclass{article}\n\\begin{document}aaa\\end{document}\n",
	})
	if _, err := app.ImportLatexTemplateFromPath(notes); err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, root, map[string]string{
		"aaa.tex": "\\documentclass{article}\n\\begin{document}aaa\\end{document}\n",
	})
	second := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if second.RelativePath != elsevierNumEntry || second.Created {
		t.Fatalf("later sample retargeted the paper: %+v", second)
	}
	if app.taskLatexEntry(created.ProjectPath) != elsevierNumEntry {
		t.Fatalf("recorded entry changed to %q", app.taskLatexEntry(created.ProjectPath))
	}
	if _, err := os.Stat(filepath.Join(root, "aaa.tex")); err != nil {
		t.Fatal(err)
	}
}

// TestCreateLatexDocumentBlankDoesNotRecordAnAmbiguousScan keeps a guess off
// the task. Several samples and no single installed main would otherwise
// freeze the alphabetical file, and installing the pack later could not
// correct it. The untouched skeleton is still removed.
func TestCreateLatexDocumentBlankDoesNotRecordAnAmbiguousScan(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	work := filepath.Join(t.TempDir(), "samples")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := app.createExpertTask(builtinLatexExpertID, "LaTeX", work)
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	root, err := app.latexDocumentRoot(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, root, elsevierSampleSources())
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(latexBlankTemplateSource), 0o644); err != nil {
		t.Fatal(err)
	}
	guessed := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if guessed.RelativePath != "elsarticle/elsarticle-template-harv.tex" || guessed.Created {
		t.Fatalf("scan = %+v", guessed)
	}
	if app.taskLatexEntry(created.ProjectPath) != "" {
		t.Fatalf("ambiguous scan was recorded as %q", app.taskLatexEntry(created.ProjectPath))
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("untouched skeleton survived the scan: %v", err)
	}

	importElsevierNumTemplate(t, app)
	chosen := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if chosen.RelativePath != elsevierNumEntry || chosen.Created {
		t.Fatalf("pack main after the scan = %+v", chosen)
	}
	if app.taskLatexEntry(created.ProjectPath) != elsevierNumEntry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(created.ProjectPath))
	}
}

// TestCreateLatexDocumentBlankKeepsInstalledMainPastSuffixTwin is the pack
// file itself. A second copy that only ends with the same path sorts first;
// recording that copy would freeze the wrong paper.
func TestCreateLatexDocumentBlankKeepsInstalledMainPastSuffixTwin(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	const entry = "elsarticle/paper.tex"
	pack := map[string]string{
		"template.json": `{"name":"Paper","main_file":"` + entry + `"}`,
		entry:           "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
		"notes.tex":     "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	}
	if _, err := app.ImportLatexTemplateFromPath(latexTemplateTestZipEntries(t, pack)); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "latex-paper")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := app.createExpertTask(builtinLatexExpertID, "LaTeX", work)
	if err != nil {
		t.Fatal(err)
	}
	root, err := app.latexDocumentRoot(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, root, map[string]string{
		entry:                      "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
		"aaa/elsarticle/paper.tex": "\\documentclass{article}\n\\begin{document}twin\\end{document}\n",
		"notes.tex":                "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	opened := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if opened.RelativePath != entry || opened.Created {
		t.Fatalf("suffix twin stole the entry: %+v", opened)
	}
	if app.taskLatexEntry(created.ProjectPath) != entry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(created.ProjectPath))
	}
}

// TestCreateLatexDocumentBlankDoesNotRecordAnAmbiguousInstalledMatch is a pack
// main that is not on disk. Two copies that only end with that path are not
// the paper, and the first one in sorted order must not be stored. One deeper
// copy is opened so the editor does not land on another sample, but it is not
// stored, so the declared file can still replace it. The same copy is stored
// when it is the only source. A single wrapped name is a real match, and an
// unrelated pair of copies does not hide a pack file that is present.
func TestCreateLatexDocumentBlankDoesNotRecordAnAmbiguousInstalledMatch(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	const entry = "elsarticle/paper.tex"
	pack := map[string]string{
		"template.json": `{"name":"Paper","main_file":"` + entry + `"}`,
		entry:           "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
	}
	if _, err := app.ImportLatexTemplateFromPath(latexTemplateTestZipEntries(t, pack)); err != nil {
		t.Fatal(err)
	}
	other := map[string]string{
		"template.json":     `{"name":"Other","main_file":"notes/chapter.tex"}`,
		"notes/chapter.tex": "\\documentclass{article}\n\\begin{document}chapter\\end{document}\n",
	}
	if _, err := app.ImportLatexTemplateFromPath(latexTemplateTestZipEntries(t, other)); err != nil {
		t.Fatal(err)
	}

	newTask := func(t *testing.T, name string) (string, string) {
		t.Helper()
		work := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(work, 0o755); err != nil {
			t.Fatal(err)
		}
		// Each paper is its own task. createExpertTask would retarget the
		// newest LaTeX task and carry its latex_entry tag into the next case.
		created, err := app.createFreshLatexExpertTask("LaTeX", work)
		if err != nil {
			t.Fatal(err)
		}
		root, err := app.latexDocumentRoot(created.ProjectPath)
		if err != nil {
			t.Fatal(err)
		}
		return created.ProjectPath, root
	}

	project, root := newTask(t, "twins")
	writeLatexWorkspaceFiles(t, root, map[string]string{
		"aaa/elsarticle/paper.tex": "\\documentclass{article}\n\\begin{document}aaa\\end{document}\n",
		"bbb/elsarticle/paper.tex": "\\documentclass{article}\n\\begin{document}bbb\\end{document}\n",
		"notes.tex":                "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(latexBlankTemplateSource), 0o644); err != nil {
		t.Fatal(err)
	}
	guessed := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, project, "", ""))
	if guessed.RelativePath != "aaa/elsarticle/paper.tex" || guessed.Created {
		t.Fatalf("ambiguous installed match = %+v", guessed)
	}
	if app.taskLatexEntry(project) != "" {
		t.Fatalf("ambiguous installed match was recorded as %q", app.taskLatexEntry(project))
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("untouched skeleton survived the ambiguous match: %v", err)
	}

	writeLatexWorkspaceFiles(t, root, map[string]string{
		entry: "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
	})
	chosen := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, project, "", ""))
	if chosen.RelativePath != entry || chosen.Created {
		t.Fatalf("declared file after the twins = %+v", chosen)
	}
	if app.taskLatexEntry(project) != entry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(project))
	}

	// notes.tex sorts first, so a documentclass scan would open it. The deeper
	// copy is the file to show, and it must not become the recorded paper.
	const nested = "zzz/elsarticle/paper.tex"
	nestedProject, nestedRoot := newTask(t, "one-copy")
	writeLatexWorkspaceFiles(t, nestedRoot, map[string]string{
		nested:      "\\documentclass{article}\n\\begin{document}nested\\end{document}\n",
		"notes.tex": "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	hint := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, nestedProject, "", ""))
	if hint.RelativePath != nested || hint.Created {
		t.Fatalf("single deeper copy = %+v", hint)
	}
	if app.taskLatexEntry(nestedProject) != "" {
		t.Fatalf("deeper copy was recorded as %q", app.taskLatexEntry(nestedProject))
	}
	writeLatexWorkspaceFiles(t, nestedRoot, map[string]string{
		entry: "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
	})
	adopted := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, nestedProject, "", ""))
	if adopted.RelativePath != entry || adopted.Created {
		t.Fatalf("declared file after the deeper copy = %+v", adopted)
	}
	if app.taskLatexEntry(nestedProject) != entry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(nestedProject))
	}

	onlyProject, onlyRoot := newTask(t, "only-copy")
	writeLatexWorkspaceFiles(t, onlyRoot, map[string]string{
		nested: "\\documentclass{article}\n\\begin{document}nested\\end{document}\n",
	})
	only := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, onlyProject, "", ""))
	if only.RelativePath != nested || only.Created {
		t.Fatalf("only deeper copy = %+v", only)
	}
	if app.taskLatexEntry(onlyProject) != nested {
		t.Fatalf("only deeper copy recorded as %q", app.taskLatexEntry(onlyProject))
	}

	wrappedProject, wrappedRoot := newTask(t, "wrapped")
	writeLatexWorkspaceFiles(t, wrappedRoot, map[string]string{
		"paper.tex": "\\documentclass{article}\n\\begin{document}wrapped\\end{document}\n",
		"notes.tex": "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	wrapped := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, wrappedProject, "", ""))
	if wrapped.RelativePath != "paper.tex" || wrapped.Created {
		t.Fatalf("wrapped pack main = %+v", wrapped)
	}
	if app.taskLatexEntry(wrappedProject) != "paper.tex" {
		t.Fatalf("wrapped entry recorded as %q", app.taskLatexEntry(wrappedProject))
	}

	// Deeper copies sort first and would win a documentclass scan. They must
	// not hide the wrapper name, and they must not keep it from being stored.
	wrappedBeside, wrappedBesideRoot := newTask(t, "wrapped-beside-copies")
	writeLatexWorkspaceFiles(t, wrappedBesideRoot, map[string]string{
		"paper.tex":                "\\documentclass{article}\n\\begin{document}wrapped\\end{document}\n",
		"aaa/elsarticle/paper.tex": "\\documentclass{article}\n\\begin{document}aaa\\end{document}\n",
		"bbb/elsarticle/paper.tex": "\\documentclass{article}\n\\begin{document}bbb\\end{document}\n",
		"notes.tex":                "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	beside := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, wrappedBeside, "", ""))
	if beside.RelativePath != "paper.tex" || beside.Created {
		t.Fatalf("deeper copies hid the wrapper: %+v", beside)
	}
	if app.taskLatexEntry(wrappedBeside) != "paper.tex" {
		t.Fatalf("wrapper recorded as %q", app.taskLatexEntry(wrappedBeside))
	}

	exactProject, exactRoot := newTask(t, "exact-beside-twins")
	writeLatexWorkspaceFiles(t, exactRoot, map[string]string{
		entry:                   "\\documentclass{article}\n\\begin{document}pack\\end{document}\n",
		"aaa/notes/chapter.tex": "\\documentclass{article}\n\\begin{document}aaa\\end{document}\n",
		"bbb/notes/chapter.tex": "\\documentclass{article}\n\\begin{document}bbb\\end{document}\n",
	})
	exact := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, exactProject, "", ""))
	if exact.RelativePath != entry || exact.Created {
		t.Fatalf("unrelated twins hid the pack file: %+v", exact)
	}
	if app.taskLatexEntry(exactProject) != entry {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(exactProject))
	}
}

func TestCreateLatexDocumentBlankRecordsTheOnlySource(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	created := app.CreateExpertTask(builtinLatexExpertID, "LaTeX")
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	root, err := app.latexDocumentRoot(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	writeLatexWorkspaceFiles(t, root, map[string]string{
		"notes.tex": "\\documentclass{article}\n\\begin{document}notes\\end{document}\n",
	})
	opened := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if opened.RelativePath != "notes.tex" || opened.Created {
		t.Fatalf("only source = %+v", opened)
	}
	if app.taskLatexEntry(created.ProjectPath) != "notes.tex" {
		t.Fatalf("recorded entry = %q", app.taskLatexEntry(created.ProjectPath))
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("blank reopen created main.tex beside the only source: %v", err)
	}
}

// TestCreateLatexDocumentTemplateApplyIgnoresStaleBlankEntry records the
// skeleton first, which is what a blank open persists. Applying a real
// template must still replace that skeleton; the blank tag belongs only to
// the blank reopen path.
func TestCreateLatexDocumentTemplateApplyIgnoresStaleBlankEntry(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	imported := importElsevierNumTemplate(t, app)
	created := app.CreateExpertTask(builtinLatexExpertID, "LaTeX")
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	root, err := app.latexDocumentRoot(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	blank := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, "", ""))
	if blank.RelativePath != "main.tex" || !blank.Created {
		t.Fatalf("empty workspace = %+v", blank)
	}
	if app.taskLatexEntry(created.ProjectPath) != "main.tex" {
		t.Fatalf("blank entry = %q", app.taskLatexEntry(created.ProjectPath))
	}

	applied := decodeCreateLatexDocumentResult(t, mustCreateLatexDocument(t, app, created.ProjectPath, imported.ID, ""))
	if applied.RelativePath != elsevierNumEntry || !applied.Created {
		t.Fatalf("template apply = %+v", applied)
	}
	if _, err := os.Stat(filepath.Join(root, "main.tex")); !os.IsNotExist(err) {
		t.Fatalf("stale skeleton was kept: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(elsevierNumEntry)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "num") {
		t.Fatalf("pack entry was not written: %q", string(got))
	}
	if app.taskLatexEntry(created.ProjectPath) != elsevierNumEntry {
		t.Fatalf("entry tag after apply = %q", app.taskLatexEntry(created.ProjectPath))
	}
}

func mustCreateLatexDocument(t *testing.T, app *App, projectPath, templateID, fileName string) string {
	t.Helper()
	raw, err := app.CreateLatexDocument(projectPath, templateID, fileName)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func latexTestSamePath(a, b string) bool {
	left, leftErr := filepath.Abs(a)
	right, rightErr := filepath.Abs(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = resolved
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = resolved
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func TestDeleteLatexTemplateKeepsTheBlankOption(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	if err := app.DeleteLatexTemplate(latexTemplateSourceBlank); err == nil {
		t.Fatal("the blank option must not be deletable")
	}
	zipPath := latexTemplateTestZip(t, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}x\\end{document}"})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteLatexTemplate(imported.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app.latexTemplatePackDir(imported.ID)); !os.IsNotExist(err) {
		t.Fatalf("pack directory survived deletion: %v", err)
	}
	library, _ := parseLatexTemplateTestLibrary(t, app)
	if library.ID != latexTemplateSourceBlank {
		t.Fatalf("after delete the library starts with %q, want the blank option", library.ID)
	}
}

func TestReimportingTheSameTemplateUpdatesInPlace(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	files := map[string]string{
		"main.tex":      "\\documentclass{article}\\begin{document}x\\end{document}",
		"template.json": `{"name":"Conference","category":"conference","version":"1.0"}`,
	}
	firstRaw, err := app.ImportLatexTemplateFromPath(latexTemplateTestZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var first LatexTemplate
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatal(err)
	}
	secondRaw, err := app.ImportLatexTemplateFromPath(latexTemplateTestZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var second LatexTemplate
	if err := json.Unmarshal([]byte(secondRaw), &second); err != nil {
		t.Fatal(err)
	}
	// A re-import must not shadow the existing entry with a duplicate: the
	// administrator's category change and the share status live on that row.
	if second.ID != first.ID {
		t.Fatalf("re-import created a second row: %q then %q", first.ID, second.ID)
	}
	raw, err := app.ListLatexTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Templates []LatexTemplate `json:"templates"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Templates) != 2 {
		t.Fatalf("library has %d templates, want the blank option plus one pack", len(payload.Templates))
	}
}

func TestMergeLatexTemplateCategoriesAddsHubOnlyCategories(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	if err := app.mergeLatexTemplateCategories([]LatexTemplateCategory{
		{ID: "conference", Name: "会议"},
		{ID: "poster", Name: "海报", SortOrder: 200},
	}); err != nil {
		t.Fatal(err)
	}
	_, categories := parseLatexTemplateTestLibrary(t, app)
	if len(categories) != 5 {
		t.Fatalf("categories = %+v, want the four built-ins plus poster", categories)
	}
	// The four product sections must stay first and in order.
	for i, id := range []string{"conference", "journal", "thesis", "other"} {
		if categories[i].ID != id {
			t.Fatalf("category %d = %q, want %q", i, categories[i].ID, id)
		}
	}
	if categories[4].ID != "poster" {
		t.Fatalf("hub category = %+v, want poster appended", categories[4])
	}
}

func TestLatexTemplateDocumentNameRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../evil", `sub\evil`, ""} {
		if got := latexTemplateDocumentName(name); got != "main.tex" {
			t.Fatalf("latexTemplateDocumentName(%q) = %q, want main.tex", name, got)
		}
	}
	if got := latexTemplateDocumentName("paper"); got != "paper.tex" {
		t.Fatalf("latexTemplateDocumentName(\"paper\") = %q, want paper.tex", got)
	}
	if got := latexTemplateDocumentName("paper.ltx"); got != "paper.ltx" {
		t.Fatalf("latexTemplateDocumentName(\"paper.ltx\") = %q, want it kept", got)
	}
}

// latexTemplateTestZipEntries writes the given paths unchanged, so a test can
// put sources at the archive root or under any subdirectory.
func latexTemplateTestZipEntries(t *testing.T, files map[string]string) string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCreateLatexDocumentUnpacksWrapperAndKeepsNestedDir(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		"elsarticle/elsarticle-template-num.tex": "\\documentclass{elsarticle}\n\\begin{document}sample\\end{document}\n",
		"elsarticle/elsarticle-num.bst":          "% bst\n",
		"elsarticle/doc/elsdoc.tex":              "\\documentclass{article}\n\\begin{document}manual\\end{document}\n",
		"elsarticle/README":                      "readme\n",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "elsarticle-template-num.tex" {
		t.Fatalf("main file = %q, want the sample at the stripped root", imported.MainFile)
	}
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "elsarticle-template-num.tex" {
		t.Fatalf("entry = %q", result.RelativePath)
	}
	root := result.WorkspacePath
	if root == "" {
		t.Fatal("workspace path was not reported")
	}
	if _, err := os.Stat(filepath.Join(root, "elsarticle-template-num.tex")); err != nil {
		t.Fatalf("entry not at workspace root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "elsarticle", "elsarticle-template-num.tex")); err == nil {
		t.Fatal("single wrapper directory was kept")
	}
	if _, err := os.Stat(filepath.Join(root, "doc", "elsdoc.tex")); err != nil {
		t.Fatalf("nested manual was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "elsarticle-num.bst")); err != nil {
		t.Fatalf("bibliography style was not unpacked: %v", err)
	}
	if len(result.SourceFiles) == 0 || result.SourceFiles[0] != "elsarticle-template-num.tex" {
		t.Fatalf("source files = %#v", result.SourceFiles)
	}
	joined := strings.Join(result.SourceFiles, "\n")
	if strings.Contains(joined, "doc/elsdoc.tex") {
		t.Fatalf("class manual must not crowd the paper source list: %#v", result.SourceFiles)
	}
	if !strings.Contains(joined, "elsarticle-num.bst") {
		t.Fatalf("source files = %#v", result.SourceFiles)
	}
}

func TestCreateLatexDocumentStripsWrapperDespiteArchiveJunk(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		".DS_Store":                       "junk",
		"Thumbs.db":                       "junk",
		"__MACOSX/elsarticle/._paper.tex": "junk",
		"elsarticle/paper.tex":            "\\documentclass{article}\n\\begin{document}x\\end{document}\n",
		"elsarticle/refs.bib":             "@article{k,title={t}}\n",
		"elsarticle/Thumbs.db":            "junk",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.MainFile != "paper.tex" {
		t.Fatalf("main file = %q", imported.MainFile)
	}
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "paper.tex" {
		t.Fatalf("entry = %q", result.RelativePath)
	}
	root := result.WorkspacePath
	if _, err := os.Stat(filepath.Join(root, "paper.tex")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "refs.bib")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "elsarticle", "paper.tex")); err == nil {
		t.Fatal("wrapper directory was kept because archive junk looked like a second root")
	}
	for _, junk := range []string{".DS_Store", "Thumbs.db", filepath.Join("__MACOSX", "elsarticle", "._paper.tex")} {
		if _, err := os.Stat(filepath.Join(root, junk)); err == nil {
			t.Fatalf("archive junk was unpacked: %s", junk)
		}
	}
}

func TestCreateLatexDocumentUnpacksFilesThatAlreadySitAtZipRoot(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		"main.tex":       "\\documentclass{article}\n\\begin{document}root\\end{document}\n",
		"chapters/a.tex": "\\section{A}\n",
		"refs.bib":       "@article{k,title={t}}\n",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(t.TempDir(), "latex-task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(taskDir, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "main.tex" {
		t.Fatalf("entry = %q", result.RelativePath)
	}
	root := result.WorkspacePath
	if _, err := os.Stat(filepath.Join(root, "main.tex")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "chapters", "a.tex")); err != nil {
		t.Fatalf("subdirectory source missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "refs.bib")); err != nil {
		t.Fatalf("bibliography missing: %v", err)
	}
}

func TestCreateLatexDocumentPinsExpertOntoUnpackedWorkspace(t *testing.T) {
	app := newLatexTemplateTestApp(t)
	created := app.CreateExpertTask(builtinLatexExpertID, "LaTeX")
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	desktop := filepath.Join(t.TempDir(), "desktop")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	tabID := "expert-" + builtinLatexExpertID
	if err := app.SetTabWorkingDir(tabID, desktop); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	owner := expertSessionUserID(builtinLatexExpertID)
	if got := app.EffectiveWorkingDirForOwner(owner); filepath.Clean(got) != filepath.Clean(desktop) {
		t.Fatalf("expert cwd = %q, want the stale desktop %q", got, desktop)
	}
	zipPath := latexTemplateTestZipEntries(t, map[string]string{
		"journal/paper.tex": "\\documentclass{article}\n\\begin{document}x\\end{document}\n",
		"journal/refs.bib":  "@article{k,title={t}}\n",
	})
	raw, err := app.ImportLatexTemplateFromPath(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var imported LatexTemplate
	if err := json.Unmarshal([]byte(raw), &imported); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.CreateLatexDocument(created.ProjectPath, imported.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var result CreateLatexDocumentResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != "paper.tex" {
		t.Fatalf("wrapper was not stripped: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(result.WorkspacePath, "paper.tex")); err != nil {
		t.Fatal(err)
	}
	if got := app.EffectiveWorkingDirForOwner(owner); filepath.Clean(got) != filepath.Clean(result.WorkspacePath) {
		t.Fatalf("expert cwd = %q, want unpacked workspace %q", got, result.WorkspacePath)
	}
}

func TestLatexTemplatePackSourcesKeepsBibliographyPastChapterCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("chapters/c%02d.tex", i)
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refs.bib"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := latexTemplatePackSources(dir, "", "main.tex")
	if len(got) == 0 || got[0] != "main.tex" {
		t.Fatalf("entry = %#v", got)
	}
	if len(got) > 24 {
		t.Fatalf("len = %d", len(got))
	}
	seen := false
	for _, item := range got {
		if item == "refs.bib" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("bibliography dropped from %#v", got)
	}
}

func TestLatexTemplateWorkspacePathInsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pack")
	if !latexTemplatePathInsideRoot(root, filepath.Join(root, "a", "b.tex")) {
		t.Fatal("a nested path must be inside the root")
	}
	if latexTemplatePathInsideRoot(root, filepath.Join(filepath.Dir(root), "b.tex")) {
		t.Fatal("a sibling path must not be inside the root")
	}
}

func TestLatexBuiltInExpertIsRegistered(t *testing.T) {
	// The LaTeX library, the new-task template picker and the LaTeX editing mode
	// are all keyed off this id, so it must exist in the binary.
	found := false
	for _, expert := range builtinExperts() {
		if expert.ID == builtinLatexExpertID {
			found = true
			if !expert.Builtin {
				t.Fatal("the LaTeX expert must be a built-in")
			}
			// The persona has to tell the model the document already exists and
			// that compiling is the verification step; those two facts are the
			// whole reason this expert differs from the plain writing ones.
			for _, want := range []string{".tex", "编译"} {
				if !strings.Contains(expert.SystemPrompt, want) {
					t.Fatalf("the LaTeX expert persona does not mention %q", want)
				}
			}
		}
	}
	if !found {
		t.Fatalf("built-in expert %q is not registered", builtinLatexExpertID)
	}
	if builtinExpertByID(builtinLatexExpertID) == nil {
		t.Fatal("builtinExpertByID cannot resolve the LaTeX expert")
	}
}
