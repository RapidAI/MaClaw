package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessConnectionDSNReadOnlyByDefault(t *testing.T) {
	dsn := accessConnectionDSN(Profile{Type: SourceAccess, FilePath: `C:\data\x.mdb`, ReadOnly: true}, "")
	if !strings.Contains(dsn, "ReadOnly=1") {
		t.Fatalf("read-only dsn = %s", dsn)
	}
	write := accessConnectionDSN(Profile{Type: SourceAccess, FilePath: `C:\data\x.mdb`, ReadOnly: false, WriteEnabled: true}, "pw")
	if strings.Contains(strings.ToLower(write), "readonly=1") {
		t.Fatalf("write dsn must not force ReadOnly: %s", write)
	}
	if !strings.Contains(write, "PWD=pw;") {
		t.Fatalf("secret must bind via PWD: %s", write)
	}
}

func TestAccessLockSidecar(t *testing.T) {
	if got := accessLockSidecar(`C:\db\app.mdb`); !strings.HasSuffix(strings.ToLower(got), ".ldb") {
		t.Fatalf("mdb sidecar = %s", got)
	}
	if got := accessLockSidecar(`C:\db\app.accdb`); !strings.HasSuffix(strings.ToLower(got), ".laccdb") {
		t.Fatalf("accdb sidecar = %s", got)
	}
}

func TestPrepareAccessWriteCreatesBackupAndRejectsLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.mdb")
	if err := os.WriteFile(path, []byte("mdb-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareAccessWrite(path); err != nil {
		t.Fatal(err)
	}
	bak := path + accessPrewriteBackupSuffix
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mdb-bytes" {
		t.Fatalf("backup = %q", got)
	}
	if err := os.WriteFile(accessLockSidecar(path), []byte("lock"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareAccessWrite(path); err != nil {
		t.Fatalf("own ACE lock sidecar must not fail-closed: %v", err)
	}
}

func TestPrepareAccessWriteMissingFile(t *testing.T) {
	err := prepareAccessWrite(filepath.Join(t.TempDir(), "missing.mdb"))
	if err == nil || !strings.Contains(err.Error(), "connection") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareAccessWriteRejectsInsufficientDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.mdb")
	if err := os.WriteFile(path, []byte("mdb-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	original := accessDiskAvailable
	t.Cleanup(func() { accessDiskAvailable = original })
	accessDiskAvailable = func(string) (int64, error) { return 0, nil }
	err := prepareAccessWrite(path)
	if err == nil || !strings.Contains(err.Error(), "quota_exceeded") {
		t.Fatalf("disk-full err = %v", err)
	}
}
