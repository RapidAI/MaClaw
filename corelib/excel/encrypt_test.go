package excel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsEncryptedWorkbookFalseForPlainXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{{Name: "S", Rows: [][]WriteCell{{{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	got, err := IsEncryptedWorkbook(path)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("plain xlsx reported encrypted")
	}
}

func TestResolveReadablePathNeedsPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enc.xlsx")
	if err := os.WriteFile(path, append(oleMagic, make([]byte, 64)...), 0600); err != nil {
		t.Fatal(err)
	}
	// Truncated OLE is not a valid EncryptedPackage container.
	got, err := IsEncryptedWorkbook(path)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		if _, err := ResolveReadablePath(path, ""); err == nil {
			t.Fatal("encrypted workbook without password must fail")
		}
	}
}

func TestDecryptWorkbookDefaultIsFailClosed(t *testing.T) {
	if _, err := DecryptWorkbook("any.xlsx", "secret"); err != ErrEncryptedUnsupported {
		t.Fatalf("err = %v, want ErrEncryptedUnsupported", err)
	}
}

func TestResolveReadablePathPlainWorkbookUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{{Name: "S", Rows: [][]WriteCell{{{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveReadablePath(path, "unused")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %s want %s", got, path)
	}
}
