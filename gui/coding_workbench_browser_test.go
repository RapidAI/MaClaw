package main

import (
	"crypto/sha256"
	"fmt"
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
