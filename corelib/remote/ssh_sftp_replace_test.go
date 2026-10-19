package remote

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceLocalFileOverwritesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := dest + ".maclawpart"
	if err := os.WriteFile(tmp, []byte("gpu84-report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceLocalFile(tmp, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "gpu84-report" {
		t.Fatalf("dest = %q", got)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("partial file remained: %v", err)
	}
}

func TestLocalDownloadDestStaysInsideDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "out")
	got, err := localDownloadDest(root, "/home/gpu84/out", "/home/gpu84/out/sub/report.pdf")
	if err != nil || got != filepath.Join(root, "sub", "report.pdf") {
		t.Fatalf("nested dest = %s err=%v", got, err)
	}
	if _, err := localDownloadDest(root, "/home/gpu84/out", "/home/gpu84/out"); !errors.Is(err, errSkipDownloadRoot) {
		t.Fatalf("walk root = %v", err)
	}
	if _, err := localDownloadDest(root, "/home/gpu84/out", "/home/gpu84/out/../../etc/passwd"); err == nil {
		t.Fatal("cleaned parent path must stay out of the destination")
	}
	if _, err := localDownloadDest(root, "/home/gpu84/out", "/etc/passwd"); err == nil {
		t.Fatal("absolute path outside the remote directory must be rejected")
	}
}
