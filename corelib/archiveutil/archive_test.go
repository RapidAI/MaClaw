package archiveutil

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractZIPAndRejectExistingDestination(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"nested/hello.txt": "hello"})
	destination := filepath.Join(root, "out")
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if !got.OK || got.Files != 1 || got.Directories != 0 {
		t.Fatalf("Extract() = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("extracted content = %q, err=%v", data, err)
	}
	got = Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if got.OK || got.Code != CodeDestinationExists {
		t.Fatalf("existing destination result = %+v", got)
	}
}

func TestExtractToDirectoryUsesSharedSafeExtractor(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"nested/hello.txt": "hello"})
	destination := filepath.Join(root, "owned-temp")
	got := ExtractToDirectory(archivePath, destination, Limits{MaxFiles: 2, MaxTotalBytes: 32})
	if !got.OK || got.Files != 1 || got.OutputPath != destination {
		t.Fatalf("ExtractToDirectory() = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("extracted content = %q, err=%v", data, err)
	}
	if got := ExtractToDirectory(archivePath, destination, Limits{}); got.OK || got.Code != CodeDestinationExists {
		t.Fatalf("non-empty internal extraction destination = %+v", got)
	}
}

func TestExtractToDirectoryRejectsSymlinkAncestorBeforeCreatingOutput(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"hello.txt": "hello"})
	realParent := t.TempDir()
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(linkedParent, "new", "out")
	got := ExtractToDirectory(archivePath, destination, Limits{})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("symlink ancestor extraction = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(realParent, "new")); !os.IsNotExist(err) {
		t.Fatalf("extraction must not create through symlink ancestor, stat err=%v", err)
	}
}

func TestExtractZIPBytesToDirectoryUsesSharedSafeExtractor(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"nested/hello.txt": "hello"})
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "owned-temp")
	got := ExtractZIPBytesToDirectory(data, destination, Limits{MaxFiles: 2, MaxTotalBytes: 32}, ExtractionPolicy{})
	if !got.OK || got.Files != 1 || got.OutputPath != destination {
		t.Fatalf("ExtractZIPBytesToDirectory() = %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt")); err != nil || string(data) != "hello" {
		t.Fatalf("extracted content = %q, err=%v", data, err)
	}
	if got := ExtractZIPBytesToDirectory(data, destination, Limits{}, ExtractionPolicy{}); got.OK || got.Code != CodeDestinationExists {
		t.Fatalf("non-empty internal byte extraction destination = %+v", got)
	}
}

func TestExtractToDirectoryFilterStillValidatesSkippedEntries(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "filtered.zip")
	writeZIPFixture(t, archivePath, map[string]string{"skip.exe": "x", "safe.txt": "ok"})
	destination := filepath.Join(root, "out")
	got := ExtractToDirectoryWithPolicy(archivePath, destination, Limits{}, ExtractionPolicy{Filter: func(entry Entry) (bool, error) {
		return filepath.Ext(entry.Path) != ".exe", nil
	}})
	if !got.OK || got.Files != 1 || got.WrittenBytes != 2 {
		t.Fatalf("filtered extraction = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(destination, "skip.exe")); !os.IsNotExist(err) {
		t.Fatalf("filtered entry was written, err=%v", err)
	}

	unsafePath := filepath.Join(root, "unsafe.zip")
	writeZIPFixture(t, unsafePath, map[string]string{"../skip.exe": "x"})
	got = ExtractToDirectoryWithPolicy(unsafePath, filepath.Join(root, "unsafe-out"), Limits{}, ExtractionPolicy{Filter: func(Entry) (bool, error) { return false, nil }})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("filtered unsafe extraction = %+v", got)
	}
}

func TestExtractZIPRejectsCaseInsensitiveDuplicates(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "duplicates.zip")
	writeZIPFixture(t, archivePath, map[string]string{"readme.txt": "a", "README.TXT": "b"})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("case-collision result = %+v", got)
	}
}

func TestExtractZIPRejectsPathTraversalAndLeavesNoDestination(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "unsafe.zip")
	writeZIPFixture(t, archivePath, map[string]string{"../escape.txt": "no"})
	destination := filepath.Join(root, "out")
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("unsafe extraction result = %+v", got)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be published, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("traversal file was written, stat err=%v", err)
	}
}

func TestExtractZIPRejectsWindowsReservedPathSegments(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"assets/NUL.txt", "assets/file. ", "logs/COM1.txt"} {
		archivePath := filepath.Join(root, strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), " ", "_")+".zip")
		writeZIPFixture(t, archivePath, map[string]string{name: "x"})
		got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: archivePath + ".out"})
		if got.OK || got.Code != CodeUnsafeEntry {
			t.Fatalf("reserved path %q result = %+v", name, got)
		}
	}
}

func TestExtractRejectsArchiveLinksByDefault(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "link.zip")
	writeZIPWithSymlink(t, zipPath, "link", "target")
	if got := Extract(Request{Action: ActionExtract, ArchivePath: zipPath, Destination: filepath.Join(root, "zip-out")}); got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("ZIP link result = %+v", got)
	}

	tarPath := filepath.Join(root, "link.tar")
	writeTarFixture(t, tarPath, []tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "target"}}, nil)
	if got := Extract(Request{Action: ActionExtract, ArchivePath: tarPath, Destination: filepath.Join(root, "tar-out")}); got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("TAR link result = %+v", got)
	}
}

func TestTrustedTarSymlinkPolicyCannotBecomeFileParent(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	tarPath := filepath.Join(root, "links.tar")
	writeTarFixture(t, tarPath, []tar.Header{
		{Name: "linked", Typeflag: tar.TypeSymlink, Linkname: "safe"},
		{Name: "linked/escape.txt", Typeflag: tar.TypeReg, Size: 1},
	}, []string{"", "x"})
	got := ExtractToDirectoryWithPolicy(tarPath, filepath.Join(root, "out"), Limits{}, ExtractionPolicy{AllowSymlinks: true})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("link-parent policy result = %+v", got)
	}
}

func TestExtractTarGzip(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.tar.gz")
	writeTarGzipFixture(t, archivePath, "folder/file.txt", "tar-data")
	destination := filepath.Join(root, "out")
	got := Run(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if !got.OK || got.Format != FormatTarGZIP || got.Files != 1 {
		t.Fatalf("Run(extract tar.gz) = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(destination, "folder", "file.txt"))
	if err != nil || string(data) != "tar-data" {
		t.Fatalf("tar output = %q, err=%v", data, err)
	}
}

func TestInspectTarGzipListsEntries(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.tar.gz")
	writeTarGzipFixture(t, archivePath, "folder/file.txt", "tar-data")
	got := Inspect(Request{Action: ActionInspect, ArchivePath: archivePath})
	if !got.OK || got.Format != FormatTarGZIP || got.Files != 1 || len(got.Entries) != 1 || got.Entries[0].Path != "folder/file.txt" {
		t.Fatalf("Inspect(tar.gz) = %+v", got)
	}
}

func TestExtractGzipAndBzip2SingleStreams(t *testing.T) {
	root := t.TempDir()
	gzipPath := filepath.Join(root, "note.gz")
	gzipFile, err := os.Create(gzipPath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(gzipFile)
	if _, err := gzipWriter.Write([]byte("gzip-data")); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipFile.Close(); err != nil {
		t.Fatal(err)
	}
	if got := Extract(Request{Action: ActionExtract, ArchivePath: gzipPath, Destination: filepath.Join(root, "gzip-out")}); !got.OK || got.Format != FormatGZIP {
		t.Fatalf("gzip extraction = %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(root, "gzip-out", "note")); err != nil || string(data) != "gzip-data" {
		t.Fatalf("gzip content = %q, err=%v", data, err)
	}

	// A compact fixed bzip2 fixture generated for the plain text "hello world".
	bzipPath := filepath.Join(root, "note.bz2")
	bzipData := []byte{0x42, 0x5a, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26, 0x53, 0x59, 0x44, 0xf7, 0x13, 0x78, 0x00, 0x00, 0x01, 0x91, 0x80, 0x40, 0x00, 0x06, 0x44, 0x90, 0x80, 0x20, 0x00, 0x22, 0x03, 0x34, 0x84, 0x30, 0x21, 0xb6, 0x81, 0x54, 0x27, 0x8b, 0xb9, 0x22, 0x9c, 0x28, 0x48, 0x22, 0x7b, 0x89, 0xbc, 0x00}
	if err := os.WriteFile(bzipPath, bzipData, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Extract(Request{Action: ActionExtract, ArchivePath: bzipPath, Destination: filepath.Join(root, "bzip-out")}); !got.OK || got.Format != FormatBZIP2 {
		t.Fatalf("bzip2 extraction = %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(root, "bzip-out", "note")); err != nil || string(data) != "hello world" {
		t.Fatalf("bzip2 content = %q, err=%v", data, err)
	}
}

func TestExtractTarBzip2FixedFixture(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.tar.bz2")
	// Produced once with Python's tarfile + bz2 modules; it contains
	// folder/file.txt with the body "tarbz2-data". Keeping it fixed verifies
	// the pure-Go BZIP2/TAR reader rather than a helper that shares production
	// code paths.
	data, err := base64.StdEncoding.DecodeString("QlpoOTFBWSZTWYSP3FEAAHR7gMqAAIBAA/kAIAB3JJ5QCAggAFRCnlPUA0D9UNDTaQSUR6IANAAH3VY9CCU6EIu5aJhVNBAhwOIaxtZJpPEBTILoz1srIWpxwHpL1z5KSRFU3R6TfagMzIvxdyRThQkISP3FEA==")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(Request{Action: ActionInspect, ArchivePath: archivePath}); !got.OK || got.Format != FormatTarBZ2 || got.Files != 1 || len(got.Entries) != 1 || got.Entries[0].Path != "folder/file.txt" {
		t.Fatalf("Inspect(tar.bz2) = %+v", got)
	}
	destination := filepath.Join(root, "out")
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if !got.OK || got.Format != FormatTarBZ2 || got.Files != 1 {
		t.Fatalf("Extract(tar.bz2) = %+v", got)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "folder", "file.txt")); err != nil || string(body) != "tarbz2-data" {
		t.Fatalf("tar.bz2 content = %q, err=%v", body, err)
	}
}

func TestInspectZipIsBounded(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"a.txt": "a", "b.txt": "b"})
	got := Inspect(Request{Action: ActionInspect, ArchivePath: archivePath, Limits: Limits{MaxListedEntries: 1}})
	if !got.OK || got.Format != FormatZIP || len(got.Entries) != 1 || !got.Truncated || got.Files != 2 {
		t.Fatalf("Inspect() = %+v", got)
	}
}

func TestCreateZIPPreservesBaseRelativePaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "reports", "daily.txt")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("daily"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "deliverables.zip")
	got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{source}, OutputPath: output})
	if !got.OK || got.Files != 1 {
		t.Fatalf("CreateZIP() = %+v", got)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "reports/daily.txt" {
		t.Fatalf("zip entries = %#v", zr.File)
	}
}

func TestCreateZIPRejectsSourceOutsideBase(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	source := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(source, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{source}, OutputPath: filepath.Join(root, "out.zip")})
	if got.OK || got.Code != CodeInvalidArgument {
		t.Fatalf("outside source result = %+v", got)
	}
}

func TestP0OnlyAcceptsFailConflictAndPreserveRootMode(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"file.txt": "ok"})
	if got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out"), ConflictPolicy: "overwrite"}); got.OK || got.Code != CodeInvalidArgument {
		t.Fatalf("extract overwrite policy = %+v", got)
	}
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{source}, OutputPath: filepath.Join(root, "out.zip"), RootMode: "flatten"}); got.OK || got.Code != CodeInvalidArgument {
		t.Fatalf("create_zip flatten root mode = %+v", got)
	}
}

func TestCreateZIPCanArchiveTheWholeBaseDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("root"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "whole.zip")
	got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{root}, OutputPath: output})
	if !got.OK || got.Files != 1 {
		t.Fatalf("CreateZIP() = %+v", got)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "root.txt" {
		t.Fatalf("zip entries = %#v", zr.File)
	}
}

func TestCreateZIPLimitsDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{root}, OutputPath: filepath.Join(root, "out.zip"), Limits: Limits{MaxFiles: 2}})
	if got.OK || got.Code != CodeLimitExceeded {
		t.Fatalf("create ZIP directory-entry limit = %+v", got)
	}
}

func writeZIPFixture(t *testing.T, target string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZIPWithSymlink(t *testing.T, target, name, linkTarget string) {
	t.Helper()
	f, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(linkTarget)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTarFixture(t *testing.T, target string, headers []tar.Header, bodies []string) {
	t.Helper()
	f, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for i := range headers {
		if err := tw.WriteHeader(&headers[i]); err != nil {
			t.Fatal(err)
		}
		if i < len(bodies) && bodies[i] != "" {
			if _, err := tw.Write([]byte(bodies[i])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTarGzipFixture(t *testing.T, target, name, body string) {
	t.Helper()
	f, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDetectTarFromMagic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: "a", Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(path)
	if err != nil || got != FormatTAR {
		t.Fatalf("Detect() = %q, %v", got, err)
	}
}

func TestDetectUsesContentForMisnamedGzip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plain.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte("not a tar stream")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(path)
	if err != nil || got != FormatGZIP {
		t.Fatalf("Detect(misnamed gzip) = %q, %v", got, err)
	}
}

func TestExtractZIPRejectsFileParentConflict(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "conflict.zip")
	writeZIPFixture(t, archivePath, map[string]string{"a": "file", "a/b.txt": "nested"})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("file parent conflict = %+v", got)
	}
}

func TestExtractZIPRejectsFileAfterChildEntry(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "reverse-conflict.zip")
	writeZIPFixture(t, archivePath, map[string]string{"a/b.txt": "nested", "a": "file"})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("reverse file parent conflict = %+v", got)
	}
}

func TestExtractZIPEnforcesActualSizeLimit(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "large.zip")
	writeZIPFixture(t, archivePath, map[string]string{"large.txt": "12345"})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out"), Limits: Limits{MaxFileBytes: 4}})
	if got.OK || got.Code != CodeLimitExceeded {
		t.Fatalf("size limit result = %+v", got)
	}
}

func TestExtractZIPRejectsCRCFailureAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "bad-crc.zip")
	writeZIPFixture(t, archivePath, map[string]string{"file.txt": "content"})
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the central-directory CRC. archive/zip reads this value as the
	// expected checksum and must reject the entry when its reader closes.
	central := bytes.LastIndex(data, []byte("PK\x01\x02"))
	if central < 0 || len(data) < central+20 {
		t.Fatal("fixture ZIP is unexpectedly short")
	}
	data[central+16] ^= 0xff
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "out")
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: destination})
	if got.OK || got.Code != CodeCorruptArchive {
		t.Fatalf("CRC failure result = %+v", got)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("CRC failure must not publish destination, stat err=%v", err)
	}
}

func TestExtractZIPRejectsTruncatedArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "truncated.zip")
	writeZIPFixture(t, archivePath, map[string]string{"file.txt": "content"})
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, data[:len(data)-8], 0o600); err != nil {
		t.Fatal(err)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeCorruptArchive {
		t.Fatalf("truncated ZIP result = %+v", got)
	}
}

func TestExtractRejectsCorruptCompressedStream(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "corrupt.gz")
	if err := os.WriteFile(archivePath, []byte{0x1f, 0x8b, 0x08, 0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeCorruptArchive {
		t.Fatalf("corrupt compressed stream result = %+v", got)
	}
}

func TestExtractZIPRejectsExcessiveDepth(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "deep.zip")
	writeZIPFixture(t, archivePath, map[string]string{"one/two/three/file.txt": "x"})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out"), Limits: Limits{MaxDirectoryDepth: 3}})
	if got.OK || got.Code != CodeLimitExceeded {
		t.Fatalf("depth limit result = %+v", got)
	}
}

func TestExtractZIPLimitsDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "many-directories.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"a/", "b/", "c/"} {
		if _, err := zw.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out"), Limits: Limits{MaxFiles: 2}})
	if got.OK || got.Code != CodeLimitExceeded {
		t.Fatalf("directory-entry limit result = %+v", got)
	}
}

func TestCreateZIPAllowsZIP64EntryMetadata(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "zip64.zip")
	f, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	// SetOffset records ZIP64-compatible offsets. The backing file is sparse:
	// this consumes only the small archive payload on filesystems that support
	// sparse holes while still making archive/zip parse a real ZIP64 directory.
	if _, err := f.Seek(1<<32, 0); err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	zw.SetOffset(1 << 32)
	w, err := zw.Create("zip64.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("zip64")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: output, Destination: filepath.Join(root, "out"), Limits: Limits{MaxInputBytes: (5 << 30)}})
	if !got.OK || got.Format != FormatZIP || got.Files != 1 {
		t.Fatalf("ZIP64 extraction result = %+v", got)
	}
	if body, err := os.ReadFile(filepath.Join(root, "out", "zip64.txt")); err != nil || string(body) != "zip64" {
		t.Fatalf("ZIP64 content = %q, err=%v", body, err)
	}
}

func TestEnsureSourceUnchangedDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := captureSourceState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = ensureSourceUnchanged(path, before)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeSourceChanged {
		t.Fatalf("source replacement result = %v", err)
	}
}

func TestExtractZIPCompressionRatioOnlyWarns(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "compressible.zip")
	writeZIPFixture(t, archivePath, map[string]string{"zeros.txt": string(bytes.Repeat([]byte{'0'}, 16<<10))})
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(root, "out"), Limits: Limits{MaxCompressionRatio: 2}})
	if !got.OK {
		t.Fatalf("compression-ratio extraction = %+v", got)
	}
	if len(got.Warnings) == 0 || got.Warnings[0] != "archive expansion ratio exceeds warning threshold" {
		t.Fatalf("compression-ratio warnings = %#v", got.Warnings)
	}
}

func TestExtractZIPRejectsSymlinkedOutputParent(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.zip")
	writeZIPFixture(t, archivePath, map[string]string{"ok.txt": "ok"})
	realParent := t.TempDir()
	linkedParent := filepath.Join(root, "link")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: filepath.Join(linkedParent, "out")})
	if got.OK || got.Code != CodeUnsafeEntry {
		t.Fatalf("symlink output parent result = %+v", got)
	}
}

func TestInspectUnsupportedFormatReturnsFallback(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Inspect(Request{Action: ActionInspect, ArchivePath: archivePath})
	if got.OK || (got.Code != CodeExternalFallbackRequired && got.Code != CodeExternalToolNotFound) || got.Format != Format7Z || got.Fallback == nil || !got.Fallback.CraftToolAllowed {
		t.Fatalf("unsupported inspect = %+v", got)
	}
}

func TestExternalStageValidatesAndPublishes(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "published")
	stage, err := PrepareExternalStage(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	if err := os.WriteFile(filepath.Join(stage.Path, "result.txt"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage, files, _, size, err := stage.Validate(Limits{})
	if err != nil || files != 1 || size != int64(len("external")) {
		t.Fatalf("external validation = files:%d size:%d err:%v", files, size, err)
	}
	if err := stage.Publish(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "result.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareMergeStageAllowsExistingDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "existing")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, err := PrepareMergeStage(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if filepath.Dir(stage) != root {
		t.Fatalf("merge stage parent = %q, want %q", filepath.Dir(stage), root)
	}
}

func TestPrepareMergeStageRejectsSymlinkedDestinationParent(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	outside := t.TempDir()
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkedParent); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareMergeStage(filepath.Join(linkedParent, "destination"))
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("symlinked merge parent error = %v", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("merge staging wrote through symlink parent: entries=%v, err=%v", entries, readErr)
	}
}

func TestExternalStagePublishRejectsConcurrentDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "published")
	stage, err := PrepareExternalStage(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	stage, _, _, _, err = stage.Validate(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	err = stage.Publish()
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeDestinationExists {
		t.Fatalf("concurrent destination publish error = %v", err)
	}
	if _, statErr := os.Stat(stage.Path); statErr != nil {
		t.Fatalf("collision must retain staging directory, stat err=%v", statErr)
	}
}

func TestExternalStagePublishRequiresAndHonorsValidation(t *testing.T) {
	root := t.TempDir()
	stage, err := PrepareExternalStage(filepath.Join(root, "published"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	if err := os.WriteFile(filepath.Join(stage.Path, "result.txt"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stage.Publish(); err == nil {
		t.Fatal("publishing without validation must fail")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CodeInvalidArgument {
		t.Fatalf("unvalidated publish error = %v", err)
	}
	stage, _, _, _, err = stage.Validate(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage.Path, "late.txt"), []byte("late"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = stage.Publish()
	if typed, ok := err.(*Error); !ok || typed.Code != CodeSourceChanged {
		t.Fatalf("mutated stage publish error = %v", err)
	}
}

func TestExternalStageRejectsSymlinkOutput(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	stage, err := PrepareExternalStage(filepath.Join(root, "published"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(stage.Path, "link")); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = ValidateExtractedDirectory(stage.Path, Limits{})
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("symlink validation err = %v", err)
	}
}

func TestValidateExtractedDirectoryRejectsInvalidRoot(t *testing.T) {
	if _, _, _, err := ValidateExtractedDirectory("", Limits{}); err == nil {
		t.Fatal("empty external output root must be rejected")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CodeInvalidArgument {
		t.Fatalf("empty external output root error = %v", err)
	}
	file := filepath.Join(t.TempDir(), "output-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := ValidateExtractedDirectory(file, Limits{})
	if typed, ok := err.(*Error); !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("file external output root error = %v", err)
	}
}

func TestValidateExtractedDirectoryRejectsSymlinkRoot(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	realRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(realRoot, "result.txt"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(t.TempDir(), "linked-output")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := ValidateExtractedDirectory(linkedRoot, Limits{})
	if typed, ok := err.(*Error); !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("symlink external output root error = %v", err)
	}
}

func TestExternalStageLimitsDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	stage, err := PrepareExternalStage(filepath.Join(root, "published"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(stage.Path, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, err = ValidateExtractedDirectory(stage.Path, Limits{MaxFiles: 2})
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeLimitExceeded {
		t.Fatalf("external directory-entry limit err = %v", err)
	}
}

func TestCreateZIPCreatesMissingOutputParent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "new", "nested", "bundle.zip")
	got := CreateZIP(Request{Action: ActionCreateZIP, BasePath: root, SourcePaths: []string{source}, OutputPath: output})
	if !got.OK {
		t.Fatalf("CreateZIP() = %+v", got)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}

func TestPublishNewFileRejectsExistingOutput(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, ".archive-create-temp.zip")
	destination := filepath.Join(root, "bundle.zip")
	if err := os.WriteFile(stage, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := publishNewFile(stage, destination)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeDestinationExists {
		t.Fatalf("existing output publish error = %v", err)
	}
	if body, readErr := os.ReadFile(destination); readErr != nil || string(body) != "old" {
		t.Fatalf("existing output was modified: %q, err=%v", body, readErr)
	}
}

func TestPublishNewFileRejectsSymlinkedOutputParent(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	realParent := t.TempDir()
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, ".archive-create-temp.zip")
	if err := os.WriteFile(stage, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := publishNewFile(stage, filepath.Join(linkedParent, "bundle.zip"))
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("symlink output parent publish error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(realParent, "bundle.zip")); !os.IsNotExist(statErr) {
		t.Fatalf("publish must not write through symlink parent, stat err=%v", statErr)
	}
}

func TestMergeValidatedDirectoryMergesFilesWithoutFollowingDestinationLink(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	source := filepath.Join(root, "stage")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "existing.txt"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "existing.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MergeValidatedDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "nested", "new.txt")); err != nil || string(data) != "new" {
		t.Fatalf("merged content = %q, err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "existing.txt")); err != nil || string(data) != "replacement" {
		t.Fatalf("existing content = %q, err=%v", data, err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "linked"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "linked", "escape.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := MergeValidatedDirectory(source, destination)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("merge through destination link error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("merge wrote through destination link, stat err=%v", statErr)
	}
}

func TestMergeValidatedDirectoryReplacesExistingRegularFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "stage")
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skill.md"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "skill.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MergeValidatedDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "skill.md"))
	if err != nil || string(data) != "new" {
		t.Fatalf("merged replacement = %q, err=%v", data, err)
	}
}

func TestReplaceValidatedTopLevelDirectoriesReplacesDirectoryAndMergesFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "stage")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "skill", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skill", "nested", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "metadata.json"), []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destination, "skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "skill", "stale.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "metadata.json"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceValidatedTopLevelDirectories(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "skill", "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale replaced file still exists, stat err=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "skill", "nested", "new.txt")); err != nil || string(data) != "new" {
		t.Fatalf("new replacement content = %q, err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "metadata.json")); err != nil || string(data) != "fresh" {
		t.Fatalf("merged metadata = %q, err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "keep.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated file = %q, err=%v", data, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(destination, ".archive-replace-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("replacement transaction left staging behind: %v, err=%v", leftovers, err)
	}
}

func TestReplaceValidatedTopLevelDirectoriesRejectsDestinationLink(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	source := filepath.Join(root, "stage")
	destination := filepath.Join(root, "destination")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skill", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "skill")); err != nil {
		t.Fatal(err)
	}
	err := ReplaceValidatedTopLevelDirectories(source, destination)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("replacement through destination link error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement wrote through destination link, stat err=%v", statErr)
	}
}

func TestReplaceValidatedTopLevelDirectoriesPreflightsSourceBeforeDeletingDestination(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("creating symlinks requires elevated privileges on some Windows hosts")
	}
	root := t.TempDir()
	source := filepath.Join(root, "stage")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skill", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(source, "invalid-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destination, "skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "skill", "stale.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ReplaceValidatedTopLevelDirectories(source, destination)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeUnsafeEntry {
		t.Fatalf("invalid staged source error = %v", err)
	}
	if data, readErr := os.ReadFile(filepath.Join(destination, "skill", "stale.txt")); readErr != nil || string(data) != "old" {
		t.Fatalf("destination was modified before preflight failed: %q, err=%v", data, readErr)
	}
}

func TestMergeValidatedDirectoryRejectsOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "stage"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := MergeValidatedDirectory(filepath.Join(root, "stage"), filepath.Join(root, "stage", "nested"))
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeInvalidArgument {
		t.Fatalf("overlapping merge roots error = %v", err)
	}
}

func TestExtractExternalRequiresExplicitApproval(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExtractExternal(Request{Action: ActionExtractExternal, ArchivePath: archivePath, Destination: filepath.Join(root, "out")})
	if got.OK || got.Code != CodeExternalApprovalRequired {
		t.Fatalf("unapproved external result = %+v", got)
	}
}

func TestExtractExternalSuccessPublishesValidatedStaging(t *testing.T) {
	program := testExternalCommand(t, `
if [ "$1" = "--version" ] || [ "$1" = "-h" ]; then echo "fake7z 1.0"; exit 0; fi
[ "$1" = "x" ] && [ "$2" = "-y" ] || exit 31
case "$3" in -o*) stage="${3#-o}" ;; *) exit 32 ;; esac
mkdir -p "$stage"
printf 'external-data' > "$stage/result.txt"
`)
	t.Setenv("PATH", filepath.Dir(program)+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "out")
	got := ExtractExternal(Request{Action: ActionExtractExternal, ArchivePath: archivePath, Destination: destination, AllowExternal: true, Limits: Limits{MaxTotalBytes: 1024}})
	if !got.OK || got.Format != Format7Z || got.Files != 1 || got.OutputPath != destination {
		t.Fatalf("external extraction result = %+v", got)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "result.txt")); err != nil || string(body) != "external-data" {
		t.Fatalf("external result content = %q, err=%v", body, err)
	}
}

func TestExtractExternalRejectsOversizedOutputAndCleansStaging(t *testing.T) {
	program := testExternalCommand(t, `
if [ "$1" = "--version" ] || [ "$1" = "-h" ]; then echo "fake7z 1.0"; exit 0; fi
stage="${3#-o}"
mkdir -p "$stage"
printf 'too-large' > "$stage/result.txt"
`)
	t.Setenv("PATH", filepath.Dir(program)+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "out")
	got := ExtractExternal(Request{Action: ActionExtractExternal, ArchivePath: archivePath, Destination: destination, AllowExternal: true, Limits: Limits{MaxFileBytes: 2, MaxTotalBytes: 16}})
	if got.OK || got.Code != CodeLimitExceeded {
		t.Fatalf("oversized external output result = %+v", got)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid external output must not publish destination, stat err=%v", err)
	}
}

func TestExternalProgramSelfCheckFailureIsExplicit(t *testing.T) {
	program := testExternalCommand(t, `exit 1`)
	_, err := inspectExternalProgram(program, Format7Z)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeExternalToolUnusable {
		t.Fatalf("self-check failure = %v", err)
	}
}

func TestExtractExternalRejectsExistingDestinationBeforeProgramRuns(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "out")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	got := ExtractExternal(Request{Action: ActionExtractExternal, ArchivePath: archivePath, Destination: destination, AllowExternal: true, Limits: Limits{MaxTotalBytes: 1024}})
	if got.OK || got.Code != CodeDestinationExists {
		t.Fatalf("existing external destination result = %+v", got)
	}
}

func TestExtractExternalWaitsForValidationBeforePublishing(t *testing.T) {
	program := testExternalCommand(t, `
if [ "$1" = "--version" ] || [ "$1" = "-h" ]; then echo "fake7z 1.0"; exit 0; fi
stage="${3#-o}"
mkdir -p "$stage"
printf 'external-data' > "$stage/result.txt"
sleep 1
`)
	t.Setenv("PATH", filepath.Dir(program)+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	archivePath := filepath.Join(root, "fixture.7z")
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "out")
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- ExtractExternal(Request{Action: ActionExtractExternal, ArchivePath: archivePath, Destination: destination, AllowExternal: true, Limits: Limits{MaxTotalBytes: 1024}})
	}()

	// The fake program creates its result before this write. Altering the
	// archive while extraction is in-flight must reject the transaction instead
	// of publishing the otherwise valid staging directory.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(archivePath, []byte("\x37\x7a\xbc\xaf\x27\x1c\x00\x01"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := <-resultCh
	if got.OK || got.Code != CodeSourceChanged {
		t.Fatalf("source change during external extraction = %+v", got)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("changed source must not publish destination, stat err=%v", err)
	}
}

func TestDetectRARMultiVolumeRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.part1.rar")
	if err := os.WriteFile(path, []byte("Rar!\x1a\x07\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Detect(path)
	typed, ok := err.(*Error)
	if !ok || typed.Code != CodeMultiVolumeUnsupported {
		t.Fatalf("Detect multi-volume RAR error = %v", err)
	}
}

func TestExtractRARSingleVolumeFixture(t *testing.T) {
	// Fixed single-volume, non-encrypted RAR5 fixture. Its small payload is
	// embedded so CI does not depend on a user's tools, files, or module cache.
	data, err := base64.StdEncoding.DecodeString("UmFyIRoHAQAzkrXlCgEFBgAFAQGAgABGzTVJHAICnQEGuwG0gwKAAPNateoMI4ADAQZhc2QuZ2/FBZomVENC9mBE3ZOFQmqQFk3oOpdKCPBUtmRmQWS8HJHNCPelLkzdnFrsv8eqq5PZdHq0VRQffcfxBvgHuPEISMaEcR9TOk1yJwi5Bq4PhPCnZbRiy8PbmvwY8duWLs2WsQlEHekm6N6VH+El5Fuw/U+7eezC+YheVNAtBI5HKV8AkzgXAckVVuswvjQpidDdBz/lkCf1StT9VP6YHXdWUQMFBAA=")
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "fixture.rar")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	inspect := Inspect(Request{Action: ActionInspect, ArchivePath: archivePath})
	if !inspect.OK || inspect.Format != FormatRAR || inspect.Files == 0 {
		t.Fatalf("Inspect(RAR) = %+v", inspect)
	}
	got := Extract(Request{Action: ActionExtract, ArchivePath: archivePath, Destination: archivePath + ".out", Limits: Limits{MaxFileBytes: 64 << 20, MaxTotalBytes: 128 << 20}})
	if !got.OK || got.Format != FormatRAR || got.Files == 0 {
		t.Fatalf("Extract(RAR) = %+v", got)
	}
}

func TestInspectUnsupportedWithoutExternalAdapterIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.iso")
	if err := os.WriteFile(path, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The public detector intentionally does not guess ISO from a suffix, so
	// verify the no-adapter contract directly rather than widening detection.
	got := externalFallback(FormatUnknown)
	if got.Code != CodeFormatUnsupported {
		t.Fatalf("fallback without adapter = %+v", got)
	}
}

func TestExternalCommandUsesArgumentArrayAndRejectsFailure(t *testing.T) {
	program := testExternalCommand(t, `
if [ "$1" = "--version" ]; then echo "fake 1.0"; exit 0; fi
[ "$1" = "x" ] || exit 23
[ "$2" = "-y" ] || exit 24
[ "${3#-o}" != "$3" ] || exit 25
[ "$4" = "$EXPECT_ARCHIVE" ] || exit 26
exit 0
`)
	t.Setenv("EXPECT_ARCHIVE", "archive name;not-a-command.7z")
	if err := runExternalCommand(program, []string{"x", "-y", "-ooutput dir", "archive name;not-a-command.7z"}); err != nil {
		t.Fatalf("argument-array command failed: %v", err)
	}
	if err := runExternalCommand(program, []string{"bad"}); err == nil {
		t.Fatal("expected non-zero external command failure")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CodeExternalExecutionFailed {
		t.Fatalf("unexpected failure: %v", err)
	}
}

func TestExternalCommandTimeoutKillsProcess(t *testing.T) {
	program := testExternalCommand(t, `sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := runExternalCommandWithContext(ctx, program, nil)
	if time.Since(started) > 3*time.Second {
		t.Fatal("timed-out external command was not stopped promptly")
	}
	if err == nil {
		t.Fatal("expected external command timeout")
	}
	if typed, ok := err.(*Error); !ok || typed.Code != CodeExternalExecutionFailed {
		t.Fatalf("unexpected timeout failure: %v", err)
	}
}

func TestBoundedExternalDiagnostic(t *testing.T) {
	diagnostic := &boundedDiagnostic{}
	if _, err := diagnostic.Write(bytes.Repeat([]byte("x"), externalDiagnosticLimit+100)); err != nil {
		t.Fatal(err)
	}
	if got := diagnostic.String(); len(got) <= externalDiagnosticLimit || !strings.Contains(got, "truncated") {
		t.Fatalf("diagnostic was not bounded/truncated: len=%d", len(got))
	}
}

func testExternalCommand(t *testing.T, script string) string {
	t.Helper()
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture is unavailable on Windows")
	}
	path := filepath.Join(t.TempDir(), "fake-extractor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
