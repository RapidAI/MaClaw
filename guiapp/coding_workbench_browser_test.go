package guiapp

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestCleanCodingWorkbenchBrowserPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "", true},
		{".", "", true},
		{"src/main.go", "src/main.go", true},
		{`src\\main.go`, "src/main.go", true},
		{"../secret", "", false},
		{"/etc/passwd", "", false},
		{`C:\\secret.txt`, "", false},
	}
	for _, tc := range cases {
		got, err := cleanCodingWorkbenchBrowserPath(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("cleanCodingWorkbenchBrowserPath(%q) error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("cleanCodingWorkbenchBrowserPath(%q) unexpectedly succeeded with %q", tc.input, got)
		}
		if tc.ok && got != tc.want {
			t.Fatalf("cleanCodingWorkbenchBrowserPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCodingWorkbenchBrowserRemotePathKeepsRemoteWorkDirForRoot(t *testing.T) {
	root := "/home/sysinfo18"
	cases := []struct {
		relative string
		want     string
	}{
		{relative: "", want: root},
		{relative: ".", want: root},
		{relative: "src", want: root + "/src"},
	}
	for _, tc := range cases {
		got := codingWorkbenchBrowserRemotePath(root, tc.relative)
		if got != tc.want {
			t.Errorf("codingWorkbenchBrowserRemotePath(%q, %q) = %q, want %q", root, tc.relative, got, tc.want)
		}
		if !remotePathWithinDir(got, root) {
			t.Errorf("codingWorkbenchBrowserRemotePath(%q, %q) = %q is outside %q", root, tc.relative, got, root)
		}
	}
}

func TestRemotePathWithinDirAllowsChildrenOfFilesystemRoot(t *testing.T) {
	if !remotePathWithinDir("/etc/hosts", "/") {
		t.Fatal("a remote work_dir of / must allow paths under the filesystem root")
	}
}

func TestOmitCloudWorkspaceAbsPath(t *testing.T) {
	if got := omitCloudWorkspaceAbsPath(`C:\Users\me\.maclaw\data\cloud-workspaces\t\cws\a.md`); got != "" {
		t.Fatalf("cloud cache path leaked: %q", got)
	}
	if got := omitCloudWorkspaceAbsPath(`C:\Users\me\.maclaw\data\cloud-workspaces-readonly\t\cws\a.md`); got != "" {
		t.Fatalf("readonly cache path leaked: %q", got)
	}
	if got := omitCloudWorkspaceAbsPath("/workspace/src/main.go"); got != "/workspace/src/main.go" {
		t.Fatalf("local path omitted unexpectedly: %q", got)
	}
}

func TestCodingWorkbenchEntryPropertiesOmitsCloudCacheAbsPath(t *testing.T) {
	properties := codingWorkbenchEntryProperties("report.md", `C:\Users\me\.maclaw\data\cloud-workspaces\t\cws\report.md`, "report.md", false, 10, 1, "0644")
	if properties.AbsPath != "" {
		t.Fatalf("cloud cache abs path leaked: %q", properties.AbsPath)
	}
	if properties.Path != "report.md" {
		t.Fatalf("path = %q", properties.Path)
	}
}

func TestCodingWorkbenchEntryPropertiesUsesPortablePermissionAndFileMetadata(t *testing.T) {
	properties := codingWorkbenchEntryProperties("src/APP.GO", "/workspace/src/APP.GO", "APP.GO", false, 1536, 123, "0640")
	if properties.Extension != "go" || !properties.SizeKnown || properties.Size != 1536 {
		t.Fatalf("file properties = %+v, want extension, size and known size", properties)
	}
	if properties.AbsPath != "/workspace/src/APP.GO" || properties.Mode != "0640" {
		t.Fatalf("file properties = %+v, want full path and portable permissions", properties)
	}

	directory := codingWorkbenchEntryProperties("src", "/workspace/src", "src", true, 4096, 123, "0755")
	if directory.SizeKnown || directory.Extension != "" {
		t.Fatalf("directory properties = %+v, directory sizes and extensions must not be reported", directory)
	}
}

func TestReadCodingWorkbenchBrowserTextFileBoundsLargePreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	content := strings.Repeat("a", codingWorkbenchBrowserMaxReadBytes+100)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, truncated, err := readCodingWorkbenchBrowserTextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected large file preview to be truncated")
	}
	if len([]rune(preview)) != codingWorkbenchBrowserMaxRunes {
		t.Fatalf("preview rune count = %d, want %d", len([]rune(preview)), codingWorkbenchBrowserMaxRunes)
	}
}

func TestReadCodingWorkbenchBrowserTextFileRejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCodingWorkbenchBrowserTextFile(path); err == nil {
		t.Fatal("expected invalid UTF-8 preview to be rejected")
	}
}

func TestSortCodingWorkbenchDirectoryEntriesPlacesDirectoriesFirst(t *testing.T) {
	entries := []CodingWorkbenchDirectoryEntry{
		{Name: "z.txt"},
		{Name: "beta", IsDir: true},
		{Name: "Alpha", IsDir: true},
		{Name: "a.txt"},
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	got := []string{entries[0].Name, entries[1].Name, entries[2].Name, entries[3].Name}
	want := []string{"Alpha", "beta", "a.txt", "z.txt"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("sorted entries = %v, want %v", got, want)
	}
}

func TestCollectCodingWorkbenchDirectoryEntriesBoundsTheFirstPage(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= codingWorkbenchBrowserMaxEntries; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	entries, truncated, err := collectCodingWorkbenchDirectoryEntries(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(entries) != codingWorkbenchBrowserMaxEntries {
		t.Fatalf("truncated=%v entries=%d, want true/%d", truncated, len(entries), codingWorkbenchBrowserMaxEntries)
	}
	if entries[0].Name != "file-000.txt" {
		t.Fatalf("first entry = %+v, want first sorted item in the bounded page", entries[0])
	}
}

func TestIsCodingWorkbenchHiddenBrowserName(t *testing.T) {
	for _, name := range []string{".maclaw-tmp", ".git", ".gitignore", ".."} {
		if !isCodingWorkbenchHiddenBrowserName(name) {
			t.Fatalf("%q must be hidden", name)
		}
	}
	for _, name := range []string{"build", "hello.cpp", "CMakeLists.txt", ""} {
		if isCodingWorkbenchHiddenBrowserName(name) {
			t.Fatalf("%q must stay visible", name)
		}
	}
}

func TestCollectCodingWorkbenchDirectoryEntriesSkipsHiddenNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".maclaw-tmp", ".git"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	entries, truncated, err := collectCodingWorkbenchDirectoryEntries(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("small directory must not be truncated")
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		if isCodingWorkbenchHiddenBrowserName(entry.Name) {
			t.Fatalf("hidden entry was listed: %+v", entry)
		}
		got = append(got, entry.Name)
	}
	if strings.Join(got, ",") != "build,hello.cpp" {
		t.Fatalf("entries = %v, want visible items only", got)
	}
}

func TestCopyCodingWorkbenchDownloadCopiesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.md")
	dest := filepath.Join(dir, "out.md")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCodingWorkbenchDownload(src, dest, 1024); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestCopyCodingWorkbenchDownloadEnforcesSizeLimit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	dest := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(src, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCodingWorkbenchDownload(src, dest, 2); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestTarCodingWorkbenchDownloadIncludesFilesAndSkipsMaclawCloud(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "docs", "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".maclaw-cloud"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".maclaw-cloud", "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "pack.tar")
	if err := tarCodingWorkbenchDownload(src, dest, "pack"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := tar.NewReader(f)
	names := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			names[hdr.Name] = string(body)
		} else {
			names[hdr.Name] = ""
		}
	}
	if names["pack/a.md"] != "hi" || names["pack/docs/b.txt"] != "b" {
		t.Fatalf("tar contents = %#v", names)
	}
	for name := range names {
		if strings.Contains(name, ".maclaw-cloud") {
			t.Fatalf("internal cache leaked into tar: %q", name)
		}
	}
}

func TestZipLatexSubmissionDirKeepsSourcesAndDropsBuildFiles(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.tex", "tex")
	write("main.pdf", "pdf")
	write("refs.bib", "bib")
	write("main.bbl", "bbl")
	write("fig/plot.png", "png")
	write("main.aux", "aux")
	write("main.log", "log")
	write("main.synctex.gz", "sync")
	write("main.tex.maclaw-bak", "bak")
	write(".maclaw-cloud/state.json", "{}")
	dest := filepath.Join(t.TempDir(), "paper.zip")
	if err := zipLatexSubmissionDir(src, dest, "paper"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zip.NewReader(f, mustZipSize(t, dest))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, item := range zr.File {
		rc, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		names[item.Name] = string(body)
	}
	for _, name := range []string{"paper/main.tex", "paper/main.pdf", "paper/refs.bib", "paper/main.bbl", "paper/fig/plot.png"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("missing %s in %#v", name, names)
		}
	}
	if names["paper/main.tex"] != "tex" || names["paper/fig/plot.png"] != "png" {
		t.Fatalf("contents = %#v", names)
	}
	for name := range names {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".aux") || strings.HasSuffix(lower, ".log") || strings.Contains(lower, "synctex") || strings.Contains(lower, "maclaw") {
			t.Fatalf("build file leaked into zip: %q", name)
		}
	}
}

func mustZipSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func TestExportLatexSubmissionZipUsesTaskWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	created := app.CreateExpertTask("builtin-latex-paper", "LaTeX")
	if created.ProjectPath == "" {
		t.Fatal("expert task was not created")
	}
	root := app.recentTaskExecutionProjectPath(created.ProjectPath)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte("tex"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.aux"), []byte("aux"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.zip")
	prev := codingWorkbenchSaveDialog
	codingWorkbenchSaveDialog = func(_ *App, title, defaultName string, _ []runtime.FileFilter) (string, error) {
		if title != "导出投稿包" || !strings.HasSuffix(defaultName, "-submission.zip") {
			t.Fatalf("dialog title=%q name=%q", title, defaultName)
		}
		return dest, nil
	}
	t.Cleanup(func() { codingWorkbenchSaveDialog = prev })
	got, err := app.ExportLatexSubmissionZip(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("saved path = %q, want %q", got, dest)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zip.NewReader(f, mustZipSize(t, dest))
	if err != nil {
		t.Fatal(err)
	}
	foundTex, foundAux := false, false
	for _, item := range zr.File {
		if strings.HasSuffix(item.Name, "/main.tex") {
			foundTex = true
		}
		if strings.HasSuffix(strings.ToLower(item.Name), ".aux") {
			foundAux = true
		}
	}
	if !foundTex || foundAux {
		t.Fatalf("tex=%v aux=%v", foundTex, foundAux)
	}
	codingWorkbenchSaveDialog = func(_ *App, _, _ string, _ []runtime.FileFilter) (string, error) {
		return "", nil
	}
	cancelled, err := app.ExportLatexSubmissionZip(created.ProjectPath)
	if err != nil || cancelled != "" {
		t.Fatalf("cancel = %q, %v", cancelled, err)
	}
}

func TestExportLatexSourceBundlePacksThePaperDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fig"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.tex", "tex")
	write("refs.bib", "bib")
	write("main.pdf", "pdf")
	write("main.aux", "aux")
	write("fig/plot.png", "png")
	pdf := filepath.Join(dir, "main.pdf")
	dest := filepath.Join(t.TempDir(), "out.zip")
	prev := codingWorkbenchSaveDialog
	codingWorkbenchSaveDialog = func(_ *App, title, defaultName string, _ []runtime.FileFilter) (string, error) {
		if title != "导出 LaTeX 源码包" || !strings.HasSuffix(defaultName, "-source.zip") {
			t.Fatalf("dialog title=%q name=%q", title, defaultName)
		}
		return dest, nil
	}
	t.Cleanup(func() { codingWorkbenchSaveDialog = prev })
	got, err := app.ExportLatexSourceBundle(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("saved = %q", got)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zip.NewReader(f, mustZipSize(t, dest))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range zr.File {
		names[item.Name] = true
	}
	for _, suffix := range []string{"/main.tex", "/refs.bib", "/main.pdf", "/fig/plot.png"} {
		found := false
		for name := range names {
			if strings.HasSuffix(name, suffix) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %#v", suffix, names)
		}
	}
	for name := range names {
		if strings.HasSuffix(strings.ToLower(name), ".aux") {
			t.Fatalf("build file leaked: %q", name)
		}
	}
	onlyPDF := t.TempDir()
	if err := os.WriteFile(filepath.Join(onlyPDF, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	codingWorkbenchSaveDialog = func(_ *App, _, _ string, _ []runtime.FileFilter) (string, error) {
		called = true
		return dest, nil
	}
	if _, err := app.ExportLatexSourceBundle(filepath.Join(onlyPDF, "report.pdf")); err == nil || !strings.Contains(err.Error(), "folder has no latex source") {
		t.Fatalf("pdf without sources: %v", err)
	}
	if called {
		t.Fatal("save dialog opened without a latex source")
	}
}

func TestExportLatexSourceBundleLiftsChapterFileToPaperRoot(t *testing.T) {
	app := newProjectSearchTestApp(t)
	parent := t.TempDir()
	paper := filepath.Join(parent, "paper")
	other := filepath.Join(parent, "other")
	if err := os.MkdirAll(filepath.Join(paper, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paper, "fig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(paper, "main.tex"), "main")
	write(filepath.Join(paper, "main.pdf"), "pdf")
	write(filepath.Join(paper, "fig", "plot.png"), "png")
	write(filepath.Join(paper, "chapters", "intro.tex"), "intro")
	write(filepath.Join(other, "notes.tex"), "notes")
	write(filepath.Join(other, "notes.pdf"), "pdf")
	dest := filepath.Join(t.TempDir(), "out.zip")
	prev := codingWorkbenchSaveDialog
	codingWorkbenchSaveDialog = func(_ *App, _, _ string, _ []runtime.FileFilter) (string, error) {
		return dest, nil
	}
	t.Cleanup(func() { codingWorkbenchSaveDialog = prev })
	if _, err := app.ExportLatexSourceBundle(filepath.Join(paper, "chapters", "intro.tex")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zip.NewReader(f, mustZipSize(t, dest))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range zr.File {
		names[item.Name] = true
	}
	for _, suffix := range []string{"/main.tex", "/fig/plot.png", "/chapters/intro.tex"} {
		found := false
		for name := range names {
			if strings.HasSuffix(name, suffix) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %#v", suffix, names)
		}
	}
	for name := range names {
		if strings.Contains(name, "notes") {
			t.Fatalf("neighboring project leaked: %q", name)
		}
	}
}

func TestExportLatexSourceBundleIgnoresChapterFigurePDF(t *testing.T) {
	app := newProjectSearchTestApp(t)
	parent := t.TempDir()
	paper := filepath.Join(parent, "paper")
	other := filepath.Join(parent, "other")
	if err := os.MkdirAll(filepath.Join(paper, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paper, "fig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(paper, "main.tex"), "\\documentclass{article}\n\\begin{document}\n\\input{chapters/intro}\n\\end{document}\n")
	write(filepath.Join(paper, "fig", "plot.png"), "png")
	write(filepath.Join(paper, "chapters", "intro.tex"), "% \\documentclass{article}\n本章说明导言区的 \\documentclass{article} 命令。\n")
	write(filepath.Join(paper, "chapters", "diagram.pdf"), "figure")
	write(filepath.Join(other, "notes.tex"), "\\documentclass{article}\n")
	write(filepath.Join(other, "notes.pdf"), "pdf")
	dest := filepath.Join(t.TempDir(), "out.zip")
	prev := codingWorkbenchSaveDialog
	codingWorkbenchSaveDialog = func(_ *App, _, _ string, _ []runtime.FileFilter) (string, error) {
		return dest, nil
	}
	t.Cleanup(func() { codingWorkbenchSaveDialog = prev })
	if _, err := app.ExportLatexSourceBundle(filepath.Join(paper, "chapters", "diagram.pdf")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zip.NewReader(f, mustZipSize(t, dest))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range zr.File {
		names[item.Name] = true
	}
	for _, suffix := range []string{"/main.tex", "/fig/plot.png", "/chapters/intro.tex", "/chapters/diagram.pdf"} {
		found := false
		for name := range names {
			if strings.HasSuffix(name, suffix) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %#v", suffix, names)
		}
	}
	for name := range names {
		if strings.Contains(name, "notes") {
			t.Fatalf("neighboring project leaked: %q", name)
		}
	}
}

func TestSanitizeCodingWorkbenchDownloadName(t *testing.T) {
	if got := sanitizeCodingWorkbenchDownloadName(`a/b:c`); got != "a_b_c" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeCodingWorkbenchDownloadName(".."); got != "download" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCodingWorkbenchRemoteDirectoryRecordsKeepsJSONBeforeSSHExitMarker(t *testing.T) {
	raw := strings.Join([]string{
		"$ python3 -c '<hidden>'",
		`{"truncated":true}`,
		`{"name":"src","is_dir":true}`,
		`{"name":"main.go","is_dir":false}`,
		"---EXIT_CODE:0---",
	}, "\n")
	entries, truncated := parseCodingWorkbenchRemoteDirectoryRecords(raw, "")
	if !truncated || len(entries) != 2 {
		t.Fatalf("truncated=%v entries=%v, want true and two entries", truncated, entries)
	}
	if entries[0].Name != "src" || !entries[0].IsDir || entries[1].Path != "main.go" {
		t.Fatalf("parsed entries = %+v", entries)
	}
}

func TestParseCodingWorkbenchRemoteDirectoryRecordsSkipsHiddenNames(t *testing.T) {
	raw := strings.Join([]string{
		`{"truncated":false}`,
		`{"name":".maclaw-tmp","is_dir":true}`,
		`{"name":".git","is_dir":true}`,
		`{"name":"hello.cpp","is_dir":false}`,
		`{"name":"build","is_dir":true}`,
	}, "\n")
	entries, truncated := parseCodingWorkbenchRemoteDirectoryRecords(raw, "")
	if truncated || len(entries) != 2 {
		t.Fatalf("truncated=%v entries=%v, want visible items only", truncated, entries)
	}
	if entries[0].Name != "build" || entries[1].Name != "hello.cpp" {
		t.Fatalf("parsed entries = %+v, want build then hello.cpp", entries)
	}
}

func TestRemotePreviewOutputIsTruncatedUsesProtocolMarkersOnly(t *testing.T) {
	if remotePreviewOutputIsTruncated("1\tconst truncated = false;\n") {
		t.Fatal("ordinary source content must not be treated as a truncated preview")
	}
	if !remotePreviewOutputIsTruncated("[remote read_file truncated: showing lines 1-2000; call again with offset=2001]") {
		t.Fatal("expected remote read marker to report truncation")
	}
}

func TestCodingWorkbenchVSCodeRemoteSnapshotPathIsTaskScoped(t *testing.T) {
	projectPath := "remote-task-01"
	digest := sha256.Sum256([]byte(projectPath))
	cacheRoot := filepath.Join(os.TempDir(), "maclaw-vscode", fmt.Sprintf("%x", digest[:]))
	localPath := filepath.Join(cacheRoot, "snapshots", "20260731T120000.000000000Z", filepath.FromSlash("src/main.go"))
	if !isPathInsideRoot(cacheRoot, localPath) {
		t.Fatalf("VS Code snapshot path %q must stay inside task cache root %q", localPath, cacheRoot)
	}
	if filepath.Base(localPath) != "main.go" {
		t.Fatalf("VS Code snapshot path %q must preserve the source filename", localPath)
	}
}

func TestCodingWorkbenchVSCodeRemoteDownloadLimit(t *testing.T) {
	if codingWorkbenchVSCodeRemoteMaxFileBytes < 1024*1024 {
		t.Fatalf("VS Code remote download limit %d is too small for source files", codingWorkbenchVSCodeRemoteMaxFileBytes)
	}
}

func TestCleanupCodingWorkbenchVSCodeRemoteSnapshotsKeepsRecentCopies(t *testing.T) {
	cacheRoot := t.TempDir()
	snapshotsRoot := filepath.Join(cacheRoot, "snapshots")
	oldSnapshot := filepath.Join(snapshotsRoot, "old")
	recentSnapshot := filepath.Join(snapshotsRoot, "recent")
	for _, snapshot := range []string{oldSnapshot, recentSnapshot} {
		if err := os.MkdirAll(snapshot, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	oldAt := now.Add(-codingWorkbenchVSCodeRemoteSnapshotRetention - time.Hour)
	if err := os.Chtimes(oldSnapshot, oldAt, oldAt); err != nil {
		t.Fatal(err)
	}
	cleanupCodingWorkbenchVSCodeRemoteSnapshots(cacheRoot, now)
	if _, err := os.Stat(oldSnapshot); !os.IsNotExist(err) {
		t.Fatalf("expired snapshot should be removed, stat error = %v", err)
	}
	if _, err := os.Stat(recentSnapshot); err != nil {
		t.Fatalf("recent snapshot should be retained: %v", err)
	}
}

func TestCodingWorkbenchLocalFileAbsPathUsesLocalCache(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "report.pdf")
	if err := os.WriteFile(file, []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := codingWorkbenchLocalFileAbsPath(root, "report.pdf")
	if err != nil {
		t.Fatalf("codingWorkbenchLocalFileAbsPath: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(file) {
		t.Fatalf("got %q, want %q", got, file)
	}
}

func TestCodingWorkbenchLocalFileAbsPathRejectsDirectoryAndEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := codingWorkbenchLocalFileAbsPath(root, "docs"); err == nil {
		t.Fatal("expected directory error")
	} else if !strings.Contains(err.Error(), "path is a directory") {
		t.Fatalf("directory error = %v", err)
	}
	if _, err := codingWorkbenchLocalFileAbsPath(root, "../secret.pdf"); err == nil {
		t.Fatal("expected path escape error")
	}
	if _, err := codingWorkbenchLocalFileAbsPath(root, ""); err == nil {
		t.Fatal("expected empty path error")
	}
	if _, err := codingWorkbenchLocalFileAbsPath(root, "missing.pdf"); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestCodingWorkbenchRejectsLocalOpenForRemoteTag(t *testing.T) {
	if (&App{}).codingWorkbenchRejectsLocalOpen("any") {
		t.Fatal("an App without a project index must not treat paths as remote")
	}
}

func TestOpenCodingWorkbenchFileLocallyRequiresProjectPath(t *testing.T) {
	app := &App{}
	if err := app.OpenCodingWorkbenchFileLocally("", "a.pdf"); err == nil {
		t.Fatal("expected project path error")
	}
}

func TestDeleteCodingWorkbenchEntryRequiresCloudWorkspace(t *testing.T) {
	app := &App{}
	if err := app.DeleteCodingWorkbenchEntry("", "notes.md"); err == nil || !strings.Contains(err.Error(), "project path is required") {
		t.Fatalf("empty project: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry("local-task", "notes.md"); err == nil || !strings.Contains(err.Error(), "only available for cloud workspaces") {
		t.Fatalf("non-cloud: %v", err)
	}
}

func TestDeleteCodingWorkbenchEntryRemovesLocalCacheAndRemoteFile(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_file")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "notes.md"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "gone.md"), []byte("drop"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, "cws_delete_file", prepared.LocalPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "gone.md"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "gone.md")); !os.IsNotExist(err) {
		t.Fatalf("local cache still has gone.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "notes.md")); err != nil {
		t.Fatalf("notes.md should remain: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == "gone.md" {
			t.Fatalf("remote still has gone.md: %+v", hub.entries)
		}
	}
	foundNotes := false
	for _, entry := range hub.entries {
		if entry.Path == "notes.md" {
			foundNotes = true
		}
	}
	if !foundNotes {
		t.Fatalf("remote lost notes.md: %+v", hub.entries)
	}
}

func TestDeleteCodingWorkbenchEntryRemovesDirectoryAndRemoteFiles(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prepared.LocalPath, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "docs", "a.md"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "keep.md"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, "cws_delete_dir", prepared.LocalPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "docs"); err != nil {
		t.Fatalf("delete dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "docs")); !os.IsNotExist(err) {
		t.Fatalf("local docs still present: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == "docs/a.md" || strings.HasPrefix(entry.Path, "docs/") {
			t.Fatalf("remote still has %q", entry.Path)
		}
	}
}

func TestDeleteCodingWorkbenchEntryRejectsProtectedAndEscapingPaths(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_guard")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prepared.LocalPath, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, cloudWorkspaceCacheStateDir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, ""); err == nil || !strings.Contains(err.Error(), "file path is required") {
		t.Fatalf("root: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, cloudWorkspaceCacheStateDir+"/state.json"); err == nil || !strings.Contains(err.Error(), "entry cannot be deleted") {
		t.Fatalf("protected: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "../secret.md"); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestCodingWorkbenchEntryProtected(t *testing.T) {
	if !codingWorkbenchEntryProtected(".maclaw-cloud") || !codingWorkbenchEntryProtected("docs/.maclaw-cloud/state.json") {
		t.Fatal("cloud cache internals must be protected")
	}
	if codingWorkbenchEntryProtected("notes.md") || codingWorkbenchEntryProtected("docs/report.pdf") {
		t.Fatal("ordinary files must be deletable")
	}
}

func TestCacheRelativeCloudWorkspacePath(t *testing.T) {
	root := t.TempDir()
	got, err := cacheRelativeCloudWorkspacePath(root, filepath.Join(root, "docs", "a.md"))
	if err != nil || got != "docs/a.md" {
		t.Fatalf("rel=%q err=%v", got, err)
	}
	if _, err := cacheRelativeCloudWorkspacePath(root, root); err == nil {
		t.Fatal("cache root itself must be rejected")
	}
	if _, err := cacheRelativeCloudWorkspacePath(root, filepath.Join(t.TempDir(), "outside.md")); err == nil {
		t.Fatal("path outside the cache must be rejected")
	}
}

func TestInferCloudWorkspaceDeleteDir(t *testing.T) {
	root := t.TempDir()
	if err := writeCloudWorkspaceState(root, cloudWorkspaceLocalState{
		LastEntries: []cloudWorkspaceManifestEntry{
			{Path: "docs/a.md"},
			{Path: "notes.md"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	isDir, err := inferCloudWorkspaceDeleteDir(root, "docs")
	if err != nil || !isDir {
		t.Fatalf("docs should infer as a folder: isDir=%v err=%v", isDir, err)
	}
	isDir, err = inferCloudWorkspaceDeleteDir(root, "notes.md")
	if err != nil || isDir {
		t.Fatalf("notes.md should infer as a file: isDir=%v err=%v", isDir, err)
	}
	isDir, err = inferCloudWorkspaceDeleteDir(root, "missing.md")
	if err != nil || isDir {
		t.Fatalf("unknown path should infer as a file: isDir=%v err=%v", isDir, err)
	}
}

func TestDeleteCodingWorkbenchEntryDeletesRemoteWhenLocalAlreadyGone(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_missing")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "gone.md"), []byte("drop"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, "cws_delete_missing", prepared.LocalPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	if err := os.Remove(filepath.Join(prepared.LocalPath, "gone.md")); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "gone.md"); err != nil {
		t.Fatalf("delete missing local: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == "gone.md" {
			t.Fatalf("remote still has gone.md: %+v", hub.entries)
		}
	}
}

func TestCollectCloudWorkspaceDeletePathsIncludesUntrackedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone.md"), []byte("drop"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := collectCloudWorkspaceDeletePaths(root, "gone.md", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "gone.md" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestListCloudWorkspaceFilesUnderWalksOnlyTheTargetFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "b.md"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := listCloudWorkspaceFilesUnder(root, "docs", true)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths, ",")
	if !strings.Contains(got, "docs/a.md") || !strings.Contains(got, "docs/nested/b.md") {
		t.Fatalf("paths=%v", paths)
	}
	for _, path := range paths {
		if path == "outside.md" {
			t.Fatalf("walked the whole workspace: %v", paths)
		}
	}
}

func TestDeleteCodingWorkbenchEntryKeepsAcceptedRemoteDeletesAfterLaterFailure(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_partial")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prepared.LocalPath, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "docs", "a.md"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "docs", "b.md"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, "cws_delete_partial", prepared.LocalPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	hub.mu.Lock()
	hub.failPush = true
	hub.mu.Unlock()
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "docs"); err == nil {
		t.Fatal("expected manifest replace to fail")
	}
	st, err := readCloudWorkspaceLocalState(prepared.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	hasA, hasB := false, false
	for _, entry := range st.LastEntries {
		if entry.Path == "docs/a.md" {
			hasA = true
		}
		if entry.Path == "docs/b.md" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("failed manifest replace must leave both baseline entries: %+v", st.LastEntries)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "docs", "a.md")); err != nil {
		t.Fatalf("local docs/a.md must stay: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "docs", "b.md")); err != nil {
		t.Fatalf("local docs/b.md must stay: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	remoteA := false
	for _, entry := range hub.entries {
		if entry.Path == "docs/a.md" {
			remoteA = true
		}
	}
	if !remoteA {
		t.Fatalf("failed manifest replace must leave the remote tree: %+v", hub.entries)
	}
}

func TestDeleteCodingWorkbenchEntryDoesNotPushUnrelatedLocalEdits(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_unrelated")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "gone.md"), []byte("drop"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, "cws_delete_unrelated", prepared.LocalPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "dirty.md"), []byte("local-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "gone.md"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "dirty.md")); err != nil {
		t.Fatalf("unrelated local edit was removed: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == "dirty.md" {
			t.Fatalf("delete pushed unrelated local edit: %+v", hub.entries)
		}
		if entry.Path == "gone.md" {
			t.Fatalf("remote still has gone.md: %+v", hub.entries)
		}
	}
}

func seedCloudWorkspaceFileForDelete(t *testing.T, app *App, hub *fakeCloudWorkspaceHub, workspaceID, localPath, name, body string) {
	t.Helper()
	rel := filepath.FromSlash(name)
	dest := filepath.Join(localPath, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := app.pushCloudWorkspace(ctx, workspaceID, localPath); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	want := filepath.ToSlash(rel)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == want {
			return
		}
	}
	t.Fatalf("seed did not upload %s: %+v", want, hub.entries)
}

func assertCloudWorkspaceFileDeleted(t *testing.T, hub *fakeCloudWorkspaceHub, localPath, name string) {
	t.Helper()
	rel := filepath.FromSlash(name)
	if _, err := os.Stat(filepath.Join(localPath, rel)); !os.IsNotExist(err) {
		t.Fatalf("local cache still has %s: %v", name, err)
	}
	want := filepath.ToSlash(rel)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, entry := range hub.entries {
		if entry.Path == want {
			t.Fatalf("remote still has %s: %+v", name, hub.entries)
		}
	}
}

func TestDeleteCodingWorkbenchEntryAllowsStolenReadOnlyMount(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_stolen")
	if err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceFileForDelete(t, app, hub, "cws_delete_stolen", prepared.LocalPath, "gone.md", "drop")
	mount := lookupHeldCloudWorkspace("cws_delete_stolen")
	if mount == nil {
		t.Fatal("missing mount")
	}
	applyCloudWorkspaceStolen(mount)
	if !mount.ReadOnly {
		t.Fatal("expected read-only")
	}
	if _, ok := heldWritableCloudWorkspacePath("cws_delete_stolen"); ok {
		t.Fatal("stolen mount must not look writable")
	}
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "gone.md"); err != nil {
		t.Fatalf("delete stolen cache: %v", err)
	}
	assertCloudWorkspaceFileDeleted(t, hub, prepared.LocalPath, "gone.md")
}

func TestDeleteCodingWorkbenchEntryAllowsUnheldCache(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_delete_unheld")
	seedCloudWorkspaceFileForDelete(t, app, hub, "cws_delete_unheld", created.WorkingDir, "gone.md", "drop")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.releaseCloudWorkspace(ctx, "cws_delete_unheld", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if lookupHeldCloudWorkspace("cws_delete_unheld") != nil {
		t.Fatal("expected no held mount after release")
	}
	if _, ok := heldWritableCloudWorkspacePath("cws_delete_unheld"); ok {
		t.Fatal("released cache must not look writable")
	}
	if err := app.DeleteCodingWorkbenchEntry(created.ProjectPath, "gone.md"); err != nil {
		t.Fatalf("delete unheld cache: %v", err)
	}
	assertCloudWorkspaceFileDeleted(t, hub, created.WorkingDir, "gone.md")
}

func TestCloudWorkspaceCacheRootFromPath(t *testing.T) {
	listing := filepath.Join("data", "cloud-workspaces", "tenant_acme", "cws_demo", "docs")
	want := normalizeProjectSessionPath(filepath.Join("data", "cloud-workspaces", "tenant_acme", "cws_demo"))
	if got := cloudWorkspaceCacheRootFromPath(listing); got != want {
		t.Fatalf("root=%q want %q", got, want)
	}
	nested := filepath.Join(listing, "a.md")
	if cloudWorkspaceIDFromPathString(nested) != "cws_demo" {
		t.Fatal("id from nested path")
	}
	if cloudWorkspaceCacheRootFromPath(filepath.Join("work", "app")) != "" {
		t.Fatal("non-cache path must be empty")
	}
}

func TestExplicitCloudWorkspaceListingDirKeepsReadOnlyRoot(t *testing.T) {
	readonly := filepath.Join("data", "cloud-workspaces-readonly", "tenant_acme", "cws_demo", "cwi_abc")
	if cloudWorkspaceIDFromPathString(readonly) != "" {
		t.Fatal("writer parser matched a read-only cache")
	}
	if cloudWorkspaceIDFromReadOnlyCachePath(readonly) != "cws_demo" {
		t.Fatalf("read-only id = %q", cloudWorkspaceIDFromReadOnlyCachePath(readonly))
	}
	dir, ok := explicitCloudWorkspaceListingDir(readonly)
	if !ok || dir != normalizeProjectSessionPath(readonly) {
		t.Fatalf("listing dir = %q ok=%v", dir, ok)
	}
	if _, ok := explicitCloudWorkspaceListingDir(filepath.Join(readonly, "docs")); ok {
		t.Fatal("nested read-only folder must not become the listing root")
	}
	writer := filepath.Join("data", "cloud-workspaces", "tenant_acme", "cws_demo")
	if _, ok := explicitCloudWorkspaceListingDir(writer); !ok {
		t.Fatal("writer cache root should list itself")
	}
	if _, ok := explicitCloudWorkspaceListingDir(filepath.Join(writer, "docs")); ok {
		t.Fatal("nested writer folder must stay on canonical resolution")
	}
}

func TestCorruptCloudWorkspaceListingFallsBackToDisk(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	cache := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_bad_listing"))
	if err := os.MkdirAll(filepath.Join(cache, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "notes.md"), []byte("on disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, cloudWorkspaceCacheStateDir, cloudWorkspaceListingFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := app.GetCodingWorkbenchDirectory(cache, "")
	if err != nil {
		t.Fatal(err)
	}
	if !codingWorkbenchEntriesContain(listed.Entries, "notes.md") {
		t.Fatalf("damaged browse index hid the disk file: %+v", listed.Entries)
	}
}

func TestCloudWorkspaceDirectoryMergesCaseVariantOnce(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	cache := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_case_listing"))
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "notes.md"), []byte("on disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("ab", 32)
	if err := writeCloudWorkspaceListing(cache, &cloudWorkspaceManifest{
		Revision: "rev-case",
		Entries:  []cloudWorkspaceManifestEntry{{Path: "Notes.md", SHA256: sum, Size: 7}},
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.GetCodingWorkbenchDirectory(cache, "")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range listed.Entries {
		if cloudWorkspacePortablePathKey(entry.Name) == cloudWorkspacePortablePathKey("notes.md") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("case variants of one file shown %d times: %+v", count, listed.Entries)
	}
}

func TestGetCodingWorkbenchDirectoryListsExplicitReadOnlyCache(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_browse_ro")
	if err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceFileForDelete(t, app, hub, "cws_browse_ro", prepared.LocalPath, "notes.md", "hello")
	writerList, err := app.GetCodingWorkbenchDirectory(prepared.LocalPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if !codingWorkbenchEntriesContain(writerList.Entries, "notes.md") {
		t.Fatalf("writer cache listing = %+v", writerList.Entries)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.releaseCloudWorkspace(ctx, "cws_browse_ro", false); err != nil {
		t.Fatal(err)
	}
	readonly, err := app.PrepareCloudWorkspaceReadOnly("cws_browse_ro")
	if err != nil {
		t.Fatal(err)
	}
	if readonly.LocalPath == "" || readonly.LocalPath == prepared.LocalPath {
		t.Fatalf("read-only cache = %q writer = %q", readonly.LocalPath, prepared.LocalPath)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "writer-only.md"), []byte("writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := app.GetCodingWorkbenchDirectory(readonly.LocalPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if !codingWorkbenchEntriesContain(listed.Entries, "notes.md") {
		t.Fatalf("read-only listing = %+v, want notes.md", listed.Entries)
	}
	if codingWorkbenchEntriesContain(listed.Entries, "writer-only.md") {
		t.Fatalf("read-only listing used the writer cache: %+v", listed.Entries)
	}
	if err := app.DeleteCodingWorkbenchEntry(readonly.LocalPath, "notes.md"); err != nil {
		t.Fatalf("delete read-only cache: %v", err)
	}
	assertCloudWorkspaceFileDeleted(t, hub, readonly.LocalPath, "notes.md")
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "writer-only.md")); err != nil {
		t.Fatalf("writer decoy must stay: %v", err)
	}
}

func codingWorkbenchEntriesContain(entries []CodingWorkbenchDirectoryEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func TestCloudWorkspaceDeleteCacheRootUsesListingSubdir(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	cache := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_subdir"))
	listing := filepath.Join(cache, "docs")
	if err := os.MkdirAll(filepath.Join(cache, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(listing, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := app.cloudWorkspaceDeleteCacheRoot("cws_subdir", listing)
	if err != nil {
		t.Fatal(err)
	}
	if got != cache {
		t.Fatalf("cache root=%q want %q", got, cache)
	}
	if found := cloudWorkspaceCacheRootFromStateDir(listing); found != cache {
		t.Fatalf("state walk=%q want %q", found, cache)
	}
}

func TestCloudWorkspaceDeleteCacheRootRejectsForeignWorkspace(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	own := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_own"))
	other := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_other"))
	listing := filepath.Join(other, "docs")
	if err := os.MkdirAll(filepath.Join(own, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(other, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(listing, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := app.cloudWorkspaceDeleteCacheRoot("cws_own", listing); err == nil {
		t.Fatal("listing from another workspace must not be used as the delete root")
	}
}

func TestDeleteCodingWorkbenchEntryLeavesLocalFileWhenRemoteFails(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_delete_remote_fail")
	if err != nil {
		t.Fatal(err)
	}
	seedCloudWorkspaceFileForDelete(t, app, hub, "cws_delete_remote_fail", prepared.LocalPath, "gone.md", "drop")
	hub.mu.Lock()
	hub.failPush = true
	hub.mu.Unlock()
	if err := app.DeleteCodingWorkbenchEntry(prepared.LocalPath, "gone.md"); err == nil {
		t.Fatal("expected remote delete to fail")
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "gone.md")); err != nil {
		t.Fatalf("local cache must stay until remote delete succeeds: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	found := false
	for _, entry := range hub.entries {
		if entry.Path == "gone.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed delete must leave the remote file: %+v", hub.entries)
	}
}

func TestDeleteCodingWorkbenchEntryRemovesNestedUnheldFile(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_delete_nested")
	seedCloudWorkspaceFileForDelete(t, app, hub, "cws_delete_nested", created.WorkingDir, "docs/gone.md", "drop")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.releaseCloudWorkspace(ctx, "cws_delete_nested", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := app.DeleteCodingWorkbenchEntry(created.ProjectPath, "docs/gone.md"); err != nil {
		t.Fatalf("delete nested: %v", err)
	}
	assertCloudWorkspaceFileDeleted(t, hub, created.WorkingDir, filepath.Join("docs", "gone.md"))
}

func TestCleanupCodingWorkbenchVSCodeRemoteSnapshotsKeepsFutureDatedCopies(t *testing.T) {
	cacheRoot := t.TempDir()
	snapshot := filepath.Join(cacheRoot, "snapshots", "future")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	future := now.Add(24 * time.Hour)
	if err := os.Chtimes(snapshot, future, future); err != nil {
		t.Fatal(err)
	}
	cleanupCodingWorkbenchVSCodeRemoteSnapshots(cacheRoot, now)
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("future-dated snapshot should not be removed: %v", err)
	}
}
