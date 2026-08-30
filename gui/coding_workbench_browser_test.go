package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
